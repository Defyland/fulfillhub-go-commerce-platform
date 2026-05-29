# FulfillHub

FulfillHub is a Go-based commerce orchestration platform for merchants that need dependable checkout, inventory reservation, payment authorization, shipment creation, and customer notifications across a failure-prone distributed environment.

> Status: Phase 2 persistence slice. The repository now includes a Go HTTP API, PostgreSQL-backed persistence with embedded migrations, request tests, authorization tests, database tests, a native benchmark, Docker build validation, and documentation baseline. RabbitMQ, Redis, and k6 load tests remain planned next steps.

## What is this product?

FulfillHub is the backend control plane behind an online store checkout. It accepts orders from merchant storefronts, validates tenant access, reserves stock, coordinates payment authorization, triggers shipment creation, and emits lifecycle events for downstream systems such as notifications, analytics, and support tooling.

The product is designed to look like a realistic mid-market commerce platform rather than a generic CRUD service. The core engineering challenge is reliable orchestration under partial failure.

## Problem it solves

Commerce backends often fail at the boundaries between order intake, stock reservation, payment authorization, and shipping. Those failures create overselling, duplicate charges, orphaned shipments, and inconsistent customer communication.

FulfillHub solves that by centralizing orchestration and explicitly designing for:

- idempotent order submission
- deterministic saga state transitions
- message retries and dead-letter handling
- merchant isolation and auditability
- operational visibility for stuck orders

## Target users

- DTC and marketplace merchants integrating checkout APIs
- Operations teams handling fulfillment exceptions and manual replays
- Support teams investigating order, payment, and shipment timelines
- Platform engineers who need a reference implementation for resilient event-driven workflows in Go

## Main features

- Merchant-facing order creation and order status APIs
- Inventory reservation before payment capture
- Payment authorization with compensation on downstream failure
- Shipment creation and tracking state projection
- Event publishing for order lifecycle milestones
- Audit logging with request, correlation, and actor metadata
- Operational runbooks for retries, DLQ replay, and dependency degradation

## Architecture overview

The current implementation starts as a Go modular monolith with strongly isolated packages and asynchronous boundaries represented through an in-memory outbox. The goal is to earn reliability and simplicity first, then add PostgreSQL, RabbitMQ, and Redis behind the same contracts.

- HTTP API entrypoint for merchant and operations access
- Domain modules for orders, inventory, payments, shipments, notifications, and reporting
- In-memory store for fast local tests and PostgreSQL for transactional state, outbox, inbox, and audit logs when `DATABASE_URL` is configured
- In-memory outbox events for the first executable slice, with RabbitMQ planned for domain fan-out and asynchronous side effects
- Redis planned for idempotency windows and rate-limiting primitives
- OpenTelemetry, Prometheus, and Grafana for observability

More detail lives in [docs/architecture/overview.md](./docs/architecture/overview.md) and [docs/diagrams/system-context.md](./docs/diagrams/system-context.md).

## Tech stack

| Component | Planned choice | Reason |
| --- | --- | --- |
| API and orchestration | Go | Strong concurrency model, low operational footprint, explicit control over latency-sensitive paths |
| Primary database | PostgreSQL | ACID guarantees, locking semantics, and mature indexing for inventory and order workflows |
| Messaging | RabbitMQ | Explicit routing keys, retry topologies, and predictable queue semantics |
| Cache and controls | Redis | Fast rate-limiting counters and idempotency support |
| Observability | OpenTelemetry, Prometheus, Grafana | Standard metrics, traces, dashboards, and correlation across sync and async flows |
| Load testing | k6 | Repeatable smoke, load, stress, and spike scenarios |
| CI | GitHub Actions | Portable repository validation and later Go quality gates |

## Domain model

Core aggregates and entities:

