// Package openclaw provides configuration conversion for OpenClaw framework.
// This converter transforms AIAgentSpec + HarnessConfig into OpenClaw JSON config.
//
// OpenClaw Config Structure (from src/config/types.openclaw.ts):
// ┌─────────────────────────────────────────────────────────────────┐
// │ openclaw.json                                                    │
// ├─────────────────────────────────────────────────────────────────┤
// │ {                                                                │
// │   "agents": {                                                    │
// │     "defaults": { ... },  // Default agent configuration         │
// │     "list": [ ... ]       // Agent instances                     │
// │   },                                                             │
// │   "models": {                    // Model providers              │
// │     "providers": {                                               │
// │       "deepseek": { "baseUrl": "...", "apiKey": "..." }          │
// │     }                                                            │
// │   },                                                             │
// │   "skills": { ... },             // Skills configuration         │
// │   "tools": { ... },              // Tools configuration          │
// │   "memory": { ... },             // Memory configuration         │
// │   "gateway": { ... },            // Gateway configuration        │
// │   "channels": { ... }            // Channel configuration        │
// │ }                                                                │
// └─────────────────────────────────────────────────────────────────┘
package openclaw

import (
	"encoding/json"

	"aiagent/api/v1"
	"aiagent/pkg/handler"
)

// ConfigConverter converts AIAgentSpec and HarnessConfig to OpenClaw config.
type ConfigConverter struct{}

// NewConfigConverter creates a new config converter.
func NewConfigConverter() *ConfigConverter {
	return &ConfigConverter{}
}

// ============================================================
// Core Conversion Methods (called by Handler)
// ============================================================

// ConvertToOpenClawConfig generates full openclaw.json from AIAgentSpec and HarnessConfig.
// This is the primary method called by GenerateFrameworkConfig.
func (c *ConfigConverter) ConvertToOpenClawConfig(spec *v1.AIAgentSpec, harnessCfg *handler.HarnessConfig) ([]byte, error) {
	config := &OpenClawConfig{
		Agents:  &AgentsConfig{},
		Gateway: &GatewayConfig{
			Mode: "local",
		},
	}

	// Convert agent defaults from harness
	if harnessCfg != nil {
		config.Agents.Defaults = c.convertAgentDefaults(harnessCfg)
		config.Models = c.convertModelsConfig(harnessCfg)
		config.Skills = c.convertSkillsConfig(harnessCfg)
		config.Memory = c.convertMemoryConfig(harnessCfg)
	}

	// Convert agent from spec
	agentCfg, err := c.ConvertAgentSpec(spec, harnessCfg)
	if err != nil {
		return nil, err
	}
	config.Agents.List = []*AgentConfig{agentCfg}

	return json.MarshalIndent(config, "", "  ")
}

// ConvertAgentSpec generates AgentConfig section for a single agent.
// This is called by GenerateAgentConfig.
func (c *ConfigConverter) ConvertAgentSpec(spec *v1.AIAgentSpec, harnessCfg *handler.HarnessConfig) (*AgentConfig, error) {
	agentCfg := &AgentConfig{
		ID:          spec.Description,
		Name:        spec.Description,
		Description: spec.Description,
	}

	// Apply model from harness
	if harnessCfg != nil && harnessCfg.Model != nil {
		agentCfg.Model = &AgentModelConfig{
			Primary: harnessCfg.Model.DefaultModel,
		}
	}

	// Apply skills from harness
	if harnessCfg != nil && harnessCfg.Skills != nil {
		for _, skill := range harnessCfg.Skills.Skills {
			if skill.Allowed {
				agentCfg.Skills = append(agentCfg.Skills, skill.Name)
			}
		}
	}

	// Apply overrides from spec
	c.applySpecOverrides(agentCfg, spec.HarnessOverride)

	return agentCfg, nil
}

// ConvertHarnessConfig generates harness-specific config sections.
// This is called by GenerateHarnessConfig.
func (c *ConfigConverter) ConvertHarnessConfig(harnessCfg *handler.HarnessConfig) ([]byte, error) {
	harnessSection := &HarnessSection{
		Model:   c.convertModelsConfig(harnessCfg),
		Skills:  c.convertSkillsConfig(harnessCfg),
		Memory:  c.convertMemoryConfig(harnessCfg),
		MCP:     c.convertMCPConfig(harnessCfg),
		Sandbox: c.convertSandboxConfig(harnessCfg),
	}

	return json.MarshalIndent(harnessSection, "", "  ")
}

