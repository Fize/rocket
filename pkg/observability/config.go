package observability

// Config holds the configuration for observability (tracing) setup.
type Config struct {
	// ServiceName is the name of this service, e.g. "rocket-manager" or "rocket-agent".
	ServiceName string

	// OTLPEndpoint is the address of the OTLP exporter (e.g. "otel-collector:4317").
	// When empty, tracing is disabled (noop TracerProvider).
	OTLPEndpoint string

	// OTLPInsecure determines whether to use insecure connection for OTLP gRPC.
	OTLPInsecure bool

	// SampleRate is the trace sampling probability (0.0 - 1.0). Default: 1.0 when OTLP is configured.
	SampleRate float64
}
