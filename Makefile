.PHONY: lint lint-fix test build run docker-build generate migrate-new migrate-status migrate-down

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

DB_URL := postgres://$(POSTGRES_USER):$(POSTGRES_PASSWORD)@localhost:5432/$(POSTGRES_DB)?sslmode=disable

migrate-new:
	@test -n "$(name)" || (echo 'usage: make migrate-new name=create_something' && exit 1)
	$(GOOSE) -dir migrations create $(name) sql

migrate-status:
	$(GOOSE) -dir migrations postgres "$(DB_URL)" status

migrate-down:
	$(GOOSE) -dir migrations postgres "$(DB_URL)" down

# Runtime agent is an independent Python project; Go commands stay unchanged.
AGENT_PYTHON ?= python3.13
AGENT_VENV ?= $(CURDIR)/runtime-agent/.venv
AGENT_BIN := $(AGENT_VENV)/bin

.PHONY: agent-setup agent-lint agent-test agent-test-jupyter agent-build agent-images agent-integration

agent-setup:
	$(AGENT_PYTHON) -m venv $(AGENT_VENV)
	cd runtime-agent && $(AGENT_BIN)/python -m pip install -r requirements-dev.txt

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
	docker build -f runtime-agent/images/r.Dockerfile -t cellestial-kernel-r:local runtime-agent

agent-integration:
	cd runtime-agent && RUN_DOCKER_TESTS=1 $(AGENT_BIN)/python -m pytest -q -m docker
