package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
)

// Harness is an independent CRD for AI Agent scaffolding capabilities.
// It defines configurations for various external capabilities that
// agents can access through the unified abstraction layer.
//
// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Type",type=string,JSONPath=.spec.type
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=.metadata.creationTimestamp
type Harness struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   HarnessSpec   `json:"spec,omitempty"`
	Status HarnessStatus `json:"status,omitempty"`
}

// HarnessSpec defines the desired state of Harness.
// Each harness has a specific type and corresponding configuration.
type HarnessSpec struct {
	// Type identifies the harness capability type.
	// +kubebuilder:validation:Enum=model;mcp;skills;knowledge;memory;state;guardrail;security;policy;sandbox
	Type HarnessType `json:"type"`

	// Model configuration for LLM model integration.
	Model *ModelHarnessSpec `json:"model,omitempty"`

	// MCP configuration for Model Context Protocol registry.
	MCP *MCPHarnessSpec `json:"mcp,omitempty"`

	// Skills configuration for skill modules.
	Skills *SkillsHarnessSpec `json:"skills,omitempty"`

	// Knowledge configuration for knowledge base/RAG.
	Knowledge *KnowledgeHarnessSpec `json:"knowledge,omitempty"`

	// Memory configuration for memory storage.
	Memory *MemoryHarnessSpec `json:"memory,omitempty"`

	// State configuration for runtime state management.
	State *StateHarnessSpec `json:"state,omitempty"`

	// Guardrail configuration for safety guardrails.
	Guardrail *GuardrailHarnessSpec `json:"guardrail,omitempty"`

	// Security configuration for security policies.
	Security *SecurityHarnessSpec `json:"security,omitempty"`

	// Policy configuration for policy control.
	Policy *PolicyHarnessSpec `json:"policy,omitempty"`

	// Sandbox configuration for execution isolation.
	Sandbox *SandboxHarnessSpec `json:"sandbox,omitempty"`
}

// HarnessType defines the type of harness capability.
type HarnessType string

const (
	HarnessTypeModel    HarnessType = "model"
	HarnessTypeMCP      HarnessType = "mcp"
	HarnessTypeSkills   HarnessType = "skills"
	HarnessTypeKnowledge HarnessType = "knowledge"
	HarnessTypeMemory   HarnessType = "memory"
	HarnessTypeState    HarnessType = "state"
	HarnessTypeGuardrail HarnessType = "guardrail"
	HarnessTypeSecurity HarnessType = "security"
	HarnessTypePolicy   HarnessType = "policy"
	HarnessTypeSandbox  HarnessType = "sandbox"
)

// ModelHarnessSpec configures LLM model integration.
type ModelHarnessSpec struct {
	// Provider identifies the model provider.
	// +kubebuilder:validation:Enum=deepseek;openai;anthropic;google;azure;aws;local
	Provider string `json:"provider"`

	// Endpoint is the API endpoint URL.
	Endpoint string `json:"endpoint,omitempty"`

	// AuthSecretRef references the secret containing API credentials.
	AuthSecretRef string `json:"authSecretRef,omitempty"`

	// Models lists available models with their configurations.
	Models []ModelConfig `json:"models"`

	// DefaultModel is the default model to use.
	DefaultModel string `json:"defaultModel,omitempty"`

	// RateLimit defines rate limiting configuration.
	RateLimit *RateLimitConfig `json:"rateLimit,omitempty"`

	// RetryConfig defines retry behavior.
	RetryConfig *RetryConfig `json:"retryConfig,omitempty"`
}

// ModelConfig defines a specific model's configuration.
type ModelConfig struct {
	// Name is the model identifier.
	Name string `json:"name"`

	// Allowed indicates if this model is accessible.
	Allowed bool `json:"allowed"`

	// ContextWindow is the maximum context length for this model.
	ContextWindow int32 `json:"contextWindow,omitempty"`

	// RateLimit override for this specific model.
	RateLimit *RateLimitConfig `json:"rateLimit,omitempty"`

	// MaxTokens maximum tokens for this model.
	MaxTokens int32 `json:"maxTokens,omitempty"`

	// CostPerToken cost per token for billing.
	CostPerToken float64 `json:"costPerToken,omitempty"`
}

