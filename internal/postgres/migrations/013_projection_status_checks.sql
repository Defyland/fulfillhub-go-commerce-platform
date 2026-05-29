-- Rollback: drop the projection status constraints below. After production
-- data exists, prefer a forward migration that expands accepted values before
-- deploying runtime branches that emit new statuses.

ALTER TABLE order_items
  DROP CONSTRAINT IF EXISTS chk_order_items_reservation_status;

ALTER TABLE order_items
  ADD CONSTRAINT chk_order_items_reservation_status CHECK (
    reservation_status IN ('pending', 'reserved', 'rejected', 'released')
  );

ALTER TABLE orders
  DROP CONSTRAINT IF EXISTS chk_orders_payment_status;

ALTER TABLE orders
  ADD CONSTRAINT chk_orders_payment_status CHECK (
    payment_status IS NULL OR payment_status IN (
      'pending_authorization',
      'authorized',
      'failed',
      'voided'
    )
  );

ALTER TABLE stock_reservations
  DROP CONSTRAINT IF EXISTS chk_stock_reservations_status;

ALTER TABLE stock_reservations
  ADD CONSTRAINT chk_stock_reservations_status CHECK (
    status IN ('reserved', 'released')
  );

ALTER TABLE payment_authorizations
  DROP CONSTRAINT IF EXISTS chk_payment_authorizations_status;

ALTER TABLE payment_authorizations
  ADD CONSTRAINT chk_payment_authorizations_status CHECK (
    status IN ('authorized', 'voided')
  );

ALTER TABLE shipments
  DROP CONSTRAINT IF EXISTS chk_shipments_status;

ALTER TABLE shipments
  ADD CONSTRAINT chk_shipments_status CHECK (
    status IN ('created', 'failed')
  );

ALTER TABLE notification_events
  DROP CONSTRAINT IF EXISTS chk_notification_events_status;

ALTER TABLE notification_events
  ADD CONSTRAINT chk_notification_events_status CHECK (
    status IN ('queued', 'sent', 'failed')
  );

ALTER TABLE compensation_events
  DROP CONSTRAINT IF EXISTS chk_compensation_events_target_order_status;

ALTER TABLE compensation_events
  ADD CONSTRAINT chk_compensation_events_target_order_status CHECK (
    target_order_status IN (
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

ALTER TABLE compensation_events
  DROP CONSTRAINT IF EXISTS chk_compensation_events_status;

ALTER TABLE compensation_events
  ADD CONSTRAINT chk_compensation_events_status CHECK (
    status IN ('recorded', 'succeeded', 'failed')
  );

ALTER TABLE warehouses
  DROP CONSTRAINT IF EXISTS chk_warehouses_status;

ALTER TABLE warehouses
  ADD CONSTRAINT chk_warehouses_status CHECK (
    status IN ('active', 'inactive', 'disabled')
  );
