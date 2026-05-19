// Package base provides base functionality for Agent Handlers.
// This file implements Harness configuration loading from mounted ConfigMaps.
package base

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"aiagent/api/v1"
)

// HarnessLoader loads Harness configurations from mounted directories.
type HarnessLoader struct {
	harnessDir string
}

// NewHarnessLoader creates a new Harness loader.
func NewHarnessLoader(harnessDir string) *HarnessLoader {
	return &HarnessLoader{
		harnessDir: harnessDir,
	}
}

// LoadHarnessConfigs loads all Harness configurations from the mounted directory.
// Expected structure (new format - framework-agnostic):
//   /etc/harness/<harness-name>/
//     ├── harness-name        (contains harness CRD name)
//     ├── harness-type        (contains harness type: model, mcp, etc.)
//     └── harness.json        (raw Harness spec JSON from Controller)
//
// Legacy structure (still supported for backwards compatibility):
//   /etc/harness/<harness-name>/
//     ├── harness-name        (contains harness CRD name)
//     ├── harness-type        (contains harness type)
//     └── model.yaml          (for model harness)
//     └── mcp.yaml            (for mcp harness)
//     └── memory.yaml         (for memory harness)
//     └── sandbox.yaml        (for sandbox harness)
//     └── skills.yaml         (for skills harness)
func (l *HarnessLoader) LoadHarnessConfigs() ([]*v1.HarnessSpec, error) {
	specs := []*v1.HarnessSpec{}

	entries, err := os.ReadDir(l.harnessDir)
	if err != nil {
		if os.IsNotExist(err) {
			return specs, nil // Directory doesn't exist, return empty
		}
		return nil, fmt.Errorf("failed to read harness directory: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue // Skip non-directory entries
		}

		harnessPath := filepath.Join(l.harnessDir, entry.Name())
		spec, err := l.loadHarnessSpec(harnessPath, entry.Name())
		if err != nil {
			continue // Skip invalid harness configs
		}
		specs = append(specs, spec)
	}

	return specs, nil
}

// loadHarnessSpec loads a single Harness spec from a directory.
// Priority: 1) harness.json (new format, framework-agnostic)
//           2) Individual YAML files (legacy format, for backwards compatibility)
func (l *HarnessLoader) loadHarnessSpec(harnessPath string, harnessName string) (*v1.HarnessSpec, error) {
	// First, try to load harness.json (new format from Controller)
	jsonPath := filepath.Join(harnessPath, "harness.json")
	jsonData, err := os.ReadFile(jsonPath)
	if err == nil {
		// Parse JSON format
		var spec v1.HarnessSpec
		if err := json.Unmarshal(jsonData, &spec); err != nil {
			return nil, fmt.Errorf("failed to parse harness.json: %w", err)
		}
		return &spec, nil
	}

	// Fall back to legacy format: read harness-type and load type-specific YAML
	typePath := filepath.Join(harnessPath, "harness-type")
	harnessTypeBytes, err := os.ReadFile(typePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read harness-type: %w", err)
	}
	harnessType := strings.TrimSpace(string(harnessTypeBytes))

	spec := &v1.HarnessSpec{
		Type: v1.HarnessType(harnessType),
	}

	// Load type-specific configuration based on harness type (legacy YAML format)
	switch v1.HarnessType(harnessType) {
	case v1.HarnessTypeModel:
		modelSpec, err := l.loadModelSpec(harnessPath)
		if err != nil {
			return nil, err
		}
		spec.Model = modelSpec

	case v1.HarnessTypeMCP:
		mcpSpec, err := l.loadMCPSpec(harnessPath)
		if err != nil {
			return nil, err
		}
		spec.MCP = mcpSpec

	case v1.HarnessTypeMemory:
		memorySpec, err := l.loadMemorySpec(harnessPath)
		if err != nil {
			return nil, err
		}
		spec.Memory = memorySpec

	case v1.HarnessTypeSandbox:
		sandboxSpec, err := l.loadSandboxSpec(harnessPath)
		if err != nil {
			return nil, err
		}
		spec.Sandbox = sandboxSpec

	case v1.HarnessTypeSkills:
		skillsSpec, err := l.loadSkillsSpec(harnessPath)
		if err != nil {
			return nil, err
		}
		spec.Skills = skillsSpec

	case v1.HarnessTypeKnowledge:
		knowledgeSpec, err := l.loadKnowledgeSpec(harnessPath)
		if err != nil {
			return nil, err
		}
		spec.Knowledge = knowledgeSpec

	default:
		return nil, fmt.Errorf("unknown harness type: %s", harnessType)
	}

	return spec, nil
}

