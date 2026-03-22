package proxy

import (
	"context"

	acp "github.com/coder/acp-go-sdk"
)

func (b *Bridge) SessionUpdate(ctx context.Context, params acp.SessionNotification) error {
	_ = ctx
	cb := b.sessionUpdateCallback()
	if cb == nil {
		return nil
	}
	return cb(params)
}

func (b *Bridge) RequestPermission(ctx context.Context, params acp.RequestPermissionRequest) (acp.RequestPermissionResponse, error) {
	cb := b.permissionCallback()
	if cb == nil {
		return acp.RequestPermissionResponse{}, methodNotFound(acp.ClientMethodSessionRequestPermission)
	}
	return cb(ctx, params)
}

func (b *Bridge) ReadTextFile(ctx context.Context, params acp.ReadTextFileRequest) (acp.ReadTextFileResponse, error) {
	_ = ctx
	_ = params
	return acp.ReadTextFileResponse{}, methodNotFound(acp.ClientMethodFsReadTextFile)
}

func (b *Bridge) WriteTextFile(ctx context.Context, params acp.WriteTextFileRequest) (acp.WriteTextFileResponse, error) {
	_ = ctx
	_ = params
	return acp.WriteTextFileResponse{}, methodNotFound(acp.ClientMethodFsWriteTextFile)
}

func (b *Bridge) CreateTerminal(ctx context.Context, params acp.CreateTerminalRequest) (acp.CreateTerminalResponse, error) {
	_ = ctx
	_ = params
	return acp.CreateTerminalResponse{}, methodNotFound(acp.ClientMethodTerminalCreate)
}

func (b *Bridge) KillTerminalCommand(ctx context.Context, params acp.KillTerminalCommandRequest) (acp.KillTerminalCommandResponse, error) {
	_ = ctx
	_ = params
	return acp.KillTerminalCommandResponse{}, methodNotFound(acp.ClientMethodTerminalKill)
}

func (b *Bridge) TerminalOutput(ctx context.Context, params acp.TerminalOutputRequest) (acp.TerminalOutputResponse, error) {
	_ = ctx
	_ = params
	return acp.TerminalOutputResponse{}, methodNotFound(acp.ClientMethodTerminalOutput)
}

func (b *Bridge) ReleaseTerminal(ctx context.Context, params acp.ReleaseTerminalRequest) (acp.ReleaseTerminalResponse, error) {
	_ = ctx
	_ = params
	return acp.ReleaseTerminalResponse{}, methodNotFound(acp.ClientMethodTerminalRelease)
}

func (b *Bridge) WaitForTerminalExit(ctx context.Context, params acp.WaitForTerminalExitRequest) (acp.WaitForTerminalExitResponse, error) {
	_ = ctx
	_ = params
	return acp.WaitForTerminalExitResponse{}, methodNotFound(acp.ClientMethodTerminalWaitForExit)
}
