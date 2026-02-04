package cluster

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	clusterv1alpha1 "github.com/hex-techs/rocket/pkg/apis/storage/v1alpha1"
)

func newClusterScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	_ = clusterv1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	return scheme
}

func TestClusterReconciler_PendingToReady(t *testing.T) {
	ctx := context.Background()
	scheme := newClusterScheme(t)

	cluster := &clusterv1alpha1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test-cluster"},
		Spec: clusterv1alpha1.ManagedClusterSpec{
			SecretRef: &corev1.LocalObjectReference{Name: "test-secret"},
		},
		Status: clusterv1alpha1.ManagedClusterStatus{State: clusterv1alpha1.ClusterPending},
	}

	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cluster).WithStatusSubresource(cluster).Build()
	r := &ClusterReconciler{Client: cl, Scheme: scheme}

	_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: cluster.Name}})
	assert.NoError(t, err)

	var got clusterv1alpha1.ManagedCluster
	err = cl.Get(ctx, types.NamespacedName{Name: cluster.Name}, &got)
	assert.NoError(t, err)
	assert.Equal(t, clusterv1alpha1.ClusterReady, got.Status.State)
	assert.NotEmpty(t, got.Status.ID)
}

func TestClusterReconciler_EdgeMode_Heartbeat(t *testing.T) {
	ctx := context.Background()
	scheme := newClusterScheme(t)

	now := metav1.Now()
	cluster := &clusterv1alpha1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "edge-cluster"},
		Spec: clusterv1alpha1.ManagedClusterSpec{
			ConnectionMode: clusterv1alpha1.ClusterConnectionModeEdge,
		},
		Status: clusterv1alpha1.ManagedClusterStatus{
			State:             clusterv1alpha1.ClusterReady,
			LastKeepAliveTime: &now,
		},
	}

	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cluster).WithStatusSubresource(cluster).Build()
	r := &ClusterReconciler{
		Client:           cl,
		Scheme:           scheme,
		HeartbeatTimeout: 1 * time.Minute,
	}

	// 1. Recent heartbeat -> should stay Ready
	_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: cluster.Name}})
	assert.NoError(t, err)

	var got clusterv1alpha1.ManagedCluster
	cl.Get(ctx, types.NamespacedName{Name: cluster.Name}, &got)
	assert.Equal(t, clusterv1alpha1.ClusterReady, got.Status.State)

	// 2. Old heartbeat -> should become Offline
	oldTime := metav1.NewTime(time.Now().Add(-5 * time.Minute))
	got.Status.LastKeepAliveTime = &oldTime
	cl.Status().Update(ctx, &got)

	_, err = r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: cluster.Name}})
	assert.NoError(t, err)

	cl.Get(ctx, types.NamespacedName{Name: cluster.Name}, &got)
	assert.Equal(t, clusterv1alpha1.ClusterOffline, got.Status.State)
}

func TestClusterReconciler_EdgeMode_Credentials(t *testing.T) {
	scheme := newClusterScheme(t)

	clusterName := "edge-cluster"
	cluster := &clusterv1alpha1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      clusterName,
			Namespace: "default",
			Annotations: map[string]string{
				AnnotationCredentialsToken: "test-token",
				AnnotationAPIServerURL:     "https://k8s.example.com",
			},
		},
		Spec: clusterv1alpha1.ManagedClusterSpec{
			ConnectionMode: clusterv1alpha1.ClusterConnectionModeEdge,
		},
	}

	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(cluster).
		WithStatusSubresource(cluster).
		Build()

	r := &ClusterReconciler{
		Client:           cl,
		Scheme:           scheme,
		HeartbeatTimeout: 1 * time.Minute,
		Namespace:        "rocket-system",
	}

	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      clusterName,
			Namespace: "default",
		},
	}

	// 1. First reconcile: Handle credentials
	_, err := r.Reconcile(context.Background(), req)
	assert.NoError(t, err)

	// Check if secret created
	secret := &corev1.Secret{}
	err = cl.Get(context.Background(), types.NamespacedName{Name: "cluster-creds-" + clusterName, Namespace: "rocket-system"}, secret)
	assert.NoError(t, err)
	assert.Equal(t, "test-token", string(secret.Data["token"]))

	// Check if cluster updated
	updatedCluster := &clusterv1alpha1.ManagedCluster{}
	err = cl.Get(context.Background(), req.NamespacedName, updatedCluster)
	assert.NoError(t, err)
	assert.NotNil(t, updatedCluster.Spec.SecretRef)
	assert.Equal(t, "cluster-creds-"+clusterName, updatedCluster.Spec.SecretRef.Name)
	assert.Equal(t, clusterv1alpha1.ClusterReady, updatedCluster.Status.State)
	assert.Empty(t, updatedCluster.Annotations[AnnotationCredentialsToken])
}

func TestClusterReconciler_HubMode_Rejected(t *testing.T) {
	ctx := context.Background()
	scheme := newClusterScheme(t)

	cluster := &clusterv1alpha1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "rejected-cluster"},
		Spec: clusterv1alpha1.ManagedClusterSpec{
			ConnectionMode: clusterv1alpha1.ClusterConnectionModeHub,
			// Missing SecretRef
		},
	}

	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cluster).WithStatusSubresource(cluster).Build()
	r := &ClusterReconciler{Client: cl, Scheme: scheme}

	_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: cluster.Name}})
	assert.NoError(t, err)

	var got clusterv1alpha1.ManagedCluster
	cl.Get(ctx, types.NamespacedName{Name: cluster.Name}, &got)
	assert.Equal(t, clusterv1alpha1.ClusterRejected, got.Status.State)
}

func TestClusterReconciler_DuplicateSecret(t *testing.T) {
	ctx := context.Background()
	scheme := newClusterScheme(t)

	secretName := "shared-secret"

	// Existing cluster using the secret
	existingCluster := &clusterv1alpha1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "existing-cluster"},
		Spec: clusterv1alpha1.ManagedClusterSpec{
			SecretRef: &corev1.LocalObjectReference{Name: secretName},
		},
		Status: clusterv1alpha1.ManagedClusterStatus{
			ID:    "existing-id",
			State: clusterv1alpha1.ClusterReady,
		},
	}

	// New cluster trying to use the same secret
	newCluster := &clusterv1alpha1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "new-cluster"},
		Spec: clusterv1alpha1.ManagedClusterSpec{
			SecretRef: &corev1.LocalObjectReference{Name: secretName},
		},
		Status: clusterv1alpha1.ManagedClusterStatus{State: clusterv1alpha1.ClusterPending},
	}

	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(existingCluster, newCluster).
		WithStatusSubresource(existingCluster, newCluster).
		Build()

	r := &ClusterReconciler{Client: cl, Scheme: scheme}

	// Reconcile the new cluster
	_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: newCluster.Name}})
	assert.NoError(t, err)

	// The new cluster should be deleted, and the existing one should be updated (if needed)
	var gotNew clusterv1alpha1.ManagedCluster
	err = cl.Get(ctx, types.NamespacedName{Name: newCluster.Name}, &gotNew)
	assert.True(t, errors.IsNotFound(err), "new cluster should be deleted")

	var gotExisting clusterv1alpha1.ManagedCluster
	err = cl.Get(ctx, types.NamespacedName{Name: existingCluster.Name}, &gotExisting)
	assert.NoError(t, err)
	assert.Equal(t, clusterv1alpha1.ClusterReady, gotExisting.Status.State)
}
