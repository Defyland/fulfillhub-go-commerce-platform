# 2026-05-29 Compose Smoke Profile

## Command

```sh
POSTGRES_PORT=15432 \
API_PORT=28080 \
RABBITMQ_PORT=15674 \
RABBITMQ_MANAGEMENT_PORT=15673 \
REDIS_PORT=16379 \
PROMETHEUS_PORT=19090 \
GRAFANA_PORT=13000 \
BASE_URL=http://localhost:28080 \
SCENARIOS=smoke \
./scripts/run_compose_profile.sh
```

## Scope

This run exercised the Docker Compose stack with the API, PostgreSQL,
RabbitMQ, Redis, outbox relay, inventory worker, payment worker, shipment
worker, order finalizer, notification worker, compensation worker, Prometheus,
and Grafana.

Raw artifacts are stored under
[`compose-2026-05-29T03-42-51Z`](./compose-2026-05-29T03-42-51Z/).

## k6 Summary

| Metric | Value |
| --- | ---: |
| Requests | 300 |
| Throughput | 4.929614 req/s |
| Error rate | 0.00% |
| p50 latency | 7.91 ms |
| p95 latency | 23.92 ms |
| p99 latency | 192.44 ms |
| Max latency | 225.56 ms |

## Resource Snapshot

| Component | CPU | Memory |
| --- | ---: | ---: |
| API | 0.00% | 10.71 MiB |
| PostgreSQL | 0.32% | 50.49 MiB |
| RabbitMQ | 2.11% | 140.9 MiB |
| Redis | 2.60% | 9.47 MiB container memory, 1.17 MiB Redis used memory |
| Outbox relay | 0.11% | 8.53 MiB |

## Async Drain

- `fulfillhub_outbox_unpublished_total`: `0`
- RabbitMQ ready messages: `0` across all declared queues
- Consumers: `1` on inventory, payment, shipment, finalizer, compensation, and notification queues
- PostgreSQL activity after smoke: `5297` commits, `0` rollbacks, `10` active backends

## Notes

This closes the first Compose-backed empirical performance artifact. A later
Compose load, stress, and spike profile covers sustained and burst behavior.
