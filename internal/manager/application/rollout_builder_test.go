package application

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	appsv1alpha1 "github.com/fize/rocket/pkg/apis/apps/v1alpha1"
)

func TestRolloutBuilder_BuildRollout_Canary(t *testing.T) {
	app := &appsv1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-app",
			Namespace: "default",
			UID:       "test-uid",
		},
		Spec: appsv1alpha1.ApplicationSpec{
			Workload: appsv1alpha1.WorkloadGVK{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
			},
			RolloutStrategy: &appsv1alpha1.RolloutStrategy{
				Type: appsv1alpha1.RolloutTypeCanary,
				Canary: &appsv1alpha1.CanaryStrategy{
					Steps: []appsv1alpha1.CanaryStep{
						{Weight: 20},
						{Weight: 50},
						{Weight: 100},
					},
				},
			},
		},
	}

	builder := NewRolloutBuilder()
	// No assignment - falls back to percentage-based steps
	rollout, err := builder.BuildRollout(app, "cluster-a", nil)

	assert.NoError(t, err)
	assert.NotNil(t, rollout)
	assert.Equal(t, "rollouts.kruise.io/v1alpha1", rollout.GetAPIVersion())
	assert.Equal(t, "Rollout", rollout.GetKind())
	assert.Equal(t, "test-app", rollout.GetName())
	assert.Equal(t, "default", rollout.GetNamespace())

	// Verify spec
	spec, found, _ := unstructured.NestedMap(rollout.Object, "spec")
	assert.True(t, found)
	assert.Contains(t, spec, "objectRef")
	assert.Contains(t, spec, "strategy")

	// Verify strategy
	strategy, found, _ := unstructured.NestedMap(spec, "strategy")
	assert.True(t, found)
	assert.Contains(t, strategy, "canary")
}

func TestRolloutBuilder_BuildRollout_CanaryWithPause(t *testing.T) {
	duration := int32(300)
	app := &appsv1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-app",
			Namespace: "default",
			UID:       "test-uid",
		},
		Spec: appsv1alpha1.ApplicationSpec{
			Workload: appsv1alpha1.WorkloadGVK{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
			},
			RolloutStrategy: &appsv1alpha1.RolloutStrategy{
				Type: appsv1alpha1.RolloutTypeCanary,
				Canary: &appsv1alpha1.CanaryStrategy{
					Steps: []appsv1alpha1.CanaryStep{
						{Weight: 20, Pause: &appsv1alpha1.RolloutPause{Duration: &duration}},
						{Weight: 50},
						{Weight: 100, Pause: &appsv1alpha1.RolloutPause{Duration: &duration}},
					},
				},
			},
		},
	}

	builder := NewRolloutBuilder()
	rollout, err := builder.BuildRollout(app, "cluster-a", nil)

	assert.NoError(t, err)
	assert.NotNil(t, rollout)

	// Verify canary steps with pause
	spec, _, _ := unstructured.NestedMap(rollout.Object, "spec")
	strategy, _, _ := unstructured.NestedMap(spec, "strategy")
	canary, _, _ := unstructured.NestedMap(strategy, "canary")
	steps, _, _ := unstructured.NestedSlice(canary, "steps")
	assert.Len(t, steps, 3)

	// First step should have pause
	step1, _ := steps[0].(map[string]interface{})
	assert.Equal(t, "20%", step1["replicas"])
	assert.Contains(t, step1, "pause")
	pause1, _ := step1["pause"].(map[string]interface{})
	assert.Equal(t, int64(300), pause1["duration"])

	// Second step should not have pause
	step2, _ := steps[1].(map[string]interface{})
	assert.Equal(t, "50%", step2["replicas"])
	assert.NotContains(t, step2, "pause")
}

