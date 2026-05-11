// Package harness provides integration for external AI Agent capabilities.
// Harnesses are externalized capabilities that agents can access through
// the unified abstraction layer. Each harness type provides specific functionality:
// - Model: LLM provider integration
// - MCP: Model Context Protocol registry
// - Memory: State and session storage
// - Sandbox: Execution isolation and remote tool execution
// - Skills: Tool/skill modules with local or remote execution
package harness

import (
	"context"
	"fmt"
	"sync"

	"aiagent/api/v1"
)

// HarnessType identifies the type of harness capability.
type HarnessType string

const (
	HarnessTypeModel    HarnessType = "model"
	HarnessTypeMCP      HarnessType = "mcp"
	HarnessTypeMemory   HarnessType = "memory"
	HarnessTypeSandbox  HarnessType = "sandbox"
	HarnessTypeSkills   HarnessType = "skills"
	HarnessTypeKnowledge HarnessType = "knowledge"
	HarnessTypeState    HarnessType = "state"
	HarnessTypeGuardrail HarnessType = "guardrail"
	HarnessTypeSecurity HarnessType = "security"
	HarnessTypePolicy   HarnessType = "policy"
)

// HarnessManager manages all harness instances for an AgentRuntime.
// It provides unified access to all external capabilities.
type HarnessManager struct {
	mu sync.RWMutex

	// modelHarness manages LLM provider connections
	modelHarness *ModelHarness

	// mcpHarness manages MCP registry and servers
	mcpHarness *MCPHarness

	// memoryHarness manages state storage backends
	memoryHarness *MemoryHarness

	// sandboxHarness manages execution isolation
	sandboxHarness *SandboxHarness

	// skillsHarness manages skill/tool modules
	skillsHarness *SkillsHarness

	// knowledgeHarness manages knowledge base/RAG
	knowledgeHarness *KnowledgeHarness

	// harnessConfigs stores the original CRD specs
	harnessConfigs map[string]*v1.HarnessSpec

	// harnessStatus tracks harness availability
	harnessStatus map[HarnessType]*HarnessStatus
}

// HarnessStatus contains the current status of a harness.
type HarnessStatus struct {
	Type         HarnessType
	Phase        HarnessPhase
	Available    bool
	ConnectionOK bool
	LastError    string
}

// HarnessPhase indicates the lifecycle phase of a harness.
type HarnessPhase string

const (
	HarnessPhaseInitializing HarnessPhase = "Initializing"
	HarnessPhaseAvailable    HarnessPhase = "Available"
	HarnessPhaseUnavailable  HarnessPhase = "Unavailable"
	HarnessPhaseError        HarnessPhase = "Error"
)

// NewHarnessManager creates a new harness manager.
func NewHarnessManager() *HarnessManager {
	return &HarnessManager{
		harnessConfigs: make(map[string]*v1.HarnessSpec),
		harnessStatus:  make(map[HarnessType]*HarnessStatus),
	}
}

// Initialize initializes all harnesses from the given specs.
func (m *HarnessManager) Initialize(ctx context.Context, specs []*v1.HarnessSpec) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, spec := range specs {
		m.harnessConfigs[string(spec.Type)] = spec

		switch spec.Type {
		case v1.HarnessTypeModel:
			if err := m.initModelHarness(ctx, spec); err != nil {
				return fmt.Errorf("failed to initialize model harness: %w", err)
			}
		case v1.HarnessTypeMCP:
			if err := m.initMCPHarness(ctx, spec); err != nil {
				return fmt.Errorf("failed to initialize MCP harness: %w", err)
			}
		case v1.HarnessTypeMemory:
			if err := m.initMemoryHarness(ctx, spec); err != nil {
				return fmt.Errorf("failed to initialize memory harness: %w", err)
			}
		case v1.HarnessTypeSandbox:
			if err := m.initSandboxHarness(ctx, spec); err != nil {
				return fmt.Errorf("failed to initialize sandbox harness: %w", err)
			}
		case v1.HarnessTypeSkills:
			if err := m.initSkillsHarness(ctx, spec); err != nil {
				return fmt.Errorf("failed to initialize skills harness: %w", err)
			}
		case v1.HarnessTypeKnowledge:
			if err := m.initKnowledgeHarness(ctx, spec); err != nil {
				return fmt.Errorf("failed to initialize knowledge harness: %w", err)
			}
		}
	}

	return nil
}

