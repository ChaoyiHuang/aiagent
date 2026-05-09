package runtime

import (
	"context"
	"testing"

	"aiagent/api/v1"
	"aiagent/pkg/agent"
	"aiagent/pkg/handler"
)

// MockHandler implements handler.Handler for testing.
type MockHandler struct {
	handlerType handler.HandlerType
	info       *handler.FrameworkInfo
}

func NewMockHandler(hType handler.HandlerType) *MockHandler {
	return &MockHandler{
		handlerType: hType,
		info: &handler.FrameworkInfo{
			Type:        hType,
			Name:        string(hType) + "-mock",
			Version:     "1.0.0",
			Description: "Mock handler for testing",
		},
	}
}

func (m *MockHandler) Type() handler.HandlerType {
	return m.handlerType
}

func (m *MockHandler) GetFrameworkInfo() *handler.FrameworkInfo {
	return m.info
}

func (m *MockHandler) GenerateFrameworkConfig(spec *v1.AIAgentSpec, harness *handler.HarnessConfig) ([]byte, error) {
	return nil, nil
}

func (m *MockHandler) GenerateAgentConfig(spec *v1.AIAgentSpec, harness *handler.HarnessConfig) ([]byte, error) {
	return nil, nil
}

func (m *MockHandler) GenerateHarnessConfig(harness *handler.HarnessConfig) ([]byte, error) {
	return nil, nil
}

func (m *MockHandler) PrepareWorkDirectory(ctx context.Context, workDir string) error {
	return nil
}

func (m *MockHandler) WriteConfigFiles(ctx context.Context, workDir string, configs map[string][]byte) error {
	return nil
}

func (m *MockHandler) StartFramework(ctx context.Context, frameworkBin string, workDir string, configPath string) error {
	return nil
}

func (m *MockHandler) StartFrameworkInstance(ctx context.Context, instanceID string, configPath string) error {
	return nil
}

func (m *MockHandler) StopFramework(ctx context.Context) error {
	return nil
}

func (m *MockHandler) StopFrameworkInstance(ctx context.Context, instanceID string) error {
	return nil
}

func (m *MockHandler) IsFrameworkRunning(ctx context.Context) bool {
	return false
}

func (m *MockHandler) GetFrameworkStatus(ctx context.Context) (*handler.FrameworkStatus, error) {
	return nil, nil
}

func (m *MockHandler) SetHarnessManager(harnessMgr handler.HarnessManagerInterface) error {
	return nil
}

func (m *MockHandler) AdaptModelHarness(harness handler.ModelHarnessInterface) ([]byte, error) {
	return nil, nil
}

func (m *MockHandler) AdaptMCPHarness(harness handler.MCPHarnessInterface) ([]byte, error) {
	return nil, nil
}

func (m *MockHandler) AdaptMemoryHarness(harness handler.MemoryHarnessInterface) ([]byte, error) {
	return nil, nil
}

func (m *MockHandler) AdaptSandboxHarness(harness handler.SandboxHarnessInterface) ([]byte, error) {
	return nil, nil
}

func (m *MockHandler) AdaptSkillsHarness(harness handler.SkillsHarnessInterface) ([]byte, error) {
	return nil, nil
}

func (m *MockHandler) GetHarnessManager() handler.HarnessManagerInterface {
	return nil
}

func (m *MockHandler) LoadAgent(ctx context.Context, spec *v1.AIAgentSpec, harness *handler.HarnessConfig) (agent.Agent, error) {
	return nil, nil
}

func (m *MockHandler) StartAgent(ctx context.Context, ag agent.Agent, config *handler.AgentConfig) error {
	return nil
}

func (m *MockHandler) StopAgent(ctx context.Context, agentID string) error {
	return nil
}

func (m *MockHandler) GetAgentStatus(ctx context.Context, agentID string) (*handler.AgentStatus, error) {
	return nil, nil
}

func (m *MockHandler) ListAgents(ctx context.Context) ([]handler.AgentInfo, error) {
	return nil, nil
}

func (m *MockHandler) SupportsMultiAgent() bool {
	return true
}

func (m *MockHandler) SupportsMultiInstance() bool {
	return false
}

func (m *MockHandler) ListFrameworkInstances(ctx context.Context) []handler.FrameworkInstanceInfo {
	return nil
}

func (m *MockHandler) GetFrameworkInstance(instanceID string) *handler.FrameworkInstanceInfo {
	return nil
}

func TestRegistry_Register(t *testing.T) {
	reg := NewRegistry()
	h := NewMockHandler(handler.HandlerTypeADK)

	err := reg.Register(h)
	if err != nil {
		t.Errorf("unexpected error registering handler: %v", err)
	}

	if !reg.Has(handler.HandlerTypeADK) {
		t.Error("expected registry to have ADK handler")
	}
}

func TestRegistry_RegisterDuplicate(t *testing.T) {
	reg := NewRegistry()
	h1 := NewMockHandler(handler.HandlerTypeADK)
	h2 := NewMockHandler(handler.HandlerTypeADK)

	err := reg.Register(h1)
	if err != nil {
		t.Errorf("unexpected error on first register: %v", err)
	}

	err = reg.Register(h2)
	if err == nil {
		t.Error("expected error on duplicate register")
	}
}

