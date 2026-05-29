package fulfillment

import (
	"context"
	"testing"
	"time"

	"github.com/Defyland/fulfillhub-go-commerce-platform/internal/commerce"
	"github.com/Defyland/fulfillhub-go-commerce-platform/internal/providers"
)

func TestProviderPaymentAuthorizerUsesPersistedCredentialReference(t *testing.T) {
	store := commerce.NewMemoryStore()
	service := commerce.NewService(store)
	order, _, err := service.CreateOrder("mer_demo", "idem-key-0001", "cor_1", validCreateOrderRequest())
	if err != nil {
		t.Fatalf("CreateOrder returned error: %v", err)
	}

	authorizer := ProviderPaymentAuthorizer{
		Orders:   store,
		Provider: providers.FakePaymentProvider{},
	}
	payment, err := authorizer.AuthorizePayment(context.Background(), service.OutboxEvents()[0])
	if err != nil {
		t.Fatalf("AuthorizePayment returned error: %v", err)
	}
	if payment.AuthorizationID != "pay_fake_"+order.OrderID {
		t.Fatalf("authorization id = %q, want fake provider id", payment.AuthorizationID)
	}
	if payment.CredentialRef == "" || payment.CredentialRef == "tok_visa_01hzsample" {
		t.Fatalf("credential ref = %q, want opaque persisted reference", payment.CredentialRef)
	}
}

func TestProviderShipmentCreatorUsesPersistedAddressReference(t *testing.T) {
	store := commerce.NewMemoryStore()
	service := commerce.NewService(store)
	order, _, err := service.CreateOrder("mer_demo", "idem-key-0001", "cor_1", validCreateOrderRequest())
	if err != nil {
		t.Fatalf("CreateOrder returned error: %v", err)
	}
	now := time.Date(2026, 5, 29, 16, 0, 0, 0, time.UTC)
	creator := ProviderShipmentCreator{
		Orders:   store,
		Provider: providers.FakeShipmentProvider{},
		Clock:    func() time.Time { return now },
	}
	shipment, err := creator.CreateShipment(context.Background(), service.OutboxEvents()[0])
	if err != nil {
		t.Fatalf("CreateShipment returned error: %v", err)
	}
	if shipment.ShipmentID != "shp_fake_"+order.OrderID {
		t.Fatalf("shipment id = %q, want fake provider id", shipment.ShipmentID)
	}
	if len(shipment.Events) != 1 || !shipment.Events[0].OccurredAt.Equal(now) {
		t.Fatalf("shipment events = %+v, want adapter timeline at %s", shipment.Events, now)
	}
	if order.ShippingAddressRef == "" {
		t.Fatal("order must persist opaque shipping address reference")
	}
}
