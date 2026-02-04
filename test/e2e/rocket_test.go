//go:build e2e

package e2e

import (
	"context"
	"os"
	"testing"
	"time"

	appsv1alpha1 "github.com/hex-techs/rocket/pkg/apis/apps/v1alpha1"
	clusterv1alpha1 "github.com/hex-techs/rocket/pkg/apis/storage/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/wait"
)

// TestMain is the entry point for all e2e tests
func TestMain(m *testing.M) {
	// Run tests
	code := m.Run()
	os.Exit(code)
}

// TestRocketE2E is the main test function that runs all e2e tests in a single environment
func TestRocketE2E(t *testing.T) {
	if os.Getenv("KUBECONFIG") == "" {
		t.Skip("KUBECONFIG not set, skipping E2E tests")
	}

	// Setup shared test environment
	env := SetupTestEnvironment(t)
	defer env.Cleanup()

	// Run all test suites
	t.Run("ClusterManagement", func(t *testing.T) {
		testClusterManagement(t, env)
	})

	t.Run("EdgeCluster", func(t *testing.T) {
		testEdgeCluster(t, env)
	})

	t.Run("ApplicationLifecycle", func(t *testing.T) {
		testApplicationLifecycle(t, env)
	})

	t.Run("Scheduling", func(t *testing.T) {
		testScheduling(t, env)
	})

	t.Run("PushModel", func(t *testing.T) {
		testPushModel(t, env)
	})

	t.Run("Features", func(t *testing.T) {
		testFeatures(t, env)
	})
}

// =============================================================================
// Cluster Management Tests
// =============================================================================

func testClusterManagement(t *testing.T, env *TestEnvironment) {
	ctx := env.Context()
	c := env.Client

	t.Run("HubClusterLifecycle", func(t *testing.T) {
		clusterName := "e2e-hub-lifecycle"

		// Create Hub cluster
		mc := env.CreateHubCluster(t, clusterName, map[string]string{"env": "test"})
		defer env.DeleteCluster(clusterName)

		// Verify cluster becomes Ready
		var got clusterv1alpha1.ManagedCluster
		err := wait.PollUntilContextTimeout(ctx, 1*time.Second, 30*time.Second, true, func(ctx context.Context) (bool, error) {
			if err := c.Get(ctx, types.NamespacedName{Name: clusterName}, &got); err != nil {
				return false, nil
			}
			return got.Status.State == clusterv1alpha1.ClusterReady, nil
		})
		require.NoError(t, err, "Hub cluster should become Ready")
		assert.Equal(t, clusterv1alpha1.ClusterReady, got.Status.State)
		assert.Equal(t, "test", mc.Labels["env"])
	})

	t.Run("ClusterOfflineDetection", func(t *testing.T) {
		clusterName := "e2e-offline-cluster"

		// Create Edge cluster without agent (will become Offline)
		mc := &clusterv1alpha1.ManagedCluster{
			ObjectMeta: metav1.ObjectMeta{Name: clusterName},
			Spec: clusterv1alpha1.ManagedClusterSpec{
				ConnectionMode: clusterv1alpha1.ClusterConnectionModeEdge,
			},
		}
		_ = c.Delete(ctx, mc)
		require.NoError(t, c.Create(ctx, mc))
		defer env.DeleteCluster(clusterName)

		// Set old heartbeat time
		wait.PollUntilContextTimeout(ctx, 100*time.Millisecond, 5*time.Second, true, func(ctx context.Context) (bool, error) {
			var latest clusterv1alpha1.ManagedCluster
			if err := c.Get(ctx, types.NamespacedName{Name: clusterName}, &latest); err != nil {
				return false, nil
			}
			latest.Status.LastKeepAliveTime = &metav1.Time{Time: time.Now().Add(-10 * time.Minute)}
			return c.Status().Update(ctx, &latest) == nil, nil
		})

		// Verify cluster becomes Offline
		var got clusterv1alpha1.ManagedCluster
		err := wait.PollUntilContextTimeout(ctx, 2*time.Second, 60*time.Second, true, func(ctx context.Context) (bool, error) {
			if err := c.Get(ctx, types.NamespacedName{Name: clusterName}, &got); err != nil {
				return false, nil
			}
			return got.Status.State == clusterv1alpha1.ClusterOffline, nil
		})
		require.NoError(t, err, "Edge cluster should become Offline")
	})
}

