# SLO and Alert Response Runbook

## Purpose

This runbook maps production alerts to concrete triage actions. FulfillHub has
an event-driven order saga, so the fastest safe response is to determine whether
the problem is request intake, outbox publication, broker consumption, provider
side effects, or data persistence.

## Service objectives

| Objective | Target | Primary signal |
| --- | --- | --- |
| API availability | 99.9 percent monthly | `up{job="fulfillhub-api"}` and `/readyz` |
| Saga progress | Oldest outbox event under 5 minutes | `fulfillhub_outbox_oldest_unpublished_age_seconds` |
| Queue health | No non-DLQ queue with backlog and zero consumers | RabbitMQ queue metrics |
| Failure budget | Failed orders below 5 percent of current order population | `fulfillhub_orders_total{status="failed"}` |
| Manual operations | Manual-review orders triaged within 15 minutes | `fulfillhub_orders_total{status="manual_review"}` |

## API Down

Alert: `FulfillHubAPIDown`

Actions:

1. Check whether the API deployment has available replicas.
2. Check recent rollout events and image digest.
3. Query pod logs for startup failures, missing secrets, or migration errors.
4. Check `/readyz` from inside the cluster if pods are running.
5. Roll back the API image if the previous digest was healthy and migrations are backward compatible.

## Runtime Metrics Unavailable

Alert: `FulfillHubRuntimeMetricsUnavailable`

Actions:

1. Check `/metrics` authorization and `METRICS_BEARER_TOKEN`.
2. Check PostgreSQL, RabbitMQ, and store readiness because the API exposes collector-up gauges.
3. Check whether the metrics failure is partial. A partial failure should not automatically roll back the API.
4. Fix credentials or dependency access before treating business metrics as healthy again.

## Outbox Stalled

Alert: `FulfillHubOutboxStalled`

Actions:

1. Confirm `fulfillhub-outbox-relay` pods are running and not crash looping.
2. Check RabbitMQ connectivity from relay logs.
3. Query oldest unpublished outbox events by `occurred_at`, `event_type`, and `correlation_id`.
4. Confirm RabbitMQ exchange topology exists before replaying anything manually.
5. Scale relay replicas only after confirming the broker is healthy.
6. If the release changed event serialization or routing, roll back the relay and workers together.

## DLQ Backlog

Alert: `FulfillHubDLQBacklog`

Actions:

1. Identify the DLQ with the largest message count.
2. Inspect one message body, message ID, routing key, retry attempt, and correlation ID.
3. Determine whether the error is data-specific, provider-specific, or code-specific.
4. Do not replay before the consumer path is verified healthy.
5. Use `fulfillhub-dlq-replay` with an explicit `OPS_ACTOR_ID` and bounded replay limit.
6. Record merchant impact and reconciliation requirements.

## Queue Without Consumers

Alert: `FulfillHubQueueWithoutConsumers`

Actions:

1. Identify the affected queue from the alert label.
2. Check the matching `fulfillhub-worker-*` deployment and rollout history.
3. Verify `WORKER_QUEUE` and `CONSUMER_NAME` match the queue topology.
4. Check RabbitMQ connection errors and credentials.
5. Restart or roll back only the affected worker branch when possible.

## Manual Review Backlog

Alert: `FulfillHubManualReviewBacklog`

Actions:

1. Query affected order IDs and correlation IDs.
2. Inspect compensation events and shipment/payment provider state.
3. Confirm whether payment authorization was voided and stock was released.
4. Route each order to support or operations with a clear customer-safe action.
5. Avoid automatic replay unless the provider side effect is known to be safe and idempotent.

## Order Failure Ratio High

Alert: `FulfillHubOrderFailureRatioHigh`

Actions:

1. Segment failed orders by merchant, SKU, provider, and failure event type.
2. Check whether failures correlate with a recent deploy, provider outage, or catalog stock issue.
3. If provider-specific, pause affected provider worker or route to manual review.
4. If stock-specific, verify inventory rows and recent reservation/compensation activity.
5. If release-specific, roll back the affected service and preserve failed order evidence for postmortem.
