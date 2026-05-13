// Package openclaw provides configuration conversion for OpenClaw framework.
// This converter transforms AIAgentSpec + HarnessConfig into OpenClaw JSON config.
//
// Configuration Sources:
// - agentConfig (from AIAgentSpec): Gateway port, internal agents, overrides
// - HarnessConfig (from AgentRuntime): Models, Skills, Memory (shared)
package openclaw

import (
	"encoding/json"
	"fmt"

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

	// 1. Parse agentConfig JSON (agent-specific configuration)
	if spec.AgentConfig != nil && spec.AgentConfig.Raw != nil {
		agentConfig, err := c.parseAgentConfigJSON(spec.AgentConfig.Raw)
		if err != nil {
			return nil, fmt.Errorf("failed to parse agentConfig: %w", err)
		}

		// Apply agentConfig to OpenClaw config
		if agentConfig.Gateway != nil {
			config.Gateway = c.convertGatewayInstanceConfig(agentConfig.Gateway)
		}
		if agentConfig.InternalAgents != nil {
			config.Agents = c.convertInternalAgentsConfig(agentConfig.InternalAgents)
		}
		// Apply overrides after agents are set
		if agentConfig.Overrides != nil {
			c.applyAgentOverrides(config, agentConfig.Overrides)
		}
	}

	// 2. Apply Harness configuration (shared platform capabilities)
	if harnessCfg != nil {
		config.Agents.Defaults = c.convertAgentDefaultsFromHarness(harnessCfg)
		config.Models = c.convertModelsConfig(harnessCfg)
		config.Skills = c.convertSkillsConfig(harnessCfg)
		config.Memory = c.convertMemoryConfig(harnessCfg)
		config.Sandbox = c.convertSandboxConfig(harnessCfg)
		// If external sandbox, set plugins path
		if config.Sandbox != nil && config.Sandbox.Mode == "external" && config.Sandbox.Plugins != nil {
			config.Plugins = config.Sandbox.Plugins
		}
	}

	// 3. Create main agent entry from AIAgentSpec metadata
	mainAgent := &AgentConfig{
		ID:          spec.Description,
		Name:        spec.Description,
		Description: spec.Description,
	}

	// Apply model from harness
	if harnessCfg != nil && harnessCfg.Model != nil {
		mainAgent.Model = &AgentModelConfig{
			Primary: harnessCfg.Model.DefaultModel,
		}
	}

	// Apply skills from harness
	if harnessCfg != nil && harnessCfg.Skills != nil {
		for _, skill := range harnessCfg.Skills.Skills {
			if skill.Allowed {
				mainAgent.Skills = append(mainAgent.Skills, skill.Name)
			}
		}
	}

	// Add main agent to list if no internal agents defined
	if len(config.Agents.List) == 0 {
		config.Agents.List = []*AgentConfig{mainAgent}
	}

	return json.MarshalIndent(config, "", "  ")
}

// ConvertAgentSpec generates AgentConfig section for a single agent.
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
func (c *ConfigConverter) ConvertHarnessConfig(harnessCfg *handler.HarnessConfig) ([]byte, error) {
	harnessSection := &HarnessSection{
		Model:   c.convertModelsConfig(harnessCfg),
		Skills:  c.convertSkillsConfig(harnessCfg),
		Memory:  c.convertMemoryConfig(harnessCfg),
	}

	return json.MarshalIndent(harnessSection, "", "  ")
}

// ============================================================
// agentConfig Parsing (AIAgentSpec.agentConfig.Raw)
// ============================================================

// AgentConfigJSON represents the agentConfig JSON structure from AIAgentSpec.
// ONLY contains agent-specific configuration (not shared Harness capabilities).
// Fields use JSON names matching the schema defined earlier.
type AgentConfigJSON struct {
	// Gateway process configuration for this agent instance
	Gateway *GatewayInstanceConfigJSON `json:"gateway"`

	// Internal sub-agents managed by this Gateway (keyed as "agents" in JSON)
	InternalAgents *InternalAgentsConfigJSON `json:"agents,omitempty"`

	// Per-agent overrides of inherited Harness defaults
	Overrides *AgentOverridesConfigJSON `json:"overrides,omitempty"`
}

