# Claude Code CLI 源码研究 — 对 AIOps Codex 的优化启示

> 基于对 Claude Code CLI（TypeScript/Bun）源码的深度分析，提炼出可直接应用于 aiops-codex（Go/gRPC）项目的优化方向。所有条目按对项目的重要性排序，每条说明：Claude Code 怎么做的、对 aiops-codex 有什么价值、如何落地。

---

## 项目对比概览

| 维度 | Claude Code CLI | AIOps Codex |
|------|----------------|-------------|
| 语言 | TypeScript (Bun runtime) | Go + Vue.js |
| 架构 | 本地 CLI Agent + 流式对话引擎 | 分布式编排器 + gRPC Agent + Web UI |
| 核心模式 | QueryEngine → Tool → Permission | Manager → Dispatcher → Collector |
| 状态管理 | AppState + FileHistoryState | Store (内存) + JSON 持久化 |
| 工具系统 | 可扩展 Tool 接口 + 动态发现 | Dynamic Tools + Agent Profile |

---

## P0：必须做——直接影响生产可用性和成本

### 1. 分层错误处理与分类重试

**Claude Code 怎么做的：**
`services/api/errors.ts` 将所有 API 错误分为 rate_limit / overloaded / network / token_revoked / prompt_too_long 等类别，每种类别有独立的重试策略、退避时间和用户提示。

**对 aiops-codex 的价值：**
当前 `internal/codex/client.go` 的 RPC 客户端对所有错误一视同仁。一个 rate_limit 错误和一个认证失败错误走同样的处理路径，导致：不该重试的错误在重试（浪费时间），该重试的错误没有退避（加剧限流）。在 Mission 场景下，8 个 Worker 同时触发 rate_limit 但没有退避，会形成"限流风暴"。

**落地方式：**

```go
// internal/codex/errors.go — 新增文件，约 60 行
type ErrorCategory string
const (
    ErrCategoryRetryable  ErrorCategory = "retryable"   // 网络抖动、临时过载
    ErrCategoryRateLimit  ErrorCategory = "rate_limit"   // 需要指数退避
    ErrCategoryFatal      ErrorCategory = "fatal"        // 认证失败、配置错误，不重试
    ErrCategoryUserAction ErrorCategory = "user_action"  // 需要用户介入（如 token 过期）
)
type CategorizedError struct {
    Category    ErrorCategory
    Original    error
    RetryAfter  time.Duration
    UserMessage string
}
func CategorizeRPCError(err error) *CategorizedError {
    // 根据 gRPC status code 分类：
    // codes.Unavailable → Retryable, codes.ResourceExhausted → RateLimit
    // codes.Unauthenticated → Fatal, codes.InvalidArgument → Fatal
}
```

改动点：`client.go` 的 `Request` 方法包一层 `RequestWithRetry`，`server.go` 的 turn 处理根据 category 决定是否重试、是否通知前端。预计改动 3 个文件，约 100 行。

---

### 2. Token 成本追踪与预算控制

**Claude Code 怎么做的：**
`cost-tracker.ts` 每个 turn 记录 input/output/cache token 用量，实时计算累计成本。`tokenBudget.ts` 允许用户设置 token 预算（如 `+500k`），到达预算时自动停止。支持 session 级别的成本持久化和恢复。

**对 aiops-codex 的价值：**
当前 `orchestrator_budget.go` 只控制并发数量（`globalActiveBudget=32`），没有 token/成本维度。生产环境中一个失控的 Mission 可能消耗大量 API token 而无人察觉。这是真金白银的风险。

**落地方式：**

```go
// internal/orchestrator/cost_tracker.go — 新增文件，约 80 行
type MissionCost struct {
    MissionID    string
    InputTokens  int64
    OutputTokens int64
    TotalCostUSD float64
    Budget       float64   // 0 = 无限制
    UpdatedAt    time.Time
}
type CostTracker struct {
    mu       sync.Mutex
    missions map[string]*MissionCost
}
func (t *CostTracker) RecordUsage(missionID string, usage TokenUsage) error {
    // 累加 token，计算成本，超预算返回 ErrBudgetExceeded
}
```

