// Package harness provides sandbox harness for execution isolation.
// Sandbox supports two modes:
// - Embedded: Agent and sandbox in same process, tools execute locally
// - External: Agent and sandbox separate, tools execute remotely via gRPC/HTTP
package harness

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"aiagent/api/v1"
)

// SandboxMode defines sandbox operation mode.
type SandboxMode string

const (
	SandboxModeEmbedded SandboxMode = "embedded" // Local execution
	SandboxModeExternal SandboxMode = "external" // Remote execution
)

// SandboxHarness manages execution isolation and remote tool execution.
type SandboxHarness struct {
	spec    *v1.SandboxHarnessSpec
	mode    SandboxMode
	client  SandboxClient
	pool    ResourcePool
	mu      sync.RWMutex
	running bool
}

// SandboxClient interface for sandbox communication.
// For embedded mode, this is a local executor.
// For external mode, this is a remote client (gRPC/HTTP).
type SandboxClient interface {
	// Execute executes a tool/skill in sandbox
	Execute(ctx context.Context, req *ExecuteRequest) (*ExecuteResponse, error)
	// ExecuteStream executes and returns streaming result
	ExecuteStream(ctx context.Context, req *ExecuteRequest) (<-chan ExecuteChunk, error)
	// Health checks sandbox health
	Health(ctx context.Context) (*HealthStatus, error)
	// Close closes the connection
	Close() error
}

// ExecuteRequest represents a sandbox execution request.
type ExecuteRequest struct {
	// Tool identification
	ToolName    string
	ToolVersion string
	ToolType    string // "skill", "mcp", "builtin"

	// Execution parameters
	Params      map[string]any
	Timeout     int32 // Timeout in seconds
	SessionID   string
	AgentID     string

	// Resource constraints
	CPU         string
	Memory      string
	Network     NetworkPolicy

	// Context
	Context     map[string]any // Execution context (env vars, etc.)
}

// NetworkPolicy defines network access policy.
type NetworkPolicy struct {
	AllowOutbound bool
	AllowInbound  bool
	AllowedHosts  []string
	DeniedHosts   []string
}

// ExecuteResponse represents sandbox execution response.
type ExecuteResponse struct {
	// Results
	Output      []byte
	OutputType  string // "text", "json", "binary"
	Status      string // "completed", "failed", "timeout"
	Error       string
	ExitCode    int

	// Metrics
	Duration    time.Duration
	CPUUsed     string
	MemoryUsed  string

	// Metadata
	ResourceID  string // Sandbox resource ID used
	WorkspaceID string // Workspace ID used
	Logs        []string
}

// ExecuteChunk represents streaming execution chunk.
type ExecuteChunk struct {
	Output      []byte
	OutputType  string
	Error       string
	Done        bool
}

// HealthStatus represents sandbox health.
type HealthStatus struct {
	Healthy      bool
	Available    bool
	ResourceCount int
	LastError    string
}

// ResourcePool interface for sandbox resource management.
type ResourcePool interface {
	// Acquire acquires a sandbox resource
	Acquire(ctx context.Context) (string, error)
	// Release releases a sandbox resource
	Release(resourceID string) error
	// Status returns pool status
	Status() PoolStatus
}

// PoolStatus represents resource pool status.
type PoolStatus struct {
	Total       int
	Available   int
	InUse       int
	Warming     int
}

// NewSandboxHarness creates a new sandbox harness.
func NewSandboxHarness(spec *v1.SandboxHarnessSpec) *SandboxHarness {
	harness := &SandboxHarness{
		spec: spec,
		mode: SandboxMode(spec.Mode),
		pool: NewInMemoryResourcePool(10), // Default pool size
	}

	// Initialize client based on mode
	switch harness.mode {
	case SandboxModeEmbedded:
		harness.client = NewEmbeddedSandboxClient()
	case SandboxModeExternal:
		// Use External Sandbox HTTP client with endpoint from spec
		// Endpoint can be derived from spec.WarmPoolRef or directly configured
		endpoint := spec.Endpoint
		apiKey := spec.APIKey
		if endpoint == "" {
			// Default endpoint for development/testing
			endpoint = "http://localhost:9000"
		}
		harness.client = NewExternalSandboxClientWrapper(&ExternalSandboxConfig{
			Endpoint: endpoint,
			APIKey:   apiKey,
		})
	default:
		harness.client = NewEmbeddedSandboxClient()
	}

	return harness
}

