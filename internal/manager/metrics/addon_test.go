package metrics

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
)

func TestRecordAddonReconcile(t *testing.T) {
	tests := []struct {
		name     string
		addon    string
		result   string
		duration time.Duration
	}{
		{
			name:     "successful reconcile",
			addon:    "test-addon",
			result:   "success",
			duration: 100 * time.Millisecond,
		},
		{
			name:     "failed reconcile",
			addon:    "test-addon",
			result:   "error",
			duration: 50 * time.Millisecond,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.NotPanics(t, func() {
				RecordAddonReconcile(tt.addon, tt.result, tt.duration)
			})
		})
	}
}

func TestRecordAddonHelmOperation(t *testing.T) {
	tests := []struct {
		name      string
		addon     string
		operation string
		result    string
		duration  time.Duration
	}{
		{
			name:      "successful install",
			addon:     "test-addon",
			operation: "install",
			result:    "success",
			duration:  1 * time.Second,
		},
		{
			name:      "failed upgrade",
			addon:     "test-addon",
			operation: "upgrade",
			result:    "error",
			duration:  2 * time.Second,
		},
		{
			name:      "successful uninstall",
			addon:     "test-addon",
			operation: "uninstall",
			result:    "success",
			duration:  500 * time.Millisecond,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.NotPanics(t, func() {
				RecordAddonHelmOperation(tt.addon, tt.operation, tt.result, tt.duration)
			})
		})
	}
}

func TestAddonMetricsRegistered(t *testing.T) {
	// Test that all addon metrics can be described (indicating they are registered)
	ch := make(chan *prometheus.Desc, 10)

	go func() {
		AddonReconcileTotal.Describe(ch)
		AddonReconcileDuration.Describe(ch)
		AddonHelmOperationTotal.Describe(ch)
		AddonHelmOperationDuration.Describe(ch)
		close(ch)
	}()

	count := 0
	for range ch {
		count++
	}

	assert.Equal(t, 4, count, "all 4 addon metrics should be registered and describable")
}
