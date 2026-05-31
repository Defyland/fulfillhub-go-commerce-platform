package main

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

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

func TestMerchantAPIKeysParsesConfiguredPairs(t *testing.T) {
	keys, err := merchantAPIKeys(func(key string) string {
		if key == "MERCHANT_API_KEYS" {
			return "fh_live_a=mer_a,fh_live_b=mer_b"
		}
		return ""
	})
	if err != nil {
		t.Fatalf("merchantAPIKeys returned error: %v", err)
	}
	if keys["fh_live_a"] != "mer_a" || keys["fh_live_b"] != "mer_b" {
		t.Fatalf("api keys = %+v, want configured merchant mappings", keys)
	}
}

func TestMerchantAPIKeysRejectsMalformedPairs(t *testing.T) {
	_, err := merchantAPIKeys(func(key string) string {
		if key == "MERCHANT_API_KEYS" {
			return "fh_live_a:mer_a"
		}
		return ""
	})
	if err == nil {
		t.Fatal("merchantAPIKeys must reject malformed pairs")
	}
}

func TestDurableStoreConfigRequiresDatabaseURLByDefault(t *testing.T) {
	_, _, err := durableStoreConfig(func(string) string { return "" })
	if err == nil {
		t.Fatal("durableStoreConfig must reject missing DATABASE_URL unless in-memory is explicit")
	}
}

func TestDurableStoreConfigAllowsExplicitInMemoryStore(t *testing.T) {
	databaseURL, allowInMemory, err := durableStoreConfig(func(key string) string {
		if key == "ALLOW_IN_MEMORY_STORE" {
			return "true"
		}
		return ""
	})
	if err != nil {
		t.Fatalf("durableStoreConfig returned error: %v", err)
	}
	if databaseURL != "" || !allowInMemory {
		t.Fatalf("config = databaseURL:%q allowInMemory:%v, want empty database URL and explicit memory", databaseURL, allowInMemory)
	}
}

func TestDurableStoreConfigPrefersDatabaseURL(t *testing.T) {
	databaseURL, allowInMemory, err := durableStoreConfig(func(key string) string {
		switch key {
		case "DATABASE_URL":
			return " postgres://example "
		case "ALLOW_IN_MEMORY_STORE":
			return "true"
		default:
			return ""
		}
	})
	if err != nil {
		t.Fatalf("durableStoreConfig returned error: %v", err)
	}
	if databaseURL != "postgres://example" || !allowInMemory {
		t.Fatalf("config = databaseURL:%q allowInMemory:%v, want trimmed database URL and explicit memory flag preserved", databaseURL, allowInMemory)
	}
}

func TestBoolEnvRejectsInvalidBooleans(t *testing.T) {
	_, err := boolEnv(func(key string) string {
		if key == "ALLOW_LOCAL_OPS_TOKEN" {
			return "maybe"
		}
		return ""
	}, "ALLOW_LOCAL_OPS_TOKEN")
	if err == nil {
		t.Fatal("boolEnv must reject invalid booleans")
	}
}

func TestHTTPServerUsesProductionTimeouts(t *testing.T) {
	server := newHTTPServer(":0", http.NewServeMux())

	if server.ReadHeaderTimeout < 5*time.Second {
		t.Fatalf("ReadHeaderTimeout = %s, want at least 5s", server.ReadHeaderTimeout)
	}
	if server.ReadTimeout == 0 || server.WriteTimeout == 0 || server.IdleTimeout == 0 {
		t.Fatalf("server timeouts must be configured: %+v", server)
	}
}

func TestPprofServerDisabledByDefault(t *testing.T) {
	server, err := startPprofServer(func(string) string { return "" }, discardLogger())
	if err != nil {
		t.Fatalf("startPprofServer returned error: %v", err)
	}
	if server != nil {
		t.Fatalf("pprof server = %+v, want nil when ENABLE_PPROF is unset", server)
	}
}

func TestPprofServerRejectsInvalidBoolean(t *testing.T) {
	_, err := startPprofServer(func(key string) string {
		if key == "ENABLE_PPROF" {
			return "maybe"
		}
		return ""
	}, discardLogger())
	if err == nil {
		t.Fatal("startPprofServer must reject invalid ENABLE_PPROF")
	}
}

func TestPprofServerUsesProductionTimeoutsAndHandlers(t *testing.T) {
	server := newPprofServer("127.0.0.1:0")

	if server.ReadHeaderTimeout < 5*time.Second {
		t.Fatalf("ReadHeaderTimeout = %s, want at least 5s", server.ReadHeaderTimeout)
	}
	if server.ReadTimeout == 0 || server.WriteTimeout == 0 || server.IdleTimeout == 0 {
		t.Fatalf("pprof server timeouts must be configured: %+v", server)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/debug/pprof/", nil)

	server.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("pprof index status = %d, want 200", rec.Code)
	}
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
