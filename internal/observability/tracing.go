package observability

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

// ConfigureTracing wires the process-wide OpenTelemetry provider from standard
// environment variables while keeping tracing disabled by default for tests.
func ConfigureTracing(ctx context.Context, defaultServiceName string, getenv func(string) string, logger *slog.Logger) (func(context.Context) error, error) {
	if getenv == nil {
		return nil, fmt.Errorf("getenv is required")
	}

	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	exporterName := strings.ToLower(strings.TrimSpace(getenv("OTEL_TRACES_EXPORTER")))
	if exporterName == "" || exporterName == "none" {
		otel.SetTracerProvider(noop.NewTracerProvider())
		return func(context.Context) error { return nil }, nil
	}

	serviceName := strings.TrimSpace(getenv("OTEL_SERVICE_NAME"))
	if serviceName == "" {
		serviceName = defaultServiceName
	}
	if serviceName == "" {
		return nil, fmt.Errorf("service name is required")
	}

	exporter, err := newTraceExporter(ctx, exporterName)
	if err != nil {
		return nil, err
	}

	provider := sdktrace.NewTracerProvider(
		sdktrace.WithResource(resource.NewWithAttributes(
			"",
			attribute.String("service.name", serviceName),
		)),
		sdktrace.WithBatcher(exporter),
	)
	otel.SetTracerProvider(provider)
	if logger != nil {
		logger.Info("otel tracing enabled", "exporter", exporterName, "service_name", serviceName)
	}
	return provider.Shutdown, nil
}

func newTraceExporter(ctx context.Context, exporterName string) (sdktrace.SpanExporter, error) {
	switch exporterName {
	case "stdout":
		return stdouttrace.New(stdouttrace.WithPrettyPrint())
	case "otlp", "otlphttp":
		return otlptracehttp.New(ctx)
	default:
		return nil, fmt.Errorf("unsupported OTEL_TRACES_EXPORTER %q; supported values are none, stdout, and otlp", exporterName)
	}
}
