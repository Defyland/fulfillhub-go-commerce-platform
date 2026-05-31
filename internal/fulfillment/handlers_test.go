package fulfillment

import (
	"context"
	"testing"
	"time"

	"github.com/Defyland/fulfillhub-go-commerce-platform/internal/commerce"
	"github.com/Defyland/fulfillhub-go-commerce-platform/internal/messaging"
)

func TestWorkerHandlersAdvanceHappyPathAndCompleteOrder(t *testing.T) {
	store := commerce.NewMemoryStore()
	service := commerce.NewService(store)
	order, _, err := service.CreateOrder("mer_demo", "idem-key-0001", "cor_1", validCreateOrderCommand())
	if err != nil {
		t.Fatalf("CreateOrder returned error: %v", err)
	}
	created := service.OutboxEvents()[0]
	ids := []string{"msg_inventory", "msg_payment", "pay_authorized", "msg_shipment", "shp_created", "msg_completed"}
	now := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	deps := Dependencies{
		Projector: store,
		Orders:    store,
		Clock:     func() time.Time { return now },
		NewID: func(string) string {
			id := ids[0]
			ids = ids[1:]
			return id
		},
	}

	inventory := handlerForTest(t, messaging.InventoryReserveQueue, deps)
	if err := inventory.HandleEvent(context.Background(), created); err != nil {
		t.Fatalf("inventory handler returned error: %v", err)
	}
	assertOrderStatus(t, store, order.OrderID, commerce.StatusInventoryReserved)
	inventoryReserved := lastOutboxEvent(service)
	payment := handlerForTest(t, messaging.PaymentsAuthorizeQueue, deps)
	if err := payment.HandleEvent(context.Background(), inventoryReserved); err != nil {
		t.Fatalf("payment handler returned error: %v", err)
	}
	assertOrderStatus(t, store, order.OrderID, commerce.StatusPaymentAuthorized)
	paymentAuthorized := lastOutboxEvent(service)
	shipment := handlerForTest(t, messaging.ShipmentsCreateQueue, deps)
	if err := shipment.HandleEvent(context.Background(), paymentAuthorized); err != nil {
		t.Fatalf("shipment handler returned error: %v", err)
	}
	assertOrderStatus(t, store, order.OrderID, commerce.StatusShipmentCreated)
	shipmentCreated := lastOutboxEvent(service)
	finalizer := handlerForTest(t, messaging.OrdersFinalizeQueue, deps)
	if err := finalizer.HandleEvent(context.Background(), shipmentCreated); err != nil {
		t.Fatalf("finalizer handler returned error: %v", err)
	}

	events := service.OutboxEvents()
	if got := eventTypes(events); len(got) != 5 || got[1] != "inventory.reserved" || got[2] != "payment.authorized" || got[3] != "shipment.created" || got[4] != "order.completed" {
		t.Fatalf("outbox event types = %v, want transactional fulfillment progression", got)
	}
	assertCausationChain(t, events)
	completed, err := store.GetOrder(context.Background(), order.OrderID)
	if err != nil {
		t.Fatalf("GetOrder returned error: %v", err)
	}
	if completed.Status != commerce.StatusCompleted {
		t.Fatalf("order status = %q, want completed", completed.Status)
	}
	if completed.Items[0].ReservationStatus != "reserved" {
		t.Fatalf("reservation status = %q, want reserved", completed.Items[0].ReservationStatus)
	}
	if completed.Payment == nil || completed.Payment.AuthorizationID != "pay_authorized" || completed.Payment.Status != "authorized" {
		t.Fatalf("payment projection = %+v, want authorized payment", completed.Payment)
	}
	if completed.Shipment == nil || completed.Shipment.ShipmentID != "shp_created" || completed.Shipment.Status != "created" {
		t.Fatalf("shipment projection = %+v, want created shipment", completed.Shipment)
	}
	logs := service.AuditLogs()
	if got := logs[len(logs)-1].Action; got != "order.completed" {
		t.Fatalf("last audit action = %q, want order.completed", got)
	}
}

func TestHandlerForQueueRejectsUnexpectedEventType(t *testing.T) {
	handler := handlerForTest(t, messaging.PaymentsAuthorizeQueue, Dependencies{
		Projector: commerce.NewMemoryStore(),
	})

	err := handler.HandleEvent(context.Background(), commerce.OutboxEvent{
		MessageID:     "msg_1",
		CorrelationID: "cor_1",
		CausationID:   "msg_1",
		EventType:     "order.created",
		OrderID:       "ord_1",
		MerchantID:    "mer_1",
	})
	if err == nil {
		t.Fatal("handler must reject unexpected event type")
	}
}

