# 协作工作台 Claude Code 式 ReAct 模式实施任务清单

日期：2026-04-08

输入文档：[workspace-claude-code-react-mode-design.md](/Users/lizhongxuan/Desktop/aiops-codex/docs/workspace-claude-code-react-mode-design.md)

交付口径：一次性把协作工作台主链路切换为 Claude Code 式 ReAct loop，不做 MVP 版本，不保留“四分类 route”作为新请求主路径。旧逻辑只允许作为历史数据兼容和回滚保护，不允许继续驱动新任务。

## 0. 总体验收口径

- [x] 用户消息进入协作工作台后，不再先强制走 `direct_answer/state_query/host_readonly/complex_task` 四分类 route。
- [x] 新主链路统一进入 `WorkspaceAgentLoop`，由 loop 完成上下文预处理、附件注入、模型调用、工具执行、错误恢复、后处理和循环决策。
- [x] 用户意图不明确时，主 Agent 必须通过平台自定义动态工具 `ask_user_question` 暂停 loop 并等待用户确认，不得启动 mission、worker、host 命令或 mutation。
- [x] 工作台 prompt 不得要求调用 Codex 内置 `request_user_input`；Codex Default mode 下的澄清统一走 `ask_user_question`，由 ai-server 生成 ChoiceCard 并回灌 tool result。
- [x] `Plan Mode` 成为正式权限状态：计划模式下只允许只读探索和计划更新，不允许执行变更、文件修改、重启服务、派发 mutation worker。
- [x] `ExitPlanMode` 成为计划审批入口，用户批准前不能进入执行模式。
- [x] 所有工具调用都有 `ToolInvocation` 记录、状态、输入摘要、输出摘要和 evidence id。
- [x] 前端证据弹框以工具调用和 evidence 为核心展示，不再用空洞的“主 Agent 计划摘要”兜底。
- [x] 右侧实时事件跨轮保留，重新输入不会清空历史事件。
- [x] loop 使用 `while` + state 对象迭代，不使用递归驱动多轮工具调用。
- [x] 上下文过长、输出截断、模型过载、工具失败、app-server 断连都有分层恢复和熔断。
- [x] Playwright 能覆盖“能力询问 -> 澄清 -> 只问能力 / 只读诊断 / 计划审批 / 执行”的完整路径。

## 1. 现状梳理与切换边界

- [x] 梳理当前协作工作台消息入口，标出所有调用 `handleWorkspaceChatMessage`、`startWorkspaceRouteTurn`、`BuildWorkspaceRoutePrompt`、`parseWorkspaceRouteReply` 的路径。
- [x] 标出所有新请求会触发 `complex_task`、`host_readonly`、`state_query` 的自动推进逻辑。
- [x] 标出当前 mission、worker、approval、card、snapshot、WebSocket、持久化之间的数据依赖。
- [x] 标出现有 route 相关测试，决定哪些改写为 ReAct loop 测试，哪些删除，哪些仅保留为历史兼容测试。
- [x] 明确新主链路切换点：新 workspace chat 请求必须进入 `WorkspaceAgentLoop`，不能再进入 route thread。
- [x] 明确旧 route thread 处置策略：不再服务新请求；如需保留，仅用于旧数据恢复、调试或短期回滚开关。
- [x] 为本次改造定义 feature/config 名称，例如 `workspace_react_loop_enabled`，默认开启；该开关只用于紧急回滚，不作为 MVP 双轨长期保留。

## 2. 数据模型与持久化

### 2.1 Loop 运行态

- [x] 新增 `AgentLoopRun` 模型，字段至少包括 `id`、`sessionId`、`status`、`mode`、`createdAt`、`updatedAt`、`activeIterationId`、`lastError`。
- [x] `AgentLoopRun.status` 支持 `running`、`waiting_user`、`waiting_approval`、`completed`、`failed`、`canceled`。
- [x] `AgentLoopRun.mode` 支持 `answer`、`readonly`、`plan`、`execute`。
- [x] 新增 `AgentLoopIteration` 模型，字段至少包括 `id`、`runId`、`index`、`stopReason`、`needsFollowUp`、`modelAttempt`、`recoveryAttempt`、`startedAt`、`completedAt`。
- [x] 每次模型调用、错误恢复重试和 tool follow-up 都必须有可追踪 iteration。
- [x] 持久化 loop run 与 iteration，确保进程重启后能展示历史证据和失败状态。