// =============================================================================
// Edge Cluster Tests
// =============================================================================

func testEdgeCluster(t *testing.T, env *TestEnvironment) {
	ctx := env.Context()
	c := env.Client

	t.Run("EdgeClusterLifecycle", func(t *testing.T) {
		clusterName := "e2e-edge-lifecycle"

		// Create Edge cluster with agent
		mc, _ := env.CreateEdgeCluster(t, clusterName, map[string]string{"type": "edge"})
		defer env.DeleteCluster(clusterName)

		// Verify cluster becomes Ready
		var got clusterv1alpha1.ManagedCluster
		err := wait.PollUntilContextTimeout(ctx, 1*time.Second, 30*time.Second, true, func(ctx context.Context) (bool, error) {
			if err := c.Get(ctx, types.NamespacedName{Name: clusterName}, &got); err != nil {
				return false, nil
			}
			return got.Status.State == clusterv1alpha1.ClusterReady, nil
		})
		require.NoError(t, err, "Edge cluster should become Ready")
		assert.Equal(t, clusterv1alpha1.ClusterConnectionModeEdge, mc.Spec.ConnectionMode)
		assert.Equal(t, "edge", mc.Labels["type"])
	})

	t.Run("EdgeClusterDeployment", func(t *testing.T) {
		clusterName := "e2e-edge-deploy"
		appName := "e2e-edge-app"
		namespace := "default"

		// Create Edge cluster with unique label
		env.CreateEdgeCluster(t, clusterName, map[string]string{"e2e-test": "edge-deploy"})
		defer env.DeleteCluster(clusterName)

		// Update cluster status with resources so scheduler can select it
		env.UpdateClusterStatus(t, clusterName, "10", "10Gi")

		// Create application targeting the Edge cluster using ClusterAffinity
		app := &appsv1alpha1.Application{
			ObjectMeta: metav1.ObjectMeta{
				Name:      appName,
				Namespace: namespace,
			},
			Spec: appsv1alpha1.ApplicationSpec{
				ClusterAffinity: &corev1.NodeAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
						NodeSelectorTerms: []corev1.NodeSelectorTerm{
							{
								MatchExpressions: []corev1.NodeSelectorRequirement{
									{
										Key:      "e2e-test",
										Operator: corev1.NodeSelectorOpIn,
										Values:   []string{"edge-deploy"},
									},
								},
							},
						},
					},
				},
				Workload: appsv1alpha1.WorkloadGVK{
					APIVersion: "apps/v1",
					Kind:       "Deployment",
				},
				Template: toRaw(map[string]interface{}{
					"metadata": map[string]interface{}{
						"labels": map[string]string{"app": appName},
					},
					"spec": map[string]interface{}{
						"containers": []interface{}{
							map[string]interface{}{
								"name":  "nginx",
								"image": "nginx:latest",
							},
						},
					},
				}),
			},
		}

		env.CreateApplication(t, app)
		defer env.DeleteApplication(appName, namespace)

		// Wait for scheduling to Edge cluster
		scheduled := env.WaitForApplicationScheduled(t, appName, namespace, 30*time.Second)
		assert.NotEmpty(t, scheduled.Status.Placement.Topology)
		assert.Equal(t, clusterName, scheduled.Status.Placement.Topology[0].Name)
	})

	t.Run("EdgeClusterHeartbeat", func(t *testing.T) {
		clusterName := "e2e-edge-heartbeat"

		// Create Edge cluster
		env.CreateEdgeCluster(t, clusterName, nil)
		defer env.DeleteCluster(clusterName)

		// Wait for heartbeat to update LastKeepAliveTime
		var got clusterv1alpha1.ManagedCluster
		err := wait.PollUntilContextTimeout(ctx, 1*time.Second, 10*time.Second, true, func(ctx context.Context) (bool, error) {
			if err := c.Get(ctx, types.NamespacedName{Name: clusterName}, &got); err != nil {
				return false, nil
			}
			return got.Status.LastKeepAliveTime != nil, nil
		})
		require.NoError(t, err, "Edge cluster should have LastKeepAliveTime set")

		// Verify heartbeat is recent (within last 10 seconds)
		assert.True(t, time.Since(got.Status.LastKeepAliveTime.Time) < 10*time.Second, "Heartbeat should be recent")
	})
}

