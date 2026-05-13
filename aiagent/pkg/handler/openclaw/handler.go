// Package openclaw provides handler implementation for OpenClaw framework.
// OpenClaw supports multiple Gateway processes (isolated mode):
// - Each AIAgent CRD → One Gateway process
// - Each Gateway can manage internal sub-agents (invisible to K8s)
//
// Handler Responsibilities for OpenClaw:
// ┌─────────────────────────────────────────────────────────────────┐
// │ ProcessMode = isolated (每进程一个 Gateway):                    │
// │   1. Generate openclaw.json per Gateway                         │
// │   2. Start Gateway process for each AIAgent                     │
// │   3. Handler manages multiple Gateway instances                 │
// │   4. Each Gateway manages its internal sub-agents               │
// └─────────────────────────────────────────────────────────────────┘
//
// Configuration Sources:
// - agentConfig (from AIAgentSpec): Gateway port, internal agents, overrides
// - Harness (from AgentRuntime): Models, Skills, Memory (shared)
package openclaw

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"aiagent/api/v1"
	"aiagent/pkg/agent"
	"aiagent/pkg/handler"
)

// OpenClawHandler implements handler.Handler for OpenClaw framework.
type OpenClawHandler struct {
	handler.BaseHandler

	// Mutex for agent and process tracking
	mu sync.RWMutex

	// Configuration converter
	converter *ConfigConverter

	// Gateway processes (isolated mode - multiple Gateway instances)
	processes      map[string]*exec.Cmd
	processStates  map[string]*GatewayState

	// Harness manager
	harnessMgr handler.HarnessManagerInterface

	// Work directory
	workDir string

	// Config directory
	configDir string

	// Framework binary path
	frameworkBin string

	// Base Gateway port (each instance gets port + offset)
	basePort int
}

// GatewayState tracks the state of a Gateway process.
type GatewayState struct {
	ProcessID  int
	StartedAt  time.Time
	ConfigPath string
	Port       int
	Running    bool
	AgentID    string
}

// NewOpenClawHandler creates a new OpenClaw handler.
func NewOpenClawHandler(cfg *handler.HandlerConfig) *OpenClawHandler {
	info := &handler.FrameworkInfo{
		Type:         handler.HandlerTypeOpenClaw,
		Name:         "OpenClaw",
		Version:      "v2026.3.8",
		Description:  "OpenClaw Multi-channel AI Gateway",
		Capabilities: []string{"channels", "skills", "tools", "sessions", "agents"},
		ConfigFormat: "json",
	}

	return &OpenClawHandler{
		BaseHandler:   *handler.NewBaseHandler(cfg, info),
		converter:     NewConfigConverter(),
		processes:     make(map[string]*exec.Cmd),
		processStates: make(map[string]*GatewayState),
		workDir:       cfg.WorkDir,
		configDir:     cfg.ConfigDir,
		frameworkBin:  cfg.FrameworkBin,
		basePort:      18789,
	}
}

func (h *OpenClawHandler) Type() handler.HandlerType {
	return handler.HandlerTypeOpenClaw
}

func (h *OpenClawHandler) GetFrameworkInfo() *handler.FrameworkInfo {
	return h.BaseHandler.GetFrameworkInfo()
}

// SetBasePort sets the base Gateway port.
func (h *OpenClawHandler) SetBasePort(port int) {
	h.basePort = port
}

// GetBasePort returns the current base Gateway port.
func (h *OpenClawHandler) GetBasePort() int {
	return h.basePort
}

// ============================================================
// Core Responsibility #1: Configuration Transformation
// ============================================================

// GenerateFrameworkConfig generates the main openclaw.json config file.
// Combines agentConfig (agent-specific) with Harness (shared capabilities).
func (h *OpenClawHandler) GenerateFrameworkConfig(spec *v1.AIAgentSpec, harnessCfg *handler.HarnessConfig) ([]byte, error) {
	return h.converter.ConvertToOpenClawConfig(spec, harnessCfg)
}

