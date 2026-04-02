# ai-server 调度器实现任务清单（MVP）

基于 [orchestrator-dispatch-collector-design.md](/Users/lizhongxuan/Desktop/aiops-codex/docs/orchestrator-dispatch-collector-design.md) 收敛出的实施清单。

配套核心交互流程图见：

- [orchestrator-core-interaction-flow.puml](/Users/lizhongxuan/Desktop/aiops-codex/docs/orchestrator-core-interaction-flow.puml)

状态说明：

- `[x]` 已完成
- `[ ]` 未完成或部分完成
- 标注 `（部分完成）` 的任务表示已有实现，但还没完全达到原完成标准

## 1. 实施目标

本轮实现只追求一条可运行的最小闭环：

1. 用户在 `WorkspaceSession` 发起 mission。
2. `ai-server` 创建 mission 和隐藏的 `PlannerSession`。
3. Planner 调用 `orchestrator_dispatch_tasks` 提交结构化任务。
4. 调度器按 host 创建或复用逻辑 Worker，并在预算允许时激活 `WorkerSession`。
5. Worker 继续复用现有 remote exec / remote file / approval / terminal 能力。
6. 调度器把进度、审批、输入请求、完成结果投影回 `WorkspaceSession`。
7. 用户可以在工作台只读查看任务细节，并跳转到 `SingleHostSession` 继续深聊。

## 2. 已冻结的 MVP 决策

- `WorkspaceSession` 正式进入后端 session 模型。
- `waiting_approval` / `waiting_input` 继续占用 active budget。
- 远程 workspace 准备先走固定 `mkdir -p`，不新增 host-agent RPC。
- Planner / Worker 先使用固定 internal preset，不先开放成 UI 可配置 profile。

## 3. 交付物清单

- `store.SessionState` 支持正式 `SessionMeta`。
- `internal/orchestrator/` 的状态、持久化、dispatcher、collector、projector 基础实现。
- `handleChatMessage` / `handleChatStop` / `handleDynamicToolCall` 按 session kind 分流。
- `PlannerPreset` / `WorkerPreset` 和 session 级 thread/turn 启动参数构建器。
- 工作台审批镜像、输入镜像、完成镜像。
- 工作台核心交互流程图 `.puml`。

## 4. 里程碑与任务分解

### M0. 文档与命名冻结

- [x] TASK-M0-001 冻结核心命名
  范围：`WorkspaceSession`、`PlannerSession`、`WorkerSession`、`Mission`、`HostWorker`、`RelayEvent`、`SessionMeta`
  完成标准：代码、日志、UI 文案和测试都不再混用旧名。

- [x] TASK-M0-002 冻结 session kind 枚举
  目标：统一为 `single_host | workspace | planner | worker`
  完成标准：后端常量、前端类型、测试 fixture 使用同一套枚举值。

- [x] TASK-M0-003 冻结 runtime preset 名称
  目标：统一为 `single_host_default | workspace_front | planner_internal | worker_internal`
  完成标准：session meta、日志、持久化和调试输出都使用同一套 preset 名称。

### M1. Session 模型与路由底座

- [x] TASK-M1-001 新增 `SessionMeta`
  文件：`internal/model/types.go`、`internal/store/memory.go`
  目标：给 `SessionState` 增加 `Meta` 字段，至少包含 `Kind`、`Visible`、`MissionID`、`WorkspaceSessionID`、`WorkerHostID`、`RuntimePreset`
  完成标准：store 中可读写 session meta，默认 session 为 `single_host + visible=true`

- [x] TASK-M1-002 扩展 session 持久化
  文件：`internal/store/memory.go`
  目标：把 `SessionMeta` 写入 stable state，并兼容旧状态文件升级
  完成标准：老状态文件可正常加载；新状态文件能恢复 meta

- [x] TASK-M1-003 提供带 meta 的 session 创建能力
  文件：`internal/store/chat_sessions.go`
  目标：支持创建 `workspace` / `planner` / `worker` session，且可以选择是否挂到 browser session 列表
  完成标准：前台 session 可见，内部 session 默认不可见

- [x] TASK-M1-004 过滤普通会话列表
  文件：`internal/store/chat_sessions.go`
  目标：`SessionSummaries()` 默认过滤 `visible=false` 的 internal session
  完成标准：浏览器历史会话不会出现 planner / worker

- [x] TASK-M1-005 过滤主机历史会话
  文件：`internal/store/hosts.go`
  目标：`HostSessions()` 默认过滤 `visible=false` 的 internal session
  完成标准：host modal / host history 默认不被 worker session 污染

