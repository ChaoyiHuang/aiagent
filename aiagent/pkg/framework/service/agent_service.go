// Package service provides gRPC service implementation for Framework Container.
// This service runs in the Framework Container and executes agents.
package service

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"aiagent/pkg/agent"
	"aiagent/pkg/framework/proto"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// AgentService implements proto.AgentServiceServer.
// It manages agent instances in the Framework Container.
type AgentService struct {
	proto.UnimplementedAgentServiceServer

	mu     sync.RWMutex
	agents map[string]*AgentInstance

	startTime time.Time
	version   string
}

// AgentInstance represents an agent instance in the Framework Container.
type AgentInstance struct {
	ID       string
	Agent    agent.Agent
	Config   *AgentInstanceConfig
	Phase    proto.AgentPhase
	Running  bool
	Metrics  *proto.AgentMetrics
	Sessions map[string]agent.Session
}

// AgentInstanceConfig contains configuration for an agent instance.
type AgentInstanceConfig struct {
	Name        string
	Description string
	Model       string
	Type        proto.AgentType
	Instruction string
	Tools       []agent.Tool
}

// NewAgentService creates a new AgentService.
func NewAgentService(version string) *AgentService {
	return &AgentService{
		agents:    make(map[string]*AgentInstance),
		startTime: time.Now(),
		version:   version,
	}
}

// CreateAgent creates a new agent instance.
func (s *AgentService) CreateAgent(ctx context.Context, req *proto.CreateAgentRequest) (*proto.CreateAgentResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check if agent already exists
	if _, exists := s.agents[req.AgentId]; exists {
		return &proto.CreateAgentResponse{
			AgentId: req.AgentId,
			Success: false,
			Error:   fmt.Sprintf("agent %s already exists", req.AgentId),
		}, nil
	}

	// Create agent configuration
	config := &AgentInstanceConfig{
		Name:        req.Name,
		Description: req.Description,
		Model:       req.Model,
		Type:        req.Type,
		Instruction: req.Instruction,
	}

	// Create agent instance based on type
	ag, err := s.createAgentFromConfig(config)
	if err != nil {
		return &proto.CreateAgentResponse{
			AgentId: req.AgentId,
			Success: false,
			Error:   err.Error(),
		}, nil
	}

	// Create instance
	instance := &AgentInstance{
		ID:       req.AgentId,
		Agent:    ag,
		Config:   config,
		Phase:    proto.AgentPhase_AGENT_PHASE_PENDING,
		Running:  false,
		Metrics:  &proto.AgentMetrics{},
		Sessions: make(map[string]agent.Session),
	}

	s.agents[req.AgentId] = instance

	log.Printf("Created agent: %s (type: %v)", req.AgentId, req.Type)

	return &proto.CreateAgentResponse{
		AgentId: req.AgentId,
		Success: true,
	}, nil
}

// createAgentFromConfig creates an agent based on configuration.
func (s *AgentService) createAgentFromConfig(config *AgentInstanceConfig) (agent.Agent, error) {
	// Create base agent
	agentType := agent.AgentTypeLLM
	switch config.Type {
	case proto.AgentType_AGENT_TYPE_SEQUENTIAL:
		agentType = agent.AgentTypeSequential
	case proto.AgentType_AGENT_TYPE_PARALLEL:
		agentType = agent.AgentTypeParallel
	case proto.AgentType_AGENT_TYPE_LOOP:
		agentType = agent.AgentTypeLoop
	}

	baseAgent := agent.NewBaseAgent(agent.Config{
		Name:        config.Name,
		Description: config.Description,
	}, agentType)

	return baseAgent, nil
}

// StartAgent starts an agent instance.
func (s *AgentService) StartAgent(ctx context.Context, req *proto.StartAgentRequest) (*proto.StartAgentResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	instance, exists := s.agents[req.AgentId]
	if !exists {
		return &proto.StartAgentResponse{
			Success: false,
			Error:   fmt.Sprintf("agent %s not found", req.AgentId),
		}, nil
	}

	instance.Running = true
	instance.Phase = proto.AgentPhase_AGENT_PHASE_RUNNING

	log.Printf("Started agent: %s", req.AgentId)

	return &proto.StartAgentResponse{Success: true}, nil
}

// StopAgent stops an agent instance.
func (s *AgentService) StopAgent(ctx context.Context, req *proto.StopAgentRequest) (*proto.StopAgentResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	instance, exists := s.agents[req.AgentId]
	if !exists {
		return &proto.StopAgentResponse{
			Success: false,
			Error:   fmt.Sprintf("agent %s not found", req.AgentId),
		}, nil
	}

	instance.Running = false
	instance.Phase = proto.AgentPhase_AGENT_PHASE_TERMINATED

	log.Printf("Stopped agent: %s", req.AgentId)

	return &proto.StopAgentResponse{Success: true}, nil
}

// DeleteAgent deletes an agent instance.
func (s *AgentService) DeleteAgent(ctx context.Context, req *proto.DeleteAgentRequest) (*proto.DeleteAgentResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.agents[req.AgentId]; !exists {
		return &proto.DeleteAgentResponse{
			Success: false,
			Error:   fmt.Sprintf("agent %s not found", req.AgentId),
		}, nil
	}

	delete(s.agents, req.AgentId)

	log.Printf("Deleted agent: %s", req.AgentId)

	return &proto.DeleteAgentResponse{Success: true}, nil
}

