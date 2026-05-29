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
	publisher := &recordingPublisher{}
	ids := []string{"msg_inventory", "msg_payment", "msg_shipment", "msg_completed"}
	now := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	deps := Dependencies{
		Publisher: publisher,
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
	payment := handlerForTest(t, messaging.PaymentsAuthorizeQueue, deps)
	if err := payment.HandleEvent(context.Background(), publisher.events[0]); err != nil {
		t.Fatalf("payment handler returned error: %v", err)
	}
	shipment := handlerForTest(t, messaging.ShipmentsCreateQueue, deps)
	if err := shipment.HandleEvent(context.Background(), publisher.events[1]); err != nil {
		t.Fatalf("shipment handler returned error: %v", err)
	}
	finalizer := handlerForTest(t, messaging.OrdersFinalizeQueue, deps)
	if err := finalizer.HandleEvent(context.Background(), publisher.events[2]); err != nil {
		t.Fatalf("finalizer handler returned error: %v", err)
	}

	if got := eventTypes(publisher.events); len(got) != 3 || got[0] != "inventory.reserved" || got[1] != "payment.authorized" || got[2] != "shipment.created" {
		t.Fatalf("published event types = %v, want inventory/payment/shipment progression", got)
	}
	completed, err := store.GetOrder(context.Background(), order.OrderID)
	if err != nil {
		t.Fatalf("GetOrder returned error: %v", err)
	}
	if completed.Status != commerce.StatusCompleted {
		t.Fatalf("order status = %q, want completed", completed.Status)
	}
	outbox := service.OutboxEvents()
	if got := outbox[len(outbox)-1].EventType; got != "order.completed" {
		t.Fatalf("last outbox event = %q, want order.completed", got)
	}
	logs := service.AuditLogs()
	if got := logs[len(logs)-1].Action; got != "order.completed" {
		t.Fatalf("last audit action = %q, want order.completed", got)
	}
}

func TestHandlerForQueueRejectsUnexpectedEventType(t *testing.T) {
	handler := handlerForTest(t, messaging.PaymentsAuthorizeQueue, Dependencies{
		Publisher: &recordingPublisher{},
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

func handlerForTest(t testing.TB, queue string, deps Dependencies) messaging.EventHandler {
	t.Helper()
	handler, err := HandlerForQueue(queue, deps)
	if err != nil {
		t.Fatalf("HandlerForQueue returned error: %v", err)
	}
	return handler
}

type recordingPublisher struct {
	events []commerce.OutboxEvent
}

func (p *recordingPublisher) Publish(_ context.Context, event commerce.OutboxEvent) error {
	p.events = append(p.events, event)
	return nil
}

func eventTypes(events []commerce.OutboxEvent) []string {
	types := make([]string, len(events))
	for idx, event := range events {
		types[idx] = event.EventType
	}
	return types
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
