package messaging

import (
	"strings"
	"time"
)

const (
	DomainExchange = "fulfillhub.domain"
	RetryExchange  = "fulfillhub.retry"
	DLXExchange    = "fulfillhub.dlx"

	InventoryReserveQueue   = "inventory.reserve"
	PaymentsAuthorizeQueue  = "payments.authorize"
	ShipmentsCreateQueue    = "shipments.create"
	OrdersFinalizeQueue     = "orders.finalize"
	OrdersCompensateQueue   = "orders.compensate"
	NotificationsEmailQueue = "notifications.email"
)

const defaultMaxRetryAttempts = 3

type QueueTopology struct {
	Queue       string
	RoutingKeys []string
	RetryQueue  string
	RetryTTL    time.Duration
	DLQ         string
}

func RoutingKey(eventType string) string {
	return strings.TrimSpace(eventType)
}

func QueueNames() []string {
	return []string{
		InventoryReserveQueue,
		PaymentsAuthorizeQueue,
		ShipmentsCreateQueue,
		OrdersFinalizeQueue,
		OrdersCompensateQueue,
		NotificationsEmailQueue,
	}
}

func QueueTopologies() []QueueTopology {
	return []QueueTopology{
		{
			Queue:       InventoryReserveQueue,
			RoutingKeys: []string{"order.created"},
			RetryQueue:  "inventory.reserve.retry.5s",
			RetryTTL:    5 * time.Second,
			DLQ:         "inventory.reserve.dlq",
		},
		{
			Queue:       PaymentsAuthorizeQueue,
			RoutingKeys: []string{"inventory.reserved"},
			RetryQueue:  "payments.authorize.retry.15s",
			RetryTTL:    15 * time.Second,
			DLQ:         "payments.authorize.dlq",
		},
		{
			Queue:       ShipmentsCreateQueue,
			RoutingKeys: []string{"payment.authorized"},
			RetryQueue:  "shipments.create.retry.30s",
			RetryTTL:    30 * time.Second,
			DLQ:         "shipments.create.dlq",
		},
		{
			Queue:       OrdersFinalizeQueue,
			RoutingKeys: []string{"shipment.created"},
			RetryQueue:  "orders.finalize.retry.15s",
			RetryTTL:    15 * time.Second,
			DLQ:         "orders.finalize.dlq",
		},
		{
			Queue:       OrdersCompensateQueue,
			RoutingKeys: []string{"inventory.rejected", "payment.failed", "shipment.failed"},
			RetryQueue:  "orders.compensate.retry.15s",
			RetryTTL:    15 * time.Second,
			DLQ:         "orders.compensate.dlq",
		},
		{
			Queue:       NotificationsEmailQueue,
			RoutingKeys: []string{"order.completed", "order.cancelled"},
			RetryQueue:  "notifications.email.retry.60s",
			RetryTTL:    60 * time.Second,
			DLQ:         "notifications.email.dlq",
		},
	}
}
