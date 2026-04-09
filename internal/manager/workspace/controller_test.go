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
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/fize/rocket/internal/manager/cluster"
	storagev1alpha1 "github.com/fize/rocket/pkg/apis/storage/v1alpha1"
	workspacev1alpha1 "github.com/fize/rocket/pkg/apis/workspace/v1alpha1"
)

func setupScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	_ = workspacev1alpha1.AddToScheme(scheme)
	_ = storagev1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	return scheme
}

func TestWorkspaceReconciler_Reconcile(t *testing.T) {
	scheme := setupScheme()

	wsName := "test-workspace"
	clusterName := "cluster-1"

	workspace := &workspacev1alpha1.Workspace{
		ObjectMeta: metav1.ObjectMeta{
			Name: wsName,
			// Namespace: ns, // Workspace is cluster-scoped
		},
		Spec: workspacev1alpha1.WorkspaceSpec{
			ClusterSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"env": "prod"},
			},
			ResourceConstraints: &workspacev1alpha1.WorkspaceConstraints{
				Quota: &corev1.ResourceQuotaSpec{
					Hard: corev1.ResourceList{
						corev1.ResourceCPU: resource.MustParse("2"),
					},
				},
			},
		},
	}

	mCluster := &storagev1alpha1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:   clusterName,
			Labels: map[string]string{"env": "prod"},
		},
		Spec: storagev1alpha1.ManagedClusterSpec{
			ConnectionMode: storagev1alpha1.ClusterConnectionModeHub,
			APIServer:      "https://localhost:6443",
			SecretRef: &corev1.LocalObjectReference{
				Name: "cluster-1-secret",
			},
		},
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cluster-1-secret",
			Namespace: "rocket-system",
		},
		Data: map[string][]byte{
			"token": []byte("dummy-token"),
		},
	}

	// Fake Hub Client
	hubClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&workspacev1alpha1.Workspace{}).
		WithObjects(workspace, mCluster, secret).
		Build()

	// Fake Edge Client
	edgeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	// Mock ClientManager
	clientManager := cluster.NewClientManager(hubClient, nil, "rocket-system")
	clientManager.ClientCreator = func(config *rest.Config, options client.Options) (client.Client, error) {
		return edgeClient, nil
	}

	reconciler := &WorkspaceReconciler{
		Client:        hubClient,
		Scheme:        scheme,
		ClientManager: clientManager,
	}

	// Run Reconcile
	_, err := reconciler.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: wsName},
	})

	assert.NoError(t, err)

	// Verify Namespace in Hub Cluster
	hubNS := &corev1.Namespace{}
	err = hubClient.Get(context.Background(), types.NamespacedName{Name: wsName}, hubNS)
	assert.NoError(t, err)

	// Verify Namespace in Edge Cluster
	edgeNS := &corev1.Namespace{}
	err = edgeClient.Get(context.Background(), types.NamespacedName{Name: wsName}, edgeNS)
	assert.NoError(t, err)

	// Verify ResourceQuota in Edge Cluster
	quota := &corev1.ResourceQuota{}
	err = edgeClient.Get(context.Background(), types.NamespacedName{Namespace: wsName, Name: "workspace-quota"}, quota)
	assert.NoError(t, err)
	assert.Equal(t, resource.MustParse("2"), quota.Spec.Hard[corev1.ResourceCPU])

	// Verify Status in Hub
	updatedWS := &workspacev1alpha1.Workspace{}
	err = hubClient.Get(context.Background(), types.NamespacedName{Name: wsName}, updatedWS)
	assert.NoError(t, err)
	assert.Contains(t, updatedWS.Status.AppliedClusters, clusterName)
}
