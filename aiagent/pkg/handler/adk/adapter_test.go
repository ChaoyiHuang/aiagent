package adk

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"aiagent/api/v1"
	"aiagent/pkg/agent"
	"aiagent/pkg/handler"
)

// createTempHandler creates a handler with temporary directories for testing.
func createTempHandler(t *testing.T) (*ADKHandler, string) {
	tmpDir := t.TempDir()
	cfg := &handler.HandlerConfig{
		Type:             handler.HandlerTypeADK,
		FrameworkVersion: "1.0.0",
		WorkDir:          tmpDir,
		ConfigDir:        filepath.Join(tmpDir, "config"),
		FrameworkBin:     filepath.Join(tmpDir, "adk-framework"),
		ProcessMode:      handler.ProcessModeIsolated, // Default to isolated for tests
	}

	// Create config directory
	os.MkdirAll(cfg.ConfigDir, 0755)

	// Create dummy framework binary
	os.WriteFile(cfg.FrameworkBin, []byte("#!/bin/sh\necho 'test'"), 0755)

	return NewADKHandler(cfg), tmpDir
}

// createTempHandlerWithMode creates a handler with specific process mode.
func createTempHandlerWithMode(t *testing.T, mode handler.ProcessModeType) (*ADKHandler, string) {
	tmpDir := t.TempDir()
	cfg := &handler.HandlerConfig{
		Type:             handler.HandlerTypeADK,
		FrameworkVersion: "1.0.0",
		WorkDir:          tmpDir,
		ConfigDir:        filepath.Join(tmpDir, "config"),
		FrameworkBin:     filepath.Join(tmpDir, "adk-framework"),
		ProcessMode:      mode,
	}

	// Create config directory
	os.MkdirAll(cfg.ConfigDir, 0755)

	// Create dummy framework binary
	os.WriteFile(cfg.FrameworkBin, []byte("#!/bin/sh\necho 'test'"), 0755)

	return NewADKHandler(cfg), tmpDir
}

func TestNewADKHandler(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &handler.HandlerConfig{
		Type:             handler.HandlerTypeADK,
		FrameworkVersion: "1.0.0",
		WorkDir:          tmpDir,
		ConfigDir:        filepath.Join(tmpDir, "config"),
		FrameworkBin:     filepath.Join(tmpDir, "adk-framework"),
	}

	h := NewADKHandler(cfg)
	if h == nil {
		t.Error("expected handler to be created")
	}

	if h.Type() != handler.HandlerTypeADK {
		t.Errorf("expected type 'adk', got '%s'", h.Type())
	}

	if !h.SupportsMultiAgent() {
		t.Error("expected ADK to support multi-agent")
	}

	if !h.SupportsMultiInstance() {
		t.Error("expected ADK to support multi-instance (isolated mode)")
	}
}

func TestADKHandler_GetFrameworkInfo(t *testing.T) {
	h, _ := createTempHandler(t)
	info := h.GetFrameworkInfo()

	if info.Name != "ADK-Go" {
		t.Errorf("expected name 'ADK-Go', got '%s'", info.Name)
	}

	if info.Type != handler.HandlerTypeADK {
		t.Errorf("expected type 'adk', got '%s'", info.Type)
	}

	if len(info.Capabilities) == 0 {
		t.Error("expected capabilities to be defined")
	}

	if info.ConfigFormat != "yaml" {
		t.Errorf("expected config format 'yaml', got '%s'", info.ConfigFormat)
	}
}

func TestADKHandler_LoadAgent(t *testing.T) {
	h, _ := createTempHandler(t)
	ctx := context.Background()

	spec := &v1.AIAgentSpec{
		Description: "test-adk-agent",
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
		return
	}

	if ag.Name() != "test-adk-agent" {
		t.Errorf("expected name 'test-adk-agent', got '%s'", ag.Name())
	}

	if ag.Type() != agent.AgentTypeLLM {
		t.Errorf("expected type 'llm', got '%s'", ag.Type())
	}
}

