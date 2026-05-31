package commerce

import (
	"encoding/json"
	"strings"
	"time"
)

type Money struct {
	Amount   int64  `json:"amount"`
	Currency string `json:"currency"`
}

type Customer struct {
	ID       string `json:"id"`
	Email    string `json:"email"`
	FullName string `json:"full_name"`
}

type Address struct {
	Line1      string `json:"line_1"`
	Line2      string `json:"line_2,omitempty"`
	City       string `json:"city"`
	State      string `json:"state"`
	PostalCode string `json:"postal_code"`
	Country    string `json:"country"`
}

type CustomerInput struct {
	ID       string
	Email    string
	FullName string
}

type AddressInput struct {
	Line1      string
	Line2      string
	City       string
	State      string
	PostalCode string
	Country    string
}

type OrderItemInput struct {
	SKU       string
	Quantity  int
	UnitPrice Money
}

type PaymentMethodInput struct {
	Provider     string
	PaymentToken string
}

type CreateOrderCommand struct {
	ExternalOrderID string
	Currency        string
	Customer        CustomerInput
	ShippingAddress AddressInput
	Items           []OrderItemInput
	PaymentMethod   PaymentMethodInput
}

type OrderStatus string

const (
	StatusPendingFulfillment  OrderStatus = "pending_fulfillment"
	StatusInventoryReserved   OrderStatus = "inventory_reserved"
	StatusPaymentAuthorized   OrderStatus = "payment_authorized"
	StatusShipmentCreated     OrderStatus = "shipment_created"
	StatusCancellationPending OrderStatus = "cancellation_pending"
	StatusManualReview        OrderStatus = "manual_review"
	StatusCancelled           OrderStatus = "cancelled"
	StatusCompleted           OrderStatus = "completed"
	StatusFailed              OrderStatus = "failed"
)

type OrderItem struct {
	SKU               string `json:"sku"`
	Quantity          int    `json:"quantity"`
	UnitPrice         Money  `json:"unit_price"`
	ReservationStatus string `json:"reservation_status"`
}

type OrderTotals struct {
	Subtotal Money `json:"subtotal"`
	Shipping Money `json:"shipping"`
	Total    Money `json:"total"`
}

type Payment struct {
	Provider        string `json:"provider"`
	Status          string `json:"status"`
	AuthorizationID string `json:"authorization_id,omitempty"`
	CredentialRef   string `json:"-"`
}

type Shipment struct {
	ShipmentID     string          `json:"shipment_id"`
	Status         string          `json:"status"`
	Carrier        string          `json:"carrier"`
	TrackingNumber string          `json:"tracking_number"`
	Events         []ShipmentEvent `json:"events,omitempty"`
}

type ShipmentEvent struct {
	OccurredAt  time.Time `json:"occurred_at"`
	Status      string    `json:"status"`
	Description string    `json:"description"`
}

type ShipmentRecord struct {
	ShipmentID     string          `json:"shipment_id"`
	OrderID        string          `json:"order_id"`
	MerchantID     string          `json:"merchant_id"`
	Carrier        string          `json:"carrier"`
	TrackingNumber string          `json:"tracking_number"`
	Status         string          `json:"status"`
	Events         []ShipmentEvent `json:"events"`
}

const EventSchemaVersion = 1

type Order struct {
	OrderID            string      `json:"order_id"`
	MerchantID         string      `json:"merchant_id"`
	ExternalOrderID    string      `json:"external_order_id"`
	Status             OrderStatus `json:"status"`
	Currency           string      `json:"currency"`
	Totals             OrderTotals `json:"totals"`
	Items              []OrderItem `json:"items"`
	Payment            *Payment    `json:"payment,omitempty"`
	Shipment           *Shipment   `json:"shipment,omitempty"`
	ShippingAddressRef string      `json:"-"`
	CreatedAt          time.Time   `json:"created_at"`
	UpdatedAt          time.Time   `json:"updated_at"`
}

type OutboxEvent struct {
	MessageID     string         `json:"message_id"`
	EventType     string         `json:"event_type"`
	SchemaVersion int            `json:"schema_version"`
	OccurredAt    time.Time      `json:"occurred_at"`
	Producer      string         `json:"producer"`
	MerchantID    string         `json:"merchant_id"`
	OrderID       string         `json:"order_id"`
	CorrelationID string         `json:"correlation_id"`
	CausationID   string         `json:"causation_id"`
	Payload       map[string]any `json:"payload"`
}

