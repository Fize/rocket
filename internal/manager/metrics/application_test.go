package metrics

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
)

func TestRecordWorkloadDeploy(t *testing.T) {
	tests := []struct {
		name        string
		application string
		cluster     string
		kind        string
		result      string
		duration    time.Duration
	}{
		{
			name:        "successful deployment",
			application: "test-app",
			cluster:     "edge-1",
			kind:        "Deployment",
			result:      "success",
			duration:    1 * time.Second,
		},
		{
			name:        "failed deployment",
			application: "test-app",
			cluster:     "edge-1",
			kind:        "StatefulSet",
			result:      "error",
			duration:    500 * time.Millisecond,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.NotPanics(t, func() {
				RecordWorkloadDeploy(tt.application, tt.cluster, tt.kind, tt.result, tt.duration)
			})
		})
	}
}

func TestRecordStatusSync(t *testing.T) {
	tests := []struct {
		name        string
		application string
		cluster     string
		result      string
	}{
		{
			name:        "successful sync",
			application: "test-app",
			cluster:     "edge-1",
			result:      "success",
		},
		{
			name:        "failed sync",
			application: "test-app",
			cluster:     "edge-1",
			result:      "error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.NotPanics(t, func() {
				RecordStatusSync(tt.application, tt.cluster, tt.result)
			})
		})
	}
}

func TestSetApplicationHealthPhase(t *testing.T) {
	tests := []struct {
		name        string
		application string
		phase       string
	}{
		{
			name:        "healthy phase",
			application: "test-app",
			phase:       "Healthy",
		},
		{
			name:        "unhealthy phase",
			application: "test-app",
			phase:       "Unhealthy",
		},
		{
			name:        "pending phase",
			application: "test-app",
			phase:       "Pending",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.NotPanics(t, func() {
				SetApplicationHealthPhase(tt.application, tt.phase)
			})
		})
	}
}

func TestClearApplicationHealthPhase(t *testing.T) {
	// First set some phases
	SetApplicationHealthPhase("test-app", "Healthy")
	SetApplicationHealthPhase("test-app", "Pending")

	// Should not panic when clearing
	assert.NotPanics(t, func() {
		ClearApplicationHealthPhase("test-app")
	})
}

func TestSetManagedApplicationTotal(t *testing.T) {
	tests := []struct {
		name  string
		count int
	}{
		{
			name:  "zero count",
			count: 0,
		},
		{
			name:  "single application",
			count: 1,
		},
		{
			name:  "multiple applications",
			count: 100,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.NotPanics(t, func() {
				SetManagedApplicationTotal(tt.count)
			})
		})
	}
}

func TestApplicationMetricsRegistered(t *testing.T) {
	ch := make(chan *prometheus.Desc, 10)

	go func() {
		WorkloadDeployTotal.Describe(ch)
		WorkloadDeployDuration.Describe(ch)
		StatusSyncTotal.Describe(ch)
		ApplicationHealthPhase.Describe(ch)
		ManagedApplicationTotal.Describe(ch)
		close(ch)
	}()

	count := 0
	for range ch {
		count++
	}

	assert.Equal(t, 5, count, "all 5 application metrics should be registered and describable")
}
