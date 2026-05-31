package messaging

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/Defyland/fulfillhub-go-commerce-platform/internal/commerce"
	amqp "github.com/rabbitmq/amqp091-go"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func TestConsumerRecordsInboxRunsHandlerAndAcks(t *testing.T) {
	inbox := NewMemoryInbox()
	ack := &fakeAcknowledger{}
	event := commerce.OutboxEvent{
		MessageID:     "msg_1",
		CorrelationID: "cor_1",
		CausationID:   "msg_root",
		EventType:     "order.created",
		OrderID:       "ord_1",
		MerchantID:    "mer_1",
	}
	handled := 0
	consumer := Consumer{
		Queue:        InventoryReserveQueue,
		ConsumerName: "inventory-worker",
		Inbox:        inbox,
		Handler: HandlerFunc(func(_ context.Context, got commerce.OutboxEvent) error {
			handled++
			if got.MessageID != event.MessageID {
				t.Fatalf("message id = %q, want %q", got.MessageID, event.MessageID)
			}
			if got.CausationID != event.CausationID {
				t.Fatalf("causation id = %q, want %q", got.CausationID, event.CausationID)
			}
			return nil
		}),
	}

	if err := consumer.ProcessDelivery(context.Background(), deliveryForTest(t, ack, event)); err != nil {
		t.Fatalf("ProcessDelivery returned error: %v", err)
	}

	if handled != 1 {
		t.Fatalf("handler calls = %d, want 1", handled)
	}
	if ack.acked != 1 {
		t.Fatalf("acked deliveries = %d, want 1", ack.acked)
	}
	if ack.nacked != 0 {
		t.Fatalf("nacked deliveries = %d, want 0", ack.nacked)
	}
}

func TestConsumerSkipsDuplicateInboxMessageAndAcks(t *testing.T) {
	inbox := NewMemoryInbox()
	event := commerce.OutboxEvent{
		MessageID:     "msg_1",
		CorrelationID: "cor_1",
		CausationID:   "msg_root",
		EventType:     "order.created",
	}
	if recorded, err := inbox.Record(context.Background(), "inventory-worker", event); err != nil || !recorded {
		t.Fatalf("seed inbox recorded=%v err=%v, want recorded message", recorded, err)
	}
	ack := &fakeAcknowledger{}
	consumer := Consumer{
		Queue:        InventoryReserveQueue,
		ConsumerName: "inventory-worker",
		Inbox:        inbox,
		Handler: HandlerFunc(func(context.Context, commerce.OutboxEvent) error {
			t.Fatal("handler must not run for duplicate message")
			return nil
		}),
	}

	if err := consumer.ProcessDelivery(context.Background(), deliveryForTest(t, ack, event)); err != nil {
		t.Fatalf("ProcessDelivery returned error: %v", err)
	}

	if ack.acked != 1 {
		t.Fatalf("acked deliveries = %d, want 1", ack.acked)
	}
	if ack.nacked != 0 {
		t.Fatalf("nacked deliveries = %d, want 0", ack.nacked)
	}
}

func TestConsumerNacksHandlerFailure(t *testing.T) {
	ack := &fakeAcknowledger{}
	consumer := Consumer{
		Queue:        InventoryReserveQueue,
		ConsumerName: "inventory-worker",
		Inbox:        NewMemoryInbox(),
		Handler: HandlerFunc(func(context.Context, commerce.OutboxEvent) error {
			return errors.New("provider timeout")
		}),
	}

	err := consumer.ProcessDelivery(context.Background(), deliveryForTest(t, ack, commerce.OutboxEvent{
		MessageID: "msg_1",
		EventType: "order.created",
	}))
	if err == nil {
		t.Fatal("ProcessDelivery must return handler error")
	}
	if ack.acked != 0 {
		t.Fatalf("acked deliveries = %d, want 0", ack.acked)
	}
	if ack.nacked != 1 {
		t.Fatalf("nacked deliveries = %d, want 1", ack.nacked)
	}
	if ack.requeued {
		t.Fatal("failed delivery must be dead-lettered instead of immediate requeue")
	}
}

