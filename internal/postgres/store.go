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

func (s *Store) InsertOrder(merchantID, idempotencyKey string, order *commerce.Order, event commerce.OutboxEvent, audit commerce.AuditLog) (*commerce.Order, bool, error) {
	ctx := context.Background()
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
		return order, true, nil
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO orders (
			order_id, merchant_id, external_order_id, status, currency,
			subtotal_amount, shipping_amount, total_amount,
			payment_provider, payment_status, payment_authorization_id,
			created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
	`, order.OrderID, merchantID, order.ExternalOrderID, order.Status, order.Currency,
		order.Totals.Subtotal.Amount, order.Totals.Shipping.Amount, order.Totals.Total.Amount,
		nullablePaymentProvider(order), nullablePaymentStatus(order), nullablePaymentAuthorization(order),
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
	return commerce.CloneOrderForStore(order), false, nil
}

func (s *Store) GetOrder(orderID string) (*commerce.Order, error) {
	return getOrderTx(context.Background(), s.db, orderID)
}

func (s *Store) UpdateOrderStatus(orderID string, status commerce.OrderStatus, now time.Time, event commerce.OutboxEvent, audit commerce.AuditLog) (*commerce.Order, error) {
	ctx := context.Background()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin update order status: %w", err)
	}
	defer rollback(tx)

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

	if err := insertOutboxEvent(ctx, tx, event); err != nil {
		return nil, err
	}
	if err := insertAuditLog(ctx, tx, audit); err != nil {
		return nil, err
	}
	order, err := getOrderTx(ctx, tx, orderID)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit update order status: %w", err)
	}
	return order, nil
}

func (s *Store) OutboxEvents() []commerce.OutboxEvent {
	rows, err := s.db.QueryContext(context.Background(), `
		SELECT message_id, correlation_id, event_type, order_id, merchant_id, occurred_at
		FROM outbox_events
		ORDER BY occurred_at ASC
	`)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var events []commerce.OutboxEvent
	for rows.Next() {
		var event commerce.OutboxEvent
		if err := rows.Scan(&event.MessageID, &event.CorrelationID, &event.EventType, &event.OrderID, &event.MerchantID, &event.OccurredAt); err != nil {
			return nil
		}
		events = append(events, event)
	}
	return events
}

func (s *Store) AuditLogs() []commerce.AuditLog {
	rows, err := s.db.QueryContext(context.Background(), `
		SELECT merchant_id, order_id, actor_type, actor_id, action, correlation_id, created_at
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
		if err := rows.Scan(&log.MerchantID, &orderID, &log.ActorType, &log.ActorID, &log.Action, &log.CorrelationID, &log.CreatedAt); err != nil {
			return nil
		}
		if orderID.Valid {
			log.OrderID = orderID.String
		}
		logs = append(logs, log)
	}
	return logs
}

func (s *Store) PendingOutboxEvents(ctx context.Context, limit int) ([]commerce.OutboxEvent, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT message_id, correlation_id, event_type, order_id, merchant_id, occurred_at
		FROM outbox_events
		WHERE published_at IS NULL
		ORDER BY occurred_at ASC
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("query pending outbox events: %w", err)
	}
	defer rows.Close()

	var events []commerce.OutboxEvent
	for rows.Next() {
		var event commerce.OutboxEvent
		if err := rows.Scan(&event.MessageID, &event.CorrelationID, &event.EventType, &event.OrderID, &event.MerchantID, &event.OccurredAt); err != nil {
			return nil, fmt.Errorf("scan pending outbox event: %w", err)
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pending outbox events: %w", err)
	}
	return events, nil
}

func (s *Store) MarkOutboxPublished(ctx context.Context, messageID string, publishedAt time.Time) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE outbox_events
		SET published_at = $2
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

func (s *Store) RecordInboxMessage(ctx context.Context, consumerName string, event commerce.OutboxEvent) (bool, error) {
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
	return rows == 1, nil
}

type queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func getOrderTx(ctx context.Context, q queryer, orderID string) (*commerce.Order, error) {
	var order commerce.Order
	var subtotalAmount, shippingAmount, totalAmount int64
	var paymentProvider, paymentStatus, paymentAuthorization sql.NullString

	if err := q.QueryRowContext(ctx, `
		SELECT order_id, merchant_id, external_order_id, status, currency,
			subtotal_amount, shipping_amount, total_amount,
			payment_provider, payment_status, payment_authorization_id,
			created_at, updated_at
		FROM orders
		WHERE order_id = $1
	`, orderID).Scan(&order.OrderID, &order.MerchantID, &order.ExternalOrderID, &order.Status, &order.Currency,
		&subtotalAmount, &shippingAmount, &totalAmount,
		&paymentProvider, &paymentStatus, &paymentAuthorization,
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
		}
	}

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
	return &order, nil
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
	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal outbox event: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO outbox_events (
			message_id, correlation_id, event_type, order_id, merchant_id, payload, occurred_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, event.MessageID, event.CorrelationID, event.EventType, event.OrderID, event.MerchantID, payload, event.OccurredAt); err != nil {
		return fmt.Errorf("insert outbox event: %w", err)
	}
	return nil
}

func insertAuditLog(ctx context.Context, tx *sql.Tx, audit commerce.AuditLog) error {
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO audit_logs (
			merchant_id, order_id, actor_type, actor_id, action, correlation_id, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, audit.MerchantID, nullableString(audit.OrderID), audit.ActorType, audit.ActorID, audit.Action, audit.CorrelationID, audit.CreatedAt); err != nil {
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

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func rollback(tx *sql.Tx) {
	_ = tx.Rollback()
}
