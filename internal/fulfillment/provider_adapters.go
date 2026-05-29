package fulfillment

import (
	"context"
	"fmt"
	"time"

	"github.com/Defyland/fulfillhub-go-commerce-platform/internal/commerce"
	"github.com/Defyland/fulfillhub-go-commerce-platform/internal/providers"
)

type OrderReader interface {
	GetOrder(ctx context.Context, orderID string) (*commerce.Order, error)
}

type ProviderPaymentAuthorizer struct {
	Orders   OrderReader
	Provider providers.PaymentProvider
}

func (a ProviderPaymentAuthorizer) AuthorizePayment(ctx context.Context, event commerce.OutboxEvent) (commerce.Payment, error) {
	if a.Orders == nil {
		return commerce.Payment{}, fmt.Errorf("order reader is required")
	}
	if a.Provider == nil {
		return commerce.Payment{}, fmt.Errorf("payment provider is required")
	}
	order, err := a.Orders.GetOrder(ctx, event.OrderID)
	if err != nil {
		return commerce.Payment{}, err
	}
	if order.Payment == nil || order.Payment.CredentialRef == "" {
		return commerce.Payment{}, fmt.Errorf("payment credential reference is required")
	}
	authorization, err := a.Provider.Authorize(ctx, providers.PaymentRequest{
		OrderID:              order.OrderID,
		MerchantID:           order.MerchantID,
		Amount:               order.Totals.Total.Amount,
		Currency:             order.Currency,
		PaymentCredentialRef: order.Payment.CredentialRef,
	})
	if err != nil {
		return commerce.Payment{}, err
	}
	return commerce.Payment{
		Provider:        order.Payment.Provider,
		Status:          authorization.Status,
		AuthorizationID: authorization.AuthorizationID,
		CredentialRef:   order.Payment.CredentialRef,
	}, nil
}

type ProviderShipmentCreator struct {
	Orders   OrderReader
	Provider providers.ShipmentProvider
	Clock    func() time.Time
}

func (c ProviderShipmentCreator) CreateShipment(ctx context.Context, event commerce.OutboxEvent) (commerce.Shipment, error) {
	if c.Orders == nil {
		return commerce.Shipment{}, fmt.Errorf("order reader is required")
	}
	if c.Provider == nil {
		return commerce.Shipment{}, fmt.Errorf("shipment provider is required")
	}
	order, err := c.Orders.GetOrder(ctx, event.OrderID)
	if err != nil {
		return commerce.Shipment{}, err
	}
	if order.ShippingAddressRef == "" {
		return commerce.Shipment{}, fmt.Errorf("shipping address reference is required")
	}
	booking, err := c.Provider.CreateShipment(ctx, providers.ShipmentRequest{
		OrderID:    order.OrderID,
		MerchantID: order.MerchantID,
		AddressID:  order.ShippingAddressRef,
	})
	if err != nil {
		return commerce.Shipment{}, err
	}
	now := time.Now().UTC()
	if c.Clock != nil {
		now = c.Clock()
	}
	return commerce.Shipment{
		ShipmentID:     booking.ShipmentID,
		Status:         "created",
		Carrier:        booking.Carrier,
		TrackingNumber: booking.TrackingNumber,
		Events: []commerce.ShipmentEvent{{
			OccurredAt:  now,
			Status:      "created",
			Description: "Shipment booking created by provider adapter.",
		}},
	}, nil
}
