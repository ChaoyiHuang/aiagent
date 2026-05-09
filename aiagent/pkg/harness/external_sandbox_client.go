// Package harness provides External Sandbox HTTP client for remote tool execution.
// This client implements the External Sandbox API as defined in external-sandbox-final-design.md:
// - POST /tools/{toolName}: File operations and Shell execution (read, write, edit, exec, process)
// - POST /skills/{skillName}: Skills execution
// - POST /workspace/create: Create workspace for session isolation
// - POST /workspace/cleanup: Cleanup workspace
// - GET /workspace/status: Get workspace status
package harness

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// ExternalSandboxClient provides HTTP client for External Sandbox Service.
// Design: Plugin directly calls External Sandbox (no Bridge forwarding)
// Consistent with OpenClaw Docker Sandbox design philosophy.
type ExternalSandboxClient struct {
	endpoint   string
	apiKey     string
	httpClient *http.Client
}

// ExternalSandboxConfig contains configuration for External Sandbox client.
type ExternalSandboxConfig struct {
	// Endpoint is the External Sandbox API URL (e.g., "http://sandbox.example.com:9000")
	Endpoint string

	// APIKey is the authentication key for External Sandbox
	APIKey string

	// Timeout is the HTTP client timeout (default 30s)
	Timeout time.Duration

	// MaxRetries is the maximum retry attempts for transient failures
	MaxRetries int
}

// NewExternalSandboxClient creates a new External Sandbox HTTP client.
func NewExternalSandboxClient(cfg *ExternalSandboxConfig) *ExternalSandboxClient {
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}
	if cfg.MaxRetries == 0 {
		cfg.MaxRetries = 3
	}

	return &ExternalSandboxClient{
		endpoint: cfg.Endpoint,
		apiKey:   cfg.APIKey,
		httpClient: &http.Client{
			Timeout: cfg.Timeout,
		},
	}
}

// ToolExecutionRequest represents a tool execution request to External Sandbox.
// Used for: read, write, edit, apply_patch, exec, process tools.
type ToolExecutionRequest struct {
	// ToolName is the tool to execute (read, write, edit, apply_patch, exec, process)
	ToolName string `json:"toolName"`

	// Params contains tool-specific parameters
	Params map[string]any `json:"params"`

	// ToolCallID is the unique tool call identifier
	ToolCallID string `json:"toolCallId,omitempty"`

	// Context contains execution context
	Context *ToolExecutionContext `json:"context,omitempty"`
}

// ToolExecutionContext contains context for tool execution.
type ToolExecutionContext struct {
	// AgentID is the agent identifier
	AgentID string `json:"agentId,omitempty"`

	// SessionKey is the session key for workspace isolation
	SessionKey string `json:"sessionKey,omitempty"`

	// SessionID is the session identifier
	SessionID string `json:"sessionId,omitempty"`

	// WorkspaceDir is the workspace directory path
	WorkspaceDir string `json:"workspaceDir,omitempty"`

	// RunID is the run identifier
	RunID string `json:"runId,omitempty"`
}

// ToolExecutionResponse represents a tool execution response from External Sandbox.
type ToolExecutionResponse struct {
	// Output is the tool execution output
	Output any `json:"output"`

	// Status is the execution status (completed, failed, timeout)
	Status string `json:"status"`

	// Duration is the execution duration in milliseconds
	Duration int64 `json:"duration"`

	// ExitCode is the exit code (for exec/process tools)
	ExitCode int `json:"exitCode,omitempty"`

	// Error is the error message if execution failed
	Error string `json:"error,omitempty"`

	// SandboxID is the sandbox resource ID
	SandboxID string `json:"sandboxId,omitempty"`

	// WorkspaceID is the workspace ID used
	WorkspaceID string `json:"workspaceId,omitempty"`
}

// SkillExecutionRequest represents a skill execution request to External Sandbox.
type SkillExecutionRequest struct {
	// SkillName is the skill to execute
	SkillName string `json:"skillName"`

	// Params contains skill-specific parameters
	Params map[string]any `json:"params"`

	// Context contains execution context
	Context *ToolExecutionContext `json:"context,omitempty"`
}

