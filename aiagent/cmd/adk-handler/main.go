// ADK Handler entry point.
// Process Manager for ADK Framework.
// Uses ImageVolume to access Framework's filesystem at /framework-rootfs.
//
// Architecture (Solution M - hostPath + Config Daemon):
// ┌─────────────────────────────────────────────────────────────────┐
// │ Config Daemon (DaemonSet on same node)                          │
// │  - Watches AIAgent CRDs via Informer                            │
// │  - Writes AgentConfig to hostPath                               │
// │  - Path: /var/lib/aiagent/configs/<namespace>/<agent-name>/     │
// │  - Files: agent-config.json, agent-meta.yaml                    │
// │  - Creates agent-index.yaml in namespace directory              │
// │                                                                 │
// │ Pod (AgentRuntime)                                              │
// │  - Mounts hostPath as /etc/agent-config                         │
// │                                                                 │
// │  Handler Container (Process Manager)                            │
// │    - Reads agent-index.yaml to discover agents                  │
// │    - Reads agent-config.json for each agent                     │
// │    - Starts Framework processes for each Agent                  │
// │    - No K8s API calls needed                                    │
// │                                                                 │
// │  Framework Container (DUMMY)                                    │
// │    - ENTRYPOINT: pause (just sleeps)                            │
// │    - Provides image content for ImageVolume                     │
// └─────────────────────────────────────────────────────────────────┘
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"gopkg.in/yaml.v3"

	"aiagent/api/v1"
	"aiagent/pkg/handler"
	"aiagent/pkg/handler/adk"
	"aiagent/pkg/harness"
)

var (
	frameworkBin = flag.String("framework", "", "Framework binary path (ImageVolume: /framework-rootfs/adk-framework)")
	workDir      = flag.String("workdir", "", "Shared work directory (e.g., /shared/workdir)")
	configDir    = flag.String("configdir", "", "Shared config directory (e.g., /shared/config)")
	harnessDir   = flag.String("harness", "", "Harness config directory (e.g., /etc/harness)")
	agentConfigDir = flag.String("agentconfig", "", "Agent config directory (e.g., /etc/agent-config)")
	namespace    = flag.String("namespace", "", "Namespace for agent configs")
	debug        = flag.Bool("debug", false, "Enable debug logging")
)

// AgentMeta contains metadata about an agent.
type AgentMeta struct {
	Name      string `yaml:"name"`
	Namespace string `yaml:"namespace"`
	Phase     string `yaml:"phase"`
	Runtime   string `yaml:"runtime,omitempty"`
	UID       string `yaml:"uid,omitempty"`
}

// AgentIndexEntry represents an entry in the agent index.
type AgentIndexEntry struct {
	Name      string `yaml:"name"`
	Namespace string `yaml:"namespace"`
	Phase     string `yaml:"phase"`
	Runtime   string `yaml:"runtime,omitempty"`
	UID       string `yaml:"uid,omitempty"`
}

// AgentIndex represents the agent index structure.
type AgentIndex struct {
	Agents []AgentIndexEntry `yaml:"agents"`
}

