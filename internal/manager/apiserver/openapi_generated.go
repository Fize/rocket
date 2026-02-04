package apiserver

import (
	"k8s.io/kube-openapi/pkg/common"
	spec "k8s.io/kube-openapi/pkg/validation/spec"
)

// GetOpenAPIDefinitions returns OpenAPI definitions for the aggregated APIServer.
// We provide minimal stub definitions to satisfy k8s.io/apiserver's requirements
// without having to run full code generation.
func GetOpenAPIDefinitions(ref common.ReferenceCallback) map[string]common.OpenAPIDefinition {
	return map[string]common.OpenAPIDefinition{
		// Our custom types from cluster.rocket.io
		"github.com/hex-techs/rocket/pkg/apis/cluster/v1alpha1.Cluster":       stubClusterDef(ref),
		"github.com/hex-techs/rocket/pkg/apis/cluster/v1alpha1.ClusterList":   stubClusterListDef(ref),
		"github.com/hex-techs/rocket/pkg/apis/cluster/v1alpha1.ClusterSpec":   stubObjectDef(),
		"github.com/hex-techs/rocket/pkg/apis/cluster/v1alpha1.ClusterStatus": stubObjectDef(),

		// Core Kubernetes types that might be referenced
		"k8s.io/apimachinery/pkg/apis/meta/v1.ObjectMeta": stubObjectDef(),
		"k8s.io/apimachinery/pkg/apis/meta/v1.TypeMeta":   stubObjectDef(),
		"k8s.io/apimachinery/pkg/apis/meta/v1.ListMeta":   stubObjectDef(),
		"k8s.io/apimachinery/pkg/apis/meta/v1.Time":       stubObjectDef(),
	}
}

func stubObjectDef() common.OpenAPIDefinition {
	return common.OpenAPIDefinition{
		Schema: spec.Schema{
			SchemaProps: spec.SchemaProps{
				Type: []string{"object"},
			},
		},
	}
}

func stubClusterDef(ref common.ReferenceCallback) common.OpenAPIDefinition {
	return common.OpenAPIDefinition{
		Schema: spec.Schema{
			SchemaProps: spec.SchemaProps{
				Type: []string{"object"},
				Properties: map[string]spec.Schema{
					"apiVersion": {SchemaProps: spec.SchemaProps{Type: []string{"string"}}},
					"kind":       {SchemaProps: spec.SchemaProps{Type: []string{"string"}}},
					"metadata":   {SchemaProps: spec.SchemaProps{Type: []string{"object"}}},
					"spec":       {SchemaProps: spec.SchemaProps{Type: []string{"object"}}},
					"status":     {SchemaProps: spec.SchemaProps{Type: []string{"object"}}},
				},
			},
		},
		Dependencies: []string{},
	}
}

func stubClusterListDef(ref common.ReferenceCallback) common.OpenAPIDefinition {
	return common.OpenAPIDefinition{
		Schema: spec.Schema{
			SchemaProps: spec.SchemaProps{
				Type: []string{"object"},
				Properties: map[string]spec.Schema{
					"apiVersion": {SchemaProps: spec.SchemaProps{Type: []string{"string"}}},
					"kind":       {SchemaProps: spec.SchemaProps{Type: []string{"string"}}},
					"metadata":   {SchemaProps: spec.SchemaProps{Type: []string{"object"}}},
					"items": {
						SchemaProps: spec.SchemaProps{
							Type: []string{"array"},
							Items: &spec.SchemaOrArray{
								Schema: &spec.Schema{SchemaProps: spec.SchemaProps{Type: []string{"object"}}},
							},
						},
					},
				},
			},
		},
		Dependencies: []string{},
	}
}