func TestInventoryHandlerWritesRejectionEventWhenReservationFails(t *testing.T) {
	store := commerce.NewMemoryStore()
	service := commerce.NewService(store)
	order, _, err := service.CreateOrder("mer_demo", "idem-key-0001", "cor_1", validCreateOrderCommand())
	if err != nil {
		t.Fatalf("CreateOrder returned error: %v", err)
	}
	ids := []string{"msg_inventory_rejected"}
	now := time.Date(2026, 5, 29, 14, 30, 0, 0, time.UTC)
	deps := Dependencies{
		Projector: store,
		Orders:    store,
		InventoryReserver: InventoryReserverFunc(func(context.Context, commerce.OutboxEvent) error {
			return context.DeadlineExceeded
		}),
		Clock: func() time.Time { return now },
		NewID: func(string) string {
			id := ids[0]
			ids = ids[1:]
			return id
		},
	}

	inventory := handlerForTest(t, messaging.InventoryReserveQueue, deps)
	if err := inventory.HandleEvent(context.Background(), service.OutboxEvents()[0]); err != nil {
		t.Fatalf("inventory handler returned error: %v", err)
	}

	if got := eventTypes(service.OutboxEvents()); len(got) != 2 || got[1] != "inventory.rejected" {
		t.Fatalf("outbox event types = %v, want inventory.rejected", got)
	}
	rejected, err := store.GetOrder(context.Background(), order.OrderID)
	if err != nil {
		t.Fatalf("GetOrder returned error: %v", err)
	}
	if rejected.Items[0].ReservationStatus != "rejected" {
		t.Fatalf("reservation status = %q, want rejected", rejected.Items[0].ReservationStatus)
	}
	logs := service.AuditLogs()
	last := logs[len(logs)-1]
	if last.Action != "inventory.rejected" {
		t.Fatalf("last audit action = %q, want inventory.rejected", last.Action)
	}
	if last.Details["error"] == "" {
		t.Fatalf("inventory rejection audit details = %+v, want error", last.Details)
	}
}

func TestInventoryHandlerWritesRejectionEventWhenStockIsInsufficient(t *testing.T) {
	store := commerce.NewMemoryStore()
	service := commerce.NewService(store)
	order, _, err := service.CreateOrder("mer_demo", "idem-key-0001", "cor_1", validCreateOrderCommand())
	if err != nil {
		t.Fatalf("CreateOrder returned error: %v", err)
	}
	ids := []string{"msg_inventory_attempt", "msg_inventory_rejected"}
	now := time.Date(2026, 5, 29, 14, 45, 0, 0, time.UTC)
	deps := Dependencies{
		Projector: insufficientStockProjector{MemoryStore: store},
		Clock:     func() time.Time { return now },
		NewID: func(string) string {
			id := ids[0]
			ids = ids[1:]
			return id
		},
	}

	inventory := handlerForTest(t, messaging.InventoryReserveQueue, deps)
	if err := inventory.HandleEvent(context.Background(), service.OutboxEvents()[0]); err != nil {
		t.Fatalf("inventory handler returned error: %v", err)
	}

	if got := eventTypes(service.OutboxEvents()); len(got) != 2 || got[1] != "inventory.rejected" {
		t.Fatalf("outbox event types = %v, want inventory.rejected", got)
	}
	rejected, err := store.GetOrder(context.Background(), order.OrderID)
	if err != nil {
		t.Fatalf("GetOrder returned error: %v", err)
	}
	if rejected.Items[0].ReservationStatus != "rejected" {
		t.Fatalf("reservation status = %q, want rejected", rejected.Items[0].ReservationStatus)
	}
	last := service.AuditLogs()[len(service.AuditLogs())-1]
	if last.Details["error"] != commerce.ErrInsufficientStock.Error() {
		t.Fatalf("inventory rejection error detail = %q, want insufficient stock", last.Details["error"])
	}
}

