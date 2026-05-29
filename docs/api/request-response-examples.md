# Request and Response Examples

## Create order

The merchant is derived from `X-API-Key`; clients do not send `merchant_id` in the request body.

```sh
curl --request POST http://localhost:8080/api/v1/orders \
  --header 'Content-Type: application/json' \
  --header 'X-API-Key: fh_live_merchant_demo' \
  --header 'Idempotency-Key: web-100045-attempt-1' \
  --data '{
    "external_order_id": "web-100045",
    "currency": "USD",
    "customer": {
      "id": "cus_23901",
      "email": "samira@example.com",
      "full_name": "Samira Costa"
    },
    "shipping_address": {
      "line_1": "55 Market Street",
      "line_2": "Suite 11",
      "city": "San Francisco",
      "state": "CA",
      "postal_code": "94105",
      "country": "US"
    },
    "items": [
      {
        "sku": "SKU-CHAIR-BLK",
        "quantity": 1,
        "unit_price": {
          "amount": 18900,
          "currency": "USD"
        }
      }
    ],
    "payment_method": {
      "provider": "stripe",
      "payment_token": "tok_visa_01hzsample"
    }
  }'
```

```json
{
  "data": {
    "order_id": "ord_01hzy72wf4ekcg7fbc7r8rtn2r",
    "merchant_id": "mer_01hzy6v4egscg4r7kb3m7jq2dk",
    "status": "pending_fulfillment",
    "accepted_at": "2026-05-28T20:15:00Z"
  },
  "meta": {
    "request_id": "req_01hzy72wf4ekcg7fbc7r8rtn2r",
    "correlation_id": "cor_01hzy72wf4ekcg7fbc7r8rtn2r"
  }
}
```

## Get order

```sh
curl --request GET http://localhost:8080/api/v1/orders/ord_01hzy72wf4ekcg7fbc7r8rtn2r \
  --header 'X-API-Key: fh_live_merchant_demo'
```

```json
{
  "data": {
    "order_id": "ord_01hzy72wf4ekcg7fbc7r8rtn2r",
    "merchant_id": "mer_01hzy6v4egscg4r7kb3m7jq2dk",
    "external_order_id": "web-100045",
    "status": "shipment_created",
    "currency": "USD",
    "totals": {
      "subtotal": {
        "amount": 18900,
        "currency": "USD"
      },
      "shipping": {
        "amount": 1200,
        "currency": "USD"
      },
      "total": {
        "amount": 20100,
        "currency": "USD"
      }
    },
    "items": [
      {
        "sku": "SKU-CHAIR-BLK",
        "quantity": 1,
        "unit_price": {
          "amount": 18900,
          "currency": "USD"
        },
        "reservation_status": "reserved"
      }
    ],
    "payment": {
      "provider": "stripe",
      "status": "authorized",
      "authorization_id": "pay_01hzy7aqwbrk4k6q31z9r1rj6z"
    },
    "shipment": {
      "shipment_id": "shp_01hzy7bkqj29z5wdpq3z6hccbc",
      "status": "label_created",
      "carrier": "ups",
      "tracking_number": "1Z999AA10123456784"
    },
    "created_at": "2026-05-28T20:15:00Z",
    "updated_at": "2026-05-28T20:15:09Z"
  },
  "meta": {
    "request_id": "req_01hzy7cn4r5k2y4r4d49gs68f0",
    "correlation_id": "cor_01hzy72wf4ekcg7fbc7r8rtn2r"
  }
}
```

## Get shipment

```sh
curl --request GET http://localhost:8080/api/v1/shipments/shp_01hzy7bkqj29z5wdpq3z6hccbc \
  --header 'X-API-Key: fh_live_merchant_demo'
```

```json
{
  "data": {
    "shipment_id": "shp_01hzy7bkqj29z5wdpq3z6hccbc",
    "order_id": "ord_01hzy72wf4ekcg7fbc7r8rtn2r",
    "merchant_id": "mer_01hzy6v4egscg4r7kb3m7jq2dk",
    "carrier": "ups",
    "tracking_number": "1Z999AA10123456784",
    "status": "in_transit",
    "events": [
      {
        "occurred_at": "2026-05-28T20:15:22Z",
        "status": "label_created",
        "description": "Label created with carrier."
      },
      {
        "occurred_at": "2026-05-28T22:30:10Z",
        "status": "in_transit",
        "description": "Package accepted at origin facility."
      }
    ]
  },
  "meta": {
    "request_id": "req_01hzy7dbfnd9sk2t7q2fef4t8d",
    "correlation_id": "cor_01hzy72wf4ekcg7fbc7r8rtn2r"
  }
}
```

## Cancel order

```sh
curl --request POST http://localhost:8080/api/v1/orders/ord_01hzy72wf4ekcg7fbc7r8rtn2r/cancel \
  --header 'Content-Type: application/json' \
  --header 'Authorization: Bearer ops-token' \
  --data '{
    "reason": "fraud_review",
    "requested_by": {
      "type": "support_agent",
      "id": "agt_4421"
    }
  }'
```

```json
{
  "data": {
    "order_id": "ord_01hzy72wf4ekcg7fbc7r8rtn2r",
    "merchant_id": "mer_01hzy6v4egscg4r7kb3m7jq2dk",
    "status": "cancellation_pending",
    "accepted_at": "2026-05-28T20:16:00Z"
  },
  "meta": {
    "request_id": "req_01hzy7gk1g1h2aajm1y8t4k3rk",
    "correlation_id": "cor_01hzy72wf4ekcg7fbc7r8rtn2r"
  }
}
```

## Validation error

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
