-- Rollback: before production data exists, drop fk_orders_merchant from
-- orders. After production data exists, keep tenant referential integrity and
-- use a forward-compatible migration if merchant ownership must change.

INSERT INTO merchants (id, name)
SELECT DISTINCT merchant_id, merchant_id
FROM orders
WHERE merchant_id <> ''
ON CONFLICT (id) DO NOTHING;

DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1
    FROM pg_constraint
    WHERE conname = 'fk_orders_merchant'
      AND conrelid = 'orders'::regclass
  ) THEN
    ALTER TABLE orders
      ADD CONSTRAINT fk_orders_merchant
      FOREIGN KEY (merchant_id)
      REFERENCES merchants(id)
      NOT VALID;
  END IF;
END $$;

ALTER TABLE orders VALIDATE CONSTRAINT fk_orders_merchant;
