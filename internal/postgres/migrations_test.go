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

func TestMigrationsAddOutboxCausation(t *testing.T) {
	initBody, err := migrationsFS.ReadFile("migrations/001_init.sql")
	if err != nil {
		t.Fatalf("read init migration: %v", err)
	}
	if !strings.Contains(string(initBody), "causation_id TEXT NOT NULL") {
		t.Fatal("initial migration does not require outbox_events.causation_id")
	}

	body, err := migrationsFS.ReadFile("migrations/006_outbox_causation.sql")
	if err != nil {
		t.Fatalf("read causation migration: %v", err)
	}
	sql := string(body)
	for _, fragment := range []string{"ADD COLUMN IF NOT EXISTS causation_id", "SET causation_id = message_id", "idx_outbox_events_causation"} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("causation migration does not include %q", fragment)
		}
	}
}

func TestMigrationsAddInventoryCatalog(t *testing.T) {
	body, err := migrationsFS.ReadFile("migrations/007_inventory_catalog.sql")
	if err != nil {
		t.Fatalf("read inventory catalog migration: %v", err)
	}
	sql := string(body)
	for _, fragment := range []string{
		"CREATE TABLE IF NOT EXISTS warehouses",
		"CREATE TABLE IF NOT EXISTS inventory_items",
		"REFERENCES merchants(id)",
		"UNIQUE (warehouse_id, sku)",
		"idx_inventory_items_sku",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("inventory catalog migration does not include %q", fragment)
		}
	}
}

func TestMigrationsAddOrdersMerchantForeignKey(t *testing.T) {
	initBody, err := migrationsFS.ReadFile("migrations/001_init.sql")
	if err != nil {
		t.Fatalf("read init migration: %v", err)
	}
	if !strings.Contains(string(initBody), "merchant_id TEXT NOT NULL REFERENCES merchants(id)") {
		t.Fatal("initial migration does not require orders.merchant_id foreign key")
	}

	body, err := migrationsFS.ReadFile("migrations/008_orders_merchant_fk.sql")
	if err != nil {
		t.Fatalf("read merchant fk migration: %v", err)
	}
	sql := string(body)
	for _, fragment := range []string{
		"INSERT INTO merchants",
		"ADD CONSTRAINT fk_orders_merchant",
		"FOREIGN KEY (merchant_id)",
		"VALIDATE CONSTRAINT fk_orders_merchant",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("merchant fk migration does not include %q", fragment)
		}
	}
}

func TestMigrationsAddReservationWarehouseProvenance(t *testing.T) {
	body, err := migrationsFS.ReadFile("migrations/009_stock_reservation_warehouse.sql")
	if err != nil {
		t.Fatalf("read reservation warehouse migration: %v", err)
	}
	sql := string(body)
	for _, fragment := range []string{
		"ADD COLUMN IF NOT EXISTS warehouse_id",
		"fk_stock_reservations_warehouse",
		"REFERENCES warehouses(id)",
		"idx_stock_reservations_warehouse_sku",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("reservation warehouse migration does not include %q", fragment)
		}
	}
}

