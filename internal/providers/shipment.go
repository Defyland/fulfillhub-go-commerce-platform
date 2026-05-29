package providers

import (
	"context"
	"fmt"
)

type ShipmentRequest struct {
	OrderID    string
	MerchantID string
	AddressID  string
}

type ShipmentBooking struct {
	ShipmentID     string
	Carrier        string
	TrackingNumber string
}

type ShipmentProvider interface {
	CreateShipment(ctx context.Context, request ShipmentRequest) (ShipmentBooking, error)
}

type FakeShipmentProvider struct{}

func (FakeShipmentProvider) CreateShipment(_ context.Context, request ShipmentRequest) (ShipmentBooking, error) {
	if request.AddressID == "" {
		return ShipmentBooking{}, fmt.Errorf("address id is required")
	}
	return ShipmentBooking{
		ShipmentID:     "shp_fake_" + request.OrderID,
		Carrier:        "fake-carrier",
		TrackingNumber: "TRACK-" + request.OrderID,
	}, nil
}
