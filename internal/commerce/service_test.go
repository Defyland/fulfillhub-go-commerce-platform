package commerce

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestCreateOrderDerivesMerchantAndWritesOutbox(t *testing.T) {
	service := NewService(NewMemoryStore())

	order, replayed, err := service.CreateOrder("mer_demo", "idem-key-0001", "cor_1", validCreateOrderRequest())
	if err != nil {
		t.Fatalf("CreateOrder returned error: %v", err)
	}
	if replayed {
		t.Fatal("first request must not be marked as idempotent replay")
	}
	if order.MerchantID != "mer_demo" {
		t.Fatalf("merchant id = %q, want derived merchant", order.MerchantID)
	}
	if order.Totals.Total.Amount != 20100 {
		t.Fatalf("total amount = %d, want 20100", order.Totals.Total.Amount)
	}

	events := service.OutboxEvents()
	if len(events) != 1 {
		t.Fatalf("outbox events = %d, want 1", len(events))
	}
	if events[0].EventType != "order.created" {
		t.Fatalf("event type = %q, want order.created", events[0].EventType)
	}
	if events[0].CausationID != events[0].MessageID {
		t.Fatalf("root causation id = %q, want message id %q", events[0].CausationID, events[0].MessageID)
	}
	logs := service.AuditLogs()
	if len(logs) != 1 {
		t.Fatalf("audit logs = %d, want 1", len(logs))
	}
	if logs[0].Action != "order.create" {
		t.Fatalf("audit action = %q, want order.create", logs[0].Action)
	}
	if logs[0].ActorID != "mer_demo" || logs[0].ActorType != "merchant" {
		t.Fatalf("audit actor = %s/%s, want merchant/mer_demo", logs[0].ActorType, logs[0].ActorID)
	}
}

func TestCreateOrderIdempotencyReturnsExistingOrder(t *testing.T) {
	service := NewService(NewMemoryStore())

	first, _, err := service.CreateOrder("mer_demo", "idem-key-0001", "cor_1", validCreateOrderRequest())
	if err != nil {
		t.Fatalf("first CreateOrder returned error: %v", err)
	}
	second, replayed, err := service.CreateOrder("mer_demo", "idem-key-0001", "cor_2", validCreateOrderRequest())
	if err != nil {
		t.Fatalf("second CreateOrder returned error: %v", err)
	}
	if !replayed {
		t.Fatal("second request with same idempotency key must be marked as replay")
	}
	if second.OrderID != first.OrderID {
		t.Fatalf("replayed order id = %q, want %q", second.OrderID, first.OrderID)
	}
	if got := len(service.OutboxEvents()); got != 1 {
		t.Fatalf("outbox events after replay = %d, want 1", got)
	}
	if got := len(service.AuditLogs()); got != 1 {
		t.Fatalf("audit logs after replay = %d, want 1", got)
	}
}

func TestCancelOrderWritesAuditLog(t *testing.T) {
	service := NewService(NewMemoryStore())
	order, _, err := service.CreateOrder("mer_demo", "idem-key-0001", "cor_1", validCreateOrderRequest())
	if err != nil {
		t.Fatalf("CreateOrder returned error: %v", err)
	}

	cancelled, err := service.CancelOrder(order.OrderID, "cor_cancel", AuditActor{
		Type:   "merchant_user",
		ID:     "usr_93842",
		Reason: "customer_requested",
	})
	if err != nil {
		t.Fatalf("CancelOrder returned error: %v", err)
	}
	if cancelled.Status != StatusCancellationPending {
		t.Fatalf("status = %q, want cancellation_pending", cancelled.Status)
	}
	logs := service.AuditLogs()
	if len(logs) != 2 {
		t.Fatalf("audit logs = %d, want 2", len(logs))
	}
	cancelLog := logs[1]
	if cancelLog.Action != "order.cancel_requested" {
		t.Fatalf("cancel audit action = %q, want order.cancel_requested", cancelLog.Action)
	}
	if cancelLog.ActorType != "merchant_user" || cancelLog.ActorID != "usr_93842" {
		t.Fatalf("cancel audit actor = %s/%s, want merchant_user/usr_93842", cancelLog.ActorType, cancelLog.ActorID)
	}
	if cancelLog.CorrelationID != "cor_cancel" {
		t.Fatalf("cancel audit correlation = %q, want cor_cancel", cancelLog.CorrelationID)
	}
	if cancelLog.Details["reason"] != "customer_requested" {
		t.Fatalf("cancel audit reason = %q, want customer_requested", cancelLog.Details["reason"])
	}
	events := service.OutboxEvents()
	if events[1].CausationID != events[1].MessageID {
		t.Fatalf("cancel causation id = %q, want message id %q", events[1].CausationID, events[1].MessageID)
	}
}

