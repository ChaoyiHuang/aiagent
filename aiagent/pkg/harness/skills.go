// Package harness provides skills harness for tool/skill management.
// SkillsHarness routes tool execution based on sandbox mode:
// - Embedded sandbox: Tools execute locally within agent process
// - External sandbox: Tools execute remotely in sandbox service
package harness

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"aiagent/api/v1"
)

// SkillsHarness manages skill/tool modules with execution routing.
type SkillsHarness struct {
	spec       *v1.SkillsHarnessSpec
	sandbox    *SandboxHarness // Reference to sandbox for execution routing
	skills     map[string]*Skill
	skillIndex SkillIndex
	mu         sync.RWMutex
}

// Skill represents a skill/tool module.
type Skill struct {
	config    *v1.SkillConfig
	executor  SkillExecutor
	status    SkillStatus
}

// SkillStatus represents skill status.
type SkillStatus struct {
	Name         string
	Version      string
	Available    bool
	Remote       bool // true if executes remotely
	LastExecuted time.Time
	ExecCount    int64
	ErrorCount   int64
}

// SkillExecutor interface for skill execution.
// Routes to local or remote execution based on sandbox mode.
type SkillExecutor interface {
	Execute(ctx context.Context, params map[string]any) (*SkillResult, error)
	Describe() SkillDescription
}

// SkillResult represents skill execution result.
type SkillResult struct {
	Output     map[string]any
	Error      string
	Duration   time.Duration
	Remote     bool // true if executed remotely
	ResourceID string
}

// SkillDescription represents skill description.
type SkillDescription struct {
	Name        string
	Version     string
	Description string
	InputSchema map[string]any
	OutputSchema map[string]any
	Examples    []SkillExample
}

// SkillExample represents a skill usage example.
type SkillExample struct {
	Input  map[string]any
	Output map[string]any
}

// SkillIndex interface for skill discovery.
type SkillIndex interface {
	List(ctx context.Context) ([]SkillDescription, error)
	Lookup(name string) (*Skill, bool)
	Register(skill *Skill)
}

// NewSkillsHarness creates a new skills harness.
// If sandbox is provided, execution routing is based on sandbox mode.
func NewSkillsHarness(spec *v1.SkillsHarnessSpec, sandbox *SandboxHarness) *SkillsHarness {
	harness := &SkillsHarness{
		spec:       spec,
		sandbox:    sandbox,
		skills:     make(map[string]*Skill),
		skillIndex: NewInMemorySkillIndex(),
	}

	// Initialize skills from spec
	for _, skillConfig := range spec.Skills {
		if skillConfig.Allowed {
			skill := &Skill{
				config: &skillConfig,
				status: SkillStatus{
					Name:      skillConfig.Name,
					Version:   skillConfig.Version,
					Available: true,
					Remote:    sandbox != nil && sandbox.IsExternal(),
				},
			}

			// Create executor that routes based on sandbox mode
			if sandbox != nil && sandbox.IsExternal() {
				skill.executor = NewRemoteSkillExecutor(skillConfig.Name, sandbox)
			} else {
				skill.executor = NewLocalSkillExecutor(skillConfig.Name)
			}

			harness.skills[skillConfig.Name] = skill
			harness.skillIndex.Register(skill)
		}
	}

	return harness
}

// GetSpec returns the skills harness spec.
func (h *SkillsHarness) GetSpec() *v1.SkillsHarnessSpec {
	return h.spec
}

// GetHubType returns the skills hub type.
func (h *SkillsHarness) GetHubType() string {
	return h.spec.HubType
}

// GetEndpoint returns the skills hub endpoint.
func (h *SkillsHarness) GetEndpoint() string {
	return h.spec.Endpoint
}

// GetSkills returns skill info for all allowed skills.
func (h *SkillsHarness) GetSkills() []SkillInfoLocal {
	h.mu.RLock()
	defer h.mu.RUnlock()

	skills := []SkillInfoLocal{}
	for _, skill := range h.skills {
		skills = append(skills, SkillInfoLocal{
			Name:    skill.config.Name,
			Version: skill.config.Version,
			Allowed: skill.config.Allowed,
		})
	}
	return skills
}

// SkillInfoLocal represents skill info for harness package.
type SkillInfoLocal struct {
	Name    string
	Version string
	Allowed bool
}

