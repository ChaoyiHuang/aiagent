// Package harness provides model harness for LLM provider integration.
package harness

import (
	"context"
	"fmt"
	"sync"
	"time"

	"aiagent/api/v1"
)

// ModelHarness manages connections to LLM providers.
type ModelHarness struct {
	spec    *v1.ModelHarnessSpec
	clients map[string]ModelClient
	mu      sync.RWMutex
}

// ModelClient interface for LLM provider clients.
type ModelClient interface {
	// Generate generates text completion
	Generate(ctx context.Context, req *GenerateRequest) (*GenerateResponse, error)
	// StreamGenerate generates streaming text completion
	StreamGenerate(ctx context.Context, req *GenerateRequest) (<-chan GenerateChunk, error)
	// CountTokens counts tokens in text
	CountTokens(ctx context.Context, text string) (int, error)
	// Close closes the client connection
	Close() error
}

// GenerateRequest represents a generation request.
type GenerateRequest struct {
	Model       string
	Prompt      string
	SystemPrompt string
	Temperature float64
	MaxTokens   int
	Stop        []string
	Tools       []ToolDefinition
	ToolChoice  string // "auto", "required", "none"
}

// ToolDefinition represents a tool definition for LLM.
type ToolDefinition struct {
	Name        string
	Description string
	Parameters  map[string]any
}

// GenerateResponse represents a generation response.
type GenerateResponse struct {
	Text         string
	TokensUsed   int
	FinishReason string
	ToolCalls    []ToolCall
}

// ToolCall represents a tool call from LLM.
type ToolCall struct {
	ID       string
	Name     string
	Args     map[string]any
}

// GenerateChunk represents a streaming generation chunk.
type GenerateChunk struct {
	Text         string
	ToolCall     *ToolCall
	FinishReason string
	Error        error
}

// NewModelHarness creates a new model harness.
func NewModelHarness(spec *v1.ModelHarnessSpec) *ModelHarness {
	harness := &ModelHarness{
		spec:    spec,
		clients: make(map[string]ModelClient),
	}

	// Initialize default client for the provider
	client := NewMockModelClient(spec.Provider)
	harness.clients[spec.Provider] = client

	// Initialize clients for each allowed model
	for _, model := range spec.Models {
		if model.Allowed {
			// All models from same provider use same client
			if _, exists := harness.clients[spec.Provider]; !exists {
				harness.clients[spec.Provider] = NewMockModelClient(spec.Provider)
			}
		}
	}

	return harness
}

// GetClient returns a model client for the given provider.
func (h *ModelHarness) GetClient(provider string) (ModelClient, error) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	client, exists := h.clients[provider]
	if !exists {
		return nil, fmt.Errorf("no client for provider '%s'", provider)
	}
	return client, nil
}

// GetDefaultModel returns the default model name.
func (h *ModelHarness) GetDefaultModel() string {
	if h.spec.DefaultModel != "" {
		return h.spec.DefaultModel
	}
	if len(h.spec.Models) > 0 {
		return h.spec.Models[0].Name
	}
	return ""
}

// GetProvider returns the provider name.
func (h *ModelHarness) GetProvider() string {
	return h.spec.Provider
}

// GetEndpoint returns the API endpoint.
func (h *ModelHarness) GetEndpoint() string {
	return h.spec.Endpoint
}

// GetSpec returns the model harness spec.
func (h *ModelHarness) GetSpec() *v1.ModelHarnessSpec {
	return h.spec
}

// GetAllowedModels returns all allowed models.
func (h *ModelHarness) GetAllowedModels() []string {
	h.mu.RLock()
	defer h.mu.RUnlock()

	models := []string{}
	for _, model := range h.spec.Models {
		if model.Allowed {
			models = append(models, model.Name)
		}
	}
	return models
}

// IsModelAllowed checks if a model is allowed.
func (h *ModelHarness) IsModelAllowed(model string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()

	for _, m := range h.spec.Models {
		if m.Name == model && m.Allowed {
			return true
		}
	}
	return false
}

// Generate generates text using the default model.
func (h *ModelHarness) Generate(ctx context.Context, req *GenerateRequest) (*GenerateResponse, error) {
	client, err := h.GetClient(h.spec.Provider)
	if err != nil {
		return nil, err
	}

	// Set default model if not specified
	if req.Model == "" {
		req.Model = h.GetDefaultModel()
	}

	// Check model is allowed
	if !h.IsModelAllowed(req.Model) {
		return nil, fmt.Errorf("model '%s' is not allowed", req.Model)
	}

	// Apply rate limiting (if configured)
	if h.spec.RateLimit != nil {
		// In real implementation, would check rate limits here
	}

	return client.Generate(ctx, req)
}

// StreamGenerate generates streaming text using the default model.
func (h *ModelHarness) StreamGenerate(ctx context.Context, req *GenerateRequest) (<-chan GenerateChunk, error) {
	client, err := h.GetClient(h.spec.Provider)
	if err != nil {
		return nil, err
	}

	if req.Model == "" {
		req.Model = h.GetDefaultModel()
	}

	if !h.IsModelAllowed(req.Model) {
		return nil, fmt.Errorf("model '%s' is not allowed", req.Model)
	}

	return client.StreamGenerate(ctx, req)
}

// CountTokens counts tokens in text.
func (h *ModelHarness) CountTokens(ctx context.Context, text string) (int, error) {
	client, err := h.GetClient(h.spec.Provider)
	if err != nil {
		return 0, err
	}
	return client.CountTokens(ctx, text)
}

// Close closes all client connections.
func (h *ModelHarness) Close() error {
	h.mu.Lock()
	defer h.mu.Unlock()

	var errs []error
	for provider, client := range h.clients {
		if err := client.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close client '%s': %w", provider, err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("errors closing clients: %v", errs)
	}
	return nil
}

// MockModelClient provides a mock implementation for testing.
type MockModelClient struct {
	provider string
}

// NewMockModelClient creates a mock model client.
func NewMockModelClient(provider string) *MockModelClient {
	return &MockModelClient{provider: provider}
}

func (c *MockModelClient) Generate(ctx context.Context, req *GenerateRequest) (*GenerateResponse, error) {
	// Mock response
	return &GenerateResponse{
		Text:         fmt.Sprintf("Mock response from %s for model %s", c.provider, req.Model),
		TokensUsed:   len(req.Prompt) + 100,
		FinishReason: "stop",
	}, nil
}

func (c *MockModelClient) StreamGenerate(ctx context.Context, req *GenerateRequest) (<-chan GenerateChunk, error) {
	ch := make(chan GenerateChunk, 10)

	go func() {
		defer close(ch)

		// Simulate streaming
		words := []string{"Mock", " response", " from", " ", c.provider, " for", " model", " ", req.Model}
		for _, word := range words {
			select {
			case ch <- GenerateChunk{Text: word}:
			case <-ctx.Done():
				ch <- GenerateChunk{Error: ctx.Err()}
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		ch <- GenerateChunk{FinishReason: "stop"}
	}()

	return ch, nil
}

func (c *MockModelClient) CountTokens(ctx context.Context, text string) (int, error) {
	// Simple approximation: 4 chars per token
	return len(text) / 4, nil
}

func (c *MockModelClient) Close() error {
	return nil
}