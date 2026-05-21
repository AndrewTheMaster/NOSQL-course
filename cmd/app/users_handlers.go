package main

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"golang.org/x/crypto/bcrypt"
)

type createUserRequest struct {
	FullName string `json:"full_name"`
	Username string `json:"username"`
	Password string `json:"password"`
}

type userPublicResponse struct {
	ID       string `json:"id"`
	FullName string `json:"full_name"`
	Username string `json:"username"`
}

// GET /users (список), POST /users (регистрация)
func usersRootHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		listUsersHandler(w, r)
	case http.MethodPost:
		createUserHandler(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// GET /users/{id}, GET /users/{id}/events
func usersSubHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/users/")
	path = strings.Trim(path, "/")
	parts := strings.Split(path, "/")

	switch {
	case len(parts) == 1 && parts[0] != "":
		getUserByIDHandler(w, r, parts[0])
	case len(parts) == 2 && parts[1] == "events" && parts[0] != "":
		listUserEventsHandler(w, r, parts[0])
	default:
		http.NotFound(w, r)
	}
}

func createUserHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req createUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		refreshExistingSession(ctx, w, r)
		writeJSON(w, http.StatusBadRequest, invalidFieldMessage("full_name"))
		return
	}

	if req.FullName == "" {
		refreshExistingSession(ctx, w, r)
		writeJSON(w, http.StatusBadRequest, invalidFieldMessage("full_name"))
		return
	}
	if req.Username == "" {
		refreshExistingSession(ctx, w, r)
		writeJSON(w, http.StatusBadRequest, invalidFieldMessage("username"))
		return
	}
	if req.Password == "" {
		refreshExistingSession(ctx, w, r)
		writeJSON(w, http.StatusBadRequest, invalidFieldMessage("password"))
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	usersCol := mongoDB.Collection("users")
	result, err := usersCol.InsertOne(ctx, User{
		FullName:     req.FullName,
		Username:     req.Username,
		PasswordHash: string(hash),
	})
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			refreshExistingSession(ctx, w, r)
			writeJSON(w, http.StatusConflict, map[string]string{"message": "user already exists"})
			return
		}
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	userID := result.InsertedID.(primitive.ObjectID).Hex()

	sid, err := createNewSessionWithUserID(ctx, userID)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	setSessionCookie(w, sid)
	w.WriteHeader(http.StatusCreated)
}

func listUsersHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	touchSessionCookie(w, r)

	q := r.URL.Query()

	if v := q.Get("limit"); v != "" {
		if _, err := strconv.ParseUint(v, 10, 64); err != nil {
			writeJSON(w, http.StatusBadRequest, invalidFieldMessage("limit"))
			return
		}
	}
	if v := q.Get("offset"); v != "" {
		if _, err := strconv.ParseUint(v, 10, 64); err != nil {
			writeJSON(w, http.StatusBadRequest, invalidFieldMessage("offset"))
			return
		}
	}

	limit, _ := strconv.ParseUint(q.Get("limit"), 10, 64)
	offset, _ := strconv.ParseUint(q.Get("offset"), 10, 64)

	filter := bson.M{}
	if id := q.Get("id"); id != "" {
		oid, err := primitive.ObjectIDFromHex(id)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, invalidFieldMessage("id"))
			return
		}
		filter["_id"] = oid
	}
	if name := q.Get("name"); name != "" {
		filter["full_name"] = bson.M{"$regex": name, "$options": "i"}
	}

	opts := options.Find().SetSkip(int64(offset))
	if limit > 0 {
		opts.SetLimit(int64(limit))
	}

	cur, err := mongoDB.Collection("users").Find(ctx, filter, opts)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	defer cur.Close(ctx)

	var users []User
	if err := cur.All(ctx, &users); err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	out := make([]userPublicResponse, 0, len(users))
	for _, u := range users {
		out = append(out, userPublicResponse{
			ID:       u.ID.Hex(),
			FullName: u.FullName,
			Username: u.Username,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"users": out,
		"count": len(out),
	})
}

func getUserByIDHandler(w http.ResponseWriter, r *http.Request, idStr string) {
	ctx := r.Context()
	touchSessionCookie(w, r)

	oid, err := primitive.ObjectIDFromHex(idStr)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "Not found"})
		return
	}

	var u User
	err = mongoDB.Collection("users").FindOne(ctx, bson.M{"_id": oid}).Decode(&u)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			writeJSON(w, http.StatusNotFound, map[string]string{"message": "Not found"})
			return
		}
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, userPublicResponse{
		ID:       u.ID.Hex(),
		FullName: u.FullName,
		Username: u.Username,
	})
}

func listUserEventsHandler(w http.ResponseWriter, r *http.Request, userIDStr string) {
	ctx := r.Context()
	touchSessionCookie(w, r)

	oid, err := primitive.ObjectIDFromHex(userIDStr)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "User not found"})
		return
	}

	n, err := mongoDB.Collection("users").CountDocuments(ctx, bson.M{"_id": oid})
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	if n == 0 {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "User not found"})
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

	filter, err := buildEventListFilter(ctx, q, oid.Hex())
	if err != nil {
		writeJSON(w, http.StatusBadRequest, invalidFieldMessage(err.Error()))
		return
	}

	findOpts := options.Find().SetSkip(offset)
	if limit > 0 {
		findOpts.SetLimit(limit)
	}

	cursor, err := mongoDB.Collection("events").Find(ctx, filter, findOpts)
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
	}{Events: make([]eventResponse, 0, len(events))}

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
