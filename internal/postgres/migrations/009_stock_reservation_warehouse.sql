-- Rollback: before production reservation data depends on warehouse-level
-- stock accounting, drop fk_stock_reservations_warehouse, drop
-- idx_stock_reservations_warehouse_sku, and drop stock_reservations.warehouse_id.
-- After production data exists, preserve reservation provenance and deploy a
-- forward-compatible corrective migration.

ALTER TABLE stock_reservations
  ADD COLUMN IF NOT EXISTS warehouse_id TEXT;

DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1
    FROM pg_constraint
    WHERE conname = 'fk_stock_reservations_warehouse'
  ) THEN
    ALTER TABLE stock_reservations
      ADD CONSTRAINT fk_stock_reservations_warehouse
      FOREIGN KEY (warehouse_id) REFERENCES warehouses(id) ON DELETE RESTRICT;
  END IF;
END $$;

CREATE INDEX IF NOT EXISTS idx_stock_reservations_warehouse_sku
  ON stock_reservations (warehouse_id, sku)
  WHERE warehouse_id IS NOT NULL;
