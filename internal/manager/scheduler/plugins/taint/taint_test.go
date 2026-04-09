package taint

import (
	"context"
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/fize/rocket/internal/manager/scheduler/framework"
	appsv1alpha1 "github.com/fize/rocket/pkg/apis/apps/v1alpha1"
	clusterv1alpha1 "github.com/fize/rocket/pkg/apis/storage/v1alpha1"
)

func TestTaintTolerationFilter(t *testing.T) {
	tests := []struct {
		name           string
		app            *appsv1alpha1.Application
		cluster        *clusterv1alpha1.ManagedCluster
		expectedStatus int
	}{
		{
			name: "no taints",
			app: &appsv1alpha1.Application{
				Spec: appsv1alpha1.ApplicationSpec{},
			},
			cluster: &clusterv1alpha1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster1",
				},
				Spec: clusterv1alpha1.ManagedClusterSpec{},
			},
			expectedStatus: framework.Success,
		},
		{
			name: "taint with matching toleration",
			app: &appsv1alpha1.Application{
				Spec: appsv1alpha1.ApplicationSpec{
					ClusterTolerations: []v1.Toleration{
						{
							Key:      "dedicated",
							Operator: v1.TolerationOpEqual,
							Value:    "gpu",
							Effect:   v1.TaintEffectNoSchedule,
						},
					},
				},
			},
			cluster: &clusterv1alpha1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster1",
				},
				Spec: clusterv1alpha1.ManagedClusterSpec{
					Taints: []v1.Taint{
						{
							Key:    "dedicated",
							Value:  "gpu",
							Effect: v1.TaintEffectNoSchedule,
						},
					},
				},
			},
			expectedStatus: framework.Success,
		},
		{
			name: "taint without matching toleration",
			app: &appsv1alpha1.Application{
				Spec: appsv1alpha1.ApplicationSpec{
					ClusterTolerations: []v1.Toleration{
						{
							Key:      "dedicated",
							Operator: v1.TolerationOpEqual,
							Value:    "cpu",
							Effect:   v1.TaintEffectNoSchedule,
						},
					},
				},
			},
			cluster: &clusterv1alpha1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster1",
				},
				Spec: clusterv1alpha1.ManagedClusterSpec{
					Taints: []v1.Taint{
						{
							Key:    "dedicated",
							Value:  "gpu",
							Effect: v1.TaintEffectNoSchedule,
						},
					},
				},
			},
			expectedStatus: framework.Unschedulable,
		},
		{
			name: "exists operator toleration",
			app: &appsv1alpha1.Application{
				Spec: appsv1alpha1.ApplicationSpec{
					ClusterTolerations: []v1.Toleration{
						{
							Key:      "dedicated",
							Operator: v1.TolerationOpExists,
							Effect:   v1.TaintEffectNoSchedule,
						},
					},
				},
			},
			cluster: &clusterv1alpha1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster1",
				},
				Spec: clusterv1alpha1.ManagedClusterSpec{
					Taints: []v1.Taint{
						{
							Key:    "dedicated",
							Value:  "gpu",
							Effect: v1.TaintEffectNoSchedule,
						},
					},
				},
			},
			expectedStatus: framework.Success,
		},
		{
			name: "wildcard toleration",
			app: &appsv1alpha1.Application{
				Spec: appsv1alpha1.ApplicationSpec{
					ClusterTolerations: []v1.Toleration{
						{
							Operator: v1.TolerationOpExists,
						},
					},
				},
			},
			cluster: &clusterv1alpha1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster1",
				},
				Spec: clusterv1alpha1.ManagedClusterSpec{
					Taints: []v1.Taint{
						{
							Key:    "dedicated",
							Value:  "gpu",
							Effect: v1.TaintEffectNoSchedule,
						},
					},
				},
			},
			expectedStatus: framework.Success,
		},
		{
			name: "no effect taint should be ignored",
			app: &appsv1alpha1.Application{
				Spec: appsv1alpha1.ApplicationSpec{},
			},
			cluster: &clusterv1alpha1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster1",
				},
				Spec: clusterv1alpha1.ManagedClusterSpec{
					Taints: []v1.Taint{
						{
							Key:    "dedicated",
							Value:  "gpu",
							Effect: v1.TaintEffectPreferNoSchedule,
						},
					},
				},
			},
			expectedStatus: framework.Success,
		},
		{
			name: "multiple taints with partial tolerations",
			app: &appsv1alpha1.Application{
				Spec: appsv1alpha1.ApplicationSpec{
					ClusterTolerations: []v1.Toleration{
						{
							Key:      "dedicated",
							Operator: v1.TolerationOpEqual,
							Value:    "gpu",
							Effect:   v1.TaintEffectNoSchedule,
						},
					},
				},
			},
			cluster: &clusterv1alpha1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster1",
				},
				Spec: clusterv1alpha1.ManagedClusterSpec{
					Taints: []v1.Taint{
						{
							Key:    "dedicated",
							Value:  "gpu",
							Effect: v1.TaintEffectNoSchedule,
						},
						{
							Key:    "restricted",
							Value:  "true",
							Effect: v1.TaintEffectNoSchedule,
						},
					},
				},
			},
			expectedStatus: framework.Unschedulable,
		},
	}

	plugin := New().(*TaintToleration)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := framework.NewCycleState()
			status := plugin.Filter(context.Background(), state, tt.app, tt.cluster)
			if status.Code != tt.expectedStatus {
				t.Errorf("expected status %d, got %d: %s", tt.expectedStatus, status.Code, status.Message)
			}
		})
	}
}