// initModelHarness initializes the model harness.
func (m *HarnessManager) initModelHarness(ctx context.Context, spec *v1.HarnessSpec) error {
	if spec.Model == nil {
		return fmt.Errorf("model spec is nil")
	}

	m.modelHarness = NewModelHarness(spec.Model)
	m.harnessStatus[HarnessTypeModel] = &HarnessStatus{
		Type:      HarnessTypeModel,
		Phase:     HarnessPhaseAvailable,
		Available: true,
	}
	return nil
}

// initMCPHarness initializes the MCP harness.
func (m *HarnessManager) initMCPHarness(ctx context.Context, spec *v1.HarnessSpec) error {
	if spec.MCP == nil {
		return fmt.Errorf("MCP spec is nil")
	}

	m.mcpHarness = NewMCPHarness(spec.MCP)
	m.harnessStatus[HarnessTypeMCP] = &HarnessStatus{
		Type:      HarnessTypeMCP,
		Phase:     HarnessPhaseAvailable,
		Available: true,
	}
	return nil
}

// initMemoryHarness initializes the memory harness.
func (m *HarnessManager) initMemoryHarness(ctx context.Context, spec *v1.HarnessSpec) error {
	if spec.Memory == nil {
		return fmt.Errorf("memory spec is nil")
	}

	m.memoryHarness = NewMemoryHarness(spec.Memory)
	m.harnessStatus[HarnessTypeMemory] = &HarnessStatus{
		Type:      HarnessTypeMemory,
		Phase:     HarnessPhaseAvailable,
		Available: true,
	}
	return nil
}

// initSandboxHarness initializes the sandbox harness.
func (m *HarnessManager) initSandboxHarness(ctx context.Context, spec *v1.HarnessSpec) error {
	if spec.Sandbox == nil {
		return fmt.Errorf("sandbox spec is nil")
	}

	m.sandboxHarness = NewSandboxHarness(spec.Sandbox)
	m.harnessStatus[HarnessTypeSandbox] = &HarnessStatus{
		Type:      HarnessTypeSandbox,
		Phase:     HarnessPhaseAvailable,
		Available: true,
	}
	return nil
}

// initSkillsHarness initializes the skills harness.
func (m *HarnessManager) initSkillsHarness(ctx context.Context, spec *v1.HarnessSpec) error {
	if spec.Skills == nil {
		return fmt.Errorf("skills spec is nil")
	}

	// Skills harness may use sandbox for remote tool execution
	m.skillsHarness = NewSkillsHarness(spec.Skills, m.sandboxHarness)
	m.harnessStatus[HarnessTypeSkills] = &HarnessStatus{
		Type:      HarnessTypeSkills,
		Phase:     HarnessPhaseAvailable,
		Available: true,
	}
	return nil
}

// initKnowledgeHarness initializes the knowledge harness.
func (m *HarnessManager) initKnowledgeHarness(ctx context.Context, spec *v1.HarnessSpec) error {
	if spec.Knowledge == nil {
		return fmt.Errorf("knowledge spec is nil")
	}

	m.knowledgeHarness = NewKnowledgeHarness(spec.Knowledge)
	m.harnessStatus[HarnessTypeKnowledge] = &HarnessStatus{
		Type:      HarnessTypeKnowledge,
		Phase:     HarnessPhaseAvailable,
		Available: true,
	}
	return nil
}

// GetModelHarness returns the model harness.
func (m *HarnessManager) GetModelHarness() *ModelHarness {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.modelHarness
}

// GetMCPHarness returns the MCP harness.
func (m *HarnessManager) GetMCPHarness() *MCPHarness {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.mcpHarness
}

