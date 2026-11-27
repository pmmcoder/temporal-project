#!/bin/sh
set -eu

ROOT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$ROOT_DIR"

if [ ! -f .env ]; then
  echo ".env is required; run make up first or copy .env.example" >&2
  exit 1
fi

TIMESTAMP=$(date +%Y%m%d-%H%M%S)
DESTINATION="backups/$TIMESTAMP"
mkdir -p "$DESTINATION"

COMPOSE="docker compose --env-file .env -f deploy/compose.yaml"

$COMPOSE exec -T postgresql sh -c 'pg_dump -U "$POSTGRES_USER" -d temporal' > "$DESTINATION/temporal.sql"
$COMPOSE exec -T postgresql sh -c 'pg_dump -U "$POSTGRES_USER" -d temporal_visibility' > "$DESTINATION/temporal_visibility.sql"

echo "backup written to $DESTINATION"
