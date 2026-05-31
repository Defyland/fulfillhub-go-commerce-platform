# Protobuf Versioning Policy

FulfillHub internal contracts live under `proto/` and use package names of the
form `fulfillhub.<domain>.v1`. The public REST contract remains
`openapi.yaml`; Protobuf is for internal service boundaries and typed provider
adapters when those boundaries become real runtime processes.

## Compatibility Rules

- Keep field numbers stable forever after a contract is merged.
- Never reuse a removed field number or name; add a `reserved` range or explicit
  reserved field declaration.
- Additive optional fields are compatible within the same package version.
- Renaming, removing, retyping, changing enum meaning, or changing request
  semantics requires a new package version such as `fulfillhub.orders.v2`.
- Enum zero values must be `*_UNSPECIFIED`.
- Request messages crossing tenant boundaries must carry `merchant_id` and
  `correlation_id` through a request context message.
- Mutating RPCs must carry an `idempotency_key` when retries can repeat side
  effects.
- Event-bearing RPCs must preserve saga `message_id`, `event_type`,
  `schema_version`, `correlation_id`, and `causation_id`.

## Current Contract Set

| Proto | Boundary | Runtime owner |
| --- | --- | --- |
| `proto/orders.proto` | Order creation, lookup, and cancellation | orders API/domain |
| `proto/inventory.proto` | Reservation and release operations | inventory worker/provider adapter |
| `proto/payments.proto` | Authorization, void, and lookup operations | payment worker/provider adapter |
| `proto/shipping.proto` | Shipment creation, lookup, and cancellation | shipment worker/provider adapter |
| `proto/saga.proto` | Saga advancement, compensation, state, and DLQ replay | fulfillment workers |

## Honest Gap

The repository commits `.proto` contracts and contract tests, but it does not
yet generate Go stubs or run a gRPC server. That is intentional for the current
local challenge scope: the executable product path is REST + outbox workers.
Generated stubs should be added when an internal process boundary is introduced
or when provider adapters need a typed RPC boundary.

Before generated code is introduced, the quality bar is:

- `.proto` files are versioned and reviewed as compatibility-sensitive changes.
- Contract tests verify package naming, `go_package`, services, RPCs,
  correlation fields, idempotency fields, and reserved ranges.
- Error mapping stays documented for both gRPC status codes and REST envelopes.
