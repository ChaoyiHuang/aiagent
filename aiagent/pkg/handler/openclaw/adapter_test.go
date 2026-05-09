package openclaw

import (
	"context"
	"testing"

	"aiagent/api/v1"
	"aiagent/pkg/agent"
	"aiagent/pkg/handler"
)

func TestNewOpenClawHandler(t *testing.T) {
	cfg := &handler.HandlerConfig{
		Type:             handler.HandlerTypeOpenClaw,
		FrameworkVersion: "2026.3.8",
		WorkDir:          "/shared/workdir",
	}

	h := NewOpenClawHandler(cfg)
	if h == nil {
		t.Error("expected handler to be created")
	}

	if h.Type() != handler.HandlerTypeOpenClaw {
		t.Errorf("expected type 'openclaw', got '%s'", h.Type())
	}

	if !h.SupportsMultiAgent() {
		t.Error("expected OpenClaw to support multi-agent")
	}

	if !h.SupportsMultiInstance() {
		t.Error("expected OpenClaw to support multi-instance (multiple Gateway processes)")
	}
}

func TestOpenClawHandler_GetFrameworkInfo(t *testing.T) {
	cfg := &handler.HandlerConfig{
		Type:             handler.HandlerTypeOpenClaw,
		FrameworkVersion: "2026.3.8",
	}

	h := NewOpenClawHandler(cfg)
	info := h.GetFrameworkInfo()

	if info.Name != "OpenClaw" {
		t.Errorf("expected name 'OpenClaw', got '%s'", info.Name)
	}

	if info.Type != handler.HandlerTypeOpenClaw {
		t.Errorf("expected type 'openclaw', got '%s'", info.Type)
	}

	if len(info.Capabilities) == 0 {
		t.Error("expected capabilities to be defined")
	}

	if info.ConfigFormat != "json" {
		t.Errorf("expected config format 'json', got '%s'", info.ConfigFormat)
	}
}

func TestOpenClawHandler_LoadAgent(t *testing.T) {
	cfg := &handler.HandlerConfig{
		Type:             handler.HandlerTypeOpenClaw,
		FrameworkVersion: "2026.3.8",
		WorkDir:          "/shared/workdir",
	}

	h := NewOpenClawHandler(cfg)
	ctx := context.Background()

	spec := &v1.AIAgentSpec{
		Description: "test-openclaw-agent",
	}

	harnessCfg := &handler.HarnessConfig{
		Model: &v1.ModelHarnessSpec{
			Provider:     "deepseek",
			DefaultModel: "deepseek-chat",
		},
	}

	ag, err := h.LoadAgent(ctx, spec, harnessCfg)
	if err != nil {
		t.Errorf("unexpected error loading agent: %v", err)
	}

	if ag == nil {
		t.Error("expected agent to be returned")
	}

	if ag.Name() != "test-openclaw-agent" {
		t.Errorf("expected name 'test-openclaw-agent', got '%s'", ag.Name())
	}

	if ag.Type() != agent.AgentTypeLLM {
		t.Errorf("expected type 'llm', got '%s'", ag.Type())
	}
}

func TestOpenClawHandler_StopAgent(t *testing.T) {
	cfg := &handler.HandlerConfig{
		Type:             handler.HandlerTypeOpenClaw,
		FrameworkVersion: "2026.3.8",
		WorkDir:          "/shared/workdir",
	}

	h := NewOpenClawHandler(cfg)
	ctx := context.Background()

	// Load agent first
	spec := &v1.AIAgentSpec{Description: "stop-test-agent"}
	h.LoadAgent(ctx, spec, nil)

	// Stop agent
	err := h.StopAgent(ctx, "stop-test-agent")
	if err != nil {
		t.Errorf("unexpected error stopping agent: %v", err)
	}
}

