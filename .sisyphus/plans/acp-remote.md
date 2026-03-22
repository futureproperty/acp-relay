# ACP-Remote: 通用远程 ACP 调用模块

## TL;DR

> **Quick Summary**: 构建一个 Go 模块 `acp-remote`，作为 ACP 协议的通用远程代理，将 ACP 客户端 (IDE/CLI/OpenClaw) 连接到运行在各种环境中的 agent 进程 (K8s Pod / Docker 容器 / OpenSandbox / 本地进程)。核心抽象是 `ExecProvider`（建立到 agent 的 stdio 管道），核心功能是基于 acp-go-sdk 的双向 ACP Bridge。
> 
> **Deliverables**:
> - Go 库 (`pkg/proxy`, `pkg/provider`, `pkg/transport`, `pkg/session`, `pkg/auth`)
> - CLI binary (`acp-remote serve` HTTP+SSE 模式 + `acp-remote stdio` agent 模式)
> - 4 个 ExecProvider 实现: local subprocess / K8s exec / Docker exec / OpenSandbox execd
> 
> **Estimated Effort**: Large
> **Parallel Execution**: YES — 4 waves
> **Critical Path**: Task 1 → Task 2 → Task 3 → Task 5 → Task 6 → Task 8-11 (parallel) → F1-F4

---

## Context

### Original Request
设计一个通用的远程 ACP 调用模块，能够为 OpenSandbox (alibaba/OpenSandbox) 创建的 workspace 和 OpenClaw (openclaw/openclaw) 等已有 ACP 支持的 agent 项目实现 ACP 打通。从参考实现 openworkspace 的 `pkg/acp/` 中汲取设计思路，但构建为独立可复用的模块。

### Interview Summary
**Key Discussions**:
- **产出形态**: Go 库 + 独立 HTTP 服务 binary (双模式: serve + stdio)
- **核心洞察**: 所有后端本质上都是 exec stdio，区别仅在于 wrapper (K8s/Docker/SSH/Local/HTTP)
- **Session 映射**: 1:1 (每个 ACP session 启动独立 agent 进程)
- **Agent 发现**: stdio 模式用 CLI flags，HTTP 模式用 `_meta` 字段动态指定
- **协议覆盖**: 核心 + 权限 (initialize/session/new/prompt/cancel + request_permission)
- **认证**: Bearer Token (HTTP 模式)
- **测试**: TDD with Go 标准 testing

**Research Findings**:
- openworkspace 的 `pkg/acp/` 有良好分层但紧耦合 K8s — 不直接复用代码，仅借鉴 EventBroker/PermissionForwarder 模式
- OpenSandbox 纯 REST+SSE，无 ACP — 需要 execd HTTP provider
- OpenClaw 有成熟 ACP bridge (TypeScript) — 验证了 stdio 代理方案的可行性
- ACP 官方 Go SDK (github.com/coder/acp-go-sdk) 已提供完整的 Agent/Client 接口和 JSON-RPC dispatch。已验证的 Agent 接口: Initialize, NewSession, Prompt, Cancel, Authenticate, SetSessionMode (6个方法)。Client 接口: ReadTextFile, WriteTextFile, RequestPermission, SessionUpdate, CreateTerminal, KillTerminalCommand, TerminalOutput, ReleaseTerminal, WaitForTerminalExit (9个方法)。可选接口 AgentLoader/AgentExperimental 不实现时 SDK 自动返回 MethodNotFound。本地缓存: `/home/ubuntu/go/pkg/mod/github.com/coder/acp-go-sdk@v0.6.3/types_gen.go:4340-4419`。使用最新稳定版本 (`go get github.com/coder/acp-go-sdk@latest`)

### Metis Review
**Identified Gaps** (addressed):
- **SDK 已处理 JSON-RPC dispatch**: 不需要自定义 JSONRPCRequest/Response 类型，SDK 的 AgentSideConnection/ClientSideConnection 自动处理
- **Agent 接口有 6 个方法必须全部实现**: Initialize, NewSession, Prompt, Cancel (4个核心转发) + Authenticate, SetSessionMode (2个透传转发). 可选接口 AgentLoader/AgentExperimental 不实现, SDK 自动返回 MethodNotFound
- **HTTP↔stdio 桥接需要 io.Pipe() 多路复用**: POST body → pipe writer, SDK output → SSE reader
- **OpenSandbox execd 可能不支持交互式 stdin/stdout**: 最后实现，先验证 API 契约
- **双向代理死锁风险**: 使用 buffered channel + goroutine-per-notification

---

## Work Objectives

### Core Objective
构建 `acp-remote` Go 模块，实现 ACP 协议的通用远程代理，通过 `ExecProvider` 抽象层支持多种后端环境。

### Concrete Deliverables
- `pkg/provider/provider.go` — ExecProvider 接口定义
- `pkg/provider/local.go` — 本地子进程 provider
- `pkg/provider/k8s.go` — Kubernetes exec provider
- `pkg/provider/docker.go` — Docker exec provider
- `pkg/provider/sandbox.go` — OpenSandbox execd provider
- `pkg/proxy/bridge.go` — 双向 ACP Bridge (实现 acp.Agent + acp.Client)
- `pkg/transport/http.go` — HTTP+SSE transport (serve 模式)
- `pkg/transport/sse.go` — SSE 事件推送
- `pkg/session/manager.go` — Session 生命周期管理
- `pkg/auth/auth.go` — Bearer Token 认证中间件
- `cmd/acp-remote/main.go` — CLI 入口 (serve + stdio 子命令)

### Definition of Done
- [ ] `go test ./... -v -count=1 -race` 全部通过
- [ ] `go vet ./...` 无错误
- [ ] `go build ./cmd/acp-remote` 编译成功
- [ ] stdio 模式 smoke test 通过 (echo agent)
- [ ] HTTP 模式 smoke test 通过 (curl + SSE)
- [ ] Bearer Token 认证拒绝无 token 请求

### Must Have
- ExecProvider 接口 + 4 种实现 (local/k8s/docker/sandbox)
- 双向 ACP Bridge 基于 acp-go-sdk Agent/Client 接口
- HTTP+SSE serve 模式 + stdio agent 模式
- 1:1 session:process 映射
- Permission 转发 (request_permission → client → approve/deny → agent)
- Bearer Token 认证 (HTTP 模式)
- TDD 全覆盖 + `-race` 检测

