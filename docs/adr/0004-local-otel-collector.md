# ADR 0004: Local OpenTelemetry Collector

## Status

Accepted

## Context

The general engineering baseline requires traces and OpenTelemetry as the
default observability standard. FulfillHub already creates HTTP, PostgreSQL,
RabbitMQ publish, outbox relay, and RabbitMQ consume spans, but a stdout-only
export path is insufficient evidence for a product-like runtime topology.

## Decision

The local Docker Compose stack includes an OpenTelemetry Collector receiving
OTLP/HTTP and OTLP/gRPC traffic. Go executables share one tracing configuration
package and support three modes:

- disabled tracing when `OTEL_TRACES_EXPORTER` is unset or `none`
- pretty stdout tracing with `OTEL_TRACES_EXPORTER=stdout`
- OTLP/HTTP export with `OTEL_TRACES_EXPORTER=otlp`

Compose uses OTLP/HTTP by default and assigns explicit `OTEL_SERVICE_NAME`
values per API, relay, and worker process.

## Consequences

- Local runtime traces now follow the same collector boundary expected from a
  production deployment.
- Unit tests remain deterministic because tracing is disabled unless explicitly
  configured.
- The collector currently exports traces through the debug exporter; a later
  production deployment can replace that with Tempo, Jaeger, or a vendor OTLP
  endpoint without changing application code.
