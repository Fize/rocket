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

package v1alpha1

import (
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// SchedulingPhase defines the scheduling lifecycle phase of the application
type SchedulingPhase string

const (
	Pending      SchedulingPhase = "Pending"
	Scheduling   SchedulingPhase = "Scheduling"
	Scheduled    SchedulingPhase = "Scheduled"
	Descheduling SchedulingPhase = "Descheduling"
	Failed       SchedulingPhase = "Failed"
)

// HealthPhase defines the health phase of the application
type HealthPhase string

const (
	Healthy     HealthPhase = "Healthy"
	Progressing HealthPhase = "Progressing"
	Degraded    HealthPhase = "Degraded"
	Unknown     HealthPhase = "Unknown"
)

// ClusterPhase defines the phase of the application in a specific cluster
type ClusterPhase string

const (
	ClusterPhaseHealthy     ClusterPhase = "Healthy"
	ClusterPhaseProgressing ClusterPhase = "Progressing"
	ClusterPhaseDegraded    ClusterPhase = "Degraded"
	ClusterPhaseUnknown     ClusterPhase = "Unknown"
)

// ClusterStatus describes the observed state of the application in a specific cluster
type ClusterStatus struct {
	// ClusterName is the name of the cluster
	ClusterName string `json:"clusterName"`
	// Phase is the current phase of the application in this cluster
	Phase ClusterPhase `json:"phase"`
	// Replicas is the desired number of replicas
	Replicas int32 `json:"replicas"`
	// ReadyReplicas is the number of ready replicas
	ReadyReplicas int32 `json:"readyReplicas"`
	// AvailableReplicas is the number of available replicas
	AvailableReplicas int32 `json:"availableReplicas"`
	// Message provides details about the current state
	// +optional
	Message string `json:"message,omitempty"`
	// Rollout contains the rollout status for this cluster
	// +optional
	Rollout *RolloutStatus `json:"rollout,omitempty"`
}

// JobAttributes defines configuration specific to Job and CronJob workloads
type JobAttributes struct {
	// Completions specifies the desired number of successfully finished pods
	// +optional
	Completions *int32 `json:"completions,omitempty"`

	// Parallelism specifies the maximum desired number of pods the job should run at any given time.
	// +optional
	Parallelism *int32 `json:"parallelism,omitempty"`

	// BackoffLimit specifies the number of retries before marking this job failed.
	// +optional
	BackoffLimit *int32 `json:"backoffLimit,omitempty"`

	// TTLSecondsAfterFinished limits the lifetime of a Job that has finished
	// +optional
	TTLSecondsAfterFinished *int32 `json:"ttlSecondsAfterFinished,omitempty"`

	// SuccessfulJobsHistoryLimit specifying how many completed jobs to keep.
	// +optional
	SuccessfulJobsHistoryLimit *int32 `json:"successfulJobsHistoryLimit,omitempty"`

	// FailedJobsHistoryLimit specifying how many failed jobs to keep.
	// +optional
	FailedJobsHistoryLimit *int32 `json:"failedJobsHistoryLimit,omitempty"`
}

// WorkloadGVK defines the GroupVersionKind of the workload
type WorkloadGVK struct {
	// APIVersion specifies the group and version of the workload to create
	APIVersion string `json:"apiVersion"`

	// Kind specifies the kind of the workload to create
	Kind string `json:"kind"`
}

// ApplicationSpec defines the desired state of Application
type ApplicationSpec struct {
	// Workload specifies the GVK of the workload to create
	Workload WorkloadGVK `json:"workload"`

	// Selector is a label query over pods that should match the replica count.
	// +optional
	Selector *metav1.LabelSelector `json:"selector,omitempty"`

	// Replicas is the desired number of replicas
	// +optional
	Replicas *int32 `json:"replicas,omitempty"`

	// Template describes the pods that will be created.
	// +optional
	// +kubebuilder:validation:XPreserveUnknownFields
	Template runtime.RawExtension `json:"template,omitempty"`

	// Schedule allows specifying a cron schedule. Only applicable for CronJob workload.
	// +optional
	Schedule string `json:"schedule,omitempty"`

	// Suspend allows suspending the execution of the workload.
	// +optional
	Suspend *bool `json:"suspend,omitempty"`

	// JobAttributes defines configuration specific to Job and CronJob workloads.
	// +optional
	JobAttributes *JobAttributes `json:"jobAttributes,omitempty"`

	// ClusterAffinity describes cluster affinity scheduling rules for the application.
	// +optional
	ClusterAffinity *v1.NodeAffinity `json:"clusterAffinity,omitempty"`

	// ClusterTolerations allows the application to schedule onto clusters with matching taints.
	// +optional
	ClusterTolerations []v1.Toleration `json:"clusterTolerations,omitempty"`

	// Overrides lists specific configurations for selected clusters.
	// These settings take precedence over the global configuration.
	// +optional
	Overrides []PolicyOverride `json:"overrides,omitempty"`

	// Resiliency defines the disruption budget and topology policy
	// +optional
	Resiliency *ResiliencyPolicy `json:"resiliency,omitempty"`

	// RolloutStrategy defines the rollout strategy for the application
	// When specified, kruise-rollout will be used to manage the deployment
	// +optional
	RolloutStrategy *RolloutStrategy `json:"rolloutStrategy,omitempty"`
}

// ResiliencyPolicy defines the disruption budget
type ResiliencyPolicy struct {
	// MinAvailable is the minimum number of available pods
	// +optional
	MinAvailable *intstr.IntOrString `json:"minAvailable,omitempty"`

	// MaxUnavailable is the maximum number of unavailable pods
	// +optional
	MaxUnavailable *intstr.IntOrString `json:"maxUnavailable,omitempty"`
}

// PolicyOverride defines specific configuration for clusters matching the selector
type PolicyOverride struct {
	// ClusterSelector selects the clusters where this override applies
	// +optional
	ClusterSelector *metav1.LabelSelector `json:"clusterSelector,omitempty"`

	// Image overrides the container image
	// +optional
	Image string `json:"image,omitempty"`

	// Env overrides or appends environment variables
	// +optional
	Env []v1.EnvVar `json:"env,omitempty"`

	// Resources overrides the compute resource requirements
	// +optional
	Resources *v1.ResourceRequirements `json:"resources,omitempty"`

	// Command overrides the entrypoint array
	// +optional
	Command []string `json:"command,omitempty"`

	// Args overrides the arguments to the entrypoint
	// +optional
	Args []string `json:"args,omitempty"`
}

// ClusterTopology describes the distribution of replicas across a cluster
type ClusterTopology struct {
	// Name is the name of the cluster
	Name string `json:"name"`
	// Replicas is the number of replicas scheduled to this cluster
	// +optional
	Replicas int32 `json:"replicas,omitempty"`
}

// PlacementStatus describes where the application is actually scheduled
type PlacementStatus struct {
	// Topology describes the scheduling result for each cluster
	// +optional
	Topology []ClusterTopology `json:"topology,omitempty"`
}

// ApplicationStatus defines the observed state of Application
type ApplicationStatus struct {
	// SchedulingPhase is the current scheduling lifecycle phase of the application
	SchedulingPhase SchedulingPhase `json:"schedulingPhase,omitempty"`

	// HealthPhase is the current health phase of the application
	HealthPhase HealthPhase `json:"healthPhase,omitempty"`

	// GlobalReadyReplicas is the total number of ready replicas across all clusters
	GlobalReadyReplicas int32 `json:"globalReadyReplicas,omitempty"`

	// GlobalReplicas is the total desired replicas
	GlobalReplicas int32 `json:"globalReplicas,omitempty"`

	// Placement indicates where the application has been scheduled
	// +optional
	Placement PlacementStatus `json:"placement,omitempty"`

	// AppliedClusters tracks the clusters where the workload has been applied.
	// This is used for cleanup when clusters are removed from placement.
	// +optional
	AppliedClusters []string `json:"appliedClusters,omitempty"`

	// ClustersStatus tracks the detailed status of the application in each cluster
	// +optional
	// +listType=map
	// +listMapKey=clusterName
	ClustersStatus []ClusterStatus `json:"clustersStatus,omitempty"`

	// ObservedGeneration represents the .metadata.generation that the condition was set based upon.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Conditions represent the latest available observations of the application's state
	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="SCHED",type="string",JSONPath=".status.schedulingPhase"
// +kubebuilder:printcolumn:name="HEALTH",type="string",JSONPath=".status.healthPhase"
// +kubebuilder:printcolumn:name="KIND",type="string",JSONPath=".spec.workload.kind"
// +kubebuilder:printcolumn:name="AGE",type="date",JSONPath=".metadata.creationTimestamp"

// Application is the Schema for the applications API
type Application struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ApplicationSpec   `json:"spec,omitempty"`
	Status ApplicationStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ApplicationList contains a list of Application
type ApplicationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Application `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Application{}, &ApplicationList{})
}
