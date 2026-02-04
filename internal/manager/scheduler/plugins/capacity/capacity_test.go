package capacity

import (
	"context"
	"encoding/json"
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/hex-techs/rocket/internal/manager/scheduler/framework"
	appsv1alpha1 "github.com/hex-techs/rocket/pkg/apis/apps/v1alpha1"
	clusterv1alpha1 "github.com/hex-techs/rocket/pkg/apis/storage/v1alpha1"
)

func int32Ptr(i int32) *int32 {
	return &i
}

func toRaw(obj interface{}) runtime.RawExtension {
	b, _ := json.Marshal(obj)
	return runtime.RawExtension{Raw: b}
}

func TestCapacityFilter(t *testing.T) {
	tests := []struct {
		name           string
		app            *appsv1alpha1.Application
		cluster        *clusterv1alpha1.ManagedCluster
		expectedStatus int
	}{
		{
			name: "no template - should pass",
			app: &appsv1alpha1.Application{
				Spec: appsv1alpha1.ApplicationSpec{},
			},
			cluster: &clusterv1alpha1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster1",
				},
			},
			expectedStatus: framework.Success,
		},
		{
			name: "no resource summary - should fail",
			app: &appsv1alpha1.Application{
				Spec: appsv1alpha1.ApplicationSpec{
					Template: toRaw(&v1.PodTemplateSpec{
						Spec: v1.PodSpec{
							Containers: []v1.Container{
								{
									Name: "app",
									Resources: v1.ResourceRequirements{
										Requests: v1.ResourceList{
											v1.ResourceCPU:    resource.MustParse("100m"),
											v1.ResourceMemory: resource.MustParse("128Mi"),
										},
									},
								},
							},
						},
					}),
				},
			},
			cluster: &clusterv1alpha1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster1",
				},
				Status: clusterv1alpha1.ManagedClusterStatus{},
			},
			expectedStatus: framework.Unschedulable,
		},
		{
			name: "sufficient resources",
			app: &appsv1alpha1.Application{
				Spec: appsv1alpha1.ApplicationSpec{
					Replicas: int32Ptr(2),
					Template: toRaw(&v1.PodTemplateSpec{
						Spec: v1.PodSpec{
							Containers: []v1.Container{
								{
									Name: "app",
									Resources: v1.ResourceRequirements{
										Requests: v1.ResourceList{
											v1.ResourceCPU:    resource.MustParse("100m"),
											v1.ResourceMemory: resource.MustParse("128Mi"),
										},
									},
								},
							},
						},
					}),
				},
			},
			cluster: &clusterv1alpha1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster1",
				},
				Status: clusterv1alpha1.ManagedClusterStatus{
					ResourceSummary: []clusterv1alpha1.ResourceSummary{
						{
							Allocatable: v1.ResourceList{
								v1.ResourceCPU:    resource.MustParse("4"),
								v1.ResourceMemory: resource.MustParse("8Gi"),
							},
							Allocated: v1.ResourceList{
								v1.ResourceCPU:    resource.MustParse("1"),
								v1.ResourceMemory: resource.MustParse("2Gi"),
							},
						},
					},
				},
			},
			expectedStatus: framework.Success,
		},
		{
			name: "partial CPU fit (should pass)",
			app: &appsv1alpha1.Application{
				Spec: appsv1alpha1.ApplicationSpec{
					Replicas: int32Ptr(2),
					Template: toRaw(&v1.PodTemplateSpec{
						Spec: v1.PodSpec{
							Containers: []v1.Container{
								{
									Name: "app",
									Resources: v1.ResourceRequirements{
										Requests: v1.ResourceList{
											v1.ResourceCPU:    resource.MustParse("2"),
											v1.ResourceMemory: resource.MustParse("128Mi"),
										},
									},
								},
							},
						},
					}),
				},
			},
			cluster: &clusterv1alpha1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster1",
				},
				Status: clusterv1alpha1.ManagedClusterStatus{
					ResourceSummary: []clusterv1alpha1.ResourceSummary{
						{
							Allocatable: v1.ResourceList{
								v1.ResourceCPU:    resource.MustParse("4"),
								v1.ResourceMemory: resource.MustParse("8Gi"),
							},
							Allocated: v1.ResourceList{
								v1.ResourceCPU:    resource.MustParse("1"),
								v1.ResourceMemory: resource.MustParse("2Gi"),
							},
						},
					},
				},
			},
			expectedStatus: framework.Success,
		},
		{
			name: "partial memory fit (should pass)",
			app: &appsv1alpha1.Application{
				Spec: appsv1alpha1.ApplicationSpec{
					Replicas: int32Ptr(2),
					Template: toRaw(&v1.PodTemplateSpec{
						Spec: v1.PodSpec{
							Containers: []v1.Container{
								{
									Name: "app",
									Resources: v1.ResourceRequirements{
										Requests: v1.ResourceList{
											v1.ResourceCPU:    resource.MustParse("100m"),
											v1.ResourceMemory: resource.MustParse("4Gi"),
										},
									},
								},
							},
						},
					}),
				},
			},
			cluster: &clusterv1alpha1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster1",
				},
				Status: clusterv1alpha1.ManagedClusterStatus{
					ResourceSummary: []clusterv1alpha1.ResourceSummary{
						{
							Allocatable: v1.ResourceList{
								v1.ResourceCPU:    resource.MustParse("4"),
								v1.ResourceMemory: resource.MustParse("8Gi"),
							},
							Allocated: v1.ResourceList{
								v1.ResourceCPU:    resource.MustParse("1"),
								v1.ResourceMemory: resource.MustParse("2Gi"),
							},
						},
					},
				},
			},
			expectedStatus: framework.Success,
		},
		{
			name: "truly insufficient CPU",
			app: &appsv1alpha1.Application{
				Spec: appsv1alpha1.ApplicationSpec{
					Replicas: int32Ptr(1),
					Template: toRaw(&v1.PodTemplateSpec{
						Spec: v1.PodSpec{
							Containers: []v1.Container{
								{
									Name: "app",
									Resources: v1.ResourceRequirements{
										Requests: v1.ResourceList{
											v1.ResourceCPU: resource.MustParse("4"),
										},
									},
								},
							},
						},
					}),
				},
			},
			cluster: &clusterv1alpha1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster1",
				},
				Status: clusterv1alpha1.ManagedClusterStatus{
					ResourceSummary: []clusterv1alpha1.ResourceSummary{
						{
							Allocatable: v1.ResourceList{
								v1.ResourceCPU: resource.MustParse("4"),
							},
							Allocated: v1.ResourceList{
								v1.ResourceCPU: resource.MustParse("1"),
							},
						},
					},
				},
			},
			expectedStatus: framework.Unschedulable,
		},
	}

	plugin := New().(*Capacity)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := framework.NewCycleState()
			status := plugin.Filter(context.Background(), state, tt.app, tt.cluster)
			if status.Code != tt.expectedStatus {
				t.Errorf("expected status %d, got %d: %s", tt.expectedStatus, status.Code, status.Message)
			}
		})
	}
}

