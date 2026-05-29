# Compose Profiling Harness

The Compose profiling harness runs the API, PostgreSQL, RabbitMQ, Redis,
outbox relay, and workers together, then captures resource and queue telemetry
around the k6 scenarios.

## Command

```sh
SCENARIOS='smoke load stress spike' ./scripts/run_compose_profile.sh
```

Use `SCENARIOS='smoke'` for a short validation run. Set `KEEP_STACK=1` to leave
the Compose stack running after the script exits.

## Captured Artifacts

Each run writes a timestamped directory under `benchmarks/results/compose-*`
with:

- Docker version and rendered Compose config
- Docker container CPU and memory snapshots
- API `/metrics` snapshots, including RabbitMQ queue gauges
- RabbitMQ queue state from the management API
- Redis memory information
- PostgreSQL activity counters
- k6 logs and summary exports for each selected scenario

## Current Limitation

The harness is versioned and syntax-validated, but measured Compose results
still require a local Docker daemon. The current workstation cannot connect to
Docker, so the empirical Compose profile remains pending.
