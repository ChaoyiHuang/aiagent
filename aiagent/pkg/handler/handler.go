// Package handler defines the interface for Agent Handlers.
// An Agent Handler is responsible for:
// 1. Configuration Transformation: AIAgentSpec + Harness → Framework-specific config files
// 2. Framework Process Management: Start/Stop/Restart framework processes
// 3. Harness Adaptation: Standard Harness → Framework-specific Harness implementation
// 4. Multi-Instance Support: Handle multiple framework process instances
//
// Key Design Principle: Harness Standardization
// - Same Harness interface works for ADK, OpenClaw, Hermes, and future frameworks
// - Each framework provides its own HarnessAdapter implementation
// - Handler bridges standard Harness (from AgentRuntime CRD) to framework-specific config
package handler

import (
	"context"

	"aiagent/api/v1"
	"aiagent/pkg/agent"
)

// ============================================================
// Standard Harness Interfaces
// These define the unified abstraction that all frameworks adapt to.
// ============================================================

// HarnessManagerInterface defines the standard Harness interface.
// This is the unified abstraction that all frameworks must adapt to.
// Framework-specific handlers implement adapters for each harness type.
type HarnessManagerInterface interface {
	// Initialize initializes all harness components from specs.
	Initialize(ctx context.Context, specs []*v1.HarnessSpec) error

	// Individual Harness getters
	GetModelHarness() ModelHarnessInterface
	GetMCPHarness() MCPHarnessInterface
	GetMemoryHarness() MemoryHarnessInterface
	GetSandboxHarness() SandboxHarnessInterface
	GetSkillsHarness() SkillsHarnessInterface
	GetKnowledgeHarness() KnowledgeHarnessInterface
	GetGuardrailHarness() GuardrailHarnessInterface
	GetSecurityHarness() SecurityHarnessInterface
	GetPolicyHarness() PolicyHarnessInterface

	// Bulk conversion for framework config
	ToFrameworkConfig(frameworkType HandlerType) ([]byte, error)
}

// ModelHarnessInterface defines standard model harness.
// Adapters convert this to framework-specific model config.
type ModelHarnessInterface interface {
	GetProvider() string       // openai, anthropic, gemini, deepseek, etc.
	GetDefaultModel() string   // gpt-4, claude-3-opus, etc.
	GetAllowedModels() []string
	GetEndpoint() string       // API endpoint URL
	GetAPIKeyRef() string      // Reference to secret (not the actual key)
	ToFrameworkConfig(frameworkType HandlerType) ([]byte, error)
}

// MCPHarnessInterface defines standard MCP (Model Context Protocol) harness.
type MCPHarnessInterface interface {
	GetRegistryType() string   // local, remote, inmemory
	GetEndpoint() string       // Registry endpoint
	GetServers() []MCPServerInfo
	ToFrameworkConfig(frameworkType HandlerType) ([]byte, error)
}

type MCPServerInfo struct {
	Name     string
	Type     string // stdio, sse, websocket
	Endpoint string
	Command  string // For stdio type
	Args     []string
	Allowed  bool
}

// MemoryHarnessInterface defines standard memory/session storage harness.
type MemoryHarnessInterface interface {
	GetType() string        // inmemory, redis, file, postgres
	GetEndpoint() string    // Storage endpoint
	GetTTL() int64          // Session TTL in seconds
	IsPersistenceEnabled() bool
	ToFrameworkConfig(frameworkType HandlerType) ([]byte, error)
}

// SandboxHarnessInterface defines standard execution isolation harness.
type SandboxHarnessInterface interface {
	GetMode() SandboxMode    // embedded, external, none
	IsExternal() bool
	GetEndpoint() string     // External sandbox endpoint
	GetTimeout() int64       // Execution timeout
	GetResourceLimits() *ResourceLimits
	ToFrameworkConfig(frameworkType HandlerType) ([]byte, error)
}

type SandboxMode string

