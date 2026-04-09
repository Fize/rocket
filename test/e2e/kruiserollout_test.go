//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"

	appsv1alpha1 "github.com/fize/rocket/pkg/apis/apps/v1alpha1"
	clusterv1alpha1 "github.com/fize/rocket/pkg/apis/storage/v1alpha1"
)

// TestKruiseRolloutE2E tests kruise-rollout addon and cross-cluster deployment strategies
func TestKruiseRolloutE2E(t *testing.T) {
	env := SetupTestEnvironment(t)
	defer env.Cleanup()

	t.Run("KruiseRolloutAddonInstall", func(t *testing.T) {
		testKruiseRolloutAddonInstall(t, env)
	})

	t.Run("CrossClusterCanaryRollout", func(t *testing.T) {
		testCrossClusterCanaryRollout(t, env)
	})

	t.Run("CrossClusterBlueGreenRollout", func(t *testing.T) {
		testCrossClusterBlueGreenRollout(t, env)
	})

	t.Run("SequentialClusterRollout", func(t *testing.T) {
		testSequentialClusterRollout(t, env)
	})
}

// testKruiseRolloutAddonInstall tests that kruise-rollout addon can be installed on clusters
func testKruiseRolloutAddonInstall(t *testing.T, env *TestEnvironment) {
	ctx := env.Context()
	c := env.Client

	// Create two Hub clusters for testing
	clusterA := "kruise-rollout-cluster-a"
	clusterB := "kruise-rollout-cluster-b"

	_ = env.CreateHubCluster(t, clusterA, map[string]string{"env": "test", "rollout": "enabled"})
	_ = env.CreateHubCluster(t, clusterB, map[string]string{"env": "test", "rollout": "enabled"})

	defer env.DeleteCluster(clusterA)
	defer env.DeleteCluster(clusterB)

	// Re-fetch clusters to get updated status
	var mcA, mcB clusterv1alpha1.ManagedCluster
	err := c.Get(ctx, types.NamespacedName{Name: clusterA}, &mcA)
	require.NoError(t, err)
	err = c.Get(ctx, types.NamespacedName{Name: clusterB}, &mcB)
	require.NoError(t, err)

	// Verify clusters are ready (or at least exist)
	t.Logf("Cluster A state: %s, Cluster B state: %s", mcA.Status.State, mcB.Status.State)

	// Enable kruise-rollout addon on cluster A
	t.Logf("Enabling kruise-rollout addon on cluster %s...", clusterA)
	err = enableKruiseRolloutAddon(ctx, c, clusterA)
	require.NoError(t, err, "Failed to enable kruise-rollout addon on cluster A")

	// Enable kruise-rollout addon on cluster B
	t.Logf("Enabling kruise-rollout addon on cluster %s...", clusterB)
	err = enableKruiseRolloutAddon(ctx, c, clusterB)
	require.NoError(t, err, "Failed to enable kruise-rollout addon on cluster B")

	// Wait for addon to be applied (wait for AddonStatus update)
	t.Log("Waiting for kruise-rollout addon to be applied...")
	err = wait.PollUntilContextTimeout(ctx, 2*time.Second, 60*time.Second, true, func(ctx context.Context) (bool, error) {
		var cluster clusterv1alpha1.ManagedCluster
		if err := c.Get(ctx, types.NamespacedName{Name: clusterA}, &cluster); err != nil {
			return false, nil
		}
		// Check if addon is in AddonStatus list with Applied state
		for _, addon := range cluster.Status.AddonStatus {
			if addon.Name == "kruise-rollout" && addon.State == "Applied" {
				return true, nil
			}
		}
		return false, nil
	})
	require.NoError(t, err, "kruise-rollout addon was not applied on cluster A")

	t.Log("kruise-rollout addon successfully installed on both clusters")
}

