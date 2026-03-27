package main

import (
	"context"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// User — документ коллекции users.
type User struct {
	ID           primitive.ObjectID `bson:"_id,omitempty"`
	FullName     string             `bson:"full_name"`
	Username     string             `bson:"username"`
	PasswordHash string             `bson:"password_hash"`
}

// EventLocation — вложенный объект адреса события.
type EventLocation struct {
	Address string `bson:"address" json:"address"`
}

// Event — документ коллекции events.
type Event struct {
	ID          primitive.ObjectID `bson:"_id,omitempty"`
	Title       string             `bson:"title"`
	Description string             `bson:"description"`
	Location    EventLocation      `bson:"location"`
	CreatedAt   string             `bson:"created_at"`
	CreatedBy   string             `bson:"created_by"`
	StartedAt   string             `bson:"started_at"`
	FinishedAt  string             `bson:"finished_at"`
}

func ensureIndexes(ctx context.Context) error {
	usersCol := mongoDB.Collection("users")
	_, err := usersCol.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "username", Value: 1}},
		Options: options.Index().SetUnique(true),
	})
	if err != nil {
		return err
	}

	eventsCol := mongoDB.Collection("events")
	_, err = eventsCol.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "title", Value: 1}},
			Options: options.Index().SetUnique(true),
		},
		{
			Keys: bson.D{{Key: "title", Value: 1}, {Key: "created_by", Value: 1}},
		},
		{
			Keys: bson.D{{Key: "created_by", Value: 1}},
		},
	})
	return err
}
