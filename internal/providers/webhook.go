package providers

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	WebhookIDHeader        = "FulfillHub-Webhook-Id"
	WebhookTimestampHeader = "FulfillHub-Webhook-Timestamp"
	WebhookSignatureHeader = "FulfillHub-Webhook-Signature"

	defaultWebhookTolerance = 5 * time.Minute
	defaultWebhookReplayTTL = 24 * time.Hour
)

var (
	ErrWebhookProviderMissing        = errors.New("webhook provider is required")
	ErrWebhookSecretMissing          = errors.New("webhook signing secret is required")
	ErrWebhookEventIDMissing         = errors.New("webhook event id is required")
	ErrWebhookTimestampMissing       = errors.New("webhook timestamp is required")
	ErrWebhookSignatureMissing       = errors.New("webhook signature is required")
	ErrWebhookTimestampInvalid       = errors.New("webhook timestamp is invalid")
	ErrWebhookTimestampOutOfWindow   = errors.New("webhook timestamp is outside tolerance")
	ErrWebhookSignatureInvalid       = errors.New("webhook signature is invalid")
	ErrWebhookReplay                 = errors.New("webhook event was already accepted")
	ErrWebhookReplayStoreUnavailable = errors.New("webhook replay store is unavailable")
)

type WebhookRequest struct {
	EventID   string
	Timestamp string
	Signature string
	Payload   []byte
}

type VerifiedWebhook struct {
	Provider        string
	EventID         string
	Timestamp       time.Time
	ReceivedAt      time.Time
	ReplayExpiresAt time.Time
}

type WebhookReplayStore interface {
	RecordWebhook(ctx context.Context, provider, eventID string, receivedAt, expiresAt time.Time) (bool, error)
}

type WebhookVerifier struct {
	Provider        string
	CurrentSecret   string
	PreviousSecrets []string
	ReplayStore     WebhookReplayStore
	Clock           func() time.Time
	Tolerance       time.Duration
	ReplayTTL       time.Duration
}

func (v WebhookVerifier) Verify(ctx context.Context, request WebhookRequest) (VerifiedWebhook, error) {
	provider := strings.TrimSpace(v.Provider)
	if provider == "" {
		return VerifiedWebhook{}, ErrWebhookProviderMissing
	}
	secrets := v.secrets()
	if len(secrets) == 0 {
		return VerifiedWebhook{}, ErrWebhookSecretMissing
	}
	eventID := strings.TrimSpace(request.EventID)
	if eventID == "" {
		return VerifiedWebhook{}, ErrWebhookEventIDMissing
	}
	if strings.TrimSpace(request.Timestamp) == "" {
		return VerifiedWebhook{}, ErrWebhookTimestampMissing
	}
	if strings.TrimSpace(request.Signature) == "" {
		return VerifiedWebhook{}, ErrWebhookSignatureMissing
	}

	timestampUnix, err := strconv.ParseInt(strings.TrimSpace(request.Timestamp), 10, 64)
	if err != nil {
		return VerifiedWebhook{}, ErrWebhookTimestampInvalid
	}
	timestamp := time.Unix(timestampUnix, 0).UTC()
	now := time.Now().UTC()
	if v.Clock != nil {
		now = v.Clock().UTC()
	}
	tolerance := v.Tolerance
	if tolerance == 0 {
		tolerance = defaultWebhookTolerance
	}
	if timestamp.Before(now.Add(-tolerance)) || timestamp.After(now.Add(tolerance)) {
		return VerifiedWebhook{}, ErrWebhookTimestampOutOfWindow
	}

	signatures := parseWebhookSignatures(request.Signature)
	if len(signatures) == 0 {
		return VerifiedWebhook{}, ErrWebhookSignatureInvalid
	}
	if !v.matchesSignature(secrets, eventID, timestampUnix, request.Payload, signatures) {
		return VerifiedWebhook{}, ErrWebhookSignatureInvalid
	}

	replayTTL := v.ReplayTTL
	if replayTTL == 0 {
		replayTTL = defaultWebhookReplayTTL
	}
	expiresAt := now.Add(replayTTL)
	if v.ReplayStore != nil {
		accepted, err := v.ReplayStore.RecordWebhook(ctx, provider, eventID, now, expiresAt)
		if err != nil {
			return VerifiedWebhook{}, fmt.Errorf("%w: %v", ErrWebhookReplayStoreUnavailable, err)
		}
		if !accepted {
			return VerifiedWebhook{}, ErrWebhookReplay
		}
	}

	return VerifiedWebhook{
		Provider:        provider,
		EventID:         eventID,
		Timestamp:       timestamp,
		ReceivedAt:      now,
		ReplayExpiresAt: expiresAt,
	}, nil
}

func (v WebhookVerifier) secrets() [][]byte {
	values := append([]string{v.CurrentSecret}, v.PreviousSecrets...)
	secrets := make([][]byte, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		secrets = append(secrets, []byte(value))
	}
	return secrets
}

func (v WebhookVerifier) matchesSignature(secrets [][]byte, eventID string, timestamp int64, payload []byte, signatures [][]byte) bool {
	for _, secret := range secrets {
		expected := webhookDigest(secret, eventID, timestamp, payload)
		for _, got := range signatures {
			if hmac.Equal(got, expected) {
				return true
			}
		}
	}
	return false
}

func SignWebhook(secret, eventID string, timestamp time.Time, payload []byte) string {
	return "v1=" + hex.EncodeToString(webhookDigest([]byte(secret), eventID, timestamp.Unix(), payload))
}

func webhookDigest(secret []byte, eventID string, timestamp int64, payload []byte) []byte {
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(strconv.FormatInt(timestamp, 10)))
	_, _ = mac.Write([]byte("."))
	_, _ = mac.Write([]byte(eventID))
	_, _ = mac.Write([]byte("."))
	_, _ = mac.Write(payload)
	return mac.Sum(nil)
}

func parseWebhookSignatures(header string) [][]byte {
	parts := strings.Split(header, ",")
	signatures := make([][]byte, 0, len(parts))
	for _, part := range parts {
		key, value, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok || key != "v1" {
			continue
		}
		decoded, err := hex.DecodeString(strings.TrimSpace(value))
		if err != nil || len(decoded) != sha256.Size {
			continue
		}
		signatures = append(signatures, decoded)
	}
	return signatures
}

type MemoryWebhookReplayStore struct {
	mu   sync.Mutex
	seen map[string]time.Time
}

func NewMemoryWebhookReplayStore() *MemoryWebhookReplayStore {
	return &MemoryWebhookReplayStore{seen: make(map[string]time.Time)}
}

func (s *MemoryWebhookReplayStore) RecordWebhook(_ context.Context, provider, eventID string, receivedAt, expiresAt time.Time) (bool, error) {
	if s == nil {
		return false, errors.New("nil memory webhook replay store")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.seen == nil {
		s.seen = make(map[string]time.Time)
	}
	for key, expiresAt := range s.seen {
		if !expiresAt.After(receivedAt) {
			delete(s.seen, key)
		}
	}
	key := provider + "\x00" + eventID
	if existing, ok := s.seen[key]; ok && existing.After(receivedAt) {
		return false, nil
	}
	s.seen[key] = expiresAt
	return true, nil
}
