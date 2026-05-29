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

func TestServerWritesStructuredRequestLog(t *testing.T) {
	var logBuffer bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logBuffer, nil))
	server := NewServerWithOptions(commerce.NewService(commerce.NewMemoryStore()), Options{
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
	server := NewServerWithOptions(commerce.NewService(commerce.NewMemoryStore()), Options{
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
	server := NewServerWithOptions(commerce.NewService(commerce.NewMemoryStore()), Options{
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

func TestMetricsReportsRabbitMQQueueMetricsDown(t *testing.T) {
	server := NewServerWithOptions(commerce.NewService(commerce.NewMemoryStore()), Options{
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

func TestCreateOrderRequiresAPIKey(t *testing.T) {
	server := testServer()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/orders", strings.NewReader(`{}`))

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
	server := NewServerWithOptions(commerce.NewService(commerce.NewMemoryStore()), Options{
		RateLimiter: &fixedLimiter{allowed: false},
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/orders", bytes.NewReader(validOrderJSON(t)))
	req.Header.Set("X-API-Key", "fh_live_merchant_demo")
	req.Header.Set("Idempotency-Key", "idem-key-0001")

	server.ServeHTTP(rec, req)

	assertStatus(t, rec, http.StatusTooManyRequests)
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

func TestOpsJWTCanReadMerchantOrder(t *testing.T) {
	const secret = "ops-jwt-secret"
	server := NewServerWithOptions(commerce.NewService(commerce.NewMemoryStore()), Options{
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

func TestOpsJWTRejectsMissingOperationsRole(t *testing.T) {
	const secret = "ops-jwt-secret"
	server := NewServerWithOptions(commerce.NewService(commerce.NewMemoryStore()), Options{
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
	server := NewServerWithOptions(commerce.NewService(commerce.NewMemoryStore()), Options{
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
	server := testServer()
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
	return NewServer(commerce.NewService(commerce.NewMemoryStore()))
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
