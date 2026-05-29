CREATE TABLE IF NOT EXISTS notification_events (
  id BIGSERIAL PRIMARY KEY,
  order_id TEXT NOT NULL REFERENCES orders(order_id) ON DELETE CASCADE,
  merchant_id TEXT NOT NULL,
  source_message_id TEXT NOT NULL UNIQUE,
  source_event_type TEXT NOT NULL,
  channel TEXT NOT NULL,
  status TEXT NOT NULL,
  correlation_id TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_notification_events_order_created_at
  ON notification_events (order_id, created_at DESC);
