package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Defyland/fulfillhub-go-commerce-platform/internal/commerce"
)

type Server struct {
	service *commerce.Service
	apiKeys map[string]string
	counter atomic.Uint64
	metrics metrics
	limiter RateLimiter
}

type RateLimiter interface {
	Allow(ctx context.Context, key string) (bool, error)
}

type Options struct {
	RateLimiter RateLimiter
}

type metrics struct {
	requests atomic.Uint64
	errors   atomic.Uint64
}

type actor struct {
	MerchantID string
	Ops        bool
}

type responseMeta struct {
	RequestID     string     `json:"request_id"`
	CorrelationID string     `json:"correlation_id"`
	Timestamp     *time.Time `json:"timestamp,omitempty"`
}

type envelope struct {
	Data any          `json:"data"`
	Meta responseMeta `json:"meta"`
}

type errorEnvelope struct {
	Error errorObject  `json:"error"`
	Meta  responseMeta `json:"meta"`
}

type errorObject struct {
	Code      string        `json:"code"`
	Message   string        `json:"message"`
	Retryable bool          `json:"retryable"`
	Details   []errorDetail `json:"details"`
}

type errorDetail struct {
	Field string `json:"field"`
	Issue string `json:"issue"`
}

type cancelOrderRequest struct {
	Reason      string `json:"reason"`
	RequestedBy struct {
		Type string `json:"type"`
		ID   string `json:"id"`
	} `json:"requested_by"`
}

func NewServer(service *commerce.Service) http.Handler {
	return NewServerWithOptions(service, Options{})
}

func NewServerWithOptions(service *commerce.Service, options Options) http.Handler {
	return &Server{
		service: service,
		apiKeys: map[string]string{
			"fh_live_merchant_demo": "mer_01hzy6v4egscg4r7kb3m7jq2dk",
			"fh_live_second_demo":   "mer_01hzy8v4egscg4r7kb3m7jq9qx",
		},
		limiter: options.RateLimiter,
	}
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.metrics.requests.Add(1)
	requestID := headerOrNew(r.Header.Get("X-Request-Id"), "req", s.counter.Add(1))
	correlationID := headerOrDefault(r.Header.Get("X-Correlation-Id"), requestID)

	w.Header().Set("X-Request-Id", requestID)
	w.Header().Set("X-Correlation-Id", correlationID)

	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/healthz":
		writeJSON(w, http.StatusOK, map[string]any{
			"status":    "ok",
			"service":   "fulfillhub-api",
			"timestamp": time.Now().UTC(),
		})
	case r.Method == http.MethodGet && r.URL.Path == "/readyz":
		writeJSON(w, http.StatusOK, map[string]any{
			"status": "ready",
			"checks": map[string]string{
				"store":  "up",
				"broker": "deferred",
				"cache":  "deferred",
			},
			"timestamp": time.Now().UTC(),
		})
	case r.Method == http.MethodGet && r.URL.Path == "/metrics":
		s.writeMetrics(w)
	case r.Method == http.MethodPost && r.URL.Path == "/api/v1/orders":
		s.createOrder(w, r, requestID, correlationID)
	case strings.HasPrefix(r.URL.Path, "/api/v1/orders/"):
		s.orderRoute(w, r, requestID, correlationID)
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/shipments/"):
		s.writeError(w, http.StatusNotFound, "not_found", "Shipment not found.", false, nil, requestID, correlationID)
	default:
		s.writeError(w, http.StatusNotFound, "not_found", "Route not found.", false, nil, requestID, correlationID)
	}
}

func (s *Server) createOrder(w http.ResponseWriter, r *http.Request, requestID, correlationID string) {
	act, ok := s.authenticateMerchant(w, r, requestID, correlationID)
	if !ok {
		return
	}
	if !s.allowWrite(w, r, act.MerchantID, requestID, correlationID) {
		return
	}

	var req commerce.CreateOrderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid_json", "Request body is not valid JSON.", false, nil, requestID, correlationID)
		return
	}

	order, replayed, err := s.service.CreateOrder(act.MerchantID, r.Header.Get("Idempotency-Key"), correlationID, req)
	if err != nil {
		s.handleCommerceError(w, err, requestID, correlationID)
		return
	}

	status := http.StatusAccepted
	if replayed {
		w.Header().Set("X-Idempotent-Replay", "true")
	}
	writeJSON(w, status, envelope{
		Data: map[string]any{
			"order_id":    order.OrderID,
			"merchant_id": order.MerchantID,
			"status":      order.Status,
			"accepted_at": order.CreatedAt,
		},
		Meta: responseMeta{RequestID: requestID, CorrelationID: correlationID},
	})
}