func main() {
	flag.Parse()

	// Get framework path from environment or flag
	fwBin := *frameworkBin
	if fwBin == "" {
		fwBin = os.Getenv("FRAMEWORK_BIN")
		if fwBin == "" {
			fwBin = "/framework-rootfs/adk-framework"
		}
	}

	// Get work directory from environment or flag
	wd := *workDir
	if wd == "" {
		wd = os.Getenv("WORK_DIR")
		if wd == "" {
			wd = "/shared/workdir"
		}
	}

	// Get config directory from environment or flag
	cfgDir := *configDir
	if cfgDir == "" {
		cfgDir = os.Getenv("CONFIG_DIR")
		if cfgDir == "" {
			cfgDir = "/shared/config"
		}
	}

	// Get harness directory from environment or flag
	hDir := *harnessDir
	if hDir == "" {
		hDir = os.Getenv("HARNESS_DIR")
		if hDir == "" {
			hDir = "/etc/harness"
		}
	}

	// Get agent config directory from environment or flag
	agCfgDir := *agentConfigDir
	if agCfgDir == "" {
		agCfgDir = os.Getenv("AGENT_CONFIG_DIR")
		if agCfgDir == "" {
			agCfgDir = "/etc/agent-config"
		}
	}

	// Get namespace from environment or flag
	ns := *namespace
	if ns == "" {
		ns = os.Getenv("NAMESPACE")
	}

	// Get process mode from environment
	processMode := handler.ProcessModeIsolated // default
	if pm := os.Getenv("PROCESS_MODE"); pm != "" {
		processMode = handler.ProcessModeType(pm)
		log.Printf("Process Mode from env: %s", pm)
	}

	log.Printf("ADK Handler starting...")
	log.Printf("Framework Binary: %s", fwBin)
	log.Printf("Work Directory: %s", wd)
	log.Printf("Config Directory: %s", cfgDir)
	log.Printf("Harness Directory: %s", hDir)
	log.Printf("Agent Config Directory: %s", agCfgDir)
	log.Printf("Namespace: %s", ns)
	log.Printf("Process Mode: %s", processMode)

	// Verify framework binary exists (ImageVolume should provide it)
	if _, err := os.Stat(fwBin); err != nil {
		log.Fatalf("Framework binary not found: %s (ImageVolume may not be configured)", fwBin)
	}

	// Create necessary directories
	os.MkdirAll(wd, 0755)
	os.MkdirAll(cfgDir, 0755)
	os.MkdirAll(filepath.Join(wd, "agents"), 0755)
	os.MkdirAll(filepath.Join(wd, "sessions"), 0755)

	// Create Handler configuration
	handlerCfg := &handler.HandlerConfig{
		Type:         handler.HandlerTypeADK,
		FrameworkBin: fwBin,
		WorkDir:      wd,
		ConfigDir:    cfgDir,
		DebugMode:    *debug,
		ProcessMode:  processMode,
	}

	// Create ADK Handler
	h := adk.NewADKHandler(handlerCfg)

	// Setup signal handling
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigChan
		log.Printf("Received signal: %v", sig)
		cancel()
	}()

	// Run handler service
	if err := runHandler(ctx, h, hDir, agCfgDir, ns, wd, cfgDir, processMode); err != nil {
		log.Fatalf("Handler error: %v", err)
	}

	log.Printf("ADK Handler shutdown complete")
}

// runHandler runs the handler service loop with agent loading from hostPath.
func runHandler(ctx context.Context, h *adk.ADKHandler, harnessDir string, agentConfigDir string, namespace string, workDir string, configDir string, processMode handler.ProcessModeType) error {
	log.Printf("Initializing Handler service...")

	// 1. Initialize Harness Manager
	harnessMgr := harness.NewHarnessManager()
	harnessSpecs, err := loadHarnessConfigs(harnessDir)
	if err != nil {
		log.Printf("Warning: failed to load harness configs: %v", err)
	} else {
		log.Printf("Loaded %d harness configs", len(harnessSpecs))
		if err := harnessMgr.Initialize(ctx, harnessSpecs); err != nil {
			log.Printf("Error initializing harness manager: %v", err)
		}
	}

	// 2. Prepare work directory
	if err := h.PrepareWorkDirectory(ctx, workDir); err != nil {
		log.Printf("Warning: failed to prepare work directory: %v", err)
	}

	// 3. Build initial HarnessConfig from HarnessManager
	harnessCfg := buildHarnessConfig(harnessMgr)

	// 4. Start polling for agent-index.yaml changes (written by Config Daemon)
	// Path structure: mount is /var/lib/aiagent/configs/<namespace> -> /etc/agent-config
	// So agent-index.yaml is directly at /etc/agent-config/agent-index.yaml
	agentIndexPath := filepath.Join(agentConfigDir, "agent-index.yaml")
	log.Printf("Watching AgentIndex at: %s", agentIndexPath)

	// Track loaded agents
	loadedAgents := make(map[string]bool)

	// Track agent index state to reduce log noise
	lastIndexSize := -1
	notFoundCount := 0
	notFoundLogInterval := 12 // Log every 12 intervals (~1 minute at 5s interval)

	// Poll interval
	pollInterval := 5 * time.Second
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	// Initial load
	loadAgentsFromIndex(ctx, h, agentIndexPath, agentConfigDir, workDir, configDir, harnessCfg, loadedAgents, processMode, &lastIndexSize, &notFoundCount, notFoundLogInterval)

	for {
		select {
		case <-ctx.Done():
			log.Printf("Shutting down handler...")
			return cleanup(ctx, h, loadedAgents)

		case <-ticker.C:
			// Poll for changes
			loadAgentsFromIndex(ctx, h, agentIndexPath, agentConfigDir, workDir, configDir, harnessCfg, loadedAgents, processMode, &lastIndexSize, &notFoundCount, notFoundLogInterval)

			// Check process health
			checkProcessHealth(ctx, h, processMode)
		}
	}
}

