# FulfillHub Event Contracts

FulfillHub domain events coordinate the order saga across inventory, payment,
shipment, order finalization, notifications, and compensation workers. RabbitMQ
delivery is intentionally treated as at least once. Correctness therefore comes
from durable outbox publication, inbox idempotency, state-machine validation,
and explicit compatibility rules.

This document is the contract policy for saga events. The routing catalog lives
in [catalog.md](./catalog.md), and the security analysis lives in
[threat-model.md](./threat-model.md).

## Envelope v1

Every event body must use this versioned envelope. The current runtime persists
the core fields in `outbox_events`; event-specific payload fields are the
logical contract for producers and consumers as the saga evolves.

| Field | Required | Description |
| --- | --- | --- |
| `message_id` | yes | Globally unique event identifier. This is the inbox idempotency key. |
| `event_type` | yes | Routing-compatible event name, for example `order.created`. |
| `schema_version` | yes | Integer contract version. The initial version is `1`. |
| `occurred_at` | yes | UTC timestamp created by the producer when the event is recorded. |
| `producer` | yes | Producing module or process, for example `orders-api` or `payment-worker`. |
| `merchant_id` | yes | Tenant boundary. Consumers must never infer tenant from request context. |
| `order_id` | yes | Aggregate identifier and primary ordering key. |
| `correlation_id` | yes | Root request/workflow identifier used for tracing and incident reconstruction. |
| `causation_id` | yes | Message that caused this event. Root events use their own `message_id`. |
| `payload` | yes | Event-specific data defined by the versioned schema. |

AMQP headers may carry transport metadata such as `traceparent`,
`fulfillhub_retry_attempt`, and broker dead-letter headers. Those headers are
not a substitute for envelope fields; consumers must be able to reconstruct the
saga from the persisted event body and audit logs.

## Compatibility Policy

Producer rules:

- Producers may add optional fields to a v1 payload only when existing consumers
  can ignore unknown fields.
- Producers must not remove required fields, change field meaning, change field
  type, or reuse an event name for a different business fact.
- Producers must create a new schema version for breaking changes.
- Producers must emit events only from the transactional outbox, never by
  publishing directly from business handlers.
- Producers must not include raw payment tokens, credentials, secrets, or full
  customer PII in event payloads.

Consumer rules:

- Consumers must bind to explicit `event_type` values and validate or at least
  reject unsupported `schema_version` values.
- Consumers must tolerate additive optional fields.
- Consumers must persist an inbox record keyed by consumer and `message_id`
  before acknowledging broker delivery.
- Consumers must treat duplicate delivery as success after inbox detection.
- Consumers must verify `merchant_id`, `order_id`, `correlation_id`, and
  `causation_id` are present before side effects run.

Versioning rules:

- Non-breaking additive changes remain in the same major schema version.
- Breaking changes create a new schema file named `<event_type>.v<N>.json`.
- Producers and consumers must support an overlap window during migrations.
- DLQ replay must use the original schema version unless an explicit migration
  replay plan is approved and audited.

## Outbox and Inbox Contract

Outbox:

- The producer writes domain state and the outbox event in the same PostgreSQL
  transaction.
- The relay publishes unpublished outbox rows to `fulfillhub.domain` using
  `event_type` as the routing key.
- Publication failure leaves the event unpublished so the relay can retry.
- The relay must preserve `message_id`, `correlation_id`, `causation_id`,
  `merchant_id`, `order_id`, and `occurred_at`.

Inbox:

- Each consumer records `(consumer_name, message_id)` before side effects.
- A duplicate inbox record means the message was already processed or accepted
  for processing and should be acknowledged without repeating side effects.
- Inbox state is part of the consistency model, not an optimization.

## Retry and DLQ Contract

- Transient failures are republished to the configured retry queue with bounded
  attempts.
- Exhausted messages route to the configured DLQ for the consumer queue.
- DLQ messages must preserve the original `message_id`, `correlation_id`,
  `causation_id`, `merchant_id`, and `order_id`.
- DLQ replay is an operator action and must be recorded in `audit_logs`.
- Replay must happen only after the consumer has been fixed or the data issue
  has been remediated.
- Replayed messages must remain idempotent; replay must not mint a new
  `message_id` unless the business operation is intentionally reissued.

## Ordering and Partitioning

FulfillHub does not rely on global broker ordering. Ordering is logical and
aggregate-scoped:

- The ordering key is `merchant_id + ":" + order_id`.
- Events for different orders may be processed in parallel.
- Events for the same order must form a valid causation chain.
- Consumers must reject or defer events that violate the order state machine.
- `causation_id` links each worker-produced event to the source event that
  triggered it.
- `merchant_id` is part of the ordering key so cross-tenant collisions cannot
  accidentally share an ordering or idempotency scope.

The broker topology can still redeliver messages out of order after retries,
consumer restarts, or DLQ replay. The domain state machine and database
constraints are the final guardrails.

## Versioned Schemas

| Event | Producer | Primary consumer | Schema |
| --- | --- | --- | --- |
| `order.created` | Orders API | Inventory reservation | [order.created.v1.json](./order.created.v1.json) |
| `inventory.reserved` | Inventory worker | Payment authorization | [inventory.reserved.v1.json](./inventory.reserved.v1.json) |
| `payment.authorized` | Payment worker | Shipment creation | [payment.authorized.v1.json](./payment.authorized.v1.json) |
| `shipment.created` | Shipment worker | Order finalizer | [shipment.created.v1.json](./shipment.created.v1.json) |
| `order.completed` | Order finalizer | Notification/analytics | [order.completed.v1.json](./order.completed.v1.json) |

## Example Envelope

```json
{
  "message_id": "msg_01hzy81xqk1v9kyxrf0g7m6w1j",
  "event_type": "payment.authorized",
  "schema_version": 1,
  "occurred_at": "2026-05-28T20:15:12Z",
  "producer": "payment-worker",
  "merchant_id": "mer_01hzy6v4egscg4r7kb3m7jq2dk",
  "order_id": "ord_01hzy72wf4ekcg7fbc7r8rtn2r",
  "correlation_id": "cor_01hzy72wf4ekcg7fbc7r8rtn2r",
  "causation_id": "msg_01hzy7ztck3kc67mw4jv0v4f8g",
  "payload": {
    "payment": {
      "provider": "fake-payment",
      "authorization_id": "pay_01hzy8",
      "amount": 20100,
      "currency": "USD"
    },
    "order_status": "payment_authorized"
  }
}
```
