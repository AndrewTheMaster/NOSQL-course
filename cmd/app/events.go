package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var eventCategories = map[string]struct{}{
	"meetup":     {},
	"concert":    {},
	"exhibition": {},
	"party":      {},
	"other":      {},
}

type createEventRequest struct {
	Title       string `json:"title"`
	Address     string `json:"address"`
	StartedAt   string `json:"started_at"`
	FinishedAt  string `json:"finished_at"`
	Description string `json:"description"`
}

type eventResponse struct {
	ID          string          `json:"id"`
	Title       string          `json:"title"`
	Category    string          `json:"category"`
	Price       uint64          `json:"price"`
	Description string          `json:"description"`
	Location    EventLocation   `json:"location"`
	CreatedAt   string          `json:"created_at"`
	CreatedBy   string          `json:"created_by"`
	StartedAt   string          `json:"started_at"`
	FinishedAt  string          `json:"finished_at"`
	Reactions   *reactionCounts `json:"reactions,omitempty"`
}

func eventToResponse(e Event) eventResponse {
	cat := e.Category
	if cat == "" {
		cat = "other"
	}
	return eventResponse{
		ID:          e.ID.Hex(),
		Title:       e.Title,
		Category:    cat,
		Price:       e.Price,
		Description: e.Description,
		Location:    e.Location,
		CreatedAt:   e.CreatedAt,
		CreatedBy:   e.CreatedBy,
		StartedAt:   e.StartedAt,
		FinishedAt:  e.FinishedAt,
	}
}

// GET /events и POST /events
func eventsRootHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		listEventsHandler(w, r)
	case http.MethodPost:
		createEventHandler(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// GET/PATCH /events/{id}, POST /events/{id}/like, POST /events/{id}/dislike
func eventsByIDHandler(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/events/")
	rest = strings.Trim(rest, "/")
	parts := strings.Split(rest, "/")
	if len(parts) == 0 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}
	id := parts[0]

	if len(parts) == 1 {
		switch r.Method {
		case http.MethodGet:
			getEventByIDHandler(w, r, id)
		case http.MethodPatch:
			patchEventHandler(w, r, id)
		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
		return
	}

	if len(parts) == 2 {
		switch parts[1] {
		case "like":
			if r.Method != http.MethodPost {
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
				return
			}
			postEventReactionHandler(w, r, id, 1, false)
		case "dislike":
			if r.Method != http.MethodPost {
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
				return
			}
			postEventReactionHandler(w, r, id, -1, true)
		default:
			http.NotFound(w, r)
		}
		return
	}

	http.NotFound(w, r)
}

// POST /events — создание события (только для авторизованных)
func createEventHandler(w http.ResponseWriter, r *http.Request) {
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
		refreshExistingSession(ctx, w, r)
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	refreshExistingSession(ctx, w, r)

	var req createEventRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, invalidFieldMessage("title"))
		return
	}

	if req.Title == "" {
		writeJSON(w, http.StatusBadRequest, invalidFieldMessage("title"))
		return
	}
	if req.Address == "" {
		writeJSON(w, http.StatusBadRequest, invalidFieldMessage("address"))
		return
	}
	if req.StartedAt == "" {
		writeJSON(w, http.StatusBadRequest, invalidFieldMessage("started_at"))
		return
	}
	if _, err := time.Parse(time.RFC3339, req.StartedAt); err != nil {
		writeJSON(w, http.StatusBadRequest, invalidFieldMessage("started_at"))
		return
	}
	if req.FinishedAt == "" {
		writeJSON(w, http.StatusBadRequest, invalidFieldMessage("finished_at"))
		return
	}
	if _, err := time.Parse(time.RFC3339, req.FinishedAt); err != nil {
		writeJSON(w, http.StatusBadRequest, invalidFieldMessage("finished_at"))
		return
	}

	event := Event{
		Title:       req.Title,
		Description: req.Description,
		Category:    "other",
		Price:       0,
		Location:    EventLocation{Address: req.Address},
		CreatedAt:   time.Now().Format(time.RFC3339),
		CreatedBy:   userID,
		StartedAt:   req.StartedAt,
		FinishedAt:  req.FinishedAt,
	}

	eventsCol := mongoDB.Collection("events")

	n, err := eventsCol.CountDocuments(ctx, bson.M{"title": req.Title})
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	if n > 0 {
		writeJSON(w, http.StatusConflict, map[string]string{"message": "event already exists"})
		return
	}

	result, err := eventsCol.InsertOne(ctx, event)
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			writeJSON(w, http.StatusConflict, map[string]string{"message": "event already exists"})
			return
		}
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	eventID := result.InsertedID.(primitive.ObjectID).Hex()
	writeJSON(w, http.StatusCreated, map[string]string{"id": eventID})
}