func TestOpenClawHandler_GetAgentStatus(t *testing.T) {
	cfg := &handler.HandlerConfig{
		Type:             handler.HandlerTypeOpenClaw,
		FrameworkVersion: "2026.3.8",
		WorkDir:          "/shared/workdir",
	}

	h := NewOpenClawHandler(cfg)
	ctx := context.Background()

	// Load agent
	spec := &v1.AIAgentSpec{Description: "status-test-agent"}
	h.LoadAgent(ctx, spec, nil)

	// Get status
	status, err := h.GetAgentStatus(ctx, "status-test-agent")
	if err != nil {
		t.Errorf("unexpected error getting status: %v", err)
	}

	if status == nil {
		t.Error("expected status to be returned")
	}

	if status.Name != "status-test-agent" {
		t.Errorf("expected name 'status-test-agent', got '%s'", status.Name)
	}
}

func TestOpenClawHandler_GetAgentStatusNotFound(t *testing.T) {
	cfg := &handler.HandlerConfig{
		Type:             handler.HandlerTypeOpenClaw,
		FrameworkVersion: "2026.3.8",
		WorkDir:          "/shared/workdir",
	}

	h := NewOpenClawHandler(cfg)
	ctx := context.Background()

	status, err := h.GetAgentStatus(ctx, "nonexistent-agent")
	if err == nil {
		t.Error("expected error for nonexistent agent")
	}
	if status != nil {
		t.Error("expected nil status for nonexistent agent")
	}
}

func TestOpenClawHandler_GenerateFrameworkConfig(t *testing.T) {
	cfg := &handler.HandlerConfig{
		Type:             handler.HandlerTypeOpenClaw,
		FrameworkVersion: "2026.3.8",
		WorkDir:          "/shared/workdir",
	}

	h := NewOpenClawHandler(cfg)

	spec := &v1.AIAgentSpec{
		Description: "config-test-agent",
	}

	harnessCfg := &handler.HarnessConfig{
		Model: &v1.ModelHarnessSpec{
			Provider:     "deepseek",
			DefaultModel: "deepseek-chat",
		},
	}

	data, err := h.GenerateFrameworkConfig(spec, harnessCfg)
	if err != nil {
		t.Errorf("unexpected error generating config: %v", err)
	}

	if len(data) == 0 {
		t.Error("expected config data to be non-empty")
	}
}

func TestOpenClawHandler_GenerateAgentConfig(t *testing.T) {
	cfg := &handler.HandlerConfig{
		Type:             handler.HandlerTypeOpenClaw,
		FrameworkVersion: "2026.3.8",
		WorkDir:          "/shared/workdir",
	}

	h := NewOpenClawHandler(cfg)

	spec := &v1.AIAgentSpec{
		Description: "agent-config-test",
	}

	harnessCfg := &handler.HarnessConfig{
		Model: &v1.ModelHarnessSpec{
			Provider:     "deepseek",
			DefaultModel: "deepseek-chat",
		},
		Skills: &v1.SkillsHarnessSpec{
			HubType: "builtin",
			Skills: []v1.SkillConfig{
				{Name: "weather", Allowed: true},
			},
		},
	}

	data, err := h.GenerateAgentConfig(spec, harnessCfg)
	if err != nil {
		t.Errorf("unexpected error generating agent config: %v", err)
	}

	if len(data) == 0 {
		t.Error("expected agent config data to be non-empty")
	}
}

func TestOpenClawHandler_GenerateHarnessConfig(t *testing.T) {
	cfg := &handler.HandlerConfig{
		Type:             handler.HandlerTypeOpenClaw,
		FrameworkVersion: "2026.3.8",
		WorkDir:          "/shared/workdir",
	}

	h := NewOpenClawHandler(cfg)

	harnessCfg := &handler.HarnessConfig{
		Model: &v1.ModelHarnessSpec{
			Provider:     "deepseek",
			DefaultModel: "deepseek-chat",
		},
		Skills: &v1.SkillsHarnessSpec{
			HubType: "builtin",
			Skills: []v1.SkillConfig{
				{Name: "weather", Allowed: true},
			},
		},
	}

	data, err := h.GenerateHarnessConfig(harnessCfg)
	if err != nil {
		t.Errorf("unexpected error generating harness config: %v", err)
	}

	if len(data) == 0 {
		t.Error("expected harness config data to be non-empty")
	}
}

