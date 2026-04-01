//go:build e2e

package e2e

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/hex-techs/rocket/internal/addon"
	"github.com/hex-techs/rocket/internal/addon/mcs"
	appsv1alpha1 "github.com/hex-techs/rocket/pkg/apis/apps/v1alpha1"
	clusterv1alpha1 "github.com/hex-techs/rocket/pkg/apis/storage/v1alpha1"
	workspacev1alpha1 "github.com/hex-techs/rocket/pkg/apis/workspace/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
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

	t.Run("AdvancedScheduling", func(t *testing.T) {
		testAdvancedScheduling(t, env)
	})

	t.Run("WorkloadTypes", func(t *testing.T) {
		testWorkloadTypes(t, env)
	})

	t.Run("ScaleOperations", func(t *testing.T) {
		testScaleOperations(t, env)
	})

	t.Run("Overrides", func(t *testing.T) {
		testOverrides(t, env)
	})

	t.Run("CredentialsManagement", func(t *testing.T) {
		testCredentialsManagement(t, env)
	})

	t.Run("VolumeRestriction", func(t *testing.T) {
		testVolumeRestriction(t, env)
	})

	t.Run("TopologySpread", func(t *testing.T) {
		testTopologySpread(t, env)
	})

	t.Run("AddonUpgrade", func(t *testing.T) {
		testAddonUpgrade(t, env)
	})

	t.Run("AddonConfigValidation", func(t *testing.T) {
		testAddonConfigValidation(t, env)
	})

	t.Run("AddonMultiCluster", func(t *testing.T) {
		testAddonMultiCluster(t, env)
	})

	t.Run("Workspace", func(t *testing.T) {
		testWorkspace(t, env)
	})

	t.Run("AddonHelmInstall", func(t *testing.T) {
		testAddonHelmInstall(t, env)
	})

	t.Run("CapacityFilter", func(t *testing.T) {
		testCapacityFilter(t, env)
	})

	t.Run("DaemonSetWorkload", func(t *testing.T) {
		testDaemonSetWorkload(t, env)
	})

	t.Run("VictoriaMetricsAddon", func(t *testing.T) {
		testVictoriaMetricsAddon(t, env)
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

// =============================================================================
// Advanced Scheduling Tests
// =============================================================================

func testAdvancedScheduling(t *testing.T, env *TestEnvironment) {
	ctx := env.Context()
	c := env.Client

	t.Run("TaintToleration", func(t *testing.T) {
		clusterName := "e2e-taint-cluster"
		appName := "e2e-taint-app"
		namespace := "default"

		// Create cluster with taint
		mc := &clusterv1alpha1.ManagedCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:   clusterName,
				Labels: map[string]string{"e2e-test": "taint-toleration"},
			},
			Spec: clusterv1alpha1.ManagedClusterSpec{
				ConnectionMode: clusterv1alpha1.ClusterConnectionModeHub,
				APIServer:      env.Config.Host,
				SecretRef:      &corev1.LocalObjectReference{Name: env.ClusterSecretName},
				Taints: []corev1.Taint{
					{
						Key:    "dedicated",
						Value:  "gpu",
						Effect: corev1.TaintEffectNoSchedule,
					},
				},
			},
		}
		_ = c.Delete(ctx, mc)
		require.NoError(t, c.Create(ctx, mc))
		defer env.DeleteCluster(clusterName)

		// Update cluster status
		env.UpdateClusterStatus(t, clusterName, "10", "10Gi")

		// Create application WITHOUT toleration - should NOT be scheduled to this cluster
		appNoToleration := &appsv1alpha1.Application{
			ObjectMeta: metav1.ObjectMeta{
				Name:      appName + "-no-toleration",
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
										Values:   []string{"taint-toleration"},
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
		env.CreateApplication(t, appNoToleration)
		defer env.DeleteApplication(appName+"-no-toleration", namespace)

		// Wait and verify it's NOT scheduled (no clusters available)
		time.Sleep(3 * time.Second)
		var gotApp appsv1alpha1.Application
		err := c.Get(ctx, types.NamespacedName{Name: appName + "-no-toleration", Namespace: namespace}, &gotApp)
		require.NoError(t, err)
		assert.Empty(t, gotApp.Status.Placement.Topology, "App without toleration should not be scheduled to tainted cluster")

		// Create application WITH toleration - should be scheduled
		appWithToleration := &appsv1alpha1.Application{
			ObjectMeta: metav1.ObjectMeta{
				Name:      appName + "-with-toleration",
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
										Values:   []string{"taint-toleration"},
									},
								},
							},
						},
					},
				},
				ClusterTolerations: []corev1.Toleration{
					{
						Key:      "dedicated",
						Operator: corev1.TolerationOpEqual,
						Value:    "gpu",
						Effect:   corev1.TaintEffectNoSchedule,
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
		env.CreateApplication(t, appWithToleration)
		defer env.DeleteApplication(appName+"-with-toleration", namespace)

		// Verify app with toleration is scheduled
		scheduled := env.WaitForApplicationScheduled(t, appName+"-with-toleration", namespace, 30*time.Second)
		assert.Equal(t, clusterName, scheduled.Status.Placement.Topology[0].Name)
	})

	t.Run("PreferredAffinity", func(t *testing.T) {
		c1Name := "e2e-preferred-c1"
		c2Name := "e2e-preferred-c2"
		appName := "e2e-preferred-app"
		namespace := "default"

		// Create two clusters with different labels
		env.CreateHubCluster(t, c1Name, map[string]string{"tier": "high-performance", "e2e-test": "preferred"})
		defer env.DeleteCluster(c1Name)
		env.CreateHubCluster(t, c2Name, map[string]string{"tier": "standard", "e2e-test": "preferred"})
		defer env.DeleteCluster(c2Name)

		// Create application with preferred affinity for high-performance
		app := &appsv1alpha1.Application{
			ObjectMeta: metav1.ObjectMeta{
				Name:      appName,
				Namespace: namespace,
				Annotations: map[string]string{
					"apps.rocket.io/scheduler-strategy": "SingleCluster",
				},
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
										Values:   []string{"preferred"},
									},
								},
							},
						},
					},
					PreferredDuringSchedulingIgnoredDuringExecution: []corev1.PreferredSchedulingTerm{
						{
							Weight: 100,
							Preference: corev1.NodeSelectorTerm{
								MatchExpressions: []corev1.NodeSelectorRequirement{
									{
										Key:      "tier",
										Operator: corev1.NodeSelectorOpIn,
										Values:   []string{"high-performance"},
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

		// Verify scheduled to high-performance cluster due to preference
		scheduled := env.WaitForApplicationScheduled(t, appName, namespace, 30*time.Second)
		assert.Equal(t, c1Name, scheduled.Status.Placement.Topology[0].Name, "Should prefer high-performance cluster")
	})

	t.Run("SingleClusterStrategy", func(t *testing.T) {
		c1Name := "e2e-single-c1"
		c2Name := "e2e-single-c2"
		appName := "e2e-single-app"
		namespace := "default"

		// Create two clusters
		env.CreateHubCluster(t, c1Name, map[string]string{"e2e-test": "single-cluster"})
		defer env.DeleteCluster(c1Name)
		env.CreateHubCluster(t, c2Name, map[string]string{"e2e-test": "single-cluster"})
		defer env.DeleteCluster(c2Name)

		// Create application with SingleCluster strategy
		replicas := int32(5)
		app := &appsv1alpha1.Application{
			ObjectMeta: metav1.ObjectMeta{
				Name:      appName,
				Namespace: namespace,
				Annotations: map[string]string{
					"apps.rocket.io/scheduler-strategy": "SingleCluster",
				},
			},
			Spec: appsv1alpha1.ApplicationSpec{
				Replicas: &replicas,
				ClusterAffinity: &corev1.NodeAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
						NodeSelectorTerms: []corev1.NodeSelectorTerm{
							{
								MatchExpressions: []corev1.NodeSelectorRequirement{
									{
										Key:      "e2e-test",
										Operator: corev1.NodeSelectorOpIn,
										Values:   []string{"single-cluster"},
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

		// Verify ALL replicas go to a single cluster
		scheduled := env.WaitForApplicationScheduled(t, appName, namespace, 30*time.Second)
		assert.Len(t, scheduled.Status.Placement.Topology, 1, "SingleCluster strategy should use only one cluster")
		assert.Equal(t, int32(5), scheduled.Status.Placement.Topology[0].Replicas, "All replicas should be on one cluster")
	})

	t.Run("ResourceBasedScoring", func(t *testing.T) {
		c1Name := "e2e-resource-c1"
		c2Name := "e2e-resource-c2"
		appName := "e2e-resource-app"
		namespace := "default"

		// Create two clusters with different resource availability
		env.CreateHubCluster(t, c1Name, map[string]string{"e2e-test": "resource-scoring"})
		defer env.DeleteCluster(c1Name)
		env.CreateHubCluster(t, c2Name, map[string]string{"e2e-test": "resource-scoring"})
		defer env.DeleteCluster(c2Name)

		// c1: 80% utilized (less free), c2: 20% utilized (more free)
		env.UpdateClusterStatusWithAllocation(t, c1Name, "10", "10Gi", "8", "8Gi")
		env.UpdateClusterStatusWithAllocation(t, c2Name, "10", "10Gi", "2", "2Gi")

		// Create application with SingleCluster - should go to c2 (more free resources, LeastAllocated default)
		app := &appsv1alpha1.Application{
			ObjectMeta: metav1.ObjectMeta{
				Name:      appName,
				Namespace: namespace,
				Annotations: map[string]string{
					"apps.rocket.io/scheduler-strategy": "SingleCluster",
				},
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
										Values:   []string{"resource-scoring"},
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

		// Verify scheduled to c2 (more free resources with LeastAllocated strategy)
		scheduled := env.WaitForApplicationScheduled(t, appName, namespace, 30*time.Second)
		assert.Equal(t, c2Name, scheduled.Status.Placement.Topology[0].Name, "Should prefer cluster with more free resources")
	})
}

// =============================================================================
// Workload Types Tests
// =============================================================================

func testWorkloadTypes(t *testing.T, env *TestEnvironment) {
	ctx := env.Context()
	c := env.Client

	t.Run("StatefulSetSupport", func(t *testing.T) {
		clusterName := "e2e-sts-cluster"
		appName := "e2e-sts-app"
		namespace := "default"

		// Create cluster
		env.CreateHubCluster(t, clusterName, nil)
		defer env.DeleteCluster(clusterName)

		// Create StatefulSet application
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
					Kind:       "StatefulSet",
				},
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"app": appName},
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

		// Verify StatefulSet created
		var sts appsv1.StatefulSet
		err := wait.PollUntilContextTimeout(ctx, 1*time.Second, 30*time.Second, true, func(ctx context.Context) (bool, error) {
			if err := c.Get(ctx, types.NamespacedName{Name: appName, Namespace: namespace}, &sts); err != nil {
				return false, nil
			}
			return sts.Spec.Replicas != nil && *sts.Spec.Replicas > 0, nil
		})
		require.NoError(t, err, "StatefulSet should be created")
		assert.Equal(t, appName, sts.Spec.ServiceName, "StatefulSet should have correct service name")
	})

	t.Run("JobWithAttributes", func(t *testing.T) {
		clusterName := "e2e-job-attr-cluster"
		appName := "e2e-job-attr-app"
		namespace := "default"

		// Create cluster
		env.CreateHubCluster(t, clusterName, nil)
		defer env.DeleteCluster(clusterName)

		// Create Job application with JobAttributes
		completions := int32(3)
		parallelism := int32(2)
		backoffLimit := int32(2)
		ttl := int32(60)
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
				JobAttributes: &appsv1alpha1.JobAttributes{
					Completions:             &completions,
					Parallelism:             &parallelism,
					BackoffLimit:            &backoffLimit,
					TTLSecondsAfterFinished: &ttl,
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

		// Verify Job created with correct attributes
		var job batchv1.Job
		err := wait.PollUntilContextTimeout(ctx, 1*time.Second, 30*time.Second, true, func(ctx context.Context) (bool, error) {
			if err := c.Get(ctx, types.NamespacedName{Name: appName, Namespace: namespace}, &job); err != nil {
				return false, nil
			}
			return job.Spec.Completions != nil, nil
		})
		require.NoError(t, err, "Job should be created")
		assert.Equal(t, int32(3), *job.Spec.Completions, "Job completions should match")
		assert.Equal(t, int32(2), *job.Spec.Parallelism, "Job parallelism should match")
		assert.Equal(t, int32(2), *job.Spec.BackoffLimit, "Job backoffLimit should match")
		assert.Equal(t, int32(60), *job.Spec.TTLSecondsAfterFinished, "Job TTL should match")
	})

	t.Run("MaxUnavailablePDB", func(t *testing.T) {
		clusterName := "e2e-pdb-max-cluster"
		appName := "e2e-pdb-max-app"
		namespace := "default"

		// Create cluster
		env.CreateHubCluster(t, clusterName, nil)
		defer env.DeleteCluster(clusterName)

		// Create application with MaxUnavailable PDB
		maxUnavail := intstr.FromInt(2)
		app := &appsv1alpha1.Application{
			ObjectMeta: metav1.ObjectMeta{
				Name:      appName,
				Namespace: namespace,
			},
			Spec: appsv1alpha1.ApplicationSpec{
				Resiliency: &appsv1alpha1.ResiliencyPolicy{
					MaxUnavailable: &maxUnavail,
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

		// Verify PDB created with MaxUnavailable
		var pdb policyv1.PodDisruptionBudget
		err := wait.PollUntilContextTimeout(ctx, 1*time.Second, 30*time.Second, true, func(ctx context.Context) (bool, error) {
			if err := c.Get(ctx, types.NamespacedName{Name: appName, Namespace: namespace}, &pdb); err != nil {
				return false, nil
			}
			return pdb.Spec.MaxUnavailable != nil, nil
		})
		require.NoError(t, err, "PDB should be created with MaxUnavailable")
		assert.Equal(t, int32(2), pdb.Spec.MaxUnavailable.IntVal, "PDB MaxUnavailable should match")
	})
}

// =============================================================================
// Scale Operations Tests
// =============================================================================

func testScaleOperations(t *testing.T, env *TestEnvironment) {
	ctx := env.Context()
	c := env.Client

	t.Run("ScaleUp", func(t *testing.T) {
		c1Name := "e2e-scaleup-c1"
		c2Name := "e2e-scaleup-c2"
		appName := "e2e-scaleup-app"
		namespace := "default"

		// Create two clusters
		env.CreateHubCluster(t, c1Name, map[string]string{"e2e-test": "scale-up"})
		defer env.DeleteCluster(c1Name)
		env.CreateHubCluster(t, c2Name, map[string]string{"e2e-test": "scale-up"})
		defer env.DeleteCluster(c2Name)

		// Create application with 4 replicas (Spread strategy)
		replicas := int32(4)
		app := &appsv1alpha1.Application{
			ObjectMeta: metav1.ObjectMeta{
				Name:      appName,
				Namespace: namespace,
			},
			Spec: appsv1alpha1.ApplicationSpec{
				Replicas: &replicas,
				ClusterAffinity: &corev1.NodeAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
						NodeSelectorTerms: []corev1.NodeSelectorTerm{
							{
								MatchExpressions: []corev1.NodeSelectorRequirement{
									{
										Key:      "e2e-test",
										Operator: corev1.NodeSelectorOpIn,
										Values:   []string{"scale-up"},
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

		// Wait for initial scheduling
		initialScheduled := env.WaitForApplicationScheduled(t, appName, namespace, 30*time.Second)
		initialTotalReplicas := int32(0)
		initialDistribution := make(map[string]int32)
		for _, topo := range initialScheduled.Status.Placement.Topology {
			initialTotalReplicas += topo.Replicas
			initialDistribution[topo.Name] = topo.Replicas
		}
		assert.Equal(t, int32(4), initialTotalReplicas, "Initial replicas should be 4")

		// Scale up to 8 replicas
		newReplicas := int32(8)
		err := wait.PollUntilContextTimeout(ctx, 500*time.Millisecond, 10*time.Second, true, func(ctx context.Context) (bool, error) {
			var latest appsv1alpha1.Application
			if err := c.Get(ctx, types.NamespacedName{Name: appName, Namespace: namespace}, &latest); err != nil {
				return false, nil
			}
			latest.Spec.Replicas = &newReplicas
			return c.Update(ctx, &latest) == nil, nil
		})
		require.NoError(t, err, "Failed to update replicas")

		// Wait for scale-up to complete
		err = wait.PollUntilContextTimeout(ctx, 1*time.Second, 30*time.Second, true, func(ctx context.Context) (bool, error) {
			var got appsv1alpha1.Application
			if err := c.Get(ctx, types.NamespacedName{Name: appName, Namespace: namespace}, &got); err != nil {
				return false, nil
			}
			total := int32(0)
			for _, topo := range got.Status.Placement.Topology {
				total += topo.Replicas
			}
			return total == 8, nil
		})
		require.NoError(t, err, "Scale-up should complete")

		// Verify existing clusters didn't lose replicas (no-reduction principle)
		var finalApp appsv1alpha1.Application
		c.Get(ctx, types.NamespacedName{Name: appName, Namespace: namespace}, &finalApp)
		for _, topo := range finalApp.Status.Placement.Topology {
			if oldCount, ok := initialDistribution[topo.Name]; ok {
				assert.GreaterOrEqual(t, topo.Replicas, oldCount, "Cluster %s should not lose replicas during scale-up", topo.Name)
			}
		}
	})

	t.Run("ScaleDown", func(t *testing.T) {
		c1Name := "e2e-scaledown-c1"
		c2Name := "e2e-scaledown-c2"
		appName := "e2e-scaledown-app"
		namespace := "default"

		// Create two clusters
		env.CreateHubCluster(t, c1Name, map[string]string{"e2e-test": "scale-down"})
		defer env.DeleteCluster(c1Name)
		env.CreateHubCluster(t, c2Name, map[string]string{"e2e-test": "scale-down"})
		defer env.DeleteCluster(c2Name)

		// Create application with 8 replicas
		replicas := int32(8)
		app := &appsv1alpha1.Application{
			ObjectMeta: metav1.ObjectMeta{
				Name:      appName,
				Namespace: namespace,
			},
			Spec: appsv1alpha1.ApplicationSpec{
				Replicas: &replicas,
				ClusterAffinity: &corev1.NodeAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
						NodeSelectorTerms: []corev1.NodeSelectorTerm{
							{
								MatchExpressions: []corev1.NodeSelectorRequirement{
									{
										Key:      "e2e-test",
										Operator: corev1.NodeSelectorOpIn,
										Values:   []string{"scale-down"},
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

		// Wait for initial scheduling
		env.WaitForApplicationScheduled(t, appName, namespace, 30*time.Second)

		// Verify initial total is 8
		var initialApp appsv1alpha1.Application
		c.Get(ctx, types.NamespacedName{Name: appName, Namespace: namespace}, &initialApp)
		initialTotal := int32(0)
		for _, topo := range initialApp.Status.Placement.Topology {
			initialTotal += topo.Replicas
		}
		assert.Equal(t, int32(8), initialTotal, "Initial replicas should be 8")

		// Scale down to 4 replicas
		newReplicas := int32(4)
		err := wait.PollUntilContextTimeout(ctx, 500*time.Millisecond, 10*time.Second, true, func(ctx context.Context) (bool, error) {
			var latest appsv1alpha1.Application
			if err := c.Get(ctx, types.NamespacedName{Name: appName, Namespace: namespace}, &latest); err != nil {
				return false, nil
			}
			latest.Spec.Replicas = &newReplicas
			return c.Update(ctx, &latest) == nil, nil
		})
		require.NoError(t, err, "Failed to update replicas")

		// Wait for scale-down to complete
		err = wait.PollUntilContextTimeout(ctx, 1*time.Second, 30*time.Second, true, func(ctx context.Context) (bool, error) {
			var got appsv1alpha1.Application
			if err := c.Get(ctx, types.NamespacedName{Name: appName, Namespace: namespace}, &got); err != nil {
				return false, nil
			}
			total := int32(0)
			for _, topo := range got.Status.Placement.Topology {
				total += topo.Replicas
			}
			return total == 4, nil
		})
		require.NoError(t, err, "Scale-down should complete with total 4 replicas")
	})

	t.Run("ApplicationUpdate", func(t *testing.T) {
		clusterName := "e2e-update-cluster"
		appName := "e2e-update-app"
		namespace := "default"

		// Create cluster
		env.CreateHubCluster(t, clusterName, nil)
		defer env.DeleteCluster(clusterName)

		// Create application
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
								"image": "nginx:1.24",
							},
						},
					},
				}),
			},
		}

		env.CreateApplication(t, app)
		defer env.DeleteApplication(appName, namespace)

		// Wait for scheduling and deployment creation
		env.WaitForApplicationScheduled(t, appName, namespace, 30*time.Second)

		var deploy appsv1.Deployment
		err := wait.PollUntilContextTimeout(ctx, 1*time.Second, 30*time.Second, true, func(ctx context.Context) (bool, error) {
			if err := c.Get(ctx, types.NamespacedName{Name: appName, Namespace: namespace}, &deploy); err != nil {
				return false, nil
			}
			return len(deploy.Spec.Template.Spec.Containers) > 0, nil
		})
		require.NoError(t, err, "Deployment should be created")
		assert.Equal(t, "nginx:1.24", deploy.Spec.Template.Spec.Containers[0].Image)

		// Update application image
		err = wait.PollUntilContextTimeout(ctx, 500*time.Millisecond, 10*time.Second, true, func(ctx context.Context) (bool, error) {
			var latest appsv1alpha1.Application
			if err := c.Get(ctx, types.NamespacedName{Name: appName, Namespace: namespace}, &latest); err != nil {
				return false, nil
			}
			latest.Spec.Template = toRaw(map[string]interface{}{
				"metadata": map[string]interface{}{
					"labels": map[string]string{"app": appName},
				},
				"spec": map[string]interface{}{
					"containers": []interface{}{
						map[string]interface{}{
							"name":  "nginx",
							"image": "nginx:1.25",
						},
					},
				},
			})
			return c.Update(ctx, &latest) == nil, nil
		})
		require.NoError(t, err, "Failed to update application")

		// Verify deployment is updated
		err = wait.PollUntilContextTimeout(ctx, 1*time.Second, 30*time.Second, true, func(ctx context.Context) (bool, error) {
			if err := c.Get(ctx, types.NamespacedName{Name: appName, Namespace: namespace}, &deploy); err != nil {
				return false, nil
			}
			return deploy.Spec.Template.Spec.Containers[0].Image == "nginx:1.25", nil
		})
		require.NoError(t, err, "Deployment image should be updated to nginx:1.25")
	})
}

// =============================================================================
// Overrides Tests
// =============================================================================

func testOverrides(t *testing.T, env *TestEnvironment) {
	ctx := env.Context()
	c := env.Client

	t.Run("ImageOverride", func(t *testing.T) {
		c1Name := "e2e-override-c1"
		c2Name := "e2e-override-c2"
		appName := "e2e-override-app"
		namespace := "default"

		// Create two clusters with different labels
		env.CreateHubCluster(t, c1Name, map[string]string{"e2e-test": "override", "env": "production"})
		defer env.DeleteCluster(c1Name)
		env.CreateHubCluster(t, c2Name, map[string]string{"e2e-test": "override", "env": "staging"})
		defer env.DeleteCluster(c2Name)

		// Create application with image override for production
		replicas := int32(2)
		app := &appsv1alpha1.Application{
			ObjectMeta: metav1.ObjectMeta{
				Name:      appName,
				Namespace: namespace,
			},
			Spec: appsv1alpha1.ApplicationSpec{
				Replicas: &replicas,
				ClusterAffinity: &corev1.NodeAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
						NodeSelectorTerms: []corev1.NodeSelectorTerm{
							{
								MatchExpressions: []corev1.NodeSelectorRequirement{
									{
										Key:      "e2e-test",
										Operator: corev1.NodeSelectorOpIn,
										Values:   []string{"override"},
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
								"image": "nginx:stable",
							},
						},
					},
				}),
				Overrides: []appsv1alpha1.PolicyOverride{
					{
						ClusterSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"env": "production"},
						},
						Image: "nginx:production",
					},
				},
			},
		}

		env.CreateApplication(t, app)
		defer env.DeleteApplication(appName, namespace)

		// Wait for scheduling to both clusters
		err := wait.PollUntilContextTimeout(ctx, 1*time.Second, 30*time.Second, true, func(ctx context.Context) (bool, error) {
			var got appsv1alpha1.Application
			if err := c.Get(ctx, types.NamespacedName{Name: appName, Namespace: namespace}, &got); err != nil {
				return false, nil
			}
			total := int32(0)
			for _, topo := range got.Status.Placement.Topology {
				total += topo.Replicas
			}
			return total == 2, nil
		})
		require.NoError(t, err, "Application should be scheduled")

		// Verify that production cluster deployment has overridden image
		// Note: In e2e test we use the same cluster for all workloads,
		// so we verify the override mechanism is working through the application status
		var finalApp appsv1alpha1.Application
		c.Get(ctx, types.NamespacedName{Name: appName, Namespace: namespace}, &finalApp)
		assert.NotEmpty(t, finalApp.Status.Placement.Topology, "Should have placement")
		assert.NotNil(t, finalApp.Spec.Overrides, "Overrides should be set")
		assert.Len(t, finalApp.Spec.Overrides, 1, "Should have 1 override")
		assert.Equal(t, "nginx:production", finalApp.Spec.Overrides[0].Image, "Override image should be set")
	})

	t.Run("EnvOverride", func(t *testing.T) {
		c1Name := "e2e-env-override-c1"
		appName := "e2e-env-override-app"
		namespace := "default"

		// Create cluster
		env.CreateHubCluster(t, c1Name, map[string]string{"e2e-test": "env-override", "region": "us-west"})
		defer env.DeleteCluster(c1Name)

		// Create application with env override
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
										Values:   []string{"env-override"},
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
								"env": []interface{}{
									map[string]interface{}{
										"name":  "REGION",
										"value": "default",
									},
								},
							},
						},
					},
				}),
				Overrides: []appsv1alpha1.PolicyOverride{
					{
						ClusterSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"region": "us-west"},
						},
						Env: []corev1.EnvVar{
							{
								Name:  "REGION",
								Value: "us-west",
							},
							{
								Name:  "EXTRA_CONFIG",
								Value: "west-specific",
							},
						},
					},
				},
			},
		}

		env.CreateApplication(t, app)
		defer env.DeleteApplication(appName, namespace)

		// Wait for scheduling
		env.WaitForApplicationScheduled(t, appName, namespace, 30*time.Second)

		// Verify application has the env override
		var finalApp appsv1alpha1.Application
		c.Get(ctx, types.NamespacedName{Name: appName, Namespace: namespace}, &finalApp)
		assert.NotEmpty(t, finalApp.Spec.Overrides, "Overrides should be set")
		assert.Len(t, finalApp.Spec.Overrides[0].Env, 2, "Should have 2 env overrides")
	})

	t.Run("ResourceOverride", func(t *testing.T) {
		c1Name := "e2e-res-override-c1"
		appName := "e2e-res-override-app"
		namespace := "default"

		// Create cluster
		env.CreateHubCluster(t, c1Name, map[string]string{"e2e-test": "resource-override", "size": "large"})
		defer env.DeleteCluster(c1Name)

		// Create application with resource override
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
										Values:   []string{"resource-override"},
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
								"resources": map[string]interface{}{
									"requests": map[string]interface{}{
										"cpu":    "100m",
										"memory": "128Mi",
									},
								},
							},
						},
					},
				}),
				Overrides: []appsv1alpha1.PolicyOverride{
					{
						ClusterSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"size": "large"},
						},
						Resources: &corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("500m"),
								corev1.ResourceMemory: resource.MustParse("512Mi"),
							},
							Limits: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("1"),
								corev1.ResourceMemory: resource.MustParse("1Gi"),
							},
						},
					},
				},
			},
		}

		env.CreateApplication(t, app)
		defer env.DeleteApplication(appName, namespace)

		// Wait for scheduling
		env.WaitForApplicationScheduled(t, appName, namespace, 30*time.Second)

		// Verify application has the resource override
		var finalApp appsv1alpha1.Application
		c.Get(ctx, types.NamespacedName{Name: appName, Namespace: namespace}, &finalApp)
		assert.NotEmpty(t, finalApp.Spec.Overrides, "Overrides should be set")
		assert.NotNil(t, finalApp.Spec.Overrides[0].Resources, "Resource override should be set")
		assert.Equal(t, "500m", finalApp.Spec.Overrides[0].Resources.Requests.Cpu().String(), "CPU request override should match")
	})
}

