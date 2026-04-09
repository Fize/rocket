package affinity

import (
	"context"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"

	"github.com/fize/rocket/internal/manager/scheduler/framework"
	appsv1alpha1 "github.com/fize/rocket/pkg/apis/apps/v1alpha1"
	clusterv1alpha1 "github.com/fize/rocket/pkg/apis/storage/v1alpha1"
)

const Name = "Affinity"

type Affinity struct{}

var _ framework.FilterPlugin = &Affinity{}
var _ framework.ScorePlugin = &Affinity{}

func New() framework.Plugin {
	return &Affinity{}
}

func (pl *Affinity) Name() string {
	return Name
}

func (pl *Affinity) Filter(ctx context.Context, state *framework.CycleState, app *appsv1alpha1.Application, cluster *clusterv1alpha1.ManagedCluster) *framework.Status {
	affinity := app.Spec.ClusterAffinity
	if affinity == nil || affinity.RequiredDuringSchedulingIgnoredDuringExecution == nil {
		return framework.NewStatus(framework.Success, "")
	}

	// Convert cluster labels to label selector for matching
	clusterLabels := labels.Set(cluster.Labels)

	// Check if cluster matches any of the node selector terms
	for _, term := range affinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms {
		if matchNodeSelectorTerm(term, clusterLabels) {
			return framework.NewStatus(framework.Success, "")
		}
	}

	return framework.NewStatus(framework.Unschedulable, "cluster affinity mismatch")
}

func matchNodeSelectorTerm(term v1.NodeSelectorTerm, clusterLabels labels.Set) bool {
	// Check MatchExpressions
	for _, expr := range term.MatchExpressions {
		if !matchExpression(expr, clusterLabels) {
			return false
		}
	}

	// Check MatchFields (typically not used for cluster scheduling)
	for _, expr := range term.MatchFields {
		if !matchExpression(expr, clusterLabels) {
			return false
		}
	}

	return true
}

func matchExpression(expr v1.NodeSelectorRequirement, clusterLabels labels.Set) bool {
	value, exists := clusterLabels[expr.Key]

	switch expr.Operator {
	case v1.NodeSelectorOpIn:
		if !exists {
			return false
		}
		for _, v := range expr.Values {
			if v == value {
				return true
			}
		}
		return false
	case v1.NodeSelectorOpNotIn:
		if !exists {
			return true
		}
		for _, v := range expr.Values {
			if v == value {
				return false
			}
		}
		return true
	case v1.NodeSelectorOpExists:
		return exists
	case v1.NodeSelectorOpDoesNotExist:
		return !exists
	case v1.NodeSelectorOpGt:
		// Not typically used for labels, but we can implement basic string comparison
		if !exists || len(expr.Values) == 0 {
			return false
		}
		return value > expr.Values[0]
	case v1.NodeSelectorOpLt:
		if !exists || len(expr.Values) == 0 {
			return false
		}
		return value < expr.Values[0]
	default:
		return false
	}
}

func (pl *Affinity) Score(ctx context.Context, state *framework.CycleState, app *appsv1alpha1.Application, cluster *clusterv1alpha1.ManagedCluster) (int64, *framework.Status) {
	affinity := app.Spec.ClusterAffinity
	if affinity == nil || len(affinity.PreferredDuringSchedulingIgnoredDuringExecution) == 0 {
		return 0, framework.NewStatus(framework.Success, "")
	}

	clusterLabels := labels.Set(cluster.Labels)
	var score int64 = 0

	for _, preferred := range affinity.PreferredDuringSchedulingIgnoredDuringExecution {
		if matchNodeSelectorTerm(preferred.Preference, clusterLabels) {
			score += int64(preferred.Weight)
		}
	}

	return score, framework.NewStatus(framework.Success, "")
}

func (pl *Affinity) ScoreExtensions() framework.ScoreExtensions {
	return pl
}

// NormalizeScore normalizes affinity scores to 0-100 range
func (pl *Affinity) NormalizeScore(ctx context.Context, state *framework.CycleState, app *appsv1alpha1.Application, scores map[string]int64) *framework.Status {
	// Find max score to normalize
	var maxScore int64 = 0
	for _, score := range scores {
		if score > maxScore {
			maxScore = score
		}
	}

	// If all scores are 0, no normalization needed
	if maxScore == 0 {
		return framework.NewStatus(framework.Success, "")
	}

	// Normalize to 0-100 range
	for cluster, score := range scores {
		scores[cluster] = score * 100 / maxScore
	}

	return framework.NewStatus(framework.Success, "")
}