// GatewayInstanceConfigJSON - Gateway process specific config
type GatewayInstanceConfigJSON struct {
	// Port for this Gateway instance
	Port int `json:"port,omitempty"`

	// Bind address: loopback, lan, auto
	Bind string `json:"bind,omitempty"`

	// Authentication for this Gateway instance
	Auth *GatewayAuthConfigJSON `json:"auth,omitempty"`

	// Control UI settings
	ControlUI *GatewayControlUIConfigJSON `json:"controlUi,omitempty"`

	// Trusted proxy IPs for X-Forwarded-For
	TrustedProxies []string `json:"trustedProxies,omitempty"`
}

// GatewayAuthConfigJSON - Gateway authentication
type GatewayAuthConfigJSON struct {
	// Auth mode: none, token, password
	Mode string `json:"mode,omitempty"`

	// Token for token mode
	Token string `json:"token,omitempty"`
}

// GatewayControlUIConfigJSON - Control UI for this instance
type GatewayControlUIConfigJSON struct {
	Enabled        bool     `json:"enabled,omitempty"`
	BasePath       string   `json:"basePath,omitempty"`
	AllowedOrigins []string `json:"allowedOrigins,omitempty"`
}

// InternalAgentsConfigJSON - Sub-agents inside Gateway
type InternalAgentsConfigJSON struct {
	// Default settings for internal agents
	Defaults *InternalAgentDefaultsJSON `json:"defaults,omitempty"`

	// List of internal sub-agents
	List []*InternalAgentDefinitionJSON `json:"list,omitempty"`
}

// InternalAgentDefaultsJSON - Default settings for sub-agents
type InternalAgentDefaultsJSON struct {
	// Override inherited model
	Model string `json:"model,omitempty"`

	// Thinking level: off, minimal, low, medium, high, xhigh, adaptive
	ThinkingDefault string `json:"thinkingDefault,omitempty"`

	// Reasoning visibility: on, off, stream
	ReasoningDefault string `json:"reasoningDefault,omitempty"`

	// Fast mode default
	FastModeDefault bool `json:"fastModeDefault,omitempty"`

	// Identity for all sub-agents
	Identity *AgentIdentityJSON `json:"identity,omitempty"`
}

// InternalAgentDefinitionJSON - Single internal sub-agent
type InternalAgentDefinitionJSON struct {
	// Agent identifier
	ID string `json:"id"`

	// Display name
	Name string `json:"name,omitempty"`

	// Override model for this sub-agent
	Model string `json:"model,omitempty"`

	// Allowed skills (subset of Harness.Skills)
	Skills []string `json:"skills,omitempty"`

	// Identity for this sub-agent
	Identity *AgentIdentityJSON `json:"identity,omitempty"`

	// Tool restrictions (subset)
	Tools *ToolsOverrideJSON `json:"tools,omitempty"`

	// Subagent spawning config
	Subagents *SubagentsConfigJSON `json:"subagents,omitempty"`
}

// AgentIdentityJSON - Agent name and avatar
type AgentIdentityJSON struct {
	Name   string `json:"name,omitempty"`
	Avatar string `json:"avatar,omitempty"`
}

// ToolsOverrideJSON - Per-agent tool restrictions
type ToolsOverrideJSON struct {
	Allow []string `json:"allow,omitempty"`
	Deny  []string `json:"deny,omitempty"`
}

// SubagentsConfigJSON - Sub-agent spawning rules
type SubagentsConfigJSON struct {
	// Allowed sub-agent IDs ("*" = any)
	AllowAgents []string `json:"allowAgents,omitempty"`

	// Default model for spawned sub-agents
	Model string `json:"model,omitempty"`

	// Require explicit agentId in spawn calls
	RequireAgentID bool `json:"requireAgentId,omitempty"`
}

