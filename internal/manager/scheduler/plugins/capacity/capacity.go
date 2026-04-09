package capacity

import (
	"context"
	"math"

	v1 "k8s.io/api/core/v1"

	"github.com/fize/rocket/internal/manager/scheduler/cache"
	appsv1alpha1 "github.com/fize/rocket/pkg/apis/apps/v1alpha1"
	clusterv1alpha1 "github.com/fize/rocket/pkg/apis/storage/v1alpha1"
	"github.com/fize/rocket/internal/manager/scheduler/framework"
)

const Name = "Capacity"

type Capacity struct{}

var _ framework.FilterPlugin = &Capacity{}

func New() framework.Plugin {
	return &Capacity{}
}

func (pl *Capacity) Name() string {
	return Name
}

func (pl *Capacity) Filter(ctx context.Context, state *framework.CycleState, app *appsv1alpha1.Application, cluster *clusterv1alpha1.ManagedCluster) *framework.Status {
	// Calculate required resources for a SINGLE pod
	// We only check if the cluster can fit at least one replica.
	// The actual number of replicas to be placed is determined later (e.g. by Spread strategy).
	podReq := cache.CalculatePodResources(app)
	if podReq == nil {
		return framework.NewStatus(framework.Success, "")
	}

	// Check if cluster has enough resources
	if len(cluster.Status.ResourceSummary) == 0 {
		return framework.NewStatus(framework.Unschedulable, "cluster resource summary missing")
	}

	allocatable := cluster.Status.ResourceSummary[0].Allocatable
	allocated := cluster.Status.ResourceSummary[0].Allocated

	// Check if at least 1 replica fits
	replicas := int64(1)

	// Check CPU capacity
	if !fitsWithReplicas(allocatable, allocated, podReq, v1.ResourceCPU, replicas) {
		return framework.NewStatus(framework.Unschedulable, "insufficient cpu for single replica")
	}

	// Check Memory capacity
	if !fitsWithReplicas(allocatable, allocated, podReq, v1.ResourceMemory, replicas) {
		return framework.NewStatus(framework.Unschedulable, "insufficient memory for single replica")
	}

	return framework.NewStatus(framework.Success, "")
}

func fitsWithReplicas(allocatable, allocated, required v1.ResourceList, name v1.ResourceName, replicas int64) bool {
	allocatableQ := allocatable[name]
	allocatedQ := allocated[name]
	requiredQ := required[name]

	available := allocatableQ.DeepCopy()
	available.Sub(allocatedQ)

	// Calculate total required for all replicas
	totalRequired := requiredQ.DeepCopy()
	for i := int64(1); i < replicas; i++ {
		totalRequired.Add(requiredQ)
	}

	return available.Cmp(totalRequired) >= 0
}

// CalculateMaxReplicas calculates how many replicas of the app can fit in the cluster
func CalculateMaxReplicas(cluster *clusterv1alpha1.ManagedCluster, app *appsv1alpha1.Application) int64 {
	podReq := cache.CalculatePodResources(app)
	if len(podReq) == 0 {
		return math.MaxInt64
	}

	if len(cluster.Status.ResourceSummary) == 0 {
		return 0
	}

	allocatable := cluster.Status.ResourceSummary[0].Allocatable
	allocated := cluster.Status.ResourceSummary[0].Allocated

	var minMax int64 = math.MaxInt64

	for name, req := range podReq {
		avail := allocatable[name].DeepCopy()
		avail.Sub(allocated[name])

		if avail.Value() <= 0 {
			return 0
		}

		// max = avail / req
		// Use MilliValue
		reqMilli := req.MilliValue()
		if reqMilli == 0 {
			continue
		}
		availMilli := avail.MilliValue()

		count := availMilli / reqMilli
		if count < minMax {
			minMax = count
		}
	}

	if minMax == math.MaxInt64 {
		return 0
	}
	return minMax
}