### 2.2 工具调用证据

- [x] 新增 `ToolInvocation` 模型，字段至少包括 `id`、`runId`、`iterationId`、`name`、`status`、`inputJSON`、`outputJSON`、`inputSummary`、`outputSummary`、`evidenceId`、`startedAt`、`completedAt`。
- [x] `ToolInvocation.status` 支持 `pending`、`running`、`waiting_user`、`waiting_approval`、`completed`、`failed`、`canceled`。
- [x] 新增 `EvidenceRecord` 或等价结构，用于保存完整命令输出、工具结果、计划内容、澄清问答、审批上下文。
- [x] 主上下文只塞 evidence summary 和 evidence id，不塞大段完整输出。
- [x] 所有 `CommandCard`、审批、worker 回执、计划更新都要能关联到 `ToolInvocation` 或 `EvidenceRecord`。
- [x] Snapshot 中增加 `toolInvocations` 或 evidence 索引投影，前端可按 id 打开详情。

### 2.3 用户交互状态

- [x] 新增 `UserQuestion` 或复用 `ToolInvocation(name=ask_user_question)` 表示等待用户澄清。
- [x] 保存问题、选项、推荐项、多选标记、用户选择、自由文本输入、提交时间。
- [x] 新增计划审批状态，保存 `ExitPlanMode` 提交的计划摘要、计划正文、风险、验证方式、用户审批结果。
- [x] 明确等待用户输入和等待审批时 runtime turn 是否占用 active 状态，避免用户重复发起冲突任务。

## 3. WorkspaceAgentLoop 后端控制器

### 3.1 Loop 主控

- [x] 新增 `WorkspaceAgentLoop` 控制器，作为 workspace chat 新主入口。
- [x] `handleWorkspaceChatMessage` 改为创建或恢复 `AgentLoopRun`，然后调用 loop 控制器。
- [x] loop 使用 `while` + `AgentLoopState`，不要递归调用自身。
- [x] `AgentLoopState` 至少包含 `messages`、`attachments`、`availableTools`、`permissionMode`、`planMode`、`tracking`、`runId`、`iterationIndex`、`abortSignal`。
- [x] 每轮 loop 开始前调用上下文预处理。
- [x] 每轮模型调用前重建动态附件。
- [x] 每轮工具执行后把工具结果转换为下一轮模型可观察的消息。
- [x] `needsFollowUp=true` 时整体替换 state 并继续循环。
- [x] `needsFollowUp=false` 时收敛 runtime turn 并写完成状态。
- [x] loop 支持取消：用户点击停止后，终止模型调用、取消未开始工具、标记运行态为 `canceled`。
- [x] loop 支持幂等：同一个用户提交或 tool result 不能重复写入两次。

### 3.2 Codex app-server 适配

- [x] 抽象 `ModelStreamClient`，屏蔽 Codex app-server 与未来自建 model client 的差异。
- [x] Codex `thread/start` / `turn/start` 仍可作为底层通道，但上层只消费统一的 `ModelStreamEvent`。
- [x] 将 Codex notification 映射为 `assistant_delta`、`tool_use`、`tool_result`、`approval_pending`、`turn_completed`、`turn_failed`。
- [x] 通过 `dynamicTools` 暴露平台工具 `ask_user_question`，不要把工作台澄清能力绑定到 Codex 内置 `request_user_input`。
- [x] prompt builder 必须使用 `ask_user_question` 作为澄清工具名，并明确禁止工作台 Default mode 调用 `request_user_input`。
- [x] 如果 Codex runtime 返回 `request_user_input unavailable/rejected`，记录为 prompt/runtime 配置错误并提示改用 `ask_user_question`，不要让它进入正常用户澄清路径。
- [x] 不允许在 Codex readLoop 同步回调里再次同步发起新的模型请求；所有 follow-up 由 loop goroutine 驱动。
- [x] 模型流式输出时持续更新聊天 UI，但工具执行状态以 `ToolInvocation` 为准。
- [x] `turn/completed` 时根据 stop reason 和 pending tool 判断是否继续 loop。

