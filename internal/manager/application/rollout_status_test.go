package application

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	appsv1alpha1 "github.com/hex-techs/rocket/pkg/apis/apps/v1alpha1"
	clusterv1alpha1 "github.com/hex-techs/rocket/pkg/apis/storage/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

func setupStatusTestScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	_ = appsv1alpha1.AddToScheme(scheme)
	_ = clusterv1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	return scheme
}

func TestNewRolloutStatusAggregator(t *testing.T) {
	cm := &mockClientManager{}
	aggregator := NewRolloutStatusAggregator(cm)

	assert.NotNil(t, aggregator)
	assert.Equal(t, cm, aggregator.ClientManager)
}

func TestRolloutStatusAggregator_AggregateRolloutStatus_NoStrategy(t *testing.T) {
	cm := &mockClientManager{}
	aggregator := NewRolloutStatusAggregator(cm)

	app := &appsv1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-app",
			Namespace: "default",
		},
		Spec: appsv1alpha1.ApplicationSpec{
			// No rollout strategy
		},
	}

	ctx := context.Background()
	err := aggregator.AggregateRolloutStatus(ctx, app, []string{"cluster-a"})

	assert.NoError(t, err)
}

func TestRolloutStatusAggregator_AggregateRolloutStatus_Success(t *testing.T) {
	scheme := setupStatusTestScheme()

	// Create a rollout in the cluster
	rollout := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "rollouts.kruise.io/v1alpha1",
			"kind":       "Rollout",
			"metadata": map[string]interface{}{
				"name":      "test-app",
				"namespace": "default",
			},
			"status": map[string]interface{}{
				"phase":               "Progressing",
				"message":             "Rollout in progress",
				"currentStepIndex":    int64(2),
				"currentStepWeight":   int64(50),
				"stableReplicas":      int64(5),
				"canaryReplicas":      int64(3),
				"updatedReplicas":     int64(3),
				"readyReplicas":       int64(8),
			},
		},
	}

	clusterClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(rollout).Build()

	cm := &mockClientManager{
		clients: map[string]client.Client{
			"cluster-a": clusterClient,
		},
	}
	aggregator := NewRolloutStatusAggregator(cm)

	app := &appsv1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-app",
			Namespace: "default",
		},
		Spec: appsv1alpha1.ApplicationSpec{
			RolloutStrategy: &appsv1alpha1.RolloutStrategy{
				Type: appsv1alpha1.RolloutTypeCanary,
				Canary: &appsv1alpha1.CanaryStrategy{
					Steps: []appsv1alpha1.CanaryStep{{Weight: 100}},
				},
			},
		},
		Status: appsv1alpha1.ApplicationStatus{
			ClustersStatus: []appsv1alpha1.ClusterStatus{
				{
					ClusterName: "cluster-a",
				},
			},
		},
	}

	ctx := context.Background()
	err := aggregator.AggregateRolloutStatus(ctx, app, []string{"cluster-a"})

	assert.NoError(t, err)
	assert.Len(t, app.Status.ClustersStatus, 1)
	assert.NotNil(t, app.Status.ClustersStatus[0].Rollout)
	assert.Equal(t, appsv1alpha1.RolloutPhaseProgressing, app.Status.ClustersStatus[0].Rollout.Phase)
	assert.Equal(t, "Rollout in progress", app.Status.ClustersStatus[0].Rollout.Message)
	assert.Equal(t, int32(2), app.Status.ClustersStatus[0].Rollout.CurrentStep)
	assert.Equal(t, int32(50), app.Status.ClustersStatus[0].Rollout.CurrentStepWeight)
	assert.Equal(t, int32(5), app.Status.ClustersStatus[0].Rollout.StableReplicas)
	assert.Equal(t, int32(3), app.Status.ClustersStatus[0].Rollout.CanaryReplicas)
	assert.Equal(t, int32(3), app.Status.ClustersStatus[0].Rollout.UpdatedReplicas)
	assert.Equal(t, int32(8), app.Status.ClustersStatus[0].Rollout.ReadyReplicas)
}