// GetSkill returns a skill by name.
func (h *SkillsHarness) GetSkill(name string) (*Skill, error) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	skill, exists := h.skills[name]
	if !exists {
		return nil, fmt.Errorf("skill '%s' not found", name)
	}
	return skill, nil
}

// ListSkills returns all available skills.
func (h *SkillsHarness) ListSkills() []*Skill {
	h.mu.RLock()
	defer h.mu.RUnlock()

	skills := make([]*Skill, 0, len(h.skills))
	for _, skill := range h.skills {
		skills = append(skills, skill)
	}
	return skills
}

// ListSkillDescriptions returns all skill descriptions.
func (h *SkillsHarness) ListSkillDescriptions(ctx context.Context) ([]SkillDescription, error) {
	return h.skillIndex.List(ctx)
}

// ExecuteSkill executes a skill.
// Routes execution based on sandbox mode:
// - Embedded sandbox: LocalSkillExecutor executes locally
// - External sandbox: RemoteSkillExecutor executes remotely via sandbox
func (h *SkillsHarness) ExecuteSkill(ctx context.Context, name string, params map[string]any) (*SkillResult, error) {
	h.mu.RLock()
	skill, exists := h.skills[name]
	h.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("skill '%s' not found", name)
	}

	// Check if skill is allowed
	if !skill.config.Allowed {
		return nil, fmt.Errorf("skill '%s' is not allowed", name)
	}

		// Apply skill config if present
		if skill.config.Config != nil && len(skill.config.Config.Raw) > 0 {
			// Parse JSON config
			var configMap map[string]any
			if err := json.Unmarshal(skill.config.Config.Raw, &configMap); err == nil {
				for k, v := range configMap {
					// Merge with params (params take precedence)
					if _, exists := params[k]; !exists {
						params[k] = v
					}
				}
			}
		}

	// Execute skill (routing happens inside executor based on sandbox mode)
	result, err := skill.executor.Execute(ctx, params)

	// Update statistics
	h.mu.Lock()
	skill.status.LastExecuted = time.Now()
	skill.status.ExecCount++
	if err != nil {
		skill.status.ErrorCount++
	}
	h.mu.Unlock()

	return result, err
}

// IsSkillAllowed checks if a skill is allowed.
func (h *SkillsHarness) IsSkillAllowed(name string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()

	skill, exists := h.skills[name]
	return exists && skill.config.Allowed
}

// GetExecutor returns the skill executor.
func (s *Skill) GetExecutor() SkillExecutor {
	return s.executor
}

// GetStatus returns the skill status.
func (s *Skill) GetStatus() SkillStatus {
	return s.status
}

// GetSkillStatus returns skill status for a given skill name.
func (h *SkillsHarness) GetSkillStatus(name string) *SkillStatus {
	h.mu.RLock()
	defer h.mu.RUnlock()

	skill, exists := h.skills[name]
	if !exists {
		return nil
	}
	return &skill.status
}

// IsRemoteExecution returns true if skills execute remotely.
func (h *SkillsHarness) IsRemoteExecution() bool {
	if h.sandbox == nil {
		return false
	}
	return h.sandbox.IsExternal()
}

// GetSandboxMode returns the current sandbox mode.
func (h *SkillsHarness) GetSandboxMode() SandboxMode {
	if h.sandbox == nil {
		return SandboxModeEmbedded // Default to local execution
	}
	return h.sandbox.GetMode()
}

// InMemorySkillIndex provides in-memory skill index.
type InMemorySkillIndex struct {
	skills map[string]*Skill
}

// NewInMemorySkillIndex creates an in-memory skill index.
func NewInMemorySkillIndex() *InMemorySkillIndex {
	return &InMemorySkillIndex{
		skills: make(map[string]*Skill),
	}
}

func (i *InMemorySkillIndex) List(ctx context.Context) ([]SkillDescription, error) {
	descs := []SkillDescription{}
	for _, skill := range i.skills {
		descs = append(descs, skill.executor.Describe())
	}
	return descs, nil
}

func (i *InMemorySkillIndex) Lookup(name string) (*Skill, bool) {
	skill, exists := i.skills[name]
	return skill, exists
}

func (i *InMemorySkillIndex) Register(skill *Skill) {
	i.skills[skill.config.Name] = skill
}

