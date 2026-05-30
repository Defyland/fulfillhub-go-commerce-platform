# Saga Event Threat Model

## Scope

This threat model covers FulfillHub saga events published through the
transactional outbox, RabbitMQ, retry queues, DLQs, and inbox-backed consumers.
It focuses on duplicate delivery, payment side effects, stock correctness,
tenant isolation, ordering, and replay.

## Assets

| Asset | Risk | Primary controls |
| --- | --- | --- |
| Order lifecycle | Invalid state transition or premature completion | State machine, `causation_id`, database status constraints |
| Inventory stock | Oversell or double release | PostgreSQL row locks, reservation records, compensation audit |
| Payment authorization | Duplicate authorization or missed void | Opaque credential references, provider idempotency, compensation |
| Tenant data | Cross-merchant event handling | Required `merchant_id`, scoped DB queries, tenant-aware idempotency |
| Outbox/inbox records | Lost or duplicated side effects | Transactional outbox, inbox uniqueness, retry/DLQ routing |
| Audit trail | Untraceable replay or manual mutation | `correlation_id`, `causation_id`, replay audit logs |

## Trust Boundaries

- API requests are synchronous and authenticated at the HTTP boundary.
- Outbox rows are trusted only when written in the same transaction as domain
  state.
- RabbitMQ delivery is not trusted for uniqueness or ordering.
- Retry and DLQ messages are trusted only as preserved copies of original
  domain events.
- Operator replay is a privileged action and must be auditable.
- Payment and shipment providers are external trust boundaries; webhook input
  must be signed and replay-protected before it can influence saga state.

## Threats and Controls

### Duplicate Delivery

Threat:

- RabbitMQ redelivers a message after consumer restart, acknowledgement loss, or
  retry routing.
- A DLQ replay reintroduces a message that was already processed.

Controls:

- `message_id` is required in every envelope.
- Consumers record `(consumer_name, message_id)` in the inbox before
  acknowledging broker delivery.
- Duplicate inbox records must be treated as already accepted and acknowledged
  without repeating side effects.
- Provider calls should use deterministic idempotency references derived from
  `message_id` or `order_id` when real providers are connected.

### Payment Side Effects

Threat:

- A duplicate or replayed `inventory.reserved` event causes multiple payment
  authorizations.
- A failed shipment leaves an authorized payment unvoided.
- Event payload accidentally exposes payment tokens.

Controls:

- Payment workers consume only `inventory.reserved` v1 and must preserve
  `causation_id`.
- Payment credentials are opaque references, never raw card or PSP tokens.
- Payment authorization IDs are persisted as projections for reconciliation.
- `shipment.failed` compensation requests payment void and stock release.
- Payment payloads may include provider and authorization ID but must not
  include raw credentials.

### Inventory Correctness

Threat:

- Concurrent `order.created` messages reserve more stock than available.
- Duplicate compensation releases stock more than once.
- Cross-warehouse reservations lose provenance.

Controls:

- Inventory reservation uses PostgreSQL row locks and available quantity checks.
- `stock_reservations` records order, SKU, quantity, status, and warehouse
  provenance.
- Compensation must be based on reservation records, not event payload quantity
  alone.
- Inventory events identify the reservation fact but consumers should reconcile
  against durable stock projections before side effects.

### Tenant Isolation

Threat:

- A consumer applies an event to an order belonging to another merchant.
- A replayed message with a mismatched `merchant_id` crosses tenant boundaries.
- Shared `order_id` values across merchants collide in idempotency or ordering.

Controls:

- `merchant_id` is required in every event envelope.
- The logical ordering key is `merchant_id + ":" + order_id`.
- Consumers must use both `merchant_id` and `order_id` when loading or mutating
  tenant-scoped state.
- Database constraints and indexes must preserve merchant ownership.
- DLQ replay must not allow operators to edit tenant identifiers in-place.

### Replay Abuse

Threat:

- An operator replays DLQ messages before the underlying bug is fixed.
- A malicious or mistaken replay mints new message IDs and repeats side effects.
- Replay loses correlation and causation, making incidents impossible to trace.

Controls:

- DLQ replay requires explicit operator context and audit logging.
- Replay must preserve `message_id`, `correlation_id`, `causation_id`,
  `merchant_id`, and `order_id`.
- Replay must be bounded by queue, routing key, and replay limit.
- Replayed messages remain subject to inbox idempotency and state-machine
  validation.
- Manual replay should be paused for provider side effects until provider state
  is reconciled.

### Ordering Violations

Threat:

- `payment.authorized` is processed before `inventory.reserved`.
- `shipment.created` arrives after cancellation has moved the order to manual
  review.
- Retry queues reorder messages for the same order.

Controls:

- Consumers validate `event_type` and `schema_version`.
- The order state machine rejects invalid status transitions.
- `causation_id` must point to the source event in the saga chain.
- Database status constraints protect projections from impossible lifecycle
  states.
- Out-of-order events should fail safely into retry or DLQ instead of forcing
  domain state.

## Security Requirements

- All core saga events must have a versioned schema.
- All producers must publish through the transactional outbox.
- All consumers must be idempotent before side effects.
- No event may contain raw payment credentials, API keys, JWTs, or provider
  secrets.
- Every replay must be auditable and bounded.
- Every event must include tenant, aggregate, correlation, and causation
  metadata.
