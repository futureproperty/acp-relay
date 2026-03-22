package transport_test

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/futureproperty/acp-relay/pkg/transport"
)

type mockMsgHandler struct {
	response []byte
	err      error
}

func (m *mockMsgHandler) ServeMessage(req []byte) ([]byte, error) {
	if m.err != nil {
		return nil, m.err
	}
	if m.response != nil {
		return m.response, nil
	}
	return req, nil
}

type mockPermHandler struct {
	mu       sync.Mutex
	approved []string
	denied   []string
}

func (m *mockPermHandler) Approve(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.approved = append(m.approved, id)
}

func (m *mockPermHandler) Deny(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.denied = append(m.denied, id)
}

func (m *mockPermHandler) approvedCalls() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.approved...)
}

func (m *mockPermHandler) deniedCalls() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.denied...)
}

func TestHTTPMessage(t *testing.T) {
	broker := transport.NewEventBroker()
	resp := []byte(`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":1}}`)
	handler := transport.NewHandler(broker, &mockMsgHandler{response: resp}, nil)

	mux := http.NewServeMux()
	handler.Register(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	body := bytes.NewBufferString(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
	res, err := http.Post(ts.URL+"/acp/message", "application/json", body)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.StatusCode)
	}
	if ct := res.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("expected application/json content-type, got %s", ct)
	}
}

func TestSSEEvents(t *testing.T) {
	broker := transport.NewEventBroker()
	handler := transport.NewHandler(broker, &mockMsgHandler{}, nil)

	mux := http.NewServeMux()
	handler.Register(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	linesCh := make(chan []string, 1)
	errCh := make(chan error, 1)

	go func() {
		res, err := http.Get(ts.URL + "/acp/events?sessionId=sess1")
		if err != nil {
			errCh <- err
			return
		}
		defer res.Body.Close()

		if res.StatusCode != http.StatusOK {
			errCh <- fmt.Errorf("status = %d", res.StatusCode)
			return
		}
		if ct := res.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
			errCh <- fmt.Errorf("content-type = %s", ct)
			return
		}

		scanner := bufio.NewScanner(res.Body)
		lines := make([]string, 0, 2)
		for scanner.Scan() {
			line := scanner.Text()
			if line == "" {
				continue
			}
			lines = append(lines, line)
			if len(lines) == 2 {
				linesCh <- lines
				return
			}
		}

		if err := scanner.Err(); err != nil {
			errCh <- err
			return
		}
		errCh <- errors.New("stream ended before event received")
	}()

	deadline := time.Now().Add(2 * time.Second)
	for {
		if time.Now().After(deadline) {
			t.Fatal("timeout waiting for subscriber registration")
		}
		broker.Publish("sess1", transport.Event{
			Type: "session_update",
			Data: map[string]string{"content": "hello"},
		})

		select {
		case lines := <-linesCh:
			if got, want := lines[0], "event: session_update"; got != want {
				t.Fatalf("first line = %q, want %q", got, want)
			}
			if got, want := lines[1], `data: {"content":"hello"}`; got != want {
				t.Fatalf("second line = %q, want %q", got, want)
			}
			return
		case err := <-errCh:
			t.Fatalf("SSE request failed: %v", err)
		case <-time.After(20 * time.Millisecond):
		}
	}
}

func TestSSEDisconnect(t *testing.T) {
	broker := transport.NewEventBroker()
	handler := transport.NewHandler(broker, &mockMsgHandler{}, nil)

	mux := http.NewServeMux()
	handler.Register(mux)
	server := httptest.NewUnstartedServer(mux)
	server.EnableHTTP2 = false
	server.Start()
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/acp/events?sessionId=sess2", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		res, err := server.Client().Do(req)
		if err == nil && res != nil {
			defer res.Body.Close()
			_, err = bufio.NewReader(res.Body).ReadString('\n')
		}
		done <- err
	}()

	// ALLOWED: test setup sync
	time.Sleep(30 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout: SSE handler didn't clean up")
	}
}

func TestApprove(t *testing.T) {
	broker := transport.NewEventBroker()
	permH := &mockPermHandler{}
	handler := transport.NewHandler(broker, &mockMsgHandler{}, permH)

	mux := http.NewServeMux()
	handler.Register(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	res, err := http.Post(ts.URL+"/acp/sessions/sess1/approve", "application/json", nil)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.StatusCode)
	}

	approved := permH.approvedCalls()
	if len(approved) != 1 || approved[0] != "sess1" {
		t.Fatalf("expected sess1 approved, got %v", approved)
	}
}

func TestDeny(t *testing.T) {
	broker := transport.NewEventBroker()
	permH := &mockPermHandler{}
	handler := transport.NewHandler(broker, &mockMsgHandler{}, permH)

	mux := http.NewServeMux()
	handler.Register(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	res, err := http.Post(ts.URL+"/acp/sessions/sess1/deny", "application/json", nil)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.StatusCode)
	}

	denied := permH.deniedCalls()
	if len(denied) != 1 || denied[0] != "sess1" {
		t.Fatalf("expected sess1 denied, got %v", denied)
	}
}
