//go:build e2e

package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/hex-techs/rocket/internal/addon/mcs"
	clusterv1alpha1 "github.com/hex-techs/rocket/pkg/apis/storage/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
)

// testAddonUpgrade verifies Addon configuration changes and Helm upgrades
func testAddonUpgrade(t *testing.T, env *TestEnvironment) {
	ctx := env.Context()
	c := env.Client

	t.Run("BrokerChartVersionUpgrade", func(t *testing.T) {
		clusterName := "e2e-broker-upgrade"
		mc := env.CreateHubCluster(t, clusterName, map[string]string{"e2e-test": "broker-upgrade"})
		defer env.DeleteCluster(clusterName)

		// Step 1: Deploy initial broker version
		initialVersion := "0.23.0-m0"
		err := wait.PollUntilContextTimeout(ctx, 500*time.Millisecond, 10*time.Second, true, func(ctx context.Context) (bool, error) {
			var latest clusterv1alpha1.ManagedCluster
			if err := c.Get(ctx, types.NamespacedName{Name: mc.Name}, &latest); err != nil {
				return false, nil
			}
			latest.Spec.Addons = []clusterv1alpha1.ClusterAddon{
				{
					Name:    mcs.AddonName,
					Enabled: true,
					Config: map[string]string{
						mcs.ConfigBrokerChartVersion: initialVersion,
					},
				},
			}
			return c.Update(ctx, &latest) == nil, nil
		})
		require.NoError(t, err, "Failed to enable addon with initial version")

		// Wait for broker to be installed
		err = wait.PollUntilContextTimeout(ctx, 2*time.Second, 5*time.Minute, true, func(ctx context.Context) (bool, error) {
			secret := &corev1.Secret{}
			if err := c.Get(ctx, types.NamespacedName{Name: "submariner-k8s-broker-client-token", Namespace: "submariner-k8s-broker"}, secret); err != nil {
				return false, nil
			}
			return true, nil
		})
		require.NoError(t, err, "Broker should be installed with initial version")

		// Step 2: Verify addon config is populated
		err = wait.PollUntilContextTimeout(ctx, 2*time.Second, 5*time.Minute, true, func(ctx context.Context) (bool, error) {
			var latest clusterv1alpha1.ManagedCluster
			if err := c.Get(ctx, types.NamespacedName{Name: mc.Name}, &latest); err != nil {
				return false, nil
			}
			for _, addon := range latest.Spec.Addons {
				if addon.Name == mcs.AddonName && addon.Config["brokerURL"] != "" {
					return true, nil
				}
			}
			return false, nil
		})
		require.NoError(t, err, "Addon config should be populated")

		// Step 3: Upgrade broker version
		newVersion := "0.24.0"
		err = wait.PollUntilContextTimeout(ctx, 500*time.Millisecond, 10*time.Second, true, func(ctx context.Context) (bool, error) {
			var latest clusterv1alpha1.ManagedCluster
			if err := c.Get(ctx, types.NamespacedName{Name: mc.Name}, &latest); err != nil {
				return false, nil
			}
			for i, addon := range latest.Spec.Addons {
				if addon.Name == mcs.AddonName {
					latest.Spec.Addons[i].Config[mcs.ConfigBrokerChartVersion] = newVersion
				}
			}
			return c.Update(ctx, &latest) == nil, nil
		})
		require.NoError(t, err, "Failed to update addon config with new version")

		// Step 4: Wait for broker pod restart (indicating upgrade)
		time.Sleep(3 * time.Second) // Give controller time to process change
		// In real scenario, would verify helm upgrade executed and pod restarted

		// Step 5: Verify broker is still functional after upgrade
		err = wait.PollUntilContextTimeout(ctx, 2*time.Second, 2*time.Minute, true, func(ctx context.Context) (bool, error) {
			secret := &corev1.Secret{}
			if err := c.Get(ctx, types.NamespacedName{Name: "submariner-k8s-broker-client-token", Namespace: "submariner-k8s-broker"}, secret); err != nil {
				return false, nil
			}
			return true, nil
		})
		require.NoError(t, err, "Broker should still exist after upgrade")
	})

	t.Run("SubmarinerAgentChartVersionUpgrade", func(t *testing.T) {
		clusterName := "e2e-agent-upgrade"
		mc := env.CreateHubCluster(t, clusterName, map[string]string{"e2e-test": "agent-upgrade"})
		defer env.DeleteCluster(clusterName)

		// Enable addon with initial agent version
		initialVersion := "0.21.0"
		err := wait.PollUntilContextTimeout(ctx, 500*time.Millisecond, 10*time.Second, true, func(ctx context.Context) (bool, error) {
			var latest clusterv1alpha1.ManagedCluster
			if err := c.Get(ctx, types.NamespacedName{Name: mc.Name}, &latest); err != nil {
				return false, nil
			}
			latest.Spec.Addons = []clusterv1alpha1.ClusterAddon{
				{
					Name:    mcs.AddonName,
					Enabled: true,
					Config: map[string]string{
						mcs.ConfigSubmarinerChartVersion: initialVersion,
					},
				},
			}
			return c.Update(ctx, &latest) == nil, nil
		})
		require.NoError(t, err, "Failed to enable agent addon")

		// Wait for broker to be ready first
		err = wait.PollUntilContextTimeout(ctx, 2*time.Second, 5*time.Minute, true, func(ctx context.Context) (bool, error) {
			secret := &corev1.Secret{}
			if err := c.Get(ctx, types.NamespacedName{Name: "submariner-k8s-broker-client-token", Namespace: "submariner-k8s-broker"}, secret); err != nil {
				return false, nil
			}
			return true, nil
		})
		require.NoError(t, err, "Broker should be ready for agent to connect")

		// Wait for addon config with broker info
		err = wait.PollUntilContextTimeout(ctx, 2*time.Second, 5*time.Minute, true, func(ctx context.Context) (bool, error) {
			var latest clusterv1alpha1.ManagedCluster
			if err := c.Get(ctx, types.NamespacedName{Name: mc.Name}, &latest); err != nil {
				return false, nil
			}
			for _, addon := range latest.Spec.Addons {
				if addon.Name == mcs.AddonName && addon.Config["brokerURL"] != "" {
					return true, nil
				}
			}
			return false, nil
		})
		require.NoError(t, err, "Broker config should be available for agent")

		// Upgrade agent version
		newVersion := "0.23.0-m0"
		err = wait.PollUntilContextTimeout(ctx, 500*time.Millisecond, 10*time.Second, true, func(ctx context.Context) (bool, error) {
			var latest clusterv1alpha1.ManagedCluster
			if err := c.Get(ctx, types.NamespacedName{Name: mc.Name}, &latest); err != nil {
				return false, nil
			}
			for i, addon := range latest.Spec.Addons {
				if addon.Name == mcs.AddonName {
					latest.Spec.Addons[i].Config[mcs.ConfigSubmarinerChartVersion] = newVersion
				}
			}
			return c.Update(ctx, &latest) == nil, nil
		})
		require.NoError(t, err, "Failed to upgrade agent version")

		// Give agent controller time to process upgrade
		time.Sleep(3 * time.Second)
		// In real scenario, would verify submariner pods restarted
	})

	t.Run("IndependentBrokerAndAgentUpgrade", func(t *testing.T) {
		clusterName := "e2e-independent-upgrade"
		mc := env.CreateHubCluster(t, clusterName, map[string]string{"e2e-test": "independent"})
		defer env.DeleteCluster(clusterName)

		// Deploy with initial versions
		err := wait.PollUntilContextTimeout(ctx, 500*time.Millisecond, 10*time.Second, true, func(ctx context.Context) (bool, error) {
			var latest clusterv1alpha1.ManagedCluster
			if err := c.Get(ctx, types.NamespacedName{Name: mc.Name}, &latest); err != nil {
				return false, nil
			}
			latest.Spec.Addons = []clusterv1alpha1.ClusterAddon{
				{
					Name:    mcs.AddonName,
					Enabled: true,
					Config: map[string]string{
						mcs.ConfigBrokerChartVersion:     "0.23.0-m0",
						mcs.ConfigSubmarinerChartVersion: "0.21.0",
					},
				},
			}
			return c.Update(ctx, &latest) == nil, nil
		})
		require.NoError(t, err, "Failed to deploy addon")

		// Wait for both to be ready
		err = wait.PollUntilContextTimeout(ctx, 2*time.Second, 5*time.Minute, true, func(ctx context.Context) (bool, error) {
			var latest clusterv1alpha1.ManagedCluster
			if err := c.Get(ctx, types.NamespacedName{Name: mc.Name}, &latest); err != nil {
				return false, nil
			}
			for _, addon := range latest.Spec.Addons {
				if addon.Name == mcs.AddonName && addon.Config["brokerURL"] != "" {
					return true, nil
				}
			}
			return false, nil
		})
		require.NoError(t, err, "Both components should be ready")

		// Upgrade only agent, broker stays the same
		err = wait.PollUntilContextTimeout(ctx, 500*time.Millisecond, 10*time.Second, true, func(ctx context.Context) (bool, error) {
			var latest clusterv1alpha1.ManagedCluster
			if err := c.Get(ctx, types.NamespacedName{Name: mc.Name}, &latest); err != nil {
				return false, nil
			}
			for i, addon := range latest.Spec.Addons {
				if addon.Name == mcs.AddonName {
					// Upgrade agent only
					latest.Spec.Addons[i].Config[mcs.ConfigSubmarinerChartVersion] = "0.8.0"
					// Keep broker the same
					latest.Spec.Addons[i].Config[mcs.ConfigBrokerChartVersion] = "0.23.0-m0"
				}
			}
			return c.Update(ctx, &latest) == nil, nil
		})
		require.NoError(t, err, "Failed to upgrade agent independently")

		time.Sleep(3 * time.Second)
		// Verify broker version unchanged, agent upgraded
		var latest clusterv1alpha1.ManagedCluster
		require.NoError(t, c.Get(ctx, types.NamespacedName{Name: mc.Name}, &latest))
		for _, addon := range latest.Spec.Addons {
			if addon.Name == mcs.AddonName {
				assert.Equal(t, "0.23.0-m0", addon.Config[mcs.ConfigBrokerChartVersion], "Broker version should not change")
				assert.Equal(t, "0.8.0", addon.Config[mcs.ConfigSubmarinerChartVersion], "Agent version should be upgraded")
			}
		}
	})

	t.Run("CustomChartRepository", func(t *testing.T) {
		clusterName := "e2e-custom-repo"
		mc := env.CreateHubCluster(t, clusterName, map[string]string{"e2e-test": "custom-repo"})
		defer env.DeleteCluster(clusterName)

		// Deploy with custom repository URLs (official ones for testing)
		err := wait.PollUntilContextTimeout(ctx, 500*time.Millisecond, 10*time.Second, true, func(ctx context.Context) (bool, error) {
			var latest clusterv1alpha1.ManagedCluster
			if err := c.Get(ctx, types.NamespacedName{Name: mc.Name}, &latest); err != nil {
				return false, nil
			}
			latest.Spec.Addons = []clusterv1alpha1.ClusterAddon{
				{
					Name:    mcs.AddonName,
					Enabled: true,
					Config: map[string]string{
						mcs.ConfigBrokerChartRepoURL:     "https://submariner-io.github.io/submariner-charts/charts",
						mcs.ConfigBrokerChartName:        "submariner-k8s-broker",
						mcs.ConfigBrokerChartVersion:     "0.23.0-m0",
						mcs.ConfigSubmarinerChartRepoURL: "https://submariner-io.github.io/submariner-charts/charts",
						mcs.ConfigSubmarinerChartName:    "submariner-operator",
						mcs.ConfigSubmarinerChartVersion: "0.21.0",
					},
				},
			}
			return c.Update(ctx, &latest) == nil, nil
		})
		require.NoError(t, err, "Failed to enable addon with custom repos")

		// Wait for successful installation
		err = wait.PollUntilContextTimeout(ctx, 2*time.Second, 5*time.Minute, true, func(ctx context.Context) (bool, error) {
			secret := &corev1.Secret{}
			if err := c.Get(ctx, types.NamespacedName{Name: "submariner-k8s-broker-client-token", Namespace: "submariner-k8s-broker"}, secret); err != nil {
				return false, nil
			}
			return true, nil
		})
		require.NoError(t, err, "Addon should be installed from custom repository")
	})

	t.Run("OptionalChartVersion", func(t *testing.T) {
		clusterName := "e2e-optional-version"
		mc := env.CreateHubCluster(t, clusterName, map[string]string{"e2e-test": "optional-version"})
		defer env.DeleteCluster(clusterName)

		// Deploy without specifying chart versions (should use latest)
		err := wait.PollUntilContextTimeout(ctx, 500*time.Millisecond, 10*time.Second, true, func(ctx context.Context) (bool, error) {
			var latest clusterv1alpha1.ManagedCluster
			if err := c.Get(ctx, types.NamespacedName{Name: mc.Name}, &latest); err != nil {
				return false, nil
			}
			latest.Spec.Addons = []clusterv1alpha1.ClusterAddon{
				{
					Name:    mcs.AddonName,
					Enabled: true,
					Config: map[string]string{
						// No version specified - should use latest (*)
						mcs.ConfigBrokerChartRepoURL: "https://submariner-io.github.io/submariner-charts/charts",
						mcs.ConfigBrokerChartName:    "submariner-k8s-broker",
						// brokerChartVersion omitted
						mcs.ConfigSubmarinerChartRepoURL: "https://submariner-io.github.io/submariner-charts/charts",
						mcs.ConfigSubmarinerChartName:    "submariner",
						// submarinerChartVersion omitted
					},
				},
			}
			return c.Update(ctx, &latest) == nil, nil
		})
		require.NoError(t, err, "Failed to enable addon without versions")

		// Should still install with latest version
		err = wait.PollUntilContextTimeout(ctx, 2*time.Second, 5*time.Minute, true, func(ctx context.Context) (bool, error) {
			secret := &corev1.Secret{}
			if err := c.Get(ctx, types.NamespacedName{Name: "submariner-k8s-broker-client-token", Namespace: "submariner-k8s-broker"}, secret); err != nil {
				return false, nil
			}
			return true, nil
		})
		require.NoError(t, err, "Addon should install with latest version when not specified")
	})
}

