-- Rollback: ALTER TABLE orders DROP CONSTRAINT IF EXISTS chk_orders_status;
-- If a new order status is introduced, deploy code that understands it before
-- expanding this constraint in a forward migration.

ALTER TABLE orders
  DROP CONSTRAINT IF EXISTS chk_orders_status;

ALTER TABLE orders
  ADD CONSTRAINT chk_orders_status CHECK (
    status IN (
      'pending_fulfillment',
      'inventory_reserved',
      'payment_authorized',
      'shipment_created',
      'cancellation_pending',
      'manual_review',
      'cancelled',
      'completed',
      'failed'
    )
  );
