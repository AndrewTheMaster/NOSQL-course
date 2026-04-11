package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	sessionCookieName = "X-Session-Id"
	redisSessionKey   = "sid:%s"
	sessionIDBytes    = 16 // 128 bit
)

var (
	// Атомарное создание сессии без user_id
	createSessionScript = redis.NewScript(`
local key = KEYS[1]
if redis.call('EXISTS', key) == 1 then
  return 0
end
redis.call('HMSET', key, 'created_at', ARGV[1], 'updated_at', ARGV[1])
redis.call('EXPIRE', key, ARGV[2])
return 1
`)

	// Атомарное создание сессии с user_id
	createSessionWithUserIDScript = redis.NewScript(`
local key = KEYS[1]
if redis.call('EXISTS', key) == 1 then
  return 0
end
redis.call('HMSET', key, 'created_at', ARGV[1], 'updated_at', ARGV[1], 'user_id', ARGV[3])
redis.call('EXPIRE', key, ARGV[2])
return 1
`)

	// Обновление TTL без изменения user_id
	touchSessionScript = redis.NewScript(`
local key = KEYS[1]
if redis.call('EXISTS', key) == 0 then
  return 0
end
redis.call('HSET', key, 'updated_at', ARGV[1])
redis.call('EXPIRE', key, ARGV[2])
return 1
`)

	// Атомарная привязка user_id к существующей сессии
	setSessionUserIDScript = redis.NewScript(`
local key = KEYS[1]
if redis.call('EXISTS', key) == 0 then
  return 0
end
redis.call('HSET', key, 'user_id', ARGV[1], 'updated_at', ARGV[2])
redis.call('EXPIRE', key, ARGV[3])
return 1
`)
)

func generateSessionID() (string, error) {
	buf := make([]byte, sessionIDBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func isValidSessionID(s string) bool {
	if len(s) != sessionIDBytes*2 {
		return false
	}
	for _, r := range s {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}

func setSessionCookie(w http.ResponseWriter, sid string) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    sid,
		Path:     "/",
		HttpOnly: true,
		MaxAge:   sessionTTLSeconds,
	})
}

func createNewSession(ctx context.Context) (string, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	for i := 0; i < 3; i++ {
		sid, err := generateSessionID()
		if err != nil {
			return "", err
		}
		key := fmt.Sprintf(redisSessionKey, sid)
		res, err := createSessionScript.Run(ctx, redisClient, []string{key}, now, strconv.Itoa(sessionTTLSeconds)).Result()
		if err != nil {
			return "", err
		}
		if res.(int64) == 1 {
			return sid, nil
		}
	}
	return "", errors.New("failed to create unique session id")
}

func createNewSessionWithUserID(ctx context.Context, userID string) (string, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	for i := 0; i < 3; i++ {
		sid, err := generateSessionID()
		if err != nil {
			return "", err
		}
		key := fmt.Sprintf(redisSessionKey, sid)
		res, err := createSessionWithUserIDScript.Run(ctx, redisClient, []string{key}, now, strconv.Itoa(sessionTTLSeconds), userID).Result()
		if err != nil {
			return "", err
		}
		if res.(int64) == 1 {
			return sid, nil
		}
	}
	return "", errors.New("failed to create unique session id with user_id")
}

func touchSession(ctx context.Context, sid string) (bool, error) {
	key := fmt.Sprintf(redisSessionKey, sid)
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := touchSessionScript.Run(ctx, redisClient, []string{key}, now, strconv.Itoa(sessionTTLSeconds)).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) || strings.Contains(err.Error(), "nil") {
			return false, nil
		}
		return false, err
	}
	return res.(int64) == 1, nil
}

// setSessionUserID привязывает user_id к существующей сессии и обновляет TTL.
// Возвращает false если сессия не существует.
func setSessionUserID(ctx context.Context, sid, userID string) (bool, error) {
	key := fmt.Sprintf(redisSessionKey, sid)
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := setSessionUserIDScript.Run(ctx, redisClient, []string{key}, userID, now, strconv.Itoa(sessionTTLSeconds)).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) || strings.Contains(err.Error(), "nil") {
			return false, nil
		}
		return false, err
	}
	return res.(int64) == 1, nil
}

func getSessionUserID(ctx context.Context, sid string) (string, error) {
	key := fmt.Sprintf(redisSessionKey, sid)
	result, err := redisClient.HGet(ctx, key, "user_id").Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return "", nil
		}
		return "", err
	}
	return result, nil
}

func deleteSession(ctx context.Context, sid string) error {
	key := fmt.Sprintf(redisSessionKey, sid)
	return redisClient.Del(ctx, key).Err()
}

// refreshExistingSession обновляет TTL существующей сессии и ставит Set-Cookie.
// Если сессии нет или она невалидна — ничего не делает.
func refreshExistingSession(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil || !isValidSessionID(cookie.Value) {
		return
	}
	updated, err := touchSession(ctx, cookie.Value)
	if err != nil || !updated {
		return
	}
	setSessionCookie(w, cookie.Value)
}
