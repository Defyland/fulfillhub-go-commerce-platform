# Benchmark Results

Measured benchmark artifacts:

- [2026-05-28 native HTTP benchmark](./2026-05-28-native-http-benchmark.md)
- [2026-05-28 k6 smoke test](./2026-05-28-k6-smoke.md)
- [2026-05-28 k6 load test](./2026-05-28-k6-load.md)
- [2026-05-28 k6 stress test](./2026-05-28-k6-stress.md)
- [2026-05-28 k6 spike test](./2026-05-28-k6-spike.md)

Raw k6 summary exports:

- [2026-05-28 k6 load summary](./2026-05-28-k6-load-summary.json)
- [2026-05-28 k6 stress summary](./2026-05-28-k6-stress-summary.json)
- [2026-05-28 k6 spike summary](./2026-05-28-k6-spike-summary.json)

The next compose-backed performance run should add PostgreSQL CPU, connection
pool, RabbitMQ queue depth, consumer lag, Redis limiter, and API memory profile
notes.
