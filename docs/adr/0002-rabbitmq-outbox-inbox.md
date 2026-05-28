# ADR 0002: Use RabbitMQ With Transactional Outbox and Inbox

- Status: accepted
- Date: 2026-05-28

## Context

FulfillHub coordinates multiple side effects that cannot be completed inside one database transaction. The platform must tolerate transient broker failures, duplicate deliveries, and consumer retries without creating duplicate shipments or inconsistent inventory.

## Decision

FulfillHub will use RabbitMQ for asynchronous domain events, a transactional outbox for reliable publish after commit, and inbox tables for idempotent consumer processing.

## Consequences

- Positive: clear retry and dead-letter topology aligned to the commerce workflow
- Positive: domain changes and publish intent are committed atomically
- Positive: duplicate message delivery becomes manageable through inbox deduplication
- Negative: more tables and operational complexity than direct synchronous calls
- Negative: message schema evolution must be governed carefully to avoid brittle consumers
