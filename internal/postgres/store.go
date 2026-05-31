package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Defyland/fulfillhub-go-commerce-platform/internal/commerce"
	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

type Store struct {
	db *sql.DB
}

func Open(ctx context.Context, databaseURL string) (*Store, error) {
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	return &Store{db: db}, nil
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

func (s *Store) DB() *sql.DB {
	return s.db
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) InsertOrder(ctx context.Context, merchantID, idempotencyKey string, order *commerce.Order, event commerce.OutboxEvent, audit commerce.AuditLog) (created *commerce.Order, replayed bool, err error) {
	ctx = contextOrBackground(ctx)
	ctx, span := postgresTracer().Start(ctx, "postgres.insert_order", trace.WithAttributes(
		attribute.String("db.system.name", "postgresql"),
		attribute.String("fulfillhub.merchant_id", merchantID),
		attribute.String("fulfillhub.order_id", order.OrderID),
		attribute.String("fulfillhub.external_order_id", order.ExternalOrderID),
	))
	defer finishSpan(span, &err, "insert order")

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, false, fmt.Errorf("begin insert order: %w", err)
	}
	defer rollback(tx)

	if existing, ok, err := orderByIdempotency(ctx, tx, merchantID, idempotencyKey); err != nil {
		return nil, false, err
	} else if ok {
		order, err := getOrderTx(ctx, tx, existing)
		if err != nil {
			return nil, false, err
		}
		if err := tx.Commit(); err != nil {
			return nil, false, fmt.Errorf("commit idempotent order lookup: %w", err)
		}
		span.SetAttributes(attribute.Bool("fulfillhub.idempotent_replay", true))
		return order, true, nil
	}

	if err := upsertMerchant(ctx, tx, merchantID); err != nil {
		return nil, false, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO orders (
			order_id, merchant_id, external_order_id, status, currency,
			subtotal_amount, shipping_amount, total_amount,
			payment_provider, payment_status, payment_authorization_id,
			payment_credential_ref, shipping_address_ref, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
	`, order.OrderID, merchantID, order.ExternalOrderID, order.Status, order.Currency,
		order.Totals.Subtotal.Amount, order.Totals.Shipping.Amount, order.Totals.Total.Amount,
		nullablePaymentProvider(order), nullablePaymentStatus(order), nullablePaymentAuthorization(order), nullablePaymentCredentialRef(order), nullableString(order.ShippingAddressRef),
		order.CreatedAt, order.UpdatedAt); err != nil {
		if isUniqueViolation(err) {
			return nil, false, commerce.ErrDuplicateOrder
		}
		return nil, false, fmt.Errorf("insert order: %w", err)
	}

	for _, item := range order.Items {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO order_items (
				order_id, sku, quantity, unit_price_amount, unit_price_currency, reservation_status
			) VALUES ($1, $2, $3, $4, $5, $6)
		`, order.OrderID, item.SKU, item.Quantity, item.UnitPrice.Amount, item.UnitPrice.Currency, item.ReservationStatus); err != nil {
			return nil, false, fmt.Errorf("insert order item: %w", err)
		}
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO idempotency_keys (merchant_id, idempotency_key, order_id)
		VALUES ($1, $2, $3)
	`, merchantID, idempotencyKey, order.OrderID); err != nil {
		return nil, false, fmt.Errorf("insert idempotency key: %w", err)
	}

	if err := insertOutboxEvent(ctx, tx, event); err != nil {
		return nil, false, err
	}
	if err := insertAuditLog(ctx, tx, audit); err != nil {
		return nil, false, err
	}

	if err := tx.Commit(); err != nil {
		return nil, false, fmt.Errorf("commit insert order: %w", err)
	}
	span.SetAttributes(attribute.Bool("fulfillhub.idempotent_replay", false))
	return commerce.CloneOrderForStore(order), false, nil
}

func (s *Store) GetOrder(ctx context.Context, orderID string) (order *commerce.Order, err error) {
	ctx = contextOrBackground(ctx)
	ctx, span := postgresTracer().Start(ctx, "postgres.get_order", trace.WithAttributes(
		attribute.String("db.system.name", "postgresql"),
		attribute.String("fulfillhub.order_id", orderID),
	))
	defer finishSpan(span, &err, "get order")
	return getOrderTx(ctx, s.db, orderID)
}

func (s *Store) GetShipment(ctx context.Context, shipmentID string) (shipment *commerce.ShipmentRecord, err error) {
	ctx = contextOrBackground(ctx)
	ctx, span := postgresTracer().Start(ctx, "postgres.get_shipment", trace.WithAttributes(
		attribute.String("db.system.name", "postgresql"),
		attribute.String("fulfillhub.shipment_id", shipmentID),
	))
	defer finishSpan(span, &err, "get shipment")
	return getShipmentTx(ctx, s.db, shipmentID)
}

func (s *Store) UpdateOrderStatus(ctx context.Context, orderID string, status commerce.OrderStatus, now time.Time, event commerce.OutboxEvent, audit commerce.AuditLog) (order *commerce.Order, err error) {
	ctx = contextOrBackground(ctx)
	ctx, span := postgresTracer().Start(ctx, "postgres.update_order_status", trace.WithAttributes(
		attribute.String("db.system.name", "postgresql"),
		attribute.String("fulfillhub.order_id", orderID),
		attribute.String("fulfillhub.order_status", string(status)),
	))
	defer finishSpan(span, &err, "update order status")

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin update order status: %w", err)
	}
	defer rollback(tx)

	currentStatus, err := lockOrderStatusForUpdate(ctx, tx, orderID)
	if err != nil {
		return nil, err
	}
	if err := commerce.ValidateOrderTransition(currentStatus, status); err != nil {
		return nil, err
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE orders
		SET status = $2, updated_at = $3, version = version + 1
		WHERE order_id = $1
	`, orderID, status, now)
	if err != nil {
		return nil, fmt.Errorf("update order status: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("inspect updated rows: %w", err)
	}
	if rows == 0 {
		return nil, commerce.ErrNotFound
	}

	event.Payload = orderStatusEventPayload(status, now, audit)
	if err := insertOutboxEvent(ctx, tx, event); err != nil {
		return nil, err
	}
	if err := insertAuditLog(ctx, tx, audit); err != nil {
		return nil, err
	}
	order, err = getOrderTx(ctx, tx, orderID)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit update order status: %w", err)
	}
	return order, nil
}

