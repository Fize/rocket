package addon

import (
	"context"
	"errors"
	"testing"

	parentaddon "github.com/hex-techs/rocket/internal/addon"
	storagev1alpha1 "github.com/hex-techs/rocket/pkg/apis/storage/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// mockAddonController implements parentaddon.AddonController for testing
type mockAddonController struct {
	reconcileFunc  func(ctx context.Context, config parentaddon.AddonConfig) error
	reconcileCalls []parentaddon.AddonConfig
}

func (m *mockAddonController) Reconcile(ctx context.Context, config parentaddon.AddonConfig) error {
	m.reconcileCalls = append(m.reconcileCalls, config)
	if m.reconcileFunc != nil {
		return m.reconcileFunc(ctx, config)
	}
	return nil
}

// mockAddon implements parentaddon.Addon for testing
type mockAddon struct {
	name           string
	managerCtrl    parentaddon.AddonController
	managerCtrlErr error
}

func (m *mockAddon) Name() string {
	return m.name
}

func (m *mockAddon) ManagerController(mgr ctrl.Manager) (parentaddon.AddonController, error) {
	return m.managerCtrl, m.managerCtrlErr
}

func (m *mockAddon) AgentController(mgr ctrl.Manager) (parentaddon.AddonController, error) {
	return nil, nil
}

func (m *mockAddon) Manifests() []runtime.Object {
	return nil
}

func TestAddonReconciler_Reconcile_ClusterNotFound(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, storagev1alpha1.AddToScheme(scheme))

	c := fake.NewClientBuilder().
		WithScheme(scheme).
		Build()

	r := &AddonReconciler{
		Client:      c,
		Scheme:      scheme,
		Controllers: make(map[string]parentaddon.AddonController),
	}

	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name: "non-existent",
		},
	}

	result, err := r.Reconcile(context.Background(), req)
	assert.NoError(t, err)
	assert.Equal(t, ctrl.Result{}, result)
}

func TestAddonReconciler_Reconcile_ClusterWithNoAddons(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, storagev1alpha1.AddToScheme(scheme))

	cluster := &storagev1alpha1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-cluster",
		},
		Spec: storagev1alpha1.ManagedClusterSpec{},
	}

	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(cluster).
		Build()

	r := &AddonReconciler{
		Client:      c,
		Scheme:      scheme,
		Controllers: make(map[string]parentaddon.AddonController),
	}

	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name: cluster.Name,
		},
	}

	result, err := r.Reconcile(context.Background(), req)
	assert.NoError(t, err)
	assert.Equal(t, ctrl.Result{}, result)
}

func TestAddonReconciler_Reconcile_ClusterWithAddons(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, storagev1alpha1.AddToScheme(scheme))

	cluster := &storagev1alpha1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-cluster",
		},
		Spec: storagev1alpha1.ManagedClusterSpec{
			Addons: []storagev1alpha1.ClusterAddon{
				{
					Name:    "test-addon",
					Enabled: true,
					Config: map[string]string{
						"key": "value",
					},
				},
			},
		},
	}

	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(cluster).
		Build()

	r := &AddonReconciler{
		Client:      c,
		Scheme:      scheme,
		Controllers: make(map[string]parentaddon.AddonController),
	}

	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name: cluster.Name,
		},
	}

	// Even with addons in spec, no controller registered means nothing happens
	result, err := r.Reconcile(context.Background(), req)
	assert.NoError(t, err)
	assert.Equal(t, ctrl.Result{}, result)
}

func TestAddonReconciler_Fields(t *testing.T) {
	scheme := runtime.NewScheme()

	c := fake.NewClientBuilder().
		WithScheme(scheme).
		Build()

	r := &AddonReconciler{
		Client:      c,
		Scheme:      scheme,
		Controllers: make(map[string]parentaddon.AddonController),
	}

	assert.NotNil(t, r.Client)
	assert.NotNil(t, r.Scheme)
	assert.NotNil(t, r.Controllers)
	assert.Empty(t, r.Controllers)
}

