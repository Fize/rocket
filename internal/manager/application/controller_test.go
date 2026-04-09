package application

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/fize/rocket/internal/manager/cluster"
	appsv1alpha1 "github.com/fize/rocket/pkg/apis/apps/v1alpha1"
	clusterv1alpha1 "github.com/fize/rocket/pkg/apis/storage/v1alpha1"
	"github.com/rancher/remotedialer"
	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func setupScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	_ = appsv1alpha1.AddToScheme(scheme)
	_ = clusterv1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	_ = policyv1.AddToScheme(scheme)
	return scheme
}

func toRaw(obj interface{}) runtime.RawExtension {
	b, _ := json.Marshal(obj)
	return runtime.RawExtension{Raw: b}
}

func TestApplicationReconciler_Reconcile_EdgeCases(t *testing.T) {
	scheme := setupScheme()
	ns := "rocket-system"

	tests := []struct {
		name          string
		app           *appsv1alpha1.Application
		clusters      []client.Object
		secrets       []client.Object
		mockClientErr bool
		verify        func(t *testing.T, cl client.Client, res ctrl.Result, err error)
	}{
		{
			name: "Connection Failure (Logs and continues)",
			app: &appsv1alpha1.Application{
				ObjectMeta: metav1.ObjectMeta{Name: "conn-fail", Namespace: "default", Finalizers: []string{ApplicationFinalizer}},
				Status: appsv1alpha1.ApplicationStatus{
					Placement: appsv1alpha1.PlacementStatus{
						Topology: []appsv1alpha1.ClusterTopology{{Name: "missing-cluster"}},
					},
				},
			},
			clusters: []client.Object{},
			verify: func(t *testing.T, cl client.Client, res ctrl.Result, err error) {
				assert.NoError(t, err)
			},
		},
		{
			name: "Resource Conflict (Mocked)",
			app: &appsv1alpha1.Application{
				ObjectMeta: metav1.ObjectMeta{Name: "conflict-app", Namespace: "default", Finalizers: []string{ApplicationFinalizer}},
				Spec: appsv1alpha1.ApplicationSpec{
					Workload: appsv1alpha1.WorkloadGVK{APIVersion: "v1", Kind: "ConfigMap"},
					Template: toRaw(&corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{Name: "test-cm"},
					}),
				},
				Status: appsv1alpha1.ApplicationStatus{
					Placement: appsv1alpha1.PlacementStatus{
						Topology: []appsv1alpha1.ClusterTopology{{Name: "hub-cluster"}},
					},
				},
			},
			clusters: []client.Object{
				&clusterv1alpha1.ManagedCluster{
					ObjectMeta: metav1.ObjectMeta{Name: "hub-cluster"},
					Spec: clusterv1alpha1.ManagedClusterSpec{
						ConnectionMode: clusterv1alpha1.ClusterConnectionModeHub,
						APIServer:      "https://localhost:6443",
						SecretRef:      &corev1.LocalObjectReference{Name: "hub-secret"},
					},
				},
			},
			secrets: []client.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: "hub-secret", Namespace: ns},
					Data:       map[string][]byte{"token": []byte("test")},
				},
			},
			mockClientErr: true,
			verify: func(t *testing.T, cl client.Client, res ctrl.Result, err error) {
				assert.Error(t, err)
				if err != nil {
					assert.Contains(t, err.Error(), "mock error")
				}
			},
		},
		{
			name: "Deletion Cleanup",
			app: &appsv1alpha1.Application{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "delete-app",
					Namespace:         "default",
					DeletionTimestamp: &metav1.Time{Time: metav1.Now().Time},
					Finalizers:        []string{ApplicationFinalizer},
				},
				Spec: appsv1alpha1.ApplicationSpec{
					Workload: appsv1alpha1.WorkloadGVK{APIVersion: "v1", Kind: "ConfigMap"},
				},
				Status: appsv1alpha1.ApplicationStatus{
					Placement: appsv1alpha1.PlacementStatus{
						Topology: []appsv1alpha1.ClusterTopology{{Name: "hub-cluster"}},
					},
					AppliedClusters: []string{"hub-cluster"},
				},
			},
			clusters: []client.Object{
				&clusterv1alpha1.ManagedCluster{
					ObjectMeta: metav1.ObjectMeta{Name: "hub-cluster"},
					Spec: clusterv1alpha1.ManagedClusterSpec{
						ConnectionMode: clusterv1alpha1.ClusterConnectionModeHub,
						APIServer:      "https://localhost:6443",
						SecretRef:      &corev1.LocalObjectReference{Name: "hub-secret"},
					},
				},
			},
			secrets: []client.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: "hub-secret", Namespace: ns},
					Data:       map[string][]byte{"token": []byte("test")},
				},
			},
			verify: func(t *testing.T, cl client.Client, res ctrl.Result, err error) {
				assert.NoError(t, err)
				// Verify object is deleted (fake client deletes it when finalizer is removed)
				updatedApp := &appsv1alpha1.Application{}
				err = cl.Get(context.Background(), types.NamespacedName{Name: "delete-app", Namespace: "default"}, updatedApp)
				assert.True(t, errors.IsNotFound(err))
			},
		},
		{
			name: "Placement Change (Cleanup)",
			app: &appsv1alpha1.Application{
				ObjectMeta: metav1.ObjectMeta{Name: "cleanup-app", Namespace: "default", Finalizers: []string{ApplicationFinalizer}},
				Spec: appsv1alpha1.ApplicationSpec{
					Workload: appsv1alpha1.WorkloadGVK{APIVersion: "v1", Kind: "ConfigMap"},
					Template: toRaw(&corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Name: "test-cm"}}),
				},
				Status: appsv1alpha1.ApplicationStatus{
					Placement: appsv1alpha1.PlacementStatus{
						Topology: []appsv1alpha1.ClusterTopology{{Name: "new-cluster"}},
					},
					AppliedClusters: []string{"old-cluster"},
				},
			},
			clusters: []client.Object{
				&clusterv1alpha1.ManagedCluster{
					ObjectMeta: metav1.ObjectMeta{Name: "new-cluster"},
					Spec:       clusterv1alpha1.ManagedClusterSpec{ConnectionMode: clusterv1alpha1.ClusterConnectionModeHub, APIServer: "https://localhost:6443", SecretRef: &corev1.LocalObjectReference{Name: "hub-secret"}},
				},
				&clusterv1alpha1.ManagedCluster{
					ObjectMeta: metav1.ObjectMeta{Name: "old-cluster"},
					Spec:       clusterv1alpha1.ManagedClusterSpec{ConnectionMode: clusterv1alpha1.ClusterConnectionModeHub, APIServer: "https://localhost:6443", SecretRef: &corev1.LocalObjectReference{Name: "hub-secret"}},
				},
			},
			secrets: []client.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: "hub-secret", Namespace: ns},
					Data:       map[string][]byte{"token": []byte("test")},
				},
			},
			verify: func(t *testing.T, cl client.Client, res ctrl.Result, err error) {
				assert.NoError(t, err)
				// Verify AppliedClusters is updated
				updatedApp := &appsv1alpha1.Application{}
				err = cl.Get(context.Background(), types.NamespacedName{Name: "cleanup-app", Namespace: "default"}, updatedApp)
				assert.NoError(t, err)
				assert.ElementsMatch(t, []string{"new-cluster"}, updatedApp.Status.AppliedClusters)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			objs := append([]client.Object{tt.app}, tt.clusters...)
			objs = append(objs, tt.secrets...)
			cl := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(objs...).
				WithStatusSubresource(&appsv1alpha1.Application{}).
				Build()

			tunnelServer := remotedialer.New(nil, nil)
			cm := cluster.NewClientManager(cl, tunnelServer, ns)

			if tt.mockClientErr {
				cm.ClientCreator = func(config *rest.Config, options client.Options) (client.Client, error) {
					return &errorMockClient{Client: fake.NewClientBuilder().WithScheme(scheme).Build()}, nil
				}
			} else {
				cm.ClientCreator = func(config *rest.Config, options client.Options) (client.Client, error) {
					return fake.NewClientBuilder().WithScheme(scheme).Build(), nil
				}
			}

			r := &ApplicationReconciler{
				Client:        cl,
				Scheme:        scheme,
				ClientManager: cm,
			}

			res, err := r.Reconcile(context.Background(), ctrl.Request{
				NamespacedName: types.NamespacedName{Name: tt.app.Name, Namespace: tt.app.Namespace},
			})

			tt.verify(t, cl, res, err)
		})
	}
}