func TestCapacityName(t *testing.T) {
	plugin := New()
	if plugin.Name() != Name {
		t.Errorf("expected name %s, got %s", Name, plugin.Name())
	}
}

func TestFitsWithReplicas(t *testing.T) {
	tests := []struct {
		name        string
		allocatable v1.ResourceList
		allocated   v1.ResourceList
		required    v1.ResourceList
		resource    v1.ResourceName
		replicas    int64
		expected    bool
	}{
		{
			name: "fits with 1 replica",
			allocatable: v1.ResourceList{
				v1.ResourceCPU: resource.MustParse("4"),
			},
			allocated: v1.ResourceList{
				v1.ResourceCPU: resource.MustParse("2"),
			},
			required: v1.ResourceList{
				v1.ResourceCPU: resource.MustParse("1"),
			},
			resource: v1.ResourceCPU,
			replicas: 1,
			expected: true,
		},
		{
			name: "fits with multiple replicas",
			allocatable: v1.ResourceList{
				v1.ResourceCPU: resource.MustParse("10"),
			},
			allocated: v1.ResourceList{
				v1.ResourceCPU: resource.MustParse("2"),
			},
			required: v1.ResourceList{
				v1.ResourceCPU: resource.MustParse("2"),
			},
			resource: v1.ResourceCPU,
			replicas: 3,
			expected: true,
		},
		{
			name: "does not fit - insufficient",
			allocatable: v1.ResourceList{
				v1.ResourceCPU: resource.MustParse("4"),
			},
			allocated: v1.ResourceList{
				v1.ResourceCPU: resource.MustParse("3"),
			},
			required: v1.ResourceList{
				v1.ResourceCPU: resource.MustParse("2"),
			},
			resource: v1.ResourceCPU,
			replicas: 1,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := fitsWithReplicas(tt.allocatable, tt.allocated, tt.required, tt.resource, tt.replicas)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestCalculateMaxReplicas(t *testing.T) {
	tests := []struct {
		name     string
		app      *appsv1alpha1.Application
		cluster  *clusterv1alpha1.ManagedCluster
		expected int64
	}{
		{
			name: "no resources in app - return max",
			app: &appsv1alpha1.Application{
				Spec: appsv1alpha1.ApplicationSpec{},
			},
			cluster: &clusterv1alpha1.ManagedCluster{
				Status: clusterv1alpha1.ManagedClusterStatus{
					ResourceSummary: []clusterv1alpha1.ResourceSummary{
						{
							Allocatable: v1.ResourceList{
								v1.ResourceCPU: resource.MustParse("4"),
							},
							Allocated: v1.ResourceList{
								v1.ResourceCPU: resource.MustParse("2"),
							},
						},
					},
				},
			},
			// When no resources in podReq, returns math.MaxInt64
			expected: 9223372036854775807,
		},
		{
			name: "no resource summary - return 0",
			app: &appsv1alpha1.Application{
				Spec: appsv1alpha1.ApplicationSpec{
					Template: toRaw(&v1.PodTemplateSpec{
						Spec: v1.PodSpec{
							Containers: []v1.Container{
								{
									Resources: v1.ResourceRequirements{
										Requests: v1.ResourceList{
											v1.ResourceCPU: resource.MustParse("1"),
										},
									},
								},
							},
						},
					}),
				},
			},
			cluster:  &clusterv1alpha1.ManagedCluster{},
			expected: 0,
		},
		{
			name: "calculate max based on CPU",
			app: &appsv1alpha1.Application{
				Spec: appsv1alpha1.ApplicationSpec{
					Template: toRaw(&v1.PodTemplateSpec{
						Spec: v1.PodSpec{
							Containers: []v1.Container{
								{
									Resources: v1.ResourceRequirements{
										Requests: v1.ResourceList{
											v1.ResourceCPU: resource.MustParse("1"),
										},
									},
								},
							},
						},
					}),
				},
			},
			cluster: &clusterv1alpha1.ManagedCluster{
				Status: clusterv1alpha1.ManagedClusterStatus{
					ResourceSummary: []clusterv1alpha1.ResourceSummary{
						{
							Allocatable: v1.ResourceList{
								v1.ResourceCPU: resource.MustParse("4"),
							},
							Allocated: v1.ResourceList{
								v1.ResourceCPU: resource.MustParse("1"),
							},
						},
					},
				},
			},
			expected: 3, // (4-1)/1 = 3
		},
		{
			name: "calculate max - limited by smallest resource",
			app: &appsv1alpha1.Application{
				Spec: appsv1alpha1.ApplicationSpec{
					Template: toRaw(&v1.PodTemplateSpec{
						Spec: v1.PodSpec{
							Containers: []v1.Container{
								{
									Resources: v1.ResourceRequirements{
										Requests: v1.ResourceList{
											v1.ResourceCPU:    resource.MustParse("1"),
											v1.ResourceMemory: resource.MustParse("2Gi"),
										},
									},
								},
							},
						},
					}),
				},
			},
			cluster: &clusterv1alpha1.ManagedCluster{
				Status: clusterv1alpha1.ManagedClusterStatus{
					ResourceSummary: []clusterv1alpha1.ResourceSummary{
						{
							Allocatable: v1.ResourceList{
								v1.ResourceCPU:    resource.MustParse("10"),
								v1.ResourceMemory: resource.MustParse("8Gi"),
							},
							Allocated: v1.ResourceList{
								v1.ResourceCPU:    resource.MustParse("0"),
								v1.ResourceMemory: resource.MustParse("2Gi"),
							},
						},
					},
				},
			},
			expected: 3, // min(10/1, 6Gi/2Gi) = min(10, 3) = 3
		},
		{
			name: "no available resources - return 0",
			app: &appsv1alpha1.Application{
				Spec: appsv1alpha1.ApplicationSpec{
					Template: toRaw(&v1.PodTemplateSpec{
						Spec: v1.PodSpec{
							Containers: []v1.Container{
								{
									Resources: v1.ResourceRequirements{
										Requests: v1.ResourceList{
											v1.ResourceCPU: resource.MustParse("1"),
										},
									},
								},
							},
						},
					}),
				},
			},
			cluster: &clusterv1alpha1.ManagedCluster{
				Status: clusterv1alpha1.ManagedClusterStatus{
					ResourceSummary: []clusterv1alpha1.ResourceSummary{
						{
							Allocatable: v1.ResourceList{
								v1.ResourceCPU: resource.MustParse("4"),
							},
							Allocated: v1.ResourceList{
								v1.ResourceCPU: resource.MustParse("4"),
							},
						},
					},
				},
			},
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CalculateMaxReplicas(tt.cluster, tt.app)
			if result != tt.expected {
				t.Errorf("expected %d, got %d", tt.expected, result)
			}
		})
	}
}

func TestCapacityFilter_InsufficientMemory(t *testing.T) {
	plugin := New().(*Capacity)
	state := framework.NewCycleState()

	app := &appsv1alpha1.Application{
		Spec: appsv1alpha1.ApplicationSpec{
			Template: toRaw(&v1.PodTemplateSpec{
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Resources: v1.ResourceRequirements{
								Requests: v1.ResourceList{
									v1.ResourceCPU:    resource.MustParse("100m"),
									v1.ResourceMemory: resource.MustParse("8Gi"),
								},
							},
						},
					},
				},
			}),
		},
	}

	cluster := &clusterv1alpha1.ManagedCluster{
		Status: clusterv1alpha1.ManagedClusterStatus{
			ResourceSummary: []clusterv1alpha1.ResourceSummary{
				{
					Allocatable: v1.ResourceList{
						v1.ResourceCPU:    resource.MustParse("10"),
						v1.ResourceMemory: resource.MustParse("8Gi"),
					},
					Allocated: v1.ResourceList{
						v1.ResourceCPU:    resource.MustParse("1"),
						v1.ResourceMemory: resource.MustParse("4Gi"),
					},
				},
			},
		},
	}

	status := plugin.Filter(context.Background(), state, app, cluster)
	if status.Code != framework.Unschedulable {
		t.Errorf("expected Unschedulable, got %d: %s", status.Code, status.Message)
	}
}
