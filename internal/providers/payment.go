package providers

import (
	"context"
	"fmt"
)

type PaymentRequest struct {
	OrderID              string
	MerchantID           string
	Amount               int64
	Currency             string
	PaymentCredentialRef string
}

type PaymentAuthorization struct {
	AuthorizationID string
	Status          string
}

type PaymentProvider interface {
	Authorize(ctx context.Context, request PaymentRequest) (PaymentAuthorization, error)
	Void(ctx context.Context, authorizationID string) error
}

type FakePaymentProvider struct{}

func (FakePaymentProvider) Authorize(_ context.Context, request PaymentRequest) (PaymentAuthorization, error) {
	if request.PaymentCredentialRef == "" {
		return PaymentAuthorization{}, fmt.Errorf("payment credential reference is required")
	}
	return PaymentAuthorization{
		AuthorizationID: "pay_fake_" + request.OrderID,
		Status:          "authorized",
	}, nil
}

func (FakePaymentProvider) Void(_ context.Context, authorizationID string) error {
	if authorizationID == "" {
		return fmt.Errorf("authorization id is required")
	}
	return nil
}