// RateLimitConfig defines rate limiting.
type RateLimitConfig struct {
	// RequestsPerMinute limits requests per minute.
	RequestsPerMinute int32 `json:"requestsPerMinute"`

	// TokensPerMinute limits tokens per minute.
	TokensPerMinute int32 `json:"tokensPerMinute,omitempty"`

	// ConcurrentRequests limits concurrent requests.
	ConcurrentRequests int32 `json:"concurrentRequests,omitempty"`
}

// RetryConfig defines retry behavior.
type RetryConfig struct {
	// MaxRetries maximum number of retries.
	MaxRetries int32 `json:"maxRetries"`

	// InitialDelayMs initial retry delay in milliseconds.
	InitialDelayMs int32 `json:"initialDelayMs"`

	// MaxDelayMs maximum retry delay in milliseconds.
	MaxDelayMs int32 `json:"maxDelayMs"`

	// Multiplier for exponential backoff.
	Multiplier float64 `json:"multiplier,omitempty"`
}

// MCPHarnessSpec configures MCP registry integration.
type MCPHarnessSpec struct {
	// RegistryType identifies the registry type.
	// +kubebuilder:validation:Enum=builtin;external;custom
	RegistryType string `json:"registryType"`

	// Endpoint is the registry endpoint URL.
	Endpoint string `json:"endpoint,omitempty"`

	// Servers lists available MCP servers.
	Servers []MCPServerConfig `json:"servers,omitempty"`

	// AuthSecretRef references credentials for the registry.
	AuthSecretRef string `json:"authSecretRef,omitempty"`

	// DiscoveryEnabled enables automatic server discovery.
	DiscoveryEnabled bool `json:"discoveryEnabled,omitempty"`
}

// MCPServerConfig defines an MCP server configuration.
type MCPServerConfig struct {
	// Name is the server identifier.
	Name string `json:"name"`

	// Type is the server type (builtin, stdio, http, etc).
	Type string `json:"type"`

	// Command to execute for stdio servers.
	Command string `json:"command,omitempty"`

	// Args for the command.
	Args []string `json:"args,omitempty"`

	// URL for HTTP servers.
	URL string `json:"url,omitempty"`

	// Env environment variables.
	Env map[string]string `json:"env,omitempty"`

	// Allowed indicates if this server is accessible.
	Allowed bool `json:"allowed"`
}

// SkillsHarnessSpec configures skill modules.
type SkillsHarnessSpec struct {
	// HubType identifies the skill hub type.
	// +kubebuilder:validation:Enum=builtin;external;local
	HubType string `json:"hubType"`

	// Endpoint is the skill hub endpoint.
	Endpoint string `json:"endpoint,omitempty"`

	// Skills lists available skills.
	Skills []SkillConfig `json:"skills,omitempty"`

	// LocalPath path to local skills directory.
	LocalPath string `json:"localPath,omitempty"`

	// AutoUpdate enables automatic skill updates.
	AutoUpdate bool `json:"autoUpdate,omitempty"`
}

// SkillConfig defines a skill configuration.
type SkillConfig struct {
	// Name is the skill identifier.
	Name string `json:"name"`

	// Version is the skill version.
	Version string `json:"version,omitempty"`

	// Allowed indicates if this skill is accessible.
	Allowed bool `json:"allowed"`

	// Config skill-specific configuration.
	// +kubebuilder:pruning:PreserveUnknownFields
	Config *apiextensionsv1.JSON `json:"config,omitempty"`
}

