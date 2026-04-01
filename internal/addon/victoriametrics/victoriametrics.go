package victoriametrics

import (
	"context"
	"fmt"
	"os"
	"sync"

	"github.com/hex-techs/rocket/internal/addon"
	storagev1alpha1 "github.com/hex-techs/rocket/pkg/apis/storage/v1alpha1"
	"github.com/hex-techs/rocket/pkg/helm"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"
)

const (
	AddonName = "victoriametrics"

	// Namespace for VictoriaMetrics components
	VictoriaMetricsNamespace = "victoriametrics"
	VmAgentNamespace         = "vm-agent"

	// Default chart configuration
	defaultVMChartRepoURL = "https://victoriametrics.github.io/helm-charts/"
	defaultVMChartName    = "victoria-metrics-single"
	defaultVMChartVersion = "0.33.0"

	defaultVmAgentChartRepoURL = "https://victoriametrics.github.io/helm-charts/"
	defaultVmAgentChartName    = "victoria-metrics-agent"
	defaultVmAgentChartVersion = "0.34.0"

	// Service names
	VictoriaMetricsServiceName = "victoria-metrics-victoria-metrics-single"

	// Environment variables for custom charts
	EnvVMChartURL     = "CHART_VICTORIAMETRICS_URL"
	EnvVmAgentChartURL = "CHART_VMAGENT_URL"

	// Config keys for VictoriaMetrics chart
	ConfigVMChartURL        = "vmChartURL"
	ConfigVMChartRepoURL    = "vmChartRepoURL"
	ConfigVMChartName       = "vmChartName"
	ConfigVMChartVersion    = "vmChartVersion"
	ConfigVMValuesConfigMap = "vmValuesConfigMap"
	ConfigVMValuesSecret    = "vmValuesSecret"
	ConfigVMValuesNamespace = "vmValuesNamespace"

	// Config keys for vmagent chart
	ConfigVmAgentChartURL        = "vmAgentChartURL"
	ConfigVmAgentChartRepoURL    = "vmAgentChartRepoURL"
	ConfigVmAgentChartName       = "vmAgentChartName"
	ConfigVmAgentChartVersion    = "vmAgentChartVersion"
	ConfigVmAgentValuesConfigMap = "vmAgentValuesConfigMap"
	ConfigVmAgentValuesSecret    = "vmAgentValuesSecret"
	ConfigVmAgentValuesNamespace = "vmAgentValuesNamespace"

	// Config keys for storage
	ConfigVMStorageClass       = "vmStorageClass"
	ConfigVMStorageSize        = "vmStorageSize"
	ConfigVmAgentStorageClass  = "vmAgentStorageClass"
	ConfigVmAgentStorageSize   = "vmAgentStorageSize"

	// Config keys for connection
	ConfigVictoriaMetricsURL = "victoriametricsURL"
)

var vmChartKeys = []string{
	ConfigVMChartURL,
	ConfigVMChartRepoURL,
	ConfigVMChartName,
	ConfigVMChartVersion,
	ConfigVMValuesConfigMap,
	ConfigVMValuesSecret,
	ConfigVMValuesNamespace,
}

var vmAgentChartKeys = []string{
	ConfigVmAgentChartURL,
	ConfigVmAgentChartRepoURL,
	ConfigVmAgentChartName,
	ConfigVmAgentChartVersion,
	ConfigVmAgentValuesConfigMap,
	ConfigVmAgentValuesSecret,
	ConfigVmAgentValuesNamespace,
}

func init() {
	addon.Register(&VictoriaMetricsAddon{})
}

type VictoriaMetricsAddon struct{}

func (a *VictoriaMetricsAddon) Name() string {
	return AddonName
}

func (a *VictoriaMetricsAddon) ManagerController(mgr ctrl.Manager) (addon.AddonController, error) {
	return &ManagerController{
		mgrClient: mgr.GetClient(),
	}, nil
}

