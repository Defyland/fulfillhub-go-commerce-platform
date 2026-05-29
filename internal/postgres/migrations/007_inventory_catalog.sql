-- Rollback: before production inventory data exists, drop inventory_items and
-- warehouses. After production data exists, preserve stock truth and deploy a
-- forward-compatible corrective migration.

CREATE TABLE IF NOT EXISTS warehouses (
  id TEXT PRIMARY KEY,
  merchant_id TEXT NOT NULL REFERENCES merchants(id) ON DELETE CASCADE,
  code TEXT NOT NULL,
  name TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'active',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (merchant_id, code)
);

CREATE TABLE IF NOT EXISTS inventory_items (
  id BIGSERIAL PRIMARY KEY,
  warehouse_id TEXT NOT NULL REFERENCES warehouses(id) ON DELETE CASCADE,
  sku TEXT NOT NULL,
  available_quantity INTEGER NOT NULL DEFAULT 0 CHECK (available_quantity >= 0),
  reserved_quantity INTEGER NOT NULL DEFAULT 0 CHECK (reserved_quantity >= 0),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (warehouse_id, sku)
);

CREATE INDEX IF NOT EXISTS idx_inventory_items_sku
  ON inventory_items (sku);
