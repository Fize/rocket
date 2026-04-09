package topology

import (
	"context"
	"testing"

	"github.com/fize/rocket/internal/manager/scheduler/framework"
	appsv1alpha1 "github.com/fize/rocket/pkg/apis/apps/v1alpha1"
	clusterv1alpha1 "github.com/fize/rocket/pkg/apis/storage/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestTopologySpreadScore(t *testing.T) {
	tests := []struct {
		name               string
		clusters           []*clusterv1alpha1.ManagedCluster
		existingPlacements []appsv1alpha1.ClusterTopology
		expectedOrder      []string
	}{
		{
			name: "favor zone with fewer replicas",
			clusters: []*clusterv1alpha1.ManagedCluster{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cluster-a",
						Labels: map[string]string{
							"topology.kubernetes.io/zone": "zone-1",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cluster-b",
						Labels: map[string]string{
							"topology.kubernetes.io/zone": "zone-2",
						},
					},
				},
			},
			existingPlacements: []appsv1alpha1.ClusterTopology{
				{Name: "cluster-a", Replicas: 10},
				{Name: "cluster-b", Replicas: 2},
			},
			expectedOrder: []string{"cluster-b", "cluster-a"},
		},
		{
			name: "equal distribution",
			clusters: []*clusterv1alpha1.ManagedCluster{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cluster-a",
						Labels: map[string]string{
							"topology.kubernetes.io/zone": "zone-1",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cluster-b",
						Labels: map[string]string{
							"topology.kubernetes.io/zone": "zone-2",
						},
					},
				},
			},
			existingPlacements: []appsv1alpha1.ClusterTopology{
				{Name: "cluster-a", Replicas: 5},
				{Name: "cluster-b", Replicas: 5},
			},
			expectedOrder: []string{"cluster-a", "cluster-b"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plugin := New().(*TopologySpread)
			state := framework.NewCycleState()

			UpdateTopologyDistribution(state, tt.clusters, tt.existingPlacements, "topology.kubernetes.io/zone")

			scores := make(map[string]int64)
			for _, cluster := range tt.clusters {
				score, status := plugin.Score(context.Background(), state, &appsv1alpha1.Application{}, cluster)
				if !status.IsSuccess() {
					t.Errorf("Score failed: %s", status.Message)
				}
				scores[cluster.Name] = score
			}

			status := plugin.NormalizeScore(context.Background(), state, &appsv1alpha1.Application{}, scores)
			if !status.IsSuccess() {
				t.Errorf("NormalizeScore failed: %s", status.Message)
			}

			if len(tt.expectedOrder) >= 2 {
				first := scores[tt.expectedOrder[0]]
				second := scores[tt.expectedOrder[1]]
				if first < second {
					t.Errorf("Expected %s (score=%d) >= %s (score=%d)",
						tt.expectedOrder[0], first, tt.expectedOrder[1], second)
				}
			}
		})
	}
}
