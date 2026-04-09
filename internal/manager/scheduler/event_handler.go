package scheduler

import (
	"context"
	"fmt"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/fize/rocket/internal/manager/scheduler/cache"
	appsv1alpha1 "github.com/fize/rocket/pkg/apis/apps/v1alpha1"
	clusterv1alpha1 "github.com/fize/rocket/pkg/apis/storage/v1alpha1"
	"github.com/fize/rocket/internal/manager/scheduler/queue"
)

// EventHandler watches for Application and Cluster changes and updates the scheduler cache/queue.
type EventHandler struct {
	client.Client
	Cache cache.Cache
	Queue queue.SchedulingQueue
}

// needsScheduling checks if the application requires scheduling.
// This is true if:
// 1. It has no placement (Initial scheduling)
// 2. The desired replicas (Spec) differ from the scheduled replicas (Placement) (Scaling)
func needsScheduling(app *appsv1alpha1.Application) bool {
	// 1. Initial scheduling
	if len(app.Status.Placement.Topology) == 0 {
		return app.Status.SchedulingPhase == "" || app.Status.SchedulingPhase == appsv1alpha1.Pending
	}

	// 2. Scaling (Check for replica drift)
	if app.Spec.Replicas != nil {
		desired := *app.Spec.Replicas
		var scheduled int32 = 0
		for _, t := range app.Status.Placement.Topology {
			scheduled += t.Replicas
		}
		if desired != scheduled {
			return true
		}
	}

	// TODO: Add support for rebalancing (e.g. if clusters change)
	return false
}

func (e *EventHandler) SetupWithManager(mgr ctrl.Manager) error {
	// Watch Applications
	if err := ctrl.NewControllerManagedBy(mgr).
		For(&appsv1alpha1.Application{}).
		WithEventFilter(predicate.Funcs{
			CreateFunc: func(e event.CreateEvent) bool {
				return needsScheduling(e.Object.(*appsv1alpha1.Application))
			},
			UpdateFunc: func(e event.UpdateEvent) bool {
				newApp := e.ObjectNew.(*appsv1alpha1.Application)
				oldApp := e.ObjectOld.(*appsv1alpha1.Application)

				// Optimization: Only trigger if relevant fields changed
				if newApp.ResourceVersion == oldApp.ResourceVersion {
					return false
				}

				return needsScheduling(newApp)
			},
			DeleteFunc: func(e event.DeleteEvent) bool {
				return true
			},
		}).
		Complete(&AppReconciler{
			Client: e.Client,
			Queue:  e.Queue,
			Cache:  e.Cache,
		}); err != nil {
		return err
	}

	// Watch Clusters
	if err := ctrl.NewControllerManagedBy(mgr).
		For(&clusterv1alpha1.ManagedCluster{}).
		Complete(&ClusterReconciler{
			Client: e.Client,
			Cache:  e.Cache,
		}); err != nil {
		return err
	}

	return nil
}

type AppReconciler struct {
	client.Client
	Queue queue.SchedulingQueue
	Cache cache.Cache
}

func (r *AppReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var app appsv1alpha1.Application
	if err := r.Get(ctx, req.NamespacedName, &app); err != nil {
		// If not found, it might be deleted.
		dummy := &appsv1alpha1.Application{}
		dummy.Name = req.Name
		dummy.Namespace = req.Namespace
		r.Queue.Delete(dummy)
		r.Cache.RemoveAssumedApplication(fmt.Sprintf("%s/%s", req.Namespace, req.Name))
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if needsScheduling(&app) {
		r.Queue.Add(&app)
	} else {
		// If it's no longer needs scheduling (e.g. Scheduled or Applied), we can potentially
		// remove the assumption.
		// NOTE: In a real-world scenario, we might want to wait a bit longer
		// to ensure the cluster status has been updated.
		r.Cache.RemoveAssumedApplication(fmt.Sprintf("%s/%s", app.Namespace, app.Name))
	}

	return ctrl.Result{}, nil
}

type ClusterReconciler struct {
	client.Client
	Cache cache.Cache
}

func (r *ClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var cluster clusterv1alpha1.ManagedCluster
	if err := r.Get(ctx, req.NamespacedName, &cluster); err != nil {
		// If not found, remove from cache
		dummy := &clusterv1alpha1.ManagedCluster{}
		dummy.Name = req.Name
		r.Cache.RemoveCluster(dummy)
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if cluster.DeletionTimestamp != nil {
		r.Cache.RemoveCluster(&cluster)
	} else {
		if err := r.Cache.AddCluster(&cluster); err != nil {
			// If already exists, update it
			_ = r.Cache.UpdateCluster(&cluster, &cluster)
		}
	}

	return ctrl.Result{}, nil
}
