# Order Saga Sequence

```mermaid
sequenceDiagram
  participant Merchant
  participant API as FulfillHub API
  participant DB as PostgreSQL
  participant Relay as Outbox Relay
  participant MQ as RabbitMQ
  participant Inventory
  participant Payment
  participant Shipment
  participant Orders as Orders Finalizer
  participant Cancel as Orders Canceller
  participant Notify as Notification Worker

  Merchant->>API: POST /api/v1/orders
  API->>DB: insert order + items + outbox event
  API-->>Merchant: 202 Accepted
  Relay->>DB: poll unpublished outbox rows
  Relay->>MQ: publish order.created
  MQ->>Inventory: consume order.created
  Inventory->>DB: lock inventory_items, persist stock_reservations + outbox event
  Relay->>MQ: publish inventory.reserved
  MQ->>Payment: consume inventory.reserved
  Payment->>DB: persist payment_authorizations + outbox event
  Relay->>MQ: publish payment.authorized
  MQ->>Shipment: consume payment.authorized
  Shipment->>DB: persist shipments + outbox event
  Relay->>MQ: publish shipment.created
  MQ->>Orders: consume shipment.created
  Orders->>DB: update order completed + outbox event
  Relay->>MQ: publish order.completed
  MQ->>Notify: consume order.completed
  Notify->>DB: persist notification_events + audit log

  Merchant->>API: POST /api/v1/orders/{id}/cancel
  API->>DB: update cancellation_pending + outbox event
  Relay->>MQ: publish order.cancel_requested
  MQ->>Cancel: consume order.cancel_requested
  alt shipment already created
    Cancel->>DB: update manual_review + outbox event
    Relay->>MQ: publish order.manual_review_required
    MQ->>Notify: consume order.manual_review_required
  else pre-shipment cancellation
    Cancel->>DB: update order cancelled + outbox event
    Relay->>MQ: publish order.cancelled
    MQ->>Notify: consume order.cancelled
  end
  Notify->>DB: persist notification_events + audit log
```

The current worker executable implements the happy path above with durable
inventory, payment, shipment, cancellation, notification, and compensation
projections.

Compensation rules:

- `inventory.rejected` ends the order as failed
- `payment.failed` releases stock and cancels the order
- `shipment.failed` triggers payment void and stock release when the shipment has not been handed off yet
