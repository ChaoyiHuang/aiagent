// Package openclaw provides a handler implementation for OpenClaw framework.
//
// IMPORTANT: OpenClaw Remote Tool Execution Analysis
//
// After thorough source code analysis of OpenClaw TypeScript repo, the findings are:
//
// 1. Skills are TEXT PROMPTS, not executable code:
//    - Skills (SKILL.md) are loaded and formatted into system prompt via formatSkillsForPrompt()
//    - LLM is instructed to read SKILL.md files when appropriate using 'read' tool
//    - Skills themselves don't have execute() methods
//
// 2. Skill Command Dispatch (slash commands like /skillname):
//    - If SKILL.md frontmatter has 'command-dispatch: tool', it dispatches to a local JS tool
//    - Tool.execute() is called directly (src/auto-reply/reply/get-reply-inline-actions.ts:222)
//    - This only works for USER slash commands, NOT for LLM automatic tool calls
//
// 3. MCP Protocol Usage:
//    - MCP (mcporter) is ONLY used for memory/search (QMD), NOT for general tool execution
//    - Cannot route skill/tool calls through MCP
//
// 4. Plugin Mechanism:
//    - Plugins register tools via registerTool(factory) - still local JS execute()
//    - Cannot make tools automatically call remote HTTP endpoints
//
// 5. NO HTTP Skill Endpoint Support:
//    - OpenClaw does NOT have built-in HTTP-based skill/tool execution
//    - The HTTP Skill Server in this bridge is for potential future custom plugin integration
//
// CONCLUSION: To enable remote tool execution, a custom OpenClaw plugin MUST be added:
//   - Create a "harness_bridge" plugin with a tool that internally makes HTTP calls
//   - This plugin's tool.execute() sends requests to Harness Bridge HTTP endpoint
//   - Requires adding new code to OpenClaw side (not just config)
//
// The HTTP Skill Server endpoints provided by this bridge:
//   - POST /skills/{skillName} - For custom plugin tool to call
//   - POST /mcp - MCP endpoint (for potential future integration)
//   - POST /health - Health check
//
// Integration flow with custom plugin:
//   OpenClaw -> custom plugin tool -> HTTP to Bridge -> SkillsHarness -> External Sandbox
package openclaw

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"aiagent/pkg/agent"
	"aiagent/pkg/harness"
)

// BridgeConfig contains configuration for the OpenClaw bridge.
type BridgeConfig struct {
	// OpenClawCLIPath is the path to OpenClaw CLI executable.
	OpenClawCLIPath string

	// WorkingDir is the working directory for the subprocess.
	WorkingDir string

	// Timeout is the timeout for bridge operations.
	Timeout time.Duration

	// LogLevel for bridge logging.
	LogLevel string

	// Env environment variables for the subprocess.
	Env map[string]string
}

// DefaultBridgeConfig returns default bridge configuration.
func DefaultBridgeConfig() *BridgeConfig {
	return &BridgeConfig{
		OpenClawCLIPath: "openclaw",
		WorkingDir:      "/app",
		Timeout:         30 * time.Second,
		LogLevel:        "info",
		Env:             make(map[string]string),
	}
}

// OpenClawBridge manages communication with OpenClaw CLI subprocess.
// It also intercepts skill execution requests and routes them based on Sandbox mode.
//
// Two integration modes:
// 1. JSON-RPC mode: OpenClaw CLI runs as subprocess, communicates via stdin/stdout
// 2. HTTP Skill Server mode: Bridge exposes HTTP endpoints for skill execution
//    OpenClaw Agent calls skills via HTTP (configured in agent-config.yaml)
//    This allows OpenClaw to call skills through Harness without modifying OpenClaw code
type OpenClawBridge struct {
	config  *BridgeConfig
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	stdout  io.Reader
	mu      sync.Mutex
	running bool
	// Request/response tracking
	requestID int
	pending   map[int]*pendingRequest

	// HarnessManager for skill execution interception
	// When set, skill calls are routed through Harness layer
	harnessManager *harness.HarnessManager

	// HTTP Skill Server (for HTTP integration mode)
	skillServer     *http.Server
	skillServerPort int
	skillServerMu   sync.Mutex
}