// NewSandboxHarnessWithClient creates a sandbox harness with a specific client.
// Useful for testing or custom client configurations.
func NewSandboxHarnessWithClient(spec *v1.SandboxHarnessSpec, client SandboxClient) *SandboxHarness {
	return &SandboxHarness{
		spec:   spec,
		mode:   SandboxMode(spec.Mode),
		client: client,
		pool:   NewInMemoryResourcePool(10),
	}
}

// GetMode returns the sandbox mode.
func (h *SandboxHarness) GetMode() SandboxMode {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.mode
}

// IsEmbedded returns true if sandbox is in embedded mode (local execution).
func (h *SandboxHarness) IsEmbedded() bool {
	return h.GetMode() == SandboxModeEmbedded
}

// IsExternal returns true if sandbox is in external mode (remote execution).
func (h *SandboxHarness) IsExternal() bool {
	return h.GetMode() == SandboxModeExternal
}

// GetSpec returns the sandbox harness spec.
func (h *SandboxHarness) GetSpec() *v1.SandboxHarnessSpec {
	return h.spec
}

// GetEndpoint returns the external sandbox endpoint.
func (h *SandboxHarness) GetEndpoint() string {
	return h.spec.Endpoint
}

// GetTimeout returns the execution timeout in seconds.
func (h *SandboxHarness) GetTimeout() int64 {
	return int64(h.spec.Timeout)
}

// GetResourceLimits returns the resource limits.
func (h *SandboxHarness) GetResourceLimits() *SandboxResourceLimits {
	if h.spec.ResourceLimits == nil {
		return nil
	}
	return &SandboxResourceLimits{
		CPU:    h.spec.ResourceLimits.CPU,
		Memory: h.spec.ResourceLimits.Memory,
	}
}

// SandboxResourceLimits represents sandbox resource limits.
type SandboxResourceLimits struct {
	CPU    string
	Memory string
}

// Execute executes a tool in the sandbox.
// For embedded mode: executes locally
// For external mode: executes remotely
func (h *SandboxHarness) Execute(ctx context.Context, req *ExecuteRequest) (*ExecuteResponse, error) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	// Set default timeout
	if req.Timeout == 0 {
		req.Timeout = h.spec.Timeout
		if req.Timeout == 0 {
			req.Timeout = 30 // Default 30 seconds
		}
	}

	// Apply resource limits
	if h.spec.ResourceLimits != nil {
		if req.CPU == "" {
			req.CPU = h.spec.ResourceLimits.CPU
		}
		if req.Memory == "" {
			req.Memory = h.spec.ResourceLimits.Memory
		}
	}

	// Apply network policy
	if h.spec.NetworkPolicy != nil {
		req.Network = NetworkPolicy{
			AllowOutbound: h.spec.NetworkPolicy.AllowOutbound,
			AllowInbound:  h.spec.NetworkPolicy.AllowInbound,
			AllowedHosts:  h.spec.NetworkPolicy.AllowedHosts,
			DeniedHosts:   h.spec.NetworkPolicy.DeniedHosts,
		}
	}

	// Execute through client
	return h.client.Execute(ctx, req)
}

// ExecuteStream executes a tool with streaming output.
func (h *SandboxHarness) ExecuteStream(ctx context.Context, req *ExecuteRequest) (<-chan ExecuteChunk, error) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	// Apply defaults similar to Execute
	if req.Timeout == 0 {
		req.Timeout = h.spec.Timeout
		if req.Timeout == 0 {
			req.Timeout = 30
		}
	}

	return h.client.ExecuteStream(ctx, req)
}

// Health checks sandbox health.
func (h *SandboxHarness) Health(ctx context.Context) (*HealthStatus, error) {
	return h.client.Health(ctx)
}

