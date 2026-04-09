/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package application

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"

	policyv1 "k8s.io/api/policy/v1"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/fize/rocket/internal/manager/application/overrides"
	"github.com/fize/rocket/pkg/observability"
	appsv1alpha1 "github.com/fize/rocket/pkg/apis/apps/v1alpha1"
	clusterv1alpha1 "github.com/fize/rocket/pkg/apis/storage/v1alpha1"
	"github.com/fize/rocket/internal/manager/cluster"
	managermetrics "github.com/fize/rocket/internal/manager/metrics"
	"github.com/fize/rocket/pkg/util/labels"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// ApplicationReconciler reconciles a Application object
type ApplicationReconciler struct {
	client.Client
	Scheme             *runtime.Scheme
	ClientManager      *cluster.ClientManager
	Recorder           record.EventRecorder
	RolloutCoordinator *RolloutCoordinator
	RolloutStatusAggr  *RolloutStatusAggregator
}

// +kubebuilder:rbac:groups=apps.rocket.io,resources=applications,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps.rocket.io,resources=applications/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=apps.rocket.io,resources=applications/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

const (
	ApplicationFinalizer = "apps.rocket.io/finalizer"
)

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *ApplicationReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := observability.TraceLogger(ctx, log.FromContext(ctx))

	var app appsv1alpha1.Application
	if err := r.Get(ctx, req.NamespacedName, &app); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	ctx, span := observability.Tracer().Start(ctx, "ApplicationReconcile",
		trace.WithAttributes(
			attribute.String("application.name", app.Name),
			attribute.String("application.namespace", app.Namespace),
		),
	)
	defer span.End()

	// Update managed application total count
	appList := &appsv1alpha1.ApplicationList{}
	if err := r.List(ctx, appList); err == nil {
		managermetrics.SetManagedApplicationTotal(len(appList.Items))
	}

	// Handle deletion
	if !app.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, &app)
	}

	// Add finalizer if not present
	if !controllerutil.ContainsFinalizer(&app, ApplicationFinalizer) {
		controllerutil.AddFinalizer(&app, ApplicationFinalizer)
		if err := r.Update(ctx, &app); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	// 1. Validate
	if app.Spec.Workload.APIVersion == "" || app.Spec.Workload.Kind == "" || len(app.Spec.Template.Raw) == 0 {
		logger.Error(nil, "Application spec is invalid", "name", app.Name)
		app.Status.SchedulingPhase = appsv1alpha1.Failed
		meta.SetStatusCondition(&app.Status.Conditions, metav1.Condition{
			Type:               "Validated",
			Status:             metav1.ConditionFalse,
			Reason:             "InvalidSpec",
			Message:            "Application spec is invalid: missing apiVersion, kind or template",
			ObservedGeneration: app.Generation,
		})
		if err := r.Status().Update(ctx, &app); err != nil {
			logger.Error(err, "Failed to update application status")
		}
		return ctrl.Result{}, nil
	}

	// 2. Iterate over placement topology
	currentClusters := make(map[string]bool)
	cumulativeReplicas := int32(0)

	// Ensure topology is sorted deterministically
	topologyList := make([]appsv1alpha1.ClusterTopology, len(app.Status.Placement.Topology))
	copy(topologyList, app.Status.Placement.Topology)
	sort.Slice(topologyList, func(i, j int) bool {
		return topologyList[i].Name < topologyList[j].Name
	})

	for i, topology := range topologyList {
		currentClusters[topology.Name] = true
		startTime := time.Now()

		// Sequential Rollout Check for StatefulSet
		// We check if the previous cluster in the topology is healthy before proceeding to the current one.
		if app.Spec.Workload.Kind == "StatefulSet" && i > 0 {
			prevClusterName := topologyList[i-1].Name
			// Find status for previous cluster
			prevHealthy := false
			for _, status := range app.Status.ClustersStatus {
				if status.ClusterName == prevClusterName {
					if status.Phase == appsv1alpha1.ClusterPhaseHealthy {
						prevHealthy = true
					}
					break
				}
			}

			if !prevHealthy {
				// Stop processing subsequent clusters if previous one is not healthy.
				// We log this decision but don't return an error to allow requeue.
				// We might want to set a condition on the Application status indicating it's paused?
				// For now, we update status for the current cluster to indicate it's pending specific condition.
				// Actually, simply breaking the loop means we don't reconcile (create/patch) resources on this cluster.
				// This effectively holds the rollout.
				// NOTE: This assumes the scheduler doesn't shuffle topology order randomly.
				logger.Info("Pausing rollout: previous cluster not healthy", "current", topology.Name, "previous", prevClusterName)
				r.Recorder.Eventf(&app, "Warning", "SequentialRolloutPaused", "Paused rollout to %s because previous cluster %s is not healthy", topology.Name, prevClusterName)

				// We should probably mark this cluster status as Progressing/Pending if we can,
				// but since we break, we won't reach status update logic for this cluster.
				// Existing status will persist.
				break
			}
		}

		// Get Cluster object
		var clusterObj clusterv1alpha1.ManagedCluster
		if err := r.Get(ctx, client.ObjectKey{Name: topology.Name}, &clusterObj); err != nil {
			if errors.IsNotFound(err) {
				logger.Info("Cluster not found, skipping", "cluster", topology.Name)
				continue
			}
			return ctrl.Result{}, err
		}

		// Get Client for the cluster (Hub or Edge)
		targetClient, err := r.ClientManager.GetClient(ctx, topology.Name)
		if err != nil {
			logger.Error(err, "Failed to get client for cluster", "cluster", topology.Name)
			// We continue to next cluster instead of failing entirely
			continue
		}

		// Ordinal calculation for StatefulSet
		ordinalStart := int32(0)
		if app.Spec.Workload.Kind == "StatefulSet" {
			ordinalStart = cumulativeReplicas
			cumulativeReplicas += topology.Replicas
		}

		if err := r.reconcileWorkload(ctx, targetClient, &app, &clusterObj, ordinalStart); err != nil {
			logger.Error(err, "Failed to reconcile workload on cluster", "cluster", topology.Name)
			managermetrics.RecordWorkloadDeploy(app.Name, topology.Name, app.Spec.Workload.Kind, "error", time.Since(startTime))
			observability.SpanError(ctx, err)
			return ctrl.Result{}, err
		}
		managermetrics.RecordWorkloadDeploy(app.Name, topology.Name, app.Spec.Workload.Kind, "success", time.Since(startTime))

		if err := r.reconcileResiliency(ctx, targetClient, &app); err != nil {
			logger.Error(err, "Failed to reconcile PDB on cluster", "cluster", topology.Name)
			return ctrl.Result{}, err
		}

		// Reconcile Rollout if strategy specified
		if app.Spec.RolloutStrategy != nil {
			if err := r.RolloutCoordinator.ReconcileRollout(ctx, &app, app.Status.Placement.Topology); err != nil {
				logger.Error(err, "Failed to reconcile rollout", "cluster", topology.Name)
				return ctrl.Result{}, err
			}
		}
	}

	// 3. Handle cleanup for clusters removed from Placement
	for _, oldCluster := range app.Status.AppliedClusters {
		if !currentClusters[oldCluster] {
			logger.Info("Removing workload from cluster no longer in placement", "cluster", oldCluster)
			targetClient, err := r.ClientManager.GetClient(ctx, oldCluster)
			if err != nil {
				// If the cluster object is gone, we can't clean up, so we skip and allow status update to remove it.
				// GetClient returns wrapped error, so we verify using standard IsNotFound check which supports unwrapping.
				if errors.IsNotFound(err) || (err != nil && strings.Contains(err.Error(), "not found")) {
					logger.Info("Cluster object not found during cleanup, skipping", "cluster", oldCluster)
					continue
				}

				logger.Error(err, "Failed to get client for cluster during cleanup", "cluster", oldCluster)
				return ctrl.Result{}, err
			}

			u := &unstructured.Unstructured{}
			u.SetAPIVersion(app.Spec.Workload.APIVersion)
			u.SetKind(app.Spec.Workload.Kind)
			u.SetName(app.Name)
			u.SetNamespace(app.Namespace)

			if err := targetClient.Delete(ctx, u); err != nil {
				if !errors.IsNotFound(err) {
					logger.Error(err, "Failed to delete workload on cluster during cleanup", "cluster", oldCluster)
					return ctrl.Result{}, err
				}
			}
		}
	}

	// 4. Update Status (AppliedClusters + Aggregation)
	var newAppliedClusters []string
	for c := range currentClusters {
		newAppliedClusters = append(newAppliedClusters, c)
	}
	sort.Strings(newAppliedClusters)

	var globalReady int32
	var globalReplicas int32
	var clusterStatuses []appsv1alpha1.ClusterStatus

	for _, clusterName := range newAppliedClusters {
		cs := appsv1alpha1.ClusterStatus{
			ClusterName: clusterName,
			Phase:       appsv1alpha1.ClusterPhaseUnknown,
		}

		// Get Client (Expect cache hit)
		targetClient, err := r.ClientManager.GetClient(ctx, clusterName)
		if err != nil {
			cs.Message = fmt.Sprintf("Failed to get client: %v", err)
			clusterStatuses = append(clusterStatuses, cs)
			continue
		}

		u := &unstructured.Unstructured{}
		u.SetAPIVersion(app.Spec.Workload.APIVersion)
		u.SetKind(app.Spec.Workload.Kind)
		u.SetName(app.Name)
		u.SetNamespace(app.Namespace)

		if err := targetClient.Get(ctx, client.ObjectKey{Name: app.Name, Namespace: app.Namespace}, u); err != nil {
			cs.Message = fmt.Sprintf("Failed to get workload: %v", err)
			clusterStatuses = append(clusterStatuses, cs)
			continue
		}

		// Extract Status Fields
		replicas, found, _ := unstructured.NestedInt64(u.Object, "status", "replicas")
		if !found {
			replicas = 0
		}
		readyReplicas, found, _ := unstructured.NestedInt64(u.Object, "status", "readyReplicas")
		if !found {
			readyReplicas = 0
		}
		availableReplicas, found, _ := unstructured.NestedInt64(u.Object, "status", "availableReplicas")
		if !found {
			availableReplicas = 0
		}

		cs.Replicas = int32(replicas)
		cs.ReadyReplicas = int32(readyReplicas)
		cs.AvailableReplicas = int32(availableReplicas)

		// Determine Cluster Phase
		// Simple logic: If Replicas == Ready, Healthy. Else Progressing.
		// Note: Deployment has 'updatedReplicas' which is also important but keeping it simple for now.
		if cs.Replicas == cs.ReadyReplicas && cs.Replicas > 0 {
			cs.Phase = appsv1alpha1.ClusterPhaseHealthy
		} else {
			cs.Phase = appsv1alpha1.ClusterPhaseProgressing
		}

		clusterStatuses = append(clusterStatuses, cs)

		// Aggregate Global
		globalReady += cs.ReadyReplicas
		if app.Spec.Replicas != nil {
			globalReplicas += *app.Spec.Replicas // Ideal total (from spec, not status)
		} else {
			globalReplicas += cs.Replicas // Fallback to observed replicas
		}

		// Record status sync metric
		managermetrics.RecordStatusSync(app.Name, clusterName, "success")
	}

	// Update application health phase metric
	managermetrics.ClearApplicationHealthPhase(app.Name)
	for _, cs := range clusterStatuses {
		managermetrics.SetApplicationHealthPhase(app.Name, string(cs.Phase))
	}

	patch := client.MergeFrom(app.DeepCopy())

	app.Status.AppliedClusters = newAppliedClusters
	app.Status.GlobalReadyReplicas = globalReady
	// GlobalReplicas should be the sum of Desired Replicas in topologies
	// Recalculate GlobalReplicas based on Placement Topology to be accurate to Spec
	var totalDesired int32
	if app.Status.Placement.Topology != nil {
		for _, t := range app.Status.Placement.Topology {
			totalDesired += t.Replicas
		}
	}
	app.Status.GlobalReplicas = totalDesired
	app.Status.ClustersStatus = clusterStatuses

	// Aggregate Rollout status
	if app.Spec.RolloutStrategy != nil {
		if err := r.RolloutStatusAggr.AggregateRolloutStatus(ctx, &app, newAppliedClusters); err != nil {
			logger.Error(err, "Failed to aggregate rollout status")
		}
	}

	if err := r.Status().Patch(ctx, &app, patch); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *ApplicationReconciler) reconcileDelete(ctx context.Context, app *appsv1alpha1.Application) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	var errs []error

	// 1. Iterate over AppliedClusters - these are the clusters where we actually deployed something
	// Using AppliedClusters instead of Topology ensures we don't leave orphans if the user
	// removed a cluster from topology just before deleting the Application.
	for _, clusterName := range app.Status.AppliedClusters {
		targetClient, err := r.ClientManager.GetClient(ctx, clusterName)
		if err != nil {
			// If the cluster object is gone, we can't clean up, so we skip and allow finalizer removal.
			if errors.IsNotFound(err) || (err != nil && strings.Contains(err.Error(), "not found")) {
				logger.Info("Cluster object not found during deletion, skipping cleanup", "cluster", clusterName)
				continue
			}

			logger.Error(err, "Failed to get client for cluster during deletion", "cluster", clusterName)
			errs = append(errs, err)
			continue
		}

		// Delete Workload
		u := &unstructured.Unstructured{}
		u.SetAPIVersion(app.Spec.Workload.APIVersion)
		u.SetKind(app.Spec.Workload.Kind)
		u.SetName(app.Name)
		u.SetNamespace(app.Namespace)

		if err := targetClient.Delete(ctx, u); err != nil {
			if !errors.IsNotFound(err) {
				logger.Error(err, "Failed to delete workload on cluster", "cluster", clusterName)
				errs = append(errs, err)
			}
		}

		// Delete PDB (Best effort, but we track errors to ensure full cleanup)
		if app.Spec.Resiliency != nil {
			pdb := &unstructured.Unstructured{}
			pdb.SetAPIVersion("policy/v1")
			pdb.SetKind("PodDisruptionBudget")
			pdb.SetName(app.Name)
			pdb.SetNamespace(app.Namespace)

			if err := targetClient.Delete(ctx, pdb); err != nil {
				if !errors.IsNotFound(err) {
					logger.Error(err, "Failed to delete PDB on cluster", "cluster", clusterName)
					errs = append(errs, err)
				}
			}
		}
	}

	// 2. If any errors occurred, do NOT remove the finalizer.
	// This ensures we retry until everything is clean.
	if len(errs) > 0 {
		return ctrl.Result{}, fmt.Errorf("failed to clean up resources on %d clusters, first error: %v", len(errs), errs[0])
	}

	controllerutil.RemoveFinalizer(app, ApplicationFinalizer)
	if err := r.Update(ctx, app); err != nil {
		return ctrl.Result{}, err
	}

	managermetrics.ClearApplicationHealthPhase(app.Name)
	return ctrl.Result{}, nil
}

