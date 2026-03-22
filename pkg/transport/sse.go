package transport

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

type Event struct {
	Type string
	Data any
}

type EventBroker struct {
	mu          sync.RWMutex
	subscribers map[string]chan Event
}

func NewEventBroker() *EventBroker {
	return &EventBroker{
		subscribers: make(map[string]chan Event),
	}
}

func (b *EventBroker) Subscribe(sessionID string) (<-chan Event, func()) {
	b.mu.Lock()
	defer b.mu.Unlock()

	ch := make(chan Event, 64)
	b.subscribers[sessionID] = ch

	return ch, func() {
		b.mu.Lock()
		defer b.mu.Unlock()

		current, ok := b.subscribers[sessionID]
		if !ok || current != ch {
			return
		}

		delete(b.subscribers, sessionID)
		close(ch)
	}
}

func (b *EventBroker) Publish(sessionID string, event Event) {
	b.mu.RLock()
	ch, ok := b.subscribers[sessionID]
	b.mu.RUnlock()
	if !ok {
		return
	}

	select {
	case ch <- event:
	default:
	}
}

type SSEWriter struct {
	w       http.ResponseWriter
	flusher http.Flusher
	mu      sync.Mutex
}

func NewSSEWriter(w http.ResponseWriter) (*SSEWriter, error) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil, fmt.Errorf("response writer does not support flushing")
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	return &SSEWriter{w: w, flusher: flusher}, nil
}

func (s *SSEWriter) WriteEvent(eventType string, data any) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	b, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	if _, err := fmt.Fprintf(s.w, "event: %s\ndata: %s\n\n", eventType, b); err != nil {
		return fmt.Errorf("write: %w", err)
	}

	s.flusher.Flush()
	return nil
}

func (s *SSEWriter) WriteComment(comment string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, err := fmt.Fprintf(s.w, ": %s\n\n", comment); err != nil {
		return err
	}

	s.flusher.Flush()
	return nil
}

func (b *EventBroker) ServeSSE(ctx context.Context, sessionID string, w http.ResponseWriter) error {
	sse, err := NewSSEWriter(w)
	if err != nil {
		return err
	}

	ch, unsub := b.Subscribe(sessionID)
	defer unsub()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case event, ok := <-ch:
			if !ok {
				return nil
			}
			if err := sse.WriteEvent(event.Type, event.Data); err != nil {
				return err
			}
		case <-ticker.C:
			_ = sse.WriteComment("keepalive")
		}
	}
}