func TestRolloutBuilder_BuildRollout_CanaryWithIstioTrafficRouting(t *testing.T) {
	app := &appsv1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-app",
			Namespace: "default",
			UID:       "test-uid",
		},
		Spec: appsv1alpha1.ApplicationSpec{
			Workload: appsv1alpha1.WorkloadGVK{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
			},
			RolloutStrategy: &appsv1alpha1.RolloutStrategy{
				Type: appsv1alpha1.RolloutTypeCanary,
				Canary: &appsv1alpha1.CanaryStrategy{
					Steps: []appsv1alpha1.CanaryStep{
						{Weight: 20},
						{Weight: 100},
					},
					TrafficRouting: &appsv1alpha1.TrafficRouting{
						Istio: &appsv1alpha1.IstioTrafficRouting{
							VirtualService:  "test-app-vs",
							DestinationRule: "test-app-dr",
						},
					},
				},
			},
		},
	}

	builder := NewRolloutBuilder()
	rollout, err := builder.BuildRollout(app, "cluster-a", nil)

	assert.NoError(t, err)
	assert.NotNil(t, rollout)

	// Verify traffic routing
	spec, _, _ := unstructured.NestedMap(rollout.Object, "spec")
	strategy, _, _ := unstructured.NestedMap(spec, "strategy")
	canary, _, _ := unstructured.NestedMap(strategy, "canary")
	trafficRouting, _, _ := unstructured.NestedMap(canary, "trafficRouting")
	istio, _, _ := unstructured.NestedMap(trafficRouting, "istio")
	vs, _, _ := unstructured.NestedMap(istio, "virtualService")
	dr, _, _ := unstructured.NestedMap(istio, "destinationRule")

	assert.Equal(t, "test-app-vs", vs["name"])
	assert.Equal(t, "test-app-dr", dr["name"])
}

func TestRolloutBuilder_BuildRollout_CanaryWithNginxTrafficRouting(t *testing.T) {
	app := &appsv1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-app",
			Namespace: "default",
			UID:       "test-uid",
		},
		Spec: appsv1alpha1.ApplicationSpec{
			Workload: appsv1alpha1.WorkloadGVK{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
			},
			RolloutStrategy: &appsv1alpha1.RolloutStrategy{
				Type: appsv1alpha1.RolloutTypeCanary,
				Canary: &appsv1alpha1.CanaryStrategy{
					Steps: []appsv1alpha1.CanaryStep{
						{Weight: 20},
						{Weight: 100},
					},
					TrafficRouting: &appsv1alpha1.TrafficRouting{
						Nginx: &appsv1alpha1.NginxTrafficRouting{
							Ingress: "test-app-ingress",
						},
					},
				},
			},
		},
	}

	builder := NewRolloutBuilder()
	rollout, err := builder.BuildRollout(app, "cluster-a", nil)

	assert.NoError(t, err)
	assert.NotNil(t, rollout)

	// Verify nginx traffic routing
	spec, _, _ := unstructured.NestedMap(rollout.Object, "spec")
	strategy, _, _ := unstructured.NestedMap(spec, "strategy")
	canary, _, _ := unstructured.NestedMap(strategy, "canary")
	trafficRouting, _, _ := unstructured.NestedMap(canary, "trafficRouting")
	nginx, _, _ := unstructured.NestedMap(trafficRouting, "nginx")
	annotation, _, _ := unstructured.NestedMap(nginx, "annotation")

	assert.Equal(t, "test-app-ingress", annotation["name"])
}

func TestRolloutBuilder_BuildRollout_CanaryWithBothTrafficRouting(t *testing.T) {
	app := &appsv1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-app",
			Namespace: "default",
			UID:       "test-uid",
		},
		Spec: appsv1alpha1.ApplicationSpec{
			Workload: appsv1alpha1.WorkloadGVK{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
			},
			RolloutStrategy: &appsv1alpha1.RolloutStrategy{
				Type: appsv1alpha1.RolloutTypeCanary,
				Canary: &appsv1alpha1.CanaryStrategy{
					Steps: []appsv1alpha1.CanaryStep{
						{Weight: 20},
						{Weight: 100},
					},
					TrafficRouting: &appsv1alpha1.TrafficRouting{
						Istio: &appsv1alpha1.IstioTrafficRouting{
							VirtualService: "test-app-vs",
						},
						Nginx: &appsv1alpha1.NginxTrafficRouting{
							Ingress: "test-app-ingress",
						},
					},
				},
			},
		},
	}

	builder := NewRolloutBuilder()
	rollout, err := builder.BuildRollout(app, "cluster-a", nil)

	assert.NoError(t, err)
	assert.NotNil(t, rollout)

	// Verify both traffic routing configurations
	spec, _, _ := unstructured.NestedMap(rollout.Object, "spec")
	strategy, _, _ := unstructured.NestedMap(spec, "strategy")
	canary, _, _ := unstructured.NestedMap(strategy, "canary")
	trafficRouting, _, _ := unstructured.NestedMap(canary, "trafficRouting")

	assert.Contains(t, trafficRouting, "istio")
	assert.Contains(t, trafficRouting, "nginx")
}

