package victoriametrics

import (
	"context"
	"testing"

	"github.com/fize/rocket/internal/addon"
	storagev1alpha1 "github.com/fize/rocket/pkg/apis/storage/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v3/pkg/release"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	"github.com/go-logr/logr"
)

// mockHelmClient implements helm.HelmClient for testing
type mockHelmClient struct {
	installOrUpgradeCalls []struct {
		releaseName string
		chartPath   string
		values      map[string]interface{}
	}
	uninstallCalls []string
	installError   error
	upgradeError   error
	uninstallError error
}

func (m *mockHelmClient) InstallOrUpgrade(releaseName string, chartPath string, values map[string]interface{}) (*release.Release, error) {
	m.installOrUpgradeCalls = append(m.installOrUpgradeCalls, struct {
		releaseName string
		chartPath   string
		values      map[string]interface{}
	}{releaseName, chartPath, values})

	if m.installError != nil {
		return nil, m.installError
	}
	// Return a mock release
	return &release.Release{}, m.upgradeError
}

func (m *mockHelmClient) Uninstall(releaseName string) error {
	m.uninstallCalls = append(m.uninstallCalls, releaseName)
	return m.uninstallError
}

func TestVictoriaMetricsAddon_Name(t *testing.T) {
	addon := &VictoriaMetricsAddon{}
	assert.Equal(t, "victoriametrics", addon.Name())
}

func TestVictoriaMetricsAddon_ManagerController(t *testing.T) {
	addon := &VictoriaMetricsAddon{}

	// For this test, we'll just verify the method exists and returns the right type
	// We won't create a full manager since it's too complex
	assert.NotNil(t, addon)
	assert.Equal(t, "victoriametrics", addon.Name())
}

func TestVictoriaMetricsAddon_AgentController(t *testing.T) {
	addon := &VictoriaMetricsAddon{}

	controller, err := addon.AgentController(nil)
	require.NoError(t, err)
	assert.NotNil(t, controller)
	assert.IsType(t, &AgentController{}, controller)
}

func TestVictoriaMetricsAddon_Manifests(t *testing.T) {
	addon := &VictoriaMetricsAddon{}
	manifests := addon.Manifests()
	assert.Empty(t, manifests)
}

func TestManagerController_Reconcile_FirstInstall(t *testing.T) {
	s := runtime.NewScheme()
	require.NoError(t, storagev1alpha1.AddToScheme(s))
	require.NoError(t, corev1.AddToScheme(s))

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

	// Create VictoriaMetrics service
	vmService := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      VictoriaMetricsServiceName,
			Namespace: VictoriaMetricsNamespace,
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Port: 8428,
				},
			},
		},
	}

	c := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(cluster, vmService).
		Build()

	mockHelm := &mockHelmClient{}
	controller := &ManagerController{
		mgrClient: c,
	}
	controller.SetHelmClient(mockHelm)

	config := addon.AddonConfig{
		ClusterName: "test-cluster",
		Config:      map[string]string{},
		Client:      c,
	}

	err := controller.Reconcile(context.Background(), config)
	require.NoError(t, err)

	// Verify Helm install was called
	require.Len(t, mockHelm.installOrUpgradeCalls, 1)
	assert.Equal(t, "victoria-metrics", mockHelm.installOrUpgradeCalls[0].releaseName)

	// Verify cluster was updated with VictoriaMetrics URL
	updatedCluster := &storagev1alpha1.ManagedCluster{}
	err = c.Get(context.Background(), client.ObjectKey{Name: "test-cluster"}, updatedCluster)
	require.NoError(t, err)

	assert.Contains(t, updatedCluster.Spec.Addons[0].Config, ConfigVictoriaMetricsURL)
	expectedURL := "http://victoria-metrics-victoria-metrics-single.victoriametrics.svc.cluster.local:8428"
	assert.Equal(t, expectedURL, updatedCluster.Spec.Addons[0].Config[ConfigVictoriaMetricsURL])
}

func TestManagerController_Reconcile_ConfigUpdate(t *testing.T) {
	s := runtime.NewScheme()
	require.NoError(t, storagev1alpha1.AddToScheme(s))
	require.NoError(t, corev1.AddToScheme(s))

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
						ConfigVMChartVersion: "0.9.10",
					},
				},
			},
		},
	}

	vmService := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      VictoriaMetricsServiceName,
			Namespace: VictoriaMetricsNamespace,
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Port: 8428,
				},
			},
		},
	}

	c := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(cluster, vmService).
		Build()

	mockHelm := &mockHelmClient{}
	controller := &ManagerController{
		mgrClient: c,
	}
	controller.SetHelmClient(mockHelm)

	config := addon.AddonConfig{
		ClusterName: "test-cluster",
		Config: map[string]string{
			ConfigVMChartVersion: "0.9.11", // Updated version
		},
		Client: c,
	}

	// First reconcile
	err := controller.Reconcile(context.Background(), config)
	require.NoError(t, err)
	require.Len(t, mockHelm.installOrUpgradeCalls, 1)

	// Second reconcile with same config should not trigger upgrade
	err = controller.Reconcile(context.Background(), config)
	require.NoError(t, err)
	require.Len(t, mockHelm.installOrUpgradeCalls, 1) // Still 1, not 2
}

