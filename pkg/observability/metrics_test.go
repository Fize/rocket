package observability

import (
	"context"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
)

func TestInitMeterProvider(t *testing.T) {
	cfg := Config{
		ServiceName: "test-service",
	}
	registry := prometheus.NewRegistry()

	err := InitMeterProvider(cfg, registry)
	assert.NoError(t, err)
}

func TestMeterReturnsNonNil(t *testing.T) {
	cfg := Config{
		ServiceName: "test-service",
	}
	registry := prometheus.NewRegistry()

	InitMeterProvider(cfg, registry)

	meter := Meter("test-instrumentation")
	assert.NotNil(t, meter)
}

func TestMeterMultipleInstruments(t *testing.T) {
	cfg := Config{
		ServiceName: "test-service",
	}
	registry := prometheus.NewRegistry()

	InitMeterProvider(cfg, registry)

	// Should be able to create multiple meters
	meter1 := Meter("test-instrumentation-1")
	meter2 := Meter("test-instrumentation-2")

	assert.NotNil(t, meter1)
	assert.NotNil(t, meter2)
}

func TestShutdownMeterProvider(t *testing.T) {
	cfg := Config{
		ServiceName: "test-service",
	}
	registry := prometheus.NewRegistry()

	InitMeterProvider(cfg, registry)

	err := ShutdownMeterProvider(context.Background())
	assert.NoError(t, err)
}

func TestShutdownMeterProviderNil(t *testing.T) {
	// Reset meterProvider for this test
	meterProvider = nil

	err := ShutdownMeterProvider(context.Background())
	assert.NoError(t, err)
}

func TestInitMeterProviderTwice(t *testing.T) {
	cfg := Config{
		ServiceName: "test-service",
	}
	registry := prometheus.NewRegistry()

	// First initialization
	err1 := InitMeterProvider(cfg, registry)
	assert.NoError(t, err1)

	// Second initialization should also work (overwriting previous)
	err2 := InitMeterProvider(cfg, registry)
	assert.NoError(t, err2)
}