type errorMockClient struct {
	client.Client
}

func (m *errorMockClient) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
	return fmt.Errorf("mock error")
}

func (m *errorMockClient) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
	return fmt.Errorf("mock error")
}

func TestApplicationReconciler_Reconcile(t *testing.T) {
	ctx := context.Background()
	scheme := setupScheme()

	// 1. Create a Hub-mode Cluster with SecretRef
	clusterName := "hub-cluster"
	secretName := "hub-secret"
	ns := "rocket-system"

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: ns,
		},
		Data: map[string][]byte{
			"token": []byte("test-token"),
		},
	}

	clusterObj := &clusterv1alpha1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: clusterName,
		},
		Spec: clusterv1alpha1.ManagedClusterSpec{
			ConnectionMode: clusterv1alpha1.ClusterConnectionModeHub,
			APIServer:      "https://localhost:6443",
			SecretRef: &corev1.LocalObjectReference{
				Name: secretName,
			},
		},
	}

	// 2. Create an Application scheduled to this cluster
	app := &appsv1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-app",
			Namespace:  "default",
			Finalizers: []string{ApplicationFinalizer},
		},
		Spec: appsv1alpha1.ApplicationSpec{
			Workload: appsv1alpha1.WorkloadGVK{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
			},
			Template: toRaw(&corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": "nginx"},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "main",
							Image: "nginx",
						},
					},
				},
			}),
		},
		Status: appsv1alpha1.ApplicationStatus{
			Placement: appsv1alpha1.PlacementStatus{
				Topology: []appsv1alpha1.ClusterTopology{
					{
						Name:     clusterName,
						Replicas: 3,
					},
				},
			},
		},
	}

	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(secret, clusterObj, app).
		WithStatusSubresource(&appsv1alpha1.Application{}).
		Build()

	// Mock target cluster client
	targetCl := fake.NewClientBuilder().WithScheme(scheme).Build()

	tunnelServer := remotedialer.New(nil, nil)
	clientManager := cluster.NewClientManager(cl, tunnelServer, ns)
	clientManager.ClientCreator = func(config *rest.Config, options client.Options) (client.Client, error) {
		return targetCl, nil
	}

	r := &ApplicationReconciler{
		Client:        cl,
		Scheme:        scheme,
		ClientManager: clientManager,
	}

	// 3. Reconcile
	_, err := r.Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      app.Name,
			Namespace: app.Namespace,
		},
	})
	assert.NoError(t, err)

	// 4. Verify
	deploy := &appsv1.Deployment{}
	err = targetCl.Get(ctx, types.NamespacedName{Name: app.Name, Namespace: app.Namespace}, deploy)
	assert.NoError(t, err)
}