改动点：`server.go` turn 完成回调中调用 `RecordUsage`；WebSocket 推送附带成本信息；前端 `ChatPage.vue` 展示实时成本。Mission 创建时可设置 budget。预计改动 4 个文件，约 150 行。

---

### 3. Turn 生命周期的状态机化

**Claude Code 怎么做的：**
`server.go` 中的 `turnTrace` 结构体精确记录每个 turn 的生命周期：requestStartedAt → threadStartedAt → turnStartedAt → firstItemStartedAt → firstAssistantAt。每个阶段都有时间戳和 ID 追踪。

**对 aiops-codex 的价值：**
当前 `server.go` 的 turn 管理分散在 `turnMu`/`turnCancels`/`turnTraces` 等多个 mutex 保护的 map 中，状态转换是隐式的。这导致：并发 bug 难以排查（不知道 turn 卡在哪个阶段）；`stalledTurnTimeout` 只能粗暴超时，无法区分"正在等待模型响应"和"工具执行卡住"。

**落地方式：**

```go
// internal/server/turn_state.go — 新增文件，约 120 行
type TurnPhase string
const (
    TurnPhaseInit      TurnPhase = "init"
    TurnPhaseQueued    TurnPhase = "queued"
    TurnPhaseRunning   TurnPhase = "running"
    TurnPhaseStreaming  TurnPhase = "streaming"
    TurnPhaseCompleting TurnPhase = "completing"
    TurnPhaseDone      TurnPhase = "done"
    TurnPhaseFailed    TurnPhase = "failed"
)
type TurnStateMachine struct {
    mu        sync.Mutex
    sessionID string
    turnID    string
    phase     TurnPhase
    trace     TurnTrace  // 每个阶段的时间戳
    cancel    context.CancelFunc
}
func (t *TurnStateMachine) Transition(to TurnPhase) error {
    // 校验合法转换，记录时间戳，非法转换返回 error 而非静默
}
```

改动点：将 `server.go` 中的 `turnMu`/`turnCancels`/`turnTraces` 合并为 `map[string]*TurnStateMachine`。`stalledTurnTimeout` 可以根据 phase 设置不同超时。预计改动 1 个文件（server.go），约 200 行重构。

---

### 4. 模型降级与 Fallback 策略

**Claude Code 怎么做的：**
`query.ts` 的 queryLoop 中：主模型请求失败 → 自动切换 fallbackModel → 清理 orphaned messages（tombstone 机制）→ 重建 StreamingToolExecutor → 通知用户降级。整个过程对用户透明。

**对 aiops-codex 的价值：**
当前 `session_runtime.go` 使用固定的 `profile.Runtime.Model`，模型不可用时整个 turn 失败。对 Mission 场景影响巨大——一个 8-worker Mission 如果因模型限流全部失败，所有任务都要重新排队。Fallback 可以将 Mission 可用性从 ~95% 提升到 ~99%。

**落地方式：**

```go
// 在 model/types.go 的 RuntimeConfig 中增加一个字段
type RuntimeConfig struct {
    Model         string `json:"model"`
    FallbackModel string `json:"fallbackModel,omitempty"` // 新增
    // ...existing fields
}
// internal/server/session_runtime.go 中
func (a *App) resolveModel(profile model.AgentProfile, attempt int) string {
    if attempt == 0 || profile.Runtime.FallbackModel == "" {
        return profile.Runtime.Model
    }
    return profile.Runtime.FallbackModel
}
```

改动点：`model/types.go` 加字段；`session_runtime.go` 的 `buildXxxThreadStartSpec` 使用 `resolveModel`；codex client turn 失败时检查错误类型，rate_limit/overloaded 用 fallback 重试。预计改动 3 个文件，约 50 行。