// GetMemoryHarness returns the memory harness.
func (m *HarnessManager) GetMemoryHarness() *MemoryHarness {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.memoryHarness
}

// GetSandboxHarness returns the sandbox harness.
func (m *HarnessManager) GetSandboxHarness() *SandboxHarness {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.sandboxHarness
}

// GetSkillsHarness returns the skills harness.
func (m *HarnessManager) GetSkillsHarness() *SkillsHarness {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.skillsHarness
}

// GetKnowledgeHarness returns the knowledge harness.
func (m *HarnessManager) GetKnowledgeHarness() *KnowledgeHarness {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.knowledgeHarness
}

// GetGuardrailHarness returns the guardrail harness (not implemented).
func (m *HarnessManager) GetGuardrailHarness() *GuardrailHarness {
	return nil
}

// GetSecurityHarness returns the security harness (not implemented).
func (m *HarnessManager) GetSecurityHarness() *SecurityHarness {
	return nil
}

// GetPolicyHarness returns the policy harness (not implemented).
func (m *HarnessManager) GetPolicyHarness() *PolicyHarness {
	return nil
}

// GetHarnessStatus returns the status of a specific harness type.
func (m *HarnessManager) GetHarnessStatus(hType HarnessType) *HarnessStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.harnessStatus[hType]
}

// GetAllHarnessStatus returns all harness statuses.
func (m *HarnessManager) GetAllHarnessStatus() map[HarnessType]*HarnessStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.harnessStatus
}

// IsHarnessAvailable checks if a harness type is available.
func (m *HarnessManager) IsHarnessAvailable(hType HarnessType) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	status := m.harnessStatus[hType]
	return status != nil && status.Available
}

// Shutdown shuts down all harnesses.
func (m *HarnessManager) Shutdown(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var errs []error

	if m.sandboxHarness != nil {
		if err := m.sandboxHarness.Shutdown(ctx); err != nil {
			errs = append(errs, fmt.Errorf("sandbox shutdown: %w", err))
		}
	}

	if m.memoryHarness != nil {
		if err := m.memoryHarness.Shutdown(ctx); err != nil {
			errs = append(errs, fmt.Errorf("memory shutdown: %w", err))
		}
	}

	if m.mcpHarness != nil {
		if err := m.mcpHarness.Shutdown(ctx); err != nil {
			errs = append(errs, fmt.Errorf("MCP shutdown: %w", err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("errors during shutdown: %v", errs)
	}

	return nil
}

// CheckHarnessHealth checks the health of a specific harness.
// For External Sandbox, this makes an HTTP health check to the remote service.
func (m *HarnessManager) CheckHarnessHealth(ctx context.Context, hType HarnessType) (*HarnessStatus, error) {
	m.mu.RLock()
	harnessStatus := m.harnessStatus[hType]
	m.mu.RUnlock()

	if harnessStatus == nil {
		return nil, fmt.Errorf("harness type %s not initialized", hType)
	}

	// For Sandbox harness, perform actual health check
	if hType == HarnessTypeSandbox && m.sandboxHarness != nil {
		health, err := m.sandboxHarness.Health(ctx)
		if err != nil {
			harnessStatus.Phase = HarnessPhaseError
			harnessStatus.Available = false
			harnessStatus.ConnectionOK = false
			harnessStatus.LastError = err.Error()
			return harnessStatus, err
		}

		harnessStatus.Phase = HarnessPhaseAvailable
		harnessStatus.Available = health.Available
		harnessStatus.ConnectionOK = health.Healthy
		if health.LastError != "" {
			harnessStatus.LastError = health.LastError
		}
	}

	return harnessStatus, nil
}

// ToHarnessConfig builds a HarnessConfig structure from current harness specs.
// This is now handled by handlers directly - they access harness specs via GetXxxHarness().spec.

// CheckAllHarnessHealth checks the health of all initialized harnesses.
func (m *HarnessManager) CheckAllHarnessHealth(ctx context.Context) map[HarnessType]*HarnessStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	results := make(map[HarnessType]*HarnessStatus)
	for hType := range m.harnessStatus {
		status, _ := m.CheckHarnessHealth(ctx, hType)
		if status != nil {
			results[hType] = status
		}
	}

	return results
}

// GetSandboxEndpoint returns the External Sandbox endpoint URL.
// Used by Plugin Generator to configure the harness-bridge plugin.
func (m *HarnessManager) GetSandboxEndpoint() string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.sandboxHarness == nil {
		return ""
	}

	if m.sandboxHarness.spec == nil {
		return ""
	}

	return m.sandboxHarness.spec.Endpoint
}

// GetSandboxAPIKey returns the External Sandbox API key.
// Used by Plugin Generator to configure authentication.
func (m *HarnessManager) GetSandboxAPIKey() string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.sandboxHarness == nil || m.sandboxHarness.spec == nil {
		return ""
	}

	return m.sandboxHarness.spec.APIKey
}