const (
	SandboxModeEmbedded SandboxMode = "embedded" // Local container isolation
	SandboxModeExternal  SandboxMode = "external" // Remote sandbox service
	SandboxModeNone      SandboxMode = "none"     // No isolation
)

type ResourceLimits struct {
	CPU    string // e.g., "1", "0.5"
	Memory string // e.g., "1Gi", "512Mi"
}

// SkillsHarnessInterface defines standard skills harness.
type SkillsHarnessInterface interface {
	GetHubType() string      // local, remote, marketplace
	GetEndpoint() string     // Skills hub endpoint
	GetSkills() []SkillInfo
	ToFrameworkConfig(frameworkType HandlerType) ([]byte, error)
}

type SkillInfo struct {
	Name    string
	Version string
	Allowed bool
	Path    string // For local skills
}

// KnowledgeHarnessInterface defines standard knowledge base harness.
type KnowledgeHarnessInterface interface {
	GetType() string      // vector, graph, file
	GetEndpoint() string
	GetCollections() []string
	ToFrameworkConfig(frameworkType HandlerType) ([]byte, error)
}

// GuardrailHarnessInterface defines standard safety guardrails harness.
type GuardrailHarnessInterface interface {
	GetRules() []GuardrailRule
	ToFrameworkConfig(frameworkType HandlerType) ([]byte, error)
}

type GuardrailRule struct {
	Name   string
	Type   string // content-filter, pii-detector, topic-restrict
	Config map[string]any
}

// SecurityHarnessInterface defines standard security policies harness.
type SecurityHarnessInterface interface {
	GetPolicies() []SecurityPolicy
	ToFrameworkConfig(frameworkType HandlerType) ([]byte, error)
}

type SecurityPolicy struct {
	Name   string
	Type   string // rate-limit, auth-required, role-based
	Config map[string]any
}

// PolicyHarnessInterface defines standard general policies harness.
type PolicyHarnessInterface interface {
	GetPolicies() []PolicyInfo
	ToFrameworkConfig(frameworkType HandlerType) ([]byte, error)
}

type PolicyInfo struct {
	Name   string
	Type   string
	Config map[string]any
}

// ============================================================
// Handler Interface
// ============================================================

// HandlerType identifies the framework type a handler supports.
type HandlerType string

const (
	HandlerTypeADK       HandlerType = "adk"
	HandlerTypeOpenClaw  HandlerType = "openclaw"
	HandlerTypeLangChain HandlerType = "langchain"
	HandlerTypeHermes    HandlerType = "hermes"
	HandlerTypeCustom    HandlerType = "custom"
)

