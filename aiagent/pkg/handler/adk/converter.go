// Package adk provides configuration conversion for ADK-Go framework.
// This converter transforms AIAgentSpec + HarnessConfig into ADK YAML config.
package adk

import (
	"gopkg.in/yaml.v3"

	"aiagent/api/v1"
	"aiagent/pkg/handler"
)

// ConfigConverter converts AIAgentSpec and HarnessConfig to ADK config.
type ConfigConverter struct{}

// NewConfigConverter creates a new config converter.
func NewConfigConverter() *ConfigConverter {
	return &ConfigConverter{}
}

// ============================================================
// Core Conversion Methods (called by Handler)
// ============================================================

// ConvertToADKConfig generates full agent.yaml from AIAgentSpec and HarnessConfig.
func (c *ConfigConverter) ConvertToADKConfig(spec *v1.AIAgentSpec, harnessCfg *handler.HarnessConfig) ([]byte, error) {
	config := &ADKAgentConfig{
		Name:        spec.Description,
		Description: spec.Description,
	}

	// Apply harness configuration
	if harnessCfg != nil {
		c.applyHarnessToConfig(config, harnessCfg)
	}

	// Apply spec overrides
	c.applySpecOverrides(config, spec.HarnessOverride)

	return c.MarshalYAML(config)
}

// ConvertAgentSpec generates AgentConfig for a single agent.
func (c *ConfigConverter) ConvertAgentSpec(spec *v1.AIAgentSpec, harnessCfg *handler.HarnessConfig) (*ADKAgentConfig, error) {
	config := &ADKAgentConfig{
		Name:        spec.Description,
		Description: spec.Description,
	}

	// Apply harness configuration
	if harnessCfg != nil {
		c.applyHarnessToConfig(config, harnessCfg)
	}

	// Apply spec overrides
	c.applySpecOverrides(config, spec.HarnessOverride)

	return config, nil
}

// ConvertHarnessConfig generates harness-specific config sections.
func (c *ConfigConverter) ConvertHarnessConfig(harnessCfg *handler.HarnessConfig) ([]byte, error) {
	if harnessCfg == nil {
		return nil, nil
	}

	harnessSection := &ADKHarnessSection{
		Model:   c.convertModelConfig(harnessCfg),
		MCP:     c.convertMCPConfig(harnessCfg),
		Memory:  c.convertMemoryConfig(harnessCfg),
		Sandbox: c.convertSandboxConfig(harnessCfg),
		Skills:  c.convertSkillsConfig(harnessCfg),
	}

	return c.MarshalYAML(harnessSection)
}

// ============================================================
// Harness Interface Conversion Methods
// ============================================================

// ConvertModelHarness converts ModelHarnessInterface to ADK model config.
func (c *ConfigConverter) ConvertModelHarness(harness handler.ModelHarnessInterface) ([]byte, error) {
	if harness == nil {
		return nil, nil
	}

	modelConfig := &ADKModelConfig{
		Provider:      harness.GetProvider(),
		DefaultModel:  harness.GetDefaultModel(),
		Endpoint:      harness.GetEndpoint(),
		AllowedModels: harness.GetAllowedModels(),
	}

	return c.MarshalYAML(modelConfig)
}

// ConvertMCPHarness converts MCPHarnessInterface to ADK MCP config.
func (c *ConfigConverter) ConvertMCPHarness(harness handler.MCPHarnessInterface) ([]byte, error) {
	if harness == nil {
		return nil, nil
	}

	mcpConfig := &ADKMCPConfig{
		RegistryType: harness.GetRegistryType(),
		Endpoint:     harness.GetEndpoint(),
		Servers:      c.convertMCPServers(harness.GetServers()),
	}

	return c.MarshalYAML(mcpConfig)
}