func TestADKHandler_StopAgent(t *testing.T) {
	h, _ := createTempHandler(t)
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

func TestADKHandler_GetAgentStatus(t *testing.T) {
	h, _ := createTempHandler(t)
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

func TestADKHandler_GetAgentStatusNotFound(t *testing.T) {
	h, _ := createTempHandler(t)
	ctx := context.Background()

	status, err := h.GetAgentStatus(ctx, "nonexistent-agent")
	if err == nil {
		t.Error("expected error for nonexistent agent")
	}
	if status != nil {
		t.Error("expected nil status for nonexistent agent")
	}
}

func TestADKHandler_GenerateFrameworkConfig(t *testing.T) {
	h, _ := createTempHandler(t)

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

func TestADKHandler_GenerateAgentConfig(t *testing.T) {
	h, _ := createTempHandler(t)

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

func TestADKHandler_GenerateHarnessConfig(t *testing.T) {
	h, _ := createTempHandler(t)

	harnessCfg := &handler.HarnessConfig{
		Model: &v1.ModelHarnessSpec{
			Provider:     "deepseek",
			DefaultModel: "deepseek-chat",
		},
		Memory: &v1.MemoryHarnessSpec{
			Type:     "redis",
			Endpoint: "redis://localhost:6379",
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

func TestADKHandler_HarnessManager(t *testing.T) {
	h, _ := createTempHandler(t)

	// GetHarnessManager should return nil initially
	if h.GetHarnessManager() != nil {
		t.Error("expected nil harness manager initially")
	}
}

func TestADKHandler_ListAgents(t *testing.T) {
	h, _ := createTempHandler(t)
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

func TestADKHandler_FrameworkInstances(t *testing.T) {
	h, _ := createTempHandler(t)
	ctx := context.Background()

	// List instances (should be empty since no processes started)
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

func TestADKHandler_FrameworkNotRunning(t *testing.T) {
	h, _ := createTempHandler(t)
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

func TestConfigConverter_ConvertToADKConfig(t *testing.T) {
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

	data, err := converter.ConvertToADKConfig(spec, harnessCfg)
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
			Models: []v1.ModelConfig{
				{Name: "deepseek-chat", Allowed: true},
				{Name: "deepseek-coder", Allowed: true},
			},
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

	if agentCfg.Name != "test-agent" {
		t.Errorf("expected name 'test-agent', got '%s'", agentCfg.Name)
	}

	if agentCfg.Model != "deepseek-chat" {
		t.Errorf("expected model 'deepseek-chat', got '%s'", agentCfg.Model)
	}

	if len(agentCfg.AllowedModels) != 2 {
		t.Errorf("expected 2 allowed models, got %d", len(agentCfg.AllowedModels))
	}

	if len(agentCfg.Tools) != 1 {
		t.Errorf("expected 1 allowed tool, got %d", len(agentCfg.Tools))
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

	mockModelHarness := &mockModelHarness{
		provider:      "deepseek",
		defaultModel:  "deepseek-chat",
		allowedModels: []string{"deepseek-chat", "deepseek-coder"},
		endpoint:      "https://api.deepseek.com",
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

func TestADKAgentWrapper(t *testing.T) {
	h, tmpDir := createTempHandler(t)
	configPath := filepath.Join(tmpDir, "config", "wrapper.yaml")
	wrapper := NewADKAgentWrapper("wrapper-agent", configPath, h)

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

// Mock implementations for harness interfaces

type mockModelHarness struct {
	provider      string
	defaultModel  string
	allowedModels []string
	endpoint      string
	apiKeyRef     string
}

func (m *mockModelHarness) GetProvider() string        { return m.provider }
func (m *mockModelHarness) GetDefaultModel() string    { return m.defaultModel }
func (m *mockModelHarness) GetAllowedModels() []string { return m.allowedModels }
func (m *mockModelHarness) GetEndpoint() string        { return m.endpoint }
func (m *mockModelHarness) GetAPIKeyRef() string       { return m.apiKeyRef }
func (m *mockModelHarness) ToFrameworkConfig(t handler.HandlerType) ([]byte, error) { return nil, nil }

type mockMemoryHarness struct {
	typ         string
	endpoint    string
	ttl         int64
	persistence bool
}

func (m *mockMemoryHarness) GetType() string            { return m.typ }
func (m *mockMemoryHarness) GetEndpoint() string        { return m.endpoint }
func (m *mockMemoryHarness) GetTTL() int64              { return m.ttl }
func (m *mockMemoryHarness) IsPersistenceEnabled() bool { return m.persistence }
func (m *mockMemoryHarness) ToFrameworkConfig(t handler.HandlerType) ([]byte, error) { return nil, nil }

type mockSandboxHarness struct {
	mode     handler.SandboxMode
	endpoint string
	timeout  int64
	limits   *handler.ResourceLimits
}

func (m *mockSandboxHarness) GetMode() handler.SandboxMode                 { return m.mode }
func (m *mockSandboxHarness) IsExternal() bool                              { return m.mode == handler.SandboxModeExternal }
func (m *mockSandboxHarness) GetEndpoint() string                           { return m.endpoint }
func (m *mockSandboxHarness) GetTimeout() int64                             { return m.timeout }
func (m *mockSandboxHarness) GetResourceLimits() *handler.ResourceLimits    { return m.limits }
func (m *mockSandboxHarness) ToFrameworkConfig(t handler.HandlerType) ([]byte, error) { return nil, nil }

type mockSkillsHarness struct {
	hubType  string
	endpoint string
	skills   []handler.SkillInfo
}

func (m *mockSkillsHarness) GetHubType() string             { return m.hubType }
func (m *mockSkillsHarness) GetEndpoint() string            { return m.endpoint }
func (m *mockSkillsHarness) GetSkills() []handler.SkillInfo { return m.skills }
func (m *mockSkillsHarness) ToFrameworkConfig(t handler.HandlerType) ([]byte, error) { return nil, nil }

type mockMCPHarness struct {
	registryType string
	endpoint     string
	servers      []handler.MCPServerInfo
}

func (m *mockMCPHarness) GetRegistryType() string              { return m.registryType }
func (m *mockMCPHarness) GetEndpoint() string                  { return m.endpoint }
func (m *mockMCPHarness) GetServers() []handler.MCPServerInfo { return m.servers }
func (m *mockMCPHarness) ToFrameworkConfig(t handler.HandlerType) ([]byte, error) { return nil, nil }

// ============================================================
// ProcessMode Tests
// ============================================================

func TestADKHandler_ProcessModeIsolated(t *testing.T) {
	h, _ := createTempHandlerWithMode(t, handler.ProcessModeIsolated)

	// Verify SupportsMultiInstance is true in isolated mode
	if !h.SupportsMultiInstance() {
		t.Error("expected SupportsMultiInstance to be true in isolated mode")
	}

	// Verify SupportsMultiAgent is always true
	if !h.SupportsMultiAgent() {
		t.Error("expected SupportsMultiAgent to be true")
	}
}

func TestADKHandler_ProcessModeShared(t *testing.T) {
	h, _ := createTempHandlerWithMode(t, handler.ProcessModeShared)

	// Verify SupportsMultiInstance is false in shared mode
	if h.SupportsMultiInstance() {
		t.Error("expected SupportsMultiInstance to be false in shared mode")
	}

	// Verify SupportsMultiAgent is true
	if !h.SupportsMultiAgent() {
		t.Error("expected SupportsMultiAgent to be true")
	}
}

func TestADKHandler_ProcessModeDefault(t *testing.T) {
	// Test default mode when not specified
	tmpDir := t.TempDir()
	cfg := &handler.HandlerConfig{
		Type:             handler.HandlerTypeADK,
		FrameworkVersion: "1.0.0",
		WorkDir:          tmpDir,
		ConfigDir:        filepath.Join(tmpDir, "config"),
		FrameworkBin:     filepath.Join(tmpDir, "adk-framework"),
		// ProcessMode not set - should default to isolated
	}

	os.MkdirAll(cfg.ConfigDir, 0755)
	os.WriteFile(cfg.FrameworkBin, []byte("#!/bin/sh\necho 'test'"), 0755)

	h := NewADKHandler(cfg)

	// Default should be isolated (verified by SupportsMultiInstance)
	if !h.SupportsMultiInstance() {
		t.Error("expected default mode to be isolated (SupportsMultiInstance=true)")
	}
}

func TestADKHandler_IsolatedMode_FrameworkInstance(t *testing.T) {
	h, tmpDir := createTempHandlerWithMode(t, handler.ProcessModeIsolated)
	ctx := context.Background()

	// Prepare work directory
	if err := h.PrepareWorkDirectory(ctx, tmpDir); err != nil {
		t.Fatalf("failed to prepare work directory: %v", err)
	}

	// Load agent
	spec := &v1.AIAgentSpec{Description: "isolated-test-agent"}
	ag, err := h.LoadAgent(ctx, spec, nil)
	if err != nil {
		t.Fatalf("failed to load agent: %v", err)
	}

	// Verify agent is created
	if ag == nil {
		t.Fatal("expected agent to be created")
	}

	// Verify agent is registered
	agents, err := h.ListAgents(ctx)
	if err != nil {
		t.Errorf("unexpected error listing agents: %v", err)
	}
	if len(agents) != 1 {
		t.Errorf("expected 1 agent, got %d", len(agents))
	}
}

func TestADKHandler_SharedMode_SingleFramework(t *testing.T) {
	h, tmpDir := createTempHandlerWithMode(t, handler.ProcessModeShared)
	ctx := context.Background()

	// Prepare work directory
	if err := h.PrepareWorkDirectory(ctx, tmpDir); err != nil {
		t.Fatalf("failed to prepare work directory: %v", err)
	}

	// Load multiple agents
	h.LoadAgent(ctx, &v1.AIAgentSpec{Description: "shared-agent-1"}, nil)
	h.LoadAgent(ctx, &v1.AIAgentSpec{Description: "shared-agent-2"}, nil)

	// Verify multiple agents registered
	agents, err := h.ListAgents(ctx)
	if err != nil {
		t.Errorf("unexpected error listing agents: %v", err)
	}
	if len(agents) != 2 {
		t.Errorf("expected 2 agents in shared mode, got %d", len(agents))
	}

	// In shared mode, Framework instances should return empty when not started
	instances := h.ListFrameworkInstances(ctx)
	if len(instances) != 0 {
		t.Errorf("expected 0 instances (not started), got %d", len(instances))
	}
}