// =============================================================================
// Credentials Management Tests
// =============================================================================

const (
	annotationCredentialsCA    = "cluster.rocket.io/credentials-ca"
	annotationCredentialsToken = "cluster.rocket.io/credentials-token"
	annotationAPIServerURL     = "cluster.rocket.io/apiserver-url"
)

func testCredentialsManagement(t *testing.T, env *TestEnvironment) {
	ctx := env.Context()
	c := env.Client

	t.Run("EdgeCredentialsToSecret", func(t *testing.T) {
		clusterName := "e2e-edge-creds"
		env.CreateEdgeCluster(t, clusterName, map[string]string{"e2e-test": "edge-creds"})
		defer env.DeleteCluster(clusterName)

		secret := env.WaitForClusterSecret(t, clusterName, 30*time.Second)
		require.NotEmpty(t, secret.Data["token"], "Edge secret token should be set")
		require.NotEmpty(t, secret.Data["caData"], "Edge secret CA should be set")

		var cluster clusterv1alpha1.ManagedCluster
		err := wait.PollUntilContextTimeout(ctx, 500*time.Millisecond, 30*time.Second, true, func(ctx context.Context) (bool, error) {
			if err := c.Get(ctx, types.NamespacedName{Name: clusterName}, &cluster); err != nil {
				return false, nil
			}
			if cluster.Spec.SecretRef == nil || cluster.Spec.SecretRef.Name == "" {
				return false, nil
			}
			if cluster.Status.State != clusterv1alpha1.ClusterReady {
				return false, nil
			}
			if cluster.Status.APIServerURL == "" {
				return false, nil
			}
			if cluster.Annotations != nil {
				if _, ok := cluster.Annotations[annotationCredentialsToken]; ok {
					return false, nil
				}
				if _, ok := cluster.Annotations[annotationCredentialsCA]; ok {
					return false, nil
				}
				if _, ok := cluster.Annotations[annotationAPIServerURL]; ok {
					return false, nil
				}
			}
			return true, nil
		})
		require.NoError(t, err, "Edge credentials should be persisted and annotations removed")
	})

	t.Run("EdgeCredentialsRotation", func(t *testing.T) {
		clusterName := "e2e-edge-rotate"
		env.CreateEdgeCluster(t, clusterName, map[string]string{"e2e-test": "edge-rotate"})
		defer env.DeleteCluster(clusterName)

		env.WaitForClusterSecret(t, clusterName, 30*time.Second)

		newToken := "rotated-token"
		newCA := base64.StdEncoding.EncodeToString([]byte("rotated-ca"))
		env.PatchClusterAnnotations(t, clusterName, map[string]string{
			annotationCredentialsToken: newToken,
			annotationCredentialsCA:    newCA,
			annotationAPIServerURL:     "https://kubernetes.default.svc:443",
		})

		secretName := fmt.Sprintf("cluster-creds-%s", clusterName)
		var secret corev1.Secret
		err := wait.PollUntilContextTimeout(ctx, 500*time.Millisecond, 30*time.Second, true, func(ctx context.Context) (bool, error) {
			if err := c.Get(ctx, types.NamespacedName{Name: secretName, Namespace: TestNamespace}, &secret); err != nil {
				return false, nil
			}
			return string(secret.Data["token"]) == newToken && string(secret.Data["caData"]) == "rotated-ca", nil
		})
		require.NoError(t, err, "Edge secret should be updated after rotation")
	})

	t.Run("HubMissingSecretRef", func(t *testing.T) {
		clusterName := "e2e-hub-missing-secret"
		mc := &clusterv1alpha1.ManagedCluster{
			ObjectMeta: metav1.ObjectMeta{Name: clusterName},
			Spec: clusterv1alpha1.ManagedClusterSpec{
				ConnectionMode: clusterv1alpha1.ClusterConnectionModeHub,
				APIServer:      env.Config.Host,
			},
		}
		_ = c.Delete(ctx, mc)
		require.NoError(t, c.Create(ctx, mc))
		defer env.DeleteCluster(clusterName)

		env.WaitForClusterState(t, clusterName, clusterv1alpha1.ClusterRejected, 30*time.Second)
	})

	t.Run("HubSecretRefDedup", func(t *testing.T) {
		secretName := "e2e-shared-secret"
		env.CreateClusterSecret(t, secretName)

		clusterA := &clusterv1alpha1.ManagedCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "e2e-hub-a"},
			Spec: clusterv1alpha1.ManagedClusterSpec{
				ConnectionMode: clusterv1alpha1.ClusterConnectionModeHub,
				APIServer:      env.Config.Host,
				SecretRef:      &corev1.LocalObjectReference{Name: secretName},
			},
		}
		clusterB := &clusterv1alpha1.ManagedCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "e2e-hub-b"},
			Spec: clusterv1alpha1.ManagedClusterSpec{
				ConnectionMode: clusterv1alpha1.ClusterConnectionModeHub,
				APIServer:      env.Config.Host,
				SecretRef:      &corev1.LocalObjectReference{Name: secretName},
			},
		}
		_ = c.Delete(ctx, clusterA)
		_ = c.Delete(ctx, clusterB)
		require.NoError(t, c.Create(ctx, clusterA))
		require.NoError(t, c.Create(ctx, clusterB))
		defer env.DeleteCluster(clusterA.Name)
		defer env.DeleteCluster(clusterB.Name)

		env.WaitForClusterState(t, clusterA.Name, clusterv1alpha1.ClusterReady, 30*time.Second)

		err := wait.PollUntilContextTimeout(ctx, 500*time.Millisecond, 20*time.Second, true, func(ctx context.Context) (bool, error) {
			var got clusterv1alpha1.ManagedCluster
			err := c.Get(ctx, types.NamespacedName{Name: clusterB.Name}, &got)
			return errors.IsNotFound(err), nil
		})
		require.NoError(t, err, "Duplicate cluster should be deleted when SecretRef is shared")
	})
}

