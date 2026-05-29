# Benchmark Results Status

The repository now includes empirical native Go and k6 smoke, load, stress, and
spike benchmark results for the executable HTTP slice.

Current results:

- [2026-05-28 native HTTP benchmark](../../benchmarks/results/2026-05-28-native-http-benchmark.md)
- [2026-05-28 k6 smoke test](../../benchmarks/results/2026-05-28-k6-smoke.md)
- [2026-05-28 k6 load test](../../benchmarks/results/2026-05-28-k6-load.md)
- [2026-05-28 k6 stress test](../../benchmarks/results/2026-05-28-k6-stress.md)
- [2026-05-28 k6 spike test](../../benchmarks/results/2026-05-28-k6-spike.md)

## Latest k6 snapshot

| Scenario | p50 | p95 | p99 | Throughput | Error rate |
| --- | ---: | ---: | ---: | ---: | ---: |
| Smoke | 0.65 ms | 4.86 ms | 47.87 ms | 4.968674 req/s | 0.00% |
| Load | 0.85 ms | 2.99 ms | 6.49 ms | 49.842908 req/s | 0.00% |
| Stress | 1.85 ms | 5.74 ms | 9.42 ms | 111.824249 req/s | 0.00% |
| Spike | 2.28 ms | 5.58 ms | 7.92 ms | 143.292968 req/s | 0.00% |

## Remaining performance gap

The current k6 runs were measured against `go run ./cmd/fulfillhub-api` with the
in-memory store because the local Docker daemon was unavailable. A
compose-backed run is still required to measure PostgreSQL CPU and connection
pool behavior, RabbitMQ queue depth and consumer lag, Redis limiter behavior,
and API memory profile under container limits.

The reproducible harness for that run is now versioned at
[`scripts/run_compose_profile.sh`](../../scripts/run_compose_profile.sh), with
usage notes in [compose-profiling.md](./compose-profiling.md).