func TestConsumerPublishesRetryAndAcksHandlerFailure(t *testing.T) {
	inbox := NewMemoryInbox()
	ack := &fakeAcknowledger{}
	retry := &fakeRetryPublisher{}
	event := commerce.OutboxEvent{
		MessageID:     "msg_1",
		CorrelationID: "cor_1",
		CausationID:   "msg_root",
		EventType:     "order.created",
	}
	consumer := Consumer{
		Queue:        InventoryReserveQueue,
		ConsumerName: "inventory-worker",
		Inbox:        inbox,
		Retry:        retry,
		Handler: HandlerFunc(func(context.Context, commerce.OutboxEvent) error {
			return errors.New("provider timeout")
		}),
	}

	err := consumer.ProcessDelivery(context.Background(), deliveryForTest(t, ack, event))
	if err == nil {
		t.Fatal("ProcessDelivery must return handler error")
	}
	if retry.calls != 1 {
		t.Fatalf("retry publishes = %d, want 1", retry.calls)
	}
	if retry.attempt != 1 {
		t.Fatalf("retry attempt = %d, want 1", retry.attempt)
	}
	if retry.event.MessageID != event.MessageID {
		t.Fatalf("retry event message id = %q, want %q", retry.event.MessageID, event.MessageID)
	}
	if retry.event.CausationID != event.CausationID {
		t.Fatalf("retry event causation id = %q, want %q", retry.event.CausationID, event.CausationID)
	}
	if ack.acked != 1 {
		t.Fatalf("acked deliveries = %d, want 1", ack.acked)
	}
	if ack.nacked != 0 {
		t.Fatalf("nacked deliveries = %d, want 0", ack.nacked)
	}
	recorded, err := inbox.Record(context.Background(), "inventory-worker", event)
	if err != nil {
		t.Fatalf("record inbox after retry release: %v", err)
	}
	if !recorded {
		t.Fatal("inbox entry must be released before scheduling retry")
	}
}

func TestConsumerNacksAfterRetryAttemptsExhausted(t *testing.T) {
	ack := &fakeAcknowledger{}
	retry := &fakeRetryPublisher{}
	delivery := deliveryForTest(t, ack, commerce.OutboxEvent{
		MessageID:     "msg_1",
		CorrelationID: "cor_1",
		EventType:     "order.created",
	})
	delivery.Headers["fulfillhub_retry_attempt"] = int32(2)
	consumer := Consumer{
		Queue:        InventoryReserveQueue,
		ConsumerName: "inventory-worker",
		Inbox:        NewMemoryInbox(),
		Retry:        retry,
		MaxRetries:   2,
		Handler: HandlerFunc(func(context.Context, commerce.OutboxEvent) error {
			return errors.New("provider timeout")
		}),
	}

	err := consumer.ProcessDelivery(context.Background(), delivery)
	if err == nil {
		t.Fatal("ProcessDelivery must return handler error")
	}
	if retry.calls != 0 {
		t.Fatalf("retry publishes = %d, want 0 after attempts exhausted", retry.calls)
	}
	if ack.acked != 0 {
		t.Fatalf("acked deliveries = %d, want 0", ack.acked)
	}
	if ack.nacked != 1 {
		t.Fatalf("nacked deliveries = %d, want 1", ack.nacked)
	}
}

func TestConsumerRejectsUnsupportedSchemaVersion(t *testing.T) {
	ack := &fakeAcknowledger{}
	delivery := deliveryForTest(t, ack, commerce.OutboxEvent{
		MessageID:     "msg_schema",
		CorrelationID: "cor_1",
		CausationID:   "msg_root",
		EventType:     "order.created",
		SchemaVersion: 99,
		OrderID:       "ord_1",
		MerchantID:    "mer_1",
	})
	consumer := Consumer{
		Queue:        InventoryReserveQueue,
		ConsumerName: "inventory-worker",
		Inbox:        NewMemoryInbox(),
		Handler:      HandlerFunc(func(context.Context, commerce.OutboxEvent) error { return nil }),
	}

	err := consumer.ProcessDelivery(context.Background(), delivery)
	if err == nil {
		t.Fatal("ProcessDelivery must reject unsupported schema versions")
	}
	if ack.acked != 0 || ack.nacked != 1 {
		t.Fatalf("ack state = acked %d nacked %d, want nack only", ack.acked, ack.nacked)
	}
}

func TestConsumerDoesNotRetryWhenInboxReleaseFails(t *testing.T) {
	ack := &fakeAcknowledger{}
	retry := &fakeRetryPublisher{}
	consumer := Consumer{
		Queue:        InventoryReserveQueue,
		ConsumerName: "inventory-worker",
		Inbox:        failingReleaseInbox{MemoryInbox: NewMemoryInbox()},
		Retry:        retry,
		Handler: HandlerFunc(func(context.Context, commerce.OutboxEvent) error {
			return errors.New("provider timeout")
		}),
	}

	err := consumer.ProcessDelivery(context.Background(), deliveryForTest(t, ack, commerce.OutboxEvent{
		MessageID:     "msg_1",
		CorrelationID: "cor_1",
		EventType:     "order.created",
	}))
	if err == nil {
		t.Fatal("ProcessDelivery must return handler error")
	}
	if retry.calls != 0 {
		t.Fatalf("retry publishes = %d, want 0 when inbox release fails", retry.calls)
	}
	if ack.acked != 0 {
		t.Fatalf("acked deliveries = %d, want 0", ack.acked)
	}
	if ack.nacked != 1 {
		t.Fatalf("nacked deliveries = %d, want 1", ack.nacked)
	}
}