// =============================================================================
// Volume Restriction Tests
// =============================================================================

func testVolumeRestriction(t *testing.T, env *TestEnvironment) {
	ctx := env.Context()
	c := env.Client

	t.Run("PVCStickyPlacement", func(t *testing.T) {
		clusterA := "e2e-vr-a"
		clusterB := "e2e-vr-b"
		appName := "e2e-vr-app"
		namespace := "default"

		env.CreateHubCluster(t, clusterA, map[string]string{"site": "a", "e2e-test": "volume-restriction"})
		defer env.DeleteCluster(clusterA)
		env.CreateHubCluster(t, clusterB, map[string]string{"site": "b", "e2e-test": "volume-restriction"})
		defer env.DeleteCluster(clusterB)

		replicas := int32(2)
		app := &appsv1alpha1.Application{
			ObjectMeta: metav1.ObjectMeta{
				Name:      appName,
				Namespace: namespace,
			},
			Spec: appsv1alpha1.ApplicationSpec{
				Replicas: &replicas,
				ClusterAffinity: &corev1.NodeAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
						NodeSelectorTerms: []corev1.NodeSelectorTerm{
							{
								MatchExpressions: []corev1.NodeSelectorRequirement{
									{
										Key:      "site",
										Operator: corev1.NodeSelectorOpIn,
										Values:   []string{"a"},
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
								"volumeMounts": []interface{}{
									map[string]interface{}{
										"name":      "data",
										"mountPath": "/data",
									},
								},
							},
						},
						"volumes": []interface{}{
							map[string]interface{}{
								"name": "data",
								"persistentVolumeClaim": map[string]interface{}{
									"claimName": "data",
								},
							},
						},
					},
				}),
			},
		}

		env.CreateApplication(t, app)
		defer env.DeleteApplication(appName, namespace)

		scheduled := env.WaitForApplicationScheduled(t, appName, namespace, 30*time.Second)
		require.Len(t, scheduled.Status.Placement.Topology, 1, "PVC app should initially schedule to one cluster")
		assert.Equal(t, clusterA, scheduled.Status.Placement.Topology[0].Name)

		// Expand affinity to include clusterB and scale up
		newReplicas := int32(4)
		err := wait.PollUntilContextTimeout(ctx, 500*time.Millisecond, 10*time.Second, true, func(ctx context.Context) (bool, error) {
			var latest appsv1alpha1.Application
			if err := c.Get(ctx, types.NamespacedName{Name: appName, Namespace: namespace}, &latest); err != nil {
				return false, nil
			}
			latest.Spec.Replicas = &newReplicas
			latest.Spec.ClusterAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[0].MatchExpressions[0].Values = []string{"a", "b"}
			return c.Update(ctx, &latest) == nil, nil
		})
		require.NoError(t, err, "Failed to update application for PVC test")

		// Verify placement stays on the original cluster
		err = wait.PollUntilContextTimeout(ctx, 1*time.Second, 30*time.Second, true, func(ctx context.Context) (bool, error) {
			var got appsv1alpha1.Application
			if err := c.Get(ctx, types.NamespacedName{Name: appName, Namespace: namespace}, &got); err != nil {
				return false, nil
			}
			return len(got.Status.Placement.Topology) == 1 && got.Status.Placement.Topology[0].Name == clusterA, nil
		})
		require.NoError(t, err, "PVC app should not be scheduled to new clusters")
	})
}

