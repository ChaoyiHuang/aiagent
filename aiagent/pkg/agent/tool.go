package agent

import (
	"context"
)

// Tool defines the interface for a callable tool.
// A tool is a piece of code that performs a specific task.
type Tool interface {
	// Name returns the name of the tool.
	Name() string

	// Description returns a description of the tool.
	// This is used by LLM to determine whether to call the tool.
	Description() string

	// IsLongRunning indicates whether the tool is a long-running operation.
	// Long-running tools typically return a resource ID first and finish later.
	IsLongRunning() bool
}

// RunnableTool extends Tool with execution capability.
type RunnableTool interface {
	Tool

	// Declaration returns the function declaration for the tool.
	Declaration() *FunctionDeclaration

	// Run executes the tool with the given arguments.
	Run(ctx ToolContext, args any) (map[string]any, error)
}

// FunctionDeclaration describes a tool's interface for LLM.
type FunctionDeclaration struct {
	// Name of the function.
	Name string

	// Description of what the function does.
	Description string

	// Parameters schema (JSON Schema format).
	Parameters map[string]any

	// Return schema (JSON Schema format).
	Return map[string]any
}

// Toolset is an interface for a collection of tools.
// It allows grouping related tools together.
type Toolset interface {
	// Name returns the name of the toolset.
	Name() string

	// Tools returns the tools in the toolset.
	// The ReadonlyContext can be used to dynamically determine which tools to return.
	Tools(ctx ReadonlyContext) ([]Tool, error)
}

// ToolContext defines the context passed to a tool when executed.
type ToolContext interface {
	CallbackContext

	// FunctionCallID returns the ID of the function call that triggered this execution.
	FunctionCallID() string

	// Actions returns the EventActions for modifying agent state.
	Actions() *EventActions

	// SearchMemory performs a semantic search on agent memory.
	SearchMemory(ctx context.Context, query string) (*SearchResponse, error)

	// ToolConfirmation returns the confirmation status for this tool.
	ToolConfirmation() *ToolConfirmation

	// RequestConfirmation requests user approval before proceeding.
	RequestConfirmation(hint string, payload any) error
}

// BaseToolContext provides a base implementation of ToolContext.
type BaseToolContext struct {
	BaseCallbackContext
	functionCallID    string
	actions           *EventActions
	toolConfirmation  *ToolConfirmation
	memory            Memory
}

// NewToolContext creates a new BaseToolContext.
func NewToolContext(
	ctx context.Context,
	invCtx InvocationContext,
	functionCallID string,
) *BaseToolContext {
	return &BaseToolContext{
		BaseCallbackContext: BaseCallbackContext{
			Context:           ctx,
			invocationContext: invCtx,
			actions:           nil,
		},
		functionCallID:    functionCallID,
		actions:           nil,
		toolConfirmation:  nil,
		memory:            invCtx.Memory(),
	}
}

func (c *BaseToolContext) FunctionCallID() string {
	return c.functionCallID
}

func (c *BaseToolContext) Actions() *EventActions {
	if c.actions == nil {
		c.actions = &EventActions{
			StateDelta:    make(map[string]any),
			ArtifactDelta: make(map[string]int64),
		}
	}
	return c.actions
}

func (c *BaseToolContext) SearchMemory(ctx context.Context, query string) (*SearchResponse, error) {
	if c.memory == nil {
		return nil, nil
	}
	return c.memory.SearchMemory(ctx, query)
}

func (c *BaseToolContext) ToolConfirmation() *ToolConfirmation {
	return c.toolConfirmation
}

func (c *BaseToolContext) RequestConfirmation(hint string, payload any) error {
	c.Actions().SkipSummarization = true
	c.toolConfirmation = &ToolConfirmation{
		Hint:    hint,
		ToolArgs: payload.(map[string]any),
	}
	return nil
}

// ToolPredicate is a function that decides whether a tool should be exposed to LLM.
type ToolPredicate func(ctx ReadonlyContext, tool Tool) bool

