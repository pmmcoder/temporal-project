#!/bin/sh
set -eu

if [ "$#" -ne 1 ]; then
  echo "usage: deploy/restore.sh backups/<timestamp>" >&2
  exit 1
fi

ROOT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$ROOT_DIR"

SOURCE=$1
if [ ! -f "$SOURCE/temporal.sql" ] || [ ! -f "$SOURCE/temporal_visibility.sql" ]; then
  echo "backup files not found under $SOURCE" >&2
  exit 1
fi
if [ ! -f .env ]; then
  echo ".env is required" >&2
  exit 1
fi

COMPOSE="docker compose --env-file .env -f deploy/compose.yaml"
TARGET_TEMPORAL=${RESTORE_DB_TEMPORAL:-temporal}
TARGET_VISIBILITY=${RESTORE_DB_VISIBILITY:-temporal_visibility}

$COMPOSE up -d postgresql
$COMPOSE exec -T postgresql sh -c "dropdb -U \"\$POSTGRES_USER\" --if-exists \"$TARGET_TEMPORAL\" && createdb -U \"\$POSTGRES_USER\" \"$TARGET_TEMPORAL\""
$COMPOSE exec -T postgresql sh -c "dropdb -U \"\$POSTGRES_USER\" --if-exists \"$TARGET_VISIBILITY\" && createdb -U \"\$POSTGRES_USER\" \"$TARGET_VISIBILITY\""
$COMPOSE exec -T postgresql sh -c "psql -U \"\$POSTGRES_USER\" -d \"$TARGET_TEMPORAL\"" < "$SOURCE/temporal.sql"
$COMPOSE exec -T postgresql sh -c "psql -U \"\$POSTGRES_USER\" -d \"$TARGET_VISIBILITY\"" < "$SOURCE/temporal_visibility.sql"

echo "restore completed; run make up to start Temporal and application services"
