package provider_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/futureproperty/acp-relay/pkg/provider"
)

var mockAgentBin string

func TestMain(m *testing.M) {
	_, filename, _, _ := runtime.Caller(0)
	repoRoot := filepath.Join(filepath.Dir(filename), "..", "..")
	mockSrc := filepath.Join(repoRoot, "testdata", "mock_agent")

	dir, err := os.MkdirTemp("", "mock-agent-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "create temp dir: %v\n", err)
		os.Exit(1)
	}

	bin := filepath.Join(dir, "mock-agent")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}

	out, err := exec.Command("go", "build", "-o", bin, mockSrc).CombinedOutput()
	if err != nil {
		fmt.Fprintf(os.Stderr, "build mock agent: %v\n%s\n", err, out)
		os.Exit(1)
	}
	mockAgentBin = bin

	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

func TestLocalProviderStdinStdout(t *testing.T) {
	p := &provider.LocalProvider{}

	proc, err := p.Start(context.Background(), provider.ExecOptions{Command: []string{mockAgentBin}})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() {
		proc.Cancel()
		_ = proc.Stdin.Close()
	}()

	req := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion":    1,
			"clientCapabilities": map[string]any{},
		},
	}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	data = append(data, '\n')
	if _, err := proc.Stdin.Write(data); err != nil {
		t.Fatalf("write stdin: %v", err)
	}

	scanner := bufio.NewScanner(proc.Stdout)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			t.Fatalf("scan stdout: %v", err)
		}
		t.Fatal("no response from mock agent")
	}
	line := scanner.Text()

	var resp map[string]any
	if err := json.Unmarshal([]byte(line), &resp); err != nil {
		t.Fatalf("parse response: %v", err)
	}

	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected result, got: %v", resp)
	}
	if result["agentName"] != "mock-agent" {
		t.Fatalf("expected agentName=mock-agent, got %v", result["agentName"])
	}
}

func TestLocalProviderContextCancel(t *testing.T) {
	p := &provider.LocalProvider{}
	ctx, cancel := context.WithCancel(context.Background())

	cmd := []string{"sleep", "3600"}
	if runtime.GOOS == "windows" {
		cmd = []string{"cmd", "/c", "timeout", "/t", "3600", "/nobreak"}
	}

	proc, err := p.Start(ctx, provider.ExecOptions{Command: cmd})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	cancel()

	err = proc.Wait()
	if err == nil {
		t.Fatal("expected error after cancel, got nil")
	}
}

func TestLocalProviderExitDetection(t *testing.T) {
	p := &provider.LocalProvider{}

	cmd := []string{"sh", "-c", "exit 0"}
	if runtime.GOOS == "windows" {
		cmd = []string{"cmd", "/c", "exit", "0"}
	}

	proc, err := p.Start(context.Background(), provider.ExecOptions{Command: cmd})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer proc.Cancel()

	if err := proc.Wait(); err != nil {
		t.Fatalf("Wait: %v", err)
	}
}

func TestLocalProviderEmptyCommand(t *testing.T) {
	p := &provider.LocalProvider{}

	_, err := p.Start(context.Background(), provider.ExecOptions{})
	if err == nil {
		t.Fatal("expected error for empty command")
	}
}