func TestPaymentHandlerWritesFailureEventWhenAuthorizationFails(t *testing.T) {
	store := commerce.NewMemoryStore()
	service := commerce.NewService(store)
	order, _, err := service.CreateOrder("mer_demo", "idem-key-0001", "cor_1", validCreateOrderCommand())
	if err != nil {
		t.Fatalf("CreateOrder returned error: %v", err)
	}
	ids := []string{"msg_inventory", "msg_payment_failed"}
	now := time.Date(2026, 5, 29, 15, 0, 0, 0, time.UTC)
	deps := Dependencies{
		Projector: store,
		Orders:    store,
		Clock:     func() time.Time { return now },
		NewID: func(string) string {
			id := ids[0]
			ids = ids[1:]
			return id
		},
	}
	inventory := handlerForTest(t, messaging.InventoryReserveQueue, deps)
	if err := inventory.HandleEvent(context.Background(), service.OutboxEvents()[0]); err != nil {
		t.Fatalf("inventory handler returned error: %v", err)
	}
	inventoryReserved := lastOutboxEvent(service)
	deps.PaymentAuthorizer = PaymentAuthorizerFunc(func(context.Context, commerce.OutboxEvent) (commerce.Payment, error) {
		return commerce.Payment{}, context.DeadlineExceeded
	})

	payment := handlerForTest(t, messaging.PaymentsAuthorizeQueue, deps)
	if err := payment.HandleEvent(context.Background(), inventoryReserved); err != nil {
		t.Fatalf("payment handler returned error: %v", err)
	}

	if got := eventTypes(service.OutboxEvents()); len(got) != 3 || got[2] != "payment.failed" {
		t.Fatalf("outbox event types = %v, want payment.failed", got)
	}
	failed, err := store.GetOrder(context.Background(), order.OrderID)
	if err != nil {
		t.Fatalf("GetOrder returned error: %v", err)
	}
	if failed.Payment == nil || failed.Payment.Status != "failed" {
		t.Fatalf("payment projection = %+v, want failed payment", failed.Payment)
	}
	logs := service.AuditLogs()
	last := logs[len(logs)-1]
	if last.Action != "payment.failed" {
		t.Fatalf("last audit action = %q, want payment.failed", last.Action)
	}
	if last.Details["error"] == "" {
		t.Fatalf("payment failure audit details = %+v, want error", last.Details)
	}
}

func TestShipmentHandlerWritesFailureEventWhenProviderFails(t *testing.T) {
	store := commerce.NewMemoryStore()
	service := commerce.NewService(store)
	order, _, err := service.CreateOrder("mer_demo", "idem-key-0001", "cor_1", validCreateOrderCommand())
	if err != nil {
		t.Fatalf("CreateOrder returned error: %v", err)
	}
	ids := []string{"msg_inventory", "msg_payment", "pay_authorized", "msg_shipment_failed"}
	now := time.Date(2026, 5, 29, 16, 0, 0, 0, time.UTC)
	deps := Dependencies{
		Projector: store,
		Orders:    store,
		Clock:     func() time.Time { return now },
		NewID: func(string) string {
			id := ids[0]
			ids = ids[1:]
			return id
		},
	}
	inventory := handlerForTest(t, messaging.InventoryReserveQueue, deps)
	if err := inventory.HandleEvent(context.Background(), service.OutboxEvents()[0]); err != nil {
		t.Fatalf("inventory handler returned error: %v", err)
	}
	payment := handlerForTest(t, messaging.PaymentsAuthorizeQueue, deps)
	if err := payment.HandleEvent(context.Background(), lastOutboxEvent(service)); err != nil {
		t.Fatalf("payment handler returned error: %v", err)
	}
	paymentAuthorized := lastOutboxEvent(service)
	deps.ShipmentCreator = ShipmentCreatorFunc(func(context.Context, commerce.OutboxEvent) (commerce.Shipment, error) {
		return commerce.Shipment{}, context.DeadlineExceeded
	})

	shipment := handlerForTest(t, messaging.ShipmentsCreateQueue, deps)
	if err := shipment.HandleEvent(context.Background(), paymentAuthorized); err != nil {
		t.Fatalf("shipment handler returned error: %v", err)
	}

	if got := eventTypes(service.OutboxEvents()); len(got) != 4 || got[3] != "shipment.failed" {
		t.Fatalf("outbox event types = %v, want shipment.failed", got)
	}
	failed, err := store.GetOrder(context.Background(), order.OrderID)
	if err != nil {
		t.Fatalf("GetOrder returned error: %v", err)
	}
	if failed.Shipment == nil || failed.Shipment.Status != "failed" {
		t.Fatalf("shipment projection = %+v, want failed shipment", failed.Shipment)
	}
	logs := service.AuditLogs()
	last := logs[len(logs)-1]
	if last.Action != "shipment.failed" {
		t.Fatalf("last audit action = %q, want shipment.failed", last.Action)
	}
	if last.Details["error"] == "" {
		t.Fatalf("shipment failure audit details = %+v, want error", last.Details)
	}
}

func TestNotificationHandlerQueuesEmailAudit(t *testing.T) {
	store := commerce.NewMemoryStore()
	service := commerce.NewService(store)
	order, _, err := service.CreateOrder("mer_demo", "idem-key-0001", "cor_1", validCreateOrderCommand())
	if err != nil {
		t.Fatalf("CreateOrder returned error: %v", err)
	}
	handler := handlerForTest(t, messaging.NotificationsEmailQueue, Dependencies{
		Projector: store,
		Clock: func() time.Time {
			return time.Date(2026, 5, 29, 13, 0, 0, 0, time.UTC)
		},
	})

	if err := handler.HandleEvent(context.Background(), commerce.OutboxEvent{
		MessageID:     "msg_completed",
		CorrelationID: "cor_1",
		CausationID:   "msg_shipment",
		EventType:     "order.completed",
		OrderID:       order.OrderID,
		MerchantID:    order.MerchantID,
	}); err != nil {
		t.Fatalf("notification handler returned error: %v", err)
	}

	logs := service.AuditLogs()
	last := logs[len(logs)-1]
	if last.Action != "notification.email_queued" {
		t.Fatalf("last audit action = %q, want notification.email_queued", last.Action)
	}
	if last.Details["source_event_type"] != "order.completed" {
		t.Fatalf("source event detail = %q, want order.completed", last.Details["source_event_type"])
	}
}

