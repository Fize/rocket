package volumerestriction

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/fize/rocket/internal/manager/scheduler/framework"
	appsv1alpha1 "github.com/fize/rocket/pkg/apis/apps/v1alpha1"
	clusterv1alpha1 "github.com/fize/rocket/pkg/apis/storage/v1alpha1"
	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

func TestNew(t *testing.T) {
	plugin := New()
	assert.NotNil(t, plugin)
	assert.Equal(t, Name, plugin.Name())
}

func TestVolumeRestriction_Name(t *testing.T) {
	vr := &VolumeRestriction{}
	assert.Equal(t, "VolumeRestriction", vr.Name())
}

func TestVolumeRestriction_Filter(t *testing.T) {
	tests := []struct {
		name              string
		app               *appsv1alpha1.Application
		cluster           *clusterv1alpha1.ManagedCluster
		expectedIsSuccess bool
	}{
		{
			name: "no template - should pass",
			app: &appsv1alpha1.Application{
				ObjectMeta: metav1.ObjectMeta{Name: "test-app"},
				Spec:       appsv1alpha1.ApplicationSpec{},
			},
			cluster: &clusterv1alpha1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster-1"},
			},
			expectedIsSuccess: true,
		},
		{
			name: "no PVC - should pass",
			app: &appsv1alpha1.Application{
				ObjectMeta: metav1.ObjectMeta{Name: "test-app"},
				Spec: appsv1alpha1.ApplicationSpec{
					Template: runtime.RawExtension{
						Raw: mustMarshal(t, v1.PodTemplateSpec{
							Spec: v1.PodSpec{
								Containers: []v1.Container{{Name: "app", Image: "nginx"}},
								Volumes: []v1.Volume{
									{
										Name: "config",
										VolumeSource: v1.VolumeSource{
											ConfigMap: &v1.ConfigMapVolumeSource{
												LocalObjectReference: v1.LocalObjectReference{Name: "cm"},
											},
										},
									},
								},
							},
						}),
					},
				},
			},
			cluster: &clusterv1alpha1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster-1"},
			},
			expectedIsSuccess: true,
		},
		{
			name: "PVC without existing placement - initial scheduling allowed",
			app: &appsv1alpha1.Application{
				ObjectMeta: metav1.ObjectMeta{Name: "test-app"},
				Spec: appsv1alpha1.ApplicationSpec{
					Template: runtime.RawExtension{
						Raw: mustMarshal(t, v1.PodTemplateSpec{
							Spec: v1.PodSpec{
								Containers: []v1.Container{{Name: "app", Image: "nginx"}},
								Volumes: []v1.Volume{
									{
										Name: "data",
										VolumeSource: v1.VolumeSource{
											PersistentVolumeClaim: &v1.PersistentVolumeClaimVolumeSource{
												ClaimName: "data-pvc",
											},
										},
									},
								},
							},
						}),
					},
				},
			},
			cluster: &clusterv1alpha1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster-1"},
			},
			expectedIsSuccess: true,
		},
		{
			name: "PVC with existing placement - same cluster allowed",
			app: &appsv1alpha1.Application{
				ObjectMeta: metav1.ObjectMeta{Name: "test-app"},
				Spec: appsv1alpha1.ApplicationSpec{
					Template: runtime.RawExtension{
						Raw: mustMarshal(t, v1.PodTemplateSpec{
							Spec: v1.PodSpec{
								Containers: []v1.Container{{Name: "app", Image: "nginx"}},
								Volumes: []v1.Volume{
									{
										Name: "data",
										VolumeSource: v1.VolumeSource{
											PersistentVolumeClaim: &v1.PersistentVolumeClaimVolumeSource{
												ClaimName: "data-pvc",
											},
										},
									},
								},
							},
						}),
					},
				},
				Status: appsv1alpha1.ApplicationStatus{
					Placement: appsv1alpha1.PlacementStatus{
						Topology: []appsv1alpha1.ClusterTopology{
							{Name: "cluster-1", Replicas: 3},
						},
					},
				},
			},
			cluster: &clusterv1alpha1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster-1"},
			},
			expectedIsSuccess: true,
		},
		{
			name: "PVC with existing placement - different cluster denied",
			app: &appsv1alpha1.Application{
				ObjectMeta: metav1.ObjectMeta{Name: "test-app"},
				Spec: appsv1alpha1.ApplicationSpec{
					Template: runtime.RawExtension{
						Raw: mustMarshal(t, v1.PodTemplateSpec{
							Spec: v1.PodSpec{
								Containers: []v1.Container{{Name: "app", Image: "nginx"}},
								Volumes: []v1.Volume{
									{
										Name: "data",
										VolumeSource: v1.VolumeSource{
											PersistentVolumeClaim: &v1.PersistentVolumeClaimVolumeSource{
												ClaimName: "data-pvc",
											},
										},
									},
								},
							},
						}),
					},
				},
				Status: appsv1alpha1.ApplicationStatus{
					Placement: appsv1alpha1.PlacementStatus{
						Topology: []appsv1alpha1.ClusterTopology{
							{Name: "cluster-1", Replicas: 3},
						},
					},
				},
			},
			cluster: &clusterv1alpha1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster-2"},
			},
			expectedIsSuccess: false,
		},
		{
			name: "invalid JSON template - should pass (fail-open)",
			app: &appsv1alpha1.Application{
				ObjectMeta: metav1.ObjectMeta{Name: "test-app"},
				Spec: appsv1alpha1.ApplicationSpec{
					Template: runtime.RawExtension{
						Raw: []byte("invalid-json"),
					},
				},
			},
			cluster: &clusterv1alpha1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster-1"},
			},
			expectedIsSuccess: true,
		},
		{
			name: "multiple PVCs with existing placement - same cluster allowed",
			app: &appsv1alpha1.Application{
				ObjectMeta: metav1.ObjectMeta{Name: "test-app"},
				Spec: appsv1alpha1.ApplicationSpec{
					Template: runtime.RawExtension{
						Raw: mustMarshal(t, v1.PodTemplateSpec{
							Spec: v1.PodSpec{
								Containers: []v1.Container{{Name: "app", Image: "nginx"}},
								Volumes: []v1.Volume{
									{
										Name: "data1",
										VolumeSource: v1.VolumeSource{
											PersistentVolumeClaim: &v1.PersistentVolumeClaimVolumeSource{
												ClaimName: "data-pvc-1",
											},
										},
									},
									{
										Name: "data2",
										VolumeSource: v1.VolumeSource{
											PersistentVolumeClaim: &v1.PersistentVolumeClaimVolumeSource{
												ClaimName: "data-pvc-2",
											},
										},
									},
								},
							},
						}),
					},
				},
				Status: appsv1alpha1.ApplicationStatus{
					Placement: appsv1alpha1.PlacementStatus{
						Topology: []appsv1alpha1.ClusterTopology{
							{Name: "cluster-1", Replicas: 2},
							{Name: "cluster-2", Replicas: 2},
						},
					},
				},
			},
			cluster: &clusterv1alpha1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster-2"},
			},
			expectedIsSuccess: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vr := &VolumeRestriction{}
			state := framework.NewCycleState()

			status := vr.Filter(context.Background(), state, tt.app, tt.cluster)
			assert.Equal(t, tt.expectedIsSuccess, status.IsSuccess())
		})
	}
}

func mustMarshal(t *testing.T, obj interface{}) []byte {
	data, err := json.Marshal(obj)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}
	return data
}
