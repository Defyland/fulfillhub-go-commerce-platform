package api

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Defyland/fulfillhub-go-commerce-platform/internal/commerce"
	"github.com/Defyland/fulfillhub-go-commerce-platform/internal/messaging"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

type Server struct {
	service       *commerce.Service
	apiKeys       map[string]string
	counter       atomic.Uint64
	metrics       metrics
	limiter       RateLimiter
	logger        *slog.Logger
	tracer        trace.Tracer
	propagator    propagation.TextMapPropagator
	opsSecrets    [][]byte
	opsIssuer     string
	opsAudience   string
	queueMetrics  messaging.QueueMetricsProvider
	outboxMetrics OutboxBacklogProvider
	metricsToken  string
	readiness     map[string]ReadinessChecker
}

type RateLimiter interface {
	Allow(ctx context.Context, key string) (bool, error)
}

type OutboxBacklogProvider interface {
	PendingOutboxCount(ctx context.Context) (int, error)
}

type ReadinessChecker interface {
	CheckReadiness(ctx context.Context) error
}

type ReadinessCheckFunc func(context.Context) error

func (f ReadinessCheckFunc) CheckReadiness(ctx context.Context) error {
	return f(ctx)
}

type Options struct {
	RateLimiter           RateLimiter
	Logger                *slog.Logger
	TracerProvider        trace.TracerProvider
	Propagator            propagation.TextMapPropagator
	OpsJWTSecret          string
	OpsJWTPreviousSecrets []string
	OpsJWTIssuer          string
	OpsJWTAudience        string
	QueueMetrics          messaging.QueueMetricsProvider
	OutboxBacklog         OutboxBacklogProvider
	MetricsBearerToken    string
	ReadinessChecks       map[string]ReadinessChecker
}

type metrics struct {
	requests atomic.Uint64
	errors   atomic.Uint64
}

type actor struct {
	MerchantID string
	ActorID    string
	Ops        bool
}

type requestState struct {
	ActorType  string
	ActorID    string
	MerchantID string
}

type opsJWTClaims struct {
	Subject   string      `json:"sub"`
	Issuer    string      `json:"iss"`
	Audience  jwtAudience `json:"aud"`
	ExpiresAt int64       `json:"exp"`
	Role      string      `json:"role"`
	Roles     []string    `json:"roles"`
}

type jwtAudience []string

