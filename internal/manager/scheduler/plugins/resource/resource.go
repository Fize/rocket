package resource

import (
	"context"

	v1 "k8s.io/api/core/v1"

	"github.com/hex-techs/rocket/internal/manager/scheduler/framework"
	appsv1alpha1 "github.com/hex-techs/rocket/pkg/apis/apps/v1alpha1"
	clusterv1alpha1 "github.com/hex-techs/rocket/pkg/apis/storage/v1alpha1"
)

const Name = "Resource"

const (
	// ScoringStrategyLeastAllocated favors clusters with fewer requested resources (load balancing)
	ScoringStrategyLeastAllocated = "LeastAllocated"
	// ScoringStrategyMostAllocated favors clusters with most requested resources (bin packing)
	ScoringStrategyMostAllocated = "MostAllocated"
)

type Resource struct {
	strategy string
}

var _ framework.ScorePlugin = &Resource{}

func New() framework.Plugin {
	return &Resource{
		strategy: ScoringStrategyLeastAllocated, // default
	}
}

func NewWithStrategy(strategy string) framework.Plugin {
	return &Resource{
		strategy: strategy,
	}
}

func (pl *Resource) Name() string {
	return Name
}

// Score implements LeastAllocated or MostAllocated strategy based on configuration
func (pl *Resource) Score(ctx context.Context, state *framework.CycleState, app *appsv1alpha1.Application, cluster *clusterv1alpha1.ManagedCluster) (int64, *framework.Status) {
	if len(cluster.Status.ResourceSummary) == 0 {
		return 0, framework.NewStatus(framework.Success, "")
	}

	allocatable := cluster.Status.ResourceSummary[0].Allocatable
	allocated := cluster.Status.ResourceSummary[0].Allocated

	var cpuScore, memScore int64

	if pl.strategy == ScoringStrategyMostAllocated {
		cpuScore = mostAllocatedScore(allocatable, allocated, v1.ResourceCPU)
		memScore = mostAllocatedScore(allocatable, allocated, v1.ResourceMemory)
	} else {
		// Default to LeastAllocated
		cpuScore = leastAllocatedScore(allocatable, allocated, v1.ResourceCPU)
		memScore = leastAllocatedScore(allocatable, allocated, v1.ResourceMemory)
	}

	return (cpuScore + memScore) / 2, framework.NewStatus(framework.Success, "")
}

func leastAllocatedScore(allocatable, allocated v1.ResourceList, name v1.ResourceName) int64 {
	capacity := allocatable[name]
	used := allocated[name]

	if capacity.IsZero() {
		return 0
	}

	// score = (capacity - used) * 100 / capacity
	available := capacity.DeepCopy()
	available.Sub(used)

	// Use milli values for better precision
	capacityMilli := capacity.MilliValue()
	availableMilli := available.MilliValue()

	if capacityMilli == 0 {
		return 0
	}

	return availableMilli * 100 / capacityMilli
}

func mostAllocatedScore(allocatable, allocated v1.ResourceList, name v1.ResourceName) int64 {
	capacity := allocatable[name]
	used := allocated[name]

	if capacity.IsZero() {
		return 0
	}

	// score = used * 100 / capacity
	// Use milli values for better precision
	capacityMilli := capacity.MilliValue()
	usedMilli := used.MilliValue()

	if capacityMilli == 0 {
		return 0
	}

	return usedMilli * 100 / capacityMilli
}

func (pl *Resource) ScoreExtensions() framework.ScoreExtensions {
	return pl
}

// NormalizeScore normalizes the scores to 0-100 range
func (pl *Resource) NormalizeScore(ctx context.Context, state *framework.CycleState, app *appsv1alpha1.Application, scores map[string]int64) *framework.Status {
	// Scores are already in 0-100 range, no normalization needed
	// But we ensure they don't exceed bounds
	for cluster, score := range scores {
		if score > 100 {
			scores[cluster] = 100
		} else if score < 0 {
			scores[cluster] = 0
		}
	}
	return framework.NewStatus(framework.Success, "")
}
