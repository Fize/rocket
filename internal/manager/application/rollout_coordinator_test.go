package application

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	typeUtil "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	appsv1alpha1 "github.com/hex-techs/rocket/pkg/apis/apps/v1alpha1"
	clusterv1alpha1 "github.com/hex-techs/rocket/pkg/apis/storage/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// mockClientManager is a mock implementation of clientManager for testing
type mockClientManager struct {
	clients map[string]client.Client
	err     error
}

func (m *mockClientManager) GetClient(ctx context.Context, clusterName string) (client.Client, error) {
	if m.err != nil {
		return nil, m.err
	}
	if cli, ok := m.clients[clusterName]; ok {
		return cli, nil
	}
	return nil, fmt.Errorf("cluster %s not found", clusterName)
}

func setupTestScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	_ = appsv1alpha1.AddToScheme(scheme)
	_ = clusterv1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	return scheme
}

func TestNewRolloutCoordinator(t *testing.T) {
	cm := &mockClientManager{}
	coordinator := NewRolloutCoordinator(cm)

	assert.NotNil(t, coordinator)
	assert.NotNil(t, coordinator.Builder)
	assert.Equal(t, cm, coordinator.ClientManager)
}

func TestRolloutCoordinator_ReconcileRollout_NoStrategy(t *testing.T) {
	cm := &mockClientManager{}
	coordinator := NewRolloutCoordinator(cm)

	app := &appsv1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-app",
			Namespace: "default",
		},
		Spec: appsv1alpha1.ApplicationSpec{
			// No rollout strategy
		},
	}

	topology := []appsv1alpha1.ClusterTopology{
		{Name: "cluster-a"},
	}

	ctx := context.Background()
	err := coordinator.ReconcileRollout(ctx, app, topology)

	assert.NoError(t, err)
}

func TestRolloutCoordinator_ReconcileRollout_Parallel(t *testing.T) {
	scheme := setupTestScheme()
	
	clusterAClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	clusterBClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	cm := &mockClientManager{
		clients: map[string]client.Client{
			"cluster-a": clusterAClient,
			"cluster-b": clusterBClient,
		},
	}
	coordinator := NewRolloutCoordinator(cm)

	app := &appsv1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-app",
			Namespace: "default",
			UID:       "test-uid",
		},
		Spec: appsv1alpha1.ApplicationSpec{
			Workload: appsv1alpha1.WorkloadGVK{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
			},
			RolloutStrategy: &appsv1alpha1.RolloutStrategy{
				Type: appsv1alpha1.RolloutTypeCanary,
				Canary: &appsv1alpha1.CanaryStrategy{
					Steps: []appsv1alpha1.CanaryStep{
						{Weight: 20},
						{Weight: 100},
					},
				},
				// No ClusterOrder, defaults to parallel
			},
		},
	}

	topology := []appsv1alpha1.ClusterTopology{
		{Name: "cluster-a"},
		{Name: "cluster-b"},
	}

	ctx := context.Background()
	err := coordinator.ReconcileRollout(ctx, app, topology)

	assert.NoError(t, err)

	// Verify rollout was created in both clusters
	rolloutA := &unstructured.Unstructured{}
	rolloutA.SetAPIVersion("rollouts.kruise.io/v1alpha1")
	rolloutA.SetKind("Rollout")
	err = clusterAClient.Get(ctx, typeUtil.NamespacedName{Name: "test-app", Namespace: "default"}, rolloutA)
	assert.NoError(t, err)

	rolloutB := &unstructured.Unstructured{}
	rolloutB.SetAPIVersion("rollouts.kruise.io/v1alpha1")
	rolloutB.SetKind("Rollout")
	err = clusterBClient.Get(ctx, typeUtil.NamespacedName{Name: "test-app", Namespace: "default"}, rolloutB)
	assert.NoError(t, err)
}

