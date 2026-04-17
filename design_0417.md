# AIOps Codex Tool Lifecycle Unification Design

## 背景

当前 `aiops-codex` 的工具执行存在两种主要路径：

- `internal/server/bifrost_runtime.go`
  - 通过 `agentloop` 的 `SetStreamObserver / SetToolObserver / SetApprovalHandler` 观察和驱动工具生命周期
- `internal/server/dynamic_tools.go`
  - 通过 server 层直接处理动态工具调用、审批、卡片、runtime phase、evidence、orchestrator 同步

这两条路径都能工作，但生命周期语义分散在不同文件和不同抽象层，已经出现以下问题：

- 同类状态转换重复实现，如 `thinking -> waiting_approval -> executing -> completed/failed`
- 审批、事件、evidence、卡片落地逻辑分散
- workspace / planner / worker / single_host 的投影策略混杂在执行逻辑中
- 后续新增工具域时，容易继续复制状态更新逻辑
- 调试、回放、审计更依赖最终 snapshot，而不是可重建的事实流

本方案采纳“执行层统一成 handler + event，展示层继续保留 snapshot/card/orchestrator projection”的方向。

## 目标

本次设计目标：

- 统一工具执行入口和生命周期语义
- 将工具执行事实沉淀为结构化事件
- 保留当前 `snapshot + cards + approvals + orchestrator` 作为 UI 读模型
- 让 `bifrost_runtime.go` 和 `dynamic_tools.go` 逐步并轨
- 为后续 replay、timeline、审计、并发 worker、MCP app、runner 等扩展打基础

非目标：

- 不把前端改成直接消费底层事件
- 不重写现有 snapshot/card 产品模型
- 不强制把所有工具降格成 shell primitive
- 不在第一阶段重构 orchestrator 全链路

## 设计原则

1. 执行真相层与产品投影层分离
2. 工具生命周期由统一事件描述，而不是散落在条件分支里
3. UI 继续以 snapshot 为中心，避免大面积前端重写
4. 新路径先接入统一模型，旧路径逐步迁移
5. 保证 single_host / workspace / planner / worker 仅在投影层体现差异

## 总体架构

建议拆成两层：

### 1. Execution Layer

负责：

- 统一工具注册与 dispatch
- 生命周期事件发射
- 审批前后控制
- 工具结果标准化
- 错误、取消、超时的统一语义

产物：

- `ToolInvocation`
- `ToolLifecycleEvent`
- `ToolExecutionResult`

### 2. Projection Layer

负责将生命周期事件投影到当前产品模型：

- runtime turn phase
- runtime activity
- cards
- approvals
- incident events
- evidence summaries
- orchestrator mission / worker phase
- websocket snapshot broadcast

产物：

- store 更新
- snapshot 更新
- UI 卡片和 approval rail

## 分层示意

```text
LLM / Dynamic Call / Orchestrator Call
                |
                v
      Unified Tool Dispatcher
                |
                v
        Tool Handler Registry
                |
                v
        Tool Lifecycle Events
                |
      +---------+---------+
      |                   |
      v                   v
  Product Projection   Audit / Replay / Metrics
      |
      v
snapshot + cards + approvals + evidence + orchestrator
```

## 核心抽象

### ToolInvocation

统一描述一次工具调用，不区分来源。

建议字段：

```go
type ToolInvocation struct {
    InvocationID   string
    SessionID      string
    ThreadID       string
    TurnID         string
    ToolName       string
    ToolKind       string
    Source         ToolInvocationSource
    HostID         string
    WorkspaceID    string
    CallID         string
    Arguments      map[string]any
    RawArguments   string
    RequiresApproval bool
    ReadOnly       bool
    StartedAt      time.Time
}
```

其中 `Source` 用于区分：

- `agentloop_tool_call`
- `dynamic_tool_call`
- `orchestrator_dispatch`
- `approval_resume`
- `system_auto_replay`

### ToolExecutionResult

统一描述工具执行结果。

