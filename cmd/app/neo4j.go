package main

import (
	"context"
	"fmt"
	"log"
	"os"

	neo4j "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

var neo4jDriver neo4j.DriverWithContext

func initNeo4j() error {
	uri := mustGetenv("NEO4J_URL")
	user := os.Getenv("NEO4J_USER")
	if user == "" {
		user = os.Getenv("NEO4J_USERNAME")
	}
	if user == "" {
		log.Fatalf("NEO4J_USER or NEO4J_USERNAME environment variable is required")
	}
	password := os.Getenv("NEO4J_PASSWORD")

	driver, err := neo4j.NewDriverWithContext(uri, neo4j.BasicAuth(user, password, ""))
	if err != nil {
		return err
	}
	if err := driver.VerifyConnectivity(context.Background()); err != nil {
		_ = driver.Close(context.Background())
		return err
	}
	neo4jDriver = driver
	return nil
}

func withWriteSession(ctx context.Context, fn func(tx neo4j.ManagedTransaction) (any, error)) error {
	session := neo4jDriver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer session.Close(ctx)
	_, err := session.ExecuteWrite(ctx, fn)
	return err
}

func withReadSession(ctx context.Context, fn func(tx neo4j.ManagedTransaction) (any, error)) (any, error) {
	session := neo4jDriver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeRead})
	defer session.Close(ctx)
	return session.ExecuteRead(ctx, fn)
}

func ensureNeo4jUser(ctx context.Context, userID string) error {
	return withWriteSession(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		_, err := tx.Run(ctx, `
			MERGE (u:User {id: $id})
		`, map[string]any{"id": userID})
		return nil, err
	})
}

func ensureNeo4jEvent(ctx context.Context, eventID, title string) error {
	return withWriteSession(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		_, err := tx.Run(ctx, `
			MERGE (e:Event {id: $id})
			SET e.title = $title
		`, map[string]any{"id": eventID, "title": title})
		return nil, err
	})
}

func addNeo4jLike(ctx context.Context, userID, eventID, title string) error {
	if err := ensureNeo4jUser(ctx, userID); err != nil {
		return err
	}
	if err := ensureNeo4jEvent(ctx, eventID, title); err != nil {
		return err
	}
	return withWriteSession(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		_, err := tx.Run(ctx, `
			MATCH (u:User {id: $userId}), (e:Event {id: $eventId})
			MERGE (u)-[:LIKED]->(e)
		`, map[string]any{"userId": userID, "eventId": eventID})
		return nil, err
	})
}

type recommendedEventScore struct {
	ID    string
	Score int
}

func getRecommendedEventScores(ctx context.Context, userID string) ([]recommendedEventScore, error) {
	result, err := withReadSession(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		res, err := tx.Run(ctx, `
			MATCH (u:User {id: $userId})-[:LIKED]->(liked:Event)
			MATCH (liked)<-[:LIKED]-(other:User)
			WHERE other.id <> u.id
			MATCH (other)-[:LIKED]->(rec:Event)
			WHERE NOT (u)-[:LIKED]->(rec)
			RETURN rec.id AS id, count(*) AS score
			ORDER BY score DESC, id ASC
		`, map[string]any{"userId": userID})
		if err != nil {
			return nil, err
		}

		var out []recommendedEventScore
		for res.Next(ctx) {
			record := res.Record()
			idVal, _ := record.Get("id")
			scoreVal, _ := record.Get("score")
			id, ok := idVal.(string)
			if !ok {
				continue
			}
			score := int(scoreVal.(int64))
			out = append(out, recommendedEventScore{ID: id, Score: score})
		}
		return out, res.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("neo4j recommendations query: %w", err)
	}
	return result.([]recommendedEventScore), nil
}
