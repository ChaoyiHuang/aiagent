package agent

import (
	"time"
)

// Event represents an interaction in a conversation between agents and users.
// It stores the content of the conversation, as well as the actions taken by
// the agents like function calls, state changes, artifact updates, etc.
type Event struct {
	// LLMResponse contains the model's response content.
	LLMResponse LLMResponse

	// ID is the unique identifier of this event.
	ID string

	// Timestamp when this event was created.
	Timestamp time.Time

	// InvocationID identifies which invocation this event belongs to.
	InvocationID string

	// Branch is the branch path for this event.
	// Format: "agent_1.agent_2.agent_3" where agent_1 is parent of agent_2.
	Branch string

	// Author is the name of the event's author (agent name or "user").
	Author string

	// Actions contains the actions attached to this event.
	Actions EventActions

	// LongRunningToolIDs contains IDs of long-running function calls.
	// Agent clients will know from this field which function call is long-running.
	LongRunningToolIDs []string
}

// LLMResponse contains the response from an LLM model.
type LLMResponse struct {
	// Content contains the response parts.
	Content *Content

	// Usage contains token usage statistics.
	Usage *Usage

	// Thought indicates if this response is a thought (internal reasoning).
	Thought bool

	// Partial indicates this is a partial streaming response.
	Partial bool
}

// Usage contains token usage statistics.
type Usage struct {
	// PromptTokens is the number of tokens in the prompt.
	PromptTokens int

	// CompletionTokens is the number of tokens in the completion.
	CompletionTokens int

	// TotalTokens is the total number of tokens.
	TotalTokens int
}

// EventActions represent the actions attached to an event.
type EventActions struct {
	// StateDelta contains state key-value pairs to update.
	StateDelta map[string]any

	// ArtifactDelta contains artifact updates. Key is filename, value is version.
	ArtifactDelta map[string]int64

	// RequestedToolConfirmations contains tool confirmation requests.
	RequestedToolConfirmations map[string]ToolConfirmation

	// SkipSummarization indicates not to call model to summarize function response.
	SkipSummarization bool

	// TransferToAgent indicates the agent should transfer to another agent.
	TransferToAgent string

	// Escalate indicates the agent is escalating to a higher-level agent.
	Escalate bool
}

// ToolConfirmation represents a request for user confirmation of a tool call.
type ToolConfirmation struct {
	// ToolName is the name of the tool requesting confirmation.
	ToolName string

	// ToolArgs are the arguments passed to the tool.
	ToolArgs map[string]any

	// Hint is a human-readable explanation of why confirmation is needed.
	Hint string

	// Confirmed indicates whether the user confirmed the tool call.
	Confirmed bool

	// Rejected indicates whether the user rejected the tool call.
	Rejected bool
}

// NewEvent creates a new Event with the current timestamp.
func NewEvent(invocationID string) *Event {
	return &Event{
		ID:           generateEventID(),
		InvocationID: invocationID,
		Timestamp:    time.Now(),
		Actions: EventActions{
			StateDelta:    make(map[string]any),
			ArtifactDelta: make(map[string]int64),
		},
	}
}

// IsFinalResponse returns whether the event is the final response of an agent.
func (e *Event) IsFinalResponse() bool {
	if e.Actions.SkipSummarization || len(e.LongRunningToolIDs) > 0 {
		return true
	}

	return !e.hasFunctionCalls() &&
		!e.hasFunctionResponses() &&
		!e.LLMResponse.Partial &&
		!e.hasTrailingCodeExecutionResult()
}

// hasFunctionCalls returns true if the event contains function calls.
func (e *Event) hasFunctionCalls() bool {
	if e.LLMResponse.Content == nil {
		return false
	}
	for _, part := range e.LLMResponse.Content.Parts {
		if part.FunctionCall != nil {
			return true
		}
	}
	return false
}

// hasFunctionResponses returns true if the event contains function responses.
func (e *Event) hasFunctionResponses() bool {
	if e.LLMResponse.Content == nil {
		return false
	}
	for _, part := range e.LLMResponse.Content.Parts {
		if part.FunctionResponse != nil {
			return true
		}
	}
	return false
}

// hasTrailingCodeExecutionResult returns true if the last part is a code execution result.
func (e *Event) hasTrailingCodeExecutionResult() bool {
	if e.LLMResponse.Content == nil || len(e.LLMResponse.Content.Parts) == 0 {
		return false
	}
	lastPart := e.LLMResponse.Content.Parts[len(e.LLMResponse.Content.Parts)-1]
	return lastPart.CodeExecutionResult != nil
}

// generateEventID creates a unique event ID.
func generateEventID() string {
	return "event-" + time.Now().Format("20060102-150405-") + randomSuffix()
}

// randomSuffix generates a short random suffix for uniqueness.
func randomSuffix() string {
	const chars = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, 4)
	for i := range b {
		b[i] = chars[i % len(chars)]
	}
	return string(b)
}

// EventBuilder provides a builder pattern for creating events.
type EventBuilder struct {
	event *Event
}

// NewEventBuilder creates a new EventBuilder.
func NewEventBuilder(invocationID string) *EventBuilder {
	return &EventBuilder{
		event: NewEvent(invocationID),
	}
}

// WithAuthor sets the author of the event.
func (b *EventBuilder) WithAuthor(author string) *EventBuilder {
	b.event.Author = author
	return b
}

// WithBranch sets the branch of the event.
func (b *EventBuilder) WithBranch(branch string) *EventBuilder {
	b.event.Branch = branch
	return b
}

// WithContent sets the content of the event.
func (b *EventBuilder) WithContent(content *Content) *EventBuilder {
	b.event.LLMResponse.Content = content
	return b
}

// WithTextContent sets a simple text content.
func (b *EventBuilder) WithTextContent(text string) *EventBuilder {
	b.event.LLMResponse.Content = &Content{
		Role: "model",
		Parts: []*Part{{Text: text}},
	}
	return b
}

// WithStateDelta adds a state delta entry.
func (b *EventBuilder) WithStateDelta(key string, value any) *EventBuilder {
	if b.event.Actions.StateDelta == nil {
		b.event.Actions.StateDelta = make(map[string]any)
	}
	b.event.Actions.StateDelta[key] = value
	return b
}

// WithArtifactDelta adds an artifact delta entry.
func (b *EventBuilder) WithArtifactDelta(name string, version int64) *EventBuilder {
	if b.event.Actions.ArtifactDelta == nil {
		b.event.Actions.ArtifactDelta = make(map[string]int64)
	}
	b.event.Actions.ArtifactDelta[name] = version
	return b
}

// WithTransferToAgent sets the transfer target.
func (b *EventBuilder) WithTransferToAgent(agentName string) *EventBuilder {
	b.event.Actions.TransferToAgent = agentName
	return b
}

// WithThought marks the event as a thought.
func (b *EventBuilder) WithThought() *EventBuilder {
	b.event.LLMResponse.Thought = true
	return b
}

// WithPartial marks the event as partial.
func (b *EventBuilder) WithPartial() *EventBuilder {
	b.event.LLMResponse.Partial = true
	return b
}

// Build returns the constructed event.
func (b *EventBuilder) Build() *Event {
	return b.event
}