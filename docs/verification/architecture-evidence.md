# Architecture evidence

This file records concrete evidence that FulfillHub is implemented as a modular
monolith with Hexagonal/Clean boundaries, not as MVC folders renamed after the
fact. It is intentionally referenced by `internal/spec/architecture_boundaries_test.go`
so documentation and code drift are caught by tests.

## Evidence matrix

| Quality claim | Evidence |
| --- | --- |
| HTTP handlers are thin primary adapters | `internal/api/server.go` authenticates, rate-limits, decodes HTTP JSON into adapter-local request structs, maps to a command, calls `commerce.Service`, and maps errors to the HTTP envelope. It does not decide order totals, saga transitions, inventory outcomes, payment outcomes, or shipment outcomes. |
| HTTP DTO separation | `createOrderRequest` and nested request DTOs live in `internal/api/server.go`. They map through `createOrderRequest.toCommand()` into `commerce.CreateOrderCommand`. The command and input types in `internal/commerce/model.go` carry no JSON tags. |
| Domain does not depend on infra | `internal/spec/architecture_boundaries_test.go` walks `internal/commerce` and fails on imports/fragments for `internal/api`, `internal/postgres`, `internal/messaging`, `internal/providers`, `database/sql`, `net/http`, pgx, RabbitMQ, or Redis. |
| Use-case orchestration is explicit | `commerce.Service` owns order creation, validation, totals, idempotency handoff, event creation, audit creation, and cancellation orchestration. `fulfillment.HandlerForQueue` owns queue-specific saga use cases for inventory, payment, shipping, notification, compensation, order finalization, and cancellation. |
| Ports are declared where consumed | `commerce.Store` is declared in `internal/commerce/store.go` because the order use case consumes it. Fulfillment ports such as `Projector`, `InventoryReserver`, `PaymentAuthorizer`, and `ShipmentCreator` are declared in `internal/fulfillment/handlers.go` because worker use cases consume them. |
| Secondary adapters implement ports | `internal/postgres.Store` implements `commerce.Store` and fulfillment projection methods with SQL transactions. `commerce.MemoryStore` is a deterministic fake/local store for tests and explicit demo mode. Provider adapters live under `internal/providers`; RabbitMQ adapters live under `internal/messaging`; Redis rate limiting lives under `internal/ratelimit`. |
| Domain invariants are tested without a database | `internal/commerce/state_machine_test.go` and `internal/commerce/service_test.go` cover status transitions, duplicate SKU validation, totals, duplicate order protection, idempotency behavior, and outbox creation against fakes/in-memory state. |
| Use cases are tested with fake adapters | `internal/fulfillment/handlers_test.go` injects fake reserver, authorizer, shipment creator, and projector behavior through small ports. `internal/commerce/service_test.go` exercises the order use case through `MemoryStore`. |
| Adapter integration is tested where applicable | `internal/postgres/*_test.go` covers migrations, store behavior, pooling, inventory reservation concurrency, outbox/inbox, and readiness-related persistence behavior. `internal/messaging/*_test.go` covers publisher, relay, topology, consumer, retry, DLQ, and integration hooks. `internal/ratelimit/*_test.go` covers Redis-backed behavior. |
| Versioned event contracts are executable | `docs/events/*.v1.json`, `docs/events/README.md`, and `internal/spec/event_contracts_test.go` validate runtime saga events against versioned v1 contracts. |
| Internal gRPC contracts are versioned but not replacing REST | `proto/orders.proto`, `proto/inventory.proto`, `proto/payments.proto`, `proto/shipping.proto`, and `proto/saga.proto` define internal process-boundary contracts. `docs/contracts/rest-vs-grpc.md` keeps REST/OpenAPI as the public external contract. |

## Known gaps

These are conscious local-scope gaps, not hidden production claims:

| Gap | Reason |
| --- | --- |
| Generated protobuf stubs and live gRPC servers are not wired yet | The current repo is still a modular monolith. `.proto` files define the future internal contracts and are contract-tested for repository quality, but generating and serving stubs would add runtime surface before there is a real process split. |
| Domain read/projection structs still include JSON tags | Request ingress has been cleaned so HTTP DTOs do not enter the domain. Some output/event structs remain tagged because they are stable serialization shapes used across API responses, event envelopes, tests, and docs. A later hard split can add presenter DTOs if the API surface grows enough to justify the duplication. |
| External infra is represented locally, not as managed cloud resources | The challenge scope does not include provisioning real managed PostgreSQL, RabbitMQ, Redis, Prometheus, Grafana, or Kubernetes clusters. The repo includes Compose, Kubernetes blueprints, readiness checks, metrics, traces, benchmarks, and runbooks so the design is production-like without external accounts. |