### Must NOT Have (Guardrails)
- 不重新实现 JSON-RPC 2.0 序列化/方法分发 — SDK 已做
- 不使用 openworkspace 的 JSONRPCRequest/JSONRPCResponse 类型
- 不使用 `time.Sleep()` 做同步（仅测试中且有明确注释可用）
- 不实现 fs/read_text_file, fs/write_text_file, terminal/* — 仅 stub 返回 MethodNotFound
- 不实现 session/load (AgentLoader), session/set_model (AgentExperimental) — 不实现可选接口，SDK 自动返回 MethodNotFound
- 不做数据持久化 — session 仅内存
- 不做 agent 重连/重启 — 进程挂了 = session 结束
- 不做多 session 复用进程 — 严格 1:1
- 不添加 TLS/mTLS, WebSocket, gRPC, rate limiting, metrics, health check
- 不写过度注释/JSDoc — 代码自文档化
- 不做过度抽象 — 不为未来可能的需求预留框架

---

## Verification Strategy

> **ZERO HUMAN INTERVENTION** — ALL verification is agent-executed. No exceptions.

### Test Decision
- **Infrastructure exists**: NO (新项目)
- **Automated tests**: TDD (RED-GREEN-REFACTOR)
- **Framework**: Go 标准 testing 包
- **每个 task 遵循**: 先写测试 (RED) → 最小实现 (GREEN) → 重构 (REFACTOR)

### QA Policy
Every task MUST include agent-executed QA scenarios.
Evidence saved to `.sisyphus/evidence/task-{N}-{scenario-slug}.{ext}`.

- **Library/Module**: Use Bash (go test) — Run tests, compare output
- **CLI**: Use interactive_bash (tmux) — Run binary, send input, validate output
- **API/Backend**: Use Bash (curl) — Send requests, assert status + response fields

---

## Execution Strategy

### Parallel Execution Waves

```
Wave 1 (Foundation — all independent):
├── Task 1: Project scaffolding + go.mod + Makefile [quick]
├── Task 2: ExecProvider interface + local subprocess provider [deep]
├── Task 3: Session manager with state machine [quick]
└── Task 4: Bearer Token auth middleware [quick]

Wave 2 (Core — depends on Wave 1):
├── Task 5: Bidirectional ACP Bridge proxy (depends: 2, 3) [deep]
├── Task 6: HTTP+SSE transport layer (depends: 3, 4) [deep]
└── Task 7: stdio transport + CLI (depends: 5) [unspecified-high]

Wave 3 (Providers — all independent, depend on Task 2):
├── Task 8: K8s exec provider (depends: 2) [unspecified-high]
├── Task 9: Docker exec provider (depends: 2) [unspecified-high]
└── Task 10: OpenSandbox execd provider (depends: 2) [unspecified-high]

Wave 4 (Integration):
├── Task 11: CLI serve subcommand + end-to-end wiring (depends: 5, 6, 7) [deep]
└── Task 12: README + usage docs (depends: 11) [writing]

Wave FINAL (After ALL tasks — 4 parallel reviews, then user okay):
├── Task F1: Plan compliance audit (oracle)
├── Task F2: Code quality review (unspecified-high)
├── Task F3: Real manual QA (unspecified-high)
└── Task F4: Scope fidelity check (deep)
-> Present results -> Get explicit user okay

Critical Path: Task 1 → Task 2 → Task 5 → Task 7 → Task 11 → F1-F4 → user okay
Parallel Speedup: ~60% faster than sequential
Max Concurrent: 4 (Wave 1, Wave 3)
```

### Dependency Matrix

| Task | Depends On | Blocks | Wave |
|------|-----------|--------|------|
| 1 | — | 2,3,4 | 1 |
| 2 | 1 | 5,8,9,10 | 1 |
| 3 | 1 | 5,6 | 1 |
| 4 | 1 | 6 | 1 |
| 5 | 2,3 | 7,11 | 2 |
| 6 | 3,4 | 11 | 2 |
| 7 | 5 | 11 | 2 |
| 8 | 2 | — | 3 |
| 9 | 2 | — | 3 |
| 10 | 2 | — | 3 |
| 11 | 5,6,7 | 12 | 4 |
| 12 | 11 | — | 4 |

### Agent Dispatch Summary

- **Wave 1**: **4** — T1 → `quick`, T2 → `deep`, T3 → `quick`, T4 → `quick`
- **Wave 2**: **3** — T5 → `deep`, T6 → `deep`, T7 → `unspecified-high`
- **Wave 3**: **3** — T8 → `unspecified-high`, T9 → `unspecified-high`, T10 → `unspecified-high`
- **Wave 4**: **2** — T11 → `deep`, T12 → `writing`
- **FINAL**: **4** — F1 → `oracle`, F2 → `unspecified-high`, F3 → `unspecified-high`, F4 → `deep`

---

## TODOs

- [ ] 1. Project Scaffolding + go.mod + Makefile

  **What to do**:
  - 初始化 git 仓库: `git init` (工作目录 `/home/ubuntu/tmp/opencodeworkspace/acp-remote` 当前不是 git repo)
  - 创建 `.gitignore` (bin/, vendor/, *.exe)
  - 创建 Go module: `go mod init github.com/yourorg/acp-remote`
  - 添加 `github.com/coder/acp-go-sdk` 依赖 (`go get github.com/coder/acp-go-sdk@latest`)。验证 Agent/Client 接口可编译。本地缓存参考: `/home/ubuntu/go/pkg/mod/github.com/coder/acp-go-sdk@v0.6.3/`
  - 创建 Makefile with targets: `test`, `build`, `vet`, `lint`
  - 创建包目录结构 (pkg/proxy, pkg/provider, pkg/transport, pkg/session, pkg/auth) 每个包一个 `doc.go`
  - 创建 cmd/acp-remote/ 目录结构
  - 确保 `go build ./...` 和 `go vet ./...` 通过

  **Must NOT do**:
  - 不写任何业务逻辑代码
  - 不添加非必要依赖

  **Recommended Agent Profile**:
  - **Category**: `quick`
  - **Skills**: [`golang-patterns`]

  **Parallelization**:
  - **Can Run In Parallel**: YES
  - **Parallel Group**: Wave 1 (with Tasks 2, 3, 4)
  - **Blocks**: Tasks 2, 3, 4 (all need project skeleton)
  - **Blocked By**: None

  **References**:
  - `go.mod` 参考 openworkspace 的: `/home/ubuntu/tmp/opencodeworkspace/openworkspace/go.mod` — 查看 acp-go-sdk 的 import path
  - acp-go-sdk GitHub: `https://github.com/coder/acp-go-sdk` — 使用最新稳定版本。本地缓存 v0.6.3 可侜参考: `/home/ubuntu/go/pkg/mod/github.com/coder/acp-go-sdk@v0.6.3/`
  - openworkspace Makefile: `/home/ubuntu/tmp/opencodeworkspace/openworkspace/Makefile` — 参考 Make target 格式

  **Acceptance Criteria**:
  - [ ] `go build ./...` 编译通过
  - [ ] `go vet ./...` 无错误
  - [ ] `go mod tidy` 无变更
  - [ ] acp-go-sdk 可正常 import (doc.go 中有 import)

  **QA Scenarios:**
  ```
  Scenario: Go module 初始化成功
    Tool: Bash
    Steps:
      1. cd /home/ubuntu/tmp/opencodeworkspace/acp-remote && go build ./...
      2. go vet ./...
      3. go mod verify
    Expected Result: 所有命令 exit 0
    Evidence: .sisyphus/evidence/task-1-scaffold-build.txt
  ```

  **Commit**: YES
  - Message: `init: project skeleton with go.mod, Makefile, package dirs`
  - Files: `go.mod, go.sum, Makefile, pkg/*/doc.go, cmd/acp-remote/`
  - Pre-commit: `go vet ./...`

- [ ] 2. ExecProvider Interface + Local Subprocess Provider

  **What to do**:
  - 定义 `ExecProvider` 接口:
    ```go
    type ExecProvider interface {
        Start(ctx context.Context, opts ExecOptions) (*Process, error)
    }
    type ExecOptions struct {
        Command []string
        Env     []string
        Dir     string
    }
    type Process struct {
        Stdin  io.WriteCloser
        Stdout io.ReadCloser
        Wait   func() error
        Cancel func()
    }
    ```
  - 实现 `LocalProvider` (os/exec.Command 封装)
  - 创建 mock ACP agent: `testdata/mock_agent/main.go` — 参考 `/home/ubuntu/tmp/opencodeworkspace/openworkspace/tests/mock_agent/main.go`，实现 stdio JSON-RPC 处理 (initialize 返回 protocolVersion, session/new 返回 sessionId, session/prompt 回写 session/update, session/cancel 关闭)。这个 mock agent 将被后续所有 task 的测试使用。
  - TDD: 先写 provider_test.go (接口 contract 测试) + local_test.go (用 mock agent 做通信测试)
  - 测试: 写入 stdin → 读取 stdout, context cancel 中止进程, 进程退出检测

  **Must NOT do**:
  - 不在这个 task 中实现 K8s/Docker/Sandbox provider — 仅接口 + local
  - 不做 agent 重启逻辑

  **Recommended Agent Profile**:
  - **Category**: `deep`
  - **Skills**: [`golang-patterns`, `golang-testing`]

  **Parallelization**:
  - **Can Run In Parallel**: YES
  - **Parallel Group**: Wave 1 (with Tasks 1, 3, 4) — 但需 Task 1 先完成 skeleton
  - **Blocks**: Tasks 5, 8, 9, 10
  - **Blocked By**: Task 1

  **References**:
  - openworkspace K8s connector (模式参考): `/home/ubuntu/tmp/opencodeworkspace/openworkspace/pkg/acp/connector.go` — 参考 Process 生命周期模式 (start/stdin/stdout/cancel/close)
  - openworkspace stream_exec: `/home/ubuntu/tmp/opencodeworkspace/openworkspace/pkg/kube/stream_exec.go` — 参考 StreamExecOpts 结构
  - Go os/exec 文档: `https://pkg.go.dev/os/exec` — Command, StdinPipe, StdoutPipe, Process.Wait

  **Acceptance Criteria**:
  - [ ] `ExecProvider` 接口在 `pkg/provider/provider.go` 中定义
  - [ ] `LocalProvider` 在 `pkg/provider/local.go` 中实现
  - [ ] `go test ./pkg/provider/... -v -race` 全部通过
  - [ ] 测试覆盖: stdin/stdout 通信, context cancel, 进程退出

  **QA Scenarios:**
  ```
  Scenario: Local provider 启动 cat 并通过 stdin/stdout 通信
    Tool: Bash
    Steps:
      1. go test ./pkg/provider/... -v -race -run TestLocalProvider
    Expected Result: PASS, 包含 stdin→stdout echo 验证
    Evidence: .sisyphus/evidence/task-2-local-provider-test.txt

  Scenario: Context cancel 中止进程
    Tool: Bash
    Steps:
      1. go test ./pkg/provider/... -v -race -run TestLocalProviderCancel
    Expected Result: PASS, 进程在 cancel 后退出, Wait() 返回 error
    Evidence: .sisyphus/evidence/task-2-cancel-test.txt
  ```

  **Commit**: YES
  - Message: `feat(provider): define ExecProvider interface and local subprocess impl`
  - Files: `pkg/provider/provider.go, pkg/provider/local.go, pkg/provider/*_test.go`
  - Pre-commit: `go test ./pkg/provider/... -race`