- [x] TASK-M1-006 扩展 session 创建 API
  文件：`internal/server/server.go`
  目标：为 `POST /api/v1/sessions` 增加可选的 `kind` 输入，默认 `single_host`
  完成标准：前端可正式创建 `workspace` 会话

- [x] TASK-M1-007 `handleChatMessage` 按 kind 分流
  文件：`internal/server/server.go`
  目标：
  1. `single_host` 继续走现有路径
  2. `workspace` 进入 orchestrator
  3. `planner` / `worker` 拒绝前台直接发消息
  完成标准：不会把 workspace 请求误发成单 host turn

- [x] TASK-M1-008 `handleChatStop` 按 kind 分流
  文件：`internal/server/server.go`
  目标：
  1. `single_host` 保持现有 stop 行为
  2. `workspace` stop 触发 `CancelMission`
  3. `planner` / `worker` 拒绝前台 stop
  完成标准：工作台 stop 会 fan-out cancel 当前 mission

### M2. Runtime preset 与 thread/turn 运行时重构

- [x] TASK-M2-001 抽象 session 级 `RuntimeSpec`
  文件：`internal/server/server.go`
  目标：把当前写死在 `ensureThread()` / `requestTurn()` 里的 `model/cwd/approvalPolicy/sandbox/developerInstructions/dynamicTools` 提取成可按 session 构造的 spec
  完成标准：`single_host` / `planner` / `worker` 都能走同一套启动入口，但参数不同

- [x] TASK-M2-002 实现 `PlannerPreset`
  文件：`internal/orchestrator/preset.go`
  目标：固定 `gpt-5.4 + medium + planner workspace cwd + planner toolset`
  完成标准：planner session 可独立启动，不暴露 remote execute_* 工具

- [x] TASK-M2-003 实现 `WorkerPreset`
  文件：`internal/orchestrator/preset.go`
  目标：固定 `gpt-5.4-mini + low + worker remote cwd + remote toolset`
  完成标准：worker session 只能操作绑定主机，且不暴露 planner dispatch 工具

- [x] TASK-M2-004 扩展 developer instructions 构建器
  文件：`internal/server/server.go`、`internal/orchestrator/prompt.go`
  目标：分别生成 `Workspace`、`Planner`、`Worker` 的 prompt 包装
  完成标准：worker prompt 明确“不是直接对用户回复”；planner prompt 明确“只做规划与派发”

- [x] TASK-M2-005 增加 `codex.Client.PendingCount()`
  文件：`internal/codex/client.go`
  目标：暴露当前 pending request 数量，支撑 `PendingRequestBudget`
  完成标准：dispatcher 可读取 pending request 并用于限流

### M3. Workspace 与 bootstrap 基础设施

- [x] TASK-M3-001 实现 `WorkspaceLease` 分配器
  文件：`internal/orchestrator/workspace.go`
  目标：生成 planner local path、worker local path、worker remote path
  完成标准：mission 和 host 维度路径稳定、可重建、可审计

- [x] TASK-M3-002 本地 workspace 目录准备
  文件：`internal/orchestrator/workspace.go`
  目标：创建本地 planner / worker 工作区
  完成标准：所有 mission 启动前本地目录可用

- [x] TASK-M3-003 远程 workspace bootstrap
  文件：`internal/orchestrator/workspace.go`、`internal/server/remote_exec.go`
  目标：在 worker 首次激活前执行固定命令 `mkdir -p <remotePath>`
  完成标准：目录准备失败会中止该 host worker；成功后写审计日志

- [x] TASK-M3-004 定义 bootstrap 审计事件
  文件：`internal/server/server.go` 或 `internal/orchestrator/workspace.go`
  目标：为 orchestrator-owned `mkdir -p` 写单独 audit event
  完成标准：后续可以明确区分“基础设施准备”和“用户发起变更”

### M4. Orchestrator 状态与持久化

- [x] TASK-M4-001 新建 `internal/orchestrator/state.go`
  目标：定义 `Mission`、`TaskRun`、`HostWorker`、`WorkspaceLease`、`RelayEvent`、`WorkerSeenState`
  完成标准：状态对象齐全，字段与设计文档一致

- [x] TASK-M4-002 新建 orchestrator store
  文件：`internal/orchestrator/store.go`
  目标：把 mission / task / worker / workspace / relay event 持久化到 `<state-dir>/orchestrator/orchestrator.json`
  完成标准：读写原子化，兼容空文件和旧版本

