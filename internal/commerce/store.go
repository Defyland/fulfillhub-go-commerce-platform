package commerce

import (
	"fmt"
	"sync"
	"time"
)

type Store interface {
	InsertOrder(merchantID, idempotencyKey string, order *Order, event OutboxEvent) (*Order, bool, error)
	GetOrder(orderID string) (*Order, error)
	UpdateOrderStatus(orderID string, status OrderStatus, now time.Time, event OutboxEvent) (*Order, error)
	OutboxEvents() []OutboxEvent
}

type MemoryStore struct {
	mu                  sync.RWMutex
	orders              map[string]*Order
	ordersByExternal    map[string]string
	ordersByIdempotency map[string]string
	outbox              []OutboxEvent
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		orders:              make(map[string]*Order),
		ordersByExternal:    make(map[string]string),
		ordersByIdempotency: make(map[string]string),
	}
}

func (s *MemoryStore) InsertOrder(merchantID, idempotencyKey string, order *Order, event OutboxEvent) (*Order, bool, error) {
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

	return cloneOrder(order), false, nil
}

func (s *MemoryStore) GetOrder(orderID string) (*Order, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	order, ok := s.orders[orderID]
	if !ok {
		return nil, ErrNotFound
	}
	return cloneOrder(order), nil
}

func (s *MemoryStore) UpdateOrderStatus(orderID string, status OrderStatus, now time.Time, event OutboxEvent) (*Order, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	order, ok := s.orders[orderID]
	if !ok {
		return nil, ErrNotFound
	}
	order.Status = status
	order.UpdatedAt = now
	s.outbox = append(s.outbox, event)

	return cloneOrder(order), nil
}

func (s *MemoryStore) OutboxEvents() []OutboxEvent {
	s.mu.RLock()
	defer s.mu.RUnlock()

	events := make([]OutboxEvent, len(s.outbox))
	copy(events, s.outbox)
	return events
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
