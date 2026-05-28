# Error Format

All non-2xx responses must use the same envelope so API consumers, dashboards, and incident tooling can parse failures consistently.

## Envelope

```json
{
  "error": {
    "code": "validation_failed",
    "message": "Request body contains invalid fields.",
    "retryable": false,
    "details": [
      {
        "field": "items[0].quantity",
        "issue": "must be greater than zero"
      }
    ]
  },
  "meta": {
    "request_id": "req_01hzy77dcrv3qj6hg4n8x6z2v1",
    "correlation_id": "cor_01hzy77dcrv3qj6hg4n8x6z2v1",
    "timestamp": "2026-05-28T20:15:00Z"
  }
}
```

## Field semantics

| Field | Meaning |
| --- | --- |
| `error.code` | Stable machine-readable category used by clients and alerting |
| `error.message` | Human-readable explanation suitable for logs and support tooling |
| `error.retryable` | Whether the caller may retry safely after backoff |
| `error.details` | Optional field-level validation or business-rule failures |
| `meta.request_id` | Unique per HTTP request |
| `meta.correlation_id` | Propagates across the full order workflow, including message handlers |
| `meta.timestamp` | UTC timestamp for the failure payload |

## Expected error codes

| HTTP status | Code | Example |
| --- | --- | --- |
| `401` | `unauthorized` | Invalid or missing API key |
| `403` | `forbidden` | Authenticated caller crossing tenant boundaries |
| `404` | `not_found` | Unknown order or shipment |
| `409` | `duplicate_order` or `invalid_state_transition` | Replayed external order ID or forbidden cancellation |
| `422` | `validation_failed` | Invalid request structure |
| `429` | `rate_limited` | Merchant exceeded write quota |
| `503` | `dependency_unavailable` | Broker, cache, or database readiness failure |

## Authorization failure example

```json
{
  "error": {
    "code": "forbidden",
    "message": "The caller cannot access this order.",
    "retryable": false,
    "details": []
  },
  "meta": {
    "request_id": "req_01hzy7dwvhrk0sqhkg1j81pc1n",
    "correlation_id": "cor_01hzy72wf4ekcg7fbc7r8rtn2r",
    "timestamp": "2026-05-28T20:15:00Z"
  }
}
```
