package metrics

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
)

func TestSetClusterConnectionState(t *testing.T) {
	tests := []struct {
		name    string
		cluster string
		ready   bool
	}{
		{
			name:    "cluster online",
			cluster: "edge-1",
			ready:   true,
		},
		{
			name:    "cluster offline",
			cluster: "edge-2",
			ready:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.NotPanics(t, func() {
				SetClusterConnectionState(tt.cluster, tt.ready)
			})
		})
	}
}

func TestSetHeartbeatLatency(t *testing.T) {
	tests := []struct {
		name    string
		cluster string
		latency time.Duration
	}{
		{
			name:    "normal latency",
			cluster: "edge-1",
			latency: 100 * time.Millisecond,
		},
		{
			name:    "high latency",
			cluster: "edge-2",
			latency: 5 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.NotPanics(t, func() {
				SetHeartbeatLatency(tt.cluster, tt.latency)
			})
		})
	}
}

func TestIncrTunnelConnections(t *testing.T) {
	tests := []struct {
		name  string
		delta float64
	}{
		{
			name:  "increment",
			delta: 1.0,
		},
		{
			name:  "decrement",
			delta: -1.0,
		},
		{
			name:  "zero delta",
			delta: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.NotPanics(t, func() {
				IncrTunnelConnections(tt.delta)
			})
		})
	}
}

func TestRecordTunnelReconnect(t *testing.T) {
	tests := []struct {
		name    string
		cluster string
		result  string
	}{
		{
			name:    "successful reconnect",
			cluster: "edge-1",
			result:  "success",
		},
		{
			name:    "failed reconnect",
			cluster: "edge-1",
			result:  "error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.NotPanics(t, func() {
				RecordTunnelReconnect(tt.cluster, tt.result)
			})
		})
	}
}

func TestRemoveClusterMetrics(t *testing.T) {
	// First set some metrics
	SetClusterConnectionState("test-cluster", true)
	SetHeartbeatLatency("test-cluster", 100*time.Millisecond)
	RecordTunnelReconnect("test-cluster", "success")

	// Should not panic when removing
	assert.NotPanics(t, func() {
		RemoveClusterMetrics("test-cluster")
	})
}

func TestSetManagedClusterTotal(t *testing.T) {
	tests := []struct {
		name  string
		count int
	}{
		{
			name:  "zero clusters",
			count: 0,
		},
		{
			name:  "single cluster",
			count: 1,
		},
		{
			name:  "multiple clusters",
			count: 50,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.NotPanics(t, func() {
				SetManagedClusterTotal(tt.count)
			})
		})
	}
}

func TestClusterMetricsRegistered(t *testing.T) {
	ch := make(chan *prometheus.Desc, 10)

	go func() {
		ClusterConnectionState.Describe(ch)
		HeartbeatLatency.Describe(ch)
		TunnelConnections.Describe(ch)
		TunnelReconnectTotal.Describe(ch)
		ManagedClusterTotal.Describe(ch)
		close(ch)
	}()

	count := 0
	for range ch {
		count++
	}

	assert.Equal(t, 5, count, "all 5 cluster metrics should be registered and describable")
}
