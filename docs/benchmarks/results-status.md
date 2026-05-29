# Benchmark Results Status

The repository now includes empirical native Go and k6 smoke, load, stress, and
spike benchmark results for the executable HTTP slice, plus a Compose-backed
smoke profile for the full PostgreSQL, RabbitMQ, Redis, relay, and worker
runtime.

Current results:

- [2026-05-28 native HTTP benchmark](../../benchmarks/results/2026-05-28-native-http-benchmark.md)
- [2026-05-28 k6 smoke test](../../benchmarks/results/2026-05-28-k6-smoke.md)
- [2026-05-28 k6 load test](../../benchmarks/results/2026-05-28-k6-load.md)
- [2026-05-28 k6 stress test](../../benchmarks/results/2026-05-28-k6-stress.md)
- [2026-05-28 k6 spike test](../../benchmarks/results/2026-05-28-k6-spike.md)
- [2026-05-29 Compose smoke profile](../../benchmarks/results/2026-05-29-compose-smoke.md)

## Latest k6 snapshot

| Scenario | p50 | p95 | p99 | Throughput | Error rate |
| --- | ---: | ---: | ---: | ---: | ---: |
| Smoke | 0.65 ms | 4.86 ms | 47.87 ms | 4.968674 req/s | 0.00% |
| Load | 0.85 ms | 2.99 ms | 6.49 ms | 49.842908 req/s | 0.00% |
| Stress | 1.85 ms | 5.74 ms | 9.42 ms | 111.824249 req/s | 0.00% |
| Spike | 2.28 ms | 5.58 ms | 7.92 ms | 143.292968 req/s | 0.00% |

## Latest Compose snapshot

| Scenario | p50 | p95 | p99 | Throughput | Error rate | Async drain |
| --- | ---: | ---: | ---: | ---: | ---: | --- |
| Smoke | 7.91 ms | 23.92 ms | 192.44 ms | 4.929614 req/s | 0.00% | Outbox `0`, RabbitMQ ready `0` |

## Remaining performance gap

The committed Compose smoke profile now measures the full local runtime under
container limits. Compose-backed load, stress, and spike runs are still required
to measure sustained PostgreSQL CPU and connection pool behavior, RabbitMQ queue
depth and consumer lag, Redis limiter behavior, and API memory profile under
higher traffic.

The reproducible harness for that run is now versioned at
[`scripts/run_compose_profile.sh`](../../scripts/run_compose_profile.sh), with
usage notes in [compose-profiling.md](./compose-profiling.md).
