package main

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type createEventRequest struct {
	Title       string `json:"title"`
	Address     string `json:"address"`
	StartedAt   string `json:"started_at"`
	FinishedAt  string `json:"finished_at"`
	Description string `json:"description"`
}

type eventResponse struct {
	ID          string        `json:"id"`
	Title       string        `json:"title"`
	Description string        `json:"description"`
	Location    EventLocation `json:"location"`
	CreatedAt   string        `json:"created_at"`
	CreatedBy   string        `json:"created_by"`
	StartedAt   string        `json:"started_at"`
	FinishedAt  string        `json:"finished_at"`
}

// GET /events и POST /events
func eventsHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		listEventsHandler(w, r)
	case http.MethodPost:
		createEventHandler(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// POST /events — создание события (только для авторизованных)
func createEventHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Проверяем сессию и авторизацию
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
		// Сессия есть, но user_id нет — обновляем TTL и возвращаем 401
		refreshExistingSession(ctx, w, r)
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	// Пользователь авторизован — сразу обновляем TTL
	refreshExistingSession(ctx, w, r)

	var req createEventRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": `invalid "title" field`})
		return
	}

	if req.Title == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": `invalid "title" field`})
		return
	}
	if req.Address == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": `invalid "address" field`})
		return
	}
	if req.StartedAt == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": `invalid "started_at" field`})
		return
	}
	if _, err := time.Parse(time.RFC3339, req.StartedAt); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": `invalid "started_at" field`})
		return
	}
	if req.FinishedAt == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": `invalid "finished_at" field`})
		return
	}
	if _, err := time.Parse(time.RFC3339, req.FinishedAt); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": `invalid "finished_at" field`})
		return
	}

	event := Event{
		Title:       req.Title,
		Description: req.Description,
		Location:    EventLocation{Address: req.Address},
		CreatedAt:   time.Now().Format(time.RFC3339),
		CreatedBy:   userID,
		StartedAt:   req.StartedAt,
		FinishedAt:  req.FinishedAt,
	}

	eventsCol := mongoDB.Collection("events")
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

// GET /events — список событий с фильтрацией и пагинацией
func listEventsHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// GET — только возвращаем существующую куку, без обновления TTL
	if cookie, err := r.Cookie(sessionCookieName); err == nil {
		http.SetCookie(w, &http.Cookie{
			Name:     sessionCookieName,
			Value:    cookie.Value,
			Path:     "/",
			HttpOnly: true,
			MaxAge:   sessionTTLSeconds,
		})
	}

	q := r.URL.Query()

	var limit, offset int
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"message": `invalid "limit" parameter`})
			return
		}
		limit = n
	}
	if v := q.Get("offset"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"message": `invalid "offset" parameter`})
			return
		}
		offset = n
	}

	filter := bson.M{}
	if title := q.Get("title"); title != "" {
		filter["title"] = bson.M{"$regex": title, "$options": "i"}
	}

	findOpts := options.Find().SetSkip(int64(offset))
	if limit > 0 {
		findOpts.SetLimit(int64(limit))
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

	resp := struct {
		Events []eventResponse `json:"events"`
		Count  int             `json:"count"`
	}{
		Events: make([]eventResponse, 0, len(events)),
	}

	for _, e := range events {
		resp.Events = append(resp.Events, eventResponse{
			ID:          e.ID.Hex(),
			Title:       e.Title,
			Description: e.Description,
			Location:    e.Location,
			CreatedAt:   e.CreatedAt,
			CreatedBy:   e.CreatedBy,
			StartedAt:   e.StartedAt,
			FinishedAt:  e.FinishedAt,
		})
	}
	resp.Count = len(resp.Events)

	writeJSON(w, http.StatusOK, resp)
}
