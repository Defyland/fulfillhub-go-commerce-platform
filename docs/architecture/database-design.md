# Database Design

PostgreSQL is the durable source of truth when `DATABASE_URL` is configured. The service still supports the in-memory store for fast local tests, but embedded migrations and a SQL-backed order store are implemented.

## Primary tables

| Table | Purpose | Key constraints |
| --- | --- | --- |
| `merchants` | Tenant registry and API credential metadata | Primary key `id` derived from API-key configuration |
| `warehouses` | Physical inventory locations | Foreign key to `merchants.id` |
| `inventory_items` | Current sellable and reserved stock per SKU and warehouse | Unique `(warehouse_id, sku)` |
| `orders` | Order aggregate root and state machine | FK to `merchants.id`, unique `(merchant_id, external_order_id)` |
| `order_items` | Immutable line items snapshot | Foreign key to `orders.id` |
| `stock_reservations` | Reservation records used by inventory saga steps | Unique `(order_id, sku)`, FK to `warehouses.id` |
| `payment_authorizations` | Provider attempts and results | Unique successful authorization per `order_id` |
| `shipments` | Carrier handoff and tracking state | Unique `tracking_number` when present |
| `notification_events` | Customer communication projection | Unique source message ID |
| `compensation_events` | Failure handling projection | Unique source message ID |
| `outbox_events` | Pending messages for broker publication | Persisted correlation and causation IDs |
| `inbox_messages` | Per-consumer message deduplication | Unique `(consumer_name, message_id)` |
| `audit_logs` | Operator and automated action trail with JSON details | Indexed by `merchant_id`, `order_id`, and `created_at` |

## Index strategy

- `orders(merchant_id, external_order_id)` unique for replay protection
- `orders(merchant_id, status, created_at desc)` for operations search
- `inventory_items(warehouse_id, sku)` unique lookup path
- `stock_reservations(order_id, status)` for reservation reconciliation
- `stock_reservations(warehouse_id, sku)` for inventory release lookup
- `outbox_events(published_at, occurred_at)` for relay polling
- `outbox_events(correlation_id, causation_id)` for saga trace reconstruction
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

### Order cancellation request

One transaction must:

1. update `orders.status` and optimistic `version`
2. insert `outbox_events` row for `order.cancel_requested`
3. insert `audit_logs` row for `order.cancel_requested`

### Order cancellation finalization

The current cancellation worker finalizes accepted cancellation requests in one
transaction:

1. update `orders.status` to `cancelled`
2. insert `outbox_events` row for `order.cancelled`
3. insert `audit_logs` row for `order.cancelled`

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

The current worker records the order-level reservation projection and mutates
catalog stock in one transaction:

1. lock an active warehouse `inventory_items` row with enough available stock
2. decrement `inventory_items.available_quantity` and increment `reserved_quantity`
3. insert or update `stock_reservations` with `warehouse_id`
4. mark `order_items.reservation_status` as `reserved`
5. insert `outbox_events` row for `inventory.reserved`
6. insert `audit_logs` row for `inventory.reserved`

### Inventory rejection

The current worker records reservation failure in one transaction. Missing or
insufficient catalog stock is treated as a business rejection, not as a broker
retry.

1. mark order item reservation status as `rejected`
2. insert `outbox_events` row for `inventory.rejected`
3. insert `audit_logs` row for `inventory.rejected` with the reservation error detail

### Payment authorization

The current worker records the provider authorization projection in one transaction:

1. insert or update `payment_authorizations`
2. update the order payment status and authorization ID
3. insert `outbox_events` row for `payment.authorized`
4. insert `audit_logs` row for `payment.authorized`

### Payment authorization failure

The current worker records provider authorization failure in one transaction:

1. update the order payment status to `failed`
2. insert `outbox_events` row for `payment.failed`
3. insert `audit_logs` row for `payment.failed` with the provider error detail

### Shipment creation

The current worker records the carrier handoff projection in one transaction:

1. insert or update `shipments`
2. touch the order version and update timestamp
3. insert `outbox_events` row for `shipment.created`
4. insert `audit_logs` row for `shipment.created`

### Shipment failure

The current worker records carrier booking failure in one transaction:

1. touch the order version and update timestamp
2. insert `outbox_events` row for `shipment.failed`
3. insert `audit_logs` row for `shipment.failed` with the provider error detail

### Notification queueing

The current worker records customer email queueing in one transaction:

1. insert or update `notification_events`
2. insert `audit_logs` row for `notification.email_queued`

### Compensation

The current worker records compensation projection state in one transaction:

1. update `orders.status` to the target compensation status
2. release reserved stock rows and restore `inventory_items` quantities for `payment.failed` and `shipment.failed`
3. void authorized payment rows for `shipment.failed`
4. insert or update `compensation_events`
5. insert `audit_logs` row for the compensation action

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
