package agent

import (
	"context"
	"testing"
)

func TestBaseAgent_Name(t *testing.T) {
	agent := NewBaseAgent(Config{
		Name:        "test-agent",
		Description: "A test agent",
	}, AgentTypeLLM)

	if agent.Name() != "test-agent" {
		t.Errorf("expected name 'test-agent', got '%s'", agent.Name())
	}
}

func TestBaseAgent_Description(t *testing.T) {
	agent := NewBaseAgent(Config{
		Name:        "test-agent",
		Description: "A test agent for testing",
	}, AgentTypeLLM)

	if agent.Description() != "A test agent for testing" {
		t.Errorf("expected description 'A test agent for testing', got '%s'", agent.Description())
	}
}

func TestBaseAgent_Type(t *testing.T) {
	tests := []struct {
		name     string
		agentType AgentType
	}{
		{"llm", AgentTypeLLM},
		{"sequential", AgentTypeSequential},
		{"parallel", AgentTypeParallel},
		{"loop", AgentTypeLoop},
		{"custom", AgentTypeCustom},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			agent := NewBaseAgent(Config{
				Name: "test-agent",
			}, tc.agentType)

			if agent.Type() != tc.agentType {
				t.Errorf("expected type '%s', got '%s'", tc.agentType, agent.Type())
			}
		})
	}
}

func TestBaseAgent_FindAgent(t *testing.T) {
	parent := NewBaseAgent(Config{
		Name: "parent",
	}, AgentTypeLLM)

	child := NewBaseAgent(Config{
		Name: "child",
	}, AgentTypeLLM)

	parentWithSub := NewBaseAgent(Config{
		Name:     "parent-with-sub",
		SubAgents: []Agent{child},
	}, AgentTypeLLM)

	// Find self
	if parent.FindAgent("parent") != parent {
		t.Error("expected to find self")
	}

	// Find sub-agent
	found := parentWithSub.FindAgent("child")
	if found == nil {
		t.Error("expected to find child agent")
	}
	if found.Name() != "child" {
		t.Errorf("expected found agent name 'child', got '%s'", found.Name())
	}

	// Not found
	if parent.FindAgent("nonexistent") != nil {
		t.Error("expected nil for nonexistent agent")
	}
}

func TestBaseAgent_SubAgents(t *testing.T) {
	child1 := NewBaseAgent(Config{Name: "child1"}, AgentTypeLLM)
	child2 := NewBaseAgent(Config{Name: "child2"}, AgentTypeLLM)

	parent := NewBaseAgent(Config{
		Name:     "parent",
		SubAgents: []Agent{child1, child2},
	}, AgentTypeLLM)

	subs := parent.SubAgents()
	if len(subs) != 2 {
		t.Errorf("expected 2 sub-agents, got %d", len(subs))
	}
}

func TestBaseInvocationContext(t *testing.T) {
	ctx := context.Background()
	agent := NewBaseAgent(Config{Name: "test"}, AgentTypeLLM)
	session := NewSession("session-1", "app", "user1", NewMapState())

	invCtx := NewInvocationContext(
		ctx,
		agent,
		nil, // artifacts
		nil, // memory
		session,
		"inv-1",
		"test",
		&Content{Role: "user", Parts: []*Part{{Text: "hello"}}},
		&RunConfig{},
	)

	if invCtx.Agent().Name() != "test" {
		t.Errorf("expected agent name 'test', got '%s'", invCtx.Agent().Name())
	}

	if invCtx.Session().ID() != "session-1" {
		t.Errorf("expected session ID 'session-1', got '%s'", invCtx.Session().ID())
	}

	if invCtx.InvocationID() != "inv-1" {
		t.Errorf("expected invocation ID 'inv-1', got '%s'", invCtx.InvocationID())
	}

	if invCtx.Ended() {
		t.Error("expected invocation not ended")
	}

	invCtx.EndInvocation()
	if !invCtx.Ended() {
		t.Error("expected invocation ended after EndInvocation()")
	}
}

