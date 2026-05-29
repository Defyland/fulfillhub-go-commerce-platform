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

func TestMigrationsAddFulfillmentProjectionTables(t *testing.T) {
	body, err := migrationsFS.ReadFile("migrations/003_fulfillment_projections.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	sql := string(body)
	for _, table := range []string{"stock_reservations", "payment_authorizations", "shipments"} {
		if !strings.Contains(sql, "CREATE TABLE IF NOT EXISTS "+table) {
			t.Fatalf("fulfillment projection migration does not create %s", table)
		}
	}
}

func TestMigrationsAddNotificationEvents(t *testing.T) {
	body, err := migrationsFS.ReadFile("migrations/004_notification_events.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	sql := string(body)
	if !strings.Contains(sql, "CREATE TABLE IF NOT EXISTS notification_events") {
		t.Fatal("notification events migration does not create notification_events")
	}
}

func TestMigrationsAddCompensationEvents(t *testing.T) {
	body, err := migrationsFS.ReadFile("migrations/005_compensation_events.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	sql := string(body)
	if !strings.Contains(sql, "CREATE TABLE IF NOT EXISTS compensation_events") {
		t.Fatal("compensation events migration does not create compensation_events")
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
	inventoryEvent := commerce.OutboxEvent{
		MessageID:     "msg_pg_inventory_" + strings.ReplaceAll(t.Name(), "/", "_"),
		CorrelationID: event.CorrelationID,
		EventType:     "inventory.reserved",
		OrderID:       order.OrderID,
		MerchantID:    order.MerchantID,
		OccurredAt:    now.Add(2 * time.Second),
	}
	if err := store.RecordInventoryReserved(ctx, event, inventoryEvent, commerce.AuditLog{
		MerchantID:    order.MerchantID,
		OrderID:       order.OrderID,
		ActorType:     "system",
		ActorID:       "fulfillment-worker",
		Action:        "inventory.reserved",
		CorrelationID: event.CorrelationID,
		CreatedAt:     inventoryEvent.OccurredAt,
	}); err != nil {
		t.Fatalf("record inventory reserved: %v", err)
	}
	paymentEvent := commerce.OutboxEvent{
		MessageID:     "msg_pg_payment_" + strings.ReplaceAll(t.Name(), "/", "_"),
		CorrelationID: event.CorrelationID,
		EventType:     "payment.authorized",
		OrderID:       order.OrderID,
		MerchantID:    order.MerchantID,
		OccurredAt:    now.Add(3 * time.Second),
	}
	if err := store.RecordPaymentAuthorized(ctx, inventoryEvent, paymentEvent, commerce.Payment{
		Provider:        "stripe",
		Status:          "authorized",
		AuthorizationID: "pay_pg_test_" + strings.ReplaceAll(t.Name(), "/", "_"),
	}, commerce.AuditLog{
		MerchantID:    order.MerchantID,
		OrderID:       order.OrderID,
		ActorType:     "system",
		ActorID:       "fulfillment-worker",
		Action:        "payment.authorized",
		CorrelationID: event.CorrelationID,
		CreatedAt:     paymentEvent.OccurredAt,
	}); err != nil {
		t.Fatalf("record payment authorized: %v", err)
	}
	shipmentEvent := commerce.OutboxEvent{
		MessageID:     "msg_pg_shipment_" + strings.ReplaceAll(t.Name(), "/", "_"),
		CorrelationID: event.CorrelationID,
		EventType:     "shipment.created",
		OrderID:       order.OrderID,
		MerchantID:    order.MerchantID,
		OccurredAt:    now.Add(4 * time.Second),
	}
	if err := store.RecordShipmentCreated(ctx, paymentEvent, shipmentEvent, commerce.Shipment{
		ShipmentID:     "shp_pg_test_" + strings.ReplaceAll(t.Name(), "/", "_"),
		Status:         "created",
		Carrier:        "fake-carrier",
		TrackingNumber: "TRACK-PG-" + strings.ReplaceAll(t.Name(), "/", "_"),
	}, commerce.AuditLog{
		MerchantID:    order.MerchantID,
		OrderID:       order.OrderID,
		ActorType:     "system",
		ActorID:       "fulfillment-worker",
		Action:        "shipment.created",
		CorrelationID: event.CorrelationID,
		CreatedAt:     shipmentEvent.OccurredAt,
	}); err != nil {
		t.Fatalf("record shipment created: %v", err)
	}
	fetched, err = store.GetOrder(ctx, order.OrderID)
	if err != nil {
		t.Fatalf("get projected order: %v", err)
	}
	if fetched.Items[0].ReservationStatus != "reserved" {
		t.Fatalf("reservation status = %q, want reserved", fetched.Items[0].ReservationStatus)
	}
	if fetched.Payment == nil || fetched.Payment.Status != "authorized" {
		t.Fatalf("payment projection = %+v, want authorized", fetched.Payment)
	}
	if fetched.Shipment == nil || fetched.Shipment.Status != "created" {
		t.Fatalf("shipment projection = %+v, want created", fetched.Shipment)
	}
	completedEvent := commerce.OutboxEvent{
		MessageID:     "msg_pg_completed_" + strings.ReplaceAll(t.Name(), "/", "_"),
		CorrelationID: event.CorrelationID,
		EventType:     "order.completed",
		OrderID:       order.OrderID,
		MerchantID:    order.MerchantID,
		OccurredAt:    now.Add(5 * time.Second),
	}
	if _, err := store.UpdateOrderStatus(ctx, order.OrderID, commerce.StatusCompleted, completedEvent.OccurredAt, completedEvent, commerce.AuditLog{
		MerchantID:    order.MerchantID,
		OrderID:       order.OrderID,
		ActorType:     "system",
		ActorID:       "fulfillment-worker",
		Action:        "order.completed",
		CorrelationID: event.CorrelationID,
		CreatedAt:     completedEvent.OccurredAt,
	}); err != nil {
		t.Fatalf("complete order: %v", err)
	}
	if err := store.RecordNotificationQueued(ctx, completedEvent, commerce.AuditLog{
		MerchantID:    order.MerchantID,
		OrderID:       order.OrderID,
		ActorType:     "system",
		ActorID:       "fulfillment-worker",
		Action:        "notification.email_queued",
		CorrelationID: event.CorrelationID,
		CreatedAt:     now.Add(6 * time.Second),
	}); err != nil {
		t.Fatalf("record notification queued: %v", err)
	}
	compensationEvent := commerce.OutboxEvent{
		MessageID:     "msg_pg_compensation_" + strings.ReplaceAll(t.Name(), "/", "_"),
		CorrelationID: event.CorrelationID,
		EventType:     "inventory.rejected",
		OrderID:       order.OrderID,
		MerchantID:    order.MerchantID,
		OccurredAt:    now.Add(7 * time.Second),
	}
	if err := store.RecordCompensation(ctx, compensationEvent, commerce.StatusFailed, commerce.AuditLog{
		MerchantID:    order.MerchantID,
		OrderID:       order.OrderID,
		ActorType:     "system",
		ActorID:       "fulfillment-worker",
		Action:        "compensation.inventory_rejected",
		CorrelationID: event.CorrelationID,
		CreatedAt:     compensationEvent.OccurredAt,
		Details: map[string]string{
			"source_message_id":   compensationEvent.MessageID,
			"source_event_type":   compensationEvent.EventType,
			"target_order_status": string(commerce.StatusFailed),
		},
	}); err != nil {
		t.Fatalf("record compensation: %v", err)
	}
	compensated, err := store.GetOrder(ctx, order.OrderID)
	if err != nil {
		t.Fatalf("get compensated order: %v", err)
	}
	if compensated.Status != commerce.StatusFailed {
		t.Fatalf("compensated status = %q, want failed", compensated.Status)
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
	pendingCount, err := store.PendingOutboxCount(ctx)
	if err != nil {
		t.Fatalf("pending outbox count: %v", err)
	}
	if pendingCount == 0 {
		t.Fatal("expected pending outbox count")
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
	if err := store.ReleaseInboxMessage(ctx, "inventory.reserve", event); err != nil {
		t.Fatalf("release inbox message: %v", err)
	}
	afterReleaseInbox, err := store.RecordInboxMessage(ctx, "inventory.reserve", event)
	if err != nil {
		t.Fatalf("record inbox after release: %v", err)
	}
	if !afterReleaseInbox {
		t.Fatal("inbox record after release must be treated as new")
	}
	for _, name := range []string{
		"postgres.insert_order",
		"postgres.get_order",
		"postgres.record_audit_log",
		"postgres.pending_outbox_events",
		"postgres.pending_outbox_count",
		"postgres.mark_outbox_published",
		"postgres.record_inbox_message",
		"postgres.release_inbox_message",
		"postgres.record_inventory_reserved",
		"postgres.record_payment_authorized",
		"postgres.record_shipment_created",
		"postgres.update_order_status",
		"postgres.record_notification_queued",
		"postgres.record_compensation",
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
