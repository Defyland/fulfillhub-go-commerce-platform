package spec

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestArchitectureDocsDeclarePortsAndAdaptersBoundaries(t *testing.T) {
	required := map[string][]string{
		"docs/architecture/ports-and-adapters.md": {
			"It is not an MVC application with renamed folders",
			"Primary adapters",
			"Use cases",
			"Domain",
			"Ports",
			"Secondary adapters",
			"HTTP request DTOs stay in `internal/api`",
		},
		"docs/architecture/go-architecture.md": {
			"Composition Roots",
			"`cmd/fulfillhub-api`",
			"`internal/commerce`",
			"`internal/fulfillment`",
			"`internal/postgres`",
			"`internal/messaging`",
		},
		"docs/architecture/module-boundaries.md": {
			"Orders",
			"Inventory",
			"Payments",
			"Shipping",
			"Saga",
			"versioned under",
		},
		"docs/architecture/dependency-rule.md": {
			"Allowed Direction",
			"Forbidden Direction",
			"HTTP DTO",
			"SQL rows",
			"RabbitMQ",
		},
		"docs/architecture/testing-strategy.md": {
			"Domain invariant tests",
			"Use-case tests",
			"Adapter integration tests",
			"Contract tests",
		},
		"docs/verification/architecture-evidence.md": {
			"Architecture evidence",
			"HTTP DTO separation",
			"Use-case orchestration",
			"Known gaps",
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

func TestCommercePackageDoesNotDependOnAdapters(t *testing.T) {
	forbidden := []string{
		"github.com/Defyland/fulfillhub-go-commerce-platform/internal/api",
		"github.com/Defyland/fulfillhub-go-commerce-platform/internal/postgres",
		"github.com/Defyland/fulfillhub-go-commerce-platform/internal/messaging",
		"github.com/Defyland/fulfillhub-go-commerce-platform/internal/providers",
		"database/sql",
		"net/http",
		"github.com/jackc/pgx",
		"github.com/rabbitmq/amqp091-go",
		"github.com/redis/go-redis",
	}

	commerceDir := filepath.Join(specRepoRoot(t), "internal", "commerce")
	if err := filepath.WalkDir(commerceDir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		relativePath, err := filepath.Rel(specRepoRoot(t), path)
		if err != nil {
			return err
		}
		for _, fragment := range forbidden {
			if strings.Contains(string(body), fragment) {
				t.Fatalf("%s must not depend on adapter/infra fragment %q", filepath.ToSlash(relativePath), fragment)
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("walk commerce package: %v", err)
	}
}

func TestHTTPDTOsAreMappedBeforeCallingUseCases(t *testing.T) {
	apiServer := readRepoFile(t, "internal/api/server.go")
	requiredFragments := []string{
		"type createOrderRequest struct",
		"func (r createOrderRequest) toCommand() commerce.CreateOrderCommand",
		"var req createOrderRequest",
		"req.toCommand()",
	}
	for _, fragment := range requiredFragments {
		if !strings.Contains(apiServer, fragment) {
			t.Fatalf("internal/api/server.go must contain %q", fragment)
		}
	}

	forbiddenFragments := []string{
		"var req commerce.CreateOrderCommand",
		"var req commerce.CreateOrderRequest",
		"`json:\"external_order_id\"`",
	}
	for _, fragment := range forbiddenFragments[:2] {
		if strings.Contains(apiServer, fragment) {
			t.Fatalf("internal/api/server.go must not decode HTTP directly into commerce command/request type %q", fragment)
		}
	}
	if !strings.Contains(apiServer, forbiddenFragments[2]) {
		t.Fatalf("internal/api/server.go should own HTTP JSON request tags")
	}
}

func TestCommerceCommandsDoNotCarryHTTPSerializationTags(t *testing.T) {
	model := readRepoFile(t, "internal/commerce/model.go")
	for _, typeName := range []string{
		"CreateOrderCommand",
		"OrderItemInput",
		"PaymentMethodInput",
		"CustomerInput",
		"AddressInput",
	} {
		block := structBlock(t, model, typeName)
		if strings.Contains(block, "`json:") {
			t.Fatalf("commerce.%s must not carry HTTP JSON tags; map adapter DTOs before calling the use case", typeName)
		}
	}
}

func TestPortsAreSmallAndDeclaredWhereUseCasesConsumeThem(t *testing.T) {
	required := map[string][]string{
		"internal/commerce/store.go": {
			"type Store interface",
			"InsertOrder(ctx context.Context",
			"GetOrder(ctx context.Context",
			"UpdateOrderStatus(ctx context.Context",
		},
		"internal/fulfillment/handlers.go": {
			"type Projector interface",
			"type InventoryReserver interface",
			"type PaymentAuthorizer interface",
			"type ShipmentCreator interface",
			"type Dependencies struct",
			"func HandlerForQueue(queue string, deps Dependencies)",
		},
		"internal/postgres/store.go": {
			"type Store struct",
			"func (s *Store) InsertOrder",
			"internal/commerce",
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

func structBlock(t testing.TB, body, typeName string) string {
	t.Helper()
	needle := "type " + typeName + " struct {"
	start := strings.Index(body, needle)
	if start == -1 {
		t.Fatalf("missing %s", needle)
	}
	end := strings.Index(body[start:], "\n}")
	if end == -1 {
		t.Fatalf("missing closing brace for %s", typeName)
	}
	return body[start : start+end+2]
}

func specRepoRoot(t testing.TB) string {
	t.Helper()
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve caller path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(currentFile), "..", ".."))
}
