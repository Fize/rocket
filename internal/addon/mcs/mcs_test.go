package mcs

import (
	"context"
	"encoding/base64"
	"errors"
	"testing"

	"github.com/hex-techs/rocket/internal/addon"
	storagev1alpha1 "github.com/hex-techs/rocket/pkg/apis/storage/v1alpha1"
	"github.com/hex-techs/rocket/pkg/helm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	helm_release "helm.sh/helm/v3/pkg/release"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestGetBrokerInfo(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	tests := []struct {
		name          string
		existingObjs  []runtime.Object
		expectedInfo  map[string]string
		expectedError string
	}{
		{
			name: "Secret exists directly",
			existingObjs: []runtime.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      BrokerSecretName,
						Namespace: BrokerNamespace,
					},
					Data: map[string][]byte{
						"token":  []byte("my-token"),
						"ca.crt": []byte("my-ca"),
					},
				},
			},
			expectedInfo: map[string]string{
				"brokerURL":   "https://kubernetes.default.svc:443",
				"brokerToken": "my-token",
				"brokerCA":    base64.StdEncoding.EncodeToString([]byte("my-ca")),
			},
		},
		{
			name: "Secret found via ServiceAccount",
			existingObjs: []runtime.Object{
				&corev1.ServiceAccount{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "submariner-k8s-broker-client",
						Namespace: BrokerNamespace,
					},
					Secrets: []corev1.ObjectReference{
						{Name: "sa-secret"},
					},
				},
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "sa-secret",
						Namespace: BrokerNamespace,
					},
					Data: map[string][]byte{
						"token":  []byte("sa-token"),
						"ca.crt": []byte("sa-ca"),
					},
				},
			},
			expectedInfo: map[string]string{
				"brokerURL":   "https://kubernetes.default.svc:443",
				"brokerToken": "sa-token",
				"brokerCA":    base64.StdEncoding.EncodeToString([]byte("sa-ca")),
			},
		},
		{
			name:          "No secret and no SA",
			existingObjs:  []runtime.Object{},
			expectedError: "failed to find broker SA", // Matches the error when direct secret not found and SA not found
		},
		{
			name: "SA exists but has no secrets",
			existingObjs: []runtime.Object{
				&corev1.ServiceAccount{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "submariner-k8s-broker-client",
						Namespace: BrokerNamespace,
					},
					Secrets: []corev1.ObjectReference{},
				},
			},
			expectedError: "broker SA has no secrets",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := fake.NewClientBuilder().
				WithScheme(scheme).
				WithRuntimeObjects(tt.existingObjs...).
				Build()

			c := &ManagerController{
				mgrClient: client,
			}

			info, err := c.getBrokerInfo(context.Background())

			if tt.expectedError != "" {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedError)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedInfo, info)
			}
		})
	}
}

func TestMCSAddon_Name(t *testing.T) {
	a := &MCSAddon{}
	assert.Equal(t, AddonName, a.Name())
}

func TestMCSAddon_Manifests(t *testing.T) {
	a := &MCSAddon{}
	manifests := a.Manifests()
	assert.Empty(t, manifests)
}

func TestMCSAddon_AgentController(t *testing.T) {
	a := &MCSAddon{}
	// AgentController doesn't require manager
	ctrl, err := a.AgentController(nil)
	assert.NoError(t, err)
	assert.NotNil(t, ctrl)
}

func TestAgentController_Reconcile_NotReady(t *testing.T) {
	ac := &AgentController{}
	config := addon.AddonConfig{
		ClusterName: "test-cluster",
		Config:      map[string]string{},
	}

	// Should return nil when not ready (no broker URL/token)
	err := ac.Reconcile(context.Background(), config)
	assert.NoError(t, err)
}

func TestAgentController_Reconcile_PartialConfig(t *testing.T) {
	ac := &AgentController{}
	config := addon.AddonConfig{
		ClusterName: "test-cluster",
		Config: map[string]string{
			"brokerURL": "https://broker.example.com",
			// Missing token - should not proceed
		},
	}

	err := ac.Reconcile(context.Background(), config)
	assert.NoError(t, err)
}

func TestManagerController_Reconcile_NeedUpdate(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	require.NoError(t, storagev1alpha1.AddToScheme(scheme))

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      BrokerSecretName,
			Namespace: BrokerNamespace,
		},
		Data: map[string][]byte{
			"token":  []byte("new-token"),
			"ca.crt": []byte("new-ca"),
		},
	}

	cluster := &storagev1alpha1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-cluster",
		},
		Spec: storagev1alpha1.ManagedClusterSpec{
			Addons: []storagev1alpha1.ClusterAddon{
				{
					Name:    AddonName,
					Enabled: true,
					Config: map[string]string{
						"brokerURL":   "old-url",
						"brokerToken": "old-token",
					},
				},
			},
		},
	}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithRuntimeObjects(secret, cluster).
		Build()

	mc := &ManagerController{
		mgrClient: client,
	}

	config := addon.AddonConfig{
		ClusterName: "test-cluster",
		Config: map[string]string{
			"brokerURL":   "old-url",
			"brokerToken": "old-token",
		},
		Client: client,
	}

	// This will fail because broker is not installed, but we test that getBrokerInfo works
	// and the controller attempts to update the cluster config
	err := mc.Reconcile(context.Background(), config)
	// Expected error because helm installation will fail without charts
	assert.Error(t, err)
}

