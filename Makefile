.PHONY: help up down logs test test-unit test-integration lint build migrate-up migrate-down clean

help:
	@echo "Targets:"
	@echo "  up               Start postgres + redis + run migrations"
	@echo "  down             Stop and remove containers"
	@echo "  logs             Tail container logs"
	@echo "  test             Run all tests"
	@echo "  test-unit        Run unit tests only"
	@echo "  test-integration Run integration tests (requires 'make up')"
	@echo "  lint             Run golangci-lint"
	@echo "  build            Build the nexus binary"
	@echo "  migrate-up       Run migrations"
	@echo "  migrate-down     Roll back one migration"
	@echo "  clean            Remove volumes and build artifacts"

up:
	docker compose -f deploy/docker-compose.yml up -d postgres redis migrate

down:
	docker compose -f deploy/docker-compose.yml down

logs:
	docker compose -f deploy/docker-compose.yml logs -f

test: test-unit test-integration

test-unit:
	go test ./internal/... ./cmd/... -race -count=1

test-integration:
	go test ./test/integration/... -race -count=1 -tags=integration

lint:
	golangci-lint run ./...

build:
	go build -trimpath -o bin/nexus ./cmd/nexus

migrate-up:
	docker compose -f deploy/docker-compose.yml run --rm migrate up

migrate-down:
	docker compose -f deploy/docker-compose.yml run --rm migrate down 1

clean:
	docker compose -f deploy/docker-compose.yml down -v
	rm -rf bin/