---

### 5. 流式工具并行执行

**Claude Code 怎么做的：**
`StreamingToolExecutor` 在模型流式输出的同时并行执行工具调用。区分 concurrency-safe 工具（read file）和 unsafe 工具（write file），safe 的并行执行，unsafe 的串行排队。

**对 aiops-codex 的价值：**
当前 Worker turn 是串行的：发送 turn → 等完整响应 → 处理工具调用 → 下一个 turn。如果 AI 同时要读 3 个远程文件，就是 3 次串行的远程读取。并行执行可以将 turn 延迟从 `N × 单次延迟` 降低到 `max(单次延迟)`，在远程主机网络延迟 200ms+ 时效果显著。

**落地方式：**

```go
// internal/server/tool_executor.go — 新增文件，约 100 行
type ToolExecutor struct {
    mu          sync.Mutex
    maxParallel int
    running     int
    queue       []*ToolCall
    results     chan *ToolResult
}
func (e *ToolExecutor) Submit(call *ToolCall) {
    e.mu.Lock()
    defer e.mu.Unlock()
    if call.ReadOnly && e.running < e.maxParallel {
        e.running++
        go e.execute(call)  // 只读工具立即并行执行
    } else {
        e.queue = append(e.queue, call)  // 写入工具排队串行
    }
}
```

改动点：`handleDynamicToolCall` 中将 `remote_read_file`/`remote_list_files`/`remote_search_files` 标记为 ReadOnly，通过 ToolExecutor 并行分发。写入工具保持串行。预计改动 2 个文件，约 150 行。

---

### 6. 熔断器与细粒度限流

**Claude Code 怎么做的：**
queryLoop 中有多层保护：`maxTurns` 限制最大 turn 数；`maxOutputTokensRecoveryCount` 限制恢复次数（最多 3 次）；`autoCompactTracking.consecutiveFailures` 作为熔断器——连续失败后停止重试。

**对 aiops-codex 的价值：**
当前 `orchestrator_budget.go` 的限流是全局维度的（`4 thread/s`、`8 turn/s`），缺少 per-mission 和 per-worker 的熔断。一个 Worker 连续失败 10 次，系统仍然会以全速重试，浪费 API 调用并可能加剧上游压力。

**落地方式：**

```go
// internal/orchestrator/circuit_breaker.go — 新增文件，约 60 行
type CircuitBreaker struct {
    ConsecutiveFailures int
    LastFailure         time.Time
    State               string // "closed" | "open" | "half_open"
    CooldownDuration    time.Duration
}
func (cb *CircuitBreaker) Allow() bool {
    if cb.State == "closed" { return true }
    if cb.State == "open" && time.Since(cb.LastFailure) > cb.CooldownDuration {
        cb.State = "half_open"
        return true
    }
    return cb.State == "half_open"
}
func (cb *CircuitBreaker) RecordFailure() {
    cb.ConsecutiveFailures++
    cb.LastFailure = time.Now()
    if cb.ConsecutiveFailures >= 3 { cb.State = "open" }
}
```

改动点：在 `Dispatcher.Dispatch` 和 `acquireOrchestratorPermit` 中为每个 worker 维护一个 CircuitBreaker。连续失败 3 次进入 cooldown（默认 30s），half_open 时只允许一个试探请求。预计改动 2 个文件，约 80 行。

---

## P1：应该做——显著提升用户体验和工程质量

### 7. 活动追踪与实时 UI 反馈

**Claude Code 怎么做的：**
`activityManager.ts` 追踪用户活动 vs CLI 活动。tool 执行层面发出细粒度 progress 事件（正在读文件、正在搜索、正在执行命令），前端实时展示。