### 3.3 Runtime 与并发

- [x] 一个 workspace session 同时只能有一个 active loop run，除非显式支持并行任务。
- [x] `waiting_user` 和 `waiting_approval` 状态必须明确阻塞当前 loop，但允许用户回答对应问题或审批。
- [x] 新消息到来时，如果当前 loop 正在等待用户澄清，则按“回答澄清问题”处理，而不是启动新任务。
- [x] 新消息到来时，如果当前 loop 正在执行中，保持现有冲突提示或实现队列；不能静默打断。
- [x] 重新加载页面后，前端能看到当前 loop 的等待状态和可继续操作入口。

## 4. 七阶段流水线实现

### 4.1 阶段 1：上下文预处理

- [x] 实现 `enforceToolResultBudget`，限制单条工具结果进入模型上下文的字符预算。
- [x] 实现命令输出摘要化，大输出只保留首尾、错误关键行、退出码、耗时和 evidence id。
- [x] 实现 `microcompactMessages`，清理旧工具结果正文，只保留摘要和 evidence id。
- [x] 实现 `snipCompactIfNeeded`，当历史超预算时裁剪低价值内容。
- [x] 实现 `autoCompactIfNeeded`，上下文接近上限时生成会话摘要。
- [x] 为 compact 结果增加 provenance，证据弹框仍能找到原始 evidence。
- [x] 为 compact 失败设置连续失败计数器。

### 4.2 阶段 2：附件注入

- [x] 实现 `workspace_state` 附件：session、selected host、mission、run status、current mode。
- [x] 实现 `approval_state` 附件：待审批数、审批类型、是否阻塞当前 run。
- [x] 实现 `event_summary` 附件：最近实时事件摘要和历史事件 evidence id。
- [x] 实现 `plan_mode` 附件：是否计划模式、计划文件/计划记录位置、只读约束。
- [x] 实现 `permission_mode` 附件：是否允许 mutation、是否允许 worker dispatch、是否只读。
- [x] 实现 `host_context` 附件：目标 host 在线状态、默认 cwd、host-agent 能力。
- [x] 实现 `tool_schema_delta` 附件：本轮新增/变更工具 schema。
- [x] 实现 `memory_prefetch` 附件：经验包、长期记忆或项目上下文摘要。
- [x] 实现 `mcp_instructions_delta` 附件：MCP 工具说明和连接状态变化。
- [x] 附件必须可测试：同一输入状态生成稳定文本或稳定结构。

### 4.3 阶段 3：API 调用

- [x] 实现 `callModel` 抽象，输入包含 messages、effective prompt、tools、abort signal、model profile。
- [x] 支持流式 assistant 文本增量。
- [x] 支持流式 tool use block 收集。
- [x] 支持边收边执行的预留接口；如暂不启用，必须保证完整 tool block 后立即进入工具执行。
- [x] 支持 fallback model 参数。
- [x] 支持 max output token 升级参数。
- [x] 所有模型调用记录 iteration id、耗时、模型名、token 估算、失败原因。

### 4.4 阶段 4：错误恢复

- [x] `prompt_too_long` 先走 context collapse drain。
- [x] drain 失败后走 reactive compact。
- [x] reactive compact 失败后截断最旧低价值消息。
- [x] compact 连续失败达到 `MAX_CONSECUTIVE_AUTOCOMPACT_FAILURES=3` 后熔断。
- [x] `max_output_tokens` 先尝试提高输出限制。
- [x] 提高限制仍失败时注入恢复消息，要求模型 no apology、no recap、直接继续。
- [x] 模型过载时切 fallback model。
- [x] Codex app-server 断连时终止当前 iteration，保留用户可见错误和重试入口。
- [x] 工具超时不直接吞掉，必须作为失败观察回灌给模型或显式终止。
- [x] 所有恢复行为写入 `AgentLoopIteration.recoveryAttempt` 和 evidence。