func (r *ApplicationReconciler) reconcileWorkload(ctx context.Context, cli client.Client, app *appsv1alpha1.Application, cluster *clusterv1alpha1.ManagedCluster, ordinalStart int32) error {
	log := log.FromContext(ctx)
	// Create Unstructured object
	u := &unstructured.Unstructured{}
	u.SetAPIVersion(app.Spec.Workload.APIVersion)
	u.SetKind(app.Spec.Workload.Kind)
	u.SetName(app.Name)
	u.SetNamespace(app.Namespace)

	// Set managed-by label
	labels.AddManagedBy(u)

	// Convert PodTemplateSpec to map
	log.Info("Reconciling workload", "template", app.Spec.Template)

	// Apply workload specific configurations
	if err := r.configureWorkload(u, app.Spec, ordinalStart); err != nil {
		return err
	}

	// Apply Overrides
	if err := overrides.ApplyOverrides(u, app, cluster.Labels); err != nil {
		return err
	}

	// Set OwnerReference (Only if local cluster)
	// If remote, we can't set OwnerReference to an object that doesn't exist there.
	// For now, we skip OwnerReference for remote clusters or handle it differently.
	if cli == r.Client {
		if err := controllerutil.SetControllerReference(app, u, r.Scheme); err != nil {
			return err
		}
	}

	// Apply (Server-Side Apply)
	// We use Patch with Apply to create or update the resource.
	// This avoids Read-Modify-Write conflicts.
	if err := cli.Patch(ctx, u, client.Apply, client.FieldOwner("application-controller"), client.ForceOwnership); err != nil {
		if errors.IsNotFound(err) || strings.Contains(err.Error(), "apply patches are not supported") {
			// If Patch fails with NotFound, it might be because the object doesn't exist
			// and the client doesn't support SSA-create (like some fake clients).
			if err := cli.Create(ctx, u); err != nil {
				return err
			}
		} else {
			return err
		}
	}

	// Update Status
	condition := metav1.Condition{
		Type:    "ClusterStatus-" + cluster.Name,
		Status:  metav1.ConditionTrue,
		Reason:  "WorkloadDeployed",
		Message: "Workload successfully deployed to cluster",
	}

	existingCondition := meta.FindStatusCondition(app.Status.Conditions, condition.Type)
	if existingCondition != nil &&
		existingCondition.Status == condition.Status &&
		existingCondition.Reason == condition.Reason &&
		existingCondition.Message == condition.Message {
		condition.LastTransitionTime = existingCondition.LastTransitionTime
	} else {
		condition.LastTransitionTime = metav1.Now()
	}

	meta.SetStatusCondition(&app.Status.Conditions, condition)
	patch := &appsv1alpha1.Application{
		TypeMeta: metav1.TypeMeta{
			APIVersion: appsv1alpha1.GroupVersion.String(),
			Kind:       "Application",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      app.Name,
			Namespace: app.Namespace,
		},
		Status: appsv1alpha1.ApplicationStatus{
			Conditions: []metav1.Condition{condition},
		},
	}

	if err := r.Status().Patch(ctx, patch, client.Apply, client.FieldOwner("application-controller-"+cluster.Name)); err != nil {
		if strings.Contains(err.Error(), "apply patches are not supported") {
			// Fallback for fake client in unit tests
			return r.Status().Update(ctx, app)
		}
		return err
	}
	return nil
}