- [x] TASK-M4-003 实现索引结构
  文件：`internal/orchestrator/store.go`
  目标：提供 `workspaceSessionID -> missionID`、`plannerSessionID -> missionID`、`workerSessionID -> missionID`、`approvalID -> workerSessionID`、`choiceID -> originSessionID`
  完成标准：approval/choice/stop 可以 O(1) 或接近 O(1) 路由

- [x] TASK-M4-004 重启恢复逻辑
  文件：`internal/orchestrator/manager.go`
  目标：启动时恢复 mission 关系、重挂 collector、重新计算预算占用
  完成标准：server 重启后不会无脑重新放大并发，也不会丢失 mission 关联

### M5. Mission 创建与 PlannerSession

- [x] TASK-M5-001 新建 `internal/orchestrator/manager.go`
  目标：实现 `StartMission`、`Dispatch`、`CancelMission`、`CancelByWorkspaceSession`、`OnSnapshot`、`OnTurnEvent` 等核心入口
  完成标准：`App` 只通过 manager 触发 mission 生命周期

- [x] TASK-M5-002 Workspace 请求触发 mission
  文件：`internal/server/server.go`
  目标：`workspace` 会话第一次发消息时自动创建 mission；后续消息绑定当前 mission 或按策略开新 mission
  完成标准：前台工作台请求不再直接启动主 agent turn

- [x] TASK-M5-003 创建隐藏的 `PlannerSession`
  文件：`internal/orchestrator/manager.go`
  目标：创建 `kind=planner` session、挂载 `PlannerPreset`、分配 planner workspace
  完成标准：planner session 独立 thread、独立 cwd、独立 transcript

- [x] TASK-M5-004 Workspace 用户输入投递到 planner
  文件：`internal/orchestrator/manager.go`
  目标：把 workspace 用户消息包装为 planner 输入
  完成标准：workspace 本体只承载用户卡片和投影卡片，不承担 planner tool 调用

### M6. Dynamic tool registry 与 dispatch 工具

- [x] TASK-M6-001 重构 `handleDynamicToolCall`
  文件：`internal/server/dynamic_tools.go`
  目标：先解析 `SessionMeta.Kind`，再按 planner/worker 不同 registry 分发
  完成标准：planner 可用 dispatch 工具；worker 继续使用 remote tools；workspace 不暴露 dispatch 工具

- [x] TASK-M6-002 实现 `orchestrator_dispatch_tasks`
  文件：`internal/server/dynamic_tools.go`、`internal/orchestrator/manager.go`
  目标：接收 planner 的结构化任务并交给 `Manager.Dispatch`
  完成标准：支持基础 schema 校验、host 存在性校验、批量返回 accepted/queued/activated

- [x] TASK-M6-003 实现 dispatch 请求校验器
  文件：`internal/orchestrator/dispatcher.go`
  目标：校验 `missionTitle`、`summary`、`taskId`、`hostId`、`instruction`、重复 host/task 等
  完成标准：错误能明确反馈给 planner tool response

### M7. Dispatcher、预算与 lazy materialization

- [x] TASK-M7-001 实现 host worker 复用逻辑
  文件：`internal/orchestrator/dispatcher.go`
  目标：同一 mission 内相同 host 只创建一个 `HostWorker`
  完成标准：相同 host 的多个任务串行进入一个 queue

- [x] TASK-M7-002 实现任务状态机
  文件：`internal/orchestrator/dispatcher.go`
  目标：支持 `queued -> ready -> dispatching -> running -> completed/failed/cancelled`
  完成标准：任务状态推进无环路、可恢复

- [x] TASK-M7-003 实现全局预算和 mission 预算
  文件：`internal/orchestrator/dispatcher.go`
  目标：限制同时活跃的 planner/worker 数量
  完成标准：预算耗尽时只排队，不启动新的 `turn/start`

- [x] TASK-M7-004 实现速率限制
  文件：`internal/orchestrator/dispatcher.go`
  目标：增加 `ThreadCreateRateLimit`、`TurnStartRateLimit`、`PendingRequestBudget`
  完成标准：1000 host 派发不会瞬时打满 codex app-server

- [x] TASK-M7-005 明确预算释放条件
  文件：`internal/orchestrator/dispatcher.go`
  目标：只在 worker turn 终态释放预算，`waiting_approval` / `waiting_input` 不释放
  完成标准：不会因为审批堆积导致 active budget 漏算