// GenerateAgentConfig generates agent-specific configuration section.
func (h *OpenClawHandler) GenerateAgentConfig(spec *v1.AIAgentSpec, harnessCfg *handler.HarnessConfig) ([]byte, error) {
	agentCfg, err := h.converter.ConvertAgentSpec(spec, harnessCfg)
	if err != nil {
		return nil, err
	}
	return json.Marshal(agentCfg)
}

// GenerateHarnessConfig generates harness-specific configuration sections.
func (h *OpenClawHandler) GenerateHarnessConfig(harnessCfg *handler.HarnessConfig) ([]byte, error) {
	return h.converter.ConvertHarnessConfig(harnessCfg)
}

// ============================================================
// Gateway Port Resolution from agentConfig
// ============================================================

// resolveGatewayPort extracts Gateway port from agentConfig or calculates default.
func (h *OpenClawHandler) resolveGatewayPort(spec *v1.AIAgentSpec, instanceIndex int) int {
	// Try to parse port from agentConfig
	if spec.AgentConfig != nil && spec.AgentConfig.Raw != nil {
		agentConfig, err := h.converter.ParseAgentConfig(spec.AgentConfig.Raw)
		if err == nil && agentConfig.Gateway != nil && agentConfig.Gateway.Port > 0 {
			return agentConfig.Gateway.Port
		}
	}

	// Calculate default port: basePort + instanceIndex
	return h.basePort + instanceIndex
}

// resolveGatewayBind extracts Gateway bind mode from agentConfig.
func (h *OpenClawHandler) resolveGatewayBind(spec *v1.AIAgentSpec) string {
	defaultBind := "loopback"

	if spec.AgentConfig != nil && spec.AgentConfig.Raw != nil {
		agentConfig, err := h.converter.ParseAgentConfig(spec.AgentConfig.Raw)
		if err == nil && agentConfig.Gateway != nil && agentConfig.Gateway.Bind != "" {
			return agentConfig.Gateway.Bind
		}
	}

	return defaultBind
}

// resolveGatewayAuth extracts Gateway auth mode from agentConfig.
func (h *OpenClawHandler) resolveGatewayAuth(spec *v1.AIAgentSpec) string {
	defaultAuth := "none"

	if spec.AgentConfig != nil && spec.AgentConfig.Raw != nil {
		agentConfig, err := h.converter.ParseAgentConfig(spec.AgentConfig.Raw)
		if err == nil && agentConfig.Gateway != nil && agentConfig.Gateway.Auth != nil {
			return agentConfig.Gateway.Auth.Mode
		}
	}

	return defaultAuth
}

// ============================================================
// Core Responsibility #2: Framework Process Management
// ============================================================

// PrepareWorkDirectory prepares OpenClaw directory structure.
func (h *OpenClawHandler) PrepareWorkDirectory(ctx context.Context, workDir string) error {
	// Create OpenClaw config directory structure
	configDir := filepath.Join(workDir, "openclaw-config")
	agentsDir := filepath.Join(workDir, "agents")
	stateDir := filepath.Join(workDir, "state")

	dirs := []string{configDir, agentsDir, stateDir}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	h.workDir = workDir
	h.configDir = configDir

	return nil
}

// WriteConfigFiles writes configuration files to work directory.
func (h *OpenClawHandler) WriteConfigFiles(ctx context.Context, workDir string, configs map[string][]byte) error {
	for name, content := range configs {
		path := filepath.Join(h.configDir, name)
		if err := os.WriteFile(path, content, 0644); err != nil {
			return fmt.Errorf("failed to write config file %s: %w", path, err)
		}
	}

	return nil
}

// StartFramework starts OpenClaw Gateway process (single instance mode - deprecated).
func (h *OpenClawHandler) StartFramework(ctx context.Context, frameworkBin string, workDir string, configPath string) error {
	// OpenClaw now supports multiple instances
	// This method is kept for interface compatibility but does nothing
	return nil
}

