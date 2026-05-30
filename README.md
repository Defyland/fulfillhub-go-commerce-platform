# FulfillHub

FulfillHub is a Go-based commerce orchestration platform for merchants that need dependable checkout, inventory reservation, payment authorization, shipment creation, and customer notifications across a failure-prone distributed environment.

> Status: Phase 4 worker slice. The repository now includes a Go HTTP API, PostgreSQL-backed persistence with embedded migrations, tenant foreign keys, catalog-backed inventory reservations, an outbox relay, RabbitMQ publisher and consumer topology, causal message metadata, workerized fulfillment happy path with durable inventory/payment/shipment/notification/cancellation/compensation projections, Redis rate limiting, inbox idempotency, DLQ replay tooling, provider adapters, OpenTelemetry OTLP tracing through a local collector, request tests, authorization tests, database tests, messaging tests, k6 smoke/load/stress/spike results, a native benchmark, Compose-backed smoke/load/stress/spike profiling, Grafana dashboard definition, Docker build validation, Docker Compose config, and documentation baseline.

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

The current implementation is a Go modular monolith with strongly isolated packages and asynchronous boundaries represented through a transactional outbox. Runtime dependencies are available through Docker Compose so reliability, observability, and performance evidence are measured against the same local topology.

- HTTP API entrypoint for merchant and operations access
- Domain modules for orders, inventory, payments, shipments, notifications, and reporting
- In-memory store for fast local tests and PostgreSQL for transactional state, outbox, inbox, and audit logs when `DATABASE_URL` is configured
- Transactional outbox events with correlation and causation IDs, plus a RabbitMQ relay for domain fan-out and asynchronous side effects
- Redis-backed rate limiting when `REDIS_URL` is configured
- OpenTelemetry Collector, Prometheus, and Grafana for observability

More detail lives in [docs/architecture/overview.md](./docs/architecture/overview.md) and [docs/diagrams/system-context.md](./docs/diagrams/system-context.md).

## Tech stack

| Component | Planned choice | Reason |
| --- | --- | --- |
| API and orchestration | Go | Strong concurrency model, low operational footprint, explicit control over latency-sensitive paths |
| Primary database | PostgreSQL | ACID guarantees, locking semantics, and mature indexing for inventory and order workflows |
| Messaging | RabbitMQ | Explicit routing keys, retry topologies, and predictable queue semantics |
| Cache and controls | Redis | Fast rate-limiting counters and idempotency support |
| Observability | OpenTelemetry Collector, Prometheus, Grafana | Standard metrics, traces, dashboards, and correlation across sync and async flows |
| Load testing | k6 | Repeatable smoke, load, stress, and spike scenarios |
| CI | GitHub Actions | Portable repository validation, Go quality gates, and coverage artifacts |

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
- `GET /api/v1/shipments/{shipmentId}`
- `GET /healthz`
- `GET /readyz`
- `GET /metrics`

## Async or event architecture

FulfillHub treats asynchronous flow as a first-class concern. The current implementation records order outbox events, ships a relay process that publishes pending events to RabbitMQ, and provides a worker executable for the fulfillment happy path.

- Order acceptance emits `order.created`
- Inventory worker consumes reserve requests, locks and decrements `inventory_items`, records `stock_reservations` with warehouse provenance, and writes `inventory.reserved` to the outbox
- Inventory reservation failures are converted to `inventory.rejected` outbox
  events so compensation can fail the order through the same broker path
- Payment worker consumes inventory reservations, records `payment_authorizations`, and writes `payment.authorized` to the outbox
- Payment authorization failures are converted to `payment.failed` outbox
  events so compensation can run through the same broker path
- Shipment worker consumes payment authorizations, records `shipments`, and writes `shipment.created` to the outbox
- Shipment provider failures are converted to `shipment.failed` outbox events
  for compensation and operations visibility
