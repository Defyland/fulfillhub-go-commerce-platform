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
