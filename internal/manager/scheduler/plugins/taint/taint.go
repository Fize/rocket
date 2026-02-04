package taint

import (
	"context"
	"fmt"

	v1 "k8s.io/api/core/v1"

	"github.com/hex-techs/rocket/internal/manager/scheduler/framework"
	appsv1alpha1 "github.com/hex-techs/rocket/pkg/apis/apps/v1alpha1"
	clusterv1alpha1 "github.com/hex-techs/rocket/pkg/apis/storage/v1alpha1"
)

const Name = "TaintToleration"

type TaintToleration struct{}

var _ framework.FilterPlugin = &TaintToleration{}

func New() framework.Plugin {
	return &TaintToleration{}
}

func (pl *TaintToleration) Name() string {
	return Name
}

func (pl *TaintToleration) Filter(ctx context.Context, state *framework.CycleState, app *appsv1alpha1.Application, cluster *clusterv1alpha1.ManagedCluster) *framework.Status {
	taints := cluster.Spec.Taints
	tolerations := app.Spec.ClusterTolerations

	if untoleratedTaint, isUntolerated := findMatchingUntoleratedTaint(taints, tolerations, func(t *v1.Taint) bool {
		return t.Effect == v1.TaintEffectNoSchedule || t.Effect == v1.TaintEffectNoExecute
	}); isUntolerated {
		return framework.NewStatus(framework.Unschedulable, fmt.Sprintf("taint %s not tolerated", untoleratedTaint.ToString()))
	}

	return framework.NewStatus(framework.Success, "")
}

func findMatchingUntoleratedTaint(taints []v1.Taint, tolerations []v1.Toleration, inclusionFilter func(*v1.Taint) bool) (v1.Taint, bool) {
	for _, taint := range taints {
		if !inclusionFilter(&taint) {
			continue
		}
		if !tolerationsTolerateTaint(tolerations, &taint) {
			return taint, true
		}
	}
	return v1.Taint{}, false
}

func tolerationsTolerateTaint(tolerations []v1.Toleration, taint *v1.Taint) bool {
	for _, toleration := range tolerations {
		if toleratesTaint(&toleration, taint) {
			return true
		}
	}
	return false
}

func toleratesTaint(toleration *v1.Toleration, taint *v1.Taint) bool {
	if len(toleration.Effect) > 0 && toleration.Effect != taint.Effect {
		return false
	}

	if toleration.Operator == v1.TolerationOpExists {
		if len(toleration.Key) > 0 && toleration.Key != taint.Key {
			return false
		}
		return true
	}

	if toleration.Operator == v1.TolerationOpEqual {
		if toleration.Key != taint.Key {
			return false
		}
		if toleration.Value != taint.Value {
			return false
		}
		return true
	}

	return false
}