func TestRolloutStatusAggregator_AggregateRolloutStatus_NotFound(t *testing.T) {
	scheme := setupStatusTestScheme()

	// No rollout in cluster
	clusterClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	cm := &mockClientManager{
		clients: map[string]client.Client{
			"cluster-a": clusterClient,
		},
	}
	aggregator := NewRolloutStatusAggregator(cm)

	app := &appsv1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-app",
			Namespace: "default",
		},
		Spec: appsv1alpha1.ApplicationSpec{
			RolloutStrategy: &appsv1alpha1.RolloutStrategy{
				Type: appsv1alpha1.RolloutTypeCanary,
				Canary: &appsv1alpha1.CanaryStrategy{
					Steps: []appsv1alpha1.CanaryStep{{Weight: 100}},
				},
			},
		},
		Status: appsv1alpha1.ApplicationStatus{
			ClustersStatus: []appsv1alpha1.ClusterStatus{
				{
					ClusterName: "cluster-a",
				},
			},
		},
	}

	ctx := context.Background()
	err := aggregator.AggregateRolloutStatus(ctx, app, []string{"cluster-a"})

	// Should not error when rollout not found, just log and continue
	assert.NoError(t, err)
}

func TestRolloutStatusAggregator_AggregateRolloutStatus_ClientError(t *testing.T) {
	cm := &mockClientManager{
		err: fmt.Errorf("failed to get cluster client"),
	}
	aggregator := NewRolloutStatusAggregator(cm)

	app := &appsv1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-app",
			Namespace: "default",
		},
		Spec: appsv1alpha1.ApplicationSpec{
			RolloutStrategy: &appsv1alpha1.RolloutStrategy{
				Type: appsv1alpha1.RolloutTypeCanary,
				Canary: &appsv1alpha1.CanaryStrategy{
					Steps: []appsv1alpha1.CanaryStep{{Weight: 100}},
				},
			},
		},
	}

	ctx := context.Background()
	err := aggregator.AggregateRolloutStatus(ctx, app, []string{"cluster-a"})

	// Should not error, just log and continue
	assert.NoError(t, err)
}

func TestRolloutStatusAggregator_AggregateRolloutStatus_MultipleClusters(t *testing.T) {
	scheme := setupStatusTestScheme()

	// Create rollouts in multiple clusters
	rolloutA := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "rollouts.kruise.io/v1alpha1",
			"kind":       "Rollout",
			"metadata": map[string]interface{}{
				"name":      "test-app",
				"namespace": "default",
			},
			"status": map[string]interface{}{
				"phase": "Succeeded",
			},
		},
	}

	rolloutB := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "rollouts.kruise.io/v1alpha1",
			"kind":       "Rollout",
			"metadata": map[string]interface{}{
				"name":      "test-app",
				"namespace": "default",
			},
			"status": map[string]interface{}{
				"phase": "Progressing",
			},
		},
	}

	clusterAClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(rolloutA).Build()
	clusterBClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(rolloutB).Build()

	cm := &mockClientManager{
		clients: map[string]client.Client{
			"cluster-a": clusterAClient,
			"cluster-b": clusterBClient,
		},
	}
	aggregator := NewRolloutStatusAggregator(cm)

	app := &appsv1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-app",
			Namespace: "default",
		},
		Spec: appsv1alpha1.ApplicationSpec{
			RolloutStrategy: &appsv1alpha1.RolloutStrategy{
				Type: appsv1alpha1.RolloutTypeCanary,
				Canary: &appsv1alpha1.CanaryStrategy{
					Steps: []appsv1alpha1.CanaryStep{{Weight: 100}},
				},
			},
		},
		Status: appsv1alpha1.ApplicationStatus{
			ClustersStatus: []appsv1alpha1.ClusterStatus{
				{ClusterName: "cluster-a"},
				{ClusterName: "cluster-b"},
			},
		},
	}

	ctx := context.Background()
	err := aggregator.AggregateRolloutStatus(ctx, app, []string{"cluster-a", "cluster-b"})

	assert.NoError(t, err)
	assert.Len(t, app.Status.ClustersStatus, 2)

	// Verify cluster-a status
	var clusterAStatus *appsv1alpha1.ClusterStatus
	var clusterBStatus *appsv1alpha1.ClusterStatus
	for i := range app.Status.ClustersStatus {
		if app.Status.ClustersStatus[i].ClusterName == "cluster-a" {
			clusterAStatus = &app.Status.ClustersStatus[i]
		}
		if app.Status.ClustersStatus[i].ClusterName == "cluster-b" {
			clusterBStatus = &app.Status.ClustersStatus[i]
		}
	}

	assert.NotNil(t, clusterAStatus)
	assert.NotNil(t, clusterAStatus.Rollout)
	assert.Equal(t, appsv1alpha1.RolloutPhaseSucceeded, clusterAStatus.Rollout.Phase)

	assert.NotNil(t, clusterBStatus)
	assert.NotNil(t, clusterBStatus.Rollout)
	assert.Equal(t, appsv1alpha1.RolloutPhaseProgressing, clusterBStatus.Rollout.Phase)
}