func TestNotificationHandlerQueuesFailureEmailAudit(t *testing.T) {
	store := commerce.NewMemoryStore()
	service := commerce.NewService(store)
	order, _, err := service.CreateOrder("mer_demo", "idem-key-0001", "cor_1", validCreateOrderCommand())
	if err != nil {
		t.Fatalf("CreateOrder returned error: %v", err)
	}
	handler := handlerForTest(t, messaging.NotificationsEmailQueue, Dependencies{
		Projector: store,
		Clock: func() time.Time {
			return time.Date(2026, 5, 29, 13, 15, 0, 0, time.UTC)
		},
	})

	if err := handler.HandleEvent(context.Background(), commerce.OutboxEvent{
		MessageID:     "msg_payment_failed",
		CorrelationID: "cor_1",
		CausationID:   "msg_inventory",
		EventType:     "payment.failed",
		OrderID:       order.OrderID,
		MerchantID:    order.MerchantID,
	}); err != nil {
		t.Fatalf("notification handler returned error: %v", err)
	}

	logs := service.AuditLogs()
	last := logs[len(logs)-1]
	if last.Action != "notification.email_queued" {
		t.Fatalf("last audit action = %q, want notification.email_queued", last.Action)
	}
	if last.Details["source_event_type"] != "payment.failed" {
		t.Fatalf("source event detail = %q, want payment.failed", last.Details["source_event_type"])
	}
}

func TestNotificationHandlerQueuesManualReviewEmailAudit(t *testing.T) {
	store := commerce.NewMemoryStore()
	service := commerce.NewService(store)
	order, _, err := service.CreateOrder("mer_demo", "idem-key-0001", "cor_1", validCreateOrderCommand())
	if err != nil {
		t.Fatalf("CreateOrder returned error: %v", err)
	}
	handler := handlerForTest(t, messaging.NotificationsEmailQueue, Dependencies{
		Projector: store,
		Clock: func() time.Time {
			return time.Date(2026, 5, 29, 13, 20, 0, 0, time.UTC)
		},
	})

	if err := handler.HandleEvent(context.Background(), commerce.OutboxEvent{
		MessageID:     "msg_manual_review",
		CorrelationID: "cor_1",
		CausationID:   "msg_cancel",
		EventType:     "order.manual_review_required",
		OrderID:       order.OrderID,
		MerchantID:    order.MerchantID,
	}); err != nil {
		t.Fatalf("notification handler returned error: %v", err)
	}

	last := service.AuditLogs()[len(service.AuditLogs())-1]
	if last.Action != "notification.email_queued" {
		t.Fatalf("last audit action = %q, want notification.email_queued", last.Action)
	}
	if last.Details["source_event_type"] != "order.manual_review_required" {
		t.Fatalf("source event detail = %q, want order.manual_review_required", last.Details["source_event_type"])
	}
}

func TestCancellationHandlerFinalizesOrderAndEmitsCancelledEvent(t *testing.T) {
	store := commerce.NewMemoryStore()
	service := commerce.NewService(store)
	order, _, err := service.CreateOrder("mer_demo", "idem-key-0001", "cor_1", validCreateOrderCommand())
	if err != nil {
		t.Fatalf("CreateOrder returned error: %v", err)
	}
	if _, err := service.CancelOrder(order.OrderID, "cor_cancel", commerce.AuditActor{
		Type:   "merchant_user",
		ID:     "usr_93842",
		Reason: "customer_requested",
	}); err != nil {
		t.Fatalf("CancelOrder returned error: %v", err)
	}
	cancelRequested := lastOutboxEvent(service)
	ids := []string{"msg_cancelled"}
	now := time.Date(2026, 5, 29, 13, 30, 0, 0, time.UTC)
	handler := handlerForTest(t, messaging.OrdersCancelQueue, Dependencies{
		Projector: store,
		Orders:    store,
		Clock:     func() time.Time { return now },
		NewID: func(string) string {
			id := ids[0]
			ids = ids[1:]
			return id
		},
	})

	if err := handler.HandleEvent(context.Background(), cancelRequested); err != nil {
		t.Fatalf("cancellation handler returned error: %v", err)
	}

	cancelled, err := store.GetOrder(context.Background(), order.OrderID)
	if err != nil {
		t.Fatalf("GetOrder returned error: %v", err)
	}
	if cancelled.Status != commerce.StatusCancelled {
		t.Fatalf("order status = %q, want cancelled", cancelled.Status)
	}
	cancelledEvent := lastOutboxEvent(service)
	if cancelledEvent.EventType != "order.cancelled" {
		t.Fatalf("last event type = %q, want order.cancelled", cancelledEvent.EventType)
	}
	if cancelledEvent.CausationID != cancelRequested.MessageID {
		t.Fatalf("cancelled causation id = %q, want %q", cancelledEvent.CausationID, cancelRequested.MessageID)
	}
	logs := service.AuditLogs()
	last := logs[len(logs)-1]
	if last.Action != "order.cancelled" {
		t.Fatalf("last audit action = %q, want order.cancelled", last.Action)
	}
}