// =============================================================================
// Application Lifecycle Tests
// =============================================================================

func testApplicationLifecycle(t *testing.T, env *TestEnvironment) {
	ctx := env.Context()
	c := env.Client

	t.Run("DeploymentLifecycle", func(t *testing.T) {
		clusterName := "e2e-app-cluster"
		appName := "e2e-deployment"
		namespace := "default"

		// Create cluster
		env.CreateHubCluster(t, clusterName, nil)
		defer env.DeleteCluster(clusterName)

		// Create application
		replicas := int32(2)
		app := &appsv1alpha1.Application{
			ObjectMeta: metav1.ObjectMeta{
				Name:      appName,
				Namespace: namespace,
			},
			Spec: appsv1alpha1.ApplicationSpec{
				Replicas: &replicas,
				Workload: appsv1alpha1.WorkloadGVK{
					APIVersion: "apps/v1",
					Kind:       "Deployment",
				},
				Template: toRaw(map[string]interface{}{
					"metadata": map[string]interface{}{
						"labels": map[string]string{"app": appName},
					},
					"spec": map[string]interface{}{
						"containers": []interface{}{
							map[string]interface{}{
								"name":  "nginx",
								"image": "nginx:latest",
							},
						},
					},
				}),
			},
		}

		env.CreateApplication(t, app)
		defer env.DeleteApplication(appName, namespace)

		// Wait for scheduling
		scheduled := env.WaitForApplicationScheduled(t, appName, namespace, 30*time.Second)
		assert.NotEmpty(t, scheduled.Status.Placement.Topology)

		// Verify deployment created
		var deploy appsv1.Deployment
		err := wait.PollUntilContextTimeout(ctx, 1*time.Second, 30*time.Second, true, func(ctx context.Context) (bool, error) {
			if err := c.Get(ctx, types.NamespacedName{Name: appName, Namespace: namespace}, &deploy); err != nil {
				return false, nil
			}
			return true, nil
		})
		require.NoError(t, err, "Deployment should be created")
	})

	t.Run("ApplicationDeletion", func(t *testing.T) {
		clusterName := "e2e-delete-cluster"
		appName := "e2e-delete-app"
		namespace := "default"

		// Create cluster
		env.CreateHubCluster(t, clusterName, nil)
		defer env.DeleteCluster(clusterName)

		// Create and then delete application
		app := &appsv1alpha1.Application{
			ObjectMeta: metav1.ObjectMeta{
				Name:      appName,
				Namespace: namespace,
			},
			Spec: appsv1alpha1.ApplicationSpec{
				Workload: appsv1alpha1.WorkloadGVK{
					APIVersion: "apps/v1",
					Kind:       "Deployment",
				},
				Template: toRaw(map[string]interface{}{
					"metadata": map[string]interface{}{
						"labels": map[string]string{"app": appName},
					},
					"spec": map[string]interface{}{
						"containers": []interface{}{
							map[string]interface{}{
								"name":  "nginx",
								"image": "nginx:latest",
							},
						},
					},
				}),
			},
		}

		env.CreateApplication(t, app)
		env.WaitForApplicationScheduled(t, appName, namespace, 30*time.Second)

		// Delete application
		env.DeleteApplication(appName, namespace)

		// Verify application is deleted
		err := wait.PollUntilContextTimeout(ctx, 1*time.Second, 30*time.Second, true, func(ctx context.Context) (bool, error) {
			var app appsv1alpha1.Application
			err := c.Get(ctx, types.NamespacedName{Name: appName, Namespace: namespace}, &app)
			return err != nil, nil
		})
		require.NoError(t, err, "Application should be deleted")
	})
}

