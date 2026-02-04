package framework

// PluginConfig defines the configuration for a single plugin.
type PluginConfig struct {
	// Name is the name of the plugin
	Name string
	// Weight is the weight of the plugin in scoring (default: 1)
	Weight int64
	// Enabled indicates if the plugin is enabled (default: true)
	Enabled bool
	// Args contains plugin-specific configuration
	Args map[string]interface{}
}

// SchedulerConfig defines the configuration for the scheduler.
type SchedulerConfig struct {
	// FilterPlugins is the list of filter plugin configurations
	FilterPlugins []PluginConfig
	// ScorePlugins is the list of score plugin configurations
	ScorePlugins []PluginConfig
	// Strategy defines the scheduling strategy (SingleCluster or Spread)
	Strategy string
	// SpreadConstraints defines constraints for spread strategy
	SpreadConstraints *SpreadConstraints
}

// SpreadConstraints defines how to spread replicas across clusters.
type SpreadConstraints struct {
	// MaxClusters limits the maximum number of clusters to spread across
	MaxClusters int
	// MinReplicas is the minimum number of replicas per cluster
	MinReplicas int32
}

const (
	// StrategySingleCluster selects one best cluster
	StrategySingleCluster = "SingleCluster"
	// StrategySpread spreads replicas across multiple clusters
	StrategySpread = "Spread"

	// AnnotationSchedulerStrategy is the annotation key for defining scheduling strategy
	AnnotationSchedulerStrategy = "apps.rocket.io/scheduler-strategy"
)

// DefaultSchedulerConfig returns a default scheduler configuration.
func DefaultSchedulerConfig() *SchedulerConfig {
	return &SchedulerConfig{
		Strategy: StrategySpread,
		FilterPlugins: []PluginConfig{
			{Name: "Health", Enabled: true},
			{Name: "Affinity", Enabled: true},
			{Name: "TaintToleration", Enabled: true},
			{Name: "Capacity", Enabled: true},
			{Name: "VolumeRestriction", Enabled: true},
		},
		ScorePlugins: []PluginConfig{
			{Name: "Affinity", Enabled: true, Weight: 1},
			{Name: "Resource", Enabled: true, Weight: 1},
		},
		SpreadConstraints: &SpreadConstraints{
			MaxClusters: 5,
			MinReplicas: 1,
		},
	}
}
