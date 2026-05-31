#!/usr/bin/env sh

set -eu

REPO_ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$REPO_ROOT"

required_dirs="
cmd
cmd/fulfillhub-api
cmd/fulfillhub-dlq-replay
cmd/fulfillhub-migrate
cmd/fulfillhub-outbox-relay
cmd/fulfillhub-worker
deployments
deployments/kubernetes
deployments/kubernetes/base
deployments/otel-collector
deployments/prometheus
deployments/prometheus/rules
internal
internal/api
internal/commerce
internal/messaging
internal/postgres
internal/providers
internal/spec
docs/adr
docs/api
docs/architecture
docs/benchmarks
docs/contracts
docs/diagrams
docs/events
docs/observability
docs/runbooks
docs/security
benchmarks
benchmarks/k6
benchmarks/results
scripts
proto
.github/workflows
"

for dir in $required_dirs; do
  if [ ! -d "$dir" ]; then
    echo "missing directory: $dir" >&2
    exit 1
  fi
done

required_files="
README.md
openapi.yaml
.markdownlint.json
.spectral.yaml
.tool-versions
Dockerfile
docker-compose.yml
go.mod
go.sum
cmd/fulfillhub-api/main.go
cmd/fulfillhub-dlq-replay/main.go
cmd/fulfillhub-migrate/main.go
cmd/fulfillhub-outbox-relay/main.go
cmd/fulfillhub-worker/main.go
deployments/prometheus/prometheus.yml
deployments/prometheus/rules/fulfillhub-alerts.yml
deployments/otel-collector/config.yml
deployments/kubernetes/base/kustomization.yaml
deployments/kubernetes/base/deployment-api.yaml
deployments/kubernetes/base/deployment-outbox-relay.yaml
deployments/kubernetes/base/deployment-workers.yaml
deployments/kubernetes/base/job-migrate.yaml
internal/api/server.go
internal/api/server_test.go
internal/commerce/model.go
internal/commerce/service.go
internal/commerce/service_test.go
internal/commerce/store.go
internal/fulfillment/handlers.go
internal/fulfillment/handlers_test.go
internal/fulfillment/provider_adapters.go
internal/fulfillment/provider_adapters_test.go
internal/messaging/inbox.go
internal/messaging/dlq.go
internal/messaging/rabbitmq.go
internal/messaging/rabbitmq_integration_test.go
internal/messaging/relay.go
internal/messaging/relay_test.go
internal/messaging/topology.go
internal/messaging/topology_test.go
internal/observability/tracing.go
internal/observability/tracing_test.go
internal/postgres/migrations.go
internal/postgres/migrations_test.go
internal/postgres/pool_test.go
internal/postgres/store.go
internal/postgres/migrations/001_init.sql
internal/postgres/migrations/002_audit_details.sql
internal/postgres/migrations/003_fulfillment_projections.sql
internal/postgres/migrations/004_notification_events.sql
internal/postgres/migrations/005_compensation_events.sql
internal/postgres/migrations/006_outbox_causation.sql
internal/postgres/migrations/007_inventory_catalog.sql
internal/postgres/migrations/008_orders_merchant_fk.sql
internal/postgres/migrations/009_stock_reservation_warehouse.sql
internal/postgres/migrations/010_demo_inventory_seed.sql
internal/postgres/migrations/011_order_status_check.sql
internal/postgres/migrations/012_order_provider_references.sql
internal/postgres/migrations/013_projection_status_checks.sql
internal/postgres/migrations/014_outbox_claims.sql
internal/providers/payment.go
internal/providers/providers_test.go
internal/providers/shipment.go
internal/providers/webhook.go
internal/providers/webhook_test.go
internal/spec/consistency_test.go
internal/spec/event_contracts_test.go
internal/spec/protobuf_contracts_test.go
internal/spec/production_readiness_test.go
proto/orders.proto
proto/inventory.proto
proto/payments.proto
proto/shipping.proto
proto/saga.proto
docs/production-readiness.md
docs/runtime.md
docs/kubernetes.md
docs/engineering-baseline.md
docs/api/request-response-examples.md
docs/api/error-format.md
docs/architecture/overview.md
docs/architecture/domain-model.md
docs/architecture/database-design.md
docs/architecture/senior-technical-assessment.md
docs/benchmarks/methodology.md
docs/benchmarks/results-status.md
docs/contracts/grpc-error-mapping.md
docs/contracts/protobuf-versioning.md
docs/contracts/rest-vs-grpc.md
docs/diagrams/system-context.md
docs/diagrams/order-saga-sequence.md
docs/events/catalog.md
docs/events/README.md
docs/events/threat-model.md
docs/events/order.created.v1.json
docs/events/inventory.reserved.v1.json
docs/events/inventory.rejected.v1.json
docs/events/payment.authorized.v1.json
docs/events/payment.failed.v1.json
docs/events/shipment.created.v1.json
docs/events/shipment.failed.v1.json
docs/events/order.cancel_requested.v1.json
docs/events/order.completed.v1.json
docs/events/order.cancelled.v1.json
docs/events/order.manual_review_required.v1.json
docs/observability/grafana-dashboard.json
docs/runbooks/incident-response.md
docs/runbooks/deployment-rollback.md
docs/runbooks/event-contract-breaking-change.md
docs/runbooks/slo-alert-response.md
docs/runbooks/data-protection.md
docs/security/threat-model.md
docs/security/authorization-matrix.md
docs/security/secrets-management.md
docs/security/supply-chain.md
docs/adr/0001-modular-monolith-first.md
docs/adr/0002-rabbitmq-outbox-inbox.md
docs/adr/0003-authentication-and-authorization.md
docs/adr/0004-local-otel-collector.md
docs/adr/0005-order-state-machine-provider-boundaries.md
docs/adr/0006-versioned-event-contracts.md
docs/architecture/deployment-readiness.md
benchmarks/baseline.md
benchmarks/k6/smoke.js
benchmarks/k6/load.js
benchmarks/k6/stress.js
benchmarks/k6/spike.js
benchmarks/results/README.md
benchmarks/results/2026-05-29-compose-smoke.md
benchmarks/results/2026-05-29-compose-load-stress-spike.md
scripts/run_compose_profile.sh
scripts/run_compose_saga_smoke.sh
scripts/validate_benchmark_budgets.py
scripts/validate_production_readiness.sh
.github/workflows/phase0-quality.yml
"