// pendingRequest tracks a pending JSON-RPC request.
type pendingRequest struct {
	response chan *jsonRPCResponse
	done     chan struct{}
}

// jsonRPCRequest represents a JSON-RPC request.
type jsonRPCRequest struct {
	Jsonrpc string      `json:"jsonrpc"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
	ID      int         `json:"id"`
}

// jsonRPCResponse represents a JSON-RPC response.
type jsonRPCResponse struct {
	Jsonrpc string          `json:"jsonrpc"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
	ID      int             `json:"id"`
}

// jsonRPCError represents a JSON-RPC error.
type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// NewOpenClawBridge creates a new OpenClaw bridge.
func NewOpenClawBridge(config *BridgeConfig) *OpenClawBridge {
	if config == nil {
		config = DefaultBridgeConfig()
	}
	return &OpenClawBridge{
		config:  config,
		pending: make(map[int]*pendingRequest),
	}
}

// Start launches the OpenClaw CLI subprocess.
func (b *OpenClawBridge) Start(ctx context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.running {
		return fmt.Errorf("bridge already running")
	}

	// Prepare command
	args := []string{"serve", "--mode=jsonrpc"}
	if b.config.LogLevel != "" {
		args = append(args, "--log-level="+b.config.LogLevel)
	}

	b.cmd = exec.CommandContext(ctx, b.config.OpenClawCLIPath, args...)
	b.cmd.Dir = b.config.WorkingDir

	// Set environment
	if len(b.config.Env) > 0 {
		env := os.Environ()
		for k, v := range b.config.Env {
			env = append(env, k+"="+v)
		}
		b.cmd.Env = env
	}

	// Setup stdin/stdout pipes
	stdin, err := b.cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdin pipe: %w", err)
	}
	b.stdin = stdin

	stdout, err := b.cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}
	b.stdout = stdout

	// Start the process
	if err := b.cmd.Start(); err != nil {
		return fmt.Errorf("failed to start OpenClaw CLI: %w", err)
	}

	b.running = true

	// Start response reader goroutine
	go b.readResponses()

	return nil
}

// Stop terminates the OpenClaw CLI subprocess.
func (b *OpenClawBridge) Stop(ctx context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if !b.running {
		return nil
	}

	// Close stdin to signal shutdown
	if b.stdin != nil {
		b.stdin.Close()
	}

	// Wait for process to exit
	done := make(chan error, 1)
	go func() {
		done <- b.cmd.Wait()
	}()

	select {
	case err := <-done:
		b.running = false
		return err
	case <-time.After(5 * time.Second):
		// Force kill if process doesn't exit gracefully
		b.cmd.Process.Kill()
		b.running = false
		return fmt.Errorf("process killed after timeout")
	case <-ctx.Done():
		b.cmd.Process.Kill()
		b.running = false
		return ctx.Err()
	}
}

// IsRunning returns whether the bridge is running.
func (b *OpenClawBridge) IsRunning() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.running
}

// SetHarnessManager sets the harness manager for skill execution interception.
// When set, skill execution requests are routed through the Harness layer,
// allowing skills to execute either locally (Embedded Sandbox) or remotely
// (External Sandbox) based on the Sandbox mode configuration.
func (b *OpenClawBridge) SetHarnessManager(manager *harness.HarnessManager) {
	b.mu.Lock()
	b.harnessManager = manager
	b.mu.Unlock()
}

// GetHarnessManager returns the harness manager.
func (b *OpenClawBridge) GetHarnessManager() *harness.HarnessManager {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.harnessManager
}

// SetRemoteEndpoint sets the HTTP endpoint for IsolatedMode communication.
// When set, agent operations communicate via HTTP instead of stdin/stdout.
func (b *OpenClawBridge) SetRemoteEndpoint(endpoint string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	// Store endpoint for HTTP client (would be used in IsolatedMode implementation)
	// In full implementation, this would configure an HTTP client
}

