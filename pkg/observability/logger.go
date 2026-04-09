package observability

import (
	"context"

	"go.opentelemetry.io/otel/trace"
	"github.com/go-logr/logr"
)

// TraceLogger returns a logger augmented with trace_id and span_id fields
// when tracing is enabled and a valid span exists in the context.
// When tracing is disabled or no span is present, the original logger is returned unchanged.
//
// Usage:
//
//	logger := observability.TraceLogger(ctx, log.FromContext(ctx))
func TraceLogger(ctx context.Context, logger logr.Logger) logr.Logger {
	if !tracerEnabled {
		return logger
	}
	span := trace.SpanFromContext(ctx)
	if !span.SpanContext().IsValid() {
		return logger
	}
	return logger.WithValues(
		"trace_id", span.SpanContext().TraceID().String(),
		"span_id", span.SpanContext().SpanID().String(),
	)
}

// SpanError records an error on the current span from context (if tracing is enabled).
// It also sets the span status to Error.
func SpanError(ctx context.Context, err error) {
	if !tracerEnabled {
		return
	}
	span := trace.SpanFromContext(ctx)
	if span.SpanContext().IsValid() {
		span.RecordError(err)
		span.SetStatus(1, err.Error()) // StatusError = 1
	}
}
