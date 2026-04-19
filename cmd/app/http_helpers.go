package main

import (
	"encoding/json"
	"net/http"
)

func invalidFieldMessage(field string) map[string]string {
	return map[string]string{"message": `invalid "` + field + `" field`}
}

func writeInvalidField(w http.ResponseWriter, field string) {
	writeJSON(w, http.StatusBadRequest, invalidFieldMessage(field))
}

func touchSessionCookie(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(sessionCookieName); err == nil && isValidSessionID(cookie.Value) {
		http.SetCookie(w, &http.Cookie{
			Name:     sessionCookieName,
			Value:    cookie.Value,
			Path:     "/",
			HttpOnly: true,
			MaxAge:   sessionTTLSeconds,
		})
	}
}

func decodeJSONMap(r *http.Request) (map[string]json.RawMessage, error) {
	var m map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&m); err != nil {
		return nil, err
	}
	return m, nil
}
