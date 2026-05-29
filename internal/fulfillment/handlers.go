package fulfillment

import (
	"context"
	"crypto/rand"
	"fmt"
	"time"

	"github.com/Defyland/fulfillhub-go-commerce-platform/internal/commerce"
	"github.com/Defyland/fulfillhub-go-commerce-platform/internal/messaging"
)

type OrderStatusUpdater interface {
	UpdateOrderStatus(ctx context.Context, orderID string, status commerce.OrderStatus, now time.Time, event commerce.OutboxEvent, audit commerce.AuditLog) (*commerce.Order, error)
}

type Projector interface {
	RecordInventoryReserved(ctx context.Context, source commerce.OutboxEvent, next commerce.OutboxEvent, audit commerce.AuditLog) error
	RecordInventoryRejected(ctx context.Context, source commerce.OutboxEvent, next commerce.OutboxEvent, audit commerce.AuditLog) error
	RecordPaymentAuthorized(ctx context.Context, source commerce.OutboxEvent, next commerce.OutboxEvent, payment commerce.Payment, audit commerce.AuditLog) error
	RecordPaymentFailed(ctx context.Context, source commerce.OutboxEvent, next commerce.OutboxEvent, audit commerce.AuditLog) error
	RecordShipmentCreated(ctx context.Context, source commerce.OutboxEvent, next commerce.OutboxEvent, shipment commerce.Shipment, audit commerce.AuditLog) error
	RecordShipmentFailed(ctx context.Context, source commerce.OutboxEvent, next commerce.OutboxEvent, audit commerce.AuditLog) error
	RecordNotificationQueued(ctx context.Context, source commerce.OutboxEvent, audit commerce.AuditLog) error
	RecordCompensation(ctx context.Context, source commerce.OutboxEvent, status commerce.OrderStatus, audit commerce.AuditLog) error
}

type InventoryReserver interface {
	ReserveInventory(ctx context.Context, event commerce.OutboxEvent) error
}

type InventoryReserverFunc func(context.Context, commerce.OutboxEvent) error

func (f InventoryReserverFunc) ReserveInventory(ctx context.Context, event commerce.OutboxEvent) error {
	return f(ctx, event)
}

type PaymentAuthorizer interface {
	AuthorizePayment(ctx context.Context, event commerce.OutboxEvent) (commerce.Payment, error)
}

type PaymentAuthorizerFunc func(context.Context, commerce.OutboxEvent) (commerce.Payment, error)

func (f PaymentAuthorizerFunc) AuthorizePayment(ctx context.Context, event commerce.OutboxEvent) (commerce.Payment, error) {
	return f(ctx, event)
}

type ShipmentCreator interface {
	CreateShipment(ctx context.Context, event commerce.OutboxEvent) (commerce.Shipment, error)
}

type ShipmentCreatorFunc func(context.Context, commerce.OutboxEvent) (commerce.Shipment, error)

func (f ShipmentCreatorFunc) CreateShipment(ctx context.Context, event commerce.OutboxEvent) (commerce.Shipment, error) {
	return f(ctx, event)
}

type Dependencies struct {
	Projector         Projector
	Orders            OrderStatusUpdater
	InventoryReserver InventoryReserver
	PaymentAuthorizer PaymentAuthorizer
	ShipmentCreator   ShipmentCreator
	Clock             func() time.Time
	NewID             func(prefix string) string
}

func HandlerForQueue(queue string, deps Dependencies) (messaging.EventHandler, error) {
	deps = deps.withDefaults()

	switch queue {
	case messaging.InventoryReserveQueue:
		return messaging.HandlerFunc(func(ctx context.Context, event commerce.OutboxEvent) error {
			return reserveInventory(ctx, deps, event)
		}), nil
	case messaging.PaymentsAuthorizeQueue:
		return messaging.HandlerFunc(func(ctx context.Context, event commerce.OutboxEvent) error {
			return authorizePayment(ctx, deps, event)
		}), nil
	case messaging.ShipmentsCreateQueue:
		return messaging.HandlerFunc(func(ctx context.Context, event commerce.OutboxEvent) error {
			return createShipment(ctx, deps, event)
		}), nil
	case messaging.OrdersFinalizeQueue:
		return messaging.HandlerFunc(func(ctx context.Context, event commerce.OutboxEvent) error {
			return completeOrder(ctx, deps, event)
		}), nil
	case messaging.NotificationsEmailQueue:
		return messaging.HandlerFunc(func(ctx context.Context, event commerce.OutboxEvent) error {
			return queueNotification(ctx, deps, event)
		}), nil
	case messaging.OrdersCompensateQueue:
		return messaging.HandlerFunc(func(ctx context.Context, event commerce.OutboxEvent) error {
			return recordCompensation(ctx, deps, event)
		}), nil
	default:
		return nil, fmt.Errorf("unsupported worker queue %q", queue)
	}
}

