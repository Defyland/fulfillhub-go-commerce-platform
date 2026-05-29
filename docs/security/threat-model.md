# Security Threat Model

## Assets

| Asset | Risk | Primary controls |
| --- | --- | --- |
| Merchant orders | Cross-tenant reads or writes | API-key derived `merchant_id`, tenant checks, scoped unique constraints |
| Payment tokens | Accidental persistence or log leakage | Request validation, no token logging, provider abstraction boundary |
| Idempotency keys | Replay or duplicate order creation | Per-merchant idempotency records and duplicate external-order rejection |
| Outbox messages | Duplicate side effects | Message IDs, inbox deduplication, explicit acknowledgements |
| Operator actions | Unauthorized cancellation or replay | Signed operations JWT, requested actor metadata, audit logs |
| Secrets | Credential disclosure | Environment variables only, secret scanning in CI |

## Trust Boundaries

- Merchant storefronts call `/api/v1` with `X-API-Key`.
- Operations callers use `Authorization: Bearer <jwt>` when `OPS_JWT_SECRET` is configured.
- `Bearer ops-token` is accepted only for local development when no JWT secret is configured.
- PostgreSQL is the durable consistency boundary for orders, outbox, inbox, and audit logs.
- RabbitMQ is an at-least-once delivery boundary; consumers must be idempotent.
- Redis is a control-plane dependency for rate limiting when `REDIS_URL` is configured.

## Implemented Controls

- Merchant identity is derived from API key configuration, never from request bodies.
- Merchant callers can only read or cancel orders belonging to their own `merchant_id`.
- Create-order requests require `Idempotency-Key` and reject duplicate external order IDs per merchant.
- Redis-backed rate limiting protects write traffic when enabled.
- Structured logs include request and correlation IDs without logging payment tokens.
- HTTP and RabbitMQ publish/consume spans propagate W3C `traceparent` for incident correlation.
- `order.create`, `order.cancel_requested`, and worker-driven `order.completed`
  audit logs are written with actor and correlation metadata.
- Worker-driven inventory, payment, and shipment projections write audit logs
  before their follow-up outbox events are published.
- Worker-driven email notification queueing writes audit logs for customer
  communication diagnostics.
- Worker-driven compensation records write audit logs for inventory, payment,
  and shipment failure diagnostics.
- DLQ replay requires `DATABASE_URL` and `OPS_ACTOR_ID`, then records durable
  `dlq.replay` audit details for success and failure attempts.
- Operations JWTs are validated with HS256, expiry, subject, `operations` or
  `ops` role claims, optional issuer/audience checks, and previous secrets
  during key rotation.
- CI runs Go tests, race detection, OpenAPI linting, markdown linting, Docker build validation, and gitleaks.

## Known Gaps

- `/metrics` is unauthenticated in the local slice and should be network-restricted in production.
- Compose-backed load, stress, and spike telemetry exists for local profiling;
  production deployment would still need authenticated metrics and network
  policy around the observability endpoints.
