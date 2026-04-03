package application

import (
	"fmt"
	"math"

	appsv1alpha1 "github.com/hex-techs/rocket/pkg/apis/apps/v1alpha1"
)

// GlobalReplicaCalculator calculates replica distribution across clusters
type GlobalReplicaCalculator struct{}

// NewGlobalReplicaCalculator creates a new GlobalReplicaCalculator
func NewGlobalReplicaCalculator() *GlobalReplicaCalculator {
	return &GlobalReplicaCalculator{}
}

// ClusterReplicaAssignment represents the replica assignment for a cluster
type ClusterReplicaAssignment struct {
	// ClusterName is the name of the cluster
	ClusterName string
	// TotalReplicas is the total desired replicas for this cluster
	TotalReplicas int32
	// CanaryReplicas is the number of canary replicas for this cluster
	CanaryReplicas int32
	// StableReplicas is the number of stable replicas for this cluster
	StableReplicas int32
	// ClusterIndex is the index of this cluster in the topology
	ClusterIndex int
}

// CalculateReplicas calculates replica distribution across clusters based on global canary percentage
// globalPercent: the global percentage of pods to update (e.g., 20 means 20% globally)
// totalReplicas: total desired replicas across all clusters
// topology: cluster topology with replica distribution
// distribution: global replica distribution configuration
// Returns a map of cluster name to its replica assignment
func (c *GlobalReplicaCalculator) CalculateReplicas(
	globalPercent int32,
	totalReplicas int32,
	topology []appsv1alpha1.ClusterTopology,
	distribution *appsv1alpha1.GlobalReplicaDistribution,
) (map[string]ClusterReplicaAssignment, error) {
	if len(topology) == 0 {
		return nil, fmt.Errorf("topology cannot be empty")
	}

	// Calculate global canary replicas
	globalCanaryReplicas := int32(math.Ceil(float64(totalReplicas) * float64(globalPercent) / 100.0))

	// If distribution is not specified, fall back to proportional distribution
	if distribution == nil {
		return c.calculateProportional(globalCanaryReplicas, topology), nil
	}

	// Calculate based on distribution mode
	switch distribution.Mode {
	case appsv1alpha1.DistributionModeEqual:
		return c.calculateEqual(globalCanaryReplicas, topology), nil
	case appsv1alpha1.DistributionModeWeighted:
		return c.calculateWeighted(globalCanaryReplicas, topology, distribution.ClusterWeights)
	case appsv1alpha1.DistributionModeSequential:
		return c.calculateSequential(globalCanaryReplicas, topology)
	default:
		return nil, fmt.Errorf("unknown distribution mode: %s", distribution.Mode)
	}
}

// calculateProportional distributes canary pods proportionally based on cluster replica counts
// This is the default behavior when GlobalReplicaDistribution is not specified
func (c *GlobalReplicaCalculator) calculateProportional(
	globalCanaryReplicas int32,
	topology []appsv1alpha1.ClusterTopology,
) map[string]ClusterReplicaAssignment {
	result := make(map[string]ClusterReplicaAssignment)

	// Calculate total replicas
	var totalReplicas int32
	for _, t := range topology {
		totalReplicas += t.Replicas
	}

	// Distribute proportionally
	remainingCanary := globalCanaryReplicas
	for i, t := range topology {
		clusterCanary := int32(math.Ceil(float64(globalCanaryReplicas) * float64(t.Replicas) / float64(totalReplicas)))
		if i == len(topology)-1 {
			// Last cluster gets remaining to avoid rounding issues
			clusterCanary = remainingCanary
		}
		if clusterCanary > t.Replicas {
			clusterCanary = t.Replicas
		}
		remainingCanary -= clusterCanary

		result[t.Name] = ClusterReplicaAssignment{
			ClusterName:    t.Name,
			TotalReplicas:  t.Replicas,
			CanaryReplicas: clusterCanary,
			StableReplicas: t.Replicas - clusterCanary,
			ClusterIndex:   i,
		}
	}

	return result
}

