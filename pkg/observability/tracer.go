package observability

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
)

// tracer stores the global tracer after initialization.
var tracer trace.Tracer

// IsTracingEnabled returns true if a real (non-noop) TracerProvider is configured.
func IsTracingEnabled() bool {
	return tracerEnabled
}

var tracerEnabled bool

// InitTracer sets up the global TracerProvider.
// When cfg.OTLPEndpoint is empty, a no-op provider is used and tracing is effectively disabled.
func InitTracer(cfg Config) error {
	if cfg.OTLPEndpoint == "" {
		otel.SetTracerProvider(trace.NewNoopTracerProvider())
		otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator())
		tracerEnabled = false
		return nil
	}

	ctx := context.Background()

	var exporter *otlptrace.Exporter
	var err error

	opts := []otlptracegrpc.Option{
		otlptracegrpc.WithEndpoint(cfg.OTLPEndpoint),
	}
	if cfg.OTLPInsecure {
		opts = append(opts, otlptracegrpc.WithInsecure())
	}
	exporter, err = otlptracegrpc.New(ctx, opts...)
	if err != nil {
		return fmt.Errorf("creating OTLP trace exporter: %w", err)
	}

	sampleRate := cfg.SampleRate
	if sampleRate <= 0 {
		sampleRate = 1.0
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceName(cfg.ServiceName),
		)),
		sdktrace.WithSampler(sdktrace.TraceIDRatioBased(sampleRate)),
	)

	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	tracer = tp.Tracer(cfg.ServiceName)
	tracerEnabled = true
	return nil
}

// Tracer returns the global tracer for this service.
// If tracing is disabled, it returns a no-op tracer.
func Tracer() trace.Tracer {
	if tracer == nil {
		return otel.Tracer("")
	}
	return tracer
}

// ShutdownTracer flushes and shuts down the TracerProvider.
func ShutdownTracer(ctx context.Context) error {
	if tp, ok := otel.GetTracerProvider().(*sdktrace.TracerProvider); ok {
		return tp.Shutdown(ctx)
	}
	return nil
}
