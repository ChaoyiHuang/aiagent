package v1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// AgentRuntime represents a runtime carrier that hosts AI Agents.
// It corresponds to a Pod instance and manages the lifecycle of
// both AgentRuntime and AIAgent CRDs.
//
// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=.status.phase
// +kubebuilder:printcolumn:name="Agents",type=integer,JSONPath=.status.agentCount
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=.metadata.creationTimestamp
type AgentRuntime struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AgentRuntimeSpec   `json:"spec,omitempty"`
	Status AgentRuntimeStatus `json:"status,omitempty"`
}

// AgentRuntimeSpec defines the desired state of AgentRuntime.
type AgentRuntimeSpec struct {
	// AgentHandler specifies the handler container configuration.
	AgentHandler AgentHandlerSpec `json:"agentHandler"`

	// AgentFramework specifies the framework container configuration.
	AgentFramework AgentFrameworkSpec `json:"agentFramework"`

	// Harness references harness capabilities for this runtime.
	Harness []HarnessReference `json:"harness,omitempty"`

	// AgentConfig references runtime-level configuration files.
	// Mounted at /etc/agent-config/runtime/.
	AgentConfig []AgentConfigReference `json:"agentConfig,omitempty"`

	// SandboxTemplateRef references a SandboxTemplate for embedded mode.
	SandboxTemplateRef string `json:"sandboxTemplateRef,omitempty"`

	// Replicas is the number of Pod instances to create.
	// +kubebuilder:default=1
	// +kubebuilder:validation:Minimum=1
	Replicas int32 `json:"replicas"`

	// NodeSelector constrains which nodes this runtime can run on.
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`

	// Affinity rules for scheduling.
	Affinity *corev1.Affinity `json:"affinity,omitempty"`

	// Tolerations for node taints.
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`

	// ServiceAccountName is the name of the ServiceAccount to use for the Pod.
	ServiceAccountName string `json:"serviceAccountName,omitempty"`

	// Resources defines resource requirements for the Pod.
	Resources *RuntimeResources `json:"resources,omitempty"`

	// ProcessMode defines how agents are organized within the runtime.
	// +kubebuilder:validation:Enum=shared;isolated
	// +kubebuilder:default=isolated
	ProcessMode ProcessModeType `json:"processMode,omitempty"`
}

// AgentHandlerSpec defines the Agent Handler container.
type AgentHandlerSpec struct {
	// Image is the container image for the Agent Handler.
	Image string `json:"image"`

	// Version is the image version/tag.
	Version string `json:"version,omitempty"`

	// Command overrides the container entrypoint.
	Command []string `json:"command,omitempty"`

	// Args are arguments to the entrypoint.
	Args []string `json:"args,omitempty"`

	// Env environment variables.
	Env []EnvVar `json:"env,omitempty"`
}

// AgentFrameworkSpec defines the Agent Framework container.
type AgentFrameworkSpec struct {
	// Image is the container image for the Agent Framework.
	Image string `json:"image"`

	// Version is the image version/tag.
	Version string `json:"version,omitempty"`

	// Type identifies the framework type (e.g., adk, openclaw, langchain, or any custom framework).
	// No enum restriction - supports any framework type.
	Type string `json:"type"`

	// Command overrides the container entrypoint.
	Command []string `json:"command,omitempty"`

	// Args are arguments to the entrypoint.
	Args []string `json:"args,omitempty"`

	// Env environment variables.
	Env []EnvVar `json:"env,omitempty"`
}

// EnvVar represents an environment variable.
type EnvVar struct {
	// Name of the environment variable.
	Name string `json:"name"`

	// Value of the environment variable.
	Value string `json:"value,omitempty"`

	// ValueFrom references a field or resource for the value.
	ValueFrom *EnvVarSource `json:"valueFrom,omitempty"`
}

// EnvVarSource represents a source for an environment variable value.
type EnvVarSource struct {
	// ConfigMapKeyRef selects a key from a ConfigMap.
	ConfigMapKeyRef *ConfigMapKeySelector `json:"configMapKeyRef,omitempty"`

	// SecretKeyRef selects a key from a Secret.
	SecretKeyRef *SecretKeySelector `json:"secretKeyRef,omitempty"`

	// FieldRef selects a field from the Pod.
	FieldRef *FieldSelector `json:"fieldRef,omitempty"`
}