func (s *Store) RecordInventoryReserved(ctx context.Context, source commerce.OutboxEvent, next commerce.OutboxEvent, audit commerce.AuditLog) (err error) {
	ctx = contextOrBackground(ctx)
	ctx, span := postgresTracer().Start(ctx, "postgres.record_inventory_reserved", trace.WithAttributes(
		attribute.String("db.system.name", "postgresql"),
		attribute.String("fulfillhub.order_id", source.OrderID),
		attribute.String("fulfillhub.merchant_id", source.MerchantID),
		attribute.String("fulfillhub.source_event_type", source.EventType),
		attribute.String("fulfillhub.next_event_type", next.EventType),
	))
	defer finishSpan(span, &err, "record inventory reservation")

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin record inventory reservation: %w", err)
	}
	defer rollback(tx)

	currentStatus, err := lockOrderStatusForUpdate(ctx, tx, source.OrderID)
	if err != nil {
		return err
	}
	if err := commerce.ValidateOrderTransition(currentStatus, commerce.StatusInventoryReserved); err != nil {
		return err
	}
	items, err := orderItemsForReservation(ctx, tx, source.OrderID)
	if err != nil {
		return err
	}
	reservations := make([]map[string]any, 0, len(items))
	for _, item := range items {
		warehouseID, err := reserveInventoryItem(ctx, tx, source.MerchantID, source.OrderID, item, next.OccurredAt)
		if err != nil {
			return err
		}
		reservations = append(reservations, map[string]any{
			"sku":          item.sku,
			"quantity":     item.quantity,
			"warehouse_id": warehouseID,
		})
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO stock_reservations (order_id, warehouse_id, sku, quantity, status, reserved_at)
			VALUES ($1, $2, $3, $4, 'reserved', $5)
			ON CONFLICT (order_id, sku) DO UPDATE
			SET warehouse_id = COALESCE(EXCLUDED.warehouse_id, stock_reservations.warehouse_id),
				quantity = EXCLUDED.quantity,
				status = EXCLUDED.status,
				reserved_at = EXCLUDED.reserved_at,
				released_at = NULL
		`, source.OrderID, nullableString(warehouseID), item.sku, item.quantity, next.OccurredAt); err != nil {
			return fmt.Errorf("upsert stock reservation: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE order_items
		SET reservation_status = 'reserved'
		WHERE order_id = $1
	`, source.OrderID); err != nil {
		return fmt.Errorf("mark order items reserved: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE orders
		SET status = $2,
			updated_at = $3,
			version = version + 1
		WHERE order_id = $1
	`, source.OrderID, commerce.StatusInventoryReserved, next.OccurredAt); err != nil {
		return fmt.Errorf("touch order after inventory reservation: %w", err)
	}
	next.Payload = map[string]any{
		"order_status": string(commerce.StatusInventoryReserved),
		"reservations": reservations,
	}
	if err := insertOutboxEvent(ctx, tx, next); err != nil {
		return err
	}
	if err := insertAuditLog(ctx, tx, audit); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit record inventory reservation: %w", err)
	}
	span.SetAttributes(attribute.Int("fulfillhub.reserved_item_count", len(items)))
	return nil
}

func (s *Store) RecordInventoryRejected(ctx context.Context, source commerce.OutboxEvent, next commerce.OutboxEvent, audit commerce.AuditLog) (err error) {
	ctx = contextOrBackground(ctx)
	ctx, span := postgresTracer().Start(ctx, "postgres.record_inventory_rejected", trace.WithAttributes(
		attribute.String("db.system.name", "postgresql"),
		attribute.String("fulfillhub.order_id", source.OrderID),
		attribute.String("fulfillhub.merchant_id", source.MerchantID),
	))
	defer finishSpan(span, &err, "record inventory rejection")

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin record inventory rejection: %w", err)
	}
	defer rollback(tx)

	if _, err := getOrderTx(ctx, tx, source.OrderID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE order_items
		SET reservation_status = 'rejected'
		WHERE order_id = $1
	`, source.OrderID); err != nil {
		return fmt.Errorf("mark order items rejected: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE orders
		SET updated_at = $2,
			version = version + 1
		WHERE order_id = $1
	`, source.OrderID, next.OccurredAt); err != nil {
		return fmt.Errorf("touch order after inventory rejection: %w", err)
	}
	next.Payload = failurePayload("inventory", audit.Details["error"])
	if err := insertOutboxEvent(ctx, tx, next); err != nil {
		return err
	}
	if err := insertAuditLog(ctx, tx, audit); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit record inventory rejection: %w", err)
	}
	return nil
}

func (s *Store) RecordPaymentAuthorized(ctx context.Context, source commerce.OutboxEvent, next commerce.OutboxEvent, payment commerce.Payment, audit commerce.AuditLog) (err error) {
	ctx = contextOrBackground(ctx)
	ctx, span := postgresTracer().Start(ctx, "postgres.record_payment_authorized", trace.WithAttributes(
		attribute.String("db.system.name", "postgresql"),
		attribute.String("fulfillhub.order_id", source.OrderID),
		attribute.String("fulfillhub.merchant_id", source.MerchantID),
		attribute.String("fulfillhub.payment_authorization_id", payment.AuthorizationID),
	))
	defer finishSpan(span, &err, "record payment authorization")

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin record payment authorization: %w", err)
	}
	defer rollback(tx)

	currentStatus, err := lockOrderStatusForUpdate(ctx, tx, source.OrderID)
	if err != nil {
		return err
	}
	if err := commerce.ValidateOrderTransition(currentStatus, commerce.StatusPaymentAuthorized); err != nil {
		return err
	}
	var provider, currency string
	var totalAmount int64
	if err := tx.QueryRowContext(ctx, `
		SELECT COALESCE(NULLIF($2, ''), payment_provider, 'fake-payment'), total_amount, currency
		FROM orders
		WHERE order_id = $1
	`, source.OrderID, payment.Provider).Scan(&provider, &totalAmount, &currency); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return commerce.ErrNotFound
		}
		return fmt.Errorf("load order for payment authorization: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO payment_authorizations (
			authorization_id, order_id, merchant_id, provider, status, amount, currency, authorized_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (order_id) DO UPDATE
		SET authorization_id = EXCLUDED.authorization_id,
			provider = EXCLUDED.provider,
			status = EXCLUDED.status,
			amount = EXCLUDED.amount,
			currency = EXCLUDED.currency,
			authorized_at = EXCLUDED.authorized_at,
			voided_at = NULL
	`, payment.AuthorizationID, source.OrderID, source.MerchantID, provider, payment.Status, totalAmount, currency, next.OccurredAt); err != nil {
		return fmt.Errorf("upsert payment authorization: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE orders
		SET status = $2,
			payment_provider = $3,
			payment_status = $4,
			payment_authorization_id = $5,
			updated_at = $6,
			version = version + 1
		WHERE order_id = $1
	`, source.OrderID, commerce.StatusPaymentAuthorized, provider, payment.Status, payment.AuthorizationID, next.OccurredAt); err != nil {
		return fmt.Errorf("update order payment authorization: %w", err)
	}
	next.Payload = map[string]any{
		"order_status": string(commerce.StatusPaymentAuthorized),
		"payment": map[string]any{
			"provider":         provider,
			"authorization_id": payment.AuthorizationID,
			"amount":           totalAmount,
			"currency":         currency,
		},
	}
	if err := insertOutboxEvent(ctx, tx, next); err != nil {
		return err
	}
	if err := insertAuditLog(ctx, tx, audit); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit record payment authorization: %w", err)
	}
	return nil
}

func (s *Store) RecordPaymentFailed(ctx context.Context, source commerce.OutboxEvent, next commerce.OutboxEvent, audit commerce.AuditLog) (err error) {
	ctx = contextOrBackground(ctx)
	ctx, span := postgresTracer().Start(ctx, "postgres.record_payment_failed", trace.WithAttributes(
		attribute.String("db.system.name", "postgresql"),
		attribute.String("fulfillhub.order_id", source.OrderID),
		attribute.String("fulfillhub.merchant_id", source.MerchantID),
	))
	defer finishSpan(span, &err, "record payment failure")

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin record payment failure: %w", err)
	}
	defer rollback(tx)

	var provider string
	if err := tx.QueryRowContext(ctx, `
		SELECT COALESCE(payment_provider, 'fake-payment')
		FROM orders
		WHERE order_id = $1
	`, source.OrderID).Scan(&provider); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return commerce.ErrNotFound
		}
		return fmt.Errorf("load order for payment failure: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE orders
		SET payment_provider = $2,
			payment_status = 'failed',
			payment_authorization_id = NULL,
			updated_at = $3,
			version = version + 1
		WHERE order_id = $1
	`, source.OrderID, provider, next.OccurredAt); err != nil {
		return fmt.Errorf("update order payment failure: %w", err)
	}
	next.Payload = failurePayload("payment", audit.Details["error"])
	if err := insertOutboxEvent(ctx, tx, next); err != nil {
		return err
	}
	if err := insertAuditLog(ctx, tx, audit); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit record payment failure: %w", err)
	}
	return nil
}

func (s *Store) RecordShipmentCreated(ctx context.Context, source commerce.OutboxEvent, next commerce.OutboxEvent, shipment commerce.Shipment, audit commerce.AuditLog) (err error) {
	ctx = contextOrBackground(ctx)
	ctx, span := postgresTracer().Start(ctx, "postgres.record_shipment_created", trace.WithAttributes(
		attribute.String("db.system.name", "postgresql"),
		attribute.String("fulfillhub.order_id", source.OrderID),
		attribute.String("fulfillhub.merchant_id", source.MerchantID),
		attribute.String("fulfillhub.shipment_id", shipment.ShipmentID),
	))
	defer finishSpan(span, &err, "record shipment")

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin record shipment: %w", err)
	}
	defer rollback(tx)

	currentStatus, err := lockOrderStatusForUpdate(ctx, tx, source.OrderID)
	if err != nil {
		return err
	}
	if err := commerce.ValidateOrderTransition(currentStatus, commerce.StatusShipmentCreated); err != nil {
		return err
	}
	if _, err := getOrderTx(ctx, tx, source.OrderID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO shipments (
			shipment_id, order_id, merchant_id, carrier, tracking_number, status, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $7)
		ON CONFLICT (order_id) DO UPDATE
		SET shipment_id = EXCLUDED.shipment_id,
			carrier = EXCLUDED.carrier,
			tracking_number = EXCLUDED.tracking_number,
			status = EXCLUDED.status,
			updated_at = EXCLUDED.updated_at
	`, shipment.ShipmentID, source.OrderID, source.MerchantID, shipment.Carrier, shipment.TrackingNumber, shipment.Status, next.OccurredAt); err != nil {
		return fmt.Errorf("upsert shipment: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE orders
		SET status = $2,
			updated_at = $3,
			version = version + 1
		WHERE order_id = $1
	`, source.OrderID, commerce.StatusShipmentCreated, next.OccurredAt); err != nil {
		return fmt.Errorf("touch order after shipment: %w", err)
	}
	next.Payload = map[string]any{
		"order_status": string(commerce.StatusShipmentCreated),
		"shipment": map[string]any{
			"shipment_id":     shipment.ShipmentID,
			"carrier":         shipment.Carrier,
			"tracking_number": shipment.TrackingNumber,
		},
	}
	if err := insertOutboxEvent(ctx, tx, next); err != nil {
		return err
	}
	if err := insertAuditLog(ctx, tx, audit); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit record shipment: %w", err)
	}
	return nil
}