func TestRolloutBuilder_BuildRollout_CanaryNilStrategy(t *testing.T) {
	app := &appsv1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-app",
			Namespace: "default",
			UID:       "test-uid",
		},
		Spec: appsv1alpha1.ApplicationSpec{
			Workload: appsv1alpha1.WorkloadGVK{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
			},
			RolloutStrategy: &appsv1alpha1.RolloutStrategy{
				Type:   appsv1alpha1.RolloutTypeCanary,
				Canary: nil, // nil canary strategy
			},
		},
	}

	builder := NewRolloutBuilder()
	rollout, err := builder.BuildRollout(app, "cluster-a", nil)

	assert.NoError(t, err)
	assert.NotNil(t, rollout)

	// Strategy should be nil when canary is nil
	spec, _, _ := unstructured.NestedMap(rollout.Object, "spec")
	strategy, found, _ := unstructured.NestedMap(spec, "strategy")
	assert.False(t, found)
	assert.Nil(t, strategy)
}

func TestRolloutBuilder_BuildRollout_CanaryEmptySteps(t *testing.T) {
	app := &appsv1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-app",
			Namespace: "default",
			UID:       "test-uid",
		},
		Spec: appsv1alpha1.ApplicationSpec{
			Workload: appsv1alpha1.WorkloadGVK{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
			},
			RolloutStrategy: &appsv1alpha1.RolloutStrategy{
				Type: appsv1alpha1.RolloutTypeCanary,
				Canary: &appsv1alpha1.CanaryStrategy{
					Steps: []appsv1alpha1.CanaryStep{}, // empty steps
				},
			},
		},
	}

	builder := NewRolloutBuilder()
	rollout, err := builder.BuildRollout(app, "cluster-a", nil)

	assert.NoError(t, err)
	assert.NotNil(t, rollout)

	// Empty steps should result in empty steps array
	spec, _, _ := unstructured.NestedMap(rollout.Object, "spec")
	strategy, _, _ := unstructured.NestedMap(spec, "strategy")
	canary, _, _ := unstructured.NestedMap(strategy, "canary")
	steps, _, _ := unstructured.NestedSlice(canary, "steps")
	assert.Len(t, steps, 0)
}

func TestRolloutBuilder_BuildRollout_BlueGreen(t *testing.T) {
	app := &appsv1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-app",
			Namespace: "default",
		},
		Spec: appsv1alpha1.ApplicationSpec{
			Workload: appsv1alpha1.WorkloadGVK{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
			},
			RolloutStrategy: &appsv1alpha1.RolloutStrategy{
				Type: appsv1alpha1.RolloutTypeBlueGreen,
				BlueGreen: &appsv1alpha1.BlueGreenStrategy{
					ActiveService:         "test-app-active",
					PreviewService:        "test-app-preview",
					AutoPromotionEnabled:  true,
					ScaleDownDelaySeconds: 30,
				},
			},
		},
	}

	builder := NewRolloutBuilder()
	rollout, err := builder.BuildRollout(app, "cluster-a", nil)

	assert.NoError(t, err)
	assert.NotNil(t, rollout)

	spec, _, _ := unstructured.NestedMap(rollout.Object, "spec")
	strategy, _, _ := unstructured.NestedMap(spec, "strategy")
	blueGreen, _, _ := unstructured.NestedMap(strategy, "blueGreen")

	assert.Equal(t, "test-app-active", blueGreen["activeService"])
	assert.Equal(t, "test-app-preview", blueGreen["previewService"])
	assert.Equal(t, true, blueGreen["autoPromotionEnabled"])
	assert.Equal(t, int64(30), blueGreen["scaleDownDelaySeconds"])
}

