/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package overrides

import (
	appsv1alpha1 "github.com/hex-techs/rocket/pkg/apis/apps/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
)

// ApplyOverrides applies the overrides defined in the Application spec to the Unstructured workload
func ApplyOverrides(u *unstructured.Unstructured, app *appsv1alpha1.Application, clusterLabels map[string]string) error {
	for _, override := range app.Spec.Overrides {
		matches, err := matchesCluster(override.ClusterSelector, clusterLabels)
		if err != nil {
			return err
		}
		if matches {
			if err := applyOverride(u, override); err != nil {
				return err
			}
		}
	}
	return nil
}

func matchesCluster(selector *metav1.LabelSelector, clusterLabels map[string]string) (bool, error) {
	if selector == nil {
		return true, nil
	}
	s, err := metav1.LabelSelectorAsSelector(selector)
	if err != nil {
		return false, err
	}
	return s.Matches(labels.Set(clusterLabels)), nil
}

func applyOverride(u *unstructured.Unstructured, override appsv1alpha1.PolicyOverride) error {
	// We assume the workload has a standard PodTemplateSpec at spec.template
	containers, found, err := unstructured.NestedSlice(u.Object, "spec", "template", "spec", "containers")
	if err != nil || !found {
		return nil
	}

	if len(containers) > 0 {
		// Apply to the first container
		container := containers[0].(map[string]interface{})

		if override.Image != "" {
			container["image"] = override.Image
		}

		if len(override.Command) > 0 {
			// Convert []string to []interface{}
			cmdSlice := make([]interface{}, len(override.Command))
			for i, v := range override.Command {
				cmdSlice[i] = v
			}
			container["command"] = cmdSlice
		}

		if len(override.Args) > 0 {
			// Convert []string to []interface{}
			argsSlice := make([]interface{}, len(override.Args))
			for i, v := range override.Args {
				argsSlice[i] = v
			}
			container["args"] = argsSlice
		}

		if len(override.Env) > 0 {
			currentEnv, _, _ := unstructured.NestedSlice(container, "env")
			newEnv, err := mergeEnv(currentEnv, override.Env)
			if err != nil {
				return err
			}
			container["env"] = newEnv
		}

		if override.Resources != nil {
			resMap, err := runtime.DefaultUnstructuredConverter.ToUnstructured(override.Resources)
			if err != nil {
				return err
			}
			container["resources"] = resMap
		}

		containers[0] = container
		if err := unstructured.SetNestedSlice(u.Object, containers, "spec", "template", "spec", "containers"); err != nil {
			return err
		}
	}
	return nil
}

func mergeEnv(current []interface{}, overrides []corev1.EnvVar) ([]interface{}, error) {
	envMap := make(map[string]interface{})
	var result []interface{}

	// Index current env vars
	for _, e := range current {
		if em, ok := e.(map[string]interface{}); ok {
			if name, ok := em["name"].(string); ok {
				envMap[name] = em
				result = append(result, em)
			}
		}
	}

	// Apply overrides
	for _, override := range overrides {
		overrideMap, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&override)
		if err != nil {
			return nil, err
		}

		name := override.Name
		if _, exists := envMap[name]; exists {
			// Replace
			for i, e := range result {
				if em, ok := e.(map[string]interface{}); ok {
					if em["name"] == name {
						result[i] = overrideMap
						break
					}
				}
			}
		} else {
			// Append
			result = append(result, overrideMap)
			envMap[name] = overrideMap
		}
	}

	return result, nil
}