- [ ] 3. Session Manager with State Machine

  **What to do**:
  - 实现 `SessionManager`:
    - 创建 session (返回 sessionID)
    - 绑定 session 到 Process
    - 获取 session 状态和关联的 Process
    - 关闭 session (清理 process)
    - 列出活跃 sessions
  - Session 状态机: `idle` → `starting` → `connected` → `working` → `error` → `closed`
  - 线程安全 (sync.RWMutex)
  - TDD: 先写 manager_test.go — 状态转换、并发访问、清理

  **Must NOT do**:
  - 不做持久化 — 纯内存 map
  - 不做 TTL/自动过期

  **Recommended Agent Profile**:
  - **Category**: `quick`
  - **Skills**: [`golang-patterns`, `golang-testing`]

  **Parallelization**:
  - **Can Run In Parallel**: YES
  - **Parallel Group**: Wave 1 (with Tasks 1, 2, 4)
  - **Blocks**: Tasks 5, 6
  - **Blocked By**: Task 1

  **References**:
  - openworkspace ACP session 状态机: `/home/ubuntu/tmp/opencodeworkspace/openworkspace/pkg/domain/acp_session.go` — 参考状态定义和转换表
  - openworkspace session store: `/home/ubuntu/tmp/opencodeworkspace/openworkspace/pkg/store/session_store.go` — 参考内存存储模式
  - openworkspace PermissionForwarder 的 registry 模式: `/home/ubuntu/tmp/opencodeworkspace/openworkspace/pkg/acp/permissions.go:112-160` — MemoryConnectorRegistry 模式

  **Acceptance Criteria**:
  - [ ] `SessionManager` 在 `pkg/session/manager.go` 中实现
  - [ ] `go test ./pkg/session/... -v -race` 全部通过
  - [ ] 测试覆盖: 创建/获取/关闭 session, 状态转换, 并发安全, 无效转换返回错误

  **QA Scenarios:**
  ```
  Scenario: Session 生命周期
    Tool: Bash
    Steps:
      1. go test ./pkg/session/... -v -race -run TestSessionLifecycle
    Expected Result: PASS, 包含 create→bind→working→close 完整流程
    Evidence: .sisyphus/evidence/task-3-session-lifecycle.txt

  Scenario: 并发 session 操作
    Tool: Bash
    Steps:
      1. go test ./pkg/session/... -v -race -run TestConcurrent
    Expected Result: PASS, 无 race condition
    Evidence: .sisyphus/evidence/task-3-concurrent.txt
  ```

  **Commit**: YES
  - Message: `feat(session): implement session manager with state machine`
  - Files: `pkg/session/manager.go, pkg/session/manager_test.go`
  - Pre-commit: `go test ./pkg/session/... -race`

