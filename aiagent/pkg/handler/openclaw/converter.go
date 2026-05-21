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
	"strings"

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
	var agentConfig *AgentConfigJSON
	if spec.AgentConfig != nil && spec.AgentConfig.Raw != nil {
		parsedConfig, err := c.parseAgentConfigJSON(spec.AgentConfig.Raw)
		if err != nil {
			return nil, fmt.Errorf("failed to parse agentConfig: %w", err)
		}
		agentConfig = parsedConfig

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
		// Apply channels configuration (Discord, Telegram, Slack)
		if agentConfig.Channels != nil {
			config.Channels = c.convertChannelsConfig(agentConfig.Channels)
			// Set plugins.entries for enabled channels so Gateway loads channel plugins
			pluginEntries := make(map[string]PluginEntry)
			if agentConfig.Channels.Discord != nil && agentConfig.Channels.Discord.Enabled {
				pluginEntries["discord"] = PluginEntry{Enabled: true}
			}
			if agentConfig.Channels.Telegram != nil && agentConfig.Channels.Telegram.Enabled {
				pluginEntries["telegram"] = PluginEntry{Enabled: true}
			}
			if agentConfig.Channels.Slack != nil && agentConfig.Channels.Slack.Enabled {
				pluginEntries["slack"] = PluginEntry{Enabled: true}
			}
			if len(pluginEntries) > 0 {
				if config.Plugins == nil {
					config.Plugins = &PluginsConfig{}
				}
				config.Plugins.Entries = pluginEntries
			}
		}
	}

	// 2. Apply Harness configuration (shared platform capabilities)
	if harnessCfg != nil {
		config.Agents.Defaults = c.convertAgentDefaultsFromHarness(harnessCfg)
		// Pass agentConfig for merge mode configuration
		config.Models = c.convertModelsConfig(harnessCfg, agentConfig)
		config.Skills = c.convertSkillsConfig(harnessCfg)
		// Memory config not supported in OpenClaw v2026.5.7 config schema
		// OpenClaw uses internal session management
		// config.Memory = c.convertMemoryConfig(harnessCfg)
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

	// Apply model from harness (use provider/model format)
	if harnessCfg != nil && harnessCfg.Model != nil {
		mainAgent.Model = &AgentModelConfig{
			Primary: harnessCfg.Model.Provider + "/" + harnessCfg.Model.DefaultModel,
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

	// Apply model from harness (use provider/model format)
	if harnessCfg != nil && harnessCfg.Model != nil {
		agentCfg.Model = &AgentModelConfig{
			Primary: harnessCfg.Model.Provider + "/" + harnessCfg.Model.DefaultModel,
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
// Note: This method doesn't have agentConfig context, so models.mode won't be set.
// Use ConvertToOpenClawConfig for full config generation with agentConfig support.
func (c *ConfigConverter) ConvertHarnessConfig(harnessCfg *handler.HarnessConfig) ([]byte, error) {
	harnessSection := &HarnessSection{
		Model:   c.convertModelsConfig(harnessCfg, nil), // nil agentConfig means no merge mode
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

	// Channel integrations (Discord, Telegram, Slack)
	Channels *ChannelsInstanceConfigJSON `json:"channels,omitempty"`

	// Models merge mode configuration (OpenClaw-specific)
	// When mergeModels is true, OpenClaw will merge harness providers with built-in providers
	Models *ModelsMergeModeConfig `json:"models,omitempty"`
}

// ChannelsInstanceConfigJSON - Channel integrations for this Gateway instance
type ChannelsInstanceConfigJSON struct {
	// Discord channel configuration
	Discord *DiscordChannelConfigJSON `json:"discord,omitempty"`

	// Telegram channel configuration
	Telegram *TelegramChannelConfigJSON `json:"telegram,omitempty"`

	// Slack channel configuration
	Slack *SlackChannelConfigJSON `json:"slack,omitempty"`
}

// DiscordChannelConfigJSON - Discord channel configuration from agentConfig
type DiscordChannelConfigJSON struct {
	// Enable Discord integration
	Enabled bool `json:"enabled,omitempty"`

	// Discord bot token (can reference secret or use direct value)
	Token string `json:"token,omitempty"`

	// Secret reference for bot token (recommended for production)
	TokenSecretRef string `json:"tokenSecretRef,omitempty"`

	// Allowed Discord user IDs (whitelist)
	// Accept both "allowedUsers" (legacy) and "allowFrom" (v2026.5.7)
	AllowedUsers []string `json:"allowedUsers,omitempty"`
	AllowFrom    []string `json:"allowFrom,omitempty"`

	// DM policy for direct messages
	DmPolicy string `json:"dmPolicy,omitempty"`

	// Allowed Discord guild/server IDs (whitelist)
	AllowedGuilds []string `json:"allowedGuilds,omitempty"`

	// Allowed Discord channel IDs (whitelist)
	AllowedChannels []string `json:"allowedChannels,omitempty"`

	// Bot command prefix
	CommandPrefix string `json:"commandPrefix,omitempty"`

	// Require @mention to trigger bot response
	MentionRequired bool `json:"mentionRequired,omitempty"`
}

// TelegramChannelConfigJSON - Telegram channel configuration from agentConfig
type TelegramChannelConfigJSON struct {
	Enabled bool   `json:"enabled,omitempty"`
	Token   string `json:"token,omitempty"`
	TokenSecretRef string `json:"tokenSecretRef,omitempty"`
	AllowedUsers []string `json:"allowedUsers,omitempty"`
}

// SlackChannelConfigJSON - Slack channel configuration from agentConfig
type SlackChannelConfigJSON struct {
	Enabled bool   `json:"enabled,omitempty"`
	Token   string `json:"token,omitempty"`
	TokenSecretRef string `json:"tokenSecretRef,omitempty"`
	AllowedChannels []string `json:"allowedChannels,omitempty"`
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
// For v2026.5.7: use bind (not host), auth object (not authMode)
func (c *ConfigConverter) convertGatewayInstanceConfig(gateway *GatewayInstanceConfigJSON) *GatewayConfig {
	cfg := &GatewayConfig{
		Mode: "local",
	}

	if gateway.Port > 0 {
		cfg.Port = gateway.Port
	}
	if gateway.Bind != "" {
		cfg.Bind = gateway.Bind
	}
	if gateway.Auth != nil {
		cfg.Auth = &GatewayAuth{
			Mode:  gateway.Auth.Mode,
			Token: gateway.Auth.Token,
		}
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
// For OpenClaw v2026.5.7: Only supported fields are model, skills, workspace at defaults level
func (c *ConfigConverter) convertInternalAgentsConfig(internal *InternalAgentsConfigJSON) *AgentsConfig {
	agents := &AgentsConfig{}

	// Convert defaults - only use model, skills, workspace (v2026.5.7 schema)
	if internal.Defaults != nil {
		agents.Defaults = &AgentDefaultsConfig{}
		if internal.Defaults.Model != "" {
			agents.Defaults.Model = &AgentModelConfig{
				Primary: internal.Defaults.Model,
			}
		}
		// Note: identity and thinkingDefault are NOT supported at defaults level in v2026.5.7
	}

	// Convert internal agent list - use v2026.5.7 schema fields
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
		// Note: identity is NOT a top-level field in agents.list[] for v2026.5.7
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

// convertChannelsConfig converts agentConfig.Channels to OpenClaw ChannelsConfig
func (c *ConfigConverter) convertChannelsConfig(channels *ChannelsInstanceConfigJSON) *ChannelsConfig {
	if channels == nil {
		return nil
	}

	cfg := &ChannelsConfig{}

	// Convert Discord configuration (v2026.5.12 schema)
	// Token uses SecretInput pointing to env var: DISCORD_BOT_TOKEN
	// ConfigDaemon resolves tokenSecretRef → writes token to hostPath
	// Handler extracts token from hostPath → injects as subprocess env var
	//
	// dmPolicy options (from OpenClaw zod-schema.providers-core.ts):
	// - "pairing" - requires pairing before DM (default)
	// - "allowlist" - requires specific user IDs in allowFrom
	// - "open" - allows all users (requires "*" in allowFrom)
	// - "disabled" - disables DM
	if channels.Discord != nil {
		// Use AllowFrom if provided directly, otherwise use AllowedUsers
		allowFrom := channels.Discord.AllowFrom
		if len(allowFrom) == 0 && len(channels.Discord.AllowedUsers) > 0 {
			allowFrom = channels.Discord.AllowedUsers
		}

		dmPolicy := channels.Discord.DmPolicy
		// Normalize dmPolicy: "all" (legacy) → "open", empty → default based on allowFrom
		if dmPolicy == "all" {
			dmPolicy = "open"
			// For "open" policy, OpenClaw requires "*" in allowFrom
			allowFrom = []string{"*"}
		} else if dmPolicy == "" {
			// Default based on allowFrom content:
			// - ["*"] → "open" (allow all users)
			// - specific IDs → "allowlist" (allow listed users)
			// - empty → "pairing" (require pairing)
			if containsString(allowFrom, "*") {
				dmPolicy = "open"
			} else if len(allowFrom) > 0 {
				dmPolicy = "allowlist"
			} else {
				dmPolicy = "pairing"
			}
		}
		// For "open" policy without "*", add "*" to satisfy OpenClaw validation
		if dmPolicy == "open" && !containsString(allowFrom, "*") {
			allowFrom = []string{"*"}
		}

		cfg.Discord = &DiscordConfig{
			Enabled:         channels.Discord.Enabled,
			Token:           &SecretInput{Source: "env", Provider: "default", ID: "DISCORD_BOT_TOKEN"},
			AllowFrom:       allowFrom,
			DmPolicy:        dmPolicy,
			MentionRequired: channels.Discord.MentionRequired,
		}
	}

	// Convert Telegram configuration
	if channels.Telegram != nil {
		cfg.Telegram = &TelegramConfig{
			Enabled:      channels.Telegram.Enabled,
			Token:        channels.Telegram.Token,
			AllowedUsers: channels.Telegram.AllowedUsers,
		}
	}

	// Convert Slack configuration
	if channels.Slack != nil {
		cfg.Slack = &SlackConfig{
			Enabled: channels.Slack.Enabled,
			Token:   channels.Slack.Token,
		}
	}

	return cfg
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
// For v2026.5.7: Skills are per-agent, this returns endpoint info only.
func (c *ConfigConverter) ConvertSkillsHarness(harness handler.SkillsHarnessInterface) ([]byte, error) {
	if harness == nil {
		return nil, nil
	}

	// For v2026.5.7: Only endpoint and localPath are valid
	skillsConfig := &SkillsConfig{
		Endpoint:  harness.GetEndpoint(),
		LocalPath: "", // LocalPath not available from interface
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
			Primary: harnessCfg.Model.Provider + "/" + harnessCfg.Model.DefaultModel,
		}
	}

	return defaults
}

// convertModelsConfig converts harness model config to OpenClaw models section.
func (c *ConfigConverter) convertModelsConfig(harnessCfg *handler.HarnessConfig, agentConfig *AgentConfigJSON) *ModelsConfig {
	if harnessCfg == nil || harnessCfg.Model == nil {
		return nil
	}

	provider := harnessCfg.Model.Provider
	modelsConfig := &ModelsConfig{
		Providers: map[string]*ModelProviderConfig{},
	}

	// Determine merge mode from agentConfig
	// If agentConfig.Models.MergeModels is true, use "merge" mode
	// Otherwise, use "" (default) which means providers are the only source
	if agentConfig != nil && agentConfig.Models != nil && agentConfig.Models.MergeModels {
		modelsConfig.Mode = "merge"
	}

	// Determine API type based on provider
	apiType := "openai-completions"
	if provider == "anthropic" {
		apiType = "anthropic-messages"
	}

	// Get API key from environment variable (uppercase provider name)
	// Controller injects PROVIDER_API_KEY from Secret as Pod env var
	// e.g., DEEPSEEK_API_KEY, ANTHROPIC_API_KEY
	apiKeyEnvVar := fmt.Sprintf("%s_API_KEY", strings.ToUpper(provider))

	modelsConfig.Providers[provider] = &ModelProviderConfig{
		BaseURL: harnessCfg.Model.Endpoint,
		APIKey:  apiKeyEnvVar,
		API:     apiType,
		Models:  c.convertModelListFromSpec(harnessCfg.Model.Models),
	}

	return modelsConfig
}

// convertSkillsConfig converts harness skills config to OpenClaw skills section.
// For v2026.5.7: Skills are per-agent, not global. This config is for endpoint discovery only.
func (c *ConfigConverter) convertSkillsConfig(harnessCfg *handler.HarnessConfig) *SkillsConfig {
	if harnessCfg == nil || harnessCfg.Skills == nil {
		return nil
	}

	// For v2026.5.7: Only endpoint and localPath are valid
	// Skills are defined at agent level (agents.defaults.skills, agents.list[].skills)
	return &SkillsConfig{
		Endpoint:  harnessCfg.Skills.Endpoint,
		LocalPath: harnessCfg.Skills.LocalPath,
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
			ID:            m.Name,
			Name:          m.Name,
			ContextWindow: int(m.ContextWindow),
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

// AgentDefaultsConfig represents default agent configuration for OpenClaw v2026.5.7.
// Schema: model, skills, workspace (identity and thinking are NOT supported at defaults level)
type AgentDefaultsConfig struct {
	Model    *AgentModelConfig `json:"model,omitempty"`
	Skills   []string          `json:"skills,omitempty"`
	Workspace string            `json:"workspace,omitempty"`
}

// AgentConfig represents a single agent configuration for OpenClaw v2026.5.7.
// Schema: id (required), name, description, workspace, model, skills, thinking, agents, etc.
// Note: identity is NOT a top-level field in agents.list[]
type AgentConfig struct {
	ID          string              `json:"id"`
	Name        string              `json:"name,omitempty"`
	Description string              `json:"description,omitempty"`
	Workspace   string              `json:"workspace,omitempty"`
	Model       *AgentModelConfig   `json:"model,omitempty"`
	Skills      []string            `json:"skills,omitempty"`
	Thinking    string              `json:"thinking,omitempty"`
	Agents      []string            `json:"agents,omitempty"`
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

// ModelsConfig represents models configuration for OpenClaw v2026.5.7.
// Schema: mode, providers
type ModelsConfig struct {
	Mode      string                         `json:"mode,omitempty"`
	Providers map[string]*ModelProviderConfig `json:"providers,omitempty"`
}

// ModelsMergeModeConfig represents models merge mode configuration in agentConfig.
// When mergeModels is true, OpenClaw will merge these providers with built-in providers.
type ModelsMergeModeConfig struct {
	// MergeModels indicates whether to merge with built-in model providers
	// If true, OpenClaw config will have models.mode = "merge"
	// If false or unset, providers will be the only source (mode = "" or "override")
	MergeModels bool `json:"mergeModels,omitempty"`
}

// ModelProviderConfig represents a model provider.
type ModelProviderConfig struct {
	BaseURL      string                   `json:"baseUrl,omitempty"`
	APIKey       string                   `json:"apiKey,omitempty"`
	API          string                   `json:"api,omitempty"` // "openai-completions" or "anthropic-messages"
	Models       []*ModelDefinitionConfig `json:"models,omitempty"`
}

// ModelDefinitionConfig represents a model definition.
type ModelDefinitionConfig struct {
	ID            string `json:"id"`
	Name          string `json:"name,omitempty"`
	ContextWindow int    `json:"contextWindow,omitempty"`
}

// SkillsConfig represents skills configuration for OpenClaw v2026.5.7.
// Schema: endpoint, localPath (no "hubType" or "skills" array - skills are per-agent)
// Note: Skills are defined at agent level (agents.defaults.skills, agents.list[].skills)
// The skills config here is only for plugin/endpoint discovery if using external skills hub.
type SkillsConfig struct {
	Endpoint  string `json:"endpoint,omitempty"`
	LocalPath string `json:"localPath,omitempty"`
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

// GatewayConfig represents gateway configuration for OpenClaw v2026.5.7.
// Schema: mode, bind, port, auth (object with mode/token), controlUi, trustedProxies
type GatewayConfig struct {
	Mode          string           `json:"mode,omitempty"`
	Bind          string           `json:"bind,omitempty"`
	Port          int              `json:"port,omitempty"`
	Auth          *GatewayAuth     `json:"auth,omitempty"`
	ControlUI     *ControlUIConfig `json:"controlUi,omitempty"`
	TrustedProxies []string        `json:"trustedProxies,omitempty"`
}

// GatewayAuth represents gateway authentication for v2026.5.7.
type GatewayAuth struct {
	Mode  string `json:"mode,omitempty"`
	Token string `json:"token,omitempty"`
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
	Enabled      bool     `json:"enabled,omitempty"`
	Token        string   `json:"token,omitempty"`
	AllowedUsers []string `json:"allowedUsers,omitempty"` // Telegram user IDs whitelist
}

// SecretInput represents a secret reference for OpenClaw v2026.5.7.
// Schema: source (env|file|value), provider (default), id (env var name or file path)
type SecretInput struct {
	Source   string `json:"source"`           // "env" | "file" | "value"
	Provider string `json:"provider,omitempty"` // "default"
	ID       string `json:"id"`               // Environment variable name or file path
}

// DiscordConfig represents Discord channel config for OpenClaw v2026.5.7.
// Schema: enabled, token (env var name), dmPolicy, allowFrom, mentionRequired
type DiscordConfig struct {
	Enabled         bool                   `json:"enabled,omitempty"`
	Token           *SecretInput           `json:"token,omitempty"`      // Secret ref for DISCORD_BOT_TOKEN
	AllowFrom       []string               `json:"allowFrom,omitempty"`
	DmPolicy        string                 `json:"dmPolicy,omitempty"`   // "all" | "pairing" | "allowlist" | "disabled"
	MentionRequired bool                   `json:"mentionRequired,omitempty"`
	Guilds          map[string]interface{} `json:"guilds,omitempty"`
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

// PluginEntry represents a single plugin entry in plugins.entries.
type PluginEntry struct {
	Enabled bool `json:"enabled"`
}

// PluginsConfig represents OpenClaw plugins configuration.
type PluginsConfig struct {
	// Load configuration for plugin discovery
	Load *PluginsLoadConfig `json:"load,omitempty"`
	// Entries lists enabled plugins (e.g., {"discord": {"enabled": true}})
	Entries map[string]PluginEntry `json:"entries,omitempty"`
}

// PluginsLoadConfig specifies plugin discovery paths.
type PluginsLoadConfig struct {
	// Paths to search for plugins
	Paths []string `json:"paths,omitempty"`
}

// containsString checks if a string is in a slice.
func containsString(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}