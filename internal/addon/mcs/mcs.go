package mcs

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/hex-techs/rocket/internal/addon"
	storagev1alpha1 "github.com/hex-techs/rocket/pkg/apis/storage/v1alpha1"
	"github.com/hex-techs/rocket/pkg/constants"
	"github.com/hex-techs/rocket/pkg/helm"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"
)

const (
	AddonName        = "mcs-lighthouse"
	BrokerNamespace  = "submariner-k8s-broker"
	BrokerSecretName = "submariner-k8s-broker-client-token" // Secret created by broker chart

	// Env vars for chart URLs and metadata (version is optional, defaults to latest)
	EnvBrokerChart     = "CHART_SUBMARINER_BROKER"
	EnvSubmarinerChart = "CHART_SUBMARINER"

	EnvBrokerChartName    = "CHART_SUBMARINER_BROKER_NAME"
	EnvBrokerChartVersion = "CHART_SUBMARINER_BROKER_VERSION" // Optional: if not set, uses latest (*)
	EnvSubmarinerName     = "CHART_SUBMARINER_NAME"
	EnvSubmarinerVersion  = "CHART_SUBMARINER_VERSION" // Optional: if not set, uses latest (*)

	defaultRepoURL                = "https://submariner-io.github.io/submariner-charts/charts"
	defaultBrokerChartName        = "submariner-k8s-broker"
	defaultBrokerChartVersion     = "0.23.0-m0"
	defaultSubmarinerChartName    = "submariner-operator"
	defaultSubmarinerChartVersion = "0.23.0-m0"

	ConfigBrokerChartURL        = "brokerChartURL"
	ConfigBrokerChartRepoURL    = "brokerChartRepoURL"
	ConfigBrokerChartName       = "brokerChartName"
	ConfigBrokerChartVersion    = "brokerChartVersion"
	ConfigBrokerValuesConfigMap = "brokerValuesConfigMap"
	ConfigBrokerValuesSecret    = "brokerValuesSecret"
	ConfigBrokerValuesNamespace = "brokerValuesNamespace"

	ConfigSubmarinerChartURL        = "submarinerChartURL"
	ConfigSubmarinerChartRepoURL    = "submarinerChartRepoURL"
	ConfigSubmarinerChartName       = "submarinerChartName"
	ConfigSubmarinerChartVersion    = "submarinerChartVersion"
	ConfigSubmarinerValuesConfigMap = "submarinerValuesConfigMap"
	ConfigSubmarinerValuesSecret    = "submarinerValuesSecret"
	ConfigSubmarinerValuesNamespace = "submarinerValuesNamespace"
)

var brokerChartKeys = []string{
	ConfigBrokerChartURL,
	ConfigBrokerChartRepoURL,
	ConfigBrokerChartName,
	ConfigBrokerChartVersion,
	ConfigBrokerValuesConfigMap,
	ConfigBrokerValuesSecret,
	ConfigBrokerValuesNamespace,
}

func init() {
	addon.Register(&MCSAddon{})
}

type MCSAddon struct{}

func (a *MCSAddon) Name() string {
	return AddonName
}

func (a *MCSAddon) ManagerController(mgr ctrl.Manager) (addon.AddonController, error) {
	return &ManagerController{
		mgrClient: mgr.GetClient(),
	}, nil
}

func (a *MCSAddon) AgentController(mgr ctrl.Manager) (addon.AddonController, error) {
	return &AgentController{}, nil
}

func (a *MCSAddon) Manifests() []runtime.Object {
	return []runtime.Object{}
}

