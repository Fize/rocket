package observability

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTracerStartWithAttributes(t *testing.T) {
	cfg := Config{
		ServiceName:  "test-service",
		OTLPEndpoint: "",
	}
	InitTracer(cfg)

	ctx := context.Background()
	ctx, span := Tracer().Start(ctx, "test-span")
	defer span.End()

	// Even with noop tracer, span should not be nil
	assert.NotNil(t, span)
}

func TestTracerNestedSpans(t *testing.T) {
	cfg := Config{
		ServiceName:  "test-service",
		OTLPEndpoint: "",
	}
	InitTracer(cfg)

	ctx := context.Background()
	ctx, parentSpan := Tracer().Start(ctx, "parent-span")
	defer parentSpan.End()

	ctx, childSpan := Tracer().Start(ctx, "child-span")
	defer childSpan.End()

	// Both spans should exist
	assert.NotNil(t, parentSpan)
	assert.NotNil(t, childSpan)
}

func TestSpanEnd(t *testing.T) {
	cfg := Config{
		ServiceName:  "test-service",
		OTLPEndpoint: "",
	}
	InitTracer(cfg)

	ctx := context.Background()
	_, span := Tracer().Start(ctx, "test-span")

	// End the span - should not panic
	assert.NotPanics(t, func() {
		span.End()
	})
}
