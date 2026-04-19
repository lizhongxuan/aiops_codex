# Bifrost 设计方案 — 替换 Codex Runtime，支持多 LLM

## 目标

1. 底层使用 Bifrost LLM 网关替换 Codex App-Server（Rust 子进程），解除对 OpenAI 的硬绑定
2. 将 Codex Runtime 的核心能力（agent loop、上下文管理、压缩、审批路由等）移植到 ai-server（Go）
3. 最终效果：用户可以使用 OpenAI / Anthropic / Ollama 等任意 LLM 运行 aiops-codex，所有现有功能（Chat、Workspace、审批、动态工具、Coroot 集成等）保持不变

## 当前架构（替换前）

```
用户 → Web UI → ai-server (Go)
                    │
                    │ stdio JSON-RPC
                    ▼
              Codex App-Server (Rust 子进程)
                    │
                    │ HTTPS
                    ▼
              OpenAI API（唯一 LLM）
```

ai-server 只是一个"转发层"：发 `thread/start` + `turn/start`，然后被动接收 Codex 的通知（`item/agentMessage/delta`、`item/completed`、`turn/completed` 等）。所有 LLM 推理、agent loop、上下文管理、本地命令执行都在 Codex 内部完成。

## 目标架构（替换后）

```
用户 → Web UI → ai-server (Go)
                    │
                    ├── Agent Loop（从 Codex 移植）
                    ├── Context Manager（从 Codex 移植）
                    ├── Compact（从 Codex 移植）
                    ├── Tool Registry + Dispatch
                    │
                    │ HTTP API
                    ▼
              Bifrost LLM 网关（Go）
                    │
                    ├── OpenAI Provider
                    ├── Anthropic Provider
                    ├── Ollama Provider
                    └── ... 其他 LLM
```

ai-server 从"转发层"变成"控制层"：自己管理 agent loop、上下文、工具调用，通过 Bifrost 网关调用任意 LLM。

---

## 第一部分：Bifrost LLM 网关

> Bifrost 负责且仅负责一件事：把统一格式的请求发给不同的 LLM，把不同格式的响应转换回统一格式。
> 参考：hermes `providers.py` + `runtime_provider.py` + `anthropic_adapter.py`

### 1.1 Provider 接口

```go
// internal/bifrost/provider.go

type ChatRequest struct {
    Model       string           `json:"model"`
    Messages    []Message        `json:"messages"`
    Tools       []ToolDefinition `json:"tools,omitempty"`
    MaxTokens   int              `json:"max_tokens,omitempty"`
    Temperature float64          `json:"temperature,omitempty"`
    Stream      bool             `json:"stream,omitempty"`
}

type Message struct {
    Role       string      `json:"role"`
    Content    interface{} `json:"content"`
    ToolCalls  []ToolCall  `json:"tool_calls,omitempty"`
    ToolCallID string      `json:"tool_call_id,omitempty"`
}

type ChatResponse struct {
    Message Message `json:"message"`
    Usage   Usage   `json:"usage"`
}

type StreamEvent struct {
    Type  string // "content_delta", "tool_call_delta", "done", "error"
    Delta string
    // tool call 相关字段...
}

type Usage struct {
    PromptTokens     int `json:"prompt_tokens"`
    CompletionTokens int `json:"completion_tokens"`
    CachedTokens     int `json:"cached_tokens,omitempty"`
}

// Provider — 所有 LLM 厂商实现此接口
type Provider interface {
    Name() string
    ChatCompletion(ctx context.Context, req ChatRequest) (*ChatResponse, error)
    StreamChatCompletion(ctx context.Context, req ChatRequest) (<-chan StreamEvent, error)
    SupportsToolCalling() bool
}
```

### 1.2 Provider 实现

**OpenAI（含兼容 API）：** 直接转发，OpenAI 格式就是 canonical format。兼容 vLLM、DeepSeek、Moonshot 等 OpenAI 兼容 API。

**Anthropic：** 参考 hermes `anthropic_adapter.py`（1200+ 行），核心转换：
- system message 提取为顶层 `system` 参数
- `tool_calls` → `tool_use` content blocks
- `tool` role messages → `tool_result` content blocks
- 连续同 role 消息合并（Anthropic 要求严格交替）
- 响应转换回 OpenAI 格式

