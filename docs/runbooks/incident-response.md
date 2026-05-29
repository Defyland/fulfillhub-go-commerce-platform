# Incident Response Runbook

## Purpose

This runbook defines the first operational playbook for FulfillHub. It focuses on the failure modes most likely to happen once Phase 1 is live.

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
3. Confirm whether the error is data-related, dependency-related, or code-related.
4. If dependency-related, restore the dependency before replaying.
5. Replay only after the failing consumer path is verified healthy.

Replay command:

```sh
RABBITMQ_URL='amqp://guest:guest@localhost:5672/' \
DLQ_QUEUE='inventory.reserve.dlq' \
TARGET_ROUTING_KEY='order.created' \
  go run ./cmd/fulfillhub-dlq-replay
```

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

## Scenario: duplicate merchant order submission

Symptoms:

- same `external_order_id` submitted multiple times
- idempotency key mismatch for otherwise equivalent payloads

Actions:

1. Check whether the original order was accepted or failed validation.
2. Confirm unique constraint behavior on `(merchant_id, external_order_id)`.
3. If the first submission succeeded, return the existing order identity instead of creating another order.
4. Audit whether the client integration is rotating idempotency keys incorrectly.
