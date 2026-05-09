// Package client provides gRPC client for connecting to Framework Container.
package client

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"aiagent/pkg/agent"
	"aiagent/pkg/framework/proto"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// FrameworkClient connects to Framework Container gRPC server.
type FrameworkClient struct {
	conn   *grpc.ClientConn
	client proto.AgentServiceClient

	mu     sync.RWMutex
	agents map[string]*RemoteAgentRef
}

// RemoteAgentRef represents a remote agent in Framework Container.
type RemoteAgentRef struct {
	ID    string
	Name  string
	Type  agent.AgentType
	Phase proto.AgentPhase
}

// NewFrameworkClient creates a new Framework gRPC client.
func NewFrameworkClient() *FrameworkClient {
	return &FrameworkClient{
		agents: make(map[string]*RemoteAgentRef),
	}
}

// Connect connects to Framework Container.
func (c *FrameworkClient) Connect(ctx context.Context, endpoint string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Create connection
	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
		grpc.WithTimeout(10 * time.Second),
	}

	conn, err := grpc.Dial(endpoint, opts...)
	if err != nil {
		return fmt.Errorf("failed to connect to Framework Container: %w", err)
	}

	c.conn = conn
	c.client = proto.NewAgentServiceClient(conn)

	// Verify connection with health check
	healthResp, err := c.client.HealthCheck(ctx, &proto.HealthCheckRequest{})
	if err != nil {
		conn.Close()
		return fmt.Errorf("health check failed: %w", err)
	}

	if !healthResp.Healthy {
		conn.Close()
		return fmt.Errorf("Framework Container is not healthy")
	}

	return nil
}

// Disconnect disconnects from Framework Container.
func (c *FrameworkClient) Disconnect(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

// IsConnected returns whether the client is connected.
func (c *FrameworkClient) IsConnected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.conn != nil
}

// CreateAgent creates an agent in Framework Container.
func (c *FrameworkClient) CreateAgent(ctx context.Context, config *AgentCreateConfig) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.client == nil {
		return fmt.Errorf("not connected to Framework Container")
	}

	// Convert config to proto
	req := &proto.CreateAgentRequest{
		AgentId:     config.ID,
		Name:        config.Name,
		Description: config.Description,
		Model:       config.Model,
		Type:        convertAgentType(config.Type),
		Instruction: config.Instruction,
	}

	// Add harness config if provided
	if config.Harness != nil {
		req.Harness = convertHarnessConfig(config.Harness)
	}

	// Call gRPC
	resp, err := c.client.CreateAgent(ctx, req)
	if err != nil {
		return fmt.Errorf("CreateAgent RPC failed: %w", err)
	}

	if !resp.Success {
		return fmt.Errorf("CreateAgent failed: %s", resp.Error)
	}

	// Store reference
	c.agents[config.ID] = &RemoteAgentRef{
		ID:    config.ID,
		Name:  config.Name,
		Type:  config.Type,
		Phase: proto.AgentPhase_AGENT_PHASE_PENDING,
	}

	return nil
}

// StartAgent starts an agent in Framework Container.
func (c *FrameworkClient) StartAgent(ctx context.Context, agentID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.client == nil {
		return fmt.Errorf("not connected to Framework Container")
	}

	resp, err := c.client.StartAgent(ctx, &proto.StartAgentRequest{AgentId: agentID})
	if err != nil {
		return fmt.Errorf("StartAgent RPC failed: %w", err)
	}

	if !resp.Success {
		return fmt.Errorf("StartAgent failed: %s", resp.Error)
	}

	// Update local reference
	if ref, exists := c.agents[agentID]; exists {
		ref.Phase = proto.AgentPhase_AGENT_PHASE_RUNNING
	}

	return nil
}

// StopAgent stops an agent in Framework Container.
func (c *FrameworkClient) StopAgent(ctx context.Context, agentID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.client == nil {
		return fmt.Errorf("not connected to Framework Container")
	}

	resp, err := c.client.StopAgent(ctx, &proto.StopAgentRequest{AgentId: agentID})
	if err != nil {
		return fmt.Errorf("StopAgent RPC failed: %w", err)
	}

	if !resp.Success {
		return fmt.Errorf("StopAgent failed: %s", resp.Error)
	}

	// Update local reference
	if ref, exists := c.agents[agentID]; exists {
		ref.Phase = proto.AgentPhase_AGENT_PHASE_TERMINATED
	}

	return nil
}