func TestMapState(t *testing.T) {
	state := NewMapState()

	// Set and Get
	state.Set("key1", "value1")
	val, err := state.Get("key1")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if val != "value1" {
		t.Errorf("expected 'value1', got '%v'", val)
	}

	// Non-existent key
	_, err = state.Get("nonexistent")
	if err != ErrStateKeyNotExist {
		t.Errorf("expected ErrStateKeyNotExist, got %v", err)
	}

	// Delete
	state.Delete("key1")
	_, err = state.Get("key1")
	if err != ErrStateKeyNotExist {
		t.Errorf("expected ErrStateKeyNotExist after delete, got %v", err)
	}

	// Clear
	state.Set("a", 1)
	state.Set("b", 2)
	state.Clear()

	_, err = state.Get("a")
	if err != ErrStateKeyNotExist {
		t.Error("expected ErrStateKeyNotExist after Clear()")
	}
}

func TestSession(t *testing.T) {
	state := NewMapState()
	session := NewSession("sess-1", "myapp", "user123", state)

	if session.ID() != "sess-1" {
		t.Errorf("expected ID 'sess-1', got '%s'", session.ID())
	}

	if session.AppName() != "myapp" {
		t.Errorf("expected appName 'myapp', got '%s'", session.AppName())
	}

	if session.UserID() != "user123" {
		t.Errorf("expected userID 'user123', got '%s'", session.UserID())
	}

	// Events
	if session.Events().Len() != 0 {
		t.Errorf("expected 0 events, got %d", session.Events().Len())
	}

	event := NewEvent("inv-1")
	event.Author = "user"
	session.AppendEvent(event)

	if session.Events().Len() != 1 {
		t.Errorf("expected 1 event after AppendEvent, got %d", session.Events().Len())
	}
}

func TestEventBuilder(t *testing.T) {
	event := NewEventBuilder("inv-1").
		WithAuthor("model").
		WithBranch("root.child").
		WithTextContent("Hello, world!").
		WithStateDelta("key1", "value1").
		WithTransferToAgent("next-agent").
		Build()

	if event.Author != "model" {
		t.Errorf("expected author 'model', got '%s'", event.Author)
	}

	if event.Branch != "root.child" {
		t.Errorf("expected branch 'root.child', got '%s'", event.Branch)
	}

	if event.LLMResponse.Content == nil {
		t.Error("expected content not nil")
	}

	if len(event.LLMResponse.Content.Parts) != 1 {
		t.Errorf("expected 1 part, got %d", len(event.LLMResponse.Content.Parts))
	}

	if event.Actions.StateDelta["key1"] != "value1" {
		t.Errorf("expected state delta key1=value1, got %v", event.Actions.StateDelta["key1"])
	}

	if event.Actions.TransferToAgent != "next-agent" {
		t.Errorf("expected transfer to 'next-agent', got '%s'", event.Actions.TransferToAgent)
	}
}

func TestEvent_IsFinalResponse(t *testing.T) {
	// Text response should be final
	textEvent := NewEventBuilder("inv-1").
		WithTextContent("Final answer").
		Build()

	if !textEvent.IsFinalResponse() {
		t.Error("expected text event to be final response")
	}

	// Partial event should not be final
	partialEvent := NewEventBuilder("inv-1").
		WithTextContent("Partial...").
		WithPartial().
		Build()

	if partialEvent.IsFinalResponse() {
		t.Error("expected partial event not to be final response")
	}

	// Function call event should not be final
	funcCallEvent := NewEvent("inv-1")
	funcCallEvent.LLMResponse.Content = &Content{
		Role: "model",
		Parts: []*Part{{
			FunctionCall: &FunctionCall{
				ID:   "fc-1",
				Name: "get_weather",
				Args: map[string]any{"city": "NYC"},
			},
		}},
	}

	if funcCallEvent.IsFinalResponse() {
		t.Error("expected function call event not to be final response")
	}
}