func (s *Store) RecordShipmentFailed(ctx context.Context, source commerce.OutboxEvent, next commerce.OutboxEvent, audit commerce.AuditLog) (err error) {
	ctx = contextOrBackground(ctx)
	ctx, span := postgresTracer().Start(ctx, "postgres.record_shipment_failed", trace.WithAttributes(
		attribute.String("db.system.name", "postgresql"),
		attribute.String("fulfillhub.order_id", source.OrderID),
		attribute.String("fulfillhub.merchant_id", source.MerchantID),
	))
	defer finishSpan(span, &err, "record shipment failure")

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin record shipment failure: %w", err)
	}
	defer rollback(tx)

	if _, err := getOrderTx(ctx, tx, source.OrderID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE orders
		SET updated_at = $2,
			version = version + 1
		WHERE order_id = $1
	`, source.OrderID, next.OccurredAt); err != nil {
		return fmt.Errorf("touch order after shipment failure: %w", err)
	}
	next.Payload = failurePayload("shipment", audit.Details["error"])
	if err := insertOutboxEvent(ctx, tx, next); err != nil {
		return err
	}
	if err := insertAuditLog(ctx, tx, audit); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit record shipment failure: %w", err)
	}
	return nil
}

func (s *Store) RecordOrderCancelled(ctx context.Context, source commerce.OutboxEvent, next commerce.OutboxEvent, audit commerce.AuditLog) (err error) {
	ctx = contextOrBackground(ctx)
	ctx, span := postgresTracer().Start(ctx, "postgres.record_order_cancelled", trace.WithAttributes(
		attribute.String("db.system.name", "postgresql"),
		attribute.String("fulfillhub.order_id", source.OrderID),
		attribute.String("fulfillhub.merchant_id", source.MerchantID),
		attribute.String("fulfillhub.source_event_type", source.EventType),
	))
	defer finishSpan(span, &err, "record order cancellation")

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin record order cancellation: %w", err)
	}
	defer rollback(tx)

	currentStatus, err := lockOrderStatusForUpdate(ctx, tx, source.OrderID)
	if err != nil {
		return err
	}
	if err := commerce.ValidateOrderTransition(currentStatus, commerce.StatusCancelled); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE orders
		SET status = $2,
			updated_at = $3,
			version = version + 1
		WHERE order_id = $1
	`, source.OrderID, commerce.StatusCancelled, next.OccurredAt)
	if err != nil {
		return fmt.Errorf("update cancelled order status: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("inspect cancelled order rows: %w", err)
	}
	if rows == 0 {
		return commerce.ErrNotFound
	}
	if err := releaseReservedStock(ctx, tx, source.OrderID, next.OccurredAt); err != nil {
		return err
	}
	if err := voidAuthorizedPayment(ctx, tx, source.OrderID, next.OccurredAt); err != nil {
		return err
	}
	next.Payload = orderStatusEventPayload(commerce.StatusCancelled, next.OccurredAt, audit)
	if err := insertOutboxEvent(ctx, tx, next); err != nil {
		return err
	}
	if err := insertAuditLog(ctx, tx, audit); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit record order cancellation: %w", err)
	}
	return nil
}