func TestMigrationsSeedLocalDemoInventory(t *testing.T) {
	body, err := migrationsFS.ReadFile("migrations/010_demo_inventory_seed.sql")
	if err != nil {
		t.Fatalf("read demo inventory seed migration: %v", err)
	}
	sql := string(body)
	for _, fragment := range []string{
		"mer_01hzy6v4egscg4r7kb3m7jq2dk",
		"mer_01hzy8v4egscg4r7kb3m7jq9qx",
		"SKU-CHAIR-BLK",
		"inventory_items",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("demo inventory seed migration does not include %q", fragment)
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
		CausationID:   "msg_pg_test_" + strings.ReplaceAll(t.Name(), "/", "_"),
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
	var merchantName string
	if err := store.DB().QueryRowContext(ctx, `SELECT name FROM merchants WHERE id = $1`, order.MerchantID).Scan(&merchantName); err != nil {
		t.Fatalf("query inserted merchant: %v", err)
	}
	if merchantName != order.MerchantID {
		t.Fatalf("merchant name = %q, want %q", merchantName, order.MerchantID)
	}
	seedInventory(t, ctx, store, order.MerchantID, "wh_pg_test_"+strings.ReplaceAll(t.Name(), "/", "_"), map[string]int{
		"SKU-CHAIR-BLK": 5,
		"SKU-LAMP-WHT":  2,
	})

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
		CausationID:   event.MessageID,
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
	assertPostgresOrderStatus(t, ctx, store, order.OrderID, commerce.StatusInventoryReserved)
	assertInventoryQuantities(t, ctx, store, order.MerchantID, "SKU-CHAIR-BLK", 4, 1)
	var reservedWarehouseID string
	if err := store.DB().QueryRowContext(ctx, `
		SELECT warehouse_id
		FROM stock_reservations
		WHERE order_id = $1 AND sku = $2
	`, order.OrderID, "SKU-CHAIR-BLK").Scan(&reservedWarehouseID); err != nil {
		t.Fatalf("query reserved warehouse: %v", err)
	}
	if reservedWarehouseID == "" {
		t.Fatal("stock reservation warehouse id must be present")
	}
	paymentEvent := commerce.OutboxEvent{
		MessageID:     "msg_pg_payment_" + strings.ReplaceAll(t.Name(), "/", "_"),
		CorrelationID: event.CorrelationID,
		CausationID:   inventoryEvent.MessageID,
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
	assertPostgresOrderStatus(t, ctx, store, order.OrderID, commerce.StatusPaymentAuthorized)
	shipmentEvent := commerce.OutboxEvent{
		MessageID:     "msg_pg_shipment_" + strings.ReplaceAll(t.Name(), "/", "_"),
		CorrelationID: event.CorrelationID,
		CausationID:   paymentEvent.MessageID,
		EventType:     "shipment.created",
		OrderID:       order.OrderID,
		MerchantID:    order.MerchantID,
		OccurredAt:    now.Add(4 * time.Second),
	}
	shipmentID := "shp_pg_test_" + strings.ReplaceAll(t.Name(), "/", "_")
	if err := store.RecordShipmentCreated(ctx, paymentEvent, shipmentEvent, commerce.Shipment{
		ShipmentID:     shipmentID,
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
	assertPostgresOrderStatus(t, ctx, store, order.OrderID, commerce.StatusShipmentCreated)
	shipment, err := store.GetShipment(ctx, shipmentID)
	if err != nil {
		t.Fatalf("get shipment: %v", err)
	}
	if shipment.OrderID != order.OrderID || shipment.MerchantID != order.MerchantID {
		t.Fatalf("shipment = %+v, want order and merchant ownership", shipment)
	}
	if shipment.Status != "created" || shipment.Carrier != "fake-carrier" || len(shipment.Events) != 1 {
		t.Fatalf("shipment projection = %+v, want carrier timeline", shipment)
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
		CausationID:   shipmentEvent.MessageID,
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
		CausationID:   completedEvent.MessageID,
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
	voidOrder := &commerce.Order{
		OrderID:         "ord_pg_void_" + strings.ReplaceAll(t.Name(), "/", "_"),
		MerchantID:      "mer_pg_test",
		ExternalOrderID: "external_pg_void_" + strings.ReplaceAll(t.Name(), "/", "_"),
		Status:          commerce.StatusPendingFulfillment,
		Currency:        "USD",
		Totals: commerce.OrderTotals{
			Subtotal: commerce.Money{Amount: 12900, Currency: "USD"},
			Shipping: commerce.Money{Amount: 900, Currency: "USD"},
			Total:    commerce.Money{Amount: 13800, Currency: "USD"},
		},
		Items: []commerce.OrderItem{{
			SKU:               "SKU-LAMP-WHT",
			Quantity:          1,
			UnitPrice:         commerce.Money{Amount: 12900, Currency: "USD"},
			ReservationStatus: "pending",
		}},
		Payment:   &commerce.Payment{Provider: "stripe", Status: "pending_authorization"},
		CreatedAt: now,
		UpdatedAt: now,
	}
	voidCreated := commerce.OutboxEvent{
		MessageID:     "msg_pg_void_created_" + strings.ReplaceAll(t.Name(), "/", "_"),
		CorrelationID: "cor_pg_void",
		CausationID:   "msg_pg_void_created_" + strings.ReplaceAll(t.Name(), "/", "_"),
		EventType:     "order.created",
		OrderID:       voidOrder.OrderID,
		MerchantID:    voidOrder.MerchantID,
		OccurredAt:    now.Add(8 * time.Second),
	}
	if _, _, err := store.InsertOrder(ctx, voidOrder.MerchantID, "idem_pg_void_"+strings.ReplaceAll(t.Name(), "/", "_"), voidOrder, voidCreated, commerce.AuditLog{
		MerchantID:    voidOrder.MerchantID,
		OrderID:       voidOrder.OrderID,
		ActorType:     "merchant",
		ActorID:       voidOrder.MerchantID,
		Action:        "order.create",
		CorrelationID: voidCreated.CorrelationID,
		CreatedAt:     voidCreated.OccurredAt,
	}); err != nil {
		t.Fatalf("insert void order: %v", err)
	}
	voidInventory := commerce.OutboxEvent{
		MessageID:     "msg_pg_void_inventory_" + strings.ReplaceAll(t.Name(), "/", "_"),
		CorrelationID: voidCreated.CorrelationID,
		CausationID:   voidCreated.MessageID,
		EventType:     "inventory.reserved",
		OrderID:       voidOrder.OrderID,
		MerchantID:    voidOrder.MerchantID,
		OccurredAt:    now.Add(9 * time.Second),
	}
	if err := store.RecordInventoryReserved(ctx, voidCreated, voidInventory, commerce.AuditLog{
		MerchantID:    voidOrder.MerchantID,
		OrderID:       voidOrder.OrderID,
		ActorType:     "system",
		ActorID:       "fulfillment-worker",
		Action:        "inventory.reserved",
		CorrelationID: voidCreated.CorrelationID,
		CreatedAt:     voidInventory.OccurredAt,
	}); err != nil {
		t.Fatalf("record void inventory reserved: %v", err)
	}
	assertInventoryQuantities(t, ctx, store, voidOrder.MerchantID, "SKU-LAMP-WHT", 1, 1)
	voidPayment := commerce.OutboxEvent{
		MessageID:     "msg_pg_void_payment_" + strings.ReplaceAll(t.Name(), "/", "_"),
		CorrelationID: voidCreated.CorrelationID,
		CausationID:   voidInventory.MessageID,
		EventType:     "payment.authorized",
		OrderID:       voidOrder.OrderID,
		MerchantID:    voidOrder.MerchantID,
		OccurredAt:    now.Add(10 * time.Second),
	}
	if err := store.RecordPaymentAuthorized(ctx, voidInventory, voidPayment, commerce.Payment{
		Provider:        "stripe",
		Status:          "authorized",
		AuthorizationID: "pay_pg_void_" + strings.ReplaceAll(t.Name(), "/", "_"),
	}, commerce.AuditLog{
		MerchantID:    voidOrder.MerchantID,
		OrderID:       voidOrder.OrderID,
		ActorType:     "system",
		ActorID:       "fulfillment-worker",
		Action:        "payment.authorized",
		CorrelationID: voidCreated.CorrelationID,
		CreatedAt:     voidPayment.OccurredAt,
	}); err != nil {
		t.Fatalf("record void payment authorized: %v", err)
	}
	voidCompensation := commerce.OutboxEvent{
		MessageID:     "msg_pg_void_compensation_" + strings.ReplaceAll(t.Name(), "/", "_"),
		CorrelationID: voidCreated.CorrelationID,
		CausationID:   voidPayment.MessageID,
		EventType:     "shipment.failed",
		OrderID:       voidOrder.OrderID,
		MerchantID:    voidOrder.MerchantID,
		OccurredAt:    now.Add(11 * time.Second),
	}
	if err := store.RecordCompensation(ctx, voidCompensation, commerce.StatusCancellationPending, commerce.AuditLog{
		MerchantID:    voidOrder.MerchantID,
		OrderID:       voidOrder.OrderID,
		ActorType:     "system",
		ActorID:       "fulfillment-worker",
		Action:        "compensation.shipment_failed",
		CorrelationID: voidCreated.CorrelationID,
		CreatedAt:     voidCompensation.OccurredAt,
		Details: map[string]string{
			"source_message_id":   voidCompensation.MessageID,
			"source_event_type":   voidCompensation.EventType,
			"target_order_status": string(commerce.StatusCancellationPending),
			"stock_release":       "requested",
			"payment_void":        "requested",
		},
	}); err != nil {
		t.Fatalf("record void compensation: %v", err)
	}
	voided, err := store.GetOrder(ctx, voidOrder.OrderID)
	if err != nil {
		t.Fatalf("get voided order: %v", err)
	}
	if voided.Status != commerce.StatusCancellationPending {
		t.Fatalf("voided order status = %q, want cancellation_pending", voided.Status)
	}
	if voided.Items[0].ReservationStatus != "released" {
		t.Fatalf("voided reservation status = %q, want released", voided.Items[0].ReservationStatus)
	}
	assertInventoryQuantities(t, ctx, store, voidOrder.MerchantID, "SKU-LAMP-WHT", 2, 0)
	if voided.Payment == nil || voided.Payment.Status != "voided" {
		t.Fatalf("voided payment = %+v, want voided", voided.Payment)
	}
	auditLogs := store.AuditLogs()
	if len(auditLogs) == 0 {
		t.Fatal("expected at least one audit log")
	}
	foundCreateAudit := false
	for _, auditLog := range auditLogs {
		if auditLog.Action == "order.create" && auditLog.ActorID == order.MerchantID {
			foundCreateAudit = true
			break
		}
	}
	if !foundCreateAudit {
		t.Fatalf("audit logs = %+v, want order.create by merchant", auditLogs)
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
	foundReplayAudit := false
	for _, auditLog := range auditLogs {
		if auditLog.Action == "dlq.replay" && auditLog.Details["queue"] == "inventory.reserve.dlq" {
			foundReplayAudit = true
			break
		}
	}
	if !foundReplayAudit {
		t.Fatalf("audit logs = %+v, want replay details", auditLogs)
	}
	cancelAudit := commerce.AuditLog{
		MerchantID:    order.MerchantID,
		OrderID:       order.OrderID,
		ActorType:     "merchant_user",
		ActorID:       "usr_cancel_pg_test",
		Action:        "order.cancel_requested",
		CorrelationID: "cor_cancel_pg_test",
		CreatedAt:     now.Add(2 * time.Second),
		Details: map[string]string{
			"reason": "customer_requested",
		},
	}
	if err := store.RecordAuditLog(ctx, cancelAudit); err != nil {
		t.Fatalf("record cancellation audit log: %v", err)
	}
	auditLogs = store.AuditLogs()
	foundCancelReasonAudit := false
	for _, auditLog := range auditLogs {
		if auditLog.Action == "order.cancel_requested" && auditLog.Details["reason"] == "customer_requested" {
			foundCancelReasonAudit = true
			break
		}
	}
	if !foundCancelReasonAudit {
		t.Fatalf("audit logs = %+v, want cancellation reason details", auditLogs)
	}
	pending, err := store.PendingOutboxEvents(ctx, 10)
	if err != nil {
		t.Fatalf("pending outbox events: %v", err)
	}
	if len(pending) == 0 {
		t.Fatal("expected pending outbox event")
	}
	if pending[0].CausationID == "" {
		t.Fatal("expected pending outbox event to include causation id")
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
		"postgres.get_shipment",
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

func seedInventory(t testing.TB, ctx context.Context, store *Store, merchantID, warehouseID string, quantities map[string]int) {
	t.Helper()
	if _, err := store.DB().ExecContext(ctx, `
		INSERT INTO warehouses (id, merchant_id, code, name, status)
		VALUES ($1, $2, 'test', 'Postgres Test Warehouse', 'active')
		ON CONFLICT (id) DO UPDATE
		SET merchant_id = EXCLUDED.merchant_id,
			code = EXCLUDED.code,
			name = EXCLUDED.name,
			status = EXCLUDED.status,
			updated_at = now()
	`, warehouseID, merchantID); err != nil {
		t.Fatalf("seed warehouse: %v", err)
	}
	for sku, quantity := range quantities {
		if _, err := store.DB().ExecContext(ctx, `
			INSERT INTO inventory_items (warehouse_id, sku, available_quantity, reserved_quantity)
			VALUES ($1, $2, $3, 0)
			ON CONFLICT (warehouse_id, sku) DO UPDATE
			SET available_quantity = EXCLUDED.available_quantity,
				reserved_quantity = EXCLUDED.reserved_quantity,
				updated_at = now()
		`, warehouseID, sku, quantity); err != nil {
			t.Fatalf("seed inventory item %s: %v", sku, err)
		}
	}
}

func assertInventoryQuantities(t testing.TB, ctx context.Context, store *Store, merchantID, sku string, available, reserved int) {
	t.Helper()
	var gotAvailable, gotReserved int
	if err := store.DB().QueryRowContext(ctx, `
		SELECT ii.available_quantity, ii.reserved_quantity
		FROM inventory_items ii
		JOIN warehouses w ON w.id = ii.warehouse_id
		WHERE w.merchant_id = $1 AND ii.sku = $2
		ORDER BY ii.id ASC
		LIMIT 1
	`, merchantID, sku).Scan(&gotAvailable, &gotReserved); err != nil {
		t.Fatalf("query inventory item %s: %v", sku, err)
	}
	if gotAvailable != available || gotReserved != reserved {
		t.Fatalf("inventory %s = available %d reserved %d, want available %d reserved %d", sku, gotAvailable, gotReserved, available, reserved)
	}
}

func assertPostgresOrderStatus(t testing.TB, ctx context.Context, store *Store, orderID string, want commerce.OrderStatus) {
	t.Helper()
	order, err := store.GetOrder(ctx, orderID)
	if err != nil {
		t.Fatalf("get order %s: %v", orderID, err)
	}
	if order.Status != want {
		t.Fatalf("order %s status = %q, want %q", orderID, order.Status, want)
	}
}