```go
type ToolExecutionResult struct {
    InvocationID string
    Status       ToolRunStatus
    OutputText   string
    OutputData   map[string]any
    ErrorText    string
    EvidenceRefs []string
    FinishedAt   time.Time
}
```

### ToolHandler

统一工具执行接口。

```go
type ToolHandler interface {
    Descriptor() ToolDescriptor
    Execute(ctx context.Context, inv ToolInvocation) (ToolExecutionResult, error)
}
```

`ToolDescriptor` 建议包含：

- `Name`
- `Domain`
- `RequiresApproval`
- `IsReadOnly`
- `SupportsStreamingProgress`
- `ProjectionHints`

### ToolLifecycleEvent

生命周期事件是本方案最核心的新增抽象。

```go
type ToolLifecycleEvent struct {
    EventID       string
    InvocationID  string
    SessionID     string
    ToolName      string
    Type          ToolLifecycleEventType
    Phase         string
    HostID        string
    Timestamp     time.Time
    Payload       map[string]any
}
```

建议最小事件集：

- `tool.started`
- `tool.progress`
- `tool.approval_requested`
- `tool.approval_resolved`
- `tool.completed`
- `tool.failed`
- `tool.cancelled`

未来可扩展：

- `tool.output_chunk`
- `tool.retry_scheduled`
- `tool.compensation_started`
- `tool.compensation_completed`

## 统一执行流程

统一后的主流程：

1. 外层请求构建 `ToolInvocation`
2. `UnifiedToolDispatcher` 根据 `ToolName` 找到 `ToolHandler`
3. 发射 `tool.started`
4. 若需要审批：
   - 发射 `tool.approval_requested`
   - 进入统一审批等待
   - 审批结束后发射 `tool.approval_resolved`
5. 执行 handler
6. 执行过程中按需发射 `tool.progress`
7. 结束后发射：
   - 成功：`tool.completed`
   - 失败：`tool.failed`
   - 取消：`tool.cancelled`
8. Projection Layer 消费这些事件，更新 snapshot/card/runtime/orchestrator

## 组件设计

### 1. UnifiedToolDispatcher

建议新增文件：

- `internal/server/tool_dispatcher.go`

职责：

- 接收统一 `ToolInvocation`
- 注入 tracing / timing / host / session 信息
- 查询 descriptor
- 管理审批前后流程
- 调用 handler
- 发射生命周期事件

接口草案：

```go
type UnifiedToolDispatcher struct {
    registry   *ToolHandlerRegistry
    emitter    ToolEventEmitter
    approvals  ToolApprovalCoordinator
}

func (d *UnifiedToolDispatcher) Dispatch(
    ctx context.Context,
    inv ToolInvocation,
) (ToolExecutionResult, error)
```

### 2. ToolHandlerRegistry

职责：

- 注册 handler
- 返回 descriptor
- 以统一键查找不同工具域

建议不要和 `agentloop.ToolRegistry` 完全复用类型，但语义尽量对齐，避免未来再分叉。

### 3. ToolEventEmitter

职责：

- 对内发布统一事件
- 可先支持同步 in-process 广播
- 后续可接持久化 event log / metrics / replay

接口草案：

```go
type ToolEventEmitter interface {
    Emit(ctx context.Context, event ToolLifecycleEvent)
}
```

第一阶段实现：

- `InProcessToolEventBus`

第二阶段可扩展：

- `PersistedToolEventBus`
- `MetricsToolEventBus`
- `AuditMirrorToolEventBus`

### 4. ProductProjectionSubscriber

建议新增统一投影入口，而不是在每个 handler 里直接写 store。

职责：

- 订阅 `ToolLifecycleEvent`
- 转成当前产品需要的副作用

建议拆成多个 subscriber：

- `RuntimeProjectionSubscriber`
- `CardProjectionSubscriber`
- `ApprovalProjectionSubscriber`
- `EvidenceProjectionSubscriber`
- `OrchestratorProjectionSubscriber`
- `SnapshotBroadcastSubscriber`

