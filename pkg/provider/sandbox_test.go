package provider_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/yourorg/acp-remote/pkg/provider"
)

// TestSandboxProviderConfig verifies SandboxProvider can be created.
func TestSandboxProviderConfig(t *testing.T) {
	p := provider.NewSandboxProvider(provider.SandboxOptions{
		Endpoint:    "http://sandbox:44772",
		AccessToken: "test-token",
	})
	if p == nil {
		t.Fatal("expected non-nil SandboxProvider")
	}
}

// TestSandboxProviderImplementsInterface is a compile-time check.
func TestSandboxProviderImplementsInterface(t *testing.T) {
	var _ provider.ExecProvider = provider.NewSandboxProvider(provider.SandboxOptions{})
	t.Log("SandboxProvider implements ExecProvider")
}

// TestSandboxProviderEmptyCommand verifies that empty command returns error.
func TestSandboxProviderEmptyCommand(t *testing.T) {
	p := provider.NewSandboxProvider(provider.SandboxOptions{Endpoint: "http://sandbox:44772"})
	_, err := p.Start(context.Background(), provider.ExecOptions{})
	if err == nil {
		t.Error("expected error for empty command")
	}
}

// TestSandboxProviderHTTPExec verifies the HTTP call structure.
func TestSandboxProviderHTTPExec(t *testing.T) {
	var capturedMethod, capturedPath string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		capturedPath = r.URL.Path
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		// Send SSE output
		w.Write([]byte("data: {\"output\":\"hello\"}\n\n"))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}))
	defer ts.Close()

	p := provider.NewSandboxProvider(provider.SandboxOptions{
		Endpoint:    ts.URL,
		AccessToken: "test-token",
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	proc, err := p.Start(ctx, provider.ExecOptions{Command: []string{"/bin/agent"}})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer proc.Cancel()

	if capturedMethod != http.MethodPost {
		t.Errorf("expected POST, got %s", capturedMethod)
	}
	if capturedPath != "/command" {
		t.Errorf("expected /command, got %s", capturedPath)
	}

	t.Logf("Method: %s, Path: %s", capturedMethod, capturedPath)
}