func (e OutboxEvent) WithDefaultCausation() OutboxEvent {
	if e.CausationID == "" {
		e.CausationID = e.MessageID
	}
	return e
}

func (e OutboxEvent) WithEnvelopeDefaults() OutboxEvent {
	e = e.WithDefaultCausation()
	if e.SchemaVersion == 0 {
		e.SchemaVersion = EventSchemaVersion
	}
	if e.Producer == "" {
		e.Producer = ProducerForEventType(e.EventType)
	}
	if len(e.Payload) == 0 {
		e.Payload = DefaultEventPayload(e)
	}
	return e
}

func (e OutboxEvent) MarshalJSON() ([]byte, error) {
	type alias OutboxEvent
	normalized := e.WithEnvelopeDefaults()
	return json.Marshal(alias(normalized))
}

func ProducerForEventType(eventType string) string {
	switch eventType {
	case "order.created", "order.cancel_requested":
		return "orders-api"
	case "inventory.reserved", "inventory.rejected":
		return "inventory-worker"
	case "payment.authorized", "payment.failed":
		return "payment-worker"
	case "shipment.created", "shipment.failed":
		return "shipment-worker"
	case "order.completed", "order.cancelled", "order.manual_review_required":
		return "orders-worker"
	default:
		return "fulfillhub"
	}
}

func DefaultEventPayload(event OutboxEvent) map[string]any {
	payload := map[string]any{}
	switch event.EventType {
	case "order.created":
		payload["order_status"] = string(StatusPendingFulfillment)
	case "inventory.reserved":
		payload["order_status"] = string(StatusInventoryReserved)
	case "payment.authorized":
		payload["order_status"] = string(StatusPaymentAuthorized)
	case "shipment.created":
		payload["order_status"] = string(StatusShipmentCreated)
	case "order.cancel_requested":
		payload["order_status"] = string(StatusCancellationPending)
	case "order.completed":
		payload["order_status"] = string(StatusCompleted)
		if !event.OccurredAt.IsZero() {
			payload["completed_at"] = event.OccurredAt.UTC().Format(time.RFC3339Nano)
		}
	case "order.cancelled":
		payload["order_status"] = string(StatusCancelled)
		if !event.OccurredAt.IsZero() {
			payload["cancelled_at"] = event.OccurredAt.UTC().Format(time.RFC3339Nano)
		}
	case "order.manual_review_required":
		payload["order_status"] = string(StatusManualReview)
	case "inventory.rejected", "payment.failed", "shipment.failed":
		stage := "unknown"
		switch event.EventType {
		case "inventory.rejected":
			stage = "inventory"
		case "payment.failed":
			stage = "payment"
		case "shipment.failed":
			stage = "shipment"
		}
		payload["failure"] = map[string]any{
			"stage":  stage,
			"reason": "unspecified",
		}
	}
	return payload
}

func OrderStatusEventPayload(status OrderStatus, at time.Time, audit AuditLog, existing map[string]any) map[string]any {
	payload := make(map[string]any, len(existing)+3)
	for key, value := range existing {
		payload[key] = value
	}
	payload["order_status"] = string(status)
	switch status {
	case StatusCancellationPending:
		if _, ok := payload["reason"]; !ok {
			payload["reason"] = strings.TrimSpace(audit.Details["reason"])
		}
		if _, ok := payload["requested_by"]; !ok {
			payload["requested_by"] = map[string]any{
				"type": strings.TrimSpace(audit.ActorType),
				"id":   strings.TrimSpace(audit.ActorID),
			}
		}
	case StatusCompleted:
		if _, ok := payload["completed_at"]; !ok {
			payload["completed_at"] = at.UTC().Format(time.RFC3339Nano)
		}
	case StatusCancelled:
		if _, ok := payload["cancelled_at"]; !ok {
			payload["cancelled_at"] = at.UTC().Format(time.RFC3339Nano)
		}
	case StatusManualReview:
		if _, ok := payload["review_reason"]; !ok {
			if reason := strings.TrimSpace(audit.Details["review_reason"]); reason != "" {
				payload["review_reason"] = reason
			}
		}
	}
	return payload
}
