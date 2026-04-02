# 删除 Planner Session 重构实施清单

基于当前项目的协作工作台实现与本轮产品决策，收敛出以下重构清单：

- 前台只保留一个用户可见的 `主 Agent Session`
- 删除独立 `PlannerSession`
- 保留 `mission / dispatcher / worker session / approval route / result projection`
- 审批继续由 `WorkerSession` 触发，并统一显示在右侧审批列表
- 计划只作为主对话中的轻量折叠控件展示，不再暴露 planner 会话概念

状态说明：

- `[x]` 已完成
- `[ ]` 未完成
- `（部分完成）` 表示已有实现或可复用基础，但尚未达到本次重构目标

## 1. 实施目标

本轮重构追求以下最小闭环：

1. 用户只和 `WorkspaceSession` 中的 `主 Agent` 对话。
2. 简单状态问题由主 Agent 直接读取 `ai-server` 投影并回复。
3. 复杂任务由主 Agent 自己生成简短计划，并直接提交结构化 step 到调度器。
4. 调度器为目标主机创建或复用 `WorkerSession`，控制并发、收集结果、维护审批。
5. 右侧审批列表只展示 `WorkerSession` 触发的审批，不再出现 planner 审批语义。
6. 主对话只显示摘要级信息：计划、运行态、审批提醒、结果总结。
7. 前端、日志、接口、测试中不再暴露 `PlannerSession` 作为产品概念。

## 2. 当前基线

- [x] 2.1 已存在正式 `WorkspaceSession`、`WorkerSession`、`mission`、`approval`、`dispatch`、`projection` 基础设施。
- [x] 2.2 当前复杂任务通过 `WorkspaceSession -> PlannerSession -> Dispatch -> WorkerSession` 链路推进。
- [x] 2.3 当前右侧审批列表已经是独立区域，审批不强依赖聊天流内卡片。
- [x] 2.4 当前主对话已经具备轻量 `plan widget` 和 `background agents widget` 的 UI 基础。
- [ ] 2.5 当前仍有大量 `PlannerSession / planner trace / planner conversation / plannerSessionId` 依赖分布在后端状态、投影、接口和前端文案中。
- [x] 2.6 当前 `WorkspaceSession` 已成为复杂任务的唯一主控 AI 会话；planner 只剩 legacy 兼容残留。

## 3. 实施原则

- [x] 3.1 只删除 `PlannerSession`，不删除 `mission / dispatcher / worker session`。
- [x] 3.2 前台不再解释“影子 session / planner session / projection”这些内部术语。
- [x] 3.3 主 Agent 负责用户表达、计划摘要和任务提交；调度与执行控制仍在 Go 层。
- [x] 3.4 审批只来自 worker，不把审批控制权放回主对话。
- [x] 3.5 优先兼容历史数据，旧 mission 的 `plannerSessionId` 等字段先允许只读回放，再逐步清理。
- [x] 3.6 先让新链路稳定跑通，再做字段删除和历史数据收敛。

## 4. 交付物清单

- [x] 4.1 新版后端职责说明文档：`主 Agent Session + Go 调度器 + WorkerSession`
- [x] 4.2 `StartMission` 不再创建 `PlannerSession`
- [x] 4.3 `WorkspaceSession` 直接拥有 orchestrator 能力：状态查询、任务派发
- [ ] 4.4 删除 planner 专属 runtime/prompt/tool registry
  （部分完成）主链路已完成 `WorkspaceSession` 直起 orchestration turn，并把 ai-server 状态查询 / dispatch 工具挂到主 Agent；但仍保留少量 legacy planner 函数名与兼容分支。
- [x] 4.5 新版工作台投影：不再输出 planner session 概念
- [x] 4.6 新版测试：覆盖“简单问题直答”和“复杂问题直接由主 Agent 派发”

## 5. 里程碑与任务分解

### M0. 命名与概念冻结

- [x] TASK-DEL-PLANNER-001 冻结产品概念
  目标：前台只保留 `主 Agent / 子 Agent / 审批列表 / 计划小控件`
  完成标准：页面文案、说明文档、接口注释不再出现“影子 session”与“PlannerSession”产品表述

- [x] TASK-DEL-PLANNER-002 冻结后端目标架构
  目标：统一为 `WorkspaceSession(主 Agent) + Mission + Dispatcher + WorkerSession`
  完成标准：设计文档、日志和实现说明一致

- [x] TASK-DEL-PLANNER-003 明确兼容策略
  目标：老数据中 `plannerSessionId / planner trace` 暂时兼容只读，不阻塞新链路上线
  完成标准：迁移方案中有明确的“兼容期”和“清理期”