**Ollama：** 走 OpenAI 兼容 API（`/v1/chat/completions`），base_url 指向本地 Ollama 服务。

### 1.3 Credential Pool

参考 hermes `credential_pool.py`。支持多 API key 轮转 + 限流感知：

```go
// internal/bifrost/credential.go

type Credential struct {
    ID             string
    Provider       string
    APIKey         string
    BaseURL        string
    Status         string    // active, exhausted, disabled
    ExhaustedUntil time.Time
}

type Pool struct {
    credentials []*Credential
    current     int
}

func (p *Pool) Select() (*Credential, error)                          // 返回当前可用凭证
func (p *Pool) MarkExhausted(id string, httpStatus int, retryAfter time.Duration) // 标记限流
```

### 1.4 错误恢复

参考 hermes `run_agent.py` 的五级恢复体系：

```
R1: 即时重试（3 次，指数退避 5s→10s→20s）
R2: 传输层恢复（重建 HTTP 客户端，仅限 TCP/TLS 错误）
R3: 凭证轮转（切换下一个 API key，仅限 429/401）
R4: Provider 降级（切换 fallback provider+model，turn-scoped）
R5: 上下文压缩重试（仅限 context_too_long 错误）
```

### 1.5 成本追踪

```go
// internal/bifrost/usage.go

type UsageRecord struct {
    SessionID    string
    Provider     string
    Model        string
    PromptTokens int
    OutputTokens int
    CostUSD      float64
    Latency      time.Duration
}

type Tracker struct {
    records []UsageRecord
    pricing map[string]ModelPricing // 内置定价表
}
```

### 1.6 Gateway 入口

```go
// internal/bifrost/gateway.go

type Gateway struct {
    providers map[string]Provider
    pool      *Pool
    tracker   *Tracker
    fallbacks []FallbackEntry
}

// ChatCompletion — agent loop 调用此方法，不关心底层用哪个 LLM
func (g *Gateway) ChatCompletion(ctx context.Context, req ChatRequest) (*ChatResponse, error)
func (g *Gateway) StreamChatCompletion(ctx context.Context, req ChatRequest) (<-chan StreamEvent, error)
```

---

## 第二部分：从 Codex Runtime 移植到 ai-server

> 这是最核心的部分。Codex App-Server 当前承担的所有职责，必须在 ai-server 中用 Go 重新实现。
> 参考：codex `codex-rs/core/src/` 源码

### 2.1 Codex 当前职责完整清单

通过分析 `internal/server/server.go` 中所有 `codexRequest()` 调用和 `handleCodexNotification()` / `handleCodexServerRequest()` 处理，Codex 承担以下职责：

| 职责 | 当前实现位置 | 移植后位置 |
|------|------------|-----------|
| LLM 推理 | Codex 内部 → OpenAI API | **Bifrost 网关** |
| Agent Loop（推理→工具→继续） | Codex `codex.rs` run_turn | **ai-server `internal/agentloop/`** |
| 上下文管理（消息历史） | Codex `context_manager/` | **ai-server `internal/agentloop/context.go`** |
| 上下文压缩 | Codex `compact.rs` | **ai-server `internal/agentloop/compact.go`** |
| 线程管理 | Codex `thread_manager.rs` | **ai-server `internal/agentloop/thread.go`** |
| 本地命令执行 | Codex `exec.rs` + sandbox | **host-agent（统一路径）** |
| 本地文件操作 | Codex `apply_patch.rs` | **host-agent（统一路径）** |
| 审批请求路由 | Codex → `requestApproval` → ai-server | **ai-server 直接处理（已有）** |
| 动态工具调用 | Codex → `item/tool/call` → ai-server | **ai-server 直接处理（已有）** |
| 流式消息推送 | Codex → `item/agentMessage/delta` | **ai-server 消费 Bifrost stream** |
| 认证管理 | Codex `login/` + token refresh | **Bifrost 各 provider adapter** |
| Skills 加载 | Codex `skills.rs` | **ai-server（已有 skill 系统）** |

### 2.2 Agent Loop（最大工作项）