type requestStateKey struct{}

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
	tracerProvider := options.TracerProvider
	if tracerProvider == nil {
		tracerProvider = otel.GetTracerProvider()
	}
	propagator := options.Propagator
	if propagator == nil {
		propagator = propagation.TraceContext{}
	}
	readiness := map[string]ReadinessChecker{
		"store": ReadinessCheckFunc(func(context.Context) error { return nil }),
	}
	for name, check := range options.ReadinessChecks {
		name = strings.TrimSpace(name)
		if name == "" || check == nil {
			continue
		}
		readiness[name] = check
	}
	return &Server{
		service: service,
		apiKeys: map[string]string{
			"fh_live_merchant_demo": "mer_01hzy6v4egscg4r7kb3m7jq2dk",
			"fh_live_second_demo":   "mer_01hzy8v4egscg4r7kb3m7jq9qx",
		},
		limiter:       options.RateLimiter,
		logger:        options.Logger,
		tracer:        tracerProvider.Tracer("github.com/Defyland/fulfillhub-go-commerce-platform/internal/api"),
		propagator:    propagator,
		opsSecrets:    opsJWTSecrets(options.OpsJWTSecret, options.OpsJWTPreviousSecrets),
		opsIssuer:     strings.TrimSpace(options.OpsJWTIssuer),
		opsAudience:   strings.TrimSpace(options.OpsJWTAudience),
		queueMetrics:  options.QueueMetrics,
		outboxMetrics: options.OutboxBacklog,
		metricsToken:  strings.TrimSpace(options.MetricsBearerToken),
		readiness:     readiness,
	}
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	startedAt := time.Now()
	s.metrics.requests.Add(1)
	requestID := headerOrNew(r.Header.Get("X-Request-Id"), "req", s.counter.Add(1))
	correlationID := headerOrDefault(r.Header.Get("X-Correlation-Id"), requestID)
	state := &requestState{}
	ctx := s.propagator.Extract(r.Context(), propagation.HeaderCarrier(r.Header))
	ctx = context.WithValue(ctx, requestStateKey{}, state)
	ctx, span := s.tracer.Start(ctx, routeSpanName(r))
	r = r.WithContext(ctx)
	rec := &statusRecorder{ResponseWriter: w}

	w = rec
	w.Header().Set("X-Request-Id", requestID)
	w.Header().Set("X-Correlation-Id", correlationID)
	defer func() {
		status := rec.status
		if status == 0 {
			status = http.StatusOK
		}
		duration := time.Since(startedAt)
		span.SetAttributes(
			attribute.String("http.request.method", r.Method),
			attribute.String("url.path", r.URL.Path),
			attribute.Int("http.response.status_code", status),
			attribute.String("fulfillhub.request_id", requestID),
			attribute.String("fulfillhub.correlation_id", correlationID),
		)
		if state.ActorType != "" {
			span.SetAttributes(attribute.String("fulfillhub.actor_type", state.ActorType))
		}
		if state.ActorID != "" {
			span.SetAttributes(attribute.String("fulfillhub.actor_id", state.ActorID))
		}
		if state.MerchantID != "" {
			span.SetAttributes(attribute.String("fulfillhub.merchant_id", state.MerchantID))
		}
		if status >= http.StatusInternalServerError {
			span.SetStatus(codes.Error, http.StatusText(status))
		}
		s.logRequest(r.Context(), r, status, rec.bytes, duration, requestID, correlationID)
		span.End()
	}()

	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/healthz":
		writeJSON(w, http.StatusOK, map[string]any{
			"status":    "ok",
			"service":   "fulfillhub-api",
			"timestamp": time.Now().UTC(),
		})
	case r.Method == http.MethodGet && r.URL.Path == "/readyz":
		s.writeReadiness(r.Context(), w, requestID, correlationID)
	case r.Method == http.MethodGet && r.URL.Path == "/metrics":
		if !s.authorizeMetrics(w, r, requestID, correlationID) {
			return
		}
		s.writeMetrics(r.Context(), w)
	case r.Method == http.MethodPost && r.URL.Path == "/api/v1/orders":
		s.createOrder(w, r, requestID, correlationID)
	case strings.HasPrefix(r.URL.Path, "/api/v1/orders/"):
		s.orderRoute(w, r, requestID, correlationID)
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/shipments/"):
		shipmentID := strings.TrimPrefix(r.URL.Path, "/api/v1/shipments/")
		s.getShipment(w, r, shipmentID, requestID, correlationID)
	default:
		s.writeError(w, http.StatusNotFound, "not_found", "Route not found.", false, nil, requestID, correlationID)
	}
}

func (s *Server) createOrder(w http.ResponseWriter, r *http.Request, requestID, correlationID string) {
	act, ok := s.authenticateMerchant(w, r, requestID, correlationID)
	if !ok {
		return
	}
	trace.SpanFromContext(r.Context()).SetAttributes(
		attribute.String("fulfillhub.merchant_id", act.MerchantID),
		attribute.String("fulfillhub.operation", "create_order"),
	)
	if !s.allowWrite(w, r, act.MerchantID, requestID, correlationID) {
		return
	}

	var req commerce.CreateOrderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid_json", "Request body is not valid JSON.", false, nil, requestID, correlationID)
		return
	}

	order, replayed, err := s.service.CreateOrderContext(r.Context(), act.MerchantID, r.Header.Get("Idempotency-Key"), correlationID, req)
	if err != nil {
		s.handleCommerceError(w, err, requestID, correlationID)
		return
	}

	status := http.StatusAccepted
	if replayed {
		w.Header().Set("X-Idempotent-Replay", "true")
	}
	trace.SpanFromContext(r.Context()).SetAttributes(
		attribute.String("fulfillhub.order_id", order.OrderID),
		attribute.Bool("fulfillhub.idempotent_replay", replayed),
	)
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
	trace.SpanFromContext(r.Context()).SetAttributes(
		attribute.String("fulfillhub.order_id", orderID),
		attribute.String("fulfillhub.operation", "get_order"),
	)
	order, err := s.service.GetOrderContext(r.Context(), orderID)
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