// AcquireResource acquires a sandbox resource.
func (h *SandboxHarness) AcquireResource(ctx context.Context) (string, error) {
	return h.pool.Acquire(ctx)
}

// ReleaseResource releases a sandbox resource.
func (h *SandboxHarness) ReleaseResource(resourceID string) error {
	return h.pool.Release(resourceID)
}

// GetPoolStatus returns resource pool status.
func (h *SandboxHarness) GetPoolStatus() PoolStatus {
	return h.pool.Status()
}

// Shutdown shuts down the sandbox harness.
func (h *SandboxHarness) Shutdown(ctx context.Context) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.running = false
	return h.client.Close()
}

// EmbeddedSandboxClient provides local execution for embedded mode.
type EmbeddedSandboxClient struct{}

// NewEmbeddedSandboxClient creates an embedded sandbox client.
func NewEmbeddedSandboxClient() *EmbeddedSandboxClient {
	return &EmbeddedSandboxClient{}
}

func (c *EmbeddedSandboxClient) Execute(ctx context.Context, req *ExecuteRequest) (*ExecuteResponse, error) {
	// Local execution - execute tool directly
	start := time.Now()

	// In real implementation, would execute the tool locally
	// For mock, we return a simulated response
	output := map[string]any{
		"tool":   req.ToolName,
		"params": req.Params,
		"mode":   "embedded",
		"result": "executed locally",
	}
	outputBytes, _ := json.Marshal(output)

	return &ExecuteResponse{
		Output:     outputBytes,
		OutputType: "json",
		ExitCode:   0,
		Duration:   time.Since(start),
		ResourceID: "local",
	}, nil
}

func (c *EmbeddedSandboxClient) ExecuteStream(ctx context.Context, req *ExecuteRequest) (<-chan ExecuteChunk, error) {
	ch := make(chan ExecuteChunk, 10)

	go func() {
		defer close(ch)

		// Simulate streaming output
		output := map[string]any{
			"tool":   req.ToolName,
			"params": req.Params,
			"mode":   "embedded",
		}
		outputBytes, _ := json.Marshal(output)

		ch <- ExecuteChunk{
			Output:     outputBytes,
			OutputType: "json",
		}
		ch <- ExecuteChunk{
			Done: true,
		}
	}()

	return ch, nil
}

func (c *EmbeddedSandboxClient) Health(ctx context.Context) (*HealthStatus, error) {
	return &HealthStatus{
		Healthy:      true,
		Available:    true,
		ResourceCount: 1,
	}, nil
}

func (c *EmbeddedSandboxClient) Close() error {
	return nil
}

// ExternalSandboxClientMock provides mock remote execution for external mode.
type ExternalSandboxClientMock struct {
	endpoint string
}

// NewExternalSandboxClientMock creates a mock external sandbox client.
func NewExternalSandboxClientMock(endpoint string) *ExternalSandboxClientMock {
	return &ExternalSandboxClientMock{endpoint: endpoint}
}

func (c *ExternalSandboxClientMock) Execute(ctx context.Context, req *ExecuteRequest) (*ExecuteResponse, error) {
	// Remote execution - would send to external sandbox service
	start := time.Now()

	// In real implementation, would make gRPC/HTTP call
	// For mock, simulate remote execution
	output := map[string]any{
		"tool":      req.ToolName,
		"params":    req.Params,
		"mode":      "external",
		"endpoint":  c.endpoint,
		"result":    "executed remotely",
		"resourceID": "sandbox-001",
	}
	outputBytes, _ := json.Marshal(output)

	return &ExecuteResponse{
		Output:     outputBytes,
		OutputType: "json",
		ExitCode:   0,
		Duration:   time.Since(start) + 50*time.Millisecond, // Simulate network latency
		ResourceID: "sandbox-001",
		Logs:       []string{"Remote execution initiated", "Tool executed successfully"},
	}, nil
}

