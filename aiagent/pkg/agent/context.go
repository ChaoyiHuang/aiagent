package agent

import (
	"context"
)

// InvocationContext represents the context of an agent invocation.
//
// An invocation:
//  1. Starts with a user message and ends with a final response.
//  2. Can contain one or multiple agent calls.
//  3. Is handled by the runtime's Run() method.
//
// An invocation runs an agent until it does not request to transfer to another
// agent.
//
// An agent call:
//  1. Is handled by agent.Run().
//  2. Ends when agent.Run() ends.
//
// An agent call can contain one or multiple steps.
// For example, LLM agent runs steps in a loop until:
//  1. A final response is generated.
//  2. The agent transfers to another agent.
//  3. EndInvocation() was called.
type InvocationContext interface {
	context.Context

	// Agent of this invocation context.
	Agent() Agent

	// Artifacts provides methods to work with artifacts of the current session.
	Artifacts() Artifacts

	// Memory is scoped to sessions of the current user_id.
	Memory() Memory

	// Session of the current invocation context.
	Session() Session

	// InvocationID returns the unique identifier of this invocation.
	InvocationID() string

	// Branch returns the branch path for this invocation.
	// Format: "agent_1.agent_2.agent_3" where agent_1 is parent of agent_2.
	// Used when multiple sub-agents shouldn't see their peer agents' conversation history.
	Branch() string

	// UserContent that started this invocation.
	UserContent() *Content

	// RunConfig stores the runtime configuration used during this invocation.
	RunConfig() *RunConfig

	// EndInvocation ends the current invocation, stopping any planned agent calls.
	EndInvocation()

	// Ended returns whether the invocation has ended.
	Ended() bool

	// WithContext returns a new InvocationContext with the given context.Context.
	WithContext(ctx context.Context) InvocationContext
}

// ReadonlyContext provides read-only access to invocation context data.
type ReadonlyContext interface {
	context.Context

	// UserContent that started this invocation.
	UserContent() *Content

	// InvocationID returns the unique identifier of this invocation.
	InvocationID() string

	// AgentName returns the name of the agent in this invocation.
	AgentName() string

	// ReadonlyState returns read-only access to session state.
	ReadonlyState() ReadonlyState

	// UserID returns the user ID for this session.
	UserID() string

	// AppName returns the application name.
	AppName() string

	// SessionID returns the session ID.
	SessionID() string

	// Branch returns the branch path for this invocation.
	Branch() string
}

// CallbackContext is passed to user callbacks during agent execution.
type CallbackContext interface {
	ReadonlyContext

	// Artifacts provides methods to work with artifacts.
	Artifacts() Artifacts

	// State returns mutable access to session state.
	State() State
}

// RunConfig stores runtime configuration for agent execution.
type RunConfig struct {
	// SaveInputBlobsAsArtifacts indicates whether to save input blobs as artifacts.
	SaveInputBlobsAsArtifacts bool

	// StreamingMode controls how responses are streamed.
	StreamingMode StreamingMode

	// MaxTurns limits the number of turns in a single invocation.
	MaxTurns int

	// MaxTokens limits the total tokens consumed in this invocation.
	MaxTokens int

	// Timeout for the entire invocation in seconds.
	Timeout int

	// EnableCodeExecution enables code execution tools.
	EnableCodeExecution bool

	// AdditionalConfig holds framework-specific configuration.
	AdditionalConfig map[string]any
}

// StreamingMode controls how model responses are streamed.
type StreamingMode string

const (
	StreamingModeNone     StreamingMode = "none"     // No streaming, wait for complete response
	StreamingModeInterim  StreamingMode = "interim"  // Stream interim responses
	StreamingModeFull     StreamingMode = "full"     // Stream all content including thoughts
)

// Artifacts interface provides methods to work with artifacts of the current session.
type Artifacts interface {
	// Save saves an artifact and returns its version.
	Save(ctx context.Context, name string, part *Part) (int64, error)

	// List returns all artifacts in the session.
	List(ctx context.Context) ([]string, error)

	// Load loads the latest version of an artifact.
	Load(ctx context.Context, name string) (*Part, error)

	// LoadVersion loads a specific version of an artifact.
	LoadVersion(ctx context.Context, name string, version int64) (*Part, error)
}