// ConvertMemoryHarness converts MemoryHarnessInterface to ADK memory config.
func (c *ConfigConverter) ConvertMemoryHarness(harness handler.MemoryHarnessInterface) ([]byte, error) {
	if harness == nil {
		return nil, nil
	}

	memoryConfig := &ADKMemoryConfig{
		Type:        harness.GetType(),
		Endpoint:    harness.GetEndpoint(),
		TTL:         int(harness.GetTTL()),
		Persistence: harness.IsPersistenceEnabled(),
	}

	return c.MarshalYAML(memoryConfig)
}

// ConvertSandboxHarness converts SandboxHarnessInterface to ADK sandbox config.
func (c *ConfigConverter) ConvertSandboxHarness(harness handler.SandboxHarnessInterface) ([]byte, error) {
	if harness == nil {
		return nil, nil
	}

	sandboxConfig := &ADKSandboxConfig{
		Mode:     string(harness.GetMode()),
		Endpoint: harness.GetEndpoint(),
		Timeout:  int(harness.GetTimeout()),
	}

	if limits := harness.GetResourceLimits(); limits != nil {
		sandboxConfig.Resources = &ADKResourceConfig{
			CPU:    limits.CPU,
			Memory: limits.Memory,
		}
	}

	return c.MarshalYAML(sandboxConfig)
}

// ConvertSkillsHarness converts SkillsHarnessInterface to ADK skills config.
func (c *ConfigConverter) ConvertSkillsHarness(harness handler.SkillsHarnessInterface) ([]byte, error) {
	if harness == nil {
		return nil, nil
	}

	skillsConfig := &ADKSkillsConfig{
		HubType:  harness.GetHubType(),
		Endpoint: harness.GetEndpoint(),
		Skills:   c.convertSkillItems(harness.GetSkills()),
	}

	return c.MarshalYAML(skillsConfig)
}

// ============================================================
// Helper Conversion Methods
// ============================================================

// applyHarnessToConfig applies harness config to ADK agent config.
func (c *ConfigConverter) applyHarnessToConfig(cfg *ADKAgentConfig, harnessCfg *handler.HarnessConfig) {
	if harnessCfg.Model != nil {
		cfg.Model = harnessCfg.Model.DefaultModel
		cfg.AllowedModels = []string{}
		for _, m := range harnessCfg.Model.Models {
			if m.Allowed {
				cfg.AllowedModels = append(cfg.AllowedModels, m.Name)
			}
		}
	}

	if harnessCfg.Skills != nil {
		for _, s := range harnessCfg.Skills.Skills {
			if s.Allowed {
				cfg.Tools = append(cfg.Tools, s.Name)
			}
		}
	}

	if harnessCfg.MCP != nil {
		for _, s := range harnessCfg.MCP.Servers {
			if s.Allowed {
				cfg.Tools = append(cfg.Tools, s.Name)
			}
		}
	}
}

// applySpecOverrides applies spec overrides to ADK config.
func (c *ConfigConverter) applySpecOverrides(cfg *ADKAgentConfig, override v1.HarnessOverrideSpec) {
	// Apply model overrides
	for _, modelOverride := range override.Model {
		if len(modelOverride.AllowedModels) > 0 {
			cfg.AllowedModels = modelOverride.AllowedModels
		}
	}

	// Apply skills overrides
	for _, skillsOverride := range override.Skills {
		cfg.Tools = append(cfg.Tools, skillsOverride.AllowedSkills...)
	}
}

// convertModelConfig converts harness model config to ADK model section.
func (c *ConfigConverter) convertModelConfig(harnessCfg *handler.HarnessConfig) *ADKModelConfig {
	if harnessCfg == nil || harnessCfg.Model == nil {
		return nil
	}

	return &ADKModelConfig{
		Provider:      harnessCfg.Model.Provider,
		DefaultModel:  harnessCfg.Model.DefaultModel,
		Endpoint:      harnessCfg.Model.Endpoint,
		AllowedModels: c.extractAllowedModels(harnessCfg.Model.Models),
	}
}

