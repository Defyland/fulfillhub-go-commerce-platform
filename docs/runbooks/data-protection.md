# Data Protection Runbook

## Purpose

This runbook defines how FulfillHub should protect, restore, retain, and purge
data in a real production environment. The repository uses PostgreSQL as the
source of truth for orders, inventory projections, outbox, inbox, and audit
logs, so operational data safety is a launch blocker.

## Backup policy

- Enable managed PostgreSQL continuous backup with point-in-time recovery.
- Keep at least 35 days of PITR for production unless legal retention requires longer.
- Store backups encrypted with a KMS key scoped to database recovery operators.
- Keep RabbitMQ definitions and runtime topology reproducible from code, not manual UI changes.
- Treat Redis rate-limit data as disposable control-plane state.

## Restore drill

Run a restore drill before production launch and at least quarterly:

1. Restore the latest production snapshot into an isolated non-production network.
2. Apply WAL replay to a timestamp chosen by the incident commander.
3. Start API, relay, and workers against the restored database with outbound provider calls disabled.
4. Run read-only order and shipment lookup smoke tests.
5. Verify `schema_migrations`, latest order rows, outbox backlog, inbox records, and audit logs.
6. Record restore start time, usable time, data loss window, and failed assumptions.

## Retention policy

| Data | Default retention | Notes |
| --- | --- | --- |
| Orders and order items | 7 years | Commerce and support history |
| Payment authorization references | 7 years | Store opaque provider references only |
| Raw payment tokens | Not stored | Use PSP/vault-owned tokenization |
| Outbox and inbox messages | 90 days after processing | Keep enough for incident reconstruction |
| Audit logs | 7 years | Append-only operational evidence |
| API request logs | 30 days hot, 1 year cold | Exclude secrets and payment tokens |
| Traces | 14 days | Correlation aid, not source of truth |

## Purge and privacy requests

1. Authenticate the merchant or data subject request through the operations process.
2. Identify orders, shipments, audit rows, and provider references by merchant and customer identifiers.
3. Remove or tokenize personal data where legal requirements allow, while preserving financial/audit records.
4. Do not delete audit logs that are legally required; redact non-required personal details instead.
5. Record the purge action with actor, reason, timestamp, and affected identifiers.

## Backward-compatible migrations

- Use expand/contract migrations for any table with live reads or writes.
- Keep application rollback compatibility for at least one deployed version.
- Do not tighten status constraints until all writers emit the new value set.
- Backfill in batches and monitor lock wait, replication lag, and API error rate.
- Prefer forward corrective migrations over destructive down migrations.
