package api

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Defyland/fulfillhub-go-commerce-platform/internal/commerce"
	"github.com/Defyland/fulfillhub-go-commerce-platform/internal/messaging"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestHealthAndReadiness(t *testing.T) {
	server := testServer()

	for _, path := range []string{"/healthz", "/readyz"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)

		server.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("%s status = %d, want 200", path, rec.Code)
		}
	}
}

func TestReadinessReportsConfiguredDependencies(t *testing.T) {
	server := newTestServerWithOptions(commerce.NewService(commerce.NewMemoryStore()), Options{
		ReadinessChecks: map[string]ReadinessChecker{
			"broker": ReadinessCheckFunc(func(context.Context) error { return nil }),
			"cache":  ReadinessCheckFunc(func(context.Context) error { return nil }),
		},
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)

	server.ServeHTTP(rec, req)

	assertStatus(t, rec, http.StatusOK)
	var body struct {
		Status string            `json:"status"`
		Checks map[string]string `json:"checks"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode readiness response: %v", err)
	}
	if body.Status != "ready" {
		t.Fatalf("readiness status = %q, want ready", body.Status)
	}
	for _, name := range []string{"store", "broker", "cache"} {
		if body.Checks[name] != "up" {
			t.Fatalf("readiness check %s = %q, want up in %+v", name, body.Checks[name], body.Checks)
		}
	}
}

func TestReadinessFailsWhenDependencyIsUnavailable(t *testing.T) {
	server := newTestServerWithOptions(commerce.NewService(commerce.NewMemoryStore()), Options{
		ReadinessChecks: map[string]ReadinessChecker{
			"broker": ReadinessCheckFunc(func(context.Context) error {
				return errors.New("connection timeout")
			}),
		},
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)

	server.ServeHTTP(rec, req)

	assertStatus(t, rec, http.StatusServiceUnavailable)
	body := rec.Body.String()
	for _, want := range []string{"dependency_unavailable", "One or more readiness checks failed.", "broker", "connection timeout"} {
		if !strings.Contains(body, want) {
			t.Fatalf("readiness failure body missing %q: %s", want, body)
		}
	}
}

func TestServerWritesStructuredRequestLog(t *testing.T) {
	var logBuffer bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logBuffer, nil))
	server := newTestServerWithOptions(commerce.NewService(commerce.NewMemoryStore()), Options{
		Logger: logger,
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/orders", bytes.NewReader(validOrderJSON(t)))
	req.Header.Set("X-API-Key", "fh_live_merchant_demo")
	req.Header.Set("Idempotency-Key", "idem-key-0001")
	req.Header.Set("X-Request-Id", "req_observability")
	req.Header.Set("X-Correlation-Id", "cor_observability")

	server.ServeHTTP(rec, req)

	assertStatus(t, rec, http.StatusAccepted)
	var entry map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(logBuffer.Bytes()), &entry); err != nil {
		t.Fatalf("decode structured log: %v\n%s", err, logBuffer.String())
	}
	for key, want := range map[string]any{
		"msg":            "http_request",
		"method":         http.MethodPost,
		"path":           "/api/v1/orders",
		"request_id":     "req_observability",
		"correlation_id": "cor_observability",
		"actor_type":     "merchant",
		"merchant_id":    "mer_01hzy6v4egscg4r7kb3m7jq2dk",
	} {
		if got := entry[key]; got != want {
			t.Fatalf("log %s = %v, want %v", key, got, want)
		}
	}
	if got := entry["status"]; got != float64(http.StatusAccepted) {
		t.Fatalf("log status = %v, want %d", got, http.StatusAccepted)
	}
	if _, ok := entry["duration_ms"].(float64); !ok {
		t.Fatalf("log duration_ms missing or not numeric: %v", entry["duration_ms"])
	}
}

func TestServerPropagatesTraceContext(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() {
		if err := provider.Shutdown(context.Background()); err != nil {
			t.Fatalf("shutdown tracer provider: %v", err)
		}
	})
	server := newTestServerWithOptions(commerce.NewService(commerce.NewMemoryStore()), Options{
		TracerProvider: provider,
		Propagator:     propagation.TraceContext{},
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.Header.Set("traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	req.Header.Set("X-Request-Id", "req_trace")
	req.Header.Set("X-Correlation-Id", "cor_trace")

	server.ServeHTTP(rec, req)

	assertStatus(t, rec, http.StatusOK)
	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("ended spans = %d, want 1", len(spans))
	}
	span := spans[0]
	if got := span.Name(); got != "GET /healthz" {
		t.Fatalf("span name = %q, want GET /healthz", got)
	}
	if got := span.SpanContext().TraceID().String(); got != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Fatalf("trace id = %q, want propagated trace id", got)
	}
	if got := span.Parent().SpanID().String(); got != "00f067aa0ba902b7" {
		t.Fatalf("parent span id = %q, want propagated parent span id", got)
	}
	assertSpanAttribute(t, span, "fulfillhub.request_id", "req_trace")
	assertSpanAttribute(t, span, "fulfillhub.correlation_id", "cor_trace")
}

func TestMetricsIncludesRabbitMQQueueGauges(t *testing.T) {
	server := newTestServerWithOptions(commerce.NewService(commerce.NewMemoryStore()), Options{
		QueueMetrics: fakeQueueMetrics{
			depths: []messaging.QueueDepth{
				{Queue: messaging.InventoryReserveQueue, MessagesReady: 7, Consumers: 1},
			},
		},
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)

	server.ServeHTTP(rec, req)

	assertStatus(t, rec, http.StatusOK)
	body := rec.Body.String()
	for _, want := range []string{
		"fulfillhub_rabbitmq_queue_metrics_up 1",
		`fulfillhub_rabbitmq_queue_messages_ready{queue="inventory.reserve"} 7`,
		`fulfillhub_rabbitmq_queue_consumers{queue="inventory.reserve"} 1`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("metrics body missing %q:\n%s", want, body)
		}
	}
}

func TestMetricsIncludesOutboxBacklogGauge(t *testing.T) {
	server := newTestServerWithOptions(commerce.NewService(commerce.NewMemoryStore()), Options{
		OutboxBacklog: fakeOutboxBacklog{count: 3, oldestAgeSeconds: 12.5},
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)

	server.ServeHTTP(rec, req)

	assertStatus(t, rec, http.StatusOK)
	body := rec.Body.String()
	for _, want := range []string{
		"fulfillhub_outbox_metrics_up 1",
		"fulfillhub_outbox_unpublished_total 3",
		"fulfillhub_outbox_oldest_unpublished_age_seconds 12.500",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("metrics body missing %q:\n%s", want, body)
		}
	}
}

func TestMetricsIncludesOrderStatusGauges(t *testing.T) {
	server := newTestServerWithOptions(commerce.NewService(commerce.NewMemoryStore()), Options{
		SagaMetrics: fakeSagaMetrics{counts: map[commerce.OrderStatus]int{
			commerce.StatusPendingFulfillment: 2,
			commerce.StatusCompleted:          5,
		}},
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)

	server.ServeHTTP(rec, req)

	assertStatus(t, rec, http.StatusOK)
	body := rec.Body.String()
	for _, want := range []string{
		"fulfillhub_order_status_metrics_up 1",
		`fulfillhub_orders_total{status="pending_fulfillment"} 2`,
		`fulfillhub_orders_total{status="completed"} 5`,
		`fulfillhub_orders_total{status="failed"} 0`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("metrics body missing %q:\n%s", want, body)
		}
	}
}

func TestMetricsReportsOutboxMetricsDown(t *testing.T) {
	server := newTestServerWithOptions(commerce.NewService(commerce.NewMemoryStore()), Options{
		OutboxBacklog: fakeOutboxBacklog{err: errors.New("database unavailable")},
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)

	server.ServeHTTP(rec, req)

	assertStatus(t, rec, http.StatusOK)
	body := rec.Body.String()
	if !strings.Contains(body, "fulfillhub_outbox_metrics_up 0") {
		t.Fatalf("metrics body does not report outbox metrics down:\n%s", body)
	}
	if strings.Contains(body, "fulfillhub_outbox_unpublished_total") {
		t.Fatalf("metrics body must not expose stale outbox backlog after error:\n%s", body)
	}
}

func TestMetricsReportsRabbitMQQueueMetricsDown(t *testing.T) {
	server := newTestServerWithOptions(commerce.NewService(commerce.NewMemoryStore()), Options{
		QueueMetrics: fakeQueueMetrics{err: errors.New("rabbitmq unavailable")},
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)

	server.ServeHTTP(rec, req)

	assertStatus(t, rec, http.StatusOK)
	if body := rec.Body.String(); !strings.Contains(body, "fulfillhub_rabbitmq_queue_metrics_up 0") {
		t.Fatalf("metrics body does not report queue metrics down:\n%s", body)
	}
}

func TestMetricsRequiresBearerTokenWhenConfigured(t *testing.T) {
	server := newTestServerWithOptions(commerce.NewService(commerce.NewMemoryStore()), Options{
		MetricsBearerToken: "metrics-secret",
	})

	for _, tc := range []struct {
		name          string
		authorization string
		wantStatus    int
	}{
		{name: "missing", wantStatus: http.StatusUnauthorized},
		{name: "invalid", authorization: "Bearer wrong-secret", wantStatus: http.StatusUnauthorized},
		{name: "valid", authorization: "Bearer metrics-secret", wantStatus: http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
			if tc.authorization != "" {
				req.Header.Set("Authorization", tc.authorization)
			}

			server.ServeHTTP(rec, req)

			assertStatus(t, rec, tc.wantStatus)
			if tc.wantStatus == http.StatusOK && !strings.Contains(rec.Body.String(), "fulfillhub_http_requests_total") {
				t.Fatalf("metrics body missing request counter:\n%s", rec.Body.String())
			}
			if tc.wantStatus == http.StatusUnauthorized && strings.Contains(rec.Body.String(), "fulfillhub_http_requests_total") {
				t.Fatalf("unauthorized response must not expose metrics:\n%s", rec.Body.String())
			}
		})
	}
}

func TestCreateOrderRequiresAPIKey(t *testing.T) {
	server := testServer()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/orders", strings.NewReader(`{}`))

	server.ServeHTTP(rec, req)

	assertStatus(t, rec, http.StatusUnauthorized)
}

func TestConfiguredAPIKeysAreRequiredForMerchantAccess(t *testing.T) {
	server := NewServerWithOptions(commerce.NewService(commerce.NewMemoryStore()), Options{})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/orders", bytes.NewReader(validOrderJSON(t)))
	req.Header.Set("X-API-Key", "fh_live_merchant_demo")
	req.Header.Set("Idempotency-Key", "idem-key-0001")

	server.ServeHTTP(rec, req)

	assertStatus(t, rec, http.StatusUnauthorized)
}

func TestCreateOrderUsesMerchantFromAPIKey(t *testing.T) {
	server := testServer()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/orders", bytes.NewReader(validOrderJSON(t)))
	req.Header.Set("X-API-Key", "fh_live_merchant_demo")
	req.Header.Set("Idempotency-Key", "idem-key-0001")

	server.ServeHTTP(rec, req)

	assertStatus(t, rec, http.StatusAccepted)
	var body struct {
		Data struct {
			OrderID    string `json:"order_id"`
			MerchantID string `json:"merchant_id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Data.MerchantID != "mer_01hzy6v4egscg4r7kb3m7jq2dk" {
		t.Fatalf("merchant_id = %q, want API-key merchant", body.Data.MerchantID)
	}
	if body.Data.OrderID == "" {
		t.Fatal("order_id must be present")
	}
}

func TestCreateOrderRejectsValidationFailure(t *testing.T) {
	server := testServer()
	payload := map[string]any{
		"external_order_id": "web-100045",
		"currency":          "USD",
		"customer": map[string]any{
			"id":        "cus_23901",
			"email":     "samira@example.com",
			"full_name": "Samira Costa",
		},
		"shipping_address": map[string]any{
			"line_1":      "55 Market Street",
			"city":        "San Francisco",
			"state":       "CA",
			"postal_code": "94105",
			"country":     "US",
		},
		"items": []map[string]any{
			{
				"sku":      "SKU-CHAIR-BLK",
				"quantity": 0,
				"unit_price": map[string]any{
					"amount":   18900,
					"currency": "USD",
				},
			},
		},
		"payment_method": map[string]any{
			"provider":      "stripe",
			"payment_token": "tok_visa_01hzsample",
		},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/orders", bytes.NewReader(raw))
	req.Header.Set("X-API-Key", "fh_live_merchant_demo")
	req.Header.Set("Idempotency-Key", "idem-key-0001")

	server.ServeHTTP(rec, req)

	assertStatus(t, rec, http.StatusUnprocessableEntity)
	if !strings.Contains(rec.Body.String(), "items[0].quantity") {
		t.Fatalf("response body does not include validation field: %s", rec.Body.String())
	}
}

func TestCreateOrderAppliesRateLimit(t *testing.T) {
	server := newTestServerWithOptions(commerce.NewService(commerce.NewMemoryStore()), Options{
		RateLimiter: &fixedLimiter{allowed: false},
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/orders", bytes.NewReader(validOrderJSON(t)))
	req.Header.Set("X-API-Key", "fh_live_merchant_demo")
	req.Header.Set("Idempotency-Key", "idem-key-0001")

	server.ServeHTTP(rec, req)

	assertStatus(t, rec, http.StatusTooManyRequests)
}

func TestCreateOrderRejectsOversizedBody(t *testing.T) {
	server := newTestServerWithOptions(commerce.NewService(commerce.NewMemoryStore()), Options{
		MaxRequestBodyBytes: 64,
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/orders", bytes.NewReader(validOrderJSON(t)))
	req.Header.Set("X-API-Key", "fh_live_merchant_demo")
	req.Header.Set("Idempotency-Key", "idem-key-0001")

	server.ServeHTTP(rec, req)

	assertStatus(t, rec, http.StatusRequestEntityTooLarge)
	if !strings.Contains(rec.Body.String(), "payload_too_large") {
		t.Fatalf("oversized body response = %s, want payload_too_large", rec.Body.String())
	}
}

func TestMerchantCannotReadAnotherMerchantOrder(t *testing.T) {
	server := testServer()
	orderID := createOrder(t, server, "fh_live_merchant_demo", "idem-key-0001")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/orders/"+orderID, nil)
	req.Header.Set("X-API-Key", "fh_live_second_demo")

	server.ServeHTTP(rec, req)

	assertStatus(t, rec, http.StatusForbidden)
}

func TestOpsTokenCanReadMerchantOrder(t *testing.T) {
	server := testServer()
	orderID := createOrder(t, server, "fh_live_merchant_demo", "idem-key-0001")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/orders/"+orderID, nil)
	req.Header.Set("Authorization", "Bearer ops-token")

	server.ServeHTTP(rec, req)

	assertStatus(t, rec, http.StatusOK)
}

func TestStaticOpsTokenRequiresExplicitLocalOption(t *testing.T) {
	server := newTestServerWithOptions(commerce.NewService(commerce.NewMemoryStore()), Options{})
	orderID := createOrder(t, server, "fh_live_merchant_demo", "idem-key-0001")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/orders/"+orderID, nil)
	req.Header.Set("Authorization", "Bearer ops-token")

	server.ServeHTTP(rec, req)

	assertStatus(t, rec, http.StatusUnauthorized)
}

func TestGetShipmentReturnsMerchantShipment(t *testing.T) {
	server, store, service := testServerWithStore()
	orderID := createOrder(t, server, "fh_live_merchant_demo", "idem-key-0001")
	shipmentID := recordShipment(t, store, service, orderID)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/shipments/"+shipmentID, nil)
	req.Header.Set("X-API-Key", "fh_live_merchant_demo")

	server.ServeHTTP(rec, req)

	assertStatus(t, rec, http.StatusOK)
	var body struct {
		Data commerce.ShipmentRecord `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode shipment response: %v", err)
	}
	if body.Data.ShipmentID != shipmentID || body.Data.OrderID != orderID {
		t.Fatalf("shipment response = %+v, want shipment %s for order %s", body.Data, shipmentID, orderID)
	}
	if body.Data.MerchantID != "mer_01hzy6v4egscg4r7kb3m7jq2dk" {
		t.Fatalf("shipment merchant = %q, want API-key merchant", body.Data.MerchantID)
	}
	if body.Data.Carrier != "ups" || len(body.Data.Events) != 1 {
		t.Fatalf("shipment projection = %+v, want carrier and timeline", body.Data)
	}
}

func TestMerchantCannotReadAnotherMerchantShipment(t *testing.T) {
	server, store, service := testServerWithStore()
	orderID := createOrder(t, server, "fh_live_merchant_demo", "idem-key-0001")
	shipmentID := recordShipment(t, store, service, orderID)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/shipments/"+shipmentID, nil)
	req.Header.Set("X-API-Key", "fh_live_second_demo")

	server.ServeHTTP(rec, req)

	assertStatus(t, rec, http.StatusForbidden)
}

func TestOpsTokenCanReadMerchantShipment(t *testing.T) {
	server, store, service := testServerWithStore()
	orderID := createOrder(t, server, "fh_live_merchant_demo", "idem-key-0001")
	shipmentID := recordShipment(t, store, service, orderID)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/shipments/"+shipmentID, nil)
	req.Header.Set("Authorization", "Bearer ops-token")

	server.ServeHTTP(rec, req)

	assertStatus(t, rec, http.StatusOK)
}

func TestGetShipmentRequiresAuthentication(t *testing.T) {
	server, store, service := testServerWithStore()
	orderID := createOrder(t, server, "fh_live_merchant_demo", "idem-key-0001")
	shipmentID := recordShipment(t, store, service, orderID)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/shipments/"+shipmentID, nil)

	server.ServeHTTP(rec, req)

	assertStatus(t, rec, http.StatusUnauthorized)
}

func TestOpsJWTCanReadMerchantOrder(t *testing.T) {
	const secret = "ops-jwt-secret"
	server := newTestServerWithOptions(commerce.NewService(commerce.NewMemoryStore()), Options{
		OpsJWTSecret: secret,
	})
	orderID := createOrder(t, server, "fh_live_merchant_demo", "idem-key-0001")
	token := signOpsJWT(t, secret, map[string]any{
		"sub":   "usr_ops_1",
		"roles": []string{"operations"},
		"exp":   time.Now().Add(time.Hour).Unix(),
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/orders/"+orderID, nil)
	req.Header.Set("Authorization", "Bearer "+token)

	server.ServeHTTP(rec, req)

	assertStatus(t, rec, http.StatusOK)
}

func TestOpsJWTValidatesIssuerAndAudience(t *testing.T) {
	const secret = "ops-jwt-secret"
	server := newTestServerWithOptions(commerce.NewService(commerce.NewMemoryStore()), Options{
		OpsJWTSecret:   secret,
		OpsJWTIssuer:   "https://ops.fulfillhub.local",
		OpsJWTAudience: "fulfillhub-ops",
	})
	orderID := createOrder(t, server, "fh_live_merchant_demo", "idem-key-0001")
	token := signOpsJWT(t, secret, map[string]any{
		"sub":   "usr_ops_1",
		"iss":   "https://ops.fulfillhub.local",
		"aud":   []string{"fulfillhub-ops"},
		"roles": []string{"operations"},
		"exp":   time.Now().Add(time.Hour).Unix(),
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/orders/"+orderID, nil)
	req.Header.Set("Authorization", "Bearer "+token)

	server.ServeHTTP(rec, req)

	assertStatus(t, rec, http.StatusOK)
}

func TestOpsJWTRejectsWrongIssuerOrAudience(t *testing.T) {
	const secret = "ops-jwt-secret"
	server := newTestServerWithOptions(commerce.NewService(commerce.NewMemoryStore()), Options{
		OpsJWTSecret:   secret,
		OpsJWTIssuer:   "https://ops.fulfillhub.local",
		OpsJWTAudience: "fulfillhub-ops",
	})
	orderID := createOrder(t, server, "fh_live_merchant_demo", "idem-key-0001")
	token := signOpsJWT(t, secret, map[string]any{
		"sub":   "usr_ops_1",
		"iss":   "https://issuer.example.invalid",
		"aud":   "fulfillhub-ops",
		"roles": []string{"operations"},
		"exp":   time.Now().Add(time.Hour).Unix(),
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/orders/"+orderID, nil)
	req.Header.Set("Authorization", "Bearer "+token)

	server.ServeHTTP(rec, req)

	assertStatus(t, rec, http.StatusUnauthorized)
}

func TestOpsJWTAcceptsPreviousSecretDuringRotation(t *testing.T) {
	const oldSecret = "old-ops-jwt-secret"
	server := newTestServerWithOptions(commerce.NewService(commerce.NewMemoryStore()), Options{
		OpsJWTSecret:          "new-ops-jwt-secret",
		OpsJWTPreviousSecrets: []string{oldSecret},
	})
	orderID := createOrder(t, server, "fh_live_merchant_demo", "idem-key-0001")
	token := signOpsJWT(t, oldSecret, map[string]any{
		"sub":   "usr_ops_1",
		"roles": []string{"operations"},
		"exp":   time.Now().Add(time.Hour).Unix(),
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/orders/"+orderID, nil)
	req.Header.Set("Authorization", "Bearer "+token)

	server.ServeHTTP(rec, req)

	assertStatus(t, rec, http.StatusOK)
}

func TestOpsJWTRejectsMissingOperationsRole(t *testing.T) {
	const secret = "ops-jwt-secret"
	server := newTestServerWithOptions(commerce.NewService(commerce.NewMemoryStore()), Options{
		OpsJWTSecret: secret,
	})
	orderID := createOrder(t, server, "fh_live_merchant_demo", "idem-key-0001")
	token := signOpsJWT(t, secret, map[string]any{
		"sub":   "usr_support_1",
		"roles": []string{"support"},
		"exp":   time.Now().Add(time.Hour).Unix(),
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/orders/"+orderID, nil)
	req.Header.Set("Authorization", "Bearer "+token)

	server.ServeHTTP(rec, req)

	assertStatus(t, rec, http.StatusUnauthorized)
}

func TestStaticOpsTokenDisabledWhenJWTSecretConfigured(t *testing.T) {
	server := newTestServerWithOptions(commerce.NewService(commerce.NewMemoryStore()), Options{
		OpsJWTSecret: "ops-jwt-secret",
	})
	orderID := createOrder(t, server, "fh_live_merchant_demo", "idem-key-0001")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/orders/"+orderID, nil)
	req.Header.Set("Authorization", "Bearer ops-token")

	server.ServeHTTP(rec, req)

	assertStatus(t, rec, http.StatusUnauthorized)
}

func TestCancelOrderAcceptsValidRequest(t *testing.T) {
	server, _, service := testServerWithStore()
	orderID := createOrder(t, server, "fh_live_merchant_demo", "idem-key-0001")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/orders/"+orderID+"/cancel", strings.NewReader(`{
		"reason": "customer_requested",
		"requested_by": {
			"type": "merchant_user",
			"id": "usr_93842"
		}
	}`))
	req.Header.Set("X-API-Key", "fh_live_merchant_demo")

	server.ServeHTTP(rec, req)

	assertStatus(t, rec, http.StatusAccepted)
	if !strings.Contains(rec.Body.String(), "cancellation_pending") {
		t.Fatalf("response body does not include cancellation status: %s", rec.Body.String())
	}
	logs := service.AuditLogs()
	cancelLog := logs[len(logs)-1]
	if cancelLog.Action != "order.cancel_requested" || cancelLog.Details["reason"] != "customer_requested" {
		t.Fatalf("cancel audit log = %+v, want reason customer_requested", cancelLog)
	}
}

func TestCancelOrderValidatesBody(t *testing.T) {
	server := testServer()
	orderID := createOrder(t, server, "fh_live_merchant_demo", "idem-key-0001")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/orders/"+orderID+"/cancel", strings.NewReader(`{}`))
	req.Header.Set("X-API-Key", "fh_live_merchant_demo")

	server.ServeHTTP(rec, req)

	assertStatus(t, rec, http.StatusUnprocessableEntity)
	if !strings.Contains(rec.Body.String(), "requested_by.type") {
		t.Fatalf("response body does not include requested_by.type: %s", rec.Body.String())
	}
}

func TestDuplicateOrderReturnsConflict(t *testing.T) {
	server := testServer()
	createOrder(t, server, "fh_live_merchant_demo", "idem-key-0001")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/orders", bytes.NewReader(validOrderJSON(t)))
	req.Header.Set("X-API-Key", "fh_live_merchant_demo")
	req.Header.Set("Idempotency-Key", "idem-key-0002")

	server.ServeHTTP(rec, req)

	assertStatus(t, rec, http.StatusConflict)
}

func TestIdempotentReplayReturnsOriginalOrder(t *testing.T) {
	server := testServer()
	first := createOrder(t, server, "fh_live_merchant_demo", "idem-key-0001")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/orders", bytes.NewReader(validOrderJSON(t)))
	req.Header.Set("X-API-Key", "fh_live_merchant_demo")
	req.Header.Set("Idempotency-Key", "idem-key-0001")

	server.ServeHTTP(rec, req)

	assertStatus(t, rec, http.StatusAccepted)
	if rec.Header().Get("X-Idempotent-Replay") != "true" {
		t.Fatal("expected X-Idempotent-Replay header")
	}
	if got := decodeOrderID(t, rec); got != first {
		t.Fatalf("order id = %q, want %q", got, first)
	}
}

func BenchmarkCreateOrder(b *testing.B) {
	for i := 0; i < b.N; i++ {
		server := testServer()
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/orders", bytes.NewReader(validOrderJSON(b)))
		req.Header.Set("X-API-Key", "fh_live_merchant_demo")
		req.Header.Set("Idempotency-Key", "idem-key-"+strings.Repeat("0", 12)+string(rune(i%26+'a')))

		server.ServeHTTP(rec, req)

		if rec.Code != http.StatusAccepted {
			b.Fatalf("status = %d, want 202: %s", rec.Code, rec.Body.String())
		}
	}
}

func testServer() http.Handler {
	server, _, _ := testServerWithStore()
	return server
}

func newTestServerWithOptions(service *commerce.Service, options Options) http.Handler {
	if len(options.APIKeys) == 0 {
		options.APIKeys = LocalDemoAPIKeys()
	}
	return NewServerWithOptions(service, options)
}

func testServerWithStore() (http.Handler, *commerce.MemoryStore, *commerce.Service) {
	store := commerce.NewMemoryStore()
	service := commerce.NewService(store)
	return NewServer(service), store, service
}

func createOrder(t testing.TB, server http.Handler, apiKey, idempotencyKey string) string {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/orders", bytes.NewReader(validOrderJSON(t)))
	req.Header.Set("X-API-Key", apiKey)
	req.Header.Set("Idempotency-Key", idempotencyKey)

	server.ServeHTTP(rec, req)

	assertStatus(t, rec, http.StatusAccepted)
	return decodeOrderID(t, rec)
}

func recordShipment(t testing.TB, store *commerce.MemoryStore, service *commerce.Service, orderID string) string {
	t.Helper()
	created := service.OutboxEvents()[0]
	advanceOrderStatusForAPITest(t, store, orderID, created.MerchantID, commerce.StatusInventoryReserved, created.CorrelationID)
	advanceOrderStatusForAPITest(t, store, orderID, created.MerchantID, commerce.StatusPaymentAuthorized, created.CorrelationID)
	shipmentID := "shp_api_test"
	occurredAt := time.Date(2026, 5, 29, 15, 30, 0, 0, time.UTC)
	event := commerce.OutboxEvent{
		MessageID:     "msg_api_shipment",
		CorrelationID: created.CorrelationID,
		CausationID:   created.MessageID,
		EventType:     "shipment.created",
		OrderID:       orderID,
		MerchantID:    created.MerchantID,
		OccurredAt:    occurredAt,
	}
	if err := store.RecordShipmentCreated(context.Background(), created, event, commerce.Shipment{
		ShipmentID:     shipmentID,
		Status:         "created",
		Carrier:        "ups",
		TrackingNumber: "1Z999AA10123456784",
		Events: []commerce.ShipmentEvent{{
			OccurredAt:  occurredAt,
			Status:      "created",
			Description: "Label created with carrier.",
		}},
	}, commerce.AuditLog{
		MerchantID:    created.MerchantID,
		OrderID:       orderID,
		ActorType:     "system",
		ActorID:       "fulfillment-worker",
		Action:        "shipment.created",
		CorrelationID: created.CorrelationID,
		CreatedAt:     occurredAt,
	}); err != nil {
		t.Fatalf("record shipment: %v", err)
	}
	return shipmentID
}

func advanceOrderStatusForAPITest(t testing.TB, store *commerce.MemoryStore, orderID, merchantID string, status commerce.OrderStatus, correlationID string) {
	t.Helper()
	now := time.Date(2026, 5, 29, 15, 20, 0, 0, time.UTC)
	event := commerce.OutboxEvent{
		MessageID:     "msg_api_" + string(status),
		CorrelationID: correlationID,
		CausationID:   "msg_api_test_causation",
		EventType:     "test.status_advanced",
		OrderID:       orderID,
		MerchantID:    merchantID,
		OccurredAt:    now,
	}
	audit := commerce.AuditLog{
		MerchantID:    merchantID,
		OrderID:       orderID,
		ActorType:     "test",
		ActorID:       "test",
		Action:        "test.status_advanced",
		CorrelationID: correlationID,
		CreatedAt:     now,
	}
	if _, err := store.UpdateOrderStatus(context.Background(), orderID, status, now, event, audit); err != nil {
		t.Fatalf("advance order to %s: %v", status, err)
	}
}

func decodeOrderID(t testing.TB, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Data struct {
			OrderID string `json:"order_id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return body.Data.OrderID
}

func validOrderJSON(t testing.TB) []byte {
	t.Helper()
	payload := map[string]any{
		"external_order_id": "web-100045",
		"currency":          "USD",
		"customer": map[string]any{
			"id":        "cus_23901",
			"email":     "samira@example.com",
			"full_name": "Samira Costa",
		},
		"shipping_address": map[string]any{
			"line_1":      "55 Market Street",
			"city":        "San Francisco",
			"state":       "CA",
			"postal_code": "94105",
			"country":     "US",
		},
		"items": []map[string]any{
			{
				"sku":      "SKU-CHAIR-BLK",
				"quantity": 1,
				"unit_price": map[string]any{
					"amount":   18900,
					"currency": "USD",
				},
			},
		},
		"payment_method": map[string]any{
			"provider":      "stripe",
			"payment_token": "tok_visa_01hzsample",
		},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return raw
}

func assertStatus(t testing.TB, rec *httptest.ResponseRecorder, want int) {
	t.Helper()
	if rec.Code != want {
		t.Fatalf("status = %d, want %d: %s", rec.Code, want, rec.Body.String())
	}
}

type fixedLimiter struct {
	allowed bool
	err     error
}

func (l *fixedLimiter) Allow(context.Context, string) (bool, error) {
	return l.allowed, l.err
}

type fakeQueueMetrics struct {
	depths []messaging.QueueDepth
	err    error
}

func (m fakeQueueMetrics) QueueDepths(context.Context) ([]messaging.QueueDepth, error) {
	return m.depths, m.err
}

type fakeOutboxBacklog struct {
	count            int
	oldestAgeSeconds float64
	err              error
}

func (m fakeOutboxBacklog) PendingOutboxCount(context.Context) (int, error) {
	return m.count, m.err
}

func (m fakeOutboxBacklog) OldestPendingOutboxAgeSeconds(context.Context) (float64, error) {
	return m.oldestAgeSeconds, m.err
}

type fakeSagaMetrics struct {
	counts map[commerce.OrderStatus]int
	err    error
}

func (m fakeSagaMetrics) OrderStatusCounts(context.Context) (map[commerce.OrderStatus]int, error) {
	return m.counts, m.err
}

func assertSpanAttribute(t testing.TB, span sdktrace.ReadOnlySpan, key, want string) {
	t.Helper()
	for _, attr := range span.Attributes() {
		if string(attr.Key) == key {
			if got := attr.Value.AsString(); got != want {
				t.Fatalf("span attribute %s = %q, want %q", key, got, want)
			}
			return
		}
	}
	t.Fatalf("span attribute %s not found", key)
}

func signOpsJWT(t testing.TB, secret string, claims map[string]any) string {
	t.Helper()
	header := map[string]string{"alg": "HS256", "typ": "JWT"}
	encodedHeader := encodeJWTPart(t, header)
	encodedClaims := encodeJWTPart(t, claims)
	signingInput := encodedHeader + "." + encodedClaims
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(signingInput))
	signature := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return signingInput + "." + signature
}

func encodeJWTPart(t testing.TB, value any) string {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal jwt part: %v", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}
