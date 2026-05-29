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

  Merchant->>API: POST /api/v1/orders
  API->>DB: insert order + items + outbox event
  API-->>Merchant: 202 Accepted
  Relay->>DB: poll unpublished outbox rows
  Relay->>MQ: publish order.created
  MQ->>Inventory: consume order.created
  Inventory->>DB: persist stock_reservations + outbox event
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
```

The current worker executable implements the happy path above with durable
inventory, payment, and shipment projections. Compensation and notification
projections are planned as the next worker slices.

Compensation rules:

- `inventory.rejected` ends the order as failed
- `payment.failed` releases stock and cancels the order
- `shipment.failed` triggers payment void and stock release when the shipment has not been handed off yet