// GetSandboxMode returns the current sandbox mode.
func (m *HarnessManager) GetSandboxMode() SandboxMode {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.sandboxHarness == nil {
		return SandboxModeEmbedded // Default
	}

	return m.sandboxHarness.GetMode()
}

// IsExternalSandbox returns true if using External Sandbox mode.
func (m *HarnessManager) IsExternalSandbox() bool {
	return m.GetSandboxMode() == SandboxModeExternal
}

// UpdateHarness updates a harness configuration dynamically.
// This allows runtime updates without full restart.
func (m *HarnessManager) UpdateHarness(ctx context.Context, spec *v1.HarnessSpec) error {
	if spec == nil {
		return fmt.Errorf("spec is nil")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Update stored config
	m.harnessConfigs[string(spec.Type)] = spec

	// Re-initialize the specific harness
	switch spec.Type {
	case v1.HarnessTypeModel:
		return m.initModelHarness(ctx, spec)
	case v1.HarnessTypeMCP:
		return m.initMCPHarness(ctx, spec)
	case v1.HarnessTypeMemory:
		return m.initMemoryHarness(ctx, spec)
	case v1.HarnessTypeSandbox:
		// For sandbox, shutdown old one first
		if m.sandboxHarness != nil {
			m.sandboxHarness.Shutdown(ctx)
		}
		return m.initSandboxHarness(ctx, spec)
	case v1.HarnessTypeSkills:
		return m.initSkillsHarness(ctx, spec)
	case v1.HarnessTypeKnowledge:
		return m.initKnowledgeHarness(ctx, spec)
	default:
		return fmt.Errorf("unknown harness type: %s", spec.Type)
	}
}

// CreateWorkspaceForSession creates a workspace for session isolation.
// Only works with External Sandbox mode.
func (m *HarnessManager) CreateWorkspaceForSession(ctx context.Context, sessionKey string) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.sandboxHarness == nil {
		return "", fmt.Errorf("sandbox harness not initialized")
	}

	if !m.sandboxHarness.IsExternal() {
		return "", fmt.Errorf("workspace creation only available with External Sandbox mode")
	}

	resp, err := m.sandboxHarness.CreateWorkspace(ctx, sessionKey)
	if err != nil {
		return "", fmt.Errorf("workspace creation failed: %w", err)
	}

	return resp.WorkspaceID, nil
}

// CleanupWorkspaceForSession cleans up a workspace.
func (m *HarnessManager) CleanupWorkspaceForSession(ctx context.Context, workspaceID string) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.sandboxHarness == nil {
		return fmt.Errorf("sandbox harness not initialized")
	}

	return m.sandboxHarness.CleanupWorkspace(ctx, workspaceID)
}

// GetWorkspaceStatusForSession gets workspace status.
func (m *HarnessManager) GetWorkspaceStatusForSession(ctx context.Context, workspaceID string) (*WorkspaceStatusResponse, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.sandboxHarness == nil {
		return nil, fmt.Errorf("sandbox harness not initialized")
	}

	return m.sandboxHarness.GetWorkspaceStatus(ctx, workspaceID)
}

// KnowledgeHarness manages knowledge base/RAG integration.
type KnowledgeHarness struct {
	spec    *v1.KnowledgeHarnessSpec
	backend KnowledgeBackend
}

