package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"

	"github.com/redis/go-redis/v9"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var (
	redisClient       *redis.Client
	mongoClient       *mongo.Client
	mongoDB           *mongo.Database
	sessionTTLSeconds int
)

func main() {
	port := mustGetenv("APP_PORT")

	ttl, err := strconv.Atoi(mustGetenv("APP_USER_SESSION_TTL"))
	if err != nil || ttl <= 0 {
		log.Fatalf("invalid APP_USER_SESSION_TTL")
	}
	sessionTTLSeconds = ttl

	// Redis
	redisAddr := fmt.Sprintf("%s:%s", mustGetenv("REDIS_HOST"), mustGetenv("REDIS_PORT"))
	redisPassword := os.Getenv("REDIS_PASSWORD")
	redisDB := 0
	if v := os.Getenv("REDIS_DB"); v != "" {
		redisDB, _ = strconv.Atoi(v)
	}
	redisClient = redis.NewClient(&redis.Options{
		Addr:     redisAddr,
		Password: redisPassword,
		DB:       redisDB,
	})
	if err := redisClient.Ping(context.Background()).Err(); err != nil {
		log.Fatalf("failed to connect to redis: %v", err)
	}

	// MongoDB
	mongoHost := mustGetenv("MONGODB_HOST")
	mongoPort := mustGetenv("MONGODB_PORT")
	mongoDatabase := mustGetenv("MONGODB_DATABASE")
	mongoUser := os.Getenv("MONGODB_USER")
	mongoPassword := os.Getenv("MONGODB_PASSWORD")

	var mongoURI string
	if mongoUser != "" && mongoPassword != "" {
		mongoURI = fmt.Sprintf("mongodb://%s:%s@%s:%s", mongoUser, mongoPassword, mongoHost, mongoPort)
	} else {
		mongoURI = fmt.Sprintf("mongodb://%s:%s", mongoHost, mongoPort)
	}

	mongoClient, err = mongo.Connect(context.Background(), options.Client().ApplyURI(mongoURI))
	if err != nil {
		log.Fatalf("failed to connect to MongoDB: %v", err)
	}
	if err := mongoClient.Ping(context.Background(), nil); err != nil {
		log.Fatalf("failed to ping MongoDB: %v", err)
	}
	mongoDB = mongoClient.Database(mongoDatabase)

	if err := ensureIndexes(context.Background()); err != nil {
		log.Fatalf("failed to ensure MongoDB indexes: %v", err)
	}

	// Routes
	http.HandleFunc("/health", healthHandler)
	http.HandleFunc("/session", sessionHandler)
	http.HandleFunc("/users", usersHandler)
	http.HandleFunc("/auth/login", authLoginHandler)
	http.HandleFunc("/auth/logout", authLogoutHandler)
	http.HandleFunc("/events", eventsHandler)

	addr := ":" + port
	log.Printf("Server starting on %s", addr)
	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}

func mustGetenv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Fatalf("%s environment variable is required", key)
	}
	return v
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