// ExecuteSkill intercepts OpenClaw's skill execution and routes based on Sandbox mode.
// This is the key interception point:
// - OpenClaw Agent calls skill via JSON-RPC
// - Bridge intercepts and decides execution location
// - External Sandbox: routes through SkillsHarness -> SandboxHarness
// - Embedded Sandbox: executes locally via OpenClaw CLI
//
// OpenClaw code doesn't need to know about Sandbox - it only sees skill results.
func (b *OpenClawBridge) ExecuteSkill(ctx context.Context, skillName string, params map[string]interface{}) (*SkillExecutionResult, error) {
	b.mu.Lock()
	harnessManager := b.harnessManager
	b.mu.Unlock()

	// If no HarnessManager configured, fallback to local execution
	if harnessManager == nil {
		return b.executeSkillLocal(ctx, skillName, params)
	}

	// Get the relevant harnesses
	skillsHarness := harnessManager.GetSkillsHarness()
	sandboxHarness := harnessManager.GetSandboxHarness()

	// ===== KEY DECISION POINT =====
	// Determine execution mode based on Sandbox configuration
	if sandboxHarness != nil && sandboxHarness.IsExternal() {
		// External Sandbox mode: Execute remotely through Harness layer
		return b.executeSkillRemote(ctx, skillsHarness, skillName, params)
	}

	// Embedded or no Sandbox mode: Execute locally via OpenClaw CLI
	return b.executeSkillLocal(ctx, skillName, params)
}

// executeSkillRemote executes skill through External Sandbox.
// Path: Bridge -> SkillsHarness -> RemoteSkillExecutor -> SandboxHarness -> External Sandbox Service
func (b *OpenClawBridge) executeSkillRemote(ctx context.Context, skillsHarness *harness.SkillsHarness, skillName string, params map[string]interface{}) (*SkillExecutionResult, error) {
	// Convert params type for harness layer
	harnessParams := make(map[string]any)
	for k, v := range params {
		harnessParams[k] = v
	}

	// Execute through Harness layer (will route to External Sandbox)
	result, err := skillsHarness.ExecuteSkill(ctx, skillName, harnessParams)
	if err != nil {
		return nil, fmt.Errorf("remote skill execution failed: %w", err)
	}

	return &SkillExecutionResult{
		Output:     result.Output,
		Error:      result.Error,
		Duration:   result.Duration,
		Remote:     true,              // Mark as remote execution
		ResourceID: result.ResourceID, // Sandbox ID used
	}, nil
}

// executeSkillLocal executes skill locally via OpenClaw CLI.
// Path: Bridge -> JSON-RPC -> OpenClaw CLI -> Local skill execution
func (b *OpenClawBridge) executeSkillLocal(ctx context.Context, skillName string, params map[string]interface{}) (*SkillExecutionResult, error) {
	// Send skill execution request to OpenClaw CLI via JSON-RPC
	rpcParams := map[string]interface{}{
		"skill_name": skillName,
		"params":     params,
	}

	result, err := b.callJSONRPC(ctx, "skill.execute", rpcParams)
	if err != nil {
		return nil, fmt.Errorf("local skill execution failed: %w", err)
	}

	var execResult SkillExecutionResult
	if err := json.Unmarshal(result, &execResult); err != nil {
		return nil, fmt.Errorf("failed to unmarshal skill result: %w", err)
	}

	execResult.Remote = false // Mark as local execution

	return &execResult, nil
}

// SkillExecutionResult represents the result of a skill execution.
// Contains execution metadata including whether it was remote or local.
type SkillExecutionResult struct {
	Output     map[string]any    `json:"output"`
	Error      string            `json:"error,omitempty"`
	Duration   time.Duration     `json:"duration"`
	Remote     bool              `json:"remote"`     // true = External Sandbox, false = Local
	ResourceID string            `json:"resourceId"` // Sandbox ID (only for remote)
	Logs       []string          `json:"logs,omitempty"`
}

