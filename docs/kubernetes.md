# Kubernetes Blueprint

The manifests in `deployments/kubernetes/base` are a production-like blueprint,
not a one-click production environment. They intentionally avoid real cloud
credentials, managed service URLs, and provider-specific ingress details.

## Included

- Namespace, service account, API service, API deployment, worker deployment,
  outbox relay deployment, and migration job.
- Readiness and liveness probes for API pods.
- Pre-sync migration job pattern for release ordering.
- ExternalSecret contract for runtime secrets.
- NetworkPolicy baseline.
- HorizontalPodAutoscaler for API pods.
- PodDisruptionBudget for API availability during voluntary disruption.
- Read-only root filesystem and non-root security context where applicable.

## Required Per Environment

Each real environment must supply:

- image tags from the release pipeline
- managed PostgreSQL URL
- RabbitMQ URL
- Redis URL when rate limiting is enabled
- operations JWT secret and rotation policy
- metrics bearer token when `/metrics` is scraped outside a trusted network
- payment and shipment webhook secrets
- ingress, DNS, TLS, and cloud IAM bindings
- External Secrets provider configuration

## Runtime Defaults

- `fulfillhub-api` refuses to start without `DATABASE_URL` unless
  `ALLOW_IN_MEMORY_STORE=true` is explicitly set.
- Local demo credentials and the static local ops token are not enabled in the
  Kubernetes blueprint.
- pprof is disabled by default and should stay off unless the pod is isolated
  and the debug endpoint is reachable only by trusted operators.

## Validation

The CI workflow parses Kubernetes YAML and runs production-readiness tests, but
it does not apply these manifests to a real cluster. Cluster-specific admission
policies, workload identity, External Secrets wiring, ingress, DNS, TLS, and
managed service connectivity remain environment responsibilities.