func TestRolloutStatusAggregator_getRolloutStatus_Success(t *testing.T) {
	scheme := setupStatusTestScheme()

	rollout := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "rollouts.kruise.io/v1alpha1",
			"kind":       "Rollout",
			"metadata": map[string]interface{}{
				"name":      "test-app",
				"namespace": "default",
			},
			"status": map[string]interface{}{
				"phase": "Progressing",
			},
		},
	}

	clusterClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(rollout).Build()
	cm := &mockClientManager{}
	aggregator := NewRolloutStatusAggregator(cm)

	app := &appsv1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-app",
			Namespace: "default",
		},
	}

	ctx := context.Background()
	status, err := aggregator.getRolloutStatus(ctx, clusterClient, app)

	assert.NoError(t, err)
	assert.NotNil(t, status)
	assert.Equal(t, appsv1alpha1.RolloutPhaseProgressing, status.Phase)
}

func TestRolloutStatusAggregator_getRolloutStatus_NotFound(t *testing.T) {
	scheme := setupStatusTestScheme()

	clusterClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	cm := &mockClientManager{}
	aggregator := NewRolloutStatusAggregator(cm)

	app := &appsv1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-app",
			Namespace: "default",
		},
	}

	ctx := context.Background()
	status, err := aggregator.getRolloutStatus(ctx, clusterClient, app)

	assert.Error(t, err)
	assert.True(t, errors.IsNotFound(err))
	assert.Nil(t, status)
}

func TestRolloutStatusAggregator_extractRolloutStatus_AllFields(t *testing.T) {
	cm := &mockClientManager{}
	aggregator := NewRolloutStatusAggregator(cm)

	rollout := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "rollouts.kruise.io/v1alpha1",
			"kind":       "Rollout",
			"status": map[string]interface{}{
				"phase":               "Succeeded",
				"message":             "Rollout completed successfully",
				"currentStepIndex":    int64(5),
				"currentStepWeight":   int64(100),
				"stableReplicas":      int64(10),
				"canaryReplicas":      int64(0),
				"updatedReplicas":     int64(10),
				"readyReplicas":       int64(10),
			},
		},
	}

	status := aggregator.extractRolloutStatus(rollout)

	assert.Equal(t, appsv1alpha1.RolloutPhaseSucceeded, status.Phase)
	assert.Equal(t, "Rollout completed successfully", status.Message)
	assert.Equal(t, int32(5), status.CurrentStep)
	assert.Equal(t, int32(100), status.CurrentStepWeight)
	assert.Equal(t, int32(10), status.StableReplicas)
	assert.Equal(t, int32(0), status.CanaryReplicas)
	assert.Equal(t, int32(10), status.UpdatedReplicas)
	assert.Equal(t, int32(10), status.ReadyReplicas)
}

func TestRolloutStatusAggregator_extractRolloutStatus_EmptyStatus(t *testing.T) {
	cm := &mockClientManager{}
	aggregator := NewRolloutStatusAggregator(cm)

	rollout := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "rollouts.kruise.io/v1alpha1",
			"kind":       "Rollout",
			"status":     map[string]interface{}{},
		},
	}

	status := aggregator.extractRolloutStatus(rollout)

	assert.NotNil(t, status)
	assert.Equal(t, appsv1alpha1.RolloutStatusPhase(""), status.Phase)
	assert.Equal(t, "", status.Message)
	assert.Equal(t, int32(0), status.CurrentStep)
	assert.Equal(t, int32(0), status.CurrentStepWeight)
}

func TestRolloutStatusAggregator_extractRolloutStatus_NoStatus(t *testing.T) {
	cm := &mockClientManager{}
	aggregator := NewRolloutStatusAggregator(cm)

	rollout := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "rollouts.kruise.io/v1alpha1",
			"kind":       "Rollout",
			// No status field
		},
	}

	status := aggregator.extractRolloutStatus(rollout)

	assert.NotNil(t, status)
	assert.Equal(t, appsv1alpha1.RolloutStatusPhase(""), status.Phase)
}

func TestRolloutStatusAggregator_extractRolloutStatus_PartialFields(t *testing.T) {
	cm := &mockClientManager{}
	aggregator := NewRolloutStatusAggregator(cm)

	rollout := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "rollouts.kruise.io/v1alpha1",
			"kind":       "Rollout",
			"status": map[string]interface{}{
				"phase": "Failed",
				// Missing other fields
			},
		},
	}

	status := aggregator.extractRolloutStatus(rollout)

	assert.NotNil(t, status)
	assert.Equal(t, appsv1alpha1.RolloutPhaseFailed, status.Phase)
	assert.Equal(t, "", status.Message)
	assert.Equal(t, int32(0), status.CurrentStep)
}

