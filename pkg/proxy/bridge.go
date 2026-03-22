package proxy

import (
	"context"
	"fmt"
	"sync"

	acp "github.com/coder/acp-go-sdk"

	"github.com/yourorg/acp-remote/pkg/provider"
	"github.com/yourorg/acp-remote/pkg/session"
)

var (
	_ acp.Agent  = (*Bridge)(nil)
	_ acp.Client = (*Bridge)(nil)
)

type Config struct {
	Command []string
	Env     []string
}

type Bridge struct {
	provider provider.ExecProvider
	sessions *session.Manager
	cfg      Config

	updateCb func(params acp.SessionNotification) error
	permCb   func(ctx context.Context, params acp.RequestPermissionRequest) (acp.RequestPermissionResponse, error)

	mu              sync.RWMutex
	conns           map[string]*acp.ClientSideConnection
	protocolVersion acp.ProtocolVersion
}

func New(p provider.ExecProvider, sm *session.Manager, cfg Config) *Bridge {
	return &Bridge{
		provider:        p,
		sessions:        sm,
		cfg:             cfg,
		conns:           make(map[string]*acp.ClientSideConnection),
		protocolVersion: acp.ProtocolVersionNumber,
	}
}

func (b *Bridge) SetSessionUpdateCallback(cb func(acp.SessionNotification) error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.updateCb = cb
}

func (b *Bridge) SetPermissionCallback(cb func(context.Context, acp.RequestPermissionRequest) (acp.RequestPermissionResponse, error)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.permCb = cb
}

func (b *Bridge) clientInitializeRequest() acp.InitializeRequest {
	b.mu.RLock()
	version := b.protocolVersion
	b.mu.RUnlock()

	return acp.InitializeRequest{
		ProtocolVersion: version,
		ClientCapabilities: acp.ClientCapabilities{
			Fs:       acp.FileSystemCapability{},
			Terminal: false,
		},
		ClientInfo: &acp.Implementation{
			Name:    "acp-remote",
			Version: "dev",
		},
	}
}

func (b *Bridge) storeConn(sessionID acp.SessionId, conn *acp.ClientSideConnection) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.conns[string(sessionID)] = conn
}

func (b *Bridge) removeConn(sessionID acp.SessionId) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.conns, string(sessionID))
}

func (b *Bridge) connForSession(sessionID acp.SessionId) (*acp.ClientSideConnection, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	conn, ok := b.conns[string(sessionID)]
	if !ok {
		return nil, fmt.Errorf("session %q: %w", sessionID, session.ErrSessionNotFound)
	}
	return conn, nil
}

func (b *Bridge) singleConn() (*acp.ClientSideConnection, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if len(b.conns) == 0 {
		return nil, fmt.Errorf("no active remote sessions")
	}
	if len(b.conns) > 1 {
		return nil, fmt.Errorf("authenticate requires exactly one active session")
	}
	for _, conn := range b.conns {
		return conn, nil
	}
	return nil, fmt.Errorf("no active remote sessions")
}

func (b *Bridge) sessionUpdateCallback() func(acp.SessionNotification) error {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.updateCb
}

func (b *Bridge) permissionCallback() func(context.Context, acp.RequestPermissionRequest) (acp.RequestPermissionResponse, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.permCb
}

func (b *Bridge) monitorRemote(sessionID acp.SessionId, proc *provider.Process) {
	if proc == nil || proc.Wait == nil {
		return
	}

	go func() {
		_ = proc.Wait()
		b.removeConn(sessionID)
		_ = b.sessions.Close(string(sessionID))
	}()
}

func methodNotFound(method string) error {
	return acp.NewMethodNotFound(method)
}