// convertMCPConfig converts harness MCP config to ADK MCP section.
func (c *ConfigConverter) convertMCPConfig(harnessCfg *handler.HarnessConfig) *ADKMCPConfig {
	if harnessCfg == nil || harnessCfg.MCP == nil {
		return nil
	}

	return &ADKMCPConfig{
		RegistryType: harnessCfg.MCP.RegistryType,
		Endpoint:     harnessCfg.MCP.Endpoint,
		Servers:      c.convertMCPServersFromSpec(harnessCfg.MCP.Servers),
	}
}

// convertMemoryConfig converts harness memory config to ADK memory section.
func (c *ConfigConverter) convertMemoryConfig(harnessCfg *handler.HarnessConfig) *ADKMemoryConfig {
	if harnessCfg == nil || harnessCfg.Memory == nil {
		return nil
	}

	return &ADKMemoryConfig{
		Type:        harnessCfg.Memory.Type,
		Endpoint:    harnessCfg.Memory.Endpoint,
		TTL:         int(harnessCfg.Memory.TTL),
		Persistence: harnessCfg.Memory.PersistenceEnabled,
	}
}

// convertSandboxConfig converts harness sandbox config to ADK sandbox section.
func (c *ConfigConverter) convertSandboxConfig(harnessCfg *handler.HarnessConfig) *ADKSandboxConfig {
	if harnessCfg == nil || harnessCfg.Sandbox == nil {
		return nil
	}

	return &ADKSandboxConfig{
		Mode:     string(harnessCfg.Sandbox.Mode),
		Endpoint: harnessCfg.Sandbox.Endpoint,
		Timeout:  int(harnessCfg.Sandbox.Timeout),
	}
}

// convertSkillsConfig converts harness skills config to ADK skills section.
func (c *ConfigConverter) convertSkillsConfig(harnessCfg *handler.HarnessConfig) *ADKSkillsConfig {
	if harnessCfg == nil || harnessCfg.Skills == nil {
		return nil
	}

	return &ADKSkillsConfig{
		HubType:  harnessCfg.Skills.HubType,
		Endpoint: harnessCfg.Skills.Endpoint,
		Skills:   c.convertSkillItemsFromSpec(harnessCfg.Skills.Skills),
	}
}

// extractAllowedModels extracts allowed model names from model configs.
func (c *ConfigConverter) extractAllowedModels(models []v1.ModelConfig) []string {
	result := []string{}
	for _, m := range models {
		if m.Allowed {
			result = append(result, m.Name)
		}
	}
	return result
}

// convertMCPServers converts MCPServerInfo to ADK MCP servers.
func (c *ConfigConverter) convertMCPServers(servers []handler.MCPServerInfo) []ADKMCPServerConfig {
	result := []ADKMCPServerConfig{}
	for _, s := range servers {
		result = append(result, ADKMCPServerConfig{
			Name:    s.Name,
			Type:    s.Type,
			Command: s.Command,
			Args:    s.Args,
			URL:     s.Endpoint,
		})
	}
	return result
}

// convertMCPServersFromSpec converts spec MCP servers to ADK MCP servers.
func (c *ConfigConverter) convertMCPServersFromSpec(servers []v1.MCPServerConfig) []ADKMCPServerConfig {
	result := []ADKMCPServerConfig{}
	for _, s := range servers {
		result = append(result, ADKMCPServerConfig{
			Name:    s.Name,
			Type:    s.Type,
			Command: s.Command,
			Args:    s.Args,
			URL:     s.URL,
		})
	}
	return result
}

// convertSkillItems converts SkillInfo to ADK skill items.
func (c *ConfigConverter) convertSkillItems(skills []handler.SkillInfo) []ADKSkillItem {
	result := []ADKSkillItem{}
	for _, s := range skills {
		result = append(result, ADKSkillItem{
			Name:    s.Name,
			Version: s.Version,
			Path:    s.Path,
		})
	}
	return result
}