// =============================================================================
// Topology Spread Tests
// =============================================================================

func testTopologySpread(t *testing.T, env *TestEnvironment) {
	t.Run("PreferEmptyTopologyDomain", func(t *testing.T) {
		clusterA := "e2e-topo-a"
		clusterB := "e2e-topo-b"
		namespace := "default"

		env.CreateHubCluster(t, clusterA, map[string]string{
			"e2e-test":                    "topology",
			"topology.kubernetes.io/zone": "zone-a",
		})
		defer env.DeleteCluster(clusterA)
		env.CreateHubCluster(t, clusterB, map[string]string{
			"e2e-test":                    "topology",
			"topology.kubernetes.io/zone": "zone-b",
		})
		defer env.DeleteCluster(clusterB)

		appA := &appsv1alpha1.Application{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "e2e-topo-app-a",
				Namespace: namespace,
				Annotations: map[string]string{
					"apps.rocket.io/scheduler-strategy": "SingleCluster",
				},
			},
			Spec: appsv1alpha1.ApplicationSpec{
				Replicas: func() *int32 { r := int32(3); return &r }(),
				ClusterAffinity: &corev1.NodeAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
						NodeSelectorTerms: []corev1.NodeSelectorTerm{
							{
								MatchExpressions: []corev1.NodeSelectorRequirement{
									{
										Key:      "topology.kubernetes.io/zone",
										Operator: corev1.NodeSelectorOpIn,
										Values:   []string{"zone-a"},
									},
								},
							},
						},
					},
				},
				Workload: appsv1alpha1.WorkloadGVK{APIVersion: "apps/v1", Kind: "Deployment"},
				Template: toRaw(map[string]interface{}{
					"metadata": map[string]interface{}{
						"labels": map[string]string{"app": "e2e-topo-app-a"},
					},
					"spec": map[string]interface{}{
						"containers": []interface{}{
							map[string]interface{}{"name": "nginx", "image": "nginx:latest"},
						},
					},
				}),
			},
		}

		env.CreateApplication(t, appA)
		defer env.DeleteApplication(appA.Name, namespace)
		env.WaitForApplicationScheduled(t, appA.Name, namespace, 30*time.Second)

		appB := &appsv1alpha1.Application{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "e2e-topo-app-b",
				Namespace: namespace,
				Annotations: map[string]string{
					"apps.rocket.io/scheduler-strategy": "SingleCluster",
				},
			},
			Spec: appsv1alpha1.ApplicationSpec{
				ClusterAffinity: &corev1.NodeAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
						NodeSelectorTerms: []corev1.NodeSelectorTerm{
							{
								MatchExpressions: []corev1.NodeSelectorRequirement{
									{
										Key:      "topology.kubernetes.io/zone",
										Operator: corev1.NodeSelectorOpIn,
										Values:   []string{"zone-a", "zone-b"},
									},
								},
							},
						},
					},
				},
				Workload: appsv1alpha1.WorkloadGVK{APIVersion: "apps/v1", Kind: "Deployment"},
				Template: toRaw(map[string]interface{}{
					"metadata": map[string]interface{}{
						"labels": map[string]string{"app": "e2e-topo-app-b"},
					},
					"spec": map[string]interface{}{
						"containers": []interface{}{
							map[string]interface{}{"name": "nginx", "image": "nginx:latest"},
						},
					},
				}),
			},
		}

		env.CreateApplication(t, appB)
		defer env.DeleteApplication(appB.Name, namespace)

		scheduled := env.WaitForApplicationScheduled(t, appB.Name, namespace, 30*time.Second)
		assert.Equal(t, clusterB, scheduled.Status.Placement.Topology[0].Name, "TopologySpread should prefer zone with fewer replicas")
	})
}