// AllowedToolsPredicate returns a predicate that allows only the given tools.
func AllowedToolsPredicate(allowedTools []string) ToolPredicate {
	allowed := make(map[string]bool)
	for _, t := range allowedTools {
		allowed[t] = true
	}

	return func(ctx ReadonlyContext, tool Tool) bool {
		return allowed[tool.Name()]
	}
}

// FilterToolset returns a toolset that filters tools using a predicate.
func FilterToolset(toolset Toolset, predicate ToolPredicate) Toolset {
	return &filteredToolset{
		toolset:   toolset,
		predicate: predicate,
	}
}

// filteredToolset implements Toolset with filtering.
type filteredToolset struct {
	toolset   Toolset
	predicate ToolPredicate
}

func (f *filteredToolset) Name() string {
	return f.toolset.Name()
}

func (f *filteredToolset) Tools(ctx ReadonlyContext) ([]Tool, error) {
	tools, err := f.toolset.Tools(ctx)
	if err != nil {
		return nil, err
	}

	filtered := make([]Tool, 0)
	for _, tool := range tools {
		if f.predicate(ctx, tool) {
			filtered = append(filtered, tool)
		}
	}
	return filtered, nil
}

// BeforeToolCallback is called before a tool's Run method.
type BeforeToolCallback func(ctx ToolContext, tool Tool, args map[string]any) (map[string]any, error)

// AfterToolCallback is called after a tool's Run method completes.
type AfterToolCallback func(ctx ToolContext, tool Tool, args, result map[string]any, err error) (map[string]any, error)

// OnToolErrorCallback is called when a tool execution fails.
type OnToolErrorCallback func(ctx ToolContext, tool Tool, args map[string]any, err error) (map[string]any, error)

// ToolBuilder provides a builder pattern for creating simple tools.
type ToolBuilder struct {
	name          string
	description   string
	isLongRunning bool
	declaration   *FunctionDeclaration
	runFunc       func(ctx ToolContext, args any) (map[string]any, error)
}

// NewToolBuilder creates a new ToolBuilder.
func NewToolBuilder(name string) *ToolBuilder {
	return &ToolBuilder{
		name: name,
		declaration: &FunctionDeclaration{
			Name: name,
		},
	}
}

// WithDescription sets the tool description.
func (b *ToolBuilder) WithDescription(desc string) *ToolBuilder {
	b.description = desc
	b.declaration.Description = desc
	return b
}

// WithLongRunning marks the tool as long-running.
func (b *ToolBuilder) WithLongRunning() *ToolBuilder {
	b.isLongRunning = true
	return b
}

// WithParameters sets the parameters schema.
func (b *ToolBuilder) WithParameters(params map[string]any) *ToolBuilder {
	b.declaration.Parameters = params
	return b
}

// WithReturn sets the return schema.
func (b *ToolBuilder) WithReturn(ret map[string]any) *ToolBuilder {
	b.declaration.Return = ret
	return b
}

// WithRun sets the run function.
func (b *ToolBuilder) WithRun(fn func(ctx ToolContext, args any) (map[string]any, error)) *ToolBuilder {
	b.runFunc = fn
	return b
}

// Build creates a RunnableTool from the builder.
func (b *ToolBuilder) Build() RunnableTool {
	return &simpleTool{
		name:          b.name,
		description:   b.description,
		isLongRunning: b.isLongRunning,
		declaration:   b.declaration,
		runFunc:       b.runFunc,
	}
}

// simpleTool implements RunnableTool.
type simpleTool struct {
	name          string
	description   string
	isLongRunning bool
	declaration   *FunctionDeclaration
	runFunc       func(ctx ToolContext, args any) (map[string]any, error)
}

func (t *simpleTool) Name() string {
	return t.name
}

func (t *simpleTool) Description() string {
	return t.description
}

func (t *simpleTool) IsLongRunning() bool {
	return t.isLongRunning
}

func (t *simpleTool) Declaration() *FunctionDeclaration {
	return t.declaration
}

func (t *simpleTool) Run(ctx ToolContext, args any) (map[string]any, error) {
	if t.runFunc == nil {
		return nil, nil
	}
	return t.runFunc(ctx, args)
}