func TestOpenClawHandler_HarnessManager(t *testing.T) {
	cfg := &handler.HandlerConfig{
		Type:             handler.HandlerTypeOpenClaw,
		FrameworkVersion: "2026.3.8",
		WorkDir:          "/shared/workdir",
	}

	h := NewOpenClawHandler(cfg)

	// GetHarnessManager should return nil initially
	if h.GetHarnessManager() != nil {
		t.Error("expected nil harness manager initially")
	}
}

func TestOpenClawHandler_ListAgents(t *testing.T) {
	cfg := &handler.HandlerConfig{
		Type:             handler.HandlerTypeOpenClaw,
		FrameworkVersion: "2026.3.8",
		WorkDir:          "/shared/workdir",
	}

	h := NewOpenClawHandler(cfg)
	ctx := context.Background()

	// Load multiple agents
	h.LoadAgent(ctx, &v1.AIAgentSpec{Description: "agent-1"}, nil)
	h.LoadAgent(ctx, &v1.AIAgentSpec{Description: "agent-2"}, nil)

	agents, err := h.ListAgents(ctx)
	if err != nil {
		t.Errorf("unexpected error listing agents: %v", err)
	}

	if len(agents) != 2 {
		t.Errorf("expected 2 agents, got %d", len(agents))
	}
}

func TestConfigConverter_ConvertToOpenClawConfig(t *testing.T) {
	converter := NewConfigConverter()

	spec := &v1.AIAgentSpec{
		Description: "converter-agent",
	}

	harnessCfg := &handler.HarnessConfig{
		Model: &v1.ModelHarnessSpec{
			Provider:     "deepseek",
			DefaultModel: "deepseek-chat",
		},
	}

	data, err := converter.ConvertToOpenClawConfig(spec, harnessCfg)
	if err != nil {
		t.Errorf("unexpected error converting config: %v", err)
	}

	if len(data) == 0 {
		t.Error("expected converted data to be non-empty")
	}
}

func TestConfigConverter_ConvertAgentSpec(t *testing.T) {
	converter := NewConfigConverter()

	spec := &v1.AIAgentSpec{
		Description: "test-agent",
	}

	harnessCfg := &handler.HarnessConfig{
		Model: &v1.ModelHarnessSpec{
			Provider:     "deepseek",
			DefaultModel: "deepseek-chat",
		},
		Skills: &v1.SkillsHarnessSpec{
			HubType: "builtin",
			Skills: []v1.SkillConfig{
				{Name: "weather", Allowed: true},
				{Name: "calculator", Allowed: false},
			},
		},
	}

	agentCfg, err := converter.ConvertAgentSpec(spec, harnessCfg)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if agentCfg.ID != "test-agent" {
		t.Errorf("expected ID 'test-agent', got '%s'", agentCfg.ID)
	}

	if agentCfg.Model == nil || agentCfg.Model.Primary != "deepseek-chat" {
		t.Error("expected model to be set")
	}

	if len(agentCfg.Skills) != 1 {
		t.Errorf("expected 1 allowed skill, got %d", len(agentCfg.Skills))
	}
}

func TestConfigConverter_ConvertHarnessConfig(t *testing.T) {
	converter := NewConfigConverter()

	harnessCfg := &handler.HarnessConfig{
		Model: &v1.ModelHarnessSpec{
			Provider:     "deepseek",
			DefaultModel: "deepseek-chat",
		},
		Memory: &v1.MemoryHarnessSpec{
			Type:     "redis",
			Endpoint: "redis://localhost:6379",
			TTL:      3600,
		},
		Sandbox: &v1.SandboxHarnessSpec{
			Mode:     v1.SandboxModeExternal,
			Endpoint: "http://sandbox:9000",
		},
	}

	data, err := converter.ConvertHarnessConfig(harnessCfg)
	if err != nil {
		t.Errorf("unexpected error converting harness: %v", err)
	}

	if len(data) == 0 {
		t.Error("expected converted data to be non-empty")
	}
}

