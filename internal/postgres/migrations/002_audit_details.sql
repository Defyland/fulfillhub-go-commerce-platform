-- Rollback: before production data exists, run
-- ALTER TABLE audit_logs DROP COLUMN IF EXISTS details. After production data
-- exists, keep the column and deploy a forward-compatible application change.

ALTER TABLE audit_logs
  ADD COLUMN IF NOT EXISTS details JSONB NOT NULL DEFAULT '{}'::jsonb;
