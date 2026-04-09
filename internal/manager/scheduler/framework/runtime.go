package framework

import (
	"context"

	appsv1alpha1 "github.com/fize/rocket/pkg/apis/apps/v1alpha1"
	clusterv1alpha1 "github.com/fize/rocket/pkg/apis/storage/v1alpha1"
)

type frameworkImpl struct {
	filterPlugins []FilterPlugin
	scorePlugins  []ScorePlugin
	pluginWeights map[string]int64
}

var _ Framework = &frameworkImpl{}

func NewFramework(filterPlugins []FilterPlugin, scorePlugins []ScorePlugin) Framework {
	return &frameworkImpl{
		filterPlugins: filterPlugins,
		scorePlugins:  scorePlugins,
		pluginWeights: make(map[string]int64),
	}
}

// NewFrameworkWithConfig creates a framework with plugin weights configuration
func NewFrameworkWithConfig(filterPlugins []FilterPlugin, scorePlugins []ScorePlugin, config *SchedulerConfig) Framework {
	fw := &frameworkImpl{
		filterPlugins: filterPlugins,
		scorePlugins:  scorePlugins,
		pluginWeights: make(map[string]int64),
	}

	// Set weights from config
	if config != nil {
		for _, pc := range config.ScorePlugins {
			if pc.Weight > 0 {
				fw.pluginWeights[pc.Name] = pc.Weight
			} else {
				fw.pluginWeights[pc.Name] = 1 // default weight
			}
		}
	}

	return fw
}

func (f *frameworkImpl) RunFilterPlugins(ctx context.Context, state *CycleState, app *appsv1alpha1.Application, cluster *clusterv1alpha1.ManagedCluster) *Status {
	for _, pl := range f.filterPlugins {
		status := pl.Filter(ctx, state, app, cluster)
		if !status.IsSuccess() {
			return status
		}
	}
	return NewStatus(Success, "")
}

func (f *frameworkImpl) RunScorePlugins(ctx context.Context, state *CycleState, app *appsv1alpha1.Application, clusters []*clusterv1alpha1.ManagedCluster) (map[string]int64, *Status) {
	// Initialize scores for all clusters
	finalScores := make(map[string]int64)
	for _, cluster := range clusters {
		finalScores[cluster.Name] = 0
	}

	// Run each plugin and accumulate weighted scores
	for _, pl := range f.scorePlugins {
		pluginScores := make(map[string]int64)

		// Score each cluster
		for _, cluster := range clusters {
			score, status := pl.Score(ctx, state, app, cluster)
			if !status.IsSuccess() {
				return nil, status
			}
			pluginScores[cluster.Name] = score
		}

		// Normalize if the plugin supports it
		if pl.ScoreExtensions() != nil {
			status := pl.ScoreExtensions().NormalizeScore(ctx, state, app, pluginScores)
			if !status.IsSuccess() {
				return nil, status
			}
		}

		// Apply weight and add to final scores
		weight := f.pluginWeights[pl.Name()]
		if weight == 0 {
			weight = 1 // default weight
		}
		for clusterName, score := range pluginScores {
			finalScores[clusterName] += score * weight
		}
	}

	// Final normalization: scale all scores to 0-100 range
	// This ensures fairness regardless of the number of plugins or weight configurations
	f.normalizeFinalScores(finalScores)

	return finalScores, NewStatus(Success, "")
}

// normalizeFinalScores normalizes the final weighted scores to 0-100 range.
// This helps ensure consistent scoring behavior and fair comparison between clusters.
func (f *frameworkImpl) normalizeFinalScores(scores map[string]int64) {
	if len(scores) == 0 {
		return
	}

	// Find min and max scores
	var minScore, maxScore int64
	first := true
	for _, score := range scores {
		if first {
			minScore = score
			maxScore = score
			first = false
		} else {
			if score < minScore {
				minScore = score
			}
			if score > maxScore {
				maxScore = score
			}
		}
	}

	// If all scores are the same, set them all to 50 (neutral)
	if maxScore == minScore {
		for cluster := range scores {
			scores[cluster] = 50
		}
		return
	}

	// Normalize to 0-100 range
	scoreRange := maxScore - minScore
	for cluster, score := range scores {
		// Shift to start from 0, then scale to 0-100
		scores[cluster] = (score - minScore) * 100 / scoreRange
	}
}
