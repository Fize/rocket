package application

import (
	"testing"

	appsv1alpha1 "github.com/fize/rocket/pkg/apis/apps/v1alpha1"
	"github.com/stretchr/testify/assert"
)

func TestCalculatePhase(t *testing.T) {
	r := &StatusReconciler{}

	tests := []struct {
		name     string
		statuses []appsv1alpha1.ClusterStatus
		expected appsv1alpha1.HealthPhase
	}{
		{
			name:     "Empty",
			statuses: []appsv1alpha1.ClusterStatus{},
			expected: appsv1alpha1.Unknown,
		},
		{
			name: "All Healthy",
			statuses: []appsv1alpha1.ClusterStatus{
				{Phase: appsv1alpha1.ClusterPhaseHealthy},
				{Phase: appsv1alpha1.ClusterPhaseHealthy},
			},
			expected: appsv1alpha1.Healthy,
		},
		{
			name: "One Progressing",
			statuses: []appsv1alpha1.ClusterStatus{
				{Phase: appsv1alpha1.ClusterPhaseHealthy},
				{Phase: appsv1alpha1.ClusterPhaseProgressing},
			},
			expected: appsv1alpha1.Progressing,
		},
		{
			name: "One Degraded",
			statuses: []appsv1alpha1.ClusterStatus{
				{Phase: appsv1alpha1.ClusterPhaseHealthy},
				{Phase: appsv1alpha1.ClusterPhaseDegraded},
			},
			expected: appsv1alpha1.Degraded,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			phase := r.calculatePhase(tt.statuses)
			assert.Equal(t, tt.expected, phase)
		})
	}
}

func TestMergeClusterStatus(t *testing.T) {
	r := &StatusReconciler{}

	existing := []appsv1alpha1.ClusterStatus{
		{ClusterName: "c1", Phase: appsv1alpha1.ClusterPhaseHealthy},
		{ClusterName: "c2", Phase: appsv1alpha1.ClusterPhaseProgressing},
	}

	new := []appsv1alpha1.ClusterStatus{
		{ClusterName: "c2", Phase: appsv1alpha1.ClusterPhaseHealthy},
		{ClusterName: "c3", Phase: appsv1alpha1.ClusterPhaseUnknown},
	}

	merged := r.mergeClusterStatus(existing, new)

	assert.Len(t, merged, 3)

	m := make(map[string]appsv1alpha1.ClusterStatus)
	for _, s := range merged {
		m[s.ClusterName] = s
	}

	assert.Equal(t, appsv1alpha1.ClusterPhaseHealthy, m["c1"].Phase)
	assert.Equal(t, appsv1alpha1.ClusterPhaseHealthy, m["c2"].Phase)
	assert.Equal(t, appsv1alpha1.ClusterPhaseUnknown, m["c3"].Phase)
}
