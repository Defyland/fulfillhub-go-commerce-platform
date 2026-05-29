-- Rollback: before production data exists, drop compensation_events. After
-- production data exists, preserve failure-handling evidence and deploy a
-- forward-compatible corrective migration.

CREATE TABLE IF NOT EXISTS compensation_events (
  id BIGSERIAL PRIMARY KEY,
  order_id TEXT NOT NULL REFERENCES orders(order_id) ON DELETE CASCADE,
  merchant_id TEXT NOT NULL,
  source_message_id TEXT NOT NULL UNIQUE,
  source_event_type TEXT NOT NULL,
  action TEXT NOT NULL,
  target_order_status TEXT NOT NULL,
  status TEXT NOT NULL,
  correlation_id TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_compensation_events_order_created_at
  ON compensation_events (order_id, created_at DESC);