// KnowledgeHarnessSpec configures knowledge base/RAG.
type KnowledgeHarnessSpec struct {
	// Type identifies the knowledge store type.
	// +kubebuilder:validation:Enum=vector;document;hybrid
	Type string `json:"type"`

	// Endpoint is the knowledge store endpoint.
	Endpoint string `json:"endpoint,omitempty"`

	// Collections available knowledge collections.
	Collections []string `json:"collections,omitempty"`

	// EmbeddingModel model for embeddings.
	EmbeddingModel string `json:"embeddingModel,omitempty"`

	// AuthSecretRef references credentials.
	AuthSecretRef string `json:"authSecretRef,omitempty"`
}

// MemoryHarnessSpec configures memory storage.
type MemoryHarnessSpec struct {
	// Type identifies the memory backend type.
	// +kubebuilder:validation:Enum=inmemory;redis;file;postgres
	Type string `json:"type"`

	// Endpoint is the memory backend endpoint.
	Endpoint string `json:"endpoint,omitempty"`

	// AuthSecretRef references credentials.
	AuthSecretRef string `json:"authSecretRef,omitempty"`

	// TTL time-to-live for entries in seconds.
	TTL int32 `json:"ttl,omitempty"`

	// MaxSize maximum storage size.
	MaxSize string `json:"maxSize,omitempty"`

	// PersistenceEnabled enables data persistence.
	PersistenceEnabled bool `json:"persistenceEnabled,omitempty"`
}

// StateHarnessSpec configures runtime state management.
type StateHarnessSpec struct {
	// Type identifies the state backend type.
	// +kubebuilder:validation:Enum=inmemory;redis;file
	Type string `json:"type"`

	// Endpoint is the state backend endpoint.
	Endpoint string `json:"endpoint,omitempty"`

	// AuthSecretRef references credentials.
	AuthSecretRef string `json:"authSecretRef,omitempty"`

	// SessionTTL session state TTL in seconds.
	SessionTTL int32 `json:"sessionTTL,omitempty"`
}

// GuardrailHarnessSpec configures safety guardrails.
type GuardrailHarnessSpec struct {
	// Type identifies the guardrail type.
	// +kubebuilder:validation:Enum=neuron;builtin;custom
	Type string `json:"type"`

	// Endpoint is the guardrail service endpoint.
	Endpoint string `json:"endpoint,omitempty"`

	// Rules guardrail rules configuration.
	Rules []GuardrailRule `json:"rules,omitempty"`

	// Enabled turns on guardrails.
	Enabled bool `json:"enabled"`
}

// GuardrailRule defines a guardrail rule.
type GuardrailRule struct {
	// Name is the rule identifier.
	Name string `json:"name"`

	// Type is the rule type.
	Type string `json:"type"`

	// Severity of the rule.
	// +kubebuilder:validation:Enum=low;medium;high;critical
	Severity string `json:"severity"`

	// Action when rule is triggered.
	// +kubebuilder:validation:Enum=warn;block;log
	Action string `json:"action"`

	// Config rule-specific configuration.
	// +kubebuilder:pruning:PreserveUnknownFields
	Config *apiextensionsv1.JSON `json:"config,omitempty"`
}

// SecurityHarnessSpec configures security policies.
type SecurityHarnessSpec struct {
	// Policies security policies.
	Policies []SecurityPolicy `json:"policies,omitempty"`

	// AuditEnabled enables security audit logging.
	AuditEnabled bool `json:"auditEnabled,omitempty"`

	// AuditLogPath path to audit log.
	AuditLogPath string `json:"auditLogPath,omitempty"`
}

// SecurityPolicy defines a security policy.
type SecurityPolicy struct {
	// Name is the policy identifier.
	Name string `json:"name"`

	// Type is the policy type.
	Type string `json:"type"`

	// Config policy-specific configuration.
	// +kubebuilder:pruning:PreserveUnknownFields
	Config *apiextensionsv1.JSON `json:"config,omitempty"`
}

// PolicyHarnessSpec configures policy control.
type PolicyHarnessSpec struct {
	// Rules policy rules.
	Rules []PolicyRule `json:"rules,omitempty"`

	// DefaultAction default action when no rule matches.
	DefaultAction string `json:"defaultAction,omitempty"`
}