// Handler is the interface for agent framework handlers.
// Each handler implementation is specific to one framework type.
//
// Handler's Core Responsibilities:
// ┌─────────────────────────────────────────────────────────────┐
// │ 1. Configuration Transformation                             │
// │    AIAgentSpec + HarnessConfig → Framework-specific config   │
// │    e.g., AIAgentSpec → openclaw.json for OpenClaw            │
// │    e.g., AIAgentSpec → agent.yaml for ADK                    │
// ├─────────────────────────────────────────────────────────────┤
// │ 2. Framework Process Management                              │
// │    - Start framework process with proper config              │
// │    - Monitor process health                                  │
// │    - Support multiple instances if framework allows          │
// ├─────────────────────────────────────────────────────────────┤
// │ 3. Harness Adaptation                                        │
// │    Standard Harness → Framework-specific Harness config      │
// │    Each framework has different harness integration          │
// ├─────────────────────────────────────────────────────────────┤
// │ 4. Agent Lifecycle (via Framework)                           │
// │    Handler doesn't directly run agents                       │
// │    Framework runs agents based on config                     │
// └─────────────────────────────────────────────────────────────┘
type Handler interface {
	// Framework identification
	Type() HandlerType
	GetFrameworkInfo() *FrameworkInfo

	// ── Core Responsibility #1: Configuration Transformation ──
	// Convert AIAgentSpec + HarnessConfig to framework-specific configuration format
	// This is the primary job of Handler
	GenerateFrameworkConfig(spec *v1.AIAgentSpec, harness *HarnessConfig) ([]byte, error)
	GenerateAgentConfig(spec *v1.AIAgentSpec, harness *HarnessConfig) ([]byte, error)
	GenerateHarnessConfig(harness *HarnessConfig) ([]byte, error)

	// ── Core Responsibility #2: Framework Process Management ──
	// Prepare work directory structure
	PrepareWorkDirectory(ctx context.Context, workDir string) error
	// Write config files to work directory (bind mount source)
	WriteConfigFiles(ctx context.Context, workDir string, configs map[string][]byte) error
	// Start framework process (single instance)
	StartFramework(ctx context.Context, frameworkBin string, workDir string, configPath string) error
	// Start multiple framework instances (if SupportsMultiInstance)
	StartFrameworkInstance(ctx context.Context, instanceID string, configPath string) error
	// Stop framework
	StopFramework(ctx context.Context) error
	StopFrameworkInstance(ctx context.Context, instanceID string) error
	// Framework health
	IsFrameworkRunning(ctx context.Context) bool
	GetFrameworkStatus(ctx context.Context) (*FrameworkStatus, error)

	// ── Core Responsibility #3: Harness Adaptation ──
	// Set harness manager (contains standard harness implementations)
	SetHarnessManager(harnessMgr HarnessManagerInterface) error
	// Adapt each harness type to framework-specific config
	AdaptModelHarness(harness ModelHarnessInterface) ([]byte, error)
	AdaptMCPHarness(harness MCPHarnessInterface) ([]byte, error)
	AdaptMemoryHarness(harness MemoryHarnessInterface) ([]byte, error)
	AdaptSandboxHarness(harness SandboxHarnessInterface) ([]byte, error)
	AdaptSkillsHarness(harness SkillsHarnessInterface) ([]byte, error)
	GetHarnessManager() HarnessManagerInterface

	// ── Core Responsibility #4: Agent Lifecycle (via Framework) ──
	// Load agent creates agent wrapper (framework runs agent internally)
	// agentName is the AIAgent CRD metadata.name, used as unique agent identifier
	LoadAgent(ctx context.Context, spec *v1.AIAgentSpec, harness *HarnessConfig, agentName string) (agent.Agent, error)
	// Start agent notifies framework to activate agent
	StartAgent(ctx context.Context, ag agent.Agent, config *AgentConfig) error
	StopAgent(ctx context.Context, agentID string) error
	GetAgentStatus(ctx context.Context, agentID string) (*AgentStatus, error)
	ListAgents(ctx context.Context) ([]AgentInfo, error)

	// ── Framework Capability Queries ──
	SupportsMultiAgent() bool    // Can run multiple agents in one framework instance
	SupportsMultiInstance() bool // Can start multiple framework processes

	// ── Instance Management (for multi-instance frameworks) ──
	ListFrameworkInstances(ctx context.Context) []FrameworkInstanceInfo
	GetFrameworkInstance(instanceID string) *FrameworkInstanceInfo
}

// FrameworkInfo contains information about a framework.
type FrameworkInfo struct {
	Type         HandlerType
	Name         string
	Version      string
	Description  string
	Capabilities []string
	ConfigFormat string // yaml, json, toml
}

// FrameworkStatus contains framework process status.
type FrameworkStatus struct {
	Running       bool
	ProcessID     int
	StartedAt     int64
	ConfigPath    string
	WorkDir       string
	InstanceCount int
	AgentCount    int
	Health        string // healthy, degraded, error
}

// FrameworkInstanceInfo contains information about a framework instance.
type FrameworkInstanceInfo struct {
	InstanceID    string
	ProcessID     int
	ConfigPath    string
	WorkDir       string
	StartedAt     int64
	AgentIDs      []string
	Status        string // running, stopped, error
}

