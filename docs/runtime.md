# Runtime Operations

This document describes the local and production-like runtime contract for the
current FulfillHub executables. It is intentionally limited to behavior this
repository can run or validate without external production infrastructure.

## Processes

| Process | Purpose | Required durable dependencies |
| --- | --- | --- |
| `fulfillhub-api` | Public REST API, readiness, metrics, optional pprof | PostgreSQL unless `ALLOW_IN_MEMORY_STORE=true` |
| `fulfillhub-outbox-relay` | Publishes transactional outbox rows to RabbitMQ | PostgreSQL, RabbitMQ |
| `fulfillhub-worker` | Consumes saga queues and writes projections | PostgreSQL, RabbitMQ |
| `fulfillhub-migrate` | Applies embedded PostgreSQL migrations | PostgreSQL |
| `fulfillhub-dlq-replay` | Audited replay from a DLQ to a target routing key | PostgreSQL, RabbitMQ |

## API Runtime Hardening

- `fulfillhub-api` handles `SIGINT` and `SIGTERM` with `signal.NotifyContext`.
- API shutdown calls `http.Server.Shutdown` with a bounded timeout.
- Startup logs include `addr`, `gomaxprocs`, and `num_cpu`.
- HTTP server timeouts are explicit: read-header, read, write, and idle.
- Request bodies are bounded with `http.MaxBytesReader`.
- JSON command payloads reject unknown fields.
- PostgreSQL is required for production-like API runs unless
  `ALLOW_IN_MEMORY_STORE=true` is set explicitly.
- PostgreSQL uses explicit `database/sql` pool defaults in `internal/postgres`.
- Redis and RabbitMQ startup checks run with bounded contexts.
- OpenTelemetry tracing is disabled by default, can log spans to stdout, or can
  export OTLP/HTTP to the local collector.

## pprof

pprof is optional and disabled by default.

| Env var | Default | Description |
| --- | --- | --- |
| `ENABLE_PPROF` | `false` | Starts a separate pprof HTTP server when `true`. |
| `PPROF_ADDR` | `127.0.0.1:6060` | Bind address for pprof endpoints. Keep loopback unless protected by network policy. |

Available endpoints are under `/debug/pprof/` on the pprof server, not the
public API server. The Kubernetes blueprint does not enable pprof by default.

## Health, Readiness, and Metrics

- `/healthz` is process liveness only.
- `/readyz` checks configured store, broker, and cache dependencies.
- `/metrics` exports Prometheus text metrics and can be protected with
  `METRICS_BEARER_TOKEN`.

## Current Local Gap

The repository does not run a generated internal gRPC server yet. Protobuf
contracts under `proto/` define the intended internal API shape, while the
current executable runtime remains REST + transactional outbox + workers.