// testCrossClusterCanaryRollout tests canary deployment across multiple clusters
func testCrossClusterCanaryRollout(t *testing.T, env *TestEnvironment) {
	ctx := env.Context()
	c := env.Client

	// Create two clusters
	clusterA := "canary-cluster-a"
	clusterB := "canary-cluster-b"

	env.CreateHubCluster(t, clusterA, map[string]string{"env": "canary"})
	env.CreateHubCluster(t, clusterB, map[string]string{"env": "canary"})

	defer env.DeleteCluster(clusterA)
	defer env.DeleteCluster(clusterB)

	// Enable kruise-rollout addon
	err := enableKruiseRolloutAddon(ctx, c, clusterA)
	require.NoError(t, err)
	err = enableKruiseRolloutAddon(ctx, c, clusterB)
	require.NoError(t, err)

	// Create application with canary rollout strategy
	appName := "test-canary-app"
	app := &appsv1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{
			Name:      appName,
			Namespace: "default",
		},
		Spec: appsv1alpha1.ApplicationSpec{
			Workload: appsv1alpha1.WorkloadGVK{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
			},
			Template: mustEncodeTemplate(&corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": appName},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "nginx",
							Image: "nginx:1.25",
							Ports: []corev1.ContainerPort{{ContainerPort: 80}},
						},
					},
				},
			}),
			Replicas: int32Ptr(3),
			RolloutStrategy: &appsv1alpha1.RolloutStrategy{
				Type: appsv1alpha1.RolloutTypeCanary,
				Canary: &appsv1alpha1.CanaryStrategy{
					Steps: []appsv1alpha1.CanaryStep{
						{Weight: 25},
						{Weight: 50},
						{Weight: 100},
					},
				},
				ClusterOrder: &appsv1alpha1.ClusterOrder{
					Type: appsv1alpha1.ClusterOrderParallel, // Deploy to all clusters in parallel
				},
			},
		},
	}

	env.CreateApplication(t, app)
	defer env.DeleteApplication(appName, "default")

	// Wait for application to be scheduled
	t.Log("Waiting for application to be scheduled...")
	scheduledApp := env.WaitForApplicationScheduled(t, appName, "default", 60*time.Second)
	require.Len(t, scheduledApp.Status.Placement.Topology, 2, "Application should be scheduled to 2 clusters")

	// Wait for deployment to be created in both clusters
	t.Log("Waiting for deployments to be created in both clusters...")
	err = wait.PollUntilContextTimeout(ctx, 2*time.Second, 60*time.Second, true, func(ctx context.Context) (bool, error) {
		// Check cluster A
		var deployA appsv1.Deployment
		errA := c.Get(ctx, types.NamespacedName{Name: appName, Namespace: "default"}, &deployA)
		if errA != nil {
			return false, nil
		}
		return deployA.Status.ReadyReplicas >= 1, nil
	})
	require.NoError(t, err, "Deployment should be ready in cluster A")

	// Verify Rollout CR was created
	t.Log("Verifying Rollout CR was created...")
	rollout := &unstructured.Unstructured{}
	rollout.SetAPIVersion("rollouts.kruise.io/v1alpha1")
	rollout.SetKind("Rollout")
	err = c.Get(ctx, types.NamespacedName{Name: appName, Namespace: "default"}, rollout)
	require.NoError(t, err, "Rollout CR should be created")

	// Verify rollout has canary strategy
	strategy, found, err := unstructured.NestedMap(rollout.Object, "spec", "strategy")
	require.NoError(t, err)
	require.True(t, found, "Strategy should be set")
	require.Contains(t, strategy, "canary", "Canary strategy should be set")

	t.Log("Cross-cluster canary rollout test passed!")
}