func (s *Store) RecordNotificationQueued(ctx context.Context, source commerce.OutboxEvent, audit commerce.AuditLog) (err error) {
	ctx = contextOrBackground(ctx)
	ctx, span := postgresTracer().Start(ctx, "postgres.record_notification_queued", trace.WithAttributes(
		attribute.String("db.system.name", "postgresql"),
		attribute.String("messaging.message.id", source.MessageID),
		attribute.String("fulfillhub.order_id", source.OrderID),
		attribute.String("fulfillhub.merchant_id", source.MerchantID),
		attribute.String("fulfillhub.source_event_type", source.EventType),
	))
	defer finishSpan(span, &err, "record notification")

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin record notification: %w", err)
	}
	defer rollback(tx)

	if _, err := getOrderTx(ctx, tx, source.OrderID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO notification_events (
			order_id, merchant_id, source_message_id, source_event_type, channel, status, correlation_id, created_at
		) VALUES ($1, $2, $3, $4, 'email', 'queued', $5, $6)
		ON CONFLICT (source_message_id) DO UPDATE
		SET status = EXCLUDED.status,
			correlation_id = EXCLUDED.correlation_id,
			created_at = EXCLUDED.created_at
	`, source.OrderID, source.MerchantID, source.MessageID, source.EventType, source.CorrelationID, audit.CreatedAt); err != nil {
		return fmt.Errorf("upsert notification event: %w", err)
	}
	if err := insertAuditLog(ctx, tx, audit); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit record notification: %w", err)
	}
	return nil
}

func (s *Store) RecordCompensation(ctx context.Context, source commerce.OutboxEvent, status commerce.OrderStatus, audit commerce.AuditLog) (err error) {
	ctx = contextOrBackground(ctx)
	ctx, span := postgresTracer().Start(ctx, "postgres.record_compensation", trace.WithAttributes(
		attribute.String("db.system.name", "postgresql"),
		attribute.String("messaging.message.id", source.MessageID),
		attribute.String("fulfillhub.order_id", source.OrderID),
		attribute.String("fulfillhub.merchant_id", source.MerchantID),
		attribute.String("fulfillhub.source_event_type", source.EventType),
		attribute.String("fulfillhub.target_order_status", string(status)),
	))
	defer finishSpan(span, &err, "record compensation")

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin record compensation: %w", err)
	}
	defer rollback(tx)

	currentStatus, err := lockOrderStatusForUpdate(ctx, tx, source.OrderID)
	if err != nil {
		return err
	}
	if err := commerce.ValidateOrderTransition(currentStatus, status); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE orders
		SET status = $2,
			updated_at = $3,
			version = version + 1
		WHERE order_id = $1
	`, source.OrderID, status, audit.CreatedAt)
	if err != nil {
		return fmt.Errorf("update compensated order status: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("inspect compensated order rows: %w", err)
	}
	if rows == 0 {
		return commerce.ErrNotFound
	}
	if err := applyCompensationEffects(ctx, tx, source, audit.CreatedAt); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO compensation_events (
			order_id, merchant_id, source_message_id, source_event_type,
			action, target_order_status, status, correlation_id, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, 'recorded', $7, $8)
		ON CONFLICT (source_message_id) DO UPDATE
		SET target_order_status = EXCLUDED.target_order_status,
			status = EXCLUDED.status,
			correlation_id = EXCLUDED.correlation_id,
			created_at = EXCLUDED.created_at
	`, source.OrderID, source.MerchantID, source.MessageID, source.EventType, audit.Action, status, source.CorrelationID, audit.CreatedAt); err != nil {
		return fmt.Errorf("upsert compensation event: %w", err)
	}
	if err := insertAuditLog(ctx, tx, audit); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit record compensation: %w", err)
	}
	return nil
}

func applyCompensationEffects(ctx context.Context, tx *sql.Tx, source commerce.OutboxEvent, at time.Time) error {
	switch source.EventType {
	case "payment.failed", "shipment.failed":
		if err := releaseReservedStock(ctx, tx, source.OrderID, at); err != nil {
			return err
		}
	}
	if source.EventType == "shipment.failed" {
		if err := voidAuthorizedPayment(ctx, tx, source.OrderID, at); err != nil {
			return err
		}
	}
	return nil
}

func releaseReservedStock(ctx context.Context, tx *sql.Tx, orderID string, at time.Time) error {
	if err := releaseInventoryItems(ctx, tx, orderID, at); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE stock_reservations
		SET status = 'released',
			released_at = $2
		WHERE order_id = $1
			AND status <> 'released'
	`, orderID, at); err != nil {
		return fmt.Errorf("release stock reservations: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE order_items
		SET reservation_status = 'released'
		WHERE order_id = $1
			AND reservation_status = 'reserved'
	`, orderID); err != nil {
		return fmt.Errorf("release order items: %w", err)
	}
	return nil
}

func voidAuthorizedPayment(ctx context.Context, tx *sql.Tx, orderID string, at time.Time) error {
	if _, err := tx.ExecContext(ctx, `
		UPDATE payment_authorizations
		SET status = 'voided',
			voided_at = $2
		WHERE order_id = $1
			AND status = 'authorized'
	`, orderID, at); err != nil {
		return fmt.Errorf("void payment authorization: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE orders
		SET payment_status = 'voided'
		WHERE order_id = $1
			AND payment_status = 'authorized'
	`, orderID); err != nil {
		return fmt.Errorf("void order payment: %w", err)
	}
	return nil
}

