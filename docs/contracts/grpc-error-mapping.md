# gRPC and REST Error Mapping

FulfillHub keeps the public API as REST/OpenAPI and treats gRPC as the internal
service contract for future process boundaries. Domain errors must map
deterministically to both transports so clients do not depend on Go error text.

## Domain to gRPC

| Domain condition | Go source | gRPC status | Retryable | Notes |
| --- | --- | --- | --- | --- |
| Validation failure | `commerce.ValidationError` | `INVALID_ARGUMENT` | no | Return field-level details when available. |
| Duplicate external order | `commerce.ErrDuplicateOrder` | `ALREADY_EXISTS` | no | Same merchant and external order ID already accepted. |
| Invalid state transition | `commerce.ErrInvalidStateTransition` | `FAILED_PRECONDITION` | no | Caller must refresh aggregate state before retrying. |
| Missing order, shipment, or reservation | `commerce.ErrNotFound` | `NOT_FOUND` | no | Must not reveal resources from another tenant. |
| Insufficient stock | `commerce.ErrInsufficientStock` | `FAILED_PRECONDITION` | no | Saga emits `inventory.rejected`; do not retry blindly. |
| Authentication missing or invalid | API auth guard | `UNAUTHENTICATED` | no | Applies to merchant API keys and operations JWTs. |
| Authenticated caller lacks tenant access | API auth guard | `PERMISSION_DENIED` | no | Do not include foreign tenant identifiers. |
| Rate limit exceeded | rate limiter | `RESOURCE_EXHAUSTED` | yes | Include retry metadata when a limiter provides it. |
| PostgreSQL, RabbitMQ, Redis unavailable | dependency adapters | `UNAVAILABLE` | yes | Preserve wrapped root cause in logs/traces. |
| Request timeout or cancellation | `context.Context` | `DEADLINE_EXCEEDED` or `CANCELLED` | maybe | Map based on `context.DeadlineExceeded` or `context.Canceled`. |
| Unexpected internal failure | fallback | `INTERNAL` | maybe | Log with correlation ID; do not expose raw error strings. |

## Domain to HTTP

The REST API uses the envelope documented in
[docs/api/error-format.md](../api/error-format.md). Current HTTP mappings in
`internal/api/server.go` are:

| Domain condition | HTTP status | Error code |
| --- | --- | --- |
| Validation failure | `422` | `validation_failed` |
| Duplicate external order | `409` | `duplicate_order` |
| Invalid state transition | `409` | `invalid_state_transition` |
| Missing resource | `404` | `not_found` |
| Authentication missing or invalid | `401` | `unauthorized` |
| Tenant access denied | `403` | `forbidden` |
| Rate limit exceeded | `429` | `rate_limited` |
| Dependency unavailable | `503` | `dependency_unavailable` |
| Payload too large | `413` | `payload_too_large` |
| Malformed or unknown-field JSON | `400` | `invalid_json` |
| Unexpected internal failure | `500` | `internal_error` |

## Implementation Rule

Use `errors.Is` and `errors.As` at transport boundaries. Internal packages may
wrap errors with context, but transport handlers must map by stable domain
sentinel or typed error, never by string matching.
