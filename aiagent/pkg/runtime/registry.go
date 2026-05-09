// Package runtime provides the runtime management for agent execution.
package runtime

import (
	"context"
	"fmt"
	"sync"

	"aiagent/pkg/handler"
)

// Registry manages registered handlers for different agent frameworks.
// It allows dynamic registration and retrieval of handlers by framework type.
type Registry struct {
	mu      sync.RWMutex
	handlers map[handler.HandlerType]handler.Handler
}

// NewRegistry creates a new handler registry.
func NewRegistry() *Registry {
	return &Registry{
		handlers: make(map[handler.HandlerType]handler.Handler),
	}
}

// Register adds a handler to the registry.
// Returns an error if a handler for the same type already exists.
func (r *Registry) Register(h handler.Handler) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	handlerType := h.Type()
	if _, exists := r.handlers[handlerType]; exists {
		return fmt.Errorf("handler for type '%s' already registered", handlerType)
	}

	r.handlers[handlerType] = h
	return nil
}

// Unregister removes a handler from the registry.
func (r *Registry) Unregister(handlerType handler.HandlerType) {
	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.handlers, handlerType)
}

// Get retrieves a handler by type.
// Returns nil if no handler is registered for the type.
func (r *Registry) Get(handlerType handler.HandlerType) handler.Handler {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return r.handlers[handlerType]
}

// GetByTypeString retrieves a handler by type string.
func (r *Registry) GetByTypeString(typeStr string) handler.Handler {
	return r.Get(handler.HandlerType(typeStr))
}

// MustGet retrieves a handler by type, panicking if not found.
func (r *Registry) MustGet(handlerType handler.HandlerType) handler.Handler {
	h := r.Get(handlerType)
	if h == nil {
		panic(fmt.Sprintf("no handler registered for type '%s'", handlerType))
	}
	return h
}

// List returns all registered handlers.
func (r *Registry) List() []handler.Handler {
	r.mu.RLock()
	defer r.mu.RUnlock()

	handlers := make([]handler.Handler, 0)
	for _, h := range r.handlers {
		handlers = append(handlers, h)
	}
	return handlers
}

// ListTypes returns all registered handler types.
func (r *Registry) ListTypes() []handler.HandlerType {
	r.mu.RLock()
	defer r.mu.RUnlock()

	types := make([]handler.HandlerType, 0)
	for t := range r.handlers {
		types = append(types, t)
	}
	return types
}

// Has checks if a handler is registered for a type.
func (r *Registry) Has(handlerType handler.HandlerType) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return r.handlers[handlerType] != nil
}

// Count returns the number of registered handlers.
func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return len(r.handlers)
}

// Clear removes all registered handlers.
func (r *Registry) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.handlers = make(map[handler.HandlerType]handler.Handler)
}

// DefaultRegistry is the global default registry.
var DefaultRegistry = NewRegistry()

// RegisterHandler registers a handler with the default registry.
func RegisterHandler(h handler.Handler) error {
	return DefaultRegistry.Register(h)
}

// GetHandler retrieves a handler from the default registry.
func GetHandler(handlerType handler.HandlerType) handler.Handler {
	return DefaultRegistry.Get(handlerType)
}

// MustGetHandler retrieves a handler from the default registry, panicking if not found.
func MustGetHandler(handlerType handler.HandlerType) handler.Handler {
	return DefaultRegistry.MustGet(handlerType)
}

// ListHandlers returns all handlers from the default registry.
func ListHandlers() []handler.Handler {
	return DefaultRegistry.List()
}

// HandlerFinder provides methods to find appropriate handlers.
type HandlerFinder struct {
	registry *Registry
}

// NewHandlerFinder creates a new HandlerFinder.
func NewHandlerFinder(registry *Registry) *HandlerFinder {
	return &HandlerFinder{registry: registry}
}

// FindForAgent finds a handler that can run the given agent spec.
func (f *HandlerFinder) FindForAgent(ctx context.Context, runtimeType string) (handler.Handler, error) {
	h := f.registry.GetByTypeString(runtimeType)
	if h == nil {
		return nil, fmt.Errorf("no handler found for runtime type '%s'", runtimeType)
	}
	return h, nil
}

// FindBest finds the best handler for given requirements.
// Currently returns the first matching handler.
func (f *HandlerFinder) FindBest(ctx context.Context, requirements HandlerRequirements) (handler.Handler, error) {
	handlers := f.registry.List()
	for _, h := range handlers {
		if f.matchesRequirements(h, requirements) {
			return h, nil
		}
	}
	return nil, fmt.Errorf("no handler matches requirements")
}

// matchesRequirements checks if a handler matches the given requirements.
func (f *HandlerFinder) matchesRequirements(h handler.Handler, req HandlerRequirements) bool {
	// Check framework type if specified
	if req.FrameworkType != "" && h.Type() != req.FrameworkType {
		return false
	}

	// Check multi-agent support
	if req.RequireMultiAgent && !h.SupportsMultiAgent() {
		return false
	}

	return true
}

// HandlerRequirements specifies requirements for finding a handler.
type HandlerRequirements struct {
	// FrameworkType required framework type.
	FrameworkType handler.HandlerType

	// RequireMultiAgent requires multi-agent support.
	RequireMultiAgent bool

	// Capabilities required capabilities.
	Capabilities []string
}