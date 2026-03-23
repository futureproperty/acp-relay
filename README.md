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

### OpenClaw Integration (via acpx + stdio)

OpenClaw has a built-in ACP runtime powered by the [acpx](https://github.com/openclaw/openclaw/tree/main/extensions/acpx) plugin.
The easiest way to connect OpenClaw to acp-relay is over **stdio** — OpenClaw spawns `acp-relay` as a child process and they speak ACP JSON-RPC over stdin/stdout. What acp-relay does downstream (local, K8s, Docker, sandbox) is completely transparent to OpenClaw.

```
OpenClaw Gateway
    │ sessions_spawn(runtime="acp")
    ▼
  acpx plugin
    │ spawns child process (stdio)
    ▼
  acp-relay stdio --provider <type> --command <agent>
    │ JSON-RPC over stdio
    ▼
  Agent process (local / K8s pod / Docker / sandbox)
```

#### Step 1 — Install acp-relay

```bash
go install github.com/futureproperty/acp-relay/cmd/acp-relay@latest
```

#### Step 2 — Configure OpenClaw

Set the acpx plugin to use acp-relay as the agent command:

```bash
# Point acpx at acp-relay (stdio mode, K8s provider example)
openclaw config set plugins.entries.acpx.config.command \
  "acp-relay stdio --provider k8s --namespace agents --pod codex-agent-0 --command claude"

# Or for local provider
openclaw config set plugins.entries.acpx.config.command \
  "acp-relay stdio --provider local --command claude"
```

Or in `~/.openclaw/openclaw.json`:

```json5
{
  acp: {
    enabled: true,
    backend: "acpx",
    defaultAgent: "codex",
  },
  plugins: {
    entries: {
      acpx: {
        enabled: true,
        config: {
          command: "acp-relay stdio --provider k8s --namespace agents --pod codex-agent-0 --command claude",
          expectedVersion: "any",
          permissionMode: "approve-all",
        },
      },
    },
  },
}
```

#### Step 3 — Spawn a session

From chat:
```text
/acp spawn codex --mode persistent --thread auto
```

From an agent tool call:
```json
{
  "task": "Summarize failing tests in this repo",
  "runtime": "acp",
  "agentId": "codex",
  "mode": "run"
}
```

OpenClaw spawns `acp-relay stdio ...` as a child process, sends `initialize` → `session/new` → `session/prompt` over stdin, and reads `session/update` events from stdout. The relay handles all downstream connectivity.
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