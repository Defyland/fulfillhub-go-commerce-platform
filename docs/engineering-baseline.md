# FulfillHub Engineering Baseline

This repository follows the initiative-wide standards below.

## Mandatory outcomes

- Product-grade `README.md` with product and engineering sections
- `openapi.yaml` once the HTTP surface exists
- `docs/adr/`, `docs/architecture/`, `docs/events/`, `docs/benchmarks/`, `docs/api/`, `docs/diagrams/`, and `docs/runbooks/`
- atomic Conventional Commit history
- GitHub Actions for lint, tests, security, build, coverage, and OpenAPI validation
- observability with structured logs, metrics, traces, request IDs, and readiness endpoints
- documented k6 performance baselines

## FulfillHub-specific emphasis

- explicit saga state machine
- transactional outbox on state-changing publishers
- inbox tables for idempotent consumers
- correlation, causation, and trace propagation across services
- retry, DLQ, and consumer acknowledgement coverage
- contract and failure tests around event schemas and compensation flows

## Phase 0 boundary

This repository intentionally stops before scaffolding the Go workspace or services. The goal of this phase is only to lock scope and standards.
