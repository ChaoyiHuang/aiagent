package agent

import (
	"errors"
	"iter"
	"time"
)

// Session represents a series of interactions between a user and agents.
//
// When a user starts interacting with your agent, session holds everything
// related to that one specific chat thread.
type Session interface {
	// ID returns the unique identifier of the session.
	ID() string

	// AppName returns name of the application.
	AppName() string

	// UserID returns the id of the user.
	UserID() string

	// State returns the state of the session.
	State() State

	// Events returns the events of the session.
	// Events include user input, model response, function call/response, etc.
	Events() Events

	// LastUpdateTime returns the time of the last update.
	LastUpdateTime() time.Time
}

// State defines a standard interface for a key-value store.
// It provides basic methods for accessing, modifying, and iterating over
// key-value pairs.
type State interface {
	// Get retrieves the value associated with a given key.
	// Returns ErrStateKeyNotExist if the key does not exist.
	Get(key string) (any, error)

	// Set assigns the given value to the given key, overwriting existing value.
	Set(key string, value any) error

	// All returns an iterator that yields all key-value pairs.
	All() iter.Seq2[string, any]

	// Delete removes a key from the state.
	Delete(key string) error

	// Clear removes all keys from the state.
	Clear() error
}

// ReadonlyState provides read-only access to session state.
type ReadonlyState interface {
	// Get retrieves the value associated with a given key.
	// Returns ErrStateKeyNotExist if the key does not exist.
	Get(key string) (any, error)

	// All returns an iterator that yields all key-value pairs.
	All() iter.Seq2[string, any]
}

// Events defines a standard interface for an Event list.
type Events interface {
	// All returns an iterator that yields all events in order.
	All() iter.Seq[*Event]

	// Len returns the total number of events.
	Len() int

	// At returns the event at the specified index.
	At(i int) *Event
}

// ErrStateKeyNotExist is returned when a state key does not exist.
var ErrStateKeyNotExist = errors.New("state key does not exist")

// Key prefixes for defining session's state scopes.
const (
	// KeyPrefixApp is the prefix for app-level state keys.
	// They are shared across all users and sessions for that application.
	KeyPrefixApp string = "app:"

	// KeyPrefixTemp is the prefix for temporary state keys.
	// Such entries are specific to the current invocation and discarded after completion.
	KeyPrefixTemp string = "temp:"

	// KeyPrefixUser is the prefix for user-level state keys.
	// They are tied to the user_id, shared across all sessions for that user.
	KeyPrefixUser string = "user:"
)

// BaseSession provides a base implementation of Session.
type BaseSession struct {
	id            string
	appName       string
	userID        string
	state         State
	events        []*Event
	lastUpdateTime time.Time
}

// NewSession creates a new BaseSession.
func NewSession(id, appName, userID string, state State) *BaseSession {
	return &BaseSession{
		id:            id,
		appName:       appName,
		userID:        userID,
		state:         state,
		events:        make([]*Event, 0),
		lastUpdateTime: time.Now(),
	}
}

func (s *BaseSession) ID() string {
	return s.id
}

func (s *BaseSession) AppName() string {
	return s.appName
}

func (s *BaseSession) UserID() string {
	return s.userID
}

func (s *BaseSession) State() State {
	return s.state
}

func (s *BaseSession) Events() Events {
	return &BaseEvents{events: s.events}
}

func (s *BaseSession) LastUpdateTime() time.Time {
	return s.lastUpdateTime
}

// AppendEvent adds an event to the session.
func (s *BaseSession) AppendEvent(event *Event) {
	s.events = append(s.events, event)
	s.lastUpdateTime = time.Now()
}

// BaseEvents provides a base implementation of Events.
type BaseEvents struct {
	events []*Event
}

func (e *BaseEvents) All() iter.Seq[*Event] {
	return func(yield func(*Event) bool) {
		for _, event := range e.events {
			if !yield(event) {
				return
			}
		}
	}
}

func (e *BaseEvents) Len() int {
	return len(e.events)
}

func (e *BaseEvents) At(i int) *Event {
	if i < 0 || i >= len(e.events) {
		return nil
	}
	return e.events[i]
}

// MapState provides a map-based implementation of State.
type MapState struct {
	data map[string]any
}

// NewMapState creates a new MapState.
func NewMapState() *MapState {
	return &MapState{
		data: make(map[string]any),
	}
}

func (s *MapState) Get(key string) (any, error) {
	val, ok := s.data[key]
	if !ok {
		return nil, ErrStateKeyNotExist
	}
	return val, nil
}

func (s *MapState) Set(key string, value any) error {
	s.data[key] = value
	return nil
}

func (s *MapState) All() iter.Seq2[string, any] {
	return func(yield func(string, any) bool) {
		for k, v := range s.data {
			if !yield(k, v) {
				return
			}
		}
	}
}

func (s *MapState) Delete(key string) error {
	delete(s.data, key)
	return nil
}