// StartSkillServer starts HTTP server for skill execution (HTTP integration mode).
// OpenClaw Agent can call skills via HTTP endpoints based on agent-config.yaml configuration.
//
// Endpoints provided:
//   - POST /skills/{skillName} - Execute a skill
//   - POST /mcp - MCP protocol endpoint for skill discovery
//
// This allows OpenClaw framework to use skills through Harness without code changes:
//   - OpenClaw config specifies: skills.type = "http", endpoint = "http://bridge:port/skills/weather"
//   - OpenClaw framework automatically sends HTTP requests to configured endpoints
//   - Bridge intercepts and routes based on Sandbox mode
func (b *OpenClawBridge) StartSkillServer(ctx context.Context, port int) error {
	b.skillServerMu.Lock()
	defer b.skillServerMu.Unlock()

	if b.skillServer != nil {
		return fmt.Errorf("skill server already running")
	}

	b.skillServerPort = port
	b.skillServer = &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: b.createSkillServerHandler(),
	}

	// Start server in background
	go func() {
		if err := b.skillServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			// Log error but don't crash
		}
	}()

	return nil
}

// StopSkillServer stops the HTTP skill server.
func (b *OpenClawBridge) StopSkillServer(ctx context.Context) error {
	b.skillServerMu.Lock()
	defer b.skillServerMu.Unlock()

	if b.skillServer == nil {
		return nil
	}

	return b.skillServer.Shutdown(ctx)
}

// GetSkillServerPort returns the skill server port.
func (b *OpenClawBridge) GetSkillServerPort() int {
	b.skillServerMu.Lock()
	defer b.skillServerMu.Unlock()
	return b.skillServerPort
}

// createSkillServerHandler creates the HTTP handler for skill server.
func (b *OpenClawBridge) createSkillServerHandler() http.Handler {
	mux := http.NewServeMux()

	// Skill execution endpoint: POST /skills/{skillName}
	mux.HandleFunc("/skills/", b.handleSkillHTTPExecution)

	// MCP protocol endpoint for skill discovery: POST /mcp
	mux.HandleFunc("/mcp", b.handleMCPRequest)

	// Health check endpoint
	mux.HandleFunc("/health", b.handleHealthCheck)

	return mux
}

// handleSkillHTTPExecution handles HTTP skill execution requests from OpenClaw Agent.
func (b *OpenClawBridge) handleSkillHTTPExecution(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract skill name from path: /skills/weather -> weather
	path := r.URL.Path
	if !strings.HasPrefix(path, "/skills/") {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}
	skillName := strings.TrimPrefix(path, "/skills/")
	if skillName == "" {
		http.Error(w, "Skill name required", http.StatusBadRequest)
		return
	}

	// Parse request body
	var params map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&params); err != nil {
		http.Error(w, fmt.Sprintf("Invalid request body: %v", err), http.StatusBadRequest)
		return
	}

	// Execute skill through Bridge (routes based on Sandbox mode)
	result, err := b.ExecuteSkill(r.Context(), skillName, params)
	if err != nil {
		http.Error(w, fmt.Sprintf("Skill execution failed: %v", err), http.StatusInternalServerError)
		return
	}

	// Return result to OpenClaw Agent
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// handleMCPRequest handles MCP protocol requests for skill discovery.
func (b *OpenClawBridge) handleMCPRequest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req MCPRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid request: %v", err), http.StatusBadRequest)
		return
	}

	// Handle MCP methods
	switch req.Method {
	case "tools/list":
		b.handleMCPToolsList(w, r, req)
	case "tools/call":
		b.handleMCPToolsCall(w, r, req)
	default:
		http.Error(w, fmt.Sprintf("Unknown method: %s", req.Method), http.StatusBadRequest)
	}
}

