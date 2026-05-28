# Database Design

PostgreSQL is the planned source of truth for all consistency-sensitive flows.

## Primary tables

| Table | Purpose | Key constraints |
| --- | --- | --- |
| `merchants` | Tenant registry and API credential metadata | Unique merchant slug |
| `warehouses` | Physical inventory locations | Foreign key to `merchants.id` |
| `inventory_items` | Current sellable and reserved stock per SKU and warehouse | Unique `(warehouse_id, sku)` |
| `orders` | Order aggregate root and state machine | Unique `(merchant_id, external_order_id)` |
| `order_items` | Immutable line items snapshot | Foreign key to `orders.id` |
| `stock_reservations` | Reservation records used by inventory saga steps | Unique `(order_id, sku)` |
| `payment_authorizations` | Provider attempts and results | Unique successful authorization per `order_id` |
| `shipments` | Carrier handoff and tracking state | Unique `tracking_number` when present |
| `outbox_events` | Pending messages for broker publication | Indexed by `published_at` and `created_at` |
| `inbox_messages` | Per-consumer message deduplication | Unique `(consumer_name, message_id)` |
| `audit_logs` | Operator and automated action trail | Indexed by `merchant_id`, `order_id`, and `created_at` |

## Index strategy

- `orders(merchant_id, external_order_id)` unique for replay protection
- `orders(merchant_id, status, created_at desc)` for operations search
- `inventory_items(warehouse_id, sku)` unique lookup path
- `stock_reservations(order_id, status)` for reservation reconciliation
- `outbox_events(published_at, created_at)` for relay polling
- `inbox_messages(consumer_name, processed_at desc)` for replay diagnostics
- `shipments(order_id)` for order read models

## Transaction boundaries

### Order acceptance

One transaction must:

1. insert `orders`
2. insert `order_items`
3. insert initial `audit_logs` row
4. insert `outbox_events` row for `order.created`

### Inventory reservation

One transaction must:

1. lock `inventory_items` row with `SELECT ... FOR UPDATE`
2. decrement available quantity and increment reserved quantity
3. insert or update `stock_reservations`
4. insert `outbox_events` row for the reservation result
5. insert `inbox_messages` record for the consumed message

### Compensation

Release of inventory or voiding payment must follow the same pattern: mutate state, write audit evidence, write outbox event, and mark inbox processing in one transaction.

## Isolation assumptions

- Default isolation: `READ COMMITTED`
- Inventory mutation: explicit row locks
- Order and inventory records: optimistic `version` column for lost-update protection where appropriate

## Migration strategy

- Forward-only migrations in Phase 1
- Every migration must declare rollback notes even if rollback is manual
- Dangerous backfills must run in batches and be documented in runbooks

## Rollback strategy

- Schema rollback is acceptable only before production data dependencies exist
- Runtime rollback relies on application compatibility with the immediately previous schema
- Data corrections after partial saga failure use compensating events, not ad hoc SQL
