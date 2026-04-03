package kruiserollout

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/hex-techs/rocket/internal/addon"
	"github.com/hex-techs/rocket/pkg/helm"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"
)

// ManagerController handles kruise-rollout installation on Hub clusters
// If the Hub cluster is also used for deploying workloads with rollout strategy,
// kruise-rollout needs to be installed on the Hub cluster as well.
type ManagerController struct {
	helmClient helm.HelmClient
	mu         sync.RWMutex
	lastConfig map[string]string
}

// SetHelmClient allows setting a custom HelmClient for testing
func (c *ManagerController) SetHelmClient(hc helm.HelmClient) {
	c.helmClient = hc
}

// getHelmClient returns the helm client, creating one if needed
func (c *ManagerController) getHelmClient(namespace string) (helm.HelmClient, error) {
	if c.helmClient != nil {
		return c.helmClient, nil
	}
	return helm.NewClientInCluster(namespace)
}

// Reconcile installs or upgrades kruise-rollout on the Hub cluster if enabled
// This is necessary when the Hub cluster also runs workloads with rollout strategies
func (c *ManagerController) Reconcile(ctx context.Context, config addon.AddonConfig) error {
	// Check if chart configuration has changed
	if c.hasConfigChanged(config.Config) {
		// Clear last config to trigger re-install/upgrade
		c.lastConfig = nil
	}

	// Resolve chart URL
	chartURL, err := resolveChartURL(ChartConfig{
		URL:          config.Config[ConfigChartURL],
		RepoURL:      firstNonEmpty(config.Config[ConfigChartRepoURL], os.Getenv(EnvChartRepoURL), DefaultRepoURL),
		ChartName:    firstNonEmpty(config.Config[ConfigChartName], os.Getenv(EnvChartName), DefaultChartName),
		ChartVersion: firstNonEmpty(config.Config[ConfigChartVersion], os.Getenv(EnvChartVersion), DefaultVersion),
	})
	if err != nil {
		return fmt.Errorf("failed to resolve chart URL: %w", err)
	}

	// Get Helm client
	helmClient, err := c.getHelmClient(DefaultNamespace)
	if err != nil {
		return fmt.Errorf("failed to create Helm client: %w", err)
	}

	// Prepare default values
	values := map[string]interface{}{
		"installation": map[string]interface{}{
			"namespace": DefaultNamespace,
		},
	}

	// Load custom values from ConfigMap or Secret if specified
	extraValues, err := loadValuesFromRef(ctx, config.Client, valuesRef{
		ConfigMapName: config.Config[ConfigValuesConfigMap],
		SecretName:    config.Config[ConfigValuesSecret],
		Namespace:     firstNonEmpty(config.Config[ConfigValuesNamespace], DefaultNamespace),
	})
	if err != nil {
		return fmt.Errorf("failed to load custom values: %w", err)
	}
	if extraValues != nil {
		values = mergeValues(values, extraValues)
	}

	// Install or upgrade kruise-rollout
	_, err = helmClient.InstallOrUpgrade(DefaultReleaseName, chartURL, values)
	if err != nil {
		return fmt.Errorf("failed to install/upgrade kruise-rollout: %w", err)
	}

	// Update last config
	c.mu.Lock()
	c.updateLastConfig(config.Config)
	c.mu.Unlock()

	return nil
}

// hasConfigChanged checks if chart configuration has changed
func (c *ManagerController) hasConfigChanged(cfg map[string]string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	chartKeys := []string{
		ConfigChartURL,
		ConfigChartRepoURL,
		ConfigChartName,
		ConfigChartVersion,
		ConfigValuesConfigMap,
		ConfigValuesSecret,
		ConfigValuesNamespace,
	}

	// Initialize lastConfig if first time
	if c.lastConfig == nil {
		c.lastConfig = make(map[string]string)
		for _, key := range chartKeys {
			c.lastConfig[key] = cfg[key]
		}
		return true
	}

	// Check if any configuration has changed
	for _, key := range chartKeys {
		oldVal := c.lastConfig[key]
		newVal := cfg[key]
		if oldVal != newVal {
			return true
		}
	}

	return false
}

