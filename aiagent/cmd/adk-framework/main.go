// ADK Framework launcher.
// This program reads JSON-RPC requests from stdin and writes responses to stdout.
// No HTTP/gRPC server - pure stdin/stdout communication.
// Integrates with adk-go library for real agent execution.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"iter"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"google.golang.org/genai"

	adkagent "google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	adkmodel "google.golang.org/adk/model"
	"google.golang.org/adk/runner"
	"google.golang.org/adk/session"
)

var (
	agentID    = flag.String("agent-id", "", "Agent ID")
	configPath = flag.String("config", "", "Config file path")
	workDir    = flag.String("workdir", "", "Working directory")
	debug      = flag.Bool("debug", false, "Enable debug logging")
	processMode = flag.String("process-mode", "isolated", "Process mode: shared or isolated")
)

// Framework holds all agents and the runner
type Framework struct {
	loader    adkagent.Loader
	runner    *runner.Runner
	agents    map[string]adkagent.Agent
	model     adkmodel.LLM
	appName   string
	sessionSvc session.Service

	// For tracking running invocations
	mu           sync.RWMutex
	invocations  map[string]*InvocationState
}

type InvocationState struct {
	ID        string
	AgentName string
	SessionID string
	StartTime time.Time
	Cancel    context.CancelFunc
}

