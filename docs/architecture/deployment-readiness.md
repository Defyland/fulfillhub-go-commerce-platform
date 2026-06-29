# Deployment Readiness

FulfillHub already has production-like concerns in its local architecture: PostgreSQL, RabbitMQ, Redis, health checks, readiness checks, metrics, tracing, and worker processes.

## Current posture

- Separate API, relay, and worker executables.
- Docker and Docker Compose support for local dependency orchestration.
- `/healthz`, `/readyz`, and `/metrics` endpoints.
- CI validation for tests, OpenAPI, Docker build, supply chain, and production readiness.
- Kubernetes blueprint under `deployments/kubernetes/base` with separate API,
  relay, worker, and migration resources.
- External Secret contract for database, broker, cache, JWT, metrics, and
  provider webhook credentials.
- Production alert rules and runbooks for rollback, SLO response, data
  protection, secrets, supply chain, and event contract changes.

## Railway omission is intentional

This repository does not provide a Railway deployment. A truthful runnable
slice needs the `fulfillhub-migrate` release step, the public API, the outbox
relay, queue-specific worker processes, PostgreSQL, RabbitMQ, Redis, and the
associated ops or metrics secrets.

A single-service Railway deploy would be misleading because it would drop the
relay and worker topology that the repository uses to prove saga durability,
retry behavior, DLQ handling, and release sequencing. The supported runnable
surfaces in this repo are Docker Compose for local end-to-end evidence and the
Kubernetes blueprint for production-like topology.

## Platform integration still required

- Cloud-specific Terraform/Pulumi or Helm overlays are still required for real
  VPC, managed PostgreSQL, RabbitMQ, Redis, ingress, TLS, DNS, IAM, and
  observability backends.
- Service mesh is deferred because broker-level idempotency, retries, DLQ, and tracing are more important for this slice.
- Canary automation should be added when a real deployment platform owns image
  promotion and traffic routing.

## Release gates

- `project-quality` must be green for the release commit.
- `fulfillhub-migrate` must complete before API or worker rollout.
- `/readyz` must stay healthy after rollout.
- Outbox age, queue backlog, DLQ depth, manual-review count, and failure ratio
  must stay below alert thresholds during canary.
- Rollback must use the previous immutable image digest and preserve migration
  compatibility.