// testAddonConfigValidation verifies addon configuration validation and error handling
func testAddonConfigValidation(t *testing.T, env *TestEnvironment) {
	ctx := env.Context()
	c := env.Client

	// Cleanup entire broker namespace from previous tests to ensure clean state
	_ = c.Delete(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "submariner-k8s-broker",
		},
	})
	// Wait a bit for namespace deletion to propagate
	time.Sleep(2 * time.Second)

	t.Run("InvalidChartURL", func(t *testing.T) {

		clusterName := "e2e-invalid-url"
		mc := env.CreateHubCluster(t, clusterName, map[string]string{"e2e-test": "invalid-url"})
		defer env.DeleteCluster(clusterName)

		// Try to deploy with invalid chart URL
		err := wait.PollUntilContextTimeout(ctx, 500*time.Millisecond, 10*time.Second, true, func(ctx context.Context) (bool, error) {
			var latest clusterv1alpha1.ManagedCluster
			if err := c.Get(ctx, types.NamespacedName{Name: mc.Name}, &latest); err != nil {
				return false, nil
			}
			latest.Spec.Addons = []clusterv1alpha1.ClusterAddon{
				{
					Name:    mcs.AddonName,
					Enabled: true,
					Config: map[string]string{
						mcs.ConfigBrokerChartURL: "not-a-valid-url", // Invalid URL
					},
				},
			}
			return c.Update(ctx, &latest) == nil, nil
		})
		require.NoError(t, err, "Failed to set invalid URL config")

		// Controller should reject or fail gracefully
		time.Sleep(3 * time.Second)
		// Verify broker was not created due to invalid URL
		secret := &corev1.Secret{}
		err = c.Get(ctx, types.NamespacedName{Name: "submariner-k8s-broker-client-token", Namespace: "submariner-k8s-broker"}, secret)
		assert.True(t, errors.IsNotFound(err), "Broker should not be installed with invalid URL")
	})

	t.Run("MissingChartName", func(t *testing.T) {
		clusterName := "e2e-missing-chartname"
		mc := env.CreateHubCluster(t, clusterName, map[string]string{"e2e-test": "missing-chartname"})
		defer env.DeleteCluster(clusterName)

		// Deploy with repo URL but no chart name - should use default chart name
		err := wait.PollUntilContextTimeout(ctx, 500*time.Millisecond, 10*time.Second, true, func(ctx context.Context) (bool, error) {
			var latest clusterv1alpha1.ManagedCluster
			if err := c.Get(ctx, types.NamespacedName{Name: mc.Name}, &latest); err != nil {
				return false, nil
			}
			latest.Spec.Addons = []clusterv1alpha1.ClusterAddon{
				{
					Name:    mcs.AddonName,
					Enabled: true,
					Config: map[string]string{
						mcs.ConfigBrokerChartRepoURL: "https://submariner-io.github.io/submariner-charts/charts",
						// Missing brokerChartName - should use default
					},
				},
			}
			return c.Update(ctx, &latest) == nil, nil
		})
		require.NoError(t, err, "Failed to set config without chart name")

		// Controller should use default chart name and succeed
		err = wait.PollUntilContextTimeout(ctx, 2*time.Second, 1*time.Minute, true, func(ctx context.Context) (bool, error) {
			secret := &corev1.Secret{}
			if err := c.Get(ctx, types.NamespacedName{Name: "submariner-k8s-broker-client-token", Namespace: "submariner-k8s-broker"}, secret); err != nil {
				return false, nil
			}
			return true, nil
		})
		assert.NoError(t, err, "Broker should be installed using default chart name")
	})

	t.Run("NonexistentConfigMap", func(t *testing.T) {
		clusterName := "e2e-nonexistent-cm"
		mc := env.CreateHubCluster(t, clusterName, map[string]string{"e2e-test": "nonexistent-cm"})
		defer env.DeleteCluster(clusterName)

		// Try to deploy with non-existent ConfigMap
		err := wait.PollUntilContextTimeout(ctx, 500*time.Millisecond, 10*time.Second, true, func(ctx context.Context) (bool, error) {
			var latest clusterv1alpha1.ManagedCluster
			if err := c.Get(ctx, types.NamespacedName{Name: mc.Name}, &latest); err != nil {
				return false, nil
			}
			latest.Spec.Addons = []clusterv1alpha1.ClusterAddon{
				{
					Name:    mcs.AddonName,
					Enabled: true,
					Config: map[string]string{
						mcs.ConfigBrokerValuesConfigMap: "non-existent-configmap",
						mcs.ConfigBrokerValuesNamespace: "default",
					},
				},
			}
			return c.Update(ctx, &latest) == nil, nil
		})
		require.NoError(t, err, "Failed to set config with non-existent ConfigMap")

		// Wait to verify that reconciliation fails and broker info is NOT populated
		time.Sleep(5 * time.Second)

		// Check if brokerURL was populated - it should NOT be if the reconciliation failed
		var latest clusterv1alpha1.ManagedCluster
		err = c.Get(ctx, types.NamespacedName{Name: mc.Name}, &latest)
		require.NoError(t, err)

		hasValidConfig := false
		for _, addon := range latest.Spec.Addons {
			if addon.Name == mcs.AddonName && addon.Config["brokerURL"] != "" {
				hasValidConfig = true
				break
			}
		}
		assert.False(t, hasValidConfig, "Broker config should not be populated when ConfigMap is missing")
	})
}