func (s *Server) getShipment(w http.ResponseWriter, r *http.Request, shipmentID, requestID, correlationID string) {
	act, ok := s.authenticate(w, r, requestID, correlationID)
	if !ok {
		return
	}
	trace.SpanFromContext(r.Context()).SetAttributes(
		attribute.String("fulfillhub.shipment_id", shipmentID),
		attribute.String("fulfillhub.operation", "get_shipment"),
	)
	shipment, err := s.service.GetShipmentContext(r.Context(), shipmentID)
	if err != nil {
		s.handleCommerceError(w, err, requestID, correlationID)
		return
	}
	trace.SpanFromContext(r.Context()).SetAttributes(
		attribute.String("fulfillhub.order_id", shipment.OrderID),
		attribute.String("fulfillhub.merchant_id", shipment.MerchantID),
	)
	if !act.Ops && act.MerchantID != shipment.MerchantID {
		s.writeError(w, http.StatusForbidden, "forbidden", "The caller cannot access this shipment.", false, nil, requestID, correlationID)
		return
	}
	writeJSON(w, http.StatusOK, envelope{
		Data: shipment,
		Meta: responseMeta{RequestID: requestID, CorrelationID: correlationID},
	})
}

func (s *Server) cancelOrder(w http.ResponseWriter, r *http.Request, orderID, requestID, correlationID string) {
	act, ok := s.authenticate(w, r, requestID, correlationID)
	if !ok {
		return
	}
	trace.SpanFromContext(r.Context()).SetAttributes(
		attribute.String("fulfillhub.order_id", orderID),
		attribute.String("fulfillhub.operation", "cancel_order"),
	)
	var cancelReq cancelOrderRequest
	if err := json.NewDecoder(r.Body).Decode(&cancelReq); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid_json", "Request body is not valid JSON.", false, nil, requestID, correlationID)
		return
	}
	if strings.TrimSpace(cancelReq.Reason) == "" || strings.TrimSpace(cancelReq.RequestedBy.Type) == "" || strings.TrimSpace(cancelReq.RequestedBy.ID) == "" {
		s.writeError(w, http.StatusUnprocessableEntity, "validation_failed", "Request body contains invalid fields.", false, []errorDetail{
			{Field: "reason", Issue: "is required"},
			{Field: "requested_by.type", Issue: "is required"},
			{Field: "requested_by.id", Issue: "is required"},
		}, requestID, correlationID)
		return
	}
	order, err := s.service.GetOrderContext(r.Context(), orderID)
	if err != nil {
		s.handleCommerceError(w, err, requestID, correlationID)
		return
	}
	if !act.Ops && act.MerchantID != order.MerchantID {
		s.writeError(w, http.StatusForbidden, "forbidden", "The caller cannot access this order.", false, nil, requestID, correlationID)
		return
	}

	order, err = s.service.CancelOrderContext(r.Context(), orderID, correlationID, commerce.AuditActor{
		Type:   cancelReq.RequestedBy.Type,
		ID:     cancelReq.RequestedBy.ID,
		Reason: cancelReq.Reason,
	})
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
	act := actor{MerchantID: merchantID}
	recordActor(r.Context(), act)
	return act, true
}