func recordCompensation(ctx context.Context, deps Dependencies, event commerce.OutboxEvent) error {
	if deps.Projector == nil {
		return fmt.Errorf("fulfillment projector is required")
	}
	status, action, err := compensationPlan(event.EventType)
	if err != nil {
		return err
	}
	if err := validateEventIdentity(event); err != nil {
		return err
	}
	now := deps.Clock()
	audit := systemAudit(event, action, now)
	audit.Details["target_order_status"] = string(status)
	return deps.Projector.RecordCompensation(ctx, event, status, audit)
}

func queueNotification(ctx context.Context, deps Dependencies, event commerce.OutboxEvent) error {
	if deps.Projector == nil {
		return fmt.Errorf("fulfillment projector is required")
	}
	if event.EventType != "order.completed" && event.EventType != "order.cancelled" {
		return fmt.Errorf("expected order.completed or order.cancelled event, got %s", event.EventType)
	}
	if err := validateEventIdentity(event); err != nil {
		return err
	}
	now := deps.Clock()
	return deps.Projector.RecordNotificationQueued(ctx, event, systemAudit(event, "notification.email_queued", now))
}

func compensationPlan(eventType string) (commerce.OrderStatus, string, error) {
	switch eventType {
	case "inventory.rejected":
		return commerce.StatusFailed, "compensation.inventory_rejected", nil
	case "payment.failed":
		return commerce.StatusCancelled, "compensation.payment_failed", nil
	case "shipment.failed":
		return commerce.StatusCancellationPending, "compensation.shipment_failed", nil
	default:
		return "", "", fmt.Errorf("unsupported compensation event %s", eventType)
	}
}

func reserveInventory(ctx context.Context, deps Dependencies, event commerce.OutboxEvent) error {
	if deps.Projector == nil {
		return fmt.Errorf("fulfillment projector is required")
	}
	if err := validateEvent(event, "order.created"); err != nil {
		return err
	}
	now := deps.Clock()
	if deps.InventoryReserver != nil {
		if err := deps.InventoryReserver.ReserveInventory(ctx, event); err != nil {
			rejected := nextEventAt(deps, event, "inventory.rejected", now)
			audit := systemAudit(event, "inventory.rejected", now)
			audit.Details["error"] = err.Error()
			return deps.Projector.RecordInventoryRejected(ctx, event, rejected, audit)
		}
	}
	next := nextEventAt(deps, event, "inventory.reserved", now)
	return deps.Projector.RecordInventoryReserved(ctx, event, next, systemAudit(event, "inventory.reserved", now))
}

func authorizePayment(ctx context.Context, deps Dependencies, event commerce.OutboxEvent) error {
	if deps.Projector == nil {
		return fmt.Errorf("fulfillment projector is required")
	}
	if err := validateEvent(event, "inventory.reserved"); err != nil {
		return err
	}
	now := deps.Clock()
	if deps.PaymentAuthorizer != nil {
		authorized, err := deps.PaymentAuthorizer.AuthorizePayment(ctx, event)
		if err != nil {
			failed := nextEventAt(deps, event, "payment.failed", now)
			audit := systemAudit(event, "payment.failed", now)
			audit.Details["error"] = err.Error()
			return deps.Projector.RecordPaymentFailed(ctx, event, failed, audit)
		}
		payment := normalizedPayment(authorized)
		if payment.AuthorizationID == "" {
			payment.AuthorizationID = deps.NewID("pay")
		}
		next := nextEventAt(deps, event, "payment.authorized", now)
		return deps.Projector.RecordPaymentAuthorized(ctx, event, next, payment, systemAudit(event, "payment.authorized", now))
	}

	next := nextEventAt(deps, event, "payment.authorized", now)
	payment := normalizedPayment(commerce.Payment{
		Provider:        "fake-payment",
		Status:          "authorized",
		AuthorizationID: deps.NewID("pay"),
	})
	return deps.Projector.RecordPaymentAuthorized(ctx, event, next, payment, systemAudit(event, "payment.authorized", now))
}

func normalizedPayment(payment commerce.Payment) commerce.Payment {
	if payment.Provider == "" {
		payment.Provider = "fake-payment"
	}
	if payment.Status == "" {
		payment.Status = "authorized"
	}
	return payment
}