// loadHarnessConfigs loads Harness configurations from mounted directory.
func loadHarnessConfigs(harnessDir string) ([]*v1.HarnessSpec, error) {
	specs := []*v1.HarnessSpec{}

	entries, err := os.ReadDir(harnessDir)
	if err != nil {
		if os.IsNotExist(err) {
			return specs, nil
		}
		return nil, fmt.Errorf("failed to read harness directory: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		harnessPath := filepath.Join(harnessDir, entry.Name())
		spec, err := loadSingleHarnessConfig(harnessPath, entry.Name())
		if err != nil {
			log.Printf("Warning: failed to load harness %s: %v", entry.Name(), err)
			continue
		}
		specs = append(specs, spec)
	}

	return specs, nil
}

// loadSingleHarnessConfig loads a single Harness configuration.
func loadSingleHarnessConfig(harnessPath string, harnessName string) (*v1.HarnessSpec, error) {
	// Read harness type
	typePath := filepath.Join(harnessPath, "harness-type")
	typeData, err := os.ReadFile(typePath)
	if err != nil {
		// Try to infer from directory content
		return inferHarnessType(harnessPath, harnessName)
	}
	harnessType := string(typeData)

	spec := &v1.HarnessSpec{
		Type: v1.HarnessType(harnessType),
	}

	// Load type-specific config
	switch v1.HarnessType(harnessType) {
	case v1.HarnessTypeModel:
		modelSpec, err := loadModelHarnessConfig(harnessPath)
		if err != nil {
			return nil, err
		}
		spec.Model = modelSpec

	case v1.HarnessTypeMCP:
		mcpSpec, err := loadMCPHarnessConfig(harnessPath)
		if err != nil {
			return nil, err
		}
		spec.MCP = mcpSpec

	case v1.HarnessTypeMemory:
		memorySpec, err := loadMemoryHarnessConfig(harnessPath)
		if err != nil {
			return nil, err
		}
		spec.Memory = memorySpec

	case v1.HarnessTypeSandbox:
		sandboxSpec, err := loadSandboxHarnessConfig(harnessPath)
		if err != nil {
			return nil, err
		}
		spec.Sandbox = sandboxSpec

	case v1.HarnessTypeSkills:
		skillsSpec, err := loadSkillsHarnessConfig(harnessPath)
		if err != nil {
			return nil, err
		}
		spec.Skills = skillsSpec
	}

	return spec, nil
}

// inferHarnessType infers harness type from directory content.
func inferHarnessType(harnessPath string, harnessName string) (*v1.HarnessSpec, error) {
	// Check for config files
	if _, err := os.Stat(filepath.Join(harnessPath, "model.yaml")); err == nil {
		modelSpec, err := loadModelHarnessConfig(harnessPath)
		if err != nil {
			return nil, err
		}
		return &v1.HarnessSpec{Type: v1.HarnessTypeModel, Model: modelSpec}, nil
	}

	if _, err := os.Stat(filepath.Join(harnessPath, "mcp.yaml")); err == nil {
		mcpSpec, err := loadMCPHarnessConfig(harnessPath)
		if err != nil {
			return nil, err
		}
		return &v1.HarnessSpec{Type: v1.HarnessTypeMCP, MCP: mcpSpec}, nil
	}

	if _, err := os.Stat(filepath.Join(harnessPath, "memory.yaml")); err == nil {
		memorySpec, err := loadMemoryHarnessConfig(harnessPath)
		if err != nil {
			return nil, err
		}
		return &v1.HarnessSpec{Type: v1.HarnessTypeMemory, Memory: memorySpec}, nil
	}

	if _, err := os.Stat(filepath.Join(harnessPath, "sandbox.yaml")); err == nil {
		sandboxSpec, err := loadSandboxHarnessConfig(harnessPath)
		if err != nil {
			return nil, err
		}
		return &v1.HarnessSpec{Type: v1.HarnessTypeSandbox, Sandbox: sandboxSpec}, nil
	}

	if _, err := os.Stat(filepath.Join(harnessPath, "skills.yaml")); err == nil {
		skillsSpec, err := loadSkillsHarnessConfig(harnessPath)
		if err != nil {
			return nil, err
		}
		return &v1.HarnessSpec{Type: v1.HarnessTypeSkills, Skills: skillsSpec}, nil
	}

	return nil, fmt.Errorf("cannot infer harness type from %s", harnessPath)
}

// loadModelHarnessConfig loads model harness config.
func loadModelHarnessConfig(harnessPath string) (*v1.ModelHarnessSpec, error) {
	configPath := filepath.Join(harnessPath, "model.yaml")
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read model.yaml: %w", err)
	}

	var spec v1.ModelHarnessSpec
	if err := yaml.Unmarshal(data, &spec); err != nil {
		return nil, fmt.Errorf("failed to parse model.yaml: %w", err)
	}

	return &spec, nil
}

