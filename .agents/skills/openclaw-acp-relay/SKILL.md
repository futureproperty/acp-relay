# Skill: OpenClaw ACP Relay Integration

Use this skill when configuring OpenClaw to spawn ACP agent sessions through acp-relay, troubleshooting ACP relay connectivity, or setting up remote agent execution via K8s/Docker/sandbox providers.

## Architecture

```
OpenClaw Gateway
    │ sessions_spawn(runtime="acp")
    ▼
  acpx plugin (ACP runtime backend)
    │ spawns child process over stdio
    ▼
  acp-relay stdio --provider <type> --command <agent>
    │ ACP JSON-RPC 2.0 over stdin/stdout
    ▼
  Agent process (local / K8s pod / Docker container / OpenSandbox)
```

OpenClaw talks ACP over stdio to acp-relay. acp-relay is a transparent bridge — it handles downstream connectivity (subprocess, K8s exec, Docker exec, sandbox HTTP) and relays all ACP messages bidirectionally. OpenClaw does not know or care what provider acp-relay uses.

## Prerequisites

- OpenClaw with the acpx plugin enabled (`openclaw plugins install acpx`)
- acp-relay binary installed: `go install github.com/futureproperty/acp-relay/cmd/acp-relay@latest`
- The target agent binary (e.g., `claude`, `codex`, `opencode`) accessible from the acp-relay execution context

## Configuration

### Minimal setup (local provider)

```bash
# Enable ACP + acpx backend
openclaw config set acp.enabled true
openclaw config set acp.backend acpx
openclaw config set plugins.entries.acpx.enabled true

# Point acpx at acp-relay
openclaw config set plugins.entries.acpx.config.command \
  "acp-relay stdio --provider local --command claude"
openclaw config set plugins.entries.acpx.config.expectedVersion any
openclaw config set plugins.entries.acpx.config.permissionMode approve-all
```

### Remote agent via K8s

```bash
openclaw config set plugins.entries.acpx.config.command \
  "acp-relay stdio --provider k8s --namespace agents --pod codex-agent-0 --command claude"
```

### Remote agent via Docker

```bash
openclaw config set plugins.entries.acpx.config.command \
  "acp-relay stdio --provider docker --container agent-container --command claude"
```

### Remote agent via OpenSandbox

```bash
openclaw config set plugins.entries.acpx.config.command \
  "acp-relay stdio --provider sandbox --endpoint http://sandbox-host:44772 --token sandbox-token"
```

### Full openclaw.json example

```json5
{
  acp: {
    enabled: true,
    backend: "acpx",
    defaultAgent: "codex",
    allowedAgents: ["codex", "claude", "opencode", "gemini"],
    maxConcurrentSessions: 4,
    stream: {
      coalesceIdleMs: 300,
      maxChunkChars: 1200,
    },
    runtime: {
      ttlMinutes: 120,
    },
  },
  plugins: {
    entries: {
      acpx: {
        enabled: true,
        config: {
          command: "acp-relay stdio --provider k8s --namespace agents --pod codex-agent-0 --command claude",
          expectedVersion: "any",
          permissionMode: "approve-all",
          nonInteractivePermissions: "deny",
          timeoutSeconds: 300,
        },
      },
    },
  },
}
```

## Spawning sessions

### From chat (operator)

```text
/acp spawn codex --mode persistent --thread auto
/acp spawn claude --mode oneshot
/acp status
/acp cancel
/acp close
```

### From agent tool call

```json
{
  "task": "Summarize failing tests in this repo",
  "runtime": "acp",
  "agentId": "codex",
  "mode": "run"
}
```

### From agent (persistent + thread-bound)

```json
{
  "task": "Fix the auth bug in src/login.ts",
  "runtime": "acp",
  "agentId": "codex",
  "mode": "session",
  "thread": true
}
```

### Resume an existing session

```json
{
  "task": "Continue where we left off",
  "runtime": "acp",
  "agentId": "codex",
  "resumeSessionId": "<previous-session-uuid>"
}
```

## ACP message flow through the relay

1. **Initialize**: OpenClaw sends `initialize` with client capabilities → acp-relay responds with agent capabilities
2. **Session create**: OpenClaw sends `session/new` with `cwd` → acp-relay starts the backend agent process → returns `sessionId`
3. **Prompt turn**: OpenClaw sends `session/prompt` with user message → acp-relay forwards to agent → agent streams `session/update` notifications back through the relay
4. **Permissions**: Agent sends `session/request_permission` → acp-relay auto-approves (HTTP mode) or forwards to OpenClaw (stdio mode) → OpenClaw handles via `permissionMode` config
5. **Cancel**: OpenClaw sends `session/cancel` → acp-relay forwards to agent
6. **Close**: OpenClaw closes the session → acp-relay terminates the agent process

## Troubleshooting

| Symptom | Cause | Fix |
|---------|-------|-----|
| `ACP runtime backend is not configured` | acpx plugin not installed or disabled | `openclaw plugins install acpx && openclaw config set plugins.entries.acpx.enabled true` |
| `ACP is disabled by policy` | `acp.enabled` is false | `openclaw config set acp.enabled true` |
| acp-relay process fails to start | Binary not in PATH or wrong provider flags | Verify `which acp-relay` and test manually: `acp-relay stdio --provider local --command <agent>` |
| Agent process fails inside K8s/Docker | Wrong namespace/pod/container or agent not installed in target | SSH/exec into the target and verify the agent binary exists |
| `AcpRuntimeError: Permission prompt unavailable` | Non-interactive session hitting permission check | Set `plugins.entries.acpx.config.permissionMode` to `approve-all` |
| Session stalls after agent completes | Agent process exited but ACP session not cleaned up | Check `ps aux | grep acp-relay`; kill stale processes |
| `expectedVersion` mismatch | acpx version check fails against acp-relay | Set `plugins.entries.acpx.config.expectedVersion` to `"any"` |

## Health check

```text
/acp doctor
```

This runs the acpx health check. With acp-relay as the command, it verifies:
- The command is executable
- stdio JSON-RPC handshake succeeds
- Agent process can be spawned through the relay

## Key constraints

- **stdio only**: OpenClaw connects to acp-relay exclusively over stdio. The relay's HTTP+SSE serve mode is for other clients (IDEs, curl), not OpenClaw.
- **`fs/*` and `terminal/*` stubs**: acp-relay returns MethodNotFound for filesystem and terminal callbacks. OpenClaw's acpx backend handles these on the client side.
- **`session/load` not implemented**: Resuming sessions through the relay is limited. Direct agent connections support richer session restoration.
- **Permission handling**: ACP sessions are non-interactive. Set `permissionMode: "approve-all"` for full agent capabilities, or `"approve-reads"` + `nonInteractivePermissions: "deny"` for graceful degradation.

## References

- [acp-relay repository](https://github.com/futureproperty/acp-relay)
- [Agent Client Protocol specification](https://agentclientprotocol.com)
- [OpenClaw ACP agents docs](https://docs.openclaw.ai/tools/acp-agents)
- [OpenClaw ACP CLI reference](https://docs.openclaw.ai/cli/acp)
- [acpx plugin source](https://github.com/openclaw/openclaw/tree/main/extensions/acpx)
