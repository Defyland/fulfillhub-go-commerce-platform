package postgres

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Defyland/fulfillhub-go-commerce-platform/internal/commerce"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace/noop"
)

func TestMigrationsIncludeConsistencyTables(t *testing.T) {
	body, err := migrationsFS.ReadFile("migrations/001_init.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	sql := string(body)
	for _, table := range []string{"orders", "order_items", "idempotency_keys", "outbox_events", "inbox_messages", "audit_logs"} {
		if !strings.Contains(sql, "CREATE TABLE IF NOT EXISTS "+table) {
			t.Fatalf("migration does not create %s", table)
		}
	}
}

func TestMigrationsAddAuditDetails(t *testing.T) {
	body, err := migrationsFS.ReadFile("migrations/002_audit_details.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	sql := string(body)
	for _, fragment := range []string{"ALTER TABLE audit_logs", "details JSONB"} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("audit details migration does not include %q", fragment)
		}
	}
}

func TestPostgresStoreIntegration(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	otel.SetTracerProvider(provider)
	t.Cleanup(func() {
		otel.SetTracerProvider(noop.NewTracerProvider())
		if err := provider.Shutdown(context.Background()); err != nil {
			t.Fatalf("shutdown tracer provider: %v", err)
		}
	})

	store, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	defer store.Close()

	if err := RunMigrations(ctx, store.DB()); err != nil {
		t.Fatalf("run migrations: %v", err)
	}

	now := time.Now().UTC()
	order := &commerce.Order{
		OrderID:         "ord_pg_test_" + strings.ReplaceAll(t.Name(), "/", "_"),
		MerchantID:      "mer_pg_test",
		ExternalOrderID: "external_pg_test_" + strings.ReplaceAll(t.Name(), "/", "_"),
		Status:          commerce.StatusPendingFulfillment,
		Currency:        "USD",
		Totals: commerce.OrderTotals{
			Subtotal: commerce.Money{Amount: 18900, Currency: "USD"},
			Shipping: commerce.Money{Amount: 1200, Currency: "USD"},
			Total:    commerce.Money{Amount: 20100, Currency: "USD"},
		},
		Items: []commerce.OrderItem{
			{
				SKU:               "SKU-CHAIR-BLK",
				Quantity:          1,
				UnitPrice:         commerce.Money{Amount: 18900, Currency: "USD"},
				ReservationStatus: "pending",
			},
		},
		Payment:   &commerce.Payment{Provider: "stripe", Status: "pending_authorization"},
		CreatedAt: now,
		UpdatedAt: now,
	}
	event := commerce.OutboxEvent{
		MessageID:     "msg_pg_test_" + strings.ReplaceAll(t.Name(), "/", "_"),
		CorrelationID: "cor_pg_test",
		EventType:     "order.created",
		OrderID:       order.OrderID,
		MerchantID:    order.MerchantID,
		OccurredAt:    now,
	}
	audit := commerce.AuditLog{
		MerchantID:    order.MerchantID,
		OrderID:       order.OrderID,
		ActorType:     "merchant",
		ActorID:       order.MerchantID,
		Action:        "order.create",
		CorrelationID: event.CorrelationID,
		CreatedAt:     now,
	}

	created, replayed, err := store.InsertOrder(ctx, order.MerchantID, "idem_pg_test_"+strings.ReplaceAll(t.Name(), "/", "_"), order, event, audit)
	if err != nil {
		t.Fatalf("insert order: %v", err)
	}
	if replayed {
		t.Fatal("first insert must not be idempotent replay")
	}
	if created.OrderID != order.OrderID {
		t.Fatalf("created order id = %q, want %q", created.OrderID, order.OrderID)
	}

	fetched, err := store.GetOrder(ctx, order.OrderID)
	if err != nil {
		t.Fatalf("get order: %v", err)
	}
	if fetched.Totals.Total.Amount != 20100 {
		t.Fatalf("total = %d, want 20100", fetched.Totals.Total.Amount)
	}
	if got := len(store.OutboxEvents()); got == 0 {
		t.Fatal("expected at least one outbox event")
	}
	auditLogs := store.AuditLogs()
	if len(auditLogs) == 0 {
		t.Fatal("expected at least one audit log")
	}
	lastAudit := auditLogs[len(auditLogs)-1]
	if lastAudit.Action != "order.create" || lastAudit.ActorID != order.MerchantID {
		t.Fatalf("last audit log = %+v, want order.create by merchant", lastAudit)
	}
	replayAudit := commerce.AuditLog{
		MerchantID:    "platform",
		ActorType:     "ops",
		ActorID:       "usr_ops_pg_test",
		Action:        "dlq.replay",
		CorrelationID: "cor_replay_pg_test",
		CreatedAt:     now.Add(time.Second),
		Details: map[string]string{
			"queue":          "inventory.reserve.dlq",
			"replayed_count": "2",
			"status":         "succeeded",
		},
	}
	if err := store.RecordAuditLog(ctx, replayAudit); err != nil {
		t.Fatalf("record audit log: %v", err)
	}
	auditLogs = store.AuditLogs()
	lastAudit = auditLogs[len(auditLogs)-1]
	if lastAudit.Action != "dlq.replay" || lastAudit.Details["queue"] != "inventory.reserve.dlq" {
		t.Fatalf("last audit log = %+v, want replay details", lastAudit)
	}
	pending, err := store.PendingOutboxEvents(ctx, 10)
	if err != nil {
		t.Fatalf("pending outbox events: %v", err)
	}
	if len(pending) == 0 {
		t.Fatal("expected pending outbox event")
	}
	if err := store.MarkOutboxPublished(ctx, event.MessageID, now); err != nil {
		t.Fatalf("mark outbox published: %v", err)
	}
	firstInbox, err := store.RecordInboxMessage(ctx, "inventory.reserve", event)
	if err != nil {
		t.Fatalf("record inbox message: %v", err)
	}
	secondInbox, err := store.RecordInboxMessage(ctx, "inventory.reserve", event)
	if err != nil {
		t.Fatalf("record duplicate inbox message: %v", err)
	}
	if !firstInbox || secondInbox {
		t.Fatalf("inbox dedupe = (%v, %v), want (true, false)", firstInbox, secondInbox)
	}
	for _, name := range []string{
		"postgres.insert_order",
		"postgres.get_order",
		"postgres.record_audit_log",
		"postgres.pending_outbox_events",
		"postgres.mark_outbox_published",
		"postgres.record_inbox_message",
	} {
		if !hasSpan(recorder.Ended(), name) {
			t.Fatalf("expected span %q in postgres integration", name)
		}
	}
}

func hasSpan(spans []sdktrace.ReadOnlySpan, name string) bool {
	for _, span := range spans {
		if span.Name() == name {
			return true
		}
	}
	return false
}