func TestToolBuilder(t *testing.T) {
	tool := NewToolBuilder("calculator").
		WithDescription("A simple calculator").
		WithParameters(map[string]any{
			"type": "object",
			"properties": map[string]any{
				"expression": map[string]any{
					"type":        "string",
					"description": "Mathematical expression to evaluate",
				},
			},
		}).
		WithRun(func(ctx ToolContext, args any) (map[string]any, error) {
			return map[string]any{"result": 42}, nil
		}).
		Build()

	if tool.Name() != "calculator" {
		t.Errorf("expected name 'calculator', got '%s'", tool.Name())
	}

	if tool.Description() != "A simple calculator" {
		t.Errorf("expected description 'A simple calculator', got '%s'", tool.Description())
	}

	if tool.IsLongRunning() {
		t.Error("expected not long-running")
	}

	if tool.Declaration().Name != "calculator" {
		t.Errorf("expected declaration name 'calculator', got '%s'", tool.Declaration().Name)
	}
}

func TestFilterToolset(t *testing.T) {
	tool1 := NewToolBuilder("tool1").WithDescription("First tool").Build()
	tool2 := NewToolBuilder("tool2").WithDescription("Second tool").Build()
	tool3 := NewToolBuilder("tool3").WithDescription("Third tool").Build()

	baseToolset := &simpleToolset{
		name:  "base",
		tools: []Tool{tool1, tool2, tool3},
	}

	filtered := FilterToolset(baseToolset, AllowedToolsPredicate([]string{"tool1", "tool3"}))

	tools, err := filtered.Tools(nil)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if len(tools) != 2 {
		t.Errorf("expected 2 filtered tools, got %d", len(tools))
	}

	// Verify tool names
	names := make([]string, len(tools))
	for i, t := range tools {
		names[i] = t.Name()
	}

	expected := []string{"tool1", "tool3"}
	for i, n := range expected {
		if names[i] != n {
			t.Errorf("expected tool '%s', got '%s'", n, names[i])
		}
	}
}

func TestInMemorySessionService(t *testing.T) {
	service := NewInMemorySessionService()

	// Create
	createResp, err := service.Create(nil, &CreateSessionRequest{
		AppName:   "test-app",
		UserID:    "user1",
		SessionID: "session-1",
		State:     map[string]any{"init": true},
	})
	if err != nil {
		t.Errorf("unexpected error creating session: %v", err)
	}

	if createResp.Session.ID() != "session-1" {
		t.Errorf("expected session ID 'session-1', got '%s'", createResp.Session.ID())
	}

	// Get
	getResp, err := service.Get(nil, &GetSessionRequest{
		AppName:   "test-app",
		UserID:    "user1",
		SessionID: "session-1",
	})
	if err != nil {
		t.Errorf("unexpected error getting session: %v", err)
	}

	if getResp.Session.ID() != "session-1" {
		t.Errorf("expected get session ID 'session-1', got '%s'", getResp.Session.ID())
	}

	// List
	listResp, err := service.List(nil, &ListSessionRequest{
		AppName: "test-app",
		UserID:  "user1",
	})
	if err != nil {
		t.Errorf("unexpected error listing sessions: %v", err)
	}

	if len(listResp.Sessions) != 1 {
		t.Errorf("expected 1 session in list, got %d", len(listResp.Sessions))
	}

	// Delete
	err = service.Delete(nil, &DeleteSessionRequest{
		AppName:   "test-app",
		UserID:    "user1",
		SessionID: "session-1",
	})
	if err != nil {
		t.Errorf("unexpected error deleting session: %v", err)
	}

	// Verify deleted
	getResp, err = service.Get(nil, &GetSessionRequest{
		AppName:   "test-app",
		UserID:    "user1",
		SessionID: "session-1",
	})
	if err == nil {
		t.Error("expected error for deleted session")
	}
}

// simpleToolset implements Toolset for testing
type simpleToolset struct {
	name  string
	tools []Tool
}

func (s *simpleToolset) Name() string {
	return s.name
}

func (s *simpleToolset) Tools(ctx ReadonlyContext) ([]Tool, error) {
	return s.tools, nil
}