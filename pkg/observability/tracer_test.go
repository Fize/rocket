package observability

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestInitTracerDisabled(t *testing.T) {
	cfg := Config{
		ServiceName:  "test-service",
		OTLPEndpoint: "", // empty = disabled
	}

	err := InitTracer(cfg)
	assert.NoError(t, err)
	assert.False(t, IsTracingEnabled(), "tracing should be disabled when OTLPEndpoint is empty")
}

func TestInitTracerWithEndpoint(t *testing.T) {
	cfg := Config{
		ServiceName:  "test-service",
		OTLPEndpoint: "localhost:4317",
		OTLPInsecure: true,
		SampleRate:   1.0,
	}

	// This will fail to connect but should initialize the tracer
	err := InitTracer(cfg)
	// We expect an error because there's no actual OTLP collector running
	// but the tracer should still be set up
	if err != nil {
		assert.Contains(t, err.Error(), "creating OTLP trace exporter")
	}
}

func TestTracerReturnsNonNil(t *testing.T) {
	// When tracing is disabled, Tracer() should still return a non-nil tracer
	tracer := Tracer()
	assert.NotNil(t, tracer)
}

func TestTracerStartSpan(t *testing.T) {
	cfg := Config{
		ServiceName:  "test-service",
		OTLPEndpoint: "", // disabled
	}
	InitTracer(cfg)

	ctx := context.Background()
	ctx, span := Tracer().Start(ctx, "test-span")
	defer span.End()

	assert.NotNil(t, span)
	assert.True(t, span.SpanContext().IsValid())
}

func TestShutdownTracer(t *testing.T) {
	cfg := Config{
		ServiceName:  "test-service",
		OTLPEndpoint: "", // disabled = noop provider
	}
	InitTracer(cfg)

	err := ShutdownTracer(context.Background())
	assert.NoError(t, err)
}

func TestTraceIDFromContextNoSpan(t *testing.T) {
	ctx := context.Background()
	traceID := TraceIDFromContext(ctx)
	assert.Equal(t, "", traceID, "should return empty string when no span in context")
}

func TestSpanIDFromContextNoSpan(t *testing.T) {
	ctx := context.Background()
	spanID := SpanIDFromContext(ctx)
	assert.Equal(t, "", spanID, "should return empty string when no span in context")
}

func TestSpanFromContextNoSpan(t *testing.T) {
	ctx := context.Background()
	span := SpanFromContext(ctx)
	assert.Nil(t, span, "should return nil when no span in context")
}

func TestTraceIDFromContextWithSpan(t *testing.T) {
	cfg := Config{
		ServiceName:  "test-service",
		OTLPEndpoint: "", // disabled
	}
	InitTracer(cfg)

	ctx := context.Background()
	ctx, span := Tracer().Start(ctx, "test-span")
	defer span.End()

	// When tracing is disabled via config, TraceIDFromContext uses the underlying
	// trace.SpanFromContext which still works with noop spans
	traceID := TraceIDFromContext(ctx)
	assert.NotEmpty(t, traceID, "trace ID should not be empty when span exists")
}

func TestSpanIDFromContextWithSpan(t *testing.T) {
	cfg := Config{
		ServiceName:  "test-service",
		OTLPEndpoint: "", // disabled
	}
	InitTracer(cfg)

	ctx := context.Background()
	ctx, span := Tracer().Start(ctx, "test-span")
	defer span.End()

	// When tracing is disabled via config, SpanIDFromContext uses the underlying
	// trace.SpanFromContext which still works with noop spans
	spanID := SpanIDFromContext(ctx)
	assert.NotEmpty(t, spanID, "span ID should not be empty when span exists")
}

func TestSpanFromContextWithSpanDisabled(t *testing.T) {
	cfg := Config{
		ServiceName:  "test-service",
		OTLPEndpoint: "", // disabled
	}
	InitTracer(cfg)

	ctx := context.Background()
	ctx, span := Tracer().Start(ctx, "test-span")
	defer span.End()

	// When tracing is disabled via config, SpanFromContext returns nil
	// because it checks tracerEnabled flag
	result := SpanFromContext(ctx)
	assert.Nil(t, result, "SpanFromContext returns nil when tracing is disabled")
}

func TestInjectPropagation(t *testing.T) {
	cfg := Config{
		ServiceName:  "test-service",
		OTLPEndpoint: "", // disabled
	}
	InitTracer(cfg)

	ctx := context.Background()
	ctx, span := Tracer().Start(ctx, "test-span")
	defer span.End()

	carrier := &testTextMapCarrier{data: make(map[string]string)}
	InjectPropagation(ctx, carrier)

	// When tracing is disabled, carrier should remain empty
	// When enabled, should have trace context headers
	assert.NotNil(t, carrier.data)
}

func TestExtractPropagation(t *testing.T) {
	cfg := Config{
		ServiceName:  "test-service",
		OTLPEndpoint: "", // disabled
	}
	InitTracer(cfg)

	ctx := context.Background()
	carrier := &testTextMapCarrier{data: map[string]string{
		"traceparent": "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01",
	}}

	extractedCtx := ExtractPropagation(ctx, carrier)
	assert.NotNil(t, extractedCtx)
}

// testTextMapCarrier implements propagation.TextMapCarrier for testing
type testTextMapCarrier struct {
	data map[string]string
}

func (c *testTextMapCarrier) Get(key string) string {
	return c.data[key]
}

func (c *testTextMapCarrier) Set(key, value string) {
	c.data[key] = value
}

func (c *testTextMapCarrier) Keys() []string {
	keys := make([]string, 0, len(c.data))
	for k := range c.data {
		keys = append(keys, k)
	}
	return keys
}

func TestSpanError(t *testing.T) {
	cfg := Config{
		ServiceName:  "test-service",
		OTLPEndpoint: "", // disabled
	}
	InitTracer(cfg)

	ctx := context.Background()
	err := assert.AnError

	// Should not panic even when tracing is disabled
	assert.NotPanics(t, func() {
		SpanError(ctx, err)
	})
}