// ============================================================
// Data Structures
// ============================================================

// HarnessConfig aggregates harness configurations from AgentRuntime CRD.
type HarnessConfig struct {
	Model     *v1.ModelHarnessSpec
	MCP       *v1.MCPHarnessSpec
	Memory    *v1.MemoryHarnessSpec
	Sandbox   *v1.SandboxHarnessSpec
	Skills    *v1.SkillsHarnessSpec
	Knowledge *v1.KnowledgeHarnessSpec
	Guardrail *v1.GuardrailHarnessSpec
	Security  *v1.SecurityHarnessSpec
	Policy    *v1.PolicyHarnessSpec
}

// AgentConfig contains agent-specific configuration.
type AgentConfig struct {
	Name         string
	Description  string
	Prompt       string
	Skills       []string
	Tools        []string
	Model        string
	Temperature  float64
	MaxTokens    int
	CustomConfig map[string]any
	ConfigFiles  []ConfigFile
}

// ConfigFile represents a configuration file to be written.
type ConfigFile struct {
	Name      string
	Content   []byte
	MountPath string
}

// AgentStatus contains the current status of an agent.
type AgentStatus struct {
	ID                string
	Name              string
	Phase             v1.AgentPhase
	Running           bool
	SessionCount      int
	LastActivityTime  int64
	Metrics           *AgentMetrics
	FrameworkSpecific map[string]any
}

// AgentMetrics contains agent performance metrics.
type AgentMetrics struct {
	TotalInvocations           int64
	SuccessfulInvocations      int64
	FailedInvocations          int64
	AverageLatency             float64
	TokensUsed                 int64
	AverageTokensPerInvocation float64
}

// AgentInfo contains basic information about an agent.
type AgentInfo struct {
	ID        string
	Name      string
	Type      agent.AgentType
	Phase     v1.AgentPhase
}

// ============================================================
// Handler Configuration
// ============================================================

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

// HandlerConfig contains configuration for creating a handler.
type HandlerConfig struct {
	Type             HandlerType
	FrameworkVersion string

	// ProcessMode defines how agents are organized
	ProcessMode ProcessModeType

	// Framework binary path (from ImageVolume mount)
	FrameworkBin string // e.g., /framework-rootfs/adk-framework, /framework-rootfs/usr/local/bin/openclaw

	// Shared directories
	WorkDir   string // e.g., /shared/workdir
	ConfigDir string // e.g., /shared/config

	// Debug mode
	DebugMode bool
}

// BaseHandler provides common functionality for handler implementations.
type BaseHandler struct {
	handlerType   HandlerType
	frameworkInfo *FrameworkInfo
	config        *HandlerConfig
	agents        map[string]*AgentInfo
}

// NewBaseHandler creates a new BaseHandler.
func NewBaseHandler(cfg *HandlerConfig, info *FrameworkInfo) *BaseHandler {
	return &BaseHandler{
		handlerType:   cfg.Type,
		frameworkInfo: info,
		config:        cfg,
		agents:        make(map[string]*AgentInfo),
	}
}

func (h *BaseHandler) Type() HandlerType {
	return h.handlerType
}

func (h *BaseHandler) GetFrameworkInfo() *FrameworkInfo {
	return h.frameworkInfo
}

func (h *BaseHandler) ListAgents(ctx context.Context) ([]AgentInfo, error) {
	agents := make([]AgentInfo, 0)
	for _, info := range h.agents {
		agents = append(agents, *info)
	}
	return agents, nil
}

func (h *BaseHandler) RegisterAgent(info *AgentInfo) {
	h.agents[info.ID] = info
}

func (h *BaseHandler) UnregisterAgent(agentID string) {
	delete(h.agents, agentID)
}

func (h *BaseHandler) GetAgent(agentID string) *AgentInfo {
	return h.agents[agentID]
}

func (h *BaseHandler) GetConfig() *HandlerConfig {
	return h.config
}