func TestConsumerReleasesInboxAfterHandlerFailure(t *testing.T) {
	inbox := NewMemoryInbox()
	event := commerce.OutboxEvent{
		MessageID:     "msg_1",
		CorrelationID: "cor_1",
		EventType:     "order.created",
	}
	attempts := 0
	consumer := Consumer{
		Queue:        InventoryReserveQueue,
		ConsumerName: "inventory-worker",
		Inbox:        inbox,
		Handler: HandlerFunc(func(context.Context, commerce.OutboxEvent) error {
			attempts++
			if attempts == 1 {
				return errors.New("provider timeout")
			}
			return nil
		}),
	}

	firstAck := &fakeAcknowledger{}
	if err := consumer.ProcessDelivery(context.Background(), deliveryForTest(t, firstAck, event)); err == nil {
		t.Fatal("first ProcessDelivery must return handler error")
	}
	secondAck := &fakeAcknowledger{}
	if err := consumer.ProcessDelivery(context.Background(), deliveryForTest(t, secondAck, event)); err != nil {
		t.Fatalf("second ProcessDelivery returned error: %v", err)
	}

	if attempts != 2 {
		t.Fatalf("handler attempts = %d, want 2", attempts)
	}
	if firstAck.nacked != 1 {
		t.Fatalf("first delivery nacks = %d, want 1", firstAck.nacked)
	}
	if secondAck.acked != 1 {
		t.Fatalf("second delivery acks = %d, want 1", secondAck.acked)
	}
}

func TestConsumerExtractsTraceparentAndCreatesConsumeSpan(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() {
		if err := provider.Shutdown(context.Background()); err != nil {
			t.Fatalf("shutdown tracer provider: %v", err)
		}
	})
	parentCtx, parentSpan := provider.Tracer("test-consumer-parent").Start(context.Background(), "publish-parent")
	parentTraceID := trace.SpanContextFromContext(parentCtx).TraceID()
	headers := amqp.Table{}
	injectTraceHeaders(parentCtx, headers)
	parentSpan.End()

	ack := &fakeAcknowledger{}
	consumer := Consumer{
		Queue:        InventoryReserveQueue,
		ConsumerName: "inventory-worker",
		Inbox:        NewMemoryInbox(),
		Tracer:       provider.Tracer("test-consumer"),
		Handler: HandlerFunc(func(ctx context.Context, _ commerce.OutboxEvent) error {
			if got := trace.SpanContextFromContext(ctx).TraceID(); got != parentTraceID {
				t.Fatalf("handler trace id = %s, want %s", got, parentTraceID)
			}
			return nil
		}),
	}
	delivery := deliveryForTest(t, ack, commerce.OutboxEvent{
		MessageID:     "msg_1",
		CorrelationID: "cor_1",
		EventType:     "order.created",
		OrderID:       "ord_1",
		MerchantID:    "mer_1",
	})
	delivery.Headers = headers

	if err := consumer.ProcessDelivery(context.Background(), delivery); err != nil {
		t.Fatalf("ProcessDelivery returned error: %v", err)
	}

	span := findSpanByName(t, recorder.Ended(), "rabbitmq.consume")
	if got := span.Parent().TraceID(); got != parentTraceID {
		t.Fatalf("consume span parent trace id = %s, want %s", got, parentTraceID)
	}
	assertSpanAttr(t, span, "messaging.destination.name", InventoryReserveQueue)
	assertSpanAttr(t, span, "fulfillhub.consumer_name", "inventory-worker")
	assertSpanAttr(t, span, "fulfillhub.event_type", "order.created")
}

func deliveryForTest(t testing.TB, ack *fakeAcknowledger, event commerce.OutboxEvent) amqp.Delivery {
	t.Helper()
	body, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}
	return amqp.Delivery{
		Acknowledger:  ack,
		DeliveryTag:   1,
		Body:          body,
		Headers:       deliveryHeadersForTest(event),
		MessageId:     event.MessageID,
		CorrelationId: event.CorrelationID,
		Type:          event.EventType,
		RoutingKey:    event.EventType,
	}
}

func deliveryHeadersForTest(event commerce.OutboxEvent) amqp.Table {
	headers := amqp.Table{}
	if event.CausationID != "" {
		headers["causation_id"] = event.CausationID
	}
	return headers
}

type fakeAcknowledger struct {
	acked    int
	nacked   int
	rejected int
	requeued bool
}

type fakeRetryPublisher struct {
	calls    int
	attempt  int
	event    commerce.OutboxEvent
	delivery amqp.Delivery
}

func (p *fakeRetryPublisher) PublishRetry(_ context.Context, delivery amqp.Delivery, event commerce.OutboxEvent, attempt int) error {
	p.calls++
	p.delivery = delivery
	p.event = event
	p.attempt = attempt
	return nil
}

type failingReleaseInbox struct {
	*MemoryInbox
}

func (i failingReleaseInbox) Release(context.Context, string, commerce.OutboxEvent) error {
	return errors.New("release failed")
}

func (a *fakeAcknowledger) Ack(uint64, bool) error {
	a.acked++
	return nil
}

func (a *fakeAcknowledger) Nack(_ uint64, _ bool, requeue bool) error {
	a.nacked++
	a.requeued = requeue
	return nil
}

func (a *fakeAcknowledger) Reject(_ uint64, requeue bool) error {
	a.rejected++
	a.requeued = requeue
	return nil
}