// AgentOverridesConfigJSON - Override inherited Harness defaults
type AgentOverridesConfigJSON struct {
	// Override model selection
	Model string `json:"model,omitempty"`

	// Skills subset (cannot add new, only restrict)
	AllowedSkills []string `json:"allowedSkills,omitempty"`
	DeniedSkills  []string `json:"deniedSkills,omitempty"`

	// Model fallback chain override
	ModelFallbacks []string `json:"modelFallbacks,omitempty"`
}

// parseAgentConfigJSON parses raw agentConfig JSON
func (c *ConfigConverter) parseAgentConfigJSON(raw []byte) (*AgentConfigJSON, error) {
	if len(raw) == 0 {
		return nil, nil
	}

	agentConfig := &AgentConfigJSON{}
	if err := json.Unmarshal(raw, agentConfig); err != nil {
		return nil, fmt.Errorf("invalid agentConfig JSON: %w", err)
	}

	return agentConfig, nil
}

// ParseAgentConfig is the public method for Handler to call
func (c *ConfigConverter) ParseAgentConfig(raw []byte) (*AgentConfigJSON, error) {
	return c.parseAgentConfigJSON(raw)
}

// convertGatewayInstanceConfig converts agentConfig.Gateway to OpenClaw GatewayConfig
func (c *ConfigConverter) convertGatewayInstanceConfig(gateway *GatewayInstanceConfigJSON) *GatewayConfig {
	cfg := &GatewayConfig{
		Mode: "local",
	}

	if gateway.Port > 0 {
		cfg.Port = gateway.Port
	}
	if gateway.Bind != "" {
		cfg.Host = gateway.Bind
	}
	if gateway.Auth != nil {
		cfg.AuthMode = gateway.Auth.Mode
	}
	if gateway.ControlUI != nil {
		cfg.ControlUI = &ControlUIConfig{
			Enabled:        gateway.ControlUI.Enabled,
			BasePath:       gateway.ControlUI.BasePath,
			AllowedOrigins: gateway.ControlUI.AllowedOrigins,
		}
	}
	cfg.TrustedProxies = gateway.TrustedProxies

	return cfg
}

// convertInternalAgentsConfig converts agentConfig.InternalAgents to OpenClaw AgentsConfig
func (c *ConfigConverter) convertInternalAgentsConfig(internal *InternalAgentsConfigJSON) *AgentsConfig {
	agents := &AgentsConfig{}

	// Convert defaults
	if internal.Defaults != nil {
		agents.Defaults = &AgentDefaultsConfig{
			ThinkingDefault:  internal.Defaults.ThinkingDefault,
			ReasoningDefault: internal.Defaults.ReasoningDefault,
			FastModeDefault:  internal.Defaults.FastModeDefault,
		}
		if internal.Defaults.Model != "" {
			agents.Defaults.Model = &AgentModelConfig{
				Primary: internal.Defaults.Model,
			}
		}
		if internal.Defaults.Identity != nil {
			agents.Defaults.Identity = &IdentityConfig{
				Name:   internal.Defaults.Identity.Name,
				Avatar: internal.Defaults.Identity.Avatar,
			}
		}
	}

	// Convert internal agent list
	for _, def := range internal.List {
		agent := &AgentConfig{
			ID:   def.ID,
			Name: def.Name,
		}
		if def.Model != "" {
			agent.Model = &AgentModelConfig{
				Primary: def.Model,
			}
		}
		agent.Skills = def.Skills
		if def.Identity != nil {
			agent.Identity = &IdentityConfig{
				Name:   def.Identity.Name,
				Avatar: def.Identity.Avatar,
			}
		}
		if def.Tools != nil {
			agent.Tools = &AgentToolsConfig{
				Allow: def.Tools.Allow,
				Deny:  def.Tools.Deny,
			}
		}
		if def.Subagents != nil {
			agent.Subagents = &SubagentsConfigOpenClaw{
				AllowAgents:    def.Subagents.AllowAgents,
				RequireAgentID: def.Subagents.RequireAgentID,
			}
			if def.Subagents.Model != "" {
				agent.Subagents.Model = &AgentModelConfig{
					Primary: def.Subagents.Model,
				}
			}
		}
		agents.List = append(agents.List, agent)
	}

	return agents
}

