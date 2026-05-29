# Authorization Matrix

## HTTP Endpoints

| Endpoint | Merchant API key | Operations token | Anonymous |
| --- | --- | --- | --- |
| `POST /api/v1/orders` | Create order for own merchant only | Denied | Denied |
| `GET /api/v1/orders/{orderId}` | Own merchant orders only | Any merchant order | Denied |
| `POST /api/v1/orders/{orderId}/cancel` | Own merchant orders only | Any merchant order | Denied |
| `GET /healthz` | Allowed | Allowed | Allowed |
| `GET /readyz` | Allowed | Allowed | Allowed |
| `GET /metrics` | Allowed in local slice | Allowed in local slice | Allowed in local slice |

## Actor Metadata

| Flow | Actor source | Audit action |
| --- | --- | --- |
| Create order | `merchant_id` derived from `X-API-Key` | `order.create` |
| Cancel order | `requested_by.type` and `requested_by.id` from request body | `order.cancel_requested` |
| DLQ replay | Environment and operator shell context | Pending durable audit record |

## Enforcement Notes

- Merchant API keys map to fixed merchant IDs in the local slice.
- Request bodies cannot override `merchant_id`.
- Operations access is intentionally minimal and represented by `Bearer ops-token`.
- Production operations access should validate JWT issuer, audience, expiry, and role claims.
- Metrics should sit behind network policy, gateway auth, or Prometheus-only scrape access outside local development.
