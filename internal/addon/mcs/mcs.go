package mcs

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"sync"

	"github.com/hex-techs/rocket/internal/addon"
	storagev1alpha1 "github.com/hex-techs/rocket/pkg/apis/storage/v1alpha1"
	"github.com/hex-techs/rocket/pkg/helm"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	AddonName        = "mcs-lighthouse"
	BrokerNamespace  = "submariner-k8s-broker"
	BrokerSecretName = "submariner-broker-client-secret" // Secret created by broker chart

	// Env vars for chart paths
	EnvBrokerChart     = "CHART_SUBMARINER_BROKER"
	EnvSubmarinerChart = "CHART_SUBMARINER"
)

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
	// 1. Ensure Broker is installed (Running once globally effectively)
	var err error
	c.once.Do(func() {
		err = c.ensureBroker(ctx)
	})
	if err != nil {
		return fmt.Errorf("failed to ensure broker: %v", err)
	}

	// 2. Retrieve Broker Info
	brokerInfo, err := c.getBrokerInfo(ctx)
	if err != nil {
		return fmt.Errorf("failed to get broker info: %v", err)
	}

	// 3. Check if we need to update the Cluster Addon Config
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

func (c *ManagerController) ensureBroker(ctx context.Context) error {
	chartPath := os.Getenv(EnvBrokerChart)
	if chartPath == "" {
		chartPath = "/charts/submariner-k8s-broker" // Default
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

	_, err = helmClient.InstallOrUpgrade("submariner-broker", chartPath, values)
	return err
}

func (c *ManagerController) getBrokerInfo(ctx context.Context) (map[string]string, error) {
	// Read the Secret created by the Broker
	secret := &corev1.Secret{}
	err := c.mgrClient.Get(ctx, types.NamespacedName{Name: BrokerSecretName, Namespace: BrokerNamespace}, secret)
	if err != nil {
		if errors.IsNotFound(err) {
			// Try to find by SA
			sa := &corev1.ServiceAccount{}
			if err := c.mgrClient.Get(ctx, types.NamespacedName{Name: "submariner-k8s-broker-client", Namespace: BrokerNamespace}, sa); err != nil {
				return nil, fmt.Errorf("failed to find broker SA: %v", err)
			}
			if len(sa.Secrets) == 0 {
				return nil, fmt.Errorf("broker SA has no secrets")
			}
			if err := c.mgrClient.Get(ctx, types.NamespacedName{Name: sa.Secrets[0].Name, Namespace: BrokerNamespace}, secret); err != nil {
				return nil, err
			}
		} else {
			return nil, err
		}
	}

	token := string(secret.Data["token"])
	ca := base64.StdEncoding.EncodeToString(secret.Data["ca.crt"])

	brokerURL := "https://kubernetes.default.svc:443"

	return map[string]string{
		"brokerURL":   brokerURL,
		"brokerToken": token,
		"brokerCA":    ca,
	}, nil
}

// AgentController implements the AddonController for the Edge side
type AgentController struct {
	helmClient helm.HelmClient // Supports dependency injection for testing
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

	// Install Submariner (Lighthouse) via Helm
	chartPath := os.Getenv(EnvSubmarinerChart)
	if chartPath == "" {
		chartPath = "/charts/submariner"
	}

	// Decode CA
	caDecoded, _ := base64.StdEncoding.DecodeString(brokerCA)

	helmClient, err := c.getHelmClient("submariner-operator")
	if err != nil {
		return err
	}

	values := map[string]interface{}{
		"broker": map[string]interface{}{
			"server":    brokerURL,
			"token":     brokerToken,
			"namespace": brokerNS,
			"ca":        string(caDecoded),
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
	_, err = helmClient.InstallOrUpgrade("submariner", chartPath, values)
	if err != nil {
		return fmt.Errorf("failed to install submariner agent: %v", err)
	}

	return nil
}
