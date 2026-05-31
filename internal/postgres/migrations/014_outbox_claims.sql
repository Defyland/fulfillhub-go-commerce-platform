-- Rollback: drop idx_outbox_events_claimable, claimed_by, and claimed_until
-- only after all relay processes are stopped and no unpublished rows depend on
-- lease-based publishing. In production, prefer a forward migration that
-- lengthens or disables leases safely.

ALTER TABLE outbox_events
  ADD COLUMN IF NOT EXISTS claimed_by TEXT,
  ADD COLUMN IF NOT EXISTS claimed_until TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_outbox_events_claimable
  ON outbox_events (published_at, claimed_until, occurred_at)
  WHERE published_at IS NULL;