// DeleteAgent deletes an agent in Framework Container.
func (c *FrameworkClient) DeleteAgent(ctx context.Context, agentID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.client == nil {
		return fmt.Errorf("not connected to Framework Container")
	}

	resp, err := c.client.DeleteAgent(ctx, &proto.DeleteAgentRequest{AgentId: agentID})
	if err != nil {
		return fmt.Errorf("DeleteAgent RPC failed: %w", err)
	}

	if !resp.Success {
		return fmt.Errorf("DeleteAgent failed: %s", resp.Error)
	}

	delete(c.agents, agentID)

	return nil
}

// GetAgentStatus gets the status of an agent.
func (c *FrameworkClient) GetAgentStatus(ctx context.Context, agentID string) (*AgentStatus, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.client == nil {
		return nil, fmt.Errorf("not connected to Framework Container")
	}

	resp, err := c.client.GetAgentStatus(ctx, &proto.GetAgentStatusRequest{AgentId: agentID})
	if err != nil {
		return nil, fmt.Errorf("GetAgentStatus RPC failed: %w", err)
	}

	return &AgentStatus{
		ID:            resp.AgentId,
		Name:          resp.Name,
		Phase:         resp.Phase,
		Running:       resp.Running,
		SessionCount:  resp.SessionCount,
		Metrics:       resp.Metrics,
	}, nil
}

// ListAgents lists all agents in Framework Container.
func (c *FrameworkClient) ListAgents(ctx context.Context) ([]*RemoteAgentRef, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.client == nil {
		return nil, fmt.Errorf("not connected to Framework Container")
	}

	resp, err := c.client.ListAgents(ctx, &proto.ListAgentsRequest{})
	if err != nil {
		return nil, fmt.Errorf("ListAgents RPC failed: %w", err)
	}

	agents := make([]*RemoteAgentRef, 0)
	for _, info := range resp.Agents {
		agents = append(agents, &RemoteAgentRef{
			ID:    info.Id,
			Name:  info.Name,
			Type:  convertProtoAgentType(info.Type),
			Phase: info.Phase,
		})
	}

	return agents, nil
}

// RunAgent runs an agent invocation and returns events.
func (c *FrameworkClient) RunAgent(ctx context.Context, agentID string, invocationID string, content *agent.Content) (chan *AgentEvent, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.client == nil {
		return nil, fmt.Errorf("not connected to Framework Container")
	}

	// Create request
	req := &proto.RunAgentRequest{
		AgentId:      agentID,
		InvocationId: invocationID,
		UserContent:  convertContentToProto(content),
	}

	// Start streaming
	stream, err := c.client.RunAgent(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("RunAgent RPC failed: %w", err)
	}

	// Create event channel
	eventChan := make(chan *AgentEvent, 100)

	// Read events in background
	go func() {
		defer close(eventChan)

		for {
			protoEvent, err := stream.Recv()
			if err == io.EOF {
				return
			}
			if err != nil {
				eventChan <- &AgentEvent{
					Type:  EventTypeError,
					Error: err.Error(),
				}
				return
			}

			eventChan <- convertProtoEvent(protoEvent)
		}
	}()

	return eventChan, nil
}

// AgentCreateConfig contains configuration for creating an agent.
type AgentCreateConfig struct {
	ID          string
	Name        string
	Description string
	Model       string
	Type        agent.AgentType
	Instruction string
	Harness     *HarnessConfig
}

// HarnessConfig contains harness configuration.
type HarnessConfig struct {
	Model    *ModelHarnessConfig
	Memory   *MemoryHarnessConfig
	Sandbox  *SandboxHarnessConfig
	Skills   *SkillsHarnessConfig
}

// ModelHarnessConfig
type ModelHarnessConfig struct {
	Provider     string
	Endpoint     string
	DefaultModel string
}

// MemoryHarnessConfig
type MemoryHarnessConfig struct {
	Type     string
	Endpoint string
	TTL      int
}

// SandboxHarnessConfig
type SandboxHarnessConfig struct {
	Type     string
	Mode     string
	Endpoint string
	Timeout  int
}

// SkillsHarnessConfig
type SkillsHarnessConfig struct {
	Skills []SkillConfig
}

// SkillConfig
type SkillConfig struct {
	Name    string
	Version string
	Allowed bool
}

// AgentStatus contains agent status from Framework Container.
type AgentStatus struct {
	ID           string
	Name         string
	Phase        proto.AgentPhase
	Running      bool
	SessionCount int64
	Metrics      *proto.AgentMetrics
}

// AgentEvent represents an event from agent execution.
type AgentEvent struct {
	InvocationID string
	Author       string
	Type         EventType
	Content      *agent.Content
	Error        string
	Timestamp    int64
	Metadata     map[string]string
}

// EventType
type EventType int

