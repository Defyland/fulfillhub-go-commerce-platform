package spec

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestSagaEventContractsAreVersionedAndRuntimeAligned(t *testing.T) {
	contracts := map[string]string{
		"order.created":      "docs/events/order.created.v1.json",
		"inventory.reserved": "docs/events/inventory.reserved.v1.json",
		"payment.authorized": "docs/events/payment.authorized.v1.json",
		"shipment.created":   "docs/events/shipment.created.v1.json",
		"order.completed":    "docs/events/order.completed.v1.json",
	}
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

	for eventType, path := range contracts {
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
