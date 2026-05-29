package providers

import (
	"context"
	"testing"
)

func TestFakePaymentProviderAuthorizesAndVoids(t *testing.T) {
	provider := FakePaymentProvider{}
	auth, err := provider.Authorize(context.Background(), PaymentRequest{
		OrderID:              "ord_1",
		MerchantID:           "mer_1",
		Amount:               20100,
		Currency:             "USD",
		PaymentCredentialRef: "paycred_visa",
	})
	if err != nil {
		t.Fatalf("Authorize returned error: %v", err)
	}
	if auth.Status != "authorized" {
		t.Fatalf("status = %q, want authorized", auth.Status)
	}
	if err := provider.Void(context.Background(), auth.AuthorizationID); err != nil {
		t.Fatalf("Void returned error: %v", err)
	}
}

func TestFakeShipmentProviderCreatesBooking(t *testing.T) {
	provider := FakeShipmentProvider{}
	booking, err := provider.CreateShipment(context.Background(), ShipmentRequest{
		OrderID:    "ord_1",
		MerchantID: "mer_1",
		AddressID:  "addr_1",
	})
	if err != nil {
		t.Fatalf("CreateShipment returned error: %v", err)
	}
	if booking.TrackingNumber == "" {
		t.Fatal("tracking number must be present")
	}
}
