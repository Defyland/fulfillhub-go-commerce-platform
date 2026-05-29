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
	order, _, err := service.CreateOrder("mer_demo", "idem-key-0001", "cor_1", validCreateOrderRequest())
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
	inventoryReserved := lastOutboxEvent(service)
	payment := handlerForTest(t, messaging.PaymentsAuthorizeQueue, deps)
	if err := payment.HandleEvent(context.Background(), inventoryReserved); err != nil {
		t.Fatalf("payment handler returned error: %v", err)
	}
	paymentAuthorized := lastOutboxEvent(service)
	shipment := handlerForTest(t, messaging.ShipmentsCreateQueue, deps)
	if err := shipment.HandleEvent(context.Background(), paymentAuthorized); err != nil {
		t.Fatalf("shipment handler returned error: %v", err)
	}
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
	order, _, err := service.CreateOrder("mer_demo", "idem-key-0001", "cor_1", validCreateOrderRequest())
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
	order, _, err := service.CreateOrder("mer_demo", "idem-key-0001", "cor_1", validCreateOrderRequest())
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
	order, _, err := service.CreateOrder("mer_demo", "idem-key-0001", "cor_1", validCreateOrderRequest())
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
	order, _, err := service.CreateOrder("mer_demo", "idem-key-0001", "cor_1", validCreateOrderRequest())
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
	order, _, err := service.CreateOrder("mer_demo", "idem-key-0001", "cor_1", validCreateOrderRequest())
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
	order, _, err := service.CreateOrder("mer_demo", "idem-key-0001", "cor_1", validCreateOrderRequest())
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

func TestCancellationHandlerFinalizesOrderAndEmitsCancelledEvent(t *testing.T) {
	store := commerce.NewMemoryStore()
	service := commerce.NewService(store)
	order, _, err := service.CreateOrder("mer_demo", "idem-key-0001", "cor_1", validCreateOrderRequest())
	if err != nil {
		t.Fatalf("CreateOrder returned error: %v", err)
	}
	if _, err := service.CancelOrder(order.OrderID, "cor_cancel", commerce.AuditActor{
		Type: "merchant_user",
		ID:   "usr_93842",
	}); err != nil {
		t.Fatalf("CancelOrder returned error: %v", err)
	}
	cancelRequested := lastOutboxEvent(service)
	ids := []string{"msg_cancelled"}
	now := time.Date(2026, 5, 29, 13, 30, 0, 0, time.UTC)
	handler := handlerForTest(t, messaging.OrdersCancelQueue, Dependencies{
		Orders: store,
		Clock:  func() time.Time { return now },
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

func TestCompensationHandlerRecordsTargetStatus(t *testing.T) {
	store := commerce.NewMemoryStore()
	service := commerce.NewService(store)
	order, _, err := service.CreateOrder("mer_demo", "idem-key-0001", "cor_1", validCreateOrderRequest())
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
	order, _, err := service.CreateOrder("mer_demo", "idem-key-0001", "cor_1", validCreateOrderRequest())
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

type insufficientStockProjector struct {
	*commerce.MemoryStore
}

func (p insufficientStockProjector) RecordInventoryReserved(context.Context, commerce.OutboxEvent, commerce.OutboxEvent, commerce.AuditLog) error {
	return commerce.ErrInsufficientStock
}

func validCreateOrderRequest() commerce.CreateOrderRequest {
	return commerce.CreateOrderRequest{
		ExternalOrderID: "web-100045",
		Currency:        "USD",
		Customer: commerce.Customer{
			ID:       "cus_23901",
			Email:    "samira@example.com",
			FullName: "Samira Costa",
		},
		ShippingAddress: commerce.Address{
			Line1:      "55 Market Street",
			City:       "San Francisco",
			State:      "CA",
			PostalCode: "94105",
			Country:    "US",
		},
		Items: []commerce.OrderItemRequest{
			{
				SKU:      "SKU-CHAIR-BLK",
				Quantity: 1,
				UnitPrice: commerce.Money{
					Amount:   18900,
					Currency: "USD",
				},
			},
		},
		PaymentMethod: commerce.PaymentMethod{
			Provider:     "stripe",
			PaymentToken: "tok_visa_01hzsample",
		},
	}
}
