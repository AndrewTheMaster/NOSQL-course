package main

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gocql/gocql"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

// eventReviewsTTLSeconds задаётся из APP_EVENT_REVIEWS_TTL в main.
var eventReviewsTTLSeconds int

type reviewSummary struct {
	Count  int     `json:"count"`
	Rating float64 `json:"rating"`
}

type reviewResponse struct {
	ID        string `json:"id"`
	EventID   string `json:"event_id"`
	Comment   string `json:"comment"`
	CreatedAt string `json:"created_at"`
	CreatedBy string `json:"created_by"`
	Rating    int    `json:"rating"`
	UpdatedAt string `json:"updated_at"`
}

type createReviewRequest struct {
	Comment string `json:"comment"`
	Rating  int    `json:"rating"`
}

// Ключ Redis как в автогрейдере ndbx: event:{md5(title)}:reviews (HASH: count, rating).
func redisEventReviewsKey(title string) string {
	h := md5.Sum([]byte(title))
	return "event:" + hex.EncodeToString(h[:]) + ":reviews"
}

func formatRatingRedis(r float64) string {
	return strconv.FormatFloat(roundRating1(r), 'f', 1, 64)
}

func writeReviewsCache(ctx context.Context, key string, summary reviewSummary) error {
	pipe := redisClient.TxPipeline()
	pipe.HSet(ctx, key,
		"count", strconv.Itoa(summary.Count),
		"rating", formatRatingRedis(summary.Rating),
	)
	pipe.Expire(ctx, key, time.Duration(eventReviewsTTLSeconds)*time.Second)
	_, err := pipe.Exec(ctx)
	return err
}

func queryIncludeReviews(include string) bool {
	for _, p := range strings.Split(include, ",") {
		if strings.TrimSpace(p) == "reviews" {
			return true
		}
	}
	return false
}

func formatReviewTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}

func roundRating1(v float64) float64 {
	return math.Round(v*10) / 10
}

func getReviewsForTitle(ctx context.Context, title string) (reviewSummary, error) {
	key := redisEventReviewsKey(title)

	n, err := redisClient.Exists(ctx, key).Result()
	if err != nil {
		return reviewSummary{}, err
	}
	if n == 1 {
		vals, err := redisClient.HGetAll(ctx, key).Result()
		if err != nil {
			return reviewSummary{}, err
		}
		count, _ := strconv.Atoi(vals["count"])
		rating, _ := strconv.ParseFloat(vals["rating"], 64)
		return reviewSummary{Count: count, Rating: rating}, nil
	}

	summary, hasRows, err := aggregateReviewsFromCassandra(ctx, title)
	if err != nil {
		return reviewSummary{}, err
	}

	if hasRows && summary.Count > 0 {
		if err := writeReviewsCache(ctx, key, summary); err != nil {
			return reviewSummary{}, err
		}
	}

	return summary, nil
}

func aggregateReviewsFromCassandra(ctx context.Context, title string) (reviewSummary, bool, error) {
	cur, err := mongoDB.Collection("events").Find(ctx, bson.M{"title": title})
	if err != nil {
		return reviewSummary{}, false, err
	}
	defer cur.Close(ctx)

	var total int
	var sum int
	hasRows := false

	for cur.Next(ctx) {
		var ev Event
		if err := cur.Decode(&ev); err != nil {
			return reviewSummary{}, false, err
		}
		eid := ev.ID.Hex()
		q := cassSession.Query(
			`SELECT rating FROM event_reviews WHERE event_id = ?`,
			eid,
		).WithContext(ctx).Consistency(cassConsistency)

		iter := q.Iter()
		var rating int8
		for iter.Scan(&rating) {
			hasRows = true
			total++
			sum += int(rating)
		}
		if err := iter.Close(); err != nil {
			return reviewSummary{}, false, err
		}
	}
	if err := cur.Err(); err != nil {
		return reviewSummary{}, false, err
	}
	if total == 0 {
		return reviewSummary{Count: 0, Rating: 0}, hasRows, nil
	}
	avg := roundRating1(float64(sum) / float64(total))
	return reviewSummary{Count: total, Rating: avg}, hasRows, nil
}

func refreshReviewsCacheForTitle(ctx context.Context, title string) error {
	summary, hasRows, err := aggregateReviewsFromCassandra(ctx, title)
	if err != nil {
		return err
	}
	key := redisEventReviewsKey(title)
	if !hasRows || summary.Count == 0 {
		return redisClient.Del(ctx, key).Err()
	}
	return writeReviewsCache(ctx, key, summary)
}

