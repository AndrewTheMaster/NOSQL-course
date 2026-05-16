#!/usr/bin/env bash
set -euo pipefail

CASSANDRA_HOST="${CASSANDRA_HOST:-cassandra}"
KEYSPACE="${CASSANDRA_KEYSPACE:?CASSANDRA_KEYSPACE is required}"
KEYSPACE="${KEYSPACE#\"}"
KEYSPACE="${KEYSPACE%\"}"
until cqlsh "${CASSANDRA_HOST}" -e 'DESCRIBE KEYSPACES' >/dev/null 2>&1; do
	sleep 2
done

sed "s/\${CASSANDRA_KEYSPACE}/${KEYSPACE}/g" /scripts/cassandra/init.cql | cqlsh "${CASSANDRA_HOST}"
