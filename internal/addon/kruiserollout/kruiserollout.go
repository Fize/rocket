package kruiserollout

import (
	"github.com/fize/rocket/internal/addon"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
)

func init() {
	addon.Register(&KruiseRolloutAddon{})
}

// KruiseRolloutAddon implements the addon.Addon interface for kruise-rollout
type KruiseRolloutAddon struct{}

// Name returns the addon name
func (a *KruiseRolloutAddon) Name() string {
	return AddonName
}

// ManagerController returns a controller that installs kruise-rollout on Hub clusters
// This is necessary when the Hub cluster also runs workloads with rollout strategies
func (a *KruiseRolloutAddon) ManagerController(mgr ctrl.Manager) (addon.AddonController, error) {
	return &ManagerController{}, nil
}

// AgentController returns the controller for edge clusters
func (a *KruiseRolloutAddon) AgentController(mgr ctrl.Manager) (addon.AddonController, error) {
	return &AgentController{}, nil
}

// Manifests returns no additional CRDs (kruise-rollout CRDs are installed via Helm)
func (a *KruiseRolloutAddon) Manifests() []runtime.Object {
	return []runtime.Object{}
}
