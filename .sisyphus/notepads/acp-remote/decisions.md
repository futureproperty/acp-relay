# Decisions

## [2026-03-22] Initial Architecture Decisions

### Module Path
- Use `github.com/yourorg/acp-remote` as module path
- Note: Not yet committed to a specific org — can be changed

### Key Design Decisions
1. ExecProvider abstraction: Start(ctx, ExecOptions) → *Process
2. Bridge implements both acp.Agent AND acp.Client interfaces
3. Initialize handled locally, not forwarded to backend
4. Permission forwarding: remote agent → local client → back to remote agent
5. Non-core Client methods return MethodNotFound (ReadTextFile, WriteTextFile, terminal/*)
6. Session state machine: idle→starting→connected→working→error→closed

### Prohibited Patterns
- NO time.Sleep() outside tests
- NO custom JSON-RPC types (SDK handles dispatch)
- NO openworkspace JSONRPCRequest/JSONRPCResponse types
- NO TLS/mTLS/WebSocket/gRPC
- NO data persistence (memory only)
- NO agent reconnect/restart