### 4.5 阶段 5：工具执行

- [x] 实现工具调度器，接收一批 tool_use blocks。
- [x] 只读工具可并行执行。
- [x] 同一 host 的命令和 mutation 工具默认串行。
- [x] 阻塞型工具 `ask_user_question` 执行后暂停 loop。
- [x] 审批型工具 `ExitPlanMode` 和 `RequestApproval` 执行后暂停 loop。
- [x] mutation 工具必须检查 permission mode。
- [x] worker dispatch 工具必须检查用户授权或计划审批。
- [x] 每个工具执行前写 `ToolInvocation(status=running)`。
- [x] 每个工具执行后写 output summary 和 evidence id。
- [x] 工具失败必须保留 input、错误、stderr、退出码或异常栈摘要。

### 4.6 阶段 6：后处理

- [x] 将 assistant 输出整理成聊天流卡片。
- [x] 将工具调用整理成右侧实时事件。
- [x] 将计划更新整理成计划小卡。
- [x] 将审批工具整理成右侧审批列表。
- [x] 将 worker 回执整理成主对话摘要和 evidence。
- [x] 运行 stop hook 或 mission completion hook，确保不会阻塞 notification readLoop。
- [x] 更新 session runtime 状态和 snapshot。
- [x] 持久化 transcript 时只保存必要投影，完整大输出走 evidence。

### 4.7 阶段 7：循环决策

- [x] 统一定义 `StopReason`：`end_turn`、`tool_use`、`max_tokens`、`waiting_user`、`waiting_approval`、`failed`、`canceled`。
- [x] `tool_use` 且工具执行完成后，`needsFollowUp=true`。
- [x] `waiting_user` 和 `waiting_approval` 时，`needsFollowUp=false` 但 run 不完成，状态保持等待。
- [x] `end_turn` 且无 pending tool 时，run completed。
- [x] `max_tokens` 进入错误恢复，未超过阈值时继续。
- [x] `failed` 按错误类型决定恢复或终止。
- [x] 每次循环决策都写入 iteration 和调试日志。

## 5. Prompt 与工具说明

### 5.1 Effective Prompt 组装

- [x] 拆分静态系统提示、项目 developer instructions、动态附件、工具说明和用户上下文。
- [x] 静态提示只包含稳定身份、职责、安全边界和输出风格。
- [x] 动态权限、host 状态、计划模式不要写死在静态提示里。
- [x] 实现 prompt builder，并输出可测试的 prompt sections。
- [x] 为 prompt builder 增加快照测试，覆盖普通模式、只读模式、计划模式、等待审批模式。
- [x] 删除或停用新主链路中的 `BuildWorkspaceRoutePrompt` 依赖。

### 5.2 意图澄清规则

- [x] 在 prompt 中加入能力询问识别规则：`can you`、`do you have a way`、`能不能`、`有没有办法`、`可以吗`、`会不会`、`是否能处理`。
- [x] 明确规定：能力询问不等于授权诊断、修改、派发 worker 或执行命令。
- [x] 对数据库、部署、恢复、同步、生产系统和高风险变更，未授权时必须先澄清。
- [x] 对明确只读请求，例如“开始只读检查，不要修改”，允许直接进入只读工具。
- [x] 对明确执行请求，例如“按计划执行修复”，允许在权限满足时进入执行。

### 5.3 工具提示词