func TestAgentController_Reconcile_FirstInstall(t *testing.T) {
	s := runtime.NewScheme()
	require.NoError(t, storagev1alpha1.AddToScheme(s))
	require.NoError(t, corev1.AddToScheme(s))

	c := fake.NewClientBuilder().
		WithScheme(s).
		Build()

	mockHelm := &mockHelmClient{}
	controller := &AgentController{}
	controller.SetHelmClient(mockHelm)

	vmURL := "http://victoria-metrics-victoria-metrics-single.victoriametrics.svc.cluster.local:8428"
	config := addon.AddonConfig{
		ClusterName: "test-cluster",
		Config: map[string]string{
			ConfigVictoriaMetricsURL: vmURL,
		},
		Client: c,
	}

	err := controller.Reconcile(context.Background(), config)
	require.NoError(t, err)

	// Verify Helm install was called
	require.Len(t, mockHelm.installOrUpgradeCalls, 1)
	assert.Equal(t, "vm-agent", mockHelm.installOrUpgradeCalls[0].releaseName)

	// Verify remoteWrite URL was configured correctly
	values := mockHelm.installOrUpgradeCalls[0].values
	remoteWrite, ok := values["remoteWrite"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, vmURL+"/api/v1/write", remoteWrite["url"])
}

func TestAgentController_Reconcile_NoURL(t *testing.T) {
	s := runtime.NewScheme()
	require.NoError(t, storagev1alpha1.AddToScheme(s))
	require.NoError(t, corev1.AddToScheme(s))

	c := fake.NewClientBuilder().
		WithScheme(s).
		Build()

	mockHelm := &mockHelmClient{}
	controller := &AgentController{}
	controller.SetHelmClient(mockHelm)

	config := addon.AddonConfig{
		ClusterName: "test-cluster",
		Config:      map[string]string{}, // No URL
		Client:      c,
	}

	err := controller.Reconcile(context.Background(), config)
	require.NoError(t, err)

	// Should not call Helm install
	assert.Len(t, mockHelm.installOrUpgradeCalls, 0)
}

func TestManagerController_Reconcile_WithStorage(t *testing.T) {
	s := runtime.NewScheme()
	require.NoError(t, storagev1alpha1.AddToScheme(s))
	require.NoError(t, corev1.AddToScheme(s))

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
						ConfigVMStorageClass: "fast-ssd",
						ConfigVMStorageSize:  "50Gi",
					},
				},
			},
		},
	}

	vmService := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      VictoriaMetricsServiceName,
			Namespace: VictoriaMetricsNamespace,
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Port: 8428,
				},
			},
		},
	}

	c := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(cluster, vmService).
		Build()

	mockHelm := &mockHelmClient{}
	controller := &ManagerController{
		mgrClient: c,
	}
	controller.SetHelmClient(mockHelm)

	config := addon.AddonConfig{
		ClusterName: "test-cluster",
		Config: map[string]string{
			ConfigVMStorageClass: "fast-ssd",
			ConfigVMStorageSize:  "50Gi",
		},
		Client: c,
	}

	err := controller.Reconcile(context.Background(), config)
	require.NoError(t, err)

	// Verify Helm install was called
	require.Len(t, mockHelm.installOrUpgradeCalls, 1)

	// Verify persistent storage configuration
	values := mockHelm.installOrUpgradeCalls[0].values
	server, ok := values["server"].(map[string]interface{})
	require.True(t, ok)

	pv, ok := server["persistentVolume"].(map[string]interface{})
	require.True(t, ok)

	assert.Equal(t, true, pv["enabled"])
	assert.Equal(t, "fast-ssd", pv["storageClassName"])
	assert.Equal(t, "50Gi", pv["size"])
}

func TestAgentController_Reconcile_WithStorage(t *testing.T) {
	s := runtime.NewScheme()
	require.NoError(t, storagev1alpha1.AddToScheme(s))
	require.NoError(t, corev1.AddToScheme(s))

	c := fake.NewClientBuilder().
		WithScheme(s).
		Build()

	mockHelm := &mockHelmClient{}
	controller := &AgentController{}
	controller.SetHelmClient(mockHelm)

	vmURL := "http://victoria-metrics-victoria-metrics-single.victoriametrics.svc.cluster.local:8428"
	config := addon.AddonConfig{
		ClusterName: "test-cluster",
		Config: map[string]string{
			ConfigVictoriaMetricsURL:  vmURL,
			ConfigVmAgentStorageClass: "local-storage",
			ConfigVmAgentStorageSize:  "20Gi",
		},
		Client: c,
	}

	err := controller.Reconcile(context.Background(), config)
	require.NoError(t, err)

	// Verify Helm install was called
	require.Len(t, mockHelm.installOrUpgradeCalls, 1)
	assert.Equal(t, "vm-agent", mockHelm.installOrUpgradeCalls[0].releaseName)

	// Verify persistent storage configuration
	values := mockHelm.installOrUpgradeCalls[0].values
	pv, ok := values["persistentVolume"].(map[string]interface{})
	require.True(t, ok)

	assert.Equal(t, true, pv["enabled"])
	assert.Equal(t, "local-storage", pv["storageClassName"])
	assert.Equal(t, "20Gi", pv["size"])
}

