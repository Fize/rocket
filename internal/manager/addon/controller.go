package addon

import (
	"context"

	"github.com/hex-techs/rocket/internal/addon"
	storagev1alpha1 "github.com/hex-techs/rocket/pkg/apis/storage/v1alpha1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// AddonReconciler reconciles Addons on ManagedCluster
type AddonReconciler struct {
	client.Client
	Scheme      *runtime.Scheme
	Controllers map[string]addon.AddonController
	// Registry allows injecting a custom addon registry for testing
	Registry addon.AddonRegistry
}

// getRegistry returns the addon registry, using global one if not set
func (r *AddonReconciler) getRegistry() addon.AddonRegistry {
	if r.Registry != nil {
		return r.Registry
	}
	return addon.GetRegistry()
}

// Reconcile handles the addon reconciliation
func (r *AddonReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var cluster storagev1alpha1.ManagedCluster
	if err := r.Get(ctx, req.NamespacedName, &cluster); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Iterate over all registered addons
	for _, a := range r.getRegistry().List() {
		// Check if enabled in cluster spec
		enabled := false
		var config map[string]string

		for _, ca := range cluster.Spec.Addons {
			if ca.Name == a.Name() {
				enabled = ca.Enabled
				config = ca.Config
				break
			}
		}

		if !enabled {
			// TODO: Handle uninstall if needed
			continue
		}

		controller, ok := r.Controllers[a.Name()]
		if !ok || controller == nil {
			continue
		}

		addonConfig := addon.AddonConfig{
			ClusterName: cluster.Name,
			Config:      config,
			Client:      r.Client,
		}

		if err := controller.Reconcile(ctx, addonConfig); err != nil {
			// Update status error
			// TODO: Update cluster status with error
			continue
		}
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *AddonReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.Controllers = make(map[string]addon.AddonController)

	for _, a := range r.getRegistry().List() {
		c, err := a.ManagerController(mgr)
		if err != nil {
			return err
		}
		if c != nil {
			r.Controllers[a.Name()] = c
		}
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&storagev1alpha1.ManagedCluster{}).
		Complete(r)
}