- Order finalizer consumes shipment creation, durably marks the order `completed`, and writes `order.completed` to the outbox
- Order cancellation worker consumes `order.cancel_requested`, durably marks
  pre-shipment orders `cancelled`, and routes orders already handed to shipment
  into `manual_review` with `order.manual_review_required`
- Notification worker consumes order completion, cancellation, manual-review,
  or fulfillment failure events and records a durable email notification projection
- Compensation worker consumes inventory, payment, or shipment failure events and records durable compensation outcomes, stock releases, and payment void projections
- Consumer idempotency is modeled through inbox deduplication, bounded retry
  queues, and DLQ routing in the RabbitMQ topology

The message catalog and routing design are documented in [docs/events/catalog.md](./docs/events/catalog.md), the versioned contract policy in [docs/events/README.md](./docs/events/README.md), the event threat model in [docs/events/threat-model.md](./docs/events/threat-model.md), and the saga diagram in [docs/diagrams/order-saga-sequence.md](./docs/diagrams/order-saga-sequence.md).

## Database design

The relational model centers on transactional consistency around orders and stock.

- The current executable slice supports in-memory storage for fast tests and PostgreSQL storage for durable runtime state
- Order creation derives `merchant_id` from `X-API-Key`, not from request bodies
- Idempotency keys protect duplicate order creation requests
- Duplicate external order IDs are rejected per merchant
- Embedded PostgreSQL migrations define orders, order items, warehouses, inventory items, stock reservations with warehouse provenance, idempotency keys, outbox events, inbox messages, and audit logs

The data model, indexes, and transaction boundaries are detailed in [docs/architecture/database-design.md](./docs/architecture/database-design.md).

## Testing strategy

The current implementation includes Go tests for:

- domain validation, totals, idempotency, duplicate external order IDs, and outbox creation
- API request handling through `httptest`
- authentication and tenant authorization
- validation and conflict error contracts
- operations token access
- shipment lookup and tenant authorization
- outbox relay success and publish-failure behavior
- inbox idempotency by consumer and message ID
- RabbitMQ consumer trace propagation, inbox deduplication, retry scheduling, and ack/nack behavior
- inventory reservation failure handling with durable `inventory.rejected` outbox events
- payment authorization failure handling with durable `payment.failed` outbox events
- shipment provider failure handling with durable `shipment.failed` outbox events
- fulfillment worker happy-path progression through durable inventory, payment, shipment, and order completion projections
- PostgreSQL inventory reservation tests that decrement `inventory_items`, persist the reservation warehouse, and restore available stock during compensation

The performance layer includes compose-backed smoke, load, stress, and spike
resource profiling for PostgreSQL, RabbitMQ, Redis, RabbitMQ queue drain, and
API memory under the same k6 scenarios.

## Performance benchmarks

This repository now includes a native Go benchmark for order creation plus k6
smoke, load, stress, and spike measurements against the local in-memory API
process.

- Benchmark plan: [benchmarks/baseline.md](./benchmarks/baseline.md)
- Methodology notes: [docs/benchmarks/methodology.md](./docs/benchmarks/methodology.md)
- Current phase status: [docs/benchmarks/results-status.md](./docs/benchmarks/results-status.md)
- Compose profiling harness: [docs/benchmarks/compose-profiling.md](./docs/benchmarks/compose-profiling.md)
- Results folder: [benchmarks/results/README.md](./benchmarks/results/README.md)
- First native result: [benchmarks/results/2026-05-28-native-http-benchmark.md](./benchmarks/results/2026-05-28-native-http-benchmark.md)
- k6 smoke result: [benchmarks/results/2026-05-28-k6-smoke.md](./benchmarks/results/2026-05-28-k6-smoke.md)
- k6 load result: [benchmarks/results/2026-05-28-k6-load.md](./benchmarks/results/2026-05-28-k6-load.md)
- k6 stress result: [benchmarks/results/2026-05-28-k6-stress.md](./benchmarks/results/2026-05-28-k6-stress.md)
- k6 spike result: [benchmarks/results/2026-05-28-k6-spike.md](./benchmarks/results/2026-05-28-k6-spike.md)
- Compose smoke result: [benchmarks/results/2026-05-29-compose-smoke.md](./benchmarks/results/2026-05-29-compose-smoke.md)
- Compose load/stress/spike result: [benchmarks/results/2026-05-29-compose-load-stress-spike.md](./benchmarks/results/2026-05-29-compose-load-stress-spike.md)

