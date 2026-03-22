# Learnings

## [2026-03-22] Session: ses_2e9d04dd4ffe3R9Q2G13c017KW — Initial Setup

### Project Overview
- Go module: `github.com/yourorg/acp-remote`
- Working directory: `/home/ubuntu/tmp/opencodeworkspace/acp-remote`
- acp-go-sdk local cache: `/home/ubuntu/go/pkg/mod/github.com/coder/acp-go-sdk@v0.6.3/`
- Reference project: `/home/ubuntu/tmp/opencodeworkspace/openworkspace/`

### ACP SDK Key Facts
- SDK version: v0.6.3
- Agent interface: 6 methods: Initialize, NewSession, Prompt, Cancel, Authenticate, SetSessionMode
- Client interface: 9 methods: ReadTextFile, WriteTextFile, RequestPermission, SessionUpdate, CreateTerminal, KillTerminalCommand, TerminalOutput, ReleaseTerminal, WaitForTerminalExit
- Optional interfaces (NOT implemented): AgentLoader, AgentExperimental → SDK auto returns MethodNotFound
- Connection creation: `NewAgentSideConnection(agent, writer, reader)` and `NewClientSideConnection(client, writer, reader)`
- Types location: `/home/ubuntu/go/pkg/mod/github.com/coder/acp-go-sdk@v0.6.3/types_gen.go:4340-4419`

### Architecture Patterns
- Initialize handled locally by acp-remote (no backend agent needed)
- NewSession → starts agent process, sends Initialize+NewSession to agent
- 1:1 session:process mapping
- stdio agent mode: connect os.Stdin/os.Stdout to Bridge AgentSideConnection
- HTTP serve mode: POST /acp/message + GET /acp/events SSE

### Reference Files
- openworkspace ACP handler: pkg/acp/handler.go
- openworkspace connector: pkg/acp/connector.go
- openworkspace permissions: pkg/acp/permissions.go
- openworkspace SSE: pkg/acp/sse.go
- openworkspace transport: pkg/acp/transport.go
- openworkspace auth: pkg/http/auth.go
- openworkspace K8s exec: pkg/kube/stream_exec.go

## [2026-03-23] Proxy bridge implementation

- Bridge local `Initialize` should negotiate/store protocol version, but backend `session/new` must always re-`Initialize` the spawned agent through `acp.ClientSideConnection` before forwarding session creation.
- Bridge-to-agent client capabilities should stay minimal (`fs` disabled, `terminal` disabled) because non-forwarded client methods intentionally return `MethodNotFound`.
- `Authenticate` has no session ID in ACP, so the bridge currently forwards it only when exactly one remote session is active; session-scoped methods (`Prompt`, `Cancel`, `SetSessionMode`) resolve by `sessionId`.
- Session cleanup is safest when driven by the provider `Wait()` goroutine: remove the stored connection and close the session manager entry after the remote process exits.

## [2026-03-23] HTTP+SSE transport

- The module declares Go 1.21, so HTTP transport registration must use classic `ServeMux` paths plus manual method/path checks instead of Go 1.22 route patterns and `PathValue`.
- `EventBroker` should guard unsubscribe with a channel identity check so a stale unsubscribe cannot close a newer subscriber registered under the same session ID.
- SSE endpoint tests are more reliable when they retry `Publish` until the subscriber is attached instead of assuming a fixed connect timing.