### M1. Mission 模型瘦身

- [x] TASK-DEL-PLANNER-004 移除 `StartMission` 对 `PlannerSessionID` 的强依赖
  文件：`internal/orchestrator/manager.go`
  目标：`StartMissionRequest` 和 `Mission` 创建路径不再要求生成 planner session
  完成标准：新建 mission 时只绑定 `WorkspaceSessionID`，不自动创建 planner session meta

- [x] TASK-DEL-PLANNER-005 移除 planner workspace lease
  文件：`internal/orchestrator/manager.go`、`internal/orchestrator/workspace.go`
  目标：不再为 planner 单独创建本地 workspace 目录
  完成标准：mission 启动时只准备 worker 所需目录；若主 Agent 需要临时工作区，则复用 workspace/front 默认根

- [x] TASK-DEL-PLANNER-006 调整 orchestrator store 索引
  文件：`internal/orchestrator/store.go`
  目标：删除或降级 `plannerSessionID -> missionID` 强索引
  完成标准：mission 路由主键变为 `workspaceSessionID -> missionID` 与 `workerSessionID -> missionID`
  说明：新 mission 已不再生成 planner 索引；`MissionByPlannerSession` 仅保留给 legacy planner 失败收敛使用。

- [x] TASK-DEL-PLANNER-007 精简 mission 状态结构
  文件：`internal/orchestrator/state.go`
  目标：让 `Mission` 不再把 planner 作为必填生命周期对象
  完成标准：`PlannerSessionID / PlannerThreadID` 标记为兼容字段或删除；核心状态仍可正常恢复
  说明：相关字段已显式标注为 legacy-only，新 mission 主链路只依赖 workspace + worker。

### M2. 主 Agent 直接承担规划与派发

- [x] TASK-DEL-PLANNER-008 `WorkspaceSession` 直接执行复杂任务 turn
  文件：`internal/server/orchestrator_integration.go`
  目标：`handleWorkspaceChatMessage` 不再调用 `startPlannerMissionTurn`，而是直接在 workspace session 上起 turn
  完成标准：复杂任务直接由主 Agent thread 承接，且不会创建 planner session

- [x] TASK-DEL-PLANNER-009 保留简单状态问题直答快路径
  文件：`internal/server/orchestrator_integration.go`
  目标：继续复用 `answerWorkspaceStateQuery(...)`
  完成标准：状态问答不创建 mission turn，不派发 worker，不触发审批

- [x] TASK-DEL-PLANNER-010 把 `orchestrator_dispatch_tasks` 挂到主 Agent 会话
  文件：`internal/server/dynamic_tools.go`
  目标：原 planner 专属 dispatch 工具改为 workspace 主 Agent 可调用
  完成标准：复杂任务可由主 Agent 直接提交结构化 step 到调度器

- [x] TASK-DEL-PLANNER-011 把 `query_ai_server_state` 挂到主 Agent 会话
  文件：`internal/server/dynamic_tools.go`
  目标：主 Agent 自己回答工作台/主机/审批/进度状态问题
  完成标准：状态问答优先走 ai-server 投影，不走目录遍历或 worker 派发

- [x] TASK-DEL-PLANNER-012 重写主 Agent orchestration prompt
  文件：`internal/orchestrator/prompt.go` 或主 Agent instructions 构建器
  目标：明确主 Agent 既负责用户表达，也负责计划和派发，但不直接做多主机执行
  完成标准：prompt 明确：
  - 简单问题直接回答
  - 复杂问题先给一句规划反馈
  - 然后提交结构化任务
  - 审批由 worker 触发并走右侧审批列表

### M3. Runtime 与 thread/turn 运行时重构

- [x] TASK-DEL-PLANNER-013 删除 planner runtime preset
  文件：`internal/orchestrator/preset.go`、`internal/server/session_runtime.go`
  目标：移除 `planner_internal` 启动规格
  完成标准：不再有 `buildPlannerThreadStartSpec / buildPlannerTurnStartSpec`

- [x] TASK-DEL-PLANNER-014 给主 Agent 增加 orchestrator 模式的 turn spec
  文件：`internal/server/session_runtime.go`
  目标：Workspace 主会话复杂任务时，能以“主 Agent 编排模式”启动 turn
  完成标准：主 Agent 在当前会话中拥有：
  - ai-server 状态查询工具
  - dispatch 工具
  - 但不直接拥有 worker remote exec 能力

- [x] TASK-DEL-PLANNER-015 保持 worker runtime 独立
  文件：`internal/server/session_runtime.go`
  目标：`WorkerSession` 继续使用独立 prompt、独立 cwd、独立 remote toolset
  完成标准：删除 planner 后，worker 隔离边界不受影响