// ConfigMapKeySelector selects a key from a ConfigMap.
type ConfigMapKeySelector struct {
	// Name of the ConfigMap.
	Name string `json:"name"`

	// Key to select.
	Key string `json:"key"`
}

// SecretKeySelector selects a key from a Secret.
type SecretKeySelector struct {
	// Name of the Secret.
	Name string `json:"name"`

	// Key to select.
	Key string `json:"key"`
}

// FieldSelector selects a field from the Pod.
type FieldSelector struct {
	// FieldPath is the path to the field.
	FieldPath string `json:"fieldPath"`
}

// HarnessReference references a Harness CRD.
type HarnessReference struct {
	// Name of the Harness CRD.
	Name string `json:"name"`

	// Namespace of the Harness CRD. Defaults to same namespace.
	Namespace string `json:"namespace,omitempty"`
}

// RuntimeResources defines Pod-level resource requirements.
type RuntimeResources struct {
	// Limits are resource limits for the entire Pod.
	Limits map[string]Quantity `json:"limits,omitempty"`

	// Requests are resource requests for the entire Pod.
	Requests map[string]Quantity `json:"requests,omitempty"`
}

// Quantity represents a Kubernetes resource quantity.
type Quantity string

// ProcessModeType defines how agents are organized in a runtime.
type ProcessModeType string

const (
	// ProcessModeShared runs all agents in a single framework process.
	// Framework handles agent scheduling internally.
	// Suitable for: ADK (single process multi-agent)
	ProcessModeShared ProcessModeType = "shared"

	// ProcessModeIsolated runs each agent in its own framework process.
	// Handler manages multiple framework process instances.
	// Suitable for: ADK (isolated mode)
	ProcessModeIsolated ProcessModeType = "isolated"
)

// AgentRuntimeStatus defines the observed state of AgentRuntime.
type AgentRuntimeStatus struct {
	// Phase indicates the current lifecycle phase.
	// +kubebuilder:validation:Enum=Pending;Creating;Running;Updating;Terminating;Failed
	Phase RuntimePhase `json:"phase"`

	// PodIPs are the IP addresses of the running Pods.
	PodIPs []string `json:"podIPs,omitempty"`

	// ReadyReplicas is the number of ready Pod instances.
	ReadyReplicas int32 `json:"readyReplicas"`

	// AgentCount is the number of agents bound to this runtime.
	AgentCount int32 `json:"agentCount"`

	// Agents shows bindings for agents scheduled to this runtime.
	Agents []AgentBindingStatus `json:"agents,omitempty"`

	// Conditions represent the latest available observations.
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// FrameworkID is the internal identifier for the running framework.
	FrameworkID string `json:"frameworkID,omitempty"`
}

// RuntimePhase defines the lifecycle phase of an AgentRuntime.
type RuntimePhase string

const (
	RuntimePhasePending    RuntimePhase = "Pending"
	RuntimePhaseCreating   RuntimePhase = "Creating"
	RuntimePhaseRunning    RuntimePhase = "Running"
	RuntimePhaseUpdating   RuntimePhase = "Updating"
	RuntimePhaseTerminating RuntimePhase = "Terminating"
	RuntimePhaseFailed     RuntimePhase = "Failed"
)

// AgentBindingStatus shows an agent's binding to this runtime.
type AgentBindingStatus struct {
	// Name of the AIAgent.
	Name string `json:"name"`

	// UID of the AIAgent for verification.
	UID string `json:"uid,omitempty"`

	// Namespace of the AIAgent.
	Namespace string `json:"namespace"`

	// Phase of the bound agent.
	Phase AgentPhase `json:"phase"`

	// BoundAt is when the agent was bound to this runtime.
	BoundAt metav1.Time `json:"boundAt,omitempty"`
}

// AgentRuntimeList contains a list of AgentRuntime.
// +kubebuilder:object:root=true
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type AgentRuntimeList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []AgentRuntime `json:"items"`
}