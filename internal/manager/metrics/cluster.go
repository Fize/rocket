package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	// ClusterConnectionState records the current state of each cluster (1=online, 0=offline).
	ClusterConnectionState = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "rocket",
			Subsystem: "cluster",
			Name:      "connection_state",
			Help:      "Current connection state of managed clusters (1=Ready, 0 otherwise).",
		},
		[]string{"cluster"},
	)

	// HeartbeatLatency records the time since last heartbeat for each cluster.
	HeartbeatLatency = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "rocket",
			Subsystem: "cluster",
			Name:      "heartbeat_latency_seconds",
			Help:      "Seconds since last heartbeat received from each cluster.",
		},
		[]string{"cluster"},
	)

	// TunnelConnections records the current number of active tunnel connections.
	TunnelConnections = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "rocket",
			Subsystem: "tunnel",
			Name:      "connections",
			Help:      "Current number of active tunnel connections from edge clusters.",
		},
	)

	// TunnelReconnectTotal counts tunnel reconnection attempts.
	TunnelReconnectTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "rocket",
			Subsystem: "tunnel",
			Name:      "reconnect_total",
			Help:      "Total number of tunnel reconnection attempts.",
		},
		[]string{"cluster", "result"},
	)
)

var (
	// ManagedClusterTotal records the total number of managed clusters.
	ManagedClusterTotal = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "rocket",
			Subsystem: "cluster",
			Name:      "managed_total",
			Help:      "Total number of managed clusters.",
		},
	)
)

func init() {
	metrics.Registry.MustRegister(
		ClusterConnectionState,
		HeartbeatLatency,
		TunnelConnections,
		TunnelReconnectTotal,
		ManagedClusterTotal,
	)
}

// SetClusterConnectionState sets the connection state gauge for a cluster.
func SetClusterConnectionState(cluster string, ready bool) {
	v := 0.0
	if ready {
		v = 1.0
	}
	ClusterConnectionState.WithLabelValues(cluster).Set(v)
}

// SetHeartbeatLatency records the heartbeat latency for a cluster.
func SetHeartbeatLatency(cluster string, d time.Duration) {
	HeartbeatLatency.WithLabelValues(cluster).Set(d.Seconds())
}

// IncrTunnelConnections increments/decrements the active tunnel connection count.
func IncrTunnelConnections(delta float64) {
	TunnelConnections.Add(delta)
}

// RecordTunnelReconnect records a tunnel reconnection attempt.
func RecordTunnelReconnect(cluster, result string) {
	TunnelReconnectTotal.WithLabelValues(cluster, result).Inc()
}

// RemoveClusterMetrics removes all metrics for a deleted cluster.
func RemoveClusterMetrics(cluster string) {
	ClusterConnectionState.DeleteLabelValues(cluster)
	HeartbeatLatency.DeleteLabelValues(cluster)
	TunnelReconnectTotal.DeletePartialMatch(prometheus.Labels{"cluster": cluster})
}

// SetManagedClusterTotal sets the total number of managed clusters.
func SetManagedClusterTotal(count int) {
	ManagedClusterTotal.Set(float64(count))
}