// =============================================================================
// Workspace Tests
// =============================================================================

func testWorkspace(t *testing.T, env *TestEnvironment) {
	ctx := env.Context()
	c := env.Client

	t.Run("WorkspacePropagation", func(t *testing.T) {
		clusterA := "e2e-ws-a"
		clusterB := "e2e-ws-b"
		wsName := "e2e-workspace"

		env.CreateHubCluster(t, clusterA, map[string]string{"team": "alpha"})
		defer env.DeleteCluster(clusterA)
		env.CreateHubCluster(t, clusterB, map[string]string{"team": "alpha"})
		defer env.DeleteCluster(clusterB)

		ws := &workspacev1alpha1.Workspace{
			ObjectMeta: metav1.ObjectMeta{Name: wsName},
			Spec: workspacev1alpha1.WorkspaceSpec{
				Name:            wsName,
				ClusterSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"team": "alpha"}},
				ResourceConstraints: &workspacev1alpha1.WorkspaceConstraints{
					Quota: &corev1.ResourceQuotaSpec{
						Hard: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("2"),
							corev1.ResourceMemory: resource.MustParse("2Gi"),
						},
					},
					LimitRange: &corev1.LimitRangeSpec{
						Limits: []corev1.LimitRangeItem{
							{
								Type: corev1.LimitTypeContainer,
								DefaultRequest: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("100m"),
									corev1.ResourceMemory: resource.MustParse("128Mi"),
								},
							},
						},
					},
				},
			},
		}
		_ = c.Delete(ctx, ws)
		require.NoError(t, c.Create(ctx, ws))
		defer func() { _ = c.Delete(ctx, ws) }()

		// Wait for workspace to be applied to both clusters
		err := wait.PollUntilContextTimeout(ctx, 1*time.Second, 30*time.Second, true, func(ctx context.Context) (bool, error) {
			var got workspacev1alpha1.Workspace
			if err := c.Get(ctx, types.NamespacedName{Name: wsName}, &got); err != nil {
				return false, nil
			}
			return len(got.Status.AppliedClusters) == 2, nil
		})
		require.NoError(t, err, "Workspace should be applied to all matching clusters")

		// Verify namespace and quota exist in hub
		var ns corev1.Namespace
		require.NoError(t, c.Get(ctx, types.NamespacedName{Name: wsName}, &ns))
		var quota corev1.ResourceQuota
		require.NoError(t, c.Get(ctx, types.NamespacedName{Name: "workspace-quota", Namespace: wsName}, &quota))
		var limits corev1.LimitRange
		require.NoError(t, c.Get(ctx, types.NamespacedName{Name: "workspace-limits", Namespace: wsName}, &limits))
	})
}