func TestRegistry_Get(t *testing.T) {
	reg := NewRegistry()
	h := NewMockHandler(handler.HandlerTypeOpenClaw)
	reg.Register(h)

	retrieved := reg.Get(handler.HandlerTypeOpenClaw)
	if retrieved == nil {
		t.Error("expected to retrieve registered handler")
	}

	if retrieved.Type() != handler.HandlerTypeOpenClaw {
		t.Errorf("expected type 'openclaw', got '%s'", retrieved.Type())
	}
}

func TestRegistry_GetNotFound(t *testing.T) {
	reg := NewRegistry()

	retrieved := reg.Get(handler.HandlerTypeADK)
	if retrieved != nil {
		t.Error("expected nil for unregistered handler")
	}
}

func TestRegistry_GetByTypeString(t *testing.T) {
	reg := NewRegistry()
	h := NewMockHandler(handler.HandlerTypeLangChain)
	reg.Register(h)

	retrieved := reg.GetByTypeString("langchain")
	if retrieved == nil {
		t.Error("expected to retrieve handler by string type")
	}
}

func TestRegistry_Unregister(t *testing.T) {
	reg := NewRegistry()
	h := NewMockHandler(handler.HandlerTypeHermes)
	reg.Register(h)

	reg.Unregister(handler.HandlerTypeHermes)

	if reg.Has(handler.HandlerTypeHermes) {
		t.Error("expected registry to not have Hermes handler after unregister")
	}
}

func TestRegistry_List(t *testing.T) {
	reg := NewRegistry()
	reg.Register(NewMockHandler(handler.HandlerTypeADK))
	reg.Register(NewMockHandler(handler.HandlerTypeOpenClaw))
	reg.Register(NewMockHandler(handler.HandlerTypeLangChain))

	handlers := reg.List()
	if len(handlers) != 3 {
		t.Errorf("expected 3 handlers, got %d", len(handlers))
	}
}

func TestRegistry_ListTypes(t *testing.T) {
	reg := NewRegistry()
	reg.Register(NewMockHandler(handler.HandlerTypeADK))
	reg.Register(NewMockHandler(handler.HandlerTypeOpenClaw))

	types := reg.ListTypes()
	if len(types) != 2 {
		t.Errorf("expected 2 types, got %d", len(types))
	}
}

func TestRegistry_Count(t *testing.T) {
	reg := NewRegistry()
	reg.Register(NewMockHandler(handler.HandlerTypeADK))
	reg.Register(NewMockHandler(handler.HandlerTypeOpenClaw))

	count := reg.Count()
	if count != 2 {
		t.Errorf("expected count 2, got %d", count)
	}
}

func TestRegistry_Clear(t *testing.T) {
	reg := NewRegistry()
	reg.Register(NewMockHandler(handler.HandlerTypeADK))
	reg.Register(NewMockHandler(handler.HandlerTypeOpenClaw))

	reg.Clear()

	if reg.Count() != 0 {
		t.Errorf("expected count 0 after clear, got %d", reg.Count())
	}
}

func TestDefaultRegistry(t *testing.T) {
	// Test default registry functions
	h := NewMockHandler(handler.HandlerTypeCustom)

	err := RegisterHandler(h)
	if err != nil {
		t.Errorf("unexpected error registering with default registry: %v", err)
	}

	retrieved := GetHandler(handler.HandlerTypeCustom)
	if retrieved == nil {
		t.Error("expected to retrieve from default registry")
	}

	// Clean up
	DefaultRegistry.Unregister(handler.HandlerTypeCustom)
}

func TestHandlerFinder_FindForAgent(t *testing.T) {
	reg := NewRegistry()
	reg.Register(NewMockHandler(handler.HandlerTypeADK))

	finder := NewHandlerFinder(reg)
	ctx := context.Background()

	h, err := finder.FindForAgent(ctx, "adk")
	if err != nil {
		t.Errorf("unexpected error finding handler: %v", err)
	}
	if h == nil {
		t.Error("expected to find handler")
	}

	// Not found
	h, err = finder.FindForAgent(ctx, "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent type")
	}
	if h != nil {
		t.Error("expected nil for nonexistent handler")
	}
}

func TestHandlerFinder_FindBest(t *testing.T) {
	reg := NewRegistry()
	adkHandler := NewMockHandler(handler.HandlerTypeADK)
	reg.Register(adkHandler)

	finder := NewHandlerFinder(reg)
	ctx := context.Background()

	// Find with requirements
	req := HandlerRequirements{
		FrameworkType: handler.HandlerTypeADK,
	}

	h, err := finder.FindBest(ctx, req)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if h == nil {
		t.Error("expected to find matching handler")
	}

	// Require multi-agent
	req = HandlerRequirements{
		RequireMultiAgent: true,
	}
	h, err = finder.FindBest(ctx, req)
	if err != nil {
		t.Errorf("unexpected error with multi-agent requirement: %v", err)
	}
	if h == nil {
		t.Error("expected to find handler with multi-agent support")
	}
}

func TestHandlerRequirements(t *testing.T) {
	req := HandlerRequirements{
		FrameworkType:     handler.HandlerTypeADK,
		RequireMultiAgent: true,
		Capabilities:      []string{"tools", "memory"},
	}

	if req.FrameworkType != handler.HandlerTypeADK {
		t.Errorf("expected framework type 'adk', got '%s'", req.FrameworkType)
	}

	if !req.RequireMultiAgent {
		t.Error("expected RequireMultiAgent to be true")
	}

	if len(req.Capabilities) != 2 {
		t.Errorf("expected 2 capabilities, got %d", len(req.Capabilities))
	}
}