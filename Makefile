.PHONY: doctor check-fast check up health logs down lint lint-fix test build run docker-build generate contract-tools contract-lint contract-check migrate-new migrate-status migrate-down

BIN_DIR := ./bin
GO ?= go
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

check-fast:
	bash -n scripts/*.sh
	@files="$$(git ls-files --cached --others --exclude-standard --deduplicate '*.go' | while IFS= read -r file; do if [ -f "$$file" ]; then gofmt -l "$$file"; fi; done)"; \
	test -z "$$files" || { printf '%s\n' "$$files"; exit 1; }
	go vet ./...

check: release-check check-fast
	go mod tidy -diff
	go build ./...
	$(MAKE) test
	$(MAKE) lint
	$(MAKE) contract-check

up:
	docker compose up --build --wait --wait-timeout 120

health:
	curl --fail --silent --show-error --max-time 10 http://localhost:$(or $(PORT),8080)/health

logs:
	docker compose logs --tail=100 api db redis s3

down:
	docker compose down

run:
	docker compose up --build

docker-build:
	docker build -f build/main.Dockerfile -t colab-backend:local \
		--build-arg RUNTIME_PYTHON_IMAGE=cellestial-kernel-python:local \
		--build-arg RUNTIME_AGENT_VERSION=local .

# Контракт: spec/openapi.json — единственный исходник; Redocly закреплён в spec/package-lock.json.
REDOCLY := spec/node_modules/.bin/redocly

$(REDOCLY): spec/package.json spec/package-lock.json
	npm ci --prefix spec --no-audit --no-fund
	touch $@

contract-tools: $(REDOCLY)

contract-lint: $(REDOCLY)
	$(REDOCLY) lint spec/openapi.json --config spec/redocly.yaml

generate: contract-lint
	cd internal/transport/http/api && go tool oapi-codegen -config cfg.yaml ../../../../spec/openapi.json

# Сверяем с повторной генерацией, включая ещё не добавленные в index изменения.
contract-check: contract-lint
	@generated="$$(mktemp)"; config="$$(mktemp)"; \
	trap 'rm -f "$$generated" "$$config"' EXIT; \
	sed "s|^output:.*|output: $$generated|" internal/transport/http/api/cfg.yaml > "$$config"; \
	(cd internal/transport/http/api && go tool oapi-codegen -config "$$config" ../../../../spec/openapi.json) && \
	diff -u internal/transport/http/api/api.gen.go "$$generated"

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

# Runtime agent является отдельным Python-проектом, Go-команды не меняются.
AGENT_PYTHON ?= python3.13
AGENT_VENV ?= $(CURDIR)/runtime-agent/.venv
AGENT_BIN := $(AGENT_VENV)/bin

.PHONY: agent-setup agent-lint agent-test agent-test-jupyter agent-build agent-images agent-integration agent-network-check

agent-setup:
	$(AGENT_PYTHON) -m venv $(AGENT_VENV)
	cd runtime-agent && $(AGENT_BIN)/python -m pip install -r requirements-dev.txt -e .

agent-lint:
	cd runtime-agent && $(AGENT_BIN)/ruff check src tests images
	cd runtime-agent && $(AGENT_BIN)/ruff format --check src tests images

agent-test:
	cd runtime-agent && $(AGENT_BIN)/python -m pytest -q -m 'not docker and not jupyter'

agent-test-jupyter:
	cd runtime-agent && $(AGENT_BIN)/python -m pip install -r requirements-kernel.txt
	cd runtime-agent && RUN_JUPYTER_TESTS=1 $(AGENT_BIN)/python -m pytest -q -m jupyter

agent-build:
	cd runtime-agent && $(AGENT_BIN)/python -m build

agent-images:
	docker build -f runtime-agent/images/python.Dockerfile -t cellestial-kernel-python:local runtime-agent

agent-integration:
	cd runtime-agent && RUN_DOCKER_TESTS=1 $(AGENT_BIN)/python -m pytest -q -m docker

# Требует файрвол из infra и живые probe-эндпоинты; в обычном CI не запускается.
agent-network-check:
	cd runtime-agent && RUN_NETWORK_TESTS=1 $(AGENT_BIN)/python -m pytest -q -m network

.PHONY: generate-mocks
generate-mocks:
	bash scripts/generate-mocks.sh

.PHONY: identity-proto identity-grpc-test
identity-proto:
	GO="$(GO)" bash scripts/generate-identity-proto.sh

# Отдельный одноразовый Redis обязателен: транспортная приёмка не допускает skip.
identity-grpc-test:
	@test -n "$(REDIS_TEST_ADDR)" || (echo 'set REDIS_TEST_ADDR=host:port for disposable Redis'; exit 1)
	$(GO) test -race -count=1 ./internal/app/identity ./internal/user/usecase ./internal/user/adapters/security ./internal/user/delivery/grpc

# Выделенный настоящий Redis; явный адрес делает ошибки фатальными, тесты не пропускаются.
.PHONY: redis-test
redis-test:
	@test -n "$(REDIS_TEST_ADDR)" || (echo 'set REDIS_TEST_ADDR=host:port'; exit 1)
	go test -race -count=1 ./internal/user/adapters/redis ./internal/transport/http

.PHONY: release-check
release-check:
	python3 -m unittest discover -s scripts/release/tests -v

.PHONY: identity-http identity-http-check identity-proto-check identity-build identity-test
identity-http:
	GO="$(GO)" python3 scripts/generate-identity-http.py

identity-http-check: $(REDOCLY)
	GO="$(GO)" python3 scripts/generate-identity-http.py --check
	$(REDOCLY) lint spec/identity/openapi.json --config spec/redocly.yaml

.PHONY: workspace-http workspace-http-check contract-generate-services contract-check-services
workspace-http:
	GO="$(GO)" python3 scripts/generate-identity-http.py --service workspace

workspace-http-check: $(REDOCLY)
	GO="$(GO)" python3 scripts/generate-identity-http.py --service workspace --check
	$(REDOCLY) lint spec/workspace/openapi.json --config spec/redocly.yaml

contract-generate-services: identity-http workspace-http

contract-check-services: identity-http-check workspace-http-check $(REDOCLY)
	$(REDOCLY) lint spec/onlineconf/openapi.json --config spec/redocly.yaml
	$(GO) test ./internal/contracttests -run TestHTTP -count=1

identity-proto-check:
	@temporary="$$(mktemp -d)"; trap 'rm -rf "$$temporary"' EXIT; \
	PROTO_OUTPUT_DIR="$$temporary" GO="$(GO)" bash scripts/generate-identity-proto.sh && \
	diff -ru gen/cellestial/identity "$$temporary/gen/cellestial/identity"

identity-build:
	$(GO) build -o bin/identity ./cmd/identity
	$(GO) build -o bin/identity-migrate ./cmd/identity-migrate

# Оба адреса должны указывать на одноразовые тестовые зависимости.
identity-test: identity-build
	@test -n "$(REDIS_TEST_ADDR)" -a -n "$(IDENTITY_TEST_DATABASE_URL)" || (echo 'set disposable REDIS_TEST_ADDR and IDENTITY_TEST_DATABASE_URL'; exit 1)
	IDENTITY_TEST_BINARY="$(CURDIR)/bin/identity" $(GO) test -race -count=1 ./internal/app/identity ./internal/transport/rpc ./internal/user/... ./migrations/identity

.PHONY: proto-generate proto-check
proto-generate:
	GO="$(GO)" bash scripts/generate-proto.sh

proto-check:
	GO="$(GO)" bash scripts/check-proto.sh
	$(GO) test ./internal/contracttests -run TestProto -count=1

.PHONY: build-services services-test services-check migration-test
build-services:
	@mkdir -p $(BIN_DIR)
	@for service in identity workspace environments migrate identity-admin; do \
		$(GO) build -o "$(BIN_DIR)/$$service" "./cmd/$$service" || exit; \
	done

migration-test:
	@test -n "$(SERVICE_TEST_DATABASE_URL)" || (echo 'set disposable SERVICE_TEST_DATABASE_URL'; exit 1)
	$(GO) test -race -count=1 ./internal/migration

services-test: build-services
	@test -n "$(SERVICE_TEST_DATABASE_URL)" -a -n "$(REDIS_TEST_ADDR)" || \
		(echo 'set disposable SERVICE_TEST_DATABASE_URL and REDIS_TEST_ADDR'; exit 1)
	MICROSERVICES_BIN_DIR="$(abspath $(BIN_DIR))" MICROSERVICES_AGENT_PYTHON="$(abspath $(AGENT_BIN))/python" \
		$(GO) test -race -count=1 ./internal/user/delivery/grpc -run '^TestMicroservicesProcesses$$'

services-check: build-services contract-check-services proto-check
	$(GO) test -race ./internal/app/identity ./internal/app/workspace ./internal/app/environments ./internal/lifecycle ./internal/transport/... ./internal/runtimesession/... ./internal/runtime/...
