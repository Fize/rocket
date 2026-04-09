package resource

import (
	"context"
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/fize/rocket/internal/manager/scheduler/framework"
	appsv1alpha1 "github.com/fize/rocket/pkg/apis/apps/v1alpha1"
	clusterv1alpha1 "github.com/fize/rocket/pkg/apis/storage/v1alpha1"
)

func TestResourceScore(t *testing.T) {
	tests := []struct {
		name          string
		cluster       *clusterv1alpha1.ManagedCluster
		expectedScore int64
	}{
		{
			name: "no resource summary",
			cluster: &clusterv1alpha1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster1",
				},
				Status: clusterv1alpha1.ManagedClusterStatus{},
			},
			expectedScore: 0,
		},
		{
			name: "50% utilized cluster",
			cluster: &clusterv1alpha1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster1",
				},
				Status: clusterv1alpha1.ManagedClusterStatus{
					ResourceSummary: []clusterv1alpha1.ResourceSummary{
						{
							Allocatable: v1.ResourceList{
								v1.ResourceCPU:    resource.MustParse("4"),
								v1.ResourceMemory: resource.MustParse("8Gi"),
							},
							Allocated: v1.ResourceList{
								v1.ResourceCPU:    resource.MustParse("2"),
								v1.ResourceMemory: resource.MustParse("4Gi"),
							},
						},
					},
				},
			},
			expectedScore: 50,
		},
		{
			name: "nearly empty cluster",
			cluster: &clusterv1alpha1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster1",
				},
				Status: clusterv1alpha1.ManagedClusterStatus{
					ResourceSummary: []clusterv1alpha1.ResourceSummary{
						{
							Allocatable: v1.ResourceList{
								v1.ResourceCPU:    resource.MustParse("4"),
								v1.ResourceMemory: resource.MustParse("8Gi"),
							},
							Allocated: v1.ResourceList{
								v1.ResourceCPU:    resource.MustParse("100m"),
								v1.ResourceMemory: resource.MustParse("256Mi"),
							},
						},
					},
				},
			},
			expectedScore: 96, // High score for mostly free cluster
		},
		{
			name: "nearly full cluster",
			cluster: &clusterv1alpha1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster1",
				},
				Status: clusterv1alpha1.ManagedClusterStatus{
					ResourceSummary: []clusterv1alpha1.ResourceSummary{
						{
							Allocatable: v1.ResourceList{
								v1.ResourceCPU:    resource.MustParse("4"),
								v1.ResourceMemory: resource.MustParse("8Gi"),
							},
							Allocated: v1.ResourceList{
								v1.ResourceCPU:    resource.MustParse("3.8"),
								v1.ResourceMemory: resource.MustParse("7.5Gi"),
							},
						},
					},
				},
			},
			expectedScore: 7, // Low score for nearly full cluster
		},
	}

	plugin := New().(*Resource)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := framework.NewCycleState()
			app := &appsv1alpha1.Application{}
			score, status := plugin.Score(context.Background(), state, app, tt.cluster)
			if !status.IsSuccess() {
				t.Errorf("expected success, got: %s", status.Message)
			}
			// Allow some tolerance for rounding
			if score < tt.expectedScore-2 || score > tt.expectedScore+2 {
				t.Errorf("expected score around %d, got %d", tt.expectedScore, score)
			}
		})
	}
}

func TestResourceName(t *testing.T) {
	plugin := New()
	if plugin.Name() != Name {
		t.Errorf("expected name %s, got %s", Name, plugin.Name())
	}
}

func TestNewWithStrategy(t *testing.T) {
	plugin := NewWithStrategy(ScoringStrategyMostAllocated).(*Resource)
	if plugin.strategy != ScoringStrategyMostAllocated {
		t.Errorf("expected strategy %s, got %s", ScoringStrategyMostAllocated, plugin.strategy)
	}
}

