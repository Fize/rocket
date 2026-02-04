package helm

import (
	"helm.sh/helm/v3/pkg/release"
)

// MockClient is a mock implementation of HelmClient for testing
type MockClient struct {
	// InstallOrUpgradeFn allows customizing the InstallOrUpgrade behavior
	InstallOrUpgradeFn func(releaseName string, chartPath string, values map[string]interface{}) (*release.Release, error)
	// UninstallFn allows customizing the Uninstall behavior
	UninstallFn func(releaseName string) error

	// Tracking calls for assertions
	InstallOrUpgradeCalls []InstallOrUpgradeCall
	UninstallCalls        []string
}

// InstallOrUpgradeCall records the parameters of an InstallOrUpgrade call
type InstallOrUpgradeCall struct {
	ReleaseName string
	ChartPath   string
	Values      map[string]interface{}
}

// Ensure MockClient implements HelmClient
var _ HelmClient = (*MockClient)(nil)

// NewMockClient creates a new MockClient with default success behavior
func NewMockClient() *MockClient {
	return &MockClient{
		InstallOrUpgradeFn: func(releaseName string, chartPath string, values map[string]interface{}) (*release.Release, error) {
			return &release.Release{
				Name:      releaseName,
				Namespace: "default",
				Version:   1,
				Info: &release.Info{
					Status: release.StatusDeployed,
				},
			}, nil
		},
		UninstallFn: func(releaseName string) error {
			return nil
		},
		InstallOrUpgradeCalls: []InstallOrUpgradeCall{},
		UninstallCalls:        []string{},
	}
}

// InstallOrUpgrade implements HelmClient
func (m *MockClient) InstallOrUpgrade(releaseName string, chartPath string, values map[string]interface{}) (*release.Release, error) {
	m.InstallOrUpgradeCalls = append(m.InstallOrUpgradeCalls, InstallOrUpgradeCall{
		ReleaseName: releaseName,
		ChartPath:   chartPath,
		Values:      values,
	})
	if m.InstallOrUpgradeFn != nil {
		return m.InstallOrUpgradeFn(releaseName, chartPath, values)
	}
	return nil, nil
}

// Uninstall implements HelmClient
func (m *MockClient) Uninstall(releaseName string) error {
	m.UninstallCalls = append(m.UninstallCalls, releaseName)
	if m.UninstallFn != nil {
		return m.UninstallFn(releaseName)
	}
	return nil
}
