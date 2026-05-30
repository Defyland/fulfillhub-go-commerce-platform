# Event Contract Breaking Change

Use this runbook when a saga consumer starts failing after an event payload change.

## Triage

- Identify the routing key and schema file under `docs/events/`.
- Check RabbitMQ DLQ depth for the affected queue.
- Compare producer payloads with the previous accepted schema.
- Confirm `message_id`, `order_id`, `merchant_id`, `correlation_id`, and `causation_id` are still present.
- Confirm the message body still declares the expected `schema_version`.
- Confirm the failure is a contract issue, not a transient provider or dependency issue.

## Recovery

- Pause the affected worker if it is creating bad compensations.
- Restore a backward-compatible producer payload.
- Replay DLQ messages only after the consumer accepts the contract and the
  replay plan preserves the original `message_id`.
- Record the incident and schema change in the audit trail.

## Prevention

- Add optional fields before requiring them.
- Keep old and new producers compatible through one deploy window.
- Update the versioned schema, event catalog, threat model, and consumer tests in
  the same change.
- Use a new schema version when a field is removed, renamed, retyped, or given a
  different business meaning.
