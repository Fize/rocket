package health

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/hex-techs/rocket/internal/manager/scheduler/framework"
	appsv1alpha1 "github.com/hex-techs/rocket/pkg/apis/apps/v1alpha1"
	clusterv1alpha1 "github.com/hex-techs/rocket/pkg/apis/storage/v1alpha1"
)

// Health is a plugin that filters out clusters that are not ready or reachable.
type Health struct{}

// Name returns the plugin name.
func (h *Health) Name() string { return "Health" }

// Filter checks if the cluster is ready and reachable.
func (h *Health) Filter(ctx context.Context, state *framework.CycleState, app *appsv1alpha1.Application, cluster *clusterv1alpha1.ManagedCluster) *framework.Status {
	for _, cond := range cluster.Status.Conditions {
		switch cond.Type {
		case "Ready":
			if cond.Status != metav1.ConditionTrue {
				return framework.NewStatus(framework.Unschedulable, "cluster is not ready")
			}
		case "Reachable":
			if cond.Status != metav1.ConditionTrue {
				return framework.NewStatus(framework.Unschedulable, "cluster is not reachable")
			}
		}
	}
	return framework.NewStatus(framework.Success, "")
}

// New initializes a new Health plugin.
func New() framework.Plugin { return &Health{} }
