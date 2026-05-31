package spec

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Defyland/fulfillhub-go-commerce-platform/internal/commerce"
	"github.com/Defyland/fulfillhub-go-commerce-platform/internal/fulfillment"
	"github.com/Defyland/fulfillhub-go-commerce-platform/internal/messaging"
)

var sagaEventContracts = map[string]string{
	"order.created":                "docs/events/order.created.v1.json",
	"inventory.reserved":           "docs/events/inventory.reserved.v1.json",
	"inventory.rejected":           "docs/events/inventory.rejected.v1.json",
	"payment.authorized":           "docs/events/payment.authorized.v1.json",
	"payment.failed":               "docs/events/payment.failed.v1.json",
	"shipment.created":             "docs/events/shipment.created.v1.json",
	"shipment.failed":              "docs/events/shipment.failed.v1.json",
	"order.cancel_requested":       "docs/events/order.cancel_requested.v1.json",
	"order.completed":              "docs/events/order.completed.v1.json",
	"order.cancelled":              "docs/events/order.cancelled.v1.json",
	"order.manual_review_required": "docs/events/order.manual_review_required.v1.json",
}

func TestSagaEventContractsAreVersionedAndRuntimeAligned(t *testing.T) {
	requiredEnvelopeFields := []string{
		"message_id",
		"event_type",
		"schema_version",
		"occurred_at",
		"producer",
		"merchant_id",
		"order_id",
		"correlation_id",
		"causation_id",
		"payload",
	}

	for eventType, path := range sagaEventContracts {
		schema := readJSONSchema(t, path)
		if got := schemaString(t, schema, "title"); !strings.Contains(got, eventType+" v1") {
			t.Fatalf("%s title = %q, want %q", path, got, eventType+" v1")
		}
		requireContainsAll(t, schemaRequired(t, schema), requiredEnvelopeFields, path+" required fields")
		properties := schemaMap(t, schema, "properties")
		if _, exists := properties["event_id"]; exists {
			t.Fatalf("%s must use runtime-aligned message_id, not event_id", path)
		}
		eventTypeSchema := asMap(t, properties["event_type"], path+".properties.event_type")
		if got := schemaString(t, eventTypeSchema, "const"); got != eventType {
			t.Fatalf("%s event_type const = %q, want %q", path, got, eventType)
		}
		versionSchema := asMap(t, properties["schema_version"], path+".properties.schema_version")
		if got := schemaNumber(t, versionSchema, "const"); got != 1 {
			t.Fatalf("%s schema_version const = %.0f, want 1", path, got)
		}
		payload := asMap(t, properties["payload"], path+".properties.payload")
		if len(schemaRequired(t, payload)) == 0 {
			t.Fatalf("%s payload must define required business fields", path)
		}
	}
}

func TestRuntimeSagaEventsMatchVersionedContracts(t *testing.T) {
	events := runtimeSagaEvents(t)
	if len(events) != len(sagaEventContracts) {
		t.Fatalf("runtime events = %d, want %d core contract events", len(events), len(sagaEventContracts))
	}

	seen := make(map[string]bool, len(events))
	for _, event := range events {
		path, ok := sagaEventContracts[event.EventType]
		if !ok {
			t.Fatalf("runtime event %s has no documented v1 schema", event.EventType)
		}
		seen[event.EventType] = true
		assertRuntimeEventMatchesSchema(t, readJSONSchema(t, path), event)
	}
	for eventType := range sagaEventContracts {
		if !seen[eventType] {
			t.Fatalf("runtime scenarios did not produce documented event %s", eventType)
		}
	}
}

