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
	GetShipment(ctx context.Context, shipmentID string) (*ShipmentRecord, error)
	UpdateOrderStatus(ctx context.Context, orderID string, status OrderStatus, now time.Time, event OutboxEvent, audit AuditLog) (*Order, error)
	OutboxEvents() []OutboxEvent
	AuditLogs() []AuditLog
}

type AuditActor struct {
	Type   string
	ID     string
	Reason string
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

func (s *MemoryStore) GetShipment(_ context.Context, shipmentID string) (*ShipmentRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, order := range s.orders {
		if order.Shipment == nil || order.Shipment.ShipmentID != shipmentID {
			continue
		}
		return cloneShipmentRecord(shipmentRecordFromOrder(order)), nil
	}
	return nil, ErrNotFound
}

func (s *MemoryStore) UpdateOrderStatus(_ context.Context, orderID string, status OrderStatus, now time.Time, event OutboxEvent, audit AuditLog) (*Order, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	order, ok := s.orders[orderID]
	if !ok {
		return nil, ErrNotFound
	}
	if err := ValidateOrderTransition(order.Status, status); err != nil {
		return nil, err
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
	if err := ValidateOrderTransition(order.Status, StatusInventoryReserved); err != nil {
		return err
	}
	for idx := range order.Items {
		order.Items[idx].ReservationStatus = "reserved"
	}
	order.Status = StatusInventoryReserved
	order.UpdatedAt = next.OccurredAt
	s.outbox = append(s.outbox, next)
	s.auditLogs = append(s.auditLogs, audit)
	return nil
}

func (s *MemoryStore) RecordInventoryRejected(_ context.Context, source OutboxEvent, next OutboxEvent, audit AuditLog) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	order, ok := s.orders[source.OrderID]
	if !ok {
		return ErrNotFound
	}
	for idx := range order.Items {
		order.Items[idx].ReservationStatus = "rejected"
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
	if err := ValidateOrderTransition(order.Status, StatusPaymentAuthorized); err != nil {
		return err
	}
	order.Payment = &payment
	order.Status = StatusPaymentAuthorized
	order.UpdatedAt = next.OccurredAt
	s.outbox = append(s.outbox, next)
	s.auditLogs = append(s.auditLogs, audit)
	return nil
}

func (s *MemoryStore) RecordPaymentFailed(_ context.Context, source OutboxEvent, next OutboxEvent, audit AuditLog) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	order, ok := s.orders[source.OrderID]
	if !ok {
		return ErrNotFound
	}
	provider := "fake-payment"
	if order.Payment != nil && order.Payment.Provider != "" {
		provider = order.Payment.Provider
	}
	order.Payment = &Payment{
		Provider: provider,
		Status:   "failed",
	}
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
	if err := ValidateOrderTransition(order.Status, StatusShipmentCreated); err != nil {
		return err
	}
	order.Shipment = &shipment
	order.Status = StatusShipmentCreated
	order.UpdatedAt = next.OccurredAt
	s.outbox = append(s.outbox, next)
	s.auditLogs = append(s.auditLogs, audit)
	return nil
}

func (s *MemoryStore) RecordShipmentFailed(_ context.Context, source OutboxEvent, next OutboxEvent, audit AuditLog) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	order, ok := s.orders[source.OrderID]
	if !ok {
		return ErrNotFound
	}
	order.Shipment = &Shipment{
		Status: "failed",
	}
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

func (s *MemoryStore) RecordCompensation(_ context.Context, source OutboxEvent, status OrderStatus, audit AuditLog) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	order, ok := s.orders[source.OrderID]
	if !ok {
		return ErrNotFound
	}
	if err := ValidateOrderTransition(order.Status, status); err != nil {
		return err
	}
	order.Status = status
	order.UpdatedAt = audit.CreatedAt
	switch source.EventType {
	case "payment.failed", "shipment.failed":
		for idx := range order.Items {
			if order.Items[idx].ReservationStatus == "reserved" {
				order.Items[idx].ReservationStatus = "released"
			}
		}
	}
	if source.EventType == "shipment.failed" && order.Payment != nil && order.Payment.Status == "authorized" {
		order.Payment.Status = "voided"
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

func (s *MemoryStore) PendingOutboxCount(_ context.Context) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	count := 0
	for _, event := range s.outbox {
		if _, ok := s.publishedOutbox[event.MessageID]; !ok {
			count++
		}
	}
	return count, nil
}

func (s *MemoryStore) OldestPendingOutboxAgeSeconds(_ context.Context) (float64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var oldest *OutboxEvent
	for idx := range s.outbox {
		event := &s.outbox[idx]
		if _, ok := s.publishedOutbox[event.MessageID]; ok {
			continue
		}
		if oldest == nil || event.OccurredAt.Before(oldest.OccurredAt) {
			oldest = event
		}
	}
	if oldest == nil {
		return 0, nil
	}
	return time.Since(oldest.OccurredAt).Seconds(), nil
}

func (s *MemoryStore) OrderStatusCounts(_ context.Context) (map[OrderStatus]int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	counts := make(map[OrderStatus]int, len(ValidOrderStatuses()))
	for _, status := range ValidOrderStatuses() {
		counts[status] = 0
	}
	for _, order := range s.orders {
		counts[order.Status]++
	}
	return counts, nil
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

func shipmentRecordFromOrder(order *Order) *ShipmentRecord {
	if order == nil || order.Shipment == nil {
		return nil
	}
	return &ShipmentRecord{
		ShipmentID:     order.Shipment.ShipmentID,
		OrderID:        order.OrderID,
		MerchantID:     order.MerchantID,
		Carrier:        order.Shipment.Carrier,
		TrackingNumber: order.Shipment.TrackingNumber,
		Status:         order.Shipment.Status,
		Events:         append([]ShipmentEvent(nil), order.Shipment.Events...),
	}
}

func cloneShipmentRecord(record *ShipmentRecord) *ShipmentRecord {
	if record == nil {
		return nil
	}
	clone := *record
	clone.Events = append([]ShipmentEvent(nil), record.Events...)
	return &clone
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
