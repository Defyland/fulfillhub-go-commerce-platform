# 2026-05-29 Compose Load, Stress, and Spike Profile

## Command

```sh
POSTGRES_PORT=15432 API_PORT=28080 RABBITMQ_PORT=15674 \
RABBITMQ_MANAGEMENT_PORT=15673 REDIS_PORT=16379 \
PROMETHEUS_PORT=19090 GRAFANA_PORT=13000 \
BASE_URL=http://localhost:28080 SCENARIOS='load stress spike' \
DRAIN_TIMEOUT_SECONDS=240 ./scripts/run_compose_profile.sh
```

Raw artifacts:
[`compose-2026-05-29T06-25-20Z/`](./compose-2026-05-29T06-25-20Z/)

## Runtime Scope

This run exercised the API, PostgreSQL, RabbitMQ, Redis, outbox relay,
inventory worker, payment worker, shipment worker, order finalizer,
notification worker, compensation worker, Prometheus, and Grafana through the
Docker Compose stack.

The harness waited after each scenario for unpublished outbox rows, ready
RabbitMQ messages, and unacknowledged RabbitMQ messages to drain to zero before
capturing post-scenario snapshots.

## k6 Results

| Scenario | Requests | p50 | p95 | p99 | Max | Throughput | Error rate |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| Load | 44,633 | 3.29 ms | 20.22 ms | 58.16 ms | 494.89 ms | 49.555333 req/s | 0.00% |
| Stress | 132,195 | 4.05 ms | 60.43 ms | 323.86 ms | 2073.97 ms | 110.137798 req/s | 0.05% |
| Spike | 42,606 | 3.72 ms | 42.69 ms | 187.62 ms | 1497.55 ms | 141.866448 req/s | 0.03% |

## Drain and Queue State

| Snapshot | Outbox unpublished | RabbitMQ ready | RabbitMQ unacked | Consumers |
| --- | ---: | ---: | ---: | ---: |
| After load | 0 | 0 | 0 | 1 per queue |
| After stress | 0 | 0 | 0 | 1 per queue |
| After spike | 0 | 0 | 0 | 1 per queue |
| Final | 0 | 0 | 0 | 1 per queue |

Final API metrics reported `219617` HTTP requests and `74` HTTP errors across
the full run. RabbitMQ direct snapshots showed all six queues drained with no
ready or unacknowledged messages.

## Resource Snapshot

Final `docker stats` snapshot:

| Service | CPU | Memory |
| --- | ---: | ---: |
| API | 0.03% | 36.95 MiB |
| PostgreSQL | 36.06% | 373.1 MiB |
| RabbitMQ | 1.34% | 471 MiB |
| Redis container | 0.64% | 10.04 MiB |
| Outbox relay | 0.76% | 11.61 MiB |
| Inventory worker | 0.03% | 11.27 MiB |
| Payments worker | 0.04% | 12.75 MiB |
| Shipments worker | 0.00% | 13.52 MiB |

PostgreSQL final activity showed `11` backends, `3596140` commits, and `0`
rollbacks. Redis memory ended at `1.38M` used memory with `4.15M` peak memory.