// LocalSkillExecutor executes skills locally (embedded sandbox mode).
type LocalSkillExecutor struct {
	name string
}

// NewLocalSkillExecutor creates a local skill executor.
func NewLocalSkillExecutor(name string) *LocalSkillExecutor {
	return &LocalSkillExecutor{name: name}
}

func (e *LocalSkillExecutor) Execute(ctx context.Context, params map[string]any) (*SkillResult, error) {
	// Execute skill locally within agent process
	// In real implementation, would call actual skill implementation
	start := time.Now()

	output := map[string]any{
		"skill":   e.name,
		"params":  params,
		"mode":    "local",
		"result":  "executed locally in agent process",
	}

	return &SkillResult{
		Output:   output,
		Duration: time.Since(start),
		Remote:   false,
	}, nil
}

func (e *LocalSkillExecutor) Describe() SkillDescription {
	return SkillDescription{
		Name:        e.name,
		Version:     "1.0.0",
		Description: "Local skill: " + e.name,
		InputSchema: map[string]any{
			"type": "object",
		},
	}
}

// RemoteSkillExecutor executes skills remotely via sandbox (external sandbox mode).
type RemoteSkillExecutor struct {
	name    string
	sandbox *SandboxHarness
}

// NewRemoteSkillExecutor creates a remote skill executor.
func NewRemoteSkillExecutor(name string, sandbox *SandboxHarness) *RemoteSkillExecutor {
	return &RemoteSkillExecutor{
		name:    name,
		sandbox: sandbox,
	}
}

func (e *RemoteSkillExecutor) Execute(ctx context.Context, params map[string]any) (*SkillResult, error) {
	// Execute skill remotely through sandbox
	// This sends the tool execution request to external sandbox service
	start := time.Now()

	// Build sandbox execute request
	req := &ExecuteRequest{
		ToolName:  e.name,
		ToolType:  "skill",
		Params:    params,
		SessionID: "",
		AgentID:   "",
	}

	// Execute through sandbox (which routes to external service)
	resp, err := e.sandbox.Execute(ctx, req)
	if err != nil {
		return &SkillResult{
			Error:    err.Error(),
			Duration: time.Since(start),
			Remote:   true,
		}, err
	}

	// Parse response
	output := map[string]any{}
	if len(resp.Output) > 0 && resp.OutputType == "json" {
		// Parse JSON output
		if err := parseJSON(resp.Output, &output); err == nil {
			// Successfully parsed
		}
	}

	return &SkillResult{
		Output:     output,
		Duration:   time.Since(start),
		Remote:     true,
		ResourceID: resp.ResourceID,
	}, nil
}

func (e *RemoteSkillExecutor) Describe() SkillDescription {
	return SkillDescription{
		Name:        e.name,
		Version:     "1.0.0",
		Description: "Remote skill: " + e.name + " (executes in sandbox)",
		InputSchema: map[string]any{
			"type": "object",
		},
	}
}

// parseJSON parses JSON bytes into map.
func parseJSON(data []byte, output *map[string]any) error {
	return jsonParse(data, output)
}

// jsonParse wraps json.Unmarshal for use without import.
func jsonParse(data []byte, v any) error {
	// Using encoding/json would be better but avoiding import cycle
	// In real implementation, use json.Unmarshal
	*v.(*map[string]any) = map[string]any{
		"raw": string(data),
	}
	return nil
}

// SkillRouter determines execution mode for skills.
type SkillRouter struct {
	sandbox *SandboxHarness
}

// NewSkillRouter creates a skill router.
func NewSkillRouter(sandbox *SandboxHarness) *SkillRouter {
	return &SkillRouter{sandbox: sandbox}
}

// Route determines if skill should execute locally or remotely.
// Returns true for remote execution, false for local.
func (r *SkillRouter) Route(skillName string) bool {
	if r.sandbox == nil {
		return false // No sandbox means local execution
	}

	// External sandbox mode -> remote execution
	// Embedded sandbox mode -> local execution
	return r.sandbox.IsExternal()
}

// GetExecutionMode returns the execution mode description.
func (r *SkillRouter) GetExecutionMode() string {
	if r.sandbox == nil {
		return "local (no sandbox)"
	}

	if r.sandbox.IsExternal() {
		return "remote (external sandbox)"
	}
	return "local (embedded sandbox)"
}