func (s *Server) authenticate(w http.ResponseWriter, r *http.Request, requestID, correlationID string) (actor, bool) {
	if apiKey := r.Header.Get("X-API-Key"); apiKey != "" {
		merchantID, ok := s.apiKeys[apiKey]
		if !ok {
			s.writeError(w, http.StatusUnauthorized, "unauthorized", "Merchant API key is missing or invalid.", false, nil, requestID, correlationID)
			return actor{}, false
		}
		act := actor{MerchantID: merchantID}
		recordActor(r.Context(), act)
		return act, true
	}
	if token := bearerToken(r.Header.Get("Authorization")); token != "" {
		if len(s.opsSecrets) == 0 && token == "ops-token" {
			act := actor{Ops: true, ActorID: "local-ops"}
			recordActor(r.Context(), act)
			return act, true
		}
		if subject, ok := validateOpsJWT(token, s.opsSecrets, s.opsIssuer, s.opsAudience, time.Now().UTC()); ok {
			act := actor{Ops: true, ActorID: subject}
			recordActor(r.Context(), act)
			return act, true
		}
		s.writeError(w, http.StatusUnauthorized, "unauthorized", "Operations token is missing or invalid.", false, nil, requestID, correlationID)
		return actor{}, false
	}
	s.writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication is required.", false, nil, requestID, correlationID)
	return actor{}, false
}

func (s *Server) authorizeMetrics(w http.ResponseWriter, r *http.Request, requestID, correlationID string) bool {
	if s.metricsToken == "" {
		return true
	}
	token := bearerToken(r.Header.Get("Authorization"))
	if subtle.ConstantTimeCompare([]byte(token), []byte(s.metricsToken)) == 1 {
		if state, ok := r.Context().Value(requestStateKey{}).(*requestState); ok {
			state.ActorType = "metrics"
			state.ActorID = "prometheus"
		}
		return true
	}
	s.writeError(w, http.StatusUnauthorized, "unauthorized", "Metrics bearer token is missing or invalid.", false, nil, requestID, correlationID)
	return false
}

func bearerToken(header string) string {
	authType, token, ok := strings.Cut(strings.TrimSpace(header), " ")
	if !ok || !strings.EqualFold(authType, "Bearer") {
		return ""
	}
	return strings.TrimSpace(token)
}

func validateOpsJWT(token string, secrets [][]byte, issuer, audience string, now time.Time) (string, bool) {
	if len(secrets) == 0 {
		return "", false
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return "", false
	}
	header, err := decodeJWTPart[map[string]any](parts[0])
	if err != nil || header["alg"] != "HS256" {
		return "", false
	}
	signingInput := parts[0] + "." + parts[1]
	got, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || !matchesAnyOpsJWTSecret(signingInput, got, secrets) {
		return "", false
	}
	claims, err := decodeJWTPart[opsJWTClaims](parts[1])
	if err != nil || strings.TrimSpace(claims.Subject) == "" || claims.ExpiresAt <= now.Unix() || !claims.hasOperationsRole() {
		return "", false
	}
	if issuer != "" && claims.Issuer != issuer {
		return "", false
	}
	if audience != "" && !claims.Audience.Contains(audience) {
		return "", false
	}
	return claims.Subject, true
}

func matchesAnyOpsJWTSecret(signingInput string, got []byte, secrets [][]byte) bool {
	for _, secret := range secrets {
		mac := hmac.New(sha256.New, secret)
		_, _ = mac.Write([]byte(signingInput))
		if hmac.Equal(got, mac.Sum(nil)) {
			return true
		}
	}
	return false
}

