# REST vs gRPC Contract Boundary

FulfillHub exposes REST/OpenAPI externally and uses Protobuf/gRPC contracts for
internal boundaries when a boundary has a real operational reason.

## Public REST

REST remains the merchant and operations-facing contract because it is simple to
exercise in a backend challenge, easy to inspect with `curl`, and already
covered by OpenAPI, request tests, auth tests, strict JSON decoding, and
structured error envelopes.

Public REST owns:

- merchant order creation
- merchant and operations order lookup
- merchant and operations cancellation requests
- shipment lookup
- health, readiness, and metrics endpoints

## Internal gRPC

gRPC is appropriate for internal APIs that need typed contracts, generated
clients, deadlines, status codes, and service-to-service evolution rules.

Internal Protobuf contracts currently cover:

- orders domain operations
- inventory reservation and release
- payment authorization and void
- shipping creation and cancellation
- saga advancement, compensation, state inspection, and DLQ replay

## Non-Goals for the Current Slice

- Do not expose gRPC as a public merchant API.
- Do not add a second runtime server until a real internal process boundary
  exists.
- Do not bypass the transactional outbox by calling internal RPCs directly from
  public request handlers for saga side effects.

## Design Rule

When REST accepts a command that starts or mutates the saga, REST persists the
state transition and outbox event. Internal gRPC contracts describe how that
domain would be split behind the worker/provider boundary, but the outbox/inbox
contract remains the source of consistency.
