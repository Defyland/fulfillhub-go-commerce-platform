package spec

import (
	"strings"
	"testing"
)

func TestDeploymentDocsExplainWhyRailwayIsIntentionallyOmitted(t *testing.T) {
	required := map[string][]string{
		"README.md": {
			"FulfillHub intentionally does not ship a Railway demo.",
			"requires the migration job, public API, outbox\nrelay, queue-specific workers, PostgreSQL, RabbitMQ, Redis",
			"Docker Compose for full local saga evidence",
			"`deployments/kubernetes/base` for production-like shape",
		},
		"docs/architecture/deployment-readiness.md": {
			"Railway omission is intentional",
			"A truthful runnable\nslice needs the `fulfillhub-migrate` release step, the public API, the outbox\nrelay, queue-specific worker processes, PostgreSQL, RabbitMQ, Redis",
			"Docker Compose for local end-to-end evidence",
			"Kubernetes blueprint for production-like topology",
		},
		"docker-compose.yml": {
			"outbox-relay:",
			"inventory-worker:",
			"payments-worker:",
			"shipments-worker:",
			"rabbitmq:",
			"redis:",
		},
		"deployments/kubernetes/base/job-migrate.yaml": {
			"kind: Job",
			"/app/fulfillhub-migrate",
		},
		"deployments/kubernetes/base/deployment-workers.yaml": {
			"inventory.reserve",
			"payments.authorize",
			"shipments.create",
		},
	}

	for path, fragments := range required {
		body := normalizeWhitespace(readRepoFile(t, path))
		for _, fragment := range fragments {
			if !strings.Contains(body, normalizeWhitespace(fragment)) {
				t.Fatalf("%s must contain %q", path, fragment)
			}
		}
	}
}

func normalizeWhitespace(value string) string {
	return strings.Join(strings.Fields(value), " ")
}
