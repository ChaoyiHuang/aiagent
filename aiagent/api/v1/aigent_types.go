// Package v1 contains API Schema definitions for the AI Agent abstraction.
// +k8s:deepcopy-gen=package
// +groupName=agent.ai
package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
)

// AIAgent represents an AI Agent business object that can be scheduled
// to different AgentRuntimes for execution.
//
// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
type AIAgent struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AIAgentSpec   `json:"spec,omitempty"`
	Status AIAgentStatus `json:"status,omitempty"`
}

// AIAgentSpec defines the desired state of AIAgent.
type AIAgentSpec struct {
	// RuntimeRef specifies how to schedule this agent.
	// Can be type-based (automatic scheduling) or name-based (fixed binding).
	RuntimeRef RuntimeReference `json:"runtimeRef"`

	// HarnessOverride allows customizing harness capabilities
	// inherited from the AgentRuntime.
	HarnessOverride HarnessOverrideSpec `json:"harnessOverride,omitempty"`

	// AgentConfig contains framework-specific configuration in JSON format.
	// This field is directly interpreted by the Handler (ADK, OpenClaw, etc).
	// The Handler determines the schema and meaning of this configuration.
	// Example for ADK: {"instruction": "You are a helpful assistant", "tools": ["search"]}
	// Example for OpenClaw: {"gateway": {"port": 18789}, "agents": ["weather", "calculator"]}
	// +kubebuilder:pruning:PreserveUnknownFields
	AgentConfig *apiextensionsv1.JSON `json:"agentConfig,omitempty"`

	// AgentConfigReferences references agent-specific configuration files.
	// Mounted at /etc/agent-config/agent/.
	// Deprecated: Use AgentConfig (JSON field above) instead for framework-specific configuration.
	AgentConfigReferences []AgentConfigReference `json:"agentConfigReferences,omitempty"`

	// VolumePolicy defines PVC lifecycle policy.
	// retain: PVC is kept after agent deletion.
	// delete: PVC is deleted when agent is deleted.
	VolumePolicy VolumePolicyType `json:"volumePolicy,omitempty"`

	// Description is a human-readable description of the agent.
	Description string `json:"description,omitempty"`
}

// RuntimeReference specifies how to schedule an AIAgent to an AgentRuntime.
type RuntimeReference struct {
	// Type specifies the agent framework type for automatic scheduling.
	// The controller will find a matching AgentRuntime.
	// No enum restriction - supports any framework type (e.g., adk, openclaw, langchain, or custom).
	Type string `json:"type,omitempty"`

	// Name specifies a specific AgentRuntime instance for fixed binding.
	// If set, the agent is directly bound to this instance.
	Name string `json:"name,omitempty"`
}

// HarnessOverrideSpec allows customizing inherited harness capabilities.
// Cannot append new harnesses, only override or deny existing ones.
type HarnessOverrideSpec struct {
	// MCP overrides for MCP registry access.
	MCP []MCPOverrideSpec `json:"mcp,omitempty"`

	// Memory overrides for memory storage access.
	Memory []MemoryOverrideSpec `json:"memory,omitempty"`

	// Sandbox overrides for sandbox access.
	Sandbox []SandboxOverrideSpec `json:"sandbox,omitempty"`

	// Skills overrides for skill access.
	Skills []SkillsOverrideSpec `json:"skills,omitempty"`

	// Model overrides for model access.
	Model []ModelOverrideSpec `json:"model,omitempty"`
}

// MCPOverrideSpec customizes MCP registry access.
type MCPOverrideSpec struct {
	// Name of the MCP registry to override.
	Name string `json:"name"`

	// AllowedServers restricts which MCP servers can be used.
	AllowedServers []string `json:"allowedServers,omitempty"`

	// DeniedServers blocks specific MCP servers.
	DeniedServers []string `json:"deniedServers,omitempty"`

	// Deny completely disables access to this registry.
	Deny bool `json:"deny,omitempty"`
}

