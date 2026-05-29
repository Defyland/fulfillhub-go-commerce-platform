package postgres

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Defyland/fulfillhub-go-commerce-platform/internal/commerce"
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

func TestPostgresStoreIntegration(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

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

	created, replayed, err := store.InsertOrder(order.MerchantID, "idem_pg_test_"+strings.ReplaceAll(t.Name(), "/", "_"), order, event)
	if err != nil {
		t.Fatalf("insert order: %v", err)
	}
	if replayed {
		t.Fatal("first insert must not be idempotent replay")
	}
	if created.OrderID != order.OrderID {
		t.Fatalf("created order id = %q, want %q", created.OrderID, order.OrderID)
	}

	fetched, err := store.GetOrder(order.OrderID)
	if err != nil {
		t.Fatalf("get order: %v", err)
	}
	if fetched.Totals.Total.Amount != 20100 {
		t.Fatalf("total = %d, want 20100", fetched.Totals.Total.Amount)
	}
	if got := len(store.OutboxEvents()); got == 0 {
		t.Fatal("expected at least one outbox event")
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
}
