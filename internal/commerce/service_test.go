package commerce

import (
	"errors"
	"testing"
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