// updateLastConfig updates the cached last configuration
func (c *ManagerController) updateLastConfig(cfg map[string]string) {
	chartKeys := []string{
		ConfigChartURL,
		ConfigChartRepoURL,
		ConfigChartName,
		ConfigChartVersion,
		ConfigValuesConfigMap,
		ConfigValuesSecret,
		ConfigValuesNamespace,
	}

	if c.lastConfig == nil {
		c.lastConfig = make(map[string]string)
	}
	for _, key := range chartKeys {
		c.lastConfig[key] = cfg[key]
	}
}

// AgentController implements the AddonController for edge clusters
// It installs kruise-rollout on each member cluster
type AgentController struct {
	helmClient helm.HelmClient
	mu         sync.RWMutex
	lastConfig map[string]string
}

// SetHelmClient allows setting a custom HelmClient for testing
func (c *AgentController) SetHelmClient(hc helm.HelmClient) {
	c.helmClient = hc
}

// getHelmClient returns the helm client, creating one if needed
func (c *AgentController) getHelmClient(namespace string) (helm.HelmClient, error) {
	if c.helmClient != nil {
		return c.helmClient, nil
	}
	return helm.NewClientInCluster(namespace)
}

// Reconcile installs or upgrades kruise-rollout on the edge cluster
func (c *AgentController) Reconcile(ctx context.Context, config addon.AddonConfig) error {
	// Check if chart configuration has changed
	if c.hasConfigChanged(config.Config) {
		// Clear last config to trigger re-install/upgrade
		c.lastConfig = nil
	}

	// Resolve chart URL
	chartURL, err := resolveChartURL(ChartConfig{
		URL:          config.Config[ConfigChartURL],
		RepoURL:      firstNonEmpty(config.Config[ConfigChartRepoURL], os.Getenv(EnvChartRepoURL), DefaultRepoURL),
		ChartName:    firstNonEmpty(config.Config[ConfigChartName], os.Getenv(EnvChartName), DefaultChartName),
		ChartVersion: firstNonEmpty(config.Config[ConfigChartVersion], os.Getenv(EnvChartVersion), DefaultVersion),
	})
	if err != nil {
		return fmt.Errorf("failed to resolve chart URL: %w", err)
	}

	// Get Helm client
	helmClient, err := c.getHelmClient(DefaultNamespace)
	if err != nil {
		return fmt.Errorf("failed to create Helm client: %w", err)
	}

	// Prepare default values
	values := map[string]interface{}{
		// Default installation configuration
		"installation": map[string]interface{}{
			"namespace": DefaultNamespace,
		},
	}

	// Load custom values from ConfigMap or Secret if specified
	extraValues, err := loadValuesFromRef(ctx, config.Client, valuesRef{
		ConfigMapName: config.Config[ConfigValuesConfigMap],
		SecretName:    config.Config[ConfigValuesSecret],
		Namespace:     firstNonEmpty(config.Config[ConfigValuesNamespace], DefaultNamespace),
	})
	if err != nil {
		return fmt.Errorf("failed to load custom values: %w", err)
	}
	if extraValues != nil {
		values = mergeValues(values, extraValues)
	}

	// Install or upgrade kruise-rollout
	_, err = helmClient.InstallOrUpgrade(DefaultReleaseName, chartURL, values)
	if err != nil {
		return fmt.Errorf("failed to install/upgrade kruise-rollout: %w", err)
	}

	// Update last config
	c.mu.Lock()
	c.updateLastConfig(config.Config)
	c.mu.Unlock()

	return nil
}

// hasConfigChanged checks if chart configuration has changed
func (c *AgentController) hasConfigChanged(cfg map[string]string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	chartKeys := []string{
		ConfigChartURL,
		ConfigChartRepoURL,
		ConfigChartName,
		ConfigChartVersion,
		ConfigValuesConfigMap,
		ConfigValuesSecret,
		ConfigValuesNamespace,
	}

	// Initialize lastConfig if first time
	if c.lastConfig == nil {
		c.lastConfig = make(map[string]string)
		for _, key := range chartKeys {
			c.lastConfig[key] = cfg[key]
		}
		return true
	}

	// Check if any configuration has changed
	for _, key := range chartKeys {
		oldVal := c.lastConfig[key]
		newVal := cfg[key]
		if oldVal != newVal {
			return true
		}
	}

	return false
}