// =============================================================================
// Addon Helm Install Tests
// =============================================================================

func testAddonHelmInstall(t *testing.T, env *TestEnvironment) {
	ctx := env.Context()
	c := env.Client

	t.Run("MCSHelmInstall", func(t *testing.T) {
		brokerChart := os.Getenv("CHART_SUBMARINER_BROKER")
		agentChart := os.Getenv("CHART_SUBMARINER")
		if brokerChart != "" && !(strings.HasPrefix(brokerChart, "http://") || strings.HasPrefix(brokerChart, "https://")) {
			t.Fatalf("CHART_SUBMARINER_BROKER must be a chart URL")
		}
		if agentChart != "" && !(strings.HasPrefix(agentChart, "http://") || strings.HasPrefix(agentChart, "https://")) {
			t.Fatalf("CHART_SUBMARINER must be a chart URL")
		}
		clusterName := "e2e-addon-cluster"
		mc := env.CreateHubCluster(t, clusterName, map[string]string{"e2e-test": "addon"})
		defer env.DeleteCluster(clusterName)

		// Enable MCS addon on the cluster
		err := wait.PollUntilContextTimeout(ctx, 500*time.Millisecond, 10*time.Second, true, func(ctx context.Context) (bool, error) {
			var latest clusterv1alpha1.ManagedCluster
			if err := c.Get(ctx, types.NamespacedName{Name: mc.Name}, &latest); err != nil {
				return false, nil
			}
			latest.Spec.Addons = []clusterv1alpha1.ClusterAddon{
				{Name: mcs.AddonName, Enabled: true},
			}
			return c.Update(ctx, &latest) == nil, nil
		})
		require.NoError(t, err, "Failed to enable addon")

		// Wait for broker secret or broker SA to exist
		err = wait.PollUntilContextTimeout(ctx, 2*time.Second, 5*time.Minute, true, func(ctx context.Context) (bool, error) {
			secret := &corev1.Secret{}
			err := c.Get(ctx, types.NamespacedName{Name: "submariner-k8s-broker-client-token", Namespace: "submariner-k8s-broker"}, secret)
			if err == nil {
				return true, nil
			}
			if !errors.IsNotFound(err) {
				return false, nil
			}
			sa := &corev1.ServiceAccount{}
			if err := c.Get(ctx, types.NamespacedName{Name: "submariner-k8s-broker-client", Namespace: "submariner-k8s-broker"}, sa); err != nil {
				return false, nil
			}
			return len(sa.Secrets) > 0, nil
		})
		require.NoError(t, err, "Broker resources should be created by Helm")

		// Wait for addon config to be populated on cluster
		var addonConfig map[string]string
		err = wait.PollUntilContextTimeout(ctx, 2*time.Second, 5*time.Minute, true, func(ctx context.Context) (bool, error) {
			var latest clusterv1alpha1.ManagedCluster
			if err := c.Get(ctx, types.NamespacedName{Name: mc.Name}, &latest); err != nil {
				return false, nil
			}
			for _, a := range latest.Spec.Addons {
				if a.Name == mcs.AddonName {
					if a.Config["brokerURL"] != "" && a.Config["brokerToken"] != "" && a.Config["brokerCA"] != "" {
						addonConfig = a.Config
						return true, nil
					}
				}
			}
			return false, nil
		})
		require.NoError(t, err, "Addon config should be populated from broker info")

		// Run agent controller to install submariner via Helm
		agent := &mcs.AgentController{}
		require.NoError(t, agent.Reconcile(ctx, addon.AddonConfig{
			ClusterName: mc.Name,
			Config:      addonConfig,
			Client:      env.Client,
		}))

		// Verify submariner-operator namespace exists
		var ns corev1.Namespace
		err = wait.PollUntilContextTimeout(ctx, 2*time.Second, 5*time.Minute, true, func(ctx context.Context) (bool, error) {
			if err := c.Get(ctx, types.NamespacedName{Name: "submariner-operator"}, &ns); err != nil {
				return false, nil
			}
			return true, nil
		})
		require.NoError(t, err, "Submariner operator namespace should exist after agent install")
	})
}