这样每种投影逻辑独立，避免单个巨型函数。

## 生命周期事件到产品投影的映射

### `tool.started`

更新：

- `runtime.turn.phase`
- `runtime.activity`
- 对 workspace/worker 创建 `ProcessLineCard`

示例映射：

- `web_search` -> `searching`
- `read_file` -> `browsing`
- `execute_command` -> `executing`

### `tool.approval_requested`

更新：

- 生成 `ApprovalRequest`
- 生成对应 approval card
- `runtime.turn.phase = waiting_approval`
- 若为 worker，通知 orchestrator worker phase

### `tool.approval_resolved`

更新：

- resolve approval
- 生成 approval memo / notice
- phase 切回 `executing` 或 `thinking`

### `tool.progress`

更新：

- 更新 process line
- 更新 activity 行
- 可用于长命令输出、远程文件处理、下载进度

### `tool.completed`

更新：

- 完成 process card
- 更新 activity 计数
- 落 evidence
- 若工具返回 MCP / card payload，则生成对应结果卡

### `tool.failed`

更新：

- process 卡失败
- error card / notice
- incident event
- 视情况让 turn 继续或失败

## 对现有代码的改造方向

### A. `bifrost_runtime.go`

当前：

- `OnToolStart` / `OnToolComplete` 直接更新 runtime/activity/card

改造目标：

- 保留 observer 接口
- 但 observer 只做 `ToolInvocation` 和 `ToolLifecycleEvent` 转换
- 真实状态更新移到 Projection Subscriber

即：

- `OnToolStart` -> emit `tool.started`
- `OnToolComplete` -> emit `tool.completed` / `tool.failed`

### B. `dynamic_tools.go`

当前：

- 大量工具直接在函数内部：
  - 申请审批
  - 改 runtime phase
  - 写 cards
  - 写 approvals
  - 写 incident/evidence
  - broadcast snapshot

改造目标：

- 保留每个 domain tool 的业务逻辑
- 把状态副作用外提到 dispatcher + projection

即：

- `handleReadonlyHostInspect`
- `requestRemoteCommandApproval`
- `requestRemoteFileChangeApproval`
- `executeLocalReadonlyDynamicTool`

这些函数逐步改成：

- 构造 invocation
- 调 dispatcher
- 获取结果
- 响应上游调用

### C. `tool_dispatcher.go`

该文件已存在，但当前更像 server 内部调度点。建议将其提升为真正统一执行入口，并逐步承接两条路径。

## 事件与 snapshot 的关系

本方案明确：

- 事件是执行真相层
- snapshot 是产品读模型

因此：

- 不要求前端直接消费事件
- 仍保留 `/api/v1/state` + `/ws` snapshot 模型
- 所有 UI 卡片仍由投影层生成

这样能保留当前前端优势：

- 页面简单
- 审批 rail 简单
- ChatPage / ProtocolWorkspacePage 不需要整体重写

## 审批统一设计

审批建议从“工具内部逻辑”提升为统一协调器。

新增抽象：

```go
type ToolApprovalCoordinator interface {
    Request(ctx context.Context, inv ToolInvocation) (ApprovalResolution, error)
}
```

职责：

- 查询 session grant / host grant / policy auto-approve
- 若需人工审批，发射 `tool.approval_requested`
- 等待结果
- 发射 `tool.approval_resolved`

这样可以把当前这些逻辑统一收口：

- `autoApproveRemoteOperationBySessionGrant`
- `autoApproveRemoteOperationByHostGrant`
- `autoApproveRemoteOperationByPolicy`
- `resolveBifrostApproval`
- 各类 `request*Approval`

## event log 持久化建议

第一阶段可以只做内存事件广播。

但建议事件模型从第一天就可持久化，后续支持：

- 审计回放
- timeline 重建
- 问题复盘
- 失败后状态恢复

建议新增：

- `internal/store/tool_event_store.go`

