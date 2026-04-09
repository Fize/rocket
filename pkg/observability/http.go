package observability

import (
	"net/http"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// NewHTTPHandler wraps an http.Handler with OpenTelemetry tracing middleware.
// When tracing is disabled, the original handler is returned unchanged.
//
// Usage:
//
//	handler := observability.NewHTTPHandler(myHandler, "my-service")
func NewHTTPHandler(handler http.Handler, name string) http.Handler {
	if !tracerEnabled {
		return handler
	}
	return otelhttp.NewHandler(handler, name)
}

// NewHTTPHandlerFunc wraps an http.HandlerFunc with OpenTelemetry tracing middleware.
// When tracing is disabled, the original handler is returned unchanged.
func NewHTTPHandlerFunc(handlerFunc http.HandlerFunc, name string) http.HandlerFunc {
	if !tracerEnabled {
		return handlerFunc
	}
	return otelhttp.NewHandler(handlerFunc, name).ServeHTTP
}
