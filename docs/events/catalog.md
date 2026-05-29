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
| `orders.compensate` | `inventory.rejected`, `payment.failed`, `shipment.failed` | `orders.compensate.retry.15s` | `orders.compensate.dlq` |
| `notifications.email` | `order.completed`, `order.cancelled` | `notifications.email.retry.60s` | `notifications.email.dlq` |

## Delivery rules

- Every message must include `message_id`, `correlation_id`, `causation_id`, and `occurred_at`
- Consumers must write `inbox_messages` before acknowledging broker delivery
- Transient failures move messages to retry queues with exponential backoff semantics
- Exhausted retries route to `fulfillhub.dlx`
- Replay from DLQ must be an explicit operator action recorded in `audit_logs`

## Implementation status

- The API writes outbox events for order creation and cancellation.
- The PostgreSQL store can load pending outbox events and mark them published.
- `cmd/fulfillhub-outbox-relay` publishes pending events to RabbitMQ.
- Inbox idempotency is implemented for memory tests and PostgreSQL-backed consumers.
- `TestRabbitPublisherIntegration` verifies live RabbitMQ publish and route delivery when `RABBITMQ_URL` is available.

## Example event payload

```json
{
  "message_id": "msg_01hzy81xqk1v9kyxrf0g7m6w1j",
  "correlation_id": "cor_01hzy72wf4ekcg7fbc7r8rtn2r",
  "causation_id": "msg_01hzy7ztck3kc67mw4jv0v4f8g",
  "event_type": "payment.authorized",
  "occurred_at": "2026-05-28T20:15:12Z",
  "data": {
    "order_id": "ord_01hzy72wf4ekcg7fbc7r8rtn2r",
    "merchant_id": "mer_01hzy6v4egscg4r7kb3m7jq2dk",
    "authorization_id": "pay_01hzy7aqwbrk4k6q31z9r1rj6z",
    "provider": "stripe"
  }
}
```
