package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Defyland/fulfillhub-go-commerce-platform/internal/commerce"
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
