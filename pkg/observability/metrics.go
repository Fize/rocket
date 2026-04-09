package observability

import (
	"context"
	"fmt"

	promclient "github.com/prometheus/client_golang/prometheus"
	otelprom "go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

var meterProvider *sdkmetric.MeterProvider

// Meter returns the global Meter for this service.
// If the MeterProvider has not been initialized, a no-op Meter is returned.
func Meter(instrumentationName string, opts ...metric.MeterOption) metric.Meter {
	if meterProvider == nil {
		return noopMeterProvider.Meter(instrumentationName, opts...)
	}
	return meterProvider.Meter(instrumentationName, opts...)
}

// noopMeterProvider provides a no-op MeterProvider for when metrics are not initialized.
var noopMeterProvider = sdkmetric.NewMeterProvider()

// InitMeterProvider initializes the global MeterProvider with a Prometheus exporter.
// The Prometheus exporter serves metrics on the existing controller-runtime /metrics endpoint
// by sharing the same prometheus.Registry.
//
// When OTLP metric export is needed in the future, an additional OTLP metric exporter
// can be added as a reader alongside the Prometheus exporter.
func InitMeterProvider(cfg Config, registry promclient.Registerer) error {
	exporter, err := otelprom.New(otelprom.WithRegisterer(registry))
	if err != nil {
		return fmt.Errorf("creating Prometheus metric exporter: %w", err)
	}

	meterProvider = sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(exporter),
		sdkmetric.WithResource(resourceFromConfig(cfg)),
	)

	return nil
}

// ShutdownMeterProvider flushes and shuts down the MeterProvider.
func ShutdownMeterProvider(ctx context.Context) error {
	if meterProvider != nil {
		return meterProvider.Shutdown(ctx)
	}
	return nil
}

// resourceFromConfig creates an OTel Resource from the observability config.
func resourceFromConfig(cfg Config) *resource.Resource {
	return resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceName(cfg.ServiceName),
	)
}
