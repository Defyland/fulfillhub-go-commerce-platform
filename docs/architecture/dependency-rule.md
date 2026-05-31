# Dependency Rule

FulfillHub dependencies point inward.

```text
cmd/* -> primary adapters -> use cases/domain -> ports
                        secondary adapters -> ports
```

Concrete infrastructure depends on domain contracts. Domain code does not depend
on concrete infrastructure.

## Allowed Direction

- `cmd/*` may import concrete adapters and use cases because commands are
  composition roots.
- `internal/api` may import `internal/commerce` to map HTTP requests to use-case
  commands and map domain errors to HTTP envelopes.
- `internal/fulfillment` may import `internal/commerce` and
  `internal/messaging` contracts to advance saga work.
- `internal/postgres` may import `internal/commerce` because it implements the
  store/projector ports.
- `internal/messaging` may import `internal/commerce` because event envelopes
  are domain contracts.

## Forbidden Direction

- `internal/commerce` must not import `internal/api`, `internal/postgres`,
  `internal/messaging`, `internal/providers`, `database/sql`, `net/http`,
  RabbitMQ, Redis, or pgx packages.
- HTTP DTO request structs must not be used as domain commands.
- SQL rows and database migration details must not appear in domain types.
- Broker delivery objects must not appear in order or payment domain types.

## Enforcement

`internal/spec/architecture_boundaries_test.go` checks the most important
rules:

- commerce stays free of adapter imports
- HTTP command DTOs remain in `internal/api`
- `commerce.CreateOrderCommand` has no JSON tags
- fulfillment ports stay defined where they are consumed

This is deliberately executable architecture documentation: if the boundary
drifts, CI fails.