// applyAgentOverrides applies agentConfig.Overrides to OpenClaw config
func (c *ConfigConverter) applyAgentOverrides(config *OpenClawConfig, overrides *AgentOverridesConfigJSON) {
	// Override default model
	if overrides.Model != "" && config.Agents.Defaults != nil {
		config.Agents.Defaults.Model = &AgentModelConfig{
			Primary: overrides.Model,
		}
	}

	// Filter skills for all agents
	if len(overrides.AllowedSkills) > 0 || len(overrides.DeniedSkills) > 0 {
		for _, agent := range config.Agents.List {
			agent.Skills = c.filterSkills(agent.Skills, overrides.AllowedSkills, overrides.DeniedSkills)
		}
	}

	// Set model fallbacks
	if len(overrides.ModelFallbacks) > 0 && config.Agents.Defaults != nil && config.Agents.Defaults.Model != nil {
		config.Agents.Defaults.Model.Fallbacks = overrides.ModelFallbacks
	}
}

// filterSkills filters skills based on allow/deny lists
func (c *ConfigConverter) filterSkills(skills []string, allowed []string, denied []string) []string {
	if len(skills) == 0 {
		return skills
	}

	// Build allowed set
	allowedSet := make(map[string]bool)
	for _, s := range allowed {
		allowedSet[s] = true
	}

	// Build denied set
	deniedSet := make(map[string]bool)
	for _, s := range denied {
		deniedSet[s] = true
	}

	// Filter skills
	result := []string{}
	for _, s := range skills {
		// Skip if denied
		if deniedSet[s] {
			continue
		}
		// If allowed list exists, only keep allowed skills
		if len(allowedSet) > 0 && !allowedSet[s] {
			continue
		}
		result = append(result, s)
	}

	return result
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

// ConvertMemoryHarness converts MemoryHarnessInterface to OpenClaw memory config.
func (c *ConfigConverter) ConvertMemoryHarness(harness handler.MemoryHarnessInterface) ([]byte, error) {
	if harness == nil {
		return nil, nil
	}

	memoryConfig := &MemoryConfig{
		Type:        harness.GetType(),
		Endpoint:    harness.GetEndpoint(),
		TTL:         harness.GetTTL(),
		Persistence: harness.IsPersistenceEnabled(),
	}

	return json.MarshalIndent(memoryConfig, "", "  ")
}

// ConvertSandboxHarness converts SandboxHarnessInterface to OpenClaw sandbox config.
// For External Sandbox, generates plugins configuration for harness-bridge plugin.
// For Embedded Sandbox, generates local sandbox configuration.
func (c *ConfigConverter) ConvertSandboxHarness(harness handler.SandboxHarnessInterface, pluginDir string) ([]byte, error) {
	if harness == nil {
		return nil, nil
	}

	sandboxConfig := &SandboxConfig{
		Mode: string(harness.GetMode()),
	}

	if harness.IsExternal() {
		// External Sandbox: Configure harness-bridge plugin path
		sandboxConfig.Plugins = &PluginsConfig{
			Load: &PluginsLoadConfig{
				Paths: []string{pluginDir},
			},
		}
		sandboxConfig.Endpoint = harness.GetEndpoint()
		sandboxConfig.Timeout = harness.GetTimeout()
	} else {
		// Embedded Sandbox: Local execution (no plugin needed)
		sandboxConfig.Mode = "embedded"
	}

	return json.MarshalIndent(sandboxConfig, "", "  ")
}

// ============================================================
// Helper Conversion Methods
// ============================================================

// convertAgentDefaultsFromHarness converts harness to agent defaults configuration.
func (c *ConfigConverter) convertAgentDefaultsFromHarness(harnessCfg *handler.HarnessConfig) *AgentDefaultsConfig {
	defaults := &AgentDefaultsConfig{}

	if harnessCfg.Model != nil {
		defaults.Model = &AgentModelConfig{
			Primary: harnessCfg.Model.DefaultModel,
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
		HubType:   harnessCfg.Skills.HubType,
		Endpoint:  harnessCfg.Skills.Endpoint,
		LocalPath: harnessCfg.Skills.LocalPath,
		Skills:    c.convertSkillItemsFromSpec(harnessCfg.Skills.Skills),
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

// convertSandboxConfig converts harness sandbox config to OpenClaw sandbox section.
// For External Sandbox, configures plugins path for harness-bridge plugin.
func (c *ConfigConverter) convertSandboxConfig(harnessCfg *handler.HarnessConfig) *SandboxConfig {
	if harnessCfg == nil || harnessCfg.Sandbox == nil {
		return nil
	}

	sandboxConfig := &SandboxConfig{
		Mode: string(harnessCfg.Sandbox.Mode),
	}

	if harnessCfg.Sandbox.Mode == v1.SandboxModeExternal {
		// External Sandbox: Configure plugins path for harness-bridge plugin
		pluginDir := "/etc/aiagent/plugins/harness-bridge"
		sandboxConfig.Endpoint = harnessCfg.Sandbox.Endpoint
		sandboxConfig.Timeout = int64(harnessCfg.Sandbox.Timeout)
		sandboxConfig.Plugins = &PluginsConfig{
			Load: &PluginsLoadConfig{
				Paths: []string{pluginDir},
			},
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

// applySpecOverrides applies HarnessOverrideSpec from AIAgentSpec.
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
	Agents   *AgentsConfig    `json:"agents,omitempty"`
	Models   *ModelsConfig    `json:"models,omitempty"`
	Skills   *SkillsConfig    `json:"skills,omitempty"`
	Tools    *ToolsConfig     `json:"tools,omitempty"`
	Memory   *MemoryConfig    `json:"memory,omitempty"`
	Gateway  *GatewayConfig   `json:"gateway,omitempty"`
	Channels *ChannelsConfig  `json:"channels,omitempty"`
	Sandbox  *SandboxConfig   `json:"sandbox,omitempty"`
	Plugins  *PluginsConfig   `json:"plugins,omitempty"`
}

// AgentsConfig represents agents configuration.
type AgentsConfig struct {
	Defaults *AgentDefaultsConfig `json:"defaults,omitempty"`
	List     []*AgentConfig       `json:"list,omitempty"`
}

// AgentDefaultsConfig represents default agent configuration.
type AgentDefaultsConfig struct {
	Model            *AgentModelConfig   `json:"model,omitempty"`
	Identity         *IdentityConfig     `json:"identity,omitempty"`
	ThinkingDefault  string              `json:"thinkingDefault,omitempty"`
	ReasoningDefault string              `json:"reasoningDefault,omitempty"`
	FastModeDefault  bool                `json:"fastModeDefault,omitempty"`
}

// AgentConfig represents a single agent configuration.
type AgentConfig struct {
	ID          string              `json:"id"`
	Name        string              `json:"name,omitempty"`
	Description string              `json:"description,omitempty"`
	Workspace   string              `json:"workspace,omitempty"`
	Model       *AgentModelConfig   `json:"model,omitempty"`
	Skills      []string            `json:"skills,omitempty"`
	Tools       *AgentToolsConfig   `json:"tools,omitempty"`
	Identity    *IdentityConfig     `json:"identity,omitempty"`
	Subagents   *SubagentsConfigOpenClaw `json:"subagents,omitempty"`
}

// AgentModelConfig represents agent model configuration.
type AgentModelConfig struct {
	Primary    string   `json:"primary,omitempty"`
	Fallbacks  []string `json:"fallbacks,omitempty"`
	Temperature float64  `json:"temperature,omitempty"`
	MaxTokens   int      `json:"maxTokens,omitempty"`
}

// AgentToolsConfig represents agent tools configuration.
type AgentToolsConfig struct {
	Allow []string `json:"allow,omitempty"`
	Deny  []string `json:"deny,omitempty"`
}

// IdentityConfig represents agent identity configuration.
type IdentityConfig struct {
	Name   string `json:"name,omitempty"`
	Avatar string `json:"avatar,omitempty"`
}

// SubagentsConfigOpenClaw represents sub-agent configuration.
type SubagentsConfigOpenClaw struct {
	AllowAgents    []string          `json:"allowAgents,omitempty"`
	Model          *AgentModelConfig `json:"model,omitempty"`
	RequireAgentID bool              `json:"requireAgentId,omitempty"`
}

// ModelsConfig represents models configuration.
type ModelsConfig struct {
	Mode      string                         `json:"mode,omitempty"`
	Providers map[string]*ModelProviderConfig `json:"providers,omitempty"`
}

// ModelProviderConfig represents a model provider.
type ModelProviderConfig struct {
	BaseURL string                     `json:"baseUrl,omitempty"`
	APIKey  string                     `json:"apiKey,omitempty"`
	Models  []*ModelDefinitionConfig   `json:"models,omitempty"`
}

// ModelDefinitionConfig represents a model definition.
type ModelDefinitionConfig struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

// SkillsConfig represents skills configuration.
type SkillsConfig struct {
	HubType   string             `json:"hubType,omitempty"`
	Endpoint  string             `json:"endpoint,omitempty"`
	LocalPath string             `json:"localPath,omitempty"`
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

// GatewayConfig represents gateway configuration.
type GatewayConfig struct {
	Mode          string           `json:"mode,omitempty"`
	Host          string           `json:"host,omitempty"`
	Port          int              `json:"port,omitempty"`
	AuthMode      string           `json:"authMode,omitempty"`
	ControlUI     *ControlUIConfig `json:"controlUi,omitempty"`
	TrustedProxies []string        `json:"trustedProxies,omitempty"`
}

// ControlUIConfig represents Control UI configuration.
type ControlUIConfig struct {
	Enabled        bool     `json:"enabled,omitempty"`
	BasePath       string   `json:"basePath,omitempty"`
	AllowedOrigins []string `json:"allowedOrigins,omitempty"`
}

// ToolsConfig represents tools configuration.
type ToolsConfig struct {
	Allow []string `json:"allow,omitempty"`
	Deny  []string `json:"deny,omitempty"`
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

// HarnessSection represents harness-only config section.
type HarnessSection struct {
	Model   *ModelsConfig  `json:"model,omitempty"`
	Skills  *SkillsConfig  `json:"skills,omitempty"`
	Memory  *MemoryConfig  `json:"memory,omitempty"`
}

// SandboxConfig represents sandbox configuration.
// For External Sandbox, includes plugins configuration for harness-bridge.
type SandboxConfig struct {
	// Mode: "external" or "embedded"
	Mode string `json:"mode"`

	// Endpoint for External Sandbox (HTTP API URL)
	Endpoint string `json:"endpoint,omitempty"`

	// Timeout for sandbox operations (seconds)
	Timeout int64 `json:"timeout,omitempty"`

	// Plugins configuration for External Sandbox
	Plugins *PluginsConfig `json:"plugins,omitempty"`
}

// PluginsConfig represents OpenClaw plugins configuration.
type PluginsConfig struct {
	// Load configuration for plugin discovery
	Load *PluginsLoadConfig `json:"load,omitempty"`
}

// PluginsLoadConfig specifies plugin discovery paths.
type PluginsLoadConfig struct {
	// Paths to search for plugins
	Paths []string `json:"paths,omitempty"`
}