-- Rollback: ALTER TABLE orders DROP COLUMN IF EXISTS shipping_address_ref;
-- ALTER TABLE orders DROP COLUMN IF EXISTS payment_credential_ref;
-- These columns store opaque integration references, not raw payment tokens or
-- customer address payloads.

ALTER TABLE orders
  ADD COLUMN IF NOT EXISTS payment_credential_ref TEXT,
  ADD COLUMN IF NOT EXISTS shipping_address_ref TEXT;
