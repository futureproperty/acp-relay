package session_test

import (
	"fmt"
	"sync"
	"testing"

	"github.com/futureproperty/acp-relay/pkg/session"
)

func TestSessionCreate(t *testing.T) {
	m := session.New()
	if err := m.Create("s1"); err != nil {
		t.Fatalf("Create: %v", err)
	}
	s, err := m.Get("s1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if s.State != session.StateIdle {
		t.Errorf("expected idle, got %s", s.State)
	}
}

func TestSessionDuplicateCreate(t *testing.T) {
	m := session.New()
	m.Create("s1")
	if err := m.Create("s1"); err == nil {
		t.Error("expected error for duplicate create")
	}
}

func TestSessionNotFound(t *testing.T) {
	m := session.New()
	_, err := m.Get("nonexistent")
	if err != session.ErrSessionNotFound {
		t.Errorf("expected ErrSessionNotFound, got %v", err)
	}
}

func TestSessionLifecycle(t *testing.T) {
	m := session.New()
	m.Create("s1")

	steps := []session.State{
		session.StateStarting,
		session.StateConnected,
		session.StateWorking,
		session.StateClosed,
	}
	for _, to := range steps {
		if err := m.Transition("s1", to); err != nil {
			t.Fatalf("Transition to %s: %v", to, err)
		}
	}
}

func TestSessionInvalidTransition(t *testing.T) {
	m := session.New()
	m.Create("s1")
	err := m.Transition("s1", session.StateWorking)
	if err == nil {
		t.Error("expected ErrInvalidTransition for idle→working, got nil")
	}
}

func TestSessionClose(t *testing.T) {
	m := session.New()
	m.Create("s1")
	m.Create("s2")

	if err := m.Close("s1"); err != nil {
		t.Fatalf("Close: %v", err)
	}
	_, err := m.Get("s1")
	if err != session.ErrSessionNotFound {
		t.Errorf("expected session removed after close")
	}
	if _, err := m.Get("s2"); err != nil {
		t.Errorf("s2 should still exist: %v", err)
	}
}

func TestSessionList(t *testing.T) {
	m := session.New()
	m.Create("a")
	m.Create("b")
	m.Create("c")
	if got := len(m.List()); got != 3 {
		t.Errorf("expected 3 sessions, got %d", got)
	}
}

func TestSessionConcurrent(t *testing.T) {
	m := session.New()
	const n = 100
	var wg sync.WaitGroup

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := fmt.Sprintf("session-%d", i)
			_ = m.Create(id)
		}(i)
	}
	wg.Wait()

	ids := m.List()
	if len(ids) != n {
		t.Errorf("expected %d sessions, got %d", n, len(ids))
	}

	for _, id := range ids {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			_ = m.Transition(id, session.StateStarting)
		}(id)
	}
	wg.Wait()
}
