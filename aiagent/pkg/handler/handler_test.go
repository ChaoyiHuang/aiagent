package handler

import (
	"context"
	"testing"

	"aiagent/api/v1"
	"aiagent/pkg/agent"
)

func TestHandlerType(t *testing.T) {
	tests := []struct {
		name     string
		hType    HandlerType
		expected string
	}{
		{"adk", HandlerTypeADK, "adk"},
		{"openclaw", HandlerTypeOpenClaw, "openclaw"},
		{"langchain", HandlerTypeLangChain, "langchain"},
		{"hermes", HandlerTypeHermes, "hermes"},
		{"custom", HandlerTypeCustom, "custom"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if string(tc.hType) != tc.expected {
				t.Errorf("expected '%s', got '%s'", tc.expected, tc.hType)
			}
		})
	}
}

func TestBaseHandler(t *testing.T) {
	cfg := &HandlerConfig{
		Type: HandlerTypeADK,
	}

	info := &FrameworkInfo{
		Type:        HandlerTypeADK,
		Name:        "ADK-Go",
		Version:     "1.0",
		Description: "Test Framework",
	}

	h := NewBaseHandler(cfg, info)

	ctx := context.Background()

	// Test Type()
	if h.Type() != HandlerTypeADK {
		t.Errorf("expected HandlerTypeADK, got %s", h.Type())
	}

	// Test GetFrameworkInfo()
	fInfo := h.GetFrameworkInfo()
	if fInfo.Name != "ADK-Go" {
		t.Errorf("expected 'ADK-Go', got '%s'", fInfo.Name)
	}

	// Test RegisterAgent and ListAgents
	agentInfo := &AgentInfo{
		ID:    "test-agent",
		Name:  "Test Agent",
		Type:  agent.AgentTypeLLM,
		Phase: v1.AgentPhasePending,
	}
	h.RegisterAgent(agentInfo)

	agents, err := h.ListAgents(ctx)
	if err != nil {
		t.Errorf("ListAgents failed: %v", err)
	}
	if len(agents) != 1 {
		t.Errorf("expected 1 agent, got %d", len(agents))
	}

	// Test GetAgent
	retrieved := h.GetAgent("test-agent")
	if retrieved == nil {
		t.Error("expected to retrieve agent")
	}
	if retrieved.Name != "Test Agent" {
		t.Errorf("expected 'Test Agent', got '%s'", retrieved.Name)
	}

	// Test UnregisterAgent
	h.UnregisterAgent("test-agent")
	agents, err = h.ListAgents(ctx)
	if err != nil {
		t.Errorf("ListAgents failed: %v", err)
	}
	if len(agents) != 0 {
		t.Errorf("expected 0 agents after unregister, got %d", len(agents))
	}

	// Test GetConfig
	config := h.GetConfig()
	if config.Type != HandlerTypeADK {
		t.Errorf("expected type 'adk', got '%s'", config.Type)
	}
}

func TestHarnessConfig(t *testing.T) {
	cfg := &HarnessConfig{
		Model: &v1.ModelHarnessSpec{
			Provider:     "deepseek",
			DefaultModel: "deepseek-chat",
		},
		Memory: &v1.MemoryHarnessSpec{
			Type: "inmemory",
			TTL:  3600,
		},
	}

	if cfg.Model.Provider != "deepseek" {
		t.Errorf("expected 'deepseek', got '%s'", cfg.Model.Provider)
	}
	if cfg.Memory.Type != "inmemory" {
		t.Errorf("expected 'inmemory', got '%s'", cfg.Memory.Type)
	}
}

func TestAgentConfig(t *testing.T) {
	cfg := &AgentConfig{
		Name:        "test-agent",
		Description: "Test Agent",
		Model:       "deepseek-chat",
		Temperature: 0.7,
		MaxTokens:   4096,
	}

	if cfg.Name != "test-agent" {
		t.Errorf("expected 'test-agent', got '%s'", cfg.Name)
	}
	if cfg.Temperature != 0.7 {
		t.Errorf("expected 0.7, got %f", cfg.Temperature)
	}
}