- [x] 编写 `ask_user_question` 工具说明，强调澄清意图、需求、授权和关键决策。
- [x] `ask_user_question` 工具说明必须包含：这是平台 dynamic tool；不要调用 Codex 内置 `request_user_input`；Default mode 澄清统一使用该工具。
- [x] 编写 `EnterPlanMode` 工具说明，强调何时进入计划模式。
- [x] 编写 `UpdatePlan` 工具说明，要求计划包含目标、范围、证据、步骤、风险、验证。
- [x] 编写 `ExitPlanMode` 工具说明，强调它是计划审批入口，不要用普通文本问“是否同意计划”。
- [x] 编写 `DispatchWorkers` 工具说明，强调授权前不可用。
- [x] 编写 `ReadonlyHostInspect` 工具说明，强调只读范围和禁止 mutation。
- [x] 编写 `RequestApproval` 工具说明，强调 mutation 操作必须审批。
- [x] 所有工具说明都加入 schema 示例和失败返回约定。

## 6. 工具实现清单

### 6.1 ask_user_question

- [x] 后端实现 `ask_user_question` dynamic tool；展示名可以是 `AskUserQuestion`，但 runtime/tool schema 名必须是 `ask_user_question`。
- [x] 从工作台 prompt、tool schema 和测试快照中移除对 Codex 内置 `request_user_input` 的依赖。
- [x] 通过 Codex app-server dynamic tool 通道注册 `ask_user_question`，确保 Codex Default mode 将其视为普通 function tool。
- [x] 输入支持 1 到 3 个问题，每个问题 2 到 4 个选项。
- [x] 支持 `recommended` 标记或推荐选项排第一。
- [x] 支持自由文本 `Other`。
- [x] 执行后创建澄清卡片和 `ToolInvocation(status=waiting_user)`。
- [x] 用户提交答案后更新 invocation，并把答案作为 tool result 回灌 loop。
- [x] `ask_user_question` tool result 按 Codex `DynamicToolCallResponse(contentItems + success)` 协议返回，结构化字段作为 JSON `inputText` 提供给模型。
- [x] 用户选择“准备修复 / 执行计划”类澄清答案后，tool result 必须携带 `next_required_tool=enter_plan_mode`，禁止模型用普通文本替代计划入口。
- [x] 模型重复调用相同 `ask_user_question` 时，复用已完成答案，并保留 `next_required_tool` 等后续动作约束。
- [x] 等待期间刷新页面仍能继续回答。
- [x] 工具执行失败或用户关闭澄清卡时，状态要写入 `ToolInvocation`，并给 loop 一个可观察的失败/取消结果。

### 6.2 EnterPlanMode

- [x] 后端实现 `EnterPlanMode` dynamic tool。
- [x] 执行后把 run mode 切到 `plan`。
- [x] 注入 plan mode 附件。
- [x] 限制可用工具集为只读工具、`UpdatePlan`、`ask_user_question`、`ExitPlanMode`。
- [x] 前端展示计划模式状态，但不遮挡对话输出和输入框。

### 6.3 UpdatePlan

- [x] 后端实现 `UpdatePlan` dynamic tool。
- [x] 计划结构包含背景、目标、范围、只读发现、实施步骤、风险、回滚、验证。
- [x] 每次更新生成计划 evidence。
- [x] 前端计划小卡展示摘要、任务数、已完成数、最近更新。
- [x] 点击计划卡打开完整计划 evidence。

### 6.4 ExitPlanMode

- [x] 后端实现 `ExitPlanMode` dynamic tool。
- [x] 校验计划存在且包含必要章节。
- [x] 创建计划审批卡，而不是普通聊天文本询问。
- [x] 审批通过后 run mode 切到 `execute` 并把审批结果回灌 loop。
- [x] 审批拒绝后 run mode 回到 `plan` 或结束，取决于用户选择。
- [x] 审批卡必须包含计划摘要、风险、将要执行的 worker/mutation 范围。

### 6.5 QueryAiServerState

- [x] 将现有 `query_ai_server_state` 纳入新工具体系。
- [x] 输出结构化状态，而不是大段自然语言。
- [x] 支持查询 session、mission、host、approval、event、runtime。
- [x] 查询结果生成 evidence summary。

### 6.6 ReadonlyHostInspect

- [x] 实现只读 host 检查工具，统一封装当前 server-local/remote host 只读能力。
- [x] 工具参数包含 host id、检查目标、允许命令类别、cwd。
- [x] 禁止写文件、重启、kill、修改配置、执行危险命令。
- [x] 命令输出统一进入 evidence。
- [x] 对离线 host 返回可解释错误，而不是自动降级到本机。