// ManagerController implements the AddonController for the Hub side
type ManagerController struct {
	mgrClient  client.Client
	once       sync.Once
	helmClient helm.HelmClient // Supports dependency injection for testing
	mu         sync.RWMutex
	// Track last applied config to detect changes
	lastBrokerConfig map[string]string
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
	// 1. Check if Broker chart config changed (requires upgrade)
	c.mu.Lock()
	shouldUpdate := c.shouldUpdateBroker(config.Config)
	c.mu.Unlock()

	if shouldUpdate {
		if err := c.ensureBroker(ctx, config); err != nil {
			return fmt.Errorf("failed to ensure broker: %v", err)
		}

		c.mu.Lock()
		c.updateLastBrokerConfig(config.Config)
		c.mu.Unlock()
	}

	// 3. Retrieve Broker Info
	brokerInfo, err := c.getBrokerInfo(ctx, config.Config)
	if err != nil {
		return fmt.Errorf("failed to get broker info: %v", err)
	}

	// 4. Check if we need to update the Cluster Addon Config
	// We verify if the current config in the ManagedCluster matches the broker info.
	needUpdate := false
	newConfig := make(map[string]string)
	for k, v := range config.Config {
		newConfig[k] = v
	}

	if newConfig["brokerURL"] != brokerInfo["brokerURL"] {
		newConfig["brokerURL"] = brokerInfo["brokerURL"]
		needUpdate = true
	}
	if newConfig["brokerToken"] != brokerInfo["brokerToken"] {
		newConfig["brokerToken"] = brokerInfo["brokerToken"]
		needUpdate = true
	}
	if newConfig["brokerCA"] != brokerInfo["brokerCA"] {
		newConfig["brokerCA"] = brokerInfo["brokerCA"]
		needUpdate = true
	}
	if newConfig["brokerNamespace"] != BrokerNamespace {
		newConfig["brokerNamespace"] = BrokerNamespace
		needUpdate = true
	}

	if needUpdate {
		// We need to update the ManagedCluster resource
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

func (c *ManagerController) shouldUpdateBroker(cfg map[string]string) bool {
	if c.lastBrokerConfig == nil {
		return true
	}

	for _, key := range brokerChartKeys {
		oldVal := c.lastBrokerConfig[key]
		newVal := cfg[key]
		if oldVal != newVal {
			return true
		}
	}

	return false
}

func (c *ManagerController) updateLastBrokerConfig(cfg map[string]string) {
	if c.lastBrokerConfig == nil {
		c.lastBrokerConfig = make(map[string]string)
	}
	for _, key := range brokerChartKeys {
		c.lastBrokerConfig[key] = cfg[key]
	}
}

func (c *ManagerController) ensureBroker(ctx context.Context, config addon.AddonConfig) error {
	chartURL, err := resolveChartURL(chartURLConfig{
		URL:          config.Config[ConfigBrokerChartURL],
		RepoURL:      firstNonEmpty(config.Config[ConfigBrokerChartRepoURL], os.Getenv(EnvBrokerChart), defaultRepoURL),
		ChartName:    firstNonEmpty(config.Config[ConfigBrokerChartName], os.Getenv(EnvBrokerChartName), defaultBrokerChartName),
		ChartVersion: firstNonEmpty(config.Config[ConfigBrokerChartVersion], os.Getenv(EnvBrokerChartVersion), defaultBrokerChartVersion),
	})
	if err != nil {
		return err
	}

	helmClient, err := c.getHelmClient(BrokerNamespace)
	if err != nil {
		return err
	}

	// Basic values
	values := map[string]interface{}{
		"submariner": map[string]interface{}{
			"serviceDiscovery": true,
		},
	}

	extraValues, err := loadValuesFromRef(ctx, config.Client, valuesRef{
		ConfigMapName: config.Config[ConfigBrokerValuesConfigMap],
		SecretName:    config.Config[ConfigBrokerValuesSecret],
		Namespace:     firstNonEmpty(config.Config[ConfigBrokerValuesNamespace], BrokerNamespace),
	})
	if err != nil {
		return err
	}
	if extraValues != nil {
		values = mergeValues(values, extraValues)
	}

	_, err = helmClient.InstallOrUpgrade("submariner-k8s-broker", chartURL, values)
	if err != nil {
		return err
	}

	// Workaround: Ensure ServiceAccount and Secret exist (for K8s 1.24+ and/or broken chart)
	saName := "submariner-k8s-broker-client"
	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      saName,
			Namespace: BrokerNamespace,
		},
	}
	if err := c.mgrClient.Create(ctx, sa); err != nil {
		if !k8serrors.IsAlreadyExists(err) {
			return fmt.Errorf("failed to create ServiceAccount: %v", err)
		}
	}

	secretName := BrokerSecretName
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: BrokerNamespace,
			Annotations: map[string]string{
				"kubernetes.io/service-account.name": saName,
			},
		},
		Type: corev1.SecretTypeServiceAccountToken,
	}
	if err := c.mgrClient.Create(ctx, secret); err != nil {
		if !k8serrors.IsAlreadyExists(err) {
			return fmt.Errorf("failed to create Secret: %v", err)
		}
	}

	return nil
}

func (c *ManagerController) getBrokerInfo(ctx context.Context, config map[string]string) (map[string]string, error) {
	// Read the Secret created by the Broker
	secret := &corev1.Secret{}
	err := c.mgrClient.Get(ctx, types.NamespacedName{Name: BrokerSecretName, Namespace: BrokerNamespace}, secret)
	if err != nil {
		return nil, fmt.Errorf("failed to get broker secret %s/%s: %w", BrokerNamespace, BrokerSecretName, err)
	}

	token := string(secret.Data["token"])
	ca := base64.StdEncoding.EncodeToString(secret.Data["ca.crt"])

	brokerURL := constants.DefaultAPIServerURL
	if u, ok := config["brokerURL"]; ok && u != "" {
		brokerURL = u
	}

	return map[string]string{
		"brokerURL":   brokerURL,
		"brokerToken": token,
		"brokerCA":    ca,
	}, nil
}

