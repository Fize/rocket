//go:build e2e

package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/fize/rocket/internal/addon/victoriametrics"
	clusterv1alpha1 "github.com/fize/rocket/pkg/apis/storage/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
)

// testVictoriaMetricsAddon tests VictoriaMetrics addon functionality
func testVictoriaMetricsAddon(t *testing.T, env *TestEnvironment) {
	ctx := env.Context()
	c := env.Client

	// Cleanup VictoriaMetrics namespace from previous tests
	_ = c.Delete(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: victoriametrics.VictoriaMetricsNamespace,
		},
	})
	time.Sleep(2 * time.Second)

	t.Run("HubClusterVMOnly", func(t *testing.T) {
		// Create a Hub cluster with VM enabled (no vmagent)
		clusterName := "e2e-hub-vm-only"
		mc := env.CreateHubCluster(t, clusterName, map[string]string{"e2e-test": "vm-hub"})
		defer env.DeleteCluster(clusterName)

		// Enable VictoriaMetrics on Hub cluster
		err := wait.PollUntilContextTimeout(ctx, 500*time.Millisecond, 10*time.Second, true, func(ctx context.Context) (bool, error) {
			var latest clusterv1alpha1.ManagedCluster
			if err := c.Get(ctx, types.NamespacedName{Name: mc.Name}, &latest); err != nil {
				return false, nil
			}
			latest.Spec.Addons = []clusterv1alpha1.ClusterAddon{
				{
					Name:    victoriametrics.AddonName,
					Enabled: true,
				},
			}
			return c.Update(ctx, &latest) == nil, nil
		})
		require.NoError(t, err, "Failed to enable VictoriaMetrics addon")

		// Wait for VictoriaMetrics Deployment to be created and ready
		err = wait.PollUntilContextTimeout(ctx, 2*time.Second, 5*time.Minute, true, func(ctx context.Context) (bool, error) {
			deploy := &appsv1.Deployment{}
			if err := c.Get(ctx, types.NamespacedName{Name: victoriametrics.VictoriaMetricsServiceName, Namespace: victoriametrics.VictoriaMetricsNamespace}, deploy); err != nil {
				if errors.IsNotFound(err) {
					return false, nil
				}
				return false, err
			}
			// Check if deployment is ready
			return deploy.Status.ReadyReplicas > 0, nil
		})
		require.NoError(t, err, "VictoriaMetrics should be running on Hub cluster")

		// Verify vmagent is NOT deployed on Hub cluster
		vmagentDeploy := &appsv1.Deployment{}
		err = c.Get(ctx, types.NamespacedName{Name: "vm-agent-victoria-metrics-agent", Namespace: victoriametrics.VmAgentNamespace}, vmagentDeploy)
		assert.True(t, errors.IsNotFound(err), "vmagent should NOT be deployed on Hub cluster")
	})

	t.Run("EdgeClusterWithVmAgent", func(t *testing.T) {
		// Create a Hub cluster first to get VM URL
		hubClusterName := "e2e-hub-for-edge"
		hubMC := env.CreateHubCluster(t, hubClusterName, map[string]string{"e2e-test": "vm-hub-edge"})
		defer env.DeleteCluster(hubClusterName)

		// Enable VictoriaMetrics on Hub cluster
		err := wait.PollUntilContextTimeout(ctx, 500*time.Millisecond, 10*time.Second, true, func(ctx context.Context) (bool, error) {
			var latest clusterv1alpha1.ManagedCluster
			if err := c.Get(ctx, types.NamespacedName{Name: hubMC.Name}, &latest); err != nil {
				return false, nil
			}
			latest.Spec.Addons = []clusterv1alpha1.ClusterAddon{
				{
					Name:    victoriametrics.AddonName,
					Enabled: true,
				},
			}
			return c.Update(ctx, &latest) == nil, nil
		})
		require.NoError(t, err, "Failed to enable VictoriaMetrics on Hub")

		// Wait for addon config to be populated with VM URL
		var vmURL string
		err = wait.PollUntilContextTimeout(ctx, 2*time.Second, 3*time.Minute, true, func(ctx context.Context) (bool, error) {
			var latest clusterv1alpha1.ManagedCluster
			if err := c.Get(ctx, types.NamespacedName{Name: hubMC.Name}, &latest); err != nil {
				return false, nil
			}
			for _, addon := range latest.Spec.Addons {
				if addon.Name == victoriametrics.AddonName {
					vmURL = addon.Config[victoriametrics.ConfigVictoriaMetricsURL]
					return vmURL != "", nil
				}
			}
			return false, nil
		})
		require.NoError(t, err, "VictoriaMetrics URL should be populated")
		assert.NotEmpty(t, vmURL, "VM URL should not be empty")

		// Create an Edge cluster with vmagent enabled
		edgeClusterName := "e2e-edge-vm"
		edgeMC, _ := env.CreateEdgeCluster(t, edgeClusterName, map[string]string{"e2e-test": "vm-edge"})
		defer env.DeleteCluster(edgeClusterName)

		// Enable vmagent on Edge cluster with Hub's VM URL
		err = wait.PollUntilContextTimeout(ctx, 500*time.Millisecond, 10*time.Second, true, func(ctx context.Context) (bool, error) {
			var latest clusterv1alpha1.ManagedCluster
			if err := c.Get(ctx, types.NamespacedName{Name: edgeMC.Name}, &latest); err != nil {
				return false, nil
			}
			latest.Spec.Addons = []clusterv1alpha1.ClusterAddon{
				{
					Name:    victoriametrics.AddonName,
					Enabled: true,
					Config: map[string]string{
						victoriametrics.ConfigVictoriaMetricsURL: vmURL,
					},
				},
			}
			return c.Update(ctx, &latest) == nil, nil
		})
		require.NoError(t, err, "Failed to enable vmagent on Edge cluster")

		// Wait for vmagent Deployment to be created and ready
		err = wait.PollUntilContextTimeout(ctx, 2*time.Second, 5*time.Minute, true, func(ctx context.Context) (bool, error) {
			deploy := &appsv1.Deployment{}
			if err := c.Get(ctx, types.NamespacedName{Name: "vm-agent-victoria-metrics-agent", Namespace: victoriametrics.VmAgentNamespace}, deploy); err != nil {
				if errors.IsNotFound(err) {
					return false, nil
				}
				return false, err
			}
			// Check if deployment is ready
			return deploy.Status.ReadyReplicas > 0, nil
		})
		require.NoError(t, err, "vmagent should be running on Edge cluster")

		// Verify VictoriaMetrics single is NOT deployed on Edge cluster
		vmDeploy := &appsv1.Deployment{}
		err = c.Get(ctx, types.NamespacedName{Name: victoriametrics.VictoriaMetricsServiceName, Namespace: victoriametrics.VictoriaMetricsNamespace}, vmDeploy)
		assert.True(t, errors.IsNotFound(err), "VictoriaMetrics single should NOT be deployed on Edge cluster")
	})

	t.Run("DisableAndReenable", func(t *testing.T) {
		// Test disabling and re-enabling the addon
		clusterName := "e2e-vm-disable"
		mc := env.CreateHubCluster(t, clusterName, map[string]string{"e2e-test": "vm-disable"})
		defer env.DeleteCluster(clusterName)

		// Enable VictoriaMetrics
		err := wait.PollUntilContextTimeout(ctx, 500*time.Millisecond, 10*time.Second, true, func(ctx context.Context) (bool, error) {
			var latest clusterv1alpha1.ManagedCluster
			if err := c.Get(ctx, types.NamespacedName{Name: mc.Name}, &latest); err != nil {
				return false, nil
			}
			latest.Spec.Addons = []clusterv1alpha1.ClusterAddon{
				{
					Name:    victoriametrics.AddonName,
					Enabled: true,
				},
			}
			return c.Update(ctx, &latest) == nil, nil
		})
		require.NoError(t, err, "Failed to enable addon")

		// Wait for deployment to be ready
		err = wait.PollUntilContextTimeout(ctx, 2*time.Second, 3*time.Minute, true, func(ctx context.Context) (bool, error) {
			deploy := &appsv1.Deployment{}
			if err := c.Get(ctx, types.NamespacedName{Name: victoriametrics.VictoriaMetricsServiceName, Namespace: victoriametrics.VictoriaMetricsNamespace}, deploy); err != nil {
				if errors.IsNotFound(err) {
					return false, nil
				}
				return false, err
			}
			return deploy.Status.ReadyReplicas > 0, nil
		})
		require.NoError(t, err, "VictoriaMetrics should be running")

		// Disable the addon
		err = wait.PollUntilContextTimeout(ctx, 500*time.Millisecond, 10*time.Second, true, func(ctx context.Context) (bool, error) {
			var latest clusterv1alpha1.ManagedCluster
			if err := c.Get(ctx, types.NamespacedName{Name: mc.Name}, &latest); err != nil {
				return false, nil
			}
			for i, addon := range latest.Spec.Addons {
				if addon.Name == victoriametrics.AddonName {
					latest.Spec.Addons[i].Enabled = false
				}
			}
			return c.Update(ctx, &latest) == nil, nil
		})
		require.NoError(t, err, "Failed to disable addon")

		// Wait for namespace to be deleted (cleanup)
		err = wait.PollUntilContextTimeout(ctx, 2*time.Second, 2*time.Minute, true, func(ctx context.Context) (bool, error) {
			ns := &corev1.Namespace{}
			err := c.Get(ctx, types.NamespacedName{Name: victoriametrics.VictoriaMetricsNamespace}, ns)
			if errors.IsNotFound(err) {
				return true, nil
			}
			return false, err
		})
		require.NoError(t, err, "VictoriaMetrics namespace should be deleted after disabling")

		// Re-enable the addon
		err = wait.PollUntilContextTimeout(ctx, 500*time.Millisecond, 10*time.Second, true, func(ctx context.Context) (bool, error) {
			var latest clusterv1alpha1.ManagedCluster
			if err := c.Get(ctx, types.NamespacedName{Name: mc.Name}, &latest); err != nil {
				return false, nil
			}
			for i, addon := range latest.Spec.Addons {
				if addon.Name == victoriametrics.AddonName {
					latest.Spec.Addons[i].Enabled = true
				}
			}
			return c.Update(ctx, &latest) == nil, nil
		})
		require.NoError(t, err, "Failed to re-enable addon")

		// Wait for deployment to be ready again
		err = wait.PollUntilContextTimeout(ctx, 2*time.Second, 3*time.Minute, true, func(ctx context.Context) (bool, error) {
			deploy := &appsv1.Deployment{}
			if err := c.Get(ctx, types.NamespacedName{Name: victoriametrics.VictoriaMetricsServiceName, Namespace: victoriametrics.VictoriaMetricsNamespace}, deploy); err != nil {
				if errors.IsNotFound(err) {
					return false, nil
				}
				return false, err
			}
			return deploy.Status.ReadyReplicas > 0, nil
		})
		require.NoError(t, err, "VictoriaMetrics should be running again after re-enabling")
	})

	t.Run("CustomChartVersion", func(t *testing.T) {
		clusterName := "e2e-vm-custom-version"
		mc := env.CreateHubCluster(t, clusterName, map[string]string{"e2e-test": "vm-version"})
		defer env.DeleteCluster(clusterName)

		// Enable VictoriaMetrics with custom chart version
		customVersion := "0.1.0"
		err := wait.PollUntilContextTimeout(ctx, 500*time.Millisecond, 10*time.Second, true, func(ctx context.Context) (bool, error) {
			var latest clusterv1alpha1.ManagedCluster
			if err := c.Get(ctx, types.NamespacedName{Name: mc.Name}, &latest); err != nil {
				return false, nil
			}
			latest.Spec.Addons = []clusterv1alpha1.ClusterAddon{
				{
					Name:    victoriametrics.AddonName,
					Enabled: true,
					Config: map[string]string{
						victoriametrics.ConfigVMChartVersion: customVersion,
					},
				},
			}
			return c.Update(ctx, &latest) == nil, nil
		})
		require.NoError(t, err, "Failed to enable addon with custom version")

		// Wait for VictoriaMetrics to be ready
		err = wait.PollUntilContextTimeout(ctx, 2*time.Second, 3*time.Minute, true, func(ctx context.Context) (bool, error) {
			deploy := &appsv1.Deployment{}
			if err := c.Get(ctx, types.NamespacedName{Name: victoriametrics.VictoriaMetricsServiceName, Namespace: victoriametrics.VictoriaMetricsNamespace}, deploy); err != nil {
				if errors.IsNotFound(err) {
					return false, nil
				}
				return false, err
			}
			return deploy.Status.ReadyReplicas > 0, nil
		})
		require.NoError(t, err, "VictoriaMetrics should be running with custom version")
	})

	t.Run("MultipleEdgeClusters", func(t *testing.T) {
		// Test one Hub cluster with multiple Edge clusters
		hubClusterName := "e2e-hub-multi"
		hubMC := env.CreateHubCluster(t, hubClusterName, map[string]string{"e2e-test": "vm-multi-hub"})
		defer env.DeleteCluster(hubClusterName)

		// Enable VictoriaMetrics on Hub cluster
		err := wait.PollUntilContextTimeout(ctx, 500*time.Millisecond, 10*time.Second, true, func(ctx context.Context) (bool, error) {
			var latest clusterv1alpha1.ManagedCluster
			if err := c.Get(ctx, types.NamespacedName{Name: hubMC.Name}, &latest); err != nil {
				return false, nil
			}
			latest.Spec.Addons = []clusterv1alpha1.ClusterAddon{
				{
					Name:    victoriametrics.AddonName,
					Enabled: true,
				},
			}
			return c.Update(ctx, &latest) == nil, nil
		})
		require.NoError(t, err, "Failed to enable addon on Hub")

		// Wait for VM URL
		var vmURL string
		err = wait.PollUntilContextTimeout(ctx, 2*time.Second, 3*time.Minute, true, func(ctx context.Context) (bool, error) {
			var latest clusterv1alpha1.ManagedCluster
			if err := c.Get(ctx, types.NamespacedName{Name: hubMC.Name}, &latest); err != nil {
				return false, nil
			}
			for _, addon := range latest.Spec.Addons {
				if addon.Name == victoriametrics.AddonName {
					vmURL = addon.Config[victoriametrics.ConfigVictoriaMetricsURL]
					return vmURL != "", nil
				}
			}
			return false, nil
		})
		require.NoError(t, err, "VM URL should be available")

		// Create two Edge clusters
		edge1Name := "e2e-edge-multi-1"
		edge1MC, _ := env.CreateEdgeCluster(t, edge1Name, map[string]string{"e2e-test": "vm-multi-edge"})
		defer env.DeleteCluster(edge1Name)

		edge2Name := "e2e-edge-multi-2"
		edge2MC, _ := env.CreateEdgeCluster(t, edge2Name, map[string]string{"e2e-test": "vm-multi-edge"})
		defer env.DeleteCluster(edge2Name)

		// Enable vmagent on both Edge clusters
		for _, clusterName := range []string{edge1MC.Name, edge2MC.Name} {
			err := wait.PollUntilContextTimeout(ctx, 500*time.Millisecond, 10*time.Second, true, func(ctx context.Context) (bool, error) {
				var latest clusterv1alpha1.ManagedCluster
				if err := c.Get(ctx, types.NamespacedName{Name: clusterName}, &latest); err != nil {
					return false, nil
				}
				latest.Spec.Addons = []clusterv1alpha1.ClusterAddon{
					{
						Name:    victoriametrics.AddonName,
						Enabled: true,
						Config: map[string]string{
							victoriametrics.ConfigVictoriaMetricsURL: vmURL,
						},
					},
				}
				return c.Update(ctx, &latest) == nil, nil
			})
			require.NoError(t, err, "Failed to enable vmagent on Edge cluster %s", clusterName)
		}

		// Verify vmagent is running on both Edge clusters
		for _, clusterName := range []string{edge1MC.Name, edge2MC.Name} {
			err := wait.PollUntilContextTimeout(ctx, 2*time.Second, 3*time.Minute, true, func(ctx context.Context) (bool, error) {
				deploy := &appsv1.Deployment{}
				if err := c.Get(ctx, types.NamespacedName{Name: "vm-agent-victoria-metrics-agent", Namespace: victoriametrics.VmAgentNamespace}, deploy); err != nil {
					if errors.IsNotFound(err) {
						return false, nil
					}
					return false, err
				}
				return deploy.Status.ReadyReplicas > 0, nil
			})
			require.NoError(t, err, "vmagent should be running on Edge cluster %s", clusterName)
		}
	})
}
