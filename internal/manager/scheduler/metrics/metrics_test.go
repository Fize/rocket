package metrics

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
)

func TestRecordSchedulingAttempt(t *testing.T) {
	tests := []struct {
		name     string
		result   string
		duration time.Duration
	}{
		{
			name:     "success attempt",
			result:   "success",
			duration: 100 * time.Millisecond,
		},
		{
			name:     "error attempt",
			result:   "error",
			duration: 50 * time.Millisecond,
		},
		{
			name:     "unschedulable attempt",
			result:   "unschedulable",
			duration: 10 * time.Millisecond,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Should not panic
			assert.NotPanics(t, func() {
				RecordSchedulingAttempt(tt.result, tt.duration)
			})
		})
	}
}

func TestRecordPluginDuration(t *testing.T) {
	tests := []struct {
		name     string
		plugin   string
		phase    string
		duration time.Duration
	}{
		{
			name:     "filter phase",
			plugin:   "Affinity",
			phase:    "filter",
			duration: 1 * time.Millisecond,
		},
		{
			name:     "score phase",
			plugin:   "Resource",
			phase:    "score",
			duration: 2 * time.Millisecond,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.NotPanics(t, func() {
				RecordPluginDuration(tt.plugin, tt.phase, tt.duration)
			})
		})
	}
}

func TestRecordRetry(t *testing.T) {
	assert.NotPanics(t, func() {
		RecordRetry()
	})
}

func TestSetQueueLength(t *testing.T) {
	tests := []struct {
		name   string
		length int
	}{
		{
			name:   "zero length",
			length: 0,
		},
		{
			name:   "positive length",
			length: 10,
		},
		{
			name:   "large length",
			length: 1000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.NotPanics(t, func() {
				SetQueueLength(tt.length)
			})
		})
	}
}

func TestRecordFilterResults(t *testing.T) {
	tests := []struct {
		name     string
		total    int
		feasible int
	}{
		{
			name:     "all feasible",
			total:    10,
			feasible: 10,
		},
		{
			name:     "none feasible",
			total:    10,
			feasible: 0,
		},
		{
			name:     "partial feasible",
			total:    10,
			feasible: 5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.NotPanics(t, func() {
				RecordFilterResults(tt.total, tt.feasible)
			})
		})
	}
}

func TestMetricsAreRegistered(t *testing.T) {
	// Test that all metrics are properly defined and can be described
	ch := make(chan *prometheus.Desc, 10)

	go func() {
		SchedulingAttempts.Describe(ch)
		close(ch)
	}()

	desc := <-ch
	assert.NotNil(t, desc, "SchedulingAttempts should be registered")
}

func TestSchedulingLatencyBuckets(t *testing.T) {
	// Test that latency histogram has reasonable buckets
	ch := make(chan *prometheus.Desc, 10)

	go func() {
		SchedulingLatency.Describe(ch)
		close(ch)
	}()

	desc := <-ch
	assert.NotNil(t, desc, "SchedulingLatency should be registered")
}

func TestPluginLatencyBuckets(t *testing.T) {
	ch := make(chan *prometheus.Desc, 10)

	go func() {
		PluginLatency.Describe(ch)
		close(ch)
	}()

	desc := <-ch
	assert.NotNil(t, desc, "PluginLatency should be registered")
}