### M4. Worker、审批与事件收敛

- [x] TASK-DEL-PLANNER-016 保持一主机一 worker 的调度模型
  文件：`internal/orchestrator/dispatcher.go`
  目标：不因为删除 planner 而破坏 host 级 worker 复用与串行任务队列
  完成标准：同一 host 的多个 step 仍进入一个逻辑 worker

- [x] TASK-DEL-PLANNER-017 审批只绑定 worker
  文件：`internal/orchestrator/store.go`、`internal/server/orchestrator_integration.go`
  目标：审批 route 只从 `approvalID -> workerSessionID`
  完成标准：前端右侧审批列表只展示 worker 审批，不再出现 planner 口径的审批卡

- [x] TASK-DEL-PLANNER-018 主对话只显示审批提醒，不显示审批控制
  文件：`web/src/pages/ProtocolWorkspacePage.vue`、相关 view model
  目标：主对话里只写“有 1 条审批等待处理”，审批动作仍在右侧列表
  完成标准：聊天流与审批流边界清晰

- [x] TASK-DEL-PLANNER-019 运行态摘要统一回投到主 Agent
  文件：`internal/server/orchestrator_integration.go`
  目标：worker 开始、等待审批、完成、失败都要以摘要形式写回 workspace 主会话
  完成标准：左侧对话始终能看到：
  - `计划生成中`
  - `已派发到 N 台主机`
  - `host-x 正在执行`
  - `host-y 等待审批`
  - `任务已完成`

### M5. 投影模型与接口收敛

- [x] TASK-DEL-PLANNER-020 从 projector 中删除 planner 前台概念
  文件：`internal/orchestrator/projector.go`
  目标：`PlanSummaryView / PlanDetailView` 不再对前端暴露 `PlannerSessionID / PlannerSessionLabel / RawPlannerTraceRef`
  完成标准：前端消费的数据结构不再要求 planner 相关字段

- [x] TASK-DEL-PLANNER-021 重命名计划详情字段
  文件：`internal/orchestrator/projector.go`、`internal/server/orchestrator_integration.go`
  目标：将 `planner conversation` 改为更中性的：
  - `agent planning summary`
  - `dispatch events`
  - `task host bindings`
  完成标准：前台和 API 命名不再暴露 planner

- [x] TASK-DEL-PLANNER-022 历史 mission 明细兼容处理
  文件：`internal/server/orchestrator_history.go`
  目标：老 mission 如果有 planner 字段，仍能回放；新 mission 不再生成 planner 专属内容
  完成标准：历史页不崩，新 mission 不新增 planner 概念

### M6. 前端工作台改造

- [x] TASK-DEL-PLANNER-023 删除前台 planner 术语
  文件：`web/src/pages/ProtocolWorkspacePage.vue`、`web/src/lib/protocolWorkspaceVm.js`
  目标：页面和 view model 中不再出现 `Planner -> AI / Planner -> Host / PlannerSession`
  完成标准：用户只看到主 Agent、background agents、审批列表、实时事件

- [x] TASK-DEL-PLANNER-024 计划控件进一步收敛
  文件：`web/src/components/protocol-workspace/ProtocolInlinePlanWidget.vue`
  目标：plan widget 只保留：
  - 总任务数
  - 已完成数
  - 每步简述
  - host-agent 标签
  - 简单状态
  完成标准：计划控件不再依赖 planner 详情字段

- [x] TASK-DEL-PLANNER-025 background agents 视图只展示 worker
  文件：`web/src/components/protocol-workspace/ProtocolBackgroundAgentsCard.vue`
  目标：只显示 host 级子 agent，不再混入 planner/internal session
  完成标准：背景 agent 列表和右侧审批列表语义一致

- [x] TASK-DEL-PLANNER-026 证据弹框收敛
  文件：`web/src/components/protocol-workspace/ProtocolEvidenceModal.vue`
  目标：从“planner/worker 多轨”收敛为：
  - `主 Agent 计划摘要`
  - `Worker 对话`
  - `Host Terminal`
  - `审批上下文`
  完成标准：证据弹框没有 planner 命名泄漏

### M7. 清理 planner 相关代码路径

- [x] TASK-DEL-PLANNER-027 移除 planner session kind 的前台分流逻辑
  文件：`internal/server/server.go`
  目标：不再允许前台出现 `planner` session kind 处理分支
  完成标准：用户可见 session 只剩 `single_host / workspace`