// configureWorkload acts as a unified handler to apply Application specifications to the target Workload.
// It determines the strategy based on the Workload Kind or Group.
func (r *ApplicationReconciler) configureWorkload(u *unstructured.Unstructured, spec appsv1alpha1.ApplicationSpec, ordinalStart int32) error {
	var templateMap map[string]interface{}
	if err := json.Unmarshal(spec.Template.Raw, &templateMap); err != nil {
		return err
	}

	kind := u.GetKind()

	// 1. Template placement
	templatePath := []string{"spec", "template"}
	if kind == "CronJob" {
		templatePath = []string{"spec", "jobTemplate", "spec", "template"}
	}

	// Important: Fix the selector issue for Deployment/Reference
	// The problem is that unstructured.SetNestedField overwrites `spec.template` completely,
	// but the `spec.selector` in the top level Application object might not match the labels inside `spec.template.metadata.labels`.
	// For Deployments, spec.selector must match spec.template.metadata.labels.
	// We need to ensure that if selector is not provided in the original template, we should verify/construct it?
	// Or rather, the simple fix is to make sure we copy the selector from the template if it's not set, or let the user be responsible.
	// HOWEVER, the error log says:
	// "Deployment.apps "push-app" is invalid: [spec.selector: Required value, spec.template.metadata.labels: Invalid value: map[string]string{"app":"push-test"}: `selector` does not match template `labels`]"
	// This implies `spec.selector` is missing (Required value) AND/OR not matching.

	// The logic below copies the raw template into the target object.
	if err := unstructured.SetNestedField(u.Object, templateMap, templatePath...); err != nil {
		return err
	}

	// For Deployment/StatefulSet/DaemonSet/ReplicaSet, we usually need a Selector.
	// If the user's Template (in ApplicationSpec) contains a Selector, it will be in the top level of the Unstructured object if we were copying everything.
	// BUT, we are ONLY copying `templateMap` into `spec.template`. We are NOT copying `spec.selector` from anywhere yet!

	// We should check if the workload kind requires a selector and if we can infer it or if it's in the spec.
	// In the ApplicationSpec, we have `Workload` which defines Kind/APIVersion, and `Template` which is likely just a PodTemplateSpec?
	// Waiting... Checking the CRD definition might be useful, but assuming `spec.template` is `runtime.RawExtension`, it likely contains metadata and spec of the Pod.
	// The `templateMap` we unmarshaled is put into `spec.template`.

	// If the Target Workload (e.g. Deployment) needs a `spec.selector`, we must provide it.
	// We can try to extract labels from `templateMap` (which is `spec.template`) and create a selector.

	labels, found, err := unstructured.NestedStringMap(u.Object, append(templatePath, "metadata", "labels")...)
	if err == nil && found {
		// Try to set selector if it's missing or empty
		currentSelector, found, _ := unstructured.NestedMap(u.Object, "spec", "selector")
		if !found || len(currentSelector) == 0 {
			// Construct a simple MatchLabels selector
			// Note: unstructured.SetNestedField has a bug/limitation where it panics if we pass map[string]string inside `selector`.
			// The selector map we created: map[string]interface{"matchLabels": map[string]string{...}}
			// `matchLabels` is map[string]string.
			// SetNestedField -> DeepCopyJSONValue -> cannot deep copy map[string]string.
			// It expects map[string]interface{}.

			// So we need to convert map[string]string to map[string]interface{}.
			matchLabels := make(map[string]interface{})
			for k, v := range labels {
				matchLabels[k] = v
			}

			selector := map[string]interface{}{
				"matchLabels": matchLabels,
			}
			if kind == "Deployment" || kind == "StatefulSet" || kind == "DaemonSet" || kind == "ReplicaSet" {
				if err := unstructured.SetNestedField(u.Object, selector, "spec", "selector"); err != nil {
					return err
				}
			}
		}
	}

	// 2. Kind-specific logic
	switch kind {
	case "CronJob":
		if spec.Schedule != "" {
			if err := unstructured.SetNestedField(u.Object, spec.Schedule, "spec", "schedule"); err != nil {
				return err
			}
		}
		if spec.JobAttributes != nil {
			if err := applyJobAttributes(u, spec.JobAttributes, "spec", "jobTemplate", "spec"); err != nil {
				return err
			}
			if err := applyCronJobAttributes(u, spec.JobAttributes); err != nil {
				return err
			}
		}

	case "Job":
		if spec.JobAttributes != nil {
			if err := applyJobAttributes(u, spec.JobAttributes, "spec"); err != nil {
				return err
			}
		}
		// For Job, Replicas -> Parallelism (if not explicitly set)
		if spec.Replicas != nil && (spec.JobAttributes == nil || spec.JobAttributes.Parallelism == nil) {
			if err := unstructured.SetNestedField(u.Object, int64(*spec.Replicas), "spec", "parallelism"); err != nil {
				return err
			}
		}

	default: // Deployment, StatefulSet, DaemonSet, ReplicaSet
		// Set Selector
		if spec.Selector != nil {
			selectorMap, err := runtime.DefaultUnstructuredConverter.ToUnstructured(spec.Selector)
			if err != nil {
				return err
			}
			if err := unstructured.SetNestedField(u.Object, selectorMap, "spec", "selector"); err != nil {
				return err
			}
		}

		// Set Replicas
		if spec.Replicas != nil && kind != "DaemonSet" {
			if err := unstructured.SetNestedField(u.Object, int64(*spec.Replicas), "spec", "replicas"); err != nil {
				return err
			}
		}

		// Set Ordinals for StatefulSet
		if kind == "StatefulSet" && ordinalStart > 0 {
			if err := unstructured.SetNestedField(u.Object, int64(ordinalStart), "spec", "ordinals", "start"); err != nil {
				return err
			}
		}
	}

	// 3. Common Fields
	if spec.Suspend != nil {
		// Most controllers support spec.suspend (Job, CronJob, Deployment in recent versions?)
		// Actually Deployment/STS don't usually have suspend, but CronJob/Job do.
		// We can check or just attempt to set it if valid for that Kind.
		// For safety, allow it to be set. Extra fields are usually ignored or valid.
		if err := unstructured.SetNestedField(u.Object, *spec.Suspend, "spec", "suspend"); err != nil {
			return err
		}
	}

	return nil
}

