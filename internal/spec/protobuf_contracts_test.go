package spec

import (
	"regexp"
	"strings"
	"testing"
)

type protoContract struct {
	path          string
	packageName   string
	serviceName   string
	rpcs          []string
	requiredTerms []string
}

func TestInternalProtobufContractsAreVersionedAndServiceScoped(t *testing.T) {
	contracts := []protoContract{
		{
			path:        "proto/orders.proto",
			packageName: "fulfillhub.orders.v1",
			serviceName: "OrdersService",
			rpcs:        []string{"CreateOrder", "GetOrder", "CancelOrder"},
			requiredTerms: []string{
				"string merchant_id = 1;",
				"string correlation_id = 2;",
				"string idempotency_key = 3;",
				"ORDER_STATUS_MANUAL_REVIEW",
			},
		},
		{
			path:        "proto/inventory.proto",
			packageName: "fulfillhub.inventory.v1",
			serviceName: "InventoryService",
			rpcs:        []string{"ReserveInventory", "ReleaseInventory", "GetReservation"},
			requiredTerms: []string{
				"string merchant_id = 1;",
				"string correlation_id = 2;",
				"string idempotency_key = 3;",
				"RESERVATION_STATUS_RELEASED",
			},
		},
		{
			path:        "proto/payments.proto",
			packageName: "fulfillhub.payments.v1",
			serviceName: "PaymentsService",
			rpcs:        []string{"AuthorizePayment", "VoidPayment", "GetPaymentAuthorization"},
			requiredTerms: []string{
				"string merchant_id = 1;",
				"string correlation_id = 2;",
				"string idempotency_key = 3;",
				"PAYMENT_STATUS_VOIDED",
			},
		},
		{
			path:        "proto/shipping.proto",
			packageName: "fulfillhub.shipping.v1",
			serviceName: "ShippingService",
			rpcs:        []string{"CreateShipment", "GetShipment", "CancelShipment"},
			requiredTerms: []string{
				"string merchant_id = 1;",
				"string correlation_id = 2;",
				"string idempotency_key = 3;",
				"SHIPMENT_STATUS_FAILED",
			},
		},
		{
			path:        "proto/saga.proto",
			packageName: "fulfillhub.saga.v1",
			serviceName: "SagaService",
			rpcs:        []string{"AdvanceSaga", "CompensateOrder", "GetSagaState", "ReplayDeadLetter"},
			requiredTerms: []string{
				"string message_id = 1;",
				"string event_type = 2;",
				"int32 schema_version = 3;",
				"string causation_id = 9;",
			},
		},
	}

	for _, contract := range contracts {
		t.Run(contract.path, func(t *testing.T) {
			body := readRepoFile(t, contract.path)
			for _, fragment := range []string{
				`syntax = "proto3";`,
				"package " + contract.packageName + ";",
				`option go_package = "github.com/Defyland/fulfillhub-go-commerce-platform/internal/contracts/`,
				"service " + contract.serviceName + " {",
				"reserved 100 to 199;",
			} {
				if !strings.Contains(body, fragment) {
					t.Fatalf("%s missing %q", contract.path, fragment)
				}
			}
			for _, rpc := range contract.rpcs {
				if !strings.Contains(body, "rpc "+rpc+"(") {
					t.Fatalf("%s missing rpc %s", contract.path, rpc)
				}
			}
			for _, term := range contract.requiredTerms {
				if !strings.Contains(body, term) {
					t.Fatalf("%s missing contract term %q", contract.path, term)
				}
			}
			requireEveryMessageHasReservedRange(t, contract.path, body)
		})
	}
}

func TestProtobufEnumsReserveZeroForUnspecified(t *testing.T) {
	enumBlock := regexp.MustCompile(`(?s)enum\s+\w+\s*\{.*?\}`)
	enumValue := regexp.MustCompile(`\w+\s*=\s*0\s*;`)
	for _, path := range []string{
		"proto/orders.proto",
		"proto/inventory.proto",
		"proto/payments.proto",
		"proto/shipping.proto",
		"proto/saga.proto",
	} {
		body := readRepoFile(t, path)
		blocks := enumBlock.FindAllString(body, -1)
		if len(blocks) == 0 {
			t.Fatalf("%s must define at least one enum", path)
		}
		for _, block := range blocks {
			match := enumValue.FindString(block)
			if !strings.Contains(match, "_UNSPECIFIED") {
				t.Fatalf("%s enum must use *_UNSPECIFIED as zero value: %s", path, block)
			}
		}
	}
}

func TestContractDocsCoverGRPCVersioningAndTransportBoundaries(t *testing.T) {
	required := map[string][]string{
		"docs/contracts/grpc-error-mapping.md": {
			"`INVALID_ARGUMENT`",
			"`ALREADY_EXISTS`",
			"`FAILED_PRECONDITION`",
			"`UNAVAILABLE`",
			"`validation_failed`",
			"`duplicate_order`",
			"errors.Is",
			"errors.As",
		},
		"docs/contracts/protobuf-versioning.md": {
			"fulfillhub.<domain>.v1",
			"Never reuse a removed field number",
			"`reserved`",
			"`proto/orders.proto`",
			"yet generate Go stubs or run a gRPC server",
		},
		"docs/contracts/rest-vs-grpc.md": {
			"REST/OpenAPI",
			"Internal Protobuf contracts",
			"Do not expose gRPC as a public merchant API",
			"transactional outbox",
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

func requireEveryMessageHasReservedRange(t testing.TB, path, body string) {
	t.Helper()
	messageCount := len(regexp.MustCompile(`(?m)^message\s+\w+\s*\{`).FindAllString(body, -1))
	reservedCount := strings.Count(body, "reserved 100 to 199;")
	if reservedCount < messageCount {
		t.Fatalf("%s has %d messages but only %d reserved ranges", path, messageCount, reservedCount)
	}
}