第一阶段：

- session 内 ring buffer

第二阶段：

- JSON persisted store

## 并发与多 worker 场景

引入统一事件层后，并发场景会更可控。

当前痛点：

- 多个 worker 同时落卡时，状态变更分散
- snapshot 只能看到结果，不容易判断状态顺序

统一事件后：

- 每个 `InvocationID` 有稳定生命周期
- worker 可带 `HostID / WorkspaceID / MissionID`
- orchestrator 只需订阅感兴趣的事件

建议事件 payload 中保留：

- `missionId`
- `workerSessionId`
- `workspaceSessionId`
- `hostId`

## 迁移计划

### Phase 1: 建基础设施，不改 UI 模型

目标：

- 增加事件模型、dispatcher、emitter、projection subscriber 骨架
- `bifrost_runtime.go` 的 tool observer 先改成 emit event

交付：

- `ToolLifecycleEvent`
- `ToolEventEmitter`
- `UnifiedToolDispatcher`
- `ProductProjectionSubscriber`

### Phase 2: 迁移动态工具路径

目标：

- 将 `dynamic_tools.go` 中最常用的工具改走 dispatcher

优先迁移工具：

- `readonly_host_inspect`
- `execute_command`
- `read_file`
- `list_files`
- `search_files`
- `write_file`

### Phase 3: 审批统一

目标：

- 将 bifrost 和 dynamic tool 的审批统一到 `ToolApprovalCoordinator`

### Phase 4: 事件持久化与 replay

目标：

- 增加 tool event store
- 支持 timeline / audit / mission detail 从 event 重建或校验

## 文件级改动建议

建议新增文件：

- `internal/server/tool_lifecycle.go`
- `internal/server/tool_event_bus.go`
- `internal/server/tool_projection.go`
- `internal/server/tool_approval_coordinator.go`

建议重点改造文件：

- `internal/server/tool_dispatcher.go`
- `internal/server/bifrost_runtime.go`
- `internal/server/dynamic_tools.go`
- `internal/server/server.go`

建议暂不大改文件：

- `web/src/store.js`
- `web/src/pages/ChatPage.vue`
- `web/src/lib/chatTurnFormatter.js`

## 风险

### 1. 双写期状态不一致

在迁移阶段，旧逻辑和新投影逻辑可能同时生效。

控制策略：

- 引入 feature flag
- 先按 tool domain 逐步切换
- 每迁移一个工具域就补回归测试

### 2. 事件模型过度抽象

如果事件设计得太泛，会让 projection payload 不够用。

控制策略：

- 核心字段固定
- domain-specific 信息进入 `Payload`
- 保持 event schema 可演进

### 3. orchestrator 投影耦合继续扩散

控制策略：

- orchestrator 作为单独 subscriber
- 不允许工具 handler 直接更新 orchestrator 状态

### 4. snapshot broadcast 过于频繁

引入事件后，广播次数可能增加。

控制策略：

- 仍保留 throttled broadcast
- `SnapshotBroadcastSubscriber` 内部节流

## 测试策略

需要补的测试分层：

- 单元测试
  - dispatcher 生命周期顺序
  - approval coordinator 自动放行 / 人工审批 / 拒绝
  - projection 对 phase/card/evidence 的映射

- 集成测试
  - bifrost tool call -> event -> projection
  - dynamic tool call -> event -> projection
  - workspace worker approval 流程

- UI 回归
  - ChatPage 工具执行过程线
  - waiting_approval 展示
  - completed/failed 结论不回退

## 建议结论

建议采纳该方向，并按“执行层统一、读模型保留”的方式推进。

原因：

- 能解决当前工具生命周期分散的问题
- 不会推翻现有前端和产品模型
- 有利于后续扩展 orchestrator、多 worker、审计回放和复杂审批

最重要的落地原则只有一句：

**以后工具执行只负责产生命令、审批请求、结果和事件；UI、卡片、审批面板、evidence、workspace 状态都由投影层负责。**
