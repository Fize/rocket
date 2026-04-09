package application

import (
	"context"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	appsv1alpha1 "github.com/fize/rocket/pkg/apis/apps/v1alpha1"
)

// RolloutStatusAggregator aggregates rollout status from multiple clusters
type RolloutStatusAggregator struct {
	ClientManager clientManager
}

// NewRolloutStatusAggregator creates a new RolloutStatusAggregator
func NewRolloutStatusAggregator(cm clientManager) *RolloutStatusAggregator {
	return &RolloutStatusAggregator{
		ClientManager: cm,
	}
}

// AggregateRolloutStatus collects rollout status from all clusters and updates Application status
func (a *RolloutStatusAggregator) AggregateRolloutStatus(ctx context.Context, app *appsv1alpha1.Application, clusters []string) error {
	logger := log.FromContext(ctx)

	if app.Spec.RolloutStrategy == nil {
		logger.V(1).Info("No rollout strategy, skipping status aggregation")
		return nil
	}

	for _, clusterName := range clusters {
		targetClient, err := a.ClientManager.GetClient(ctx, clusterName)
		if err != nil {
			logger.Error(err, "Failed to get client for cluster", "cluster", clusterName)
			continue
		}

		rolloutStatus, err := a.getRolloutStatus(ctx, targetClient, app)
		if err != nil {
			logger.Error(err, "Failed to get rollout status", "cluster", clusterName)
			continue
		}

		// Update Application status for this cluster
		a.updateClusterRolloutStatus(app, clusterName, rolloutStatus)
		logger.V(1).Info("Rollout status aggregated", "cluster", clusterName, "phase", rolloutStatus.Phase)
	}

	return nil
}

// getRolloutStatus retrieves rollout status from a cluster
func (a *RolloutStatusAggregator) getRolloutStatus(ctx context.Context, cli client.Client, app *appsv1alpha1.Application) (*appsv1alpha1.RolloutStatus, error) {
	rollout := &unstructured.Unstructured{}
	rollout.SetAPIVersion("rollouts.kruise.io/v1alpha1")
	rollout.SetKind("Rollout")
	rollout.SetName(app.Name)
	rollout.SetNamespace(app.Namespace)

	if err := cli.Get(ctx, client.ObjectKey{Name: app.Name, Namespace: app.Namespace}, rollout); err != nil {
		return nil, err
	}

	return a.extractRolloutStatus(rollout), nil
}

// extractRolloutStatus extracts rollout status from unstructured object
func (a *RolloutStatusAggregator) extractRolloutStatus(rollout *unstructured.Unstructured) *appsv1alpha1.RolloutStatus {
	status := &appsv1alpha1.RolloutStatus{}

	// Extract phase
	if phase, found, _ := unstructured.NestedString(rollout.Object, "status", "phase"); found {
		status.Phase = appsv1alpha1.RolloutStatusPhase(phase)
	}

	// Extract message
	if message, found, _ := unstructured.NestedString(rollout.Object, "status", "message"); found {
		status.Message = message
	}

	// Extract current step
	if currentStep, found, _ := unstructured.NestedInt64(rollout.Object, "status", "currentStepIndex"); found {
		status.CurrentStep = int32(currentStep)
	}

	// Extract current step weight
	if weight, found, _ := unstructured.NestedInt64(rollout.Object, "status", "currentStepWeight"); found {
		status.CurrentStepWeight = int32(weight)
	}

	// Extract stable replicas
	if stableReplicas, found, _ := unstructured.NestedInt64(rollout.Object, "status", "stableReplicas"); found {
		status.StableReplicas = int32(stableReplicas)
	}

	// Extract canary replicas
	if canaryReplicas, found, _ := unstructured.NestedInt64(rollout.Object, "status", "canaryReplicas"); found {
		status.CanaryReplicas = int32(canaryReplicas)
	}

	// Extract updated replicas
	if updatedReplicas, found, _ := unstructured.NestedInt64(rollout.Object, "status", "updatedReplicas"); found {
		status.UpdatedReplicas = int32(updatedReplicas)
	}

	// Extract ready replicas
	if readyReplicas, found, _ := unstructured.NestedInt64(rollout.Object, "status", "readyReplicas"); found {
		status.ReadyReplicas = int32(readyReplicas)
	}

	return status
}

// updateClusterRolloutStatus updates the rollout status for a specific cluster in Application status
func (a *RolloutStatusAggregator) updateClusterRolloutStatus(app *appsv1alpha1.Application, clusterName string, rolloutStatus *appsv1alpha1.RolloutStatus) {
	// Find the cluster status and update
	for i := range app.Status.ClustersStatus {
		if app.Status.ClustersStatus[i].ClusterName == clusterName {
			app.Status.ClustersStatus[i].Rollout = rolloutStatus
			return
		}
	}

	// If cluster not found in status, add it (this shouldn't normally happen)
	app.Status.ClustersStatus = append(app.Status.ClustersStatus, appsv1alpha1.ClusterStatus{
		ClusterName: clusterName,
		Rollout:     rolloutStatus,
	})
}
