-- Rollback: before local demo runs depend on seeded inventory, delete the
-- inserted inventory_items, warehouses, and merchants for the two demo API-key
-- merchants. After local runs have created orders, leave the seed data in place
-- and adjust quantities with a forward-compatible inventory correction.

INSERT INTO merchants (id, name)
VALUES
  ('mer_01hzy6v4egscg4r7kb3m7jq2dk', 'Demo Merchant'),
  ('mer_01hzy8v4egscg4r7kb3m7jq9qx', 'Second Demo Merchant')
ON CONFLICT (id) DO NOTHING;

INSERT INTO warehouses (id, merchant_id, code, name, status)
VALUES
  ('wh_demo_primary', 'mer_01hzy6v4egscg4r7kb3m7jq2dk', 'primary', 'Demo Primary Warehouse', 'active'),
  ('wh_demo_second', 'mer_01hzy8v4egscg4r7kb3m7jq9qx', 'primary', 'Second Demo Primary Warehouse', 'active')
ON CONFLICT (merchant_id, code) DO UPDATE
SET name = EXCLUDED.name,
    status = EXCLUDED.status,
    updated_at = now();

INSERT INTO inventory_items (warehouse_id, sku, available_quantity, reserved_quantity)
SELECT id, 'SKU-CHAIR-BLK', 10000000, 0
FROM warehouses
WHERE merchant_id = 'mer_01hzy6v4egscg4r7kb3m7jq2dk' AND code = 'primary'
UNION ALL
SELECT id, 'SKU-LAMP-WHT', 1000000, 0
FROM warehouses
WHERE merchant_id = 'mer_01hzy6v4egscg4r7kb3m7jq2dk' AND code = 'primary'
UNION ALL
SELECT id, 'SKU-CHAIR-BLK', 10000000, 0
FROM warehouses
WHERE merchant_id = 'mer_01hzy8v4egscg4r7kb3m7jq9qx' AND code = 'primary'
UNION ALL
SELECT id, 'SKU-LAMP-WHT', 1000000, 0
FROM warehouses
WHERE merchant_id = 'mer_01hzy8v4egscg4r7kb3m7jq9qx' AND code = 'primary'
ON CONFLICT (warehouse_id, sku) DO UPDATE
SET available_quantity = GREATEST(inventory_items.available_quantity, EXCLUDED.available_quantity),
    updated_at = now();
