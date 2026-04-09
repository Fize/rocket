package application

import (
	"testing"

	"github.com/stretchr/testify/assert"
	appsv1alpha1 "github.com/fize/rocket/pkg/apis/apps/v1alpha1"
)

func TestGlobalReplicaCalculator_CalculateReplicas_EqualDistribution(t *testing.T) {
	calculator := NewGlobalReplicaCalculator()

	topology := []appsv1alpha1.ClusterTopology{
		{Name: "cluster-a", Replicas: 50},
		{Name: "cluster-b", Replicas: 50},
	}

	// Global 20% canary = 20 pods total, split equally = 10 each
	distribution := &appsv1alpha1.GlobalReplicaDistribution{
		Mode: appsv1alpha1.DistributionModeEqual,
	}

	result, err := calculator.CalculateReplicas(20, 100, topology, distribution)
	assert.NoError(t, err)
	assert.Len(t, result, 2)

	// Each cluster should get 10 canary pods (20% / 2 clusters)
	assert.Equal(t, int32(10), result["cluster-a"].CanaryReplicas)
	assert.Equal(t, int32(40), result["cluster-a"].StableReplicas)
	assert.Equal(t, int32(10), result["cluster-b"].CanaryReplicas)
	assert.Equal(t, int32(40), result["cluster-b"].StableReplicas)
}

func TestGlobalReplicaCalculator_CalculateReplicas_WeightedDistribution(t *testing.T) {
	calculator := NewGlobalReplicaCalculator()

	topology := []appsv1alpha1.ClusterTopology{
		{Name: "cluster-a", Replicas: 50},
		{Name: "cluster-b", Replicas: 50},
	}

	// Global 20% canary = 20 pods
	// Cluster A weight 25% = 5 pods
	// Cluster B weight 75% = 15 pods
	distribution := &appsv1alpha1.GlobalReplicaDistribution{
		Mode: appsv1alpha1.DistributionModeWeighted,
		ClusterWeights: []appsv1alpha1.ClusterReplicaWeight{
			{ClusterName: "cluster-a", Weight: 25},
			{ClusterName: "cluster-b", Weight: 75},
		},
	}

	result, err := calculator.CalculateReplicas(20, 100, topology, distribution)
	assert.NoError(t, err)
	assert.Len(t, result, 2)

	// Cluster A: 25% of 20 = 5 canary pods
	assert.Equal(t, int32(5), result["cluster-a"].CanaryReplicas)
	assert.Equal(t, int32(45), result["cluster-a"].StableReplicas)

	// Cluster B: 75% of 20 = 15 canary pods
	assert.Equal(t, int32(15), result["cluster-b"].CanaryReplicas)
	assert.Equal(t, int32(35), result["cluster-b"].StableReplicas)
}

func TestGlobalReplicaCalculator_CalculateReplicas_SequentialDistribution(t *testing.T) {
	calculator := NewGlobalReplicaCalculator()

	topology := []appsv1alpha1.ClusterTopology{
		{Name: "cluster-a", Replicas: 50},
		{Name: "cluster-b", Replicas: 50},
	}

	// Global 20% canary = 20 pods, all go to first cluster
	distribution := &appsv1alpha1.GlobalReplicaDistribution{
		Mode: appsv1alpha1.DistributionModeSequential,
	}

	result, err := calculator.CalculateReplicas(20, 100, topology, distribution)
	assert.NoError(t, err)
	assert.Len(t, result, 2)

	// First cluster gets all canary pods
	assert.Equal(t, int32(20), result["cluster-a"].CanaryReplicas)
	assert.Equal(t, int32(30), result["cluster-a"].StableReplicas)

	// Other clusters get no canary
	assert.Equal(t, int32(0), result["cluster-b"].CanaryReplicas)
	assert.Equal(t, int32(50), result["cluster-b"].StableReplicas)
}

