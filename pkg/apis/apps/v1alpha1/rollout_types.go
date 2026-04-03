package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// RolloutType defines the type of rollout strategy
type RolloutType string

const (
	// RolloutTypeCanary represents a canary deployment strategy
	RolloutTypeCanary RolloutType = "Canary"
	// RolloutTypeBlueGreen represents a blue-green deployment strategy
	RolloutTypeBlueGreen RolloutType = "BlueGreen"
	// RolloutTypeABTest represents an A/B testing deployment strategy
	RolloutTypeABTest RolloutType = "ABTest"
)

// ClusterOrderType defines how clusters are ordered for rollout
type ClusterOrderType string

const (
	// ClusterOrderSequential means clusters are updated one after another
	ClusterOrderSequential ClusterOrderType = "Sequential"
	// ClusterOrderParallel means all clusters are updated simultaneously
	ClusterOrderParallel ClusterOrderType = "Parallel"
)

// RolloutStrategy defines the rollout strategy for an application
type RolloutStrategy struct {
	// Type specifies the rollout type (Canary, BlueGreen, ABTest)
	Type RolloutType `json:"type"`

	// Canary defines the canary deployment configuration
	// +optional
	Canary *CanaryStrategy `json:"canary,omitempty"`

	// BlueGreen defines the blue-green deployment configuration
	// +optional
	BlueGreen *BlueGreenStrategy `json:"blueGreen,omitempty"`

	// ABTest defines the A/B testing configuration
	// +optional
	ABTest *ABTestStrategy `json:"abTest,omitempty"`

	// ClusterOrder defines how clusters are ordered during rollout
	// +optional
	ClusterOrder *ClusterOrder `json:"clusterOrder,omitempty"`

	// Paused indicates if the rollout is paused
	// +optional
	Paused bool `json:"paused,omitempty"`

	// Rollback indicates if we should rollback to the previous version
	// +optional
	Rollback bool `json:"rollback,omitempty"`
}

// CanaryStrategy defines the configuration for canary deployment
type CanaryStrategy struct {
	// Steps define the sequence of steps for the canary deployment
	// Each step specifies the percentage of pods to update
	Steps []CanaryStep `json:"steps"`

	// TrafficRouting specifies how to route traffic to canary pods
	// +optional
	TrafficRouting *TrafficRouting `json:"trafficRouting,omitempty"`

	// GlobalReplicaDistribution controls how canary pods are distributed across clusters
	// When enabled, the platform calculates pod counts per cluster instead of applying
	// the same percentage to all clusters
	// +optional
	GlobalReplicaDistribution *GlobalReplicaDistribution `json:"globalReplicaDistribution,omitempty"`
}

// GlobalReplicaDistribution defines how canary pods are distributed globally across clusters
type GlobalReplicaDistribution struct {
	// Mode specifies the distribution mode
	// - Equal: Distribute pods equally across all clusters
	// - Weighted: Distribute pods based on cluster weights
	// - Sequential: Update one cluster at a time (requires ClusterOrder)
	// +kubebuilder:validation:Enum=Equal;Weighted;Sequential
	Mode DistributionMode `json:"mode"`

	// ClusterWeights specifies the weight for each cluster when mode is Weighted
	// The weight determines what percentage of canary pods go to each cluster
	// +optional
	ClusterWeights []ClusterReplicaWeight `json:"clusterWeights,omitempty"`

	// MaxUnavailable is the maximum number of pods that can be unavailable during the update
	// This is a global constraint across all clusters
	// +optional
	MaxUnavailable *int32 `json:"maxUnavailable,omitempty"`
}

// DistributionMode defines how pods are distributed across clusters
type DistributionMode string

const (
	// DistributionModeEqual distributes pods equally across all clusters
	DistributionModeEqual DistributionMode = "Equal"
	// DistributionModeWeighted distributes pods based on cluster weights
	DistributionModeWeighted DistributionMode = "Weighted"
	// DistributionModeSequential updates one cluster at a time
	DistributionModeSequential DistributionMode = "Sequential"
)

// ClusterReplicaWeight defines the weight for a cluster in replica distribution
type ClusterReplicaWeight struct {
	// ClusterName is the name of the cluster
	ClusterName string `json:"clusterName"`

	// Weight is the relative weight for this cluster (e.g., 30 means 30%)
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=100
	Weight int32 `json:"weight"`
}

// CanaryStep defines a single step in the canary deployment
type CanaryStep struct {
	// Weight is the percentage of traffic to route to canary pods
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=100
	Weight int32 `json:"weight"`

	// Pause specifies how long to pause at this step
	// +optional
	Pause *RolloutPause `json:"pause,omitempty"`
}

