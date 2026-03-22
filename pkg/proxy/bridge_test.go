package proxy_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	acp "github.com/coder/acp-go-sdk"

	"github.com/futureproperty/acp-relay/pkg/provider"
	"github.com/futureproperty/acp-relay/pkg/proxy"
	"github.com/futureproperty/acp-relay/pkg/session"
)

var mockAgentBin string

func TestMain(m *testing.M) {
	_, filename, _, _ := runtime.Caller(0)
	repoRoot := filepath.Join(filepath.Dir(filename), "..", "..")
	mockSrc := filepath.Join(repoRoot, "testdata", "mock_agent")

	dir, err := os.MkdirTemp("", "proxy-mock-agent-*")
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

func TestBridgeEndToEnd(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	sm := session.New()
	bridge := proxy.New(&provider.LocalProvider{}, sm, proxy.Config{Command: []string{mockAgentBin}})

	updates := make(chan acp.SessionNotification, 4)
	bridge.SetSessionUpdateCallback(func(n acp.SessionNotification) error {
		updates <- n
		return nil
	})

	initResp, err := bridge.Initialize(ctx, acp.InitializeRequest{ProtocolVersion: acp.ProtocolVersionNumber})
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if initResp.ProtocolVersion != acp.ProtocolVersionNumber {
		t.Fatalf("protocol version = %d, want %d", initResp.ProtocolVersion, acp.ProtocolVersionNumber)
	}
	if initResp.AgentInfo == nil || initResp.AgentInfo.Name != "acp-relay" {
		t.Fatalf("unexpected agent info: %#v", initResp.AgentInfo)
	}

	sessResp, err := bridge.NewSession(ctx, acp.NewSessionRequest{Cwd: t.TempDir(), McpServers: []acp.McpServer{}})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if sessResp.SessionId != acp.SessionId("mock-session-001") {
		t.Fatalf("session id = %q, want %q", sessResp.SessionId, acp.SessionId("mock-session-001"))
	}

	sess, err := sm.Get(string(sessResp.SessionId))
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if sess.State != session.StateConnected {
		t.Fatalf("state = %s, want %s", sess.State, session.StateConnected)
	}

	if _, err := bridge.Prompt(ctx, acp.PromptRequest{SessionId: sessResp.SessionId, Prompt: []acp.ContentBlock{acp.TextBlock("hello")}}); err != nil {
		t.Fatalf("Prompt: %v", err)
	}

	select {
	case <-ctx.Done():
		t.Fatal("timed out waiting for session update")
	case <-updates:
	}

	if err := bridge.Cancel(ctx, acp.CancelNotification{SessionId: sessResp.SessionId}); err != nil {
		t.Fatalf("Cancel: %v", err)
	}

	if err := waitForCondition(ctx, func() error {
		_, err := sm.Get(string(sessResp.SessionId))
		if err == session.ErrSessionNotFound {
			return nil
		}
		if err != nil {
			return err
		}
		return errors.New("session still present")
	}); err != nil {
		t.Fatal(err)
	}
}

func TestBridgeForwardsCallbacksAndMethods(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	remote := newRemoteTestAgent()
	bridge := proxy.New(&inMemoryProvider{agent: remote}, session.New(), proxy.Config{Command: []string{"in-memory-agent"}})

	updates := make(chan acp.SessionNotification, 8)
	permissions := make(chan acp.RequestPermissionRequest, 2)
	bridge.SetSessionUpdateCallback(func(n acp.SessionNotification) error {
		updates <- n
		return nil
	})
	bridge.SetPermissionCallback(func(ctx context.Context, params acp.RequestPermissionRequest) (acp.RequestPermissionResponse, error) {
		permissions <- params
		return acp.RequestPermissionResponse{
			Outcome: acp.RequestPermissionOutcome{
				Selected: &acp.RequestPermissionOutcomeSelected{OptionId: params.Options[0].OptionId},
			},
		}, nil
	})

	if _, err := bridge.Initialize(ctx, acp.InitializeRequest{ProtocolVersion: acp.ProtocolVersionNumber}); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	sessResp, err := bridge.NewSession(ctx, acp.NewSessionRequest{Cwd: t.TempDir(), McpServers: []acp.McpServer{}})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}

	if _, err := bridge.Authenticate(ctx, acp.AuthenticateRequest{MethodId: acp.AuthMethodId("token")}); err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	select {
	case req := <-remote.authenticateRequests:
		if req.MethodId != acp.AuthMethodId("token") {
			t.Fatalf("authenticate method id = %q", req.MethodId)
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for authenticate forwarding")
	}

	if _, err := bridge.SetSessionMode(ctx, acp.SetSessionModeRequest{SessionId: sessResp.SessionId, ModeId: acp.SessionModeId("review")}); err != nil {
		t.Fatalf("SetSessionMode: %v", err)
	}
	select {
	case req := <-remote.modeRequests:
		if req.ModeId != acp.SessionModeId("review") {
			t.Fatalf("mode id = %q", req.ModeId)
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for set session mode forwarding")
	}

	promptResp, err := bridge.Prompt(ctx, acp.PromptRequest{SessionId: sessResp.SessionId, Prompt: []acp.ContentBlock{acp.TextBlock("need approval")}})
	if err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	if promptResp.StopReason != acp.StopReasonEndTurn {
		t.Fatalf("stop reason = %q, want %q", promptResp.StopReason, acp.StopReasonEndTurn)
	}

	select {
	case req := <-permissions:
		if req.SessionId != sessResp.SessionId {
			t.Fatalf("permission session id = %q, want %q", req.SessionId, sessResp.SessionId)
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for permission callback")
	}

	select {
	case resp := <-remote.permissionResponses:
		if resp.Outcome.Selected == nil || resp.Outcome.Selected.OptionId != acp.PermissionOptionId("allow") {
			t.Fatalf("unexpected permission response: %#v", resp)
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for permission response")
	}

	first := waitForUpdate(t, ctx, updates)
	second := waitForUpdate(t, ctx, updates)
	if first.Update.AgentMessageChunk == nil && second.Update.AgentMessageChunk == nil {
		t.Fatalf("expected forwarded agent message updates, got %#v and %#v", first, second)
	}
}

func TestBridgeClientMethodStubs(t *testing.T) {
	t.Parallel()

	bridge := proxy.New(&provider.LocalProvider{}, session.New(), proxy.Config{})
	ctx := context.Background()

	tests := []struct {
		name   string
		method string
		call   func() error
	}{
		{
			name:   "ReadTextFile",
			method: acp.ClientMethodFsReadTextFile,
			call: func() error {
				_, err := bridge.ReadTextFile(ctx, acp.ReadTextFileRequest{})
				return err
			},
		},
		{
			name:   "WriteTextFile",
			method: acp.ClientMethodFsWriteTextFile,
			call: func() error {
				_, err := bridge.WriteTextFile(ctx, acp.WriteTextFileRequest{})
				return err
			},
		},
		{
			name:   "CreateTerminal",
			method: acp.ClientMethodTerminalCreate,
			call: func() error {
				_, err := bridge.CreateTerminal(ctx, acp.CreateTerminalRequest{})
				return err
			},
		},
		{
			name:   "KillTerminalCommand",
			method: acp.ClientMethodTerminalKill,
			call: func() error {
				_, err := bridge.KillTerminalCommand(ctx, acp.KillTerminalCommandRequest{})
				return err
			},
		},
		{
			name:   "TerminalOutput",
			method: acp.ClientMethodTerminalOutput,
			call: func() error {
				_, err := bridge.TerminalOutput(ctx, acp.TerminalOutputRequest{})
				return err
			},
		},
		{
			name:   "ReleaseTerminal",
			method: acp.ClientMethodTerminalRelease,
			call: func() error {
				_, err := bridge.ReleaseTerminal(ctx, acp.ReleaseTerminalRequest{})
				return err
			},
		},
		{
			name:   "WaitForTerminalExit",
			method: acp.ClientMethodTerminalWaitForExit,
			call: func() error {
				_, err := bridge.WaitForTerminalExit(ctx, acp.WaitForTerminalExitRequest{})
				return err
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.call()
			var reqErr *acp.RequestError
			if !errors.As(err, &reqErr) {
				t.Fatalf("expected request error, got %v", err)
			}
			if reqErr.Code != -32601 {
				t.Fatalf("code = %d, want -32601", reqErr.Code)
			}
			data, ok := reqErr.Data.(map[string]any)
			if !ok {
				t.Fatalf("unexpected error data: %#v", reqErr.Data)
			}
			if data["method"] != tc.method {
				t.Fatalf("method = %v, want %s", data["method"], tc.method)
			}
		})
	}
}

type inMemoryProvider struct {
	agent *remoteTestAgent
}

func (p *inMemoryProvider) Start(ctx context.Context, opts provider.ExecOptions) (*provider.Process, error) {
	_ = opts

	clientToAgentReader, clientToAgentWriter := io.Pipe()
	agentToClientReader, agentToClientWriter := io.Pipe()
	conn := acp.NewAgentSideConnection(p.agent, agentToClientWriter, clientToAgentReader)
	p.agent.setConn(conn)

	done := make(chan error, 1)
	var once sync.Once
	finish := func(err error) {
		once.Do(func() {
			done <- err
			close(done)
		})
	}

	go func() {
		<-ctx.Done()
		_ = clientToAgentWriter.Close()
		_ = agentToClientWriter.Close()
		_ = clientToAgentReader.Close()
		finish(ctx.Err())
	}()
	go func() {
		<-conn.Done()
		finish(nil)
	}()

	return &provider.Process{
		Stdin:  clientToAgentWriter,
		Stdout: agentToClientReader,
		Wait: func() error {
			err, ok := <-done
			if !ok {
				return nil
			}
			return err
		},
		Cancel: func() {
			_ = clientToAgentWriter.Close()
			_ = agentToClientWriter.Close()
			_ = clientToAgentReader.Close()
			finish(context.Canceled)
		},
	}, nil
}

type remoteTestAgent struct {
	conn                 *acp.AgentSideConnection
	authenticateRequests chan acp.AuthenticateRequest
	modeRequests         chan acp.SetSessionModeRequest
	permissionResponses  chan acp.RequestPermissionResponse
}

func newRemoteTestAgent() *remoteTestAgent {
	return &remoteTestAgent{
		authenticateRequests: make(chan acp.AuthenticateRequest, 2),
		modeRequests:         make(chan acp.SetSessionModeRequest, 2),
		permissionResponses:  make(chan acp.RequestPermissionResponse, 2),
	}
}

func (a *remoteTestAgent) setConn(conn *acp.AgentSideConnection) {
	a.conn = conn
}

func (a *remoteTestAgent) Initialize(ctx context.Context, params acp.InitializeRequest) (acp.InitializeResponse, error) {
	_ = ctx
	return acp.InitializeResponse{
		ProtocolVersion:   params.ProtocolVersion,
		AgentCapabilities: acp.AgentCapabilities{},
		AuthMethods:       []acp.AuthMethod{{Id: acp.AuthMethodId("token"), Name: "Token"}},
	}, nil
}

func (a *remoteTestAgent) NewSession(ctx context.Context, params acp.NewSessionRequest) (acp.NewSessionResponse, error) {
	_ = ctx
	_ = params
	return acp.NewSessionResponse{SessionId: acp.SessionId("permission-session-001")}, nil
}

func (a *remoteTestAgent) Prompt(ctx context.Context, params acp.PromptRequest) (acp.PromptResponse, error) {
	if err := a.conn.SessionUpdate(ctx, acp.SessionNotification{SessionId: params.SessionId, Update: acp.UpdateAgentMessageText("requesting permission")}); err != nil {
		return acp.PromptResponse{}, err
	}

	resp, err := a.conn.RequestPermission(ctx, acp.RequestPermissionRequest{
		SessionId: params.SessionId,
		ToolCall:  acp.RequestPermissionToolCall{ToolCallId: acp.ToolCallId("tool-1")},
		Options: []acp.PermissionOption{
			{Kind: acp.PermissionOptionKindAllowOnce, Name: "Allow", OptionId: acp.PermissionOptionId("allow")},
			{Kind: acp.PermissionOptionKindRejectOnce, Name: "Reject", OptionId: acp.PermissionOptionId("reject")},
		},
	})
	if err != nil {
		return acp.PromptResponse{}, err
	}
	a.permissionResponses <- resp

	if err := a.conn.SessionUpdate(ctx, acp.SessionNotification{SessionId: params.SessionId, Update: acp.UpdateAgentMessageText("permission resolved")}); err != nil {
		return acp.PromptResponse{}, err
	}

	return acp.PromptResponse{StopReason: acp.StopReasonEndTurn}, nil
}

func (a *remoteTestAgent) Cancel(ctx context.Context, params acp.CancelNotification) error {
	_ = ctx
	_ = params
	return nil
}

func (a *remoteTestAgent) Authenticate(ctx context.Context, params acp.AuthenticateRequest) (acp.AuthenticateResponse, error) {
	_ = ctx
	a.authenticateRequests <- params
	return acp.AuthenticateResponse{}, nil
}

func (a *remoteTestAgent) SetSessionMode(ctx context.Context, params acp.SetSessionModeRequest) (acp.SetSessionModeResponse, error) {
	_ = ctx
	a.modeRequests <- params
	return acp.SetSessionModeResponse{}, nil
}

func waitForCondition(ctx context.Context, fn func() error) error {
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		if err := fn(); err == nil {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func waitForUpdate(t *testing.T, ctx context.Context, updates <-chan acp.SessionNotification) acp.SessionNotification {
	t.Helper()

	select {
	case update := <-updates:
		return update
	case <-ctx.Done():
		t.Fatal("timed out waiting for session update")
		return acp.SessionNotification{}
	}
}
