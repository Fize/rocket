package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	// WorkloadDeployTotal counts workload deployment operations.
	WorkloadDeployTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "rocket",
			Subsystem: "application",
			Name:      "workload_deploy_total",
			Help:      "Total number of workload deployment operations.",
		},
		[]string{"application", "cluster", "kind", "result"},
	)

	// WorkloadDeployDuration records workload deployment duration.
	WorkloadDeployDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "rocket",
			Subsystem: "application",
			Name:      "workload_deploy_duration_seconds",
			Help:      "Duration of workload deployment operations in seconds.",
			Buckets:   []float64{0.1, 0.5, 1, 2, 5, 10, 30, 60, 120},
		},
		[]string{"application", "cluster", "kind"},
	)

	// StatusSyncTotal counts application status sync operations.
	StatusSyncTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "rocket",
			Subsystem: "application",
			Name:      "status_sync_total",
			Help:      "Total number of application status sync operations.",
		},
		[]string{"application", "cluster", "result"},
	)

	// ApplicationHealthPhase records the current health phase of applications.
	ApplicationHealthPhase = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "rocket",
			Subsystem: "application",
			Name:      "health_phase",
			Help:      "Current health phase of applications. 1 means the application is in this phase.",
		},
		[]string{"application", "phase"},
	)
)

var (
	// ManagedApplicationTotal records the total number of managed applications.
	ManagedApplicationTotal = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "rocket",
			Subsystem: "application",
			Name:      "managed_total",
			Help:      "Total number of managed applications.",
		},
	)
)

func init() {
	metrics.Registry.MustRegister(
		WorkloadDeployTotal,
		WorkloadDeployDuration,
		StatusSyncTotal,
		ApplicationHealthPhase,
		ManagedApplicationTotal,
	)
}

// RecordWorkloadDeploy records a workload deployment attempt.
func RecordWorkloadDeploy(application, cluster, kind, result string, duration time.Duration) {
	WorkloadDeployTotal.WithLabelValues(application, cluster, kind, result).Inc()
	WorkloadDeployDuration.WithLabelValues(application, cluster, kind).Observe(duration.Seconds())
}

// RecordStatusSync records a status sync operation.
func RecordStatusSync(application, cluster, result string) {
	StatusSyncTotal.WithLabelValues(application, cluster, result).Inc()
}

// SetApplicationHealthPhase sets the health phase gauge for an application.
func SetApplicationHealthPhase(application, phase string) {
	ApplicationHealthPhase.WithLabelValues(application, phase).Set(1)
}

// ClearApplicationHealthPhase clears all health phase gauges for an application.
func ClearApplicationHealthPhase(application string) {
	ApplicationHealthPhase.DeletePartialMatch(prometheus.Labels{"application": application})
}

// SetManagedApplicationTotal sets the total number of managed applications.
func SetManagedApplicationTotal(count int) {
	ManagedApplicationTotal.Set(float64(count))
}
