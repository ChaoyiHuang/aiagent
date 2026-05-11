// Package harness provides MCP harness for Model Context Protocol integration.
package harness

import (
	"context"
	"fmt"
	"sync"

	"aiagent/api/v1"
)

// MCPHarness manages MCP registry and server connections.
type MCPHarness struct {
	spec    *v1.MCPHarnessSpec
	servers map[string]*MCPServer
	registry MCPRegistry
	mu      sync.RWMutex
}

// MCPServer represents an MCP server connection.
type MCPServer struct {
	config *v1.MCPServerConfig
	client MCPClient
	status ServerStatus
}

// ServerStatus represents MCP server status.
type ServerStatus struct {
	Name       string
	Type       string
	Connected  bool
	ToolsCount int
	LastError  string
}

// MCPClient interface for MCP server communication.
type MCPClient interface {
	// ListTools lists available tools
	ListTools(ctx context.Context) ([]ToolInfo, error)
	// CallTool calls a tool
	CallTool(ctx context.Context, name string, args map[string]any) (*ToolResult, error)
	// ListResources lists available resources
	ListResources(ctx context.Context) ([]ResourceInfo, error)
	// ReadResource reads a resource
	ReadResource(ctx context.Context, uri string) (*ResourceContent, error)
	// Close closes the connection
	Close() error
}

// ToolInfo represents tool information from MCP.
type ToolInfo struct {
	Name        string
	Description string
	InputSchema map[string]any
}

// ToolResult represents tool execution result.
type ToolResult struct {
	Content []ContentBlock
	Error   string
}

// ContentBlock represents content in tool result.
type ContentBlock struct {
	Type string // "text", "image", "resource"
	Text string
	Data []byte
	MimeType string
}

// ResourceInfo represents resource information.
type ResourceInfo struct {
	URI         string
	Name        string
	Description string
	MimeType    string
}

// ResourceContent represents resource content.
type ResourceContent struct {
	URI      string
	MimeType string
	Text     string
	Data     []byte
}

// MCPRegistry interface for MCP registry.
type MCPRegistry interface {
	Discover(ctx context.Context) ([]ToolInfo, error)
	Lookup(name string) (*MCPServer, bool)
	Register(server *MCPServer)
}

// NewMCPHarness creates a new MCP harness.
func NewMCPHarness(spec *v1.MCPHarnessSpec) *MCPHarness {
	harness := &MCPHarness{
		spec:    spec,
		servers: make(map[string]*MCPServer),
		registry: NewInMemoryRegistry(),
	}

	// Initialize servers from spec
	for _, serverConfig := range spec.Servers {
		if serverConfig.Allowed {
			server := &MCPServer{
				config: &serverConfig,
				client: NewMockMCPClient(serverConfig.Name),
				status: ServerStatus{
					Name:      serverConfig.Name,
					Type:      serverConfig.Type,
					Connected: true,
				},
			}
			harness.servers[serverConfig.Name] = server
			harness.registry.Register(server)
		}
	}

	return harness
}

// GetServer returns an MCP server by name.
func (h *MCPHarness) GetServer(name string) (*MCPServer, error) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	server, exists := h.servers[name]
	if !exists {
		return nil, fmt.Errorf("MCP server '%s' not found", name)
	}
	return server, nil
}

// GetSpec returns the MCP harness spec.
func (h *MCPHarness) GetSpec() *v1.MCPHarnessSpec {
	return h.spec
}

// GetRegistryType returns the registry type.
func (h *MCPHarness) GetRegistryType() string {
	return h.spec.RegistryType
}

// GetEndpoint returns the registry endpoint.
func (h *MCPHarness) GetEndpoint() string {
	return h.spec.Endpoint
}

// ListServers returns all MCP servers.
func (h *MCPHarness) ListServers() []*MCPServer {
	h.mu.RLock()
	defer h.mu.RUnlock()

	servers := make([]*MCPServer, 0, len(h.servers))
	for _, server := range h.servers {
		servers = append(servers, server)
	}
	return servers
}