func TestApplicationReconciler_JobWorkload(t *testing.T) {
	ctx := context.Background()
	scheme := setupScheme()

	clusterName := "hub-cluster"
	secretName := "hub-secret"
	ns := "rocket-system"

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: ns},
		Data:       map[string][]byte{"token": []byte("test-token")},
	}

	clusterObj := &clusterv1alpha1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{Name: clusterName},
		Spec: clusterv1alpha1.ManagedClusterSpec{
			ConnectionMode: clusterv1alpha1.ClusterConnectionModeHub,
			APIServer:      "https://localhost:6443",
			SecretRef:      &corev1.LocalObjectReference{Name: secretName},
		},
	}

	app := &appsv1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-job",
			Namespace:  "default",
			Finalizers: []string{ApplicationFinalizer},
		},
		Spec: appsv1alpha1.ApplicationSpec{
			Workload: appsv1alpha1.WorkloadGVK{
				APIVersion: "batch/v1",
				Kind:       "Job",
			},
			Template: toRaw(&corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					Containers: []corev1.Container{
						{
							Name:  "worker",
							Image: "busybox",
						},
					},
				},
			}),
		},
		Status: appsv1alpha1.ApplicationStatus{
			Placement: appsv1alpha1.PlacementStatus{
				Topology: []appsv1alpha1.ClusterTopology{
					{Name: clusterName},
				},
			},
		},
	}

	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(secret, clusterObj, app).
		WithStatusSubresource(&appsv1alpha1.Application{}).
		Build()
	targetCl := fake.NewClientBuilder().WithScheme(scheme).Build()

	tunnelServer := remotedialer.New(nil, nil)
	clientManager := cluster.NewClientManager(cl, tunnelServer, ns)
	clientManager.ClientCreator = func(config *rest.Config, options client.Options) (client.Client, error) {
		return targetCl, nil
	}

	r := &ApplicationReconciler{
		Client:        cl,
		Scheme:        scheme,
		ClientManager: clientManager,
	}

	_, err := r.Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{Name: app.Name, Namespace: app.Namespace},
	})
	assert.NoError(t, err)
}

