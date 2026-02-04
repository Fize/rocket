package overrides

import (
	"testing"

	appsv1alpha1 "github.com/hex-techs/rocket/pkg/apis/apps/v1alpha1"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestApplyOverrides(t *testing.T) {
	tests := []struct {
		name          string
		u             *unstructured.Unstructured
		app           *appsv1alpha1.Application
		clusterLabels map[string]string
		validate      func(*testing.T, *unstructured.Unstructured)
	}{
		{
			name: "override image",
			u: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"spec": map[string]interface{}{
						"template": map[string]interface{}{
							"spec": map[string]interface{}{
								"containers": []interface{}{
									map[string]interface{}{
										"name":  "main",
										"image": "nginx:latest",
									},
								},
							},
						},
					},
				},
			},
			app: &appsv1alpha1.Application{
				Spec: appsv1alpha1.ApplicationSpec{
					Overrides: []appsv1alpha1.PolicyOverride{
						{
							ClusterSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{"env": "prod"},
							},
							Image: "nginx:1.21",
						},
					},
				},
			},
			clusterLabels: map[string]string{"env": "prod"},
			validate: func(t *testing.T, u *unstructured.Unstructured) {
				containers, _, _ := unstructured.NestedSlice(u.Object, "spec", "template", "spec", "containers")
				container := containers[0].(map[string]interface{})
				assert.Equal(t, "nginx:1.21", container["image"])
			},
		},
		{
			name: "no match no override",
			u: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"spec": map[string]interface{}{
						"template": map[string]interface{}{
							"spec": map[string]interface{}{
								"containers": []interface{}{
									map[string]interface{}{
										"name":  "main",
										"image": "nginx:latest",
									},
								},
							},
						},
					},
				},
			},
			app: &appsv1alpha1.Application{
				Spec: appsv1alpha1.ApplicationSpec{
					Overrides: []appsv1alpha1.PolicyOverride{
						{
							ClusterSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{"env": "prod"},
							},
							Image: "nginx:1.21",
						},
					},
				},
			},
			clusterLabels: map[string]string{"env": "dev"},
			validate: func(t *testing.T, u *unstructured.Unstructured) {
				containers, _, _ := unstructured.NestedSlice(u.Object, "spec", "template", "spec", "containers")
				container := containers[0].(map[string]interface{})
				assert.Equal(t, "nginx:latest", container["image"])
			},
		},
		{
			name: "merge env",
			u: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"spec": map[string]interface{}{
						"template": map[string]interface{}{
							"spec": map[string]interface{}{
								"containers": []interface{}{
									map[string]interface{}{
										"name": "main",
										"env": []interface{}{
											map[string]interface{}{"name": "FOO", "value": "BAR"},
										},
									},
								},
							},
						},
					},
				},
			},
			app: &appsv1alpha1.Application{
				Spec: appsv1alpha1.ApplicationSpec{
					Overrides: []appsv1alpha1.PolicyOverride{
						{
							Env: []corev1.EnvVar{
								{Name: "FOO", Value: "NEW"},
								{Name: "BAZ", Value: "QUX"},
							},
						},
					},
				},
			},
			clusterLabels: map[string]string{},
			validate: func(t *testing.T, u *unstructured.Unstructured) {
				containers, _, _ := unstructured.NestedSlice(u.Object, "spec", "template", "spec", "containers")
				container := containers[0].(map[string]interface{})
				env, _, _ := unstructured.NestedSlice(container, "env")
				assert.Len(t, env, 2)

				envMap := make(map[string]string)
				for _, e := range env {
					m := e.(map[string]interface{})
					envMap[m["name"].(string)] = m["value"].(string)
				}
				assert.Equal(t, "NEW", envMap["FOO"])
				assert.Equal(t, "QUX", envMap["BAZ"])
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ApplyOverrides(tt.u, tt.app, tt.clusterLabels)
			assert.NoError(t, err)
			tt.validate(t, tt.u)
		})
	}
}

func TestApplyOverrides_Command(t *testing.T) {
	u := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"spec": map[string]interface{}{
				"template": map[string]interface{}{
					"spec": map[string]interface{}{
						"containers": []interface{}{
							map[string]interface{}{
								"name":    "main",
								"image":   "nginx",
								"command": []interface{}{"/bin/sh"},
							},
						},
					},
				},
			},
		},
	}

	app := &appsv1alpha1.Application{
		Spec: appsv1alpha1.ApplicationSpec{
			Overrides: []appsv1alpha1.PolicyOverride{
				{
					Command: []string{"/custom/entrypoint", "-c", "run"},
				},
			},
		},
	}

	err := ApplyOverrides(u, app, map[string]string{})
	assert.NoError(t, err)

	containers, _, _ := unstructured.NestedSlice(u.Object, "spec", "template", "spec", "containers")
	container := containers[0].(map[string]interface{})
	cmd := container["command"].([]interface{})
	assert.Len(t, cmd, 3)
	assert.Equal(t, "/custom/entrypoint", cmd[0])
	assert.Equal(t, "-c", cmd[1])
	assert.Equal(t, "run", cmd[2])
}