// SkillExecutionResponse represents a skill execution response from External Sandbox.
type SkillExecutionResponse struct {
	// Output is the skill execution output
	Output any `json:"output"`

	// Status is the execution status
	Status string `json:"status"`

	// Duration is the execution duration in milliseconds
	Duration int64 `json:"duration"`

	// Error is the error message if execution failed
	Error string `json:"error,omitempty"`

	// SandboxID is the sandbox resource ID
	SandboxID string `json:"sandboxId,omitempty"`

	// WorkspaceID is the workspace ID used
	WorkspaceID string `json:"workspaceId,omitempty"`
}

// WorkspaceCreateRequest represents a workspace creation request.
type WorkspaceCreateRequest struct {
	// SessionKey is the session key for workspace isolation
	SessionKey string `json:"sessionKey"`

	// Scope is the workspace scope (session, shared, global)
	Scope string `json:"scope,omitempty"`
}

// WorkspaceCreateResponse represents a workspace creation response.
type WorkspaceCreateResponse struct {
	// WorkspaceID is the created workspace identifier
	WorkspaceID string `json:"workspaceId"`

	// Path is the workspace path
	Path string `json:"path"`

	// Status is the creation status
	Status string `json:"status"`
}

// WorkspaceCleanupRequest represents a workspace cleanup request.
type WorkspaceCleanupRequest struct {
	// WorkspaceID is the workspace to cleanup
	WorkspaceID string `json:"workspaceId"`
}

// WorkspaceCleanupResponse represents a workspace cleanup response.
type WorkspaceCleanupResponse struct {
	// Success indicates if cleanup was successful
	Success bool `json:"success"`

	// Status is the cleanup status
	Status string `json:"status"`
}

// WorkspaceStatusResponse represents a workspace status response.
type WorkspaceStatusResponse struct {
	// WorkspaceID is the workspace identifier
	WorkspaceID string `json:"workspaceId"`

	// Size is the workspace size in bytes
	Size int64 `json:"size"`

	// Files is the number of files in the workspace
	Files int `json:"files"`

	// LastAccessed is the last access timestamp
	LastAccessed time.Time `json:"lastAccessed"`

	// Status is the workspace status (active, idle, cleanup_pending)
	Status string `json:"status"`

	// CreatedAt is the creation timestamp
	CreatedAt time.Time `json:"createdAt"`
}

// ExecuteTool executes a tool in External Sandbox.
// Supported tools: read, write, edit, apply_patch, exec, process.
func (c *ExternalSandboxClient) ExecuteTool(ctx context.Context, req *ToolExecutionRequest) (*ToolExecutionResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("request is nil")
	}

	url := c.endpoint + "/tools/" + encodeURIComponent(req.ToolName)

	// Build headers with nil-safe context access
	headers := map[string]string{
		"Content-Type":  "application/json",
		"Authorization": "Bearer " + c.apiKey,
	}

	if req.Context != nil {
		headers["X-Agent-Id"] = req.Context.AgentID
		headers["X-Session-Key"] = req.Context.SessionKey
		headers["X-Session-Id"] = req.Context.SessionID
		headers["X-Run-Id"] = req.Context.RunID
		headers["X-Workspace-ID"] = resolveWorkspaceID(req.Context.SessionKey, "workspace-default")
	} else {
		headers["X-Workspace-ID"] = "workspace-default"
	}

	resp, err := c.doPost(ctx, url, req, headers)
	if err != nil {
		return nil, fmt.Errorf("External Sandbox tool execution failed: %w", err)
	}

	var result ToolExecutionResponse
	if err := json.Unmarshal(resp, &result); err != nil {
		return nil, fmt.Errorf("failed to parse tool response: %w", err)
	}

	return &result, nil
}