- [x] TASK-M7-006 实现 worker lazy materialization
  文件：`internal/orchestrator/dispatcher.go`
  目标：逻辑 worker 先存在，只有真正执行首个 task 时才创建 thread/session binding
  完成标准：mission 覆盖 1000 host 时不会立刻创建 1000 个活跃 thread

- [x] TASK-M7-007 实现 idle thread 解绑
  文件：`internal/orchestrator/dispatcher.go`
  目标：worker 长时间 idle 后 `store.ClearThread(sessionID)`，但保留 session 和 worker state
  完成标准：不要求显式 `thread/delete`

### M8. Collector 与 projector

- [x] TASK-M8-001 新建 `internal/orchestrator/collector.go`
  目标：消费 turn、approval、choice、remote exec、snapshot 事件
  完成标准：所有 worker / planner 关键事件都能进入 orchestrator

- [x] TASK-M8-002 接入权威 hook
  文件：`internal/server/server.go`、`internal/server/dynamic_tools.go`、`internal/server/remote_exec.go`
  目标：在 turn started/completed/aborted、approval requested/resolved、choice requested/resolved、remote exec started/finished 处调用 orchestrator hook
  完成标准：collector 不依赖纯 snapshot diff 才能得出核心状态

- [x] TASK-M8-003 实现 snapshot fallback
  文件：`internal/orchestrator/collector.go`
  目标：从 snapshot 补 reply、卡片摘要和兜底状态
  完成标准：reply 和 UI 摘要在 hook 缺失时仍可恢复

- [x] TASK-M8-004 实现去重状态
  文件：`internal/orchestrator/collector.go`
  目标：使用 `WorkerSeenState` 去重 card/approval/choice/reply
  完成标准：重复 snapshot 不会重复生成 relay event

- [x] TASK-M8-005 新建 `internal/orchestrator/projector.go`
  目标：把 relay events 投影为工作台卡片和详情读模型
  完成标准：MissionCard / WorkerProgressCard / WorkerApprovalCard / WorkerCompletionCard 稳定 upsert

- [x] TASK-M8-006 输出详情读模型
  文件：`internal/orchestrator/projector.go`
  目标：生成 `PlanSummaryView`、`PlanDetailView`、`DispatchSummaryView`、`DispatchHostDetailView`、`WorkerReadonlyDetailView`
  完成标准：查看详情不触发新的 planner/worker turn

### M9. Approval / choice 镜像路由

- [x] TASK-M9-001 实现 approval mirror 映射
  文件：`internal/orchestrator/manager.go`
  目标：记录 `workspace approval card -> workerSessionID + approvalID`
  完成标准：从工作台点击审批可定位原始 WorkerSession approval

- [x] TASK-M9-002 改造 `handleApprovalDecision`
  文件：`internal/server/server.go`
  目标：先查当前 session；查不到时走 orchestrator mirror 路由
  完成标准：工作台和原始 worker session 都能处理审批

- [x] TASK-M9-003 实现 choice mirror 映射
  文件：`internal/orchestrator/manager.go`
  目标：支持 planner/worker `request_user_input` 镜像到 workspace
  完成标准：内部 session 不会直接暴露到前台等待输入

- [x] TASK-M9-004 改造 `handleChoiceAnswer`
  文件：`internal/server/server.go`
  目标：支持 workspace -> planner/worker 的输入路由
  完成标准：工作台可回答 planner/worker 发出的 choice 请求

### M10. 取消、异常与恢复

- [x] TASK-M10-001 实现 mission cancel
  文件：`internal/orchestrator/manager.go`
  目标：中断所有活跃 worker turn，取消远程 exec，清空 queued/ready task
  完成标准：workspace stop 可完整 fan-out cancel

- [x] TASK-M10-002 实现单 task cancel
  文件：`internal/orchestrator/manager.go`
  目标：支持内部取消单 host task
  完成标准：后续 planner 重规划时可单点撤销

- [x] TASK-M10-003 实现 host offline reconcile
  文件：`internal/orchestrator/collector.go`、`internal/orchestrator/manager.go`
  目标：host 下线时标记 worker/task 失败或 `needs_reconcile`
  完成标准：断连不会留下永远 running 的任务

- [x] TASK-M10-004 实现 restart reconcile
  文件：`internal/orchestrator/manager.go`
  目标：server 重启后对无 session/thread 的任务做 reconcile
  完成标准：恢复后状态一致，可继续推进剩余 host

### M11. Web 工作台联调