func TestApplyOverrides_Args(t *testing.T) {
	u := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"spec": map[string]interface{}{
				"template": map[string]interface{}{
					"spec": map[string]interface{}{
						"containers": []interface{}{
							map[string]interface{}{
								"name":  "main",
								"image": "nginx",
								"args":  []interface{}{"--old-arg"},
							},
						},
					},
				},
			},
		},
	}

	app := &appsv1alpha1.Application{
		Spec: appsv1alpha1.ApplicationSpec{
			Overrides: []appsv1alpha1.PolicyOverride{
				{
					Args: []string{"--new-arg1", "--new-arg2=value"},
				},
			},
		},
	}

	err := ApplyOverrides(u, app, map[string]string{})
	assert.NoError(t, err)

	containers, _, _ := unstructured.NestedSlice(u.Object, "spec", "template", "spec", "containers")
	container := containers[0].(map[string]interface{})
	args := container["args"].([]interface{})
	assert.Len(t, args, 2)
	assert.Equal(t, "--new-arg1", args[0])
	assert.Equal(t, "--new-arg2=value", args[1])
}

func TestApplyOverrides_Resources(t *testing.T) {
	u := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"spec": map[string]interface{}{
				"template": map[string]interface{}{
					"spec": map[string]interface{}{
						"containers": []interface{}{
							map[string]interface{}{
								"name":  "main",
								"image": "nginx",
							},
						},
					},
				},
			},
		},
	}

	app := &appsv1alpha1.Application{
		Spec: appsv1alpha1.ApplicationSpec{
			Overrides: []appsv1alpha1.PolicyOverride{
				{
					Resources: &corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							corev1.ResourceCPU:    mustParseQuantity("2"),
							corev1.ResourceMemory: mustParseQuantity("4Gi"),
						},
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    mustParseQuantity("1"),
							corev1.ResourceMemory: mustParseQuantity("2Gi"),
						},
					},
				},
			},
		},
	}

	err := ApplyOverrides(u, app, map[string]string{})
	assert.NoError(t, err)

	containers, _, _ := unstructured.NestedSlice(u.Object, "spec", "template", "spec", "containers")
	container := containers[0].(map[string]interface{})
	resources, ok := container["resources"].(map[string]interface{})
	assert.True(t, ok)
	assert.NotNil(t, resources["limits"])
	assert.NotNil(t, resources["requests"])
}

func TestApplyOverrides_NoContainers(t *testing.T) {
	u := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"spec": map[string]interface{}{
				"template": map[string]interface{}{
					"spec": map[string]interface{}{
						// No containers
					},
				},
			},
		},
	}

	app := &appsv1alpha1.Application{
		Spec: appsv1alpha1.ApplicationSpec{
			Overrides: []appsv1alpha1.PolicyOverride{
				{
					Image: "nginx:1.21",
				},
			},
		},
	}

	err := ApplyOverrides(u, app, map[string]string{})
	assert.NoError(t, err)
}

func TestApplyOverrides_EmptyContainers(t *testing.T) {
	u := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"spec": map[string]interface{}{
				"template": map[string]interface{}{
					"spec": map[string]interface{}{
						"containers": []interface{}{},
					},
				},
			},
		},
	}

	app := &appsv1alpha1.Application{
		Spec: appsv1alpha1.ApplicationSpec{
			Overrides: []appsv1alpha1.PolicyOverride{
				{
					Image: "nginx:1.21",
				},
			},
		},
	}

	err := ApplyOverrides(u, app, map[string]string{})
	assert.NoError(t, err)
}

func TestApplyOverrides_MultipleOverrides(t *testing.T) {
	u := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"spec": map[string]interface{}{
				"template": map[string]interface{}{
					"spec": map[string]interface{}{
						"containers": []interface{}{
							map[string]interface{}{
								"name":  "main",
								"image": "nginx:latest",
							},
						},
					},
				},
			},
		},
	}

	app := &appsv1alpha1.Application{
		Spec: appsv1alpha1.ApplicationSpec{
			Overrides: []appsv1alpha1.PolicyOverride{
				{
					ClusterSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"region": "us-west"},
					},
					Image:   "nginx:us-west",
					Command: []string{"/bin/west-start"},
				},
				{
					ClusterSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"region": "us-east"},
					},
					Image: "nginx:us-east",
				},
			},
		},
	}

	// Test us-west cluster
	err := ApplyOverrides(u, app, map[string]string{"region": "us-west"})
	assert.NoError(t, err)

	containers, _, _ := unstructured.NestedSlice(u.Object, "spec", "template", "spec", "containers")
	container := containers[0].(map[string]interface{})
	assert.Equal(t, "nginx:us-west", container["image"])
	cmd := container["command"].([]interface{})
	assert.Equal(t, "/bin/west-start", cmd[0])
}

func mustParseQuantity(s string) resource.Quantity {
	q, _ := resource.ParseQuantity(s)
	return q
}
