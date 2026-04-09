package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	// HeartbeatTotal counts agent heartbeat attempts.
	HeartbeatTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "rocket",
			Subsystem: "agent",
			Name:      "heartbeat_total",
			Help:      "Total number of agent heartbeat send attempts.",
		},
		[]string{"result"},
	)

	// HeartbeatLatency records the round-trip latency of heartbeat requests.
	HeartbeatLatency = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Namespace: "rocket",
			Subsystem: "agent",
			Name:      "heartbeat_latency_seconds",
			Help:      "Round-trip latency of heartbeat requests in seconds.",
			Buckets:   []float64{0.01, 0.05, 0.1, 0.5, 1, 2, 5},
		},
	)

	// TunnelConnected records whether the agent's tunnel connection is active (1=connected, 0=disconnected).
	TunnelConnected = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "rocket",
			Subsystem: "agent",
			Name:      "tunnel_connected",
			Help:      "Whether the agent tunnel connection is active (1=connected, 0=disconnected).",
		},
	)

	// TunnelReconnectTotal counts agent tunnel reconnection attempts.
	TunnelReconnectTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "rocket",
			Subsystem: "agent",
			Name:      "tunnel_reconnect_total",
			Help:      "Total number of agent tunnel reconnection attempts.",
		},
		[]string{"result"},
	)
)

func init() {
	metrics.Registry.MustRegister(
		HeartbeatTotal,
		HeartbeatLatency,
		TunnelConnected,
		TunnelReconnectTotal,
	)
}

// RecordHeartbeat records a heartbeat send attempt.
func RecordHeartbeat(result string, latency time.Duration) {
	HeartbeatTotal.WithLabelValues(result).Inc()
	HeartbeatLatency.Observe(latency.Seconds())
}

// SetTunnelConnected sets the tunnel connection gauge.
func SetTunnelConnected(connected bool) {
	v := 0.0
	if connected {
		v = 1.0
	}
	TunnelConnected.Set(v)
}

// RecordTunnelReconnect records a tunnel reconnection attempt.
func RecordTunnelReconnect(result string) {
	TunnelReconnectTotal.WithLabelValues(result).Inc()
}
