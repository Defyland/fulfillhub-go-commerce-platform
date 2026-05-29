# Secrets Management

## Purpose

FulfillHub must treat secrets as runtime infrastructure, not repository content.
This document defines the production model for API keys, provider credentials,
JWT signing secrets, metrics tokens, database URLs, broker URLs, and webhook
signing secrets.

## Secret sources

Production deployments should use one of these KMS-backed stores:

- AWS Secrets Manager with KMS customer-managed keys
- HashiCorp Vault with Kubernetes auth
- GCP Secret Manager or Azure Key Vault for those platforms

Kubernetes receives secrets through External Secrets or an equivalent operator.
The repository contains only the `ExternalSecret` contract under
`deployments/kubernetes/base/external-secret.yaml`.

## Rotation policy

| Secret | Rotation cadence | Safe rotation strategy |
| --- | --- | --- |
| `OPS_JWT_SECRET` | 90 days or incident-driven | Set previous secret in `OPS_JWT_PREVIOUS_SECRETS`, deploy, rotate clients, then remove old value |
| `METRICS_BEARER_TOKEN` | 180 days | Update Prometheus secret and API pods in the same rollout window |
| `DATABASE_URL` | Managed credential rotation | Use dual users or managed rotation window with readiness verification |
| `RABBITMQ_URL` | 180 days | Create new user, grant vhost permissions, deploy, then revoke old user |
| `PAYMENT_WEBHOOK_SECRET` | Provider-driven | Accept current and previous HMAC secret during provider rotation |
| `SHIPMENT_WEBHOOK_SECRET` | Provider-driven | Accept current and previous HMAC secret during provider rotation |

## Access control

- Application pods can read only runtime secrets required by their process.
- CI can read package publish credentials only in protected release workflows.
- Operators can read break-glass secrets only through audited access.
- Merchant API keys should be scoped per merchant and revocable without code deploys.
- Provider credentials should be scoped by environment and capability.

## Logging and audit controls

- Never log raw `DATABASE_URL`, `RABBITMQ_URL`, `REDIS_URL`, JWTs, API keys, or webhook secrets.
- Store payment credentials as opaque references, not card data or provider tokens.
- Audit secret rotations with actor, reason, secret name, and affected environment.
- Treat failed webhook signature verification as a security signal when it spikes.

## Incident response

1. Revoke the suspected secret in the provider or secret manager.
2. Deploy a new value and keep previous-secret compatibility only when safe.
3. Rotate dependent clients or provider webhook configuration.
4. Search logs and traces for accidental secret exposure.
5. Record incident scope, exposure window, and revocation evidence.