func TestConfigConverter_ConvertModelHarness(t *testing.T) {
	converter := NewConfigConverter()

	// Create mock model harness
	mockModelHarness := &mockModelHarness{
		provider:      "deepseek",
		defaultModel:  "deepseek-chat",
		allowedModels: []string{"deepseek-chat", "deepseek-coder"},
		endpoint:      "https://api.deepseek.com",
		apiKeyRef:     "secret-ref",
	}

	data, err := converter.ConvertModelHarness(mockModelHarness)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if len(data) == 0 {
		t.Error("expected data to be non-empty")
	}
}

func TestConfigConverter_ConvertMemoryHarness(t *testing.T) {
	converter := NewConfigConverter()

	mockMemoryHarness := &mockMemoryHarness{
		typ:         "redis",
		endpoint:    "redis://localhost:6379",
		ttl:         3600,
		persistence: true,
	}

	data, err := converter.ConvertMemoryHarness(mockMemoryHarness)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if len(data) == 0 {
		t.Error("expected data to be non-empty")
	}
}

func TestConfigConverter_ConvertSandboxHarness(t *testing.T) {
	converter := NewConfigConverter()

	mockSandboxHarness := &mockSandboxHarness{
		mode:     handler.SandboxModeExternal,
		endpoint: "http://sandbox:9000",
		timeout:  60,
	}

	data, err := converter.ConvertSandboxHarness(mockSandboxHarness)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if len(data) == 0 {
		t.Error("expected data to be non-empty")
	}
}

func TestConfigConverter_ConvertSkillsHarness(t *testing.T) {
	converter := NewConfigConverter()

	mockSkillsHarness := &mockSkillsHarness{
		hubType:  "builtin",
		endpoint: "",
		skills: []handler.SkillInfo{
			{Name: "weather", Version: "1.0", Allowed: true},
			{Name: "calculator", Version: "1.0", Allowed: true},
		},
	}

	data, err := converter.ConvertSkillsHarness(mockSkillsHarness)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if len(data) == 0 {
		t.Error("expected data to be non-empty")
	}
}

func TestOpenClawAgentWrapper(t *testing.T) {
	cfg := &handler.HandlerConfig{
		Type:             handler.HandlerTypeOpenClaw,
		FrameworkVersion: "2026.3.8",
		WorkDir:          "/shared/workdir",
	}

	h := NewOpenClawHandler(cfg)
	configPath := "/shared/config/wrapper.json"
	wrapper := NewOpenClawAgentWrapper("wrapper-agent", configPath, h)

	if wrapper.Name() != "wrapper-agent" {
		t.Errorf("expected name 'wrapper-agent', got '%s'", wrapper.Name())
	}

	if wrapper.Type() != agent.AgentTypeLLM {
		t.Errorf("expected type 'llm', got '%s'", wrapper.Type())
	}

	if wrapper.AgentID() != "wrapper-agent" {
		t.Errorf("expected agent ID 'wrapper-agent', got '%s'", wrapper.AgentID())
	}

	if wrapper.ConfigPath() != configPath {
		t.Errorf("expected config path '%s', got '%s'", configPath, wrapper.ConfigPath())
	}
}

func TestOpenClawHandler_FrameworkInstances(t *testing.T) {
	cfg := &handler.HandlerConfig{
		Type:             handler.HandlerTypeOpenClaw,
		FrameworkVersion: "2026.3.8",
		WorkDir:          "/shared/workdir",
	}

	h := NewOpenClawHandler(cfg)
	ctx := context.Background()

	// List instances (should be empty since gateway not started)
	instances := h.ListFrameworkInstances(ctx)
	if len(instances) != 0 {
		t.Errorf("expected 0 instances, got %d", len(instances))
	}

	// Get non-existent instance
	instance := h.GetFrameworkInstance("nonexistent")
	if instance != nil {
		t.Error("expected nil for non-existent instance")
	}
}