func TestRolloutCoordinator_ReconcileRollout_Sequential(t *testing.T) {
	scheme := setupTestScheme()
	
	clusterAClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	clusterBClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	cm := &mockClientManager{
		clients: map[string]client.Client{
			"cluster-a": clusterAClient,
			"cluster-b": clusterBClient,
		},
	}
	coordinator := NewRolloutCoordinator(cm)

	app := &appsv1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-app",
			Namespace: "default",
			UID:       "test-uid",
		},
		Spec: appsv1alpha1.ApplicationSpec{
			Workload: appsv1alpha1.WorkloadGVK{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
			},
			RolloutStrategy: &appsv1alpha1.RolloutStrategy{
				Type: appsv1alpha1.RolloutTypeCanary,
				Canary: &appsv1alpha1.CanaryStrategy{
					Steps: []appsv1alpha1.CanaryStep{{Weight: 100}},
				},
				ClusterOrder: &appsv1alpha1.ClusterOrder{
					Type: appsv1alpha1.ClusterOrderSequential,
				},
			},
		},
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

	topology := []appsv1alpha1.ClusterTopology{
		{Name: "cluster-a"},
		{Name: "cluster-b"},
	}

	ctx := context.Background()
	err := coordinator.ReconcileRollout(ctx, app, topology)

	assert.NoError(t, err)

	// Verify rollout was created in cluster-b
	rolloutB := &unstructured.Unstructured{}
	rolloutB.SetAPIVersion("rollouts.kruise.io/v1alpha1")
	rolloutB.SetKind("Rollout")
	err = clusterBClient.Get(ctx, typeUtil.NamespacedName{Name: "test-app", Namespace: "default"}, rolloutB)
	assert.NoError(t, err)
}

func TestRolloutCoordinator_ReconcileRollout_SequentialBlocked(t *testing.T) {
	scheme := setupTestScheme()
	
	clusterAClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	clusterBClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	cm := &mockClientManager{
		clients: map[string]client.Client{
			"cluster-a": clusterAClient,
			"cluster-b": clusterBClient,
		},
	}
	coordinator := NewRolloutCoordinator(cm)

	// Previous cluster not complete
	app := &appsv1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-app",
			Namespace: "default",
			UID:       "test-uid",
		},
		Spec: appsv1alpha1.ApplicationSpec{
			Workload: appsv1alpha1.WorkloadGVK{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
			},
			RolloutStrategy: &appsv1alpha1.RolloutStrategy{
				Type: appsv1alpha1.RolloutTypeCanary,
				Canary: &appsv1alpha1.CanaryStrategy{
					Steps: []appsv1alpha1.CanaryStep{{Weight: 100}},
				},
				ClusterOrder: &appsv1alpha1.ClusterOrder{
					Type: appsv1alpha1.ClusterOrderSequential,
				},
			},
		},
		Status: appsv1alpha1.ApplicationStatus{
			ClustersStatus: []appsv1alpha1.ClusterStatus{
				{
					ClusterName: "cluster-a",
					Rollout: &appsv1alpha1.RolloutStatus{
						Phase: appsv1alpha1.RolloutPhaseProgressing, // Not complete
					},
				},
			},
		},
	}

	topology := []appsv1alpha1.ClusterTopology{
		{Name: "cluster-a"},
		{Name: "cluster-b"},
	}

	ctx := context.Background()
	err := coordinator.ReconcileRollout(ctx, app, topology)

	assert.NoError(t, err)

	// Verify rollout was NOT created in cluster-b
	rolloutB := &unstructured.Unstructured{}
	rolloutB.SetAPIVersion("rollouts.kruise.io/v1alpha1")
	rolloutB.SetKind("Rollout")
	err = clusterBClient.Get(ctx, typeUtil.NamespacedName{Name: "test-app", Namespace: "default"}, rolloutB)
	assert.True(t, errors.IsNotFound(err))
}

