#!/usr/bin/env sh

set -eu

REPO_ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$REPO_ROOT"

API_PORT=${API_PORT:-18080}
POSTGRES_PORT=${POSTGRES_PORT:-15432}
RABBITMQ_PORT=${RABBITMQ_PORT:-15671}
RABBITMQ_MANAGEMENT_PORT=${RABBITMQ_MANAGEMENT_PORT:-15673}
REDIS_PORT=${REDIS_PORT:-16379}
PROMETHEUS_PORT=${PROMETHEUS_PORT:-19090}
GRAFANA_PORT=${GRAFANA_PORT:-13000}
OTEL_COLLECTOR_OTLP_GRPC_PORT=${OTEL_COLLECTOR_OTLP_GRPC_PORT:-14317}
OTEL_COLLECTOR_OTLP_HTTP_PORT=${OTEL_COLLECTOR_OTLP_HTTP_PORT:-14318}
BASE_URL=${BASE_URL:-http://localhost:${API_PORT}}
METRICS_BEARER_TOKEN=${METRICS_BEARER_TOKEN:-local-metrics-token}
SAGA_TIMEOUT_SECONDS=${SAGA_TIMEOUT_SECONDS:-90}
RATE_LIMIT_PER_MINUTE=${RATE_LIMIT_PER_MINUTE:-60000}
export METRICS_BEARER_TOKEN
export RATE_LIMIT_PER_MINUTE
export API_PORT
export POSTGRES_PORT
export RABBITMQ_PORT
export RABBITMQ_MANAGEMENT_PORT
export REDIS_PORT
export PROMETHEUS_PORT
export GRAFANA_PORT
export OTEL_COLLECTOR_OTLP_GRPC_PORT
export OTEL_COLLECTOR_OTLP_HTTP_PORT

require_command() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing required command: $1" >&2
    exit 1
  fi
}

wait_for_api() {
  deadline=$(($(date +%s) + 90))
  while [ "$(date +%s)" -lt "$deadline" ]; do
    if curl -fsS "$BASE_URL/readyz" >/dev/null 2>&1; then
      return 0
    fi
    sleep 2
  done
  echo "API did not become ready at $BASE_URL/readyz" >&2
  exit 1
}

json_field() {
  field=$1
  python3 -c "import json,sys; print(json.load(sys.stdin)$field)"
}

post_order() {
  unique=$(date -u +%Y%m%d%H%M%S)
  curl -fsS -X POST "$BASE_URL/api/v1/orders" \
    -H "Content-Type: application/json" \
    -H "X-API-Key: fh_live_merchant_demo" \
    -H "Idempotency-Key: compose-saga-${unique}" \
    -d "{
      \"external_order_id\":\"compose-saga-${unique}\",
      \"currency\":\"USD\",
      \"customer\":{\"id\":\"cus-compose-${unique}\",\"email\":\"samira@example.com\",\"full_name\":\"Samira Costa\"},
      \"shipping_address\":{\"line_1\":\"55 Market Street\",\"city\":\"San Francisco\",\"state\":\"CA\",\"postal_code\":\"94105\",\"country\":\"US\"},
      \"items\":[{\"sku\":\"SKU-CHAIR-BLK\",\"quantity\":1,\"unit_price\":{\"amount\":18900,\"currency\":\"USD\"}}],
      \"payment_method\":{\"provider\":\"stripe\",\"payment_token\":\"tok_visa_01hzsample\"}
    }"
}

order_status() {
  order_id=$1
  curl -fsS -H "X-API-Key: fh_live_merchant_demo" "$BASE_URL/api/v1/orders/$order_id" |
    json_field '["data"]["status"]'
}

metrics() {
  curl -fsS -H "Authorization: Bearer ${METRICS_BEARER_TOKEN}" "$BASE_URL/metrics"
}

wait_for_outbox_drain() {
  deadline=$(($(date +%s) + 30))
  last_backlog=""
  while [ "$(date +%s)" -lt "$deadline" ]; do
    metrics_body=$(metrics)
    last_backlog=$(printf '%s\n' "$metrics_body" | awk '$1 == "fulfillhub_outbox_unpublished_total" { print int($2); found = 1 } END { if (!found) print -1 }')
    if [ "$last_backlog" -eq 0 ]; then
      printf '%s\n' "$metrics_body"
      return 0
    fi
    sleep 2
  done
  echo "outbox did not drain after saga completion; last backlog: $last_backlog" >&2
  return 1
}

require_command docker
require_command curl
require_command python3
require_command awk

if ! docker info >/dev/null 2>&1; then
  echo "Docker daemon is not available; start Docker before running compose saga smoke." >&2
  exit 1
fi

trap 'docker compose down >/dev/null 2>&1 || true' EXIT INT TERM

docker compose up -d --build
wait_for_api

response=$(post_order)
order_id=$(printf '%s\n' "$response" | json_field '["data"]["order_id"]')

deadline=$(($(date +%s) + SAGA_TIMEOUT_SECONDS))
last_status=""
while [ "$(date +%s)" -lt "$deadline" ]; do
  last_status=$(order_status "$order_id")
  if [ "$last_status" = "completed" ]; then
    break
  fi
  sleep 2
done

if [ "$last_status" != "completed" ]; then
  echo "order $order_id did not complete within ${SAGA_TIMEOUT_SECONDS}s; last status: $last_status" >&2
  docker compose logs --tail=120 api outbox-relay inventory-worker payments-worker shipments-worker orders-finalizer >&2 || true
  exit 1
fi

metrics_body=$(wait_for_outbox_drain)
printf '%s\n' "$metrics_body" | grep -Fq 'fulfillhub_orders_total{status="completed"}'
printf 'compose saga smoke completed order %s\n' "$order_id"