func TestApplicationReconciler_WithOverrides(t *testing.T) {
	ctx := context.Background()
	scheme := setupScheme()

	clusterName := "prod-cluster"
	secretName := "prod-secret"
	ns := "rocket-system"

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: ns},
		Data:       map[string][]byte{"token": []byte("test-token")},
	}

	clusterObj := &clusterv1alpha1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:   clusterName,
			Labels: map[string]string{"env": "prod"},
		},
		Spec: clusterv1alpha1.ManagedClusterSpec{
			ConnectionMode: clusterv1alpha1.ClusterConnectionModeHub,
			APIServer:      "https://localhost:6443",
			SecretRef:      &corev1.LocalObjectReference{Name: secretName},
		},
	}

	app := &appsv1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "override-app",
			Namespace:  "default",
			Finalizers: []string{ApplicationFinalizer},
		},
		Spec: appsv1alpha1.ApplicationSpec{
			Workload: appsv1alpha1.WorkloadGVK{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
			},
			Template: toRaw(&corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "nginx", Image: "nginx:1.19"}},
				},
			}),
			Overrides: []appsv1alpha1.PolicyOverride{
				{
					ClusterSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"env": "prod"},
					},
					Image: "nginx:1.21",
				},
			},
		},
		Status: appsv1alpha1.ApplicationStatus{
			Placement: appsv1alpha1.PlacementStatus{
				Topology: []appsv1alpha1.ClusterTopology{{Name: clusterName}},
			},
		},
	}

	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(secret, clusterObj, app).
		WithStatusSubresource(&appsv1alpha1.Application{}).
		Build()
	targetCl := fake.NewClientBuilder().WithScheme(scheme).Build()

	tunnelServer := remotedialer.New(nil, nil)
	clientManager := cluster.NewClientManager(cl, tunnelServer, ns)
	clientManager.ClientCreator = func(config *rest.Config, options client.Options) (client.Client, error) {
		return targetCl, nil
	}

	r := &ApplicationReconciler{
		Client:        cl,
		Scheme:        scheme,
		ClientManager: clientManager,
	}

	_, err := r.Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{Name: app.Name, Namespace: app.Namespace},
	})
	assert.NoError(t, err)
}

