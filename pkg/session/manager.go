package session

import (
	"errors"
	"fmt"
	"sync"

	"github.com/yourorg/acp-remote/pkg/provider"
)

// State represents the session lifecycle state.
type State string

const (
	StateIdle      State = "idle"
	StateStarting  State = "starting"
	StateConnected State = "connected"
	StateWorking   State = "working"
	StateError     State = "error"
	StateClosed    State = "closed"
)

// ErrInvalidTransition is returned when a state transition is not allowed.
var ErrInvalidTransition = errors.New("invalid state transition")

// ErrSessionNotFound is returned when a session ID does not exist.
var ErrSessionNotFound = errors.New("session not found")

// validTransitions defines allowed state transitions.
var validTransitions = map[State]map[State]struct{}{
	StateIdle: {
		StateStarting: {},
	},
	StateStarting: {
		StateConnected: {},
		StateError:     {},
	},
	StateConnected: {
		StateWorking: {},
		StateError:   {},
		StateClosed:  {},
	},
	StateWorking: {
		StateConnected: {},
		StateError:     {},
		StateClosed:    {},
	},
	StateError: {
		StateClosed: {},
	},
	StateClosed: {},
}

// Session holds the state and associated process for a single ACP session.
type Session struct {
	ID      string
	State   State
	Process *provider.Process
}

// Manager manages ACP session lifecycle.
type Manager struct {
	mu       sync.RWMutex
	sessions map[string]*Session
}

// New creates a new session Manager.
func New() *Manager {
	return &Manager{
		sessions: make(map[string]*Session),
	}
}

// Create creates a new session with the given ID in the idle state.
// Returns an error if a session with the same ID already exists.
func (m *Manager) Create(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.sessions[id]; exists {
		return fmt.Errorf("session %q already exists", id)
	}
	m.sessions[id] = &Session{ID: id, State: StateIdle}
	return nil
}

// Get returns the session for the given ID, or ErrSessionNotFound.
func (m *Manager) Get(id string) (*Session, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.sessions[id]
	if !ok {
		return nil, ErrSessionNotFound
	}
	return s, nil
}

// Transition moves the session to the new state.
// Returns ErrInvalidTransition if the transition is not allowed.
func (m *Manager) Transition(id string, to State) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	if !ok {
		return ErrSessionNotFound
	}
	allowed, ok := validTransitions[s.State]
	if !ok {
		return fmt.Errorf("%w: %s has no transitions", ErrInvalidTransition, s.State)
	}
	if _, ok := allowed[to]; !ok {
		return fmt.Errorf("%w: %s → %s", ErrInvalidTransition, s.State, to)
	}
	s.State = to
	return nil
}

// BindProcess associates a provider.Process with the session.
func (m *Manager) BindProcess(id string, proc *provider.Process) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	if !ok {
		return ErrSessionNotFound
	}
	s.Process = proc
	return nil
}

// Close terminates the session: cancels the process and marks it as closed.
func (m *Manager) Close(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	if !ok {
		return ErrSessionNotFound
	}
	if s.Process != nil {
		s.Process.Cancel()
	}
	s.State = StateClosed
	delete(m.sessions, id)
	return nil
}

// List returns all active session IDs.
func (m *Manager) List() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	ids := make([]string, 0, len(m.sessions))
	for id := range m.sessions {
		ids = append(ids, id)
	}
	return ids
}
