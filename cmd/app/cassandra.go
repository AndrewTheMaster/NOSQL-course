package main

import (
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

func initCassandra() error {
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

	p, err := strconv.Atoi(port)
	if err != nil || p <= 0 {
		log.Fatalf("invalid CASSANDRA_PORT: must be a positive integer, got %q", port)
	}
	cluster := gocql.NewCluster(hosts...)
	cluster.Port = p
	cluster.Keyspace = keyspace
	if user != "" {
		cluster.Authenticator = gocql.PasswordAuthenticator{
			Username: user,
			Password: pass,
		}
	}
	cluster.Consistency = parseConsistency(os.Getenv("CASSANDRA_CONSISTENCY"))
	cassConsistency = cluster.Consistency

	cassSession, err = cluster.CreateSession()
	if err != nil {
		return fmt.Errorf("cassandra session: %w", err)
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
