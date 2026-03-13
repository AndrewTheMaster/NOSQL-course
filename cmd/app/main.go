package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	sessionCookieName = "X-Session-Id"
	redisSessionKey   = "sid:%s"
	sessionIDBytes    = 16 // 128 бит
)

type HealthCheckResponse struct {
	Status string `json:"status"`
}

var (
	redisClient       *redis.Client
	sessionTTLSeconds int
)

func main() {
	port := os.Getenv("APP_PORT")
	if port == "" {
		log.Fatal("APP_PORT environment variable is required")
	}

	ttlStr := os.Getenv("APP_USER_SESSION_TTL")
	if ttlStr == "" {
		log.Fatal("APP_USER_SESSION_TTL environment variable is required")
	}

	ttl, err := strconv.Atoi(ttlStr)
	if err != nil || ttl <= 0 {
		log.Fatalf("invalid APP_USER_SESSION_TTL value: %q", ttlStr)
	}
	sessionTTLSeconds = ttl

	redisHost := os.Getenv("REDIS_HOST")
	redisPort := os.Getenv("REDIS_PORT")
	if redisHost == "" || redisPort == "" {
		log.Fatal("REDIS_HOST and REDIS_PORT environment variables are required")
	}

	redisPassword := os.Getenv("REDIS_PASSWORD")
	redisDBStr := os.Getenv("REDIS_DB")
	if redisDBStr == "" {
		redisDBStr = "0"
	}
	redisDB, err := strconv.Atoi(redisDBStr)
	if err != nil || redisDB < 0 {
		log.Fatalf("invalid REDIS_DB value: %q", redisDBStr)
	}

	redisClient = redis.NewClient(&redis.Options{
		Addr:     fmt.Sprintf("%s:%s", redisHost, redisPort),
		Password: redisPassword,
		DB:       redisDB,
	})

	if err := redisClient.Ping(context.Background()).Err(); err != nil {
		log.Fatalf("failed to connect to redis: %v", err)
	}

	http.HandleFunc("/health", healthCheckHandler)
	http.HandleFunc("/session", sessionHandler)

	addr := ":" + port
	log.Printf("Server starting on %s", addr)
	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}

func healthCheckHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if cookie, err := r.Cookie(sessionCookieName); err == nil {
		// Возвращаем ту же Cookie, но не трогаем TTL в Redis
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
	_ = json.NewEncoder(w).Encode(HealthCheckResponse{Status: "ok"})
}

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

	sid := cookie.Value

	updated, err := touchSession(ctx, sid)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	if !updated {
		sid, err = createNewSession(ctx)
		if err != nil {
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
		setSessionCookie(w, sid)
		w.WriteHeader(http.StatusCreated)
		return
	}

	setSessionCookie(w, sid)
	w.WriteHeader(http.StatusOK)
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

func generateSessionID() (string, error) {
	buf := make([]byte, sessionIDBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

var (
	errSessionExists    = errors.New("session already exists")
	createSessionScript = redis.NewScript(`
local key = KEYS[1]
if redis.call('EXISTS', key) == 1 then
  return 0
end
redis.call('HMSET', key, 'created_at', ARGV[1], 'updated_at', ARGV[1])
redis.call('EXPIRE', key, ARGV[2])
return 1
`)

	touchSessionScript = redis.NewScript(`
local key = KEYS[1]
if redis.call('EXISTS', key) == 0 then
  return 0
end
redis.call('HSET', key, 'updated_at', ARGV[1])
redis.call('EXPIRE', key, ARGV[2])
return 1
`)
)

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

		n, ok := res.(int64)
		if !ok {
			return "", fmt.Errorf("unexpected result type: %T", res)
		}
		if n == 1 {
			return sid, nil
		}
	}
	return "", errors.New("failed to create unique session id")
}

func touchSession(ctx context.Context, sid string) (bool, error) {
	key := fmt.Sprintf(redisSessionKey, sid)
	now := time.Now().UTC().Format(time.RFC3339)

	res, err := touchSessionScript.Run(ctx, redisClient, []string{key}, now, strconv.Itoa(sessionTTLSeconds)).Result()
	if err != nil {
		// Если ключа нет, приводим к false
		if errors.Is(err, redis.Nil) || strings.Contains(err.Error(), "nil") {
			return false, nil
		}
		return false, err
	}

	n, ok := res.(int64)
	if !ok {
		return false, fmt.Errorf("unexpected result type: %T", res)
	}
	return n == 1, nil
}