// ============================================================
// Harness Interface Conversion Methods
// ============================================================

// ConvertModelHarness converts ModelHarnessInterface to OpenClaw models config.
func (c *ConfigConverter) ConvertModelHarness(harness handler.ModelHarnessInterface) ([]byte, error) {
	if harness == nil {
		return nil, nil
	}

	modelConfig := &ModelsConfig{
		Providers: map[string]*ModelProviderConfig{},
	}

	provider := harness.GetProvider()
	modelConfig.Providers[provider] = &ModelProviderConfig{
		BaseURL: harness.GetEndpoint(),
		APIKey:  harness.GetAPIKeyRef(),
		Models:  c.convertModelList(harness.GetAllowedModels()),
	}

	return json.MarshalIndent(modelConfig, "", "  ")
}

// ConvertMCPHarness converts MCPHarnessInterface to OpenClaw MCP config.
func (c *ConfigConverter) ConvertMCPHarness(harness handler.MCPHarnessInterface) ([]byte, error) {
	if harness == nil {
		return nil, nil
	}

	mcpConfig := &MCPConfig{
		RegistryType: harness.GetRegistryType(),
		Endpoint:     harness.GetEndpoint(),
		Servers:      c.convertMCPServers(harness.GetServers()),
	}

	return json.MarshalIndent(mcpConfig, "", "  ")
}

// ConvertMemoryHarness converts MemoryHarnessInterface to OpenClaw memory config.
func (c *ConfigConverter) ConvertMemoryHarness(harness handler.MemoryHarnessInterface) ([]byte, error) {
	if harness == nil {
		return nil, nil
	}

	memoryConfig := &MemoryConfig{
		Type:         harness.GetType(),
		Endpoint:     harness.GetEndpoint(),
		TTL:          harness.GetTTL(),
		Persistence:  harness.IsPersistenceEnabled(),
	}

	return json.MarshalIndent(memoryConfig, "", "  ")
}

// ConvertSandboxHarness converts SandboxHarnessInterface to OpenClaw sandbox config.
func (c *ConfigConverter) ConvertSandboxHarness(harness handler.SandboxHarnessInterface) ([]byte, error) {
	if harness == nil {
		return nil, nil
	}

	sandboxConfig := &SandboxConfig{
		Mode:     string(harness.GetMode()),
		Endpoint: harness.GetEndpoint(),
		Timeout:  harness.GetTimeout(),
	}

	if limits := harness.GetResourceLimits(); limits != nil {
		sandboxConfig.Resources = &ResourceConfig{
			CPU:    limits.CPU,
			Memory: limits.Memory,
		}
	}

	return json.MarshalIndent(sandboxConfig, "", "  ")
}

// ConvertSkillsHarness converts SkillsHarnessInterface to OpenClaw skills config.
func (c *ConfigConverter) ConvertSkillsHarness(harness handler.SkillsHarnessInterface) ([]byte, error) {
	if harness == nil {
		return nil, nil
	}

	skillsConfig := &SkillsConfig{
		HubType:  harness.GetHubType(),
		Endpoint: harness.GetEndpoint(),
		Skills:   c.convertSkillInfoList(harness.GetSkills()),
	}

	return json.MarshalIndent(skillsConfig, "", "  ")
}

// ============================================================
// Helper Conversion Methods
// ============================================================

// convertAgentDefaults converts harness to agent defaults configuration.
func (c *ConfigConverter) convertAgentDefaults(harnessCfg *handler.HarnessConfig) *AgentDefaultsConfig {
	defaults := &AgentDefaultsConfig{}

	if harnessCfg.Model != nil {
		defaults.Model = &AgentModelConfig{
			Primary: harnessCfg.Model.DefaultModel,
		}
	}

	if harnessCfg.Sandbox != nil {
		defaults.Sandbox = &AgentSandboxConfig{
			Enabled: harnessCfg.Sandbox.Mode != "",
			Mode:    string(harnessCfg.Sandbox.Mode),
		}
	}

	return defaults
}

// convertModelsConfig converts harness model config to OpenClaw models section.
func (c *ConfigConverter) convertModelsConfig(harnessCfg *handler.HarnessConfig) *ModelsConfig {
	if harnessCfg == nil || harnessCfg.Model == nil {
		return nil
	}

	modelsConfig := &ModelsConfig{
		Providers: map[string]*ModelProviderConfig{},
	}

	provider := harnessCfg.Model.Provider
	modelsConfig.Providers[provider] = &ModelProviderConfig{
		BaseURL: harnessCfg.Model.Endpoint,
		APIKey:  harnessCfg.Model.AuthSecretRef,
		Models:  c.convertModelListFromSpec(harnessCfg.Model.Models),
	}

	return modelsConfig
}