// MemoryOverrideSpec customizes memory storage access.
type MemoryOverrideSpec struct {
	// Name of the memory harness to override.
	Name string `json:"name"`

	// Config overrides memory configuration.
	// +kubebuilder:pruning:PreserveUnknownFields
	Config *apiextensionsv1.JSON `json:"config,omitempty"`
}

// SandboxOverrideSpec customizes sandbox access.
type SandboxOverrideSpec struct {
	// Name of the sandbox harness to override.
	Name string `json:"name"`

	// Deny disables access to this sandbox.
	Deny bool `json:"deny,omitempty"`
}

// SkillsOverrideSpec customizes skill access.
type SkillsOverrideSpec struct {
	// Name of the skills harness to override.
	Name string `json:"name"`

	// AllowedSkills restricts which skills can be used.
	AllowedSkills []string `json:"allowedSkills,omitempty"`

	// DeniedSkills blocks specific skills.
	DeniedSkills []string `json:"deniedSkills,omitempty"`
}

// ModelOverrideSpec customizes model access.
type ModelOverrideSpec struct {
	// Name of the model harness to override.
	Name string `json:"name"`

	// AllowedModels restricts which models can be used.
	AllowedModels []string `json:"allowedModels,omitempty"`

	// DeniedModels blocks specific models.
	DeniedModels []string `json:"deniedModels,omitempty"`
}

// AgentConfigReference references a ConfigMap or Secret for agent configuration.
type AgentConfigReference struct {
	// Name of the configuration reference.
	Name string `json:"name"`

	// ConfigMapRef references a ConfigMap.
	ConfigMapRef *ConfigMapReference `json:"configMapRef,omitempty"`

	// SecretRef references a Secret.
	SecretRef *SecretReference `json:"secretRef,omitempty"`
}

// ConfigMapReference references a ConfigMap.
type ConfigMapReference struct {
	// Name of the ConfigMap.
	Name string `json:"name"`
}

// SecretReference references a Secret.
type SecretReference struct {
	// Name of the Secret.
	Name string `json:"name"`
}

// VolumePolicyType defines PVC lifecycle policy.
// +kubebuilder:validation:Enum=retain;delete
type VolumePolicyType string

const (
	VolumePolicyRetain VolumePolicyType = "retain"
	VolumePolicyDelete VolumePolicyType = "delete"
)

// AIAgentStatus defines the observed state of AIAgent.
type AIAgentStatus struct {
	// Phase indicates the current lifecycle phase.
	// +kubebuilder:validation:Enum=Pending;Scheduling;Running;Migrating;Failed;Terminated
	Phase AgentPhase `json:"phase"`

	// RuntimeRefStatus shows the currently bound AgentRuntime.
	RuntimeRef RuntimeReferenceStatus `json:"runtimeRef,omitempty"`

	// Conditions represent the latest available observations.
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// AgentID is the framework-specific identifier generated at runtime.
	AgentID string `json:"agentID,omitempty"`
}

// AgentPhase defines the lifecycle phase of an AIAgent.
type AgentPhase string

const (
	AgentPhasePending   AgentPhase = "Pending"
	AgentPhaseScheduling AgentPhase = "Scheduling"
	AgentPhaseRunning   AgentPhase = "Running"
	AgentPhaseMigrating AgentPhase = "Migrating"
	AgentPhaseFailed    AgentPhase = "Failed"
	AgentPhaseTerminated AgentPhase = "Terminated"
)

// RuntimeReferenceStatus shows the current runtime binding.
type RuntimeReferenceStatus struct {
	// Name of the currently bound AgentRuntime.
	Name string `json:"name"`

	// UID of the bound AgentRuntime for verification.
	UID string `json:"uid,omitempty"`
}

// AIAgentList contains a list of AIAgent.
// +kubebuilder:object:root=true
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type AIAgentList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []AIAgent `json:"items"`
}