for file in $required_files; do
  if [ ! -f "$file" ]; then
    echo "missing file: $file" >&2
    exit 1
  fi
done

while IFS= read -r heading; do
  [ -n "$heading" ] || continue
  if ! grep -Fq "## $heading" README.md; then
    echo "missing README section: $heading" >&2
    exit 1
  fi
done <<'EOF'
What is this product?
Problem it solves
Target users
Main features
Architecture overview
Tech stack
Domain model
API documentation
Async or event architecture
Database design
Testing strategy
Performance benchmarks
Observability
Security considerations
Trade-offs and decisions
How to run locally
How to run tests
Failure scenarios
Roadmap
EOF

unformatted=$(gofmt -l cmd internal)
if [ -n "$unformatted" ]; then
  echo "gofmt required for:" >&2
  echo "$unformatted" >&2
  exit 1
fi

go test ./...
go vet ./...
./scripts/validate_production_readiness.sh

if command -v npx >/dev/null 2>&1; then
  npx -y @stoplight/spectral-cli lint openapi.yaml
fi

if ! grep -Fq 'openapi: 3.0.3' openapi.yaml; then
  echo 'openapi.yaml must declare OpenAPI 3.0.3' >&2
  exit 1
fi

if ! grep -Fq '/api/v1/orders:' openapi.yaml; then
  echo 'openapi.yaml must define /api/v1/orders' >&2
  exit 1
fi

for migration in internal/postgres/migrations/*.sql; do
  if ! grep -Fq 'Rollback:' "$migration"; then
    echo "missing rollback note in migration: $migration" >&2
    exit 1
  fi
done

echo "Project validation passed."
