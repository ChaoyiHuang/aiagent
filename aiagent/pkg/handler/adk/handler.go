// Package adk provides handler implementation for ADK-Go framework.
// ADK-Go supports two process modes:
// - shared: single Framework process, multiple agents in same process
// - isolated: each agent runs in its own Framework process
//
// Handler Responsibilities for ADK-Go:
// ┌─────────────────────────────────────────────────────────────────┐
// │ ProcessMode = shared (单进程多Agent):                           │
// │   1. Generate agent.yaml with all agents defined                │
// │   2. Start single Framework process                             │
// │   3. Framework manages all agents internally                    │
// │   4. Handler monitors single process health                     │
// ├─────────────────────────────────────────────────────────────────┤
// │ ProcessMode = isolated (每进程一个Agent):                       │
// │   1. Generate agent.yaml per agent                              │
// │   2. Start Framework process for each agent                     │
// │   3. Handler manages multiple process instances                 │
// │   4. Handler monitors each process health                       │
// └─────────────────────────────────────────────────────────────────┘
package adk

import (
	"context"
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

// ADKHandler implements handler.Handler for ADK-Go framework.
type ADKHandler struct {
	handler.BaseHandler

	// Mutex for agent and process tracking
	mu sync.RWMutex

	// Process mode
	processMode handler.ProcessModeType

	// Configuration converter
	converter *ConfigConverter

	// Framework process (for shared mode - single process)
	mainProcess    *exec.Cmd
	mainProcessPID int
	mainProcessRunning bool

	// Framework processes (for isolated mode - multiple processes)
	processes      map[string]*exec.Cmd
	processStates  map[string]*ProcessState

	// Harness manager
	harnessMgr handler.HarnessManagerInterface

	// Work directory
	workDir string

	// Config directory
	configDir string

	// Framework binary path
	frameworkBin string

	// Main config path (for shared mode)
	mainConfigPath string
}

// ProcessState tracks the state of a framework process.
type ProcessState struct {
	ProcessID  int
	StartedAt  time.Time
	ConfigPath string
	Running    bool
	AgentID    string
}

// NewADKHandler creates a new ADK handler.
func NewADKHandler(cfg *handler.HandlerConfig) *ADKHandler {
	info := &handler.FrameworkInfo{
		Type:         handler.HandlerTypeADK,
		Name:         "ADK-Go",
		Version:      "1.0.0",
		Description:  "Google Agent Development Kit for Go",
		Capabilities: []string{"multi-agent", "workflow", "tools", "sessions"},
		ConfigFormat: "yaml",
	}

	// Default to isolated mode if not specified
	processMode := cfg.ProcessMode
	if processMode == "" {
		processMode = handler.ProcessModeIsolated
	}

	return &ADKHandler{
		BaseHandler:    *handler.NewBaseHandler(cfg, info),
		processMode:    processMode,
		converter:      NewConfigConverter(),
		processes:      make(map[string]*exec.Cmd),
		processStates:  make(map[string]*ProcessState),
		workDir:        cfg.WorkDir,
		configDir:      cfg.ConfigDir,
		frameworkBin:   cfg.FrameworkBin,
	}
}

func (h *ADKHandler) Type() handler.HandlerType {
	return handler.HandlerTypeADK
}

func (h *ADKHandler) GetFrameworkInfo() *handler.FrameworkInfo {
	return h.BaseHandler.GetFrameworkInfo()
}

// ============================================================
// Core Responsibility #1: Configuration Transformation
// ============================================================

// GenerateFrameworkConfig generates the main agent.yaml config file.
func (h *ADKHandler) GenerateFrameworkConfig(spec *v1.AIAgentSpec, harnessCfg *handler.HarnessConfig) ([]byte, error) {
	return h.converter.ConvertToADKConfig(spec, harnessCfg)
}

// GenerateAgentConfig generates agent-specific configuration.
func (h *ADKHandler) GenerateAgentConfig(spec *v1.AIAgentSpec, harnessCfg *handler.HarnessConfig) ([]byte, error) {
	agentCfg, err := h.converter.ConvertAgentSpec(spec, harnessCfg)
	if err != nil {
		return nil, err
	}
	return h.converter.MarshalYAML(agentCfg)
}

// GenerateHarnessConfig generates harness-specific configuration sections.
func (h *ADKHandler) GenerateHarnessConfig(harnessCfg *handler.HarnessConfig) ([]byte, error) {
	return h.converter.ConvertHarnessConfig(harnessCfg)
}

// ============================================================
// Core Responsibility #2: Framework Process Management
// ============================================================

// PrepareWorkDirectory prepares ADK directory structure.
func (h *ADKHandler) PrepareWorkDirectory(ctx context.Context, workDir string) error {
	// Create directory structure
	agentsDir := filepath.Join(workDir, "agents")
	configDir := filepath.Join(workDir, "config")
	sessionsDir := filepath.Join(workDir, "sessions")

	dirs := []string{agentsDir, configDir, sessionsDir}
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
func (h *ADKHandler) WriteConfigFiles(ctx context.Context, workDir string, configs map[string][]byte) error {
	configDir := filepath.Join(workDir, "config")

	for name, content := range configs {
		path := filepath.Join(configDir, name)
		if err := os.WriteFile(path, content, 0644); err != nil {
			return fmt.Errorf("failed to write config file %s: %w", path, err)
		}
	}

	return nil
}

// StartFramework starts ADK Framework process.
// Behavior depends on ProcessMode:
// - shared: starts single Framework process with main config
// - isolated: no-op (processes started per agent via StartFrameworkInstance)
func (h *ADKHandler) StartFramework(ctx context.Context, frameworkBin string, workDir string, configPath string) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.processMode == handler.ProcessModeShared {
		// Shared mode: start single Framework process
		if h.mainProcessRunning {
			return fmt.Errorf("framework already running in shared mode")
		}

		args := []string{
			"--config", configPath,
			"--workdir", workDir,
			"--mode", "jsonrpc",
		}

		cmd := exec.CommandContext(ctx, frameworkBin, args...)
		cmd.Dir = workDir

		if err := cmd.Start(); err != nil {
			return fmt.Errorf("failed to start ADK framework: %w", err)
		}

		h.mainProcess = cmd
		h.mainProcessPID = cmd.Process.Pid
		h.mainProcessRunning = true
		h.mainConfigPath = configPath

		return nil
	}

	// Isolated mode: Framework process per agent, started via StartFrameworkInstance
	return nil
}

// StartFrameworkInstance starts a Framework process for a specific agent.
// Only applicable in isolated mode.
func (h *ADKHandler) StartFrameworkInstance(ctx context.Context, instanceID string, configPath string) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.processMode != handler.ProcessModeIsolated {
		return fmt.Errorf("StartFrameworkInstance only valid in isolated mode")
	}

	// Check if process already running
	if _, exists := h.processStates[instanceID]; exists && h.processStates[instanceID].Running {
		return fmt.Errorf("instance %s already running", instanceID)
	}

	// Prepare command
	args := []string{
		"--config", configPath,
		"--workdir", h.workDir,
		"--mode", "jsonrpc",
	}

	cmd := exec.CommandContext(ctx, h.frameworkBin, args...)
	cmd.Dir = h.workDir

	// Start process
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start ADK framework: %w", err)
	}

	h.processes[instanceID] = cmd
	h.processStates[instanceID] = &ProcessState{
		ProcessID:  cmd.Process.Pid,
		StartedAt:  time.Now(),
		ConfigPath: configPath,
		Running:    true,
		AgentID:    instanceID,
	}

	return nil
}

