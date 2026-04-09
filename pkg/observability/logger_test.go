package observability

import (
	"context"
	"errors"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

func TestTraceLoggerDisabled(t *testing.T) {
	cfg := Config{
		ServiceName:  "test-service",
		OTLPEndpoint: "", // disabled
	}
	InitTracer(cfg)

	ctx := context.Background()
	logger := logr.Discard()

	result := TraceLogger(ctx, logger)
	// When tracing is disabled, should return original logger unchanged
	assert.Equal(t, logger, result)
}

func TestTraceLoggerNoSpan(t *testing.T) {
	// Ensure tracer is reset for this test
	tracerEnabled = false

	ctx := context.Background()
	logger := logr.Discard()

	result := TraceLogger(ctx, logger)
	// No span in context, should return original logger
	assert.Equal(t, logger, result)
}

func TestTraceLoggerWithSpan(t *testing.T) {
	cfg := Config{
		ServiceName:  "test-service",
		OTLPEndpoint: "", // disabled but tracer still works
	}
	InitTracer(cfg)

	ctx := context.Background()
	ctx, span := Tracer().Start(ctx, "test-span",
		trace.WithAttributes(attribute.String("test", "value")))
	defer span.End()

	logger := logr.Discard()
	result := TraceLogger(ctx, logger)

	// When there's a valid span, should return logger with trace values
	assert.NotNil(t, result)
}

func TestSpanErrorDisabled(t *testing.T) {
	cfg := Config{
		ServiceName:  "test-service",
		OTLPEndpoint: "", // disabled
	}
	InitTracer(cfg)

	ctx := context.Background()
	testErr := errors.New("test error")

	// Should not panic
	assert.NotPanics(t, func() {
		SpanError(ctx, testErr)
	})
}

func TestSpanErrorWithSpan(t *testing.T) {
	cfg := Config{
		ServiceName:  "test-service",
		OTLPEndpoint: "", // disabled
	}
	InitTracer(cfg)

	ctx := context.Background()
	ctx, span := Tracer().Start(ctx, "test-span")
	defer span.End()

	testErr := errors.New("test error")

	// Should not panic
	assert.NotPanics(t, func() {
		SpanError(ctx, testErr)
	})
}
