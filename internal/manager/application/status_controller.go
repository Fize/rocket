package application

import (
	"context"
	"fmt"
	"sync"

	"github.com/hex-techs/rocket/internal/manager/cluster"
	"github.com/hex-techs/rocket/internal/manager/sharding"
	appsv1alpha1 "github.com/hex-techs/rocket/pkg/apis/apps/v1alpha1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

// StatusReconciler reconciles the status of Application objects
type StatusReconciler struct {
	client.Client
	Scheme        *runtime.Scheme
	ClientManager *cluster.ClientManager
	ShardManager  *sharding.ShardManager

	ctrl            controller.Controller
	watchedClusters sync.Map // map[string]bool
}

// Reconcile reads that state of the cluster for a Application object and makes changes based on the state read
func (r *StatusReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	app := &appsv1alpha1.Application{}
	if err := r.Get(ctx, req.NamespacedName, app); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// If application is being deleted, we don't need to update status
	if !app.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}

	// Ensure we are watching the clusters where this app is deployed
	for _, clusterName := range app.Status.AppliedClusters {
		// Check sharding responsibility
		if !r.ShardManager.IsResponsibleFor(clusterName) {
			continue
		}

		if err := r.ensureWatch(ctx, clusterName, app.Spec.Workload); err != nil {
			log.FromContext(ctx).Error(err, "Failed to ensure watch for cluster", "cluster", clusterName)
		}
	}

	// Aggregate status
	newStatus, err := r.aggregateStatus(ctx, app)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Update status if changed
	// We only update the clusters we are responsible for.
	// But we need to merge with existing status?
	// If we use SSA, we can just send the list of clusters we manage.
	// But controller-runtime Update() is not SSA by default.
	// We need to Patch() with Apply.

	// For now, let's assume we are the only one updating (Leader Election) OR we implement merging.
	// Since we added +listMapKey, we can try to update only our entries?
	// But standard Update replaces the whole status.
	// We need to use Patch with Apply.

	patch := client.MergeFrom(app.DeepCopy())
	app.Status.ClustersStatus = r.mergeClusterStatus(app.Status.ClustersStatus, newStatus.ClustersStatus)
	// Phase calculation needs global view.
	// If we are sharded, we can't calculate global Phase easily unless we read the full status first.
	// We read 'app' at the beginning. It has the current status (from all shards).
	// We update our part.
	// Then we recalculate Phase based on the merged status.

	app.Status.HealthPhase = r.calculatePhase(app.Status.ClustersStatus)

	if err := r.Status().Patch(ctx, app, patch); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *StatusReconciler) mergeClusterStatus(existing, new []appsv1alpha1.ClusterStatus) []appsv1alpha1.ClusterStatus {
	// Create a map of existing
	statusMap := make(map[string]appsv1alpha1.ClusterStatus)
	for _, s := range existing {
		statusMap[s.ClusterName] = s
	}

	// Update with new (only for clusters we manage)
	for _, s := range new {
		statusMap[s.ClusterName] = s
	}

	// Convert back to list
	result := make([]appsv1alpha1.ClusterStatus, 0, len(statusMap))
	for _, s := range statusMap {
		result = append(result, s)
	}
	return result
}

func (r *StatusReconciler) calculatePhase(statuses []appsv1alpha1.ClusterStatus) appsv1alpha1.HealthPhase {
	if len(statuses) == 0 {
		return appsv1alpha1.Unknown
	}
	allHealthy := true
	anyDegraded := false
	for _, cs := range statuses {
		if cs.Phase != appsv1alpha1.ClusterPhaseHealthy {
			allHealthy = false
		}
		if cs.Phase == appsv1alpha1.ClusterPhaseDegraded {
			anyDegraded = true
		}
	}

	if anyDegraded {
		return appsv1alpha1.Degraded
	} else if allHealthy {
		return appsv1alpha1.Healthy
	} else {
		return appsv1alpha1.Progressing
	}
}

func (r *StatusReconciler) ensureWatch(ctx context.Context, clusterName string, workload appsv1alpha1.WorkloadGVK) error {
	// Check if already watching
	key := fmt.Sprintf("%s/%s/%s", clusterName, workload.APIVersion, workload.Kind)
	if _, ok := r.watchedClusters.Load(key); ok {
		return nil
	}

	c, err := r.ClientManager.GetCluster(ctx, clusterName)
	if err != nil {
		return err
	}

	// Determine object to watch based on GVK
	u := &unstructured.Unstructured{}
	u.SetAPIVersion(workload.APIVersion)
	u.SetKind(workload.Kind)

	// Get informer
	informer, err := c.GetCache().GetInformer(ctx, u)
	if err != nil {
		return err
	}

	// Watch
	src := &source.Informer{
		Informer: informer,
		Handler: handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, o client.Object) []reconcile.Request {
			labels := o.GetLabels()
			appName := labels["rocket.io/application"]
			if appName == "" {
				return nil
			}
			return []reconcile.Request{
				{NamespacedName: types.NamespacedName{
					Name:      appName,
					Namespace: o.GetNamespace(),
				}},
			}
		}),
	}

	err = r.ctrl.Watch(src)
	if err != nil {
		return err
	}

	r.watchedClusters.Store(key, true)
	return nil
}

