# Module Boundaries

FulfillHub models business capabilities as modules inside one Go process. The
modules communicate through commands, ports, and events rather than direct
database ownership leaks.

## Orders

- Owns order creation, cancellation request, order states, totals, validation,
  idempotency semantics, audit creation, and order event emission.
- Public REST starts in `internal/api`, maps HTTP DTOs to
  `commerce.CreateOrderCommand`, then calls the orders use case.
- Persistence is accessed through `commerce.Store`.

## Inventory

- Owns reservation and release semantics for order items.
- Runtime behavior is currently implemented as fulfillment use cases plus
  `Projector` persistence operations.
- Catalog and stock reservation writes live in the Postgres adapter.
- Event contracts: `inventory.reserved`, `inventory.rejected`.

## Payments

- Owns authorization, failure projection, and void compensation semantics.
- Provider integration is behind `PaymentAuthorizer`; fake and provider-backed
  implementations can be swapped without changing saga orchestration.
- Event contracts: `payment.authorized`, `payment.failed`.

## Shipping

- Owns shipment creation, failed shipment projection, and manual-review handoff
  when cancellation races with shipment creation.
- Provider integration is behind `ShipmentCreator`.
- Event contracts: `shipment.created`, `shipment.failed`.

## Saga

- Owns worker queue selection, state progression, compensation, retry/DLQ
  behavior, and inbox idempotency.
- The outbox/inbox boundary is the consistency mechanism. Saga use cases do not
  publish directly from domain handlers.
- Event contracts are versioned under `docs/events` and runtime-validated in
  `internal/spec/event_contracts_test.go`.

## Boundary Rule

Domain packages may define what they need from the outside world, but adapters
must implement those needs. Domain packages must not import API, Postgres,
RabbitMQ, Redis, provider SDKs, or generated transport code.
