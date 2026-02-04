package cache

import (
	"testing"

	"k8s.io/apimachinery/pkg/labels"
)

func TestSimpleIndexer_Clone(t *testing.T) {
	// Create and populate an indexer
	indexer := NewSimpleIndexer()
	indexer.Add("cluster-a", map[string]string{
		"region": "us-east",
		"zone":   "zone-1",
	})
	indexer.Add("cluster-b", map[string]string{
		"region": "us-west",
		"zone":   "zone-2",
	})

	// Clone the indexer
	cloned := indexer.Clone()

	// Verify cloned data matches original
	if got := cloned.GetClustersByLabel("region", "us-east"); len(got) != 1 || got[0] != "cluster-a" {
		t.Errorf("Clone GetClustersByLabel(region, us-east) = %v, want [cluster-a]", got)
	}
	if got := cloned.GetClustersByLabel("region", "us-west"); len(got) != 1 || got[0] != "cluster-b" {
		t.Errorf("Clone GetClustersByLabel(region, us-west) = %v, want [cluster-b]", got)
	}

	// Modify original after clone
	indexer.Add("cluster-c", map[string]string{
		"region": "us-east",
		"zone":   "zone-3",
	})

	// Verify clone is not affected
	usEastClusters := cloned.GetClustersByLabel("region", "us-east")
	if len(usEastClusters) != 1 {
		t.Errorf("Clone should not see cluster-c, got %v", usEastClusters)
	}

	// Verify original has the new cluster
	usEastClustersOriginal := indexer.GetClustersByLabel("region", "us-east")
	if len(usEastClustersOriginal) != 2 {
		t.Errorf("Original should have 2 clusters in us-east, got %v", usEastClustersOriginal)
	}

	// Remove from original
	indexer.Remove("cluster-a")

	// Verify clone still has cluster-a
	if got := cloned.GetClustersByLabel("region", "us-east"); len(got) != 1 || got[0] != "cluster-a" {
		t.Errorf("Clone should still have cluster-a, got %v", got)
	}
}

func TestSimpleIndexer_CloneEmpty(t *testing.T) {
	indexer := NewSimpleIndexer()
	cloned := indexer.Clone()

	if got := cloned.GetClustersByLabel("any", "value"); got != nil {
		t.Errorf("Cloned empty indexer GetClustersByLabel should return nil, got %v", got)
	}
}

func TestSimpleIndexer_CloneLabelSelector(t *testing.T) {
	indexer := NewSimpleIndexer()
	indexer.Add("cluster-a", map[string]string{
		"env":    "prod",
		"region": "us-east",
	})
	indexer.Add("cluster-b", map[string]string{
		"env":    "staging",
		"region": "us-east",
	})

	cloned := indexer.Clone()

	// Test with label selector
	selector := labels.SelectorFromSet(labels.Set{"env": "prod"})
	matches := cloned.ListClustersByLabelSelector(selector)
	if len(matches) != 1 || matches[0] != "cluster-a" {
		t.Errorf("ListClustersByLabelSelector(env=prod) = %v, want [cluster-a]", matches)
	}

	// Modify original
	indexer.Add("cluster-c", map[string]string{
		"env":    "prod",
		"region": "us-west",
	})

	// Clone should not see the change
	matchesAfter := cloned.ListClustersByLabelSelector(selector)
	if len(matchesAfter) != 1 {
		t.Errorf("Clone should not see cluster-c, got %v", matchesAfter)
	}
}

func TestSimpleIndexer_GetClustersByLabelKey(t *testing.T) {
	indexer := NewSimpleIndexer()
	indexer.Add("cluster-a", map[string]string{
		"region": "us-east",
		"env":    "prod",
	})
	indexer.Add("cluster-b", map[string]string{
		"region": "us-west",
		"env":    "staging",
	})
	indexer.Add("cluster-c", map[string]string{
		"env": "prod", // no region label
	})

	// Get all clusters with "region" key
	regionClusters := indexer.GetClustersByLabelKey("region")
	if len(regionClusters) != 2 {
		t.Errorf("GetClustersByLabelKey(region) = %v, want 2 clusters", regionClusters)
	}

	// Check that cluster-a and cluster-b are in the result
	hasA, hasB := false, false
	for _, name := range regionClusters {
		if name == "cluster-a" {
			hasA = true
		}
		if name == "cluster-b" {
			hasB = true
		}
	}
	if !hasA || !hasB {
		t.Errorf("GetClustersByLabelKey(region) should contain cluster-a and cluster-b, got %v", regionClusters)
	}

	// Get all clusters with "env" key
	envClusters := indexer.GetClustersByLabelKey("env")
	if len(envClusters) != 3 {
		t.Errorf("GetClustersByLabelKey(env) = %v, want 3 clusters", envClusters)
	}

	// Non-existent key should return nil
	nonExistent := indexer.GetClustersByLabelKey("non-existent")
	if nonExistent != nil {
		t.Errorf("GetClustersByLabelKey(non-existent) = %v, want nil", nonExistent)
	}
}

func TestSimpleIndexer_GetClustersByLabelKey_Empty(t *testing.T) {
	indexer := NewSimpleIndexer()

	result := indexer.GetClustersByLabelKey("any")
	if result != nil {
		t.Errorf("Empty indexer GetClustersByLabelKey should return nil, got %v", result)
	}
}

func TestSimpleIndexer_GetClustersByLabelKey_AfterRemove(t *testing.T) {
	indexer := NewSimpleIndexer()
	indexer.Add("cluster-a", map[string]string{
		"region": "us-east",
	})
	indexer.Add("cluster-b", map[string]string{
		"region": "us-west",
	})

	// Remove one cluster
	indexer.Remove("cluster-a")

	regionClusters := indexer.GetClustersByLabelKey("region")
	if len(regionClusters) != 1 || regionClusters[0] != "cluster-b" {
		t.Errorf("After remove, GetClustersByLabelKey(region) = %v, want [cluster-b]", regionClusters)
	}

	// Remove the last cluster with that key
	indexer.Remove("cluster-b")

	regionClusters = indexer.GetClustersByLabelKey("region")
	if regionClusters != nil {
		t.Errorf("After removing all, GetClustersByLabelKey(region) = %v, want nil", regionClusters)
	}
}