**对 aiops-codex 的价值：**
当前用户在 Web UI 看到的是 ThinkingCard "正在思考"，然后突然出现结果卡片，中间过程是黑盒。`ChatPage.vue` 已经有了 `activeActivityLine`/`currentReadingFile`/`currentSearchQuery` 等展示逻辑，但数据来源是前端推断而非后端推送。只需后端补上 activity 事件推送，前端几乎不用改。

**落地方式：**

```go
// internal/server/activity.go — 新增文件，约 40 行
type ActivityEvent struct {
    SessionID string `json:"sessionId"`
    Kind      string `json:"kind"`   // "reading" | "searching" | "executing" | "writing"
    Target    string `json:"target"` // 文件路径、搜索词、命令
    Phase     string `json:"phase"`  // "start" | "end"
}
func (a *App) emitActivity(sessionID string, event ActivityEvent) {
    a.broadcastWS(sessionID, map[string]any{"type": "activity", "payload": event})
}
```

改动点：在 `dynamic_tools.go` 的 `executeRemoteReadFileTool`/`executeRemoteSearchFilesTool`/`executeApprovedRemoteMutation` 等入口和出口各加一行 `emitActivity` 调用。前端 store 接收 activity 事件更新 `runtime.activity` 字段。预计改动 2 个文件，约 60 行。

---

### 8. 优雅关闭（Graceful Shutdown）

**Claude Code 怎么做的：**
`gracefulShutdown.ts` 分阶段关闭：停止接受新请求 → 等待进行中 turn 完成（带超时）→ 保存会话状态 → 清理资源。`cleanupRegistry.ts` 让每个资源在创建时注册清理函数，shutdown 时统一执行。

**对 aiops-codex 的价值：**
当前 aiops-codex 的关闭逻辑简单——直接 kill。正在执行的 turn 会丢失，terminal session 可能残留，状态可能不一致。在生产环境滚动更新时，这意味着用户正在进行的对话会中断。

**落地方式：**

```go
// internal/server/shutdown.go — 新增文件，约 60 行
func (a *App) GracefulShutdown(ctx context.Context) error {
    // Phase 1: 停止接受新连接
    a.httpServer.SetKeepAlivesEnabled(false)
    // Phase 2: 等待活跃 turn 完成（最多 30s）
    a.drainActiveTurns(ctx)
    // Phase 3: 断开 agent 连接（发送 graceful disconnect）
    a.disconnectAgents(ctx)
    // Phase 4: 持久化状态
    a.store.SaveStableState(a.cfg.StatePath)
    // Phase 5: 关闭服务器
    a.grpcServer.GracefulStop()
    return a.httpServer.Shutdown(ctx)
}

// internal/server/cleanup.go — 新增文件，约 40 行
type CleanupRegistry struct {
    mu       sync.Mutex
    cleanups []func(context.Context) error
}
func (r *CleanupRegistry) Register(fn func(context.Context) error) func() {
    // 返回取消注册函数
}
func (r *CleanupRegistry) RunAll(ctx context.Context) []error {
    // 执行所有注册的清理函数
}
```

改动点：`cmd/` 的 main 函数中捕获 SIGTERM/SIGINT，调用 `GracefulShutdown`。`createTerminalSession`/`createRemoteTerminalSession` 中注册清理函数。预计改动 3 个文件，约 120 行。

---

### 9. server.go 的职责拆分

**Claude Code 怎么做的：**
Claude Code 将关注点严格分离：`query.ts`（对话循环）、`Tool.ts`（工具定义）、`hooks.ts`（事件钩子）、`state/AppState.tsx`（状态管理）各司其职。

**对 aiops-codex 的价值：**
当前 `server.go` 承载了 HTTP 路由、WebSocket 管理、gRPC 处理、turn 管理、认证、OAuth 等所有职责，是项目中最大的单文件。新功能开发时经常要在这个文件中多处修改，合并冲突频繁，代码审查困难。

**落地方式：**

