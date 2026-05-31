# Testing Strategy

FulfillHub uses tests as architecture guardrails, not only correctness checks.

## Test Layers

| Layer | Purpose | Examples |
| --- | --- | --- |
| Domain invariant tests | Validate state transitions, validation errors, duplicate SKU handling, cancellation invariants without infrastructure | `internal/commerce/*_test.go` |
| Use-case tests with fakes | Exercise application services through memory/fake adapters | `commerce.NewMemoryStore`, fulfillment handler tests |
| Adapter integration tests | Verify SQL migrations, Postgres projections, RabbitMQ confirms, Redis limiter behavior, provider webhook verification | `internal/postgres`, `internal/messaging`, `internal/ratelimit`, `internal/providers` |
| Contract tests | Prevent docs, OpenAPI, Protobuf, event schemas, runtime events, and deployment artifacts from drifting | `internal/spec` |
| Runtime tests | Verify process timeouts, pprof opt-in, readiness, metrics, tracing, shutdown, and command config | `cmd/*`, `internal/api`, `internal/observability` |
| Performance tests | Benchmark HTTP path and local Compose runtime with k6 smoke/load/stress/spike profiles | `benchmarks` |

## What Must Stay True

- Handlers remain thin and delegate business decisions to use cases.
- HTTP DTOs map into use-case commands.
- Domain invariants run without Postgres, RabbitMQ, Redis, or provider clients.
- Use cases are testable through fake or memory adapters.
- Adapters get integration tests when they own infrastructure behavior.
- Event contracts are tested both as schemas and as runtime-produced events.

## Known Local Constraint

Some tests use local TCP listeners such as `miniredis`. In restricted sandboxes,
those tests can fail with listener permission errors even though they pass on a
normal developer machine and in CI. That is a sandbox constraint, not a design
dependency on external infrastructure.