**Codex 实现：** `codex.rs` 的 `run_turn()` 约 1500 行 Rust 代码，核心流程：

1. `run_pre_sampling_compact()` — 预采样压缩检查
2. `build_prompt()` — 构建完整 prompt（system + tools + history）
3. `run_sampling_request()` — 调用 LLM API
4. `try_run_sampling_request()` — 带重试的流式 API 调用
5. 流式处理：逐 token 解析 assistant message、tool calls、plan items
6. Plan Mode 特殊处理（`PlanModeStreamState`）
7. 并行 tool call 执行
8. 中断处理（`CancellationToken`）
9. `run_auto_compact()` — 自动压缩检查
10. 循环直到 LLM 不再产生 tool calls

**Go 移植方案：**

```go
// internal/agentloop/loop.go

type Loop struct {
    gateway    *bifrost.Gateway
    toolReg    *ToolRegistry
    compressor *Compressor
    ctxMgr     *ContextManager
}

func (l *Loop) RunTurn(ctx context.Context, session *Session, userInput string) error {
    session.AppendUserMessage(userInput)
    iteration := 0

    for iteration < session.MaxIterations {
        // 1. 预采样压缩（从 Codex run_pre_sampling_compact 移植）
        if l.compressor.ShouldCompress(session.EstimateTokens()) {
            if err := l.compressor.Compact(ctx, session); err != nil {
                log.Printf("pre-sampling compact failed: %v", err)
            }
        }

        // 2. 构建请求
        req := bifrost.ChatRequest{
            Model:    session.Model,
            Messages: session.Messages(),
            Tools:    l.toolReg.Definitions(session.EnabledToolsets),
        }

        // 3. 流式调用 LLM（通过 Bifrost 网关）
        stream, err := l.gateway.StreamChatCompletion(ctx, req)
        if err != nil {
            return err
        }

        // 4. 消费流，实时推送到 WebSocket
        result, err := l.consumeStream(ctx, session, stream)
        if err != nil {
            return err
        }

        // 5. 无 tool calls → 对话结束
        if len(result.ToolCalls) == 0 {
            session.AppendAssistantMessage(result.Content)
            session.BroadcastTurnCompleted()
            return nil
        }

        // 6. 执行 tool calls
        session.AppendAssistantMessageWithTools(result.Content, result.ToolCalls)
        for _, tc := range result.ToolCalls {
            toolResult := l.executeTool(ctx, session, tc)
            session.AppendToolResult(tc.ID, toolResult)
        }

        // 7. 自动压缩检查
        if l.compressor.ShouldCompress(session.EstimateTokens()) {
            l.compressor.Compact(ctx, session)
        }

        iteration++
    }
    return fmt.Errorf("max iterations exceeded")
}

// consumeStream 消费 Bifrost 流式响应，实时推送到前端
func (l *Loop) consumeStream(ctx context.Context, session *Session, stream <-chan bifrost.StreamEvent) (*StreamResult, error) {
    var content strings.Builder
    var toolCalls []bifrost.ToolCall
    cardID := model.NewID("msg")

    // 创建 AssistantMessageCard
    session.UpsertCard(model.Card{
        ID: cardID, Type: "AssistantMessageCard", Status: "inProgress",
    })

    for event := range stream {
        select {
        case <-ctx.Done():
            return nil, ctx.Err()
        default:
        }

        switch event.Type {
        case "content_delta":
            content.WriteString(event.Delta)
            // 实时推送增量到 WebSocket（替代 Codex 的 item/agentMessage/delta）
            session.UpdateCard(cardID, func(c *model.Card) {
                c.Text += event.Delta
            })
            session.BroadcastSnapshot()

        case "tool_call_delta":
            // 累积 tool call 参数
            toolCalls = mergeToolCallDelta(toolCalls, event)

        case "done":
            session.UpdateCard(cardID, func(c *model.Card) {
                c.Status = "completed"
            })
        }
    }

    return &StreamResult{Content: content.String(), ToolCalls: toolCalls}, nil
}

// executeTool 执行单个工具调用
// 复用现有的 dynamic_tools.go 逻辑，只是调用入口从 JSON-RPC 变成直接调用
func (l *Loop) executeTool(ctx context.Context, session *Session, tc bifrost.ToolCall) string {
    args := parseToolArgs(tc.Function.Arguments)

    // 检查是否需要审批（复用现有审批逻辑）
    entry := l.toolReg.Get(tc.Function.Name)
    if entry != nil && entry.RequiresApproval {
        decision := session.RequestApproval(ctx, tc)
        if decision != "accept" {
            return `{"error": "Operation declined by user"}`
        }
    }

    // 广播工具执行开始
    toolCardID := model.NewID("tool")
    session.UpsertCard(model.Card{
        ID: toolCardID, Type: "CommandExecutionCard",
        Command: tc.Function.Name, Status: "inProgress",
    })

    // 执行工具
    result, err := l.toolReg.Dispatch(ctx, tc.Function.Name, args)
    if err != nil {
        session.UpdateCard(toolCardID, func(c *model.Card) {
            c.Status = "failed"
            c.Output = err.Error()
        })
        return fmt.Sprintf(`{"error": "%s"}`, err.Error())
    }

    session.UpdateCard(toolCardID, func(c *model.Card) {
        c.Status = "completed"
        c.Output = result
    })
    session.BroadcastSnapshot()
    return result
}
```

