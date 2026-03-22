package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	acp "github.com/coder/acp-go-sdk"

	"github.com/yourorg/acp-remote/pkg/auth"
	"github.com/yourorg/acp-remote/pkg/provider"
	"github.com/yourorg/acp-remote/pkg/proxy"
	"github.com/yourorg/acp-remote/pkg/session"
	"github.com/yourorg/acp-remote/pkg/transport"
)

const bridgeMessageScannerMaxSize = 10 * 1024 * 1024

func runServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	listen := fs.String("listen", ":8080", "listen address")
	token := fs.String("token", "", "bearer token for auth (empty = no auth)")
	defaultProvider := fs.String("default-provider", "local", "default provider (local)")
	defaultCommand := fs.String("default-command", "", "default command to run")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if *defaultProvider != "local" {
		return fmt.Errorf("unsupported provider: %s (only 'local' supported)", *defaultProvider)
	}
	if *defaultCommand == "" {
		return fmt.Errorf("--default-command is required")
	}

	cmd := strings.Fields(*defaultCommand)
	if len(cmd) == 0 {
		return fmt.Errorf("--default-command must contain an executable")
	}

	sm := session.New()
	bridge := proxy.New(&provider.LocalProvider{}, sm, proxy.Config{Command: cmd})
	broker := transport.NewEventBroker()
	msgHandler := newBridgeMessageHandler(bridge, broker)
	defer msgHandler.Close()
	defer closeAllSessions(sm)

	bridge.SetSessionUpdateCallback(func(notification acp.SessionNotification) error {
		broker.Publish(string(notification.SessionId), transport.Event{
			Type: "session_update",
			Data: notification,
		})
		return nil
	})
	bridge.SetPermissionCallback(func(ctx context.Context, req acp.RequestPermissionRequest) (acp.RequestPermissionResponse, error) {
		_ = ctx
		return autoApprovePermission(req), nil
	})

	httpHandler := transport.NewHandler(broker, msgHandler, noopPermissionHandler{})
	mux := http.NewServeMux()
	httpHandler.Register(mux)

	handler := auth.Middleware(*token)(mux)
	srv := &http.Server{
		Addr:    *listen,
		Handler: handler,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		fmt.Fprintf(os.Stderr, "acp-remote serve listening on %s\n", *listen)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	case err, ok := <-errCh:
		if !ok {
			return nil
		}
		return err
	}
}

type bridgeMessageHandler struct {
	mu              sync.Mutex
	requestW        *io.PipeWriter
	responseR       *io.PipeReader
	responseW       *io.PipeWriter
	responseScanner *bufio.Scanner
	broker          *transport.EventBroker
	closeOnce       sync.Once
}

func newBridgeMessageHandler(bridge *proxy.Bridge, broker *transport.EventBroker) *bridgeMessageHandler {
	requestR, requestW := io.Pipe()
	responseR, responseW := io.Pipe()
	_ = acp.NewAgentSideConnection(bridge, responseW, requestR)

	scanner := bufio.NewScanner(responseR)
	scanner.Buffer(make([]byte, 0, 1024*1024), bridgeMessageScannerMaxSize)

	return &bridgeMessageHandler{
		requestW:        requestW,
		responseR:       responseR,
		responseW:       responseW,
		responseScanner: scanner,
		broker:          broker,
	}
}

func (h *bridgeMessageHandler) Close() error {
	var err error
	h.closeOnce.Do(func() {
		err = errors.Join(h.requestW.Close(), h.responseW.Close(), h.responseR.Close())
	})
	return err
}

func (h *bridgeMessageHandler) ServeMessage(req []byte) ([]byte, error) {
	env, err := parseRPCEnvelope(req)
	if err != nil {
		return jsonRPCParseError(), nil
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	if _, err := h.requestW.Write(append(append([]byte(nil), req...), '\n')); err != nil {
		return nil, fmt.Errorf("write request: %w", err)
	}
	if env.ID == nil {
		return []byte{}, nil
	}

	for h.responseScanner.Scan() {
		line := append([]byte(nil), h.responseScanner.Bytes()...)
		switch classifyJSONRPCMessage(line) {
		case jsonRPCResponse:
			return line, nil
		case jsonRPCNotification:
			h.forwardNotification(line)
		}
	}

	if err := h.responseScanner.Err(); err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	return nil, fmt.Errorf("read response: connection closed")
}

type jsonRPCMessageType int

const (
	jsonRPCUnknown jsonRPCMessageType = iota
	jsonRPCResponse
	jsonRPCNotification
)

type jsonRPCEnvelope struct {
	ID     *json.RawMessage `json:"id"`
	Method string           `json:"method"`
	Params json.RawMessage  `json:"params"`
}

func parseRPCEnvelope(raw []byte) (jsonRPCEnvelope, error) {
	var env jsonRPCEnvelope
	err := json.Unmarshal(raw, &env)
	return env, err
}

func classifyJSONRPCMessage(raw []byte) jsonRPCMessageType {
	env, err := parseRPCEnvelope(raw)
	if err != nil {
		return jsonRPCUnknown
	}
	if env.ID != nil && env.Method == "" {
		return jsonRPCResponse
	}
	if env.ID == nil && env.Method != "" {
		return jsonRPCNotification
	}
	return jsonRPCUnknown
}

func (h *bridgeMessageHandler) forwardNotification(raw []byte) {
	env, err := parseRPCEnvelope(raw)
	if err != nil || env.Method != acp.ClientMethodSessionUpdate || h.broker == nil {
		return
	}

	var notification acp.SessionNotification
	if err := json.Unmarshal(env.Params, &notification); err != nil {
		return
	}

	h.broker.Publish(string(notification.SessionId), transport.Event{
		Type: "session_update",
		Data: notification,
	})
}

func jsonRPCParseError() []byte {
	return []byte(`{"jsonrpc":"2.0","id":null,"error":{"code":-32700,"message":"Parse error"}}`)
}

func autoApprovePermission(req acp.RequestPermissionRequest) acp.RequestPermissionResponse {
	selected := firstAllowOption(req.Options)
	if selected == "" && len(req.Options) > 0 {
		selected = req.Options[0].OptionId
	}
	if selected == "" {
		return acp.RequestPermissionResponse{Outcome: acp.NewRequestPermissionOutcomeCancelled()}
	}
	return acp.RequestPermissionResponse{Outcome: acp.NewRequestPermissionOutcomeSelected(selected)}
}

func firstAllowOption(options []acp.PermissionOption) acp.PermissionOptionId {
	for _, option := range options {
		if option.Kind == acp.PermissionOptionKindAllowOnce || option.Kind == acp.PermissionOptionKindAllowAlways {
			return option.OptionId
		}
	}
	return ""
}

type noopPermissionHandler struct{}

func (noopPermissionHandler) Approve(string) {}

func (noopPermissionHandler) Deny(string) {}

func closeAllSessions(sm *session.Manager) error {
	ids := sm.List()
	if len(ids) == 0 {
		return nil
	}

	var errs []error
	for _, id := range ids {
		if err := sm.Close(id); err != nil && !errors.Is(err, session.ErrSessionNotFound) {
			errs = append(errs, fmt.Errorf("close session %s: %w", id, err))
		}
	}
	return errors.Join(errs...)
}
