package main

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

const redisRecommendationsField = "events"

var recommendationsTTLSeconds int

func redisUserRecommendationsKey(userID string) string {
	return "user:" + userID + ":recomms"
}

type recommendationsResponse struct {
	Events []eventResponse `json:"events"`
}

// GET /recommendations
func recommendationsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

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

	refreshExistingSession(ctx, w, r)

	events, err := getUserRecommendations(ctx, userID)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, recommendationsResponse{Events: events})
}

func getUserRecommendations(ctx context.Context, userID string) ([]eventResponse, error) {
	key := redisUserRecommendationsKey(userID)

	cached, err := redisClient.HGet(ctx, key, redisRecommendationsField).Result()
	if err == nil {
		var events []eventResponse
		if err := json.Unmarshal([]byte(cached), &events); err == nil {
			return events, nil
		}
	}

	events, err := buildRecommendations(ctx, userID)
	if err != nil {
		return nil, err
	}

	payload, err := json.Marshal(events)
	if err != nil {
		return nil, err
	}

	pipe := redisClient.TxPipeline()
	pipe.HSet(ctx, key, redisRecommendationsField, string(payload))
	pipe.Expire(ctx, key, time.Duration(recommendationsTTLSeconds)*time.Second)
	if _, err := pipe.Exec(ctx); err != nil {
		return nil, err
	}

	return events, nil
}

func buildRecommendations(ctx context.Context, userID string) ([]eventResponse, error) {
	scores, err := getRecommendedEventScores(ctx, userID)
	if err != nil {
		return nil, err
	}
	if len(scores) == 0 {
		return []eventResponse{}, nil
	}

	scoreByID := make(map[string]int, len(scores))
	ids := make([]primitive.ObjectID, 0, len(scores))
	for _, s := range scores {
		oid, err := primitive.ObjectIDFromHex(s.ID)
		if err != nil {
			continue
		}
		scoreByID[s.ID] = s.Score
		ids = append(ids, oid)
	}
	if len(ids) == 0 {
		return []eventResponse{}, nil
	}

	cur, err := mongoDB.Collection("events").Find(ctx, bson.M{"_id": bson.M{"$in": ids}})
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)

	var found []Event
	if err := cur.All(ctx, &found); err != nil {
		return nil, err
	}

	type titlePick struct {
		event Event
		score int
	}
	byTitle := make(map[string]titlePick)
	for _, ev := range found {
		id := ev.ID.Hex()
		score := scoreByID[id]
		pick, ok := byTitle[ev.Title]
		if !ok {
			byTitle[ev.Title] = titlePick{event: ev, score: score}
			continue
		}
		if ev.StartedAt < pick.event.StartedAt {
			if pick.score > score {
				score = pick.score
			}
			byTitle[ev.Title] = titlePick{event: ev, score: score}
			continue
		}
		if score > pick.score {
			byTitle[ev.Title] = titlePick{event: pick.event, score: score}
		}
	}

	picks := make([]titlePick, 0, len(byTitle))
	for _, pick := range byTitle {
		picks = append(picks, pick)
	}

	sort.Slice(picks, func(i, j int) bool {
		if picks[i].score != picks[j].score {
			return picks[i].score > picks[j].score
		}
		if picks[i].event.StartedAt != picks[j].event.StartedAt {
			return picks[i].event.StartedAt < picks[j].event.StartedAt
		}
		return picks[i].event.ID.Hex() < picks[j].event.ID.Hex()
	})

	out := make([]eventResponse, 0, len(picks))
	for _, pick := range picks {
		out = append(out, eventToResponse(pick.event))
	}
	return out, nil
}
