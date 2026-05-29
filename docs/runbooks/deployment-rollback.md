# Deployment and Rollback Runbook

## Purpose

This runbook describes how to promote, verify, and roll back FulfillHub in a
production-like Kubernetes environment. It assumes the manifests under
`deployments/kubernetes/base` are rendered by the deployment platform with a real
image digest, External Secrets provider, ingress, and managed dependency
endpoints.

## Pre-deploy checklist

1. Confirm the release commit passed the full `project-quality` workflow.
2. Confirm the image digest was built from the release commit, not a mutable tag.
3. Confirm SBOM, vulnerability scan, and secret scan artifacts exist for the release.
4. Review every pending migration for expand/contract compatibility.
5. Confirm the previous image digest and rollback command are available.
6. Confirm `DATABASE_URL`, `RABBITMQ_URL`, `REDIS_URL`, `OPS_JWT_SECRET`, and `METRICS_BEARER_TOKEN` are present in the secret manager.
7. Confirm Alertmanager notifications are not silenced for outbox, DLQ, readiness, or error-ratio alerts.

## Deployment flow

1. Render manifests with the immutable image digest.
2. Apply the `fulfillhub-migrate` job and wait for completion.
3. Roll out `fulfillhub-api` with `maxUnavailable: 0`.
4. Roll out `fulfillhub-outbox-relay`.
5. Roll out queue workers one branch at a time: inventory, payments, shipments, orders, cancellation, compensation, notifications.
6. Confirm `/readyz` is green from inside the cluster.
7. Confirm Prometheus scrapes `/metrics` with the configured bearer token.
8. Watch these signals for at least one canary window:
   - `fulfillhub_outbox_oldest_unpublished_age_seconds`
   - `fulfillhub_rabbitmq_queue_messages_ready`
   - `fulfillhub_rabbitmq_queue_consumers`
   - `fulfillhub_orders_total{status="failed"}`
   - `fulfillhub_orders_total{status="manual_review"}`

## Rollback triggers

Start rollback when any of these conditions hold after the first mitigation
attempt:

- `/readyz` remains failing for a new release while the previous release was healthy.
- Outbox age continues increasing for more than one alert window.
- A non-DLQ queue has messages but zero consumers.
- DLQ depth grows with new message IDs after the release.
- Order failure ratio materially exceeds the expected baseline.
- Provider webhooks fail signature verification because a secret rotation was deployed incorrectly.

## Rollback flow

1. Pause deploy automation for the release.
2. Capture current alert state, release digest, migration version, queue depth, and top affected merchants.
3. If the issue is application-only and migrations are backward compatible, roll `fulfillhub-api`, relay, and workers back to the previous image digest.
4. If the issue is a bad secret rotation, restore the previous secret version and restart only affected pods.
5. If the issue is a bad migration, stop writes first, confirm whether the previous binary can run against the current schema, and prefer a forward corrective migration over destructive rollback.
6. Keep DLQ replay paused until the rolled-back runtime is verified healthy.
7. After rollback, confirm readiness, outbox age, DLQ depth, and failure ratio return to baseline.

## Migration rollback rules

- Do not drop columns, tables, or enum/check values during incident rollback unless data loss has been explicitly accepted.
- Prefer a forward migration that restores compatibility.
- If a constraint blocks the previous binary, add a compatibility-expanding migration first.
- If data was written in a new shape, document the reconciliation query and expected row count before backfill or cleanup.

## Post-incident actions

1. Record root cause, release commit, migration version, and affected merchants.
2. Add or update a regression test matching the failure mode.
3. Add or update an alert if detection was late.
4. Add runbook detail if mitigation required undocumented judgment.
5. Review whether provider side effects require reconciliation or compensation.