func (s *Store) OutboxEvents() []commerce.OutboxEvent {
	rows, err := s.db.QueryContext(context.Background(), `
		SELECT message_id, correlation_id, causation_id, event_type, order_id, merchant_id, payload, occurred_at
		FROM outbox_events
		ORDER BY occurred_at ASC
	`)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var events []commerce.OutboxEvent
	for rows.Next() {
		event, err := scanOutboxEvent(rows)
		if err != nil {
			return nil
		}
		events = append(events, event.WithEnvelopeDefaults())
	}
	return events
}

func (s *Store) AuditLogs() []commerce.AuditLog {
	rows, err := s.db.QueryContext(context.Background(), `
		SELECT merchant_id, order_id, actor_type, actor_id, action, correlation_id, created_at, details
		FROM audit_logs
		ORDER BY created_at ASC, id ASC
	`)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var logs []commerce.AuditLog
	for rows.Next() {
		var log commerce.AuditLog
		var orderID sql.NullString
		var details []byte
		if err := rows.Scan(&log.MerchantID, &orderID, &log.ActorType, &log.ActorID, &log.Action, &log.CorrelationID, &log.CreatedAt, &details); err != nil {
			return nil
		}
		if orderID.Valid {
			log.OrderID = orderID.String
		}
		if len(details) > 0 {
			if err := json.Unmarshal(details, &log.Details); err != nil {
				return nil
			}
		}
		logs = append(logs, log)
	}
	return logs
}

func (s *Store) RecordAuditLog(ctx context.Context, audit commerce.AuditLog) (err error) {
	ctx = contextOrBackground(ctx)
	ctx, span := postgresTracer().Start(ctx, "postgres.record_audit_log", trace.WithAttributes(
		attribute.String("db.system.name", "postgresql"),
		attribute.String("fulfillhub.audit_action", audit.Action),
		attribute.String("fulfillhub.actor_type", audit.ActorType),
		attribute.String("fulfillhub.actor_id", audit.ActorID),
		attribute.String("fulfillhub.correlation_id", audit.CorrelationID),
	))
	defer finishSpan(span, &err, "record audit log")

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin record audit log: %w", err)
	}
	defer rollback(tx)
	if err := insertAuditLog(ctx, tx, audit); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit record audit log: %w", err)
	}
	return nil
}

func (s *Store) PendingOutboxEvents(ctx context.Context, limit int) (events []commerce.OutboxEvent, err error) {
	ctx = contextOrBackground(ctx)
	ctx, span := postgresTracer().Start(ctx, "postgres.pending_outbox_events", trace.WithAttributes(
		attribute.String("db.system.name", "postgresql"),
		attribute.Int("fulfillhub.outbox.limit", limit),
	))
	defer finishSpan(span, &err, "load pending outbox events")

	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `
		WITH picked AS (
			SELECT message_id
			FROM outbox_events
			WHERE published_at IS NULL
				AND (claimed_until IS NULL OR claimed_until <= now())
			ORDER BY occurred_at ASC
			FOR UPDATE SKIP LOCKED
			LIMIT $1
		)
		UPDATE outbox_events event
		SET claimed_by = 'fulfillhub-outbox-relay',
			claimed_until = now() + interval '30 seconds'
		FROM picked
		WHERE event.message_id = picked.message_id
		RETURNING event.message_id, event.correlation_id, event.causation_id, event.event_type,
			event.order_id, event.merchant_id, event.payload, event.occurred_at
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("claim pending outbox events: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		event, err := scanOutboxEvent(rows)
		if err != nil {
			return nil, fmt.Errorf("scan pending outbox event: %w", err)
		}
		events = append(events, event.WithEnvelopeDefaults())
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pending outbox events: %w", err)
	}
	span.SetAttributes(attribute.Int("fulfillhub.outbox.pending_count", len(events)))
	return events, nil
}

func (s *Store) PendingOutboxCount(ctx context.Context) (count int, err error) {
	ctx = contextOrBackground(ctx)
	ctx, span := postgresTracer().Start(ctx, "postgres.pending_outbox_count", trace.WithAttributes(
		attribute.String("db.system.name", "postgresql"),
	))
	defer finishSpan(span, &err, "count pending outbox events")

	if err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM outbox_events
		WHERE published_at IS NULL
	`).Scan(&count); err != nil {
		return 0, fmt.Errorf("count pending outbox events: %w", err)
	}
	span.SetAttributes(attribute.Int("fulfillhub.outbox.pending_count", count))
	return count, nil
}

