# Ports and Adapters

FulfillHub is a modular monolith with hexagonal boundaries. It is not an MVC application with renamed folders: HTTP, CLI, workers, PostgreSQL, RabbitMQ,
Redis, provider clients, and test fakes sit at the edges. Application use cases
and domain invariants sit inside those edges.

## Adapter Map

| Boundary | Role | Examples |
| --- | --- | --- |
| Primary adapters | Accept commands from outside the application | `internal/api`, `cmd/fulfillhub-*`, RabbitMQ consumers |
| Use cases | Orchestrate application rules, idempotency, state changes, and event emission | `commerce.Service`, fulfillment handlers selected by `HandlerForQueue` |
| Domain model | Owns business state and invariants | `commerce.Order`, order statuses, state machine, validation errors |
| Ports | Interfaces defined where behavior is consumed | `commerce.Store`, `fulfillment.Projector`, provider ports |
| Secondary adapters | Implement ports against technical systems | `postgres.Store`, `commerce.MemoryStore`, RabbitMQ publisher/consumer, Redis limiter, provider adapters |

## Current Use Cases

- `CreateOrderContext`: validates merchant/idempotency command input, computes
  totals, creates an order aggregate, emits `order.created`, and writes state +
  outbox + audit through the `commerce.Store` port.
- `CancelOrderContext`: validates actor input, enforces state transition rules,
  emits `order.cancel_requested`, and writes audit through the same port.
- `HandlerForQueue`: chooses the fulfillment use case for each worker queue and
  invokes small ports for projection, payment, shipment, notification, and
  compensation behavior.

## Rules

- HTTP request DTOs stay in `internal/api`.
- Domain commands such as `commerce.CreateOrderCommand` do not carry JSON tags.
- Database rows and SQL details stay in `internal/postgres`.
- RabbitMQ delivery details stay in `internal/messaging`.
- Provider request/response details stay in `internal/providers` and
  `internal/fulfillment` adapters.
- Ports are small and declared by the package that consumes the dependency.
- Outbox/inbox events are the consistency boundary between saga steps.

## Why This Shape

The repo is intentionally a modular monolith because the domain still benefits
from single-repo transaction visibility and local challenge ergonomics. The
ports are already present so orders, inventory, payments, shipping, and saga
work can later split into services without rewriting the domain model or public
REST contract.