// =============================================================================
// Scheduling Tests
// =============================================================================

func testScheduling(t *testing.T, env *TestEnvironment) {
	ctx := env.Context()
	c := env.Client

	t.Run("BasicScheduling", func(t *testing.T) {
		clusterName := "e2e-sched-cluster"
		appName := "e2e-sched-app"
		namespace := "default"

		// Create cluster with unique label
		env.CreateHubCluster(t, clusterName, map[string]string{"e2e-test": "basic-scheduling"})
		defer env.DeleteCluster(clusterName)

		// Create application with ClusterAffinity to target specific cluster
		app := &appsv1alpha1.Application{
			ObjectMeta: metav1.ObjectMeta{
				Name:      appName,
				Namespace: namespace,
			},
			Spec: appsv1alpha1.ApplicationSpec{
				ClusterAffinity: &corev1.NodeAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
						NodeSelectorTerms: []corev1.NodeSelectorTerm{
							{
								MatchExpressions: []corev1.NodeSelectorRequirement{
									{
										Key:      "e2e-test",
										Operator: corev1.NodeSelectorOpIn,
										Values:   []string{"basic-scheduling"},
									},
								},
							},
						},
					},
				},
				Workload: appsv1alpha1.WorkloadGVK{
					APIVersion: "apps/v1",
					Kind:       "Deployment",
				},
				Template: toRaw(map[string]interface{}{
					"metadata": map[string]interface{}{
						"labels": map[string]string{"app": appName},
					},
					"spec": map[string]interface{}{
						"containers": []interface{}{
							map[string]interface{}{
								"name":  "nginx",
								"image": "nginx:latest",
							},
						},
					},
				}),
			},
		}

		env.CreateApplication(t, app)
		defer env.DeleteApplication(appName, namespace)

		// Verify scheduling
		scheduled := env.WaitForApplicationScheduled(t, appName, namespace, 30*time.Second)
		assert.Equal(t, clusterName, scheduled.Status.Placement.Topology[0].Name)
	})

	t.Run("WaterfillScheduling", func(t *testing.T) {
		c1Name := "e2e-waterfill-c1"
		c2Name := "e2e-waterfill-c2"
		appName := "e2e-waterfill-app"
		namespace := "default"

		// Create clusters with different capacities
		env.CreateHubCluster(t, c1Name, nil)
		defer env.DeleteCluster(c1Name)
		env.CreateHubCluster(t, c2Name, nil)
		defer env.DeleteCluster(c2Name)

		// Update c1 with limited capacity (2 CPU)
		env.UpdateClusterStatus(t, c1Name, "2", "10Gi")
		// c2 has default 10 CPU

		// Create Deployment application with 3 replicas for waterfill test
		// Template should be a PodTemplateSpec (metadata + spec), NOT nested spec.selector/spec.template
		replicas := int32(3)
		app := &appsv1alpha1.Application{
			ObjectMeta: metav1.ObjectMeta{
				Name:      appName,
				Namespace: namespace,
			},
			Spec: appsv1alpha1.ApplicationSpec{
				Replicas: &replicas,
				Workload: appsv1alpha1.WorkloadGVK{
					APIVersion: "apps/v1",
					Kind:       "Deployment",
				},
				Template: toRaw(map[string]interface{}{
					"metadata": map[string]interface{}{
						"labels": map[string]string{"app": appName},
					},
					"spec": map[string]interface{}{
						"containers": []interface{}{
							map[string]interface{}{
								"name":  "nginx",
								"image": "nginx:latest",
								"resources": map[string]interface{}{
									"requests": map[string]interface{}{
										"cpu": "1",
									},
								},
							},
						},
					},
				}),
			},
		}

		env.CreateApplication(t, app)
		defer env.DeleteApplication(appName, namespace)

		// Verify all replicas are scheduled
		err := wait.PollUntilContextTimeout(ctx, 1*time.Second, 30*time.Second, true, func(ctx context.Context) (bool, error) {
			var got appsv1alpha1.Application
			if err := c.Get(ctx, types.NamespacedName{Name: appName, Namespace: namespace}, &got); err != nil {
				return false, nil
			}
			total := int32(0)
			for _, topo := range got.Status.Placement.Topology {
				total += topo.Replicas
			}
			return total == 3, nil
		})
		require.NoError(t, err, "All 3 replicas should be scheduled")
	})
}