## Observability

FulfillHub’s operational baseline includes:

- structured JSON logs for every API request
- `request_id`, `correlation_id`, actor type, and merchant metadata in request logs
- OpenTelemetry HTTP spans with W3C `traceparent` propagation
- OpenTelemetry PostgreSQL spans for order, outbox, inbox, and audit persistence
- OpenTelemetry outbox relay and RabbitMQ publish spans with AMQP `traceparent` headers
- OpenTelemetry RabbitMQ consume spans with inbox idempotency and explicit acknowledgement outcomes
- optional stdout trace export via `OTEL_TRACES_EXPORTER=stdout`
- OTLP/HTTP trace export via `OTEL_TRACES_EXPORTER=otlp` and the Compose OpenTelemetry Collector
- Prometheus-compatible request and error counters
- Prometheus outbox backlog gauge for unpublished relay events
- Prometheus RabbitMQ queue depth and consumer gauges when `RABBITMQ_URL` is configured
- optional `/metrics` bearer protection via `METRICS_BEARER_TOKEN`
- `/healthz` liveness and `/readyz` readiness endpoints for configured store, broker, and cache dependencies
- `/metrics` for the current executable slice
- Grafana dashboards for checkout throughput, saga outcomes, queue depth, and retry volume
- dashboard definition in [docs/observability/grafana-dashboard.json](./docs/observability/grafana-dashboard.json)
- Prometheus alert rules for API availability, outbox stall, DLQ backlog,
  missing consumers, manual review backlog, and order failure ratio in
  [deployments/prometheus/rules/fulfillhub-alerts.yml](./deployments/prometheus/rules/fulfillhub-alerts.yml)

Compose-backed smoke, load, stress, and spike resource measurements are
included alongside the HTTP, SQL, RabbitMQ, publish-path, and consume-path
runtime baseline.

## Security considerations

- Merchant-facing APIs authenticate through scoped API keys and derive `merchant_id` from the key
- Operations-only capabilities use JWT bearer tokens with role, expiry, issuer, audience, and rotation controls when configured
- The local slice accepts `Bearer ops-token` only when `OPS_JWT_SECRET` is not set
- Tenant isolation is enforced on every read and write path via `merchant_id`
- Input validation rejects malformed SKU, quantity, address, and idempotency payloads
- Rate limiting protects order creation and lookup endpoints
- Secrets are expected to come from environment variables or secret managers, never committed files
- Audit logs record privileged actions, manual replays, and cancellation reasons
- Threat modeling covers duplicate requests, replay attacks, stock poisoning, and credential leakage

The explicit auth strategy is captured in [docs/adr/0003-authentication-and-authorization.md](./docs/adr/0003-authentication-and-authorization.md).
The current threat model and endpoint authorization matrix live in:

- [docs/security/threat-model.md](./docs/security/threat-model.md)
- [docs/security/authorization-matrix.md](./docs/security/authorization-matrix.md)

## Trade-offs and decisions

- Start with a modular monolith instead of early microservices to keep consistency work tractable
- Prefer RabbitMQ over Kafka because command-style routing, retries, and queue ownership are central here
- Use orchestration-style sagas because the order lifecycle needs explicit operational visibility
- Keep OpenAPI contract-first to stabilize integrations as handlers evolve
- Keep provider adapters lightweight while persisting durable inventory, payment, shipment, and compensation projections

The most important architecture decisions are recorded in:

