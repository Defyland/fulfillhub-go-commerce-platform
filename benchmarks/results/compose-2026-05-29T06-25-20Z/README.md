# Compose Profiling Run

- Timestamp: 2026-05-29T06-25-20Z
- Base URL: `http://localhost:28080`
- Scenarios: load stress spike
- Async drain timeout: 240s
- Result directory: benchmarks/results/compose-2026-05-29T06-25-20Z

Captured artifacts include Docker stats, API Prometheus metrics, RabbitMQ queue
state, Redis memory info, PostgreSQL activity, k6 logs, k6 summary exports, and
post-scenario snapshots taken only after unpublished outbox, ready queue, and
unacknowledged queue metrics drain to zero.