func TestCancellationHandlerReleasesStockAndVoidsPaymentBeforeShipment(t *testing.T) {
	store := commerce.NewMemoryStore()
	service := commerce.NewService(store)
	order, _, err := service.CreateOrder("mer_demo", "idem-key-0001", "cor_1", validCreateOrderCommand())
	if err != nil {
		t.Fatalf("CreateOrder returned error: %v", err)
	}
	ids := []string{"msg_inventory", "msg_payment", "pay_authorized", "msg_cancelled"}
	now := time.Date(2026, 5, 29, 13, 45, 0, 0, time.UTC)
	deps := Dependencies{
		Projector: store,
		Orders:    store,
		Clock:     func() time.Time { return now },
		NewID: func(string) string {
			id := ids[0]
			ids = ids[1:]
			return id
		},
	}
	if err := handlerForTest(t, messaging.InventoryReserveQueue, deps).HandleEvent(context.Background(), service.OutboxEvents()[0]); err != nil {
		t.Fatalf("inventory handler returned error: %v", err)
	}
	if err := handlerForTest(t, messaging.PaymentsAuthorizeQueue, deps).HandleEvent(context.Background(), lastOutboxEvent(service)); err != nil {
		t.Fatalf("payment handler returned error: %v", err)
	}
	if _, err := service.CancelOrder(order.OrderID, "cor_cancel", commerce.AuditActor{
		Type:   "merchant_user",
		ID:     "usr_93842",
		Reason: "customer_requested",
	}); err != nil {
		t.Fatalf("CancelOrder returned error: %v", err)
	}
	cancelRequested := lastOutboxEvent(service)

	if err := handlerForTest(t, messaging.OrdersCancelQueue, deps).HandleEvent(context.Background(), cancelRequested); err != nil {
		t.Fatalf("cancellation handler returned error: %v", err)
	}

	cancelled, err := store.GetOrder(context.Background(), order.OrderID)
	if err != nil {
		t.Fatalf("GetOrder returned error: %v", err)
	}
	if cancelled.Status != commerce.StatusCancelled {
		t.Fatalf("order status = %q, want cancelled", cancelled.Status)
	}
	if cancelled.Items[0].ReservationStatus != "released" {
		t.Fatalf("reservation status = %q, want released", cancelled.Items[0].ReservationStatus)
	}
	if cancelled.Payment == nil || cancelled.Payment.Status != "voided" {
		t.Fatalf("payment projection = %+v, want voided", cancelled.Payment)
	}
	cancelledEvent := lastOutboxEvent(service)
	if cancelledEvent.EventType != "order.cancelled" {
		t.Fatalf("last event type = %q, want order.cancelled", cancelledEvent.EventType)
	}
	last := service.AuditLogs()[len(service.AuditLogs())-1]
	if last.Details["stock_release"] != "requested" || last.Details["payment_void"] != "requested" {
		t.Fatalf("cancellation audit details = %+v, want stock release and payment void", last.Details)
	}
}

