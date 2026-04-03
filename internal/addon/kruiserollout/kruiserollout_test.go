package kruiserollout

import (
	"context"
	"testing"

	"github.com/hex-techs/rocket/internal/addon"
	"github.com/hex-techs/rocket/pkg/helm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v3/pkg/release"
)

// mockHelmClient implements helm.HelmClient for testing
type mockHelmClient struct {
	installOrUpgradeCalled bool
	lastReleaseName        string
	lastChartURL           string
	lastValues             map[string]interface{}
	installOrUpgradeError  error
}

func (m *mockHelmClient) InstallOrUpgrade(releaseName, chartURL string, values map[string]interface{}) (*release.Release, error) {
	m.installOrUpgradeCalled = true
	m.lastReleaseName = releaseName
	m.lastChartURL = chartURL
	m.lastValues = values
	return &release.Release{Name: releaseName}, m.installOrUpgradeError
}

func (m *mockHelmClient) Uninstall(releaseName string) error {
	return nil
}

// Ensure mock implements interface
var _ helm.HelmClient = (*mockHelmClient)(nil)

func TestKruiseRolloutAddon_Name(t *testing.T) {
	a := &KruiseRolloutAddon{}
	assert.Equal(t, "kruise-rollout", a.Name())
}

func TestKruiseRolloutAddon_ManagerController(t *testing.T) {
	a := &KruiseRolloutAddon{}
	// Test that ManagerController returns a valid controller without needing a real manager
	controller, err := a.ManagerController(nil)
	require.NoError(t, err)
	assert.NotNil(t, controller)
}

func TestKruiseRolloutAddon_AgentController(t *testing.T) {
	a := &KruiseRolloutAddon{}
	// Test that AgentController returns a valid controller without needing a real manager
	controller, err := a.AgentController(nil)
	require.NoError(t, err)
	assert.NotNil(t, controller)
}

func TestKruiseRolloutAddon_Manifests(t *testing.T) {
	a := &KruiseRolloutAddon{}
	manifests := a.Manifests()
	assert.Empty(t, manifests)
}

func TestManagerController_Reconcile(t *testing.T) {
	controller := &ManagerController{}

	config := addon.AddonConfig{
		ClusterName: "test-cluster",
		Config:      map[string]string{},
	}

	err := controller.Reconcile(context.Background(), config)
	assert.NoError(t, err)
}

func TestAgentController_Reconcile(t *testing.T) {
	mockClient := &mockHelmClient{}
	controller := &AgentController{}
	controller.SetHelmClient(mockClient)

	config := addon.AddonConfig{
		ClusterName: "test-cluster",
		Config: map[string]string{
			ConfigChartRepoURL: "https://openkruise.github.io/charts/",
			ConfigChartName:    "kruise-rollout",
			ConfigChartVersion: "0.6.2",
		},
	}

	err := controller.Reconcile(context.Background(), config)
	assert.NoError(t, err)
	assert.True(t, mockClient.installOrUpgradeCalled)
	assert.Equal(t, "kruise-rollout", mockClient.lastReleaseName)
}

func TestAgentController_Reconcile_Defaults(t *testing.T) {
	mockClient := &mockHelmClient{}
	controller := &AgentController{}
	controller.SetHelmClient(mockClient)

	config := addon.AddonConfig{
		ClusterName: "test-cluster",
		Config:      map[string]string{},
	}

	err := controller.Reconcile(context.Background(), config)
	assert.NoError(t, err)
	assert.True(t, mockClient.installOrUpgradeCalled)
}

