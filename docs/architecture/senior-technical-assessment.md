# Senior Technical Assessment

This document evaluates the project author against
[`specs/general-project-spec.md`](../../specs/general-project-spec.md) as
evidence of senior backend engineering quality.

## Assessment outcome

The current repository clears a senior-level bar for this portfolio context.
The strongest evidence is not isolated code style; it is end-to-end system
thinking across contracts, transactional boundaries, failure handling,
observability, security controls, performance evidence, and disciplined commit
history.

## What already demonstrates seniority

- The repository is product-shaped, not just framework-shaped. `README.md`,
  `openapi.yaml`, runbooks, ADRs, architecture notes, and benchmark artifacts
  tell one coherent story.
- The design favors explicit boundaries over accidental coupling. Orders,
  persistence, messaging, fulfillment, and provider adapters are separated by
  narrow interfaces instead of leaking infrastructure details through the
  domain.
- Reliability concerns were designed in early: transactional outbox, inbox
  deduplication, retry queues, DLQs, causation IDs, correlation IDs, and DLQ
  replay audit logging are senior concerns, not junior afterthoughts.
- Data consistency is treated as a first-class problem. The PostgreSQL layer
  carries foreign keys, unique constraints, indexes, version increments,
  transaction boundaries, rollback notes, and compensation side effects.
- The implementation includes operational evidence, not only claims: health and
  readiness probes, Prometheus metrics, OpenTelemetry traces, Grafana
  dashboards, runbooks, and measured k6 plus Compose benchmark results.
- The commit history shows intent. The sequence of focused commits demonstrates
  incremental delivery instead of a single opaque dump.

## What needed to be better technically

- Documentation drift existed after the runtime introduced `manual_review`.
  This was a real quality gap because operational docs still described the
  older `cancellation_pending` interpretation for that failure path.
- That drift also exposed a missing guardrail: important runtime-to-doc
  invariants were validated socially, not automatically.
- Security quality gates were materially improved only after adding a code-call
  vulnerability scan and upgrading the Go toolchain plus affected modules.
- Provider adapters are present at the interface level, but a production-grade
  provider rollout would still need a stronger payment-token strategy, such as
  durable token references or vault-backed indirection, before wiring external
  providers directly into async workers.
- Order status transitions were previously implicit across service, handlers,
  and store methods. That made invalid event ordering harder to detect and left
  the database without a lifecycle invariant.

## Changes applied in this review

- Added `internal/spec/consistency_test.go` to keep OpenAPI, operational docs,
  event topology, and CI quality gates aligned with shipped runtime behavior.
- Fixed the incident runbook so shipment-failure manual review now matches the
  real `manual_review` status.
- Kept the new spec-consistency test under repository validation so the guard
  cannot silently disappear.
- Preserved the previously added `govulncheck` gate and dependency/toolchain
  upgrades as part of the senior-quality baseline.
- Added a formal order state machine, applied it to in-memory and Postgres
  writes, and backed it with a Postgres status `CHECK` constraint.
- Added opaque payment/address provider references and wired worker adapters
  through the fake payment and shipment providers.
- Added saga/business metrics, concurrent inventory reservation coverage,
  benchmark budget validation, Compose saga smoke, SBOM generation, and Trivy
  supply-chain scans.

## Remaining recommendations

- Extend the spec-consistency tests to other high-risk invariants if more order
  states or worker branches are introduced.
- If the project evolves from fake adapters to real payment or shipment
  providers, move tokenization to a dedicated vault or PSP-owned token source
  while preserving the opaque-reference contract.
- Keep benchmark artifacts current when meaningful runtime behavior changes,
  especially if queue topology, readiness checks, or worker branching changes.