func (s *Store) OldestPendingOutboxAgeSeconds(ctx context.Context) (ageSeconds float64, err error) {
	ctx = contextOrBackground(ctx)
	ctx, span := postgresTracer().Start(ctx, "postgres.oldest_pending_outbox_age", trace.WithAttributes(
		attribute.String("db.system.name", "postgresql"),
	))
	defer finishSpan(span, &err, "measure oldest pending outbox age")

	if err := s.db.QueryRowContext(ctx, `
		SELECT COALESCE(EXTRACT(EPOCH FROM now() - MIN(occurred_at)), 0)
		FROM outbox_events
		WHERE published_at IS NULL
	`).Scan(&ageSeconds); err != nil {
		return 0, fmt.Errorf("measure oldest pending outbox age: %w", err)
	}
	span.SetAttributes(attribute.Float64("fulfillhub.outbox.oldest_pending_age_seconds", ageSeconds))
	return ageSeconds, nil
}

func (s *Store) OrderStatusCounts(ctx context.Context) (counts map[commerce.OrderStatus]int, err error) {
	ctx = contextOrBackground(ctx)
	ctx, span := postgresTracer().Start(ctx, "postgres.order_status_counts", trace.WithAttributes(
		attribute.String("db.system.name", "postgresql"),
	))
	defer finishSpan(span, &err, "count orders by status")

	rows, err := s.db.QueryContext(ctx, `
		SELECT status, COUNT(*)
		FROM orders
		GROUP BY status
	`)
	if err != nil {
		return nil, fmt.Errorf("count orders by status: %w", err)
	}
	defer rows.Close()

	counts = make(map[commerce.OrderStatus]int, len(commerce.ValidOrderStatuses()))
	for _, status := range commerce.ValidOrderStatuses() {
		counts[status] = 0
	}
	for rows.Next() {
		var status commerce.OrderStatus
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return nil, fmt.Errorf("scan order status count: %w", err)
		}
		counts[status] = count
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate order status counts: %w", err)
	}
	return counts, nil
}

func (s *Store) MarkOutboxPublished(ctx context.Context, messageID string, publishedAt time.Time) (err error) {
	ctx = contextOrBackground(ctx)
	ctx, span := postgresTracer().Start(ctx, "postgres.mark_outbox_published", trace.WithAttributes(
		attribute.String("db.system.name", "postgresql"),
		attribute.String("messaging.message.id", messageID),
	))
	defer finishSpan(span, &err, "mark outbox published")

	result, err := s.db.ExecContext(ctx, `
		UPDATE outbox_events
		SET published_at = $2,
			claimed_by = NULL,
			claimed_until = NULL
		WHERE message_id = $1 AND published_at IS NULL
	`, messageID, publishedAt)
	if err != nil {
		return fmt.Errorf("mark outbox event published: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("inspect outbox mark rows: %w", err)
	}
	if rows == 0 {
		return commerce.ErrNotFound
	}
	return nil
}

func (s *Store) RecordInboxMessage(ctx context.Context, consumerName string, event commerce.OutboxEvent) (recorded bool, err error) {
	ctx = contextOrBackground(ctx)
	ctx, span := postgresTracer().Start(ctx, "postgres.record_inbox_message", trace.WithAttributes(
		attribute.String("db.system.name", "postgresql"),
		attribute.String("messaging.message.id", event.MessageID),
		attribute.String("fulfillhub.consumer_name", consumerName),
		attribute.String("fulfillhub.correlation_id", event.CorrelationID),
	))
	defer finishSpan(span, &err, "record inbox message")

	result, err := s.db.ExecContext(ctx, `
		INSERT INTO inbox_messages (consumer_name, message_id, correlation_id)
		VALUES ($1, $2, $3)
		ON CONFLICT DO NOTHING
	`, consumerName, event.MessageID, event.CorrelationID)
	if err != nil {
		return false, fmt.Errorf("record inbox message: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("inspect inbox rows: %w", err)
	}
	recorded = rows == 1
	span.SetAttributes(attribute.Bool("fulfillhub.inbox_recorded", recorded))
	return recorded, nil
}

func (s *Store) ReleaseInboxMessage(ctx context.Context, consumerName string, event commerce.OutboxEvent) (err error) {
	ctx = contextOrBackground(ctx)
	ctx, span := postgresTracer().Start(ctx, "postgres.release_inbox_message", trace.WithAttributes(
		attribute.String("db.system.name", "postgresql"),
		attribute.String("messaging.message.id", event.MessageID),
		attribute.String("fulfillhub.consumer_name", consumerName),
		attribute.String("fulfillhub.correlation_id", event.CorrelationID),
	))
	defer finishSpan(span, &err, "release inbox message")

	if _, err := s.db.ExecContext(ctx, `
		DELETE FROM inbox_messages
		WHERE consumer_name = $1 AND message_id = $2
	`, consumerName, event.MessageID); err != nil {
		return fmt.Errorf("release inbox message: %w", err)
	}
	return nil
}

type queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type reservationItem struct {
	sku      string
	quantity int
}

func lockOrderStatusForUpdate(ctx context.Context, tx *sql.Tx, orderID string) (commerce.OrderStatus, error) {
	var status commerce.OrderStatus
	if err := tx.QueryRowContext(ctx, `
		SELECT status
		FROM orders
		WHERE order_id = $1
		FOR UPDATE
	`, orderID).Scan(&status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", commerce.ErrNotFound
		}
		return "", fmt.Errorf("lock order status: %w", err)
	}
	return status, nil
}

func reserveInventoryItem(ctx context.Context, tx *sql.Tx, merchantID, orderID string, item reservationItem, at time.Time) (string, error) {
	var existingWarehouse sql.NullString
	var existingQuantity int
	var existingStatus string
	if err := tx.QueryRowContext(ctx, `
		SELECT warehouse_id, quantity, status
		FROM stock_reservations
		WHERE order_id = $1 AND sku = $2
		FOR UPDATE
	`, orderID, item.sku).Scan(&existingWarehouse, &existingQuantity, &existingStatus); err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			return "", fmt.Errorf("lock existing stock reservation: %w", err)
		}
	} else if existingStatus == "reserved" {
		if existingQuantity != item.quantity {
			return "", fmt.Errorf("reservation quantity mismatch for sku %s", item.sku)
		}
		return existingWarehouse.String, nil
	}

	var warehouseID string
	if err := tx.QueryRowContext(ctx, `
		WITH candidate AS (
			SELECT ii.id, ii.warehouse_id
			FROM inventory_items ii
			JOIN warehouses w ON w.id = ii.warehouse_id
			WHERE w.merchant_id = $1
				AND w.status = 'active'
				AND ii.sku = $2
				AND ii.available_quantity >= $3
			ORDER BY ii.available_quantity DESC, ii.id ASC
			FOR UPDATE OF ii
			LIMIT 1
		)
		UPDATE inventory_items ii
		SET available_quantity = ii.available_quantity - $3,
			reserved_quantity = ii.reserved_quantity + $3,
			updated_at = $4
		FROM candidate
		WHERE ii.id = candidate.id
		RETURNING candidate.warehouse_id
	`, merchantID, item.sku, item.quantity, at).Scan(&warehouseID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", fmt.Errorf("%w for sku %s", commerce.ErrInsufficientStock, item.sku)
		}
		return "", fmt.Errorf("reserve inventory item: %w", err)
	}
	return warehouseID, nil
}

