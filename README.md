# FulfillHub

Checkout and fulfillment platform built in Go to showcase distributed systems architecture.

## Status

Phase 0 bootstrap only. This repository currently establishes naming, scope, documentation structure, and engineering expectations. It does not yet contain service code or Go workspace scaffolding.

## Product intent

FulfillHub is planned as a commerce platform coordinating order creation, inventory reservation, payment authorization, shipment creation, customer notification, and compensating flows.

## Planned stack

- Go
- PostgreSQL
- RabbitMQ
- Redis
- OpenTelemetry
- Prometheus and Grafana
- Docker Compose
- k6

## Engineering focus

This project is meant to demonstrate:

- saga orchestration
- transactional outbox and inbox patterns
- idempotent consumers
- retry and dead-letter flows
- distributed tracing
- failure-aware integration tests

## Bootstrap contents

- repository initialized and synchronized with GitHub
- mandatory documentation folders created
- baseline engineering spec captured in `docs/engineering-baseline.md`

## Next phase

The first implementation slice should prioritize order lifecycle, RabbitMQ abstractions, outbox persistence, saga orchestration, and inbox-based duplicate protection.