// calculateEqual distributes canary pods equally across all clusters
func (c *GlobalReplicaCalculator) calculateEqual(
	globalCanaryReplicas int32,
	topology []appsv1alpha1.ClusterTopology,
) map[string]ClusterReplicaAssignment {
	result := make(map[string]ClusterReplicaAssignment)

	// Calculate canary replicas per cluster
	canaryPerCluster := globalCanaryReplicas / int32(len(topology))
	remaining := globalCanaryReplicas % int32(len(topology))

	for i, t := range topology {
		clusterCanary := canaryPerCluster
		// Distribute remainder to first few clusters
		if i < int(remaining) {
			clusterCanary++
		}
		// Cap at cluster's total replicas
		if clusterCanary > t.Replicas {
			clusterCanary = t.Replicas
		}

		result[t.Name] = ClusterReplicaAssignment{
			ClusterName:    t.Name,
			TotalReplicas:  t.Replicas,
			CanaryReplicas: clusterCanary,
			StableReplicas: t.Replicas - clusterCanary,
			ClusterIndex:   i,
		}
	}

	return result
}

// calculateWeighted distributes canary pods based on cluster weights
func (c *GlobalReplicaCalculator) calculateWeighted(
	globalCanaryReplicas int32,
	topology []appsv1alpha1.ClusterTopology,
	weights []appsv1alpha1.ClusterReplicaWeight,
) (map[string]ClusterReplicaAssignment, error) {
	result := make(map[string]ClusterReplicaAssignment)

	// Build weight map
	weightMap := make(map[string]int32)
	var totalWeight int32
	for _, w := range weights {
		weightMap[w.ClusterName] = w.Weight
		totalWeight += w.Weight
	}

	// If no weights specified, fall back to equal distribution
	if totalWeight == 0 {
		return c.calculateEqual(globalCanaryReplicas, topology), nil
	}

	// Distribute based on weights
	remainingCanary := globalCanaryReplicas
	for i, t := range topology {
		weight := weightMap[t.Name]
		clusterCanary := int32(math.Ceil(float64(globalCanaryReplicas) * float64(weight) / float64(totalWeight)))
		if i == len(topology)-1 {
			clusterCanary = remainingCanary
		}
		// Cap at cluster's total replicas
		if clusterCanary > t.Replicas {
			clusterCanary = t.Replicas
		}
		remainingCanary -= clusterCanary

		result[t.Name] = ClusterReplicaAssignment{
			ClusterName:    t.Name,
			TotalReplicas:  t.Replicas,
			CanaryReplicas: clusterCanary,
			StableReplicas: t.Replicas - clusterCanary,
			ClusterIndex:   i,
		}
	}

	return result, nil
}

// calculateSequential assigns all canary pods to the first cluster in sequence
// This is used for sequential rollout mode
func (c *GlobalReplicaCalculator) calculateSequential(
	globalCanaryReplicas int32,
	topology []appsv1alpha1.ClusterTopology,
) (map[string]ClusterReplicaAssignment, error) {
	result := make(map[string]ClusterReplicaAssignment)

	for i, t := range topology {
		var clusterCanary int32
		var clusterStable int32

		if i == 0 {
			// First cluster gets all canary pods
			clusterCanary = globalCanaryReplicas
			if clusterCanary > t.Replicas {
				clusterCanary = t.Replicas
			}
			clusterStable = t.Replicas - clusterCanary
		} else {
			// Other clusters get no canary pods yet
			clusterCanary = 0
			clusterStable = t.Replicas
		}

		result[t.Name] = ClusterReplicaAssignment{
			ClusterName:    t.Name,
			TotalReplicas:  t.Replicas,
			CanaryReplicas: clusterCanary,
			StableReplicas: clusterStable,
			ClusterIndex:   i,
		}
	}

	return result, nil
}

// CalculateGlobalProgress calculates the global progress percentage across all clusters
// This is used to determine when to move to the next step
func (c *GlobalReplicaCalculator) CalculateGlobalProgress(
	assignments map[string]ClusterReplicaAssignment,
	totalReplicas int32,
) int32 {
	var updatedReplicas int32
	for _, assignment := range assignments {
		updatedReplicas += assignment.CanaryReplicas
	}

	if totalReplicas == 0 {
		return 0
	}
	return int32(float64(updatedReplicas) * 100.0 / float64(totalReplicas))
}