func TestAddonReconciler_Reconcile_WithMockRegistry(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, storagev1alpha1.AddToScheme(scheme))

	// Create mock registry with addon
	mockReg := parentaddon.NewRegistry()
	mockCtrl := &mockAddonController{}
	mockReg.Register(&mockAddon{
		name:        "test-addon",
		managerCtrl: mockCtrl,
	})

	cluster := &storagev1alpha1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-cluster",
		},
		Spec: storagev1alpha1.ManagedClusterSpec{
			Addons: []storagev1alpha1.ClusterAddon{
				{
					Name:    "test-addon",
					Enabled: true,
					Config: map[string]string{
						"key": "value",
					},
				},
			},
		},
	}

	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(cluster).
		WithStatusSubresource(&storagev1alpha1.ManagedCluster{}).
		Build()

	r := &AddonReconciler{
		Client:   c,
		Scheme:   scheme,
		Registry: mockReg,
		Controllers: map[string]parentaddon.AddonController{
			"test-addon": mockCtrl,
		},
	}

	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name: cluster.Name,
		},
	}

	result, err := r.Reconcile(context.Background(), req)
	assert.NoError(t, err)
	assert.Equal(t, ctrl.Result{}, result)

	// Verify addon controller was called
	require.Len(t, mockCtrl.reconcileCalls, 1)
	assert.Equal(t, "test-cluster", mockCtrl.reconcileCalls[0].ClusterName)
	assert.Equal(t, "value", mockCtrl.reconcileCalls[0].Config["key"])

	// Verify addon status was updated
	var updatedCluster storagev1alpha1.ManagedCluster
	err = c.Get(context.Background(), types.NamespacedName{Name: cluster.Name}, &updatedCluster)
	require.NoError(t, err)
	require.Len(t, updatedCluster.Status.AddonStatus, 1)
	assert.Equal(t, "test-addon", updatedCluster.Status.AddonStatus[0].Name)
	assert.Equal(t, "Applied", updatedCluster.Status.AddonStatus[0].State)
}

func TestAddonReconciler_Reconcile_AddonDisabled(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, storagev1alpha1.AddToScheme(scheme))

	mockReg := parentaddon.NewRegistry()
	mockCtrl := &mockAddonController{}
	mockReg.Register(&mockAddon{
		name:        "test-addon",
		managerCtrl: mockCtrl,
	})

	cluster := &storagev1alpha1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-cluster",
		},
		Spec: storagev1alpha1.ManagedClusterSpec{
			Addons: []storagev1alpha1.ClusterAddon{
				{
					Name:    "test-addon",
					Enabled: false, // Disabled
				},
			},
		},
	}

	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(cluster).
		WithStatusSubresource(&storagev1alpha1.ManagedCluster{}).
		Build()

	r := &AddonReconciler{
		Client:   c,
		Scheme:   scheme,
		Registry: mockReg,
		Controllers: map[string]parentaddon.AddonController{
			"test-addon": mockCtrl,
		},
	}

	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name: cluster.Name,
		},
	}

	result, err := r.Reconcile(context.Background(), req)
	assert.NoError(t, err)
	assert.Equal(t, ctrl.Result{}, result)

	// Verify addon controller was NOT called
	assert.Len(t, mockCtrl.reconcileCalls, 0)

	// Verify addon status was updated to Disabled
	var updatedCluster storagev1alpha1.ManagedCluster
	err = c.Get(context.Background(), types.NamespacedName{Name: cluster.Name}, &updatedCluster)
	require.NoError(t, err)
	require.Len(t, updatedCluster.Status.AddonStatus, 1)
	assert.Equal(t, "test-addon", updatedCluster.Status.AddonStatus[0].Name)
	assert.Equal(t, "Disabled", updatedCluster.Status.AddonStatus[0].State)
}

