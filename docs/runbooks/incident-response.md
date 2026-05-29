# Incident Response Runbook

## Purpose

This runbook defines the operational playbook for the current FulfillHub local runtime. It focuses on the failure modes most likely to happen across the API, PostgreSQL, RabbitMQ, Redis, relay, and worker processes.

## Global response checklist

1. Confirm whether `/healthz` and `/readyz` are green.
2. Capture current request error rate, queue depth, and DB connectivity.
3. Identify affected merchants, order IDs, and correlation IDs.
4. Freeze any manual replay or cancellation operations until the failure mode is understood.
5. Record mitigation actions in audit logs or incident notes.

## Scenario: RabbitMQ DLQ growth

Symptoms:

- `*.dlq` queue depth rising
- order saga completion rate dropping
- repeated consumer retry logs for the same message IDs

Actions:

1. Identify the consumer queue with the highest DLQ growth.
2. Inspect the most recent dead-lettered message and correlation ID.
3. Check the matching `*.retry.*` queue to confirm whether messages are still
   in delayed retry or have already exhausted retry attempts.
4. Confirm whether the error is data-related, dependency-related, or code-related.
5. If dependency-related, restore the dependency before replaying.
6. Replay only after the failing consumer path is verified healthy.

Replay command:

```sh
RABBITMQ_URL='amqp://guest:guest@localhost:5672/' \
DATABASE_URL='postgres://fulfillhub:postgres@localhost:5432/fulfillhub?sslmode=disable' \
DLQ_QUEUE='inventory.reserve.dlq' \
TARGET_ROUTING_KEY='order.created' \
OPS_ACTOR_ID='usr_ops_1' \
  go run ./cmd/fulfillhub-dlq-replay
```

The command refuses unaudited replay by requiring `DATABASE_URL` and
`OPS_ACTOR_ID`. It records `dlq.replay` in `audit_logs.details` with queue,
target routing key, replay limit, replayed count, status, and any replay error.

## Scenario: inventory oversell suspicion

Symptoms:

- negative available stock
- duplicate reservation entries for the same order and SKU
- merchant reports successful orders with unavailable inventory

Actions:

1. Query `inventory_items` and `stock_reservations` for the affected SKU and warehouse.
2. Verify whether optimistic version mismatches or lock timeouts were ignored.
3. Stop replay of inventory-related messages until the root cause is isolated.
4. Reconcile order state before reopening the queue.

## Scenario: readiness failing because PostgreSQL is unavailable

Symptoms:

- `/readyz` returns `503`
- order creation endpoints fail fast
- queue consumers stop progressing

Actions:

1. Confirm connectivity, credentials, and connection pool saturation.
2. Prevent manual retries that would create customer confusion.
3. Restore DB access first, then inspect outbox lag and consumer backlog.
4. Re-run only the minimal set of affected messages after health stabilizes.

## Scenario: inventory reservation failure after order acceptance

Symptoms:

- `inventory.rejected` events appear after `order.created`
- affected order items have reservation status `rejected`
- compensation worker records `compensation.inventory_rejected`

Actions:

1. Inspect the `inventory.rejected` audit details for the stock or reservation error.
2. Confirm whether the SKU is actually unavailable or the inventory dependency is degraded.
3. If dependency-related, wait for retry queues to drain before manual intervention.
4. If stock is unavailable, let compensation fail the order and notify the merchant.

## Scenario: payment authorization failure after inventory reservation

Symptoms:

- `payment.failed` events appear after `inventory.reserved`
- affected orders have payment status `failed`
- compensation worker records `compensation.payment_failed`
- reserved stock rows move to `released`

Actions:

1. Inspect the `payment.failed` audit details for the provider error.
2. Confirm whether the payment provider outage is transient or request-specific.
3. If transient, wait for retry queues to drain before manual intervention.
4. If request-specific, let compensation cancel the order and notify the merchant.

## Scenario: shipment provider failure after payment authorization

Symptoms:

- `shipment.failed` events appear after `payment.authorized`
- compensation worker records `compensation.shipment_failed`
- affected orders move to `cancellation_pending` for manual review
- authorized payment rows move to `voided`
- reserved stock rows move to `released`

Actions:

1. Inspect the `shipment.failed` audit details for the carrier/provider error.
2. Confirm whether the payment authorization can be voided automatically or
   needs manual provider action.
3. Stop replaying shipment messages until carrier health is verified.
4. Let compensation move the order to manual review before notifying support.

## Scenario: duplicate merchant order submission

Symptoms:

- same `external_order_id` submitted multiple times
- idempotency key mismatch for otherwise equivalent payloads

Actions:

1. Check whether the original order was accepted or failed validation.
2. Confirm unique constraint behavior on `(merchant_id, external_order_id)`.
3. If the first submission succeeded, return the existing order identity instead of creating another order.
4. Audit whether the client integration is rotating idempotency keys incorrectly.