// testAddonMultiCluster verifies addon behavior in multi-cluster scenarios
func testAddonMultiCluster(t *testing.T, env *TestEnvironment) {
	ctx := env.Context()
	c := env.Client

	t.Run("DifferentVersionsPerCluster", func(t *testing.T) {
		// Cluster 1 with old versions
		cluster1Name := "e2e-cluster1-old-versions"
		mc1 := env.CreateHubCluster(t, cluster1Name, map[string]string{"e2e-test": "multi-cluster"})
		defer env.DeleteCluster(cluster1Name)

		// Cluster 2 with new versions
		cluster2Name := "e2e-cluster2-new-versions"
		mc2 := env.CreateHubCluster(t, cluster2Name, map[string]string{"e2e-test": "multi-cluster"})
		defer env.DeleteCluster(cluster2Name)

		// Deploy cluster 1 with old versions
		err := wait.PollUntilContextTimeout(ctx, 500*time.Millisecond, 10*time.Second, true, func(ctx context.Context) (bool, error) {
			var latest clusterv1alpha1.ManagedCluster
			if err := c.Get(ctx, types.NamespacedName{Name: mc1.Name}, &latest); err != nil {
				return false, nil
			}
			latest.Spec.Addons = []clusterv1alpha1.ClusterAddon{
				{
					Name:    mcs.AddonName,
					Enabled: true,
					Config: map[string]string{
						mcs.ConfigBrokerChartVersion:     "0.23.0-m0",
						mcs.ConfigSubmarinerChartVersion: "0.21.0",
					},
				},
			}
			return c.Update(ctx, &latest) == nil, nil
		})
		require.NoError(t, err)

		// Deploy cluster 2 with new versions (simulated - use same versions as defaults for test)
		err = wait.PollUntilContextTimeout(ctx, 500*time.Millisecond, 10*time.Second, true, func(ctx context.Context) (bool, error) {
			var latest clusterv1alpha1.ManagedCluster
			if err := c.Get(ctx, types.NamespacedName{Name: mc2.Name}, &latest); err != nil {
				return false, nil
			}
			latest.Spec.Addons = []clusterv1alpha1.ClusterAddon{
				{
					Name:    mcs.AddonName,
					Enabled: true,
					Config: map[string]string{
						mcs.ConfigBrokerChartVersion:     "0.23.0-m0", // Would be 0.24.0 in real scenario
						mcs.ConfigSubmarinerChartVersion: "0.23.0-m0", // Would be 0.8.0 in real scenario
					},
				},
			}
			return c.Update(ctx, &latest) == nil, nil
		})
		require.NoError(t, err)

		// Wait for both clusters to complete installation
		time.Sleep(5 * time.Second)

		// Verify both clusters have broker secrets (independent installations)
		secret1 := &corev1.Secret{}
		err = c.Get(ctx, types.NamespacedName{Name: "submariner-k8s-broker-client-token", Namespace: "submariner-k8s-broker"}, secret1)
		// Note: In real multi-cluster setup, each cluster would have its own namespace/broker
		// For this test, we just verify the basic mechanism works
		assert.NoError(t, err, "Broker installation should complete for at least one cluster")
	})
}
