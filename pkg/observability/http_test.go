package observability

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewHTTPHandlerDisabled(t *testing.T) {
	cfg := Config{
		ServiceName:  "test-service",
		OTLPEndpoint: "", // disabled
	}
	InitTracer(cfg)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// When tracing is disabled, should return original handler (wrapped in noop)
	wrapped := NewHTTPHandler(handler, "test-handler")
	// Just verify it doesn't panic and returns something
	assert.NotNil(t, wrapped)
}

func TestNewHTTPHandlerFuncDisabled(t *testing.T) {
	cfg := Config{
		ServiceName:  "test-service",
		OTLPEndpoint: "", // disabled
	}
	InitTracer(cfg)

	handlerFunc := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// When tracing is disabled, should return original handler func
	wrapped := NewHTTPHandlerFunc(handlerFunc, "test-handler-func")
	// Just verify it doesn't panic and returns something
	assert.NotNil(t, wrapped)
}

func TestNewHTTPHandlerEnabled(t *testing.T) {
	// Don't actually connect, just set tracerEnabled
	tracerEnabled = true

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrapped := NewHTTPHandler(handler, "test-handler")
	// When tracing is enabled, should return a different handler
	assert.NotNil(t, wrapped)
	// Should be able to serve a request
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestNewHTTPHandlerFuncEnabled(t *testing.T) {
	// Don't actually connect, just set tracerEnabled
	tracerEnabled = true

	handlerFunc := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrapped := NewHTTPHandlerFunc(handlerFunc, "test-handler-func")
	// When tracing is enabled, should return a different handler func
	assert.NotNil(t, wrapped)
	// Should be able to serve a request
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestNewHTTPHandlerServesRequest(t *testing.T) {
	// Reset tracerEnabled for this test
	tracerEnabled = false

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrapped := NewHTTPHandler(handler, "test-handler")

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	wrapped.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}
