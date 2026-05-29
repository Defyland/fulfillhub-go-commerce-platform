package fulfillment

import (
	"context"
	"crypto/rand"
	"fmt"
	"time"

	"github.com/Defyland/fulfillhub-go-commerce-platform/internal/commerce"
	"github.com/Defyland/fulfillhub-go-commerce-platform/internal/messaging"
)

type Publisher interface {
	Publish(ctx context.Context, event commerce.OutboxEvent) error
}

type OrderStatusUpdater interface {
	UpdateOrderStatus(ctx context.Context, orderID string, status commerce.OrderStatus, now time.Time, event commerce.OutboxEvent, audit commerce.AuditLog) (*commerce.Order, error)
}

type Dependencies struct {
	Publisher Publisher
	Orders    OrderStatusUpdater
	Clock     func() time.Time
	NewID     func(prefix string) string
}

func HandlerForQueue(queue string, deps Dependencies) (messaging.EventHandler, error) {
	deps = deps.withDefaults()

	switch queue {
	case messaging.InventoryReserveQueue:
		return messaging.HandlerFunc(func(ctx context.Context, event commerce.OutboxEvent) error {
			return publishNext(ctx, deps, event, "order.created", "inventory.reserved")
		}), nil
	case messaging.PaymentsAuthorizeQueue:
		return messaging.HandlerFunc(func(ctx context.Context, event commerce.OutboxEvent) error {
			return publishNext(ctx, deps, event, "inventory.reserved", "payment.authorized")
		}), nil
	case messaging.ShipmentsCreateQueue:
		return messaging.HandlerFunc(func(ctx context.Context, event commerce.OutboxEvent) error {
			return publishNext(ctx, deps, event, "payment.authorized", "shipment.created")
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

func publishNext(ctx context.Context, deps Dependencies, event commerce.OutboxEvent, expectedType, nextType string) error {
	if deps.Publisher == nil {
		return fmt.Errorf("publisher is required")
	}
	if err := validateEvent(event, expectedType); err != nil {
		return err
	}
	return deps.Publisher.Publish(ctx, nextEvent(deps, event, nextType))
}

func completeOrder(ctx context.Context, deps Dependencies, event commerce.OutboxEvent) error {
	if deps.Orders == nil {
		return fmt.Errorf("order status updater is required")
	}
	if err := validateEvent(event, "shipment.created"); err != nil {
		return err
	}

	now := deps.Clock()
	completed := nextEvent(deps, event, "order.completed")
	completed.OccurredAt = now
	audit := commerce.AuditLog{
		MerchantID:    event.MerchantID,
		OrderID:       event.OrderID,
		ActorType:     "system",
		ActorID:       "fulfillment-worker",
		Action:        "order.completed",
		CorrelationID: event.CorrelationID,
		CreatedAt:     now,
		Details: map[string]string{
			"source_message_id": event.MessageID,
			"source_event_type": event.EventType,
		},
	}
	_, err := deps.Orders.UpdateOrderStatus(ctx, event.OrderID, commerce.StatusCompleted, now, completed, audit)
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
	now := deps.Clock()
	return commerce.OutboxEvent{
		MessageID:     deps.NewID("msg"),
		CorrelationID: previous.CorrelationID,
		EventType:     eventType,
		OrderID:       previous.OrderID,
		MerchantID:    previous.MerchantID,
		OccurredAt:    now,
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
