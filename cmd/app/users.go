package main

import (
	"encoding/json"
	"net/http"

	"go.mongodb.org/mongo-driver/mongo"
	"golang.org/x/crypto/bcrypt"
)

type createUserRequest struct {
	FullName string `json:"full_name"`
	Username string `json:"username"`
	Password string `json:"password"`
}

// POST /users
func usersHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx := r.Context()

	var req createUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		refreshExistingSession(ctx, w, r)
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": `invalid "full_name" field`})
		return
	}

	if req.FullName == "" {
		refreshExistingSession(ctx, w, r)
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": `invalid "full_name" field`})
		return
	}
	if req.Username == "" {
		refreshExistingSession(ctx, w, r)
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": `invalid "username" field`})
		return
	}
	if req.Password == "" {
		refreshExistingSession(ctx, w, r)
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": `invalid "password" field`})
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

	userID := result.InsertedID.(interface{ Hex() string }).Hex()

	// При успешной регистрации всегда создаём новую сессию с user_id
	sid, err := createNewSessionWithUserID(ctx, userID)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	setSessionCookie(w, sid)
	w.WriteHeader(http.StatusCreated)
}