func TestRolloutBuilder_BuildRollout_BlueGreenMinimal(t *testing.T) {
	// BlueGreen with minimal configuration (only required fields)
	app := &appsv1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-app",
			Namespace: "default",
		},
		Spec: appsv1alpha1.ApplicationSpec{
			Workload: appsv1alpha1.WorkloadGVK{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
			},
			RolloutStrategy: &appsv1alpha1.RolloutStrategy{
				Type: appsv1alpha1.RolloutTypeBlueGreen,
				BlueGreen: &appsv1alpha1.BlueGreenStrategy{
					ActiveService: "test-app-active",
					// PreviewService empty
					// AutoPromotionEnabled false
					// ScaleDownDelaySeconds 0
				},
			},
		},
	}

	builder := NewRolloutBuilder()
	rollout, err := builder.BuildRollout(app, "cluster-a", nil)

	assert.NoError(t, err)
	assert.NotNil(t, rollout)

	spec, _, _ := unstructured.NestedMap(rollout.Object, "spec")
	strategy, _, _ := unstructured.NestedMap(spec, "strategy")
	blueGreen, _, _ := unstructured.NestedMap(strategy, "blueGreen")

	assert.Equal(t, "test-app-active", blueGreen["activeService"])
	assert.NotContains(t, blueGreen, "previewService")
	assert.NotContains(t, blueGreen, "autoPromotionEnabled")
	assert.NotContains(t, blueGreen, "scaleDownDelaySeconds")
}

func TestRolloutBuilder_BuildRollout_BlueGreenNilStrategy(t *testing.T) {
	app := &appsv1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-app",
			Namespace: "default",
		},
		Spec: appsv1alpha1.ApplicationSpec{
			Workload: appsv1alpha1.WorkloadGVK{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
			},
			RolloutStrategy: &appsv1alpha1.RolloutStrategy{
				Type:      appsv1alpha1.RolloutTypeBlueGreen,
				BlueGreen: nil, // nil blueGreen strategy
			},
		},
	}

	builder := NewRolloutBuilder()
	rollout, err := builder.BuildRollout(app, "cluster-a", nil)

	assert.NoError(t, err)
	assert.NotNil(t, rollout)

	spec, _, _ := unstructured.NestedMap(rollout.Object, "spec")
	strategy, found, _ := unstructured.NestedMap(spec, "strategy")
	assert.False(t, found)
	assert.Nil(t, strategy)
}

func TestRolloutBuilder_BuildRollout_ABTest(t *testing.T) {
	app := &appsv1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-app",
			Namespace: "default",
		},
		Spec: appsv1alpha1.ApplicationSpec{
			Workload: appsv1alpha1.WorkloadGVK{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
			},
			RolloutStrategy: &appsv1alpha1.RolloutStrategy{
				Type: appsv1alpha1.RolloutTypeABTest,
				ABTest: &appsv1alpha1.ABTestStrategy{
					BaselineCluster:   "cluster-a",
					CandidateClusters: []string{"cluster-b", "cluster-c"},
					TrafficSplit:      30,
				},
			},
		},
	}

	builder := NewRolloutBuilder()
	// Candidate cluster with 50 total replicas
	assignment := &ClusterReplicaAssignment{
		ClusterName:   "cluster-b",
		TotalReplicas: 50,
	}
	rollout, err := builder.BuildRollout(app, "cluster-b", assignment)

	assert.NoError(t, err)
	assert.NotNil(t, rollout)

	spec, _, _ := unstructured.NestedMap(rollout.Object, "spec")
	strategy, _, _ := unstructured.NestedMap(spec, "strategy")
	canary, _, _ := unstructured.NestedMap(strategy, "canary")
	steps, _, _ := unstructured.NestedSlice(canary, "steps")

	assert.Len(t, steps, 1)
	step, _ := steps[0].(map[string]interface{})
	// 30% of 50 = 15 replicas
	assert.Equal(t, int64(15), step["replicas"])
}