func TestCancellationHandlerRoutesCreatedShipmentToManualReview(t *testing.T) {
	store := commerce.NewMemoryStore()
	service := commerce.NewService(store)
	order, _, err := service.CreateOrder("mer_demo", "idem-key-0001", "cor_1", validCreateOrderCommand())
	if err != nil {
		t.Fatalf("CreateOrder returned error: %v", err)
	}
	created := service.OutboxEvents()[0]
	advanceOrderStatusForTest(t, store, order, commerce.StatusInventoryReserved, created.CorrelationID)
	advanceOrderStatusForTest(t, store, order, commerce.StatusPaymentAuthorized, created.CorrelationID)
	shipmentCreated := commerce.OutboxEvent{
		MessageID:     "msg_shipment_created",
		CorrelationID: created.CorrelationID,
		CausationID:   created.MessageID,
		EventType:     "shipment.created",
		OrderID:       order.OrderID,
		MerchantID:    order.MerchantID,
		OccurredAt:    time.Date(2026, 5, 29, 13, 25, 0, 0, time.UTC),
	}
	if err := store.RecordShipmentCreated(context.Background(), created, shipmentCreated, commerce.Shipment{
		ShipmentID:     "shp_manual_review",
		Status:         "created",
		Carrier:        "fake-carrier",
		TrackingNumber: "TRACK-manual-review",
	}, commerce.AuditLog{
		MerchantID:    order.MerchantID,
		OrderID:       order.OrderID,
		ActorType:     "system",
		ActorID:       "fulfillment-worker",
		Action:        "shipment.created",
		CorrelationID: created.CorrelationID,
		CreatedAt:     shipmentCreated.OccurredAt,
	}); err != nil {
		t.Fatalf("RecordShipmentCreated returned error: %v", err)
	}
	if _, err := service.CancelOrder(order.OrderID, "cor_cancel", commerce.AuditActor{
		Type:   "merchant_user",
		ID:     "usr_93842",
		Reason: "customer_requested",
	}); err != nil {
		t.Fatalf("CancelOrder returned error: %v", err)
	}
	cancelRequested := lastOutboxEvent(service)
	ids := []string{"msg_manual_review"}
	now := time.Date(2026, 5, 29, 13, 30, 0, 0, time.UTC)
	handler := handlerForTest(t, messaging.OrdersCancelQueue, Dependencies{
		Projector: store,
		Orders:    store,
		Clock:     func() time.Time { return now },
		NewID: func(string) string {
			id := ids[0]
			ids = ids[1:]
			return id
		},
	})

	if err := handler.HandleEvent(context.Background(), cancelRequested); err != nil {
		t.Fatalf("cancellation handler returned error: %v", err)
	}

	reviewed, err := store.GetOrder(context.Background(), order.OrderID)
	if err != nil {
		t.Fatalf("GetOrder returned error: %v", err)
	}
	if reviewed.Status != commerce.StatusManualReview {
		t.Fatalf("order status = %q, want manual_review", reviewed.Status)
	}
	reviewEvent := lastOutboxEvent(service)
	if reviewEvent.EventType != "order.manual_review_required" {
		t.Fatalf("last event type = %q, want order.manual_review_required", reviewEvent.EventType)
	}
	if reviewEvent.CausationID != cancelRequested.MessageID {
		t.Fatalf("manual review causation id = %q, want %q", reviewEvent.CausationID, cancelRequested.MessageID)
	}
	last := service.AuditLogs()[len(service.AuditLogs())-1]
	if last.Action != "order.manual_review_required" {
		t.Fatalf("last audit action = %q, want order.manual_review_required", last.Action)
	}
	if last.Details["review_reason"] != "shipment_already_created" || last.Details["shipment_id"] != "shp_manual_review" {
		t.Fatalf("manual review audit details = %+v, want shipment reason", last.Details)
	}
}

func TestOrderFinalizerDoesNotCompleteCancellationPendingOrder(t *testing.T) {
	store := commerce.NewMemoryStore()
	service := commerce.NewService(store)
	order, _, err := service.CreateOrder("mer_demo", "idem-key-0001", "cor_1", validCreateOrderCommand())
	if err != nil {
		t.Fatalf("CreateOrder returned error: %v", err)
	}
	created := service.OutboxEvents()[0]
	advanceOrderStatusForTest(t, store, order, commerce.StatusInventoryReserved, created.CorrelationID)
	advanceOrderStatusForTest(t, store, order, commerce.StatusPaymentAuthorized, created.CorrelationID)
	shipmentCreated := commerce.OutboxEvent{
		MessageID:     "msg_shipment_created",
		CorrelationID: created.CorrelationID,
		CausationID:   created.MessageID,
		EventType:     "shipment.created",
		OrderID:       order.OrderID,
		MerchantID:    order.MerchantID,
		OccurredAt:    time.Date(2026, 5, 29, 13, 25, 0, 0, time.UTC),
	}
	if err := store.RecordShipmentCreated(context.Background(), created, shipmentCreated, commerce.Shipment{
		ShipmentID:     "shp_cancel_race",
		Status:         "created",
		Carrier:        "fake-carrier",
		TrackingNumber: "TRACK-cancel-race",
	}, commerce.AuditLog{
		MerchantID:    order.MerchantID,
		OrderID:       order.OrderID,
		ActorType:     "system",
		ActorID:       "fulfillment-worker",
		Action:        "shipment.created",
		CorrelationID: created.CorrelationID,
		CreatedAt:     shipmentCreated.OccurredAt,
	}); err != nil {
		t.Fatalf("RecordShipmentCreated returned error: %v", err)
	}
	if _, err := service.CancelOrder(order.OrderID, "cor_cancel", commerce.AuditActor{
		Type:   "merchant_user",
		ID:     "usr_93842",
		Reason: "customer_requested",
	}); err != nil {
		t.Fatalf("CancelOrder returned error: %v", err)
	}
	eventsBefore := len(service.OutboxEvents())
	finalizer := handlerForTest(t, messaging.OrdersFinalizeQueue, Dependencies{
		Orders: store,
		Clock: func() time.Time {
			return time.Date(2026, 5, 29, 13, 30, 0, 0, time.UTC)
		},
	})

	if err := finalizer.HandleEvent(context.Background(), shipmentCreated); err != nil {
		t.Fatalf("finalizer handler returned error: %v", err)
	}

	eventsAfter := service.OutboxEvents()
	if len(eventsAfter) != eventsBefore {
		t.Fatalf("outbox events = %d, want unchanged %d", len(eventsAfter), eventsBefore)
	}
	cancelled, err := store.GetOrder(context.Background(), order.OrderID)
	if err != nil {
		t.Fatalf("GetOrder returned error: %v", err)
	}
	if cancelled.Status != commerce.StatusCancellationPending {
		t.Fatalf("order status = %q, want cancellation_pending", cancelled.Status)
	}
}