func TestAgentStatus(t *testing.T) {
	status := &AgentStatus{
		ID:           "test-agent",
		Name:         "Test Agent",
		Phase:        v1.AgentPhaseRunning,
		Running:      true,
		SessionCount: 5,
	}

	if status.ID != "test-agent" {
		t.Errorf("expected 'test-agent', got '%s'", status.ID)
	}
	if !status.Running {
		t.Error("expected Running to be true")
	}
	if status.Phase != v1.AgentPhaseRunning {
		t.Errorf("expected Running phase, got '%s'", status.Phase)
	}
}

func TestFrameworkInfo(t *testing.T) {
	info := &FrameworkInfo{
		Type:         HandlerTypeADK,
		Name:         "ADK-Go",
		Version:      "1.0.0",
		Description:  "Test Description",
		Capabilities: []string{"multi-agent", "tools"},
		ConfigFormat: "yaml",
	}

	if info.Type != HandlerTypeADK {
		t.Errorf("expected ADK type, got '%s'", info.Type)
	}
	if len(info.Capabilities) != 2 {
		t.Errorf("expected 2 capabilities, got %d", len(info.Capabilities))
	}
}

func TestFrameworkStatus(t *testing.T) {
	status := &FrameworkStatus{
		Running:       true,
		ProcessID:     12345,
		ConfigPath:    "/etc/config.yaml",
		WorkDir:       "/work",
		InstanceCount: 1,
		AgentCount:    3,
		Health:        "healthy",
	}

	if !status.Running {
		t.Error("expected Running to be true")
	}
	if status.ProcessID != 12345 {
		t.Errorf("expected 12345, got %d", status.ProcessID)
	}
	if status.Health != "healthy" {
		t.Errorf("expected 'healthy', got '%s'", status.Health)
	}
}

func TestFrameworkInstanceInfo(t *testing.T) {
	info := &FrameworkInstanceInfo{
		InstanceID:    "instance-1",
		ProcessID:     12345,
		ConfigPath:    "/etc/config.yaml",
		WorkDir:       "/work",
		StartedAt:     1700000000,
		AgentIDs:      []string{"agent-1", "agent-2"},
		Status:        "running",
	}

	if info.InstanceID != "instance-1" {
		t.Errorf("expected 'instance-1', got '%s'", info.InstanceID)
	}
	if len(info.AgentIDs) != 2 {
		t.Errorf("expected 2 agent IDs, got %d", len(info.AgentIDs))
	}
}

func TestAgentInfo(t *testing.T) {
	info := &AgentInfo{
		ID:    "test-agent",
		Name:  "Test Agent",
		Type:  agent.AgentTypeLLM,
		Phase: v1.AgentPhaseRunning,
	}

	if info.ID != "test-agent" {
		t.Errorf("expected 'test-agent', got '%s'", info.ID)
	}
	if info.Type != agent.AgentTypeLLM {
		t.Errorf("expected LLM type, got '%s'", info.Type)
	}
}

func TestHandlerConfig(t *testing.T) {
	cfg := &HandlerConfig{
		Type:             HandlerTypeOpenClaw,
		FrameworkVersion: "2026.3.8",
		FrameworkBin:     "/shared/bin/openclaw",
		WorkDir:          "/shared/workdir",
		ConfigDir:        "/shared/config",
		DebugMode:        true,
	}

	if cfg.Type != HandlerTypeOpenClaw {
		t.Errorf("expected OpenClaw type, got '%s'", cfg.Type)
	}
	if cfg.FrameworkBin != "/shared/bin/openclaw" {
		t.Errorf("expected '/shared/bin/openclaw', got '%s'", cfg.FrameworkBin)
	}
}