package cache

import (
	"encoding/json"
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	appsv1alpha1 "github.com/hex-techs/rocket/pkg/apis/apps/v1alpha1"
	clusterv1alpha1 "github.com/hex-techs/rocket/pkg/apis/storage/v1alpha1"
)

func toRaw(obj interface{}) runtime.RawExtension {
	b, _ := json.Marshal(obj)
	return runtime.RawExtension{Raw: b}
}

func TestCacheAssumeApplication(t *testing.T) {
	cache := NewCache()
	cluster := &clusterv1alpha1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster1",
		},
		Status: clusterv1alpha1.ManagedClusterStatus{
			ResourceSummary: []clusterv1alpha1.ResourceSummary{
				{
					Allocatable: v1.ResourceList{
						v1.ResourceCPU:    resource.MustParse("10"),
						v1.ResourceMemory: resource.MustParse("10Gi"),
					},
					Allocated: v1.ResourceList{
						v1.ResourceCPU:    resource.MustParse("1"),
						v1.ResourceMemory: resource.MustParse("1Gi"),
					},
				},
			},
		},
	}
	_ = cache.AddCluster(cluster)

	app := &appsv1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "app1",
			Namespace: "default",
		},
		Spec: appsv1alpha1.ApplicationSpec{
			Template: toRaw(&v1.PodTemplateSpec{
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Resources: v1.ResourceRequirements{
								Requests: v1.ResourceList{
									v1.ResourceCPU:    resource.MustParse("500m"),
									v1.ResourceMemory: resource.MustParse("512Mi"),
								},
							},
						},
					},
				},
			}),
		},
	}

	// Assume 2 replicas
	err := cache.AssumeApplication(app, "cluster1", 2)
	if err != nil {
		t.Fatalf("failed to assume application: %v", err)
	}

	snapshot := cache.Snapshot()
	info := snapshot.Clusters["cluster1"]
	if info == nil {
		t.Fatal("cluster1 not found")
	}

	// Check RequestedResources (2 * 500m = 1000m = 1)
	cpuReq := info.RequestedResources[v1.ResourceCPU]
	if cpuReq.MilliValue() != 1000 {
		t.Errorf("expected 1000m CPU requested, got %v", cpuReq.MilliValue())
	}

	// Check if snapshot applied RequestedResources to Allocated
	// Original Allocated was 1 CPU, now should be 2 CPU
	allocatedCPU := info.Cluster.Status.ResourceSummary[0].Allocated[v1.ResourceCPU]
	if allocatedCPU.MilliValue() != 2000 {
		t.Errorf("expected 2000m CPU allocated in snapshot, got %v", allocatedCPU.MilliValue())
	}

	// Remove assumption
	cache.RemoveAssumedApplication("default/app1")
	snapshot = cache.Snapshot()
	info = snapshot.Clusters["cluster1"]
	if len(info.RequestedResources) != 0 {
		t.Errorf("expected 0 RequestedResources after removal, got %d", len(info.RequestedResources))
	}
	cpuAfter := info.Cluster.Status.ResourceSummary[0].Allocated[v1.ResourceCPU]
	if cpuAfter.MilliValue() != 1000 {
		t.Errorf("expected 1000m CPU allocated after removal, got %v", cpuAfter.MilliValue())
	}
}

func TestCacheAddCluster(t *testing.T) {
	cache := NewCache()
	cluster := &clusterv1alpha1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster1",
			Labels: map[string]string{
				"region": "us-west",
				"env":    "prod",
			},
		},
	}

	err := cache.AddCluster(cluster)
	if err != nil {
		t.Fatalf("failed to add cluster: %v", err)
	}

	// Try to add the same cluster again
	err = cache.AddCluster(cluster)
	if err == nil {
		t.Error("expected error when adding duplicate cluster")
	}

	snapshot := cache.Snapshot()
	if len(snapshot.Clusters) != 1 {
		t.Errorf("expected 1 cluster, got %d", len(snapshot.Clusters))
	}

	if _, ok := snapshot.Clusters["cluster1"]; !ok {
		t.Error("cluster1 not found in snapshot")
	}
}

func TestCacheUpdateCluster(t *testing.T) {
	cache := NewCache()
	cluster := &clusterv1alpha1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster1",
			Labels: map[string]string{
				"region": "us-west",
			},
		},
	}

	err := cache.AddCluster(cluster)
	if err != nil {
		t.Fatalf("failed to add cluster: %v", err)
	}

	updatedCluster := cluster.DeepCopy()
	updatedCluster.Labels["env"] = "prod"
	updatedCluster.Status.ResourceSummary = []clusterv1alpha1.ResourceSummary{
		{
			Allocatable: v1.ResourceList{
				v1.ResourceCPU: resource.MustParse("4"),
			},
		},
	}

	err = cache.UpdateCluster(cluster, updatedCluster)
	if err != nil {
		t.Fatalf("failed to update cluster: %v", err)
	}

	snapshot := cache.Snapshot()
	clusterInfo := snapshot.Clusters["cluster1"]
	if clusterInfo == nil {
		t.Fatal("cluster1 not found after update")
	}

	if clusterInfo.Cluster.Labels["env"] != "prod" {
		t.Error("cluster labels not updated")
	}

	if len(clusterInfo.Cluster.Status.ResourceSummary) == 0 {
		t.Error("cluster resource summary not updated")
	}
}

