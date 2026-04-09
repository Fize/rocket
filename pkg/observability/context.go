package observability

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// SpanFromContext returns the current span from the context.
// Returns nil if tracing is disabled or no span exists.
func SpanFromContext(ctx context.Context) trace.Span {
	if !tracerEnabled {
		return nil
	}
	return trace.SpanFromContext(ctx)
}

// TraceIDFromContext extracts the trace ID from the context.
// Returns an empty string if no valid span exists.
func TraceIDFromContext(ctx context.Context) string {
	span := trace.SpanFromContext(ctx)
	if sc := span.SpanContext(); sc.IsValid() {
		return sc.TraceID().String()
	}
	return ""
}

// SpanIDFromContext extracts the span ID from the context.
// Returns an empty string if no valid span exists.
func SpanIDFromContext(ctx context.Context) string {
	span := trace.SpanFromContext(ctx)
	if sc := span.SpanContext(); sc.IsValid() {
		return sc.SpanID().String()
	}
	return ""
}

// InjectPropagation injects trace propagation headers into a carrier (e.g. HTTP headers).
// This is useful for propagating trace context across service boundaries.
func InjectPropagation(ctx context.Context, carrier propagation.TextMapCarrier) {
	otel.GetTextMapPropagator().Inject(ctx, carrier)
}

// ExtractPropagation extracts trace propagation headers from a carrier.
// Returns a new context with the extracted trace context.
func ExtractPropagation(ctx context.Context, carrier propagation.TextMapCarrier) context.Context {
	return otel.GetTextMapPropagator().Extract(ctx, carrier)
}
