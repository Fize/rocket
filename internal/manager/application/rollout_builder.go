package application

import (
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	appsv1alpha1 "github.com/fize/rocket/pkg/apis/apps/v1alpha1"
)

// RolloutBuilder builds Rollout resources for kruise-rollout
type RolloutBuilder struct {
	calculator *GlobalReplicaCalculator
}

// NewRolloutBuilder creates a new RolloutBuilder
func NewRolloutBuilder() *RolloutBuilder {
	return &RolloutBuilder{
		calculator: NewGlobalReplicaCalculator(),
	}
}

// BuildRollout builds a Rollout resource for the given application and cluster
// assignment: the replica assignment for this cluster (canary/stable replicas)
func (b *RolloutBuilder) BuildRollout(
	app *appsv1alpha1.Application,
	clusterName string,
	assignment *ClusterReplicaAssignment,
) (*unstructured.Unstructured, error) {
	if app.Spec.RolloutStrategy == nil {
		return nil, fmt.Errorf("no rollout strategy specified")
	}

	strategy := app.Spec.RolloutStrategy

	rollout := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "rollouts.kruise.io/v1alpha1",
			"kind":       "Rollout",
			"metadata": map[string]interface{}{
				"name":      app.Name,
				"namespace": app.Namespace,
				"labels": map[string]interface{}{
					"app.kubernetes.io/name":       app.Name,
					"app.kubernetes.io/managed-by": "rocket",
					"rocket.io/application":        app.Name,
					"rocket.io/cluster":            clusterName,
				},
				"ownerReferences": []interface{}{
					map[string]interface{}{
						"apiVersion":         app.APIVersion,
						"blockOwnerDeletion": true,
						"controller":         true,
						"kind":               app.Kind,
						"name":               app.Name,
						"uid":                string(app.UID),
					},
				},
			},
			"spec": b.buildRolloutSpec(app, strategy, assignment),
		},
	}

	return rollout, nil
}

// buildRolloutSpec builds the spec for Rollout
func (b *RolloutBuilder) buildRolloutSpec(
	app *appsv1alpha1.Application,
	strategy *appsv1alpha1.RolloutStrategy,
	assignment *ClusterReplicaAssignment,
) map[string]interface{} {
	spec := map[string]interface{}{
		"objectRef": map[string]interface{}{
			"workloadRef": map[string]interface{}{
				"apiVersion": app.Spec.Workload.APIVersion,
				"kind":       app.Spec.Workload.Kind,
				"name":       app.Name,
			},
		},
	}

	// Build strategy-specific configuration
	switch strategy.Type {
	case appsv1alpha1.RolloutTypeCanary:
		if s := b.buildCanaryStrategy(strategy, assignment); s != nil {
			spec["strategy"] = s
		}
	case appsv1alpha1.RolloutTypeBlueGreen:
		if s := b.buildBlueGreenStrategy(strategy); s != nil {
			spec["strategy"] = s
		}
	case appsv1alpha1.RolloutTypeABTest:
		if s := b.buildABTestStrategy(strategy, assignment); s != nil {
			spec["strategy"] = s
		}
	}

	return spec
}

// buildCanaryStrategy builds canary rollout strategy
// When GlobalReplicaDistribution is enabled, it uses absolute replica counts
// Otherwise, it falls back to percentage-based steps
func (b *RolloutBuilder) buildCanaryStrategy(strategy *appsv1alpha1.RolloutStrategy, assignment *ClusterReplicaAssignment) map[string]interface{} {
	if strategy.Canary == nil {
		return nil
	}

	var steps []interface{}

	// Check if global distribution is enabled
	if strategy.Canary.GlobalReplicaDistribution != nil && assignment != nil {
		// Use absolute replica count for this cluster
		// The canary replicas are already calculated by GlobalReplicaCalculator
		steps = []interface{}{
			map[string]interface{}{
				"replicas": int64(assignment.CanaryReplicas), // Use int64 for JSON compatibility
			},
		}
	} else {
		// Fall back to percentage-based steps
		steps = b.buildCanarySteps(strategy.Canary.Steps)
	}

	canary := map[string]interface{}{
		"steps": steps,
	}

	// Add traffic routing if specified
	if strategy.Canary.TrafficRouting != nil {
		canary["trafficRouting"] = b.buildTrafficRouting(strategy.Canary.TrafficRouting)
	}

	return map[string]interface{}{
		"canary": canary,
	}
}