// KnowledgeBackend interface for knowledge storage.
type KnowledgeBackend interface {
	Query(ctx context.Context, query string, limit int) ([]KnowledgeResult, error)
	Store(ctx context.Context, doc KnowledgeDocument) error
	Delete(ctx context.Context, id string) error
	Close() error
}

// KnowledgeResult represents a knowledge query result.
type KnowledgeResult struct {
	ID       string
	Content  string
	Score    float64
	Metadata map[string]string
}

// KnowledgeDocument represents a document to store.
type KnowledgeDocument struct {
	ID       string
	Content  string
	Metadata map[string]string
}

// NewKnowledgeHarness creates a new knowledge harness.
func NewKnowledgeHarness(spec *v1.KnowledgeHarnessSpec) *KnowledgeHarness {
	return &KnowledgeHarness{
		spec:    spec,
		backend: NewInMemoryKnowledgeBackend(),
	}
}

// Query queries the knowledge base.
func (h *KnowledgeHarness) Query(ctx context.Context, query string, limit int) ([]KnowledgeResult, error) {
	return h.backend.Query(ctx, query, limit)
}

// Store stores a document in the knowledge base.
func (h *KnowledgeHarness) Store(ctx context.Context, doc KnowledgeDocument) error {
	return h.backend.Store(ctx, doc)
}

// InMemoryKnowledgeBackend provides in-memory knowledge storage.
type InMemoryKnowledgeBackend struct {
	docs map[string]KnowledgeDocument
}

// NewInMemoryKnowledgeBackend creates an in-memory backend.
func NewInMemoryKnowledgeBackend() *InMemoryKnowledgeBackend {
	return &InMemoryKnowledgeBackend{
		docs: make(map[string]KnowledgeDocument),
	}
}

func (b *InMemoryKnowledgeBackend) Query(ctx context.Context, query string, limit int) ([]KnowledgeResult, error) {
	// Simple text matching for in-memory backend
	results := []KnowledgeResult{}
	for _, doc := range b.docs {
		if len(results) >= limit {
			break
		}
		// Simple substring match
		if contains(doc.Content, query) {
			results = append(results, KnowledgeResult{
				ID:      doc.ID,
				Content: doc.Content,
				Score:   1.0,
			})
		}
	}
	return results, nil
}

func (b *InMemoryKnowledgeBackend) Store(ctx context.Context, doc KnowledgeDocument) error {
	b.docs[doc.ID] = doc
	return nil
}

func (b *InMemoryKnowledgeBackend) Delete(ctx context.Context, id string) error {
	delete(b.docs, id)
	return nil
}

func (b *InMemoryKnowledgeBackend) Close() error {
	return nil
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && containsSubstring(s, substr))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// GuardrailRule represents a guardrail rule.
type GuardrailRule struct {
	Name   string
	Type   string
	Config map[string]any
}

// SecurityPolicy represents a security policy.
type SecurityPolicy struct {
	Name   string
	Type   string
	Config map[string]any
}

// PolicyInfo represents a policy info.
type PolicyInfo struct {
	Name   string
	Type   string
	Config map[string]any
}

// GuardrailHarness is a stub implementation for guardrail harness.
type GuardrailHarness struct{}

func (h *GuardrailHarness) GetRules() []GuardrailRule { return nil }
func (h *GuardrailHarness) ToFrameworkConfig(frameworkType string) ([]byte, error) { return nil, nil }

// SecurityHarness is a stub implementation for security harness.
type SecurityHarness struct{}

func (h *SecurityHarness) GetPolicies() []SecurityPolicy { return nil }
func (h *SecurityHarness) ToFrameworkConfig(frameworkType string) ([]byte, error) { return nil, nil }

// PolicyHarness is a stub implementation for policy harness.
type PolicyHarness struct{}

func (h *PolicyHarness) GetPolicies() []PolicyInfo { return nil }
func (h *PolicyHarness) ToFrameworkConfig(frameworkType string) ([]byte, error) { return nil, nil }