// ExecuteSkill executes a skill in External Sandbox.
func (c *ExternalSandboxClient) ExecuteSkill(ctx context.Context, req *SkillExecutionRequest) (*SkillExecutionResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("request is nil")
	}

	url := c.endpoint + "/skills/" + encodeURIComponent(req.SkillName)

	headers := map[string]string{
		"Content-Type":  "application/json",
		"Authorization": "Bearer " + c.apiKey,
	}

	if req.Context != nil {
		headers["X-Agent-Id"] = req.Context.AgentID
		headers["X-Session-Key"] = req.Context.SessionKey
		headers["X-Workspace-ID"] = resolveWorkspaceID(req.Context.SessionKey, "workspace-default")
	} else {
		headers["X-Workspace-ID"] = "workspace-default"
	}

	resp, err := c.doPost(ctx, url, req.Params, headers)
	if err != nil {
		return nil, fmt.Errorf("External Sandbox skill execution failed: %w", err)
	}

	var result SkillExecutionResponse
	if err := json.Unmarshal(resp, &result); err != nil {
		return nil, fmt.Errorf("failed to parse skill response: %w", err)
	}

	return &result, nil
}

// CreateWorkspace creates a workspace for session isolation.
func (c *ExternalSandboxClient) CreateWorkspace(ctx context.Context, req *WorkspaceCreateRequest) (*WorkspaceCreateResponse, error) {
	url := c.endpoint + "/workspace/create"

	headers := map[string]string{
		"Content-Type":  "application/json",
		"Authorization": "Bearer " + c.apiKey,
	}

	resp, err := c.doPost(ctx, url, req, headers)
	if err != nil {
		return nil, fmt.Errorf("workspace creation failed: %w", err)
	}

	var result WorkspaceCreateResponse
	if err := json.Unmarshal(resp, &result); err != nil {
		return nil, fmt.Errorf("failed to parse workspace response: %w", err)
	}

	return &result, nil
}

// CleanupWorkspace cleans up a workspace.
func (c *ExternalSandboxClient) CleanupWorkspace(ctx context.Context, req *WorkspaceCleanupRequest) (*WorkspaceCleanupResponse, error) {
	url := c.endpoint + "/workspace/cleanup"

	headers := map[string]string{
		"Content-Type":  "application/json",
		"Authorization": "Bearer " + c.apiKey,
	}

	resp, err := c.doPost(ctx, url, req, headers)
	if err != nil {
		return nil, fmt.Errorf("workspace cleanup failed: %w", err)
	}

	var result WorkspaceCleanupResponse
	if err := json.Unmarshal(resp, &result); err != nil {
		return nil, fmt.Errorf("failed to parse cleanup response: %w", err)
	}

	return &result, nil
}

// GetWorkspaceStatus gets workspace status.
func (c *ExternalSandboxClient) GetWorkspaceStatus(ctx context.Context, workspaceID string) (*WorkspaceStatusResponse, error) {
	url := c.endpoint + "/workspace/status?workspaceId=" + encodeURIComponent(workspaceID)

	headers := map[string]string{
		"Authorization":    "Bearer " + c.apiKey,
		"X-Workspace-ID":   workspaceID,
	}

	resp, err := c.doGet(ctx, url, headers)
	if err != nil {
		return nil, fmt.Errorf("workspace status check failed: %w", err)
	}

	var result WorkspaceStatusResponse
	if err := json.Unmarshal(resp, &result); err != nil {
		return nil, fmt.Errorf("failed to parse status response: %w", err)
	}

	return &result, nil
}

// Health checks External Sandbox health.
func (c *ExternalSandboxClient) Health(ctx context.Context) (*HealthStatus, error) {
	url := c.endpoint + "/health"

	resp, err := c.doGet(ctx, url, map[string]string{})
	if err != nil {
		return &HealthStatus{
			Healthy:   false,
			Available: false,
			LastError: err.Error(),
		}, err
	}

	var health struct {
		Healthy      bool `json:"healthy"`
		Available    bool `json:"available"`
		ResourcePool struct {
			Total     int `json:"total"`
			Available int `json:"available"`
			InUse     int `json:"inUse"`
		} `json:"resourcePool"`
	}

	if err := json.Unmarshal(resp, &health); err != nil {
		return &HealthStatus{
			Healthy:   false,
			Available: false,
			LastError: "failed to parse health response",
		}, err
	}

	return &HealthStatus{
		Healthy:       health.Healthy,
		Available:     health.Available,
		ResourceCount: health.ResourcePool.Available,
	}, nil
}