// loadModelSpec loads Model harness configuration.
func (l *HarnessLoader) loadModelSpec(harnessPath string) (*v1.ModelHarnessSpec, error) {
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

// loadMCPSpec loads MCP harness configuration.
func (l *HarnessLoader) loadMCPSpec(harnessPath string) (*v1.MCPHarnessSpec, error) {
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

// loadMemorySpec loads Memory harness configuration.
func (l *HarnessLoader) loadMemorySpec(harnessPath string) (*v1.MemoryHarnessSpec, error) {
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

// loadSandboxSpec loads Sandbox harness configuration.
func (l *HarnessLoader) loadSandboxSpec(harnessPath string) (*v1.SandboxHarnessSpec, error) {
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

// loadSkillsSpec loads Skills harness configuration.
func (l *HarnessLoader) loadSkillsSpec(harnessPath string) (*v1.SkillsHarnessSpec, error) {
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

// loadKnowledgeSpec loads Knowledge harness configuration.
func (l *HarnessLoader) loadKnowledgeSpec(harnessPath string) (*v1.KnowledgeHarnessSpec, error) {
	configPath := filepath.Join(harnessPath, "knowledge.yaml")
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read knowledge.yaml: %w", err)
	}

	var spec v1.KnowledgeHarnessSpec
	if err := yaml.Unmarshal(data, &spec); err != nil {
		return nil, fmt.Errorf("failed to parse knowledge.yaml: %w", err)
	}

	return &spec, nil
}

// AgentIndexLoader loads AgentIndex configuration.
type AgentIndexLoader struct {
	indexPath string
}

// NewAgentIndexLoader creates a new AgentIndex loader.
func NewAgentIndexLoader(indexPath string) *AgentIndexLoader {
	return &AgentIndexLoader{
		indexPath: indexPath,
	}
}

// AgentIndex represents the agent index structure.
type AgentIndex struct {
	Agents []AgentIndexEntry `yaml:"agents"`
}

// AgentIndexEntry represents an entry in the agent index.
type AgentIndexEntry struct {
	Name      string `yaml:"name"`
	Namespace string `yaml:"namespace"`
	ConfigMap string `yaml:"configMap"`
	Phase     string `yaml:"phase"`
	UID       string `yaml:"uid,omitempty"`
}

// LoadAgentIndex loads the AgentIndex configuration.
func (l *AgentIndexLoader) LoadAgentIndex() (*AgentIndex, error) {
	data, err := os.ReadFile(l.indexPath)
	if err != nil {
		if os.IsNotExist(err) {
			return &AgentIndex{Agents: []AgentIndexEntry{}}, nil
		}
		return nil, fmt.Errorf("failed to read agent-index.yaml: %w", err)
	}

	var index AgentIndex
	if err := yaml.Unmarshal(data, &index); err != nil {
		return nil, fmt.Errorf("failed to parse agent-index.yaml: %w", err)
	}

	return &index, nil
}

// LoadAgentConfig loads an agent's configuration from its ConfigMap mount path.
func LoadAgentConfig(agentName string, configDir string) ([]byte, error) {
	// Expected path: /etc/agent-config/agent/<agent-name>/agent.yaml
	configPath := filepath.Join(configDir, "agent", agentName, "agent.yaml")
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read agent config for %s: %w", agentName, err)
	}
	return data, nil
}

// Watcher watches for changes in a directory.
type Watcher struct {
	path      string
	interval  int
	callback  func()
	stopChan  chan struct{}
}

// NewWatcher creates a new directory watcher.
func NewWatcher(path string, intervalSeconds int, callback func()) *Watcher {
	return &Watcher{
		path:     path,
		interval: intervalSeconds,
		callback: callback,
		stopChan: make(chan struct{}),
	}
}

// Start starts watching the directory for changes.
func (w *Watcher) Start() {
	go func() {
		// Initial check
		w.callback()

		ticker := time.NewTicker(time.Duration(w.interval) * time.Second)
		defer ticker.Stop()

		lastModTime := w.getLatestModTime()

		for {
			select {
			case <-ticker.C:
				currentModTime := w.getLatestModTime()
				if currentModTime.After(lastModTime) {
					lastModTime = currentModTime
					w.callback()
				}
			case <-w.stopChan:
				return
			}
		}
	}()
}

// Stop stops the watcher.
func (w *Watcher) Stop() {
	close(w.stopChan)
}

// getLatestModTime returns the latest modification time in the directory.
func (w *Watcher) getLatestModTime() time.Time {
	latest := time.Time{}
	entries, err := os.ReadDir(w.path)
	if err != nil {
		return latest
	}

	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if info.ModTime().After(latest) {
			latest = info.ModTime()
		}
	}

	return latest
}