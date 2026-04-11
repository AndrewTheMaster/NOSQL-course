package main

import (
	"encoding/json"
	"net/http"
)

type healthResponse struct {
	Status string `json:"status"`
}

// GET /health
func healthHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Возвращаем существующую Cookie без обновления TTL
	if cookie, err := r.Cookie(sessionCookieName); err == nil {
		http.SetCookie(w, &http.Cookie{
			Name:     sessionCookieName,
			Value:    cookie.Value,
			Path:     "/",
			HttpOnly: true,
			MaxAge:   sessionTTLSeconds,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(healthResponse{Status: "ok"})
}

// POST /session
func sessionHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx := r.Context()

	cookie, err := r.Cookie(sessionCookieName)
	if err != nil || !isValidSessionID(cookie.Value) {
		sid, err := createNewSession(ctx)
		if err != nil {
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
		setSessionCookie(w, sid)
		w.WriteHeader(http.StatusCreated)
		return
	}

	updated, err := touchSession(ctx, cookie.Value)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	if !updated {
		sid, err := createNewSession(ctx)
		if err != nil {
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
		setSessionCookie(w, sid)
		w.WriteHeader(http.StatusCreated)
		return
	}

	setSessionCookie(w, cookie.Value)
	w.WriteHeader(http.StatusOK)
}