func (s *MapState) Clear() error {
	s.data = make(map[string]any)
	return nil
}

// SessionService is a session storage service.
type SessionService interface {
	// Create creates a new session.
	Create(ctx *InvocationContext, req *CreateSessionRequest) (*CreateSessionResponse, error)

	// Get retrieves a session.
	Get(ctx *InvocationContext, req *GetSessionRequest) (*GetSessionResponse, error)

	// List lists sessions for a user.
	List(ctx *InvocationContext, req *ListSessionRequest) (*ListSessionResponse, error)

	// Delete deletes a session.
	Delete(ctx *InvocationContext, req *DeleteSessionRequest) error

	// AppendEvent appends an event to a session.
	AppendEvent(ctx *InvocationContext, session Session, event *Event) error
}

// CreateSessionRequest represents a request to create a session.
type CreateSessionRequest struct {
	AppName   string
	UserID    string
	SessionID string
	State     map[string]any
}

// CreateSessionResponse represents a response for session creation.
type CreateSessionResponse struct {
	Session Session
}

// GetSessionRequest represents a request to get a session.
type GetSessionRequest struct {
	AppName   string
	UserID    string
	SessionID string

	// NumRecentEvents returns at most NumRecentEvents most recent events.
	NumRecentEvents int

	// After returns events with timestamp >= the given time.
	After time.Time
}

// GetSessionResponse represents a response from Get.
type GetSessionResponse struct {
	Session Session
}

// ListSessionRequest represents a request to list sessions.
type ListSessionRequest struct {
	AppName string
	UserID  string
}

// ListSessionResponse represents a response from List.
type ListSessionResponse struct {
	Sessions []Session
}

// DeleteSessionRequest represents a request to delete a session.
type DeleteSessionRequest struct {
	AppName   string
	UserID    string
	SessionID string
}

// InMemorySessionService provides an in-memory implementation of SessionService.
type InMemorySessionService struct {
	appState  map[string]map[string]any
	userState map[string]map[string]map[string]any
	sessions  map[string]map[string]map[string]*BaseSession // app -> user -> session
}

// NewInMemorySessionService creates a new InMemorySessionService.
func NewInMemorySessionService() *InMemorySessionService {
	return &InMemorySessionService{
		appState:  make(map[string]map[string]any),
		userState: make(map[string]map[string]map[string]any),
		sessions:  make(map[string]map[string]map[string]*BaseSession),
	}
}

func (s *InMemorySessionService) Create(ctx *InvocationContext, req *CreateSessionRequest) (*CreateSessionResponse, error) {
	sessionID := req.SessionID
	if sessionID == "" {
		sessionID = generateSessionID()
	}

	state := NewMapState()
	for k, v := range req.State {
		state.Set(k, v)
	}

	session := NewSession(sessionID, req.AppName, req.UserID, state)

	// Initialize nested maps if needed
	if s.sessions[req.AppName] == nil {
		s.sessions[req.AppName] = make(map[string]map[string]*BaseSession)
	}
	if s.sessions[req.AppName][req.UserID] == nil {
		s.sessions[req.AppName][req.UserID] = make(map[string]*BaseSession)
	}

	s.sessions[req.AppName][req.UserID][sessionID] = session

	return &CreateSessionResponse{Session: session}, nil
}

func (s *InMemorySessionService) Get(ctx *InvocationContext, req *GetSessionRequest) (*GetSessionResponse, error) {
	if s.sessions[req.AppName] == nil ||
		s.sessions[req.AppName][req.UserID] == nil ||
		s.sessions[req.AppName][req.UserID][req.SessionID] == nil {
		return nil, errors.New("session not found")
	}

	return &GetSessionResponse{
		Session: s.sessions[req.AppName][req.UserID][req.SessionID],
	}, nil
}

func (s *InMemorySessionService) List(ctx *InvocationContext, req *ListSessionRequest) (*ListSessionResponse, error) {
	if s.sessions[req.AppName] == nil ||
		s.sessions[req.AppName][req.UserID] == nil {
		return &ListSessionResponse{Sessions: []Session{}}, nil
	}

	sessions := make([]Session, 0)
	for _, session := range s.sessions[req.AppName][req.UserID] {
		sessions = append(sessions, session)
	}

	return &ListSessionResponse{Sessions: sessions}, nil
}

func (s *InMemorySessionService) Delete(ctx *InvocationContext, req *DeleteSessionRequest) error {
	if s.sessions[req.AppName] == nil ||
		s.sessions[req.AppName][req.UserID] == nil {
		return nil
	}

	delete(s.sessions[req.AppName][req.UserID], req.SessionID)
	return nil
}

func (s *InMemorySessionService) AppendEvent(ctx *InvocationContext, session Session, event *Event) error {
	if bs, ok := session.(*BaseSession); ok {
		bs.AppendEvent(event)
	}
	return nil
}

func generateSessionID() string {
	return "session-" + time.Now().Format("20060102-150405")
}