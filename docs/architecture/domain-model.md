# Domain Model

## Core aggregates

| Entity | Role | Key invariants |
| --- | --- | --- |
| `Merchant` | Tenant boundary and API credential owner | Cannot see or mutate another merchant's data |
| `Warehouse` | Inventory location used for reservation and shipment origin | Must belong to a merchant or approved network |
| `InventoryItem` | Current sellable stock for a SKU at a warehouse | `available_quantity + reserved_quantity` must stay consistent |
| `Order` | Root aggregate that owns the orchestration state machine | External order ID must be unique per merchant |
| `OrderItem` | Snapshot of requested SKU, quantity, and price | Immutable after order acceptance |
| `StockReservation` | Holds inventory during payment and shipment coordination | Reservation state changes must be idempotent and tied to a warehouse |
| `PaymentAuthorization` | Payment provider attempt and outcome | Only one active successful authorization per order |
| `Shipment` | Carrier booking and timeline | Cannot exist before payment authorization in the happy path |
| `OutboxEvent` | Durable event waiting for broker publication | Must be committed in the same transaction as domain state |
| `InboxMessage` | Consumer deduplication record | Message ID plus consumer name must be unique |
| `AuditLog` | Operator or automated decision record | Must capture actor identity and correlation metadata |

## Order state machine

```text
pending_fulfillment
  -> inventory_reserved
  -> payment_authorized
  -> shipment_created
  -> completed

Compensation paths:
pending_fulfillment -> failed
inventory_reserved -> cancelled
payment_authorized -> cancellation_pending -> cancelled
shipment_created -> cancellation_pending -> manual_review
```

## Bounded responsibilities

- Orders own customer-visible status and the orchestration timeline.
- Inventory owns stock truth and reservation semantics. PostgreSQL reservations lock and mutate `inventory_items` in the same transaction that records the reservation event.
- Payments own provider correlation and authorization status.
- Shipments own carrier-facing identifiers and delivery timeline.
- Notifications never own source-of-truth order state; they consume events only.

## Cross-cutting rules

- All user-visible records are scoped by `merchant_id`.
- Idempotency keys are required for order creation.
- Monetary values use minor currency units.
- Manual operator actions must be audit logged.