// buildBlueGreenStrategy builds blue-green rollout strategy
func (b *RolloutBuilder) buildBlueGreenStrategy(strategy *appsv1alpha1.RolloutStrategy) map[string]interface{} {
	if strategy.BlueGreen == nil {
		return nil
	}

	blueGreen := map[string]interface{}{
		"activeService": strategy.BlueGreen.ActiveService,
	}

	if strategy.BlueGreen.PreviewService != "" {
		blueGreen["previewService"] = strategy.BlueGreen.PreviewService
	}

	if strategy.BlueGreen.AutoPromotionEnabled {
		blueGreen["autoPromotionEnabled"] = true
	}

	if strategy.BlueGreen.ScaleDownDelaySeconds > 0 {
		blueGreen["scaleDownDelaySeconds"] = int64(strategy.BlueGreen.ScaleDownDelaySeconds)
	}

	return map[string]interface{}{
		"blueGreen": blueGreen,
	}
}

// buildABTestStrategy builds A/B test rollout strategy
// In cross-cluster scenarios, ABTest uses different replicas based on cluster role
func (b *RolloutBuilder) buildABTestStrategy(strategy *appsv1alpha1.RolloutStrategy, assignment *ClusterReplicaAssignment) map[string]interface{} {
	if strategy.ABTest == nil || assignment == nil {
		return nil
	}

	// Check if this cluster is a candidate cluster
	isCandidate := false
	for _, c := range strategy.ABTest.CandidateClusters {
		if c == assignment.ClusterName {
			isCandidate = true
			break
		}
	}

	var canaryReplicas int32
	if isCandidate {
		// Candidate cluster gets the specified percentage
		canaryReplicas = int32(float64(assignment.TotalReplicas) * float64(strategy.ABTest.TrafficSplit) / 100.0)
	} else {
		// Baseline cluster gets no canary (all stable)
		canaryReplicas = 0
	}

	steps := []interface{}{
		map[string]interface{}{
			"replicas": int64(canaryReplicas), // Use int64 for JSON compatibility
		},
	}

	canary := map[string]interface{}{
		"steps": steps,
	}

	return map[string]interface{}{
		"canary": canary,
	}
}

// buildCanarySteps builds canary steps
func (b *RolloutBuilder) buildCanarySteps(steps []appsv1alpha1.CanaryStep) []interface{} {
	var result []interface{}

	for _, step := range steps {
		stepObj := map[string]interface{}{
			"replicas": fmt.Sprintf("%d%%", step.Weight),
		}

		// Add pause configuration
		if step.Pause != nil && step.Pause.Duration != nil {
			stepObj["pause"] = map[string]interface{}{
				"duration": int64(*step.Pause.Duration),
			}
		}

		result = append(result, stepObj)
	}

	return result
}

// buildTrafficRouting builds traffic routing configuration
func (b *RolloutBuilder) buildTrafficRouting(tr *appsv1alpha1.TrafficRouting) map[string]interface{} {
	traffic := map[string]interface{}{}

	if tr.Istio != nil {
		istioConfig := map[string]interface{}{
			"virtualService": map[string]interface{}{
				"name": tr.Istio.VirtualService,
			},
		}
		if tr.Istio.DestinationRule != "" {
			istioConfig["destinationRule"] = map[string]interface{}{
				"name": tr.Istio.DestinationRule,
			}
		}
		traffic["istio"] = istioConfig
	}

	if tr.Nginx != nil {
		traffic["nginx"] = map[string]interface{}{
			"annotation": map[string]interface{}{
				"name": tr.Nginx.Ingress,
			},
		}
	}

	return traffic
}

// BuildEmptyRollout builds an empty Rollout for deletion
func (b *RolloutBuilder) BuildEmptyRollout(app *appsv1alpha1.Application) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "rollouts.kruise.io/v1alpha1",
			"kind":       "Rollout",
			"metadata": map[string]interface{}{
				"name":      app.Name,
				"namespace": app.Namespace,
			},
		},
	}
}

// getRolloutGVK returns the GroupVersionKind for Rollout
func getRolloutGVK() schema.GroupVersionKind {
	return schema.GroupVersionKind{
		Group:   "rollouts.kruise.io",
		Version: "v1alpha1",
		Kind:    "Rollout",
	}
}