```
internal/server/
├── server.go           // 仅保留 App 结构体定义和 Start/Stop
├── routes.go           // HTTP 路由注册（从 server.go 抽出）
├── websocket.go        // WebSocket 管理（从 server.go 抽出）
├── turn.go             // Turn 生命周期（从 server.go 抽出）
├── auth.go             // Session/OAuth 认证（从 server.go 抽出）
├── shutdown.go         // 优雅关闭（新增）
└── ...existing files   // 保持不变
```

纯重构，不改变行为。预计从 server.go 中抽出 4 个文件，每个 200-400 行。

---

### 10. Store 层的接口抽象

**Claude Code 怎么做的：**
`sessionStorage.ts` 通过接口抽象存储层，支持文件存储和内存存储两种实现，方便测试和切换。

**对 aiops-codex 的价值：**
当前 `internal/store/memory.go` 是纯内存实现，1300+ 行直接操作 map。所有测试都依赖真实的 Store 实例，无法 mock。未来如果要切换到 Redis/数据库（多实例部署必需），改动面巨大。

**落地方式：**

```go
// internal/store/interfaces.go — 新增文件，约 40 行
type SessionStore interface {
    EnsureSession(sessionID string) *SessionState
    Session(sessionID string) *SessionState
    UpsertCard(sessionID string, card model.Card)
    UpdateCard(sessionID, cardID string, fn func(*model.Card))
    // ...
}
type HostStore interface {
    UpsertHost(host model.Host)
    Hosts() []model.Host
    MarkHostOffline(hostID string)
    MarkStaleHosts(timeout time.Duration) []string
}
```

改动点：抽取接口定义，`memory.go` 的 `Store` 实现这些接口，`server.go` 中的 `App.store` 字段类型改为接口。预计改动 3 个文件，约 80 行。为未来 Redis 实现和测试 mock 打下基础。

---

### 11. 配置验证前置

**Claude Code 怎么做的：**
`utils/envValidation.ts` 在启动时做完整的配置验证，不合法的配置直接报错退出，而不是运行时才发现。

**对 aiops-codex 的价值：**
当前 `config.go` 的 `Load()` 只读取环境变量，验证分散在各处。比如 `SessionSecret = "dev-insecure-session-secret"` 在 production 模式下是安全隐患，但只有 `ValidateHostAgentSecurity` 会检查部分配置。用户可能带着不安全的配置上线而不自知。

**落地方式：**

```go
// internal/config/config.go 中增加 Validate 方法，约 30 行
func (c Config) Validate() []error {
    var errs []error
    if c.SessionSecret == "dev-insecure-session-secret" &&
       c.HostAgentSecurityProfile == "production" {
        errs = append(errs, fmt.Errorf("production requires a secure session secret"))
    }
    if err := c.ValidateHostAgentSecurity(); err != nil {
        errs = append(errs, err)
    }
    // 检查 CodexPath 是否存在、StatePath 目录是否可写等
    return errs
}
```

改动点：`cmd/` 的 main 函数中 `config.Load()` 后立即调用 `Validate()`，有错误则打印并退出。预计改动 2 个文件，约 40 行。

---

## P2：值得做——提升系统能力上限

### 12. 上下文压缩（Context Compaction）

**Claude Code 怎么做的：**
`services/compact/` 自动检测 token 接近阈值时触发压缩，按 API 轮次分组消息，将旧消息压缩为摘要，保留关键上下文语义。支持 micro-compact（增量缓存编辑）和 reactive compact（413 错误后紧急压缩）。

**对 aiops-codex 的价值：**
当前 orchestrator 的 `DefaultEventWindowSize = 128` 是简单的滑动窗口——超过 128 个事件就丢弃最早的。对长时间运行的 Mission（几十个 task、上百个事件），早期的关键上下文（如初始计划、重要决策）会被丢失，导致后续 Worker 缺少背景信息。

**落地方式：**

