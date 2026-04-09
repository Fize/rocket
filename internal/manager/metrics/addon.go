package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	// AddonReconcileTotal counts addon reconcile operations.
	AddonReconcileTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "rocket",
			Subsystem: "addon",
			Name:      "reconcile_total",
			Help:      "Total number of addon reconcile operations.",
		},
		[]string{"name", "result"},
	)

	// AddonReconcileDuration records addon reconcile duration.
	AddonReconcileDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "rocket",
			Subsystem: "addon",
			Name:      "reconcile_duration_seconds",
			Help:      "Duration of addon reconcile operations in seconds.",
			Buckets:   []float64{0.1, 0.5, 1, 2, 5, 10, 30, 60, 120, 300},
		},
		[]string{"name"},
	)

	// AddonHelmOperationTotal counts Helm operations for addons.
	AddonHelmOperationTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "rocket",
			Subsystem: "addon",
			Name:      "helm_operation_total",
			Help:      "Total number of Helm operations for addons.",
		},
		[]string{"name", "operation", "result"},
	)

	// AddonHelmOperationDuration records Helm operation duration.
	AddonHelmOperationDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "rocket",
			Subsystem: "addon",
			Name:      "helm_operation_duration_seconds",
			Help:      "Duration of Helm operations for addons in seconds.",
			Buckets:   []float64{0.5, 1, 2, 5, 10, 30, 60, 120, 300},
		},
		[]string{"name", "operation"},
	)
)

func init() {
	metrics.Registry.MustRegister(
		AddonReconcileTotal,
		AddonReconcileDuration,
		AddonHelmOperationTotal,
		AddonHelmOperationDuration,
	)
}

// RecordAddonReconcile records an addon reconcile operation.
func RecordAddonReconcile(name, result string, duration time.Duration) {
	AddonReconcileTotal.WithLabelValues(name, result).Inc()
	AddonReconcileDuration.WithLabelValues(name).Observe(duration.Seconds())
}

// RecordAddonHelmOperation records a Helm operation for an addon.
func RecordAddonHelmOperation(name, operation, result string, duration time.Duration) {
	AddonHelmOperationTotal.WithLabelValues(name, operation, result).Inc()
	AddonHelmOperationDuration.WithLabelValues(name, operation).Observe(duration.Seconds())
}
