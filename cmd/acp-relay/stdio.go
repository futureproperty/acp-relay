package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	acp "github.com/coder/acp-go-sdk"

	"github.com/futureproperty/acp-relay/pkg/provider"
	"github.com/futureproperty/acp-relay/pkg/proxy"
	"github.com/futureproperty/acp-relay/pkg/session"
)

func runStdio(args []string) error {
	fs := flag.NewFlagSet("stdio", flag.ContinueOnError)
	providerName := fs.String("provider", "local", "provider type (local)")
	commandStr := fs.String("command", "", "command to run (space-separated)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if *providerName != "local" {
		return fmt.Errorf("unsupported provider: %s (only 'local' supported)", *providerName)
	}
	if *commandStr == "" {
		return fmt.Errorf("--command is required")
	}

	cmd := strings.Fields(*commandStr)

	sm := session.New()
	p := &provider.LocalProvider{}
	bridge := proxy.New(p, sm, proxy.Config{Command: cmd})

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// AgentSideConnection: SDK dispatches incoming JSON-RPC (from stdin) to bridge.
	// peerInput = os.Stdout (SDK writes responses there)
	// peerOutput = os.Stdin (SDK reads requests from there)
	agentConn := acp.NewAgentSideConnection(bridge, os.Stdout, os.Stdin)

	// Forward SessionUpdate notifications from remote agent to the local client.
	bridge.SetSessionUpdateCallback(func(notification acp.SessionNotification) error {
		return agentConn.SessionUpdate(context.Background(), notification)
	})

	// Forward RequestPermission from remote agent to the local client.
	bridge.SetPermissionCallback(func(ctx context.Context, req acp.RequestPermissionRequest) (acp.RequestPermissionResponse, error) {
		return agentConn.RequestPermission(ctx, req)
	})

	select {
	case <-ctx.Done():
	case <-agentConn.Done():
	}
	return nil
}