func main() {
	flag.Parse()

	log.Printf("ADK Framework starting with adk-go integration...")
	log.Printf("Agent ID: %s", *agentID)
	log.Printf("Config: %s", *configPath)
	log.Printf("Work Dir: %s", *workDir)
	log.Printf("Process Mode: %s", *processMode)

	ctx := context.Background()

	// Load configuration
	config, err := loadConfig(*configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Create framework with real adk-go agents
	framework, err := createFramework(ctx, config)
	if err != nil {
		log.Fatalf("Failed to create framework: %v", err)
	}

	log.Printf("Framework created with %d agents", len(framework.agents))
	for name := range framework.agents {
		log.Printf("  - Agent: %s", name)
	}

	// Start JSON-RPC server (stdin/stdout)
	server := NewStdioJSONRPCServer(os.Stdin, os.Stdout)

	// Register handlers
	server.RegisterMethod("agent.run", framework.handleAgentRun)
	server.RegisterMethod("agent.status", framework.handleAgentStatus)
	server.RegisterMethod("agent.stop", framework.handleAgentStop)
	server.RegisterMethod("agent.list", framework.handleAgentList)
	server.RegisterMethod("framework.status", framework.handleFrameworkStatus)

	log.Printf("ADK Framework ready, listening on stdin")

	// Run until stdin closes
	server.Run()
}

// StdioJSONRPCServer handles JSON-RPC over stdin/stdout.
type StdioJSONRPCServer struct {
	stdin  io.Reader
	stdout io.Writer

	methods map[string]MethodHandler
	mu      sync.RWMutex
}

type MethodHandler func(ctx context.Context, params json.RawMessage) (json.RawMessage, error)

type jsonRPCRequest struct {
	Jsonrpc string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
	ID      int             `json:"id,omitempty"`
}

type jsonRPCResponse struct {
	Jsonrpc string          `json:"jsonrpc"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
	ID      int             `json:"id"`
}

type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func NewStdioJSONRPCServer(stdin io.Reader, stdout io.Writer) *StdioJSONRPCServer {
	return &StdioJSONRPCServer{
		stdin:  stdin,
		stdout: stdout,
		methods: make(map[string]MethodHandler),
	}
}

func (s *StdioJSONRPCServer) RegisterMethod(method string, handler MethodHandler) {
	s.mu.Lock()
	s.methods[method] = handler
	s.mu.Unlock()
}

func (s *StdioJSONRPCServer) Run() {
	scanner := bufio.NewScanner(s.stdin)
	encoder := json.NewEncoder(s.stdout)

	for scanner.Scan() {
		line := scanner.Bytes()

		var req jsonRPCRequest
		if err := json.Unmarshal(line, &req); err != nil {
			// Invalid request, skip
			continue
		}

		// Handle request
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		result, err := s.handleRequest(ctx, req)
		cancel()

		// Build response
		resp := jsonRPCResponse{
			Jsonrpc: "2.0",
			ID:      req.ID,
		}

		if err != nil {
			resp.Error = &jsonRPCError{
				Code:    -1,
				Message: err.Error(),
			}
		} else {
			resp.Result = result
		}

		// Write response
		encoder.Encode(resp)
	}

	log.Printf("stdin closed, shutting down")
}

func (s *StdioJSONRPCServer) handleRequest(ctx context.Context, req jsonRPCRequest) (json.RawMessage, error) {
	s.mu.RLock()
	handler, exists := s.methods[req.Method]
	s.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("method not found: %s", req.Method)
	}

	return handler(ctx, req.Params)
}

// Handler implementations using real adk-go

func (f *Framework) handleAgentRun(ctx context.Context, params json.RawMessage) (json.RawMessage, error) {
	var req struct {
		InvocationID string `json:"invocation_id"`
		SessionID    string `json:"session_id"`
		UserID       string `json:"user_id"`
		AgentName    string `json:"agent_name"`
		Message      string `json:"message"`
	}

	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	// Determine which agent to run
	agentName := req.AgentName
	if agentName == "" {
		agentName = *agentID
	}

	// Get the agent (for verification only)
	_, err := f.loader.LoadAgent(agentName)
	if err != nil {
		return nil, fmt.Errorf("agent not found: %s", err)
	}

	// Create user message content
	msg := genai.NewContentFromText(req.Message, genai.RoleUser)

	// Run the agent using runner
	runConfig := adkagent.RunConfig{
		StreamingMode: adkagent.StreamingModeNone,
	}

	events := make([]map[string]any, 0)

	// Use runner to execute
	for event, runErr := range f.runner.Run(ctx, req.UserID, req.SessionID, msg, runConfig) {
		if runErr != nil {
			events = append(events, map[string]any{
				"type":    "error",
				"message": runErr.Error(),
			})
			break
		}

		// Convert event to response format
		eventData := convertEventToMap(event)
		events = append(events, eventData)
	}

	// Add completion event
	events = append(events, map[string]any{"type": "complete"})

	return json.Marshal(map[string]any{"events": events})
}

func (f *Framework) handleAgentStatus(ctx context.Context, params json.RawMessage) (json.RawMessage, error) {
	var req struct {
		AgentName string `json:"agent_name"`
	}

	if err := json.Unmarshal(params, &req); err != nil {
		req.AgentName = *agentID
	}

	agentName := req.AgentName
	if agentName == "" {
		agentName = *agentID
	}

	// Get agent info from loader
	agent, err := f.loader.LoadAgent(agentName)
	if err != nil {
		return nil, fmt.Errorf("agent not found: %s", err)
	}

	status := map[string]any{
		"agent_id":    agentName,
		"name":        agent.Name(),
		"description": agent.Description(),
		"type":        "llm",
		"running":     true,
		"timestamp":   time.Now().Unix(),
		"sub_agents":  getSubAgentNames(agent),
	}

	return json.Marshal(status)
}

func (f *Framework) handleAgentStop(ctx context.Context, params json.RawMessage) (json.RawMessage, error) {
	var req struct {
		AgentName string `json:"agent_name"`
	}

	if err := json.Unmarshal(params, &req); err != nil {
		req.AgentName = *agentID
	}

	agentName := req.AgentName
	if agentName == "" {
		agentName = *agentID
	}

	result := map[string]any{
		"agent_id":  agentName,
		"stopped":   true,
		"timestamp": time.Now().Unix(),
	}

	return json.Marshal(result)
}

func (f *Framework) handleAgentList(ctx context.Context, params json.RawMessage) (json.RawMessage, error) {
	agentNames := f.loader.ListAgents()

	agentsInfo := make([]map[string]any, 0)
	for _, name := range agentNames {
		agent, err := f.loader.LoadAgent(name)
		if err != nil {
			continue
		}
		agentsInfo = append(agentsInfo, map[string]any{
			"name":        agent.Name(),
			"description": agent.Description(),
			"sub_agents":  getSubAgentNames(agent),
		})
	}

	return json.Marshal(map[string]any{
		"agents": agentsInfo,
		"count":  len(agentsInfo),
	})
}

func (f *Framework) handleFrameworkStatus(ctx context.Context, params json.RawMessage) (json.RawMessage, error) {
	f.mu.RLock()
	invCount := len(f.invocations)
	f.mu.RUnlock()

	status := map[string]any{
		"framework_id":     *agentID,
		"process_mode":     *processMode,
		"agent_count":      len(f.agents),
		"running_invocations": invCount,
		"model_name":       f.model.Name(),
		"timestamp":        time.Now().Unix(),
	}

	return json.Marshal(status)
}

func convertEventToMap(event *session.Event) map[string]any {
	if event == nil {
		return nil
	}

	data := map[string]any{
		"author":    event.Author,
		"timestamp": event.Timestamp.Unix(),
		"invocation_id": event.InvocationID,
	}

	// Extract content from LLMResponse
	if event.Content != nil && len(event.Content.Parts) > 0 {
		textParts := make([]string, 0)
		for _, part := range event.Content.Parts {
			if part.Text != "" {
				textParts = append(textParts, part.Text)
			}
		}
		if len(textParts) > 0 {
			data["content"] = strings.Join(textParts, "\n")
		}
	}

	// Add partial flag
	data["partial"] = event.LLMResponse.Partial
	data["is_final"] = event.IsFinalResponse()

	// Add state delta if present
	if event.Actions.StateDelta != nil && len(event.Actions.StateDelta) > 0 {
		data["state_delta"] = event.Actions.StateDelta
	}

	// Add transfer info
	if event.Actions.TransferToAgent != "" {
		data["transfer_to"] = event.Actions.TransferToAgent
	}

	return data
}

func getSubAgentNames(agent adkagent.Agent) []string {
	names := make([]string, 0)
	for _, sub := range agent.SubAgents() {
		names = append(names, sub.Name())
	}
	return names
}

// Config and Framework creation

type AgentConfig struct {
	Name        string         `yaml:"name"`
	Description string         `yaml:"description"`
	Model       string         `yaml:"model"`
	Instruction string         `yaml:"instruction"`
	Tools       []string       `yaml:"tools"`
	APIKey      string         `yaml:"api_key"`
	Endpoint    string         `yaml:"endpoint"`
	SubAgents   []AgentConfig  `yaml:"sub_agents"`
}

type FrameworkConfig struct {
	AppName     string        `yaml:"app_name"`
	ProcessMode string        `yaml:"process_mode"`
	Agents      []AgentConfig `yaml:"agents"`
}

func loadConfig(path string) (*FrameworkConfig, error) {
	if path == "" {
		// Default config for single agent
		return &FrameworkConfig{
			AppName:     *agentID,
			ProcessMode: *processMode,
			Agents: []AgentConfig{
				{
					Name:        *agentID,
					Description: "AI Agent",
					Model:       "deepseek-chat",
					Instruction: "You are a helpful AI assistant.",
				},
			},
		}, nil
	}

	// TODO: Parse YAML config file
	// For now, return default config
	return &FrameworkConfig{
		AppName:     *agentID,
		ProcessMode: *processMode,
		Agents: []AgentConfig{
			{
				Name:        *agentID,
				Description: "AI Agent",
				Model:       "deepseek-chat",
				Instruction: "You are a helpful AI assistant.",
			},
		},
	}, nil
}

// Custom model that implements adkmodel.LLM interface
// This model can use OpenAI-compatible APIs (like DeepSeek)
type customModel struct {
	name     string
	apiKey   string
	endpoint string
}

func newCustomModel(name, apiKey, endpoint string) *customModel {
	if endpoint == "" {
		endpoint = "https://api.deepseek.com"
	}
	return &customModel{
		name:     name,
		apiKey:   apiKey,
		endpoint: endpoint,
	}
}

func (m *customModel) Name() string {
	return m.name
}

func (m *customModel) GenerateContent(ctx context.Context, req *adkmodel.LLMRequest, stream bool) iter.Seq2[*adkmodel.LLMResponse, error] {
	return func(yield func(*adkmodel.LLMResponse, error) bool) {
		// For now, return a mock response
		// In production, this would call the actual API
		resp := &adkmodel.LLMResponse{
			Content: genai.NewContentFromText(
				fmt.Sprintf("Response from %s model. Model is ready but API integration pending.", m.name),
				genai.RoleModel,
			),
			Partial: false,
		}
		yield(resp, nil)
	}
}

func createFramework(ctx context.Context, config *FrameworkConfig) (*Framework, error) {
	// Get API key from environment
	apiKey := os.Getenv("DEEPSEEK_API_KEY")
	if apiKey == "" {
		apiKey = os.Getenv("OPENAI_API_KEY")
	}

	// Create the model
	model := newCustomModel(
		config.Agents[0].Model,
		apiKey,
		config.Agents[0].Endpoint,
	)

	// Create agents based on config
	agents := make(map[string]adkagent.Agent)
	var rootAgent adkagent.Agent

	for i, agentCfg := range config.Agents {
		agent, err := createRealAgent(ctx, model, agentCfg)
		if err != nil {
			return nil, fmt.Errorf("failed to create agent %s: %w", agentCfg.Name, err)
		}
		agents[agentCfg.Name] = agent

		// First agent is root
		if i == 0 {
			rootAgent = agent
		}
	}

	// Create loader based on process mode
	var loader adkagent.Loader
	var err error
	if len(agents) == 1 {
		loader = adkagent.NewSingleLoader(rootAgent)
	} else {
		agentList := make([]adkagent.Agent, 0)
		for _, a := range agents {
			if a != rootAgent {
				agentList = append(agentList, a)
			}
		}
		loader, err = adkagent.NewMultiLoader(rootAgent, agentList...)
		if err != nil {
			return nil, fmt.Errorf("failed to create multi loader: %w", err)
		}
	}

	// Create session service (in-memory for now)
	sessionSvc := session.InMemoryService()

	// Create runner
	r, err := runner.New(runner.Config{
		AppName:         config.AppName,
		Agent:           rootAgent,
		SessionService:  sessionSvc,
		AutoCreateSession: true,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create runner: %w", err)
	}

	return &Framework{
		loader:      loader,
		runner:      r,
		agents:      agents,
		model:       model,
		appName:     config.AppName,
		sessionSvc:  sessionSvc,
		invocations: make(map[string]*InvocationState),
	}, nil
}

func createRealAgent(ctx context.Context, model adkmodel.LLM, config AgentConfig) (adkagent.Agent, error) {
	instruction := config.Instruction
	if instruction == "" {
		instruction = "You are a helpful AI assistant. Respond to user queries appropriately."
	}

	// Create LLM agent using adk-go
	agent, err := llmagent.New(llmagent.Config{
		Name:        config.Name,
		Model:       model,
		Description: config.Description,
		Instruction: instruction,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create llmagent: %w", err)
	}

	return agent, nil
}