func TestGlobalReplicaCalculator_CalculateReplicas_ProportionalDistribution(t *testing.T) {
	calculator := NewGlobalReplicaCalculator()

	topology := []appsv1alpha1.ClusterTopology{
		{Name: "cluster-a", Replicas: 30},
		{Name: "cluster-b", Replicas: 70},
	}

	// Global 20% canary = 20 pods
	// Proportional: A gets 30% of 20 = 6, B gets 70% of 20 = 14
	result, err := calculator.CalculateReplicas(20, 100, topology, nil)
	assert.NoError(t, err)
	assert.Len(t, result, 2)

	// Cluster A: 30% of total, gets 6 canary
	assert.Equal(t, int32(6), result["cluster-a"].CanaryReplicas)
	assert.Equal(t, int32(24), result["cluster-a"].StableReplicas)

	// Cluster B: 70% of total, gets 14 canary
	assert.Equal(t, int32(14), result["cluster-b"].CanaryReplicas)
	assert.Equal(t, int32(56), result["cluster-b"].StableReplicas)
}

func TestGlobalReplicaCalculator_CalculateReplicas_CapAtClusterTotal(t *testing.T) {
	calculator := NewGlobalReplicaCalculator()

	topology := []appsv1alpha1.ClusterTopology{
		{Name: "cluster-a", Replicas: 5},  // Small cluster
		{Name: "cluster-b", Replicas: 95}, // Large cluster
	}

	// Global 50% canary = 50 pods
	// Equal distribution would give 25 each, but cluster-a only has 5 pods
	distribution := &appsv1alpha1.GlobalReplicaDistribution{
		Mode: appsv1alpha1.DistributionModeEqual,
	}

	result, err := calculator.CalculateReplicas(50, 100, topology, distribution)
	assert.NoError(t, err)

	// Cluster A should be capped at 5 (its total replicas)
	assert.Equal(t, int32(5), result["cluster-a"].CanaryReplicas)
	assert.Equal(t, int32(0), result["cluster-a"].StableReplicas)

	// Cluster B gets the rest
	assert.LessOrEqual(t, result["cluster-b"].CanaryReplicas, int32(95))
}

func TestGlobalReplicaCalculator_CalculateGlobalProgress(t *testing.T) {
	calculator := NewGlobalReplicaCalculator()

	assignments := map[string]ClusterReplicaAssignment{
		"cluster-a": {CanaryReplicas: 10, TotalReplicas: 50},
		"cluster-b": {CanaryReplicas: 10, TotalReplicas: 50},
	}

	// Total canary: 20, total replicas: 100, should be 20%
	progress := calculator.CalculateGlobalProgress(assignments, 100)
	assert.Equal(t, int32(20), progress)
}

func TestGlobalReplicaCalculator_CalculateReplicas_EmptyTopology(t *testing.T) {
	calculator := NewGlobalReplicaCalculator()

	_, err := calculator.CalculateReplicas(20, 100, []appsv1alpha1.ClusterTopology{}, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "topology cannot be empty")
}

func TestGlobalReplicaCalculator_CalculateReplicas_ThreeClusters(t *testing.T) {
	calculator := NewGlobalReplicaCalculator()

	topology := []appsv1alpha1.ClusterTopology{
		{Name: "cluster-a", Replicas: 40},
		{Name: "cluster-b", Replicas: 30},
		{Name: "cluster-c", Replicas: 30},
	}

	// Global 30% canary = 30 pods
	// Equal: 10 per cluster
	distribution := &appsv1alpha1.GlobalReplicaDistribution{
		Mode: appsv1alpha1.DistributionModeEqual,
	}

	result, err := calculator.CalculateReplicas(30, 100, topology, distribution)
	assert.NoError(t, err)
	assert.Len(t, result, 3)

	// Each cluster should get 10 canary pods
	assert.Equal(t, int32(10), result["cluster-a"].CanaryReplicas)
	assert.Equal(t, int32(30), result["cluster-a"].StableReplicas)
	assert.Equal(t, int32(10), result["cluster-b"].CanaryReplicas)
	assert.Equal(t, int32(20), result["cluster-b"].StableReplicas)
	assert.Equal(t, int32(10), result["cluster-c"].CanaryReplicas)
	assert.Equal(t, int32(20), result["cluster-c"].StableReplicas)
}