func TestAddonReconciler_Reconcile_ControllerError(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, storagev1alpha1.AddToScheme(scheme))

	mockReg := parentaddon.NewRegistry()
	mockCtrl := &mockAddonController{
		reconcileFunc: func(ctx context.Context, config parentaddon.AddonConfig) error {
			return errors.New("reconcile failed")
		},
	}
	mockReg.Register(&mockAddon{
		name:        "test-addon",
		managerCtrl: mockCtrl,
	})

	cluster := &storagev1alpha1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-cluster",
		},
		Spec: storagev1alpha1.ManagedClusterSpec{
			Addons: []storagev1alpha1.ClusterAddon{
				{
					Name:    "test-addon",
					Enabled: true,
				},
			},
		},
	}

	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(cluster).
		WithStatusSubresource(&storagev1alpha1.ManagedCluster{}).
		Build()

	r := &AddonReconciler{
		Client:   c,
		Scheme:   scheme,
		Registry: mockReg,
		Controllers: map[string]parentaddon.AddonController{
			"test-addon": mockCtrl,
		},
	}

	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name: cluster.Name,
		},
	}

	// Error is logged but does not fail reconciliation
	result, err := r.Reconcile(context.Background(), req)
	assert.NoError(t, err)
	assert.Equal(t, ctrl.Result{}, result)

	// Verify addon status was updated to Failed
	var updatedCluster storagev1alpha1.ManagedCluster
	err = c.Get(context.Background(), types.NamespacedName{Name: cluster.Name}, &updatedCluster)
	require.NoError(t, err)
	require.Len(t, updatedCluster.Status.AddonStatus, 1)
	assert.Equal(t, "test-addon", updatedCluster.Status.AddonStatus[0].Name)
	assert.Equal(t, "Failed", updatedCluster.Status.AddonStatus[0].State)
}

func TestAddonReconciler_Reconcile_NilController(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, storagev1alpha1.AddToScheme(scheme))

	mockReg := parentaddon.NewRegistry()
	mockReg.Register(&mockAddon{
		name:        "test-addon",
		managerCtrl: nil, // No controller
	})

	cluster := &storagev1alpha1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-cluster",
		},
		Spec: storagev1alpha1.ManagedClusterSpec{
			Addons: []storagev1alpha1.ClusterAddon{
				{
					Name:    "test-addon",
					Enabled: true,
				},
			},
		},
	}

	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(cluster).
		WithStatusSubresource(&storagev1alpha1.ManagedCluster{}).
		Build()

	r := &AddonReconciler{
		Client:      c,
		Scheme:      scheme,
		Registry:    mockReg,
		Controllers: map[string]parentaddon.AddonController{},
	}

	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name: cluster.Name,
		},
	}

	result, err := r.Reconcile(context.Background(), req)
	assert.NoError(t, err)
	assert.Equal(t, ctrl.Result{}, result)

	// Verify addon status was updated to Pending (no controller registered)
	var updatedCluster storagev1alpha1.ManagedCluster
	err = c.Get(context.Background(), types.NamespacedName{Name: cluster.Name}, &updatedCluster)
	require.NoError(t, err)
	require.Len(t, updatedCluster.Status.AddonStatus, 1)
	assert.Equal(t, "test-addon", updatedCluster.Status.AddonStatus[0].Name)
	assert.Equal(t, "Pending", updatedCluster.Status.AddonStatus[0].State)
}

func TestAddonReconciler_GetRegistry(t *testing.T) {
	scheme := runtime.NewScheme()

	c := fake.NewClientBuilder().
		WithScheme(scheme).
		Build()

	// With custom registry
	mockReg := parentaddon.NewRegistry()
	r := &AddonReconciler{
		Client:   c,
		Scheme:   scheme,
		Registry: mockReg,
	}
	assert.Equal(t, mockReg, r.getRegistry())

	// Without custom registry - falls back to global
	r2 := &AddonReconciler{
		Client: c,
		Scheme: scheme,
	}
	assert.Equal(t, parentaddon.GetRegistry(), r2.getRegistry())
}