// convertSkillsConfig converts harness skills config to OpenClaw skills section.
func (c *ConfigConverter) convertSkillsConfig(harnessCfg *handler.HarnessConfig) *SkillsConfig {
	if harnessCfg == nil || harnessCfg.Skills == nil {
		return nil
	}

	return &SkillsConfig{
		HubType:  harnessCfg.Skills.HubType,
		Endpoint: harnessCfg.Skills.Endpoint,
		LocalPath: harnessCfg.Skills.LocalPath,
		Skills:   c.convertSkillItemsFromSpec(harnessCfg.Skills.Skills),
	}
}

// convertMemoryConfig converts harness memory config to OpenClaw memory section.
func (c *ConfigConverter) convertMemoryConfig(harnessCfg *handler.HarnessConfig) *MemoryConfig {
	if harnessCfg == nil || harnessCfg.Memory == nil {
		return nil
	}

	return &MemoryConfig{
		Type:        harnessCfg.Memory.Type,
		Endpoint:    harnessCfg.Memory.Endpoint,
		TTL:         int64(harnessCfg.Memory.TTL),
		Persistence: harnessCfg.Memory.PersistenceEnabled,
	}
}

// convertMCPConfig converts harness MCP config to OpenClaw MCP section.
func (c *ConfigConverter) convertMCPConfig(harnessCfg *handler.HarnessConfig) *MCPConfig {
	if harnessCfg == nil || harnessCfg.MCP == nil {
		return nil
	}

	return &MCPConfig{
		RegistryType: harnessCfg.MCP.RegistryType,
		Endpoint:     harnessCfg.MCP.Endpoint,
		Servers:      c.convertMCPServersFromSpec(harnessCfg.MCP.Servers),
	}
}

// convertSandboxConfig converts harness sandbox config to OpenClaw sandbox section.
func (c *ConfigConverter) convertSandboxConfig(harnessCfg *handler.HarnessConfig) *SandboxConfig {
	if harnessCfg == nil || harnessCfg.Sandbox == nil {
		return nil
	}

	sandboxConfig := &SandboxConfig{
		Mode:     string(harnessCfg.Sandbox.Mode),
		Endpoint: harnessCfg.Sandbox.Endpoint,
		Timeout:  int64(harnessCfg.Sandbox.Timeout),
	}

	if harnessCfg.Sandbox.ResourceLimits != nil {
		sandboxConfig.Resources = &ResourceConfig{
			CPU:    harnessCfg.Sandbox.ResourceLimits.CPU,
			Memory: harnessCfg.Sandbox.ResourceLimits.Memory,
		}
	}

	return sandboxConfig
}

// convertModelList converts string array to model definitions.
func (c *ConfigConverter) convertModelList(models []string) []*ModelDefinitionConfig {
	if len(models) == 0 {
		return nil
	}

	result := make([]*ModelDefinitionConfig, len(models))
	for i, name := range models {
		result[i] = &ModelDefinitionConfig{
			ID:   name,
			Name: name,
		}
	}
	return result
}

// convertModelListFromSpec converts spec model items to model definitions.
func (c *ConfigConverter) convertModelListFromSpec(models []v1.ModelConfig) []*ModelDefinitionConfig {
	if len(models) == 0 {
		return nil
	}

	result := make([]*ModelDefinitionConfig, len(models))
	for i, m := range models {
		result[i] = &ModelDefinitionConfig{
			ID:   m.Name,
			Name: m.Name,
		}
	}
	return result
}

// convertMCPServers converts MCPServerInfo to OpenClaw MCP servers.
func (c *ConfigConverter) convertMCPServers(servers []handler.MCPServerInfo) []*MCPServerConfig {
	if len(servers) == 0 {
		return nil
	}

	result := make([]*MCPServerConfig, len(servers))
	for i, s := range servers {
		result[i] = &MCPServerConfig{
			Name:    s.Name,
			Type:    s.Type,
			Command: s.Command,
			Args:    s.Args,
			URL:     s.Endpoint,
			Allowed: s.Allowed,
		}
	}
	return result
}