func TestRolloutStatusAggregator_updateClusterRolloutStatus_Existing(t *testing.T) {
	cm := &mockClientManager{}
	aggregator := NewRolloutStatusAggregator(cm)

	app := &appsv1alpha1.Application{
		Status: appsv1alpha1.ApplicationStatus{
			ClustersStatus: []appsv1alpha1.ClusterStatus{
				{
					ClusterName: "cluster-a",
					Rollout: &appsv1alpha1.RolloutStatus{
						Phase: appsv1alpha1.RolloutPhaseProgressing,
					},
				},
				{
					ClusterName: "cluster-b",
					Rollout: &appsv1alpha1.RolloutStatus{
						Phase: appsv1alpha1.RolloutPhaseInitial,
					},
				},
			},
		},
	}

	newStatus := &appsv1alpha1.RolloutStatus{
		Phase: appsv1alpha1.RolloutPhaseSucceeded,
	}

	aggregator.updateClusterRolloutStatus(app, "cluster-a", newStatus)

	// Verify cluster-a was updated
	var clusterAStatus *appsv1alpha1.ClusterStatus
	for i := range app.Status.ClustersStatus {
		if app.Status.ClustersStatus[i].ClusterName == "cluster-a" {
			clusterAStatus = &app.Status.ClustersStatus[i]
			break
		}
	}

	assert.NotNil(t, clusterAStatus)
	assert.Equal(t, appsv1alpha1.RolloutPhaseSucceeded, clusterAStatus.Rollout.Phase)

	// Verify cluster-b was not changed
	var clusterBStatus *appsv1alpha1.ClusterStatus
	for i := range app.Status.ClustersStatus {
		if app.Status.ClustersStatus[i].ClusterName == "cluster-b" {
			clusterBStatus = &app.Status.ClustersStatus[i]
			break
		}
	}
	assert.NotNil(t, clusterBStatus)
	assert.Equal(t, appsv1alpha1.RolloutPhaseInitial, clusterBStatus.Rollout.Phase)
}

func TestRolloutStatusAggregator_updateClusterRolloutStatus_NewCluster(t *testing.T) {
	cm := &mockClientManager{}
	aggregator := NewRolloutStatusAggregator(cm)

	app := &appsv1alpha1.Application{
		Status: appsv1alpha1.ApplicationStatus{
			ClustersStatus: []appsv1alpha1.ClusterStatus{
				{
					ClusterName: "cluster-a",
					Rollout: &appsv1alpha1.RolloutStatus{
						Phase: appsv1alpha1.RolloutPhaseSucceeded,
					},
				},
			},
		},
	}

	newStatus := &appsv1alpha1.RolloutStatus{
		Phase: appsv1alpha1.RolloutPhaseProgressing,
	}

	aggregator.updateClusterRolloutStatus(app, "cluster-b", newStatus)

	// Verify cluster-b was added
	assert.Len(t, app.Status.ClustersStatus, 2)

	var clusterBStatus *appsv1alpha1.ClusterStatus
	for i := range app.Status.ClustersStatus {
		if app.Status.ClustersStatus[i].ClusterName == "cluster-b" {
			clusterBStatus = &app.Status.ClustersStatus[i]
			break
		}
	}

	assert.NotNil(t, clusterBStatus)
	assert.Equal(t, "cluster-b", clusterBStatus.ClusterName)
	assert.Equal(t, appsv1alpha1.RolloutPhaseProgressing, clusterBStatus.Rollout.Phase)
}

func TestRolloutStatusAggregator_updateClusterRolloutStatus_EmptyStatus(t *testing.T) {
	cm := &mockClientManager{}
	aggregator := NewRolloutStatusAggregator(cm)

	app := &appsv1alpha1.Application{
		Status: appsv1alpha1.ApplicationStatus{
			ClustersStatus: []appsv1alpha1.ClusterStatus{},
		},
	}

	newStatus := &appsv1alpha1.RolloutStatus{
		Phase: appsv1alpha1.RolloutPhaseProgressing,
	}

	aggregator.updateClusterRolloutStatus(app, "cluster-a", newStatus)

	// Verify cluster-a was added
	assert.Len(t, app.Status.ClustersStatus, 1)
	assert.Equal(t, "cluster-a", app.Status.ClustersStatus[0].ClusterName)
	assert.Equal(t, appsv1alpha1.RolloutPhaseProgressing, app.Status.ClustersStatus[0].Rollout.Phase)
}

