# Database Design

PostgreSQL is the durable source of truth when `DATABASE_URL` is configured. The service still supports the in-memory store for fast local tests, but embedded migrations and a SQL-backed order store are implemented.

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
| `notification_events` | Customer communication projection | Unique source message ID |
| `compensation_events` | Failure handling projection | Unique source message ID |
| `outbox_events` | Pending messages for broker publication | Indexed by `published_at` and `created_at` |
| `inbox_messages` | Per-consumer message deduplication | Unique `(consumer_name, message_id)` |
| `audit_logs` | Operator and automated action trail with JSON details | Indexed by `merchant_id`, `order_id`, and `created_at` |

## Index strategy

- `orders(merchant_id, external_order_id)` unique for replay protection
- `orders(merchant_id, status, created_at desc)` for operations search
- `inventory_items(warehouse_id, sku)` unique lookup path
- `stock_reservations(order_id, status)` for reservation reconciliation
- `outbox_events(published_at, created_at)` for relay polling
- `inbox_messages(consumer_name, processed_at desc)` for replay diagnostics
- `shipments(order_id)` for order read models
- `notification_events(order_id, created_at desc)` for customer timeline diagnostics
- `compensation_events(order_id, created_at desc)` for failure timeline diagnostics

## Transaction boundaries

### Order acceptance

One transaction must:

1. insert `orders`
2. insert `order_items`
3. insert `outbox_events` row for `order.created`
4. insert initial `audit_logs` row for `order.create`

### Order cancellation

One transaction must:

1. update `orders.status` and optimistic `version`
2. insert `outbox_events` row for `order.cancel_requested`
3. insert `audit_logs` row for `order.cancel_requested`

### DLQ replay

One transaction must:

1. insert `audit_logs` row for `dlq.replay`
2. include queue, target routing key, replay limit, replayed count, status, and error details

### Order completion

The current worker happy path finalizes orders in one transaction:

1. update `orders.status` to `completed`
2. insert `outbox_events` row for `order.completed`
3. insert `audit_logs` row for `order.completed`

### Inventory reservation

The current worker records the order-level reservation projection in one transaction:

1. insert or update `stock_reservations`
2. mark `order_items.reservation_status` as `reserved`
3. insert `outbox_events` row for `inventory.reserved`
4. insert `audit_logs` row for `inventory.reserved`

### Payment authorization

The current worker records the provider authorization projection in one transaction:

1. insert or update `payment_authorizations`
2. update the order payment status and authorization ID
3. insert `outbox_events` row for `payment.authorized`
4. insert `audit_logs` row for `payment.authorized`

### Shipment creation

The current worker records the carrier handoff projection in one transaction:

1. insert or update `shipments`
2. touch the order version and update timestamp
3. insert `outbox_events` row for `shipment.created`
4. insert `audit_logs` row for `shipment.created`

### Notification queueing

The current worker records customer email queueing in one transaction:

1. insert or update `notification_events`
2. insert `audit_logs` row for `notification.email_queued`

### Compensation

The current worker records compensation projection state in one transaction:

1. update `orders.status` to the target compensation status
2. insert or update `compensation_events`
3. insert `audit_logs` row for the compensation action

## Isolation assumptions

- Default isolation: `READ COMMITTED`
- Inventory mutation: explicit row locks
- Order and inventory records: optimistic `version` column for lost-update protection where appropriate

## Migration strategy

- Embedded forward-only migrations under `internal/postgres/migrations/`
- Every migration must declare rollback notes even if rollback is manual
- Dangerous backfills must run in batches and be documented in runbooks

## Rollback strategy

- Schema rollback is acceptable only before production data dependencies exist
- Runtime rollback relies on application compatibility with the immediately previous schema
- Data corrections after partial saga failure use compensating events, not ad hoc SQL