func findEventByID(ctx context.Context, idStr string) (Event, error) {
	oid, err := primitive.ObjectIDFromHex(idStr)
	if err != nil {
		return Event{}, mongo.ErrNoDocuments
	}
	var ev Event
	err = mongoDB.Collection("events").FindOne(ctx, bson.M{"_id": oid}).Decode(&ev)
	return ev, err
}

func validateCreateReviewBody(req createReviewRequest) (field string, ok bool) {
	if req.Rating < 1 || req.Rating > 5 {
		return "rating", false
	}
	if req.Comment == "" {
		return "comment", false
	}
	if utf8.RuneCountInString(req.Comment) > 300 {
		return "comment", false
	}
	return "", true
}

func reviewExistsForUser(ctx context.Context, eventID, userID string) (bool, error) {
	var id gocql.UUID
	err := cassSession.Query(
		`SELECT id FROM event_reviews WHERE event_id = ? AND created_by = ?`,
		eventID, userID,
	).WithContext(ctx).Consistency(cassConsistency).Scan(&id)
	if err == gocql.ErrNotFound {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func insertReview(ctx context.Context, eventID, userID string, id gocql.UUID, rating int8, comment string, createdAt, updatedAt time.Time) error {
	if err := cassSession.Query(
		`INSERT INTO event_reviews (event_id, created_by, id, rating, comment, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		eventID, userID, id, rating, comment, createdAt, updatedAt,
	).WithContext(ctx).Consistency(cassConsistency).Exec(); err != nil {
		return err
	}
	return cassSession.Query(
		`INSERT INTO event_reviews_timeline (event_id, created_at, id, rating, comment, created_by, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		eventID, createdAt, id, rating, comment, userID, updatedAt,
	).WithContext(ctx).Consistency(cassConsistency).Exec()
}

func createEventReviewHandler(w http.ResponseWriter, r *http.Request, idStr string) {
	ctx := r.Context()

	cookie, err := r.Cookie(sessionCookieName)
	if err != nil || !isValidSessionID(cookie.Value) {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	userID, err := getSessionUserID(ctx, cookie.Value)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	if userID == "" {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	ev, err := findEventByID(ctx, idStr)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			refreshExistingSession(ctx, w, r)
			writeJSON(w, http.StatusNotFound, map[string]string{"message": "Event not found"})
			return
		}
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	var req createReviewRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		refreshExistingSession(ctx, w, r)
		writeJSON(w, http.StatusBadRequest, invalidFieldMessage("comment"))
		return
	}
	if field, ok := validateCreateReviewBody(req); !ok {
		refreshExistingSession(ctx, w, r)
		writeJSON(w, http.StatusBadRequest, invalidFieldMessage(field))
		return
	}

	eventID := ev.ID.Hex()
	exists, err := reviewExistsForUser(ctx, eventID, userID)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	if exists {
		refreshExistingSession(ctx, w, r)
		writeJSON(w, http.StatusConflict, map[string]string{"message": "Already exists"})
		return
	}

	reviewID := gocql.TimeUUID()
	now := time.Now().UTC()
	if err := insertReview(ctx, eventID, userID, reviewID, int8(req.Rating), req.Comment, now, now); err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	if err := refreshReviewsCacheForTitle(ctx, ev.Title); err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	refreshExistingSession(ctx, w, r)
	writeJSON(w, http.StatusCreated, map[string]string{"id": reviewID.String()})
}

func listEventReviewsHandler(w http.ResponseWriter, r *http.Request, idStr string) {
	ctx := r.Context()
	touchSessionCookie(w, r)

	ev, err := findEventByID(ctx, idStr)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			writeJSON(w, http.StatusNotFound, map[string]string{"message": "Event not found"})
			return
		}
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	q := r.URL.Query()
	var limit, offset int64
	if v := q.Get("limit"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 0 {
			writeJSON(w, http.StatusBadRequest, invalidFieldMessage("limit"))
			return
		}
		limit = n
	}
	if v := q.Get("offset"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 0 {
			writeJSON(w, http.StatusBadRequest, invalidFieldMessage("offset"))
			return
		}
		offset = n
	}

	eventID := ev.ID.Hex()

	all, err := loadReviewsForEvent(ctx, eventID)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	start := int(offset)
	if start > len(all) {
		start = len(all)
	}
	end := len(all)
	if limit > 0 {
		end = start + int(limit)
		if end > len(all) {
			end = len(all)
		}
	}
	page := all[start:end]

	resp := struct {
		Reviews []reviewResponse `json:"reviews"`
		Count   int              `json:"count"`
	}{
		Reviews: make([]reviewResponse, 0, len(page)),
		Count:   len(page),
	}
	resp.Reviews = append(resp.Reviews, page...)

	writeJSON(w, http.StatusOK, resp)
}

func loadReviewsForEvent(ctx context.Context, eventID string) ([]reviewResponse, error) {
	fetchLimit := 10000
	q := cassSession.Query(
		`SELECT id, rating, comment, created_by, created_at, updated_at FROM event_reviews_timeline WHERE event_id = ? LIMIT ?`,
		eventID, fetchLimit,
	).WithContext(ctx).Consistency(cassConsistency)

	iter := q.Iter()
	var out []reviewResponse
	var id gocql.UUID
	var rating int8
	var comment, createdBy string
	var createdAt, updatedAt time.Time

	for iter.Scan(&id, &rating, &comment, &createdBy, &createdAt, &updatedAt) {
		out = append(out, reviewResponse{
			ID:        id.String(),
			EventID:   eventID,
			Comment:   comment,
			CreatedAt: formatReviewTime(createdAt),
			CreatedBy: createdBy,
			Rating:    int(rating),
			UpdatedAt: formatReviewTime(updatedAt),
		})
	}
	if err := iter.Close(); err != nil {
		return nil, err
	}
	return out, nil
}

func patchEventReviewHandler(w http.ResponseWriter, r *http.Request, eventIDStr, reviewIDStr string) {
	ctx := r.Context()

	cookie, err := r.Cookie(sessionCookieName)
	if err != nil || !isValidSessionID(cookie.Value) {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	userID, err := getSessionUserID(ctx, cookie.Value)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	if userID == "" {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	ev, err := findEventByID(ctx, eventIDStr)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			refreshExistingSession(ctx, w, r)
			writeJSON(w, http.StatusNotFound, map[string]string{"message": "Event not found"})
			return
		}
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	reviewUUID, err := gocql.ParseUUID(reviewIDStr)
	if err != nil {
		refreshExistingSession(ctx, w, r)
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "Event not found"})
		return
	}

	eventID := ev.ID.Hex()

	var raw map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		refreshExistingSession(ctx, w, r)
		writeJSON(w, http.StatusBadRequest, invalidFieldMessage("rating"))
		return
	}

	var existingID gocql.UUID
	var rating int8
	var comment string
	var createdAt, updatedAt time.Time
	err = cassSession.Query(
		`SELECT id, rating, comment, created_at, updated_at FROM event_reviews WHERE event_id = ? AND created_by = ?`,
		eventID, userID,
	).WithContext(ctx).Consistency(cassConsistency).Scan(&existingID, &rating, &comment, &createdAt, &updatedAt)
	if err == gocql.ErrNotFound || existingID != reviewUUID {
		refreshExistingSession(ctx, w, r)
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "Event not found"})
		return
	}
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	updated := false
	if v, ok := raw["rating"]; ok {
		var rVal int
		if err := json.Unmarshal(v, &rVal); err != nil || rVal < 1 || rVal > 5 {
			refreshExistingSession(ctx, w, r)
			writeJSON(w, http.StatusBadRequest, invalidFieldMessage("rating"))
			return
		}
		rating = int8(rVal)
		updated = true
	}
	if v, ok := raw["comment"]; ok {
		var cVal string
		if err := json.Unmarshal(v, &cVal); err != nil {
			refreshExistingSession(ctx, w, r)
			writeJSON(w, http.StatusBadRequest, invalidFieldMessage("comment"))
			return
		}
		if utf8.RuneCountInString(cVal) > 300 {
			refreshExistingSession(ctx, w, r)
			writeJSON(w, http.StatusBadRequest, invalidFieldMessage("comment"))
			return
		}
		comment = cVal
		updated = true
	}
	if !updated {
		for k := range raw {
			refreshExistingSession(ctx, w, r)
			writeJSON(w, http.StatusBadRequest, invalidFieldMessage(k))
			return
		}
	}

	now := time.Now().UTC()
	updatedAt = now

	if err := cassSession.Query(
		`UPDATE event_reviews SET rating = ?, comment = ?, updated_at = ? WHERE event_id = ? AND created_by = ?`,
		rating, comment, updatedAt, eventID, userID,
	).WithContext(ctx).Consistency(cassConsistency).Exec(); err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	if err := cassSession.Query(
		`UPDATE event_reviews_timeline SET rating = ?, comment = ?, updated_at = ? WHERE event_id = ? AND created_at = ? AND id = ?`,
		rating, comment, updatedAt, eventID, createdAt, reviewUUID,
	).WithContext(ctx).Consistency(cassConsistency).Exec(); err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	if err := refreshReviewsCacheForTitle(ctx, ev.Title); err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	refreshExistingSession(ctx, w, r)
	w.WriteHeader(http.StatusNoContent)
}