// BlueGreenStrategy defines the configuration for blue-green deployment
type BlueGreenStrategy struct {
	// ActiveService specifies the service name for the active (blue) environment
	ActiveService string `json:"activeService"`

	// PreviewService specifies the service name for the preview (green) environment
	// +optional
	PreviewService string `json:"previewService,omitempty"`

	// AutoPromotionEnabled automatically promotes the preview environment to active
	// +optional
	AutoPromotionEnabled bool `json:"autoPromotionEnabled,omitempty"`

	// ScaleDownDelaySeconds specifies the time to wait before scaling down the old environment
	// +optional
	ScaleDownDelaySeconds int32 `json:"scaleDownDelaySeconds,omitempty"`
}

// ABTestStrategy defines the configuration for A/B testing
type ABTestStrategy struct {
	// BaselineCluster specifies the cluster that serves the baseline version
	BaselineCluster string `json:"baselineCluster"`

	// CandidateClusters specifies the clusters that serve the candidate version
	CandidateClusters []string `json:"candidateClusters"`

	// TrafficSplit specifies the percentage of traffic to route to candidate clusters
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=100
	TrafficSplit int32 `json:"trafficSplit"`
}

// ClusterOrder defines the order of cluster updates during rollout
type ClusterOrder struct {
	// Type specifies the ordering type (Sequential or Parallel)
	Type ClusterOrderType `json:"type"`

	// Clusters specifies the explicit order of clusters for sequential rollout
	// Only used when Type is Sequential
	// +optional
	Clusters []string `json:"clusters,omitempty"`
}

// TrafficRouting defines how traffic is routed to different versions
type TrafficRouting struct {
	// Istio specifies Istio traffic routing configuration
	// +optional
	Istio *IstioTrafficRouting `json:"istio,omitempty"`

	// Nginx specifies NGINX Ingress traffic routing configuration
	// +optional
	Nginx *NginxTrafficRouting `json:"nginx,omitempty"`
}

// IstioTrafficRouting defines Istio traffic routing configuration
type IstioTrafficRouting struct {
	// VirtualService specifies the VirtualService name
	VirtualService string `json:"virtualService"`

	// DestinationRule specifies the DestinationRule name
	// +optional
	DestinationRule string `json:"destinationRule,omitempty"`
}

// NginxTrafficRouting defines NGINX Ingress traffic routing configuration
type NginxTrafficRouting struct {
	// AnnotationPrefix specifies the NGINX annotation prefix
	// +optional
	AnnotationPrefix string `json:"annotationPrefix,omitempty"`

	// Ingress specifies the Ingress resource name
	Ingress string `json:"ingress"`
}

// RolloutPause defines how to pause a rollout
type RolloutPause struct {
	// Duration specifies the duration of the pause in seconds
	// +optional
	Duration *int32 `json:"duration,omitempty"`
}

// RolloutStatusPhase defines the phase of a rollout
type RolloutStatusPhase string

const (
	// RolloutPhaseInitial indicates the rollout is in initial state
	RolloutPhaseInitial RolloutStatusPhase = "Initial"
	// RolloutPhaseProgressing indicates the rollout is in progress
	RolloutPhaseProgressing RolloutStatusPhase = "Progressing"
	// RolloutPhasePaused indicates the rollout is paused
	RolloutPhasePaused RolloutStatusPhase = "Paused"
	// RolloutPhaseSucceeded indicates the rollout has succeeded
	RolloutPhaseSucceeded RolloutStatusPhase = "Succeeded"
	// RolloutPhaseFailed indicates the rollout has failed
	RolloutPhaseFailed RolloutStatusPhase = "Failed"
)

// RolloutStatus describes the status of a rollout in a cluster
type RolloutStatus struct {
	// Phase is the current phase of the rollout
	Phase RolloutStatusPhase `json:"phase"`

	// CurrentStep is the current step index (0-indexed)
	// +optional
	CurrentStep int32 `json:"currentStep,omitempty"`

	// CurrentStepWeight is the weight percentage of the current step
	// +optional
	CurrentStepWeight int32 `json:"currentStepWeight,omitempty"`

	// StableReplicas is the number of stable replicas
	// +optional
	StableReplicas int32 `json:"stableReplicas,omitempty"`

	// CanaryReplicas is the number of canary replicas
	// +optional
	CanaryReplicas int32 `json:"canaryReplicas,omitempty"`

	// UpdatedReplicas is the number of updated replicas
	// +optional
	UpdatedReplicas int32 `json:"updatedReplicas,omitempty"`

	// ReadyReplicas is the number of ready replicas
	// +optional
	ReadyReplicas int32 `json:"readyReplicas,omitempty"`

	// Message provides details about the current state
	// +optional
	Message string `json:"message,omitempty"`

	// LastUpdateTime is the last time the status was updated
	// +optional
	LastUpdateTime metav1.Time `json:"lastUpdateTime,omitempty"`
}