### 2.3 Context Manager

**Codex 实现：** `context_manager/` 管理消息历史，关键功能：
- `ensure_call_outputs_present()` — 每个 tool_call 必须有对应的 tool_result，缺失时补 stub
- `remove_orphan_outputs()` — 移除没有对应 tool_call 的孤立 tool_result
- `strip_images_when_unsupported()` — 模型不支持 vision 时剥离图片
- Token 用量追踪

**Go 移植方案：**

```go
// internal/agentloop/context.go

type ContextManager struct {
    messages  []bifrost.Message
    tokenInfo *TokenUsageInfo
}

// Sanitize 在每次 API 调用前清理消息历史
// 从 Codex context_manager/normalize.rs 移植
func (cm *ContextManager) Sanitize() {
    cm.ensureCallOutputsPresent()
    cm.removeOrphanOutputs()
}

func (cm *ContextManager) ensureCallOutputsPresent() {
    callIDs := make(map[string]bool)
    resultIDs := make(map[string]bool)
    for _, msg := range cm.messages {
        for _, tc := range msg.ToolCalls {
            callIDs[tc.ID] = true
        }
        if msg.Role == "tool" && msg.ToolCallID != "" {
            resultIDs[msg.ToolCallID] = true
        }
    }
    for id := range callIDs {
        if !resultIDs[id] {
            cm.messages = append(cm.messages, bifrost.Message{
                Role: "tool", ToolCallID: id,
                Content: "[Tool execution was interrupted or result was lost]",
            })
        }
    }
}

func (cm *ContextManager) removeOrphanOutputs() {
    callIDs := make(map[string]bool)
    for _, msg := range cm.messages {
        for _, tc := range msg.ToolCalls {
            callIDs[tc.ID] = true
        }
    }
    filtered := cm.messages[:0]
    for _, msg := range cm.messages {
        if msg.Role == "tool" && msg.ToolCallID != "" && !callIDs[msg.ToolCallID] {
            continue
        }
        filtered = append(filtered, msg)
    }
    cm.messages = filtered
}
```

### 2.4 上下文压缩

**Codex 实现：** `compact.rs` + `compact_remote.rs`，两种模式：
- 本地压缩（`run_inline_auto_compact_task`）：用当前模型生成摘要
- 远程压缩（`run_compact_task`）：调用专门的压缩 API

**Claude Code 五层防爆体系**（参考 https://lizhongxuan.github.io/claude-code-study-site/context/five-layers.html）：

```
L1: 源头截断 — 单工具结果 50K 字符上限，超限落盘只给 2KB 预览
L2: 去重 — 文件读取 hash 追踪，未变时返回 stub
L3: 微压缩 — 每轮 API 调用前清理旧的读取类工具结果
L4: 自动压缩 — token > 83% 窗口时触发 9 维结构化摘要
L5: 兜底 — 压缩本身也超限时，按轮次从最旧开始丢弃
```

**Go 移植方案：**

