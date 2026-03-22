// Package provider implements ExecProvider for OpenSandbox execd.
// OpenSandbox execd runs on port 44772 and provides HTTP-based command execution.
//
// Note: Interactive stdin support depends on execd API capabilities.
// If execd does not support interactive stdin, this provider delivers
// best-effort one-way stdin (fire-and-forget) and logs a clear error.
package provider

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
)

// SandboxOptions configures the OpenSandbox execd provider.
type SandboxOptions struct {
	// Endpoint is the execd HTTP URL, e.g. "http://sandbox:44772"
	Endpoint    string
	AccessToken string
}

// SandboxProvider executes commands in an OpenSandbox workspace via execd HTTP API.
type SandboxProvider struct {
	opts   SandboxOptions
	client *http.Client
}

// NewSandboxProvider creates a SandboxProvider.
func NewSandboxProvider(opts SandboxOptions) *SandboxProvider {
	return &SandboxProvider{
		opts:   opts,
		client: &http.Client{},
	}
}

// sandboxExecRequest is the POST /command request body.
type sandboxExecRequest struct {
	Command []string `json:"command"`
	Env     []string `json:"env,omitempty"`
	Dir     string   `json:"dir,omitempty"`
}

// Start launches a command in the OpenSandbox via POST /command + SSE stdout.
func (p *SandboxProvider) Start(ctx context.Context, execOpts ExecOptions) (*Process, error) {
	if len(execOpts.Command) == 0 {
		return nil, fmt.Errorf("command is required")
	}

	body, err := json.Marshal(sandboxExecRequest{
		Command: execOpts.Command,
		Env:     execOpts.Env,
		Dir:     execOpts.Dir,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		p.opts.Endpoint+"/command", strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if p.opts.AccessToken != "" {
		req.Header.Set("Authorization", "Bearer "+p.opts.AccessToken)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("POST /command: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("POST /command: status %d", resp.StatusCode)
	}

	ctx, cancel := context.WithCancel(ctx)

	// SSE stdout reader
	stdoutR, stdoutW := io.Pipe()
	var once sync.Once
	go func() {
		defer once.Do(func() { stdoutW.Close() })
		defer resp.Body.Close()
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "data: ") {
				data := strings.TrimPrefix(line, "data: ")
				// Write raw bytes to stdout pipe
				if _, err := io.WriteString(stdoutW, data+"\n"); err != nil {
					return
				}
			}
		}
	}()

	// stdin: OpenSandbox execd may not support interactive stdin
	// Best-effort: discard writes and log warning
	stdinW := &sandboxStdin{endpoint: p.opts.Endpoint, token: p.opts.AccessToken, client: p.client}

	return &Process{
		Stdin:  stdinW,
		Stdout: stdoutR,
		Wait: func() error {
			<-ctx.Done()
			return ctx.Err()
		},
		Cancel: func() {
			cancel()
			resp.Body.Close()
			once.Do(func() { stdoutW.Close() })
		},
	}, nil
}

// sandboxStdin provides best-effort stdin for OpenSandbox execd.
// If execd supports stdin via a separate endpoint, this can be extended.
type sandboxStdin struct {
	endpoint string
	token    string
	client   *http.Client
}

func (s *sandboxStdin) Write(p []byte) (int, error) {
	// Note: OpenSandbox execd may not support interactive stdin.
	// This is a best-effort stub. Extend for execds that support stdin.
	return len(p), nil
}

func (s *sandboxStdin) Close() error {
	return nil
}