func TestRolloutCoordinator_ReconcileRollout_ClusterClientError(t *testing.T) {
	cm := &mockClientManager{
		err: fmt.Errorf("failed to get cluster client"),
	}
	coordinator := NewRolloutCoordinator(cm)

	app := &appsv1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-app",
			Namespace: "default",
			UID:       "test-uid",
		},
		Spec: appsv1alpha1.ApplicationSpec{
			Workload: appsv1alpha1.WorkloadGVK{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
			},
			RolloutStrategy: &appsv1alpha1.RolloutStrategy{
				Type: appsv1alpha1.RolloutTypeCanary,
				Canary: &appsv1alpha1.CanaryStrategy{
					Steps: []appsv1alpha1.CanaryStep{{Weight: 100}},
				},
			},
		},
	}

	topology := []appsv1alpha1.ClusterTopology{
		{Name: "cluster-a"},
	}

	ctx := context.Background()
	err := coordinator.ReconcileRollout(ctx, app, topology)

	// Should not error, just log and continue
	assert.NoError(t, err)
}

func TestRolloutCoordinator_ReconcileRollout_MissingCluster(t *testing.T) {
	cm := &mockClientManager{
		clients: map[string]client.Client{}, // empty
	}
	coordinator := NewRolloutCoordinator(cm)

	app := &appsv1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-app",
			Namespace: "default",
			UID:       "test-uid",
		},
		Spec: appsv1alpha1.ApplicationSpec{
			Workload: appsv1alpha1.WorkloadGVK{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
			},
			RolloutStrategy: &appsv1alpha1.RolloutStrategy{
				Type: appsv1alpha1.RolloutTypeCanary,
				Canary: &appsv1alpha1.CanaryStrategy{
					Steps: []appsv1alpha1.CanaryStep{{Weight: 100}},
				},
			},
		},
	}

	topology := []appsv1alpha1.ClusterTopology{
		{Name: "cluster-a"},
	}

	ctx := context.Background()
	err := coordinator.ReconcileRollout(ctx, app, topology)

	// Should not error, just log and continue
	assert.NoError(t, err)
}