```go
// internal/agentloop/compact.go

const (
    MaxToolResultChars     = 50_000  // L1: 单工具上限
    MaxResultsPerMsgChars  = 200_000 // L1: 单消息聚合上限
    CompactThresholdRatio  = 0.83    // L4: 触发阈值
    CompactBufferTokens    = 13_000  // L4: 缓冲区
    MaxCompactRetries      = 3       // L5: 最大重试
)

type Compressor struct {
    gateway       *bifrost.Gateway
    contextWindow int
    summaryModel  string // 用于生成摘要的便宜模型
}

// ShouldCompress 判断是否需要压缩
func (c *Compressor) ShouldCompress(estimatedTokens int) bool {
    threshold := c.contextWindow - CompactBufferTokens
    return estimatedTokens > int(float64(threshold)*CompactThresholdRatio)
}

// Compact 执行压缩
func (c *Compressor) Compact(ctx context.Context, session *Session) error {
    messages := session.Messages()

    // L1: 裁剪超大工具结果
    c.truncateLargeToolResults(messages)

    // L3: 清理旧的读取类工具结果
    c.microcompact(messages)

    // L4: 生成 9 维结构化摘要
    summary, err := c.generateSummary(ctx, messages)
    if err != nil {
        return err
    }

    // 构建压缩后的消息：system + summary + 最近几轮
    compacted := c.buildCompactedHistory(messages, summary)
    session.ReplaceMessages(compacted)
    return nil
}

// generateSummary 9 维结构化摘要（从 Claude Code compact-intent 移植）
func (c *Compressor) generateSummary(ctx context.Context, messages []bifrost.Message) (string, error) {
    // 收集所有用户消息（从 Codex collect_user_messages 移植）
    userMsgs := collectUserMessages(messages)

    prompt := `Summarize this conversation using these 9 dimensions:
1. Primary Request and Intent — what the user originally asked
2. Key Technical Concepts — important decisions and their rationale
3. Files and Code Sections — specific files/configs mentioned (include exact paths)
4. Errors and Fixes — all errors encountered and how they were resolved
5. Problem Solving — the reasoning chain and diagnostic steps taken
6. All User Messages — preserve EVERY user message verbatim (critical for intent tracking)
7. Pending Tasks — what still needs to be done
8. Current Work — what was happening right before this compression
9. Optional Next Step — include DIRECT QUOTES from the most recent conversation

Think in <analysis> tags first, then output the summary in <summary> tags.
Continue the conversation from where it left off without asking further questions.`

    resp, err := c.gateway.ChatCompletion(ctx, bifrost.ChatRequest{
        Model:    c.summaryModel,
        Messages: []bifrost.Message{
            {Role: "system", Content: prompt},
            {Role: "user", Content: formatMessagesForSummary(messages, userMsgs)},
        },
        MaxTokens: 20_000,
    })
    if err != nil {
        return "", err
    }

    // 提取 <summary> 内容，丢弃 <analysis>
    return extractSummaryTag(resp.Message.Content.(string)), nil
}
```

### 2.5 Tool Registry + Dispatch

**当前状态：** 工具定义散落在 Codex 的 `developerInstructions` 字符串和 `dynamic_tools.go` 中。

**移植后：** 统一的 Tool Registry，所有工具（本地+远程+Coroot）注册到同一个 registry。

