package main

import (
	"testing"
	"time"
)

func TestRelayBatchSizeUsesDefault(t *testing.T) {
	batchSize, err := relayBatchSize(func(string) string { return "" })
	if err != nil {
		t.Fatalf("relayBatchSize returned error: %v", err)
	}
	if batchSize != 100 {
		t.Fatalf("batchSize = %d, want 100", batchSize)
	}
}

func TestRelayBatchSizeUsesOverride(t *testing.T) {
	batchSize, err := relayBatchSize(func(key string) string {
		if key == "OUTBOX_RELAY_BATCH_SIZE" {
			return "1000"
		}
		return ""
	})
	if err != nil {
		t.Fatalf("relayBatchSize returned error: %v", err)
	}
	if batchSize != 1000 {
		t.Fatalf("batchSize = %d, want 1000", batchSize)
	}
}

func TestRelayBatchSizeRejectsInvalidOverride(t *testing.T) {
	for _, value := range []string{"0", "-1", "not-a-number"} {
		t.Run(value, func(t *testing.T) {
			_, err := relayBatchSize(func(key string) string {
				if key == "OUTBOX_RELAY_BATCH_SIZE" {
					return value
				}
				return ""
			})
			if err == nil {
				t.Fatal("relayBatchSize must reject invalid values")
			}
		})
	}
}

func TestRelayIntervalUsesDefault(t *testing.T) {
	interval, err := relayInterval(func(string) string { return "" })
	if err != nil {
		t.Fatalf("relayInterval returned error: %v", err)
	}
	if interval != 250*time.Millisecond {
		t.Fatalf("interval = %s, want 250ms", interval)
	}
}

func TestRelayIntervalUsesOverride(t *testing.T) {
	interval, err := relayInterval(func(key string) string {
		if key == "OUTBOX_RELAY_INTERVAL" {
			return "100ms"
		}
		return ""
	})
	if err != nil {
		t.Fatalf("relayInterval returned error: %v", err)
	}
	if interval != 100*time.Millisecond {
		t.Fatalf("interval = %s, want 100ms", interval)
	}
}

func TestRelayIntervalRejectsInvalidOverride(t *testing.T) {
	for _, value := range []string{"0", "-1s", "not-a-duration"} {
		t.Run(value, func(t *testing.T) {
			_, err := relayInterval(func(key string) string {
				if key == "OUTBOX_RELAY_INTERVAL" {
					return value
				}
				return ""
			})
			if err == nil {
				t.Fatal("relayInterval must reject invalid values")
			}
		})
	}
}