```go
// internal/orchestrator/compactor.go — 新增文件，约 80 行
type EventCompactor struct {
    maxEvents int
}
func (c *EventCompactor) Compact(events []RelayEvent) []RelayEvent {
    if len(events) <= c.maxEvents { return events }
    cutoff := len(events) - c.maxEvents/2
    oldEvents := events[:cutoff]
    summary := summarizeEvents(oldEvents) // 将旧事件合并为一条摘要
    compacted := make([]RelayEvent, 0, c.maxEvents/2+1)
    compacted = append(compacted, RelayEvent{Type: EventTypeSnapshot, Summary: summary})
    compacted = append(compacted, events[cutoff:]...)
    return compacted
}
```

改动点：在 `store.go` 的 `UpdateMission` 中，事件数超过阈值时调用 Compact。摘要可以先用简单的文本拼接，后续升级为 LLM 生成。预计改动 2 个文件，约 100 行。

---

### 13. Hook 系统（事件驱动扩展）

**Claude Code 怎么做的：**
`utils/hooks.ts`（5000+ 行）支持 pre/post tool use、session start/end、compact 前后等 20+ 事件点。Hook 可以阻断操作（blocking）或仅观察。支持 HTTP webhook 和本地命令两种执行方式，内置去重和超时。

**对 aiops-codex 的价值：**
当前事件流是单向的（Collector 收集 → WebSocket 推送），用户无法在事件发生时插入自定义逻辑。比如：无法在远程命令执行前做安全审计、无法在 Mission 完成后触发 CI/CD、无法在文件变更前做合规检查。Hook 系统让平台从"工具"变成"平台"。

**落地方式：**

```go
// internal/server/hooks.go — 新增文件，约 150 行
type HookEvent string
const (
    HookPreRemoteExec    HookEvent = "pre_remote_exec"
    HookPostRemoteExec   HookEvent = "post_remote_exec"
    HookPreFileChange    HookEvent = "pre_file_change"
    HookPostTaskComplete HookEvent = "post_task_complete"
    HookMissionComplete  HookEvent = "mission_complete"
)
type HookHandler struct {
    Event    HookEvent
    URL      string        // Webhook URL
    Blocking bool          // 是否阻断操作
    Timeout  time.Duration
}
type HookEngine struct {
    handlers map[HookEvent][]HookHandler
}
func (e *HookEngine) Execute(event HookEvent, payload map[string]any) (*HookResult, error) {
    // blocking handler 返回 deny 时阻止操作
}
```

改动点：在 `dynamic_tools.go` 的关键路径（远程执行、文件变更）前后调用 `HookEngine.Execute`。Hook 配置可以存在 AgentProfile 中或独立的配置文件中。预计新增 1 个文件 + 改动 2 个文件，约 250 行。

---

### 14. 文件操作历史与回滚

**Claude Code 怎么做的：**
`utils/fileHistory.ts` 每次 tool 修改文件前自动创建备份，支持按 turn 粒度回滚（rewind），跟踪文件变更的 diff stats。用户可以用 `/rewind` 命令撤销 AI 的修改。

**对 aiops-codex 的价值：**
当前远程文件操作（`remote_files.go`）是不可逆的。如果 AI 修改了远程主机上的配置文件导致服务异常，没有快速回滚手段。在多 Worker 并行修改文件的 Mission 场景下，某个 Worker 的修改出错需要精确回滚，而不是回滚所有 Worker 的修改。

**落地方式：**

```go
// internal/server/file_history.go — 新增文件，约 100 行
type FileSnapshot struct {
    SessionID string; TaskID string; HostID string
    FilePath  string; Content []byte; Hash string
    CreatedAt time.Time
}
type FileHistoryTracker struct {
    snapshots map[string][]FileSnapshot // key: sessionID
}
func (t *FileHistoryTracker) TrackBeforeEdit(sessionID, taskID, hostID, path string, content []byte)
func (t *FileHistoryTracker) RewindToTask(sessionID, taskID string) []FileSnapshot
```