// AgentController implements the AddonController for the Edge side
type AgentController struct {
	helmClient           helm.HelmClient // Supports dependency injection for testing
	mu                   sync.RWMutex
	lastSubmarinerConfig map[string]string
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
	brokerURL := config.Config["brokerURL"]
	brokerToken := config.Config["brokerToken"]
	brokerCA := config.Config["brokerCA"]
	brokerNS := config.Config["brokerNamespace"]

	if brokerURL == "" || brokerToken == "" {
		// Not ready yet
		return nil
	}

	// Check if submariner chart config has changed (requires upgrade)
	if c.hasSubmarinerConfigChanged(config.Config) {
		// Clear last config to trigger re-install
		c.lastSubmarinerConfig = nil
	}

	// Install Submariner (Lighthouse) via Helm
	chartURL, err := resolveChartURL(chartURLConfig{
		URL:          config.Config[ConfigSubmarinerChartURL],
		RepoURL:      firstNonEmpty(config.Config[ConfigSubmarinerChartRepoURL], os.Getenv(EnvSubmarinerChart), defaultRepoURL),
		ChartName:    firstNonEmpty(config.Config[ConfigSubmarinerChartName], os.Getenv(EnvSubmarinerName), defaultSubmarinerChartName),
		ChartVersion: firstNonEmpty(config.Config[ConfigSubmarinerChartVersion], os.Getenv(EnvSubmarinerVersion), defaultSubmarinerChartVersion),
	})
	if err != nil {
		return err
	}

	helmClient, err := c.getHelmClient("submariner-operator")
	if err != nil {
		return err
	}

	// Note: submariner-operator chart expects CA in base64-encoded format
	values := map[string]interface{}{
		"broker": map[string]interface{}{
			"server":    brokerURL,
			"token":     brokerToken,
			"namespace": brokerNS,
			"ca":        brokerCA,
		},
		"submariner": map[string]interface{}{
			"clusterId":        config.ClusterName,
			"natEnabled":       false, // Assume flat network for now or auto-detect
			"serviceDiscovery": true,
		},
		"serviceAccounts": map[string]interface{}{
			"globalnet": map[string]interface{}{
				"create": false,
			},
		},
	}

	// Check if already installed
	// We assume InstallOrUpgrade handles idempotency
	extraValues, err := loadValuesFromRef(ctx, config.Client, valuesRef{
		ConfigMapName: config.Config[ConfigSubmarinerValuesConfigMap],
		SecretName:    config.Config[ConfigSubmarinerValuesSecret],
		Namespace:     firstNonEmpty(config.Config[ConfigSubmarinerValuesNamespace], "submariner-operator"),
	})
	if err != nil {
		return err
	}
	if extraValues != nil {
		values = mergeValues(values, extraValues)
	}

	_, err = helmClient.InstallOrUpgrade("submariner", chartURL, values)
	if err != nil {
		return fmt.Errorf("failed to install submariner agent: %v", err)
	}

	return nil
}

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
	if cfg.URL != "" {
		if !strings.HasPrefix(cfg.URL, "http://") && !strings.HasPrefix(cfg.URL, "https://") {
			return "", fmt.Errorf("chartURL must be a URL")
		}
		return cfg.URL, nil
	}
	if !strings.HasPrefix(cfg.RepoURL, "http://") && !strings.HasPrefix(cfg.RepoURL, "https://") {
		return "", fmt.Errorf("chart repo URL must be a URL")
	}
	if cfg.ChartName == "" {
		return "", fmt.Errorf("chart name must be set")
	}
	// If version is not specified, use latest (represented by *)
	// Helm will resolve * to the latest available version in the repo
	if cfg.ChartVersion == "" {
		cfg.ChartVersion = "*"
	}
	return fmt.Sprintf("%s/%s-%s.tgz", strings.TrimRight(cfg.RepoURL, "/"), cfg.ChartName, cfg.ChartVersion), nil
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

// hasSubmarinerConfigChanged checks if submariner-related chart configuration has changed
// Returns true if any of the chart configuration keys have been modified
func (c *AgentController) hasSubmarinerConfigChanged(cfg map[string]string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	submarinerChartKeys := []string{
		ConfigSubmarinerChartURL,
		ConfigSubmarinerChartRepoURL,
		ConfigSubmarinerChartName,
		ConfigSubmarinerChartVersion,
		ConfigSubmarinerValuesConfigMap,
		ConfigSubmarinerValuesSecret,
		ConfigSubmarinerValuesNamespace,
		// Also monitor broker config changes which affect agent installation
		"brokerURL",
		"brokerToken",
		"brokerCA",
		"brokerNamespace",
	}

	// Initialize lastConfig if first time
	if c.lastSubmarinerConfig == nil {
		c.lastSubmarinerConfig = make(map[string]string)
		for _, key := range submarinerChartKeys {
			c.lastSubmarinerConfig[key] = cfg[key]
		}
		return true
	}

	// Check if any configuration has changed
	for _, key := range submarinerChartKeys {
		oldVal := c.lastSubmarinerConfig[key]
		newVal := cfg[key]
		if oldVal != newVal {
			// Update lastConfig for next reconciliation
			c.lastSubmarinerConfig[key] = newVal
			return true
		}
	}

	return false
}