- `Merchant`: tenant boundary, API credentials, and warehouse access rules
- `Warehouse`: physical inventory location and fulfillment origin
- `InventoryItem`: sellable SKU state with available and reserved quantities
- `Order`: orchestration root with state machine and failure history
- `OrderItem`: immutable order line snapshot captured at checkout time
- `PaymentAuthorization`: payment provider correlation and authorization result
- `Shipment`: carrier booking, tracking number, and delivery milestones
- `OutboxEvent` and `InboxMessage`: delivery guarantees for async handoff
- `AuditLog`: actor, action, request, and before/after operational evidence

See [docs/architecture/domain-model.md](./docs/architecture/domain-model.md) for invariants and lifecycle ownership.

## API documentation

The initial HTTP contract is defined in [openapi.yaml](./openapi.yaml). The implemented endpoints are:

Supporting API docs:

- [docs/api/request-response-examples.md](./docs/api/request-response-examples.md)
- [docs/api/error-format.md](./docs/api/error-format.md)

The API surface is versioned under `/api/v1` and covers:

- `POST /api/v1/orders`
- `GET /api/v1/orders/{orderId}`
- `POST /api/v1/orders/{orderId}/cancel`
- `GET /healthz`
- `GET /readyz`
- `GET /metrics`

## Async or event architecture

FulfillHub treats asynchronous flow as a first-class concern. The current executable slice records outbox events in memory so the order workflow can be tested before RabbitMQ is introduced.

- Order acceptance emits `order.created`
- Inventory module consumes reserve requests and emits either `inventory.reserved` or `inventory.rejected`
- Payment module emits `payment.authorized` or `payment.failed`
- Shipment module emits `shipment.created` or `shipment.failed`
- Order orchestrator finalizes the saga with `order.completed` or `order.cancelled`
- Future consumers will use inbox deduplication, retry queues, and DLQ routing

The message catalog and routing design are documented in [docs/events/catalog.md](./docs/events/catalog.md) and [docs/diagrams/order-saga-sequence.md](./docs/diagrams/order-saga-sequence.md).

## Database design

The relational model centers on transactional consistency around orders and stock.

- The current executable slice stores orders and outbox events in memory
- Order creation derives `merchant_id` from `X-API-Key`, not from request bodies
- Idempotency keys protect duplicate order creation requests
- Duplicate external order IDs are rejected per merchant
- Embedded PostgreSQL migrations define orders, order items, idempotency keys, outbox events, inbox messages, and audit logs

The data model, indexes, and transaction boundaries are detailed in [docs/architecture/database-design.md](./docs/architecture/database-design.md).

## Testing strategy

The current implementation includes Go tests for:

- domain validation, totals, idempotency, duplicate external order IDs, and outbox creation
- API request handling through `httptest`
- authentication and tenant authorization
- validation and conflict error contracts
- operations token access

The remaining planned test layers are PostgreSQL integration tests, RabbitMQ messaging tests, and k6 load tests once those runtime dependencies are added.

## Performance benchmarks

This repository now includes a native Go benchmark for order creation and still defines the broader k6 benchmark methodology and acceptance targets.

- Benchmark plan: [benchmarks/baseline.md](./benchmarks/baseline.md)
- Methodology notes: [docs/benchmarks/methodology.md](./docs/benchmarks/methodology.md)
- Current phase status: [docs/benchmarks/results-status.md](./docs/benchmarks/results-status.md)
- Results folder: [benchmarks/results/README.md](./benchmarks/results/README.md)
- First native result: [benchmarks/results/2026-05-28-native-http-benchmark.md](./benchmarks/results/2026-05-28-native-http-benchmark.md)

The first measured baseline is an in-process handler benchmark. Networked k6 latency percentiles are still pending.

## Observability

FulfillHub’s operational baseline includes:

- structured JSON logs for every request and async handler
- `request_id`, `correlation_id`, `causation_id`, and `tenant_id` propagation
- OpenTelemetry traces spanning HTTP handlers, SQL, and message publish/consume cycles
- Prometheus counters, histograms, and queue lag gauges
- `/healthz` liveness and `/readyz` readiness endpoints
- `/metrics` Prometheus-compatible request and error counters in the current executable slice
- Grafana dashboards for checkout throughput, saga outcomes, queue depth, and retry volume

## Security considerations

