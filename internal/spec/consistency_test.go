package spec

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Defyland/fulfillhub-go-commerce-platform/internal/commerce"
)

func TestOpenAPIEnumeratesRuntimeOrderStatuses(t *testing.T) {
	openAPI := readRepoFile(t, "openapi.yaml")
	for _, status := range []commerce.OrderStatus{
		commerce.StatusPendingFulfillment,
		commerce.StatusInventoryReserved,
		commerce.StatusPaymentAuthorized,
		commerce.StatusShipmentCreated,
		commerce.StatusCancellationPending,
		commerce.StatusManualReview,
		commerce.StatusCancelled,
		commerce.StatusCompleted,
		commerce.StatusFailed,
	} {
		if !strings.Contains(openAPI, string(status)) {
			t.Fatalf("openapi.yaml must document runtime order status %q", status)
		}
	}
}

func TestManualReviewFlowRemainsConsistentAcrossDocsAndRuntime(t *testing.T) {
	requiredFragments := map[string][]string{
		"docs/architecture/domain-model.md":         {string(commerce.StatusManualReview)},
		"docs/architecture/database-design.md":      {"order.manual_review_required", string(commerce.StatusManualReview)},
		"docs/events/catalog.md":                    {"order.manual_review_required", string(commerce.StatusManualReview)},
		"docs/runbooks/incident-response.md":        {string(commerce.StatusManualReview)},
		"internal/fulfillment/handlers.go":          {"order.manual_review_required", string(commerce.StatusManualReview)},
		"internal/messaging/topology.go":            {"order.manual_review_required"},
		"docs/observability/grafana-dashboard.json": {"orders.cancel"},
		".github/workflows/phase0-quality.yml":      {"govulncheck@v1.3.0 ./..."},
	}

	for path, fragments := range requiredFragments {
		body := readRepoFile(t, path)
		for _, fragment := range fragments {
			if !strings.Contains(body, fragment) {
				t.Fatalf("%s must contain %q", path, fragment)
			}
		}
	}

	runbook := readRepoFile(t, "docs/runbooks/incident-response.md")
	if strings.Contains(runbook, "`cancellation_pending` for manual review") {
		t.Fatal("incident runbook still describes manual review with the stale cancellation_pending status")
	}
}

func readRepoFile(t testing.TB, relativePath string) string {
	t.Helper()
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve caller path")
	}
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(currentFile), "..", ".."))
	body, err := os.ReadFile(filepath.Join(repoRoot, relativePath))
	if err != nil {
		t.Fatalf("read %s: %v", relativePath, err)
	}
	return string(body)
}