func createShipment(ctx context.Context, deps Dependencies, event commerce.OutboxEvent) error {
	if deps.Projector == nil {
		return fmt.Errorf("fulfillment projector is required")
	}
	if err := validateEvent(event, "payment.authorized"); err != nil {
		return err
	}
	now := deps.Clock()
	if deps.ShipmentCreator != nil {
		shipment, err := deps.ShipmentCreator.CreateShipment(ctx, event)
		if err != nil {
			failed := nextEventAt(deps, event, "shipment.failed", now)
			audit := systemAudit(event, "shipment.failed", now)
			audit.Details["error"] = err.Error()
			return deps.Projector.RecordShipmentFailed(ctx, event, failed, audit)
		}
		next := nextEventAt(deps, event, "shipment.created", now)
		return deps.Projector.RecordShipmentCreated(ctx, event, next, normalizedShipment(deps, shipment, now), systemAudit(event, "shipment.created", now))
	}

	next := nextEventAt(deps, event, "shipment.created", now)
	shipmentID := deps.NewID("shp")
	shipment := normalizedShipment(deps, commerce.Shipment{
		ShipmentID:     shipmentID,
		Status:         "created",
		Carrier:        "fake-carrier",
		TrackingNumber: "TRACK-" + shipmentID,
		Events: []commerce.ShipmentEvent{
			{
				OccurredAt:  now,
				Status:      "created",
				Description: "Shipment booking created by fulfillment worker.",
			},
		},
	}, now)
	return deps.Projector.RecordShipmentCreated(ctx, event, next, shipment, systemAudit(event, "shipment.created", now))
}

func normalizedShipment(deps Dependencies, shipment commerce.Shipment, now time.Time) commerce.Shipment {
	if shipment.ShipmentID == "" {
		shipment.ShipmentID = deps.NewID("shp")
	}
	if shipment.Status == "" {
		shipment.Status = "created"
	}
	if shipment.Carrier == "" {
		shipment.Carrier = "fake-carrier"
	}
	if shipment.TrackingNumber == "" {
		shipment.TrackingNumber = "TRACK-" + shipment.ShipmentID
	}
	if len(shipment.Events) == 0 {
		shipment.Events = []commerce.ShipmentEvent{{
			OccurredAt:  now,
			Status:      shipment.Status,
			Description: "Shipment booking created by fulfillment worker.",
		}}
	}
	return shipment
}

func completeOrder(ctx context.Context, deps Dependencies, event commerce.OutboxEvent) error {
	if deps.Orders == nil {
		return fmt.Errorf("order status updater is required")
	}
	if err := validateEvent(event, "shipment.created"); err != nil {
		return err
	}

	now := deps.Clock()
	completed := nextEventAt(deps, event, "order.completed", now)
	_, err := deps.Orders.UpdateOrderStatus(ctx, event.OrderID, commerce.StatusCompleted, now, completed, systemAudit(event, "order.completed", now))
	return err
}

func validateEvent(event commerce.OutboxEvent, expectedType string) error {
	if event.EventType != expectedType {
		return fmt.Errorf("expected %s event, got %s", expectedType, event.EventType)
	}
	return validateEventIdentity(event)
}

func validateEventIdentity(event commerce.OutboxEvent) error {
	if event.MessageID == "" {
		return fmt.Errorf("message id is required")
	}
	if event.CorrelationID == "" {
		return fmt.Errorf("correlation id is required")
	}
	if event.OrderID == "" {
		return fmt.Errorf("order id is required")
	}
	if event.MerchantID == "" {
		return fmt.Errorf("merchant id is required")
	}
	return nil
}

func nextEvent(deps Dependencies, previous commerce.OutboxEvent, eventType string) commerce.OutboxEvent {
	return nextEventAt(deps, previous, eventType, deps.Clock())
}

func nextEventAt(deps Dependencies, previous commerce.OutboxEvent, eventType string, now time.Time) commerce.OutboxEvent {
	return commerce.OutboxEvent{
		MessageID:     deps.NewID("msg"),
		CorrelationID: previous.CorrelationID,
		EventType:     eventType,
		OrderID:       previous.OrderID,
		MerchantID:    previous.MerchantID,
		OccurredAt:    now,
	}
}

func systemAudit(event commerce.OutboxEvent, action string, now time.Time) commerce.AuditLog {
	return commerce.AuditLog{
		MerchantID:    event.MerchantID,
		OrderID:       event.OrderID,
		ActorType:     "system",
		ActorID:       "fulfillment-worker",
		Action:        action,
		CorrelationID: event.CorrelationID,
		CreatedAt:     now,
		Details: map[string]string{
			"source_message_id": event.MessageID,
			"source_event_type": event.EventType,
		},
	}
}

func (d Dependencies) withDefaults() Dependencies {
	if d.Clock == nil {
		d.Clock = func() time.Time { return time.Now().UTC() }
	}
	if d.NewID == nil {
		d.NewID = func(prefix string) string {
			var bytes [16]byte
			if _, err := rand.Read(bytes[:]); err == nil {
				return fmt.Sprintf("%s_%x", prefix, bytes)
			}
			return fmt.Sprintf("%s_%d", prefix, d.Clock().UnixNano())
		}
	}
	return d
}