- Merchant-facing APIs authenticate through scoped API keys and derive `merchant_id` from the key
- Operations-only capabilities use JWT bearer tokens with role claims
- Tenant isolation is enforced on every read and write path via `merchant_id`
- Input validation rejects malformed SKU, quantity, address, and idempotency payloads
- Rate limiting protects order creation and lookup endpoints
- Secrets are expected to come from environment variables or secret managers, never committed files
- Audit logs record privileged actions, manual replays, and cancellation reasons
- Threat modeling covers duplicate requests, replay attacks, stock poisoning, and credential leakage

The explicit auth strategy is captured in [docs/adr/0003-authentication-and-authorization.md](./docs/adr/0003-authentication-and-authorization.md).

## Trade-offs and decisions

- Start with a modular monolith instead of early microservices to keep consistency work tractable
- Prefer RabbitMQ over Kafka because command-style routing, retries, and queue ownership are central here
- Use orchestration-style sagas because the order lifecycle needs explicit operational visibility
- Keep OpenAPI contract-first to stabilize integrations before handlers exist
- Defer carrier and payment provider implementation details until the first vertical slice

The most important architecture decisions are recorded in:

- [docs/adr/0001-modular-monolith-first.md](./docs/adr/0001-modular-monolith-first.md)
- [docs/adr/0002-rabbitmq-outbox-inbox.md](./docs/adr/0002-rabbitmq-outbox-inbox.md)
- [docs/adr/0003-authentication-and-authorization.md](./docs/adr/0003-authentication-and-authorization.md)

## How to run locally

Run the service locally with Go:

```sh
git clone git@github.com:Defyland/fulfillhub-go-commerce-platform.git
cd fulfillhub-go-commerce-platform
go run ./cmd/fulfillhub-api
```

The API listens on `:8080` by default. Use `HTTP_ADDR=:9090` to choose another address.

To run with PostgreSQL persistence, provide `DATABASE_URL`. On startup the API applies embedded migrations and switches from the in-memory store to the PostgreSQL store.

```sh
DATABASE_URL='postgres://fulfillhub:postgres@localhost:5432/fulfillhub?sslmode=disable' \
  go run ./cmd/fulfillhub-api
```

Run the full repository validation:

```sh
./scripts/validate_phase0.sh
```

## How to run tests

Run all Go tests:

```sh
go test ./...
```

Run the PostgreSQL integration test when a database is available:

```sh
DATABASE_URL='postgres://fulfillhub:postgres@localhost:5432/fulfillhub_test?sslmode=disable' \
  go test ./internal/postgres -run TestPostgresStoreIntegration -count=1
```

Run the native benchmark:

```sh
go test -bench=. ./internal/api -run '^$'
```

The GitHub Actions workflow at `.github/workflows/phase0-quality.yml` runs repository validation, `gofmt`, `go vet`, tests, PostgreSQL integration tests, benchmark smoke, markdown linting, OpenAPI validation, secret scanning, and Docker build validation.

## Failure scenarios

The implementation is expected to handle, test, and document at least these scenarios:

- duplicate merchant submission with the same idempotency key
- inventory reservation failure after order acceptance
- payment authorization timeout after inventory reservation
- shipment provider failure after payment authorization
- duplicate message delivery to inventory or shipment consumers
- RabbitMQ backlog growth and DLQ accumulation
- PostgreSQL write outage during saga progression
- manual cancellation while fulfillment is already in progress

Runbook detail lives in [docs/runbooks/incident-response.md](./docs/runbooks/incident-response.md).

## Roadmap

1. Phase 0: repository narrative, OpenAPI contract, ADRs, event catalog, benchmark plan, and quality gates
2. Phase 1: Go workspace bootstrap, HTTP API slice, in-memory outbox, request tests, authorization tests, native benchmark, and Docker build
3. Phase 2: PostgreSQL schema, transactional outbox persistence, RabbitMQ relay, inbox deduplication, and failure simulations
4. Phase 3: k6 performance baselines, dashboards, DLQ replay tooling, Redis rate limiting, and provider adapters