func TestRolloutStatusAggregator_AggregateRolloutStatus_EmptyClusters(t *testing.T) {
	cm := &mockClientManager{}
	aggregator := NewRolloutStatusAggregator(cm)

	app := &appsv1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-app",
			Namespace: "default",
		},
		Spec: appsv1alpha1.ApplicationSpec{
			RolloutStrategy: &appsv1alpha1.RolloutStrategy{
				Type: appsv1alpha1.RolloutTypeCanary,
				Canary: &appsv1alpha1.CanaryStrategy{
					Steps: []appsv1alpha1.CanaryStep{{Weight: 100}},
				},
			},
		},
	}

	ctx := context.Background()
	err := aggregator.AggregateRolloutStatus(ctx, app, []string{})

	assert.NoError(t, err)
}

func TestRolloutStatusAggregator_AggregateRolloutStatus_AllPhases(t *testing.T) {
	scheme := setupStatusTestScheme()

	phases := []appsv1alpha1.RolloutStatusPhase{
		appsv1alpha1.RolloutPhaseInitial,
		appsv1alpha1.RolloutPhaseProgressing,
		appsv1alpha1.RolloutPhasePaused,
		appsv1alpha1.RolloutPhaseSucceeded,
		appsv1alpha1.RolloutPhaseFailed,
	}

	for _, phase := range phases {
		t.Run(string(phase), func(t *testing.T) {
			rollout := &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "rollouts.kruise.io/v1alpha1",
					"kind":       "Rollout",
					"metadata": map[string]interface{}{
						"name":      "test-app",
						"namespace": "default",
					},
					"status": map[string]interface{}{
						"phase": string(phase),
					},
				},
			}

			clusterClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(rollout).Build()

			cm := &mockClientManager{
				clients: map[string]client.Client{
					"cluster-a": clusterClient,
				},
			}
			aggregator := NewRolloutStatusAggregator(cm)

			app := &appsv1alpha1.Application{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-app",
					Namespace: "default",
				},
				Spec: appsv1alpha1.ApplicationSpec{
					RolloutStrategy: &appsv1alpha1.RolloutStrategy{
						Type: appsv1alpha1.RolloutTypeCanary,
						Canary: &appsv1alpha1.CanaryStrategy{
							Steps: []appsv1alpha1.CanaryStep{{Weight: 100}},
						},
					},
				},
				Status: appsv1alpha1.ApplicationStatus{
					ClustersStatus: []appsv1alpha1.ClusterStatus{
						{ClusterName: "cluster-a"},
					},
				},
			}

			ctx := context.Background()
			err := aggregator.AggregateRolloutStatus(ctx, app, []string{"cluster-a"})

			assert.NoError(t, err)
			assert.Equal(t, phase, app.Status.ClustersStatus[0].Rollout.Phase)
		})
	}
}

func TestRolloutStatusAggregator_AggregateRolloutStatus_InvalidReplicaValues(t *testing.T) {
	scheme := setupStatusTestScheme()

	// Test with negative values (should be handled gracefully)
	rollout := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "rollouts.kruise.io/v1alpha1",
			"kind":       "Rollout",
			"metadata": map[string]interface{}{
				"name":      "test-app",
				"namespace": "default",
			},
			"status": map[string]interface{}{
				"phase":            "Progressing",
				"stableReplicas":   int64(-1), // Invalid but test handling
				"canaryReplicas":   int64(100),
				"currentStepIndex": int64(-1), // Invalid but test handling
			},
		},
	}

	clusterClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(rollout).Build()

	cm := &mockClientManager{
		clients: map[string]client.Client{
			"cluster-a": clusterClient,
		},
	}
	aggregator := NewRolloutStatusAggregator(cm)

	app := &appsv1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-app",
			Namespace: "default",
		},
		Spec: appsv1alpha1.ApplicationSpec{
			RolloutStrategy: &appsv1alpha1.RolloutStrategy{
				Type: appsv1alpha1.RolloutTypeCanary,
				Canary: &appsv1alpha1.CanaryStrategy{
					Steps: []appsv1alpha1.CanaryStep{{Weight: 100}},
				},
			},
		},
		Status: appsv1alpha1.ApplicationStatus{
			ClustersStatus: []appsv1alpha1.ClusterStatus{
				{ClusterName: "cluster-a"},
			},
		},
	}

	ctx := context.Background()
	err := aggregator.AggregateRolloutStatus(ctx, app, []string{"cluster-a"})

	assert.NoError(t, err)
	assert.Equal(t, int32(-1), app.Status.ClustersStatus[0].Rollout.StableReplicas)
	assert.Equal(t, int32(100), app.Status.ClustersStatus[0].Rollout.CanaryReplicas)
	assert.Equal(t, int32(-1), app.Status.ClustersStatus[0].Rollout.CurrentStep)
}