// GetAgentStatus returns the status of an agent.
func (s *AgentService) GetAgentStatus(ctx context.Context, req *proto.GetAgentStatusRequest) (*proto.GetAgentStatusResponse, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	instance, exists := s.agents[req.AgentId]
	if !exists {
		return nil, status.Errorf(codes.NotFound, "agent %s not found", req.AgentId)
	}

	return &proto.GetAgentStatusResponse{
		AgentId:          instance.ID,
		Name:             instance.Config.Name,
		Phase:            instance.Phase,
		Running:          instance.Running,
		SessionCount:     int64(len(instance.Sessions)),
		LastActivityTime: instance.Metrics.TotalInvocations,
		Metrics:          instance.Metrics,
	}, nil
}

// ListAgents lists all agents in the Framework Container.
func (s *AgentService) ListAgents(ctx context.Context, req *proto.ListAgentsRequest) (*proto.ListAgentsResponse, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	agents := make([]*proto.AgentInfo, 0)
	for _, instance := range s.agents {
		agents = append(agents, &proto.AgentInfo{
			Id:    instance.ID,
			Name:  instance.Config.Name,
			Type:  instance.Config.Type,
			Phase: instance.Phase,
		})
	}

	return &proto.ListAgentsResponse{Agents: agents}, nil
}

// RunAgent runs an agent invocation and streams events.
func (s *AgentService) RunAgent(req *proto.RunAgentRequest, stream proto.AgentService_RunAgentServer) error {
	s.mu.RLock()
	instance, exists := s.agents[req.AgentId]
	s.mu.RUnlock()

	if !exists {
		return status.Errorf(codes.NotFound, "agent %s not found", req.AgentId)
	}

	if !instance.Running {
		return status.Errorf(codes.FailedPrecondition, "agent %s is not running", req.AgentId)
	}

	// Create invocation context
	userContent := convertProtoToContent(req.UserContent)
	session := createOrGetSession(instance, req.SessionId)

	invCtx := agent.NewInvocationContext(
		stream.Context(),
		instance.Agent,
		nil, // artifacts
		nil, // memory
		session,
		req.InvocationId,
		"",  // branch
		userContent,
		&agent.RunConfig{},
	)

	// Run agent and stream events
	for event, err := range instance.Agent.Run(invCtx) {
		if err != nil {
			// Send error event
			protoEvent := &proto.AgentEvent{
				InvocationId: req.InvocationId,
				Type:         proto.EventType_EVENT_TYPE_ERROR,
				Error:        err.Error(),
				Timestamp:    time.Now().Unix(),
			}
			if err := stream.Send(protoEvent); err != nil {
				return err
			}
			break
		}

		// Convert and send event
		protoEvent := convertEventToProto(event, req.InvocationId)
		if err := stream.Send(protoEvent); err != nil {
			return err
		}
	}

	// Send completion event
	completeEvent := &proto.AgentEvent{
		InvocationId: req.InvocationId,
		Type:         proto.EventType_EVENT_TYPE_COMPLETE,
		Timestamp:    time.Now().Unix(),
	}
	return stream.Send(completeEvent)
}

// SendMessage sends a message to an agent.
func (s *AgentService) SendMessage(ctx context.Context, req *proto.SendMessageRequest) (*proto.SendMessageResponse, error) {
	s.mu.RLock()
	_, exists := s.agents[req.AgentId]
	s.mu.RUnlock()

	if !exists {
		return nil, status.Errorf(codes.NotFound, "agent %s not found", req.AgentId)
	}

	// For now, return a simple response
	// In real implementation, this would invoke the agent
	return &proto.SendMessageResponse{
		Response: &proto.Content{
			Type: "text",
			Text: fmt.Sprintf("Agent %s received message", req.AgentId),
		},
		SessionId: req.SessionId,
	}, nil
}

// HealthCheck returns the health status of the Framework Container.
func (s *AgentService) HealthCheck(ctx context.Context, req *proto.HealthCheckRequest) (*proto.HealthCheckResponse, error) {
	s.mu.RLock()
	agentCount := len(s.agents)
	s.mu.RUnlock()

	return &proto.HealthCheckResponse{
		Healthy:       true,
		Version:       s.version,
		UptimeSeconds: int64(time.Since(s.startTime).Seconds()),
		AgentCount:    int32(agentCount),
	}, nil
}

// Helper functions

func convertProtoToContent(content *proto.Content) *agent.Content {
	if content == nil {
		return nil
	}

	return &agent.Content{
		Role: "model",
		Parts: []*agent.Part{
			{Text: content.Text},
		},
	}
}

func convertEventToProto(event *agent.Event, invocationID string) *proto.AgentEvent {
	if event == nil {
		return nil
	}

	eventType := proto.EventType_EVENT_TYPE_TEXT

	content := &proto.Content{}
	if event.LLMResponse.Content != nil && len(event.LLMResponse.Content.Parts) > 0 {
		content.Text = event.LLMResponse.Content.Parts[0].Text
	}

	return &proto.AgentEvent{
		InvocationId: invocationID,
		Author:       event.Author,
		Type:         eventType,
		Content:      content,
		Timestamp:    event.Timestamp.Unix(),
	}
}

func createOrGetSession(instance *AgentInstance, sessionID string) agent.Session {
	if sessionID == "" {
		sessionID = fmt.Sprintf("session-%d", time.Now().UnixNano())
	}

	if session, exists := instance.Sessions[sessionID]; exists {
		return session
	}

	// Create new session with MapState
	session := agent.NewSession(sessionID, instance.ID, "default-user", agent.NewMapState())
	instance.Sessions[sessionID] = session

	return session
}