package addon

import (
	"context"
	"fmt"

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

	// Track if status needs update
	statusUpdated := false

	// Iterate over all registered addons
	for _, a := range r.getRegistry().List() {
		addonName := a.Name()

		// Check if enabled in cluster spec
		enabled := false
		var config map[string]string

		for _, ca := range cluster.Spec.Addons {
			if ca.Name == addonName {
				enabled = ca.Enabled
				config = ca.Config
				break
			}
		}

		if !enabled {
			// Update status to Disabled if it was previously enabled
			r.updateAddonStatus(&cluster, addonName, "Disabled", "Addon is disabled")
			statusUpdated = true
			continue
		}

		controller, ok := r.Controllers[addonName]
		if !ok || controller == nil {
			// No controller registered, skip but mark as Pending
			r.updateAddonStatus(&cluster, addonName, "Pending", "Controller not registered")
			statusUpdated = true
			continue
		}

		addonConfig := addon.AddonConfig{
			ClusterName: cluster.Name,
			Config:      config,
			Client:      r.Client,
		}

		if err := controller.Reconcile(ctx, addonConfig); err != nil {
			// Update status with error
			r.updateAddonStatus(&cluster, addonName, "Failed", fmt.Sprintf("Reconcile error: %v", err))
			statusUpdated = true
			continue
		}

		// Update status as Applied
		r.updateAddonStatus(&cluster, addonName, "Applied", "Addon successfully applied")
		statusUpdated = true
	}

	// Update cluster status if needed
	if statusUpdated {
		// Re-fetch cluster to ensure we have the latest version
		var latestCluster storagev1alpha1.ManagedCluster
		if err := r.Get(ctx, req.NamespacedName, &latestCluster); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to re-fetch cluster: %w", err)
		}
		latestCluster.Status.AddonStatus = cluster.Status.AddonStatus
		if err := r.Status().Update(ctx, &latestCluster); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to update cluster addon status: %w", err)
		}
	}

	return ctrl.Result{}, nil
}

// updateAddonStatus updates the addon status in the cluster
func (r *AddonReconciler) updateAddonStatus(cluster *storagev1alpha1.ManagedCluster, addonName, state, message string) {
	// Find existing status
	for i := range cluster.Status.AddonStatus {
		if cluster.Status.AddonStatus[i].Name == addonName {
			cluster.Status.AddonStatus[i].State = state
			cluster.Status.AddonStatus[i].Message = message
			return
		}
	}

	// Add new status
	cluster.Status.AddonStatus = append(cluster.Status.AddonStatus, storagev1alpha1.AddonStatus{
		Name:    addonName,
		State:   state,
		Message: message,
	})
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
