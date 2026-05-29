#!/usr/bin/env sh

set -eu

REPO_ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$REPO_ROOT"

required_dirs="
cmd
cmd/fulfillhub-api
cmd/fulfillhub-dlq-replay
cmd/fulfillhub-outbox-relay
cmd/fulfillhub-worker
deployments
deployments/prometheus
internal
internal/api
internal/commerce
internal/messaging
internal/postgres
internal/providers
docs/adr
docs/api
docs/architecture
docs/benchmarks
docs/diagrams
docs/events
docs/observability
docs/runbooks
benchmarks
benchmarks/k6
benchmarks/results
scripts
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
cmd/fulfillhub-outbox-relay/main.go
cmd/fulfillhub-worker/main.go
deployments/prometheus/prometheus.yml
internal/api/server.go
internal/api/server_test.go
internal/commerce/model.go
internal/commerce/service.go
internal/commerce/service_test.go
internal/commerce/store.go
internal/fulfillment/handlers.go
internal/fulfillment/handlers_test.go
internal/messaging/inbox.go
internal/messaging/dlq.go
internal/messaging/rabbitmq.go
internal/messaging/rabbitmq_integration_test.go
internal/messaging/relay.go
internal/messaging/relay_test.go
internal/messaging/topology.go
internal/postgres/migrations.go
internal/postgres/migrations_test.go
internal/postgres/store.go
internal/postgres/migrations/001_init.sql
internal/postgres/migrations/002_audit_details.sql
internal/postgres/migrations/003_fulfillment_projections.sql
internal/postgres/migrations/004_notification_events.sql
internal/postgres/migrations/005_compensation_events.sql
internal/providers/payment.go
internal/providers/providers_test.go
internal/providers/shipment.go
docs/engineering-baseline.md
docs/api/request-response-examples.md
docs/api/error-format.md
docs/architecture/overview.md
docs/architecture/domain-model.md
docs/architecture/database-design.md
docs/benchmarks/methodology.md
docs/benchmarks/results-status.md
docs/diagrams/system-context.md
docs/diagrams/order-saga-sequence.md
docs/events/catalog.md
docs/observability/grafana-dashboard.json
docs/runbooks/incident-response.md
docs/adr/0001-modular-monolith-first.md
docs/adr/0002-rabbitmq-outbox-inbox.md
docs/adr/0003-authentication-and-authorization.md
benchmarks/baseline.md
benchmarks/k6/smoke.js
benchmarks/k6/load.js
benchmarks/k6/stress.js
benchmarks/k6/spike.js
benchmarks/results/README.md
benchmarks/results/2026-05-29-compose-smoke.md
scripts/run_compose_profile.sh
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

echo "Project validation passed."