// StopFramework stops all Framework processes.
func (h *ADKHandler) StopFramework(ctx context.Context) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Stop main process (shared mode)
	if h.mainProcessRunning && h.mainProcess != nil && h.mainProcess.Process != nil {
		h.mainProcess.Process.Signal(os.Interrupt)
		h.mainProcessRunning = false
	}

	// Stop all isolated processes
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

// StopFrameworkInstance stops a specific Framework process.
// Only applicable in isolated mode.
func (h *ADKHandler) StopFrameworkInstance(ctx context.Context, instanceID string) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	return h.stopFrameworkInstanceInternal(instanceID)
}

// stopFrameworkInstanceInternal stops a specific Framework process without locking.
// Must be called with h.mu already held.
func (h *ADKHandler) stopFrameworkInstanceInternal(instanceID string) error {
	if h.processMode == handler.ProcessModeShared {
		return fmt.Errorf("StopFrameworkInstance only valid in isolated mode")
	}

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

// IsFrameworkRunning checks if any Framework process is running.
func (h *ADKHandler) IsFrameworkRunning(ctx context.Context) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if h.processMode == handler.ProcessModeShared {
		return h.mainProcessRunning
	}

	for _, state := range h.processStates {
		if state.Running {
			return true
		}
	}
	return false
}