func TestCacheRemoveCluster(t *testing.T) {
	cache := NewCache()
	cluster := &clusterv1alpha1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster1",
		},
	}

	err := cache.AddCluster(cluster)
	if err != nil {
		t.Fatalf("failed to add cluster: %v", err)
	}

	err = cache.RemoveCluster(cluster)
	if err != nil {
		t.Fatalf("failed to remove cluster: %v", err)
	}

	snapshot := cache.Snapshot()
	if len(snapshot.Clusters) != 0 {
		t.Errorf("expected 0 clusters after removal, got %d", len(snapshot.Clusters))
	}
}

func TestCacheSnapshot(t *testing.T) {
	cache := NewCache()

	clusters := []*clusterv1alpha1.ManagedCluster{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "cluster1",
				Labels: map[string]string{
					"region": "us-west",
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "cluster2",
				Labels: map[string]string{
					"region": "us-east",
				},
			},
		},
	}

	for _, cluster := range clusters {
		if err := cache.AddCluster(cluster); err != nil {
			t.Fatalf("failed to add cluster: %v", err)
		}
	}

	snapshot1 := cache.Snapshot()
	if len(snapshot1.Clusters) != 2 {
		t.Errorf("expected 2 clusters in snapshot, got %d", len(snapshot1.Clusters))
	}

	// Verify snapshot is a deep copy
	snapshot1.Clusters["cluster1"].Cluster.Labels["test"] = "value"

	snapshot2 := cache.Snapshot()
	if _, ok := snapshot2.Clusters["cluster1"].Cluster.Labels["test"]; ok {
		t.Error("snapshot is not a deep copy, modifications affected cache")
	}
}

func TestIndexerAddRemove(t *testing.T) {
	indexer := NewSimpleIndexer()

	indexer.Add("cluster1", map[string]string{
		"region": "us-west",
		"env":    "prod",
	})

	indexer.Add("cluster2", map[string]string{
		"region": "us-west",
		"env":    "dev",
	})

	indexer.Add("cluster3", map[string]string{
		"region": "us-east",
		"env":    "prod",
	})

	// Test GetClustersByLabel
	westClusters := indexer.GetClustersByLabel("region", "us-west")
	if len(westClusters) != 2 {
		t.Errorf("expected 2 clusters in us-west, got %d", len(westClusters))
	}

	prodClusters := indexer.GetClustersByLabel("env", "prod")
	if len(prodClusters) != 2 {
		t.Errorf("expected 2 clusters in prod, got %d", len(prodClusters))
	}

	// Test Remove
	indexer.Remove("cluster1")
	westClusters = indexer.GetClustersByLabel("region", "us-west")
	if len(westClusters) != 1 {
		t.Errorf("expected 1 cluster in us-west after removal, got %d", len(westClusters))
	}

	prodClusters = indexer.GetClustersByLabel("env", "prod")
	if len(prodClusters) != 1 {
		t.Errorf("expected 1 cluster in prod after removal, got %d", len(prodClusters))
	}
}

func TestIndexerLabelSelector(t *testing.T) {
	indexer := NewSimpleIndexer()

	indexer.Add("cluster1", map[string]string{
		"region": "us-west",
		"env":    "prod",
		"tier":   "frontend",
	})

	indexer.Add("cluster2", map[string]string{
		"region": "us-west",
		"env":    "dev",
		"tier":   "backend",
	})

	indexer.Add("cluster3", map[string]string{
		"region": "us-east",
		"env":    "prod",
		"tier":   "frontend",
	})

	// Test label selector matching
	selector, _ := metav1.LabelSelectorAsSelector(&metav1.LabelSelector{
		MatchLabels: map[string]string{
			"env": "prod",
		},
	})

	clusters := indexer.ListClustersByLabelSelector(selector)
	if len(clusters) != 2 {
		t.Errorf("expected 2 clusters matching env=prod, got %d", len(clusters))
	}

	// Test multiple label matching
	selector2, _ := metav1.LabelSelectorAsSelector(&metav1.LabelSelector{
		MatchLabels: map[string]string{
			"region": "us-west",
			"env":    "prod",
		},
	})

	clusters2 := indexer.ListClustersByLabelSelector(selector2)
	if len(clusters2) != 1 {
		t.Errorf("expected 1 cluster matching region=us-west,env=prod, got %d", len(clusters2))
	}
}

func TestClusterInfoClone(t *testing.T) {
	cluster := &clusterv1alpha1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster1",
			Labels: map[string]string{
				"region": "us-west",
			},
		},
	}

	info := NewClusterInfo(cluster)
	info.RequestedResources = v1.ResourceList{
		v1.ResourceCPU: resource.MustParse("1"),
	}

	cloned := info.Clone()

	// Modify original
	info.Cluster.Labels["test"] = "value"
	info.RequestedResources[v1.ResourceMemory] = resource.MustParse("1Gi")

	// Verify clone is independent
	if _, ok := cloned.Cluster.Labels["test"]; ok {
		t.Error("clone was affected by modifications to original cluster")
	}

	if _, ok := cloned.RequestedResources[v1.ResourceMemory]; ok {
		t.Error("clone was affected by modifications to original resources")
	}
}