### 6.7 DispatchWorkers

- [x] 将现有 `orchestrator_dispatch_tasks` 改造成新 loop 下的 `DispatchWorkers`。
- [x] 强制检查 run mode 和授权状态。
- [x] 支持只读 worker 与执行 worker 的权限差异。
- [x] 每个派发任务生成 `ToolInvocation` 和 mission/task 关联。
- [x] worker 结果 fan-in 后以 tool result 形式回灌主 Agent loop。
- [x] 派发失败时保留失败 evidence，并允许模型重规划或询问用户。

### 6.8 RequestApproval

- [x] mutation 命令和文件变更统一走 `RequestApproval`。
- [x] 审批上下文包含命令、host、cwd、风险、预期影响、回滚建议。
- [x] 用户批准后继续对应工具或 worker。
- [x] 用户拒绝后把拒绝结果回灌 loop，让模型调整方案。

### 6.9 SummarizeEvidence

- [x] 实现 evidence 摘要工具或后处理器。
- [x] 长命令输出生成关键行摘要。
- [x] worker 多任务结果生成按 host/step 聚合摘要。
- [x] 摘要必须保留 evidence id，支持点击查看原始输出。

## 7. Mission / Worker / Approval 整合

- [x] 保留 Go 调度器对 mission、worker、budget、approval 的可靠控制。
- [x] `DispatchWorkers` 作为唯一从主 Agent loop 进入 worker 调度的入口。
- [x] worker 不直接面向用户输出，必须通过 loop fan-in 或 evidence 投影回主对话。
- [x] worker 产生审批时，主 loop 状态进入 `waiting_approval` 或记录外部阻塞状态。
- [x] worker 完成后把结果作为 tool result 回灌主 loop，允许主 Agent 继续总结、重规划或追问。
- [x] 多 worker 并发结果按 host 和 task 聚合，避免刷爆主对话。
- [x] worker 失败策略明确：可重试、可跳过、需用户决策、终止 mission。
- [x] 取消当前任务时同时取消未开始 worker，并向运行中 worker 发送取消信号。

## 8. 前端改造清单

### 8.1 ViewModel 与状态

- [x] 更新 protocol workspace view model，消费 `AgentLoopRun`、`ToolInvocation`、`EvidenceRecord`。
- [x] 右侧实时事件改为基于 tool invocation / evidence，而不是仅基于当前 mission。
- [x] 实时事件跨轮保留，按时间倒序或正序稳定展示。
- [x] 新消息发送后不得清空历史事件。
- [x] 输入框根据 run status 展示可发送、等待用户回答、等待审批、执行中、已取消状态。

### 8.2 澄清卡片

- [x] 新增 `ask_user_question` 卡片组件；界面标题可显示为“澄清问题”或 `AskUserQuestion`，但状态和证据使用 runtime 工具名。
- [x] 同一个澄清卡片在 snapshot / runtime 状态刷新时保留用户已选选项，避免提交时回退到默认推荐项。
- [x] 模型重复调用相同 `ask_user_question` 时复用最新已完成答案，不再生成重复澄清卡。
- [x] 支持单选、推荐选项、Other 输入。
- [x] 支持多选澄清问题。
- [x] 提交后禁用重复提交。
- [x] 提交失败时保留用户选择并显示错误。
- [x] 回放历史时显示用户最终选择。

### 8.3 计划模式 UI

- [x] 计划小卡默认折叠，样式与 agents 小卡一致。
- [x] 折叠状态不遮挡对话输出和输入框。
- [x] 展开时展示计划摘要、步骤、风险、验证，不展示内部 thread 名称。
- [x] `ExitPlanMode` 审批卡展示计划批准/拒绝入口。
- [x] 计划审批后显示审批结果和后续执行状态。

### 8.4 证据弹框

