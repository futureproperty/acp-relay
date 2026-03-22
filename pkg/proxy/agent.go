package proxy

import (
	"context"
	"fmt"

	acp "github.com/coder/acp-go-sdk"

	"github.com/yourorg/acp-remote/pkg/provider"
	"github.com/yourorg/acp-remote/pkg/session"
)

func (b *Bridge) Initialize(ctx context.Context, params acp.InitializeRequest) (acp.InitializeResponse, error) {
	_ = ctx

	version := acp.ProtocolVersion(acp.ProtocolVersionNumber)
	if params.ProtocolVersion != 0 {
		version = params.ProtocolVersion
	}

	b.mu.Lock()
	b.protocolVersion = version
	b.mu.Unlock()

	return acp.InitializeResponse{
		ProtocolVersion:   version,
		AgentCapabilities: acp.AgentCapabilities{},
		AgentInfo: &acp.Implementation{
			Name:    "acp-remote",
			Version: "dev",
		},
		AuthMethods: []acp.AuthMethod{},
	}, nil
}

func (b *Bridge) NewSession(ctx context.Context, params acp.NewSessionRequest) (acp.NewSessionResponse, error) {
	if len(b.cfg.Command) == 0 {
		return acp.NewSessionResponse{}, fmt.Errorf("bridge command is required")
	}

	proc, err := b.provider.Start(ctx, provider.ExecOptions{
		Command: append([]string(nil), b.cfg.Command...),
		Env:     append([]string(nil), b.cfg.Env...),
		Dir:     params.Cwd,
	})
	if err != nil {
		return acp.NewSessionResponse{}, fmt.Errorf("start remote agent: %w", err)
	}

	conn := acp.NewClientSideConnection(b, proc.Stdin, proc.Stdout)
	if _, err := conn.Initialize(ctx, b.clientInitializeRequest()); err != nil {
		proc.Cancel()
		return acp.NewSessionResponse{}, fmt.Errorf("initialize remote agent: %w", err)
	}

	resp, err := conn.NewSession(ctx, params)
	if err != nil {
		proc.Cancel()
		return acp.NewSessionResponse{}, fmt.Errorf("create remote session: %w", err)
	}

	if err := b.sessions.Create(string(resp.SessionId)); err != nil {
		proc.Cancel()
		return acp.NewSessionResponse{}, fmt.Errorf("create local session: %w", err)
	}
	if err := b.sessions.Transition(string(resp.SessionId), session.StateStarting); err != nil {
		proc.Cancel()
		return acp.NewSessionResponse{}, fmt.Errorf("transition local session to starting: %w", err)
	}
	if err := b.sessions.BindProcess(string(resp.SessionId), proc); err != nil {
		proc.Cancel()
		return acp.NewSessionResponse{}, fmt.Errorf("bind local session process: %w", err)
	}
	if err := b.sessions.Transition(string(resp.SessionId), session.StateConnected); err != nil {
		proc.Cancel()
		return acp.NewSessionResponse{}, fmt.Errorf("transition local session to connected: %w", err)
	}

	b.storeConn(resp.SessionId, conn)
	b.monitorRemote(resp.SessionId, proc)

	return resp, nil
}

func (b *Bridge) Prompt(ctx context.Context, params acp.PromptRequest) (acp.PromptResponse, error) {
	conn, err := b.connForSession(params.SessionId)
	if err != nil {
		return acp.PromptResponse{}, err
	}

	if err := b.sessions.Transition(string(params.SessionId), session.StateWorking); err != nil {
		return acp.PromptResponse{}, fmt.Errorf("transition session to working: %w", err)
	}

	resp, err := conn.Prompt(ctx, params)
	if err != nil {
		_ = b.sessions.Transition(string(params.SessionId), session.StateError)
		return acp.PromptResponse{}, err
	}
	if err := b.sessions.Transition(string(params.SessionId), session.StateConnected); err != nil {
		return acp.PromptResponse{}, fmt.Errorf("transition session to connected: %w", err)
	}

	return resp, nil
}

func (b *Bridge) Cancel(ctx context.Context, params acp.CancelNotification) error {
	conn, err := b.connForSession(params.SessionId)
	if err != nil {
		return err
	}
	return conn.Cancel(ctx, params)
}

func (b *Bridge) Authenticate(ctx context.Context, params acp.AuthenticateRequest) (acp.AuthenticateResponse, error) {
	conn, err := b.singleConn()
	if err != nil {
		return acp.AuthenticateResponse{}, err
	}
	return conn.Authenticate(ctx, params)
}

func (b *Bridge) SetSessionMode(ctx context.Context, params acp.SetSessionModeRequest) (acp.SetSessionModeResponse, error) {
	conn, err := b.connForSession(params.SessionId)
	if err != nil {
		return acp.SetSessionModeResponse{}, err
	}
	return conn.SetSessionMode(ctx, params)
}
