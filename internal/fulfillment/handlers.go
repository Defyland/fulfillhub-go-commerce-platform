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
	RecordPaymentAuthorized(ctx context.Context, source commerce.OutboxEvent, next commerce.OutboxEvent, payment commerce.Payment, audit commerce.AuditLog) error
	RecordShipmentCreated(ctx context.Context, source commerce.OutboxEvent, next commerce.OutboxEvent, shipment commerce.Shipment, audit commerce.AuditLog) error
}

type Dependencies struct {
	Projector Projector
	Orders    OrderStatusUpdater
	Clock     func() time.Time
	NewID     func(prefix string) string
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
	case messaging.OrdersCompensateQueue, messaging.NotificationsEmailQueue:
		return messaging.HandlerFunc(func(context.Context, commerce.OutboxEvent) error {
			return nil
		}), nil
	default:
		return nil, fmt.Errorf("unsupported worker queue %q", queue)
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
	next := nextEventAt(deps, event, "payment.authorized", now)
	payment := commerce.Payment{
		Provider:        "fake-payment",
		Status:          "authorized",
		AuthorizationID: deps.NewID("pay"),
	}
	return deps.Projector.RecordPaymentAuthorized(ctx, event, next, payment, systemAudit(event, "payment.authorized", now))
}

func createShipment(ctx context.Context, deps Dependencies, event commerce.OutboxEvent) error {
	if deps.Projector == nil {
		return fmt.Errorf("fulfillment projector is required")
	}
	if err := validateEvent(event, "payment.authorized"); err != nil {
		return err
	}
	now := deps.Clock()
	next := nextEventAt(deps, event, "shipment.created", now)
	shipmentID := deps.NewID("shp")
	shipment := commerce.Shipment{
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
	}
	return deps.Projector.RecordShipmentCreated(ctx, event, next, shipment, systemAudit(event, "shipment.created", now))
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
