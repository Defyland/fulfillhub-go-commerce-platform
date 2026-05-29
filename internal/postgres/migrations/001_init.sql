CREATE TABLE IF NOT EXISTS schema_migrations (
  version TEXT PRIMARY KEY,
  applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS merchants (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS orders (
  order_id TEXT PRIMARY KEY,
  merchant_id TEXT NOT NULL,
  external_order_id TEXT NOT NULL,
  status TEXT NOT NULL,
  currency CHAR(3) NOT NULL,
  subtotal_amount BIGINT NOT NULL,
  shipping_amount BIGINT NOT NULL,
  total_amount BIGINT NOT NULL,
  payment_provider TEXT,
  payment_status TEXT,
  payment_authorization_id TEXT,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL,
  version BIGINT NOT NULL DEFAULT 1,
  UNIQUE (merchant_id, external_order_id)
);

CREATE INDEX IF NOT EXISTS idx_orders_merchant_status_created_at
  ON orders (merchant_id, status, created_at DESC);

CREATE TABLE IF NOT EXISTS order_items (
  id BIGSERIAL PRIMARY KEY,
  order_id TEXT NOT NULL REFERENCES orders(order_id) ON DELETE CASCADE,
  sku TEXT NOT NULL,
  quantity INTEGER NOT NULL CHECK (quantity > 0),
  unit_price_amount BIGINT NOT NULL CHECK (unit_price_amount >= 0),
  unit_price_currency CHAR(3) NOT NULL,
  reservation_status TEXT NOT NULL,
  UNIQUE (order_id, sku)
);

CREATE TABLE IF NOT EXISTS idempotency_keys (
  merchant_id TEXT NOT NULL,
  idempotency_key TEXT NOT NULL,
  order_id TEXT NOT NULL REFERENCES orders(order_id) ON DELETE CASCADE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (merchant_id, idempotency_key)
);

CREATE TABLE IF NOT EXISTS outbox_events (
  message_id TEXT PRIMARY KEY,
  correlation_id TEXT NOT NULL,
  event_type TEXT NOT NULL,
  order_id TEXT NOT NULL REFERENCES orders(order_id) ON DELETE CASCADE,
  merchant_id TEXT NOT NULL,
  payload JSONB NOT NULL DEFAULT '{}'::jsonb,
  occurred_at TIMESTAMPTZ NOT NULL,
  published_at TIMESTAMPTZ,
  retry_count INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_outbox_events_unpublished
  ON outbox_events (published_at, occurred_at)
  WHERE published_at IS NULL;

CREATE TABLE IF NOT EXISTS inbox_messages (
  consumer_name TEXT NOT NULL,
  message_id TEXT NOT NULL,
  correlation_id TEXT NOT NULL,
  processed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (consumer_name, message_id)
);

CREATE TABLE IF NOT EXISTS audit_logs (
  id BIGSERIAL PRIMARY KEY,
  merchant_id TEXT NOT NULL,
  order_id TEXT,
  actor_type TEXT NOT NULL,
  actor_id TEXT NOT NULL,
  action TEXT NOT NULL,
  correlation_id TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_audit_logs_merchant_order_created_at
  ON audit_logs (merchant_id, order_id, created_at DESC);
