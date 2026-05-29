package main

import (
	"testing"

	"github.com/Defyland/fulfillhub-go-commerce-platform/internal/messaging"
)

func TestLoadSettingsDefaultsWorkerQueueAndConsumerName(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://example")
	t.Setenv("RABBITMQ_URL", "amqp://example")
	t.Setenv("WORKER_QUEUE", "")
	t.Setenv("CONSUMER_NAME", "")

	cfg, err := loadSettings()
	if err != nil {
		t.Fatalf("loadSettings returned error: %v", err)
	}
	if cfg.queue != messaging.InventoryReserveQueue {
		t.Fatalf("queue = %q, want %q", cfg.queue, messaging.InventoryReserveQueue)
	}
	if cfg.consumerName != messaging.InventoryReserveQueue {
		t.Fatalf("consumer name = %q, want %q", cfg.consumerName, messaging.InventoryReserveQueue)
	}
}

func TestLoadSettingsUsesExplicitQueueAndConsumerName(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://example")
	t.Setenv("RABBITMQ_URL", "amqp://example")
	t.Setenv("WORKER_QUEUE", messaging.OrdersFinalizeQueue)
	t.Setenv("CONSUMER_NAME", "orders-finalizer")

	cfg, err := loadSettings()
	if err != nil {
		t.Fatalf("loadSettings returned error: %v", err)
	}
	if cfg.queue != messaging.OrdersFinalizeQueue {
		t.Fatalf("queue = %q, want %q", cfg.queue, messaging.OrdersFinalizeQueue)
	}
	if cfg.consumerName != "orders-finalizer" {
		t.Fatalf("consumer name = %q, want orders-finalizer", cfg.consumerName)
	}
}

func TestLoadSettingsRequiresExternalDependencies(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	t.Setenv("RABBITMQ_URL", "")
	t.Setenv("WORKER_QUEUE", "")
	t.Setenv("CONSUMER_NAME", "")

	_, err := loadSettings()
	if err == nil {
		t.Fatal("loadSettings must require DATABASE_URL and RABBITMQ_URL")
	}
}
