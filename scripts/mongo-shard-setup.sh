#!/usr/bin/env bash
set -euo pipefail

until mongosh "mongodb://mongodb:27017" --quiet --eval 'db.adminCommand({ping:1})' >/dev/null 2>&1; do
  sleep 2
done

mongosh "mongodb://mongodb:27017" --eval '
try { sh.addShard("shard1/shard1-1:27017,shard1-2:27017,shard1-3:27017"); } catch (e) {}
try { sh.addShard("shard2/shard2-1:27017,shard2-2:27017,shard2-3:27017"); } catch (e) {}
'

mongosh "mongodb://mongodb:27017" --eval '
try { sh.enableSharding("eventhub"); } catch (e) {}
'

mongosh "mongodb://mongodb:27017" --eval '
try {
  sh.shardCollection("eventhub.events", { created_by: "hashed" });
} catch (e) {}
'
