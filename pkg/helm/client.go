package helm

import (
	"os"
	"time"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/release"
)

// HelmClient defines the interface for Helm operations, enabling mock in tests
type HelmClient interface {
	// InstallOrUpgrade installs a new release or upgrades an existing one
	InstallOrUpgrade(releaseName string, chartPath string, values map[string]interface{}) (*release.Release, error)
	// Uninstall removes a release
	Uninstall(releaseName string) error
}

// Client implements HelmClient interface
type Client struct {
	settings *cli.EnvSettings
	cfg      *action.Configuration
	ns       string
}

// Ensure Client implements HelmClient
var _ HelmClient = (*Client)(nil)

func NewClient(namespace string) (*Client, error) {
	settings := cli.New()
	cfg := new(action.Configuration)

	// We use the in-cluster config or default kubeconfig
	if err := cfg.Init(settings.RESTClientGetter(), namespace, os.Getenv("HELM_DRIVER"), func(format string, v ...interface{}) {
		// Log callback
	}); err != nil {
		return nil, err
	}

	return &Client{
		settings: settings,
		cfg:      cfg,
		ns:       namespace,
	}, nil
}

// NewClientInCluster creates a helm client from a rest.Config
func NewClientInCluster(namespace string) (*Client, error) {
	return NewClient(namespace)
}

func (c *Client) InstallOrUpgrade(releaseName string, chartPath string, values map[string]interface{}) (*release.Release, error) {
	resolvedChartPath, err := c.resolveChartPath(chartPath)
	if err != nil {
		return nil, err
	}

	histClient := action.NewHistory(c.cfg)
	histClient.Max = 1
	if _, err := histClient.Run(releaseName); err == nil {
		// Upgrade
		client := action.NewUpgrade(c.cfg)
		client.Namespace = c.ns
		client.ReuseValues = false
		client.Wait = true
		client.Timeout = 5 * time.Minute

		ch, err := loader.Load(resolvedChartPath)
		if err != nil {
			return nil, err
		}

		return client.Run(releaseName, ch, values)
	}

	// Install
	client := action.NewInstall(c.cfg)
	client.ReleaseName = releaseName
	client.Namespace = c.ns
	client.CreateNamespace = true
	client.Wait = true
	client.Timeout = 5 * time.Minute

	ch, err := loader.Load(resolvedChartPath)
	if err != nil {
		return nil, err
	}

	return client.Run(ch, values)
}

func (c *Client) resolveChartPath(chartPath string) (string, error) {
	chartPathOptions := action.ChartPathOptions{}
	return chartPathOptions.LocateChart(chartPath, c.settings)
}

func (c *Client) Uninstall(releaseName string) error {
	client := action.NewUninstall(c.cfg)
	_, err := client.Run(releaseName)
	return err
}
