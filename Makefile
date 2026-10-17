.PHONY: doctor check-fast check up health logs down lint lint-fix test build run docker-build generate migrate-new migrate-status migrate-down

BIN_DIR := ./bin
LINT_BIN := $(BIN_DIR)/golangci-lint
LINT_VER := v2.13.2

$(LINT_BIN):
	curl -sSfL https://golangci-lint.run/install.sh | sh -s -- -b $(BIN_DIR) $(LINT_VER)

lint: $(LINT_BIN)
	$(LINT_BIN) run ./...

lint-fix: $(LINT_BIN)
	$(LINT_BIN) run --fix ./...

test:
	@bash scripts/test.sh

build:
	go build -o bin/server ./cmd/main

doctor:
	@bash scripts/doctor.sh

# Быстрый цикл; для изменённого пакета дополнительно go test ./internal/<package>.
check-fast:
	bash -n scripts/*.sh
	@test -z "$$(gofmt -l $$(git ls-files --cached --others --exclude-standard '*.go'))" || (gofmt -l $$(git ls-files --cached --others --exclude-standard '*.go'); exit 1)
	go vet ./...

# Проверки job test/lint в CI; Docker проверяется отдельно make docker-build.
check: check-fast
	go mod tidy -diff
	go build ./...
	$(MAKE) test
	$(MAKE) lint

up:
	docker compose up --build --wait --wait-timeout 120

health:
	curl --fail --silent --show-error --max-time 10 http://localhost:$(or $(PORT),8080)/health

logs:
	docker compose logs --tail=100 api db s3

down:
	docker compose down

run:
	docker compose up --build

docker-build:
	docker build -f build/main.Dockerfile -t colab-backend:local .

SPEC_FILE := internal/api/openapi.yaml

generate:
	go run ./cmd/apidog -o $(SPEC_FILE)
	cd internal/api && go tool oapi-codegen -config cfg.yaml openapi.yaml

-include .env

GOOSE := go run github.com/pressly/goose/v3/cmd/goose@v3.28.0

DB_URL := postgres://$(POSTGRES_USER):$(POSTGRES_PASSWORD)@localhost:$(or $(POSTGRES_PORT),5432)/$(POSTGRES_DB)?sslmode=disable

migrate-new:
	@test -n "$(name)" || (echo 'usage: make migrate-new name=create_something' && exit 1)
	$(GOOSE) -dir migrations create $(name) sql

migrate-status:
	$(GOOSE) -dir migrations postgres "$(DB_URL)" status

migrate-down:
	$(GOOSE) -dir migrations postgres "$(DB_URL)" down
