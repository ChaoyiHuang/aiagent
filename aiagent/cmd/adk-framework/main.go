// ADK Framework launcher.
// This program reads JSON-RPC requests from stdin and writes responses to stdout.
// No HTTP/gRPC server - pure stdin/stdout communication.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"sync"
	"time"

	"aiagent/pkg/agent"
)

var (
	agentID    = flag.String("agent-id", "", "Agent ID")
	configPath = flag.String("config", "", "Config file path")
	workDir    = flag.String("workdir", "", "Working directory")
	debug      = flag.Bool("debug", false, "Enable debug logging")
)

func main() {
	flag.Parse()

	log.Printf("ADK Framework starting...")
	log.Printf("Agent ID: %s", *agentID)
	log.Printf("Config: %s", *configPath)
	log.Printf("Work Dir: %s", *workDir)

	// Load configuration
	config, err := loadConfig(*configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Create agent
	ag, err := createAgent(config)
	if err != nil {
		log.Fatalf("Failed to create agent: %v", err)
	}

	// Start JSON-RPC server (stdin/stdout)
	server := NewStdioJSONRPCServer(os.Stdin, os.Stdout)

	// Register handlers
	server.RegisterMethod("agent.run", handleAgentRun(ag))
	server.RegisterMethod("agent.status", handleAgentStatus(ag))
	server.RegisterMethod("agent.stop", handleAgentStop(ag))

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
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
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

// Handler implementations

func handleAgentRun(ag agent.Agent) MethodHandler {
	return func(ctx context.Context, params json.RawMessage) (json.RawMessage, error) {
		var req struct {
			InvocationID string          `json:"invocation_id"`
			SessionID    string          `json:"session_id"`
			Content      *agent.Content  `json:"content"`
		}

		if err := json.Unmarshal(params, &req); err != nil {
			return nil, fmt.Errorf("invalid params: %w", err)
		}

		// Create invocation context
		session := agent.NewSession(req.SessionID, *agentID, "user", agent.NewMapState())
		invCtx := agent.NewInvocationContext(
			ctx,
			ag,
			nil, nil, session,
			req.InvocationID, "", req.Content,
			&agent.RunConfig{},
		)

		// Run agent and collect events
		events := make([]map[string]any, 0)
		for event, err := range ag.Run(invCtx) {
			if err != nil {
				events = append(events, map[string]any{
					"type":    "error",
					"message": err.Error(),
				})
				break
			}

			// Convert event
			eventData := convertEventToMap(event)
			events = append(events, eventData)
		}

		// Add completion event
		events = append(events, map[string]any{"type": "complete"})

		return json.Marshal(map[string]any{"events": events})
	}
}

func handleAgentStatus(ag agent.Agent) MethodHandler {
	return func(ctx context.Context, params json.RawMessage) (json.RawMessage, error) {
		status := map[string]any{
			"agent_id":    *agentID,
			"name":        ag.Name(),
			"description": ag.Description(),
			"type":        string(ag.Type()),
			"running":     true,
			"timestamp":   time.Now().Unix(),
		}

		return json.Marshal(status)
	}
}

func handleAgentStop(ag agent.Agent) MethodHandler {
	return func(ctx context.Context, params json.RawMessage) (json.RawMessage, error) {
		// Cleanup agent resources
		result := map[string]any{
			"agent_id":  *agentID,
			"stopped":   true,
			"timestamp": time.Now().Unix(),
		}

		return json.Marshal(result)
	}
}

func convertEventToMap(event *agent.Event) map[string]any {
	if event == nil {
		return nil
	}

	data := map[string]any{
		"author":    event.Author,
		"timestamp": event.Timestamp.Unix(),
	}

	// Extract content
	if event.LLMResponse.Content != nil && len(event.LLMResponse.Content.Parts) > 0 {
		data["content"] = event.LLMResponse.Content.Parts[0].Text
	}

	return data
}

// Config and Agent creation

type AgentConfig struct {
	Name        string            `yaml:"name"`
	Description string            `yaml:"description"`
	Model       string            `yaml:"model"`
	Instruction string            `yaml:"instruction"`
	Tools       []string          `yaml:"tools"`
	Skills      []string          `yaml:"skills"`
	Custom      map[string]any    `yaml:"custom"`
}

func loadConfig(path string) (*AgentConfig, error) {
	if path == "" {
		return &AgentConfig{
			Name:        *agentID,
			Description: *agentID,
			Model:       "deepseek-chat",
		}, nil
	}

	// For now, return default config
	// In production, would parse YAML file
	return &AgentConfig{
		Name:        *agentID,
		Description: *agentID,
		Model:       "deepseek-chat",
	}, nil
}

func createAgent(config *AgentConfig) (agent.Agent, error) {
	return agent.NewBaseAgent(agent.Config{
		Name:        config.Name,
		Description: config.Description,
	}, agent.AgentTypeLLM), nil
}