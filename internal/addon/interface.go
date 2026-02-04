package addon

import (
	"context"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// AddonConfig holds configuration for the addon
type AddonConfig struct {
	// ClusterName is the name of the target cluster
	ClusterName string
	// Config is the configuration map for the addon
	Config map[string]string
	// Client is the k8s client
	Client client.Client
}

// Addon defines the contract for a Rocket extension
type Addon interface {
	// Name returns the unique identifier (e.g., "mcs-discovery", "istio-mesh")
	Name() string

	// ManagerController returns the controller implementation for the Hub side
	// Returns nil if this addon only has Agent-side logic
	ManagerController(mgr ctrl.Manager) (AddonController, error)

	// AgentController returns the controller implementation for the Edge side
	// Returns nil if this addon only has Manager-side logic
	AgentController(mgr ctrl.Manager) (AddonController, error)

	// Manifests returns the generic CRDs or resources required by this addon
	Manifests() []runtime.Object
}

// AddonController defines the interface for addon controllers
type AddonController interface {
	// Reconcile handles the feature lifecycle (Install, Upgrade, ConfigUpdate, Uninstall)
	Reconcile(ctx context.Context, config AddonConfig) error
}
