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

	if got := eventTypes(service.OutboxEvents()); len(got) != 5 || got[1] != "inventory.reserved" || got[2] != "payment.authorized" || got[3] != "shipment.created" || got[4] != "order.completed" {
		t.Fatalf("outbox event types = %v, want transactional fulfillment progression", got)
	}
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
		EventType:     "order.created",
		OrderID:       "ord_1",
		MerchantID:    "mer_1",
	})
	if err == nil {
		t.Fatal("handler must reject unexpected event type")
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
