// Package harness provides memory harness for state storage.
package harness

import (
	"context"
	"fmt"
	"sync"
	"time"

	"aiagent/api/v1"
)

// MemoryHarness manages memory storage backends.
type MemoryHarness struct {
	spec   *v1.MemoryHarnessSpec
	store  MemoryStore
	mu     sync.RWMutex
}

// MemoryStore interface for memory storage backends.
type MemoryStore interface {
	// Store stores a value with key
	Store(ctx context.Context, key string, value []byte, ttl int32) error
	// Retrieve retrieves a value by key
	Retrieve(ctx context.Context, key string) ([]byte, error)
	// Delete deletes a value by key
	Delete(ctx context.Context, key string) error
	// Exists checks if a key exists
	Exists(ctx context.Context, key string) (bool, error)
	// List lists keys matching prefix
	List(ctx context.Context, prefix string) ([]string, error)
	// Clear clears all keys
	Clear(ctx context.Context) error
	// Close closes the store
	Close() error
}

// NewMemoryHarness creates a new memory harness.
func NewMemoryHarness(spec *v1.MemoryHarnessSpec) *MemoryHarness {
	harness := &MemoryHarness{
		spec: spec,
	}

	// Initialize backend based on type
	switch spec.Type {
	case "inmemory":
		harness.store = NewInMemoryStore()
	case "redis":
		// In real implementation, would connect to Redis
		harness.store = NewInMemoryStore() // Mock for testing
	case "file":
		harness.store = NewInMemoryStore() // Mock for testing
	default:
		harness.store = NewInMemoryStore()
	}

	return harness
}

// GetStore returns the memory store.
func (h *MemoryHarness) GetStore() MemoryStore {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.store
}

// GetSpec returns the memory harness spec.
func (h *MemoryHarness) GetSpec() *v1.MemoryHarnessSpec {
	return h.spec
}

// GetType returns the memory type.
func (h *MemoryHarness) GetType() string {
	return h.spec.Type
}

// GetEndpoint returns the storage endpoint.
func (h *MemoryHarness) GetEndpoint() string {
	return h.spec.Endpoint
}

// GetTTL returns the TTL in seconds.
func (h *MemoryHarness) GetTTL() int64 {
	return int64(h.spec.TTL)
}

// IsPersistenceEnabled returns if persistence is enabled.
func (h *MemoryHarness) IsPersistenceEnabled() bool {
	return h.spec.PersistenceEnabled
}

// Store stores a value.
func (h *MemoryHarness) Store(ctx context.Context, key string, value []byte) error {
	ttl := h.spec.TTL
	if ttl == 0 {
		ttl = 3600 // Default 1 hour
	}
	return h.store.Store(ctx, key, value, ttl)
}

// Retrieve retrieves a value.
func (h *MemoryHarness) Retrieve(ctx context.Context, key string) ([]byte, error) {
	return h.store.Retrieve(ctx, key)
}

// Delete deletes a value.
func (h *MemoryHarness) Delete(ctx context.Context, key string) error {
	return h.store.Delete(ctx, key)
}

// Exists checks if a key exists.
func (h *MemoryHarness) Exists(ctx context.Context, key string) (bool, error) {
	return h.store.Exists(ctx, key)
}

// List lists keys matching prefix.
func (h *MemoryHarness) List(ctx context.Context, prefix string) ([]string, error) {
	return h.store.List(ctx, prefix)
}

// Clear clears all keys.
func (h *MemoryHarness) Clear(ctx context.Context) error {
	return h.store.Clear(ctx)
}

// Shutdown shuts down the memory harness.
func (h *MemoryHarness) Shutdown(ctx context.Context) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	return h.store.Close()
}

// InMemoryStore provides in-memory storage backend.
type InMemoryStore struct {
	data map[string]*memoryEntry
	mu   sync.RWMutex
}

type memoryEntry struct {
	value     []byte
	expiresAt time.Time
}

// NewInMemoryStore creates an in-memory store.
func NewInMemoryStore() *InMemoryStore {
	return &InMemoryStore{
		data: make(map[string]*memoryEntry),
	}
}

func (s *InMemoryStore) Store(ctx context.Context, key string, value []byte, ttl int32) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var expiresAt time.Time
	if ttl > 0 {
		expiresAt = time.Now().Add(time.Duration(ttl) * time.Second)
	}

	s.data[key] = &memoryEntry{
		value:     value,
		expiresAt: expiresAt,
	}
	return nil
}

func (s *InMemoryStore) Retrieve(ctx context.Context, key string) ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entry, exists := s.data[key]
	if !exists {
		return nil, fmt.Errorf("key '%s' not found", key)
	}

	// Check expiration
	if !entry.expiresAt.IsZero() && time.Now().After(entry.expiresAt) {
		return nil, fmt.Errorf("key '%s' expired", key)
	}

	return entry.value, nil
}

func (s *InMemoryStore) Delete(ctx context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.data, key)
	return nil
}

func (s *InMemoryStore) Exists(ctx context.Context, key string) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entry, exists := s.data[key]
	if !exists {
		return false, nil
	}

	// Check expiration
	if !entry.expiresAt.IsZero() && time.Now().After(entry.expiresAt) {
		return false, nil
	}

	return true, nil
}

func (s *InMemoryStore) List(ctx context.Context, prefix string) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	keys := []string{}
	now := time.Now()

	for key, entry := range s.data {
		// Check prefix match
		if len(prefix) > 0 && !hasPrefix(key, prefix) {
			continue
		}
		// Check expiration
		if !entry.expiresAt.IsZero() && now.After(entry.expiresAt) {
			continue
		}
		keys = append(keys, key)
	}
	return keys, nil
}

func (s *InMemoryStore) Clear(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.data = make(map[string]*memoryEntry)
	return nil
}

func (s *InMemoryStore) Close() error {
	s.Clear(context.Background())
	return nil
}

func hasPrefix(s, prefix string) bool {
	if len(prefix) > len(s) {
		return false
	}
	for i := 0; i < len(prefix); i++ {
		if s[i] != prefix[i] {
			return false
		}
	}
	return true
}

// StateMemoryKey generates a state memory key.
func StateMemoryKey(agentID, sessionID, stateKey string) string {
	return fmt.Sprintf("state:%s:%s:%s", agentID, sessionID, stateKey)
}

// SessionMemoryKey generates a session memory key.
func SessionMemoryKey(agentID, sessionID string) string {
	return fmt.Sprintf("session:%s:%s", agentID, sessionID)
}

// ConversationMemoryKey generates a conversation memory key.
func ConversationMemoryKey(agentID, sessionID string) string {
	return fmt.Sprintf("conversation:%s:%s", agentID, sessionID)
}