// Memory interface provides methods to access agent memory across the
// sessions of the current user_id.
type Memory interface {
	// AddSessionToMemory adds a session to the agent's memory.
	AddSessionToMemory(ctx context.Context, session Session) error

	// SearchMemory performs a semantic search on the agent's memory.
	SearchMemory(ctx context.Context, query string) (*SearchResponse, error)
}

// SearchResponse contains results from a memory search.
type SearchResponse struct {
	// Memories found by the search query.
	Memories []*MemoryEntry

	// Total count of matching memories.
	Total int
}

// MemoryEntry represents a single memory entry.
type MemoryEntry struct {
	// Content of the memory.
	Content string

	// SessionID where this memory was stored.
	SessionID string

	// Timestamp when this memory was created.
	Timestamp int64

	// Score relevance score for this memory in the search results.
	Score float64
}

// BaseInvocationContext provides a base implementation of InvocationContext.
type BaseInvocationContext struct {
	context.Context

	agent        Agent
	artifacts    Artifacts
	memory       Memory
	session      Session
	invocationID string
	branch       string
	userContent  *Content
	runConfig    *RunConfig
	ended        bool
}

// NewInvocationContext creates a new BaseInvocationContext.
func NewInvocationContext(
	ctx context.Context,
	agent Agent,
	artifacts Artifacts,
	memory Memory,
	session Session,
	invocationID string,
	branch string,
	userContent *Content,
	runConfig *RunConfig,
) *BaseInvocationContext {
	return &BaseInvocationContext{
		Context:      ctx,
		agent:        agent,
		artifacts:    artifacts,
		memory:       memory,
		session:      session,
		invocationID: invocationID,
		branch:       branch,
		userContent:  userContent,
		runConfig:    runConfig,
		ended:        false,
	}
}

func (c *BaseInvocationContext) Agent() Agent {
	return c.agent
}

func (c *BaseInvocationContext) Artifacts() Artifacts {
	return c.artifacts
}

func (c *BaseInvocationContext) Memory() Memory {
	return c.memory
}

func (c *BaseInvocationContext) Session() Session {
	return c.session
}

func (c *BaseInvocationContext) InvocationID() string {
	return c.invocationID
}

func (c *BaseInvocationContext) Branch() string {
	return c.branch
}

func (c *BaseInvocationContext) UserContent() *Content {
	return c.userContent
}

func (c *BaseInvocationContext) RunConfig() *RunConfig {
	return c.runConfig
}

func (c *BaseInvocationContext) EndInvocation() {
	c.ended = true
}

func (c *BaseInvocationContext) Ended() bool {
	return c.ended
}

func (c *BaseInvocationContext) WithContext(ctx context.Context) InvocationContext {
	return &BaseInvocationContext{
		Context:      ctx,
		agent:        c.agent,
		artifacts:    c.artifacts,
		memory:       c.memory,
		session:      c.session,
		invocationID: c.invocationID,
		branch:       c.branch,
		userContent:  c.userContent,
		runConfig:    c.runConfig,
		ended:        c.ended,
	}
}

// BaseCallbackContext provides a base implementation of CallbackContext.
type BaseCallbackContext struct {
	context.Context
	invocationContext InvocationContext
	actions           *EventActions
}

func (c *BaseCallbackContext) UserContent() *Content {
	return c.invocationContext.UserContent()
}

func (c *BaseCallbackContext) InvocationID() string {
	return c.invocationContext.InvocationID()
}

func (c *BaseCallbackContext) AgentName() string {
	return c.invocationContext.Agent().Name()
}

func (c *BaseCallbackContext) ReadonlyState() ReadonlyState {
	return c.invocationContext.Session().State()
}

func (c *BaseCallbackContext) UserID() string {
	return c.invocationContext.Session().UserID()
}

func (c *BaseCallbackContext) AppName() string {
	return c.invocationContext.Session().AppName()
}

func (c *BaseCallbackContext) SessionID() string {
	return c.invocationContext.Session().ID()
}

func (c *BaseCallbackContext) Branch() string {
	return c.invocationContext.Branch()
}

func (c *BaseCallbackContext) Artifacts() Artifacts {
	return c.invocationContext.Artifacts()
}

func (c *BaseCallbackContext) State() State {
	return c.invocationContext.Session().State()
}