func TestSagaEventDocumentationProtectsOperationalInvariants(t *testing.T) {
	required := map[string][]string{
		"docs/events/README.md": {
			"Envelope v1",
			"`message_id`",
			"Compatibility Policy",
			"Outbox and Inbox Contract",
			"Retry and DLQ Contract",
			"Ordering and Partitioning",
			"merchant_id + \":\" + order_id",
		},
		"docs/events/threat-model.md": {
			"Duplicate Delivery",
			"Payment Side Effects",
			"Inventory Correctness",
			"Tenant Isolation",
			"Replay Abuse",
			"Ordering Violations",
		},
		"docs/events/catalog.md": {
			"Versioned event envelope",
			"schema_version",
			"producer",
			"payload",
		},
		"docs/adr/0006-versioned-event-contracts.md": {
			"message_id",
			"schema registry is deferred",
		},
		"docs/runbooks/event-contract-breaking-change.md": {
			"message_id",
			"schema_version",
			"preserves the original `message_id`",
		},
		"README.md": {
			"docs/events/README.md",
			"docs/events/threat-model.md",
		},
		"docs/security/threat-model.md": {
			"docs/events/README.md",
			"docs/events/threat-model.md",
		},
		"scripts/validate_phase0.sh": {
			"docs/events/order.created.v1.json",
			"docs/events/threat-model.md",
			"docs/adr/0006-versioned-event-contracts.md",
			"docs/runbooks/event-contract-breaking-change.md",
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

func runtimeSagaEvents(t testing.TB) []commerce.OutboxEvent {
	t.Helper()
	eventsByType := map[string]commerce.OutboxEvent{}
	capture := func(event commerce.OutboxEvent) {
		eventsByType[event.EventType] = event
	}

	happyStore, happyService, created := newContractOrder(t, "idem-contract-0001")
	capture(created)
	happyDeps := contractDeps(happyStore)
	runContractHandler(t, messaging.InventoryReserveQueue, happyDeps, created)
	inventoryReserved := lastRuntimeEvent(t, happyService)
	capture(inventoryReserved)
	runContractHandler(t, messaging.PaymentsAuthorizeQueue, happyDeps, inventoryReserved)
	paymentAuthorized := lastRuntimeEvent(t, happyService)
	capture(paymentAuthorized)
	runContractHandler(t, messaging.ShipmentsCreateQueue, happyDeps, paymentAuthorized)
	shipmentCreated := lastRuntimeEvent(t, happyService)
	capture(shipmentCreated)
	runContractHandler(t, messaging.OrdersFinalizeQueue, happyDeps, shipmentCreated)
	capture(lastRuntimeEvent(t, happyService))

	cancelStore, cancelService, _ := newContractOrder(t, "idem-contract-0002")
	cancelled := cancelThroughRuntime(t, cancelStore, cancelService, "customer_requested")
	capture(cancelled.cancelRequested)
	capture(cancelled.finalEvent)

	manualStore, manualService, manualCreated := newContractOrder(t, "idem-contract-0003")
	manualDeps := contractDeps(manualStore)
	runContractHandler(t, messaging.InventoryReserveQueue, manualDeps, manualCreated)
	runContractHandler(t, messaging.PaymentsAuthorizeQueue, manualDeps, lastRuntimeEvent(t, manualService))
	runContractHandler(t, messaging.ShipmentsCreateQueue, manualDeps, lastRuntimeEvent(t, manualService))
	manualReview := cancelThroughRuntime(t, manualStore, manualService, "customer_requested")
	capture(manualReview.finalEvent)

	inventoryFailureStore, inventoryFailureService, inventoryFailureCreated := newContractOrder(t, "idem-contract-0004")
	inventoryFailureDeps := contractDeps(inventoryFailureStore)
	inventoryFailureDeps.InventoryReserver = fulfillment.InventoryReserverFunc(func(context.Context, commerce.OutboxEvent) error {
		return errors.New("stock exhausted")
	})
	runContractHandler(t, messaging.InventoryReserveQueue, inventoryFailureDeps, inventoryFailureCreated)
	capture(lastRuntimeEvent(t, inventoryFailureService))

	paymentFailureStore, paymentFailureService, paymentFailureCreated := newContractOrder(t, "idem-contract-0005")
	paymentFailureDeps := contractDeps(paymentFailureStore)
	runContractHandler(t, messaging.InventoryReserveQueue, paymentFailureDeps, paymentFailureCreated)
	paymentFailureDeps.PaymentAuthorizer = fulfillment.PaymentAuthorizerFunc(func(context.Context, commerce.OutboxEvent) (commerce.Payment, error) {
		return commerce.Payment{}, errors.New("provider timeout")
	})
	runContractHandler(t, messaging.PaymentsAuthorizeQueue, paymentFailureDeps, lastRuntimeEvent(t, paymentFailureService))
	capture(lastRuntimeEvent(t, paymentFailureService))

	shipmentFailureStore, shipmentFailureService, shipmentFailureCreated := newContractOrder(t, "idem-contract-0006")
	shipmentFailureDeps := contractDeps(shipmentFailureStore)
	runContractHandler(t, messaging.InventoryReserveQueue, shipmentFailureDeps, shipmentFailureCreated)
	runContractHandler(t, messaging.PaymentsAuthorizeQueue, shipmentFailureDeps, lastRuntimeEvent(t, shipmentFailureService))
	shipmentFailureDeps.ShipmentCreator = fulfillment.ShipmentCreatorFunc(func(context.Context, commerce.OutboxEvent) (commerce.Shipment, error) {
		return commerce.Shipment{}, errors.New("carrier unavailable")
	})
	runContractHandler(t, messaging.ShipmentsCreateQueue, shipmentFailureDeps, lastRuntimeEvent(t, shipmentFailureService))
	capture(lastRuntimeEvent(t, shipmentFailureService))

	events := make([]commerce.OutboxEvent, 0, len(sagaEventContracts))
	for eventType := range sagaEventContracts {
		event, ok := eventsByType[eventType]
		if !ok {
			continue
		}
		events = append(events, event)
	}
	return events
}

type cancellationResult struct {
	cancelRequested commerce.OutboxEvent
	finalEvent      commerce.OutboxEvent
}

func cancelThroughRuntime(t testing.TB, store *commerce.MemoryStore, service *commerce.Service, reason string) cancellationResult {
	t.Helper()
	orderID := service.OutboxEvents()[0].OrderID
	if _, err := service.CancelOrder(orderID, "cor_contract_cancel", commerce.AuditActor{
		Type:   "merchant_user",
		ID:     "usr_contract",
		Reason: reason,
	}); err != nil {
		t.Fatalf("cancel order: %v", err)
	}
	cancelRequested := lastRuntimeEvent(t, service)
	runContractHandler(t, messaging.OrdersCancelQueue, contractDeps(store), cancelRequested)
	return cancellationResult{
		cancelRequested: cancelRequested,
		finalEvent:      lastRuntimeEvent(t, service),
	}
}

func newContractOrder(t testing.TB, idempotencyKey string) (*commerce.MemoryStore, *commerce.Service, commerce.OutboxEvent) {
	t.Helper()
	store := commerce.NewMemoryStore()
	service := commerce.NewService(store)
	if _, _, err := service.CreateOrder("mer_contract", idempotencyKey, "cor_contract", contractOrderRequest()); err != nil {
		t.Fatalf("create order: %v", err)
	}
	return store, service, service.OutboxEvents()[0]
}

func contractDeps(store *commerce.MemoryStore) fulfillment.Dependencies {
	var counter int
	now := time.Date(2026, 5, 29, 14, 15, 0, 0, time.UTC)
	return fulfillment.Dependencies{
		Projector: store,
		Orders:    store,
		Clock:     func() time.Time { return now },
		NewID: func(prefix string) string {
			counter++
			return fmt.Sprintf("%s_contract_%02d", prefix, counter)
		},
	}
}

func runContractHandler(t testing.TB, queue string, deps fulfillment.Dependencies, event commerce.OutboxEvent) {
	t.Helper()
	handler, err := fulfillment.HandlerForQueue(queue, deps)
	if err != nil {
		t.Fatalf("handler for %s: %v", queue, err)
	}
	if err := handler.HandleEvent(context.Background(), event); err != nil {
		t.Fatalf("%s handling %s: %v", queue, event.EventType, err)
	}
}

func lastRuntimeEvent(t testing.TB, service *commerce.Service) commerce.OutboxEvent {
	t.Helper()
	events := service.OutboxEvents()
	if len(events) == 0 {
		t.Fatal("service produced no outbox events")
	}
	return events[len(events)-1]
}

func contractOrderRequest() commerce.CreateOrderRequest {
	return commerce.CreateOrderRequest{
		ExternalOrderID: "web-contract-1",
		Currency:        "USD",
		Customer: commerce.Customer{
			Email: "samira@example.com",
		},
		ShippingAddress: commerce.Address{
			Line1:      "55 Market Street",
			City:       "San Francisco",
			State:      "CA",
			PostalCode: "94105",
			Country:    "US",
		},
		Items: []commerce.OrderItemRequest{{
			SKU:      "SKU-CHAIR-BLK",
			Quantity: 1,
			UnitPrice: commerce.Money{
				Amount:   18900,
				Currency: "USD",
			},
		}},
		PaymentMethod: commerce.PaymentMethod{
			Provider:     "stripe",
			PaymentToken: "tok_visa_01hzsample",
		},
	}
}

func assertRuntimeEventMatchesSchema(t testing.TB, schema map[string]any, event commerce.OutboxEvent) {
	t.Helper()
	raw, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal runtime %s event: %v", event.EventType, err)
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("decode runtime %s event: %v", event.EventType, err)
	}
	assertObjectMatchesSchema(t, schema, body, event.EventType)
	if got := body["producer"]; got != commerce.ProducerForEventType(event.EventType) {
		t.Fatalf("%s producer = %v, want %s", event.EventType, got, commerce.ProducerForEventType(event.EventType))
	}
}

func assertObjectMatchesSchema(t testing.TB, schema map[string]any, object map[string]any, label string) {
	t.Helper()
	properties := schemaMap(t, schema, "properties")
	for _, field := range schemaRequired(t, schema) {
		value, ok := object[field]
		if !ok {
			t.Fatalf("%s missing required field %s: %+v", label, field, object)
		}
		fieldSchema := asMap(t, properties[field], label+"."+field+".schema")
		assertValueMatchesSchema(t, fieldSchema, value, label+"."+field)
	}
}

func assertValueMatchesSchema(t testing.TB, schema map[string]any, value any, label string) {
	t.Helper()
	if expected, ok := schema["const"]; ok && !reflect.DeepEqual(value, expected) {
		t.Fatalf("%s = %v, want const %v", label, value, expected)
	}
	switch schema["type"] {
	case "string":
		got, ok := value.(string)
		if !ok {
			t.Fatalf("%s = %T, want string", label, value)
		}
		if minimum := schemaMinimum(t, schema, "minLength"); minimum > 0 && len(got) < int(minimum) {
			t.Fatalf("%s length = %d, want at least %.0f", label, len(got), minimum)
		}
		if schema["format"] == "date-time" {
			if _, err := time.Parse(time.RFC3339Nano, got); err != nil {
				t.Fatalf("%s = %q, want RFC3339 date-time: %v", label, got, err)
			}
		}
	case "integer":
		got, ok := value.(float64)
		if !ok || math.Trunc(got) != got {
			t.Fatalf("%s = %v, want integer", label, value)
		}
		if minimum := schemaMinimum(t, schema, "minimum"); got < minimum {
			t.Fatalf("%s = %.0f, want at least %.0f", label, got, minimum)
		}
	case "object":
		got, ok := value.(map[string]any)
		if !ok {
			t.Fatalf("%s = %T, want object", label, value)
		}
		assertObjectMatchesSchema(t, schema, got, label)
	case "array":
		got, ok := value.([]any)
		if !ok {
			t.Fatalf("%s = %T, want array", label, value)
		}
		if minimum := schemaMinimum(t, schema, "minItems"); minimum > 0 && len(got) < int(minimum) {
			t.Fatalf("%s length = %d, want at least %.0f", label, len(got), minimum)
		}
		itemSchema, hasItemSchema := schema["items"].(map[string]any)
		if hasItemSchema {
			for idx, item := range got {
				assertValueMatchesSchema(t, itemSchema, item, fmt.Sprintf("%s[%d]", label, idx))
			}
		}
	}
}

func schemaMinimum(t testing.TB, schema map[string]any, key string) float64 {
	t.Helper()
	value, ok := schema[key]
	if !ok {
		return 0
	}
	minimum, ok := value.(float64)
	if !ok {
		t.Fatalf("%s must be a number", key)
	}
	return minimum
}

func readJSONSchema(t testing.TB, path string) map[string]any {
	t.Helper()
	var schema map[string]any
	if err := json.Unmarshal([]byte(readRepoFile(t, path)), &schema); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return schema
}

func schemaMap(t testing.TB, schema map[string]any, key string) map[string]any {
	t.Helper()
	return asMap(t, schema[key], key)
}

func asMap(t testing.TB, value any, label string) map[string]any {
	t.Helper()
	got, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("%s must be an object", label)
	}
	return got
}

func schemaString(t testing.TB, schema map[string]any, key string) string {
	t.Helper()
	value, ok := schema[key].(string)
	if !ok {
		t.Fatalf("%s must be a string", key)
	}
	return value
}

func schemaNumber(t testing.TB, schema map[string]any, key string) float64 {
	t.Helper()
	value, ok := schema[key].(float64)
	if !ok {
		t.Fatalf("%s must be a number", key)
	}
	return value
}

func schemaRequired(t testing.TB, schema map[string]any) []string {
	t.Helper()
	raw, ok := schema["required"].([]any)
	if !ok {
		t.Fatalf("schema required must be an array")
	}
	required := make([]string, 0, len(raw))
	for _, value := range raw {
		field, ok := value.(string)
		if !ok {
			t.Fatalf("required field %v must be a string", value)
		}
		required = append(required, field)
	}
	return required
}

func requireContainsAll(t testing.TB, got []string, want []string, label string) {
	t.Helper()
	present := make(map[string]bool, len(got))
	for _, value := range got {
		present[value] = true
	}
	for _, value := range want {
		if !present[value] {
			t.Fatalf("%s missing %s; got %s", label, value, fmt.Sprint(got))
		}
	}
}