func TestRolloutBuilder_BuildRollout_ABTestNilStrategy(t *testing.T) {
	app := &appsv1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-app",
			Namespace: "default",
		},
		Spec: appsv1alpha1.ApplicationSpec{
			Workload: appsv1alpha1.WorkloadGVK{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
			},
			RolloutStrategy: &appsv1alpha1.RolloutStrategy{
				Type:   appsv1alpha1.RolloutTypeABTest,
				ABTest: nil, // nil abtest strategy
			},
		},
	}

	builder := NewRolloutBuilder()
	rollout, err := builder.BuildRollout(app, "cluster-a", nil)

	assert.NoError(t, err)
	assert.NotNil(t, rollout)

	spec, _, _ := unstructured.NestedMap(rollout.Object, "spec")
	strategy, found, _ := unstructured.NestedMap(spec, "strategy")
	assert.False(t, found)
	assert.Nil(t, strategy)
}

func TestRolloutBuilder_BuildRollout_NoStrategy(t *testing.T) {
	app := &appsv1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-app",
			Namespace: "default",
		},
	}

	builder := NewRolloutBuilder()
	rollout, err := builder.BuildRollout(app, "cluster-a", nil)

	assert.Error(t, err)
	assert.Nil(t, rollout)
	assert.Contains(t, err.Error(), "no rollout strategy")
}

func TestRolloutBuilder_BuildEmptyRollout(t *testing.T) {
	app := &appsv1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-app",
			Namespace: "default",
		},
	}

	builder := NewRolloutBuilder()
	rollout := builder.BuildEmptyRollout(app)

	assert.NotNil(t, rollout)
	assert.Equal(t, "rollouts.kruise.io/v1alpha1", rollout.GetAPIVersion())
	assert.Equal(t, "Rollout", rollout.GetKind())
	assert.Equal(t, "test-app", rollout.GetName())
	assert.Equal(t, "default", rollout.GetNamespace())
}

func TestRolloutBuilder_GetRolloutGVK(t *testing.T) {
	gvk := getRolloutGVK()

	assert.Equal(t, "rollouts.kruise.io", gvk.Group)
	assert.Equal(t, "v1alpha1", gvk.Version)
	assert.Equal(t, "Rollout", gvk.Kind)
}

func TestRolloutBuilder_BuildRollout_OwnerReferences(t *testing.T) {
	app := &appsv1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-app",
			Namespace: "default",
			UID:       "test-uid-12345",
		},
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1alpha1",
			Kind:       "Application",
		},
		Spec: appsv1alpha1.ApplicationSpec{
			Workload: appsv1alpha1.WorkloadGVK{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
			},
			RolloutStrategy: &appsv1alpha1.RolloutStrategy{
				Type: appsv1alpha1.RolloutTypeCanary,
				Canary: &appsv1alpha1.CanaryStrategy{
					Steps: []appsv1alpha1.CanaryStep{
						{Weight: 100},
					},
				},
			},
		},
	}

	builder := NewRolloutBuilder()
	rollout, err := builder.BuildRollout(app, "cluster-a", nil)

	assert.NoError(t, err)
	assert.NotNil(t, rollout)

	// Verify owner references
	ownerRefs := rollout.GetOwnerReferences()
	assert.Len(t, ownerRefs, 1)
	assert.Equal(t, "apps/v1alpha1", ownerRefs[0].APIVersion)
	assert.Equal(t, "Application", ownerRefs[0].Kind)
	assert.Equal(t, "test-app", ownerRefs[0].Name)
	assert.Equal(t, "test-uid-12345", string(ownerRefs[0].UID))
	assert.True(t, *ownerRefs[0].Controller)
	assert.True(t, *ownerRefs[0].BlockOwnerDeletion)
}

