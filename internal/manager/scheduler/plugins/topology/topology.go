package topology

import (
	"context"

	"github.com/fize/rocket/internal/manager/scheduler/framework"
	appsv1alpha1 "github.com/fize/rocket/pkg/apis/apps/v1alpha1"
	clusterv1alpha1 "github.com/fize/rocket/pkg/apis/storage/v1alpha1"
)

const Name = "TopologySpread"

// TopologySpread is a plugin that favors spreading replicas across different topology domains.
type TopologySpread struct {
	topologyKey string
}

var _ framework.ScorePlugin = &TopologySpread{}

// New creates a new TopologySpread plugin with default topology key.
func New() framework.Plugin {
	return &TopologySpread{
		topologyKey: "topology.kubernetes.io/zone",
	}
}

// NewWithTopologyKey creates a new TopologySpread plugin with custom topology key.
func NewWithTopologyKey(key string) framework.Plugin {
	return &TopologySpread{
		topologyKey: key,
	}
}

func (pl *TopologySpread) Name() string {
	return Name
}

// Score favors clusters in topology domains with fewer replicas.
func (pl *TopologySpread) Score(ctx context.Context, state *framework.CycleState, app *appsv1alpha1.Application, cluster *clusterv1alpha1.ManagedCluster) (int64, *framework.Status) {
	topologyValue, hasTopology := cluster.Labels[pl.topologyKey]
	if !hasTopology {
		return 50, framework.NewStatus(framework.Success, "")
	}

	topologyDistribution, ok := state.Read(topologyDistributionKey)
	if !ok {
		return 50, framework.NewStatus(framework.Success, "")
	}

	dist, ok := topologyDistribution.(map[string]int32)
	if !ok {
		return 50, framework.NewStatus(framework.Success, "")
	}

	currentReplicas := dist[topologyValue]
	return int64(currentReplicas), framework.NewStatus(framework.Success, "")
}

func (pl *TopologySpread) ScoreExtensions() framework.ScoreExtensions {
	return pl
}

// NormalizeScore inverts the scores so that topology domains with fewer replicas get higher scores.
func (pl *TopologySpread) NormalizeScore(ctx context.Context, state *framework.CycleState, app *appsv1alpha1.Application, scores map[string]int64) *framework.Status {
	var maxReplicas int64 = 0
	for _, score := range scores {
		if score > maxReplicas {
			maxReplicas = score
		}
	}

	if maxReplicas == 0 {
		for cluster := range scores {
			scores[cluster] = 100
		}
		return framework.NewStatus(framework.Success, "")
	}

	for cluster, replicaCount := range scores {
		scores[cluster] = (maxReplicas - replicaCount) * 100 / maxReplicas
	}

	return framework.NewStatus(framework.Success, "")
}

const topologyDistributionKey = "TopologyDistribution"

// UpdateTopologyDistribution updates the topology distribution in the cycle state.
func UpdateTopologyDistribution(state *framework.CycleState, clusters []*clusterv1alpha1.ManagedCluster, placements []appsv1alpha1.ClusterTopology, topologyKey string) {
	distribution := make(map[string]int32)

	clusterTopology := make(map[string]string)
	for _, cluster := range clusters {
		if value, ok := cluster.Labels[topologyKey]; ok {
			clusterTopology[cluster.Name] = value
		}
	}

	for _, placement := range placements {
		if topology, ok := clusterTopology[placement.Name]; ok {
			distribution[topology] += placement.Replicas
		}
	}

	state.Write(topologyDistributionKey, distribution)
}