// PolicyRule defines a policy rule.
type PolicyRule struct {
	// Name is the rule identifier.
	Name string `json:"name"`

	// Condition for the rule.
	Condition string `json:"condition"`

	// Action when condition is met.
	Action string `json:"action"`
}

// SandboxHarnessSpec configures execution isolation.
type SandboxHarnessSpec struct {
	// Type identifies the sandbox type.
	// +kubebuilder:validation:Enum=gvisor;docker;kata;firecracker;custom
	Type string `json:"type"`

	// Mode defines sandbox operation mode.
	// +kubebuilder:validation:Enum=external;embedded
	Mode SandboxModeType `json:"mode"`

	// Endpoint is the External Sandbox API URL (e.g., "http://sandbox.example.com:9000").
	// Only applicable for external mode.
	Endpoint string `json:"endpoint,omitempty"`

	// APIKey is the authentication key for External Sandbox.
	// Should reference a Secret in production: authSecretRef.
	APIKey string `json:"apiKey,omitempty"`

	// AuthSecretRef references a Secret containing the API key.
	// Preferred over inline apiKey for security.
	AuthSecretRef string `json:"authSecretRef,omitempty"`

	// TemplateRef references a SandboxTemplate CRD.
	TemplateRef string `json:"templateRef,omitempty"`

	// WarmPoolRef references a SandboxWarmPool CRD.
	WarmPoolRef string `json:"warmPoolRef,omitempty"`

	// Timeout execution timeout in seconds.
	Timeout int32 `json:"timeout,omitempty"`

	// ResourceLimits sandbox resource limits.
	ResourceLimits *SandboxResourceLimits `json:"resourceLimits,omitempty"`

	// NetworkPolicy network isolation policy.
	NetworkPolicy *SandboxNetworkPolicy `json:"networkPolicy,omitempty"`
}

// SandboxModeType defines sandbox operation mode.
type SandboxModeType string

const (
	SandboxModeExternal SandboxModeType = "external"
	SandboxModeEmbedded SandboxModeType = "embedded"
)

// SandboxResourceLimits defines sandbox resource constraints.
type SandboxResourceLimits struct {
	// CPU limit.
	CPU string `json:"cpu,omitempty"`

	// Memory limit.
	Memory string `json:"memory,omitempty"`

	// PIDs limit.
	PIDs int32 `json:"pids,omitempty"`
}

// SandboxNetworkPolicy defines network isolation.
type SandboxNetworkPolicy struct {
	// AllowOutbound allows outbound connections.
	AllowOutbound bool `json:"allowOutbound"`

	// AllowInbound allows inbound connections.
	AllowInbound bool `json:"allowInbound"`

	// AllowedHosts whitelist of allowed hosts.
	AllowedHosts []string `json:"allowedHosts,omitempty"`

	// DeniedHosts blacklist of denied hosts.
	DeniedHosts []string `json:"deniedHosts,omitempty"`
}

// HarnessStatus defines the observed state of Harness.
type HarnessStatus struct {
	// Phase indicates the current state.
	// +kubebuilder:validation:Enum=Available;Unavailable;Error
	Phase HarnessPhase `json:"phase"`

	// Conditions represent the latest observations.
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// LastSyncTime when the harness was last synchronized.
	LastSyncTime metav1.Time `json:"lastSyncTime,omitempty"`

	// ConnectionStatus connection to the underlying service.
	ConnectionStatus string `json:"connectionStatus,omitempty"`
}

// HarnessPhase defines the state of a Harness.
type HarnessPhase string

const (
	HarnessPhaseAvailable   HarnessPhase = "Available"
	HarnessPhaseUnavailable HarnessPhase = "Unavailable"
	HarnessPhaseError       HarnessPhase = "Error"
)

// HarnessList contains a list of Harness.
// +kubebuilder:object:root=true
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type HarnessList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []Harness `json:"items"`
}