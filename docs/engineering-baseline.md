# FulfillHub Engineering Baseline

This repository applies the portfolio-wide engineering spec to a commerce orchestration domain. The current state includes a Phase 1 executable Go slice plus the product narrative, architecture decisions, contracts, quality gates, and operational expectations needed for the next persistence and messaging phases.

## Required artifacts in this repository

- Product and engineering entrypoint in `README.md`
- Contract-first HTTP API in `openapi.yaml`
- API examples and error model in `docs/api/`
- Architecture notes and data model in `docs/architecture/`
- Architecture decision records in `docs/adr/`
- Event topology in `docs/events/`
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
- in-memory order store and outbox event recording
- unit, request, authorization, validation, and native benchmark coverage

It intentionally does not include yet:

- database migrations
- Docker Compose runtime
- live RabbitMQ, PostgreSQL, or Redis integrations
- k6 network load test results

The next phase must add durable persistence and messaging while preserving the contracts and decisions already documented here.
