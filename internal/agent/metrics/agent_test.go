package metrics

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
)

func TestRecordHeartbeat(t *testing.T) {
	tests := []struct {
		name     string
		result   string
		latency  time.Duration
	}{
		{
			name:    "successful heartbeat",
			result:  "success",
			latency: 50 * time.Millisecond,
		},
		{
			name:    "failed heartbeat",
			result:  "error",
			latency: 100 * time.Millisecond,
		},
		{
			name:    "timeout heartbeat",
			result:  "timeout",
			latency: 5 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.NotPanics(t, func() {
				RecordHeartbeat(tt.result, tt.latency)
			})
		})
	}
}

func TestSetTunnelConnected(t *testing.T) {
	tests := []struct {
		name      string
		connected bool
	}{
		{
			name:      "connected",
			connected: true,
		},
		{
			name:      "disconnected",
			connected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.NotPanics(t, func() {
				SetTunnelConnected(tt.connected)
			})
		})
	}
}

func TestRecordTunnelReconnect(t *testing.T) {
	tests := []struct {
		name   string
		result string
	}{
		{
			name:   "successful reconnect",
			result: "success",
		},
		{
			name:   "failed reconnect",
			result: "error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.NotPanics(t, func() {
				RecordTunnelReconnect(tt.result)
			})
		})
	}
}

func TestAgentMetricsRegistered(t *testing.T) {
	ch := make(chan *prometheus.Desc, 10)

	go func() {
		HeartbeatTotal.Describe(ch)
		HeartbeatLatency.Describe(ch)
		TunnelConnected.Describe(ch)
		TunnelReconnectTotal.Describe(ch)
		close(ch)
	}()

	count := 0
	for range ch {
		count++
	}

	assert.Equal(t, 4, count, "all 4 agent metrics should be registered and describable")
}