- [docs/adr/0001-modular-monolith-first.md](./docs/adr/0001-modular-monolith-first.md)
- [docs/adr/0002-rabbitmq-outbox-inbox.md](./docs/adr/0002-rabbitmq-outbox-inbox.md)
- [docs/adr/0003-authentication-and-authorization.md](./docs/adr/0003-authentication-and-authorization.md)
- [docs/adr/0004-local-otel-collector.md](./docs/adr/0004-local-otel-collector.md)
- [docs/architecture/senior-technical-assessment.md](./docs/architecture/senior-technical-assessment.md)

## How to run locally

Requires Go `1.25.10` or newer.

Run the service locally with Go:

```sh
git clone git@github.com:Defyland/fulfillhub-go-commerce-platform.git
cd fulfillhub-go-commerce-platform
go run ./cmd/fulfillhub-api
```

The API listens on `:8080` by default. Use `HTTP_ADDR=:9090` to choose another address.

Enable local OpenTelemetry span output with:

```sh
OTEL_TRACES_EXPORTER=stdout go run ./cmd/fulfillhub-api
```

Export spans to an OTLP/HTTP collector with:

```sh
OTEL_TRACES_EXPORTER=otlp \
OTEL_EXPORTER_OTLP_TRACES_ENDPOINT='http://localhost:4318/v1/traces' \
OTEL_SERVICE_NAME='fulfillhub-api' \
  go run ./cmd/fulfillhub-api
```

Require signed operations JWTs with:

```sh
OPS_JWT_SECRET='local-development-secret' \
OPS_JWT_PREVIOUS_SECRETS='previous-secret-for-rotation' \
OPS_JWT_ISSUER='https://ops.fulfillhub.local' \
OPS_JWT_AUDIENCE='fulfillhub-ops' \
  go run ./cmd/fulfillhub-api
```

To run with PostgreSQL persistence, provide `DATABASE_URL`. On startup the API applies embedded migrations and switches from the in-memory store to the PostgreSQL store.

```sh
DATABASE_URL='postgres://fulfillhub:postgres@localhost:5432/fulfillhub?sslmode=disable' \
  go run ./cmd/fulfillhub-api
```

Migrations seed local demo inventory for the built-in demo API-key merchants so
the Compose worker stack can exercise the happy path without a separate catalog
admin API.

For controlled deploys, run migrations as a separate release step instead of
letting application pods own rollout sequencing:

```sh
DATABASE_URL='postgres://fulfillhub:postgres@localhost:5432/fulfillhub?sslmode=disable' \
MIGRATION_TIMEOUT='60s' \
  go run ./cmd/fulfillhub-migrate
```

To enable Redis-backed write rate limiting, provide `REDIS_URL`. The default
limit is `120` writes per merchant per minute and can be changed with
`RATE_LIMIT_PER_MINUTE`.

```sh
REDIS_URL='redis://localhost:6379/0' RATE_LIMIT_PER_MINUTE=600 \
  go run ./cmd/fulfillhub-api
```

To expose RabbitMQ queue gauges on `/metrics`, provide `RABBITMQ_URL`.

```sh
RABBITMQ_URL='amqp://guest:guest@localhost:5672/' go run ./cmd/fulfillhub-api
```

To require a bearer token for `/metrics`, provide `METRICS_BEARER_TOKEN` and
scrape with `Authorization: Bearer <token>`.

```sh
METRICS_BEARER_TOKEN='local-metrics-token' go run ./cmd/fulfillhub-api
```

Docker Compose enables the same metrics bearer control with the local
`local-metrics-token` token so Prometheus and profiling scripts scrape through
the authenticated path by default.

Run the outbox relay when PostgreSQL and RabbitMQ are available:

```sh
DATABASE_URL='postgres://fulfillhub:postgres@localhost:5432/fulfillhub?sslmode=disable' \
RABBITMQ_URL='amqp://guest:guest@localhost:5672/' \
OTEL_TRACES_EXPORTER='stdout' \
  go run ./cmd/fulfillhub-outbox-relay
```

