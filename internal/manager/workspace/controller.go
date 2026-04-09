/*
Copyright 2026.

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

package workspace

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/fize/rocket/internal/manager/cluster"
	storagev1alpha1 "github.com/fize/rocket/pkg/apis/storage/v1alpha1"
	workspacev1alpha1 "github.com/fize/rocket/pkg/apis/workspace/v1alpha1"
	pkglabel "github.com/fize/rocket/pkg/util/labels"
)

// WorkspaceReconciler reconciles a Workspace object
type WorkspaceReconciler struct {
	client.Client
	Scheme        *runtime.Scheme
	ClientManager *cluster.ClientManager
}

// +kubebuilder:rbac:groups=workspace.rocket.io,resources=workspaces,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=workspace.rocket.io,resources=workspaces/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=storage.rocket.io,resources=managedclusters,verbs=get;list;watch

// Reconcile is part of the main kubernetes reconciliation loop
func (r *WorkspaceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var workspace workspacev1alpha1.Workspace
	if err := r.Get(ctx, req.NamespacedName, &workspace); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	nsName := workspace.Spec.Name
	if nsName == "" {
		nsName = workspace.Name
	}

	// 0. Ensure namespace exists in Hub cluster
	hubNS := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: nsName,
		},
	}
	// Create or Update the Namespace, ensuring the OwnerReference is set (Adoption)
	if _, err := ctrl.CreateOrUpdate(ctx, r.Client, hubNS, func() error {
		pkglabel.AddManagedBy(hubNS)

		// This block is executed for both Create (before) and Update (after fetching)
		// It ensures that even if the Namespace exists, we try to adopt it by setting the OwnerRef.
		return controllerutil.SetControllerReference(&workspace, hubNS, r.Scheme)
	}); err != nil {
		logger.Error(err, "Failed to reconcile hub namespace")
		return ctrl.Result{}, err
	}

	// 1. List all ManagedClusters
	var clusterList storagev1alpha1.ManagedClusterList
	if err := r.List(ctx, &clusterList); err != nil {
		logger.Error(err, "Failed to list managed clusters")
		return ctrl.Result{}, err
	}

	// 2. Filter clusters
	var targetClusters []storagev1alpha1.ManagedCluster
	selector, err := metav1.LabelSelectorAsSelector(workspace.Spec.ClusterSelector)
	if err != nil {
		logger.Error(err, "Invalid cluster selector")
		return ctrl.Result{}, nil
	}

	for _, c := range clusterList.Items {
		if selector.Matches(labels.Set(c.Labels)) {
			targetClusters = append(targetClusters, c)
		}
	}

	// 3. Propagate to each cluster
	var appliedClusters []string
	for _, c := range targetClusters {
		nsName := workspace.Spec.Name
		if nsName == "" {
			nsName = workspace.Name
		}

		edgeClient, err := r.ClientManager.GetClient(ctx, c.Name)
		if err != nil {
			logger.Error(err, "Failed to get client for cluster", "cluster", c.Name)
			continue
		}

		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: nsName,
			},
		}
		if _, err := ctrl.CreateOrUpdate(ctx, edgeClient, ns, func() error {
			pkglabel.AddManagedBy(ns)
			return nil
		}); err != nil {
			logger.Error(err, "Failed to reconcile namespace", "cluster", c.Name)
			continue
		}

		if workspace.Spec.ResourceConstraints != nil && workspace.Spec.ResourceConstraints.Quota != nil {
			quota := &corev1.ResourceQuota{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "workspace-quota",
					Namespace: nsName,
				},
			}
			if _, err := ctrl.CreateOrUpdate(ctx, edgeClient, quota, func() error {
				pkglabel.AddManagedBy(quota)
				quota.Spec = *workspace.Spec.ResourceConstraints.Quota
				return nil
			}); err != nil {
				logger.Error(err, "Failed to reconcile quota", "cluster", c.Name)
			}
		}

		if workspace.Spec.ResourceConstraints != nil && workspace.Spec.ResourceConstraints.LimitRange != nil {
			limits := &corev1.LimitRange{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "workspace-limits",
					Namespace: nsName,
				},
			}
			if _, err := ctrl.CreateOrUpdate(ctx, edgeClient, limits, func() error {
				pkglabel.AddManagedBy(limits)
				limits.Spec = *workspace.Spec.ResourceConstraints.LimitRange
				return nil
			}); err != nil {
				logger.Error(err, "Failed to reconcile limitrange", "cluster", c.Name)
			}
		}

		appliedClusters = append(appliedClusters, c.Name)
	}

	workspace.Status.AppliedClusters = appliedClusters
	if err := r.Status().Update(ctx, &workspace); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *WorkspaceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&workspacev1alpha1.Workspace{}).
		Complete(r)
}