func (a *VictoriaMetricsAddon) AgentController(mgr ctrl.Manager) (addon.AddonController, error) {
	return &AgentController{}, nil
}

func (a *VictoriaMetricsAddon) Manifests() []runtime.Object {
	return []runtime.Object{}
}

// ManagerController implements the AddonController for the Hub side
type ManagerController struct {
	mgrClient  client.Client
	once       sync.Once
	helmClient helm.HelmClient
	mu         sync.RWMutex
	// Track last applied config to detect changes
	lastVMConfig map[string]string
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
	return helm.NewClient(namespace)
}

func (c *ManagerController) Reconcile(ctx context.Context, config addon.AddonConfig) error {
	// 1. Check if VictoriaMetrics chart config changed (requires upgrade)
	c.mu.Lock()
	shouldUpdate := c.shouldUpdateVM(config.Config)
	c.mu.Unlock()

	if shouldUpdate {
		if err := c.ensureVictoriaMetrics(ctx, config); err != nil {
			return fmt.Errorf("failed to ensure VictoriaMetrics: %v", err)
		}

		c.mu.Lock()
		c.updateLastVMConfig(config.Config)
		c.mu.Unlock()
	}

	// 2. Get VictoriaMetrics service URL
	vmURL, err := c.getVictoriaMetricsURL(ctx, config)
	if err != nil {
		return fmt.Errorf("failed to get VictoriaMetrics URL: %v", err)
	}

	// 3. Check if we need to update the Cluster Addon Config
	needUpdate := false
	newConfig := make(map[string]string)
	for k, v := range config.Config {
		newConfig[k] = v
	}

	if newConfig[ConfigVictoriaMetricsURL] != vmURL {
		newConfig[ConfigVictoriaMetricsURL] = vmURL
		needUpdate = true
	}

	if needUpdate {
		// Update the ManagedCluster resource with the VictoriaMetrics URL
		cluster := &storagev1alpha1.ManagedCluster{}
		if err := config.Client.Get(ctx, types.NamespacedName{Name: config.ClusterName}, cluster); err != nil {
			return err
		}

		for i, addons := range cluster.Spec.Addons {
			if addons.Name == AddonName {
				cluster.Spec.Addons[i].Config = newConfig
				break
			}
		}

		if err := config.Client.Update(ctx, cluster); err != nil {
			return err
		}
	}

	return nil
}

func (c *ManagerController) shouldUpdateVM(cfg map[string]string) bool {
	if c.lastVMConfig == nil {
		return true
	}

	for _, key := range vmChartKeys {
		oldVal := c.lastVMConfig[key]
		newVal := cfg[key]
		if oldVal != newVal {
			return true
		}
	}

	return false
}

func (c *ManagerController) updateLastVMConfig(cfg map[string]string) {
	if c.lastVMConfig == nil {
		c.lastVMConfig = make(map[string]string)
	}
	for _, key := range vmChartKeys {
		c.lastVMConfig[key] = cfg[key]
	}
}