// doPost performs HTTP POST request.
func (c *ExternalSandboxClient) doPost(ctx context.Context, url string, body any, headers map[string]string) ([]byte, error) {
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	for k, v := range headers {
		if v != "" {
			req.Header.Set(k, v)
		}
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP error %d: %s", resp.StatusCode, string(bodyBytes))
	}

	return io.ReadAll(resp.Body)
}

// doGet performs HTTP GET request.
func (c *ExternalSandboxClient) doGet(ctx context.Context, url string, headers map[string]string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	for k, v := range headers {
		if v != "" {
			req.Header.Set(k, v)
		}
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP error %d: %s", resp.StatusCode, string(bodyBytes))
	}

	return io.ReadAll(resp.Body)
}

// Close closes the HTTP client.
func (c *ExternalSandboxClient) Close() error {
	c.httpClient.CloseIdleConnections()
	return nil
}

// encodeURIComponent encodes a string for URL path component.
func encodeURIComponent(s string) string {
	// Simple URL encoding for path components
	result := ""
	for _, c := range s {
		if isURLSafeChar(c) {
			result += string(c)
		} else {
			result += fmt.Sprintf("%%%02X", c)
		}
	}
	return result
}

// isURLSafeChar checks if a character is safe for URL.
func isURLSafeChar(c rune) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') ||
		c == '-' || c == '_' || c == '.' || c == '~'
}

// resolveWorkspaceID resolves workspace ID from session key.
// Consistent with OpenClaw Sandbox scope=session: workspace-{sessionKey}
func resolveWorkspaceID(sessionKey, defaultWorkspace string) string {
	if sessionKey == "" || sessionKey == "default" {
		return defaultWorkspace
	}
	// Extract session key part (format: "channel:accountId:threadId:sessionId")
	parts := splitString(sessionKey, ":")
	if len(parts) > 0 {
		lastPart := parts[len(parts)-1]
		if lastPart != "" {
			return "workspace-" + lastPart
		}
	}
	return defaultWorkspace
}

// splitString splits a string by separator.
func splitString(s, sep string) []string {
	if s == "" {
		return []string{}
	}
	result := []string{}
	start := 0
	for i := 0; i <= len(s)-len(sep); i++ {
		if s[i:i+len(sep)] == sep {
			result = append(result, s[start:i])
			start = i + len(sep)
			i += len(sep) - 1
		}
	}
	result = append(result, s[start:])
	return result
}

// ConvertToolResponse converts ToolExecutionResponse to ExecuteResponse.
func ConvertToolResponse(toolResp *ToolExecutionResponse) *ExecuteResponse {
	outputBytes, _ := json.Marshal(toolResp.Output)

	return &ExecuteResponse{
		Output:     outputBytes,
		OutputType: "json",
		Status:     toolResp.Status,
		Duration:   time.Duration(toolResp.Duration) * time.Millisecond,
		ExitCode:   toolResp.ExitCode,
		Error:      toolResp.Error,
		ResourceID: toolResp.SandboxID,
		Logs:       []string{},
	}
}

// ConvertSkillResponse converts SkillExecutionResponse to ExecuteResponse.
func ConvertSkillResponse(skillResp *SkillExecutionResponse) *ExecuteResponse {
	outputBytes, _ := json.Marshal(skillResp.Output)

	return &ExecuteResponse{
		Output:     outputBytes,
		OutputType: "json",
		Status:     skillResp.Status,
		Duration:   time.Duration(skillResp.Duration) * time.Millisecond,
		Error:      skillResp.Error,
		ResourceID: skillResp.SandboxID,
		Logs:       []string{},
	}
}