- [ ] TASK-DEL-PLANNER-028 清理 planner hook 和恢复逻辑
  （部分完成）planner reply mirror、planner interruption 已从主链路移除；legacy planner 失败收敛与兼容恢复逻辑仍保留。
  文件：`internal/server/orchestrator_integration.go`、`internal/orchestrator/manager.go`
  目标：删除 planner 专属的：
  - 恢复失败
  - turn 记录
  - reply mirror
  - planner interruption
  完成标准：mission 失败和恢复只围绕 workspace + worker 处理

- [x] TASK-DEL-PLANNER-029 清理 planner 专属动态工具入口
  文件：`internal/server/dynamic_tools.go`
  目标：删除 `handlePlannerDynamicToolCall` 及其注册路径
  完成标准：dispatch 与 ai-server 状态查询统一归入主 Agent orchestration 工具集

- [x] TASK-DEL-PLANNER-030 清理 planner 历史轨迹读取逻辑
  文件：`internal/server/orchestrator_history.go`、`internal/orchestrator/history.go`
  目标：历史明细不再依赖 planner transcript 作为主证据来源
  完成标准：worker / approval / terminal / dispatch event 成为主要证据

### M8. 测试与验收

- [x] TASK-DEL-PLANNER-031 单元测试：简单状态问题直答
  目标：问“有哪些主机在线”“当前待审批多少条”时，不创建 mission turn
  完成标准：直接返回 ai-server 投影答案

- [x] TASK-DEL-PLANNER-032 单元测试：复杂问题直接由主 Agent 派发
  目标：复杂任务不再创建 planner session
  完成标准：创建 mission 后，workspace 主会话直接承担 planning+dispatch

- [x] TASK-DEL-PLANNER-033 单元测试：审批仍只路由到 worker
  目标：删除 planner 后，审批 route 和继续执行逻辑不回归
  完成标准：approval -> worker session 路径稳定

- [x] TASK-DEL-PLANNER-034 前端回归：主对话中的计划小控件
  目标：复杂任务发送后，左侧能看到简短 plan widget
  完成标准：不需要 planner 会话投影也能展示计划

- [x] TASK-DEL-PLANNER-035 前端回归：右侧审批列表
  目标：复杂任务中的审批仍稳定显示在右侧
  完成标准：聊天流不承担审批动作

- [ ] TASK-DEL-PLANNER-036 真实 smoke：复杂任务
  流程：
  1. 新建工作台
  2. 发送复杂任务
  3. 左侧看到“计划生成中”
  4. plan widget 出现
  5. background agents 出现
  6. 右侧出现审批
  7. 处理审批后任务继续推进
  完成标准：全链路不再依赖 planner session
  （部分完成）已在真实浏览器验证 `/protocol` 会自动进入 workspace，并能看到主 Agent 状态卡、plan widget、background agents / 右侧事件流；同时 `scripts/smoke_workspace_main_agent_0401.mjs` 已跑通 `workspace 创建 -> 状态问题直答 -> 复杂任务进入 planning`。本轮尚未在浏览器里完整回放“新发复杂任务 -> 审批 -> 续跑”整条交互链。

## 6. 风险与回滚点

- [ ] 6.1 风险：主 Agent 上下文变重
  对策：先限制复杂任务规模，避免一开始就覆盖大量主机

- [ ] 6.2 风险：删除 planner 后，部分老投影字段失效
  对策：兼容期保留旧字段只读输出，新链路逐步切换

- [ ] 6.3 风险：复杂任务和简单状态问答的边界判断不准
  对策：先用明确规则分类，再根据真实使用微调

- [ ] 6.4 风险：调度器与主 Agent 的职责重新耦合
  对策：坚持“智能在主 Agent，状态机在 Go 调度器”，不要把生命周期控制放回 LLM

## 7. 最终验收标准

- [x] 7.1 前台页面不再出现 `PlannerSession`、`planner trace`、`Planner -> AI` 等术语
- [x] 7.2 主 Agent 能直接回答工作台状态问题，不触发主机遍历
- [x] 7.3 复杂任务发送后，主对话中能直接看到简短计划控件
- [x] 7.4 子 agent 的执行状态能通过对话摘要和 background agents 小控件看到
- [x] 7.5 所有审批只出现在右侧审批列表
- [x] 7.6 删除 planner 后，mission stop、worker 失联、审批续跑、历史回放仍正常
- [ ] 7.7 自动化测试和真实浏览器 smoke 都通过
  （部分完成）后端单测、前端回归、build 全绿；真实浏览器已验证入口自动进入 workspace 和现有 mission 的主 Agent/plan 展示，`scripts/smoke_workspace_main_agent_0401.mjs` 已验证 workspace 创建、状态直答和复杂任务 planning，但未完成完整复杂任务审批续跑链回放。
