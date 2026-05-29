package commerce

import (
	"context"
	"fmt"
	"sync"
	"time"
)

type Store interface {
	InsertOrder(ctx context.Context, merchantID, idempotencyKey string, order *Order, event OutboxEvent, audit AuditLog) (*Order, bool, error)
	GetOrder(ctx context.Context, orderID string) (*Order, error)
	UpdateOrderStatus(ctx context.Context, orderID string, status OrderStatus, now time.Time, event OutboxEvent, audit AuditLog) (*Order, error)
	OutboxEvents() []OutboxEvent
	AuditLogs() []AuditLog
}

type AuditActor struct {
	Type string
	ID   string
}

type AuditLog struct {
	MerchantID    string
	OrderID       string
	ActorType     string
	ActorID       string
	Action        string
	CorrelationID string
	CreatedAt     time.Time
	Details       map[string]string
}

type MemoryStore struct {
	mu                  sync.RWMutex
	orders              map[string]*Order
	ordersByExternal    map[string]string
	ordersByIdempotency map[string]string
	outbox              []OutboxEvent
	auditLogs           []AuditLog
	publishedOutbox     map[string]time.Time
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		orders:              make(map[string]*Order),
		ordersByExternal:    make(map[string]string),
		ordersByIdempotency: make(map[string]string),
		publishedOutbox:     make(map[string]time.Time),
	}
}

func (s *MemoryStore) InsertOrder(_ context.Context, merchantID, idempotencyKey string, order *Order, event OutboxEvent, audit AuditLog) (*Order, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	idempotencyRef := scopedKey(merchantID, idempotencyKey)
	if existingID, ok := s.ordersByIdempotency[idempotencyRef]; ok {
		return cloneOrder(s.orders[existingID]), true, nil
	}

	externalRef := scopedKey(merchantID, order.ExternalOrderID)
	if _, ok := s.ordersByExternal[externalRef]; ok {
		return nil, false, ErrDuplicateOrder
	}

	s.orders[order.OrderID] = cloneOrder(order)
	s.ordersByExternal[externalRef] = order.OrderID
	s.ordersByIdempotency[idempotencyRef] = order.OrderID
	s.outbox = append(s.outbox, event)
	s.auditLogs = append(s.auditLogs, audit)

	return cloneOrder(order), false, nil
}

func (s *MemoryStore) GetOrder(_ context.Context, orderID string) (*Order, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	order, ok := s.orders[orderID]
	if !ok {
		return nil, ErrNotFound
	}
	return cloneOrder(order), nil
}

func (s *MemoryStore) UpdateOrderStatus(_ context.Context, orderID string, status OrderStatus, now time.Time, event OutboxEvent, audit AuditLog) (*Order, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	order, ok := s.orders[orderID]
	if !ok {
		return nil, ErrNotFound
	}
	order.Status = status
	order.UpdatedAt = now
	s.outbox = append(s.outbox, event)
	s.auditLogs = append(s.auditLogs, audit)

	return cloneOrder(order), nil
}

func (s *MemoryStore) RecordInventoryReserved(_ context.Context, source OutboxEvent, next OutboxEvent, audit AuditLog) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	order, ok := s.orders[source.OrderID]
	if !ok {
		return ErrNotFound
	}
	for idx := range order.Items {
		order.Items[idx].ReservationStatus = "reserved"
	}
	order.UpdatedAt = next.OccurredAt
	s.outbox = append(s.outbox, next)
	s.auditLogs = append(s.auditLogs, audit)
	return nil
}

func (s *MemoryStore) RecordPaymentAuthorized(_ context.Context, source OutboxEvent, next OutboxEvent, payment Payment, audit AuditLog) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	order, ok := s.orders[source.OrderID]
	if !ok {
		return ErrNotFound
	}
	if payment.Provider == "" && order.Payment != nil {
		payment.Provider = order.Payment.Provider
	}
	order.Payment = &payment
	order.UpdatedAt = next.OccurredAt
	s.outbox = append(s.outbox, next)
	s.auditLogs = append(s.auditLogs, audit)
	return nil
}

func (s *MemoryStore) RecordShipmentCreated(_ context.Context, source OutboxEvent, next OutboxEvent, shipment Shipment, audit AuditLog) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	order, ok := s.orders[source.OrderID]
	if !ok {
		return ErrNotFound
	}
	order.Shipment = &shipment
	order.UpdatedAt = next.OccurredAt
	s.outbox = append(s.outbox, next)
	s.auditLogs = append(s.auditLogs, audit)
	return nil
}

func (s *MemoryStore) RecordNotificationQueued(_ context.Context, source OutboxEvent, audit AuditLog) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.orders[source.OrderID]; !ok {
		return ErrNotFound
	}
	s.auditLogs = append(s.auditLogs, audit)
	return nil
}

func (s *MemoryStore) OutboxEvents() []OutboxEvent {
	s.mu.RLock()
	defer s.mu.RUnlock()

	events := make([]OutboxEvent, len(s.outbox))
	copy(events, s.outbox)
	return events
}

func (s *MemoryStore) AuditLogs() []AuditLog {
	s.mu.RLock()
	defer s.mu.RUnlock()

	logs := make([]AuditLog, len(s.auditLogs))
	for idx, log := range s.auditLogs {
		logs[idx] = cloneAuditLog(log)
	}
	return logs
}

func (s *MemoryStore) PendingOutboxEvents(_ context.Context, limit int) ([]OutboxEvent, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if limit <= 0 {
		limit = len(s.outbox)
	}
	events := make([]OutboxEvent, 0, min(limit, len(s.outbox)))
	for _, event := range s.outbox {
		if _, ok := s.publishedOutbox[event.MessageID]; ok {
			continue
		}
		events = append(events, event)
		if len(events) == limit {
			break
		}
	}
	return events, nil
}

func (s *MemoryStore) MarkOutboxPublished(_ context.Context, messageID string, publishedAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.publishedOutbox[messageID] = publishedAt
	return nil
}

func scopedKey(merchantID, value string) string {
	return fmt.Sprintf("%s:%s", merchantID, value)
}

func cloneOrder(order *Order) *Order {
	if order == nil {
		return nil
	}
	clone := *order
	clone.Items = append([]OrderItem(nil), order.Items...)
	if order.Payment != nil {
		payment := *order.Payment
		clone.Payment = &payment
	}
	if order.Shipment != nil {
		shipment := *order.Shipment
		shipment.Events = append([]ShipmentEvent(nil), order.Shipment.Events...)
		clone.Shipment = &shipment
	}
	return &clone
}

func CloneOrderForStore(order *Order) *Order {
	return cloneOrder(order)
}

func cloneAuditLog(log AuditLog) AuditLog {
	clone := log
	if log.Details != nil {
		clone.Details = make(map[string]string, len(log.Details))
		for key, value := range log.Details {
			clone.Details[key] = value
		}
	}
	return clone
}