func TestCompensationHandlerRecordsTargetStatus(t *testing.T) {
	store := commerce.NewMemoryStore()
	service := commerce.NewService(store)
	order, _, err := service.CreateOrder("mer_demo", "idem-key-0001", "cor_1", validCreateOrderCommand())
	if err != nil {
		t.Fatalf("CreateOrder returned error: %v", err)
	}
	handler := handlerForTest(t, messaging.OrdersCompensateQueue, Dependencies{
		Projector: store,
		Clock: func() time.Time {
			return time.Date(2026, 5, 29, 14, 0, 0, 0, time.UTC)
		},
	})

	if err := handler.HandleEvent(context.Background(), commerce.OutboxEvent{
		MessageID:     "msg_inventory_rejected",
		CorrelationID: "cor_1",
		CausationID:   "msg_created",
		EventType:     "inventory.rejected",
		OrderID:       order.OrderID,
		MerchantID:    order.MerchantID,
	}); err != nil {
		t.Fatalf("compensation handler returned error: %v", err)
	}

	compensated, err := store.GetOrder(context.Background(), order.OrderID)
	if err != nil {
		t.Fatalf("GetOrder returned error: %v", err)
	}
	if compensated.Status != commerce.StatusFailed {
		t.Fatalf("order status = %q, want failed", compensated.Status)
	}
	logs := service.AuditLogs()
	last := logs[len(logs)-1]
	if last.Action != "compensation.inventory_rejected" {
		t.Fatalf("last audit action = %q, want compensation.inventory_rejected", last.Action)
	}
	if last.Details["target_order_status"] != string(commerce.StatusFailed) {
		t.Fatalf("target status detail = %q, want failed", last.Details["target_order_status"])
	}
}

func TestCompensationHandlerReleasesStockAndVoidsPayment(t *testing.T) {
	store := commerce.NewMemoryStore()
	service := commerce.NewService(store)
	order, _, err := service.CreateOrder("mer_demo", "idem-key-0001", "cor_1", validCreateOrderCommand())
	if err != nil {
		t.Fatalf("CreateOrder returned error: %v", err)
	}
	ids := []string{"msg_inventory", "msg_payment", "pay_authorized"}
	now := time.Date(2026, 5, 29, 14, 30, 0, 0, time.UTC)
	deps := Dependencies{
		Projector: store,
		Orders:    store,
		Clock:     func() time.Time { return now },
		NewID: func(string) string {
			id := ids[0]
			ids = ids[1:]
			return id
		},
	}
	if err := handlerForTest(t, messaging.InventoryReserveQueue, deps).HandleEvent(context.Background(), service.OutboxEvents()[0]); err != nil {
		t.Fatalf("inventory handler returned error: %v", err)
	}
	if err := handlerForTest(t, messaging.PaymentsAuthorizeQueue, deps).HandleEvent(context.Background(), lastOutboxEvent(service)); err != nil {
		t.Fatalf("payment handler returned error: %v", err)
	}
	compensation := handlerForTest(t, messaging.OrdersCompensateQueue, Dependencies{
		Projector: store,
		Clock:     func() time.Time { return now.Add(time.Minute) },
	})

	if err := compensation.HandleEvent(context.Background(), commerce.OutboxEvent{
		MessageID:     "msg_shipment_failed",
		CorrelationID: "cor_1",
		CausationID:   lastOutboxEvent(service).MessageID,
		EventType:     "shipment.failed",
		OrderID:       order.OrderID,
		MerchantID:    order.MerchantID,
	}); err != nil {
		t.Fatalf("compensation handler returned error: %v", err)
	}

	compensated, err := store.GetOrder(context.Background(), order.OrderID)
	if err != nil {
		t.Fatalf("GetOrder returned error: %v", err)
	}
	if compensated.Status != commerce.StatusCancellationPending {
		t.Fatalf("order status = %q, want cancellation_pending", compensated.Status)
	}
	if compensated.Items[0].ReservationStatus != "released" {
		t.Fatalf("reservation status = %q, want released", compensated.Items[0].ReservationStatus)
	}
	if compensated.Payment == nil || compensated.Payment.Status != "voided" {
		t.Fatalf("payment projection = %+v, want voided payment", compensated.Payment)
	}
	last := service.AuditLogs()[len(service.AuditLogs())-1]
	if last.Details["stock_release"] != "requested" || last.Details["payment_void"] != "requested" {
		t.Fatalf("compensation audit details = %+v, want stock release and payment void", last.Details)
	}
}