func (c *ManagerController) ensureVictoriaMetrics(ctx context.Context, config addon.AddonConfig) error {
	// Create namespace if not exists
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: VictoriaMetricsNamespace,
		},
	}
	if err := c.mgrClient.Create(ctx, ns); err != nil {
		// Ignore if already exists
	}

	chartURL, err := resolveChartURL(chartURLConfig{
		URL:          config.Config[ConfigVMChartURL],
		RepoURL:      firstNonEmpty(config.Config[ConfigVMChartRepoURL], os.Getenv(EnvVMChartURL), defaultVMChartRepoURL),
		ChartName:    firstNonEmpty(config.Config[ConfigVMChartName], defaultVMChartName),
		ChartVersion: firstNonEmpty(config.Config[ConfigVMChartVersion], defaultVMChartVersion),
	})
	if err != nil {
		return err
	}

	helmClient, err := c.getHelmClient(VictoriaMetricsNamespace)
	if err != nil {
		return err
	}

	// Basic values for VictoriaMetrics single server
	values := map[string]interface{}{
		"server": map[string]interface{}{
			"service": map[string]interface{}{
				"type": "ClusterIP",
			},
		},
	}

	// Configure persistent storage if storageClass is provided
	if storageClass := config.Config[ConfigVMStorageClass]; storageClass != "" {
		values["server"].(map[string]interface{})["persistentVolume"] = map[string]interface{}{
			"enabled":         true,
			"storageClassName": storageClass,
			"size":            firstNonEmpty(config.Config[ConfigVMStorageSize], "16Gi"),
		}
	} else {
		// Default: no persistent storage
		values["server"].(map[string]interface{})["persistentVolume"] = map[string]interface{}{
			"enabled": false,
		}
	}

	// Load extra values if provided
	extraValues, err := loadValuesFromRef(ctx, config.Client, valuesRef{
		ConfigMapName: config.Config[ConfigVMValuesConfigMap],
		SecretName:    config.Config[ConfigVMValuesSecret],
		Namespace:     firstNonEmpty(config.Config[ConfigVMValuesNamespace], VictoriaMetricsNamespace),
	})
	if err != nil {
		return err
	}
	if extraValues != nil {
		values = mergeValues(values, extraValues)
	}

	_, err = helmClient.InstallOrUpgrade("victoria-metrics", chartURL, values)
	if err != nil {
		return fmt.Errorf("failed to install VictoriaMetrics: %v", err)
	}

	return nil
}

func (c *ManagerController) getVictoriaMetricsURL(ctx context.Context, config addon.AddonConfig) (string, error) {
	// Construct the service URL
	// Format: http://<service-name>.<namespace>.svc.cluster.local:<port>
	serviceName := VictoriaMetricsServiceName
	namespace := VictoriaMetricsNamespace
	port := 8428 // Default VictoriaMetrics port

	// Allow override from config
	if url := config.Config[ConfigVictoriaMetricsURL]; url != "" {
		return url, nil
	}

	// Verify service exists
	svc := &corev1.Service{}
	err := c.mgrClient.Get(ctx, types.NamespacedName{
		Name:      serviceName,
		Namespace: namespace,
	}, svc)
	if err != nil {
		return "", fmt.Errorf("VictoriaMetrics service not found: %v", err)
	}

	// Construct internal DNS name
	vmURL := fmt.Sprintf("http://%s.%s.svc.cluster.local:%d", serviceName, namespace, port)
	return vmURL, nil
}