func opsJWTSecrets(current string, previous []string) [][]byte {
	values := append([]string{current}, previous...)
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

func decodeJWTPart[T any](part string) (T, error) {
	var value T
	raw, err := base64.RawURLEncoding.DecodeString(part)
	if err != nil {
		return value, err
	}
	if err := json.Unmarshal(raw, &value); err != nil {
		return value, err
	}
	return value, nil
}

func (a *jwtAudience) UnmarshalJSON(raw []byte) error {
	var single string
	if err := json.Unmarshal(raw, &single); err == nil {
		*a = jwtAudience{single}
		return nil
	}
	var many []string
	if err := json.Unmarshal(raw, &many); err != nil {
		return err
	}
	*a = many
	return nil
}

func (a jwtAudience) Contains(want string) bool {
	for _, got := range a {
		if got == want {
			return true
		}
	}
	return false
}

func (c opsJWTClaims) hasOperationsRole() bool {
	for _, role := range append(c.Roles, c.Role) {
		switch strings.TrimSpace(role) {
		case "operations", "ops":
			return true
		}
	}
	return false
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

func (s *Server) writeReadiness(ctx context.Context, w http.ResponseWriter, requestID, correlationID string) {
	checks := make(map[string]string, len(s.readiness))
	details := make([]errorDetail, 0)
	names := make([]string, 0, len(s.readiness))
	for name := range s.readiness {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		checkCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		err := s.readiness[name].CheckReadiness(checkCtx)
		cancel()
		if err != nil {
			checks[name] = "down"
			details = append(details, errorDetail{Field: name, Issue: err.Error()})
			continue
		}
		checks[name] = "up"
	}
	if len(details) > 0 {
		s.writeError(w, http.StatusServiceUnavailable, "dependency_unavailable", "One or more readiness checks failed.", true, details, requestID, correlationID)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":    "ready",
		"checks":    checks,
		"timestamp": time.Now().UTC(),
	})
}

func (s *Server) writeMetrics(ctx context.Context, w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	_, _ = fmt.Fprintf(w, "# HELP fulfillhub_http_requests_total Total HTTP requests handled.\n")
	_, _ = fmt.Fprintf(w, "# TYPE fulfillhub_http_requests_total counter\n")
	_, _ = fmt.Fprintf(w, "fulfillhub_http_requests_total %d\n", s.metrics.requests.Load())
	_, _ = fmt.Fprintf(w, "# HELP fulfillhub_http_errors_total Total HTTP error responses returned.\n")
	_, _ = fmt.Fprintf(w, "# TYPE fulfillhub_http_errors_total counter\n")
	_, _ = fmt.Fprintf(w, "fulfillhub_http_errors_total %d\n", s.metrics.errors.Load())
	s.writeOutboxMetrics(ctx, w)
	if s.queueMetrics == nil {
		return
	}

	depths, err := s.queueMetrics.QueueDepths(ctx)
	_, _ = fmt.Fprintf(w, "# HELP fulfillhub_rabbitmq_queue_metrics_up Whether RabbitMQ queue metrics were collected successfully.\n")
	_, _ = fmt.Fprintf(w, "# TYPE fulfillhub_rabbitmq_queue_metrics_up gauge\n")
	if err != nil {
		_, _ = fmt.Fprintf(w, "fulfillhub_rabbitmq_queue_metrics_up 0\n")
		return
	}
	_, _ = fmt.Fprintf(w, "fulfillhub_rabbitmq_queue_metrics_up 1\n")
	_, _ = fmt.Fprintf(w, "# HELP fulfillhub_rabbitmq_queue_messages_ready Ready messages reported by RabbitMQ queue inspection.\n")
	_, _ = fmt.Fprintf(w, "# TYPE fulfillhub_rabbitmq_queue_messages_ready gauge\n")
	for _, depth := range depths {
		_, _ = fmt.Fprintf(w, "fulfillhub_rabbitmq_queue_messages_ready{queue=\"%s\"} %d\n", prometheusLabelValue(depth.Queue), depth.MessagesReady)
	}
	_, _ = fmt.Fprintf(w, "# HELP fulfillhub_rabbitmq_queue_consumers Consumers reported by RabbitMQ queue inspection.\n")
	_, _ = fmt.Fprintf(w, "# TYPE fulfillhub_rabbitmq_queue_consumers gauge\n")
	for _, depth := range depths {
		_, _ = fmt.Fprintf(w, "fulfillhub_rabbitmq_queue_consumers{queue=\"%s\"} %d\n", prometheusLabelValue(depth.Queue), depth.Consumers)
	}
}

func (s *Server) writeOutboxMetrics(ctx context.Context, w http.ResponseWriter) {
	if s.outboxMetrics == nil {
		return
	}
	count, err := s.outboxMetrics.PendingOutboxCount(ctx)
	_, _ = fmt.Fprintf(w, "# HELP fulfillhub_outbox_metrics_up Whether outbox metrics were collected successfully.\n")
	_, _ = fmt.Fprintf(w, "# TYPE fulfillhub_outbox_metrics_up gauge\n")
	if err != nil {
		_, _ = fmt.Fprintf(w, "fulfillhub_outbox_metrics_up 0\n")
		return
	}
	_, _ = fmt.Fprintf(w, "fulfillhub_outbox_metrics_up 1\n")
	_, _ = fmt.Fprintf(w, "# HELP fulfillhub_outbox_unpublished_total Unpublished outbox events waiting for relay.\n")
	_, _ = fmt.Fprintf(w, "# TYPE fulfillhub_outbox_unpublished_total gauge\n")
	_, _ = fmt.Fprintf(w, "fulfillhub_outbox_unpublished_total %d\n", count)
}

func prometheusLabelValue(value string) string {
	return strings.NewReplacer(`\`, `\\`, "\n", `\n`, `"`, `\"`).Replace(value)
}

func (s *Server) logRequest(ctx context.Context, r *http.Request, status, bytes int, duration time.Duration, requestID, correlationID string) {
	if s.logger == nil {
		return
	}
	level := slog.LevelInfo
	if status >= http.StatusInternalServerError {
		level = slog.LevelError
	} else if status >= http.StatusBadRequest {
		level = slog.LevelWarn
	}
	attrs := []slog.Attr{
		slog.String("method", r.Method),
		slog.String("path", r.URL.Path),
		slog.Int("status", status),
		slog.Int("response_bytes", bytes),
		slog.Float64("duration_ms", float64(duration.Microseconds())/1000),
		slog.String("request_id", requestID),
		slog.String("correlation_id", correlationID),
	}
	if state, ok := ctx.Value(requestStateKey{}).(*requestState); ok {
		if state.ActorType != "" {
			attrs = append(attrs, slog.String("actor_type", state.ActorType))
		}
		if state.ActorID != "" {
			attrs = append(attrs, slog.String("actor_id", state.ActorID))
		}
		if state.MerchantID != "" {
			attrs = append(attrs, slog.String("merchant_id", state.MerchantID))
		}
	}
	spanContext := trace.SpanContextFromContext(ctx)
	if spanContext.HasTraceID() {
		attrs = append(attrs, slog.String("trace_id", spanContext.TraceID().String()))
	}
	if spanContext.HasSpanID() {
		attrs = append(attrs, slog.String("span_id", spanContext.SpanID().String()))
	}
	s.logger.LogAttrs(ctx, level, "http_request", attrs...)
}

type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (r *statusRecorder) WriteHeader(status int) {
	if r.status != 0 {
		return
	}
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(body []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	n, err := r.ResponseWriter.Write(body)
	r.bytes += n
	return n, err
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

func routeSpanName(r *http.Request) string {
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/healthz":
		return "GET /healthz"
	case r.Method == http.MethodGet && r.URL.Path == "/readyz":
		return "GET /readyz"
	case r.Method == http.MethodGet && r.URL.Path == "/metrics":
		return "GET /metrics"
	case r.Method == http.MethodPost && r.URL.Path == "/api/v1/orders":
		return "POST /api/v1/orders"
	case strings.HasPrefix(r.URL.Path, "/api/v1/orders/") && strings.HasSuffix(r.URL.Path, "/cancel"):
		return r.Method + " /api/v1/orders/{orderId}/cancel"
	case strings.HasPrefix(r.URL.Path, "/api/v1/orders/"):
		return r.Method + " /api/v1/orders/{orderId}"
	case strings.HasPrefix(r.URL.Path, "/api/v1/shipments/"):
		return r.Method + " /api/v1/shipments/{shipmentId}"
	default:
		return r.Method + " " + r.URL.Path
	}
}

func recordActor(ctx context.Context, act actor) {
	state, ok := ctx.Value(requestStateKey{}).(*requestState)
	if !ok || state == nil {
		return
	}
	if act.Ops {
		state.ActorType = "ops"
		state.ActorID = act.ActorID
		return
	}
	state.ActorType = "merchant"
	state.ActorID = act.MerchantID
	state.MerchantID = act.MerchantID
}
