/*
Copyright 2026.

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

package v1alpha1

import (
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// WorkspaceSpec defines the workspace behavior
type WorkspaceSpec struct {
	// Name defines the name of the namespace to be created in the member clusters.
	// If not specified, it defaults to the name of the Workspace resource.
	// +optional
	Name string `json:"name,omitempty"`

	// ClusterSelector selects the clusters where this workspace should be provisioned.
	// +optional
	ClusterSelector *metav1.LabelSelector `json:"clusterSelector,omitempty"`

	// ResourceConstraints defines the quota and limits for this workspace
	// +optional
	ResourceConstraints *WorkspaceConstraints `json:"resourceConstraints,omitempty"`
}

// WorkspaceConstraints defines the logical resource constraints for the workspace
type WorkspaceConstraints struct {
	// Quota defines the ResourceQuota to be applied to the namespace
	// +optional
	Quota *v1.ResourceQuotaSpec `json:"quota,omitempty"`

	// LimitRange defines the LimitRange to be applied to the namespace
	// +optional
	LimitRange *v1.LimitRangeSpec `json:"limitRange,omitempty"`
}

// WorkspaceStatus defines the observed state of Workspace
type WorkspaceStatus struct {
	// Conditions represent the latest available observations of the workspace's state
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// AppliedClusters tracks the clusters where the workspace has been successfully provisioned
	// +optional
	AppliedClusters []string `json:"appliedClusters,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="NAMESPACE",type="string",JSONPath=".spec.name"
// +kubebuilder:printcolumn:name="AGE",type="date",JSONPath=".metadata.creationTimestamp"

// Workspace is the Schema for the workspaces API
type Workspace struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   WorkspaceSpec   `json:"spec,omitempty"`
	Status WorkspaceStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// WorkspaceList contains a list of Workspace
type WorkspaceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Workspace `json:"items"`
}