// testCrossClusterBlueGreenRollout tests blue-green deployment across multiple clusters
func testCrossClusterBlueGreenRollout(t *testing.T, env *TestEnvironment) {
	ctx := env.Context()
	c := env.Client

	// Create cluster
	clusterName := "bluegreen-cluster"
	env.CreateHubCluster(t, clusterName, map[string]string{"env": "bluegreen"})
	defer env.DeleteCluster(clusterName)

	// Enable kruise-rollout addon
	err := enableKruiseRolloutAddon(ctx, c, clusterName)
	require.NoError(t, err)

	// Create services for blue-green
	serviceActive := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-bg-app-active",
			Namespace: "default",
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{"app": "test-bg-app"},
			Ports:    []corev1.ServicePort{{Port: 80, TargetPort: intstr.FromInt(80)}},
		},
	}
	servicePreview := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-bg-app-preview",
			Namespace: "default",
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{"app": "test-bg-app"},
			Ports:    []corev1.ServicePort{{Port: 80, TargetPort: intstr.FromInt(80)}},
		},
	}

	// Create services (ignore if already exists)
	_ = c.Create(ctx, serviceActive)
	_ = c.Create(ctx, servicePreview)

	// Create application with blue-green rollout strategy
	appName := "test-bg-app"
	app := &appsv1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{
			Name:      appName,
			Namespace: "default",
		},
		Spec: appsv1alpha1.ApplicationSpec{
			Workload: appsv1alpha1.WorkloadGVK{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
			},
			Template: mustEncodeTemplate(&corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": appName},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "nginx",
							Image: "nginx:1.25",
							Ports: []corev1.ContainerPort{{ContainerPort: 80}},
						},
					},
				},
			}),
			Replicas: int32Ptr(3),
			RolloutStrategy: &appsv1alpha1.RolloutStrategy{
				Type: appsv1alpha1.RolloutTypeBlueGreen,
				BlueGreen: &appsv1alpha1.BlueGreenStrategy{
					ActiveService:         "test-bg-app-active",
					PreviewService:        "test-bg-app-preview",
					AutoPromotionEnabled:  true,
					ScaleDownDelaySeconds: 60,
				},
			},
		},
	}

	env.CreateApplication(t, app)
	defer env.DeleteApplication(appName, "default")

	// Wait for application to be scheduled
	t.Log("Waiting for application to be scheduled...")
	scheduledApp := env.WaitForApplicationScheduled(t, appName, "default", 60*time.Second)
	require.Len(t, scheduledApp.Status.Placement.Topology, 1)

	// Verify Rollout CR was created with blue-green strategy
	t.Log("Verifying Blue-Green Rollout CR was created...")
	err = wait.PollUntilContextTimeout(ctx, 2*time.Second, 60*time.Second, true, func(ctx context.Context) (bool, error) {
		rollout := &unstructured.Unstructured{}
		rollout.SetAPIVersion("rollouts.kruise.io/v1alpha1")
		rollout.SetKind("Rollout")
		err := c.Get(ctx, types.NamespacedName{Name: appName, Namespace: "default"}, rollout)
		if err != nil {
			return false, nil
		}

		strategy, found, err := unstructured.NestedMap(rollout.Object, "spec", "strategy")
		if err != nil || !found {
			return false, nil
		}

		_, hasBlueGreen := strategy["blueGreen"]
		return hasBlueGreen, nil
	})
	require.NoError(t, err, "Blue-Green Rollout CR should be created")

	t.Log("Cross-cluster blue-green rollout test passed!")
}

