# acp-relay

> Universal remote ACP proxy — connects ACP clients to agent processes running in K8s pods, Docker containers, OpenSandbox workspaces, or local subprocesses.

## Overview

acp-relay implements the [Agent Client Protocol](https://agentclientprotocol.com) as a proxy bridge. It provides:
- **stdio mode**: transparent ACP agent (use in pipes or as process bridge)
- **serve mode**: HTTP+SSE server accepting ACP requests and forwarding to backend agents

Core abstraction: `ExecProvider` — starts a subprocess and returns stdio pipes.

### Architecture

```
Client (IDE/CLI/OpenClaw)
    │ JSON-RPC over HTTP+SSE (serve mode)
    │ JSON-RPC over stdio (stdio mode)
    ▼
┌─────────────────────────┐
│      acp-relay          │
│  ┌──────────────────┐   │
│  │  ACP Bridge      │   │  implements acp.Agent + acp.Client
│  └────────┬─────────┘   │
│           │              │
│  ┌────────▼─────────┐   │
│  │  ExecProvider    │   │  local / k8s / docker / sandbox
│  └────────┬─────────┘   │
└───────────┼─────────────┘
            │ stdio (JSON-RPC)
            ▼
        Agent Process
```

## Installation

```bash
# Install binary
go install github.com/futureproperty/acp-relay/cmd/acp-relay@latest

# Or use as a library
go get github.com/futureproperty/acp-relay
```

## Quick Start

### stdio Mode

Run acp-relay as a transparent ACP agent that forwards to a local backend:

```bash
# Forward to a local agent binary
acp-relay stdio --provider local --command ./my-agent

# Example: pipe an initialize request
echo '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":1,"clientCapabilities":{}}}' \
  | acp-relay stdio --provider local --command ./my-agent
```

### serve Mode

Start an HTTP+SSE ACP server:

```bash
acp-relay serve \
  --listen :8080 \
  --token my-secret-token \
  --default-provider local \
  --default-command ./my-agent
```

Then connect with any ACP client:

```bash
# Initialize
curl -H 'Authorization: Bearer my-secret-token' \
     -H 'Content-Type: application/json' \
     -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":1,"clientCapabilities":{}}}' \
     http://localhost:8080/acp/message

# Subscribe to SSE events
curl -H 'Authorization: Bearer my-secret-token' \
     http://localhost:8080/acp/events?sessionId=<session-id>
```

## ExecProvider Implementations

| Provider | Package | Description |
|----------|---------|-------------|
| `LocalProvider` | `pkg/provider` | Runs agent as local subprocess via `os/exec` |
| `K8sProvider` | `pkg/provider` | Exec into a running Kubernetes pod |
| `DockerProvider` | `pkg/provider` | Exec into a running Docker container |
| `SandboxProvider` | `pkg/provider` | Connect to OpenSandbox execd HTTP API (port 44772) |

### OpenSandbox Example

```go
p := provider.NewSandboxProvider(provider.SandboxOptions{
    Endpoint:    "http://sandbox-host:44772",
    AccessToken: "sandbox-token",
})
```

### OpenClaw Example

OpenClaw uses acp-relay to spawn sessions on remote code agents (e.g. Claude Code, OpenCode).
Run acp-relay in serve mode as the bridge between OpenClaw and the backend agent:

```bash
# Start acp-relay pointing at a code agent (e.g. claude, opencode)
acp-relay serve \
  --listen :8080 \
  --token my-secret-token \
  --default-provider local \
  --default-command claude
```

Then configure OpenClaw to connect to it as an ACP endpoint:

```yaml
# openclaw config
agents:
  - name: claude-remote
    endpoint: http://acp-relay-host:8080
    token: my-secret-token
```

When OpenClaw sends `session/new`, acp-relay spawns the agent process and relays all
ACP messages (`session/prompt`, `session/update`, `session/request_permission`, etc.)
bidirectionally between OpenClaw and the code agent.

## ACP Method Support Matrix

| Method | Direction | Status |
|--------|-----------|--------|
| `initialize` | client→agent | ✅ Handled locally by acp-relay |
| `session/new` | client→agent | ✅ Starts backend agent process |
| `session/prompt` | client→agent | ✅ Forwarded to backend |
| `session/cancel` | client→agent | ✅ Forwarded to backend |
| `authenticate` | client→agent | ✅ Forwarded to backend |
| `session/set_mode` | client→agent | ✅ Forwarded to backend |
| `session/load` | client→agent | ⚠️ Not implemented (SDK returns MethodNotFound) |
| `session/update` | agent→client | ✅ Forwarded to client |
| `session/request_permission` | agent→client | ✅ Auto-approved (HTTP mode) / forwarded (stdio mode) |
| `fs/read_text_file` | agent→client | ❌ Stub (MethodNotFound) |
| `fs/write_text_file` | agent→client | ❌ Stub (MethodNotFound) |
| `terminal/*` | agent→client | ❌ Stub (MethodNotFound) |

## Development

```bash
# Run tests
make test

# Build binary
make build

# Static analysis
make vet

# Integration tests (requires real K8s/Docker/Sandbox)
go test ./... -tags integration
```