func (s *Server) allowWrite(w http.ResponseWriter, r *http.Request, merchantID, requestID, correlationID string) bool {
	if s.limiter == nil {
		return true
	}
	allowed, err := s.limiter.Allow(r.Context(), "merchant:"+merchantID+":write")
	if err != nil {
		s.writeError(w, http.StatusServiceUnavailable, "dependency_unavailable", "Rate limiter is unavailable.", true, nil, requestID, correlationID)
		return false
	}
	if !allowed {
		s.writeError(w, http.StatusTooManyRequests, "rate_limited", "Too many order creation requests for this merchant.", true, nil, requestID, correlationID)
		return false
	}
	return true
}

func (s *Server) orderRoute(w http.ResponseWriter, r *http.Request, requestID, correlationID string) {
	if strings.HasSuffix(r.URL.Path, "/cancel") && r.Method == http.MethodPost {
		orderID := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/v1/orders/"), "/cancel")
		s.cancelOrder(w, r, orderID, requestID, correlationID)
		return
	}
	if r.Method == http.MethodGet {
		orderID := strings.TrimPrefix(r.URL.Path, "/api/v1/orders/")
		s.getOrder(w, r, orderID, requestID, correlationID)
		return
	}
	s.writeError(w, http.StatusNotFound, "not_found", "Route not found.", false, nil, requestID, correlationID)
}

func (s *Server) getOrder(w http.ResponseWriter, r *http.Request, orderID, requestID, correlationID string) {
	act, ok := s.authenticate(w, r, requestID, correlationID)
	if !ok {
		return
	}
	order, err := s.service.GetOrder(orderID)
	if err != nil {
		s.handleCommerceError(w, err, requestID, correlationID)
		return
	}
	if !act.Ops && act.MerchantID != order.MerchantID {
		s.writeError(w, http.StatusForbidden, "forbidden", "The caller cannot access this order.", false, nil, requestID, correlationID)
		return
	}
	writeJSON(w, http.StatusOK, envelope{
		Data: order,
		Meta: responseMeta{RequestID: requestID, CorrelationID: correlationID},
	})
}

func (s *Server) cancelOrder(w http.ResponseWriter, r *http.Request, orderID, requestID, correlationID string) {
	act, ok := s.authenticate(w, r, requestID, correlationID)
	if !ok {
		return
	}
	var cancelReq cancelOrderRequest
	if err := json.NewDecoder(r.Body).Decode(&cancelReq); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid_json", "Request body is not valid JSON.", false, nil, requestID, correlationID)
		return
	}
	if strings.TrimSpace(cancelReq.Reason) == "" || strings.TrimSpace(cancelReq.RequestedBy.ID) == "" {
		s.writeError(w, http.StatusUnprocessableEntity, "validation_failed", "Request body contains invalid fields.", false, []errorDetail{
			{Field: "reason", Issue: "is required"},
			{Field: "requested_by.id", Issue: "is required"},
		}, requestID, correlationID)
		return
	}
	order, err := s.service.GetOrder(orderID)
	if err != nil {
		s.handleCommerceError(w, err, requestID, correlationID)
		return
	}
	if !act.Ops && act.MerchantID != order.MerchantID {
		s.writeError(w, http.StatusForbidden, "forbidden", "The caller cannot access this order.", false, nil, requestID, correlationID)
		return
	}

	order, err = s.service.CancelOrder(orderID, correlationID)
	if err != nil {
		s.handleCommerceError(w, err, requestID, correlationID)
		return
	}
	writeJSON(w, http.StatusAccepted, envelope{
		Data: map[string]any{
			"order_id":    order.OrderID,
			"merchant_id": order.MerchantID,
			"status":      order.Status,
			"accepted_at": order.UpdatedAt,
		},
		Meta: responseMeta{RequestID: requestID, CorrelationID: correlationID},
	})
}