Relay throughput can be tuned with `OUTBOX_RELAY_BATCH_SIZE` and
`OUTBOX_RELAY_INTERVAL`. The Compose stack defaults to a larger batch and a
shorter interval so performance profiles do not leave the HTTP outbox backlog
behind the request rate.

Run one worker process for a queue:

```sh
DATABASE_URL='postgres://fulfillhub:postgres@localhost:5432/fulfillhub?sslmode=disable' \
RABBITMQ_URL='amqp://guest:guest@localhost:5672/' \
WORKER_QUEUE='inventory.reserve' \
CONSUMER_NAME='inventory-worker' \
OTEL_TRACES_EXPORTER='stdout' \
  go run ./cmd/fulfillhub-worker
```

Replay a DLQ queue explicitly:

```sh
RABBITMQ_URL='amqp://guest:guest@localhost:5672/' \
DATABASE_URL='postgres://fulfillhub:postgres@localhost:5432/fulfillhub?sslmode=disable' \
DLQ_QUEUE='inventory.reserve.dlq' \
TARGET_ROUTING_KEY='order.created' \
OPS_ACTOR_ID='usr_ops_1' \
  go run ./cmd/fulfillhub-dlq-replay
```

The replay command writes a durable `dlq.replay` audit log with queue, target
routing key, replay limit, replayed count, status, and error details when a
partial replay fails.

Run the local infrastructure stack, including PostgreSQL, RabbitMQ, Redis,
OpenTelemetry Collector, Prometheus, Grafana, the API, relay, and workers:

```sh
docker compose up --build
```

Host ports can be overridden when local services already use the defaults:

```sh
POSTGRES_PORT=15432 API_PORT=18080 OTEL_COLLECTOR_OTLP_HTTP_PORT=14318 \
  docker compose up --build
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

Run the RabbitMQ integration test when a broker is available:

```sh
RABBITMQ_URL='amqp://guest:guest@localhost:5672/' \
  go test ./internal/messaging -run TestRabbitPublisherIntegration -count=1
```

Run the native benchmark:

```sh
go test -bench=. ./internal/api -run '^$'
```

Run k6 smoke against a running API:

```sh
BASE_URL='http://localhost:8080' k6 run benchmarks/k6/smoke.js
```

Run the longer k6 profiles against a running API:

```sh
BASE_URL='http://localhost:8080' k6 run benchmarks/k6/load.js
BASE_URL='http://localhost:8080' k6 run benchmarks/k6/stress.js
BASE_URL='http://localhost:8080' k6 run benchmarks/k6/spike.js
```

The GitHub Actions workflow at `.github/workflows/phase0-quality.yml` runs repository validation, production-readiness validation, `gofmt`, `go vet`, tests, PostgreSQL integration tests, benchmark smoke, markdown linting, OpenAPI validation, secret scanning, supply-chain scans, and Docker build validation.

Production deployment and operations artifacts are captured in
[docs/production-readiness.md](./docs/production-readiness.md) and
[deployments/kubernetes/base](./deployments/kubernetes/base). The manifests are
a blueprint for real environments: image tags, managed PostgreSQL/RabbitMQ/Redis
endpoints, External Secrets provider, ingress, and cloud IAM bindings must be
supplied by the target platform.

Operational launch controls are documented in the deployment, alert, data
protection, secrets, and supply-chain runbooks under `docs/runbooks` and
`docs/security`.

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
4. Phase 3: k6 scripts, dashboards, DLQ replay tooling, Redis rate limiting, and provider adapters
5. Phase 4: workerized fulfillment happy path, full trace propagation, and compose-backed performance profiling
6. Production-readiness pack: deployment blueprint, controlled migrations,
   alerting/runbooks, secrets model, data protection policy, and provider
   webhook hardening