const (
	EventTypeText EventType = 0
	EventTypeError EventType = 1
	EventTypeComplete EventType = 2
)

// Helper conversion functions

func convertAgentType(t agent.AgentType) proto.AgentType {
	switch t {
	case agent.AgentTypeLLM:
		return proto.AgentType_AGENT_TYPE_LLM
	case agent.AgentTypeSequential:
		return proto.AgentType_AGENT_TYPE_SEQUENTIAL
	case agent.AgentTypeParallel:
		return proto.AgentType_AGENT_TYPE_PARALLEL
	case agent.AgentTypeLoop:
		return proto.AgentType_AGENT_TYPE_LOOP
	default:
		return proto.AgentType_AGENT_TYPE_CUSTOM
	}
}

func convertProtoAgentType(t proto.AgentType) agent.AgentType {
	switch t {
	case proto.AgentType_AGENT_TYPE_LLM:
		return agent.AgentTypeLLM
	case proto.AgentType_AGENT_TYPE_SEQUENTIAL:
		return agent.AgentTypeSequential
	case proto.AgentType_AGENT_TYPE_PARALLEL:
		return agent.AgentTypeParallel
	case proto.AgentType_AGENT_TYPE_LOOP:
		return agent.AgentTypeLoop
	default:
		return agent.AgentTypeCustom
	}
}

func convertHarnessConfig(h *HarnessConfig) *proto.HarnessConfig {
	if h == nil {
		return nil
	}

	return &proto.HarnessConfig{
		Model:   convertModelHarnessConfig(h.Model),
		Memory:  convertMemoryHarnessConfig(h.Memory),
		Sandbox: convertSandboxHarnessConfig(h.Sandbox),
		Skills:  convertSkillsHarnessConfig(h.Skills),
	}
}

func convertModelHarnessConfig(m *ModelHarnessConfig) *proto.ModelHarnessConfig {
	if m == nil {
		return nil
	}

	return &proto.ModelHarnessConfig{
		Provider:     m.Provider,
		Endpoint:     m.Endpoint,
		DefaultModel: m.DefaultModel,
	}
}

func convertMemoryHarnessConfig(m *MemoryHarnessConfig) *proto.MemoryHarnessConfig {
	if m == nil {
		return nil
	}

	return &proto.MemoryHarnessConfig{
		Type:     m.Type,
		Endpoint: m.Endpoint,
		Ttl:      int32(m.TTL),
	}
}

func convertSandboxHarnessConfig(s *SandboxHarnessConfig) *proto.SandboxHarnessConfig {
	if s == nil {
		return nil
	}

	return &proto.SandboxHarnessConfig{
		Type:     s.Type,
		Mode:     s.Mode,
		Endpoint: s.Endpoint,
		Timeout:  int32(s.Timeout),
	}
}

func convertSkillsHarnessConfig(s *SkillsHarnessConfig) *proto.SkillsHarnessConfig {
	if s == nil {
		return nil
	}

	skills := make([]*proto.SkillConfig, 0)
	for _, skill := range s.Skills {
		skills = append(skills, &proto.SkillConfig{
			Name:    skill.Name,
			Version: skill.Version,
			Allowed: skill.Allowed,
		})
	}

	return &proto.SkillsHarnessConfig{Skills: skills}
}

func convertContentToProto(c *agent.Content) *proto.Content {
	if c == nil {
		return nil
	}

	// Extract text from Parts if available
	text := ""
	if len(c.Parts) > 0 {
		text = c.Parts[0].Text
	}

	return &proto.Content{
		Type:     "text",
		Text:     text,
		MimeType: "text/plain",
	}
}

func convertProtoEvent(e *proto.AgentEvent) *AgentEvent {
	return &AgentEvent{
		InvocationID: e.InvocationId,
		Author:       e.Author,
		Type:         convertProtoEventType(e.Type),
		Content:      convertProtoToContent(e.Content),
		Error:        e.Error,
		Timestamp:    e.Timestamp,
		Metadata:     e.Metadata,
	}
}

func convertProtoEventType(t proto.EventType) EventType {
	switch t {
	case proto.EventType_EVENT_TYPE_TEXT:
		return EventTypeText
	case proto.EventType_EVENT_TYPE_ERROR:
		return EventTypeError
	case proto.EventType_EVENT_TYPE_COMPLETE:
		return EventTypeComplete
	default:
		return EventTypeText
	}
}

func convertProtoToContent(c *proto.Content) *agent.Content {
	if c == nil {
		return nil
	}

	return &agent.Content{
		Role: "model",
		Parts: []*agent.Part{
			{Text: c.Text},
		},
	}
}