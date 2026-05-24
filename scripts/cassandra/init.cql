CREATE KEYSPACE IF NOT EXISTS ${CASSANDRA_KEYSPACE}
WITH replication = {'class': 'SimpleStrategy', 'replication_factor': 1};

USE ${CASSANDRA_KEYSPACE};

CREATE TABLE IF NOT EXISTS event_reactions (
	event_id text,
	created_by text,
	like_value tinyint,
	created_at timestamp,
	PRIMARY KEY (event_id, created_by)
);

CREATE INDEX IF NOT EXISTS event_reactions_like_value_idx ON event_reactions (like_value);

CREATE TABLE IF NOT EXISTS event_reviews (
	event_id text,
	created_by text,
	id uuid,
	rating tinyint,
	comment text,
	created_at timestamp,
	updated_at timestamp,
	PRIMARY KEY ((event_id), created_by)
);

CREATE TABLE IF NOT EXISTS event_reviews_timeline (
	event_id text,
	created_at timestamp,
	id uuid,
	rating tinyint,
	comment text,
	created_by text,
	updated_at timestamp,
	PRIMARY KEY ((event_id), created_at, id)
) WITH CLUSTERING ORDER BY (created_at DESC, id ASC);