// AgentController implements the AddonController for the Edge side
type AgentController struct {
	helmClient         helm.HelmClient
	mu                 sync.RWMutex
	lastVmAgentConfig  map[string]string
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

func (c *AgentController) Reconcile(ctx context.Context, config addon.AddonConfig) error {
	vmURL := config.Config[ConfigVictoriaMetricsURL]

	if vmURL == "" {
		// Not ready yet
		return nil
	}

	// Check if vmagent chart config has changed
	if c.hasVmAgentConfigChanged(config.Config) {
		c.lastVmAgentConfig = nil
	}

	// Note: Helm will create the namespace automatically via CreateNamespace option

	chartURL, err := resolveChartURL(chartURLConfig{
		URL:          config.Config[ConfigVmAgentChartURL],
		RepoURL:      firstNonEmpty(config.Config[ConfigVmAgentChartRepoURL], os.Getenv(EnvVmAgentChartURL), defaultVmAgentChartRepoURL),
		ChartName:    firstNonEmpty(config.Config[ConfigVmAgentChartName], defaultVmAgentChartName),
		ChartVersion: firstNonEmpty(config.Config[ConfigVmAgentChartVersion], defaultVmAgentChartVersion),
	})
	if err != nil {
		return err
	}

	helmClient, err := c.getHelmClient(VmAgentNamespace)
	if err != nil {
		return err
	}

	// Configure vmagent to scrape local cluster and send to VictoriaMetrics
	values := map[string]interface{}{
		"remoteWrite": map[string]interface{}{
			"url": vmURL + "/api/v1/write",
		},
		"service": map[string]interface{}{
			"type": "ClusterIP",
		},
	}

	// Configure persistent storage if storageClass is provided
	if storageClass := config.Config[ConfigVmAgentStorageClass]; storageClass != "" {
		values["persistentVolume"] = map[string]interface{}{
			"enabled":          true,
			"storageClassName": storageClass,
			"size":             firstNonEmpty(config.Config[ConfigVmAgentStorageSize], "10Gi"),
		}
	} else {
		// Default: no persistent storage (use emptyDir)
		values["persistentVolume"] = map[string]interface{}{
			"enabled": false,
		}
	}

	// Load extra values if provided
	extraValues, err := loadValuesFromRef(ctx, config.Client, valuesRef{
		ConfigMapName: config.Config[ConfigVmAgentValuesConfigMap],
		SecretName:    config.Config[ConfigVmAgentValuesSecret],
		Namespace:     firstNonEmpty(config.Config[ConfigVmAgentValuesNamespace], VmAgentNamespace),
	})
	if err != nil {
		return err
	}
	if extraValues != nil {
		values = mergeValues(values, extraValues)
	}

	_, err = helmClient.InstallOrUpgrade("vm-agent", chartURL, values)
	if err != nil {
		return fmt.Errorf("failed to install vmagent: %v", err)
	}

	return nil
}

// hasVmAgentConfigChanged checks if vmagent-related chart configuration has changed
func (c *AgentController) hasVmAgentConfigChanged(cfg map[string]string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Initialize lastConfig if first time
	if c.lastVmAgentConfig == nil {
		c.lastVmAgentConfig = make(map[string]string)
		for _, key := range vmAgentChartKeys {
			c.lastVmAgentConfig[key] = cfg[key]
		}
		c.lastVmAgentConfig[ConfigVictoriaMetricsURL] = cfg[ConfigVictoriaMetricsURL]
		return true
	}

	// Check if any configuration has changed
	allKeys := append(vmAgentChartKeys, ConfigVictoriaMetricsURL)
	for _, key := range allKeys {
		oldVal := c.lastVmAgentConfig[key]
		newVal := cfg[key]
		if oldVal != newVal {
			c.lastVmAgentConfig[key] = newVal
			return true
		}
	}

	return false
}

// Helper types and functions

type chartURLConfig struct {
	URL          string
	RepoURL      string
	ChartName    string
	ChartVersion string
}

type valuesRef struct {
	ConfigMapName string
	SecretName    string
	Namespace     string
}

func resolveChartURL(cfg chartURLConfig) (string, error) {
	// If full URL is provided, use it
	if cfg.URL != "" {
		return cfg.URL, nil
	}

	// Otherwise construct from repo URL, chart name, and version
	if cfg.RepoURL == "" {
		return "", fmt.Errorf("chart repo URL must be set")
	}
	if cfg.ChartName == "" {
		return "", fmt.Errorf("chart name must be set")
	}
	if cfg.ChartVersion == "" {
		cfg.ChartVersion = "*"
	}

	// Construct chart URL: <repo-url>/<chart-name>-<version>.tgz
	repoURL := cfg.RepoURL
	if repoURL[len(repoURL)-1] != '/' {
		repoURL += "/"
	}
	return fmt.Sprintf("%s%s-%s.tgz", repoURL, cfg.ChartName, cfg.ChartVersion), nil
}

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
	if ref.ConfigMapName != "" {
		cm := &corev1.ConfigMap{}
		if err := c.Get(ctx, types.NamespacedName{Name: ref.ConfigMapName, Namespace: ref.Namespace}, cm); err != nil {
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

	if ref.SecretName != "" {
		secret := &corev1.Secret{}
		if err := c.Get(ctx, types.NamespacedName{Name: ref.SecretName, Namespace: ref.Namespace}, secret); err != nil {
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

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