改动点：在 `executeApprovedRemoteFileChange` 执行前，先通过 agent 读取文件当前内容保存快照。回滚时将快照内容写回。预计新增 1 个文件 + 改动 1 个文件，约 150 行。

---

### 15. Approval 批量处理与自动审批规则

**Claude Code 怎么做的：**
`ToolPermissionContext` 支持 allow/deny/ask 规则，按工具名模式匹配。用户可以设置"所有 read 操作自动批准"、"特定目录下的 write 自动批准"等。

**对 aiops-codex 的价值：**
当前 `ChatPage.vue` 的 approval 是逐个处理的（`activeApprovalCard` 一次只展示一个）。Mission 场景下多个 Worker 同时请求审批，用户需要逐个点击，严重影响效率。已有的 `autoApproveRemoteOperationByPolicy` 只支持 session 级别的 grant，不支持规则化的自动审批。

**落地方式：**

```go
// 在 model/types.go 的 AgentCapabilityPermissions 中增加
type AutoApprovalRule struct {
    Pattern    string   `json:"pattern"`    // "remote_read_*" | "remote_exec_readonly"
    HostIDs    []string `json:"hostIds"`    // 限定主机范围，空=所有
    MaxPerTurn int      `json:"maxPerTurn"` // 单 turn 最多自动批准次数
}
```

改动点：`autoApproveRemoteOperationByPolicy` 中匹配 AutoApprovalRule；前端增加"全部批准同类操作"按钮和规则配置 UI。预计改动 3 个文件，约 100 行。

---

### 16. 虚拟滚动列表

**Claude Code 怎么做的：**
`hooks/useVirtualScroll.ts` + `VirtualMessageList.tsx` 只渲染可视区域内的消息，DOM 节点数恒定在 ~20-30 个。

**对 aiops-codex 的价值：**
当前 `ChatPage.vue` 的 `v-for="card in visibleCards"` 是全量渲染。长时间运行的 Mission 产生几百个 card 时，DOM 节点线性增长，页面明显卡顿。

**落地方式：**
引入 `vue-virtual-scroller`，将 `v-for` 替换为 `<RecycleScroller>` 组件。预计改动 1 个文件 + 1 个依赖，约 30 行。

---

## P3：可以做——锦上添花

### 17. 工具权限的细粒度控制

**Claude Code 怎么做的：**
`ToolPermissionContext` 按工具名/模式匹配 allow/deny/ask 规则，规则来源分层（user → project → managed settings）。

**对 aiops-codex 的价值：**
当前 `agent_profile_policy.go` 的权限是 profile 级别的粗粒度控制。不同 Agent Profile 无法精确控制"允许读文件但禁止执行命令"这样的细粒度策略。

**落地方式：**
在 `AgentCapabilityPermissions` 中增加 `ToolRules []ToolPermissionRule` 字段，在 `dynamic_tools.go` 的工具分发前匹配规则。预计改动 2 个文件，约 60 行。

---

### 18. 跨会话记忆

**Claude Code 怎么做的：**
`memdir/` + `SessionMemory/` 自动从对话中提取关键记忆，按项目/用户/全局分层存储，新会话自动加载相关记忆，支持记忆老化和相关性排序。

**对 aiops-codex 的价值：**
当前每次新会话都是从零开始。系统不记得"上次这台主机的 nginx 配置在 /opt/nginx/conf 下"或"这个用户偏好用 vim 编辑"。对运维场景，积累的上下文知识非常有价值。

**落地方式：**
新增 `internal/store/session_memory.go`，在 turn 完成后提取关键信息存储，新会话开始时加载相关记忆注入 system prompt。预计新增 1 个文件 + 改动 2 个文件，约 200 行。

---

### 19. 终端会话注册表

**Claude Code 怎么做的：**
`concurrentSessions.ts` 用 PID 文件注册每个 session，自动清理 stale session，实时更新状态（busy/idle/waiting），支持 `claude ps` 查看所有活跃 session。

