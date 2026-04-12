package main

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"strconv"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
)

// likeTTLSeconds задаётся из APP_LIKE_TTL в main.
var likeTTLSeconds int

// Ключ Redis как в автогрейдере ndbx: event:{md5(title)}:reactions (HASH: likes, dislikes).
func redisEventReactionsKey(title string) string {
	h := md5.Sum([]byte(title))
	return "event:" + hex.EncodeToString(h[:]) + ":reactions"
}

type reactionCounts struct {
	Likes    int `json:"likes"`
	Dislikes int `json:"dislikes"`
}

func queryIncludeReactions(q string) bool {
	for _, p := range strings.Split(q, ",") {
		if strings.TrimSpace(p) == "reactions" {
			return true
		}
	}
	return false
}

func getReactionsForTitle(ctx context.Context, title string) (reactionCounts, error) {
	key := redisEventReactionsKey(title)

	n, err := redisClient.Exists(ctx, key).Result()
	if err != nil {
		return reactionCounts{}, err
	}
	if n == 1 {
		vals, err := redisClient.HGetAll(ctx, key).Result()
		if err != nil {
			return reactionCounts{}, err
		}
		likes, _ := strconv.Atoi(vals["likes"])
		dislikes, _ := strconv.Atoi(vals["dislikes"])
		return reactionCounts{Likes: likes, Dislikes: dislikes}, nil
	}

	// cache miss — считаем в Cassandra
	counts, hasRows, err := aggregateReactionsFromCassandra(ctx, title)
	if err != nil {
		return reactionCounts{}, err
	}

	if hasRows {
		pipe := redisClient.TxPipeline()
		pipe.HSet(ctx, key, "likes", strconv.Itoa(counts.Likes), "dislikes", strconv.Itoa(counts.Dislikes))
		pipe.Expire(ctx, key, time.Duration(likeTTLSeconds)*time.Second)
		if _, err := pipe.Exec(ctx); err != nil {
			return reactionCounts{}, err
		}
	}

	return counts, nil
}

func aggregateReactionsFromCassandra(ctx context.Context, title string) (reactionCounts, bool, error) {
	cur, err := mongoDB.Collection("events").Find(ctx, bson.M{"title": title})
	if err != nil {
		return reactionCounts{}, false, err
	}
	defer cur.Close(ctx)

	var total reactionCounts
	hasRows := false

	for cur.Next(ctx) {
		var ev Event
		if err := cur.Decode(&ev); err != nil {
			return reactionCounts{}, false, err
		}
		eid := ev.ID.Hex()
		q := cassSession.Query(
			`SELECT like_value FROM event_reactions WHERE event_id = ?`,
			eid,
		).WithContext(ctx).Consistency(cassConsistency)

		iter := q.Iter()
		var lv int
		for iter.Scan(&lv) {
			hasRows = true
			switch lv {
			case 1:
				total.Likes++
			case -1:
				total.Dislikes++
			}
		}
		if err := iter.Close(); err != nil {
			return reactionCounts{}, false, err
		}
	}
	return total, hasRows, cur.Err()
}

func refreshReactionsCacheForTitle(ctx context.Context, title string) error {
	counts, hasRows, err := aggregateReactionsFromCassandra(ctx, title)
	if err != nil {
		return err
	}
	key := redisEventReactionsKey(title)
	if !hasRows {
		return redisClient.Del(ctx, key).Err()
	}
	pipe := redisClient.TxPipeline()
	pipe.HSet(ctx, key, "likes", strconv.Itoa(counts.Likes), "dislikes", strconv.Itoa(counts.Dislikes))
	pipe.Expire(ctx, key, time.Duration(likeTTLSeconds)*time.Second)
	_, err = pipe.Exec(ctx)
	return err
}

func upsertEventReaction(ctx context.Context, eventID, userID string, value int) error {
	now := time.Now().UTC()
	return cassSession.Query(
		`INSERT INTO event_reactions (event_id, created_by, like_value, created_at) VALUES (?, ?, ?, ?)`,
		eventID,
		userID,
		value,
		now,
	).WithContext(ctx).Consistency(cassConsistency).Exec()
}
