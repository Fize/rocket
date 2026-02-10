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
	"k8s.io/apimachinery/pkg/types"
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
			name:          "No secret",
			existingObjs:  []runtime.Object{},
			expectedError: "failed to get broker secret",
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

			info, err := c.getBrokerInfo(context.Background(), nil)

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
	mockHelm := helm.NewMockClient()
	mc.SetHelmClient(mockHelm)

	config := addon.AddonConfig{
		ClusterName: "test-cluster",
		Config: map[string]string{
			"brokerURL":   "old-url",
			"brokerToken": "old-token",
		},
		Client: client,
	}

	err := mc.Reconcile(context.Background(), config)
	assert.NoError(t, err)

	var updated storagev1alpha1.ManagedCluster
	require.NoError(t, client.Get(context.Background(), types.NamespacedName{Name: "test-cluster"}, &updated))
	require.Len(t, updated.Spec.Addons, 1)
	assert.Equal(t, "old-url", updated.Spec.Addons[0].Config["brokerURL"])
	assert.Equal(t, "new-token", updated.Spec.Addons[0].Config["brokerToken"])
	assert.Equal(t, BrokerNamespace, updated.Spec.Addons[0].Config["brokerNamespace"])
}

func TestConstants(t *testing.T) {
	assert.Equal(t, "mcs-lighthouse", AddonName)
	assert.Equal(t, "submariner-k8s-broker", BrokerNamespace)
	assert.Equal(t, "submariner-k8s-broker-client-token", BrokerSecretName)
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

	err := mc.ensureBroker(context.Background(), addon.AddonConfig{
		Config: map[string]string{},
		Client: client,
	})
	assert.NoError(t, err)

	// Verify helm was called correctly
	require.Len(t, mockHelm.InstallOrUpgradeCalls, 1)
	call := mockHelm.InstallOrUpgradeCalls[0]
	assert.Equal(t, "submariner-k8s-broker", call.ReleaseName)
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

	err := mc.ensureBroker(context.Background(), addon.AddonConfig{
		Config: map[string]string{},
		Client: client,
	})
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

func TestResolveChartURL_VersionOptional(t *testing.T) {
	// Test that version can be omitted and defaults to "*" (latest)
	tests := []struct {
		name    string
		cfg     chartURLConfig
		want    string
		wantErr bool
	}{
		{
			name: "With explicit version",
			cfg: chartURLConfig{
				RepoURL:      "https://submariner-io.github.io/submariner-charts/charts",
				ChartName:    "submariner-k8s-broker",
				ChartVersion: "0.23.0-m0",
			},
			want: "https://submariner-io.github.io/submariner-charts/charts/submariner-k8s-broker-0.23.0-m0.tgz",
		},
		{
			name: "Without version (uses latest)",
			cfg: chartURLConfig{
				RepoURL:      "https://submariner-io.github.io/submariner-charts/charts",
				ChartName:    "submariner-k8s-broker",
				ChartVersion: "",
			},
			want: "https://submariner-io.github.io/submariner-charts/charts/submariner-k8s-broker-*.tgz",
		},
		{
			name: "With direct URL (ignores version)",
			cfg: chartURLConfig{
				URL:          "https://example.com/charts/my-chart.tgz",
				ChartVersion: "1.0.0", // Should be ignored
			},
			want: "https://example.com/charts/my-chart.tgz",
		},
		{
			name: "Missing chart name",
			cfg: chartURLConfig{
				RepoURL:      "https://submariner-io.github.io/submariner-charts/charts",
				ChartName:    "",
				ChartVersion: "0.23.0",
			},
			wantErr: true,
		},
		{
			name: "Invalid repo URL",
			cfg: chartURLConfig{
				RepoURL:      "not-a-url",
				ChartName:    "my-chart",
				ChartVersion: "1.0.0",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveChartURL(tt.cfg)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.want, got)
			}
		})
	}
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
func TestManagerController_BrokerConfigChanged_Detection(t *testing.T) {
	// Test that broker config changes are properly detected and trigger upgrade
	mc := &ManagerController{}

	// First reconciliation - should detect as changed (first time)
	config1 := addon.AddonConfig{
		Config: map[string]string{
			ConfigBrokerChartVersion: "0.23.0-m0",
		},
	}
	changed := mc.shouldUpdateBroker(config1.Config)
	assert.True(t, changed, "should detect change on first reconciliation")

	// Second reconciliation with same config - should still detect change
	// because we haven't called updateLastBrokerConfig
	changed = mc.shouldUpdateBroker(config1.Config)
	assert.True(t, changed, "should still detect change when config hasn't been marked as applied")

	// Third reconciliation with version change - should detect change
	config2 := addon.AddonConfig{
		Config: map[string]string{
			ConfigBrokerChartVersion: "0.24.0", // Changed version
		},
	}
	changed = mc.shouldUpdateBroker(config2.Config)
	assert.True(t, changed, "should detect change when version updated")

	// Fourth reconciliation with different repo - should detect change
	config3 := addon.AddonConfig{
		Config: map[string]string{
			ConfigBrokerChartVersion: "0.24.0",
			ConfigBrokerChartRepoURL: "https://custom-repo.example.com", // Changed repo
		},
	}
	changed = mc.shouldUpdateBroker(config3.Config)
	assert.True(t, changed, "should detect change when repo URL updated")
}

func TestAgentController_SubmarinerConfigChanged_Detection(t *testing.T) {
	// Test that submariner config changes are properly detected and trigger upgrade
	ac := &AgentController{}

	// First reconciliation - should detect as changed (first time)
	config1 := addon.AddonConfig{
		Config: map[string]string{
			ConfigSubmarinerChartVersion: "0.7.0",
			"brokerURL":                  "https://broker.example.com",
			"brokerToken":                "token123",
		},
	}
	changed := ac.hasSubmarinerConfigChanged(config1.Config)
	assert.True(t, changed, "should detect change on first reconciliation")

	// Second reconciliation with same config - should not detect change
	changed = ac.hasSubmarinerConfigChanged(config1.Config)
	assert.False(t, changed, "should not detect change when config is same")

	// Third reconciliation with chart version change - should detect change
	config2 := addon.AddonConfig{
		Config: map[string]string{
			ConfigSubmarinerChartVersion: "0.8.0", // Changed version
			"brokerURL":                  "https://broker.example.com",
			"brokerToken":                "token123",
		},
	}
	changed = ac.hasSubmarinerConfigChanged(config2.Config)
	assert.True(t, changed, "should detect change when chart version updated")

	// Fourth reconciliation with broker token change - should detect change
	config3 := addon.AddonConfig{
		Config: map[string]string{
			ConfigSubmarinerChartVersion: "0.8.0",
			"brokerURL":                  "https://broker.example.com",
			"brokerToken":                "token456", // Changed token
		},
	}
	changed = ac.hasSubmarinerConfigChanged(config3.Config)
	assert.True(t, changed, "should detect change when broker token updated")

	// Fifth reconciliation with same token - should not detect change
	changed = ac.hasSubmarinerConfigChanged(config3.Config)
	assert.False(t, changed, "should not detect change when config is same after update")
}
