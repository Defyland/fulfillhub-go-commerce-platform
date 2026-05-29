package messaging

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Defyland/fulfillhub-go-commerce-platform/internal/commerce"
	amqp "github.com/rabbitmq/amqp091-go"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestRelayPublishesAndMarksPendingEvents(t *testing.T) {
	source := &fakeOutboxSource{
		events: []commerce.OutboxEvent{
			{MessageID: "msg_1", EventType: "order.created", CorrelationID: "cor_1", CausationID: "msg_1"},
			{MessageID: "msg_2", EventType: "payment.authorized", CorrelationID: "cor_1", CausationID: "msg_1"},
		},
	}
	publisher := &fakePublisher{}
	now := time.Date(2026, 5, 28, 20, 0, 0, 0, time.UTC)

	published, err := (Relay{
		Source:    source,
		Publisher: publisher,
		Clock:     func() time.Time { return now },
	}).RunOnce(context.Background(), 10)
	if err != nil {
		t.Fatalf("RunOnce returned error: %v", err)
	}

	if published != 2 {
		t.Fatalf("published = %d, want 2", published)
	}
	if len(publisher.events) != 2 {
		t.Fatalf("published events = %d, want 2", len(publisher.events))
	}
	if got := source.marked["msg_1"]; !got.Equal(now) {
		t.Fatalf("msg_1 marked at %v, want %v", got, now)
	}
}

func TestRelayCreatesOutboxPublishSpans(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() {
		if err := provider.Shutdown(context.Background()); err != nil {
			t.Fatalf("shutdown tracer provider: %v", err)
		}
	})
	source := &fakeOutboxSource{
		events: []commerce.OutboxEvent{
			{
				MessageID:     "msg_1",
				EventType:     "order.created",
				CorrelationID: "cor_1",
				CausationID:   "msg_1",
				OrderID:       "ord_1",
				MerchantID:    "mer_1",
			},
		},
	}

	published, err := (Relay{
		Source:    source,
		Publisher: &fakePublisher{},
		Tracer:    provider.Tracer("test-relay"),
	}).RunOnce(context.Background(), 10)
	if err != nil {
		t.Fatalf("RunOnce returned error: %v", err)
	}
	if published != 1 {
		t.Fatalf("published = %d, want 1", published)
	}

	publishSpan := findSpanByName(t, recorder.Ended(), "outbox.publish")
	assertSpanAttr(t, publishSpan, "messaging.message.id", "msg_1")
	assertSpanAttr(t, publishSpan, "fulfillhub.event_type", "order.created")
	assertSpanAttr(t, publishSpan, "fulfillhub.correlation_id", "cor_1")
	assertSpanAttr(t, publishSpan, "fulfillhub.causation_id", "msg_1")
}

func TestInjectTraceHeadersAddsTraceparent(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() {
		if err := provider.Shutdown(context.Background()); err != nil {
			t.Fatalf("shutdown tracer provider: %v", err)
		}
	})
	ctx, span := provider.Tracer("test-rabbit").Start(context.Background(), "publish-parent")
	defer span.End()
	headers := amqp.Table{}

	injectTraceHeaders(ctx, headers)

	traceparent, ok := headers["traceparent"].(string)
	if !ok {
		t.Fatalf("traceparent header missing: %+v", headers)
	}
	if !strings.HasPrefix(traceparent, "00-") {
		t.Fatalf("traceparent = %q, want W3C trace context", traceparent)
	}
}

func TestRelayStopsBeforeMarkingFailedPublish(t *testing.T) {
	source := &fakeOutboxSource{
		events: []commerce.OutboxEvent{{MessageID: "msg_1", EventType: "order.created"}},
	}
	publisher := &fakePublisher{err: errors.New("broker unavailable")}

	published, err := (Relay{Source: source, Publisher: publisher}).RunOnce(context.Background(), 10)
	if err == nil {
		t.Fatal("RunOnce must return publish error")
	}
	if published != 0 {
		t.Fatalf("published = %d, want 0", published)
	}
	if len(source.marked) != 0 {
		t.Fatalf("marked events = %d, want 0", len(source.marked))
	}
}

func TestMemoryInboxDeduplicatesByConsumerAndMessage(t *testing.T) {
	inbox := NewMemoryInbox()
	event := commerce.OutboxEvent{MessageID: "msg_1", CorrelationID: "cor_1"}

	first, err := inbox.Record(context.Background(), "inventory.reserve", event)
	if err != nil {
		t.Fatalf("first record returned error: %v", err)
	}
	second, err := inbox.Record(context.Background(), "inventory.reserve", event)
	if err != nil {
		t.Fatalf("second record returned error: %v", err)
	}
	otherConsumer, err := inbox.Record(context.Background(), "payments.authorize", event)
	if err != nil {
		t.Fatalf("other consumer record returned error: %v", err)
	}

	if !first {
		t.Fatal("first record must be new")
	}
	if second {
		t.Fatal("second record for same consumer/message must be duplicate")
	}
	if !otherConsumer {
		t.Fatal("same message id for a different consumer must be new")
	}
}

func TestRoutingKeyUsesEventType(t *testing.T) {
	if got := RoutingKey(" order.created "); got != "order.created" {
		t.Fatalf("routing key = %q, want order.created", got)
	}
}

type fakeOutboxSource struct {
	events []commerce.OutboxEvent
	marked map[string]time.Time
}

func (s *fakeOutboxSource) PendingOutboxEvents(context.Context, int) ([]commerce.OutboxEvent, error) {
	return s.events, nil
}

func (s *fakeOutboxSource) MarkOutboxPublished(_ context.Context, messageID string, publishedAt time.Time) error {
	if s.marked == nil {
		s.marked = make(map[string]time.Time)
	}
	s.marked[messageID] = publishedAt
	return nil
}

type fakePublisher struct {
	events []commerce.OutboxEvent
	err    error
}

func (p *fakePublisher) Publish(_ context.Context, event commerce.OutboxEvent) error {
	if p.err != nil {
		return p.err
	}
	p.events = append(p.events, event)
	return nil
}

func findSpanByName(t testing.TB, spans []sdktrace.ReadOnlySpan, name string) sdktrace.ReadOnlySpan {
	t.Helper()
	for _, span := range spans {
		if span.Name() == name {
			return span
		}
	}
	t.Fatalf("span %q not found; got %d spans", name, len(spans))
	return nil
}

func assertSpanAttr(t testing.TB, span sdktrace.ReadOnlySpan, key, want string) {
	t.Helper()
	for _, attr := range span.Attributes() {
		if string(attr.Key) == key {
			if got := attr.Value.AsString(); got != want {
				t.Fatalf("span attr %s = %q, want %q", key, got, want)
			}
			return
		}
	}
	t.Fatalf("span attr %s not found", key)
}
