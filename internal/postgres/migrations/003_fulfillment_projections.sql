CREATE TABLE IF NOT EXISTS stock_reservations (
  order_id TEXT NOT NULL,
  sku TEXT NOT NULL,
  quantity INTEGER NOT NULL CHECK (quantity > 0),
  status TEXT NOT NULL,
  reserved_at TIMESTAMPTZ NOT NULL,
  released_at TIMESTAMPTZ,
  PRIMARY KEY (order_id, sku),
  FOREIGN KEY (order_id, sku) REFERENCES order_items(order_id, sku) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_stock_reservations_status
  ON stock_reservations (status, reserved_at DESC);

CREATE TABLE IF NOT EXISTS payment_authorizations (
  authorization_id TEXT PRIMARY KEY,
  order_id TEXT NOT NULL UNIQUE REFERENCES orders(order_id) ON DELETE CASCADE,
  merchant_id TEXT NOT NULL,
  provider TEXT NOT NULL,
  status TEXT NOT NULL,
  amount BIGINT NOT NULL CHECK (amount >= 0),
  currency CHAR(3) NOT NULL,
  authorized_at TIMESTAMPTZ NOT NULL,
  voided_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_payment_authorizations_order_status
  ON payment_authorizations (order_id, status);

CREATE TABLE IF NOT EXISTS shipments (
  shipment_id TEXT PRIMARY KEY,
  order_id TEXT NOT NULL UNIQUE REFERENCES orders(order_id) ON DELETE CASCADE,
  merchant_id TEXT NOT NULL,
  carrier TEXT NOT NULL,
  tracking_number TEXT NOT NULL UNIQUE,
  status TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_shipments_order_status
  ON shipments (order_id, status);
