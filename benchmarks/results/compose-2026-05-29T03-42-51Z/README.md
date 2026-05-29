# Compose Profiling Run

- Timestamp: 2026-05-29T03-42-51Z
- Base URL: `http://localhost:28080`
- Scenarios: smoke
- Async drain timeout: 60s
- Result directory: benchmarks/results/compose-2026-05-29T03-42-51Z

Captured artifacts include Docker stats, API Prometheus metrics, RabbitMQ queue
state, Redis memory info, PostgreSQL activity, k6 logs, k6 summary exports, and
post-scenario snapshots taken only after unpublished outbox and ready queue
metrics drain to zero.