// convertMCPServersFromSpec converts spec MCP servers to OpenClaw MCP servers.
func (c *ConfigConverter) convertMCPServersFromSpec(servers []v1.MCPServerConfig) []*MCPServerConfig {
	if len(servers) == 0 {
		return nil
	}

	result := make([]*MCPServerConfig, len(servers))
	for i, s := range servers {
		result[i] = &MCPServerConfig{
			Name:    s.Name,
			Type:    s.Type,
			Command: s.Command,
			Args:    s.Args,
			URL:     s.URL,
			Allowed: s.Allowed,
		}
	}
	return result
}

// convertSkillInfoList converts SkillInfo to OpenClaw skill items.
func (c *ConfigConverter) convertSkillInfoList(skills []handler.SkillInfo) []*SkillItemConfig {
	if len(skills) == 0 {
		return nil
	}

	result := make([]*SkillItemConfig, len(skills))
	for i, s := range skills {
		result[i] = &SkillItemConfig{
			Name:    s.Name,
			Version: s.Version,
			Path:    s.Path,
			Allowed: s.Allowed,
		}
	}
	return result
}

// convertSkillItemsFromSpec converts spec skill items to OpenClaw skill items.
func (c *ConfigConverter) convertSkillItemsFromSpec(skills []v1.SkillConfig) []*SkillItemConfig {
	if len(skills) == 0 {
		return nil
	}

	result := make([]*SkillItemConfig, len(skills))
	for i, s := range skills {
		result[i] = &SkillItemConfig{
			Name:    s.Name,
			Version: s.Version,
			Allowed: s.Allowed,
		}
	}
	return result
}

// applySpecOverrides applies spec overrides to agent config.
func (c *ConfigConverter) applySpecOverrides(cfg *AgentConfig, override v1.HarnessOverrideSpec) {
	// Apply skills overrides
	for _, skillsOverride := range override.Skills {
		cfg.Skills = append(cfg.Skills, skillsOverride.AllowedSkills...)
	}

	// Apply model overrides
	for _, modelOverride := range override.Model {
		if len(modelOverride.AllowedModels) > 0 {
			if cfg.Model == nil {
				cfg.Model = &AgentModelConfig{}
			}
			if cfg.Model.Primary == "" && len(modelOverride.AllowedModels) > 0 {
				cfg.Model.Primary = modelOverride.AllowedModels[0]
			}
		}
	}
}

// ============================================================
// OpenClaw Config Types (matching TypeScript types)
// ============================================================

// OpenClawConfig represents the full OpenClaw config file structure.
type OpenClawConfig struct {
	Agents  *AgentsConfig   `json:"agents,omitempty"`
	Models  *ModelsConfig   `json:"models,omitempty"`
	Skills  *SkillsConfig   `json:"skills,omitempty"`
	Tools   *ToolsConfig    `json:"tools,omitempty"`
	Memory  *MemoryConfig   `json:"memory,omitempty"`
	Gateway *GatewayConfig  `json:"gateway,omitempty"`
	Channels *ChannelsConfig `json:"channels,omitempty"`
	MCP     *MCPConfig      `json:"mcp,omitempty"`
}

// AgentsConfig represents agents configuration.
type AgentsConfig struct {
	Defaults *AgentDefaultsConfig `json:"defaults,omitempty"`
	List     []*AgentConfig       `json:"list,omitempty"`
}

// AgentDefaultsConfig represents default agent configuration.
type AgentDefaultsConfig struct {
	Model    *AgentModelConfig   `json:"model,omitempty"`
	Sandbox  *AgentSandboxConfig `json:"sandbox,omitempty"`
	Identity *IdentityConfig     `json:"identity,omitempty"`
}

// AgentConfig represents a single agent configuration.
type AgentConfig struct {
	ID          string            `json:"id"`
	Name        string            `json:"name,omitempty"`
	Description string            `json:"description,omitempty"`
	Workspace   string            `json:"workspace,omitempty"`
	Model       *AgentModelConfig `json:"model,omitempty"`
	Skills      []string          `json:"skills,omitempty"`
	Tools       []string          `json:"tools,omitempty"`
	Runtime     *RuntimeConfig    `json:"runtime,omitempty"`
}

// AgentModelConfig represents agent model configuration.
type AgentModelConfig struct {
	Primary    string   `json:"primary,omitempty"`
	Fallbacks  []string `json:"fallbacks,omitempty"`
	Temperature float64  `json:"temperature,omitempty"`
	MaxTokens   int      `json:"maxTokens,omitempty"`
}

// AgentSandboxConfig represents agent sandbox configuration.
type AgentSandboxConfig struct {
	Enabled bool   `json:"enabled,omitempty"`
	Mode    string `json:"mode,omitempty"`
}