func TestRolloutCoordinator_isRolloutComplete(t *testing.T) {
	cm := &mockClientManager{}
	coordinator := NewRolloutCoordinator(cm)

	tests := []struct {
		name        string
		app         *appsv1alpha1.Application
		clusterName string
		expected    bool
	}{
		{
			name: "rollout succeeded",
			app: &appsv1alpha1.Application{
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
			},
			clusterName: "cluster-a",
			expected:    true,
		},
		{
			name: "rollout initial (treated as complete)",
			app: &appsv1alpha1.Application{
				Status: appsv1alpha1.ApplicationStatus{
					ClustersStatus: []appsv1alpha1.ClusterStatus{
						{
							ClusterName: "cluster-a",
							Rollout: &appsv1alpha1.RolloutStatus{
								Phase: appsv1alpha1.RolloutPhaseInitial,
							},
						},
					},
				},
			},
			clusterName: "cluster-a",
			expected:    true,
		},
		{
			name: "rollout progressing",
			app: &appsv1alpha1.Application{
				Status: appsv1alpha1.ApplicationStatus{
					ClustersStatus: []appsv1alpha1.ClusterStatus{
						{
							ClusterName: "cluster-a",
							Rollout: &appsv1alpha1.RolloutStatus{
								Phase: appsv1alpha1.RolloutPhaseProgressing,
							},
						},
					},
				},
			},
			clusterName: "cluster-a",
			expected:    false,
		},
		{
			name: "rollout failed",
			app: &appsv1alpha1.Application{
				Status: appsv1alpha1.ApplicationStatus{
					ClustersStatus: []appsv1alpha1.ClusterStatus{
						{
							ClusterName: "cluster-a",
							Rollout: &appsv1alpha1.RolloutStatus{
								Phase: appsv1alpha1.RolloutPhaseFailed,
							},
						},
					},
				},
			},
			clusterName: "cluster-a",
			expected:    false,
		},
		{
			name: "rollout paused",
			app: &appsv1alpha1.Application{
				Status: appsv1alpha1.ApplicationStatus{
					ClustersStatus: []appsv1alpha1.ClusterStatus{
						{
							ClusterName: "cluster-a",
							Rollout: &appsv1alpha1.RolloutStatus{
								Phase: appsv1alpha1.RolloutPhasePaused,
							},
						},
					},
				},
			},
			clusterName: "cluster-a",
			expected:    false,
		},
		{
			name: "cluster not found in status",
			app: &appsv1alpha1.Application{
				Status: appsv1alpha1.ApplicationStatus{
					ClustersStatus: []appsv1alpha1.ClusterStatus{
						{
							ClusterName: "cluster-b",
							Rollout: &appsv1alpha1.RolloutStatus{
								Phase: appsv1alpha1.RolloutPhaseSucceeded,
							},
						},
					},
				},
			},
			clusterName: "cluster-a",
			expected:    false,
		},
		{
			name: "rollout status nil",
			app: &appsv1alpha1.Application{
				Status: appsv1alpha1.ApplicationStatus{
					ClustersStatus: []appsv1alpha1.ClusterStatus{
						{
							ClusterName: "cluster-a",
							Rollout:     nil,
						},
					},
				},
			},
			clusterName: "cluster-a",
			expected:    false,
		},
		{
			name: "empty clusters status",
			app: &appsv1alpha1.Application{
				Status: appsv1alpha1.ApplicationStatus{
					ClustersStatus: []appsv1alpha1.ClusterStatus{},
				},
			},
			clusterName: "cluster-a",
			expected:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := coordinator.isRolloutComplete(context.Background(), tt.app, tt.clusterName)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestRolloutCoordinator_applyRollout_Create(t *testing.T) {
	scheme := setupTestScheme()
	cm := &mockClientManager{}
	coordinator := NewRolloutCoordinator(cm)

	clusterClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	rollout := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "rollouts.kruise.io/v1alpha1",
			"kind":       "Rollout",
			"metadata": map[string]interface{}{
				"name":      "test-app",
				"namespace": "default",
			},
		},
	}

	ctx := context.Background()
	err := coordinator.applyRollout(ctx, clusterClient, rollout)

	assert.NoError(t, err)

	// Verify rollout was created
	created := &unstructured.Unstructured{}
	created.SetAPIVersion("rollouts.kruise.io/v1alpha1")
	created.SetKind("Rollout")
	err = clusterClient.Get(ctx, typeUtil.NamespacedName{Name: "test-app", Namespace: "default"}, created)
	assert.NoError(t, err)
}

func TestRolloutCoordinator_DeleteRollout(t *testing.T) {
	scheme := setupTestScheme()

	// Create existing rollout
	existing := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "rollouts.kruise.io/v1alpha1",
			"kind":       "Rollout",
			"metadata": map[string]interface{}{
				"name":      "test-app",
				"namespace": "default",
			},
		},
	}

	clusterAClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(existing).Build()
	clusterBClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	cm := &mockClientManager{
		clients: map[string]client.Client{
			"cluster-a": clusterAClient,
			"cluster-b": clusterBClient,
		},
	}
	coordinator := NewRolloutCoordinator(cm)

	app := &appsv1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-app",
			Namespace: "default",
		},
	}

	ctx := context.Background()
	err := coordinator.DeleteRollout(ctx, app, []string{"cluster-a", "cluster-b"})

	assert.NoError(t, err)

	// Verify rollout was deleted from cluster-a
	deleted := &unstructured.Unstructured{}
	deleted.SetAPIVersion("rollouts.kruise.io/v1alpha1")
	deleted.SetKind("Rollout")
	err = clusterAClient.Get(ctx, typeUtil.NamespacedName{Name: "test-app", Namespace: "default"}, deleted)
	assert.True(t, errors.IsNotFound(err))
}