// StartFrameworkInstance starts a Gateway process for a specific agent.
// Gateway startup parameters derived from agentConfig:
//   --port    from agentConfig.gateway.port (or basePort + instanceIndex)
//   --bind    from agentConfig.gateway.bind (default: loopback)
//   --auth    from agentConfig.gateway.auth.mode (default: none)
func (h *OpenClawHandler) StartFrameworkInstance(ctx context.Context, instanceID string, configPath string) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Check if process already running
	if _, exists := h.processStates[instanceID]; exists && h.processStates[instanceID].Running {
		return fmt.Errorf("gateway instance %s already running", instanceID)
	}

	// Calculate port for this instance
	instanceIndex := len(h.processStates)
	port := h.basePort + instanceIndex

	// Try to load port from saved config (if agentConfig specified it)
	savedPort, err := h.loadPortFromConfig(instanceID)
	if err == nil && savedPort > 0 {
		port = savedPort
	}

	// Prepare command
	// Gateway command: openclaw gateway --allow-unconfigured --port <port>
	args := []string{
		"gateway",
		"--allow-unconfigured",
		"--bind", "loopback",
		"--port", fmt.Sprintf("%d", port),
		"--auth", "none",
		"--force",
		"--config", configPath,
	}

	cmd := exec.CommandContext(ctx, h.frameworkBin, args...)
	cmd.Dir = h.workDir

	// Set environment
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("OPENCLAW_CONFIG_DIR=%s", h.configDir),
		fmt.Sprintf("OPENCLAW_STATE_DIR=%s", h.workDir),
	)

	// Start process
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start openclaw gateway: %w", err)
	}

	h.processes[instanceID] = cmd
	h.processStates[instanceID] = &GatewayState{
		ProcessID:  cmd.Process.Pid,
		StartedAt:  time.Now(),
		ConfigPath: configPath,
		Port:       port,
		Running:    true,
		AgentID:    instanceID,
	}

	return nil
}

// loadPortFromConfig attempts to load Gateway port from saved agentConfig.
func (h *OpenClawHandler) loadPortFromConfig(instanceID string) (int, error) {
	// Look for saved agentConfig in configDir
	configPath := filepath.Join(h.configDir, fmt.Sprintf("%s-agentconfig.json", instanceID))
	data, err := os.ReadFile(configPath)
	if err != nil {
		return 0, err
	}

	agentConfig := &AgentConfigJSON{}
	if err := json.Unmarshal(data, agentConfig); err != nil {
		return 0, err
	}

	if agentConfig.Gateway != nil && agentConfig.Gateway.Port > 0 {
		return agentConfig.Gateway.Port, nil
	}

	return 0, fmt.Errorf("no port in agentConfig")
}

// saveAgentConfig saves agentConfig for later port resolution.
func (h *OpenClawHandler) saveAgentConfig(instanceID string, raw []byte) error {
	configPath := filepath.Join(h.configDir, fmt.Sprintf("%s-agentconfig.json", instanceID))
	return os.WriteFile(configPath, raw, 0644)
}

// StopFramework stops all Gateway processes.
func (h *OpenClawHandler) StopFramework(ctx context.Context) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	for instanceID, cmd := range h.processes {
		if cmd.Process != nil {
			cmd.Process.Signal(os.Interrupt)
		}
		if state, exists := h.processStates[instanceID]; exists {
			state.Running = false
		}
	}

	return nil
}

// StopFrameworkInstance stops a specific Gateway process.
func (h *OpenClawHandler) StopFrameworkInstance(ctx context.Context, instanceID string) error {
	return h.stopFrameworkInstanceInternal(instanceID)
}

// stopFrameworkInstanceInternal stops a Gateway without locking (must be called with lock held).
func (h *OpenClawHandler) stopFrameworkInstanceInternal(instanceID string) error {
	cmd, exists := h.processes[instanceID]
	if !exists {
		return nil
	}

	if cmd.Process != nil {
		cmd.Process.Signal(os.Interrupt)
	}

	if state, exists := h.processStates[instanceID]; exists {
		state.Running = false
	}
	delete(h.processes, instanceID)

	return nil
}

// IsFrameworkRunning checks if any Gateway process is running.
func (h *OpenClawHandler) IsFrameworkRunning(ctx context.Context) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()

	for _, state := range h.processStates {
		if state.Running {
			return true
		}
	}
	return false
}

