SHELL := /bin/sh

GO ?= go
DOCKER_COMPOSE ?= docker compose
API_URL ?= http://localhost:8080
E2E_SUCCESS_URL ?= https://httpbin.org/anything
E2E_FAILURE_URL ?= https://httpbin.org/status/500
WAIT_ATTEMPTS ?= 30
WAIT_SLEEP ?= 1
BIN_DIR ?= bin

.DEFAULT_GOAL := help

.PHONY: help deps fmt fmt-check vet test build check clean \
	run-api run-relay run-worker \
	require-curl require-jq require-docker \
	compose-build up down down-volumes restart ps logs health \
	e2e-success e2e-failure ssrf-test inspect-state assert-e2e-state compose-test

help: ## Show available Makefile targets.
	@awk 'BEGIN {FS = ":.*##"; printf "Usage: make <target>\n\nTargets:\n"} /^[a-zA-Z0-9_.-]+:.*##/ {printf "  %-18s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

deps: ## Download Go modules.
	$(GO) mod download

fmt: ## Format Go source files.
	@gofmt -w $$(find . -name '*.go' -not -path './vendor/*')

fmt-check: ## Check whether Go source files are gofmt-formatted.
	@files=$$(gofmt -l $$(find . -name '*.go' -not -path './vendor/*')); \
	if [ -n "$$files" ]; then \
		echo "gofmt is required for:"; \
		echo "$$files"; \
		exit 1; \
	fi

vet: ## Run go vet.
	$(GO) vet ./...

test: ## Run unit tests.
	$(GO) test ./...

build: ## Build api, relay, and worker binaries into ./bin.
	@mkdir -p $(BIN_DIR)
	$(GO) build -o $(BIN_DIR)/api ./cmd/api
	$(GO) build -o $(BIN_DIR)/relay ./cmd/relay
	$(GO) build -o $(BIN_DIR)/worker ./cmd/worker

check: fmt-check vet test build ## Run formatting checks, vet, tests, and builds.

clean: ## Remove local build artifacts.
	rm -rf $(BIN_DIR)

run-api: ## Run the API service against local PostgreSQL and Redis.
	$(GO) run ./cmd/api

run-relay: ## Run the outbox relay against local PostgreSQL and Redis.
	$(GO) run ./cmd/relay

run-worker: ## Run the worker against local PostgreSQL and Redis.
	$(GO) run ./cmd/worker

require-curl:
	@command -v curl >/dev/null || { echo "curl is required" >&2; exit 1; }

require-jq:
	@command -v jq >/dev/null || { echo "jq is required" >&2; exit 1; }

require-docker:
	@command -v docker >/dev/null || { echo "docker is required" >&2; exit 1; }

compose-build: require-docker ## Build Docker images.
	$(DOCKER_COMPOSE) build

up: require-docker ## Start the full stack with Docker Compose.
	$(DOCKER_COMPOSE) up --build -d

down: require-docker ## Stop Docker Compose services.
	$(DOCKER_COMPOSE) down

down-volumes: require-docker ## Stop services and remove Docker Compose volumes.
	$(DOCKER_COMPOSE) down -v

restart: down up ## Restart the full Docker Compose stack.

ps: require-docker ## Show Docker Compose service status.
	$(DOCKER_COMPOSE) ps -a

logs: require-docker ## Tail Docker Compose logs for app services.
	$(DOCKER_COMPOSE) logs -f api relay worker

health: require-curl ## Wait until API health and readiness endpoints are ready.
	@i=1; \
	while [ $$i -le $(WAIT_ATTEMPTS) ]; do \
		if curl -fsS "$(API_URL)/healthz" >/tmp/rc_notify_health.json && curl -fsS "$(API_URL)/readyz" >/tmp/rc_notify_ready.json; then \
			printf "healthz: "; cat /tmp/rc_notify_health.json; printf "\n"; \
			printf "readyz: "; cat /tmp/rc_notify_ready.json; printf "\n"; \
			exit 0; \
		fi; \
		sleep $(WAIT_SLEEP); \
		i=$$((i + 1)); \
	done; \
	echo "API is not ready at $(API_URL)" >&2; \
	$(DOCKER_COMPOSE) ps; \
	$(DOCKER_COMPOSE) logs --tail=120 api relay worker; \
	exit 1

e2e-success: require-curl require-jq ## Create a notification and verify a successful delivery.
	@set -eu; \
	response=$$(curl -fsS -X POST "$(API_URL)/notifications" \
		-H 'Content-Type: application/json' \
		-d '{"url":"$(E2E_SUCCESS_URL)","method":"POST","headers":{"Content-Type":"application/json"},"body":{"event":"compose_e2e","source":"make"},"max_attempts":3}'); \
	id=$$(printf '%s' "$$response" | jq -r '.id'); \
	echo "created success notification $$id"; \
	i=1; \
	while [ $$i -le $(WAIT_ATTEMPTS) ]; do \
		body=$$(curl -fsS "$(API_URL)/notifications/$$id"); \
		notif_status=$$(printf '%s' "$$body" | jq -r '.status'); \
		echo "success poll $$i: $$notif_status"; \
		if [ "$$notif_status" = "succeeded" ]; then \
			printf '%s\n' "$$body" | jq .; \
			curl -fsS "$(API_URL)/notifications/$$id/attempts" | jq .; \
			exit 0; \
		fi; \
		if [ "$$notif_status" = "failed" ]; then \
			printf '%s\n' "$$body" | jq . >&2; \
			exit 1; \
		fi; \
		sleep $(WAIT_SLEEP); \
		i=$$((i + 1)); \
	done; \
	echo "success notification did not finish in time: $$id" >&2; \
	exit 1