```go
// internal/agentloop/tools.go

type ToolEntry struct {
    Name             string
    Description      string
    Parameters       map[string]interface{} // JSON Schema
    Handler          func(ctx context.Context, session *Session, args map[string]interface{}) (string, error)
    RequiresApproval bool
    IsReadOnly       bool
}

type ToolRegistry struct {
    tools map[string]*ToolEntry
}

// 注册所有现有工具（从 dynamic_tools.go 迁移）
func NewToolRegistry(app *App) *ToolRegistry {
    reg := &ToolRegistry{tools: make(map[string]*ToolEntry)}

    // 远程主机工具（从 remoteDynamicTools() 迁移）
    reg.Register(ToolEntry{
        Name: "execute_readonly_query",
        Description: "Execute a read-only shell command on the target host",
        Handler: app.handleReadonlyExec,
        IsReadOnly: true,
    })
    reg.Register(ToolEntry{
        Name: "execute_command",
        Description: "Execute a shell command on the target host (requires approval)",
        Handler: app.handleMutationExec,
        RequiresApproval: true,
    })

    // Coroot 工具（从 corootDynamicTools() 迁移）
    reg.Register(ToolEntry{
        Name: "coroot_list_services",
        Description: "List all services monitored by Coroot",
        Handler: app.handleCorootListServices,
        IsReadOnly: true,
    })

    // Workspace 工具（从 workspaceDynamicTools() 迁移）
    reg.Register(ToolEntry{
        Name: "ask_user_question",
        Description: "Ask the user a clarifying question",
        Handler: app.handleAskUserQuestion,
    })

    return reg
}

// Definitions 返回 OpenAI function calling 格式的工具定义
// Bifrost 的 Anthropic adapter 会自动转换为 Anthropic 格式
func (r *ToolRegistry) Definitions(enabledSets []string) []bifrost.ToolDefinition {
    var defs []bifrost.ToolDefinition
    for _, entry := range r.tools {
        defs = append(defs, bifrost.ToolDefinition{
            Type: "function",
            Function: bifrost.FunctionDef{
                Name:        entry.Name,
                Description: entry.Description,
                Parameters:  entry.Parameters,
            },
        })
    }
    // 按名字排序（保证 prompt cache 稳定性）
    sort.Slice(defs, func(i, j int) bool {
        return defs[i].Function.Name < defs[j].Function.Name
    })
    return defs
}
```

### 2.6 Session 管理（替代 Codex Thread/Turn）

**Codex 概念映射：**

| Codex 概念 | 移植后概念 | 说明 |
|-----------|-----------|------|
| Thread | Session | 一个对话会话，持有完整消息历史 |
| Turn | Turn | 一次用户交互（用户消息 → agent 完成） |
| ThreadManager | SessionManager | 管理多个并发 session |
| thread/start | NewSession() | 初始化对话 |
| turn/start | RunTurn() | 启动一轮交互 |
| turn/interrupt | CancelTurn() | 中断当前轮次 |

```go
// internal/agentloop/session.go

type Session struct {
    ID            string
    ctxMgr        *ContextManager
    model         string
    systemPrompt  string
    enabledTools  []string
    maxIterations int
    store         *store.Store // 用于 card/snapshot 广播

    mu            sync.Mutex
    cancelFn      context.CancelFunc // 用于中断
}

func NewSession(id string, spec SessionSpec) *Session {
    s := &Session{
        ID:            id,
        ctxMgr:        NewContextManager(),
        model:         spec.Model,
        maxIterations: spec.MaxIterations,
        store:         spec.Store,
    }
    // 构建 system prompt（从 Codex build_prompt 移植）
    s.systemPrompt = buildSystemPrompt(spec)
    s.ctxMgr.AppendSystem(s.systemPrompt)
    return s
}

// SessionSpec 替代 Codex 的 thread/start 参数
type SessionSpec struct {
    Model                 string
    Cwd                   string
    DeveloperInstructions string
    DynamicTools          []string
    ApprovalPolicy        string
    SandboxMode           string
    MaxIterations         int
    Store                 *store.Store
}
```

### 2.7 审批流程（无需改动）

审批逻辑完全在 ai-server 层（`server.go` 的 `handleCodexServerRequest` 中的 `requestApproval` 处理）。替换后：

- **之前：** Codex 发 `item/commandExecution/requestApproval` → ai-server 评估策略 → 响应 Codex
- **之后：** Agent Loop 的 `executeTool()` 直接调用审批评估 → 等待用户决策 → 继续

现有的 `evaluateCommandPolicy()`、`autoApproveBySessionGrant()`、`autoApproveByHostGrant()` 等函数完全复用。

### 2.8 Workspace 模式

**Codex 实现：** 通过多个 thread 实现 planner + workers 并发。

**移植后：** 多个 Session 并发，每个 Session 有独立的 agent loop：