// =============================================================================
// Feature Tests
// =============================================================================

func testFeatures(t *testing.T, env *TestEnvironment) {
	ctx := env.Context()
	c := env.Client

	t.Run("JobSupport", func(t *testing.T) {
		clusterName := "e2e-job-cluster"
		appName := "e2e-job-app"
		namespace := "default"

		// Create cluster
		env.CreateHubCluster(t, clusterName, nil)
		defer env.DeleteCluster(clusterName)

		// Create Job application
		// Template should be a PodTemplateSpec (only metadata + spec), NOT the full JobSpec
		app := &appsv1alpha1.Application{
			ObjectMeta: metav1.ObjectMeta{
				Name:      appName,
				Namespace: namespace,
			},
			Spec: appsv1alpha1.ApplicationSpec{
				Workload: appsv1alpha1.WorkloadGVK{
					APIVersion: "batch/v1",
					Kind:       "Job",
				},
				Template: toRaw(map[string]interface{}{
					"spec": map[string]interface{}{
						"containers": []interface{}{
							map[string]interface{}{
								"name":    "busybox",
								"image":   "busybox:latest",
								"command": []string{"echo", "hello"},
							},
						},
						"restartPolicy": "Never",
					},
				}),
			},
		}

		env.CreateApplication(t, app)
		defer env.DeleteApplication(appName, namespace)

		// Wait for scheduling
		env.WaitForApplicationScheduled(t, appName, namespace, 30*time.Second)

		// Verify Job created
		var job batchv1.Job
		err := wait.PollUntilContextTimeout(ctx, 1*time.Second, 30*time.Second, true, func(ctx context.Context) (bool, error) {
			if err := c.Get(ctx, types.NamespacedName{Name: appName, Namespace: namespace}, &job); err != nil {
				return false, nil
			}
			return true, nil
		})
		require.NoError(t, err, "Job should be created")
	})

	t.Run("CronJobSupport", func(t *testing.T) {
		clusterName := "e2e-cronjob-cluster"
		appName := "e2e-cronjob-app"
		namespace := "default"

		// Create cluster
		env.CreateHubCluster(t, clusterName, nil)
		defer env.DeleteCluster(clusterName)

		// Create CronJob application
		// Template should be a PodTemplateSpec (only metadata + spec), NOT the full JobSpec
		app := &appsv1alpha1.Application{
			ObjectMeta: metav1.ObjectMeta{
				Name:      appName,
				Namespace: namespace,
			},
			Spec: appsv1alpha1.ApplicationSpec{
				Workload: appsv1alpha1.WorkloadGVK{
					APIVersion: "batch/v1",
					Kind:       "CronJob",
				},
				Schedule: "* * * * *",
				Template: toRaw(map[string]interface{}{
					"spec": map[string]interface{}{
						"containers": []interface{}{
							map[string]interface{}{
								"name":    "busybox",
								"image":   "busybox:latest",
								"command": []string{"echo", "hello"},
							},
						},
						"restartPolicy": "OnFailure",
					},
				}),
			},
		}

		env.CreateApplication(t, app)
		defer env.DeleteApplication(appName, namespace)

		// Wait for scheduling
		env.WaitForApplicationScheduled(t, appName, namespace, 30*time.Second)

		// Verify CronJob created
		var cj batchv1.CronJob
		err := wait.PollUntilContextTimeout(ctx, 1*time.Second, 30*time.Second, true, func(ctx context.Context) (bool, error) {
			if err := c.Get(ctx, types.NamespacedName{Name: appName, Namespace: namespace}, &cj); err != nil {
				return false, nil
			}
			return cj.Spec.Schedule == "* * * * *", nil
		})
		require.NoError(t, err, "CronJob should be created with correct schedule")
	})

	t.Run("ResiliencyPDB", func(t *testing.T) {
		clusterName := "e2e-pdb-cluster"
		appName := "e2e-pdb-app"
		namespace := "default"

		// Create cluster
		env.CreateHubCluster(t, clusterName, nil)
		defer env.DeleteCluster(clusterName)

		// Create application with PDB
		minAvail := intstr.FromInt(1)
		app := &appsv1alpha1.Application{
			ObjectMeta: metav1.ObjectMeta{
				Name:      appName,
				Namespace: namespace,
			},
			Spec: appsv1alpha1.ApplicationSpec{
				Resiliency: &appsv1alpha1.ResiliencyPolicy{
					MinAvailable: &minAvail,
				},
				Workload: appsv1alpha1.WorkloadGVK{
					APIVersion: "apps/v1",
					Kind:       "Deployment",
				},
				Template: toRaw(map[string]interface{}{
					"metadata": map[string]interface{}{
						"labels": map[string]string{"app": appName},
					},
					"spec": map[string]interface{}{
						"containers": []interface{}{
							map[string]interface{}{
								"name":  "nginx",
								"image": "nginx:latest",
							},
						},
					},
				}),
			},
		}

		env.CreateApplication(t, app)
		defer env.DeleteApplication(appName, namespace)

		// Wait for scheduling
		env.WaitForApplicationScheduled(t, appName, namespace, 30*time.Second)

		// Verify PDB created
		var pdb policyv1.PodDisruptionBudget
		err := wait.PollUntilContextTimeout(ctx, 1*time.Second, 30*time.Second, true, func(ctx context.Context) (bool, error) {
			if err := c.Get(ctx, types.NamespacedName{Name: appName, Namespace: namespace}, &pdb); err != nil {
				return false, nil
			}
			return pdb.Spec.MinAvailable != nil && pdb.Spec.MinAvailable.IntVal == 1, nil
		})
		require.NoError(t, err, "PDB should be created with MinAvailable=1")
	})
}

