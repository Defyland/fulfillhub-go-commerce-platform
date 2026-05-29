# ADR 0005: Order State Machine and Provider Boundaries

## Status

Accepted

## Context

FulfillHub uses an asynchronous order saga across inventory, payment, shipment,
finalization, cancellation, notification, and compensation workers. The runtime
already exposed intermediate order statuses, but transition rules were implicit
in handlers and projections. Provider abstractions also existed, but workers
could complete the happy path without exercising those provider boundaries.

For a commerce platform, status drift and provider-token handling are high-risk
areas. The system needs executable invariants at the domain and persistence
layers, plus CI evidence that the distributed saga can complete end to end.

## Decision

- Define order lifecycle transitions in `internal/commerce` as the canonical
  state machine.
- Validate all status writes in memory and Postgres stores before mutating an
  order.
- Add a Postgres `CHECK` constraint for accepted order statuses.
- Treat payment and address inputs as opaque references for internal provider
  adapters; do not expose raw credential references through JSON responses.
- Wire fake payment and shipment providers through worker adapters so local
  Compose exercises the same boundary shape as a real provider integration.
- Add CI guardrails for Compose saga smoke, k6 budget validation, SBOM
  generation, and container/repository vulnerability scanning.

## Consequences

- Invalid event ordering now fails fast instead of silently corrupting lifecycle
  state.
- Database constraints provide defense in depth if future code paths bypass
  domain validation.
- Provider integrations become replaceable behind explicit adapter contracts.
- Existing tests that constructed impossible states must advance fixtures through
  valid lifecycle transitions.
- CI takes longer, but it now proves more than unit correctness: it validates
  saga completion, performance budget adherence, and supply-chain posture.
