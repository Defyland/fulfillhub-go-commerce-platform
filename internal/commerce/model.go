package commerce

import "time"

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

type OrderItemRequest struct {
	SKU       string `json:"sku"`
	Quantity  int    `json:"quantity"`
	UnitPrice Money  `json:"unit_price"`
}

type PaymentMethod struct {
	Provider     string `json:"provider"`
	PaymentToken string `json:"payment_token"`
}

type CreateOrderRequest struct {
	ExternalOrderID string             `json:"external_order_id"`
	Currency        string             `json:"currency"`
	Customer        Customer           `json:"customer"`
	ShippingAddress Address            `json:"shipping_address"`
	Items           []OrderItemRequest `json:"items"`
	PaymentMethod   PaymentMethod      `json:"payment_method"`
}

type OrderStatus string

const (
	StatusPendingFulfillment  OrderStatus = "pending_fulfillment"
	StatusCancellationPending OrderStatus = "cancellation_pending"
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

type Order struct {
	OrderID         string      `json:"order_id"`
	MerchantID      string      `json:"merchant_id"`
	ExternalOrderID string      `json:"external_order_id"`
	Status          OrderStatus `json:"status"`
	Currency        string      `json:"currency"`
	Totals          OrderTotals `json:"totals"`
	Items           []OrderItem `json:"items"`
	Payment         *Payment    `json:"payment,omitempty"`
	Shipment        *Shipment   `json:"shipment,omitempty"`
	CreatedAt       time.Time   `json:"created_at"`
	UpdatedAt       time.Time   `json:"updated_at"`
}

type OutboxEvent struct {
	MessageID     string    `json:"message_id"`
	CorrelationID string    `json:"correlation_id"`
	CausationID   string    `json:"causation_id"`
	EventType     string    `json:"event_type"`
	OrderID       string    `json:"order_id"`
	MerchantID    string    `json:"merchant_id"`
	OccurredAt    time.Time `json:"occurred_at"`
}

func (e OutboxEvent) WithDefaultCausation() OutboxEvent {
	if e.CausationID == "" {
		e.CausationID = e.MessageID
	}
	return e
}