// GetFrameworkStatus returns Gateway status.
func (h *OpenClawHandler) GetFrameworkStatus(ctx context.Context) (*handler.FrameworkStatus, error) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	runningCount := 0
	for _, state := range h.processStates {
		if state.Running {
			runningCount++
		}
	}

	return &handler.FrameworkStatus{
		Running:       runningCount > 0,
		InstanceCount: len(h.processStates),
		AgentCount:    len(h.processStates),
		WorkDir:       h.workDir,
		ConfigPath:    h.configDir,
		Health:        "healthy",
	}, nil
}

// ============================================================
// Core Responsibility #3: Harness Adaptation
// ============================================================

func (h *OpenClawHandler) SetHarnessManager(harnessMgr handler.HarnessManagerInterface) error {
	h.harnessMgr = harnessMgr
	return nil
}

func (h *OpenClawHandler) GetHarnessManager() handler.HarnessManagerInterface {
	return h.harnessMgr
}

func (h *OpenClawHandler) AdaptModelHarness(harness handler.ModelHarnessInterface) ([]byte, error) {
	return h.converter.ConvertModelHarness(harness)
}

func (h *OpenClawHandler) AdaptMCPHarness(harness handler.MCPHarnessInterface) ([]byte, error) {
	// MCP not supported - return nil
	return nil, nil
}

func (h *OpenClawHandler) AdaptMemoryHarness(harness handler.MemoryHarnessInterface) ([]byte, error) {
	return h.converter.ConvertMemoryHarness(harness)
}

func (h *OpenClawHandler) AdaptSandboxHarness(harness handler.SandboxHarnessInterface) ([]byte, error) {
	if harness == nil {
		return nil, nil
	}

	// External Sandbox: Generate harness-bridge plugin
	if harness.IsExternal() {
		pluginDir := filepath.Join(h.configDir, "plugins", "harness-bridge")
		return h.converter.ConvertSandboxHarness(harness, pluginDir)
	}

	// Embedded Sandbox: No plugin needed
	return h.converter.ConvertSandboxHarness(harness, "")
}

func (h *OpenClawHandler) AdaptSkillsHarness(harness handler.SkillsHarnessInterface) ([]byte, error) {
	return h.converter.ConvertSkillsHarness(harness)
}

// ============================================================
// Core Responsibility #4: Agent Lifecycle (via Gateway)
// ============================================================

// LoadAgent creates agent wrapper and prepares Gateway config.
func (h *OpenClawHandler) LoadAgent(ctx context.Context, spec *v1.AIAgentSpec, harnessCfg *handler.HarnessConfig) (agent.Agent, error) {
	agentID := spec.Description

	// Save agentConfig for port resolution
	if spec.AgentConfig != nil && spec.AgentConfig.Raw != nil {
		if err := h.saveAgentConfig(agentID, spec.AgentConfig.Raw); err != nil {
			// Non-critical error, continue
		}
	}

	// Generate config file for this Gateway instance
	configData, err := h.GenerateFrameworkConfig(spec, harnessCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to generate config: %w", err)
	}

	// Write config to work directory
	configPath := filepath.Join(h.configDir, fmt.Sprintf("%s.json", agentID))
	if err := os.WriteFile(configPath, configData, 0644); err != nil {
		return nil, fmt.Errorf("failed to write config: %w", err)
	}

	// Create agent wrapper
	wrapper := NewOpenClawAgentWrapper(agentID, configPath, h)

	// Register agent with BaseHandler
	h.mu.Lock()
	h.BaseHandler.RegisterAgent(&handler.AgentInfo{
		ID:    agentID,
		Name:  agentID,
		Type:  agent.AgentTypeLLM,
		Phase: v1.AgentPhasePending,
	})
	h.mu.Unlock()

	return wrapper, nil
}

