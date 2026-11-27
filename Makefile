SHELL := /bin/sh

COMPOSE := docker compose --env-file .env -f deploy/compose.yaml

.PHONY: up down restart deploy logs ps test lint smoke backup ensure-env dev-worker

up: ensure-env
	$(COMPOSE) up -d --build

down:
	$(COMPOSE) down

restart: down up

deploy: ensure-env
	$(COMPOSE) up -d --build app-api app-worker

logs:
	$(COMPOSE) logs -f --tail=200

ps:
	$(COMPOSE) ps

test:
	go test ./...
	cd web && npm test

lint:
	go vet ./...
	go run go.temporal.io/sdk/contrib/tools/workflowcheck@v0.6.0 ./...
	cd web && npm run build

smoke:
	$(MAKE) up
	go run ./cmd/smoke

backup:
	./deploy/backup.sh

dev-worker: ensure-env
	$(COMPOSE) -f deploy/compose.dev.yaml up -d --build postgresql temporal temporal-bootstrap
	TEMPORAL_ADDRESS=127.0.0.1:7233 go run ./cmd/platform worker

ensure-env:
	@test -f .env || cp .env.example .env