- [ ] 4. Bearer Token Auth Middleware

  **What to do**:
  - 实现 `AuthMiddleware(token string) func(http.Handler) http.Handler`
  - 检查 `Authorization: Bearer <token>` header
  - 无 token/错误 token → 401 Unauthorized (JSON body `{"error":"unauthorized"}`)
  - 空 token 配置 → 跳过认证 (开发模式)
  - TDD: 先写 auth_test.go

  **Must NOT do**:
  - 不做 JWT/OAuth — 仅简单 Bearer Token 比对
  - 不做 rate limiting

  **Recommended Agent Profile**:
  - **Category**: `quick`
  - **Skills**: [`golang-patterns`, `golang-testing`]

  **Parallelization**:
  - **Can Run In Parallel**: YES
  - **Parallel Group**: Wave 1 (with Tasks 1, 2, 3)
  - **Blocks**: Task 6
  - **Blocked By**: Task 1

  **References**:
  - openworkspace auth 中间件: `/home/ubuntu/tmp/opencodeworkspace/openworkspace/pkg/http/auth.go` — 参考中间件 pattern
  - openworkspace auth 测试: `/home/ubuntu/tmp/opencodeworkspace/openworkspace/pkg/http/auth_test.go` — 参考测试 pattern

  **Acceptance Criteria**:
  - [ ] `AuthMiddleware` 在 `pkg/auth/auth.go` 中实现
  - [ ] `go test ./pkg/auth/... -v -race` 全部通过
  - [ ] 测试覆盖: 有效 token, 无效 token, 缺少 token, 空 token 配置(跳过)

  **QA Scenarios:**
  ```
  Scenario: 有效 token 通过认证
    Tool: Bash
    Steps:
      1. go test ./pkg/auth/... -v -race -run TestAuthValid
    Expected Result: PASS, 请求通过中间件
    Evidence: .sisyphus/evidence/task-4-auth-valid.txt

  Scenario: 无效 token 被拒绝
    Tool: Bash
    Steps:
      1. go test ./pkg/auth/... -v -race -run TestAuthInvalid
    Expected Result: PASS, 返回 401 + JSON error body
    Evidence: .sisyphus/evidence/task-4-auth-invalid.txt
  ```

  **Commit**: YES
  - Message: `feat(auth): add bearer token HTTP middleware`
  - Files: `pkg/auth/auth.go, pkg/auth/auth_test.go`
  - Pre-commit: `go test ./pkg/auth/... -race`

---