func TestOpenClawHandler_FrameworkNotRunning(t *testing.T) {
	cfg := &handler.HandlerConfig{
		Type:             handler.HandlerTypeOpenClaw,
		FrameworkVersion: "2026.3.8",
		WorkDir:          "/shared/workdir",
	}

	h := NewOpenClawHandler(cfg)
	ctx := context.Background()

	// IsFrameworkRunning should return false initially
	if h.IsFrameworkRunning(ctx) {
		t.Error("expected framework to not be running initially")
	}

	// GetFrameworkStatus should work even when not running
	status, err := h.GetFrameworkStatus(ctx)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if status.Running {
		t.Error("expected status Running to be false")
	}
}

// Mock implementations for harness interfaces

type mockModelHarness struct {
	provider      string
	defaultModel  string
	allowedModels []string
	endpoint      string
	apiKeyRef     string
}

func (m *mockModelHarness) GetProvider() string       { return m.provider }
func (m *mockModelHarness) GetDefaultModel() string   { return m.defaultModel }
func (m *mockModelHarness) GetAllowedModels() []string { return m.allowedModels }
func (m *mockModelHarness) GetEndpoint() string       { return m.endpoint }
func (m *mockModelHarness) GetAPIKeyRef() string      { return m.apiKeyRef }
func (m *mockModelHarness) ToFrameworkConfig(t handler.HandlerType) ([]byte, error) { return nil, nil }

type mockMemoryHarness struct {
	typ         string
	endpoint    string
	ttl         int64
	persistence bool
}

func (m *mockMemoryHarness) GetType() string        { return m.typ }
func (m *mockMemoryHarness) GetEndpoint() string    { return m.endpoint }
func (m *mockMemoryHarness) GetTTL() int64          { return m.ttl }
func (m *mockMemoryHarness) IsPersistenceEnabled() bool { return m.persistence }
func (m *mockMemoryHarness) ToFrameworkConfig(t handler.HandlerType) ([]byte, error) { return nil, nil }

type mockSandboxHarness struct {
	mode     handler.SandboxMode
	endpoint string
	timeout  int64
	limits   *handler.ResourceLimits
}

func (m *mockSandboxHarness) GetMode() handler.SandboxMode    { return m.mode }
func (m *mockSandboxHarness) IsExternal() bool                 { return m.mode == handler.SandboxModeExternal }
func (m *mockSandboxHarness) GetEndpoint() string              { return m.endpoint }
func (m *mockSandboxHarness) GetTimeout() int64                { return m.timeout }
func (m *mockSandboxHarness) GetResourceLimits() *handler.ResourceLimits { return m.limits }
func (m *mockSandboxHarness) ToFrameworkConfig(t handler.HandlerType) ([]byte, error) { return nil, nil }

type mockSkillsHarness struct {
	hubType  string
	endpoint string
	skills   []handler.SkillInfo
}

func (m *mockSkillsHarness) GetHubType() string      { return m.hubType }
func (m *mockSkillsHarness) GetEndpoint() string     { return m.endpoint }
func (m *mockSkillsHarness) GetSkills() []handler.SkillInfo { return m.skills }
func (m *mockSkillsHarness) ToFrameworkConfig(t handler.HandlerType) ([]byte, error) { return nil, nil }

type mockMCPHarness struct {
	registryType string
	endpoint     string
	servers      []handler.MCPServerInfo
}

func (m *mockMCPHarness) GetRegistryType() string   { return m.registryType }
func (m *mockMCPHarness) GetEndpoint() string       { return m.endpoint }
func (m *mockMCPHarness) GetServers() []handler.MCPServerInfo { return m.servers }
func (m *mockMCPHarness) ToFrameworkConfig(t handler.HandlerType) ([]byte, error) { return nil, nil }