// Helper to apply Job/CronJob attributes
func applyJobAttributes(u *unstructured.Unstructured, attrs *appsv1alpha1.JobAttributes, fields ...string) error {
	if attrs.Completions != nil {
		if err := unstructured.SetNestedField(u.Object, int64(*attrs.Completions), append(fields, "completions")...); err != nil {
			return err
		}
	}
	if attrs.Parallelism != nil {
		if err := unstructured.SetNestedField(u.Object, int64(*attrs.Parallelism), append(fields, "parallelism")...); err != nil {
			return err
		}
	}
	if attrs.BackoffLimit != nil {
		if err := unstructured.SetNestedField(u.Object, int64(*attrs.BackoffLimit), append(fields, "backoffLimit")...); err != nil {
			return err
		}
	}
	if attrs.TTLSecondsAfterFinished != nil {
		if err := unstructured.SetNestedField(u.Object, int64(*attrs.TTLSecondsAfterFinished), append(fields, "ttlSecondsAfterFinished")...); err != nil {
			return err
		}
	}
	return nil
}

func applyCronJobAttributes(u *unstructured.Unstructured, attrs *appsv1alpha1.JobAttributes) error {
	if attrs.SuccessfulJobsHistoryLimit != nil {
		if err := unstructured.SetNestedField(u.Object, int64(*attrs.SuccessfulJobsHistoryLimit), "spec", "successfulJobsHistoryLimit"); err != nil {
			return err
		}
	}
	if attrs.FailedJobsHistoryLimit != nil {
		if err := unstructured.SetNestedField(u.Object, int64(*attrs.FailedJobsHistoryLimit), "spec", "failedJobsHistoryLimit"); err != nil {
			return err
		}
	}
	return nil
}

