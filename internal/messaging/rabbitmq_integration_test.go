package messaging

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/Defyland/fulfillhub-go-commerce-platform/internal/commerce"
	amqp "github.com/rabbitmq/amqp091-go"
)

func TestRabbitPublisherIntegration(t *testing.T) {
	rabbitURL := os.Getenv("RABBITMQ_URL")
	if rabbitURL == "" {
		t.Skip("RABBITMQ_URL not set")
	}

	conn, err := amqp.Dial(rabbitURL)
	if err != nil {
		t.Fatalf("dial rabbitmq: %v", err)
	}
	defer conn.Close()
	channel, err := conn.Channel()
	if err != nil {
		t.Fatalf("open channel: %v", err)
	}
	defer channel.Close()

	if err := DeclareTopology(channel); err != nil {
		t.Fatalf("declare topology: %v", err)
	}
	if _, err := channel.QueuePurge(InventoryReserveQueue, false); err != nil {
		t.Fatalf("purge queue: %v", err)
	}

	publisher, err := NewRabbitPublisher(rabbitURL)
	if err != nil {
		t.Fatalf("new publisher: %v", err)
	}
	defer publisher.Close()

	event := commerce.OutboxEvent{
		MessageID:     "msg_integration_" + time.Now().UTC().Format("20060102150405"),
		CorrelationID: "cor_integration",
		CausationID:   "msg_root_integration",
		EventType:     "order.created",
		OrderID:       "ord_integration",
		MerchantID:    "mer_integration",
		OccurredAt:    time.Now().UTC(),
	}
	if err := publisher.Publish(context.Background(), event); err != nil {
		t.Fatalf("publish event: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		delivery, ok, err := channel.Get(InventoryReserveQueue, false)
		if err != nil {
			t.Fatalf("get delivery: %v", err)
		}
		if !ok {
			time.Sleep(100 * time.Millisecond)
			continue
		}
		defer delivery.Ack(false)

		if delivery.MessageId != event.MessageID {
			t.Fatalf("message id = %q, want %q", delivery.MessageId, event.MessageID)
		}
		if delivery.CorrelationId != event.CorrelationID {
			t.Fatalf("correlation id = %q, want %q", delivery.CorrelationId, event.CorrelationID)
		}
		if delivery.Headers["causation_id"] != event.CausationID {
			t.Fatalf("causation header = %q, want %q", delivery.Headers["causation_id"], event.CausationID)
		}
		if delivery.Type != event.EventType {
			t.Fatalf("type = %q, want %q", delivery.Type, event.EventType)
		}
		var decoded commerce.OutboxEvent
		if err := json.Unmarshal(delivery.Body, &decoded); err != nil {
			t.Fatalf("decode delivery body: %v", err)
		}
		if decoded.OrderID != event.OrderID || decoded.MerchantID != event.MerchantID || decoded.CausationID != event.CausationID {
			t.Fatalf("decoded event = %+v, want order/merchant from published event", decoded)
		}
		return
	}

	t.Fatal("timed out waiting for order.created delivery")
}