func handlerForTest(t testing.TB, queue string, deps Dependencies) messaging.EventHandler {
	t.Helper()
	handler, err := HandlerForQueue(queue, deps)
	if err != nil {
		t.Fatalf("HandlerForQueue returned error: %v", err)
	}
	return handler
}

func eventTypes(events []commerce.OutboxEvent) []string {
	types := make([]string, len(events))
	for idx, event := range events {
		types[idx] = event.EventType
	}
	return types
}

func lastOutboxEvent(service *commerce.Service) commerce.OutboxEvent {
	events := service.OutboxEvents()
	return events[len(events)-1]
}

func assertCausationChain(t testing.TB, events []commerce.OutboxEvent) {
	t.Helper()
	if len(events) == 0 {
		t.Fatal("causation chain requires at least one event")
	}
	if events[0].CausationID != events[0].MessageID {
		t.Fatalf("root causation id = %q, want message id %q", events[0].CausationID, events[0].MessageID)
	}
	for idx := 1; idx < len(events); idx++ {
		if events[idx].CausationID != events[idx-1].MessageID {
			t.Fatalf("event %s causation id = %q, want previous message id %q", events[idx].EventType, events[idx].CausationID, events[idx-1].MessageID)
		}
	}
}

func assertOrderStatus(t testing.TB, store *commerce.MemoryStore, orderID string, want commerce.OrderStatus) {
	t.Helper()
	order, err := store.GetOrder(context.Background(), orderID)
	if err != nil {
		t.Fatalf("GetOrder returned error: %v", err)
	}
	if order.Status != want {
		t.Fatalf("order status = %q, want %q", order.Status, want)
	}
}

func advanceOrderStatusForTest(t testing.TB, store *commerce.MemoryStore, order *commerce.Order, status commerce.OrderStatus, correlationID string) {
	t.Helper()
	now := time.Date(2026, 5, 29, 13, 20, 0, 0, time.UTC)
	event := commerce.OutboxEvent{
		MessageID:     "msg_" + string(status),
		CorrelationID: correlationID,
		CausationID:   "msg_test_causation",
		EventType:     "test.status_advanced",
		OrderID:       order.OrderID,
		MerchantID:    order.MerchantID,
		OccurredAt:    now,
	}
	audit := commerce.AuditLog{
		MerchantID:    order.MerchantID,
		OrderID:       order.OrderID,
		ActorType:     "test",
		ActorID:       "test",
		Action:        "test.status_advanced",
		CorrelationID: correlationID,
		CreatedAt:     now,
	}
	if _, err := store.UpdateOrderStatus(context.Background(), order.OrderID, status, now, event, audit); err != nil {
		t.Fatalf("advance order to %s: %v", status, err)
	}
}

type insufficientStockProjector struct {
	*commerce.MemoryStore
}

func (p insufficientStockProjector) RecordInventoryReserved(context.Context, commerce.OutboxEvent, commerce.OutboxEvent, commerce.AuditLog) error {
	return commerce.ErrInsufficientStock
}

func validCreateOrderCommand() commerce.CreateOrderCommand {
	return commerce.CreateOrderCommand{
		ExternalOrderID: "web-100045",
		Currency:        "USD",
		Customer: commerce.CustomerInput{
			ID:       "cus_23901",
			Email:    "samira@example.com",
			FullName: "Samira Costa",
		},
		ShippingAddress: commerce.AddressInput{
			Line1:      "55 Market Street",
			City:       "San Francisco",
			State:      "CA",
			PostalCode: "94105",
			Country:    "US",
		},
		Items: []commerce.OrderItemInput{
			{
				SKU:      "SKU-CHAIR-BLK",
				Quantity: 1,
				UnitPrice: commerce.Money{
					Amount:   18900,
					Currency: "USD",
				},
			},
		},
		PaymentMethod: commerce.PaymentMethodInput{
			Provider:     "stripe",
			PaymentToken: "tok_visa_01hzsample",
		},
	}
}
