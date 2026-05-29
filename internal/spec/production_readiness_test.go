package spec

import (
	"io"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestProductionDeploymentBlueprintDefinesReleaseSafetyControls(t *testing.T) {
	required := map[string][]string{
		"cmd/fulfillhub-migrate/main.go": {
			"postgres.RunMigrations",
			"MIGRATION_TIMEOUT",
		},
		"Dockerfile": {
			"/app/fulfillhub-migrate",
			"/app/fulfillhub-api",
			"/app/fulfillhub-worker",
		},
		"deployments/kubernetes/base/job-migrate.yaml": {
			"kind: Job",
			"/app/fulfillhub-migrate",
			"argocd.argoproj.io/hook: PreSync",
			"readOnlyRootFilesystem: true",
		},
		"deployments/kubernetes/base/deployment-api.yaml": {
			"kind: Deployment",
			"maxUnavailable: 0",
			"livenessProbe:",
			"readinessProbe:",
			"readOnlyRootFilesystem: true",
		},
		"deployments/kubernetes/base/deployment-workers.yaml": {
			"inventory.reserve",
			"payments.authorize",
			"shipments.create",
			"orders.finalize",
			"orders.cancel",
			"orders.compensate",
			"notifications.email",
		},
		"deployments/kubernetes/base/external-secret.yaml": {
			"kind: ExternalSecret",
			"DATABASE_URL",
			"RABBITMQ_URL",
			"OPS_JWT_SECRET",
			"PAYMENT_WEBHOOK_SECRET",
			"SHIPMENT_WEBHOOK_SECRET",
		},
		"deployments/kubernetes/base/hpa-api.yaml": {
			"kind: HorizontalPodAutoscaler",
			"averageUtilization: 65",
		},
		"deployments/kubernetes/base/poddisruptionbudget-api.yaml": {
			"kind: PodDisruptionBudget",
			"minAvailable: 2",
		},
		"deployments/kubernetes/base/networkpolicy.yaml": {
			"kind: NetworkPolicy",
			"policyTypes:",
			"Egress",
		},
	}

	for path, fragments := range required {
		body := readRepoFile(t, path)
		for _, fragment := range fragments {
			if !strings.Contains(body, fragment) {
				t.Fatalf("%s must contain %q", path, fragment)
			}
		}
		if strings.HasSuffix(path, ".yaml") {
			assertValidYAMLDocuments(t, path, body)
		}
	}
}

func TestProductionReadinessDocsDeclareBoundariesAndGates(t *testing.T) {
	required := map[string][]string{
		"docs/production-readiness.md": {
			"Release gates",
			"Deployment model",
			"Migration policy",
			"Rollback policy",
			"Alert handling",
			"Data recovery",
			"Secret rotation",
			"Release integrity",
			"Provider hardening",
			"Production gap log",
			"fulfillhub-migrate",
			"ExternalSecret",
			"SBOM",
			"PITR",
		},
		"docs/runbooks/deployment-rollback.md": {
			"Pre-deploy checklist",
			"Deployment flow",
			"Rollback triggers",
			"Rollback flow",
			"Migration rollback rules",
		},
		"docs/runbooks/slo-alert-response.md": {
			"Service objectives",
			"FulfillHubOutboxStalled",
			"FulfillHubDLQBacklog",
			"FulfillHubQueueWithoutConsumers",
			"FulfillHubOrderFailureRatioHigh",
		},
		"docs/runbooks/data-protection.md": {
			"Backup policy",
			"Restore drill",
			"Retention policy",
			"Purge and privacy requests",
			"Backward-compatible migrations",
		},
		"docs/security/secrets-management.md": {
			"Secret sources",
			"Rotation policy",
			"PAYMENT_WEBHOOK_SECRET",
			"SHIPMENT_WEBHOOK_SECRET",
		},
		"docs/security/supply-chain.md": {
			"Current automated controls",
			"Release artifact policy",
			"Cosign",
			"SLSA",
		},
		"README.md": {
			"go run ./cmd/fulfillhub-migrate",
			"docs/production-readiness.md",
			"deployments/kubernetes/base",
		},
	}

	for path, fragments := range required {
		body := readRepoFile(t, path)
		for _, fragment := range fragments {
			if !strings.Contains(body, fragment) {
				t.Fatalf("%s must contain %q", path, fragment)
			}
		}
	}
}

func TestPrometheusAlertRulesDeclareActionableRunbooks(t *testing.T) {
	alertRules := readRepoFile(t, "deployments/prometheus/rules/fulfillhub-alerts.yml")
	assertYAMLParses(t, "deployments/prometheus/rules/fulfillhub-alerts.yml", alertRules)

	alerts := []string{
		"FulfillHubAPIDown",
		"FulfillHubRuntimeMetricsUnavailable",
		"FulfillHubOutboxStalled",
		"FulfillHubDLQBacklog",
		"FulfillHubQueueWithoutConsumers",
		"FulfillHubManualReviewBacklog",
		"FulfillHubOrderFailureRatioHigh",
	}
	for _, alert := range alerts {
		if !strings.Contains(alertRules, "alert: "+alert) {
			t.Fatalf("alert rules must contain %s", alert)
		}
	}
	if strings.Count(alertRules, "runbook_url:") < len(alerts) {
		t.Fatalf("each alert must declare a runbook_url")
	}
	for _, fragment := range []string{
		"severity: page",
		"severity: ticket",
		"fulfillhub_outbox_oldest_unpublished_age_seconds",
		"fulfillhub_rabbitmq_queue_messages_ready",
		`fulfillhub_orders_total{status="manual_review"}`,
		`fulfillhub_orders_total{status="failed"}`,
	} {
		if !strings.Contains(alertRules, fragment) {
			t.Fatalf("alert rules must contain %q", fragment)
		}
	}

	prometheus := readRepoFile(t, "deployments/prometheus/prometheus.yml")
	if !strings.Contains(prometheus, "rule_files:") || !strings.Contains(prometheus, "/etc/prometheus/rules/*.yml") {
		t.Fatal("prometheus.yml must load production alert rules")
	}
	compose := readRepoFile(t, "docker-compose.yml")
	if !strings.Contains(compose, "./deployments/prometheus/rules:/etc/prometheus/rules:ro") {
		t.Fatal("docker-compose.yml must mount Prometheus alert rules")
	}
}

func TestProductionReadinessValidationRunsInCI(t *testing.T) {
	required := map[string][]string{
		"scripts/validate_production_readiness.sh": {
			"FulfillHubOutboxStalled",
			"runbook_url:",
			"go test ./internal/spec",
			"Kubernetes production blueprint must not contain literal local credentials",
		},
		"scripts/validate_phase0.sh": {
			"./scripts/validate_production_readiness.sh",
			"deployments/prometheus/rules/fulfillhub-alerts.yml",
			"docs/security/secrets-management.md",
			"docs/runbooks/data-protection.md",
		},
		".github/workflows/phase0-quality.yml": {
			"production-readiness",
			"Validate production readiness pack",
			"./scripts/validate_production_readiness.sh",
		},
	}

	for path, fragments := range required {
		body := readRepoFile(t, path)
		for _, fragment := range fragments {
			if !strings.Contains(body, fragment) {
				t.Fatalf("%s must contain %q", path, fragment)
			}
		}
	}
}

func assertValidYAMLDocuments(t testing.TB, path, body string) {
	t.Helper()
	decoder := yaml.NewDecoder(strings.NewReader(body))
	decoded := 0
	for {
		var doc map[string]any
		err := decoder.Decode(&doc)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("decode %s: %v", path, err)
		}
		if len(doc) == 0 {
			continue
		}
		decoded++
		if _, ok := doc["apiVersion"].(string); !ok {
			t.Fatalf("%s document %d must declare apiVersion", path, decoded)
		}
		if _, ok := doc["kind"].(string); !ok {
			t.Fatalf("%s document %d must declare kind", path, decoded)
		}
		metadata, ok := doc["metadata"].(map[string]any)
		if !ok {
			t.Fatalf("%s document %d must declare metadata", path, decoded)
		}
		if _, ok := metadata["name"].(string); !ok {
			t.Fatalf("%s document %d must declare metadata.name", path, decoded)
		}
	}
	if decoded == 0 {
		t.Fatalf("%s must contain at least one YAML document", path)
	}
}

func assertYAMLParses(t testing.TB, path, body string) {
	t.Helper()
	var value any
	if err := yaml.Unmarshal([]byte(body), &value); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
}
