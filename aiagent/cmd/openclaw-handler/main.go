// OpenClaw Handler entry point.
// Process Manager for OpenClaw Framework.
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
// │  Handler Container (Gateway Manager)                            │
// │    - Reads agent-index.yaml to discover agents                  │
// │    - Reads agent-config.json for each agent                     │
// │    - Starts Gateway process for each AIAgent                    │
// │    - Each Gateway manages internal sub-agents                   │
// │    - No K8s API calls needed                                    │
// │                                                                 │
// │  Gateway Process #1 (AIAgent: openclaw-1)                       │
// │    Port: 18789                                                  │
// │    Internal agents: weather, calculator, assistant (invisible) │
// │                                                                 │
// │  Gateway Process #2 (AIAgent: openclaw-2)                       │
// │    Port: 18790                                                  │
// │    Internal agents: weather, calculator, assistant (invisible) │
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
	"aiagent/pkg/handler/openclaw"
	"aiagent/pkg/harness"
)

var (
	gatewayURL   = flag.String("gateway", "", "OpenClaw Gateway URL (e.g., http://localhost:18789)")
	workDir      = flag.String("workdir", "", "Shared work directory (e.g., /shared/workdir)")
	configDir    = flag.String("configdir", "", "Shared config directory (e.g., /shared/config)")
	frameworkBin = flag.String("framework", "", "Framework binary path (ImageVolume: /framework-rootfs/usr/local/bin/openclaw)")
	harnessDir   = flag.String("harness", "", "Harness config directory (e.g., /etc/harness)")
	agentConfigDir = flag.String("agentconfig", "", "Agent config directory (e.g., /etc/agent-config)")
	namespace    = flag.String("namespace", "", "Namespace for agent configs")
	basePort     = flag.Int("baseport", 18789, "Base Gateway port (each instance gets port + offset)")
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

	// Get Gateway URL from environment or flag
	gwURL := *gatewayURL
	if gwURL == "" {
		gwURL = os.Getenv("OPENCLAW_GATEWAY_URL")
		if gwURL == "" {
			gwURL = "http://localhost:18789"
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

	// Get framework binary path from environment or flag
	fwBin := *frameworkBin
	if fwBin == "" {
		fwBin = os.Getenv("FRAMEWORK_BIN")
		if fwBin == "" {
			fwBin = "/framework-rootfs/usr/local/bin/openclaw"
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
	if ns == "" {
		ns = "aiagent-system" // Default namespace
	}

	// Get base port from environment or flag
	baseP := *basePort
	if bp := os.Getenv("BASE_GATEWAY_PORT"); bp != "" {
		if parsed, err := parsePort(bp); err == nil {
			baseP = parsed
		}
	}

	log.Printf("OpenClaw Handler starting...")
	log.Printf("Gateway URL: %s", gwURL)
	log.Printf("Work Directory: %s", wd)
	log.Printf("Config Directory: %s", cfgDir)
	log.Printf("Framework Binary: %s", fwBin)
	log.Printf("Harness Directory: %s", hDir)
	log.Printf("Agent Config Directory: %s", agCfgDir)
	log.Printf("Namespace: %s", ns)
	log.Printf("Base Gateway Port: %d", baseP)

	// Create Handler configuration
	handlerCfg := &handler.HandlerConfig{
		Type:         handler.HandlerTypeOpenClaw,
		FrameworkBin: fwBin,
		WorkDir:      wd,
		ConfigDir:    cfgDir,
		DebugMode:    *debug,
	}

	// Create OpenClaw Handler
	h := openclaw.NewOpenClawHandler(handlerCfg)
	h.SetBasePort(baseP)

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
	if err := runHandler(ctx, h, hDir, agCfgDir, ns, wd, cfgDir, baseP); err != nil {
		log.Fatalf("Handler error: %v", err)
	}

	log.Printf("OpenClaw Handler shutdown complete")
}

func parsePort(s string) (int, error) {
	var port int
	_, err := fmt.Sscanf(s, "%d", &port)
	return port, err
}

// runHandler runs the handler service loop with Gateway management from hostPath.
func runHandler(ctx context.Context, h *openclaw.OpenClawHandler, harnessDir string, agentConfigDir string, namespace string, workDir string, configDir string, basePort int) error {
	log.Printf("Initializing OpenClaw Handler service...")

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

	// 3. Build HarnessConfig from HarnessManager
	harnessCfg := buildHarnessConfig(harnessMgr)

	// 4. Prepare config directory
	configDir = filepath.Join(workDir, "openclaw-config")
	os.MkdirAll(configDir, 0755)

	// 5. Watch AgentIndex for changes (agent-index.yaml written by Config Daemon)
	// Path structure: mount is /var/lib/aiagent/configs/<namespace> -> /etc/agent-config
	// So agent-index.yaml is directly at /etc/agent-config/agent-index.yaml
	agentIndexPath := filepath.Join(agentConfigDir, "agent-index.yaml")
	log.Printf("Watching AgentIndex at: %s", agentIndexPath)

	// Track loaded gateways
	loadedGateways := make(map[string]bool)
	portAssignments := make(map[string]int)
	nextPort := basePort

	// Poll interval
	pollInterval := 5 * time.Second
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	// Initial load
	loadGatewaysFromIndex(ctx, h, agentIndexPath, agentConfigDir, workDir, configDir, harnessCfg, loadedGateways, portAssignments, &nextPort)

	for {
		select {
		case <-ctx.Done():
			log.Printf("Shutting down handler...")
			return cleanup(ctx, h, loadedGateways)

		case <-ticker.C:
			// Poll for changes
			loadGatewaysFromIndex(ctx, h, agentIndexPath, agentConfigDir, workDir, configDir, harnessCfg, loadedGateways, portAssignments, &nextPort)

			// Check Gateway health
			checkGatewayHealth(ctx, h)
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
		return inferHarnessType(harnessPath, harnessName)
	}
	harnessType := string(typeData)

	spec := &v1.HarnessSpec{
		Type: v1.HarnessType(harnessType),
	}

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

// loadGatewaysFromIndex loads gateways from agent-index.yaml (written by Config Daemon).
func loadGatewaysFromIndex(ctx context.Context, h *openclaw.OpenClawHandler, indexPath string, agentConfigDir string, workDir string, configDir string, harnessCfg *handler.HarnessConfig, loadedGateways map[string]bool, portAssignments map[string]int, nextPort *int) {
	// Read agent index
	data, err := os.ReadFile(indexPath)
	if err != nil {
		if os.IsNotExist(err) {
			if *debug {
				log.Printf("Agent index not found at %s, waiting for Config Daemon", indexPath)
			}
			return
		}
		log.Printf("Error reading agent index: %v", err)
		return
	}

	var index AgentIndex
	if err := yaml.Unmarshal(data, &index); err != nil {
		log.Printf("Error parsing agent index: %v", err)
		return
	}

	log.Printf("AgentIndex contains %d agents", len(index.Agents))

	// Process each agent (each becomes a Gateway instance)
	for _, entry := range index.Agents {
		if loadedGateways[entry.Name] {
			continue // Already loaded
		}

		if entry.Phase != "Running" && entry.Phase != "Pending" {
			continue
		}

		log.Printf("Loading Gateway for agent: %s (phase: %s)", entry.Name, entry.Phase)

		// Load agent config from hostPath (Config Daemon writes to /etc/agent-config/<agent-name>/)
		agentSpec, agentConfig, err := loadAgentFromHostPath(entry.Name, agentConfigDir)
		if err != nil {
			log.Printf("Error loading agent %s from hostPath: %v", entry.Name, err)
			continue
		}

		// Generate OpenClaw JSON config using agentConfig if available
		configData, err := generateGatewayConfig(h, agentSpec, agentConfig, harnessCfg)
		if err != nil {
			log.Printf("Error generating config for %s: %v", entry.Name, err)
			continue
		}

		// Write config file
		configPath := filepath.Join(configDir, fmt.Sprintf("%s.json", entry.Name))
		if err := os.WriteFile(configPath, configData, 0644); err != nil {
			log.Printf("Error writing config for %s: %v", entry.Name, err)
			continue
		}

		log.Printf("Generated config for %s at %s", entry.Name, configPath)

		// Assign port
		port := *nextPort
		*nextPort = *nextPort + 1
		portAssignments[entry.Name] = port

		// Start Gateway instance
		if err := h.StartFrameworkInstance(ctx, entry.Name, configPath); err != nil {
			log.Printf("Error starting Gateway for %s: %v", entry.Name, err)
			continue
		}

		log.Printf("Started Gateway for %s on port %d", entry.Name, port)
		loadedGateways[entry.Name] = true
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
		if *debug {
			log.Printf("Agent meta not found for %s: %v", agentName, err)
		}
		agentSpec := &v1.AIAgentSpec{
			Description: agentName,
			RuntimeRef:  v1.RuntimeReference{Type: "openclaw"},
		}
		return agentSpec, nil, nil
	}

	var meta AgentMeta
	if err := yaml.Unmarshal(metaData, &meta); err != nil {
		log.Printf("Error parsing agent meta for %s: %v", agentName, err)
		agentSpec := &v1.AIAgentSpec{
			Description: agentName,
			RuntimeRef:  v1.RuntimeReference{Type: "openclaw"},
		}
		return agentSpec, nil, nil
	}

	// Create AIAgentSpec from meta
	agentSpec := &v1.AIAgentSpec{
		Description: meta.Name,
		RuntimeRef:  v1.RuntimeReference{
			Type: "openclaw",
			Name: meta.Runtime,
		},
	}

	// Read agent-config.json
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

// generateGatewayConfig generates Gateway config using agentConfig or agentSpec.
func generateGatewayConfig(h *openclaw.OpenClawHandler, agentSpec *v1.AIAgentSpec, agentConfig map[string]interface{}, harnessCfg *handler.HarnessConfig) ([]byte, error) {
	// If agentConfig is provided, use it directly
	if agentConfig != nil {
		return json.MarshalIndent(agentConfig, "", "  ")
	}

	// Otherwise, generate from agentSpec + harnessCfg
	return h.GenerateFrameworkConfig(agentSpec, harnessCfg)
}

// checkGatewayHealth checks health of Gateway processes.
func checkGatewayHealth(ctx context.Context, h *openclaw.OpenClawHandler) {
	status, err := h.GetFrameworkStatus(ctx)
	if err != nil {
		log.Printf("Error getting Gateway status: %v", err)
		return
	}

	log.Printf("Gateway status: running=%v, instances=%d, health=%s", status.Running, status.InstanceCount, status.Health)
}

// cleanup stops all Gateway processes.
func cleanup(ctx context.Context, h *openclaw.OpenClawHandler, loadedGateways map[string]bool) error {
	log.Printf("Stopping all Gateways...")

	for gatewayName := range loadedGateways {
		if err := h.StopAgent(ctx, gatewayName); err != nil {
			log.Printf("Error stopping Gateway %s: %v", gatewayName, err)
		}
	}

	log.Printf("Stopping Framework...")
	if err := h.StopFramework(ctx); err != nil {
		log.Printf("Error stopping Framework: %v", err)
	}

	return nil
}