// loadMCPHarnessConfig loads MCP harness config.
func loadMCPHarnessConfig(harnessPath string) (*v1.MCPHarnessSpec, error) {
	configPath := filepath.Join(harnessPath, "mcp.yaml")
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read mcp.yaml: %w", err)
	}

	var spec v1.MCPHarnessSpec
	if err := yaml.Unmarshal(data, &spec); err != nil {
		return nil, fmt.Errorf("failed to parse mcp.yaml: %w", err)
	}

	return &spec, nil
}

// loadMemoryHarnessConfig loads memory harness config.
func loadMemoryHarnessConfig(harnessPath string) (*v1.MemoryHarnessSpec, error) {
	configPath := filepath.Join(harnessPath, "memory.yaml")
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read memory.yaml: %w", err)
	}

	var spec v1.MemoryHarnessSpec
	if err := yaml.Unmarshal(data, &spec); err != nil {
		return nil, fmt.Errorf("failed to parse memory.yaml: %w", err)
	}

	return &spec, nil
}

// loadSandboxHarnessConfig loads sandbox harness config.
func loadSandboxHarnessConfig(harnessPath string) (*v1.SandboxHarnessSpec, error) {
	configPath := filepath.Join(harnessPath, "sandbox.yaml")
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read sandbox.yaml: %w", err)
	}

	var spec v1.SandboxHarnessSpec
	if err := yaml.Unmarshal(data, &spec); err != nil {
		return nil, fmt.Errorf("failed to parse sandbox.yaml: %w", err)
	}

	return &spec, nil
}

// loadSkillsHarnessConfig loads skills harness config.
func loadSkillsHarnessConfig(harnessPath string) (*v1.SkillsHarnessSpec, error) {
	configPath := filepath.Join(harnessPath, "skills.yaml")
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read skills.yaml: %w", err)
	}

	var spec v1.SkillsHarnessSpec
	if err := yaml.Unmarshal(data, &spec); err != nil {
		return nil, fmt.Errorf("failed to parse skills.yaml: %w", err)
	}

	return &spec, nil
}

// buildHarnessConfig builds Handler's HarnessConfig from HarnessManager.
func buildHarnessConfig(mgr *harness.HarnessManager) *handler.HarnessConfig {
	cfg := &handler.HarnessConfig{}

	if modelHarness := mgr.GetModelHarness(); modelHarness != nil {
		cfg.Model = modelHarness.GetSpec()
	}

	if mcpHarness := mgr.GetMCPHarness(); mcpHarness != nil {
		cfg.MCP = mcpHarness.GetSpec()
	}

	if memoryHarness := mgr.GetMemoryHarness(); memoryHarness != nil {
		cfg.Memory = memoryHarness.GetSpec()
	}

	if sandboxHarness := mgr.GetSandboxHarness(); sandboxHarness != nil {
		cfg.Sandbox = sandboxHarness.GetSpec()
	}

	if skillsHarness := mgr.GetSkillsHarness(); skillsHarness != nil {
		cfg.Skills = skillsHarness.GetSpec()
	}

	return cfg
}

