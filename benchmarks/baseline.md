# FulfillHub Benchmark Baseline

## Scope

This document defines the benchmark matrix and acceptance targets for FulfillHub. The repository now has a native in-process Go benchmark; k6 network load tests remain the next performance milestone.

## Planned scenarios

| Scenario | Goal | Traffic profile | Success criteria |
| --- | --- | --- | --- |
| Smoke | Catch obvious regressions before merge | 5 virtual users for 1 minute | Zero server errors, readiness stays green |
| Load | Validate steady-state checkout throughput | 50 virtual users for 15 minutes | p95 create-order latency under 250 ms, error rate under 1% |
| Stress | Find degradation point | Ramp from 50 to 250 virtual users over 20 minutes | Graceful saturation, no duplicate orders, no data corruption |
| Spike | Observe sudden demand bursts | Jump from 20 to 200 virtual users in 30 seconds | Rate limiting and queue buffering behave predictably |

## Current measured baseline

| Benchmark | Result | Scope |
| --- | --- | --- |
| `BenchmarkCreateOrder-10` | 15275 ns/op | In-process Go HTTP handler via `httptest` |

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

## Exit criteria for the next performance phase

- k6 smoke result committed under `benchmarks/results/`
- k6 load result committed under `benchmarks/results/`
- Result summary mirrored into `docs/benchmarks/`
- Reproducible k6 command line recorded in repository docs
