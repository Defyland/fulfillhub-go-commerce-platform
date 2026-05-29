package main

import (
	"strings"
	"testing"
	"time"
)

func TestLoadSettingsRequiresDatabaseURL(t *testing.T) {
	_, err := loadSettings(func(string) string { return "" })
	if err == nil || !strings.Contains(err.Error(), "DATABASE_URL") {
		t.Fatalf("loadSettings error = %v, want DATABASE_URL requirement", err)
	}
}

func TestLoadSettingsUsesDefaultTimeout(t *testing.T) {
	cfg, err := loadSettings(func(key string) string {
		if key == "DATABASE_URL" {
			return "postgres://fulfillhub:postgres@localhost:5432/fulfillhub"
		}
		return ""
	})
	if err != nil {
		t.Fatalf("loadSettings returned error: %v", err)
	}
	if cfg.timeout != 30*time.Second {
		t.Fatalf("timeout = %s, want 30s", cfg.timeout)
	}
}

func TestLoadSettingsRejectsInvalidTimeout(t *testing.T) {
	_, err := loadSettings(func(key string) string {
		switch key {
		case "DATABASE_URL":
			return "postgres://fulfillhub:postgres@localhost:5432/fulfillhub"
		case "MIGRATION_TIMEOUT":
			return "0s"
		default:
			return ""
		}
	})
	if err == nil || !strings.Contains(err.Error(), "MIGRATION_TIMEOUT") {
		t.Fatalf("loadSettings error = %v, want MIGRATION_TIMEOUT requirement", err)
	}
}