func TestRolloutBuilder_BuildRollout_Labels(t *testing.T) {
	app := &appsv1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-app",
			Namespace: "default",
		},
		Spec: appsv1alpha1.ApplicationSpec{
			Workload: appsv1alpha1.WorkloadGVK{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
			},
			RolloutStrategy: &appsv1alpha1.RolloutStrategy{
				Type: appsv1alpha1.RolloutTypeCanary,
				Canary: &appsv1alpha1.CanaryStrategy{
					Steps: []appsv1alpha1.CanaryStep{{Weight: 100}},
				},
			},
		},
	}

	builder := NewRolloutBuilder()
	rollout, err := builder.BuildRollout(app, "cluster-a", nil)

	assert.NoError(t, err)
	assert.NotNil(t, rollout)

	// Verify labels
	labels := rollout.GetLabels()
	assert.Equal(t, "test-app", labels["app.kubernetes.io/name"])
	assert.Equal(t, "rocket", labels["app.kubernetes.io/managed-by"])
	assert.Equal(t, "test-app", labels["rocket.io/application"])
	assert.Equal(t, "cluster-a", labels["rocket.io/cluster"])
}

func TestRolloutBuilder_BuildRollout_UnsupportedStrategyType(t *testing.T) {
	app := &appsv1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-app",
			Namespace: "default",
		},
		Spec: appsv1alpha1.ApplicationSpec{
			Workload: appsv1alpha1.WorkloadGVK{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
			},
			RolloutStrategy: &appsv1alpha1.RolloutStrategy{
				Type: "UnsupportedType",
				Canary: &appsv1alpha1.CanaryStrategy{
					Steps: []appsv1alpha1.CanaryStep{{Weight: 100}},
				},
			},
		},
	}

	builder := NewRolloutBuilder()
	rollout, err := builder.BuildRollout(app, "cluster-a", nil)

	assert.NoError(t, err)
	assert.NotNil(t, rollout)

	// Strategy should be nil for unsupported type
	spec, _, _ := unstructured.NestedMap(rollout.Object, "spec")
	strategy, found, _ := unstructured.NestedMap(spec, "strategy")
	assert.False(t, found)
	assert.Nil(t, strategy)
}

func TestRolloutBuilder_BuildRollout_WeightBoundaryValues(t *testing.T) {
	// Test with 0% and 100% weights
	app := &appsv1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-app",
			Namespace: "default",
		},
		Spec: appsv1alpha1.ApplicationSpec{
			Workload: appsv1alpha1.WorkloadGVK{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
			},
			RolloutStrategy: &appsv1alpha1.RolloutStrategy{
				Type: appsv1alpha1.RolloutTypeCanary,
				Canary: &appsv1alpha1.CanaryStrategy{
					Steps: []appsv1alpha1.CanaryStep{
						{Weight: 0},
						{Weight: 50},
						{Weight: 100},
					},
				},
			},
		},
	}

	builder := NewRolloutBuilder()
	rollout, err := builder.BuildRollout(app, "cluster-a", nil)

	assert.NoError(t, err)
	assert.NotNil(t, rollout)

	spec, _, _ := unstructured.NestedMap(rollout.Object, "spec")
	strategy, _, _ := unstructured.NestedMap(spec, "strategy")
	canary, _, _ := unstructured.NestedMap(strategy, "canary")
	steps, _, _ := unstructured.NestedSlice(canary, "steps")

	assert.Len(t, steps, 3)
	step0, _ := steps[0].(map[string]interface{})
	step1, _ := steps[1].(map[string]interface{})
	step2, _ := steps[2].(map[string]interface{})

	assert.Equal(t, "0%", step0["replicas"])
	assert.Equal(t, "50%", step1["replicas"])
	assert.Equal(t, "100%", step2["replicas"])
}

func TestRolloutBuilder_BuildRollout_CloneSetWorkload(t *testing.T) {
	app := &appsv1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-app",
			Namespace: "default",
		},
		Spec: appsv1alpha1.ApplicationSpec{
			Workload: appsv1alpha1.WorkloadGVK{
				APIVersion: "apps.kruise.io/v1alpha1",
				Kind:       "CloneSet",
			},
			RolloutStrategy: &appsv1alpha1.RolloutStrategy{
				Type: appsv1alpha1.RolloutTypeCanary,
				Canary: &appsv1alpha1.CanaryStrategy{
					Steps: []appsv1alpha1.CanaryStep{
						{Weight: 25},
						{Weight: 100},
					},
				},
			},
		},
	}

	builder := NewRolloutBuilder()
	rollout, err := builder.BuildRollout(app, "cluster-a", nil)

	assert.NoError(t, err)
	assert.NotNil(t, rollout)

	// Verify objectRef points to CloneSet
	spec, _, _ := unstructured.NestedMap(rollout.Object, "spec")
	objectRef, _, _ := unstructured.NestedMap(spec, "objectRef")
	workloadRef, _, _ := unstructured.NestedMap(objectRef, "workloadRef")

	assert.Equal(t, "apps.kruise.io/v1alpha1", workloadRef["apiVersion"])
	assert.Equal(t, "CloneSet", workloadRef["kind"])
	assert.Equal(t, "test-app", workloadRef["name"])
}

