package main

import (
	"encoding/json"
	"net/http"

	"go.mongodb.org/mongo-driver/bson"
	"golang.org/x/crypto/bcrypt"
)

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// POST /auth/login
func authLoginHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx := r.Context()

	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Username == "" || req.Password == "" {
		refreshExistingSession(ctx, w, r)
		writeJSON(w, http.StatusUnauthorized, map[string]string{"message": "invalid credentials"})
		return
	}

	usersCol := mongoDB.Collection("users")
	var user User
	if err := usersCol.FindOne(ctx, bson.M{"username": req.Username}).Decode(&user); err != nil {
		refreshExistingSession(ctx, w, r)
		writeJSON(w, http.StatusUnauthorized, map[string]string{"message": "invalid credentials"})
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		refreshExistingSession(ctx, w, r)
		writeJSON(w, http.StatusUnauthorized, map[string]string{"message": "invalid credentials"})
		return
	}

	userID := user.ID.Hex()

	// Привязываем user_id к активной сессии или создаём новую
	var sid string
	cookie, err := r.Cookie(sessionCookieName)
	if err == nil && isValidSessionID(cookie.Value) {
		ok, err := setSessionUserID(ctx, cookie.Value, userID)
		if err == nil && ok {
			sid = cookie.Value
		}
	}
	if sid == "" {
		sid, err = createNewSessionWithUserID(ctx, userID)
		if err != nil {
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
	}

	setSessionCookie(w, sid)
	w.WriteHeader(http.StatusNoContent)
}

// POST /auth/logout
func authLogoutHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx := r.Context()

	cookie, err := r.Cookie(sessionCookieName)
	if err != nil || !isValidSessionID(cookie.Value) {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	_ = deleteSession(ctx, cookie.Value)

	// Max-Age=0 удаляет куки на стороне клиента
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    cookie.Value,
		Path:     "/",
		HttpOnly: true,
		MaxAge:   0,
	})
	w.WriteHeader(http.StatusNoContent)
}
