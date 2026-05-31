# Go Architecture

FulfillHub uses Go packages as architectural boundaries. Package names describe
business or adapter roles, not framework layers.

## Package Roles

| Package | Architectural role |
| --- | --- |
| `internal/commerce` | Order domain, order use cases, state machine, domain errors, store port, memory fake adapter |
| `internal/fulfillment` | Saga application use cases and provider-facing ports for inventory, payment, shipping, cancellation, notification, and compensation |
| `internal/api` | Primary HTTP adapter, auth, REST DTOs, error envelope mapping, metrics/readiness endpoints |
| `internal/postgres` | Secondary persistence adapter for orders, outbox, inbox, projections, audit, and migrations |
| `internal/messaging` | RabbitMQ secondary adapter, relay, consumer, inbox idempotency, retry/DLQ topology |
| `internal/providers` | Provider request/webhook primitives and adapter tests |
| `internal/ratelimit` | Redis-backed secondary adapter for rate limiting |
| `internal/observability` | OpenTelemetry process wiring |
| `internal/spec` | Architecture, contract, readiness, and documentation drift tests |
| `cmd/*` | CLI/process composition roots |

## Composition Roots

The `cmd` packages wire adapters to use cases. They may know about environment
variables, process signals, runtime config, and concrete adapters. Inner
packages must not.

Examples:

- `cmd/fulfillhub-api` wires `postgres.Store`, Redis limiter, RabbitMQ queue
  inspector, tracing, and the HTTP adapter.
- `cmd/fulfillhub-worker` wires `postgres.Store`, provider adapters, and a
  queue-specific fulfillment handler.
- `cmd/fulfillhub-outbox-relay` wires the Postgres outbox port to RabbitMQ.

## Naming

The existing `commerce.Service` is an application service/use-case coordinator.
It is not an anemic repository wrapper: it validates command input, enforces
idempotency shape, creates aggregates, derives opaque references, emits
versioned events, and writes audit records through a port.

The repository avoids introducing a generic `usecase` package because Go
package names should stay close to the business capability. The architecture is
enforced by dependencies and tests, not folder ceremony.