func TestApplicationReconciler_PDB(t *testing.T) {
	scheme := setupScheme()
	clusterName := "cluster-1"

	minAvailable := intstr.FromInt(1)
	app := &appsv1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-app",
			Namespace:  "default",
			Finalizers: []string{ApplicationFinalizer},
		},
		Spec: appsv1alpha1.ApplicationSpec{
			Workload: appsv1alpha1.WorkloadGVK{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
			},
			Template: toRaw(&corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "test"}},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "main", Image: "nginx"}},
				},
			}),
			Resiliency: &appsv1alpha1.ResiliencyPolicy{
				MinAvailable: &minAvailable,
			},
		},
		Status: appsv1alpha1.ApplicationStatus{
			Placement: appsv1alpha1.PlacementStatus{
				Topology: []appsv1alpha1.ClusterTopology{{Name: clusterName}},
			},
		},
	}

	mCluster := &clusterv1alpha1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{Name: clusterName},
		Spec: clusterv1alpha1.ManagedClusterSpec{
			ConnectionMode: clusterv1alpha1.ClusterConnectionModeHub,
			APIServer:      "https://example.com",
			SecretRef:      &corev1.LocalObjectReference{Name: "cluster-secret"},
		},
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "cluster-secret", Namespace: "rocket-system"},
		Data: map[string][]byte{
			"kubeconfig": []byte("dummy"),
		},
	}

	hubClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&appsv1alpha1.Application{}).
		WithObjects(app, mCluster, secret).
		Build()

	edgeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	clientManager := cluster.NewClientManager(hubClient, nil, "rocket-system")
	clientManager.ClientCreator = func(config *rest.Config, options client.Options) (client.Client, error) {
		return edgeClient, nil
	}

	reconciler := &ApplicationReconciler{Client: hubClient, Scheme: scheme, ClientManager: clientManager}

	// Create context with logger to see what's happening
	ctx := context.Background()

	res, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "test-app", Namespace: "default"}})
	assert.NoError(t, err)
	assert.False(t, res.Requeue)

	// Verify PDB in edge cluster
	edgePDB := &policyv1.PodDisruptionBudget{}
	err = edgeClient.Get(context.Background(), types.NamespacedName{Name: "test-app", Namespace: "default"}, edgePDB)
	assert.NoError(t, err)

	// Verify Workload in edge cluster
	edgeDep := &unstructured.Unstructured{}
	edgeDep.SetAPIVersion("apps/v1")
	edgeDep.SetKind("Deployment")
	err = edgeClient.Get(context.Background(), types.NamespacedName{Name: "test-app", Namespace: "default"}, edgeDep)
	assert.NoError(t, err)
}

func TestApplicationReconciler_CronJobSpecs(t *testing.T) {
	scheme := setupScheme()

	// Create a Reconciler with a fake client
	r := &ApplicationReconciler{
		Client: fake.NewClientBuilder().WithScheme(scheme).Build(),
		Scheme: scheme,
	}

	// 1. Define the CronJob Application
	completions := int32(5)
	parallelism := int32(2)
	ttl := int32(100)
	suspend := true

	app := &appsv1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cronjob",
			Namespace: "default",
		},
		Spec: appsv1alpha1.ApplicationSpec{
			Workload: appsv1alpha1.WorkloadGVK{APIVersion: "batch/v1", Kind: "CronJob"},
			Schedule: "*/5 * * * *",
			Suspend:  &suspend,
			JobAttributes: &appsv1alpha1.JobAttributes{
				Completions:             &completions,
				Parallelism:             &parallelism,
				TTLSecondsAfterFinished: &ttl,
			},
			Template: runtime.RawExtension{Raw: []byte(`{"spec":{"template":{"spec":{"containers":[{"name":"c1","image":"nginx"}]}}}}`)},
		},
	}

	// 2. Prepare the empty Unstructured object that the controller would "Get"
	u := &unstructured.Unstructured{}
	u.SetAPIVersion("batch/v1")
	u.SetKind("CronJob")
	u.SetName("test-cronjob")

	// 3. Invoke configureWorkload directly (white-box testing)
	// We use the internal helper to verify the logic without mocking the entire Reconcile loop
	err := r.configureWorkload(u, app.Spec, 0)
	assert.NoError(t, err)

	// 4. Verify Schedule
	sched, found, _ := unstructured.NestedString(u.Object, "spec", "schedule")
	assert.True(t, found)
	assert.Equal(t, "*/5 * * * *", sched)

	// 5. Verify Suspend
	susp, found, _ := unstructured.NestedBool(u.Object, "spec", "suspend")
	assert.True(t, found)
	assert.True(t, susp)

	// 6. Verify JobAttributes (nested deep in spec.jobTemplate.spec)
	jobSpecPath := []string{"spec", "jobTemplate", "spec"}

	comp, found, _ := unstructured.NestedInt64(u.Object, append(jobSpecPath, "completions")...)
	assert.True(t, found, "Completions not found")
	assert.Equal(t, int64(5), comp)

	para, found, _ := unstructured.NestedInt64(u.Object, append(jobSpecPath, "parallelism")...)
	assert.True(t, found, "Parallelism not found")
	assert.Equal(t, int64(2), para)

	ttlVal, found, _ := unstructured.NestedInt64(u.Object, append(jobSpecPath, "ttlSecondsAfterFinished")...)
	assert.True(t, found, "TTL not found")
	assert.Equal(t, int64(100), ttlVal)
}

