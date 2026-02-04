package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	// SchedulingAttempts tracks the total number of scheduling attempts
	SchedulingAttempts = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "rocket",
			Subsystem: "scheduler",
			Name:      "scheduling_attempts_total",
			Help:      "Total number of scheduling attempts by result (success, error, unschedulable)",
		},
		[]string{"result"},
	)

	// SchedulingLatency tracks the scheduling latency in seconds
	SchedulingLatency = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "rocket",
			Subsystem: "scheduler",
			Name:      "scheduling_duration_seconds",
			Help:      "Scheduling latency in seconds",
			Buckets:   []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1, 5, 10},
		},
		[]string{"result"},
	)

	// PluginLatency tracks per-plugin execution latency
	PluginLatency = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "rocket",
			Subsystem: "scheduler",
			Name:      "plugin_duration_seconds",
			Help:      "Plugin execution latency in seconds",
			Buckets:   []float64{0.0001, 0.0005, 0.001, 0.005, 0.01, 0.05, 0.1},
		},
		[]string{"plugin", "phase"}, // phase: filter, score
	)

	// QueueLength tracks the current queue length
	QueueLength = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "rocket",
			Subsystem: "scheduler",
			Name:      "queue_length",
			Help:      "Current number of applications in the scheduling queue",
		},
	)

	// RetryCount tracks the number of scheduling retries
	RetryCount = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: "rocket",
			Subsystem: "scheduler",
			Name:      "retry_total",
			Help:      "Total number of scheduling retries",
		},
	)

	// FilteredClusters tracks how many clusters were filtered out
	FilteredClusters = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Namespace: "rocket",
			Subsystem: "scheduler",
			Name:      "filtered_clusters",
			Help:      "Number of clusters filtered out per scheduling cycle",
			Buckets:   []float64{0, 1, 2, 5, 10, 20, 50, 100},
		},
	)

	// FeasibleClusters tracks how many clusters passed filtering
	FeasibleClusters = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Namespace: "rocket",
			Subsystem: "scheduler",
			Name:      "feasible_clusters",
			Help:      "Number of feasible clusters per scheduling cycle",
			Buckets:   []float64{0, 1, 2, 5, 10, 20, 50, 100},
		},
	)
)

func init() {
	// Register all metrics with controller-runtime's registry
	metrics.Registry.MustRegister(
		SchedulingAttempts,
		SchedulingLatency,
		PluginLatency,
		QueueLength,
		RetryCount,
		FilteredClusters,
		FeasibleClusters,
	)
}

// RecordSchedulingAttempt records a scheduling attempt with the given result
func RecordSchedulingAttempt(result string, duration time.Duration) {
	SchedulingAttempts.WithLabelValues(result).Inc()
	SchedulingLatency.WithLabelValues(result).Observe(duration.Seconds())
}

// RecordPluginDuration records plugin execution duration
func RecordPluginDuration(plugin, phase string, duration time.Duration) {
	PluginLatency.WithLabelValues(plugin, phase).Observe(duration.Seconds())
}

// RecordRetry records a scheduling retry
func RecordRetry() {
	RetryCount.Inc()
}

// SetQueueLength sets the current queue length
func SetQueueLength(length int) {
	QueueLength.Set(float64(length))
}

// RecordFilterResults records the filtering results
func RecordFilterResults(total, feasible int) {
	FilteredClusters.Observe(float64(total - feasible))
	FeasibleClusters.Observe(float64(feasible))
}