// ListAllTools lists all tools from all servers.
func (h *MCPHarness) ListAllTools(ctx context.Context) ([]ToolInfo, error) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	allTools := []ToolInfo{}
	for _, server := range h.servers {
		tools, err := server.client.ListTools(ctx)
		if err != nil {
			// Continue with other servers
			continue
		}
		allTools = append(allTools, tools...)
		server.status.ToolsCount = len(tools)
	}
	return allTools, nil
}

// CallTool calls a tool on the appropriate server.
func (h *MCPHarness) CallTool(ctx context.Context, serverName, toolName string, args map[string]any) (*ToolResult, error) {
	server, err := h.GetServer(serverName)
	if err != nil {
		return nil, err
	}

	return server.client.CallTool(ctx, toolName, args)
}

// Discover discovers tools from the registry.
func (h *MCPHarness) Discover(ctx context.Context) ([]ToolInfo, error) {
	return h.registry.Discover(ctx)
}

// IsServerAllowed checks if a server is allowed.
func (h *MCPHarness) IsServerAllowed(name string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()

	server, exists := h.servers[name]
	return exists && server.config.Allowed
}

// GetServerStatus returns server status.
func (h *MCPHarness) GetServerStatus(name string) *ServerStatus {
	h.mu.RLock()
	defer h.mu.RUnlock()

	server, exists := h.servers[name]
	if !exists {
		return nil
	}
	return &server.status
}

// Shutdown shuts down all servers.
func (h *MCPHarness) Shutdown(ctx context.Context) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	var errs []error
	for name, server := range h.servers {
		if err := server.client.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close server '%s': %w", name, err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("errors during shutdown: %v", errs)
	}
	return nil
}

// InMemoryRegistry provides in-memory MCP registry.
type InMemoryRegistry struct {
	servers map[string]*MCPServer
}

// NewInMemoryRegistry creates an in-memory registry.
func NewInMemoryRegistry() *InMemoryRegistry {
	return &InMemoryRegistry{
		servers: make(map[string]*MCPServer),
	}
}

func (r *InMemoryRegistry) Register(server *MCPServer) {
	r.servers[server.config.Name] = server
}

func (r *InMemoryRegistry) Discover(ctx context.Context) ([]ToolInfo, error) {
	allTools := []ToolInfo{}
	for _, server := range r.servers {
		tools, err := server.client.ListTools(ctx)
		if err != nil {
			continue
		}
		allTools = append(allTools, tools...)
	}
	return allTools, nil
}

func (r *InMemoryRegistry) Lookup(name string) (*MCPServer, bool) {
	server, exists := r.servers[name]
	return server, exists
}

// MockMCPClient provides mock MCP client for testing.
type MockMCPClient struct {
	name string
}

// NewMockMCPClient creates a mock MCP client.
func NewMockMCPClient(name string) *MockMCPClient {
	return &MockMCPClient{name: name}
}

func (c *MockMCPClient) ListTools(ctx context.Context) ([]ToolInfo, error) {
	// Mock tools
	return []ToolInfo{
		{
			Name:        c.name + "_tool1",
			Description: "Mock tool 1 from " + c.name,
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"input": map[string]any{"type": "string"},
				},
			},
		},
		{
			Name:        c.name + "_tool2",
			Description: "Mock tool 2 from " + c.name,
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{"type": "string"},
				},
			},
		},
	}, nil
}

func (c *MockMCPClient) CallTool(ctx context.Context, name string, args map[string]any) (*ToolResult, error) {
	return &ToolResult{
		Content: []ContentBlock{
			{Type: "text", Text: "Mock result from " + name},
		},
	}, nil
}

func (c *MockMCPClient) ListResources(ctx context.Context) ([]ResourceInfo, error) {
	return []ResourceInfo{
		{
			URI:         c.name + "/resource1",
			Name:        "Resource 1",
			Description: "Mock resource 1",
			MimeType:    "text/plain",
		},
	}, nil
}

func (c *MockMCPClient) ReadResource(ctx context.Context, uri string) (*ResourceContent, error) {
	return &ResourceContent{
		URI:      uri,
		MimeType: "text/plain",
		Text:     "Mock resource content from " + uri,
	}, nil
}

func (c *MockMCPClient) Close() error {
	return nil
}