package volumerestriction

import (
	"context"
	"encoding/json"

	"github.com/fize/rocket/internal/manager/scheduler/framework"
	appsv1alpha1 "github.com/fize/rocket/pkg/apis/apps/v1alpha1"
	clusterv1alpha1 "github.com/fize/rocket/pkg/apis/storage/v1alpha1"
	v1 "k8s.io/api/core/v1"
)

const Name = "VolumeRestriction"

type VolumeRestriction struct{}

var _ framework.FilterPlugin = &VolumeRestriction{}

func New() framework.Plugin {
	return &VolumeRestriction{}
}

func (pl *VolumeRestriction) Name() string {
	return Name
}

func (pl *VolumeRestriction) Filter(ctx context.Context, state *framework.CycleState, app *appsv1alpha1.Application, cluster *clusterv1alpha1.ManagedCluster) *framework.Status {
	if len(app.Spec.Template.Raw) == 0 {
		return framework.NewStatus(framework.Success, "")
	}

	var template v1.PodTemplateSpec
	if err := json.Unmarshal(app.Spec.Template.Raw, &template); err != nil {
		return framework.NewStatus(framework.Success, "")
	}

	hasPVC := false
	for _, vol := range template.Spec.Volumes {
		if vol.PersistentVolumeClaim != nil {
			hasPVC = true
			break
		}
	}

	if !hasPVC {
		return framework.NewStatus(framework.Success, "")
	}

	// For applications with PVC:
	// 1. Initial Scheduling (No placement yet): Allowed (Auto-select clusters)
	// 2. Scaling/Re-scheduling (Placement exists): Restricted to existing clusters ONLY.
	//    This ensures that HPA or manual scaling only increases replicas on the specific
	//    clusters where the PVCs (and their data) are bound, maintaining topology stickiness.
	if app.Status.Placement.Topology != nil && len(app.Status.Placement.Topology) > 0 {
		for _, topology := range app.Status.Placement.Topology {
			if topology.Name == cluster.Name {
				return framework.NewStatus(framework.Success, "")
			}
		}
		// Cluster not in existing topology -> Deny
		return framework.NewStatus(framework.Unschedulable, "topology is fixed for applications with PVC; cannot schedule to new clusters automatically")
	}

	// Initial scheduling -> Allow
	return framework.NewStatus(framework.Success, "")
}