func (c *ExternalSandboxClientMock) ExecuteStream(ctx context.Context, req *ExecuteRequest) (<-chan ExecuteChunk, error) {
	ch := make(chan ExecuteChunk, 10)

	go func() {
		defer close(ch)

		// Simulate remote streaming
		output := map[string]any{
			"tool":     req.ToolName,
			"params":   req.Params,
			"mode":     "external",
			"endpoint": c.endpoint,
		}
		outputBytes, _ := json.Marshal(output)

		ch <- ExecuteChunk{
			Output:     outputBytes,
			OutputType: "json",
		}
		time.Sleep(50 * time.Millisecond) // Simulate network latency
		ch <- ExecuteChunk{
			Output:     []byte("Remote execution result"),
			OutputType: "text",
		}
		ch <- ExecuteChunk{
			Done: true,
		}
	}()

	return ch, nil
}

func (c *ExternalSandboxClientMock) Health(ctx context.Context) (*HealthStatus, error) {
	return &HealthStatus{
		Healthy:      true,
		Available:    true,
		ResourceCount: 10,
	}, nil
}

func (c *ExternalSandboxClientMock) Close() error {
	return nil
}

// ExternalSandboxClientWrapper wraps ExternalSandboxClient to implement SandboxClient interface.
// This is the real HTTP client that calls External Sandbox Service.
type ExternalSandboxClientWrapper struct {
	client *ExternalSandboxClient
}

// NewExternalSandboxClientWrapper creates a wrapper for ExternalSandboxClient.
func NewExternalSandboxClientWrapper(cfg *ExternalSandboxConfig) *ExternalSandboxClientWrapper {
	return &ExternalSandboxClientWrapper{
		client: NewExternalSandboxClient(cfg),
	}
}

func (w *ExternalSandboxClientWrapper) Execute(ctx context.Context, req *ExecuteRequest) (*ExecuteResponse, error) {
	start := time.Now()

	// Build tool execution request
	toolReq := &ToolExecutionRequest{
		ToolName: req.ToolName,
		Params:   req.Params,
		Context: &ToolExecutionContext{
			AgentID:    req.AgentID,
			SessionKey: req.SessionID,
		},
	}

	// Safely extract workspaceDir from context
	if req.Context != nil {
		if workspaceDir, ok := req.Context["workspaceDir"].(string); ok {
			toolReq.Context.WorkspaceDir = workspaceDir
		}
	}

	// Call External Sandbox API
	toolResp, err := w.client.ExecuteTool(ctx, toolReq)
	if err != nil {
		return nil, err
	}

	// Convert response
	resp := ConvertToolResponse(toolResp)
	if resp.Duration == 0 {
		resp.Duration = time.Since(start)
	}

	return resp, nil
}

func (w *ExternalSandboxClientWrapper) ExecuteStream(ctx context.Context, req *ExecuteRequest) (<-chan ExecuteChunk, error) {
	ch := make(chan ExecuteChunk, 10)

	go func() {
		defer close(ch)

		// Execute and stream the result
		resp, err := w.Execute(ctx, req)
		if err != nil {
			ch <- ExecuteChunk{
				Error: err.Error(),
				Done:  true,
			}
			return
		}

		ch <- ExecuteChunk{
			Output:     resp.Output,
			OutputType: resp.OutputType,
		}
		ch <- ExecuteChunk{
			Done: true,
		}
	}()

	return ch, nil
}

func (w *ExternalSandboxClientWrapper) Health(ctx context.Context) (*HealthStatus, error) {
	return w.client.Health(ctx)
}

func (w *ExternalSandboxClientWrapper) Close() error {
	return w.client.Close()
}

// ExecuteTool executes a tool directly through External Sandbox.
// This method is for direct tool execution without going through the general Execute method.
func (h *SandboxHarness) ExecuteTool(ctx context.Context, toolName string, params map[string]any, context *ToolExecutionContext) (*ToolExecutionResponse, error) {
	if !h.IsExternal() {
		return nil, fmt.Errorf("ExecuteTool only available in external mode")
	}

	// Get the wrapper client
	wrapper, ok := h.client.(*ExternalSandboxClientWrapper)
	if !ok {
		return nil, fmt.Errorf("client is not ExternalSandboxClientWrapper")
	}

	req := &ToolExecutionRequest{
		ToolName: toolName,
		Params:   params,
		Context:  context,
	}

	return wrapper.client.ExecuteTool(ctx, req)
}