func TestRolloutCoordinator_DeleteRollout_ClientError(t *testing.T) {
	cm := &mockClientManager{
		err: fmt.Errorf("failed to get cluster client"),
	}
	coordinator := NewRolloutCoordinator(cm)

	app := &appsv1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-app",
			Namespace: "default",
		},
	}

	ctx := context.Background()
	err := coordinator.DeleteRollout(ctx, app, []string{"cluster-a"})

	// Should not error, just log and continue
	assert.NoError(t, err)
}

func TestRolloutCoordinator_DeleteRollout_NotFound(t *testing.T) {
	scheme := setupTestScheme()

	// No existing rollout
	clusterClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	cm := &mockClientManager{
		clients: map[string]client.Client{
			"cluster-a": clusterClient,
		},
	}
	coordinator := NewRolloutCoordinator(cm)

	app := &appsv1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-app",
			Namespace: "default",
		},
	}

	ctx := context.Background()
	err := coordinator.DeleteRollout(ctx, app, []string{"cluster-a"})

	// Should not error for NotFound
	assert.NoError(t, err)
}

func TestGetRolloutGVR(t *testing.T) {
	gvr := getRolloutGVR()

	assert.Equal(t, "rollouts.kruise.io", gvr.Group)
	assert.Equal(t, "v1alpha1", gvr.Version)
	assert.Equal(t, "rollouts", gvr.Resource)
}

func TestRolloutCoordinator_ReconcileRollout_BlueGreen(t *testing.T) {
	scheme := setupTestScheme()
	
	clusterAClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	cm := &mockClientManager{
		clients: map[string]client.Client{
			"cluster-a": clusterAClient,
		},
	}
	coordinator := NewRolloutCoordinator(cm)

	app := &appsv1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-app",
			Namespace: "default",
			UID:       "test-uid",
		},
		Spec: appsv1alpha1.ApplicationSpec{
			Workload: appsv1alpha1.WorkloadGVK{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
			},
			RolloutStrategy: &appsv1alpha1.RolloutStrategy{
				Type: appsv1alpha1.RolloutTypeBlueGreen,
				BlueGreen: &appsv1alpha1.BlueGreenStrategy{
					ActiveService:         "active-svc",
					PreviewService:        "preview-svc",
					AutoPromotionEnabled:  true,
					ScaleDownDelaySeconds: 60,
				},
			},
		},
	}

	topology := []appsv1alpha1.ClusterTopology{
		{Name: "cluster-a"},
	}

	ctx := context.Background()
	err := coordinator.ReconcileRollout(ctx, app, topology)

	assert.NoError(t, err)

	// Verify rollout was created
	rollout := &unstructured.Unstructured{}
	rollout.SetAPIVersion("rollouts.kruise.io/v1alpha1")
	rollout.SetKind("Rollout")
	err = clusterAClient.Get(ctx, typeUtil.NamespacedName{Name: "test-app", Namespace: "default"}, rollout)
	assert.NoError(t, err)

	// Verify blue-green strategy
	spec, _, _ := unstructured.NestedMap(rollout.Object, "spec")
	strategy, _, _ := unstructured.NestedMap(spec, "strategy")
	blueGreen, _, _ := unstructured.NestedMap(strategy, "blueGreen")

	assert.Equal(t, "active-svc", blueGreen["activeService"])
	assert.Equal(t, "preview-svc", blueGreen["previewService"])
	assert.Equal(t, true, blueGreen["autoPromotionEnabled"])
}