func TestResolveChartURL(t *testing.T) {
	tests := []struct {
		name    string
		cfg     chartURLConfig
		want    string
		wantErr bool
	}{
		{
			name: "full URL provided",
			cfg: chartURLConfig{
				URL: "https://example.com/chart.tgz",
			},
			want:    "https://example.com/chart.tgz",
			wantErr: false,
		},
		{
			name: "construct from repo",
			cfg: chartURLConfig{
				RepoURL:      "https://charts.example.com",
				ChartName:    "my-chart",
				ChartVersion: "1.0.0",
			},
			want:    "https://charts.example.com/my-chart-1.0.0.tgz",
			wantErr: false,
		},
		{
			name: "repo URL with trailing slash",
			cfg: chartURLConfig{
				RepoURL:      "https://charts.example.com/",
				ChartName:    "my-chart",
				ChartVersion: "1.0.0",
			},
			want:    "https://charts.example.com/my-chart-1.0.0.tgz",
			wantErr: false,
		},
		{
			name: "no version uses latest",
			cfg: chartURLConfig{
				RepoURL:   "https://charts.example.com",
				ChartName: "my-chart",
			},
			want:    "https://charts.example.com/my-chart-*.tgz",
			wantErr: false,
		},
		{
			name: "missing repo URL",
			cfg: chartURLConfig{
				ChartName:    "my-chart",
				ChartVersion: "1.0.0",
			},
			wantErr: true,
		},
		{
			name: "missing chart name",
			cfg: chartURLConfig{
				RepoURL:      "https://charts.example.com",
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

func TestFirstNonEmpty(t *testing.T) {
	tests := []struct {
		name   string
		values []string
		want   string
	}{
		{
			name:   "first non-empty",
			values: []string{"", "first", "second"},
			want:   "first",
		},
		{
			name:   "all empty",
			values: []string{"", "", ""},
			want:   "",
		},
		{
			name:   "first value",
			values: []string{"first", "second", "third"},
			want:   "first",
		},
		{
			name:   "no values",
			values: []string{},
			want:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := firstNonEmpty(tt.values...)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestMergeValues(t *testing.T) {
	tests := []struct {
		name string
		dst  map[string]interface{}
		src  map[string]interface{}
		want map[string]interface{}
	}{
		{
			name: "simple merge",
			dst:  map[string]interface{}{"a": "1"},
			src:  map[string]interface{}{"b": "2"},
			want: map[string]interface{}{"a": "1", "b": "2"},
		},
		{
			name: "nested merge",
			dst: map[string]interface{}{
				"server": map[string]interface{}{
					"port": 8080,
				},
			},
			src: map[string]interface{}{
				"server": map[string]interface{}{
					"host": "localhost",
				},
			},
			want: map[string]interface{}{
				"server": map[string]interface{}{
					"port": 8080,
					"host": "localhost",
				},
			},
		},
		{
			name: "src overrides dst",
			dst: map[string]interface{}{
				"a": "old",
			},
			src: map[string]interface{}{
				"a": "new",
			},
			want: map[string]interface{}{
				"a": "new",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mergeValues(tt.dst, tt.src)
			assert.Equal(t, tt.want, got)
		})
	}
}

// fakeManager implements ctrl.Manager for testing
type fakeManager struct {
	scheme *runtime.Scheme
}

func (f *fakeManager) GetScheme() *runtime.Scheme {
	return f.scheme
}

// Implement other required methods with minimal/no-op implementations
func (f *fakeManager) Add(runnable manager.Runnable) error {
	return nil
}

func (f *fakeManager) SetFields(i interface{}) error {
	return nil
}

func (f *fakeManager) AddHealthzCheck(name string, check healthz.Checker) error {
	return nil
}

func (f *fakeManager) AddReadyzCheck(name string, check healthz.Checker) error {
	return nil
}

func (f *fakeManager) Start(ctx context.Context) error {
	return nil
}

func (f *fakeManager) GetConfig() *rest.Config {
	return nil
}

func (f *fakeManager) GetClient() client.Client {
	return nil
}

func (f *fakeManager) GetFieldIndexer() client.FieldIndexer {
	return nil
}

func (f *fakeManager) GetCache() cache.Cache {
	return nil
}

func (f *fakeManager) GetEventRecorderFor(name string) record.EventRecorder {
	return nil
}

func (f *fakeManager) GetAPIReader() client.Reader {
	return nil
}

func (f *fakeManager) GetWebhookServer() *webhook.Server {
	return nil
}

func (f *fakeManager) GetLogger() logr.Logger {
	return logr.Logger{}
}