// loadAgentsFromIndex loads agents from agent-index.yaml (written by Config Daemon).
func loadAgentsFromIndex(ctx context.Context, h *adk.ADKHandler, indexPath string, agentConfigDir string, workDir string, configDir string, harnessCfg *handler.HarnessConfig, loadedAgents map[string]bool, processMode handler.ProcessModeType, lastIndexSize *int, notFoundCount *int, notFoundLogInterval int) {
	// Read agent index (written by Config Daemon)
	data, err := os.ReadFile(indexPath)
	if err != nil {
		if os.IsNotExist(err) {
			*notFoundCount++
			// Only log at interval to reduce noise
			if *notFoundCount == 1 || *notFoundCount%notFoundLogInterval == 0 {
				log.Printf("Agent index not found at %s, waiting for Config Daemon (check #%d)", indexPath, *notFoundCount)
			}
			return
		}
		log.Printf("Error reading agent index: %v", err)
		return
	}

	// Reset not found count when file exists
	*notFoundCount = 0

	var index AgentIndex
	if err := yaml.Unmarshal(data, &index); err != nil {
		log.Printf("Error parsing agent index: %v", err)
		return
	}

	// Only log when index size changes
	currentSize := len(index.Agents)
	if currentSize != *lastIndexSize {
		log.Printf("AgentIndex contains %d agents (changed from %d)", currentSize, *lastIndexSize)
		*lastIndexSize = currentSize
	}

	// Process each agent in the index
	for _, entry := range index.Agents {
		if loadedAgents[entry.Name] {
			continue // Already loaded
		}

		// Only load agents in Running, Pending, or Scheduling phase
		if entry.Phase != "Running" && entry.Phase != "Pending" && entry.Phase != "Scheduling" {
			continue
		}

		log.Printf("Loading agent: %s (phase: %s)", entry.Name, entry.Phase)

		// Load agent from hostPath (Config Daemon writes to /etc/agent-config/<agent-name>/)
		agentSpec, agentConfig, err := loadAgentFromHostPath(entry.Name, agentConfigDir)
		if err != nil {
			log.Printf("Error loading agent %s from hostPath: %v", entry.Name, err)
			continue
		}

		// Generate Framework config using agentConfig if available, or use agentSpec
		configData, err := generateFrameworkConfig(h, agentSpec, agentConfig, harnessCfg)
		if err != nil {
			log.Printf("Error generating config for %s: %v", entry.Name, err)
			continue
		}

		// Write config file
		configPath := filepath.Join(configDir, fmt.Sprintf("%s.yaml", entry.Name))
		if err := os.WriteFile(configPath, configData, 0644); err != nil {
			log.Printf("Error writing config for %s: %v", entry.Name, err)
			continue
		}

		log.Printf("Generated config for %s at %s", entry.Name, configPath)

		// Register agent with handler (this makes AgentCount accurate)
		ag, err := h.LoadAgent(ctx, agentSpec, harnessCfg)
		if err != nil {
			log.Printf("Warning: failed to register agent %s: %v", entry.Name, err)
		} else {
			log.Printf("Registered agent %s with handler", entry.Name)
		}

		// Start Framework process based on process mode
		if processMode == handler.ProcessModeIsolated {
			// Isolated mode: start Framework process for each agent
			if err := h.StartFrameworkInstance(ctx, entry.Name, configPath); err != nil {
				log.Printf("Error starting Framework instance for %s: %v", entry.Name, err)
				continue
			}
			log.Printf("Started Framework instance for %s", entry.Name)
		}

		// For shared mode, Framework is started once (first agent triggers it)
		if processMode == handler.ProcessModeShared {
			if !h.IsFrameworkRunning(ctx) {
				combinedConfigPath := filepath.Join(configDir, "agents.yaml")
				if err := h.StartFramework(ctx, h.GetConfig().FrameworkBin, workDir, combinedConfigPath); err != nil {
					log.Printf("Error starting shared Framework: %v", err)
					continue
				}
				log.Printf("Started shared Framework process")
			}
		}

		// Mark agent phase as Running
		if ag != nil {
			h.StartAgent(ctx, ag, nil)
		}

		loadedAgents[entry.Name] = true
		log.Printf("Successfully loaded agent: %s", entry.Name)
	}
}