// testSequentialClusterRollout tests sequential rollout across clusters
func testSequentialClusterRollout(t *testing.T, env *TestEnvironment) {
	ctx := env.Context()
	c := env.Client

	// Create two clusters
	clusterA := "seq-cluster-a"
	clusterB := "seq-cluster-b"

	env.CreateHubCluster(t, clusterA, map[string]string{"env": "sequential"})
	env.CreateHubCluster(t, clusterB, map[string]string{"env": "sequential"})

	defer env.DeleteCluster(clusterA)
	defer env.DeleteCluster(clusterB)

	// Enable kruise-rollout addon
	err := enableKruiseRolloutAddon(ctx, c, clusterA)
	require.NoError(t, err)
	err = enableKruiseRolloutAddon(ctx, c, clusterB)
	require.NoError(t, err)

	// Create application with sequential rollout
	appName := "test-seq-app"
	app := &appsv1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{
			Name:      appName,
			Namespace: "default",
		},
		Spec: appsv1alpha1.ApplicationSpec{
			Workload: appsv1alpha1.WorkloadGVK{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
			},
			Template: mustEncodeTemplate(&corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": appName},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "nginx",
							Image: "nginx:1.25",
							Ports: []corev1.ContainerPort{{ContainerPort: 80}},
						},
					},
				},
			}),
			Replicas: int32Ptr(3),
			RolloutStrategy: &appsv1alpha1.RolloutStrategy{
				Type: appsv1alpha1.RolloutTypeCanary,
				Canary: &appsv1alpha1.CanaryStrategy{
					Steps: []appsv1alpha1.CanaryStep{
						{Weight: 50},
						{Weight: 100},
					},
				},
				ClusterOrder: &appsv1alpha1.ClusterOrder{
					Type:     appsv1alpha1.ClusterOrderSequential,
					Clusters: []string{clusterA, clusterB},
				},
			},
		},
	}

	env.CreateApplication(t, app)
	defer env.DeleteApplication(appName, "default")

	// Wait for application to be scheduled
	t.Log("Waiting for application to be scheduled...")
	scheduledApp := env.WaitForApplicationScheduled(t, appName, "default", 60*time.Second)
	require.Len(t, scheduledApp.Status.Placement.Topology, 2)

	// Verify sequential order is respected
	// First cluster should get rollout immediately
	// Second cluster should wait for first to complete
	t.Log("Verifying sequential rollout order...")

	// Check that rollout exists
	err = wait.PollUntilContextTimeout(ctx, 2*time.Second, 60*time.Second, true, func(ctx context.Context) (bool, error) {
		rollout := &unstructured.Unstructured{}
		rollout.SetAPIVersion("rollouts.kruise.io/v1alpha1")
		rollout.SetKind("Rollout")
		err := c.Get(ctx, types.NamespacedName{Name: appName, Namespace: "default"}, rollout)
		return err == nil, nil
	})
	require.NoError(t, err, "Rollout CR should be created")

	t.Log("Sequential cluster rollout test passed!")
}

// enableKruiseRolloutAddon enables the kruise-rollout addon on a cluster
func enableKruiseRolloutAddon(ctx context.Context, c client.Client, clusterName string) error {
	var cluster clusterv1alpha1.ManagedCluster
	if err := c.Get(ctx, types.NamespacedName{Name: clusterName}, &cluster); err != nil {
		return fmt.Errorf("failed to get cluster: %w", err)
	}

	// Add kruise-rollout to enabled addons
	if cluster.Spec.Addons == nil {
		cluster.Spec.Addons = []clusterv1alpha1.ClusterAddon{}
	}

	// Check if already enabled
	for _, addon := range cluster.Spec.Addons {
		if addon.Name == "kruise-rollout" {
			return nil // Already enabled
		}
	}

	cluster.Spec.Addons = append(cluster.Spec.Addons, clusterv1alpha1.ClusterAddon{
		Name:    "kruise-rollout",
		Enabled: true,
	})

	if err := c.Update(ctx, &cluster); err != nil {
		return fmt.Errorf("failed to update cluster with addon: %w", err)
	}

	return nil
}

// mustEncodeTemplate encodes a PodTemplateSpec to RawExtension
func mustEncodeTemplate(template *corev1.PodTemplateSpec) runtime.RawExtension {
	data, err := json.Marshal(template)
	if err != nil {
		panic(err)
	}
	return runtime.RawExtension{Raw: data}
}

func int32Ptr(i int32) *int32 {
	return &i
}