func (r *StatusReconciler) aggregateStatus(ctx context.Context, app *appsv1alpha1.Application) (*appsv1alpha1.ApplicationStatus, error) {
	status := &appsv1alpha1.ApplicationStatus{}
	status.ClustersStatus = []appsv1alpha1.ClusterStatus{}

	// We iterate over AppliedClusters
	for _, clusterName := range app.Status.AppliedClusters {
		if !r.ShardManager.IsResponsibleFor(clusterName) {
			continue
		}

		clusterStatus := appsv1alpha1.ClusterStatus{
			ClusterName: clusterName,
			Phase:       appsv1alpha1.ClusterPhaseUnknown,
		}

		c, err := r.ClientManager.GetClient(ctx, clusterName)
		if err != nil {
			clusterStatus.Message = fmt.Sprintf("Failed to get client: %v", err)
			status.ClustersStatus = append(status.ClustersStatus, clusterStatus)
			continue
		}

		// Get workload dynamically using Unstructured
		u := &unstructured.Unstructured{}
		u.SetAPIVersion(app.Spec.Workload.APIVersion)
		u.SetKind(app.Spec.Workload.Kind)

		if err := c.Get(ctx, types.NamespacedName{Name: app.Name, Namespace: app.Namespace}, u); err != nil {
			if errors.IsNotFound(err) {
				clusterStatus.Phase = appsv1alpha1.ClusterPhaseUnknown
				clusterStatus.Message = "Workload not found"
			} else {
				clusterStatus.Message = err.Error()
			}
		} else {
			// Extract status fields generically
			// Most workloads (Deployment, StatefulSet, Kruise cloneset) have these fields
			replicas, _, _ := unstructured.NestedInt64(u.Object, "spec", "replicas")
			readyReplicas, _, _ := unstructured.NestedInt64(u.Object, "status", "readyReplicas")

			clusterStatus.Replicas = int32(replicas)
			clusterStatus.ReadyReplicas = int32(readyReplicas)

			if clusterStatus.ReadyReplicas == clusterStatus.Replicas && clusterStatus.Replicas > 0 {
				clusterStatus.Phase = appsv1alpha1.ClusterPhaseHealthy
			} else {
				clusterStatus.Phase = appsv1alpha1.ClusterPhaseProgressing
			}

			// Check for common error conditions in status.conditions
			if conditions, found, _ := unstructured.NestedSlice(u.Object, "status", "conditions"); found {
				for _, cond := range conditions {
					if cMap, ok := cond.(map[string]interface{}); ok {
						cType, _ := cMap["type"].(string)
						cStatus, _ := cMap["status"].(string)
						cMessage, _ := cMap["message"].(string)

						// Common failure signals in Deployment/StatefulSet
						if (cType == "Progressing" && cStatus == "False") ||
							(cType == "ReplicaFailure" && cStatus == "True") {
							clusterStatus.Phase = appsv1alpha1.ClusterPhaseDegraded
							clusterStatus.Message = cMessage
						}
					}
				}
			}
		}

		status.ClustersStatus = append(status.ClustersStatus, clusterStatus)
	}

	// Determine overall phase
	if len(status.ClustersStatus) == 0 {
		status.HealthPhase = appsv1alpha1.Unknown
	} else {
		allHealthy := true
		anyDegraded := false
		for _, cs := range status.ClustersStatus {
			if cs.Phase != appsv1alpha1.ClusterPhaseHealthy {
				allHealthy = false
			}
			if cs.Phase == appsv1alpha1.ClusterPhaseDegraded {
				anyDegraded = true
			}
		}

		if anyDegraded {
			status.HealthPhase = appsv1alpha1.Degraded
		} else if allHealthy {
			status.HealthPhase = appsv1alpha1.Healthy
		} else {
			status.HealthPhase = appsv1alpha1.Progressing
		}
	}

	return status, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *StatusReconciler) SetupWithManager(mgr ctrl.Manager) error {
	c, err := controller.New("application-status-controller", mgr, controller.Options{
		Reconciler: r,
	})
	if err != nil {
		return err
	}
	r.ctrl = c

	return c.Watch(source.Kind(mgr.GetCache(), client.Object(&appsv1alpha1.Application{}), &handler.EnqueueRequestForObject{}))
}