- [x] 证据弹框默认展示当前点击的 `ToolInvocation`。
- [x] 支持 tab：输入、输出、原始 evidence、关联审批、关联 worker、关联计划。
- [x] 命令详情显示 host、cwd、命令、退出码、耗时、stdout/stderr 摘要和完整输出入口。
- [x] `ask_user_question` 详情显示问题、选项和用户选择。
- [x] `DispatchWorkers` 详情显示主 Agent 发给子 Agent 的任务全文、约束、host 映射。
- [x] 不再出现没有正文价值的“主 Agent 计划摘要”空页面。

### 8.5 审批与等待态

- [x] 右侧审批列表展示 `ExitPlanMode`、`RequestApproval`、worker approval。
- [x] 输入框附近展示当前阻塞原因。
- [x] 用户处理审批后，前端立即刷新对应 invocation 和 run 状态。
- [x] 审批拒绝时显示模型后续处理结果，而不是静默结束。

## 9. API / WebSocket / Snapshot

- [x] 新增或扩展 workspace snapshot，包含 loop run、iterations、tool invocations、evidence summaries。
- [x] 新增提交用户澄清答案 API。
- [x] 新增计划审批 API 或复用现有 approval API 并区分 approval kind。
- [x] 新增按 evidence id 读取完整证据 API。
- [x] 新增按 invocation id 读取工具详情 API。
- [x] WebSocket 事件区分 `loop_started`、`iteration_started`、`tool_started`、`tool_completed`、`waiting_user`、`waiting_approval`、`loop_completed`、`loop_failed`。
- [x] 大输出不通过 snapshot 全量推送，只推 summary 和 evidence id。
- [x] 高频 output delta 使用节流或增量通道，避免广播风暴。

## 10. 历史数据与兼容

- [x] 旧 session 中的 route/planning/worker card 仍能展示。
- [x] 旧 mission 恢复时不自动迁移成新 loop run，除非用户继续该会话。
- [x] 用户在旧会话继续输入时，新输入进入 `WorkspaceAgentLoop`。
- [x] 旧 `PlannerSession`、route thread 等字段只作为历史 evidence 显示，不暴露给用户。
- [x] 删除或隐藏前端所有 “PlannerSession / 影子 session / planner trace” 用户可见文案。
- [x] 为旧数据加载增加回归测试，确保不会因缺少 loop 字段崩溃。

## 11. 测试清单

### 11.1 Go 单元测试

- [x] prompt builder 普通模式快照测试。
- [x] prompt builder plan mode 快照测试。
- [x] prompt builder waiting approval 快照测试。
- [x] attachment builder 覆盖 workspace、approval、event、host、permission、tool schema。
- [x] context compact 预算测试。
- [x] compact 连续失败熔断测试。
- [x] stop reason 到 loop 决策映射测试。
- [x] `ask_user_question` tool schema 和状态转换测试。
- [x] prompt builder 快照断言包含 `ask_user_question`，且不包含要求模型调用 `request_user_input` 的工作台指令。
- [x] Codex Default mode 适配测试：澄清通过 dynamic tool `ask_user_question` 触发 ChoiceCard，不依赖内置 `request_user_input`。
- [x] EnterPlanMode / UpdatePlan / ExitPlanMode 状态转换测试。
- [x] DispatchWorkers 授权拦截测试。
- [x] ReadonlyHostInspect 禁止 mutation 测试。
- [x] EvidenceRecord 生成和读取测试。

### 11.2 Go 集成测试

- [x] “你有办法修复 pg 不同步的问题吗？”生成平台澄清 ChoiceCard / `ask_user_question` 等价事件，不创建 mission。
- [x] 用户选择“只问能力”后直接回答，不执行工具。
- [x] 用户选择“准备修复 / 执行计划”类澄清答案后，回灌 `next_required_tool=enter_plan_mode`。
- [x] 重复 `ask_user_question` 复用已完成答案时仍保留 `next_required_tool=enter_plan_mode`。
- [x] 用户选择“开始只读诊断”后只调用只读工具，不产生 mutation approval。
- [x] 用户进入计划模式后，mutation 工具被拒绝。
- [x] `ExitPlanMode` 审批通过后允许 `DispatchWorkers`。
- [x] `ExitPlanMode` 审批拒绝后回灌 `next_mode=plan`，run mode 回到 `planning`。
- [x] worker 结果 fan-in 后主 loop 继续总结。
- [x] app-server `max_tokens` 模拟恢复。
- [x] app-server `prompt_too_long` 模拟 compact。
- [x] app-server 断连后 loop failed 且保留 evidence。
- [x] 页面刷新后 waiting_user 状态可恢复。

