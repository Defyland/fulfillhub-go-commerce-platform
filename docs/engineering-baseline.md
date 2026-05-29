# FulfillHub Engineering Baseline

This repository applies the portfolio-wide engineering spec to a commerce
orchestration domain. The current state includes an executable Go API slice,
PostgreSQL persistence, RabbitMQ outbox relay code, Redis rate limiting, inbox
idempotency, DLQ replay tooling, provider adapters, k6 scripts, measured
results, Compose smoke/load/stress/spike profiling, Grafana dashboard definition, product
narrative, architecture decisions, contracts, quality gates, and operational
expectations.

## Required artifacts in this repository

- Product and engineering entrypoint in `README.md`
- Contract-first HTTP API in `openapi.yaml`
- API examples and error model in `docs/api/`
- Architecture notes and data model in `docs/architecture/`
- Architecture decision records in `docs/adr/`
- Event topology in `docs/events/`
- Threat model and authorization matrix in `docs/security/`
- Visual diagrams in `docs/diagrams/`
- Performance methodology in `docs/benchmarks/` and `benchmarks/`
- Operational failure handling in `docs/runbooks/`
- Repository quality gates in `.github/workflows/phase0-quality.yml`
- Go API runtime under `cmd/fulfillhub-api`
- Go domain and HTTP tests under `internal/`
- Docker build definition in `Dockerfile`

## FulfillHub-specific engineering commitments

- Order orchestration is modeled as an explicit saga with observable state transitions
- State-changing publishes are expected to use a transactional outbox
- Consumers are expected to use inbox deduplication and explicit acknowledgements
- Correlation identifiers must propagate across HTTP, SQL, and RabbitMQ boundaries
- Tenant isolation is non-negotiable on every read and write path
- Failure scenarios must be testable, not only documented

## Current boundary

The current executable slice includes:

- Go module scaffolding
- HTTP order creation, lookup, and cancellation handlers
- health, readiness, and metrics endpoints
- structured HTTP request logs plus OpenTelemetry HTTP, SQL, RabbitMQ publish, and RabbitMQ consume spans
- in-memory order store and outbox event recording
- embedded PostgreSQL migrations and SQL-backed order/outbox/audit persistence
- RabbitMQ publisher topology and outbox relay process
- RabbitMQ consumer primitive with inbox deduplication and explicit ack/nack behavior
- fulfillment worker executable with durable inventory, payment, shipment, notification, compensation, and order-completion projections
- live RabbitMQ topology integration coverage
- Redis-backed rate limiting
- inbox idempotency primitives
- DLQ replay command with durable audit records
- payment and shipment provider adapter interfaces
- k6 smoke, load, stress, and spike scripts
- measured k6 smoke, load, stress, and spike results
- measured Compose smoke, load, stress, and spike profiling with outbox and queue drain
- Grafana dashboard definition
- unit, request, authorization, validation, audit, worker, integration, race, and native benchmark coverage

The longer measured performance matrix now runs against the full Docker Compose
stack and records PostgreSQL, RabbitMQ, Redis, API, relay, and worker resource
snapshots.