func TestRolloutCoordinator_ReconcileRollout_ABTest(t *testing.T) {
	scheme := setupTestScheme()
	
	clusterAClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	cm := &mockClientManager{
		clients: map[string]client.Client{
			"cluster-a": clusterAClient,
		},
	}
	coordinator := NewRolloutCoordinator(cm)

	app := &appsv1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-app",
			Namespace: "default",
			UID:       "test-uid",
		},
		Spec: appsv1alpha1.ApplicationSpec{
			Workload: appsv1alpha1.WorkloadGVK{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
			},
			RolloutStrategy: &appsv1alpha1.RolloutStrategy{
				Type: appsv1alpha1.RolloutTypeABTest,
				ABTest: &appsv1alpha1.ABTestStrategy{
					BaselineCluster:   "cluster-a",
					CandidateClusters: []string{"cluster-b"},
					TrafficSplit:      20,
				},
			},
		},
	}

	topology := []appsv1alpha1.ClusterTopology{
		{Name: "cluster-a"},
	}

	ctx := context.Background()
	err := coordinator.ReconcileRollout(ctx, app, topology)

	assert.NoError(t, err)

	// Verify rollout was created
	rollout := &unstructured.Unstructured{}
	rollout.SetAPIVersion("rollouts.kruise.io/v1alpha1")
	rollout.SetKind("Rollout")
	err = clusterAClient.Get(ctx, typeUtil.NamespacedName{Name: "test-app", Namespace: "default"}, rollout)
	assert.NoError(t, err)
}

func TestRolloutCoordinator_ReconcileRollout_EmptyTopology(t *testing.T) {
	cm := &mockClientManager{}
	coordinator := NewRolloutCoordinator(cm)

	app := &appsv1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-app",
			Namespace: "default",
			UID:       "test-uid",
		},
		Spec: appsv1alpha1.ApplicationSpec{
			Workload: appsv1alpha1.WorkloadGVK{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
			},
			RolloutStrategy: &appsv1alpha1.RolloutStrategy{
				Type: appsv1alpha1.RolloutTypeCanary,
				Canary: &appsv1alpha1.CanaryStrategy{
					Steps: []appsv1alpha1.CanaryStep{{Weight: 100}},
				},
			},
		},
	}

	// Empty topology
	topology := []appsv1alpha1.ClusterTopology{}

	ctx := context.Background()
	err := coordinator.ReconcileRollout(ctx, app, topology)

	assert.NoError(t, err)
}

func TestRolloutCoordinator_ReconcileRollout_FirstClusterSequential(t *testing.T) {
	// Test that first cluster in sequential rollout is always allowed
	clusterAClient := fake.NewClientBuilder().WithScheme(setupTestScheme()).Build()

	cm := &mockClientManager{
		clients: map[string]client.Client{
			"cluster-a": clusterAClient,
		},
	}
	coordinator := NewRolloutCoordinator(cm)

	// No previous cluster status needed for first cluster
	app := &appsv1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-app",
			Namespace: "default",
			UID:       "test-uid",
		},
		Spec: appsv1alpha1.ApplicationSpec{
			Workload: appsv1alpha1.WorkloadGVK{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
			},
			RolloutStrategy: &appsv1alpha1.RolloutStrategy{
				Type: appsv1alpha1.RolloutTypeCanary,
				Canary: &appsv1alpha1.CanaryStrategy{
					Steps: []appsv1alpha1.CanaryStep{{Weight: 100}},
				},
				ClusterOrder: &appsv1alpha1.ClusterOrder{
					Type: appsv1alpha1.ClusterOrderSequential,
				},
			},
		},
		Status: appsv1alpha1.ApplicationStatus{
			// Empty status - first cluster should still proceed
			ClustersStatus: []appsv1alpha1.ClusterStatus{},
		},
	}

	topology := []appsv1alpha1.ClusterTopology{
		{Name: "cluster-a"},
	}

	ctx := context.Background()
	err := coordinator.ReconcileRollout(ctx, app, topology)

	assert.NoError(t, err)

	// Verify rollout was created
	rollout := &unstructured.Unstructured{}
	rollout.SetAPIVersion("rollouts.kruise.io/v1alpha1")
	rollout.SetKind("Rollout")
	err = clusterAClient.Get(ctx, typeUtil.NamespacedName{Name: "test-app", Namespace: "default"}, rollout)
	assert.NoError(t, err)
}