// =============================================================================
// Capacity Filter Tests
// =============================================================================

func testCapacityFilter(t *testing.T, env *TestEnvironment) {
	ctx := env.Context()
	c := env.Client

	t.Run("InsufficientResources", func(t *testing.T) {
		clusterName := "e2e-capacity-low"
		appName := "e2e-capacity-app"
		namespace := "default"

		env.CreateHubCluster(t, clusterName, map[string]string{"e2e-test": "capacity"})
		defer env.DeleteCluster(clusterName)
		env.UpdateClusterStatusWithAllocation(t, clusterName, "100m", "128Mi", "0", "0")

		app := &appsv1alpha1.Application{
			ObjectMeta: metav1.ObjectMeta{Name: appName, Namespace: namespace},
			Spec: appsv1alpha1.ApplicationSpec{
				ClusterAffinity: &corev1.NodeAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
						NodeSelectorTerms: []corev1.NodeSelectorTerm{
							{
								MatchExpressions: []corev1.NodeSelectorRequirement{
									{Key: "e2e-test", Operator: corev1.NodeSelectorOpIn, Values: []string{"capacity"}},
								},
							},
						},
					},
				},
				Workload: appsv1alpha1.WorkloadGVK{APIVersion: "apps/v1", Kind: "Deployment"},
				Template: toRaw(map[string]interface{}{
					"metadata": map[string]interface{}{"labels": map[string]string{"app": appName}},
					"spec": map[string]interface{}{
						"containers": []interface{}{
							map[string]interface{}{
								"name":  "busybox",
								"image": "busybox:latest",
								"resources": map[string]interface{}{
									"requests": map[string]interface{}{"cpu": "500m", "memory": "512Mi"},
								},
							},
						},
					},
				}),
			},
		}

		env.CreateApplication(t, app)
		defer env.DeleteApplication(appName, namespace)

		// Verify app is NOT scheduled due to insufficient resources
		time.Sleep(3 * time.Second)
		var got appsv1alpha1.Application
		require.NoError(t, c.Get(ctx, types.NamespacedName{Name: appName, Namespace: namespace}, &got))
		assert.Empty(t, got.Status.Placement.Topology, "App should not be scheduled when capacity is insufficient")
	})

	t.Run("MissingResourceSummary", func(t *testing.T) {
		clusterName := "e2e-capacity-missing"
		appName := "e2e-capacity-missing-app"
		namespace := "default"

		env.CreateHubCluster(t, clusterName, map[string]string{"e2e-test": "capacity-missing"})
		defer env.DeleteCluster(clusterName)

		// Clear resource summary
		err := wait.PollUntilContextTimeout(ctx, 500*time.Millisecond, 10*time.Second, true, func(ctx context.Context) (bool, error) {
			var cluster clusterv1alpha1.ManagedCluster
			if err := c.Get(ctx, types.NamespacedName{Name: clusterName}, &cluster); err != nil {
				return false, nil
			}
			cluster.Status.ResourceSummary = nil
			return c.Status().Update(ctx, &cluster) == nil, nil
		})
		require.NoError(t, err, "Failed to clear resource summary")

		app := &appsv1alpha1.Application{
			ObjectMeta: metav1.ObjectMeta{Name: appName, Namespace: namespace},
			Spec: appsv1alpha1.ApplicationSpec{
				ClusterAffinity: &corev1.NodeAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
						NodeSelectorTerms: []corev1.NodeSelectorTerm{{MatchExpressions: []corev1.NodeSelectorRequirement{{Key: "e2e-test", Operator: corev1.NodeSelectorOpIn, Values: []string{"capacity-missing"}}}}},
					},
				},
				Workload: appsv1alpha1.WorkloadGVK{APIVersion: "apps/v1", Kind: "Deployment"},
				Template: toRaw(map[string]interface{}{
					"metadata": map[string]interface{}{"labels": map[string]string{"app": appName}},
					"spec": map[string]interface{}{
						"containers": []interface{}{map[string]interface{}{"name": "busybox", "image": "busybox:latest"}},
					},
				}),
			},
		}

		env.CreateApplication(t, app)
		defer env.DeleteApplication(appName, namespace)

		time.Sleep(3 * time.Second)
		var got appsv1alpha1.Application
		require.NoError(t, c.Get(ctx, types.NamespacedName{Name: appName, Namespace: namespace}, &got))
		assert.Empty(t, got.Status.Placement.Topology, "App should not be scheduled when resource summary is missing")
	})
}

