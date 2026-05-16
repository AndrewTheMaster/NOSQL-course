#!/usr/bin/env bash
set -euo pipefail

wait_mongo() {
  local uri=$1
  until mongosh "$uri" --quiet --eval 'db.adminCommand({ping:1})' >/dev/null 2>&1; do
    sleep 1
  done
}

for uri in \
  "mongodb://mongo-config:27017" \
  "mongodb://shard1-1:27017" \
  "mongodb://shard1-2:27017" \
  "mongodb://shard1-3:27017" \
  "mongodb://shard2-1:27017" \
  "mongodb://shard2-2:27017" \
  "mongodb://shard2-3:27017"; do
  wait_mongo "$uri"
done

mongosh "mongodb://mongo-config:27017" --eval '
try {
  rs.initiate({
    _id: "cfg",
    configsvr: true,
    members: [{ _id: 0, host: "mongo-config:27017" }]
  });
} catch (e) {}
'

sleep 10

mongosh "mongodb://shard1-1:27017" --eval '
try {
  rs.initiate({
    _id: "shard1",
    members: [
      { _id: 0, host: "shard1-1:27017" },
      { _id: 1, host: "shard1-2:27017" },
      { _id: 2, host: "shard1-3:27017" }
    ]
  });
} catch (e) {}
'

sleep 10

mongosh "mongodb://shard2-1:27017" --eval '
try {
  rs.initiate({
    _id: "shard2",
    members: [
      { _id: 0, host: "shard2-1:27017" },
      { _id: 1, host: "shard2-2:27017" },
      { _id: 2, host: "shard2-3:27017" }
    ]
  });
} catch (e) {}
'

sleep 15
