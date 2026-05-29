package providers

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestWebhookVerifierAcceptsSignedWebhookAndRecordsReplayWindow(t *testing.T) {
	now := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	payload := []byte(`{"type":"payment.authorized","id":"evt_1"}`)
	store := NewMemoryWebhookReplayStore()
	verifier := WebhookVerifier{
		Provider:      "stripe",
		CurrentSecret: "whsec_current",
		ReplayStore:   store,
		Clock:         func() time.Time { return now },
		ReplayTTL:     2 * time.Hour,
	}

	verified, err := verifier.Verify(context.Background(), WebhookRequest{
		EventID:   "evt_1",
		Timestamp: "1780056000",
		Signature: SignWebhook("whsec_current", "evt_1", now, payload),
		Payload:   payload,
	})
	if err != nil {
		t.Fatalf("Verify returned error: %v", err)
	}
	if verified.Provider != "stripe" || verified.EventID != "evt_1" {
		t.Fatalf("verified webhook = %+v", verified)
	}
	if !verified.ReplayExpiresAt.Equal(now.Add(2 * time.Hour)) {
		t.Fatalf("ReplayExpiresAt = %s, want %s", verified.ReplayExpiresAt, now.Add(2*time.Hour))
	}
}

func TestWebhookVerifierAcceptsPreviousSecretDuringRotation(t *testing.T) {
	now := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	payload := []byte(`{"type":"shipment.created","id":"evt_rotated"}`)
	verifier := WebhookVerifier{
		Provider:        "shippo",
		CurrentSecret:   "whsec_current",
		PreviousSecrets: []string{"whsec_previous"},
		Clock:           func() time.Time { return now },
	}

	if _, err := verifier.Verify(context.Background(), WebhookRequest{
		EventID:   "evt_rotated",
		Timestamp: "1780056000",
		Signature: SignWebhook("whsec_previous", "evt_rotated", now, payload),
		Payload:   payload,
	}); err != nil {
		t.Fatalf("Verify with previous secret returned error: %v", err)
	}
}

func TestWebhookVerifierRejectsTamperedPayload(t *testing.T) {
	now := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	verifier := WebhookVerifier{
		Provider:      "stripe",
		CurrentSecret: "whsec_current",
		Clock:         func() time.Time { return now },
	}

	_, err := verifier.Verify(context.Background(), WebhookRequest{
		EventID:   "evt_1",
		Timestamp: "1780056000",
		Signature: SignWebhook("whsec_current", "evt_1", now, []byte(`{"amount":100}`)),
		Payload:   []byte(`{"amount":101}`),
	})
	if !errors.Is(err, ErrWebhookSignatureInvalid) {
		t.Fatalf("Verify error = %v, want ErrWebhookSignatureInvalid", err)
	}
}

func TestWebhookVerifierRejectsStaleTimestamp(t *testing.T) {
	now := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	stale := now.Add(-10 * time.Minute)
	payload := []byte(`{"type":"payment.authorized","id":"evt_stale"}`)
	verifier := WebhookVerifier{
		Provider:      "stripe",
		CurrentSecret: "whsec_current",
		Clock:         func() time.Time { return now },
		Tolerance:     5 * time.Minute,
	}

	_, err := verifier.Verify(context.Background(), WebhookRequest{
		EventID:   "evt_stale",
		Timestamp: "1780055400",
		Signature: SignWebhook("whsec_current", "evt_stale", stale, payload),
		Payload:   payload,
	})
	if !errors.Is(err, ErrWebhookTimestampOutOfWindow) {
		t.Fatalf("Verify error = %v, want ErrWebhookTimestampOutOfWindow", err)
	}
}

func TestWebhookVerifierRejectsReplayBeforeSideEffects(t *testing.T) {
	now := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	payload := []byte(`{"type":"payment.authorized","id":"evt_replay"}`)
	store := NewMemoryWebhookReplayStore()
	verifier := WebhookVerifier{
		Provider:      "stripe",
		CurrentSecret: "whsec_current",
		ReplayStore:   store,
		Clock:         func() time.Time { return now },
	}
	request := WebhookRequest{
		EventID:   "evt_replay",
		Timestamp: "1780056000",
		Signature: SignWebhook("whsec_current", "evt_replay", now, payload),
		Payload:   payload,
	}

	if _, err := verifier.Verify(context.Background(), request); err != nil {
		t.Fatalf("first Verify returned error: %v", err)
	}
	if _, err := verifier.Verify(context.Background(), request); !errors.Is(err, ErrWebhookReplay) {
		t.Fatalf("second Verify error = %v, want ErrWebhookReplay", err)
	}
}

func TestWebhookVerifierRequiresConfiguredSecret(t *testing.T) {
	verifier := WebhookVerifier{Provider: "stripe"}

	_, err := verifier.Verify(context.Background(), WebhookRequest{
		EventID:   "evt_1",
		Timestamp: "1780056000",
		Signature: "v1=abc",
	})
	if !errors.Is(err, ErrWebhookSecretMissing) {
		t.Fatalf("Verify error = %v, want ErrWebhookSecretMissing", err)
	}
}