// StartAgent starts a Gateway process for this agent.
func (h *OpenClawHandler) StartAgent(ctx context.Context, ag agent.Agent, config *handler.AgentConfig) error {
	wrapper, ok := ag.(*OpenClawAgentWrapper)
	if !ok {
		return fmt.Errorf("agent is not an OpenClaw agent wrapper")
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	// Start Gateway process for this agent
	if err := h.StartFrameworkInstance(ctx, wrapper.agentID, wrapper.configPath); err != nil {
		return fmt.Errorf("failed to start gateway: %w", err)
	}

	// Update agent phase
	if info := h.BaseHandler.GetAgent(wrapper.agentID); info != nil {
		info.Phase = v1.AgentPhaseRunning
	}

	return nil
}

// StopAgent stops a Gateway process for this agent.
func (h *OpenClawHandler) StopAgent(ctx context.Context, agentID string) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Stop Gateway process
	if err := h.stopFrameworkInstanceInternal(agentID); err != nil {
		return err
	}

	// Update agent phase
	if info := h.BaseHandler.GetAgent(agentID); info != nil {
		info.Phase = v1.AgentPhaseTerminated
	}

	return nil
}

// GetAgentStatus returns agent status.
func (h *OpenClawHandler) GetAgentStatus(ctx context.Context, agentID string) (*handler.AgentStatus, error) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	info := h.BaseHandler.GetAgent(agentID)
	if info == nil {
		return nil, fmt.Errorf("agent %s not found", agentID)
	}

	running := false
	if state, exists := h.processStates[agentID]; exists {
		running = state.Running
	}

	return &handler.AgentStatus{
		ID:      agentID,
		Name:    info.Name,
		Phase:   info.Phase,
		Running: running,
	}, nil
}

// ============================================================
// Framework Capability Queries
// ============================================================

func (h *OpenClawHandler) SupportsMultiAgent() bool {
	return true // Each Gateway can manage internal sub-agents
}

func (h *OpenClawHandler) SupportsMultiInstance() bool {
	return true // OpenClaw now supports multiple Gateway instances
}

// ============================================================
// Instance Management
// ============================================================

func (h *OpenClawHandler) ListFrameworkInstances(ctx context.Context) []handler.FrameworkInstanceInfo {
	h.mu.RLock()
	defer h.mu.RUnlock()

	instances := []handler.FrameworkInstanceInfo{}
	for instanceID, state := range h.processStates {
		status := "stopped"
		if state.Running {
			status = "running"
		}
		instances = append(instances, handler.FrameworkInstanceInfo{
			InstanceID: instanceID,
			ProcessID:  state.ProcessID,
			ConfigPath: state.ConfigPath,
			WorkDir:    h.workDir,
			StartedAt:  state.StartedAt.Unix(),
			Status:     status,
		})
	}
	return instances
}

func (h *OpenClawHandler) GetFrameworkInstance(instanceID string) *handler.FrameworkInstanceInfo {
	h.mu.RLock()
	defer h.mu.RUnlock()

	state, exists := h.processStates[instanceID]
	if !exists {
		return nil
	}

	status := "stopped"
	if state.Running {
		status = "running"
	}

	return &handler.FrameworkInstanceInfo{
		InstanceID: instanceID,
		ProcessID:  state.ProcessID,
		ConfigPath: state.ConfigPath,
		WorkDir:    h.workDir,
		StartedAt:  state.StartedAt.Unix(),
		Status:     status,
	}
}

// ============================================================
// Agent Wrapper
// ============================================================

// OpenClawAgentWrapper wraps an OpenClaw agent for tracking.
type OpenClawAgentWrapper struct {
	agent.BaseAgent
	agentID    string
	configPath string
	handler    *OpenClawHandler
}

// NewOpenClawAgentWrapper creates a new agent wrapper.
func NewOpenClawAgentWrapper(agentID string, configPath string, h *OpenClawHandler) *OpenClawAgentWrapper {
	return &OpenClawAgentWrapper{
		BaseAgent: *agent.NewBaseAgent(agent.Config{
			Name:        agentID,
			Description: agentID,
		}, agent.AgentTypeLLM),
		agentID:    agentID,
		configPath: configPath,
		handler:    h,
	}
}

func (w *OpenClawAgentWrapper) AgentID() string {
	return w.agentID
}

func (w *OpenClawAgentWrapper) ConfigPath() string {
	return w.configPath
}

func (w *OpenClawAgentWrapper) Run(invCtx agent.InvocationContext) iter.Seq2[*agent.Event, error] {
	return func(yield func(*agent.Event, error) bool) {
		// Agent execution is handled by Gateway process
		yield(nil, fmt.Errorf("agent execution handled by Gateway process"))
	}
}