func releaseInventoryItems(ctx context.Context, tx *sql.Tx, orderID string, at time.Time) error {
	if _, err := tx.ExecContext(ctx, `
		UPDATE inventory_items ii
		SET available_quantity = ii.available_quantity + sr.quantity,
			reserved_quantity = GREATEST(ii.reserved_quantity - sr.quantity, 0),
			updated_at = $2
		FROM stock_reservations sr
		WHERE sr.order_id = $1
			AND sr.status = 'reserved'
			AND sr.warehouse_id IS NOT NULL
			AND ii.warehouse_id = sr.warehouse_id
			AND ii.sku = sr.sku
	`, orderID, at); err != nil {
		return fmt.Errorf("release inventory items: %w", err)
	}
	return nil
}

func orderItemsForReservation(ctx context.Context, q queryer, orderID string) ([]reservationItem, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT sku, quantity
		FROM order_items
		WHERE order_id = $1
		ORDER BY id ASC
	`, orderID)
	if err != nil {
		return nil, fmt.Errorf("load order items for reservation: %w", err)
	}
	defer rows.Close()

	var items []reservationItem
	for rows.Next() {
		var item reservationItem
		if err := rows.Scan(&item.sku, &item.quantity); err != nil {
			return nil, fmt.Errorf("scan order item for reservation: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate order items for reservation: %w", err)
	}
	if len(items) == 0 {
		return nil, commerce.ErrNotFound
	}
	return items, nil
}

func getOrderTx(ctx context.Context, q queryer, orderID string) (*commerce.Order, error) {
	var order commerce.Order
	var subtotalAmount, shippingAmount, totalAmount int64
	var paymentProvider, paymentStatus, paymentAuthorization, paymentCredentialRef, shippingAddressRef sql.NullString

	if err := q.QueryRowContext(ctx, `
		SELECT order_id, merchant_id, external_order_id, status, currency,
			subtotal_amount, shipping_amount, total_amount,
			payment_provider, payment_status, payment_authorization_id, payment_credential_ref, shipping_address_ref,
			created_at, updated_at
		FROM orders
		WHERE order_id = $1
	`, orderID).Scan(&order.OrderID, &order.MerchantID, &order.ExternalOrderID, &order.Status, &order.Currency,
		&subtotalAmount, &shippingAmount, &totalAmount,
		&paymentProvider, &paymentStatus, &paymentAuthorization, &paymentCredentialRef, &shippingAddressRef,
		&order.CreatedAt, &order.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, commerce.ErrNotFound
		}
		return nil, fmt.Errorf("get order: %w", err)
	}

	order.Totals = commerce.OrderTotals{
		Subtotal: commerce.Money{Amount: subtotalAmount, Currency: order.Currency},
		Shipping: commerce.Money{Amount: shippingAmount, Currency: order.Currency},
		Total:    commerce.Money{Amount: totalAmount, Currency: order.Currency},
	}
	if paymentProvider.Valid || paymentStatus.Valid || paymentAuthorization.Valid {
		order.Payment = &commerce.Payment{
			Provider:        paymentProvider.String,
			Status:          paymentStatus.String,
			AuthorizationID: paymentAuthorization.String,
			CredentialRef:   paymentCredentialRef.String,
		}
	}
	order.ShippingAddressRef = shippingAddressRef.String

	rows, err := q.QueryContext(ctx, `
		SELECT sku, quantity, unit_price_amount, unit_price_currency, reservation_status
		FROM order_items
		WHERE order_id = $1
		ORDER BY id ASC
	`, orderID)
	if err != nil {
		return nil, fmt.Errorf("get order items: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var item commerce.OrderItem
		if err := rows.Scan(&item.SKU, &item.Quantity, &item.UnitPrice.Amount, &item.UnitPrice.Currency, &item.ReservationStatus); err != nil {
			return nil, fmt.Errorf("scan order item: %w", err)
		}
		order.Items = append(order.Items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate order items: %w", err)
	}
	var shipment commerce.Shipment
	var shipmentCreatedAt time.Time
	if err := q.QueryRowContext(ctx, `
		SELECT shipment_id, status, carrier, tracking_number, created_at
		FROM shipments
		WHERE order_id = $1
		ORDER BY created_at DESC
		LIMIT 1
	`, orderID).Scan(&shipment.ShipmentID, &shipment.Status, &shipment.Carrier, &shipment.TrackingNumber, &shipmentCreatedAt); err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("get shipment: %w", err)
		}
	} else {
		shipment.Events = []commerce.ShipmentEvent{{
			OccurredAt:  shipmentCreatedAt,
			Status:      shipment.Status,
			Description: "Shipment projection created.",
		}}
		order.Shipment = &shipment
	}
	return &order, nil
}

func getShipmentTx(ctx context.Context, q queryer, shipmentID string) (*commerce.ShipmentRecord, error) {
	var record commerce.ShipmentRecord
	var shipmentCreatedAt time.Time
	if err := q.QueryRowContext(ctx, `
		SELECT shipment_id, order_id, merchant_id, carrier, tracking_number, status, created_at
		FROM shipments
		WHERE shipment_id = $1
	`, shipmentID).Scan(
		&record.ShipmentID,
		&record.OrderID,
		&record.MerchantID,
		&record.Carrier,
		&record.TrackingNumber,
		&record.Status,
		&shipmentCreatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, commerce.ErrNotFound
		}
		return nil, fmt.Errorf("get shipment: %w", err)
	}
	record.Events = []commerce.ShipmentEvent{{
		OccurredAt:  shipmentCreatedAt,
		Status:      record.Status,
		Description: "Shipment projection created.",
	}}
	return &record, nil
}

func orderByIdempotency(ctx context.Context, tx *sql.Tx, merchantID, idempotencyKey string) (string, bool, error) {
	var orderID string
	if err := tx.QueryRowContext(ctx, `
		SELECT order_id
		FROM idempotency_keys
		WHERE merchant_id = $1 AND idempotency_key = $2
	`, merchantID, idempotencyKey).Scan(&orderID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("lookup idempotency key: %w", err)
	}
	return orderID, true, nil
}

func insertOutboxEvent(ctx context.Context, tx *sql.Tx, event commerce.OutboxEvent) error {
	event = event.WithEnvelopeDefaults()
	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal outbox event: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO outbox_events (
			message_id, correlation_id, causation_id, event_type, order_id, merchant_id, payload, occurred_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`, event.MessageID, event.CorrelationID, event.CausationID, event.EventType, event.OrderID, event.MerchantID, payload, event.OccurredAt); err != nil {
		return fmt.Errorf("insert outbox event: %w", err)
	}
	return nil
}