func TestResolveChartURL(t *testing.T) {
	tests := []struct {
		name        string
		cfg         ChartConfig
		wantURL     string
		wantErr     bool
		errContains string
	}{
		{
			name: "full URL provided",
			cfg: ChartConfig{
				URL: "https://example.com/chart.tgz",
			},
			wantURL: "https://example.com/chart.tgz",
		},
		{
			name: "construct from repo",
			cfg: ChartConfig{
				RepoURL:      "https://charts.example.com",
				ChartName:    "my-chart",
				ChartVersion: "1.0.0",
			},
			wantURL: "https://charts.example.com/my-chart-1.0.0.tgz",
		},
		{
			name: "repo URL with trailing slash",
			cfg: ChartConfig{
				RepoURL:      "https://charts.example.com/",
				ChartName:    "my-chart",
				ChartVersion: "1.0.0",
			},
			wantURL: "https://charts.example.com/my-chart-1.0.0.tgz",
		},
		{
			name: "empty version uses default",
			cfg: ChartConfig{
				RepoURL:      "https://charts.example.com",
				ChartName:    "my-chart",
				ChartVersion: "",
			},
			wantURL: "https://charts.example.com/my-chart-0.6.2.tgz",
		},
		{
			name: "invalid URL scheme",
			cfg: ChartConfig{
				URL: "ftp://example.com/chart.tgz",
			},
			wantErr:     true,
			errContains: "must be a valid HTTP/HTTPS URL",
		},
		{
			name: "missing chart name",
			cfg: ChartConfig{
				RepoURL:      "https://charts.example.com",
				ChartName:    "",
				ChartVersion: "1.0.0",
			},
			wantErr:     true,
			errContains: "chart name must be set",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveChartURL(tt.cfg)
			if tt.wantErr {
				assert.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tt.wantURL, got)
		})
	}
}

func TestAgentController_ConfigChanged(t *testing.T) {
	controller := &AgentController{}

	// First call should return true (initialization)
	cfg1 := map[string]string{
		ConfigChartURL:     "https://example.com/chart1.tgz",
		ConfigChartVersion: "1.0.0",
	}
	assert.True(t, controller.hasConfigChanged(cfg1))

	// Same config should return false
	assert.False(t, controller.hasConfigChanged(cfg1))

	// Different config should return true
	cfg2 := map[string]string{
		ConfigChartURL:     "https://example.com/chart2.tgz",
		ConfigChartVersion: "2.0.0",
	}
	assert.True(t, controller.hasConfigChanged(cfg2))
}

func TestAgentController_updateLastConfig(t *testing.T) {
	controller := &AgentController{}

	cfg := map[string]string{
		ConfigChartURL:          "https://example.com/chart.tgz",
		ConfigChartRepoURL:      "https://charts.example.com",
		ConfigChartName:         "kruise-rollout",
		ConfigChartVersion:      "1.0.0",
		ConfigValuesConfigMap:   "my-configmap",
		ConfigValuesSecret:      "my-secret",
		ConfigValuesNamespace:   "default",
	}

	controller.updateLastConfig(cfg)

	// Verify the config was stored
	assert.NotNil(t, controller.lastConfig)
	assert.Equal(t, "https://example.com/chart.tgz", controller.lastConfig[ConfigChartURL])
	assert.Equal(t, "1.0.0", controller.lastConfig[ConfigChartVersion])
}

func TestFirstNonEmpty(t *testing.T) {
	tests := []struct {
		name   string
		values []string
		want   string
	}{
		{
			name:   "first non-empty",
			values: []string{"", "a", "b"},
			want:   "a",
		},
		{
			name:   "all empty",
			values: []string{"", "", ""},
			want:   "",
		},
		{
			name:   "first value",
			values: []string{"first", "second"},
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
			dst:  map[string]interface{}{"outer": map[string]interface{}{"a": "1"}},
			src:  map[string]interface{}{"outer": map[string]interface{}{"b": "2"}},
			want: map[string]interface{}{"outer": map[string]interface{}{"a": "1", "b": "2"}},
		},
		{
			name: "src overrides dst",
			dst:  map[string]interface{}{"a": "1"},
			src:  map[string]interface{}{"a": "2"},
			want: map[string]interface{}{"a": "2"},
		},
		{
			name: "nil dst",
			dst:  nil,
			src:  map[string]interface{}{"a": "1"},
			want: map[string]interface{}{"a": "1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mergeValues(tt.dst, tt.src)
			assert.Equal(t, tt.want, got)
		})
	}
}
