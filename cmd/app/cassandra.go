package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/gocql/gocql"
)

var (
	cassSession     *gocql.Session
	cassConsistency gocql.Consistency
)

func initCassandra(ctx context.Context) error {
	hosts := strings.Split(os.Getenv("CASSANDRA_HOSTS"), ",")
	for i := range hosts {
		hosts[i] = strings.TrimSpace(hosts[i])
	}
	if len(hosts) == 0 || hosts[0] == "" {
		log.Fatalf("CASSANDRA_HOSTS is required")
	}

	port := mustGetenv("CASSANDRA_PORT")
	keyspace := strings.Trim(strings.TrimSpace(os.Getenv("CASSANDRA_KEYSPACE")), `"`)
	if keyspace == "" {
		log.Fatalf("CASSANDRA_KEYSPACE is required")
	}

	user := os.Getenv("CASSANDRA_USERNAME")
	pass := os.Getenv("CASSANDRA_PASSWORD")

	cluster := gocql.NewCluster(hosts...)
	if p, err := strconv.Atoi(port); err == nil && p > 0 {
		cluster.Port = p
	} else {
		cluster.Port = 9042
	}
	cluster.Keyspace = "system"
	if user != "" {
		cluster.Authenticator = gocql.PasswordAuthenticator{
			Username: user,
			Password: pass,
		}
	}
	cluster.Consistency = parseConsistency(os.Getenv("CASSANDRA_CONSISTENCY"))
	cassConsistency = cluster.Consistency

	sysSess, err := cluster.CreateSession()
	if err != nil {
		return fmt.Errorf("cassandra system session: %w", err)
	}
	defer sysSess.Close()

	err = sysSess.
		Query(fmt.Sprintf(
			`CREATE KEYSPACE IF NOT EXISTS %s WITH replication = {'class': 'SimpleStrategy', 'replication_factor': 1}`,
			cqlID(keyspace),
		)).
		WithContext(ctx).
		Exec()
	if err != nil {
		return fmt.Errorf("create keyspace: %w", err)
	}

	cluster.Keyspace = keyspace
	cassSession, err = cluster.CreateSession()
	if err != nil {
		return fmt.Errorf("cassandra session: %w", err)
	}

	_ = cassSession.Query(`DROP TABLE IF EXISTS event_reactions`).WithContext(ctx).Exec()

	err = cassSession.Query(`
CREATE TABLE event_reactions (
	event_id text,
	created_by text,
	like_value tinyint,
	created_at timestamp,
	PRIMARY KEY (event_id, created_by)
)`).WithContext(ctx).Exec()
	if err != nil {
		return fmt.Errorf("create table: %w", err)
	}

	err = cassSession.Query(`
CREATE INDEX IF NOT EXISTS event_reactions_like_value_idx ON event_reactions (like_value)
`).WithContext(ctx).Exec()
	if err != nil {
		return fmt.Errorf("create index on like_value: %w", err)
	}

	return nil
}

func parseConsistency(s string) gocql.Consistency {
	s = strings.TrimSpace(strings.ToUpper(s))
	switch s {
	case "ONE":
		return gocql.One
	case "QUORUM":
		return gocql.Quorum
	case "LOCAL_QUORUM":
		return gocql.LocalQuorum
	case "ALL":
		return gocql.All
	case "ANY":
		return gocql.Any
	case "TWO":
		return gocql.Two
	case "THREE":
		return gocql.Three
	default:
		return gocql.One
	}
}

// cqlID оборачивает идентификатор в двойные кавычки (экранирование ").
func cqlID(id string) string {
	return `"` + strings.ReplaceAll(id, `"`, `\"`) + `"`
}
