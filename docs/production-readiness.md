# Production Readiness

## Purpose

This document defines the production-readiness evidence for FulfillHub. The goal
is not to claim that this repository alone operates a live cloud environment.
The goal is to show the engineering controls expected before a real production
launch: controlled deploys, secret isolation, observable failure modes,
provider-grade boundaries, data recovery, and CI gates that keep those controls
from drifting.

## Current production-readiness scope

| Area | Evidence in this repository | Production dependency |
| --- | --- | --- |
| Deploy and rollback | Kubernetes blueprint in `deployments/kubernetes/base`, controlled migration binary, rollout/rollback runbook | Real cluster, ingress, registry, image tag promotion, managed dependencies |
| Secrets | ExternalSecret blueprint, runtime env contract, rotation documentation | Secrets Manager, Vault, or equivalent KMS-backed provider |
| Providers | Signed webhook verifier, opaque payment/address references, adapter boundary tests | Real PSP/carrier credentials, webhook endpoint wiring, reconciliation jobs |
| Internal contracts | Versioned Protobuf contracts, gRPC/REST error mapping, contract tests | Generated stubs and gRPC server only when an internal process boundary exists |
| Observability | Prometheus metrics, Grafana dashboard, alert rules, SLO runbooks | Production Prometheus/Grafana/Alertmanager and paging integration |
| Data | PostgreSQL migrations with rollback notes, backup/restore policy | Managed PostgreSQL PITR, tested restore target, retention jobs |
| Supply chain | SBOM, Trivy scans, Gitleaks, Docker build validation | Signed release images, provenance attestation, branch protection |
| Performance | k6 and Compose profiling artifacts | Prod-like load environment and capacity plan |
| Resilience | Outbox/inbox, retry/DLQ topology, compensation flows, saga smoke | Chaos exercises against managed dependencies |

## Release gates

Every production candidate should satisfy these gates before promotion:

1. GitHub Actions `project-quality` is green on the release commit.
2. The image was built from the exact commit SHA and scanned with no high or critical unfixed findings.
3. SBOM and provenance are attached to the release artifact.
4. Database migrations were reviewed for expand/contract compatibility and rollback notes.
5. `fulfillhub-migrate` completed before application rollout.
6. `/readyz` is green after rollout for API pods.
7. Prometheus can scrape `/metrics` using `METRICS_BEARER_TOKEN`.
8. Outbox age, queue depth, DLQ depth, and order failure ratio remain below alert thresholds during canary.
9. Optional pprof is disabled unless a time-bounded operator debug session
   explicitly enables it on a protected loopback or private endpoint.
10. Rollback command and previous image tag are known before rollout starts.

## Deployment model

The Kubernetes blueprint uses separate runtime units:

- `fulfillhub-migrate`: one-shot migration job, intended as a pre-sync/pre-rollout step.
- `fulfillhub-api`: horizontally scaled API deployment with liveness and readiness probes.
- `fulfillhub-outbox-relay`: horizontally scaled relay workers protected by Postgres outbox locking.
- `fulfillhub-worker-*`: queue-specific worker deployments so operational teams can scale or pause individual saga branches.

The blueprint intentionally excludes managed PostgreSQL, RabbitMQ, Redis, ingress
controller, DNS, TLS, and cloud IAM because those vary by platform. The runtime
contract is carried by `ConfigMap` and `ExternalSecret` resources, not committed
literal secrets.

Runtime process behavior is documented in [docs/runtime.md](./runtime.md).
Kubernetes environment responsibilities are documented in
[docs/kubernetes.md](./kubernetes.md).

## Migration policy

Migrations must follow expand/contract rules:

1. Add nullable columns, new tables, indexes, and permissive constraints first.
2. Deploy code that can read both old and new shapes.
3. Backfill in bounded batches if data volume requires it.
4. Tighten constraints only after the old write path is gone.
5. Roll back application code before reverting schema unless a migration is explicitly reversible and data-safe.

`fulfillhub-migrate` uses the existing advisory-lock migration runner so only one
release job applies schema changes at a time.

## Rollback policy

Rollback is safe only when the release has maintained backward compatibility
with the previous schema. For each release, record:

- previous image digest
- migration versions applied
- whether the previous binary can run against the current schema
- operational checks after rollback
- manual compensation steps for partially processed provider or saga side effects

Detailed actions live in [runbooks/deployment-rollback.md](./runbooks/deployment-rollback.md).
Alert handling lives in [runbooks/slo-alert-response.md](./runbooks/slo-alert-response.md).
Data recovery and retention live in [runbooks/data-protection.md](./runbooks/data-protection.md).
Secret rotation lives in [security/secrets-management.md](./security/secrets-management.md).
Release integrity lives in [security/supply-chain.md](./security/supply-chain.md).

## Provider hardening

External providers must be treated as untrusted networks:

- Payment and shipment provider secrets are injected through the secret manager.
- Webhooks must be signed with HMAC SHA-256 and verified before parsing business content.
- Webhook timestamps must be inside a bounded tolerance window.
- Event IDs must be recorded in a replay store before side effects run.
- Duplicate provider events must be acknowledged idempotently.
- Provider reconciliation must compare local projections against provider state after outages.

The reusable verifier is implemented in `internal/providers/webhook.go`.

## Production gap log

These items remain intentionally platform-dependent and should be implemented
when attaching the repository to a real environment:

- Cloud-specific Terraform or Pulumi for VPC, database, broker, Redis, IAM, DNS, TLS, and observability backends.
- Real PSP and carrier adapters with sandbox contract tests.
- Generated Protobuf stubs and a gRPC runtime only when the monolith is split by
  an actual internal process boundary.
- Webhook HTTP endpoints and reconciliation workers for the selected providers.
- Signed image publishing with organization-controlled keyless identity.
- PITR restore drill against a real managed PostgreSQL snapshot.
- Prod-like performance test using realistic merchant and SKU distribution.
