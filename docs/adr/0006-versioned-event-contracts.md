# ADR 0006: Use Versioned Event Contracts for Saga Messages

## Status

Accepted.

## Context

FulfillHub coordinates order fulfillment through multiple workers and RabbitMQ topics. A producer change can break inventory, payment, shipment, notification, or compensation consumers even when the HTTP API remains stable.

## Decision

Document core saga events as versioned JSON schemas under `docs/events/`. Producers must treat schema changes as compatibility-sensitive, and consumers must persist inbox state before acknowledging broker messages.

## Consequences

- Saga evolution is reviewed as a contract change, not only an implementation change.
- New fields are optional until consumers adopt them.
- DLQ replay can rely on stable `message_id`, `correlation_id`, and `causation_id`.
- The contract uses `message_id` instead of a separate `event_id` so documentation
  stays aligned with the runtime `OutboxEvent` model and inbox idempotency key.
- A full schema registry is deferred until the broker topology and runtime deployment justify the operational overhead.