// =============================================================================
// Push Model Tests (Edge Cluster Application Deployment)
// =============================================================================

func testPushModel(t *testing.T, env *TestEnvironment) {
	ctx := env.Context()
	c := env.Client

	t.Run("PushModelDeployment", func(t *testing.T) {
		clusterName := "e2e-push-cluster"
		appName := "e2e-push-app"
		namespace := "default"

		// Create Edge cluster
		env.CreateEdgeCluster(t, clusterName, map[string]string{"model": "push"})
		defer env.DeleteCluster(clusterName)

		// Create application
		replicas := int32(2)
		app := &appsv1alpha1.Application{
			ObjectMeta: metav1.ObjectMeta{
				Name:      appName,
				Namespace: namespace,
			},
			Spec: appsv1alpha1.ApplicationSpec{
				Replicas: &replicas,
				Workload: appsv1alpha1.WorkloadGVK{
					APIVersion: "apps/v1",
					Kind:       "Deployment",
				},
				Template: toRaw(map[string]interface{}{
					"metadata": map[string]interface{}{
						"labels": map[string]string{"app": appName},
					},
					"spec": map[string]interface{}{
						"containers": []interface{}{
							map[string]interface{}{
								"name":  "nginx",
								"image": "nginx:latest",
							},
						},
					},
				}),
			},
		}

		env.CreateApplication(t, app)
		defer env.DeleteApplication(appName, namespace)

		// Verify application is scheduled to the Edge cluster
		scheduled := env.WaitForApplicationScheduled(t, appName, namespace, 30*time.Second)
		assert.Equal(t, clusterName, scheduled.Status.Placement.Topology[0].Name)
		assert.Equal(t, int32(2), scheduled.Status.Placement.Topology[0].Replicas)
	})

	t.Run("PushModelMultiCluster", func(t *testing.T) {
		c1Name := "e2e-push-c1"
		c2Name := "e2e-push-c2"
		appName := "e2e-push-multi"
		namespace := "default"

		// Create two Edge clusters
		env.CreateEdgeCluster(t, c1Name, nil)
		defer env.DeleteCluster(c1Name)
		env.CreateEdgeCluster(t, c2Name, nil)
		defer env.DeleteCluster(c2Name)

		// Create application with multiple replicas
		replicas := int32(4)
		app := &appsv1alpha1.Application{
			ObjectMeta: metav1.ObjectMeta{
				Name:      appName,
				Namespace: namespace,
			},
			Spec: appsv1alpha1.ApplicationSpec{
				Replicas: &replicas,
				Workload: appsv1alpha1.WorkloadGVK{
					APIVersion: "apps/v1",
					Kind:       "Deployment",
				},
				Template: toRaw(map[string]interface{}{
					"metadata": map[string]interface{}{
						"labels": map[string]string{"app": appName},
					},
					"spec": map[string]interface{}{
						"containers": []interface{}{
							map[string]interface{}{
								"name":  "nginx",
								"image": "nginx:latest",
							},
						},
					},
				}),
			},
		}

		env.CreateApplication(t, app)
		defer env.DeleteApplication(appName, namespace)

		// Verify application is scheduled across clusters
		err := wait.PollUntilContextTimeout(ctx, 1*time.Second, 30*time.Second, true, func(ctx context.Context) (bool, error) {
			var got appsv1alpha1.Application
			if err := c.Get(ctx, types.NamespacedName{Name: appName, Namespace: namespace}, &got); err != nil {
				return false, nil
			}
			// Check total replicas scheduled
			total := int32(0)
			for _, topo := range got.Status.Placement.Topology {
				total += topo.Replicas
			}
			return total == replicas, nil
		})
		require.NoError(t, err, "All replicas should be scheduled")
	})

	t.Run("PushModelJobWorkload", func(t *testing.T) {
		clusterName := "e2e-push-job-cluster"
		appName := "e2e-push-job"
		namespace := "default"

		// Create Edge cluster
		env.CreateEdgeCluster(t, clusterName, nil)
		defer env.DeleteCluster(clusterName)

		// Create Job application
		app := &appsv1alpha1.Application{
			ObjectMeta: metav1.ObjectMeta{
				Name:      appName,
				Namespace: namespace,
			},
			Spec: appsv1alpha1.ApplicationSpec{
				Workload: appsv1alpha1.WorkloadGVK{
					APIVersion: "batch/v1",
					Kind:       "Job",
				},
				Template: toRaw(map[string]interface{}{
					"spec": map[string]interface{}{
						"containers": []interface{}{
							map[string]interface{}{
								"name":    "busybox",
								"image":   "busybox:latest",
								"command": []string{"echo", "push-model-test"},
							},
						},
						"restartPolicy": "Never",
					},
				}),
			},
		}

		env.CreateApplication(t, app)
		defer env.DeleteApplication(appName, namespace)

		// Verify application is scheduled to the Edge cluster
		scheduled := env.WaitForApplicationScheduled(t, appName, namespace, 30*time.Second)
		assert.Equal(t, clusterName, scheduled.Status.Placement.Topology[0].Name)
	})
}
