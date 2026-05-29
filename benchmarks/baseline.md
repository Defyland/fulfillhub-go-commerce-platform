# FulfillHub Benchmark Baseline

## Scope

This document defines the benchmark matrix and acceptance targets for
FulfillHub. The repository now has a native in-process Go benchmark, k6 smoke,
load, stress, and spike results against the local in-memory API process, and
Compose-backed smoke, load, stress, and spike profiles against the full local
runtime.

## Planned scenarios

| Scenario | Goal | Traffic profile | Success criteria |
| --- | --- | --- | --- |
| Smoke | Catch obvious regressions before merge | 5 virtual users for 1 minute | Zero server errors, readiness stays green |
| Load | Validate steady-state checkout throughput | 50 virtual users for 15 minutes | p95 create-order latency under 250 ms, error rate under 1% |
| Stress | Find degradation point | Ramp from 50 to 250 virtual users over 20 minutes | Graceful saturation, no duplicate orders, no data corruption |
| Spike | Observe sudden demand bursts | Jump from 20 to 200 virtual users in 30 seconds | Rate limiting and queue buffering behave predictably |

The k6 scripts live under `benchmarks/k6/`.

## Current measured baseline

| Benchmark | Result | Scope |
| --- | --- | --- |
| `BenchmarkCreateOrder-10` | 15291 ns/op | In-process Go HTTP handler via `httptest` |
| k6 smoke | p95 4.86 ms, p99 47.87 ms, 0.00% errors | 5 VUs for 1 minute against in-memory API |
| k6 load | p95 2.99 ms, p99 6.49 ms, 0.00% errors | 50 VUs for 15 minutes against in-memory API |
| k6 stress | p95 5.74 ms, p99 9.42 ms, 0.00% errors | 50 to 250 VU ramp over 20 minutes against in-memory API |
| k6 spike | p95 5.58 ms, p99 7.92 ms, 0.00% errors | 20 to 200 VU spike over 5 minutes against in-memory API |
| Compose k6 smoke | p95 23.92 ms, p99 192.44 ms, 0.00% errors | 5 VUs for 1 minute against API, PostgreSQL, RabbitMQ, Redis, relay, and workers |
| Compose k6 load | p95 20.22 ms, p99 58.16 ms, 0.00% errors | 50 VUs for 15 minutes against API, PostgreSQL, RabbitMQ, Redis, relay, and workers |
| Compose k6 stress | p95 60.43 ms, p99 323.86 ms, 0.05% errors | 50 to 250 VU ramp over 20 minutes against API, PostgreSQL, RabbitMQ, Redis, relay, and workers |
| Compose k6 spike | p95 42.69 ms, p99 187.62 ms, 0.03% errors | 20 to 200 VU spike over 5 minutes against API, PostgreSQL, RabbitMQ, Redis, relay, and workers |

## Measured metrics

Every benchmark run must record:

- p50 latency
- p95 latency
- p99 latency
- throughput
- error rate
- PostgreSQL CPU and connection pool notes
- RabbitMQ queue depth and consumer lag notes
- memory profile of the API process

## Target endpoints

- `POST /api/v1/orders`
- `GET /api/v1/orders/{orderId}`
- `POST /api/v1/orders/{orderId}/cancel`

## Performance phase closure

The current Compose profiling run captures k6 load, stress, and spike against
the Docker Compose stack, PostgreSQL CPU and connection notes, RabbitMQ ready
and unacknowledged queue drain, Redis memory behavior, and API memory under
sustained and burst container limits.
