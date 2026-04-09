package health

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/fize/rocket/internal/manager/scheduler/framework"
	appsv1alpha1 "github.com/fize/rocket/pkg/apis/apps/v1alpha1"
	clusterv1alpha1 "github.com/fize/rocket/pkg/apis/storage/v1alpha1"
)

func TestHealthFilter(t *testing.T) {
	tests := []struct {
		name           string
		cluster        *clusterv1alpha1.ManagedCluster
		expectedStatus int
	}{
		{
			name: "no conditions - should pass",
			cluster: &clusterv1alpha1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster1",
				},
				Status: clusterv1alpha1.ManagedClusterStatus{},
			},
			expectedStatus: framework.Success,
		},
		{
			name: "ready and reachable",
			cluster: &clusterv1alpha1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster1",
				},
				Status: clusterv1alpha1.ManagedClusterStatus{
					Conditions: []metav1.Condition{
						{
							Type:   "Ready",
							Status: metav1.ConditionTrue,
						},
						{
							Type:   "Reachable",
							Status: metav1.ConditionTrue,
						},
					},
				},
			},
			expectedStatus: framework.Success,
		},
		{
			name: "not ready",
			cluster: &clusterv1alpha1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster1",
				},
				Status: clusterv1alpha1.ManagedClusterStatus{
					Conditions: []metav1.Condition{
						{
							Type:   "Ready",
							Status: metav1.ConditionFalse,
						},
						{
							Type:   "Reachable",
							Status: metav1.ConditionTrue,
						},
					},
				},
			},
			expectedStatus: framework.Unschedulable,
		},
		{
			name: "not reachable",
			cluster: &clusterv1alpha1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster1",
				},
				Status: clusterv1alpha1.ManagedClusterStatus{
					Conditions: []metav1.Condition{
						{
							Type:   "Ready",
							Status: metav1.ConditionTrue,
						},
						{
							Type:   "Reachable",
							Status: metav1.ConditionFalse,
						},
					},
				},
			},
			expectedStatus: framework.Unschedulable,
		},
		{
			name: "unknown ready status",
			cluster: &clusterv1alpha1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster1",
				},
				Status: clusterv1alpha1.ManagedClusterStatus{
					Conditions: []metav1.Condition{
						{
							Type:   "Ready",
							Status: metav1.ConditionUnknown,
						},
					},
				},
			},
			expectedStatus: framework.Unschedulable,
		},
		{
			name: "only ready condition present",
			cluster: &clusterv1alpha1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster1",
				},
				Status: clusterv1alpha1.ManagedClusterStatus{
					Conditions: []metav1.Condition{
						{
							Type:   "Ready",
							Status: metav1.ConditionTrue,
						},
					},
				},
			},
			expectedStatus: framework.Success,
		},
	}

	plugin := New().(*Health)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := framework.NewCycleState()
			app := &appsv1alpha1.Application{}
			status := plugin.Filter(context.Background(), state, app, tt.cluster)
			if status.Code != tt.expectedStatus {
				t.Errorf("expected status %d, got %d: %s", tt.expectedStatus, status.Code, status.Message)
			}
		})
	}
}
