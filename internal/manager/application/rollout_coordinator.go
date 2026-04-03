package application

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	appsv1alpha1 "github.com/hex-techs/rocket/pkg/apis/apps/v1alpha1"
)

// RolloutCoordinator coordinates rollout across multiple clusters
type RolloutCoordinator struct {
	ClientManager clientManager
	Builder       *RolloutBuilder
	Calculator    *GlobalReplicaCalculator
}

type clientManager interface {
	GetClient(ctx context.Context, clusterName string) (client.Client, error)
}

// NewRolloutCoordinator creates a new RolloutCoordinator
func NewRolloutCoordinator(cm clientManager) *RolloutCoordinator {
	return &RolloutCoordinator{
		ClientManager: cm,
		Builder:       NewRolloutBuilder(),
		Calculator:    NewGlobalReplicaCalculator(),
	}
}

// ReconcileRollout reconciles rollout resources across clusters
// It calculates global replica distribution and applies Rollout CR to each cluster
func (c *RolloutCoordinator) ReconcileRollout(ctx context.Context, app *appsv1alpha1.Application, topology []appsv1alpha1.ClusterTopology) error {
	logger := log.FromContext(ctx)

	if app.Spec.RolloutStrategy == nil {
		logger.V(1).Info("No rollout strategy, skipping")
		return nil
	}

	strategy := app.Spec.RolloutStrategy

	// Get total replicas across all clusters
	var totalReplicas int32
	for _, t := range topology {
		totalReplicas += t.Replicas
	}

	// Determine rollout order based on ClusterOrder
	var orderedClusters []string
	if strategy.ClusterOrder != nil && strategy.ClusterOrder.Type == appsv1alpha1.ClusterOrderSequential {
		// Sequential rollout: respect the order in topology
		for _, t := range topology {
			orderedClusters = append(orderedClusters, t.Name)
		}
	} else {
		// Parallel rollout: all clusters at once
		for _, t := range topology {
			orderedClusters = append(orderedClusters, t.Name)
		}
	}

	// For Canary strategy with GlobalReplicaDistribution, calculate per-cluster replicas
	var clusterAssignments map[string]ClusterReplicaAssignment
	if strategy.Type == appsv1alpha1.RolloutTypeCanary && strategy.Canary != nil && strategy.Canary.GlobalReplicaDistribution != nil {
		// Get current step percentage from Application status or use first step
		currentPercent := c.getCurrentStepPercent(app)

		// Calculate replica distribution
		var err error
		clusterAssignments, err = c.Calculator.CalculateReplicas(
			currentPercent,
			totalReplicas,
			topology,
			strategy.Canary.GlobalReplicaDistribution,
		)
		if err != nil {
			return fmt.Errorf("failed to calculate replica distribution: %w", err)
		}

		logger.Info("Global replica distribution calculated", "totalReplicas", totalReplicas, "currentPercent", currentPercent)
		for clusterName, assignment := range clusterAssignments {
			logger.Info("Cluster assignment", "cluster", clusterName, "canary", assignment.CanaryReplicas, "stable", assignment.StableReplicas)
		}
	}

	// Process each cluster according to rollout strategy
	for i, clusterName := range orderedClusters {
		logger := logger.WithValues("cluster", clusterName, "index", i)

		// For sequential rollout, check if previous cluster is ready
		if strategy.ClusterOrder != nil && strategy.ClusterOrder.Type == appsv1alpha1.ClusterOrderSequential && i > 0 {
			prevCluster := orderedClusters[i-1]
			if !c.isRolloutComplete(ctx, app, prevCluster) {
				logger.Info("Pausing sequential rollout: previous cluster not complete", "previous", prevCluster)
				continue
			}
		}

		// Get client for this cluster
		targetClient, err := c.ClientManager.GetClient(ctx, clusterName)
		if err != nil {
			logger.Error(err, "Failed to get client for cluster")
			continue
		}

		// Get assignment for this cluster
		var assignment *ClusterReplicaAssignment
		if clusterAssignments != nil {
			if a, ok := clusterAssignments[clusterName]; ok {
				assignment = &a
			}
		}

		// Build Rollout resource
		rollout, err := c.Builder.BuildRollout(app, clusterName, assignment)
		if err != nil {
			logger.Error(err, "Failed to build rollout resource")
			continue
		}

		// Apply Rollout to cluster
		if err := c.applyRollout(ctx, targetClient, rollout); err != nil {
			logger.Error(err, "Failed to apply rollout")
			continue
		}

		logger.Info("Rollout applied successfully", "cluster", clusterName)
	}

	return nil
}

// getCurrentStepPercent gets the current step percentage from Application status
// If no status is available, it returns the first step percentage
func (c *RolloutCoordinator) getCurrentStepPercent(app *appsv1alpha1.Application) int32 {
	// Check if we have current step info in status
	for _, status := range app.Status.ClustersStatus {
		if status.Rollout != nil && status.Rollout.CurrentStepWeight > 0 {
			return status.Rollout.CurrentStepWeight
		}
	}

	// Default to first step weight
	if app.Spec.RolloutStrategy != nil && app.Spec.RolloutStrategy.Canary != nil && len(app.Spec.RolloutStrategy.Canary.Steps) > 0 {
		return app.Spec.RolloutStrategy.Canary.Steps[0].Weight
	}

	return 0
}

// isRolloutComplete checks if rollout is complete in a specific cluster
func (c *RolloutCoordinator) isRolloutComplete(ctx context.Context, app *appsv1alpha1.Application, clusterName string) bool {
	// Check from Application status
	for _, status := range app.Status.ClustersStatus {
		if status.ClusterName == clusterName && status.Rollout != nil {
			return status.Rollout.Phase == appsv1alpha1.RolloutPhaseSucceeded || status.Rollout.Phase == appsv1alpha1.RolloutPhaseInitial
		}
	}
	return false
}

// applyRollout creates or updates a Rollout resource in the target cluster
func (c *RolloutCoordinator) applyRollout(ctx context.Context, cli client.Client, rollout client.Object) error {
	// Try to create, if already exists, update
	if err := cli.Create(ctx, rollout); err != nil {
		if errors.IsAlreadyExists(err) {
			// Update existing
			if err := cli.Update(ctx, rollout); err != nil {
				return fmt.Errorf("failed to update rollout: %w", err)
			}
			return nil
		}
		return fmt.Errorf("failed to create rollout: %w", err)
	}
	return nil
}

// DeleteRollout deletes rollout resources from all clusters
func (c *RolloutCoordinator) DeleteRollout(ctx context.Context, app *appsv1alpha1.Application, clusters []string) error {
	logger := log.FromContext(ctx)

	for _, clusterName := range clusters {
		targetClient, err := c.ClientManager.GetClient(ctx, clusterName)
		if err != nil {
			logger.Error(err, "Failed to get client for cluster during rollout deletion", "cluster", clusterName)
			continue
		}

		rollout := c.Builder.BuildEmptyRollout(app)
		if err := targetClient.Delete(ctx, rollout); err != nil && !errors.IsNotFound(err) {
			logger.Error(err, "Failed to delete rollout", "cluster", clusterName)
			continue
		}

		logger.Info("Rollout deleted", "cluster", clusterName)
	}

	return nil
}

// getRolloutGVR returns the GroupVersionResource for Rollout
func getRolloutGVR() schema.GroupVersionResource {
	return schema.GroupVersionResource{
		Group:    "rollouts.kruise.io",
		Version:  "v1alpha1",
		Resource: "rollouts",
	}
}