func TestApplicationReconciler_applyCronJobAttributes(t *testing.T) {
	tests := []struct {
		name     string
		attrs    *appsv1alpha1.JobAttributes
		validate func(t *testing.T, u *unstructured.Unstructured)
	}{
		{
			name: "SuccessfulJobsHistoryLimit only",
			attrs: &appsv1alpha1.JobAttributes{
				SuccessfulJobsHistoryLimit: func(i int32) *int32 { return &i }(5),
			},
			validate: func(t *testing.T, u *unstructured.Unstructured) {
				val, found, _ := unstructured.NestedInt64(u.Object, "spec", "successfulJobsHistoryLimit")
				assert.True(t, found)
				assert.Equal(t, int64(5), val)
			},
		},
		{
			name: "FailedJobsHistoryLimit only",
			attrs: &appsv1alpha1.JobAttributes{
				FailedJobsHistoryLimit: func(i int32) *int32 { return &i }(3),
			},
			validate: func(t *testing.T, u *unstructured.Unstructured) {
				val, found, _ := unstructured.NestedInt64(u.Object, "spec", "failedJobsHistoryLimit")
				assert.True(t, found)
				assert.Equal(t, int64(3), val)
			},
		},
		{
			name: "Both limits",
			attrs: &appsv1alpha1.JobAttributes{
				SuccessfulJobsHistoryLimit: func(i int32) *int32 { return &i }(10),
				FailedJobsHistoryLimit:     func(i int32) *int32 { return &i }(5),
			},
			validate: func(t *testing.T, u *unstructured.Unstructured) {
				successVal, found, _ := unstructured.NestedInt64(u.Object, "spec", "successfulJobsHistoryLimit")
				assert.True(t, found)
				assert.Equal(t, int64(10), successVal)

				failVal, found, _ := unstructured.NestedInt64(u.Object, "spec", "failedJobsHistoryLimit")
				assert.True(t, found)
				assert.Equal(t, int64(5), failVal)
			},
		},
		{
			name:  "Nil attrs",
			attrs: nil,
			validate: func(t *testing.T, u *unstructured.Unstructured) {
				_, found, _ := unstructured.NestedInt64(u.Object, "spec", "successfulJobsHistoryLimit")
				assert.False(t, found)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u := &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "batch/v1",
					"kind":       "CronJob",
					"spec":       map[string]interface{}{},
				},
			}

			var err error
			if tt.attrs != nil {
				err = applyCronJobAttributes(u, tt.attrs)
			}
			assert.NoError(t, err)
			tt.validate(t, u)
		})
	}
}

