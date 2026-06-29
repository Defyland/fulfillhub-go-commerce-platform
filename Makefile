GO ?= go
GOFMT ?= gofmt

.PHONY: fmt fmt-check vet test coverage race benchmark-smoke build build-check validate-baseline validate-production benchmark-budgets postgres-integration rabbitmq-integration docker-build compose-config review

fmt:
	$(GOFMT) -w cmd internal

fmt-check:
	test -z "$$($(GOFMT) -l cmd internal)"

vet:
	$(GO) vet ./...

test:
	$(GO) test ./...

coverage:
	$(GO) test -coverprofile=coverage.out ./...
	$(GO) tool cover -func=coverage.out | tee coverage.txt

race:
	$(GO) test -race ./...

benchmark-smoke:
	$(GO) test -bench=. -run '^$$' -benchtime=1x ./internal/api

build:
	mkdir -p bin
	$(GO) build -o bin/fulfillhub-api ./cmd/fulfillhub-api
	$(GO) build -o bin/fulfillhub-dlq-replay ./cmd/fulfillhub-dlq-replay
	$(GO) build -o bin/fulfillhub-migrate ./cmd/fulfillhub-migrate
	$(GO) build -o bin/fulfillhub-outbox-relay ./cmd/fulfillhub-outbox-relay
	$(GO) build -o bin/fulfillhub-worker ./cmd/fulfillhub-worker

build-check:
	@tmpdir="$$(mktemp -d)"; \
	trap 'rm -rf "$$tmpdir"' EXIT; \
	$(GO) build -o "$$tmpdir/fulfillhub-api" ./cmd/fulfillhub-api; \
	$(GO) build -o "$$tmpdir/fulfillhub-dlq-replay" ./cmd/fulfillhub-dlq-replay; \
	$(GO) build -o "$$tmpdir/fulfillhub-migrate" ./cmd/fulfillhub-migrate; \
	$(GO) build -o "$$tmpdir/fulfillhub-outbox-relay" ./cmd/fulfillhub-outbox-relay; \
	$(GO) build -o "$$tmpdir/fulfillhub-worker" ./cmd/fulfillhub-worker

validate-baseline:
	./scripts/validate_phase0.sh

validate-production:
	./scripts/validate_production_readiness.sh

benchmark-budgets:
	./scripts/validate_benchmark_budgets.py

postgres-integration:
	$(GO) test ./internal/postgres -run 'TestPostgres(StoreIntegration|InventoryReservationConcurrency)' -count=1

rabbitmq-integration:
	$(GO) test ./internal/messaging -run TestRabbitPublisherIntegration -count=1

docker-build:
	docker build .

compose-config:
	docker compose config

review:
	$(MAKE) validate-baseline
	$(MAKE) validate-production
	$(MAKE) fmt-check
	$(MAKE) vet
	$(MAKE) test
	$(MAKE) benchmark-smoke
	$(MAKE) benchmark-budgets
	$(MAKE) build-check
