package v1alpha1

import (
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ClusterState defines the state of the cluster
type ClusterState string

const (
	// ClusterPending means the cluster is waiting for approval
	ClusterPending ClusterState = "Pending"
	// ClusterReady means the cluster has been approved and is connected/ready
	ClusterReady ClusterState = "Ready"
	// ClusterOffline means the cluster is disconnected
	ClusterOffline ClusterState = "Offline"
	// ClusterRejected means the cluster registration was rejected
	ClusterRejected ClusterState = "Rejected"
)

// ClusterConnectionMode defines the connection mode of the cluster
type ClusterConnectionMode string

const (
	// ClusterConnectionModeHub means the manager actively connects to the cluster
	ClusterConnectionModeHub ClusterConnectionMode = "Hub"
	// ClusterConnectionModeEdge means the agent connects to the manager
	ClusterConnectionModeEdge ClusterConnectionMode = "Edge"
)

// ClusterSpec defines the desired state of Cluster
type ClusterSpec struct {
	// ConnectionMode specifies how the cluster connects to the manager
	// +optional
	ConnectionMode ClusterConnectionMode `json:"connectionMode,omitempty"`

	// APIServer is the API server URL of the cluster
	// This field is optional and used for direct connectivity mode
	// +optional
	APIServer string `json:"apiServer,omitempty"`

	// SecretRef is a reference to the secret containing cluster credentials
	// The secret should contain: caData, certData, keyData, and token
	// +optional
	SecretRef *v1.LocalObjectReference `json:"secretRef,omitempty"`

	// Taints are the taints applied to the cluster for scheduling
	// +optional
	Taints []v1.Taint `json:"taints,omitempty"`

	// Addons specifies the list of addons to enable for this cluster
	// +optional
	Addons []ClusterAddon `json:"addons,omitempty"`
}

// ClusterAddon defines the desired state of an addon
type ClusterAddon struct {
	// Name is the name of the addon
	Name string `json:"name"`
	// Enabled specifies whether the addon is enabled
	// +optional
	Enabled bool `json:"enabled,omitempty"`
	// Config is the configuration for the addon
	// +optional
	Config map[string]string `json:"config,omitempty"`
}

// ClusterStatus defines the observed state of Cluster
type ClusterStatus struct {
	// State represents the current state of the cluster
	// +optional
	State ClusterState `json:"state,omitempty"`

	// ID is the cluster ID (may be set by the cloud provider or agent)
	// +optional
	ID string `json:"id,omitempty"`

	// KubernetesVersion is the version of the kubernetes cluster
	// +optional
	KubernetesVersion string `json:"kubernetesVersion,omitempty"`

	// AgentVersion is the version of the rocket agent
	// +optional
	AgentVersion string `json:"agentVersion,omitempty"`

	// APIServerURL is the url of the child cluster apiserver
	// +optional
	APIServerURL string `json:"apiServerURL,omitempty"`

	// LastKeepAliveTime is the last time the agent sent a heartbeat
	// +optional
	LastKeepAliveTime *metav1.Time `json:"lastKeepAliveTime,omitempty"`

	// NodeSummary represents the summary of nodes status in the member cluster
	// +optional
	NodeSummary []NodeSummary `json:"nodeSummary,omitempty"`

	// ResourceSummary represents the summary of resources in the member cluster
	// +optional
	ResourceSummary []ResourceSummary `json:"resourceSummary,omitempty"`

	// Conditions represents the latest available observations of the cluster's state
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// AddonStatus represents the status of addons
	// +optional
	AddonStatus []AddonStatus `json:"addonStatus,omitempty"`
}

// AddonStatus defines the observed state of an addon
type AddonStatus struct {
	// Name is the name of the addon
	Name string `json:"name"`
	// State is the state of the addon
	State string `json:"state"`
	// Message is the detailed message
	// +optional
	Message string `json:"message,omitempty"`
}

// NodeSummary represents the summary of nodes status in a specific cluster
type NodeSummary struct {
	// Name is the name of the resource pool
	Name string `json:"name,omitempty"`
	// TotalNum is the total number of nodes in the cluster
	// +optional
	TotalNum int `json:"totalNum,omitempty"`
	// ReadyNum is the number of ready nodes in the cluster
	// +optional
	ReadyNum int `json:"readyNum,omitempty"`
}

// ResourceSummary represents the summary of resources in the member cluster
type ResourceSummary struct {
	// Name is the name of the resource pool
	Name string `json:"name,omitempty"`
	// Allocatable represents the resources of a cluster that are available for scheduling
	// Total amount of allocatable resources on all nodes
	// +optional
	Allocatable v1.ResourceList `json:"allocatable,omitempty"`
	// Allocating represents the resources of a cluster that are pending for scheduling
	// Total amount of required resources of all Pods that are waiting for scheduling
	// +optional
	Allocating v1.ResourceList `json:"allocating,omitempty"`
	// Allocated represents the resources of a cluster that have been scheduled
	// Total amount of required resources of all Pods that have been scheduled to nodes
	// +optional
	Allocated v1.ResourceList `json:"allocated,omitempty"`
}

// +genclient
// +genclient:nonNamespaced
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true

// Cluster is the Schema for the virtual clusters API
type Cluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ClusterSpec   `json:"spec,omitempty"`
	Status ClusterStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true

// ClusterList contains a list of Cluster
type ClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Cluster `json:"items"`
}