// loadAgentFromHostPath loads agent config from hostPath directory.
// Config Daemon writes: /var/lib/aiagent/configs/<namespace>/<agent-name>/agent-config.json and agent-meta.yaml
// But the mount is /var/lib/aiagent/configs/<namespace> -> /etc/agent-config
// So inside Pod: /etc/agent-config/<agent-name>/agent-config.json
func loadAgentFromHostPath(agentName string, agentConfigDir string) (*v1.AIAgentSpec, map[string]interface{}, error) {
	// Read agent-meta.yaml for basic metadata
	// Path structure inside Pod: /etc/agent-config/<agent-name>/agent-meta.yaml
	metaPath := filepath.Join(agentConfigDir, agentName, "agent-meta.yaml")
	metaData, err := os.ReadFile(metaPath)
	if err != nil {
		// If meta not found, create minimal spec
		if *debug {
			log.Printf("Agent meta not found for %s: %v", agentName, err)
		}
		agentSpec := &v1.AIAgentSpec{
			Description: agentName,
			RuntimeRef:  v1.RuntimeReference{Type: "adk"},
		}
		return agentSpec, nil, nil
	}

	var meta AgentMeta
	if err := yaml.Unmarshal(metaData, &meta); err != nil {
		log.Printf("Error parsing agent meta for %s: %v", agentName, err)
		agentSpec := &v1.AIAgentSpec{
			Description: agentName,
			RuntimeRef:  v1.RuntimeReference{Type: "adk"},
		}
		return agentSpec, nil, nil
	}

	// Create AIAgentSpec from meta
	agentSpec := &v1.AIAgentSpec{
		Description: meta.Name,
		RuntimeRef:  v1.RuntimeReference{
			Type: "adk",
			Name: meta.Runtime,
		},
	}

	// Read agent-config.json (from spec.agentConfig)
	// Path structure inside Pod: /etc/agent-config/<agent-name>/agent-config.json
	agentConfigPath := filepath.Join(agentConfigDir, agentName, "agent-config.json")
	agentConfigData, err := os.ReadFile(agentConfigPath)
	if err != nil {
		if *debug {
			log.Printf("Agent config not found for %s, using defaults", agentName)
		}
		return agentSpec, nil, nil
	}

	// Parse agent config JSON
	var agentConfig map[string]interface{}
	if err := json.Unmarshal(agentConfigData, &agentConfig); err != nil {
		log.Printf("Error parsing agent config for %s: %v", agentName, err)
		return agentSpec, nil, nil
	}

	if *debug {
		log.Printf("Loaded agent config for %s: %v", agentName, agentConfig)
	}

	return agentSpec, agentConfig, nil
}

// generateFrameworkConfig generates framework config using agentConfig or agentSpec.
func generateFrameworkConfig(h *adk.ADKHandler, agentSpec *v1.AIAgentSpec, agentConfig map[string]interface{}, harnessCfg *handler.HarnessConfig) ([]byte, error) {
	// If agentConfig is provided (from spec.agentConfig), use it directly
	// The Handler passes it to the Framework
	if agentConfig != nil {
		// Merge with harness config if needed
		return json.MarshalIndent(agentConfig, "", "  ")
	}

	// Otherwise, generate from agentSpec + harnessCfg
	return h.GenerateFrameworkConfig(agentSpec, harnessCfg)
}

// checkProcessHealth checks health of Framework processes.
func checkProcessHealth(ctx context.Context, h *adk.ADKHandler, processMode handler.ProcessModeType) {
	status, err := h.GetFrameworkStatus(ctx)
	if err != nil {
		log.Printf("Error getting Framework status: %v", err)
		return
	}

	if !status.Running {
		log.Printf("Warning: Framework process not running")
		return
	}

	log.Printf("Framework status: running=%v, agents=%d, health=%s", status.Running, status.AgentCount, status.Health)
}

// cleanup stops all agents and Framework processes.
func cleanup(ctx context.Context, h *adk.ADKHandler, loadedAgents map[string]bool) error {
	log.Printf("Stopping all agents...")

	for agentName := range loadedAgents {
		if err := h.StopAgent(ctx, agentName); err != nil {
			log.Printf("Error stopping agent %s: %v", agentName, err)
		}
	}

	log.Printf("Stopping Framework processes...")
	if err := h.StopFramework(ctx); err != nil {
		log.Printf("Error stopping Framework: %v", err)
	}

	return nil
}