### 11.3 前端单元测试

- [x] view model 渲染跨轮实时事件。
- [x] view model 不因新用户消息清空事件流。
- [x] `ask_user_question` 卡片提交和历史回放。
- [x] 计划小卡折叠/展开布局。
- [x] 证据弹框按 invocation 渲染命令详情。
- [x] 证据弹框按 invocation 渲染 DispatchWorkers 任务全文。
- [x] 审批卡处理后状态刷新。
- [x] 输入框等待态提示。

### 11.4 Playwright 端到端测试

- [x] 打开 `/protocol`，输入“你有办法修复 pg 不同步的问题吗？”。
- [x] 断言没有出现“plan 正在运行中”。
- [x] 断言出现澄清卡片和三个选项。
- [x] 选择“只问能力”，断言没有命令事件和 mission。
- [x] 再次输入同一句，选择“开始只读诊断”。
- [x] 断言只读工具事件出现。
- [x] 选择“制定修复计划”，断言模型进入 Plan Mode 并生成 `ExitPlanMode` 计划审批卡。
- [x] 点击实时事件，断言证据弹框显示工具名、输入、输出摘要。
- [x] 输入明确计划请求，断言进入 Plan Mode。
- [x] 点击 `ExitPlanMode` 审批拒绝，断言页面显示 `decline` 事件并回到规划/等待补充输入。
- [x] 点击 `ExitPlanMode` 审批通过，断言允许 worker dispatch。
- [x] worker 完成后断言主对话输出汇总，右侧事件保留历史。

## 12. 删除与替换清单

- [x] 新主链路不再调用 `BuildWorkspaceRoutePrompt`。
- [x] 新主链路不再调用 `startWorkspaceRouteTurn`。
- [x] 新主链路不再依赖 `parseWorkspaceRouteReply`。
- [x] route 相关 UI 文案全部替换为 loop / tool / evidence 文案。
- [x] route 相关测试改写为 loop 测试。
- [x] 保留 legacy route 代码时必须加注释，说明只用于历史兼容或回滚，不服务新请求。
- [x] 移除任何会在用户意图不明确时自动创建 mission 的路径。

## 13. 运维与可观测性

- [x] 日志包含 `runId`、`iterationId`、`toolInvocationId`、`sessionId`。
- [x] 指标包含 loop duration、iteration count、tool count、waiting_user count、waiting_approval count。
- [x] 指标包含 compact attempt、compact failure、fallback model count、max token recovery count。
- [x] 指标包含 authorization_required 拦截次数，用于观察是否问得过多。
- [x] 错误日志必须区分模型错误、工具错误、权限错误、用户取消、app-server 断连。
- [x] 前端调试面板或日志能定位某个实时事件对应的 invocation/evidence。

## 14. 完成交付检查

- [x] `go test ./internal/server` 通过。
- [x] 相关 Go 包测试全部通过。
- [x] 前端单元测试通过。
- [x] `npm run build` 通过。
- [x] `git diff --check` 通过。
- [x] Playwright 全流程通过。
- [x] 手工验证“能力询问不执行”。
- [ ] 手工验证“明确只读诊断直接只读执行”。
- [ ] 手工验证“计划审批前不能 dispatch”。
- [ ] 手工验证“审批通过后能 dispatch 并 fan-in 总结”。
- [x] 手工验证“实时事件跨轮保留”。
- [x] 手工验证“证据弹框展示具体工具输入输出”。
- [x] 文档更新：设计文档、任务清单、架构说明、回滚说明。