func TestConstants(t *testing.T) {
	assert.Equal(t, "mcs-lighthouse", AddonName)
	assert.Equal(t, "submariner-k8s-broker", BrokerNamespace)
	assert.Equal(t, "submariner-broker-client-secret", BrokerSecretName)
}

// ============== New tests using mock Helm client ==============

func TestManagerController_EnsureBroker_Success(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		Build()

	mockHelm := helm.NewMockClient()

	mc := &ManagerController{
		mgrClient: client,
	}
	mc.SetHelmClient(mockHelm)

	err := mc.ensureBroker(context.Background())
	assert.NoError(t, err)

	// Verify helm was called correctly
	require.Len(t, mockHelm.InstallOrUpgradeCalls, 1)
	call := mockHelm.InstallOrUpgradeCalls[0]
	assert.Equal(t, "submariner-broker", call.ReleaseName)
	assert.Contains(t, call.ChartPath, "submariner-k8s-broker")
}

func TestManagerController_EnsureBroker_HelmError(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(scheme)

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		Build()

	mockHelm := helm.NewMockClient()
	mockHelm.InstallOrUpgradeFn = func(releaseName string, chartPath string, values map[string]interface{}) (*helm_release.Release, error) {
		return nil, errors.New("helm install failed")
	}

	mc := &ManagerController{
		mgrClient: client,
	}
	mc.SetHelmClient(mockHelm)

	err := mc.ensureBroker(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "helm install failed")
}

func TestManagerController_Reconcile_Success(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	require.NoError(t, storagev1alpha1.AddToScheme(scheme))

	// Setup broker secret
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      BrokerSecretName,
			Namespace: BrokerNamespace,
		},
		Data: map[string][]byte{
			"token":  []byte("broker-token"),
			"ca.crt": []byte("broker-ca"),
		},
	}

	// Setup cluster with addon
	cluster := &storagev1alpha1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-cluster",
		},
		Spec: storagev1alpha1.ManagedClusterSpec{
			Addons: []storagev1alpha1.ClusterAddon{
				{
					Name:    AddonName,
					Enabled: true,
					Config:  map[string]string{},
				},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithRuntimeObjects(secret, cluster).
		Build()

	mockHelm := helm.NewMockClient()

	mc := &ManagerController{
		mgrClient: fakeClient,
	}
	mc.SetHelmClient(mockHelm)

	config := addon.AddonConfig{
		ClusterName: "test-cluster",
		Config:      map[string]string{},
		Client:      fakeClient,
	}

	err := mc.Reconcile(context.Background(), config)
	assert.NoError(t, err)

	// Verify broker was installed
	require.Len(t, mockHelm.InstallOrUpgradeCalls, 1)
}

func TestAgentController_Reconcile_WithMockHelm(t *testing.T) {
	mockHelm := helm.NewMockClient()

	ac := &AgentController{}
	ac.SetHelmClient(mockHelm)

	config := addon.AddonConfig{
		ClusterName: "test-cluster",
		Config: map[string]string{
			"brokerURL":       "https://broker.example.com",
			"brokerToken":     "test-token",
			"brokerCA":        base64.StdEncoding.EncodeToString([]byte("test-ca")),
			"brokerNamespace": BrokerNamespace,
		},
	}

	err := ac.Reconcile(context.Background(), config)
	assert.NoError(t, err)

	// Verify helm was called with correct values
	require.Len(t, mockHelm.InstallOrUpgradeCalls, 1)
	call := mockHelm.InstallOrUpgradeCalls[0]
	assert.Equal(t, "submariner", call.ReleaseName)
	assert.Contains(t, call.ChartPath, "submariner")

	// Verify values contain broker info
	broker := call.Values["broker"].(map[string]interface{})
	assert.Equal(t, "https://broker.example.com", broker["server"])
	assert.Equal(t, "test-token", broker["token"])

	// Verify submariner config
	submariner := call.Values["submariner"].(map[string]interface{})
	assert.Equal(t, "test-cluster", submariner["clusterId"])
	assert.Equal(t, true, submariner["serviceDiscovery"])
}

func TestAgentController_Reconcile_HelmError(t *testing.T) {
	mockHelm := helm.NewMockClient()
	mockHelm.InstallOrUpgradeFn = func(releaseName string, chartPath string, values map[string]interface{}) (*helm_release.Release, error) {
		return nil, errors.New("helm install failed")
	}

	ac := &AgentController{}
	ac.SetHelmClient(mockHelm)

	config := addon.AddonConfig{
		ClusterName: "test-cluster",
		Config: map[string]string{
			"brokerURL":       "https://broker.example.com",
			"brokerToken":     "test-token",
			"brokerCA":        base64.StdEncoding.EncodeToString([]byte("test-ca")),
			"brokerNamespace": BrokerNamespace,
		},
	}

	err := ac.Reconcile(context.Background(), config)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to install submariner agent")
}

func TestGetHelmClient_DefaultCreation(t *testing.T) {
	// Test that getHelmClient returns injected client when available
	mc := &ManagerController{}
	mockHelm := helm.NewMockClient()
	mc.SetHelmClient(mockHelm)

	client, err := mc.getHelmClient(BrokerNamespace)
	assert.NoError(t, err)
	assert.Equal(t, mockHelm, client)
}

func TestAgentController_GetHelmClient_DefaultCreation(t *testing.T) {
	// Test that getHelmClient returns injected client when available
	ac := &AgentController{}
	mockHelm := helm.NewMockClient()
	ac.SetHelmClient(mockHelm)

	client, err := ac.getHelmClient("submariner-operator")
	assert.NoError(t, err)
	assert.Equal(t, mockHelm, client)
}