func scanOutboxEvent(rows *sql.Rows) (commerce.OutboxEvent, error) {
	var event commerce.OutboxEvent
	var payload []byte
	if err := rows.Scan(&event.MessageID, &event.CorrelationID, &event.CausationID, &event.EventType, &event.OrderID, &event.MerchantID, &payload, &event.OccurredAt); err != nil {
		return commerce.OutboxEvent{}, err
	}
	if len(payload) > 0 {
		var envelope commerce.OutboxEvent
		if err := json.Unmarshal(payload, &envelope); err != nil {
			return commerce.OutboxEvent{}, err
		}
		if envelope.MessageID == "" {
			envelope.MessageID = event.MessageID
		}
		if envelope.CorrelationID == "" {
			envelope.CorrelationID = event.CorrelationID
		}
		if envelope.CausationID == "" {
			envelope.CausationID = event.CausationID
		}
		if envelope.EventType == "" {
			envelope.EventType = event.EventType
		}
		if envelope.OrderID == "" {
			envelope.OrderID = event.OrderID
		}
		if envelope.MerchantID == "" {
			envelope.MerchantID = event.MerchantID
		}
		if envelope.OccurredAt.IsZero() {
			envelope.OccurredAt = event.OccurredAt
		}
		return envelope.WithEnvelopeDefaults(), nil
	}
	return event.WithEnvelopeDefaults(), nil
}

func orderStatusEventPayload(status commerce.OrderStatus, at time.Time, audit commerce.AuditLog) map[string]any {
	payload := map[string]any{"order_status": string(status)}
	switch status {
	case commerce.StatusCompleted:
		payload["completed_at"] = at.UTC().Format(time.RFC3339Nano)
	case commerce.StatusCancelled:
		payload["cancelled_at"] = at.UTC().Format(time.RFC3339Nano)
	case commerce.StatusManualReview:
		if reason := audit.Details["review_reason"]; reason != "" {
			payload["review_reason"] = reason
		}
	}
	return payload
}

func failurePayload(stage, reason string) map[string]any {
	if reason == "" {
		reason = "unspecified"
	}
	return map[string]any{
		"failure": map[string]any{
			"stage":  stage,
			"reason": reason,
		},
	}
}

func upsertMerchant(ctx context.Context, tx *sql.Tx, merchantID string) error {
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO merchants (id, name)
		VALUES ($1, $1)
		ON CONFLICT (id) DO NOTHING
	`, merchantID); err != nil {
		return fmt.Errorf("upsert merchant: %w", err)
	}
	return nil
}

func insertAuditLog(ctx context.Context, tx *sql.Tx, audit commerce.AuditLog) error {
	if audit.Details == nil {
		audit.Details = map[string]string{}
	}
	details, err := json.Marshal(audit.Details)
	if err != nil {
		return fmt.Errorf("marshal audit details: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO audit_logs (
			merchant_id, order_id, actor_type, actor_id, action, correlation_id, created_at, details
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`, audit.MerchantID, nullableString(audit.OrderID), audit.ActorType, audit.ActorID, audit.Action, audit.CorrelationID, audit.CreatedAt, details); err != nil {
		return fmt.Errorf("insert audit log: %w", err)
	}
	return nil
}

func nullableString(value string) sql.NullString {
	if value == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: value, Valid: true}
}

func nullablePaymentProvider(order *commerce.Order) sql.NullString {
	if order.Payment == nil || order.Payment.Provider == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: order.Payment.Provider, Valid: true}
}

func nullablePaymentStatus(order *commerce.Order) sql.NullString {
	if order.Payment == nil || order.Payment.Status == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: order.Payment.Status, Valid: true}
}

func nullablePaymentAuthorization(order *commerce.Order) sql.NullString {
	if order.Payment == nil || order.Payment.AuthorizationID == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: order.Payment.AuthorizationID, Valid: true}
}

func nullablePaymentCredentialRef(order *commerce.Order) sql.NullString {
	if order.Payment == nil || order.Payment.CredentialRef == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: order.Payment.CredentialRef, Valid: true}
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func rollback(tx *sql.Tx) {
	_ = tx.Rollback()
}

func contextOrBackground(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func postgresTracer() trace.Tracer {
	return otel.Tracer("github.com/Defyland/fulfillhub-go-commerce-platform/internal/postgres")
}

func finishSpan(span trace.Span, err *error, description string) {
	if err != nil && *err != nil {
		span.RecordError(*err)
		span.SetStatus(codes.Error, description)
	}
	span.End()
}