// convertSkillItemsFromSpec converts spec skill configs to ADK skill items.
func (c *ConfigConverter) convertSkillItemsFromSpec(skills []v1.SkillConfig) []ADKSkillItem {
	result := []ADKSkillItem{}
	for _, s := range skills {
		result = append(result, ADKSkillItem{
			Name:    s.Name,
			Version: s.Version,
		})
	}
	return result
}

// MarshalYAML converts struct to YAML bytes.
func (c *ConfigConverter) MarshalYAML(v interface{}) ([]byte, error) {
	return yaml.Marshal(v)
}

// ============================================================
// ADK Config Types
// ============================================================

// ADKAgentConfig represents ADK agent configuration.
type ADKAgentConfig struct {
	Name          string            `yaml:"name"`
	Description   string            `yaml:"description"`
	Model         string            `yaml:"model,omitempty"`
	AllowedModels []string          `yaml:"allowedModels,omitempty"`
	Instruction   string            `yaml:"instruction,omitempty"`
	Tools         []string          `yaml:"tools,omitempty"`
	SubAgents     []*ADKAgentConfig `yaml:"subAgents,omitempty"`
	Custom        map[string]any    `yaml:"custom,omitempty"`
}

// ADKHarnessSection represents harness configuration section.
type ADKHarnessSection struct {
	Model   *ADKModelConfig  `yaml:"model,omitempty"`
	MCP     *ADKMCPConfig    `yaml:"mcp,omitempty"`
	Memory  *ADKMemoryConfig `yaml:"memory,omitempty"`
	Sandbox *ADKSandboxConfig `yaml:"sandbox,omitempty"`
	Skills  *ADKSkillsConfig `yaml:"skills,omitempty"`
}

// ADKModelConfig represents model configuration.
type ADKModelConfig struct {
	Provider      string   `yaml:"provider"`
	DefaultModel  string   `yaml:"defaultModel"`
	Endpoint      string   `yaml:"endpoint,omitempty"`
	AllowedModels []string `yaml:"allowedModels,omitempty"`
}

// ADKMCPConfig represents MCP configuration.
type ADKMCPConfig struct {
	RegistryType string              `yaml:"registryType"`
	Endpoint     string              `yaml:"endpoint,omitempty"`
	Servers      []ADKMCPServerConfig `yaml:"servers,omitempty"`
}

// ADKMCPServerConfig represents MCP server configuration.
type ADKMCPServerConfig struct {
	Name    string   `yaml:"name"`
	Type    string   `yaml:"type,omitempty"`
	Command string   `yaml:"command,omitempty"`
	Args    []string `yaml:"args,omitempty"`
	URL     string   `yaml:"url,omitempty"`
}

// ADKMemoryConfig represents memory configuration.
type ADKMemoryConfig struct {
	Type        string `yaml:"type"`
	Endpoint    string `yaml:"endpoint,omitempty"`
	TTL         int    `yaml:"ttl,omitempty"`
	Persistence bool   `yaml:"persistence,omitempty"`
}

// ADKSandboxConfig represents sandbox configuration.
type ADKSandboxConfig struct {
	Mode      string            `yaml:"mode,omitempty"`
	Endpoint  string            `yaml:"endpoint,omitempty"`
	Timeout   int               `yaml:"timeout,omitempty"`
	Resources *ADKResourceConfig `yaml:"resources,omitempty"`
}

// ADKResourceConfig represents resource limits.
type ADKResourceConfig struct {
	CPU    string `yaml:"cpu,omitempty"`
	Memory string `yaml:"memory,omitempty"`
}

// ADKSkillsConfig represents skills configuration.
type ADKSkillsConfig struct {
	HubType  string         `yaml:"hubType,omitempty"`
	Endpoint string         `yaml:"endpoint,omitempty"`
	Skills   []ADKSkillItem `yaml:"skills,omitempty"`
}

// ADKSkillItem represents a skill item.
type ADKSkillItem struct {
	Name    string `yaml:"name"`
	Version string `yaml:"version,omitempty"`
	Path    string `yaml:"path,omitempty"`
}