```go
// internal/agentloop/workspace.go

type WorkspaceRuntime struct {
    plannerSession *Session
    workerSessions map[string]*Session // hostID → Session
    orchestrator   *orchestrator.Manager
}

// StartPlannerTurn 启动 planner 的一轮交互
func (w *WorkspaceRuntime) StartPlannerTurn(ctx context.Context, loop *Loop, message string) error {
    return loop.RunTurn(ctx, w.plannerSession, message)
}

// StartWorkerTurn 启动 worker 的一轮交互
func (w *WorkspaceRuntime) StartWorkerTurn(ctx context.Context, loop *Loop, hostID, task string) error {
    worker, ok := w.workerSessions[hostID]
    if !ok {
        worker = NewSession(model.NewID("worker"), SessionSpec{
            Model: w.plannerSession.model,
            DeveloperInstructions: buildWorkerInstructions(hostID),
            // ...
        })
        w.workerSessions[hostID] = worker
    }
    return loop.RunTurn(ctx, worker, task)
}
```

### 2.9 与 server.go 的集成点

**替换前后的调用对比：**

```go
// ===== 替换前（通过 Codex JSON-RPC）=====

// 创建线程
a.codexRequest(ctx, "thread/start", threadParams, &result)
// 启动轮次
a.codexRequest(ctx, "turn/start", turnParams, &result)
// 中断轮次
a.codexRequest(ctx, "turn/interrupt", interruptParams, &result)
// 审批响应
a.codex.Respond(ctx, rawID, approvalDecision)

// ===== 替换后（直接调用 agent loop）=====

// 创建会话（替代 thread/start）
session := agentloop.NewSession(sessionID, spec)
// 启动轮次（替代 turn/start）
go a.agentLoop.RunTurn(ctx, session, userMessage)
// 中断轮次（替代 turn/interrupt）
session.Cancel()
// 审批响应（直接通过 channel 通知 agent loop）
session.ResolveApproval(approvalID, decision)
```

**handleChatMessage 改造：**

```go
func (a *App) handleChatMessage(w http.ResponseWriter, r *http.Request, sessionID string) {
    // ... 解析请求 ...

    // 替换前：
    // a.startTurn(ctx, sessionID, req)
    //   → a.ensureThread(ctx, sessionID)
    //     → a.codexRequest(ctx, "thread/start", ...)
    //   → a.requestTurn(ctx, sessionID, threadID, req)
    //     → a.codexRequest(ctx, "turn/start", ...)

    // 替换后：
    session := a.getOrCreateSession(sessionID)
    go a.agentLoop.RunTurn(ctx, session, req.Message)
}
```

---

## 第三部分：功能等价验证清单

替换完成后，以下所有功能必须正常工作：

### 3.1 Chat 模式

| 功能 | 验证方法 |
|------|---------|
| 发送消息，AI 回复 | 发送 "hello"，收到回复 |
| 流式消息显示 | 回复逐字出现，不是一次性显示 |
| AI 自动调用工具 | 发送 "看下 CPU"，AI 调用 host_summary |
| 工具结果显示 | 工具执行结果显示在 UI 卡片中 |
| 多轮对话 | 连续对话，AI 记住上下文 |
| 中断对话 | 点击 Stop，AI 停止生成 |
| 长对话压缩 | 对话超过 context window 时自动压缩 |

### 3.2 审批系统

| 功能 | 验证方法 |
|------|---------|
| 命令审批 | AI 要执行 `systemctl restart nginx`，弹出审批卡片 |
| 文件变更审批 | AI 要修改配置文件，弹出审批卡片 |
| 自动放行 | 已授权的命令自动通过 |
| 审批拒绝 | 拒绝后 AI 收到拒绝消息 |

### 3.3 远程主机操作

| 功能 | 验证方法 |
|------|---------|
| 选择远程主机 | 切换到远程主机 |
| 远程只读查询 | 在远程主机执行 `top` |
| 远程变更命令 | 在远程主机执行 `systemctl restart`（需审批） |
| 远程文件读写 | 读取/修改远程配置文件 |

### 3.4 Workspace 模式

| 功能 | 验证方法 |
|------|---------|
| Planner 分解任务 | 发送多主机任务，Planner 生成计划 |
| Worker 执行任务 | Worker 在各主机上执行分配的任务 |
| 任务状态追踪 | UI 显示每个 worker 的执行状态 |

### 3.5 Coroot 集成

