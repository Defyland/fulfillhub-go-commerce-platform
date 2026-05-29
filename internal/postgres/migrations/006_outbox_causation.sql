-- Rollback: keep the column once production events exist so causal traces stay
-- reconstructable. Before production data exists, drop idx_outbox_events_causation
-- and the outbox_events.causation_id column.

ALTER TABLE outbox_events
  ADD COLUMN IF NOT EXISTS causation_id TEXT;

UPDATE outbox_events
SET causation_id = message_id
WHERE causation_id IS NULL;

ALTER TABLE outbox_events
  ALTER COLUMN causation_id SET NOT NULL;

CREATE INDEX IF NOT EXISTS idx_outbox_events_causation
  ON outbox_events (correlation_id, causation_id);
