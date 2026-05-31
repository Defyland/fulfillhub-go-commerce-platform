# Authorization Matrix

## HTTP Endpoints

| Endpoint | Merchant API key | Operations token | Anonymous |
| --- | --- | --- | --- |
| `POST /api/v1/orders` | Create order for own merchant only | Denied | Denied |
| `GET /api/v1/orders/{orderId}` | Own merchant orders only | Any merchant order | Denied |
| `POST /api/v1/orders/{orderId}/cancel` | Own merchant orders only | Any merchant order | Denied |
| `GET /api/v1/shipments/{shipmentId}` | Own merchant shipments only | Any merchant shipment | Denied |
| `GET /healthz` | Allowed | Allowed | Allowed |
| `GET /readyz` | Allowed | Allowed | Allowed |
| `GET /metrics` | Allowed only when no metrics bearer token is configured | Allowed only when no metrics bearer token is configured | Allowed only when no metrics bearer token is configured |
| `GET /metrics` with metrics bearer | Denied unless token matches `METRICS_BEARER_TOKEN` | Denied unless token matches `METRICS_BEARER_TOKEN` | Denied unless token matches `METRICS_BEARER_TOKEN` |

## Actor Metadata

| Flow | Actor source | Audit action |
| --- | --- | --- |
| Create order | `merchant_id` derived from `X-API-Key` | `order.create` |
| Cancel order | `requested_by.type`, `requested_by.id`, and `reason` from request body | `order.cancel_requested` |
| DLQ replay | `OPS_ACTOR_ID` environment variable | `dlq.replay` |

## Enforcement Notes

- Merchant API keys map to fixed merchant IDs in the local slice.
- Request bodies cannot override `merchant_id`.
- Operations access validates HS256 JWTs when `OPS_JWT_SECRET` is configured,
  including optional `OPS_JWT_ISSUER`, `OPS_JWT_AUDIENCE`, and previous secrets
  during key rotation.
- Accepted operations roles are `operations` and `ops`.
- `Bearer ops-token` is a local-only fallback when `ALLOW_LOCAL_OPS_TOKEN=true`
  and `OPS_JWT_SECRET` is absent.
- `METRICS_BEARER_TOKEN` enables runtime bearer protection for `/metrics`.
- Metrics should still sit behind network policy, gateway auth, or Prometheus-only scrape access outside local development.