e2e-failure: require-curl require-jq ## Verify failed delivery and manual retry behavior.
	@set -eu; \
	wait_for_failed() { \
		notification_id="$$1"; \
		i=1; \
		while [ $$i -le $(WAIT_ATTEMPTS) ]; do \
			body=$$(curl -fsS "$(API_URL)/notifications/$$notification_id"); \
			notif_status=$$(printf '%s' "$$body" | jq -r '.status'); \
			echo "failure poll $$i: $$notif_status"; \
			if [ "$$notif_status" = "failed" ]; then \
				printf '%s\n' "$$body" | jq .; \
				curl -fsS "$(API_URL)/notifications/$$notification_id/attempts" | jq .; \
				return 0; \
			fi; \
			sleep $(WAIT_SLEEP); \
			i=$$((i + 1)); \
		done; \
		echo "failure notification did not fail in time: $$notification_id" >&2; \
		return 1; \
	}; \
	response=$$(curl -fsS -X POST "$(API_URL)/notifications" \
		-H 'Content-Type: application/json' \
		-d '{"url":"$(E2E_FAILURE_URL)","method":"POST","headers":{"Content-Type":"application/json"},"body":{"event":"compose_failure","source":"make"},"max_attempts":1}'); \
	id=$$(printf '%s' "$$response" | jq -r '.id'); \
	echo "created failure notification $$id"; \
	wait_for_failed "$$id"; \
	curl -fsS -X POST "$(API_URL)/notifications/$$id/retry" | jq .; \
	wait_for_failed "$$id"

ssrf-test: require-curl ## Verify private/internal notification targets are rejected.
	@set -eu; \
	tmp=$$(mktemp); \
	code=$$(curl -sS -o "$$tmp" -w '%{http_code}' -X POST "$(API_URL)/notifications" \
		-H 'Content-Type: application/json' \
		-d '{"url":"http://127.0.0.1:80/metadata","method":"POST","body":{"event":"ssrf"}}'); \
	echo "http_code=$$code"; \
	cat "$$tmp"; printf "\n"; \
	rm -f "$$tmp"; \
	if [ "$$code" != "400" ]; then \
		echo "expected HTTP 400 for SSRF test, got $$code" >&2; \
		exit 1; \
	fi

inspect-state: require-docker ## Print PostgreSQL status counts and Redis consumer group state.
	$(DOCKER_COMPOSE) exec -T postgres psql -U notify -d notify \
		-c "select status, count(*) from notifications group by status order by status;" \
		-c "select status, count(*) from delivery_attempts group by status order by status;" \
		-c "select status, count(*) from outbox_events group by status order by status;"
	$(DOCKER_COMPOSE) exec -T redis redis-cli XINFO GROUPS notification-deliveries

assert-e2e-state: require-docker ## Assert the clean compose-test database and Redis state.
	@set -eu; \
	notifications_succeeded=$$($(DOCKER_COMPOSE) exec -T postgres psql -U notify -d notify -Atqc "select count(*) from notifications where status = 'succeeded';"); \
	notifications_failed=$$($(DOCKER_COMPOSE) exec -T postgres psql -U notify -d notify -Atqc "select count(*) from notifications where status = 'failed';"); \
	attempts_succeeded=$$($(DOCKER_COMPOSE) exec -T postgres psql -U notify -d notify -Atqc "select count(*) from delivery_attempts where status = 'succeeded';"); \
	attempts_failed=$$($(DOCKER_COMPOSE) exec -T postgres psql -U notify -d notify -Atqc "select count(*) from delivery_attempts where status = 'failed';"); \
	outbox_published=$$($(DOCKER_COMPOSE) exec -T postgres psql -U notify -d notify -Atqc "select count(*) from outbox_events where status = 'published';"); \
	redis_pending=$$($(DOCKER_COMPOSE) exec -T redis redis-cli XINFO GROUPS notification-deliveries | awk '/pending/{getline; print; exit}'); \
	echo "notifications succeeded=$$notifications_succeeded failed=$$notifications_failed"; \
	echo "attempts succeeded=$$attempts_succeeded failed=$$attempts_failed"; \
	echo "outbox published=$$outbox_published"; \
	echo "redis pending=$$redis_pending"; \
	test "$$notifications_succeeded" = "1"; \
	test "$$notifications_failed" = "1"; \
	test "$$attempts_succeeded" = "1"; \
	test "$$attempts_failed" = "2"; \
	test "$$outbox_published" = "3"; \
	test "$$redis_pending" = "0"

compose-test: require-docker require-curl require-jq ## Rebuild, run full Docker Compose E2E tests, assert state, and clean up.
	@set -eu; \
	trap '$(DOCKER_COMPOSE) down -v' EXIT INT TERM; \
	$(DOCKER_COMPOSE) down -v; \
	$(DOCKER_COMPOSE) up --build -d; \
	$(MAKE) --no-print-directory health; \
	$(MAKE) --no-print-directory e2e-success; \
	$(MAKE) --no-print-directory e2e-failure; \
	$(MAKE) --no-print-directory ssrf-test; \
	$(MAKE) --no-print-directory assert-e2e-state
