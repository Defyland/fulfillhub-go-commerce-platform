import http from 'k6/http';
import { check, sleep } from 'k6';

export const options = {
  vus: 5,
  duration: '1m',
  thresholds: {
    http_req_failed: ['rate<0.01'],
    http_req_duration: ['p(95)<250'],
  },
};

const baseUrl = __ENV.BASE_URL || 'http://localhost:8080';

export default function () {
  const id = `${__VU}-${__ITER}-${Date.now()}`;
  const response = http.post(`${baseUrl}/api/v1/orders`, JSON.stringify(orderPayload(id)), {
    headers: {
      'Content-Type': 'application/json',
      'X-API-Key': 'fh_live_merchant_demo',
      'Idempotency-Key': `k6-smoke-${id}`,
    },
  });

  check(response, {
    'order accepted': (res) => res.status === 202,
  });
  sleep(1);
}

function orderPayload(id) {
  return {
    external_order_id: `k6-smoke-${id}`,
    currency: 'USD',
    customer: {
      id: `cus-${id}`,
      email: 'samira@example.com',
      full_name: 'Samira Costa',
    },
    shipping_address: {
      line_1: '55 Market Street',
      city: 'San Francisco',
      state: 'CA',
      postal_code: '94105',
      country: 'US',
    },
    items: [
      {
        sku: 'SKU-CHAIR-BLK',
        quantity: 1,
        unit_price: {
          amount: 18900,
          currency: 'USD',
        },
      },
    ],
    payment_method: {
      provider: 'stripe',
      payment_token: 'tok_visa_01hzsample',
    },
  };
}