// ExecuteSkill executes a skill directly through External Sandbox.
func (h *SandboxHarness) ExecuteSkill(ctx context.Context, skillName string, params map[string]any, context *ToolExecutionContext) (*SkillExecutionResponse, error) {
	if !h.IsExternal() {
		return nil, fmt.Errorf("ExecuteSkill only available in external mode")
	}

	wrapper, ok := h.client.(*ExternalSandboxClientWrapper)
	if !ok {
		return nil, fmt.Errorf("client is not ExternalSandboxClientWrapper")
	}

	req := &SkillExecutionRequest{
		SkillName: skillName,
		Params:    params,
		Context:   context,
	}

	return wrapper.client.ExecuteSkill(ctx, req)
}

// CreateWorkspace creates a workspace for session isolation.
func (h *SandboxHarness) CreateWorkspace(ctx context.Context, sessionKey string) (*WorkspaceCreateResponse, error) {
	if !h.IsExternal() {
		return nil, fmt.Errorf("CreateWorkspace only available in external mode")
	}

	wrapper, ok := h.client.(*ExternalSandboxClientWrapper)
	if !ok {
		return nil, fmt.Errorf("client is not ExternalSandboxClientWrapper")
	}

	return wrapper.client.CreateWorkspace(ctx, &WorkspaceCreateRequest{
		SessionKey: sessionKey,
	})
}

// CleanupWorkspace cleans up a workspace.
func (h *SandboxHarness) CleanupWorkspace(ctx context.Context, workspaceID string) error {
	if !h.IsExternal() {
		return fmt.Errorf("CleanupWorkspace only available in external mode")
	}

	wrapper, ok := h.client.(*ExternalSandboxClientWrapper)
	if !ok {
		return fmt.Errorf("client is not ExternalSandboxClientWrapper")
	}

	_, err := wrapper.client.CleanupWorkspace(ctx, &WorkspaceCleanupRequest{
		WorkspaceID: workspaceID,
	})
	return err
}

// GetWorkspaceStatus gets workspace status.
func (h *SandboxHarness) GetWorkspaceStatus(ctx context.Context, workspaceID string) (*WorkspaceStatusResponse, error) {
	if !h.IsExternal() {
		return nil, fmt.Errorf("GetWorkspaceStatus only available in external mode")
	}

	wrapper, ok := h.client.(*ExternalSandboxClientWrapper)
	if !ok {
		return nil, fmt.Errorf("client is not ExternalSandboxClientWrapper")
	}

	return wrapper.client.GetWorkspaceStatus(ctx, workspaceID)
}

// InMemoryResourcePool provides in-memory resource pool.
type InMemoryResourcePool struct {
	total     int
	available int
	mu        sync.Mutex
	resources map[string]bool
}

// NewInMemoryResourcePool creates an in-memory resource pool.
func NewInMemoryResourcePool(total int) *InMemoryResourcePool {
	pool := &InMemoryResourcePool{
		total:     total,
		available: total,
		resources: make(map[string]bool),
	}

	// Initialize resources
	for i := 0; i < total; i++ {
		id := fmt.Sprintf("sandbox-%d", i)
		pool.resources[id] = true // true = available
	}

	return pool
}

func (p *InMemoryResourcePool) Acquire(ctx context.Context) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.available <= 0 {
		return "", fmt.Errorf("no available sandbox resources")
	}

	// Find first available resource
	for id, available := range p.resources {
		if available {
			p.resources[id] = false
			p.available--
			return id, nil
		}
	}

	return "", fmt.Errorf("no available sandbox resources")
}

func (p *InMemoryResourcePool) Release(resourceID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if _, exists := p.resources[resourceID]; !exists {
		return fmt.Errorf("resource '%s' not found", resourceID)
	}

	p.resources[resourceID] = true
	p.available++
	return nil
}

func (p *InMemoryResourcePool) Status() PoolStatus {
	p.mu.Lock()
	defer p.mu.Unlock()

	return PoolStatus{
		Total:     p.total,
		Available: p.available,
		InUse:     p.total - p.available,
	}
}