// handleMCPToolsList handles MCP tools/list request.
func (b *OpenClawBridge) handleMCPToolsList(w http.ResponseWriter, r *http.Request, req MCPRequest) {
	harnessManager := b.GetHarnessManager()
	if harnessManager == nil {
		// No harness, return empty list
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(MCPResponse{
			Jsonrpc: "2.0",
			ID:      req.ID,
			Result:  map[string]any{"tools": []any{}},
		})
		return
	}

	skillsHarness := harnessManager.GetSkillsHarness()
	if skillsHarness == nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(MCPResponse{
			Jsonrpc: "2.0",
			ID:      req.ID,
			Result:  map[string]any{"tools": []any{}},
		})
		return
	}

	// Get skill descriptions
	skills := skillsHarness.ListSkills()
	tools := []MCPToolInfo{}
	for _, skill := range skills {
		executor := skill.GetExecutor()
		desc := executor.Describe()
		tools = append(tools, MCPToolInfo{
			Name:        desc.Name,
			Description: desc.Description,
			InputSchema: desc.InputSchema,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(MCPResponse{
		Jsonrpc: "2.0",
		ID:      req.ID,
		Result:  map[string]any{"tools": tools},
	})
}

// handleMCPToolsCall handles MCP tools/call request.
func (b *OpenClawBridge) handleMCPToolsCall(w http.ResponseWriter, r *http.Request, req MCPRequest) {
	params, ok := req.Params.(map[string]any)
	if !ok {
		http.Error(w, "Invalid params", http.StatusBadRequest)
		return
	}

	toolName, ok := params["name"].(string)
	if !ok {
		http.Error(w, "Tool name required", http.StatusBadRequest)
		return
	}

	toolArgs, _ := params["arguments"].(map[string]any)

	result, err := b.ExecuteSkill(r.Context(), toolName, toolArgs)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(MCPResponse{
			Jsonrpc: "2.0",
			ID:      req.ID,
			Error:   &MCPError{Code: -1, Message: err.Error()},
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(MCPResponse{
		Jsonrpc: "2.0",
		ID:      req.ID,
		Result:  map[string]any{"content": result.Output},
	})
}

// handleHealthCheck handles health check requests.
func (b *OpenClawBridge) handleHealthCheck(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"status":    "healthy",
		"timestamp": time.Now().Unix(),
	})
}

// MCPRequest represents MCP protocol request.
type MCPRequest struct {
	Jsonrpc string         `json:"jsonrpc"`
	Method  string         `json:"method"`
	Params  any            `json:"params,omitempty"`
	ID      int            `json:"id"`
}

// MCPResponse represents MCP protocol response.
type MCPResponse struct {
	Jsonrpc string         `json:"jsonrpc"`
	Result  any            `json:"result,omitempty"`
	Error   *MCPError      `json:"error,omitempty"`
	ID      int            `json:"id"`
}

// MCPError represents MCP error.
type MCPError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// MCPToolInfo represents MCP tool information.
type MCPToolInfo struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

// SendMessage sends a message to the OpenClaw agent.
func (b *OpenClawBridge) SendMessage(ctx context.Context, msg *AgentMessage) (*AgentResponse, error) {
	params := map[string]interface{}{
		"agent_id":  msg.AgentID,
		"session_id": msg.SessionID,
		"content":    msg.Content,
	}

	result, err := b.callJSONRPC(ctx, "agent.sendMessage", params)
	if err != nil {
		return nil, err
	}

	var response AgentResponse
	if err := json.Unmarshal(result, &response); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return &response, nil
}

// CreateAgent creates a new OpenClaw agent.
func (b *OpenClawBridge) CreateAgent(ctx context.Context, config *OpenClawAgentConfig) (*AgentInfo, error) {
	params := map[string]interface{}{
		"id":          config.ID,
		"name":        config.Name,
		"description": config.Description,
		"model":       config.Model,
		"skills":      config.Skills,
		"tools":       config.Tools,
		"workspace":   config.Workspace,
	}

	result, err := b.callJSONRPC(ctx, "agent.create", params)
	if err != nil {
		return nil, err
	}

	var info AgentInfo
	if err := json.Unmarshal(result, &info); err != nil {
		return nil, fmt.Errorf("failed to unmarshal agent info: %w", err)
	}

	return &info, nil
}

// StartAgent starts an OpenClaw agent.
func (b *OpenClawBridge) StartAgent(ctx context.Context, agentID string) error {
	params := map[string]interface{}{
		"agent_id": agentID,
	}

	_, err := b.callJSONRPC(ctx, "agent.start", params)
	return err
}

// StopAgent stops an OpenClaw agent.
func (b *OpenClawBridge) StopAgent(ctx context.Context, agentID string) error {
	params := map[string]interface{}{
		"agent_id": agentID,
	}

	_, err := b.callJSONRPC(ctx, "agent.stop", params)
	return err
}

// GetAgentStatus gets the status of an OpenClaw agent.
func (b *OpenClawBridge) GetAgentStatus(ctx context.Context, agentID string) (*AgentStatus, error) {
	params := map[string]interface{}{
		"agent_id": agentID,
	}

	result, err := b.callJSONRPC(ctx, "agent.status", params)
	if err != nil {
		return nil, err
	}

	var status AgentStatus
	if err := json.Unmarshal(result, &status); err != nil {
		return nil, fmt.Errorf("failed to unmarshal status: %w", err)
	}

	return &status, nil
}

// CreateSession creates a new session for an agent.
func (b *OpenClawBridge) CreateSession(ctx context.Context, agentID, userID string) (*SessionInfo, error) {
	params := map[string]interface{}{
		"agent_id": agentID,
		"user_id":  userID,
	}

	result, err := b.callJSONRPC(ctx, "session.create", params)
	if err != nil {
		return nil, err
	}

	var info SessionInfo
	if err := json.Unmarshal(result, &info); err != nil {
		return nil, fmt.Errorf("failed to unmarshal session info: %w", err)
	}

	return &info, nil
}

// LoadAgentConfig loads agent configuration from a file.
func (b *OpenClawBridge) LoadAgentConfig(ctx context.Context, configPath string) (*OpenClawAgentConfig, error) {
	params := map[string]interface{}{
		"config_path": configPath,
	}

	result, err := b.callJSONRPC(ctx, "agent.loadConfig", params)
	if err != nil {
		return nil, err
	}

	var config OpenClawAgentConfig
	if err := json.Unmarshal(result, &config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	return &config, nil
}

// callJSONRPC makes a JSON-RPC call and waits for response.
func (b *OpenClawBridge) callJSONRPC(ctx context.Context, method string, params interface{}) (json.RawMessage, error) {
	b.mu.Lock()
	if !b.running {
		b.mu.Unlock()
		return nil, fmt.Errorf("bridge not running")
	}

	// Generate request ID
	b.requestID++
	id := b.requestID

	// Create pending request
	pending := &pendingRequest{
		response: make(chan *jsonRPCResponse, 1),
		done:     make(chan struct{}),
	}
	b.pending[id] = pending
	b.mu.Unlock()

	// Build request
	req := jsonRPCRequest{
		Jsonrpc: "2.0",
		Method:  method,
		Params:  params,
		ID:      id,
	}

	// Send request
	reqData, err := json.Marshal(req)
	if err != nil {
		b.cleanupPending(id)
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	reqData = append(reqData, '\n')

	b.mu.Lock()
	_, err = b.stdin.Write(reqData)
	b.mu.Unlock()

	if err != nil {
		b.cleanupPending(id)
		return nil, fmt.Errorf("failed to write request: %w", err)
	}

	// Wait for response
	select {
	case resp := <-pending.response:
		b.cleanupPending(id)
		if resp.Error != nil {
			return nil, fmt.Errorf("JSON-RPC error: %s (code %d)", resp.Error.Message, resp.Error.Code)
		}
		return resp.Result, nil
	case <-time.After(b.config.Timeout):
		b.cleanupPending(id)
		return nil, fmt.Errorf("request timed out")
	case <-ctx.Done():
		b.cleanupPending(id)
		return nil, ctx.Err()
	}
}

// readResponses reads JSON-RPC responses from stdout.
func (b *OpenClawBridge) readResponses() {
	decoder := json.NewDecoder(b.stdout)
	for {
		var resp jsonRPCResponse
		if err := decoder.Decode(&resp); err != nil {
			if err == io.EOF {
				return
			}
			// Log error but continue reading
			continue
		}

		b.mu.Lock()
		pending, exists := b.pending[resp.ID]
		b.mu.Unlock()

		if exists {
			pending.response <- &resp
		}
	}
}

// cleanupPending removes a pending request.
func (b *OpenClawBridge) cleanupPending(id int) {
	b.mu.Lock()
	delete(b.pending, id)
	b.mu.Unlock()
}

// AgentMessage represents a message to send to an agent.
type AgentMessage struct {
	AgentID   string         `json:"agent_id"`
	SessionID string         `json:"session_id"`
	Content   *agent.Content `json:"content"`
}

// AgentResponse represents a response from an agent.
type AgentResponse struct {
	Content   *agent.Content `json:"content"`
	SessionID string         `json:"session_id"`
	Branch    string         `json:"branch,omitempty"`
}

// AgentInfo contains information about an OpenClaw agent.
type AgentInfo struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	Model       string            `json:"model,omitempty"`
	Status      string            `json:"status"`
	Channels    []string          `json:"channels,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

// AgentStatus contains the status of an OpenClaw agent.
type AgentStatus struct {
	ID            string `json:"id"`
	Status        string `json:"status"`
	SessionCount  int    `json:"session_count"`
	LastActivity  int64  `json:"last_activity"`
	Invocations   int64  `json:"invocations"`
}

// SessionInfo contains session information.
type SessionInfo struct {
	ID        string `json:"id"`
	AgentID   string `json:"agent_id"`
	UserID    string `json:"user_id"`
	CreatedAt int64  `json:"created_at"`
}

// OpenClawAgentConfig represents OpenClaw agent configuration.
type OpenClawAgentConfig struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	Model       string            `json:"model"`
	Workspace   string            `json:"workspace,omitempty"`
	Skills      []string          `json:"skills,omitempty"`
	Tools       []string          `json:"tools,omitempty"`
	Channels    []ChannelConfig   `json:"channels,omitempty"`
	Config      map[string]any    `json:"config,omitempty"`

	// Skill endpoints configuration (HTTP integration mode)
	// When Sandbox mode is external, skills are accessed via HTTP endpoints
	// OpenClaw framework reads this config and knows to call skills via HTTP
	SkillEndpoints []SkillEndpointConfig `json:"skillEndpoints,omitempty"`

	// MCP configuration for skill discovery (HTTP integration mode)
	MCPConfig *AgentMCPConfig `json:"mcpConfig,omitempty"`
}

// SkillEndpointConfig documents skill configuration for agent config.
// NOTE: OpenClaw does NOT natively support HTTP skill endpoints.
// This configuration is for documentation/planning purposes.
// Actual integration:
//   - Embedded Sandbox: Skills as local SKILL.md files
//   - External Sandbox: MCP protocol via mcporter -> Bridge MCP endpoint
type SkillEndpointConfig struct {
	Name     string `json:"name"`                // Skill name
	Type     string `json:"type"`                // "builtin" (local) or "http" (documentation only)
	Endpoint string `json:"endpoint,omitempty"`  // HTTP endpoint (documentation only)
}

// AgentMCPConfig configures MCP protocol endpoint for skill discovery in agent config.
	// OpenClaw uses mcporter runtime to connect to MCP servers.
	// For External Sandbox, Bridge exposes MCP endpoint routing to SkillsHarness.
	// Flow: OpenClaw -> mcporter -> Bridge MCP -> SkillsHarness -> RemoteExecutor -> Sandbox
type AgentMCPConfig struct {
	ServerURL string          `json:"serverUrl"`           // MCP server URL (Harness Bridge)
	Tools     []MCPToolConfig `json:"tools,omitempty"`     // MCP tools exposed by Bridge
}

// MCPToolConfig defines a skill as an MCP tool.
// When OpenClaw calls an MCP tool via mcporter:
// 1. mcporter sends request to Bridge MCP endpoint
// 2. Bridge routes to SkillsHarness.ExecuteSkill
// 3. SkillsHarness uses RemoteExecutor for External Sandbox
// 4. RemoteExecutor sends to External Sandbox Service
// 5. Result returns through same chain
type MCPToolConfig struct {
	Name        string         `json:"name"`                  // Skill name (also MCP tool name)
	Description string         `json:"description,omitempty"` // Tool description
	InputSchema map[string]any `json:"inputSchema,omitempty"` // JSON schema for input
}

// ChannelConfig represents a channel configuration.
type ChannelConfig struct {
	Type    string            `json:"type"`
	Name    string            `json:"name"`
	Enabled bool              `json:"enabled"`
	Config  map[string]string `json:"config,omitempty"`
}