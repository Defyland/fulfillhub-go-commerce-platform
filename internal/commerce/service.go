package commerce

import (
	"context"
	cryptorand "crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"
)

var (
	ErrDuplicateOrder         = errors.New("duplicate order")
	ErrInvalidStateTransition = errors.New("invalid state transition")
	ErrNotFound               = errors.New("not found")
	ErrInsufficientStock      = errors.New("insufficient stock")
)

type FieldError struct {
	Field string
	Issue string
}

type ValidationError struct {
	Fields []FieldError
}

func (e ValidationError) Error() string {
	return "validation failed"
}

type Service struct {
	store   Store
	counter atomic.Uint64
	clock   func() time.Time
}

func NewService(store Store) *Service {
	return &Service{
		store: store,
		clock: func() time.Time {
			return time.Now().UTC()
		},
	}
}

func (s *Service) CreateOrder(merchantID, idempotencyKey, correlationID string, req CreateOrderCommand) (*Order, bool, error) {
	return s.CreateOrderContext(context.Background(), merchantID, idempotencyKey, correlationID, req)
}

func (s *Service) CreateOrderContext(ctx context.Context, merchantID, idempotencyKey, correlationID string, req CreateOrderCommand) (*Order, bool, error) {
	if err := validateCreateOrder(merchantID, idempotencyKey, req); err != nil {
		return nil, false, err
	}

	now := s.clock()
	shipping := Money{Amount: 1200, Currency: req.Currency}
	subtotal := Money{Currency: req.Currency}
	items := make([]OrderItem, 0, len(req.Items))

	for _, item := range req.Items {
		subtotal.Amount += int64(item.Quantity) * item.UnitPrice.Amount
		items = append(items, OrderItem{
			SKU:               item.SKU,
			Quantity:          item.Quantity,
			UnitPrice:         item.UnitPrice,
			ReservationStatus: "pending",
		})
	}

	order := &Order{
		OrderID:         s.nextID("ord"),
		MerchantID:      merchantID,
		ExternalOrderID: req.ExternalOrderID,
		Status:          StatusPendingFulfillment,
		Currency:        req.Currency,
		Totals: OrderTotals{
			Subtotal: subtotal,
			Shipping: shipping,
			Total: Money{
				Amount:   subtotal.Amount + shipping.Amount,
				Currency: req.Currency,
			},
		},
		Items: items,
		Payment: &Payment{
			Provider:      req.PaymentMethod.Provider,
			Status:        "pending_authorization",
			CredentialRef: opaqueReference("paycred", req.PaymentMethod.PaymentToken),
		},
		ShippingAddressRef: opaqueReference("addr", addressFingerprint(req.ShippingAddress)),
		CreatedAt:          now,
		UpdatedAt:          now,
	}

	messageID := s.nextID("msg")
	event := OutboxEvent{
		MessageID:     messageID,
		SchemaVersion: EventSchemaVersion,
		Producer:      ProducerForEventType("order.created"),
		CorrelationID: correlationID,
		CausationID:   messageID,
		EventType:     "order.created",
		OrderID:       order.OrderID,
		MerchantID:    merchantID,
		OccurredAt:    now,
		Payload: map[string]any{
			"external_order_id": order.ExternalOrderID,
			"order_status":      string(order.Status),
			"currency":          order.Currency,
			"total_amount":      order.Totals.Total.Amount,
			"item_count":        len(order.Items),
		},
	}

	audit := AuditLog{
		MerchantID:    merchantID,
		OrderID:       order.OrderID,
		ActorType:     "merchant",
		ActorID:       merchantID,
		Action:        "order.create",
		CorrelationID: correlationID,
		CreatedAt:     now,
	}

	return s.store.InsertOrder(ctx, merchantID, idempotencyKey, order, event, audit)
}

func (s *Service) GetOrder(orderID string) (*Order, error) {
	return s.GetOrderContext(context.Background(), orderID)
}

func (s *Service) CancelOrder(orderID, correlationID string, actor AuditActor) (*Order, error) {
	return s.CancelOrderContext(context.Background(), orderID, correlationID, actor)
}

func (s *Service) GetOrderContext(ctx context.Context, orderID string) (*Order, error) {
	return s.store.GetOrder(ctx, orderID)
}

func (s *Service) GetShipmentContext(ctx context.Context, shipmentID string) (*ShipmentRecord, error) {
	return s.store.GetShipment(ctx, shipmentID)
}

func (s *Service) CancelOrderContext(ctx context.Context, orderID, correlationID string, actor AuditActor) (*Order, error) {
	if err := validateCancelActor(actor); err != nil {
		return nil, err
	}
	order, err := s.store.GetOrder(ctx, orderID)
	if err != nil {
		return nil, err
	}
	if order.Status == StatusCompleted || order.Status == StatusCancelled {
		return nil, ErrInvalidStateTransition
	}
	if err := ValidateOrderTransition(order.Status, StatusCancellationPending); err != nil {
		return nil, err
	}

	now := s.clock()
	messageID := s.nextID("msg")
	event := OutboxEvent{
		MessageID:     messageID,
		SchemaVersion: EventSchemaVersion,
		Producer:      ProducerForEventType("order.cancel_requested"),
		CorrelationID: correlationID,
		CausationID:   messageID,
		EventType:     "order.cancel_requested",
		OrderID:       orderID,
		MerchantID:    order.MerchantID,
		OccurredAt:    now,
		Payload: map[string]any{
			"order_status": string(StatusCancellationPending),
			"reason":       strings.TrimSpace(actor.Reason),
			"requested_by": map[string]any{
				"type": strings.TrimSpace(actor.Type),
				"id":   strings.TrimSpace(actor.ID),
			},
		},
	}
	audit := AuditLog{
		MerchantID:    order.MerchantID,
		OrderID:       orderID,
		ActorType:     strings.TrimSpace(actor.Type),
		ActorID:       strings.TrimSpace(actor.ID),
		Action:        "order.cancel_requested",
		CorrelationID: correlationID,
		CreatedAt:     now,
		Details: map[string]string{
			"reason": strings.TrimSpace(actor.Reason),
		},
	}

	return s.store.UpdateOrderStatus(ctx, orderID, StatusCancellationPending, now, event, audit)
}