// GetFrameworkStatus returns Framework status.
func (h *ADKHandler) GetFrameworkStatus(ctx context.Context) (*handler.FrameworkStatus, error) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	agents, _ := h.BaseHandler.ListAgents(ctx)

	if h.processMode == handler.ProcessModeShared {
		return &handler.FrameworkStatus{
			Running:       h.mainProcessRunning,
			ProcessID:     h.mainProcessPID,
			ConfigPath:    h.mainConfigPath,
			WorkDir:       h.workDir,
			InstanceCount: 1,
			AgentCount:    len(agents),
			Health:        "healthy",
		}, nil
	}

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

func (h *ADKHandler) SetHarnessManager(harnessMgr handler.HarnessManagerInterface) error {
	h.harnessMgr = harnessMgr
	return nil
}

func (h *ADKHandler) GetHarnessManager() handler.HarnessManagerInterface {
	return h.harnessMgr
}

func (h *ADKHandler) AdaptModelHarness(harness handler.ModelHarnessInterface) ([]byte, error) {
	return h.converter.ConvertModelHarness(harness)
}

func (h *ADKHandler) AdaptMCPHarness(harness handler.MCPHarnessInterface) ([]byte, error) {
	return h.converter.ConvertMCPHarness(harness)
}

func (h *ADKHandler) AdaptMemoryHarness(harness handler.MemoryHarnessInterface) ([]byte, error) {
	return h.converter.ConvertMemoryHarness(harness)
}

func (h *ADKHandler) AdaptSandboxHarness(harness handler.SandboxHarnessInterface) ([]byte, error) {
	return h.converter.ConvertSandboxHarness(harness)
}

func (h *ADKHandler) AdaptSkillsHarness(harness handler.SkillsHarnessInterface) ([]byte, error) {
	return h.converter.ConvertSkillsHarness(harness)
}

// ============================================================
// Core Responsibility #4: Agent Lifecycle (via Framework)
// ============================================================

