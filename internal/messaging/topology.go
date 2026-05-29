package messaging

import "strings"

const (
	DomainExchange = "fulfillhub.domain"
	RetryExchange  = "fulfillhub.retry"
	DLXExchange    = "fulfillhub.dlx"

	InventoryReserveQueue   = "inventory.reserve"
	PaymentsAuthorizeQueue  = "payments.authorize"
	ShipmentsCreateQueue    = "shipments.create"
	OrdersCompensateQueue   = "orders.compensate"
	NotificationsEmailQueue = "notifications.email"
)

func RoutingKey(eventType string) string {
	return strings.TrimSpace(eventType)
}
