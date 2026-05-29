package observability

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace/noop"
)

func TestConfigureTracingDefaultsToDisabled(t *testing.T) {
	resetOpenTelemetry(t)

	shutdown, err := ConfigureTracing(context.Background(), "fulfillhub-test", emptyEnv, nil)
	if err != nil {
		t.Fatalf("ConfigureTracing returned error: %v", err)
	}
	if shutdown == nil {
		t.Fatal("shutdown must be non-nil")
	}
	if err := shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown returned error: %v", err)
	}
}

func TestConfigureTracingConfiguresStdoutExporter(t *testing.T) {
	resetOpenTelemetry(t)

	shutdown, err := ConfigureTracing(context.Background(), "fulfillhub-test", mapEnv(map[string]string{
		"OTEL_TRACES_EXPORTER": "stdout",
		"OTEL_SERVICE_NAME":    "fulfillhub-custom",
	}), slog.Default())
	if err != nil {
		t.Fatalf("ConfigureTracing returned error: %v", err)
	}
	if err := shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown returned error: %v", err)
	}
}

func TestConfigureTracingConfiguresOTLPHTTPExporter(t *testing.T) {
	resetOpenTelemetry(t)

	shutdown, err := ConfigureTracing(context.Background(), "fulfillhub-test", mapEnv(map[string]string{
		"OTEL_TRACES_EXPORTER":               "otlp",
		"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT": "http://127.0.0.1:4318/v1/traces",
	}), nil)
	if err != nil {
		t.Fatalf("ConfigureTracing returned error: %v", err)
	}
	if err := shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown returned error: %v", err)
	}
}

func TestConfigureTracingRejectsUnsupportedExporter(t *testing.T) {
	resetOpenTelemetry(t)

	_, err := ConfigureTracing(context.Background(), "fulfillhub-test", mapEnv(map[string]string{
		"OTEL_TRACES_EXPORTER": "zipkin",
	}), nil)
	if err == nil {
		t.Fatal("expected unsupported exporter error")
	}
	if !strings.Contains(err.Error(), "OTEL_TRACES_EXPORTER") {
		t.Fatalf("error = %q, want OTEL_TRACES_EXPORTER context", err.Error())
	}
}

func TestConfigureTracingInstallsTraceContextAndBaggagePropagators(t *testing.T) {
	resetOpenTelemetry(t)

	_, err := ConfigureTracing(context.Background(), "fulfillhub-test", emptyEnv, nil)
	if err != nil {
		t.Fatalf("ConfigureTracing returned error: %v", err)
	}

	fields := otel.GetTextMapPropagator().Fields()
	if !contains(fields, "traceparent") {
		t.Fatalf("propagator fields = %v, want traceparent", fields)
	}
	if !contains(fields, "baggage") {
		t.Fatalf("propagator fields = %v, want baggage", fields)
	}
}

func resetOpenTelemetry(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		otel.SetTracerProvider(noop.NewTracerProvider())
		otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator())
	})
}

func emptyEnv(string) string {
	return ""
}

func mapEnv(values map[string]string) func(string) string {
	return func(key string) string {
		return values[key]
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