| 功能 | 验证方法 |
|------|---------|
| AI 调用 Coroot 工具 | 发送 "查看服务健康状态"，AI 调用 coroot_list_services |
| 监控数据展示 | Coroot 查询结果显示在 UI 卡片中 |

### 3.6 多 LLM 支持（新增）

| 功能 | 验证方法 |
|------|---------|
| OpenAI 模型 | 配置 OpenAI API key，正常对话 |
| Anthropic Claude | 配置 Anthropic API key，正常对话 + tool calling |
| Ollama 本地模型 | 配置 Ollama 地址，正常对话 |
| 模型切换 | 在 Agent Profile 中切换模型，立即生效 |
| Fallback | 主模型不可用时自动切换备用模型 |

---

## 第四部分：实施计划

### 阶段 1：Bifrost 网关（1 周）

- [ ] `internal/bifrost/provider.go` — Provider 接口定义
- [ ] `internal/bifrost/openai.go` — OpenAI provider（含兼容 API）
- [ ] `internal/bifrost/anthropic.go` — Anthropic provider（消息格式转换）
- [ ] `internal/bifrost/ollama.go` — Ollama provider
- [ ] `internal/bifrost/credential.go` — Credential Pool
- [ ] `internal/bifrost/gateway.go` — Gateway 入口 + 错误恢复
- [ ] `internal/bifrost/usage.go` — 成本追踪

### 阶段 2：Agent Loop 移植（2 周）

- [ ] `internal/agentloop/loop.go` — 主循环（从 Codex run_turn 移植）
- [ ] `internal/agentloop/context.go` — Context Manager（从 Codex context_manager 移植）
- [ ] `internal/agentloop/compact.go` — 上下文压缩（从 Codex compact.rs 移植）
- [ ] `internal/agentloop/session.go` — Session 管理（替代 Codex Thread/Turn）
- [ ] `internal/agentloop/tools.go` — Tool Registry（从 dynamic_tools.go 迁移）
- [ ] `internal/agentloop/stream.go` — 流式响应处理 + WebSocket 推送
- [ ] `internal/agentloop/workspace.go` — Workspace 多 session 管理

### 阶段 3：集成 + 并行运行（1 周）

- [ ] 修改 `server.go`：`handleChatMessage` 支持 Bifrost 路径
- [ ] 特性开关：`USE_BIFROST=true` 切换到新路径
- [ ] 保留 Codex 路径作为 fallback
- [ ] 修改 `config.go`：新增 Bifrost 配置项

### 阶段 4：验证 + 切换（1 周）

- [ ] 运行第三部分的功能等价验证清单
- [ ] 性能对比（延迟、token 用量）
- [ ] 默认启用 Bifrost，Codex 降级为可选
- [ ] 更新部署文档和 README

### 阶段 5：清理（0.5 周）

- [ ] 删除 `internal/codex/` 包
- [ ] 删除 `Config.CodexPath` 和 `Config.CodexHome`
- [ ] 删除 `handleCodexNotification` 和 `handleCodexServerRequest`
- [ ] 更新 Docker 构建（不再需要 codex 二进制）
- [ ] 更新 README：支持的 LLM 列表、配置方式

---

## 第五部分：配置变更

### 替换前

```bash
export CODEX_API_KEY=sk-xxx          # OpenAI API Key
export CODEX_APP_SERVER_PATH=codex   # Codex 二进制路径
```

### 替换后

```bash
# 主 LLM 配置
export LLM_PROVIDER=openai           # openai / anthropic / ollama
export LLM_MODEL=gpt-4o             # 模型名
export LLM_API_KEY=sk-xxx           # API Key
export LLM_BASE_URL=                 # 自定义 endpoint（可选）

# Fallback 配置（可选）
export LLM_FALLBACK_PROVIDER=anthropic
export LLM_FALLBACK_MODEL=claude-sonnet-4-20250514
export LLM_FALLBACK_API_KEY=sk-ant-xxx

# 多 Key 轮转（可选）
export LLM_API_KEYS=sk-1,sk-2,sk-3

# 压缩用的便宜模型（可选）
export LLM_COMPACT_MODEL=gpt-4o-mini
```

Agent Profile 中新增 `runtime.provider` 和 `runtime.model` 字段，支持在 UI 中切换。