func (s *Service) OutboxEvents() []OutboxEvent {
	return s.store.OutboxEvents()
}

func (s *Service) AuditLogs() []AuditLog {
	return s.store.AuditLogs()
}

func (s *Service) nextID(prefix string) string {
	var bytes [16]byte
	if _, err := cryptorand.Read(bytes[:]); err == nil {
		return fmt.Sprintf("%s_%s", prefix, hex.EncodeToString(bytes[:]))
	}
	return fmt.Sprintf("%s_%d_%012d", prefix, s.clock().UnixNano(), s.counter.Add(1))
}

func opaqueReference(prefix, value string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(value)))
	return fmt.Sprintf("%s_%s", prefix, hex.EncodeToString(sum[:])[:24])
}

func addressFingerprint(address AddressInput) string {
	parts := []string{
		address.Line1,
		address.Line2,
		address.City,
		address.State,
		address.PostalCode,
		address.Country,
	}
	return strings.ToLower(strings.Join(parts, "|"))
}

func validateCreateOrder(merchantID, idempotencyKey string, req CreateOrderCommand) error {
	var fields []FieldError
	if strings.TrimSpace(merchantID) == "" {
		fields = append(fields, FieldError{Field: "merchant_id", Issue: "is required from authentication context"})
	}
	if len(strings.TrimSpace(idempotencyKey)) < 12 {
		fields = append(fields, FieldError{Field: "Idempotency-Key", Issue: "must contain at least 12 characters"})
	}
	if strings.TrimSpace(req.ExternalOrderID) == "" {
		fields = append(fields, FieldError{Field: "external_order_id", Issue: "is required"})
	}
	if len(req.Currency) != 3 {
		fields = append(fields, FieldError{Field: "currency", Issue: "must be a three-letter ISO currency code"})
	}
	if strings.TrimSpace(req.Customer.Email) == "" {
		fields = append(fields, FieldError{Field: "customer.email", Issue: "is required"})
	}
	if strings.TrimSpace(req.ShippingAddress.Country) == "" {
		fields = append(fields, FieldError{Field: "shipping_address.country", Issue: "is required"})
	}
	if len(req.Items) == 0 {
		fields = append(fields, FieldError{Field: "items", Issue: "must contain at least one item"})
	}
	seenSKUs := map[string]int{}
	for idx, item := range req.Items {
		sku := strings.TrimSpace(item.SKU)
		if sku == "" {
			fields = append(fields, FieldError{Field: fmt.Sprintf("items[%d].sku", idx), Issue: "is required"})
		} else if firstIdx, ok := seenSKUs[sku]; ok {
			fields = append(fields, FieldError{Field: fmt.Sprintf("items[%d].sku", idx), Issue: fmt.Sprintf("duplicates items[%d].sku", firstIdx)})
		} else {
			seenSKUs[sku] = idx
		}
		if item.Quantity < 1 {
			fields = append(fields, FieldError{Field: fmt.Sprintf("items[%d].quantity", idx), Issue: "must be greater than zero"})
		}
		if item.UnitPrice.Amount < 0 {
			fields = append(fields, FieldError{Field: fmt.Sprintf("items[%d].unit_price.amount", idx), Issue: "must not be negative"})
		}
		if item.UnitPrice.Currency != req.Currency {
			fields = append(fields, FieldError{Field: fmt.Sprintf("items[%d].unit_price.currency", idx), Issue: "must match order currency"})
		}
	}
	if strings.TrimSpace(req.PaymentMethod.Provider) == "" {
		fields = append(fields, FieldError{Field: "payment_method.provider", Issue: "is required"})
	}
	if strings.TrimSpace(req.PaymentMethod.PaymentToken) == "" {
		fields = append(fields, FieldError{Field: "payment_method.payment_token", Issue: "is required"})
	}
	if len(fields) > 0 {
		return ValidationError{Fields: fields}
	}
	return nil
}

func validateCancelActor(actor AuditActor) error {
	var fields []FieldError
	if strings.TrimSpace(actor.Reason) == "" {
		fields = append(fields, FieldError{Field: "reason", Issue: "is required"})
	}
	if strings.TrimSpace(actor.Type) == "" {
		fields = append(fields, FieldError{Field: "requested_by.type", Issue: "is required"})
	}
	if strings.TrimSpace(actor.ID) == "" {
		fields = append(fields, FieldError{Field: "requested_by.id", Issue: "is required"})
	}
	if len(fields) > 0 {
		return ValidationError{Fields: fields}
	}
	return nil
}