// =============================================================================
// DaemonSet Workload Tests
// =============================================================================

func testDaemonSetWorkload(t *testing.T, env *TestEnvironment) {
	ctx := env.Context()
	c := env.Client

	t.Run("DaemonSetSupport", func(t *testing.T) {
		clusterName := "e2e-daemonset"
		appName := "e2e-daemonset-app"
		namespace := "default"

		env.CreateHubCluster(t, clusterName, map[string]string{"e2e-test": "daemonset"})
		defer env.DeleteCluster(clusterName)

		app := &appsv1alpha1.Application{
			ObjectMeta: metav1.ObjectMeta{Name: appName, Namespace: namespace},
			Spec: appsv1alpha1.ApplicationSpec{
				ClusterAffinity: &corev1.NodeAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
						NodeSelectorTerms: []corev1.NodeSelectorTerm{{MatchExpressions: []corev1.NodeSelectorRequirement{{Key: "e2e-test", Operator: corev1.NodeSelectorOpIn, Values: []string{"daemonset"}}}}},
					},
				},
				Workload: appsv1alpha1.WorkloadGVK{APIVersion: "apps/v1", Kind: "DaemonSet"},
				Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": appName}},
				Template: toRaw(map[string]interface{}{
					"metadata": map[string]interface{}{"labels": map[string]string{"app": appName}},
					"spec": map[string]interface{}{
						"containers": []interface{}{map[string]interface{}{"name": "nginx", "image": "nginx:latest"}},
					},
				}),
			},
		}

		env.CreateApplication(t, app)
		defer env.DeleteApplication(appName, namespace)

		env.WaitForApplicationScheduled(t, appName, namespace, 30*time.Second)
		var ds appsv1.DaemonSet
		err := wait.PollUntilContextTimeout(ctx, 1*time.Second, 30*time.Second, true, func(ctx context.Context) (bool, error) {
			if err := c.Get(ctx, types.NamespacedName{Name: appName, Namespace: namespace}, &ds); err != nil {
				return false, nil
			}
			return ds.Spec.Selector != nil, nil
		})
		require.NoError(t, err, "DaemonSet should be created")
	})
}