// ========== New tests for GlobalReplicaDistribution ==========

func TestRolloutBuilder_BuildRollout_CanaryWithGlobalDistribution(t *testing.T) {
	app := &appsv1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-app",
			Namespace: "default",
			UID:       "test-uid",
		},
		Spec: appsv1alpha1.ApplicationSpec{
			Workload: appsv1alpha1.WorkloadGVK{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
			},
			RolloutStrategy: &appsv1alpha1.RolloutStrategy{
				Type: appsv1alpha1.RolloutTypeCanary,
				Canary: &appsv1alpha1.CanaryStrategy{
					Steps: []appsv1alpha1.CanaryStep{
						{Weight: 20}, // This will be ignored when GlobalReplicaDistribution is set
					},
					GlobalReplicaDistribution: &appsv1alpha1.GlobalReplicaDistribution{
						Mode: appsv1alpha1.DistributionModeEqual,
					},
				},
			},
		},
	}

	// Assignment with 10 canary pods for this cluster
	assignment := &ClusterReplicaAssignment{
		ClusterName:    "cluster-a",
		TotalReplicas:  50,
		CanaryReplicas: 10,
		StableReplicas: 40,
		ClusterIndex:   0,
	}

	builder := NewRolloutBuilder()
	rollout, err := builder.BuildRollout(app, "cluster-a", assignment)

	assert.NoError(t, err)
	assert.NotNil(t, rollout)

	// Verify canary uses absolute replica count
	spec, _, _ := unstructured.NestedMap(rollout.Object, "spec")
	strategy, _, _ := unstructured.NestedMap(spec, "strategy")
	canary, _, _ := unstructured.NestedMap(strategy, "canary")
	steps, _, _ := unstructured.NestedSlice(canary, "steps")

	assert.Len(t, steps, 1)
	step, _ := steps[0].(map[string]interface{})
	// Should be absolute number, not percentage
	assert.Equal(t, int64(10), step["replicas"])
}

func TestRolloutBuilder_BuildRollout_ABTestWithAssignment(t *testing.T) {
	app := &appsv1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-app",
			Namespace: "default",
		},
		Spec: appsv1alpha1.ApplicationSpec{
			Workload: appsv1alpha1.WorkloadGVK{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
			},
			RolloutStrategy: &appsv1alpha1.RolloutStrategy{
				Type: appsv1alpha1.RolloutTypeABTest,
				ABTest: &appsv1alpha1.ABTestStrategy{
					BaselineCluster:   "cluster-a",
					CandidateClusters: []string{"cluster-b"},
					TrafficSplit:      30,
				},
			},
		},
	}

	// Test candidate cluster
	candidateAssignment := &ClusterReplicaAssignment{
		ClusterName:    "cluster-b",
		TotalReplicas:  100,
		CanaryReplicas: 0, // Will be calculated
		StableReplicas: 0,
		ClusterIndex:   1,
	}

	builder := NewRolloutBuilder()
	rollout, err := builder.BuildRollout(app, "cluster-b", candidateAssignment)

	assert.NoError(t, err)
	assert.NotNil(t, rollout)

	// Verify candidate cluster gets 30% of pods
	spec, _, _ := unstructured.NestedMap(rollout.Object, "spec")
	strategy, _, _ := unstructured.NestedMap(spec, "strategy")
	canary, _, _ := unstructured.NestedMap(strategy, "canary")
	steps, _, _ := unstructured.NestedSlice(canary, "steps")

	assert.Len(t, steps, 1)
	step, _ := steps[0].(map[string]interface{})
	assert.Equal(t, int64(30), step["replicas"]) // 30% of 100
}
