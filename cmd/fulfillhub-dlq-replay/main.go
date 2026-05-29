package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Defyland/fulfillhub-go-commerce-platform/internal/commerce"
	"github.com/Defyland/fulfillhub-go-commerce-platform/internal/messaging"
	"github.com/Defyland/fulfillhub-go-commerce-platform/internal/postgres"
	amqp "github.com/rabbitmq/amqp091-go"
)

type replaySettings struct {
	RabbitURL       string
	DatabaseURL     string
	Queue           string
	Exchange        string
	RoutingKey      string
	Limit           int
	ActorID         string
	AuditMerchantID string
	CorrelationID   string
}

func main() {
	settings, err := loadReplaySettings(os.Getenv, time.Now)
	if err != nil {
		log.Fatal(err)
	}

	setupCtx, cancelSetup := context.WithTimeout(context.Background(), 30*time.Second)
	store, err := postgres.Open(setupCtx, settings.DatabaseURL)
	if err != nil {
		cancelSetup()
		log.Fatal(err)
	}
	defer store.Close()
	if err := postgres.RunMigrations(setupCtx, store.DB()); err != nil {
		cancelSetup()
		log.Fatal(err)
	}
	cancelSetup()

	conn, err := amqp.Dial(settings.RabbitURL)
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()
	channel, err := conn.Channel()
	if err != nil {
		log.Fatal(err)
	}
	defer channel.Close()
	if err := messaging.DeclareTopology(channel); err != nil {
		log.Fatal(err)
	}

	replayCtx, cancelReplay := context.WithTimeout(context.Background(), 30*time.Second)
	replayed, replayErr := messaging.ReplayDLQ(replayCtx, channel, messaging.DLQReplayConfig{
		Queue:      settings.Queue,
		Exchange:   settings.Exchange,
		RoutingKey: settings.RoutingKey,
		Limit:      settings.Limit,
	})
	cancelReplay()

	auditCtx, cancelAudit := context.WithTimeout(context.Background(), 10*time.Second)
	audit := buildReplayAudit(settings, replayed, replayErr, time.Now().UTC())
	if err := store.RecordAuditLog(auditCtx, audit); err != nil {
		cancelAudit()
		log.Fatalf("record dlq replay audit: %v", err)
	}
	cancelAudit()

	if replayErr != nil {
		log.Fatal(replayErr)
	}
	log.Printf("replayed %d messages from %s", replayed, settings.Queue)
}

func loadReplaySettings(getenv func(string) string, now func() time.Time) (replaySettings, error) {
	settings := replaySettings{
		RabbitURL:       strings.TrimSpace(getenv("RABBITMQ_URL")),
		DatabaseURL:     strings.TrimSpace(getenv("DATABASE_URL")),
		Queue:           strings.TrimSpace(getenv("DLQ_QUEUE")),
		Exchange:        messaging.DomainExchange,
		RoutingKey:      strings.TrimSpace(getenv("TARGET_ROUTING_KEY")),
		Limit:           100,
		ActorID:         strings.TrimSpace(getenv("OPS_ACTOR_ID")),
		AuditMerchantID: strings.TrimSpace(getenv("AUDIT_MERCHANT_ID")),
		CorrelationID:   strings.TrimSpace(getenv("REPLAY_CORRELATION_ID")),
	}
	if settings.AuditMerchantID == "" {
		settings.AuditMerchantID = "platform"
	}
	if settings.CorrelationID == "" {
		settings.CorrelationID = fmt.Sprintf("cor_dlq_replay_%d", now().UTC().UnixNano())
	}
	if raw := strings.TrimSpace(getenv("REPLAY_LIMIT")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			return replaySettings{}, fmt.Errorf("REPLAY_LIMIT must be a positive integer")
		}
		settings.Limit = parsed
	}
	switch {
	case settings.RabbitURL == "":
		return replaySettings{}, fmt.Errorf("RABBITMQ_URL is required")
	case settings.DatabaseURL == "":
		return replaySettings{}, fmt.Errorf("DATABASE_URL is required")
	case settings.Queue == "":
		return replaySettings{}, fmt.Errorf("DLQ_QUEUE is required")
	case settings.RoutingKey == "":
		return replaySettings{}, fmt.Errorf("TARGET_ROUTING_KEY is required")
	case settings.ActorID == "":
		return replaySettings{}, fmt.Errorf("OPS_ACTOR_ID is required")
	}
	return settings, nil
}

func buildReplayAudit(settings replaySettings, replayed int, replayErr error, at time.Time) commerce.AuditLog {
	status := "succeeded"
	details := map[string]string{
		"queue":              settings.Queue,
		"exchange":           settings.Exchange,
		"target_routing_key": settings.RoutingKey,
		"replay_limit":       strconv.Itoa(settings.Limit),
		"replayed_count":     strconv.Itoa(replayed),
		"status":             status,
	}
	if replayErr != nil {
		status = "failed"
		details["status"] = status
		details["error"] = replayErr.Error()
	}
	return commerce.AuditLog{
		MerchantID:    settings.AuditMerchantID,
		ActorType:     "ops",
		ActorID:       settings.ActorID,
		Action:        "dlq.replay",
		CorrelationID: settings.CorrelationID,
		CreatedAt:     at,
		Details:       details,
	}
}