func TestMostAllocatedScore(t *testing.T) {
	tests := []struct {
		name          string
		cluster       *clusterv1alpha1.ManagedCluster
		expectedScore int64
	}{
		{
			name: "50% utilized - MostAllocated prefers this",
			cluster: &clusterv1alpha1.ManagedCluster{
				Status: clusterv1alpha1.ManagedClusterStatus{
					ResourceSummary: []clusterv1alpha1.ResourceSummary{
						{
							Allocatable: v1.ResourceList{
								v1.ResourceCPU:    resource.MustParse("4"),
								v1.ResourceMemory: resource.MustParse("8Gi"),
							},
							Allocated: v1.ResourceList{
								v1.ResourceCPU:    resource.MustParse("2"),
								v1.ResourceMemory: resource.MustParse("4Gi"),
							},
						},
					},
				},
			},
			expectedScore: 50, // Avg of used percentages
		},
		{
			name: "90% utilized - MostAllocated gives high score",
			cluster: &clusterv1alpha1.ManagedCluster{
				Status: clusterv1alpha1.ManagedClusterStatus{
					ResourceSummary: []clusterv1alpha1.ResourceSummary{
						{
							Allocatable: v1.ResourceList{
								v1.ResourceCPU:    resource.MustParse("4"),
								v1.ResourceMemory: resource.MustParse("8Gi"),
							},
							Allocated: v1.ResourceList{
								v1.ResourceCPU:    resource.MustParse("3.6"),
								v1.ResourceMemory: resource.MustParse("7.2Gi"),
							},
						},
					},
				},
			},
			expectedScore: 90,
		},
	}

	plugin := NewWithStrategy(ScoringStrategyMostAllocated).(*Resource)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := framework.NewCycleState()
			app := &appsv1alpha1.Application{}
			score, status := plugin.Score(context.Background(), state, app, tt.cluster)
			if !status.IsSuccess() {
				t.Errorf("expected success, got: %s", status.Message)
			}
			if score < tt.expectedScore-2 || score > tt.expectedScore+2 {
				t.Errorf("expected score around %d, got %d", tt.expectedScore, score)
			}
		})
	}
}

func TestLeastAllocatedScoreFunc(t *testing.T) {
	allocatable := v1.ResourceList{
		v1.ResourceCPU: resource.MustParse("4"),
	}
	allocated := v1.ResourceList{
		v1.ResourceCPU: resource.MustParse("1"),
	}

	score := leastAllocatedScore(allocatable, allocated, v1.ResourceCPU)
	// (4-1)*100/4 = 75
	if score != 75 {
		t.Errorf("expected 75, got %d", score)
	}
}

func TestMostAllocatedScoreFunc(t *testing.T) {
	allocatable := v1.ResourceList{
		v1.ResourceCPU: resource.MustParse("4"),
	}
	allocated := v1.ResourceList{
		v1.ResourceCPU: resource.MustParse("3"),
	}

	score := mostAllocatedScore(allocatable, allocated, v1.ResourceCPU)
	// 3*100/4 = 75
	if score != 75 {
		t.Errorf("expected 75, got %d", score)
	}
}

func TestScoreExtensions(t *testing.T) {
	plugin := New().(*Resource)
	ext := plugin.ScoreExtensions()
	if ext != plugin {
		t.Error("ScoreExtensions should return the plugin itself")
	}
}

func TestNormalizeScore(t *testing.T) {
	plugin := New().(*Resource)
	state := framework.NewCycleState()
	app := &appsv1alpha1.Application{}

	scores := map[string]int64{
		"cluster1": 50,
		"cluster2": 150, // Over 100
		"cluster3": -10, // Negative
	}

	status := plugin.NormalizeScore(context.Background(), state, app, scores)
	if !status.IsSuccess() {
		t.Errorf("expected success, got: %s", status.Message)
	}

	if scores["cluster1"] != 50 {
		t.Errorf("expected 50, got %d", scores["cluster1"])
	}
	if scores["cluster2"] != 100 {
		t.Errorf("expected 100, got %d", scores["cluster2"])
	}
	if scores["cluster3"] != 0 {
		t.Errorf("expected 0, got %d", scores["cluster3"])
	}
}

func TestLeastAllocatedScoreZeroCapacity(t *testing.T) {
	allocatable := v1.ResourceList{
		v1.ResourceCPU: resource.MustParse("0"),
	}
	allocated := v1.ResourceList{
		v1.ResourceCPU: resource.MustParse("0"),
	}

	score := leastAllocatedScore(allocatable, allocated, v1.ResourceCPU)
	if score != 0 {
		t.Errorf("expected 0 for zero capacity, got %d", score)
	}
}

func TestMostAllocatedScoreZeroCapacity(t *testing.T) {
	allocatable := v1.ResourceList{
		v1.ResourceCPU: resource.MustParse("0"),
	}
	allocated := v1.ResourceList{
		v1.ResourceCPU: resource.MustParse("0"),
	}

	score := mostAllocatedScore(allocatable, allocated, v1.ResourceCPU)
	if score != 0 {
		t.Errorf("expected 0 for zero capacity, got %d", score)
	}
}

func int32Ptr(i int32) *int32 {
	return &i
}
