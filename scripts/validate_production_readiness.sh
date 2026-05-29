#!/usr/bin/env sh

set -eu

REPO_ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$REPO_ROOT"

required_files="
docs/production-readiness.md
docs/runbooks/deployment-rollback.md
docs/runbooks/slo-alert-response.md
docs/runbooks/data-protection.md
docs/security/secrets-management.md
docs/security/supply-chain.md
deployments/kubernetes/base/kustomization.yaml
deployments/kubernetes/base/external-secret.yaml
deployments/kubernetes/base/job-migrate.yaml
deployments/kubernetes/base/deployment-api.yaml
deployments/kubernetes/base/deployment-workers.yaml
deployments/prometheus/rules/fulfillhub-alerts.yml
cmd/fulfillhub-migrate/main.go
"

for file in $required_files; do
  if [ ! -f "$file" ]; then
    echo "missing production readiness file: $file" >&2
    exit 1
  fi
done

for alert in \
  FulfillHubAPIDown \
  FulfillHubOutboxStalled \
  FulfillHubDLQBacklog \
  FulfillHubQueueWithoutConsumers \
  FulfillHubManualReviewBacklog \
  FulfillHubOrderFailureRatioHigh; do
  if ! grep -Fq "alert: $alert" deployments/prometheus/rules/fulfillhub-alerts.yml; then
    echo "missing alert: $alert" >&2
    exit 1
  fi
done

if ! grep -Fq "rule_files:" deployments/prometheus/prometheus.yml; then
  echo "Prometheus config must load alert rule files" >&2
  exit 1
fi

if ! grep -Fq "runbook_url:" deployments/prometheus/rules/fulfillhub-alerts.yml; then
  echo "alert rules must include runbook URLs" >&2
  exit 1
fi

if grep -R -E '(fh_live_|whsec_|ops-token|postgres://|amqp://guest|local-metrics-token)' deployments/kubernetes >/dev/null; then
  echo "Kubernetes production blueprint must not contain literal local credentials" >&2
  exit 1
fi

go test ./internal/spec -run 'Production|Prometheus'

echo "Production readiness validation passed."