func getEventByIDHandler(w http.ResponseWriter, r *http.Request, idStr string) {
	ctx := r.Context()
	touchSessionCookie(w, r)

	oid, err := primitive.ObjectIDFromHex(idStr)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "Not found"})
		return
	}

	var ev Event
	err = mongoDB.Collection("events").FindOne(ctx, bson.M{"_id": oid}).Decode(&ev)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			writeJSON(w, http.StatusNotFound, map[string]string{"message": "Not found"})
			return
		}
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	includeRx := queryIncludeReactions(r.URL.Query().Get("include"))
	er := eventToResponse(ev)
	if includeRx {
		rc, err := getReactionsForTitle(ctx, ev.Title)
		if err != nil {
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
		er.Reactions = &reactionCounts{Likes: rc.Likes, Dislikes: rc.Dislikes}
	}

	writeJSON(w, http.StatusOK, er)
}

func postEventReactionHandler(w http.ResponseWriter, r *http.Request, idStr string, value int, clearCookieOn401 bool) {
	ctx := r.Context()

	cookie, err := r.Cookie(sessionCookieName)
	if err != nil || !isValidSessionID(cookie.Value) {
		if clearCookieOn401 {
			clearSessionCookie(w)
		}
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	userID, err := getSessionUserID(ctx, cookie.Value)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	if userID == "" {
		if clearCookieOn401 {
			clearSessionCookie(w)
		}
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	oid, err := primitive.ObjectIDFromHex(idStr)
	if err != nil {
		refreshExistingSession(ctx, w, r)
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "Event not found"})
		return
	}

	var ev Event
	err = mongoDB.Collection("events").FindOne(ctx, bson.M{"_id": oid}).Decode(&ev)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			refreshExistingSession(ctx, w, r)
			writeJSON(w, http.StatusNotFound, map[string]string{"message": "Event not found"})
			return
		}
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	if err := upsertEventReaction(ctx, ev.ID.Hex(), userID, value); err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	if err := refreshReactionsCacheForTitle(ctx, ev.Title); err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	refreshExistingSession(ctx, w, r)
	w.WriteHeader(http.StatusNoContent)
}

func patchEventHandler(w http.ResponseWriter, r *http.Request, idStr string) {
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
		refreshExistingSession(ctx, w, r)
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	refreshExistingSession(ctx, w, r)

	raw, err := decodeJSONMap(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, invalidFieldMessage("category"))
		return
	}

	var categoryPtr *string
	var pricePtr *uint64
	var cityPtr *string

	if rawMsg, ok := raw["category"]; ok {
		var s string
		if err := json.Unmarshal(rawMsg, &s); err != nil {
			writeJSON(w, http.StatusBadRequest, invalidFieldMessage("category"))
			return
		}
		if _, ok := eventCategories[s]; !ok {
			writeJSON(w, http.StatusBadRequest, invalidFieldMessage("category"))
			return
		}
		categoryPtr = &s
	}
	if rawMsg, ok := raw["price"]; ok {
		var n uint64
		if err := json.Unmarshal(rawMsg, &n); err != nil {
			writeJSON(w, http.StatusBadRequest, invalidFieldMessage("price"))
			return
		}
		pricePtr = &n
	}
	if rawMsg, ok := raw["city"]; ok {
		var s string
		if err := json.Unmarshal(rawMsg, &s); err != nil {
			writeJSON(w, http.StatusBadRequest, invalidFieldMessage("city"))
			return
		}
		cityPtr = &s
	}

	if categoryPtr == nil && pricePtr == nil && cityPtr == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	oid, err := primitive.ObjectIDFromHex(idStr)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{
			"message": "Not found. Be sure that event exists and you are the organizer",
		})
		return
	}

	var existing Event
	err = mongoDB.Collection("events").FindOne(ctx, bson.M{"_id": oid}).Decode(&existing)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			writeJSON(w, http.StatusNotFound, map[string]string{
				"message": "Not found. Be sure that event exists and you are the organizer",
			})
			return
		}
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	if existing.CreatedBy != userID {
		writeJSON(w, http.StatusNotFound, map[string]string{
			"message": "Not found. Be sure that event exists and you are the organizer",
		})
		return
	}

	setDoc := bson.M{}
	unsetDoc := bson.M{}
	if categoryPtr != nil {
		setDoc["category"] = *categoryPtr
	}
	if pricePtr != nil {
		setDoc["price"] = *pricePtr
	}
	if cityPtr != nil {
		if *cityPtr == "" {
			unsetDoc["location.city"] = ""
		} else {
			setDoc["location.city"] = *cityPtr
		}
	}

	update := bson.M{}
	if len(setDoc) > 0 {
		update["$set"] = setDoc
	}
	if len(unsetDoc) > 0 {
		update["$unset"] = unsetDoc
	}

	_, err = mongoDB.Collection("events").UpdateOne(ctx, bson.M{"_id": oid}, update)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// GET /events — список событий с фильтрацией и пагинацией
func listEventsHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	touchSessionCookie(w, r)

	q := r.URL.Query()

	filter, err := buildEventListFilter(ctx, q, "")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, invalidFieldMessage(err.Error()))
		return
	}

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

	findOpts := options.Find().SetSkip(offset)
	if limit > 0 {
		findOpts.SetLimit(limit)
	}

	eventsCol := mongoDB.Collection("events")
	cursor, err := eventsCol.Find(ctx, filter, findOpts)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	defer cursor.Close(ctx)

	var events []Event
	if err := cursor.All(ctx, &events); err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	includeRx := queryIncludeReactions(q.Get("include"))
	titleRx := map[string]reactionCounts{}

	resp := struct {
		Events []eventResponse `json:"events"`
		Count  int             `json:"count"`
	}{
		Events: make([]eventResponse, 0, len(events)),
	}

	for _, e := range events {
		er := eventToResponse(e)
		if includeRx {
			rc, ok := titleRx[e.Title]
			if !ok {
				var err error
				rc, err = getReactionsForTitle(ctx, e.Title)
				if err != nil {
					http.Error(w, "Internal server error", http.StatusInternalServerError)
					return
				}
				titleRx[e.Title] = rc
			}
			er.Reactions = &reactionCounts{Likes: rc.Likes, Dislikes: rc.Dislikes}
		}
		resp.Events = append(resp.Events, er)
	}
	resp.Count = len(resp.Events)

	writeJSON(w, http.StatusOK, resp)
}

func buildEventListFilter(ctx context.Context, q url.Values, fixedCreatedBy string) (bson.M, error) {
	get := q.Get

	filter := bson.M{}

	if v := get("id"); v != "" {
		oid, err := primitive.ObjectIDFromHex(v)
		if err != nil {
			return nil, errField("id")
		}
		filter["_id"] = oid
	}

	if v := get("title"); v != "" {
		filter["title"] = bson.M{"$regex": v, "$options": "i"}
	}

	if v := get("category"); v != "" {
		if _, ok := eventCategories[v]; !ok {
			return nil, errField("category")
		}
		filter["category"] = v
	}

	priceRange := bson.M{}
	if v := get("price_from"); v != "" {
		n, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			return nil, errField("price_from")
		}
		priceRange["$gte"] = n
	}
	if v := get("price_to"); v != "" {
		n, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			return nil, errField("price_to")
		}
		priceRange["$lte"] = n
	}
	if len(priceRange) > 0 {
		filter["price"] = priceRange
	}

	if v := get("city"); v != "" {
		filter["location.city"] = v
	}

	if v := get("address"); v != "" {
		filter["location.address"] = v
	}

	if fixedCreatedBy != "" {
		filter["created_by"] = fixedCreatedBy
	} else {
		if v := get("user_id"); v != "" {
			if _, err := primitive.ObjectIDFromHex(v); err != nil {
				return nil, errField("user_id")
			}
			filter["created_by"] = v
		} else if v := get("user"); v != "" {
			var u User
			err := mongoDB.Collection("users").FindOne(ctx, bson.M{"username": v}).Decode(&u)
			if err != nil {
				if err == mongo.ErrNoDocuments {
					filter["created_by"] = bson.M{"$in": bson.A{}}
				} else {
					return nil, errField("user")
				}
			} else {
				filter["created_by"] = u.ID.Hex()
			}
		}
	}

	df, dt := get("date_from"), get("date_to")
	if df != "" || dt != "" {
		var startBound time.Time
		var endExclusive time.Time
		var hasStart, hasEnd bool

		if df != "" {
			t, err := parseYYYYMMDDStrict(df)
			if err != nil {
				return nil, errField("date_from")
			}
			startBound = t
			hasStart = true
		}
		if dt != "" {
			t, err := parseYYYYMMDDStrict(dt)
			if err != nil {
				return nil, errField("date_to")
			}
			endExclusive = t.Add(24 * time.Hour)
			hasEnd = true
		}
		if hasStart && hasEnd && !startBound.Before(endExclusive) {
			return nil, errField("date_to")
		}

		started := bson.M{"$dateFromString": bson.M{"dateString": "$started_at"}}
		var parts bson.A
		if hasStart {
			parts = append(parts, bson.M{"$gte": bson.A{started, startBound}})
		}
		if hasEnd {
			parts = append(parts, bson.M{"$lt": bson.A{started, endExclusive}})
		}
		switch len(parts) {
		case 1:
			filter["$expr"] = parts[0]
		case 2:
			filter["$expr"] = bson.M{"$and": parts}
		}
	}

	return filter, nil
}

type fieldError string

func (e fieldError) Error() string { return string(e) }

func errField(name string) fieldError { return fieldError(name) }

func parseYYYYMMDDStrict(s string) (time.Time, error) {
	if len(s) != 8 {
		return time.Time{}, errField("date")
	}
	return time.ParseInLocation("20060102", s, time.UTC)
}
