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

  Merchant->>API: POST /api/v1/orders
  API->>DB: insert order + items + outbox event
  API-->>Merchant: 202 Accepted
  Relay->>DB: poll unpublished outbox rows
  Relay->>MQ: publish order.created
  MQ->>Inventory: consume order.created
  Inventory->>DB: reserve stock + outbox event
  Inventory->>MQ: publish inventory.reserved
  MQ->>Payment: consume inventory.reserved
  Payment->>DB: persist authorization + outbox event
  Payment->>MQ: publish payment.authorized
  MQ->>Shipment: consume payment.authorized
  Shipment->>DB: create shipment + outbox event
  Shipment->>MQ: publish shipment.created
  MQ->>API: project saga completion
```

Compensation rules:

- `inventory.rejected` ends the order as failed
- `payment.failed` releases stock and cancels the order
- `shipment.failed` triggers payment void and stock release when the shipment has not been handed off yet
