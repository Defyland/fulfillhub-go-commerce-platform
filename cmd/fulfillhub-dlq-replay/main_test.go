package main

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Defyland/fulfillhub-go-commerce-platform/internal/messaging"
)

func TestLoadReplaySettingsParsesAuditConfiguration(t *testing.T) {
	settings, err := loadReplaySettings(testEnv(map[string]string{
		"RABBITMQ_URL":          "amqp://guest:guest@localhost:5672/",
		"DATABASE_URL":          "postgres://fulfillhub:postgres@localhost:5432/fulfillhub?sslmode=disable",
		"DLQ_QUEUE":             "inventory.reserve.dlq",
		"TARGET_ROUTING_KEY":    "order.created",
		"REPLAY_LIMIT":          "5",
		"OPS_ACTOR_ID":          "usr_ops_1",
		"AUDIT_MERCHANT_ID":     "platform_ops",
		"REPLAY_CORRELATION_ID": "cor_replay_1",
	}), time.Now)
	if err != nil {
		t.Fatalf("loadReplaySettings returned error: %v", err)
	}
	if settings.RabbitURL == "" || settings.DatabaseURL == "" {
		t.Fatal("settings must include RabbitMQ and PostgreSQL URLs")
	}
	if settings.Exchange != messaging.DomainExchange {
		t.Fatalf("exchange = %q, want %q", settings.Exchange, messaging.DomainExchange)
	}
	if settings.Limit != 5 {
		t.Fatalf("limit = %d, want 5", settings.Limit)
	}
	if settings.ActorID != "usr_ops_1" || settings.AuditMerchantID != "platform_ops" || settings.CorrelationID != "cor_replay_1" {
		t.Fatalf("audit settings not parsed: %+v", settings)
	}
}

func TestLoadReplaySettingsRequiresDatabaseAndActor(t *testing.T) {
	_, err := loadReplaySettings(testEnv(map[string]string{
		"RABBITMQ_URL":       "amqp://guest:guest@localhost:5672/",
		"DLQ_QUEUE":          "inventory.reserve.dlq",
		"TARGET_ROUTING_KEY": "order.created",
	}), time.Now)
	if err == nil {
		t.Fatal("loadReplaySettings must require DATABASE_URL before unaudited replay")
	}
	if !strings.Contains(err.Error(), "DATABASE_URL") {
		t.Fatalf("error = %v, want DATABASE_URL requirement", err)
	}

	_, err = loadReplaySettings(testEnv(map[string]string{
		"RABBITMQ_URL":       "amqp://guest:guest@localhost:5672/",
		"DATABASE_URL":       "postgres://fulfillhub:postgres@localhost:5432/fulfillhub?sslmode=disable",
		"DLQ_QUEUE":          "inventory.reserve.dlq",
		"TARGET_ROUTING_KEY": "order.created",
	}), time.Now)
	if err == nil {
		t.Fatal("loadReplaySettings must require OPS_ACTOR_ID")
	}
	if !strings.Contains(err.Error(), "OPS_ACTOR_ID") {
		t.Fatalf("error = %v, want OPS_ACTOR_ID requirement", err)
	}
}

func TestLoadReplaySettingsGeneratesCorrelationID(t *testing.T) {
	now := time.Date(2026, 5, 29, 12, 0, 0, 123, time.UTC)
	settings, err := loadReplaySettings(testEnv(map[string]string{
		"RABBITMQ_URL":       "amqp://guest:guest@localhost:5672/",
		"DATABASE_URL":       "postgres://fulfillhub:postgres@localhost:5432/fulfillhub?sslmode=disable",
		"DLQ_QUEUE":          "inventory.reserve.dlq",
		"TARGET_ROUTING_KEY": "order.created",
		"OPS_ACTOR_ID":       "usr_ops_1",
	}), func() time.Time { return now })
	if err != nil {
		t.Fatalf("loadReplaySettings returned error: %v", err)
	}
	if settings.CorrelationID != "cor_dlq_replay_1780056000000000123" {
		t.Fatalf("correlation id = %q, want generated replay correlation", settings.CorrelationID)
	}
	if settings.AuditMerchantID != "platform" {
		t.Fatalf("audit merchant = %q, want platform", settings.AuditMerchantID)
	}
}

func TestBuildReplayAuditIncludesFailureDetails(t *testing.T) {
	settings := replaySettings{
		Queue:           "inventory.reserve.dlq",
		Exchange:        messaging.DomainExchange,
		RoutingKey:      "order.created",
		Limit:           10,
		ActorID:         "usr_ops_1",
		AuditMerchantID: "platform",
		CorrelationID:   "cor_replay_1",
	}
	replayErr := errors.New("republish dlq message: broker unavailable")

	audit := buildReplayAudit(settings, 3, replayErr, time.Unix(1, 0).UTC())

	if audit.Action != "dlq.replay" || audit.ActorType != "ops" || audit.ActorID != "usr_ops_1" {
		t.Fatalf("unexpected audit identity: %+v", audit)
	}
	if audit.Details["status"] != "failed" || audit.Details["replayed_count"] != "3" {
		t.Fatalf("unexpected audit details: %+v", audit.Details)
	}
	if !strings.Contains(audit.Details["error"], "broker unavailable") {
		t.Fatalf("audit error detail = %q, want broker error", audit.Details["error"])
	}
}

func testEnv(values map[string]string) func(string) string {
	return func(key string) string {
		return values[key]
	}
}