func TestApplicationReconciler_applyJobAttributes(t *testing.T) {
	tests := []struct {
		name     string
		attrs    *appsv1alpha1.JobAttributes
		validate func(t *testing.T, u *unstructured.Unstructured)
	}{
		{
			name: "Completions only",
			attrs: &appsv1alpha1.JobAttributes{
				Completions: func(i int32) *int32 { return &i }(10),
			},
			validate: func(t *testing.T, u *unstructured.Unstructured) {
				val, found, _ := unstructured.NestedInt64(u.Object, "spec", "completions")
				assert.True(t, found)
				assert.Equal(t, int64(10), val)
			},
		},
		{
			name: "Parallelism only",
			attrs: &appsv1alpha1.JobAttributes{
				Parallelism: func(i int32) *int32 { return &i }(5),
			},
			validate: func(t *testing.T, u *unstructured.Unstructured) {
				val, found, _ := unstructured.NestedInt64(u.Object, "spec", "parallelism")
				assert.True(t, found)
				assert.Equal(t, int64(5), val)
			},
		},
		{
			name: "BackoffLimit only",
			attrs: &appsv1alpha1.JobAttributes{
				BackoffLimit: func(i int32) *int32 { return &i }(3),
			},
			validate: func(t *testing.T, u *unstructured.Unstructured) {
				val, found, _ := unstructured.NestedInt64(u.Object, "spec", "backoffLimit")
				assert.True(t, found)
				assert.Equal(t, int64(3), val)
			},
		},
		{
			name: "All attributes",
			attrs: &appsv1alpha1.JobAttributes{
				Completions:             func(i int32) *int32 { return &i }(10),
				Parallelism:             func(i int32) *int32 { return &i }(5),
				BackoffLimit:            func(i int32) *int32 { return &i }(3),
				TTLSecondsAfterFinished: func(i int32) *int32 { return &i }(100),
			},
			validate: func(t *testing.T, u *unstructured.Unstructured) {
				completions, _, _ := unstructured.NestedInt64(u.Object, "spec", "completions")
				assert.Equal(t, int64(10), completions)

				parallelism, _, _ := unstructured.NestedInt64(u.Object, "spec", "parallelism")
				assert.Equal(t, int64(5), parallelism)

				backoff, _, _ := unstructured.NestedInt64(u.Object, "spec", "backoffLimit")
				assert.Equal(t, int64(3), backoff)

				ttl, _, _ := unstructured.NestedInt64(u.Object, "spec", "ttlSecondsAfterFinished")
				assert.Equal(t, int64(100), ttl)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u := &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "batch/v1",
					"kind":       "Job",
					"spec":       map[string]interface{}{},
				},
			}

			err := applyJobAttributes(u, tt.attrs, []string{"spec"}...)
			assert.NoError(t, err)
			tt.validate(t, u)
		})
	}
}

func TestApplicationReconciler_StatefulSetGlobalOrdinals(t *testing.T) {
	scheme := setupScheme()

	// Mock client capable of SSA (Server-Side Apply) simulation not strictly needed here
	// as we will intercept the patch inside a mock or just test configureWorkload.
	// Testing configureWorkload is cleaner for logic verification.
	r := &ApplicationReconciler{
		Client: fake.NewClientBuilder().WithScheme(scheme).Build(),
		Scheme: scheme,
	}

	app := &appsv1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{Name: "global-sts", Namespace: "default"},
		Spec: appsv1alpha1.ApplicationSpec{
			Workload: appsv1alpha1.WorkloadGVK{APIVersion: "apps/v1", Kind: "StatefulSet"},
			Replicas: func(i int32) *int32 { return &i }(10),
			Template: runtime.RawExtension{Raw: []byte(`{"spec":{"template":{"spec":{"containers":[{"image":"nginx"}]}}}}`)},
		},
	}

	// Case 1: First Cluster (Ordinal Start = 0)
	u1 := &unstructured.Unstructured{}
	u1.SetKind("StatefulSet")
	err := r.configureWorkload(u1, app.Spec, 0)
	assert.NoError(t, err)

	// Expect NO 'ordinals' field or 'start' == 0
	val, found, _ := unstructured.NestedInt64(u1.Object, "spec", "ordinals", "start")
	if found {
		assert.Equal(t, int64(0), val)
	}

	// Case 2: Second Cluster (Ordinal Start = 5)
	u2 := &unstructured.Unstructured{}
	u2.SetKind("StatefulSet")
	err = r.configureWorkload(u2, app.Spec, 5)
	assert.NoError(t, err)

	val, found, err = unstructured.NestedInt64(u2.Object, "spec", "ordinals", "start")
	assert.NoError(t, err)
	assert.True(t, found, "ordinals.start should be set for non-zero offset")
	assert.Equal(t, int64(5), val)
}
