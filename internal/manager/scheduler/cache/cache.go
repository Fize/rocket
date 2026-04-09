package cache

import (
	"encoding/json"
	"fmt"
	"sync"

	v1 "k8s.io/api/core/v1"

	appsv1alpha1 "github.com/fize/rocket/pkg/apis/apps/v1alpha1"
	clusterv1alpha1 "github.com/fize/rocket/pkg/apis/storage/v1alpha1"
)

type clusterCache struct {
	mx       sync.RWMutex
	clusters map[string]*ClusterInfo
	indexer  *SimpleIndexer
}

func NewCache() Cache {
	return &clusterCache{
		clusters: make(map[string]*ClusterInfo),
		indexer:  NewSimpleIndexer(),
	}
}

func (c *clusterCache) AddCluster(cluster *clusterv1alpha1.ManagedCluster) error {
	c.mx.Lock()
	defer c.mx.Unlock()

	if _, ok := c.clusters[cluster.Name]; ok {
		return fmt.Errorf("cluster %s already exists", cluster.Name)
	}

	info := NewClusterInfo(cluster)
	c.clusters[cluster.Name] = info
	c.indexer.Add(cluster.Name, cluster.Labels)
	return nil
}

func (c *clusterCache) UpdateCluster(oldCluster, newCluster *clusterv1alpha1.ManagedCluster) error {
	c.mx.Lock()
	defer c.mx.Unlock()

	if _, ok := c.clusters[oldCluster.Name]; !ok {
		return fmt.Errorf("cluster %s does not exist", oldCluster.Name)
	}

	// Update info
	info := c.clusters[oldCluster.Name]
	info.Cluster = newCluster

	// Update indexer
	c.indexer.Remove(oldCluster.Name)
	c.indexer.Add(newCluster.Name, newCluster.Labels)

	return nil
}

func (c *clusterCache) RemoveCluster(cluster *clusterv1alpha1.ManagedCluster) error {
	c.mx.Lock()
	defer c.mx.Unlock()

	if _, ok := c.clusters[cluster.Name]; !ok {
		return fmt.Errorf("cluster %s does not exist", cluster.Name)
	}

	delete(c.clusters, cluster.Name)
	c.indexer.Remove(cluster.Name)
	return nil
}

func (c *clusterCache) Snapshot() *Snapshot {
	c.mx.RLock()
	defer c.mx.RUnlock()

	clusters := make(map[string]*ClusterInfo)
	for k, v := range c.clusters {
		cloned := v.Clone()
		// Apply RequestedResources to the cloned Cluster object's status
		// so that plugins see the optimistic resource usage.
		if len(cloned.Cluster.Status.ResourceSummary) > 0 {
			summary := &cloned.Cluster.Status.ResourceSummary[0]
			if summary.Allocated == nil {
				summary.Allocated = make(v1.ResourceList)
			}
			for name, quantity := range cloned.RequestedResources {
				q := summary.Allocated[name]
				q.Add(quantity)
				summary.Allocated[name] = q
			}
		}
		clusters[k] = cloned
	}

	// Clone the indexer to provide a true point-in-time snapshot.
	// This ensures the snapshot is immutable and won't change during a scheduling cycle,
	// even if the underlying cache is modified by other goroutines.
	return &Snapshot{
		Clusters: clusters,
		Indexer:  c.indexer.Clone(),
	}
}

func (c *clusterCache) AssumeApplication(app *appsv1alpha1.Application, clusterName string, replicas int32) error {
	c.mx.Lock()
	defer c.mx.Unlock()

	info, ok := c.clusters[clusterName]
	if !ok {
		return fmt.Errorf("cluster %s does not exist", clusterName)
	}

	podReq := CalculatePodResources(app)
	if podReq == nil {
		return nil
	}

	appKey := fmt.Sprintf("%s/%s", app.Namespace, app.Name)
	if _, exists := info.AssumedApplications[appKey]; exists {
		// Already assumed, maybe replicas changed?
		// For simplicity, we'll just return nil or update it.
		return nil
	}

	totalReq := make(v1.ResourceList)
	for name, quantity := range podReq {
		q := quantity.DeepCopy()
		for i := int32(1); i < replicas; i++ {
			q.Add(quantity)
		}
		totalReq[name] = q

		// Update aggregate RequestedResources
		agg := info.RequestedResources[name]
		agg.Add(q)
		info.RequestedResources[name] = agg
	}

	info.AssumedApplications[appKey] = AssumedApplicationInfo{
		Resources: totalReq,
		Replicas:  replicas,
	}
	return nil
}

func (c *clusterCache) ForgetApplication(app *appsv1alpha1.Application, clusterName string, replicas int32) error {
	c.mx.Lock()
	defer c.mx.Unlock()

	info, ok := c.clusters[clusterName]
	if !ok {
		return fmt.Errorf("cluster %s does not exist", clusterName)
	}

	appKey := fmt.Sprintf("%s/%s", app.Namespace, app.Name)
	assumed, exists := info.AssumedApplications[appKey]
	if !exists {
		return nil
	}

	for name, quantity := range assumed.Resources {
		agg := info.RequestedResources[name]
		agg.Sub(quantity)
		if agg.IsZero() || agg.Sign() < 0 {
			delete(info.RequestedResources, name)
		} else {
			info.RequestedResources[name] = agg
		}
	}

	delete(info.AssumedApplications, appKey)
	return nil
}

func (c *clusterCache) RemoveAssumedApplication(appKey string) {
	c.mx.Lock()
	defer c.mx.Unlock()

	for _, info := range c.clusters {
		if assumed, exists := info.AssumedApplications[appKey]; exists {
			for name, quantity := range assumed.Resources {
				agg := info.RequestedResources[name]
				agg.Sub(quantity)
				if agg.IsZero() || agg.Sign() < 0 {
					delete(info.RequestedResources, name)
				} else {
					info.RequestedResources[name] = agg
				}
			}
			delete(info.AssumedApplications, appKey)
		}
	}
}

func CalculatePodResources(app *appsv1alpha1.Application) v1.ResourceList {
	if len(app.Spec.Template.Raw) == 0 {
		return nil
	}
	var template v1.PodTemplateSpec
	if err := json.Unmarshal(app.Spec.Template.Raw, &template); err != nil {
		return nil
	}

	podReq := v1.ResourceList{}

	// Containers
	for _, container := range template.Spec.Containers {
		AddResourceList(podReq, container.Resources.Requests)
	}

	// InitContainers (max)
	for _, container := range template.Spec.InitContainers {
		MaxResourceList(podReq, container.Resources.Requests)
	}

	return podReq
}

func AddResourceList(list, new v1.ResourceList) {
	for name, quantity := range new {
		if value, ok := list[name]; ok {
			value.Add(quantity)
			list[name] = value
		} else {
			list[name] = quantity.DeepCopy()
		}
	}
}

func MaxResourceList(list, new v1.ResourceList) {
	for name, quantity := range new {
		if value, ok := list[name]; ok {
			if quantity.Cmp(value) > 0 {
				list[name] = quantity.DeepCopy()
			}
		} else {
			list[name] = quantity.DeepCopy()
		}
	}
}