- [x] TASK-M11-001 新建或改造 workspace session 入口
  文件：`web/src/store.js`、`web/src/pages/*`
  目标：可以创建 `workspace` 类型会话
  完成标准：用户可以正式进入工作台而不是只看 mock 数据

- [x] TASK-M11-002 渲染 MissionCard 和 host 聚合卡片
  文件：`web/src/components/*`
  目标：支持工作台摘要流和 host 维度详情
  完成标准：mission / progress / approval / completion 4 类卡片稳定渲染

- [x] TASK-M11-003 实现详情抽屉
  文件：`web/src/pages/ProtocolPage.vue` 或相关组件
  目标：展示 `PlanDetailView`、`DispatchHostDetailView`、`WorkerReadonlyDetailView`
  完成标准：打开详情不触发新的 planner/worker turn

- [x] TASK-M11-004 实现跳转到 `SingleHostSession`
  文件：`web/src/store.js`、`web/src/pages/ChatPage.vue`
  目标：从工作台 host 详情跳到单机对话，并自动选择对应 host
  完成标准：用户可以从只读 worker 详情平滑切到可互动单机会话

### M12. 测试与验收

- [x] TASK-M12-001 SessionMeta 单测
  范围：session 创建、持久化、过滤
  完成标准：internal session 不会出现在普通列表中

- [x] TASK-M12-002 dispatcher 单测
  范围：worker 复用、预算释放、lazy materialization、离线 host 拒绝
  当前状态：已补齐 `1000 host` 激活上限；离线 host 拒绝在 server/planner dispatch 集成测试中覆盖
  完成标准：1000 host 场景下只激活前 N 个 worker

- [x] TASK-M12-003 collector 单测
  范围：hook + snapshot fallback 去重
  完成标准：重复 snapshot 不会重复记 event

- [x] TASK-M12-004 projector 单测
  范围：MissionCard / WorkerProgressCard / WorkerApprovalCard / WorkerCompletionCard upsert
  完成标准：同 host 不会刷出多张 progress 卡

- [x] TASK-M12-005 approval / choice 路由集成测试
  范围：workspace 点击审批、回答 choice
  完成标准：都能准确回到原始 internal session

- [x] TASK-M12-006 端到端集成测试
  场景：
  1. workspace 发起 mission
  2. planner dispatch 32 host
  3. mission budget=4
  4. 其中 1 台进入审批
  5. 审批通过后继续
  6. 其他 host 分批推进
  7. 最终工作台看到完整 mission 卡片链
  完成标准：完整闭环可跑通

- [x] TASK-M12-007 手动 smoke 清单
  范围：新建 workspace 会话、mission stop、host offline、restart recover、jump 到单机会话
  完成标准：核心交互都能在真实 UI 中验证

## 5. 推荐实施顺序

建议按下面的批次推进，而不是一次性大改：

### Batch 1

- M1 Session 模型与路由底座
- M2 Runtime preset 与 thread/turn 重构

目标：先让后端具备正式的 `workspace/planner/worker` 会话语义。

### Batch 2

- M3 Workspace 与 bootstrap
- M4 Orchestrator 状态与持久化
- M5 Mission 创建与 PlannerSession

目标：先跑通 mission 创建和 planner 独立会话。

### Batch 3

- M6 Dynamic tool registry 与 dispatch 工具
- M7 Dispatcher、预算与 lazy materialization

目标：让 planner 真能派发，worker 真能按预算启动。

### Batch 4

- M8 Collector 与 projector
- M9 Approval / choice 镜像路由

目标：让前台工作台真能看见并处理 mission 过程。

### Batch 5

- M10 取消、异常与恢复
- M11 Web 联调
- M12 测试与验收

目标：从“能跑”提升到“可联调、可恢复、可验收”。

## 6. 完成定义

以下条件同时满足，才视为 MVP 完成：

- `workspace` 会话能正式创建并进入后端。
- 用户发起的 workspace 请求不会走单 host 旧路径。
- planner 能独立 thread / cwd / tool domain 运行。
- worker 能独立 thread / remote cwd / host binding 运行。
- 同一 host 的多个任务在单个逻辑 worker 中串行。
- `waiting_approval` / `waiting_input` 不会错误释放 active budget。
- 工作台能镜像审批和输入请求，并回路由到 internal session。
- 工作台能稳定显示 mission / progress / approval / completion。
- 用户能从工作台跳回单机对话继续互动。
- 重启、断连、stop 都有可预测结果。