- [ ] 5. Bidirectional ACP Bridge Proxy

  **What to do**:
  - 实现 `Bridge` 结构体，它是 acp-remote 的核心:
    - 实现 `acp.Agent` 接口 (6个方法: Initialize, NewSession, Prompt, Cancel, Authenticate, SetSessionMode)
    - 实现 `acp.Client` 接口 (9个方法, 其中 SessionUpdate + RequestPermission 转发, 其余返回错误)
    - **Initialize 生命周期设计**:
      - `Initialize()`: 由 Bridge 本地处理，返回 acp-remote 的 capabilities，不需要后端 agent
      - `NewSession()`: 通过 `ExecProvider.Start()` 启动 agent 进程，建立 `acp.ClientSideConnection`，先向 agent 发 `Initialize` 再发 `NewSession`
      - `Prompt()`/`Cancel()`/`Authenticate()`/`SetSessionMode()`: 转发给已建立的 agent 连接
    - 代理流程: Agent.Prompt() → ClientSideConn.Prompt() → remote agent → Client.SessionUpdate() → AgentSideConn.SessionUpdate() → local client
  - Permission 转发: remote agent 发 request_permission → 转发给 local client → 等待回复 → 转回给 remote agent
  - Agent 接口核心方法 (Initialize, NewSession, Prompt, Cancel): 实际转发到远程 agent
  - Agent 接口非核心方法 (Authenticate, SetSessionMode): 转发到远程 agent (如果 agent 不支持会返回错误)
  - 不实现 AgentLoader (session/load) 和 AgentExperimental (session/set_model) 可选接口 — SDK 自动返回 MethodNotFound
  - 非核心 Client 方法 (ReadTextFile, WriteTextFile, terminal/*) 返回 MethodNotFound
  - TDD: 用 local provider + mock ACP agent (参考 `/home/ubuntu/tmp/opencodeworkspace/openworkspace/tests/mock_agent/main.go` 的 stdio JSON-RPC agent) 做端到端测试。在 `testdata/mock_agent/main.go` 创建 mock agent，实现 initialize/session_new/session_prompt/session_cancel 的 JSON-RPC 处理。
  - 测试: initialize → session/new → prompt → session/update 流 → cancel → session 关闭

  **Must NOT do**:
  - 不重新实现 JSON-RPC dispatch — 全用 SDK
  - 不在 Bridge 内做 HTTP/SSE — 那是 Transport 层的事
  - 不用 time.Sleep 做同步

  **Recommended Agent Profile**:
  - **Category**: `deep`
  - **Skills**: [`golang-patterns`, `golang-testing`]

  **Parallelization**:
  - **Can Run In Parallel**: NO (critical path)
  - **Parallel Group**: Wave 2
  - **Blocks**: Tasks 7, 11
  - **Blocked By**: Tasks 2, 3

  **References**:
  - acp-go-sdk Agent 接口: `https://github.com/coder/acp-go-sdk` — `Agent` interface: Initialize, NewSession, Prompt, Cancel, Authenticate, SetSessionMode (6 个方法). 可选接口: `AgentLoader` (LoadSession), `AgentExperimental` (SetSessionModel) — 不实现可选接口, SDK 自动返回 MethodNotFound. 本地缓存位置: `/home/ubuntu/go/pkg/mod/github.com/coder/acp-go-sdk@v0.6.3/types_gen.go:4340-4367`
  - acp-go-sdk Client 接口: 同上 — `Client` interface with SessionUpdate/RequestPermission/ReadTextFile/WriteTextFile/terminal methods
  - acp-go-sdk Connection: 同上 — `NewAgentSideConnection(agent, writer, reader)` 和 `NewClientSideConnection(client, writer, reader)`
  - openworkspace handler 转发模式: `/home/ubuntu/tmp/opencodeworkspace/openworkspace/pkg/acp/handler.go:61-75` — 方法路由 pattern (仅作参考, SDK 已处理)
  - openworkspace connector: `/home/ubuntu/tmp/opencodeworkspace/openworkspace/pkg/acp/connector.go:78-102` — SendPrompt + stdin pipe 模式
  - openworkspace PermissionForwarder: `/home/ubuntu/tmp/opencodeworkspace/openworkspace/pkg/acp/permissions.go:41-80` — 权限请求阻塞等待模式

  **Acceptance Criteria**:
  - [ ] `Bridge` 在 `pkg/proxy/bridge.go` 中实现
  - [ ] `pkg/proxy/agent.go` 实现 `acp.Agent` 接口
  - [ ] `pkg/proxy/client.go` 实现 `acp.Client` 接口
  - [ ] `go test ./pkg/proxy/... -v -race` 全部通过
  - [ ] 端到端测试: initialize → session/new → prompt → session/update → cancel
  - [ ] Permission 转发测试通过
  - [ ] 可选接口 (AgentLoader/AgentExperimental) 未实现，SDK 自动返回 MethodNotFound（通过测试验证）

  **QA Scenarios:**
  ```
  Scenario: 完整 ACP 会话流程
    Tool: Bash
    Steps:
      1. go test ./pkg/proxy/... -v -race -run TestBridgeEndToEnd
    Expected Result: PASS, 完整的 init→new→prompt→update→cancel 流程
    Evidence: .sisyphus/evidence/task-5-bridge-e2e.txt

  Scenario: Agent 进程崩溃处理
    Tool: Bash
    Steps:
      1. go test ./pkg/proxy/... -v -race -run TestBridgeAgentCrash
    Expected Result: PASS, session 转为 error 状态, prompt 返回错误
    Evidence: .sisyphus/evidence/task-5-agent-crash.txt

  Scenario: Permission 转发
    Tool: Bash
    Steps:
      1. go test ./pkg/proxy/... -v -race -run TestBridgePermission
    Expected Result: PASS, permission request 被正确转发并解决
    Evidence: .sisyphus/evidence/task-5-permission.txt
  ```

  **Commit**: YES
  - Message: `feat(proxy): implement bidirectional ACP bridge`
  - Files: `pkg/proxy/bridge.go, pkg/proxy/agent.go, pkg/proxy/client.go, pkg/proxy/*_test.go`
  - Pre-commit: `go test ./pkg/proxy/... -race`

- [ ] 6. HTTP+SSE Transport Layer

  **What to do**:
  - 实现 HTTP+SSE transport 将 HTTP 请求桥接到 Bridge:
    - `POST /acp/message` — 接收 JSON-RPC 请求, 转发给 Bridge, 返回 JSON-RPC 响应
    - `GET /acp/events?sessionId=xxx` — SSE 流, 推送 session/update 和 permission_request 事件
    - `POST /acp/sessions/{sessionId}/approve` — 批准权限请求
    - `POST /acp/sessions/{sessionId}/deny` — 拒绝权限请求
  - 实现 `EventBroker` (channel-based pub/sub, sessionID 级别, buffered channel, non-blocking publish)
  - 实现 `SSEWriter` (Content-Type: text/event-stream, keepalive 30s)
  - 关键: HTTP POST body → io.Pipe() → SDK AgentSideConnection; SDK output → io.Pipe() → SSE
  - TDD: 用 httptest 做请求/响应测试, SSE 流测试

  **Must NOT do**:
  - 不做 WebSocket
  - 不做 TLS/HTTPS
  - 不做 CORS

  **Recommended Agent Profile**:
  - **Category**: `deep`
  - **Skills**: [`golang-patterns`, `golang-testing`]

  **Parallelization**:
  - **Can Run In Parallel**: YES (with Task 5)
  - **Parallel Group**: Wave 2
  - **Blocks**: Task 11
  - **Blocked By**: Tasks 3, 4

  **References**:
  - openworkspace Transport: `/home/ubuntu/tmp/opencodeworkspace/openworkspace/pkg/acp/transport.go` — 完整的 HTTP transport 实现 (RegisterRoutes, handleMessage, handleEvents, handleApprove/Deny)
  - openworkspace SSE: `/home/ubuntu/tmp/opencodeworkspace/openworkspace/pkg/acp/sse.go` — SSEWriter + EventBroker 实现
  - openworkspace Transport 测试: `/home/ubuntu/tmp/opencodeworkspace/openworkspace/pkg/acp/transport_test.go` — httptest 测试模式

  **Acceptance Criteria**:
  - [ ] HTTP endpoints 在 `pkg/transport/http.go` 中实现
  - [ ] EventBroker + SSEWriter 在 `pkg/transport/sse.go` 中实现
  - [ ] `go test ./pkg/transport/... -v -race` 全部通过
  - [ ] 测试覆盖: JSON-RPC POST, SSE 事件, approve/deny, 错误处理, client disconnect

  **QA Scenarios:**
  ```
  Scenario: JSON-RPC POST 请求/响应
    Tool: Bash
    Steps:
      1. go test ./pkg/transport/... -v -race -run TestHTTPMessage
    Expected Result: PASS, initialize 请求正确返回
    Evidence: .sisyphus/evidence/task-6-http-message.txt

  Scenario: SSE 事件流
    Tool: Bash
    Steps:
      1. go test ./pkg/transport/... -v -race -run TestSSEEvents
    Expected Result: PASS, session/update 事件通过 SSE 正确推送
    Evidence: .sisyphus/evidence/task-6-sse-events.txt

  Scenario: Client disconnect 清理
    Tool: Bash
    Steps:
      1. go test ./pkg/transport/... -v -race -run TestSSEDisconnect
    Expected Result: PASS, context cancel 后 SSE handler 干净退出
    Evidence: .sisyphus/evidence/task-6-sse-disconnect.txt
  ```

  **Commit**: YES
  - Message: `feat(transport): implement HTTP+SSE server transport`
  - Files: `pkg/transport/http.go, pkg/transport/sse.go, pkg/transport/*_test.go`
  - Pre-commit: `go test ./pkg/transport/... -race`

- [ ] 7. stdio Transport + CLI Subcommand

  **What to do**:
  - 实现 `stdio` 子命令:
    - 读取 CLI flags: `--provider` (local/k8s/docker/sandbox), provider-specific flags
    - 创建 ExecProvider 实例
    - 创建 Bridge
    - 将 os.Stdin/os.Stdout 连接到 Bridge 的 AgentSideConnection
    - 处理 SIGINT/SIGTERM 优雅退出
  - 初版只支持 `--provider local --command "<agent-binary>"` (其他 provider 在 Wave 3 添加)
  - TDD: 用 io.Pipe 模拟 stdin/stdout, 测试完整 JSON-RPC 会话

  **Must NOT do**:
  - 不在这个 task 实现 serve 子命令 — 那是 Task 11
  - 不做 daemon 模式

  **Recommended Agent Profile**:
  - **Category**: `unspecified-high`
  - **Skills**: [`golang-patterns`, `golang-testing`]

  **Parallelization**:
  - **Can Run In Parallel**: NO (depends on Task 5 completion)
  - **Parallel Group**: Wave 2 (sequential after Task 5)
  - **Blocks**: Task 11
  - **Blocked By**: Task 5

  **References**:
  - openworkspace mock agent: `/home/ubuntu/tmp/opencodeworkspace/openworkspace/tests/mock_agent/main.go` — 参考 stdio JSON-RPC agent 实现
  - acp-go-sdk stdio 连接: `https://github.com/coder/acp-go-sdk` — `NewAgentSideConnection(agent, os.Stdout, os.Stdin)`

  **Acceptance Criteria**:
  - [ ] `cmd/acp-remote/stdio.go` 实现 stdio 子命令
  - [ ] `go build ./cmd/acp-remote` 编译成功
  - [ ] 集成测试: pipe JSON-RPC initialize 请求 → 得到正确响应

  **QA Scenarios:**
  ```
  Scenario: stdio 模式 end-to-end (使用 mock agent)
    Tool: Bash
    Preconditions: 先构建 mock agent binary (在 Task 2 中已存在 testdata/mock_agent.go 或 test helper)
    Steps:
      1. go build -o bin/acp-remote ./cmd/acp-remote
      2. go build -o bin/mock-agent ./testdata/mock_agent/
      3. echo '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":1,"clientCapabilities":{}}}' | ./bin/acp-remote stdio --provider local --command ./bin/mock-agent
    Expected Result: stdout 包含 JSON-RPC response with protocolVersion (mock agent 回复)
    Evidence: .sisyphus/evidence/task-7-stdio-e2e.txt

  Scenario: SIGINT 优雅退出
    Tool: interactive_bash (tmux)
    Steps:
      1. 启动 acp-remote stdio --provider local --command sleep -- 3600
      2. 发送 SIGINT
      3. 等待进程退出
    Expected Result: 进程在 SIGINT 后干净退出, exit code 0
    Evidence: .sisyphus/evidence/task-7-sigint.txt
  ```

  **Commit**: YES
  - Message: `feat(cmd/stdio): implement stdio transport mode`
  - Files: `cmd/acp-remote/stdio.go, cmd/acp-remote/main.go`
  - Pre-commit: `go test ./... -race`

---

- [ ] 8. Kubernetes Exec Provider

  **What to do**:
  - 实现 `K8sProvider` implementing `ExecProvider`:
    - 使用 `k8s.io/client-go/tools/remotecommand` (SPDY executor)
    - 配置: namespace, pod name, container name, command, kubeconfig path
    - `Start()`: 创建 K8s exec request, 建立 SPDY stream, 返回 stdin/stdout pipes
    - 用 io.Pipe() 作为 stdin/stdout 的桥接
    - context cancel → 关闭 stream
  - TDD: 用 mock K8s client (fake clientset) 测试配置和调用模式
  - 集成测试: `//go:build integration` tag, 需要真实 K8s 集群

  **Must NOT do**:
  - 不做 auto-discovery (namespace/pod 由调用方指定)
  - 不做 reconnect

  **Recommended Agent Profile**:
  - **Category**: `unspecified-high`
  - **Skills**: [`golang-patterns`, `golang-testing`]

  **Parallelization**:
  - **Can Run In Parallel**: YES
  - **Parallel Group**: Wave 3 (with Tasks 9, 10)
  - **Blocks**: None
  - **Blocked By**: Task 2 (ExecProvider interface)

  **References**:
  - openworkspace K8s stream exec: `/home/ubuntu/tmp/opencodeworkspace/openworkspace/pkg/kube/stream_exec.go` — **核心参考** — K8sStreamExecutor.StreamExec() 实现, SPDY executor 创建, StreamOptions 绑定
  - openworkspace K8s client: `/home/ubuntu/tmp/opencodeworkspace/openworkspace/pkg/kube/client.go` — K8s client 初始化模式
  - openworkspace connector start: `/home/ubuntu/tmp/opencodeworkspace/openworkspace/pkg/acp/connector.go:105-146` — io.Pipe() + goroutine + StreamExec 调用模式
  - K8s remotecommand 文档: `https://pkg.go.dev/k8s.io/client-go/tools/remotecommand`

  **Acceptance Criteria**:
  - [ ] `K8sProvider` 在 `pkg/provider/k8s.go` 中实现
  - [ ] `go test ./pkg/provider/... -v -race -run TestK8s` 通过 (单元测试, mock client)
  - [ ] 集成测试在 `pkg/provider/k8s_integration_test.go` (`//go:build integration`)

  **QA Scenarios:**
  ```
  Scenario: K8s provider 单元测试
    Tool: Bash
    Steps:
      1. go test ./pkg/provider/... -v -race -run TestK8sProvider
    Expected Result: PASS, mock K8s client 上的 exec 调用结构正确
    Evidence: .sisyphus/evidence/task-8-k8s-unit.txt
  ```

  **Commit**: YES
  - Message: `feat(provider/k8s): implement Kubernetes exec provider`
  - Files: `pkg/provider/k8s.go, pkg/provider/k8s_test.go, pkg/provider/k8s_integration_test.go`
  - Pre-commit: `go test ./pkg/provider/... -race`

- [ ] 9. Docker Exec Provider

  **What to do**:
  - 实现 `DockerProvider` implementing `ExecProvider`:
    - 使用 Docker Engine API (`github.com/docker/docker/client`)
    - 配置: container ID/name, command, Docker host (default unix socket)
    - `Start()`: ContainerExecCreate → ContainerExecAttach → 返回 HijackedResponse 的 Conn
    - 用 `stdcopy.StdCopy` 做 stdout/stderr demux (或直接 attach with TTY=false)
    - `resp.CloseWrite()` 关闭 stdin 而不关 stdout
    - context cancel → kill exec process
  - TDD: 用 mock Docker client 测试
  - 集成测试: `//go:build integration` tag

  **Must NOT do**:
  - 不做 container 创建/启动 — 假设 container 已运行
  - 不做 TLS 连接到 remote Docker daemon

  **Recommended Agent Profile**:
  - **Category**: `unspecified-high`
  - **Skills**: [`golang-patterns`, `golang-testing`]

  **Parallelization**:
  - **Can Run In Parallel**: YES
  - **Parallel Group**: Wave 3 (with Tasks 8, 10)
  - **Blocks**: None
  - **Blocked By**: Task 2

  **References**:
  - Docker Engine API exec: `https://docs.docker.com/engine/api/v1.43/#tag/Exec`
  - Docker Go SDK: `https://pkg.go.dev/github.com/docker/docker/client` — ContainerExecCreate, ContainerExecAttach, ContainerExecInspect
  - stdcopy 文档: `https://pkg.go.dev/github.com/docker/docker/pkg/stdcopy`

  **Acceptance Criteria**:
  - [ ] `DockerProvider` 在 `pkg/provider/docker.go` 中实现
  - [ ] `go test ./pkg/provider/... -v -race -run TestDocker` 通过
  - [ ] 集成测试在 `pkg/provider/docker_integration_test.go`

  **QA Scenarios:**
  ```
  Scenario: Docker provider 单元测试
    Tool: Bash
    Steps:
      1. go test ./pkg/provider/... -v -race -run TestDockerProvider
    Expected Result: PASS, mock Docker client 上的 exec 调用结构正确
    Evidence: .sisyphus/evidence/task-9-docker-unit.txt
  ```

  **Commit**: YES
  - Message: `feat(provider/docker): implement Docker exec provider`
  - Files: `pkg/provider/docker.go, pkg/provider/docker_test.go, pkg/provider/docker_integration_test.go`
  - Pre-commit: `go test ./pkg/provider/... -race`

- [ ] 10. OpenSandbox Execd Provider

  **What to do**:
  - 实现 `SandboxProvider` implementing `ExecProvider`:
    - 通过 HTTP API 连接 OpenSandbox execd daemon (port 44772)
    - 配置: sandbox endpoint URL, access token, command
    - `Start()`: POST /command 启动命令, SSE 接收 stdout 流
    - stdin: 通过 HTTP request body 或 WebSocket 发送 (取决于 execd API)
    - 如果 execd 不支持交互式 stdin: 注释说明限制, 提供 best-effort 实现
  - TDD: 用 httptest 模拟 execd API
  - 集成测试: `//go:build integration` tag, 需要真实 OpenSandbox 实例

  **Must NOT do**:
  - 不做 sandbox 创建/启动 — 假设 sandbox 已运行
  - 不做 SDK 层集成 — 直接用 HTTP

  **Recommended Agent Profile**:
  - **Category**: `unspecified-high`
  - **Skills**: [`golang-patterns`, `golang-testing`]

  **Parallelization**:
  - **Can Run In Parallel**: YES
  - **Parallel Group**: Wave 3 (with Tasks 8, 9)
  - **Blocks**: None
  - **Blocked By**: Task 2

  **References**:
  - OpenSandbox execd API: `https://github.com/alibaba/OpenSandbox` — execd API docs (port 44772), POST /command + SSE
  - OpenSandbox 架构: 研究报告中的 Execution Plane 架构分析

  **Acceptance Criteria**:
  - [ ] `SandboxProvider` 在 `pkg/provider/sandbox.go` 中实现
  - [ ] `go test ./pkg/provider/... -v -race -run TestSandbox` 通过
  - [ ] 集成测试在 `pkg/provider/sandbox_integration_test.go`
  - [ ] 如果 execd 不支持交互式 stdin, 有清晰的文档说明和错误处理

  **QA Scenarios:**
  ```
  Scenario: Sandbox provider 单元测试
    Tool: Bash
    Steps:
      1. go test ./pkg/provider/... -v -race -run TestSandboxProvider
    Expected Result: PASS, mock execd HTTP server 上的命令执行流程正确
    Evidence: .sisyphus/evidence/task-10-sandbox-unit.txt
  ```

  **Commit**: YES
  - Message: `feat(provider/sandbox): implement OpenSandbox execd provider`
  - Files: `pkg/provider/sandbox.go, pkg/provider/sandbox_test.go, pkg/provider/sandbox_integration_test.go`
  - Pre-commit: `go test ./pkg/provider/... -race`

- [ ] 11. CLI Serve Subcommand + End-to-End Wiring

  **What to do**:
  - 实现 `serve` 子命令:
    - CLI flags: `--listen :8080`, `--token secret`, `--default-provider local`, `--default-command ./mock-agent` (默认 provider 配置)
    - 创建 HTTP server + 注册路由 (transport 层)
    - 应用 auth 中间件
    - ACP 请求生命周期设计:
      - `initialize`: **由 acp-remote 本地处理**，返回 acp-remote 的 capabilities (不需要后端 agent)
      - `session/new`: 解析 `_meta` 中的 provider 配置 (如果有)，否则使用默认 provider + command。通过 ExecProvider 启动 agent 进程，然后将 `initialize` + `session/new` 一起转发给 agent
      - `session/prompt`, `session/cancel`: 转发给已建立的 agent 连接
    - Session manager 串联所有组件
    - 处理 SIGINT/SIGTERM 优雅关闭
  - 完善 main.go: 注册 serve 和 stdio 两个子命令 (flag.NewFlagSet 或 简单 os.Args 分发)
  - End-to-end 测试: 启动 serve → curl initialize → curl session/new → curl prompt → SSE events

  **Must NOT do**:
  - 不做 graceful shutdown beyond basic signal handling
  - 不做 process manager/daemonize

  **Recommended Agent Profile**:
  - **Category**: `deep`
  - **Skills**: [`golang-patterns`, `golang-testing`]

  **Parallelization**:
  - **Can Run In Parallel**: NO (integration)
  - **Parallel Group**: Wave 4
  - **Blocks**: Task 12
  - **Blocked By**: Tasks 5, 6, 7

  **References**:
  - openworkspace server: `/home/ubuntu/tmp/opencodeworkspace/openworkspace/pkg/http/server.go` — HTTP server 初始化, RegisterRoutes, Shutdown 模式
  - openworkspace 集成测试: `/home/ubuntu/tmp/opencodeworkspace/openworkspace/tests/acp_test.go` — 完整的 end-to-end 测试 (testEnv 设置, httptest.NewServer, JSON-RPC 调用, SSE 订阅)

  **Acceptance Criteria**:
  - [ ] `cmd/acp-remote/serve.go` 实现 serve 子命令
  - [ ] `cmd/acp-remote/main.go` 支持 serve/stdio 子命令分发
  - [ ] `go build ./cmd/acp-remote` 编译成功
  - [ ] End-to-end: `curl /acp/message` initialize 正确响应
  - [ ] Auth: 无 token 请求返回 401

  **QA Scenarios:**
  ```
  Scenario: HTTP serve 模式 end-to-end (使用 mock agent)
    Tool: Bash
    Preconditions: mock agent binary 已构建 (bin/mock-agent)
    Steps:
      1. go build -o bin/acp-remote ./cmd/acp-remote && go build -o bin/mock-agent ./testdata/mock_agent/
      2. ./bin/acp-remote serve --listen :18080 --token test-secret --default-provider local --default-command ./bin/mock-agent &
      3. sleep 1
      4. curl -sf -H 'Authorization: Bearer test-secret' -H 'Content-Type: application/json' -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":1,"clientCapabilities":{}}}' http://localhost:18080/acp/message
      5. kill %1
    Expected Result: step 4 返回 JSON-RPC response with protocolVersion (acp-remote 本地处理 initialize)
    Evidence: .sisyphus/evidence/task-11-serve-e2e.txt

  Scenario: Auth 拒绝无 token 请求
    Tool: Bash
    Steps:
      1. ./bin/acp-remote serve --listen :18081 --token test-secret --default-provider local --default-command ./bin/mock-agent &
      2. sleep 1
      3. curl -sf -o /dev/null -w '%{http_code}' http://localhost:18081/acp/message
      4. kill %1
    Expected Result: step 3 返回 401
    Evidence: .sisyphus/evidence/task-11-auth-reject.txt
  ```

  **Commit**: YES
  - Message: `feat(cmd/serve): implement HTTP serve mode with end-to-end wiring`
  - Files: `cmd/acp-remote/serve.go, cmd/acp-remote/main.go`
  - Pre-commit: `go test ./... -race`

- [ ] 12. README + Architecture Docs

  **What to do**:
  - 撰写 README.md:
    - 项目简介 + 架构图 (ASCII art)
    - 安装: `go install` + `go get` (库模式)
    - 快速开始: stdio 模式 + serve 模式示例
    - ExecProvider 列表和配置说明
    - ACP 方法支持矩阵 (支持/stub/不支持)
    - OpenSandbox 对接示例 + OpenClaw 对接示例
    - 开发: make test/build/vet

  **Must NOT do**:
  - 不过度文档化 — 保持简洁实用

  **Recommended Agent Profile**:
  - **Category**: `writing`
  - **Skills**: []

  **Parallelization**:
  - **Can Run In Parallel**: NO
  - **Parallel Group**: Wave 4 (after Task 11)
  - **Blocks**: None
  - **Blocked By**: Task 11

  **Acceptance Criteria**:
  - [ ] README.md 存在且包含: 项目简介, 架构图, 安装, 快速开始, Provider 列表, ACP 方法矩阵

  **QA Scenarios:**
  ```
  Scenario: README 完整性
    Tool: Bash
    Steps:
      1. grep -c '## ' README.md
    Expected Result: 至少 6 个 section headers
    Evidence: .sisyphus/evidence/task-12-readme.txt
  ```

  **Commit**: YES
  - Message: `docs: README with architecture, usage, and examples`
  - Files: `README.md`
  - Pre-commit: —

---

## Final Verification Wave (MANDATORY — after ALL implementation tasks)

> 4 review agents run in PARALLEL. ALL must APPROVE. Present consolidated results to user and get explicit "okay" before completing.

- [ ] F1. **Plan Compliance Audit** — `oracle`
  Read the plan end-to-end. For each "Must Have": verify implementation exists (`go test`, read file, curl endpoint). For each "Must NOT Have": search codebase for forbidden patterns — reject with file:line if found. Check evidence files exist in `.sisyphus/evidence/`. Compare deliverables against plan.
  Output: `Must Have [N/N] | Must NOT Have [N/N] | Tasks [N/N] | VERDICT: APPROVE/REJECT`

- [ ] F2. **Code Quality Review** — `unspecified-high`
  Run `go vet ./...` + `golangci-lint run` + `go test ./... -race`. Review all files for: `time.Sleep` in non-test code, custom JSON-RPC types, unused imports, empty error handlers, `any` type abuse. Check Go patterns: error wrapping, context propagation, interface compliance.
  Output: `Build [PASS/FAIL] | Vet [PASS/FAIL] | Tests [N pass/N fail] | Files [N clean/N issues] | VERDICT`

- [ ] F3. **Real Manual QA** — `unspecified-high`
  Start from clean state. Build binary. Test stdio mode with echo agent (full JSON-RPC conversation). Test serve mode with curl (initialize → session/new → prompt → SSE events). Test auth rejection. Test agent crash handling. Save to `.sisyphus/evidence/final-qa/`.
  Output: `Scenarios [N/N pass] | Integration [N/N] | Edge Cases [N tested] | VERDICT`

- [ ] F4. **Scope Fidelity Check** — `deep`
  For each task: read "What to do", read actual diff (`git log`/`git diff`). Verify 1:1 — everything in spec was built (no missing), nothing beyond spec was built (no creep). Check "Must NOT do" compliance. Detect cross-task contamination. Flag unaccounted changes.
  Output: `Tasks [N/N compliant] | Contamination [CLEAN/N issues] | Unaccounted [CLEAN/N files] | VERDICT`

---

## Commit Strategy

| Task | Commit Message | Files | Pre-commit |
|------|---------------|-------|------------|
| 1 | `init: project skeleton with go.mod, Makefile, package dirs` | go.mod, Makefile, pkg/*/doc.go, cmd/ | `go vet ./...` |
| 2 | `feat(provider): define ExecProvider interface and local subprocess impl` | pkg/provider/*.go | `go test ./pkg/provider/... -race` |
| 3 | `feat(session): implement session manager with state machine` | pkg/session/*.go | `go test ./pkg/session/... -race` |
| 4 | `feat(auth): add bearer token HTTP middleware` | pkg/auth/*.go | `go test ./pkg/auth/... -race` |
| 5 | `feat(proxy): implement bidirectional ACP bridge` | pkg/proxy/*.go | `go test ./pkg/proxy/... -race` |
| 6 | `feat(transport): implement HTTP+SSE server transport` | pkg/transport/*.go | `go test ./pkg/transport/... -race` |
| 7 | `feat(cmd/stdio): implement stdio transport mode` | cmd/acp-remote/stdio.go | `go test ./... -race` |
| 8 | `feat(provider/k8s): implement Kubernetes exec provider` | pkg/provider/k8s*.go | `go test ./pkg/provider/... -race` |
| 9 | `feat(provider/docker): implement Docker exec provider` | pkg/provider/docker*.go | `go test ./pkg/provider/... -race` |
| 10 | `feat(provider/sandbox): implement OpenSandbox execd provider` | pkg/provider/sandbox*.go | `go test ./pkg/provider/... -race` |
| 11 | `feat(cmd/serve): implement HTTP serve mode with end-to-end wiring` | cmd/acp-remote/serve.go, main.go | `go test ./... -race` |
| 12 | `docs: README with architecture, usage, and examples` | README.md | — |

---

## Success Criteria

### Verification Commands
```bash
# Build
go build -o bin/acp-remote ./cmd/acp-remote  # Expected: binary at bin/acp-remote

# All tests pass with race detection
go test ./... -v -count=1 -race  # Expected: all PASS

# Static analysis
go vet ./...  # Expected: no errors

# stdio mode smoke test (with mock agent)
go build -o bin/mock-agent ./testdata/mock_agent/
echo '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":1,"clientCapabilities":{}}}' \
  | ./bin/acp-remote stdio --provider local --command ./bin/mock-agent
# Expected: JSON response with protocolVersion (from mock agent)

# HTTP serve mode smoke test (with mock agent and default provider)
./bin/acp-remote serve --listen :18080 --token test-secret --default-provider local --default-command ./bin/mock-agent &
sleep 1
curl -sf -H "Authorization: Bearer test-secret" \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":1,"clientCapabilities":{}}}' \
  http://localhost:18080/acp/message
# Expected: {"jsonrpc":"2.0","id":1,"result":{...}} (acp-remote 本地处理 initialize)

# Auth rejection
curl -sf -o /dev/null -w "%{http_code}" \
  http://localhost:18080/acp/message
# Expected: 401
```

### Final Checklist
- [ ] All "Must Have" present
- [ ] All "Must NOT Have" absent
- [ ] All tests pass with `-race`
- [ ] stdio mode works end-to-end
- [ ] HTTP+SSE mode works end-to-end
- [ ] Permission forwarding works
- [ ] 4 ExecProvider implementations compile and test