func TestRolloutCoordinator_ReconcileRollout_ClusterOrderNil(t *testing.T) {
	// Test that nil ClusterOrder defaults to parallel behavior
	scheme := setupTestScheme()
	
	clusterAClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	clusterBClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	cm := &mockClientManager{
		clients: map[string]client.Client{
			"cluster-a": clusterAClient,
			"cluster-b": clusterBClient,
		},
	}
	coordinator := NewRolloutCoordinator(cm)

	app := &appsv1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-app",
			Namespace: "default",
			UID:       "test-uid",
		},
		Spec: appsv1alpha1.ApplicationSpec{
			Workload: appsv1alpha1.WorkloadGVK{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
			},
			RolloutStrategy: &appsv1alpha1.RolloutStrategy{
				Type: appsv1alpha1.RolloutTypeCanary,
				Canary: &appsv1alpha1.CanaryStrategy{
					Steps: []appsv1alpha1.CanaryStep{{Weight: 100}},
				},
				// ClusterOrder is nil - should behave as parallel
			},
		},
	}

	topology := []appsv1alpha1.ClusterTopology{
		{Name: "cluster-a"},
		{Name: "cluster-b"},
	}

	ctx := context.Background()
	err := coordinator.ReconcileRollout(ctx, app, topology)

	assert.NoError(t, err)

	// Verify rollout was created in both clusters
	rolloutA := &unstructured.Unstructured{}
	rolloutA.SetAPIVersion("rollouts.kruise.io/v1alpha1")
	rolloutA.SetKind("Rollout")
	err = clusterAClient.Get(ctx, typeUtil.NamespacedName{Name: "test-app", Namespace: "default"}, rolloutA)
	assert.NoError(t, err)

	rolloutB := &unstructured.Unstructured{}
	rolloutB.SetAPIVersion("rollouts.kruise.io/v1alpha1")
	rolloutB.SetKind("Rollout")
	err = clusterBClient.Get(ctx, typeUtil.NamespacedName{Name: "test-app", Namespace: "default"}, rolloutB)
	assert.NoError(t, err)
}

func TestRolloutCoordinator_ReconcileRollout_MultipleClustersSequential(t *testing.T) {
	// Test sequential rollout with 3 clusters
	scheme := setupTestScheme()
	
	clusterAClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	clusterBClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	clusterCClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	cm := &mockClientManager{
		clients: map[string]client.Client{
			"cluster-a": clusterAClient,
			"cluster-b": clusterBClient,
			"cluster-c": clusterCClient,
		},
	}
	coordinator := NewRolloutCoordinator(cm)

	// Only cluster-a is complete
	app := &appsv1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-app",
			Namespace: "default",
			UID:       "test-uid",
		},
		Spec: appsv1alpha1.ApplicationSpec{
			Workload: appsv1alpha1.WorkloadGVK{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
			},
			RolloutStrategy: &appsv1alpha1.RolloutStrategy{
				Type: appsv1alpha1.RolloutTypeCanary,
				Canary: &appsv1alpha1.CanaryStrategy{
					Steps: []appsv1alpha1.CanaryStep{{Weight: 100}},
				},
				ClusterOrder: &appsv1alpha1.ClusterOrder{
					Type: appsv1alpha1.ClusterOrderSequential,
				},
			},
		},
		Status: appsv1alpha1.ApplicationStatus{
			ClustersStatus: []appsv1alpha1.ClusterStatus{
				{
					ClusterName: "cluster-a",
					Rollout: &appsv1alpha1.RolloutStatus{
						Phase: appsv1alpha1.RolloutPhaseSucceeded,
					},
				},
				{
					ClusterName: "cluster-b",
					Rollout: &appsv1alpha1.RolloutStatus{
						Phase: appsv1alpha1.RolloutPhaseProgressing, // Not complete
					},
				},
			},
		},
	}

	topology := []appsv1alpha1.ClusterTopology{
		{Name: "cluster-a"},
		{Name: "cluster-b"},
		{Name: "cluster-c"},
	}

	ctx := context.Background()
	err := coordinator.ReconcileRollout(ctx, app, topology)

	assert.NoError(t, err)

	// cluster-a should get rollout (already complete but still reconcile)
	rolloutA := &unstructured.Unstructured{}
	rolloutA.SetAPIVersion("rollouts.kruise.io/v1alpha1")
	rolloutA.SetKind("Rollout")
	err = clusterAClient.Get(ctx, typeUtil.NamespacedName{Name: "test-app", Namespace: "default"}, rolloutA)
	assert.NoError(t, err)

	// cluster-b should get rollout (it's progressing, meaning it's in progress)
	rolloutB := &unstructured.Unstructured{}
	rolloutB.SetAPIVersion("rollouts.kruise.io/v1alpha1")
	rolloutB.SetKind("Rollout")
	err = clusterBClient.Get(ctx, typeUtil.NamespacedName{Name: "test-app", Namespace: "default"}, rolloutB)
	assert.NoError(t, err)

	// cluster-c should NOT get rollout (cluster-b not complete)
	rolloutC := &unstructured.Unstructured{}
	rolloutC.SetAPIVersion("rollouts.kruise.io/v1alpha1")
	rolloutC.SetKind("Rollout")
	err = clusterCClient.Get(ctx, typeUtil.NamespacedName{Name: "test-app", Namespace: "default"}, rolloutC)
	assert.True(t, errors.IsNotFound(err))
}