// LoadAgent creates agent wrapper.
// IMPORTANT: Uses agentName (from AIAgent CRD metadata.name) as agentID, NOT spec.Description.
// This ensures each agent has a unique identifier for isolated mode.
func (h *ADKHandler) LoadAgent(ctx context.Context, spec *v1.AIAgentSpec, harnessCfg *handler.HarnessConfig, agentName string) (agent.Agent, error) {
	// Use provided agentName as agentID (this is the AIAgent CRD metadata.name)
	// If not provided, fall back to spec.Description (for backwards compatibility)
	agentID := agentName
	if agentID == "" {
		agentID = spec.Description
	}

	// Generate config file
	configData, err := h.GenerateFrameworkConfig(spec, harnessCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to generate config: %w", err)
	}

	// Write config to work directory
	configPath := filepath.Join(h.configDir, fmt.Sprintf("%s.yaml", agentID))
	if err := os.WriteFile(configPath, configData, 0644); err != nil {
		return nil, fmt.Errorf("failed to write config: %w", err)
	}

	// Create agent wrapper
	wrapper := NewADKAgentWrapper(agentID, configPath, h)

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

// StartAgent starts an agent.
// Behavior depends on ProcessMode:
// - shared: Framework already running, agent config added to main config
// - isolated: Start Framework process for this agent
func (h *ADKHandler) StartAgent(ctx context.Context, ag agent.Agent, config *handler.AgentConfig) error {
	wrapper, ok := ag.(*ADKAgentWrapper)
	if !ok {
		return fmt.Errorf("agent is not an ADK agent wrapper")
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	if h.processMode == handler.ProcessModeIsolated {
		// Isolated mode: start Framework process for this agent
		if err := h.StartFrameworkInstance(ctx, wrapper.agentID, wrapper.configPath); err != nil {
			return fmt.Errorf("failed to start framework: %w", err)
		}
	}

	// Update agent phase
	if info := h.BaseHandler.GetAgent(wrapper.agentID); info != nil {
		info.Phase = v1.AgentPhaseRunning
	}

	return nil
}

// StopAgent stops an agent.
// Behavior depends on ProcessMode:
// - shared: remove agent from main config, Framework handles internally
// - isolated: stop Framework process for this agent
func (h *ADKHandler) StopAgent(ctx context.Context, agentID string) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.processMode == handler.ProcessModeIsolated {
		// Isolated mode: stop Framework process (use internal version to avoid deadlock)
		if err := h.stopFrameworkInstanceInternal(agentID); err != nil {
			return err
		}
	}

	// Update agent phase
	if info := h.BaseHandler.GetAgent(agentID); info != nil {
		info.Phase = v1.AgentPhaseTerminated
	}

	return nil
}

// GetAgentStatus returns agent status.
func (h *ADKHandler) GetAgentStatus(ctx context.Context, agentID string) (*handler.AgentStatus, error) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	info := h.BaseHandler.GetAgent(agentID)
	if info == nil {
		return nil, fmt.Errorf("agent %s not found", agentID)
	}

	running := false
	if h.processMode == handler.ProcessModeShared {
		running = h.mainProcessRunning
	} else if state, exists := h.processStates[agentID]; exists {
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

func (h *ADKHandler) SupportsMultiAgent() bool {
	return true // ADK supports multiple agents
}

func (h *ADKHandler) SupportsMultiInstance() bool {
	// Only supports multiple instances in isolated mode
	return h.processMode == handler.ProcessModeIsolated
}

// ============================================================
// Instance Management
// ============================================================

func (h *ADKHandler) ListFrameworkInstances(ctx context.Context) []handler.FrameworkInstanceInfo {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if h.processMode == handler.ProcessModeShared {
		if h.mainProcessRunning {
			return []handler.FrameworkInstanceInfo{
				{
					InstanceID: "main",
					ProcessID:  h.mainProcessPID,
					ConfigPath: h.mainConfigPath,
					WorkDir:    h.workDir,
					Status:     "running",
				},
			}
		}
		return nil
	}

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

func (h *ADKHandler) GetFrameworkInstance(instanceID string) *handler.FrameworkInstanceInfo {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if h.processMode == handler.ProcessModeShared {
		if instanceID == "main" && h.mainProcessRunning {
			return &handler.FrameworkInstanceInfo{
				InstanceID: "main",
				ProcessID:  h.mainProcessPID,
				ConfigPath: h.mainConfigPath,
				WorkDir:    h.workDir,
				Status:     "running",
			}
		}
		return nil
	}

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

// ADKAgentWrapper wraps an ADK agent for tracking.
type ADKAgentWrapper struct {
	agent.BaseAgent
	agentID    string
	configPath string
	handler    *ADKHandler
}

// NewADKAgentWrapper creates a new agent wrapper.
func NewADKAgentWrapper(agentID string, configPath string, h *ADKHandler) *ADKAgentWrapper {
	return &ADKAgentWrapper{
		BaseAgent: *agent.NewBaseAgent(agent.Config{
			Name:        agentID,
			Description: agentID,
		}, agent.AgentTypeLLM),
		agentID:    agentID,
		configPath: configPath,
		handler:    h,
	}
}

func (w *ADKAgentWrapper) AgentID() string {
	return w.agentID
}

func (w *ADKAgentWrapper) ConfigPath() string {
	return w.configPath
}

func (w *ADKAgentWrapper) Run(invCtx agent.InvocationContext) iter.Seq2[*agent.Event, error] {
	return func(yield func(*agent.Event, error) bool) {
		// Agent execution is handled by Framework process
		yield(nil, fmt.Errorf("agent execution handled by Framework process"))
	}
}