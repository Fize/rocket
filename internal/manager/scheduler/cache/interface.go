package cache

import (
	"sync"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"

	appsv1alpha1 "github.com/fize/rocket/pkg/apis/apps/v1alpha1"
	clusterv1alpha1 "github.com/fize/rocket/pkg/apis/storage/v1alpha1"
)

// AssumedApplicationInfo tracks information about an assumed application.
type AssumedApplicationInfo struct {
	Resources v1.ResourceList
	Replicas  int32
}

// ClusterInfo is a wrapper around Cluster object with additional information.
type ClusterInfo struct {
	Cluster *clusterv1alpha1.ManagedCluster

	// RequestedResources is the sum of resources requested by Applications that are
	// assigned to this cluster but not yet reflected in the Cluster status.
	// This is used for optimistic scheduling.
	RequestedResources v1.ResourceList

	// AssumedApplications tracks applications that have been optimistically assigned to this cluster.
	// Key is "namespace/name"
	AssumedApplications map[string]AssumedApplicationInfo
}

func NewClusterInfo(cluster *clusterv1alpha1.ManagedCluster) *ClusterInfo {
	return &ClusterInfo{
		Cluster:             cluster,
		RequestedResources:  make(v1.ResourceList),
		AssumedApplications: make(map[string]AssumedApplicationInfo),
	}
}

func (c *ClusterInfo) Clone() *ClusterInfo {
	cloned := &ClusterInfo{
		Cluster:             c.Cluster.DeepCopy(),
		RequestedResources:  c.RequestedResources.DeepCopy(),
		AssumedApplications: make(map[string]AssumedApplicationInfo),
	}
	for k, v := range c.AssumedApplications {
		cloned.AssumedApplications[k] = AssumedApplicationInfo{
			Resources: v.Resources.DeepCopy(),
			Replicas:  v.Replicas,
		}
	}
	return cloned
}

// Cache collects clusters' information and provides a snapshot for the scheduler.
type Cache interface {
	// AddCluster adds a cluster to the cache.
	AddCluster(cluster *clusterv1alpha1.ManagedCluster) error
	// UpdateCluster updates a cluster in the cache.
	UpdateCluster(oldCluster, newCluster *clusterv1alpha1.ManagedCluster) error
	// RemoveCluster removes a cluster from the cache.
	RemoveCluster(cluster *clusterv1alpha1.ManagedCluster) error

	// Snapshot returns a snapshot of the current cache.
	Snapshot() *Snapshot

	// AssumeApplication optimistically adds resources requested by an application to a cluster.
	AssumeApplication(app *appsv1alpha1.Application, clusterName string, replicas int32) error
	// ForgetApplication removes optimistically added resources.
	ForgetApplication(app *appsv1alpha1.Application, clusterName string, replicas int32) error
	// RemoveAssumedApplication removes all assumptions for a given application across all clusters.
	RemoveAssumedApplication(appKey string)
}

// Snapshot is a snapshot of the cache.
type Snapshot struct {
	Clusters map[string]*ClusterInfo
	// Indexer is used to filter clusters by labels.
	Indexer Indexer
}

// Indexer is an interface for indexing clusters by labels.
type Indexer interface {
	// GetClustersByLabel returns cluster names that have the specific label key and value.
	GetClustersByLabel(key, value string) []string
	// GetClustersByLabelKey returns cluster names that have the specific label key (any value).
	// This is useful for implementing the "Exists" operator.
	GetClustersByLabelKey(key string) []string
	// ListClustersByLabelSelector returns cluster names matching the selector.
	ListClustersByLabelSelector(selector labels.Selector) []string
}

// SimpleIndexer implements Indexer interface.
type SimpleIndexer struct {
	mx            sync.RWMutex
	index         map[string]map[string][]string // key -> value -> [clusterNames]
	clusterLabels map[string]map[string]string
}

func NewSimpleIndexer() *SimpleIndexer {
	return &SimpleIndexer{
		index:         make(map[string]map[string][]string),
		clusterLabels: make(map[string]map[string]string),
	}
}

func (i *SimpleIndexer) Add(name string, labels map[string]string) {
	i.mx.Lock()
	defer i.mx.Unlock()

	i.clusterLabels[name] = labels
	for k, v := range labels {
		if _, ok := i.index[k]; !ok {
			i.index[k] = make(map[string][]string)
		}
		i.index[k][v] = append(i.index[k][v], name)
	}
}

func (i *SimpleIndexer) Remove(name string) {
	i.mx.Lock()
	defer i.mx.Unlock()

	labels, ok := i.clusterLabels[name]
	if !ok {
		return
	}
	delete(i.clusterLabels, name)

	for k, v := range labels {
		if values, ok := i.index[k]; ok {
			if clusters, ok := values[v]; ok {
				newClusters := make([]string, 0, len(clusters)-1)
				for _, c := range clusters {
					if c != name {
						newClusters = append(newClusters, c)
					}
				}
				if len(newClusters) == 0 {
					delete(values, v)
				} else {
					values[v] = newClusters
				}
			}
			if len(values) == 0 {
				delete(i.index, k)
			}
		}
	}
}

func (i *SimpleIndexer) GetClustersByLabel(key, value string) []string {
	i.mx.RLock()
	defer i.mx.RUnlock()

	if values, ok := i.index[key]; ok {
		if clusters, ok := values[value]; ok {
			res := make([]string, len(clusters))
			copy(res, clusters)
			return res
		}
	}
	return nil
}

// GetClustersByLabelKey returns all clusters that have the specified label key (any value).
// This is useful for implementing the "Exists" operator in affinity expressions.
func (i *SimpleIndexer) GetClustersByLabelKey(key string) []string {
	i.mx.RLock()
	defer i.mx.RUnlock()

	values, ok := i.index[key]
	if !ok {
		return nil
	}

	// Collect all unique cluster names that have this key
	clusterSet := make(map[string]struct{})
	for _, clusters := range values {
		for _, name := range clusters {
			clusterSet[name] = struct{}{}
		}
	}

	result := make([]string, 0, len(clusterSet))
	for name := range clusterSet {
		result = append(result, name)
	}
	return result
}

func (i *SimpleIndexer) ListClustersByLabelSelector(selector labels.Selector) []string {
	i.mx.RLock()
	defer i.mx.RUnlock()

	var res []string
	for name, lbls := range i.clusterLabels {
		if selector.Matches(labels.Set(lbls)) {
			res = append(res, name)
		}
	}
	return res
}

// Clone creates a deep copy of the SimpleIndexer.
// This is useful for creating a true point-in-time snapshot that won't change
// when the source cache is modified.
func (i *SimpleIndexer) Clone() *SimpleIndexer {
	i.mx.RLock()
	defer i.mx.RUnlock()

	clone := &SimpleIndexer{
		index:         make(map[string]map[string][]string),
		clusterLabels: make(map[string]map[string]string),
	}

	// Deep copy clusterLabels
	for name, lbls := range i.clusterLabels {
		clonedLabels := make(map[string]string)
		for k, v := range lbls {
			clonedLabels[k] = v
		}
		clone.clusterLabels[name] = clonedLabels
	}

	// Deep copy index
	for key, values := range i.index {
		clone.index[key] = make(map[string][]string)
		for value, clusters := range values {
			clonedClusters := make([]string, len(clusters))
			copy(clonedClusters, clusters)
			clone.index[key][value] = clonedClusters
		}
	}

	return clone
}