// updateLastConfig updates the cached last configuration
func (c *AgentController) updateLastConfig(cfg map[string]string) {
	chartKeys := []string{
		ConfigChartURL,
		ConfigChartRepoURL,
		ConfigChartName,
		ConfigChartVersion,
		ConfigValuesConfigMap,
		ConfigValuesSecret,
		ConfigValuesNamespace,
	}

	if c.lastConfig == nil {
		c.lastConfig = make(map[string]string)
	}
	for _, key := range chartKeys {
		c.lastConfig[key] = cfg[key]
	}
}

// resolveChartURL resolves the Helm chart URL
func resolveChartURL(cfg ChartConfig) (string, error) {
	// If full URL is provided, use it directly
	if cfg.URL != "" {
		if !strings.HasPrefix(cfg.URL, "http://") && !strings.HasPrefix(cfg.URL, "https://") {
			return "", fmt.Errorf("chartURL must be a valid HTTP/HTTPS URL")
		}
		return cfg.URL, nil
	}

	// Construct URL from repo URL, chart name, and version
	if !strings.HasPrefix(cfg.RepoURL, "http://") && !strings.HasPrefix(cfg.RepoURL, "https://") {
		return "", fmt.Errorf("chart repo URL must be a valid HTTP/HTTPS URL")
	}
	if cfg.ChartName == "" {
		return "", fmt.Errorf("chart name must be set")
	}
	if cfg.ChartVersion == "" {
		cfg.ChartVersion = DefaultVersion
	}

	// Construct chart URL: https://repo-url/chart-name-version.tgz
	return fmt.Sprintf("%s/%s-%s.tgz", strings.TrimRight(cfg.RepoURL, "/"), cfg.ChartName, cfg.ChartVersion), nil
}

type valuesRef struct {
	ConfigMapName string
	SecretName    string
	Namespace     string
}

// loadValuesFromRef loads custom values from a ConfigMap or Secret
func loadValuesFromRef(ctx context.Context, c client.Client, ref valuesRef) (map[string]interface{}, error) {
	if ref.ConfigMapName != "" && ref.SecretName != "" {
		return nil, fmt.Errorf("only one of values ConfigMap or Secret can be specified")
	}
	if ref.ConfigMapName == "" && ref.SecretName == "" {
		return nil, nil
	}
	if ref.Namespace == "" {
		return nil, fmt.Errorf("values namespace must be set")
	}

	var raw []byte

	// Load from ConfigMap
	if ref.ConfigMapName != "" {
		cm := &corev1.ConfigMap{}
		if err := c.Get(ctx, client.ObjectKey{Name: ref.ConfigMapName, Namespace: ref.Namespace}, cm); err != nil {
			return nil, err
		}
		if v, ok := cm.Data["values.yaml"]; ok {
			raw = []byte(v)
		} else if v, ok := cm.Data["values"]; ok {
			raw = []byte(v)
		} else {
			return nil, fmt.Errorf("values ConfigMap must contain values.yaml or values")
		}
	}

	// Load from Secret
	if ref.SecretName != "" {
		secret := &corev1.Secret{}
		if err := c.Get(ctx, client.ObjectKey{Name: ref.SecretName, Namespace: ref.Namespace}, secret); err != nil {
			return nil, err
		}
		if v, ok := secret.Data["values.yaml"]; ok {
			raw = v
		} else if v, ok := secret.Data["values"]; ok {
			raw = v
		} else {
			return nil, fmt.Errorf("values Secret must contain values.yaml or values")
		}
	}

	var values map[string]interface{}
	if err := yaml.Unmarshal(raw, &values); err != nil {
		return nil, err
	}
	if values == nil {
		values = map[string]interface{}{}
	}
	return values, nil
}

// mergeValues recursively merges source values into destination
func mergeValues(dst, src map[string]interface{}) map[string]interface{} {
	if dst == nil {
		dst = map[string]interface{}{}
	}
	for k, v := range src {
		if srcMap, ok := v.(map[string]interface{}); ok {
			if dstMap, ok := dst[k].(map[string]interface{}); ok {
				dst[k] = mergeValues(dstMap, srcMap)
				continue
			}
		}
		dst[k] = v
	}
	return dst
}

// firstNonEmpty returns the first non-empty string from the provided values
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