func (s *Server) authenticateMerchant(w http.ResponseWriter, r *http.Request, requestID, correlationID string) (actor, bool) {
	apiKey := r.Header.Get("X-API-Key")
	merchantID, ok := s.apiKeys[apiKey]
	if !ok {
		s.writeError(w, http.StatusUnauthorized, "unauthorized", "Merchant API key is missing or invalid.", false, nil, requestID, correlationID)
		return actor{}, false
	}
	return actor{MerchantID: merchantID}, true
}

func (s *Server) authenticate(w http.ResponseWriter, r *http.Request, requestID, correlationID string) (actor, bool) {
	if apiKey := r.Header.Get("X-API-Key"); apiKey != "" {
		merchantID, ok := s.apiKeys[apiKey]
		if !ok {
			s.writeError(w, http.StatusUnauthorized, "unauthorized", "Merchant API key is missing or invalid.", false, nil, requestID, correlationID)
			return actor{}, false
		}
		return actor{MerchantID: merchantID}, true
	}
	if r.Header.Get("Authorization") == "Bearer ops-token" {
		return actor{Ops: true}, true
	}
	s.writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication is required.", false, nil, requestID, correlationID)
	return actor{}, false
}

func (s *Server) handleCommerceError(w http.ResponseWriter, err error, requestID, correlationID string) {
	var validation commerce.ValidationError
	switch {
	case errors.As(err, &validation):
		details := make([]errorDetail, 0, len(validation.Fields))
		for _, field := range validation.Fields {
			details = append(details, errorDetail{Field: field.Field, Issue: field.Issue})
		}
		s.writeError(w, http.StatusUnprocessableEntity, "validation_failed", "Request body contains invalid fields.", false, details, requestID, correlationID)
	case errors.Is(err, commerce.ErrDuplicateOrder):
		s.writeError(w, http.StatusConflict, "duplicate_order", "The external order ID has already been accepted for this merchant.", false, nil, requestID, correlationID)
	case errors.Is(err, commerce.ErrInvalidStateTransition):
		s.writeError(w, http.StatusConflict, "invalid_state_transition", "The requested state transition is not allowed.", false, nil, requestID, correlationID)
	case errors.Is(err, commerce.ErrNotFound):
		s.writeError(w, http.StatusNotFound, "not_found", "Resource not found.", false, nil, requestID, correlationID)
	default:
		s.writeError(w, http.StatusInternalServerError, "internal_error", "Unexpected server error.", true, nil, requestID, correlationID)
	}
}

func (s *Server) writeError(w http.ResponseWriter, status int, code, message string, retryable bool, details []errorDetail, requestID, correlationID string) {
	s.metrics.errors.Add(1)
	if details == nil {
		details = []errorDetail{}
	}
	now := time.Now().UTC()
	writeJSON(w, status, errorEnvelope{
		Error: errorObject{
			Code:      code,
			Message:   message,
			Retryable: retryable,
			Details:   details,
		},
		Meta: responseMeta{
			RequestID:     requestID,
			CorrelationID: correlationID,
			Timestamp:     &now,
		},
	})
}

func (s *Server) writeMetrics(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	_, _ = fmt.Fprintf(w, "# HELP fulfillhub_http_requests_total Total HTTP requests handled.\n")
	_, _ = fmt.Fprintf(w, "# TYPE fulfillhub_http_requests_total counter\n")
	_, _ = fmt.Fprintf(w, "fulfillhub_http_requests_total %d\n", s.metrics.requests.Load())
	_, _ = fmt.Fprintf(w, "# HELP fulfillhub_http_errors_total Total HTTP error responses returned.\n")
	_, _ = fmt.Fprintf(w, "# TYPE fulfillhub_http_errors_total counter\n")
	_, _ = fmt.Fprintf(w, "fulfillhub_http_errors_total %d\n", s.metrics.errors.Load())
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func headerOrNew(value, prefix string, n uint64) string {
	if strings.TrimSpace(value) != "" {
		return value
	}
	return fmt.Sprintf("%s_%012d", prefix, n)
}

func headerOrDefault(value, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return value
	}
	return fallback
}
