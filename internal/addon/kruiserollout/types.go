package kruiserollout

const (
	// AddonName is the unique identifier for the kruise-rollout addon
	AddonName = "kruise-rollout"

	// DefaultVersion is the default kruise-rollout version to install
	DefaultVersion = "0.6.2"

	// DefaultNamespace is the namespace where kruise-rollout will be installed
	DefaultNamespace = "kruise-rollout"

	// DefaultReleaseName is the Helm release name
	DefaultReleaseName = "kruise-rollout"

	// DefaultRepoURL is the default Helm chart repository URL
	DefaultRepoURL = "https://openkruise.github.io/charts/"

	// DefaultChartName is the default chart name
	DefaultChartName = "kruise-rollout"

	// Environment variables for chart configuration
	EnvChartURL     = "CHART_KRUISE_ROLLOUT_URL"
	EnvChartRepoURL = "CHART_KRUISE_ROLLOUT_REPO"
	EnvChartName    = "CHART_KRUISE_ROLLOUT_NAME"
	EnvChartVersion = "CHART_KRUISE_ROLLOUT_VERSION"

	// Config keys for ManagedCluster addon config
	ConfigChartURL        = "chartURL"
	ConfigChartRepoURL    = "chartRepoURL"
	ConfigChartName       = "chartName"
	ConfigChartVersion    = "chartVersion"
	ConfigValuesConfigMap = "valuesConfigMap"
	ConfigValuesSecret    = "valuesSecret"
	ConfigValuesNamespace = "valuesNamespace"
)

// ChartConfig holds the configuration for the kruise-rollout Helm chart
type ChartConfig struct {
	URL          string
	RepoURL      string
	ChartName    string
	ChartVersion string
}
