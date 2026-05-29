# Event Catalog

## Exchange topology

| Exchange | Type | Purpose |
| --- | --- | --- |
| `fulfillhub.domain` | topic | Primary domain event fan-out |
| `fulfillhub.retry` | topic | Delayed retry routing for transient failures |
| `fulfillhub.dlx` | topic | Dead-letter capture for exhausted messages |

## Core routing keys

| Routing key | Producer | Primary consumers |
| --- | --- | --- |
| `order.created` | Orders | Inventory, analytics |
| `inventory.reserved` | Inventory | Payments, analytics |
| `inventory.rejected` | Inventory | Orders, notifications |
| `payment.authorized` | Payments | Shipments, analytics |
| `payment.failed` | Payments | Orders, inventory release |
| `shipment.created` | Shipments | Orders, notifications |
| `shipment.failed` | Shipments | Orders, payments |
| `order.completed` | Orders | Notifications, analytics |
| `order.cancelled` | Orders | Notifications, analytics |

## Queue design

| Queue | Bound routing keys | Retry queue | DLQ |
| --- | --- | --- | --- |
| `inventory.reserve` | `order.created` | `inventory.reserve.retry.5s` | `inventory.reserve.dlq` |
| `payments.authorize` | `inventory.reserved` | `payments.authorize.retry.15s` | `payments.authorize.dlq` |
| `shipments.create` | `payment.authorized` | `shipments.create.retry.30s` | `shipments.create.dlq` |
| `orders.finalize` | `shipment.created` | `orders.finalize.retry.15s` | `orders.finalize.dlq` |
| `orders.compensate` | `inventory.rejected`, `payment.failed`, `shipment.failed` | `orders.compensate.retry.15s` | `orders.compensate.dlq` |
| `notifications.email` | `order.completed`, `order.cancelled` | `notifications.email.retry.60s` | `notifications.email.dlq` |

## Delivery rules

- Every message must include `message_id`, `correlation_id`, `causation_id`, and `occurred_at`
- Consumers must write `inbox_messages` before acknowledging broker delivery
- Transient handler failures are acknowledged only after a copy is published to
  `fulfillhub.retry`, where queue TTL delays redelivery back to
  `fulfillhub.domain`
- Exhausted retries are nacked from the main queue and route to `fulfillhub.dlx`
- Replay from DLQ must be an explicit operator action recorded in `audit_logs`

## Implementation status

- The API writes outbox events for order creation and cancellation.
- The PostgreSQL store can load pending outbox events and mark them published.
- Outbox rows persist `causation_id`; API-originated root events use their own
  `message_id`, and worker-emitted saga events use the source message ID.
- `cmd/fulfillhub-outbox-relay` publishes pending events to RabbitMQ and injects
  `traceparent` plus `causation_id` into AMQP headers.
- `cmd/fulfillhub-dlq-replay` requires PostgreSQL audit logging and records
  `dlq.replay` details for successful or failed replay attempts.
- Inbox idempotency is implemented for memory tests and PostgreSQL-backed consumers.
- RabbitMQ consumers extract `traceparent`, create consume spans, record inbox
  entries before handlers run, backfill causal metadata from AMQP headers when
  needed, ack duplicates, publish bounded retries for handler failures, and
  nack exhausted failures to DLQs.
- RabbitMQ topology declaration creates each primary queue, retry queue, and
  dead-letter queue listed in the queue design table.
- `cmd/fulfillhub-worker` consumes inventory, payment, shipment, and order
  finalization queues for the current happy-path saga.
- Inventory, payment, and shipment workers persist their projections and write
  the next saga event through the transactional outbox instead of publishing
  directly to RabbitMQ.
- Inventory worker reservation failures write `inventory.rejected` to the
  transactional outbox with audit details before compensation consumes the
  failure event.
- Payment worker authorization failures write `payment.failed` to the
  transactional outbox with audit details before compensation consumes the
  failure event.
- Shipment worker provider failures write `shipment.failed` to the
  transactional outbox with audit details before compensation consumes the
  failure event.
- The order finalizer updates the order to `completed` and writes
  `order.completed` through the transactional outbox.
- The notification worker records durable email queueing projections for
  `order.completed` and `order.cancelled`.
- The compensation worker records durable failure projections for
  `inventory.rejected`, `payment.failed`, and `shipment.failed`.
- `TestRabbitPublisherIntegration` verifies live RabbitMQ publish and route delivery when `RABBITMQ_URL` is available.

## Example event payload

```json
{
  "message_id": "msg_01hzy81xqk1v9kyxrf0g7m6w1j",
  "correlation_id": "cor_01hzy72wf4ekcg7fbc7r8rtn2r",
  "causation_id": "msg_01hzy7ztck3kc67mw4jv0v4f8g",
  "event_type": "payment.authorized",
  "order_id": "ord_01hzy72wf4ekcg7fbc7r8rtn2r",
  "merchant_id": "mer_01hzy6v4egscg4r7kb3m7jq2dk",
  "occurred_at": "2026-05-28T20:15:12Z"
}
```