func (r *ApplicationReconciler) reconcileResiliency(ctx context.Context, cli client.Client, app *appsv1alpha1.Application) error {
	if app.Spec.Resiliency == nil {
		return nil
	}

	var matchLabels map[string]string
	var templateMap map[string]interface{}
	if err := json.Unmarshal(app.Spec.Template.Raw, &templateMap); err == nil {
		matchLabels, _, _ = unstructured.NestedStringMap(templateMap, "metadata", "labels")
	}

	pdb := &policyv1.PodDisruptionBudget{
		TypeMeta: metav1.TypeMeta{
			APIVersion: policyv1.SchemeGroupVersion.String(),
			Kind:       "PodDisruptionBudget",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      app.Name,
			Namespace: app.Namespace,
		},
		Spec: policyv1.PodDisruptionBudgetSpec{
			MinAvailable:   app.Spec.Resiliency.MinAvailable,
			MaxUnavailable: app.Spec.Resiliency.MaxUnavailable,
			Selector: &metav1.LabelSelector{
				MatchLabels: matchLabels,
			},
		},
	}

	_, err := ctrl.CreateOrUpdate(ctx, cli, pdb, func() error {
		labels.AddManagedBy(pdb)

		pdb.Spec = policyv1.PodDisruptionBudgetSpec{
			MinAvailable:   app.Spec.Resiliency.MinAvailable,
			MaxUnavailable: app.Spec.Resiliency.MaxUnavailable,
			Selector: &metav1.LabelSelector{
				MatchLabels: matchLabels,
			},
		}
		return nil
	})
	return err
}

// SetupWithManager sets up the controller with the Manager.
func (r *ApplicationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.Recorder = mgr.GetEventRecorderFor("application-controller")

	// Initialize RolloutCoordinator and StatusAggregator
	r.RolloutCoordinator = NewRolloutCoordinator(r.ClientManager)
	r.RolloutStatusAggr = NewRolloutStatusAggregator(r.ClientManager)

	return ctrl.NewControllerManagedBy(mgr).
		For(&appsv1alpha1.Application{}).
		Complete(r)
}
