package main

import "testing"

func TestRateLimitPerMinuteUsesDefault(t *testing.T) {
	limit, err := rateLimitPerMinute(func(string) string { return "" })
	if err != nil {
		t.Fatalf("rateLimitPerMinute returned error: %v", err)
	}
	if limit != 120 {
		t.Fatalf("limit = %d, want 120", limit)
	}
}

func TestRateLimitPerMinuteUsesOverride(t *testing.T) {
	limit, err := rateLimitPerMinute(func(key string) string {
		if key == "RATE_LIMIT_PER_MINUTE" {
			return "10000"
		}
		return ""
	})
	if err != nil {
		t.Fatalf("rateLimitPerMinute returned error: %v", err)
	}
	if limit != 10000 {
		t.Fatalf("limit = %d, want 10000", limit)
	}
}

func TestRateLimitPerMinuteRejectsInvalidOverride(t *testing.T) {
	for _, value := range []string{"0", "-1", "not-a-number"} {
		t.Run(value, func(t *testing.T) {
			_, err := rateLimitPerMinute(func(key string) string {
				if key == "RATE_LIMIT_PER_MINUTE" {
					return value
				}
				return ""
			})
			if err == nil {
				t.Fatal("expected error")
			}
		})
	}
}
