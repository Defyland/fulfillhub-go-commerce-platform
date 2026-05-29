# Benchmark Results Status

The repository now includes empirical native Go and k6 smoke, load, stress, and
spike benchmark results for the executable HTTP slice, plus Compose-backed
smoke, load, stress, and spike profiles for the full PostgreSQL, RabbitMQ,
Redis, relay, and worker runtime.

Current results:

- [2026-05-28 native HTTP benchmark](../../benchmarks/results/2026-05-28-native-http-benchmark.md)
- [2026-05-28 k6 smoke test](../../benchmarks/results/2026-05-28-k6-smoke.md)
- [2026-05-28 k6 load test](../../benchmarks/results/2026-05-28-k6-load.md)
- [2026-05-28 k6 stress test](../../benchmarks/results/2026-05-28-k6-stress.md)
- [2026-05-28 k6 spike test](../../benchmarks/results/2026-05-28-k6-spike.md)
- [2026-05-29 Compose smoke profile](../../benchmarks/results/2026-05-29-compose-smoke.md)
- [2026-05-29 Compose load, stress, and spike profile](../../benchmarks/results/2026-05-29-compose-load-stress-spike.md)

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
| Load | 3.29 ms | 20.22 ms | 58.16 ms | 49.555333 req/s | 0.00% | Outbox `0`, RabbitMQ ready `0`, unacked `0` |
| Stress | 4.05 ms | 60.43 ms | 323.86 ms | 110.137798 req/s | 0.05% | Outbox `0`, RabbitMQ ready `0`, unacked `0` |
| Spike | 3.72 ms | 42.69 ms | 187.62 ms | 141.866448 req/s | 0.03% | Outbox `0`, RabbitMQ ready `0`, unacked `0` |

## Compose Profiling Notes

The committed Compose profiles now measure the full local runtime under
container limits. The load/stress/spike run ended with PostgreSQL at `373.1
MiB`, RabbitMQ at `471 MiB`, API at `36.95 MiB`, Redis at `1.38M` used memory
and `4.15M` peak memory, and PostgreSQL reporting `0` rollbacks.