func TestCreateOrderRejectsDuplicateExternalOrderID(t *testing.T) {
	service := NewService(NewMemoryStore())

	if _, _, err := service.CreateOrder("mer_demo", "idem-key-0001", "cor_1", validCreateOrderRequest()); err != nil {
		t.Fatalf("first CreateOrder returned error: %v", err)
	}
	_, _, err := service.CreateOrder("mer_demo", "idem-key-0002", "cor_2", validCreateOrderRequest())
	if !errors.Is(err, ErrDuplicateOrder) {
		t.Fatalf("error = %v, want ErrDuplicateOrder", err)
	}
}

func TestServicePassesContextToStore(t *testing.T) {
	store := &contextCapturingStore{}
	service := NewService(store)
	ctx := context.WithValue(context.Background(), contextKey("request_id"), "req_1")

	if _, _, err := service.CreateOrderContext(ctx, "mer_demo", "idem-key-0001", "cor_1", validCreateOrderRequest()); err != nil {
		t.Fatalf("CreateOrderContext returned error: %v", err)
	}

	if got := store.value; got != "req_1" {
		t.Fatalf("store context value = %v, want req_1", got)
	}
}

func TestCreateOrderValidatesRequest(t *testing.T) {
	service := NewService(NewMemoryStore())
	req := validCreateOrderRequest()
	req.Items[0].Quantity = 0

	_, _, err := service.CreateOrder("mer_demo", "short", "cor_1", req)
	var validation ValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("error = %v, want ValidationError", err)
	}
	if len(validation.Fields) != 2 {
		t.Fatalf("validation fields = %d, want 2", len(validation.Fields))
	}
}

type contextKey string

type contextCapturingStore struct {
	value any
}

func (s *contextCapturingStore) InsertOrder(ctx context.Context, _ string, _ string, order *Order, _ OutboxEvent, _ AuditLog) (*Order, bool, error) {
	s.value = ctx.Value(contextKey("request_id"))
	return CloneOrderForStore(order), false, nil
}

func (s *contextCapturingStore) GetOrder(context.Context, string) (*Order, error) {
	return nil, ErrNotFound
}

func (s *contextCapturingStore) GetShipment(context.Context, string) (*ShipmentRecord, error) {
	return nil, ErrNotFound
}

func (s *contextCapturingStore) UpdateOrderStatus(context.Context, string, OrderStatus, time.Time, OutboxEvent, AuditLog) (*Order, error) {
	return nil, ErrNotFound
}

func (s *contextCapturingStore) OutboxEvents() []OutboxEvent {
	return nil
}

func (s *contextCapturingStore) AuditLogs() []AuditLog {
	return nil
}

func validCreateOrderRequest() CreateOrderRequest {
	return CreateOrderRequest{
		ExternalOrderID: "web-100045",
		Currency:        "USD",
		Customer: Customer{
			ID:       "cus_23901",
			Email:    "samira@example.com",
			FullName: "Samira Costa",
		},
		ShippingAddress: Address{
			Line1:      "55 Market Street",
			City:       "San Francisco",
			State:      "CA",
			PostalCode: "94105",
			Country:    "US",
		},
		Items: []OrderItemRequest{
			{
				SKU:      "SKU-CHAIR-BLK",
				Quantity: 1,
				UnitPrice: Money{
					Amount:   18900,
					Currency: "USD",
				},
			},
		},
		PaymentMethod: PaymentMethod{
			Provider:     "stripe",
			PaymentToken: "tok_visa_01hzsample",
		},
	}
}