// RuntimeConfig represents agent runtime configuration.
type RuntimeConfig struct {
	Type string `json:"type,omitempty"`
}

// ModelsConfig represents models configuration.
type ModelsConfig struct {
	Mode      string                        `json:"mode,omitempty"`
	Providers map[string]*ModelProviderConfig `json:"providers,omitempty"`
}

// ModelProviderConfig represents a model provider.
type ModelProviderConfig struct {
	BaseURL string                `json:"baseUrl,omitempty"`
	APIKey  string                `json:"apiKey,omitempty"`
	Models  []*ModelDefinitionConfig `json:"models,omitempty"`
}

// ModelDefinitionConfig represents a model definition.
type ModelDefinitionConfig struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

// SkillsConfig represents skills configuration.
type SkillsConfig struct {
	HubType   string            `json:"hubType,omitempty"`
	Endpoint  string            `json:"endpoint,omitempty"`
	LocalPath string            `json:"localPath,omitempty"`
	Skills    []*SkillItemConfig `json:"skills,omitempty"`
}

// SkillItemConfig represents a skill item.
type SkillItemConfig struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
	Path    string `json:"path,omitempty"`
	Allowed bool   `json:"allowed,omitempty"`
}

// MemoryConfig represents memory configuration.
type MemoryConfig struct {
	Type        string `json:"type,omitempty"`
	Endpoint    string `json:"endpoint,omitempty"`
	TTL         int64  `json:"ttl,omitempty"`
	Persistence bool   `json:"persistence,omitempty"`
}

// MCPConfig represents MCP configuration.
type MCPConfig struct {
	RegistryType string             `json:"registryType,omitempty"`
	Endpoint     string             `json:"endpoint,omitempty"`
	Servers      []*MCPServerConfig `json:"servers,omitempty"`
}

// MCPServerConfig represents an MCP server.
type MCPServerConfig struct {
	Name    string   `json:"name"`
	Type    string   `json:"type,omitempty"`
	Command string   `json:"command,omitempty"`
	Args    []string `json:"args,omitempty"`
	URL     string   `json:"url,omitempty"`
	Allowed bool     `json:"allowed,omitempty"`
}

// SandboxConfig represents sandbox configuration.
type SandboxConfig struct {
	Mode      string          `json:"mode,omitempty"`
	Endpoint  string          `json:"endpoint,omitempty"`
	Timeout   int64           `json:"timeout,omitempty"`
	Resources *ResourceConfig `json:"resources,omitempty"`
}

// ResourceConfig represents resource limits.
type ResourceConfig struct {
	CPU    string `json:"cpu,omitempty"`
	Memory string `json:"memory,omitempty"`
}

// GatewayConfig represents gateway configuration.
type GatewayConfig struct {
	Mode string `json:"mode,omitempty"`
	Host string `json:"host,omitempty"`
	Port int    `json:"port,omitempty"`
}

// ToolsConfig represents tools configuration.
type ToolsConfig struct {
	Allow []string `json:"allow,omitempty"`
}

// ChannelsConfig represents channels configuration.
type ChannelsConfig struct {
	Telegram *TelegramConfig `json:"telegram,omitempty"`
	Discord  *DiscordConfig  `json:"discord,omitempty"`
	Slack    *SlackConfig    `json:"slack,omitempty"`
}

// TelegramConfig represents Telegram channel config.
type TelegramConfig struct {
	Enabled bool   `json:"enabled,omitempty"`
	Token   string `json:"token,omitempty"`
}

// DiscordConfig represents Discord channel config.
type DiscordConfig struct {
	Enabled bool   `json:"enabled,omitempty"`
	Token   string `json:"token,omitempty"`
}

// SlackConfig represents Slack channel config.
type SlackConfig struct {
	Enabled bool   `json:"enabled,omitempty"`
	Token   string `json:"token,omitempty"`
}

// IdentityConfig represents agent identity configuration.
type IdentityConfig struct {
	Name   string `json:"name,omitempty"`
	Avatar string `json:"avatar,omitempty"`
}

// HarnessSection represents harness-only config section.
type HarnessSection struct {
	Model   *ModelsConfig   `json:"model,omitempty"`
	Skills  *SkillsConfig   `json:"skills,omitempty"`
	Memory  *MemoryConfig   `json:"memory,omitempty"`
	MCP     *MCPConfig      `json:"mcp,omitempty"`
	Sandbox *SandboxConfig  `json:"sandbox,omitempty"`
}