**对 aiops-codex 的价值：**
当前 `terminal.go` 用固定 TTL 过期终端，不区分用户创建和 AI 创建，没有全局视图。终端进程崩溃后 session 对象可能残留。

**落地方式：**
新增 `internal/server/terminal_registry.go`，维护所有终端的状态，定期 sweep 清理已退出的终端，暴露 list API 给前端。预计新增 1 个文件 + 改动 1 个文件，约 100 行。

---

### 20. 审计日志结构化

**Claude Code 怎么做的：**
每个工具调用都有结构化的 telemetry，包含工具名、参数、结果、耗时、风险等级等。

**对 aiops-codex 的价值：**
当前 `auditRemoteToolEvent` 是简单的文本日志。结构化审计日志可以支持：按主机/工具/时间段查询操作历史、异常操作告警、合规审计报告。

**落地方式：**
定义 `AuditEvent` 结构体，替换现有的文本日志写入，输出为 JSON 格式。预计改动 1 个文件，约 40 行。

---

### 21. Thinking 预判增强

**Claude Code 怎么做的：**
queryLoop 在 turn 开始时并行预取相关记忆和技能发现，在模型流式输出的同时完成，不阻塞主流程。

**对 aiops-codex 的价值：**
当前 `ChatPage.vue` 的 `inferThinkingPrelude` 是纯前端关键词匹配。后端推送 tool 执行事件后，前端可以展示更准确的进度提示。

**落地方式：**
在 codex notification handler 中解析 `toolUse/start` 事件，推送给前端。前端已有展示逻辑，几乎不用改。预计改动 1 个文件，约 20 行。

---

## 总览：改动量与收益矩阵

| # | 优化项 | 核心价值 | 改动量 | 优先级 |
|---|--------|---------|--------|--------|
| 1 | 错误分类与重试 | 消除限流风暴，减少无效重试 | ~100 行 | P0 |
| 2 | Token 成本追踪 | 防止生产成本失控 | ~150 行 | P0 |
| 3 | Turn 状态机化 | 消除并发 bug，提升可观测性 | ~200 行 | P0 |
| 4 | 模型 Fallback | Mission 可用性 95%→99% | ~50 行 | P0 |
| 5 | 工具并行执行 | 多工具 turn 延迟降低 50-70% | ~150 行 | P0 |
| 6 | 熔断器限流 | 防止失败 Worker 雪崩 | ~80 行 | P0 |
| 7 | 活动追踪 UI | 用户等待从黑盒变透明 | ~60 行 | P1 |
| 8 | 优雅关闭 | 滚动更新不丢失用户会话 | ~120 行 | P1 |
| 9 | server.go 拆分 | 降低维护成本，减少合并冲突 | 纯重构 | P1 |
| 10 | Store 接口抽象 | 为多实例部署和测试 mock 铺路 | ~80 行 | P1 |
| 11 | 配置验证前置 | 防止不安全配置上线 | ~40 行 | P1 |
| 12 | 上下文压缩 | 长 Mission 保留关键上下文 | ~100 行 | P2 |
| 13 | Hook 系统 | 平台可扩展性质变 | ~250 行 | P2 |
| 14 | 文件操作回滚 | 远程修改可撤销 | ~150 行 | P2 |
| 15 | 批量审批 | Mission 审批效率提升 | ~100 行 | P2 |
| 16 | 虚拟滚动 | 长对话 UI 不卡顿 | ~30 行 | P2 |
| 17 | 工具级权限 | 细粒度安全控制 | ~60 行 | P3 |
| 18 | 跨会话记忆 | 运维知识积累 | ~200 行 | P3 |
| 19 | 终端注册表 | 终端资源可见可控 | ~100 行 | P3 |
| 20 | 审计日志结构化 | 合规审计支持 | ~40 行 | P3 |
| 21 | Thinking 预判 | 感知延迟降低 | ~20 行 | P3 |
