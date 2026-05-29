# Compose Profiling Harness

The Compose profiling harness runs the API, PostgreSQL, RabbitMQ, Redis,
OpenTelemetry Collector, outbox relay, and workers together, then captures
resource and queue telemetry around the k6 scenarios.

## Command

```sh
SCENARIOS='smoke load stress spike' ./scripts/run_compose_profile.sh
```

Use `SCENARIOS='smoke'` for a short validation run. Set `KEEP_STACK=1` to leave
the Compose stack running after the script exits. The harness sets
`RATE_LIMIT_PER_MINUTE=60000` by default so latency profiling is not dominated
by the operational Redis write quota; override it lower when intentionally
profiling limiter rejections. It also waits up to `DRAIN_TIMEOUT_SECONDS=60`
for unpublished outbox events plus ready and unacknowledged RabbitMQ messages
to drain before taking post-scenario snapshots.

When local services already occupy the default host ports, override only the
host bindings. Internal container URLs remain unchanged:

```sh
POSTGRES_PORT=15432 API_PORT=18080 \
BASE_URL='http://localhost:18080' \
SCENARIOS='smoke' ./scripts/run_compose_profile.sh
```

## Captured Artifacts

Each run writes a timestamped directory under `benchmarks/results/compose-*`
with:

- Docker version and rendered Compose config
- Docker container CPU and memory snapshots
- OpenTelemetry Collector service wiring and OTLP exporter environment
- API `/metrics` snapshots, including RabbitMQ queue gauges, scraped with the
  local Compose metrics bearer token
- RabbitMQ queue state from the management API
- Redis memory information
- PostgreSQL activity counters
- post-scenario snapshots after outbox plus ready and unacknowledged queue drain
- k6 logs and summary exports for each selected scenario

## Current Status

The harness is versioned, syntax-validated, and has a committed Compose smoke
profile under
[`benchmarks/results/compose-2026-05-29T03-42-51Z`](../../benchmarks/results/compose-2026-05-29T03-42-51Z/).
Measured Compose load, stress, and spike profiles are committed under
[`benchmarks/results/compose-2026-05-29T06-25-20Z`](../../benchmarks/results/compose-2026-05-29T06-25-20Z/).

## CI Coverage

`.github/workflows/phase0-quality.yml` runs a Compose smoke profile with the
`smoke` scenario on GitHub-hosted Linux runners and uploads the captured
profiling artifacts. Longer load, stress, and spike profiles are intentionally
local because they take substantially longer than the CI smoke gate.