func TestRolloutCoordinator_DeleteRollout_EmptyClusters(t *testing.T) {
	cm := &mockClientManager{}
	coordinator := NewRolloutCoordinator(cm)

	app := &appsv1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-app",
			Namespace: "default",
		},
	}

	ctx := context.Background()
	err := coordinator.DeleteRollout(ctx, app, []string{})

	assert.NoError(t, err)
}

func TestRolloutCoordinator_DeleteRollout_MultipleClusters(t *testing.T) {
	scheme := setupTestScheme()

	// Create existing rollout in cluster A
	existingA := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "rollouts.kruise.io/v1alpha1",
			"kind":       "Rollout",
			"metadata": map[string]interface{}{
				"name":      "test-app",
				"namespace": "default",
			},
		},
	}

	// Create existing rollout in cluster B
	existingB := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "rollouts.kruise.io/v1alpha1",
			"kind":       "Rollout",
			"metadata": map[string]interface{}{
				"name":      "test-app",
				"namespace": "default",
			},
		},
	}

	clusterAClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(existingA).Build()
	clusterBClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(existingB).Build()

	cm := &mockClientManager{
		clients: map[string]client.Client{
			"cluster-a": clusterAClient,
			"cluster-b": clusterBClient,
		},
	}
	coordinator := NewRolloutCoordinator(cm)

	app := &appsv1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-app",
			Namespace: "default",
		},
	}

	ctx := context.Background()
	err := coordinator.DeleteRollout(ctx, app, []string{"cluster-a", "cluster-b"})

	assert.NoError(t, err)

	// Verify rollout was deleted from cluster-a
	deletedA := &unstructured.Unstructured{}
	deletedA.SetAPIVersion("rollouts.kruise.io/v1alpha1")
	deletedA.SetKind("Rollout")
	err = clusterAClient.Get(ctx, typeUtil.NamespacedName{Name: "test-app", Namespace: "default"}, deletedA)
	assert.True(t, errors.IsNotFound(err))

	// Verify rollout was deleted from cluster-b
	deletedB := &unstructured.Unstructured{}
	deletedB.SetAPIVersion("rollouts.kruise.io/v1alpha1")
	deletedB.SetKind("Rollout")
	err = clusterBClient.Get(ctx, typeUtil.NamespacedName{Name: "test-app", Namespace: "default"}, deletedB)
	assert.True(t, errors.IsNotFound(err))
}
