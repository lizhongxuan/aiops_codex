# 两个 Chat 对话框对齐 Codex / Claude Code UX 的超详细实施清单

基于 [chat_fix_codex.md](/Users/lizhongxuan/Desktop/aiops-codex/chat_fix_codex.md) 的整理结论，下面把两个 chat 的改造拆成一份可直接执行的工程任务清单，并把后续的 MCP 监控图表卡、控制面板卡一起纳入实施范围。

适用范围：

- [ChatPage.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/pages/ChatPage.vue)
- [ProtocolConversationPane.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/components/protocol-workspace/ProtocolConversationPane.vue)
- [ProtocolWorkspacePage.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/pages/ProtocolWorkspacePage.vue)
- [protocolWorkspaceVm.js](/Users/lizhongxuan/Desktop/aiops-codex/web/src/lib/protocolWorkspaceVm.js)
- [workspaceViewModel.js](/Users/lizhongxuan/Desktop/aiops-codex/web/src/lib/workspaceViewModel.js)

默认假设：

- 共用 turn 语义模型，但允许页面层分别适配
- 本轮以前端重构为主，不主动改后端协议，除非 formatter 确认缺字段
- 审批、timeline、evidence modal 的业务语义保持不变，只调整展示边界
- MCP 响应优先走结构化 UI payload，不允许以 raw JSON 或大段 markdown 直接塞回聊天正文

状态说明：

- `[ ]` 未开始
- `[~]` 进行中
- `[x]` 已完成
- `[!]` 有风险或需额外确认

---

## 1. 总体验收目标

- [ ] 用户第一眼看到的是“结果 / 当前阻塞”，而不是过程噪音
- [ ] 两个 chat 都以 turn 为最小渲染单元，而不是 raw cards
- [ ] 过程层默认可折叠，展开后仍然清晰可读
- [ ] plan / background agents / approval / timeline 各回各位，不再混入正文
- [ ] 用户滚离底部时不会被强制抢走阅读位置
- [ ] 长会话可以加载历史且不明显卡顿
- [ ] 输入器能更稳地处理大块粘贴、图片和路径
- [ ] MCP 监控图表卡和控制面板卡能自然进入 chat，并且不会破坏 turn 结构

---

## 2. 里程碑总览

### P0. 基线冻结

目标：

- 明确当前两个 chat 的真实状态和验收样本

交付物：

- 场景样本清单
- 截图基线
- 行为验收矩阵

### P1. 共享基础层

目标：

- 建立统一的 `turn / process / final` 数据模型
- 明确折叠、摘要、排序、滚动规则

交付物：

- 共享 formatter
- 共享分类规则
- 共享 scroll / unread 状态模型

### P2. 协议工作台先落地

目标：

- 先把协议工作台从 `normalizedMessages + statusCard` 改成真正的 turn 线程

交付物：

- `ProtocolTurnGroup`
- `ProtocolProcessFold`
- `formattedTurns`

### P3. 主聊天同步升级

目标：

- 把主聊天从卡片流升级成 turn 级线程

交付物：

- `ChatTurnGroup`
- `ChatProcessFold`
- `ChatComposerDock`

### P4. 共性交互增强

目标：

- 完成 unread、history、virtualization、away summary、输入器增强

交付物：

- scroll/unread 行为
- 顶部分页 sentinel
- 虚拟滚动
- 输入器增强

### P4A. MCP UI 卡片与控制面板

目标：

- 把监控图表、KPI、控制面板、变更表单定义成结构化 UI surface，并接进两个 chat

交付物：

- `McpUiCard` 数据契约
- `McpUiCardHost`
- 只读图表卡
- 可变更控制面板卡
- MCP drawer / approval / timeline / evidence 整合

### P5. 测试与发布收口

目标：

- 为结构、行为和视觉建立稳定回归保护

交付物：

- 单测
- 页面测试
- 截图验收
- 上线检查清单

---

## 3. P0 基线冻结

### [ ] TASK-CHAT-FIX-001 盘点两个 chat 的现有状态来源

- 目标：明确两个页面现在分别依赖哪些 cards、runtime 字段、computed 状态，避免后续 formatter 漏接数据。
- 涉及文件：
  - [ChatPage.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/pages/ChatPage.vue)
  - [ProtocolConversationPane.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/components/protocol-workspace/ProtocolConversationPane.vue)
  - [protocolWorkspaceVm.js](/Users/lizhongxuan/Desktop/aiops-codex/web/src/lib/protocolWorkspaceVm.js)
- 前置依赖：无
- 实施动作：
  1. 列出主聊天使用的运行态字段：`store.snapshot.cards`、`store.runtime.activity`、`store.runtime.turn`、`pendingApprovals`、`activePlanCard`、`latestTerminalCard`。
  2. 列出协议工作台使用的输入字段：`messages`、`conversationCards`、`planCards`、`stepItems`、`backgroundAgents`、`runningAgents`、`statusCard`。
  3. 标记哪些字段是“最终消息”，哪些是“过程痕迹”，哪些是“阻塞态”。
- 完成标准：文档或注释中能清楚说明两个 chat 的输入面。
- 验证方式：后续 formatter 任务不再需要临时补找数据源。

### [ ] TASK-CHAT-FIX-002 冻结 6 组验收样本

- 目标：提前准备后续结构改造的回归样本。
- 涉及文件：
  - [web/tests/ChatPage.spec.js](/Users/lizhongxuan/Desktop/aiops-codex/web/tests/ChatPage.spec.js)
  - [web/tests/ProtocolWorkspacePage.spec.js](/Users/lizhongxuan/Desktop/aiops-codex/web/tests/ProtocolWorkspacePage.spec.js)
  - [web/tests/chat-ui-visual.spec.js](/Users/lizhongxuan/Desktop/aiops-codex/web/tests/chat-ui-visual.spec.js)
  - [web/tests/protocol-chat-ui.spec.js](/Users/lizhongxuan/Desktop/aiops-codex/web/tests/protocol-chat-ui.spec.js)
- 前置依赖：TASK-CHAT-FIX-001
- 实施动作：
  1. 准备“简单问答已完成”样本。
  2. 准备“读取文件 + 搜索 + 完成回答”样本。
  3. 准备“等待审批”样本。
  4. 准备“失败”样本。
  5. 准备“后台 agent 运行中”样本。
  6. 准备“长线程含多轮消息”样本。
- 完成标准：测试中有可复用 fixture 或 mock 数据。
- 验证方式：两页的主要状态都能在本地稳定复现。

### [ ] TASK-CHAT-FIX-003 补一份统一验收矩阵

- 目标：把“看起来像 Codex”拆成可执行的行为检查项。
- 涉及文件：
  - [todo_chat_fix_codex.md](/Users/lizhongxuan/Desktop/aiops-codex/todo_chat_fix_codex.md)
  - [chat_fix_codex.md](/Users/lizhongxuan/Desktop/aiops-codex/chat_fix_codex.md)
- 前置依赖：TASK-CHAT-FIX-002
- 实施动作：
  1. 把验收维度拆成结构、滚动、折叠、输入器、性能、视觉六类。
  2. 给每类补明确的“完成时应该看到什么”。
  3. 给每类补至少一个自动化或人工验证入口。
- 完成标准：不再依赖抽象描述判断是否达标。
- 验证方式：后续每个任务都能挂靠到一条验收项。

---

## 4. P1 共享基础层

### [ ] TASK-CHAT-FIX-004 新增共享 formatter 文件

- 目标：建立两个 chat 都能复用的 turn 级格式化入口。
- 涉及文件：
  - [web/src/lib/chatTurnFormatter.js](/Users/lizhongxuan/Desktop/aiops-codex/web/src/lib/chatTurnFormatter.js)
- 前置依赖：TASK-CHAT-FIX-001
- 实施动作：
  1. 定义共享输出结构：`turns`、`widgets`、`blockingState`、`meta`。
  2. 定义 turn 字段：`id`、`status`、`elapsedLabel`、`userMessage`、`processGroup`、`finalMessage`。
  3. 暴露最少两个入口：一个给主聊天，一个给协议工作台。
- 完成标准：页面层不再直接从 raw cards 推断 turn 结构。
- 验证方式：formatter 输出的 shape 稳定且可测试。

### [ ] TASK-CHAT-FIX-005 在共享 formatter 中补语义分类器

- 目标：统一决定卡片属于 user、process、final、approval、error 等哪一类。
- 涉及文件：
  - [web/src/lib/chatTurnFormatter.js](/Users/lizhongxuan/Desktop/aiops-codex/web/src/lib/chatTurnFormatter.js)
  - [workspaceViewModel.js](/Users/lizhongxuan/Desktop/aiops-codex/web/src/lib/workspaceViewModel.js)
- 前置依赖：TASK-CHAT-FIX-004
- 实施动作：
  1. 基于已有 `isUserMessageCard`、`isAssistantMessageCard`、`isApprovalCard`、`isProcessCard` 建立共享分类入口。
  2. 给 process 再细分：`read/search/list/command/thinking/agent_status/notice`。
  3. 把 `internal_routing` 单独分类，避免混入 assistant 正文。
- 完成标准：后续折叠规则不再直接写在页面模板里。
- 验证方式：给典型 card 输入能产出稳定分类结果。

### [ ] TASK-CHAT-FIX-006 实现 turn 切分规则

- 目标：统一按用户轮次切 turn，而不是由页面自己拼。
- 涉及文件：
  - [web/src/lib/chatTurnFormatter.js](/Users/lizhongxuan/Desktop/aiops-codex/web/src/lib/chatTurnFormatter.js)
- 前置依赖：TASK-CHAT-FIX-005
- 实施动作：
  1. 以最近用户消息作为新 turn 的起点。
  2. 把后续 assistant/progress/process/approval 归并到当前 turn。
  3. 允许“没有最终回答但仍在运行”的 turn 合法存在。
  4. 允许“失败 / 中止 / 等待审批 / 等待输入”作为 turn 状态输出。
- 完成标准：长线程能被稳定分割为多轮 turn。
- 验证方式：多轮样本中 turn 数量与用户消息数量关系正确。

### [ ] TASK-CHAT-FIX-007 实现 process summary 与 live hint 生成

- 目标：让折叠头部在不展开时也足够有信息量。
- 涉及文件：
  - [web/src/lib/chatTurnFormatter.js](/Users/lizhongxuan/Desktop/aiops-codex/web/src/lib/chatTurnFormatter.js)
  - [ChatPage.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/pages/ChatPage.vue)
- 前置依赖：TASK-CHAT-FIX-006
- 实施动作：
  1. 汇总文件浏览数、搜索数、列表数、命令数、主机数、审批数。
  2. 从当前 activity 或最新 process item 里提取 `liveHint`。
  3. 输出统一短语，例如“已浏览 2 个文件，运行 1 次搜索”。
  4. 避免 summary 和 live hint 同时重复表达同一件事。
- 完成标准：每个运行中或已完成 turn 都有可读摘要。
- 验证方式：过程折叠头部不再只剩“处理中”。

### [ ] TASK-CHAT-FIX-008 实现默认展开 / 折叠规则

- 目标：统一 process group 在不同生命周期下的默认状态。
- 涉及文件：
  - [web/src/lib/chatTurnFormatter.js](/Users/lizhongxuan/Desktop/aiops-codex/web/src/lib/chatTurnFormatter.js)
- 前置依赖：TASK-CHAT-FIX-006
- 实施动作：
  1. `running`、`thinking`、`planning` 默认展开轻量过程层。
  2. `completed` 默认折叠。
  3. `waiting_approval` 正文保留轻状态，审批主体走独立区。
  4. `failed` 错误保持展开，过程层可折叠。
  5. `aborted` 保留终止结果，过程层折叠。
- 完成标准：页面层只读取 `collapsedByDefault`，不再自己判断。
- 验证方式：不同状态样本都符合预期。

### [ ] TASK-CHAT-FIX-009 补文本清洗与内部文案过滤

- 目标：把 route JSON、纯内部 dispatch 文案从两个 chat 主线程移除。
- 涉及文件：
  - [web/src/lib/chatTurnFormatter.js](/Users/lizhongxuan/Desktop/aiops-codex/web/src/lib/chatTurnFormatter.js)
  - [protocolWorkspaceVm.js](/Users/lizhongxuan/Desktop/aiops-codex/web/src/lib/protocolWorkspaceVm.js)
  - [ChatPage.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/pages/ChatPage.vue)
- 前置依赖：TASK-CHAT-FIX-005
- 实施动作：
  1. 复用并扩展 `cleanAssistantMessageText` 的思路。
  2. 把主聊天中不该暴露的内部提示也统一走清洗。
  3. 明确哪些 notice 应过滤，哪些转进 process group。
- 完成标准：主线程中不再出现 route JSON 和纯内部路由说明。
- 验证方式：相关视觉测试和文本断言通过。

### [ ] TASK-CHAT-FIX-010 建立 ordered rows 与场景排序规则

- 目标：让对象数据和列表数据的展示顺序统一。
- 涉及文件：
  - [workspaceViewModel.js](/Users/lizhongxuan/Desktop/aiops-codex/web/src/lib/workspaceViewModel.js)
  - [protocolWorkspaceVm.js](/Users/lizhongxuan/Desktop/aiops-codex/web/src/lib/protocolWorkspaceVm.js)
  - [web/src/lib/chatTurnFormatter.js](/Users/lizhongxuan/Desktop/aiops-codex/web/src/lib/chatTurnFormatter.js)
- 前置依赖：TASK-CHAT-FIX-004
- 实施动作：
  1. 固化关键 ordered keys：`Task / Host / Status / Approval / Thread / Session`。
  2. 统一 `process items` 排序规则：未完成优先，阻塞优先，最近事件优先。
  3. 统一 background agents 和 approvals 的排序规则。
- 完成标准：同类数据在两个 chat 中顺序一致。
- 验证方式：fixture 截图和对象 rows 断言稳定。

### [ ] TASK-CHAT-FIX-011 新增共享 unread / scroll 状态 composable

- 目标：把自动吸底、未读分隔线、回到底部 pill 的逻辑从页面里抽出来。
- 涉及文件：
  - [web/src/composables/useChatScrollState.js](/Users/lizhongxuan/Desktop/aiops-codex/web/src/composables/useChatScrollState.js)
  - [ChatPage.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/pages/ChatPage.vue)
  - [ProtocolConversationPane.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/components/protocol-workspace/ProtocolConversationPane.vue)
- 前置依赖：TASK-CHAT-FIX-004
- 实施动作：
  1. 定义 `isPinnedToBottom`、`unreadCount`、`showUnreadPill`、`dividerTurnId`。
  2. 定义“用户主动滚离底部”的判定。
  3. 定义“哪些新内容会增加 unreadCount”，优先按新 assistant turn 计数。
  4. 定义点击 pill 回到底部的统一行为。
- 完成标准：两个页面都能复用同一套滚动状态管理。
- 验证方式：滚动相关行为测试可独立编写。

### [ ] TASK-CHAT-FIX-012 为共享基础层补单测

- 目标：先把 formatter 和规则层测稳，再动页面。
- 涉及文件：
  - [web/tests/chat-turn-formatter.spec.js](/Users/lizhongxuan/Desktop/aiops-codex/web/tests/chat-turn-formatter.spec.js)
  - [web/tests/protocol-chat-ui.spec.js](/Users/lizhongxuan/Desktop/aiops-codex/web/tests/protocol-chat-ui.spec.js)
- 前置依赖：TASK-CHAT-FIX-004 至 TASK-CHAT-FIX-011
- 实施动作：
  1. 为 turn 切分写样本测试。
  2. 为 collapse 默认规则写测试。
  3. 为 summary、live hint、文本清洗写测试。
  4. 为 ordered rows 和 process 排序写测试。
- 完成标准：共享基础逻辑具备稳定回归保护。
- 验证方式：本地跑相关测试通过。

---

## 5. P2 协议工作台先落地

### [ ] TASK-CHAT-FIX-013 新增 `ProtocolTurnGroup.vue`

- 目标：让协议工作台以 turn 为最小渲染单元。
- 涉及文件：
  - [web/src/components/protocol-workspace/ProtocolTurnGroup.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/components/protocol-workspace/ProtocolTurnGroup.vue)
- 前置依赖：TASK-CHAT-FIX-004 至 TASK-CHAT-FIX-008
- 实施动作：
  1. 定义 `turn` props。
  2. 渲染 `userMessage`。
  3. 渲染 `processGroup`。
  4. 渲染 `finalMessage` 或 `blockingState`。
- 完成标准：页面模板不再直接循环 `normalizedMessages`。
- 验证方式：单个 turn 在视觉上能看出层次。

### [ ] TASK-CHAT-FIX-014 新增 `ProtocolProcessFold.vue`

- 目标：把现有 `statusCard` 升级为真正的过程折叠块。
- 涉及文件：
  - [web/src/components/protocol-workspace/ProtocolProcessFold.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/components/protocol-workspace/ProtocolProcessFold.vue)
- 前置依赖：TASK-CHAT-FIX-007、TASK-CHAT-FIX-008
- 实施动作：
  1. 头部显示 `elapsedLabel`。
  2. 次级信息显示 `summary`。
  3. 运行中显示 `liveHint`。
  4. 展开区渲染 process items。
  5. 支持 turn 粒度的展开状态记忆。
- 完成标准：协议工作台不再只显示一张孤立状态卡。
- 验证方式：运行中和已完成状态下都能正确表现。

### [ ] TASK-CHAT-FIX-015 在协议 VM 中输出 `formattedTurns`

- 目标：把协议工作台现有 conversation/process/result 数据接入共享 formatter。
- 涉及文件：
  - [protocolWorkspaceVm.js](/Users/lizhongxuan/Desktop/aiops-codex/web/src/lib/protocolWorkspaceVm.js)
  - [web/src/lib/chatTurnFormatter.js](/Users/lizhongxuan/Desktop/aiops-codex/web/src/lib/chatTurnFormatter.js)
- 前置依赖：TASK-CHAT-FIX-004 至 TASK-CHAT-FIX-010
- 实施动作：
  1. 保留现有 `conversationItems`、`approvalItems`、`backgroundAgents`、`planCardModel`、`eventItems` 输出。
  2. 新增 `formattedTurns` 输出。
  3. 新增 `activeProcessTurnId` 或同类字段，便于 UI 默认展开当前轮。
- 完成标准：协议工作台页面只消费结构化 turns，不再自己拼消息。
- 验证方式：`buildProtocolWorkspaceModel` 输出更完整且可直接渲染。

### [ ] TASK-CHAT-FIX-016 重构 `ProtocolConversationPane.vue` 渲染逻辑

- 目标：把协议工作台的主线程从“消息 + statusCard”切到 turn 线程。
- 涉及文件：
  - [ProtocolConversationPane.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/components/protocol-workspace/ProtocolConversationPane.vue)
  - [ProtocolTurnGroup.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/components/protocol-workspace/ProtocolTurnGroup.vue)
  - [ProtocolProcessFold.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/components/protocol-workspace/ProtocolProcessFold.vue)
- 前置依赖：TASK-CHAT-FIX-013、TASK-CHAT-FIX-014、TASK-CHAT-FIX-015
- 实施动作：
  1. 用 `formattedTurns` 替换 `normalizedMessages` 渲染。
  2. 保留空态、composer、agent-select 等已有能力。
  3. 清理 `statusCard` 相关分支。
- 完成标准：协议工作台主线程由 turn 驱动。
- 验证方式：页面上能清楚看到每轮的 user/process/final 结构。

### [ ] TASK-CHAT-FIX-017 收紧协议工作台正文与 widget / 右栏边界

- 目标：确保 plan、agents、approval、timeline 不再回流正文。
- 涉及文件：
  - [ProtocolConversationPane.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/components/protocol-workspace/ProtocolConversationPane.vue)
  - [ProtocolWorkspacePage.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/pages/ProtocolWorkspacePage.vue)
  - [ProtocolInlinePlanWidget.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/components/protocol-workspace/ProtocolInlinePlanWidget.vue)
  - [ProtocolBackgroundAgentsCard.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/components/protocol-workspace/ProtocolBackgroundAgentsCard.vue)
  - [ProtocolApprovalRail.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/components/protocol-workspace/ProtocolApprovalRail.vue)
- 前置依赖：TASK-CHAT-FIX-016
- 实施动作：
  1. 正文里不再重复展示 plan 卡片本体。
  2. 正文里不再重复展示 background agent 过程流。
  3. 审批正文只留“等待审批”的轻提示，不再重复审批卡主体。
  4. timeline 只保留关键状态变化，不再复制过程日志。
- 完成标准：协议工作台各区域职责清晰。
- 验证方式：视觉上不再像“卡片堆栈”。

### [ ] TASK-CHAT-FIX-018 协议工作台接入 unread divider 与新结果 pill

- 目标：解决用户上滑看历史时被强制吸底的问题。
- 涉及文件：
  - [ProtocolConversationPane.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/components/protocol-workspace/ProtocolConversationPane.vue)
  - [web/src/composables/useChatScrollState.js](/Users/lizhongxuan/Desktop/aiops-codex/web/src/composables/useChatScrollState.js)
- 前置依赖：TASK-CHAT-FIX-011、TASK-CHAT-FIX-016
- 实施动作：
  1. 接入 `isPinnedToBottom` 和 `showUnreadPill`。
  2. 在 turn 列表中插入 unread divider。
  3. 底部显示“`N 条新结果`” pill。
  4. 用户点击 pill 时平滑回到底部。
- 完成标准：协议工作台不再在用户阅读过程中抢滚动位置。
- 验证方式：交互测试覆盖“上滑后有新内容”的场景。

### [ ] TASK-CHAT-FIX-019 协议工作台过程项接入 evidence modal

- 目标：让过程层保持简洁，同时保留深层钻取能力。
- 涉及文件：
  - [ProtocolProcessFold.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/components/protocol-workspace/ProtocolProcessFold.vue)
  - [ProtocolEvidenceModal.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/components/protocol-workspace/ProtocolEvidenceModal.vue)
  - [ProtocolWorkspacePage.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/pages/ProtocolWorkspacePage.vue)
- 前置依赖：TASK-CHAT-FIX-014、TASK-CHAT-FIX-017
- 实施动作：
  1. 过程列表只展示摘要级标题。
  2. 需要深入时，通过点击事件打开 evidence modal。
  3. 把 transcript、terminal、审批上下文继续留在 modal，不回流正文。
- 完成标准：过程折叠层不会因为细节过多再次膨胀。
- 验证方式：展开和钻取路径都能正常工作。

### [ ] TASK-CHAT-FIX-020 为协议工作台补页面测试与截图

- 目标：先把协议工作台的结构重构测稳。
- 涉及文件：
  - [web/tests/ProtocolWorkspacePage.spec.js](/Users/lizhongxuan/Desktop/aiops-codex/web/tests/ProtocolWorkspacePage.spec.js)
  - [web/tests/protocol-chat-ui.spec.js](/Users/lizhongxuan/Desktop/aiops-codex/web/tests/protocol-chat-ui.spec.js)
  - [web/tests/protocol-ux-fixes.spec.js](/Users/lizhongxuan/Desktop/aiops-codex/web/tests/protocol-ux-fixes.spec.js)
  - [web/tests/screenshots](/Users/lizhongxuan/Desktop/aiops-codex/web/tests/screenshots)
- 前置依赖：TASK-CHAT-FIX-016 至 TASK-CHAT-FIX-019
- 实施动作：
  1. 增加“completed turn 默认折叠”的断言。
  2. 增加“waiting approval 只在正文显示轻提示”的断言。
  3. 增加“background agents 不回流正文”的断言。
  4. 更新协议工作台截图基线。
- 完成标准：协议工作台结构变化有稳定测试保护。
- 验证方式：相关测试和截图对比通过。

---

## 6. P3 主聊天同步升级

### [ ] TASK-CHAT-FIX-021 新增 `ChatTurnGroup.vue`

- 目标：让主聊天也按 turn 渲染，而不是继续平铺 `visibleCards`。
- 涉及文件：
  - [web/src/components/chat/ChatTurnGroup.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/components/chat/ChatTurnGroup.vue)
- 前置依赖：TASK-CHAT-FIX-004 至 TASK-CHAT-FIX-008
- 实施动作：
  1. 渲染用户消息。
  2. 渲染 process fold。
  3. 渲染 final message。
  4. 渲染等待审批、等待输入、失败等阻塞态的轻提示。
- 完成标准：主聊天每一轮都能读成一段完整线程。
- 验证方式：长线程不会再像无序卡片流。

### [ ] TASK-CHAT-FIX-022 新增 `ChatProcessFold.vue`

- 目标：把主聊天顶部独立的 `activity-summary` 和 thinking 状态收进 turn。
- 涉及文件：
  - [web/src/components/chat/ChatProcessFold.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/components/chat/ChatProcessFold.vue)
  - [ThinkingCard.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/components/ThinkingCard.vue)
- 前置依赖：TASK-CHAT-FIX-007、TASK-CHAT-FIX-008
- 实施动作：
  1. 头部显示 `elapsedLabel` 和 `summary`。
  2. 运行中显示 `liveHint`。
  3. 若 turn 处于 thinking/planning/executing，用轻状态代替独立大块 ThinkingCard。
  4. 保留展开查看过程细项的能力。
- 完成标准：主聊天顶部不再漂浮一块独立 activity summary。
- 验证方式：运行中 turn 的状态收束在本轮内。

### [ ] TASK-CHAT-FIX-023 新增 `ChatComposerDock.vue`

- 目标：把主聊天的 plan、composer、必要状态收成统一底部主控区。
- 涉及文件：
  - [web/src/components/chat/ChatComposerDock.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/components/chat/ChatComposerDock.vue)
  - [Omnibar.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/components/Omnibar.vue)
  - [PlanCard.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/components/PlanCard.vue)
- 前置依赖：TASK-CHAT-FIX-021
- 实施动作：
  1. 把 `activePlanCard` 收到 composer 上方。
  2. 定义 dock 的层级：plan widget -> follow-up hint -> Omnibar。
  3. 保留 stop / send / follow-up 模式。
- 完成标准：主聊天底部形成统一输入区，而不是多个散落区块。
- 验证方式：视觉上 composer 成为主控件。

### [ ] TASK-CHAT-FIX-024 把主聊天改为消费 `formattedTurns`

- 目标：用 turn model 接管当前 `visibleCards` 逻辑。
- 涉及文件：
  - [ChatPage.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/pages/ChatPage.vue)
  - [web/src/lib/chatTurnFormatter.js](/Users/lizhongxuan/Desktop/aiops-codex/web/src/lib/chatTurnFormatter.js)
  - [ChatTurnGroup.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/components/chat/ChatTurnGroup.vue)
  - [ChatProcessFold.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/components/chat/ChatProcessFold.vue)
- 前置依赖：TASK-CHAT-FIX-021、TASK-CHAT-FIX-022
- 实施动作：
  1. 保留原始 cards 数据源，但把页面渲染切到 `formattedTurns`。
  2. 把 `activeActivityLine`、`summaryLine`、`showThinking` 接入 formatter 输入。
  3. 逐步清理旧的消息流判断分支。
- 完成标准：主聊天主线程不再直接遍历 `visibleCards`。
- 验证方式：主线程层次明确，且旧功能不明显回退。

### [ ] TASK-CHAT-FIX-025 收紧主聊天审批与正文边界

- 目标：继续保留 approval overlay，但让正文只展示必要阻塞提示。
- 涉及文件：
  - [ChatPage.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/pages/ChatPage.vue)
  - [AuthCard.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/components/AuthCard.vue)
- 前置依赖：TASK-CHAT-FIX-024
- 实施动作：
  1. overlay 继续保留审批主入口。
  2. 当前 turn 只显示“等待审批”的轻状态与排队数量。
  3. 清理正文中可能残留的审批卡影子。
- 完成标准：审批不会打散主线程节奏。
- 验证方式：审批样本中正文与 overlay 分工清晰。

### [ ] TASK-CHAT-FIX-026 收紧主聊天终端输出边界

- 目标：终端输出继续留在 terminal dock，不要直接污染聊天线程。
- 涉及文件：
  - [ChatPage.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/pages/ChatPage.vue)
  - [web/src/components/workspace/WorkspaceHostTerminal.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/components/workspace/WorkspaceHostTerminal.vue)
- 前置依赖：TASK-CHAT-FIX-024
- 实施动作：
  1. 聊天线程只保留命令摘要，不再回显大段 output。
  2. terminal dock 保持接管和重连逻辑。
  3. 若 turn 需要引用终端结果，只显示短 label 和“查看终端”入口。
- 完成标准：聊天线程更像对话，不像终端镜像。
- 验证方式：命令型任务场景中正文明显更干净。

### [ ] TASK-CHAT-FIX-027 主聊天接入 unread divider 与回到底部 pill

- 目标：让主聊天和协议工作台拥有一致的滚动体验。
- 涉及文件：
  - [ChatPage.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/pages/ChatPage.vue)
  - [web/src/composables/useChatScrollState.js](/Users/lizhongxuan/Desktop/aiops-codex/web/src/composables/useChatScrollState.js)
- 前置依赖：TASK-CHAT-FIX-011、TASK-CHAT-FIX-024
- 实施动作：
  1. 取代当前单一 `autoFollowTail` 逻辑。
  2. 上滑后冻结自动吸底。
  3. 到来新 turn 时插入未读分隔线和未读 pill。
  4. 输入开始后也不要立刻强制 repin。
- 完成标准：主聊天滚动行为更接近成熟 chat 产品。
- 验证方式：相关交互测试通过。

### [ ] TASK-CHAT-FIX-028 主聊天补 away summary 与 history sentinel

- 目标：提升长线程回访体验。
- 涉及文件：
  - [ChatPage.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/pages/ChatPage.vue)
  - [web/src/composables/useAwaySummary.js](/Users/lizhongxuan/Desktop/aiops-codex/web/src/composables/useAwaySummary.js)
  - [SessionHistoryDrawer.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/components/SessionHistoryDrawer.vue)
- 前置依赖：TASK-CHAT-FIX-024
- 实施动作：
  1. 记录用户失焦和回焦时间。
  2. 在满足条件时生成 away summary turn 或 boundary notice。
  3. 在顶部加入 history sentinel 或 compact boundary 提示。
- 完成标准：用户切走一段时间再回来时能快速找回上下文。
- 验证方式：长线程样本中有可读的“离开期间摘要”。

### [ ] TASK-CHAT-FIX-029 为主聊天补页面测试与截图

- 目标：把主聊天的结构变化也稳定下来。
- 涉及文件：
  - [web/tests/ChatPage.spec.js](/Users/lizhongxuan/Desktop/aiops-codex/web/tests/ChatPage.spec.js)
  - [web/tests/chat-ui-visual.spec.js](/Users/lizhongxuan/Desktop/aiops-codex/web/tests/chat-ui-visual.spec.js)
  - [web/tests/screenshots](/Users/lizhongxuan/Desktop/aiops-codex/web/tests/screenshots)
- 前置依赖：TASK-CHAT-FIX-024 至 TASK-CHAT-FIX-028
- 实施动作：
  1. 增加“最终回答优先可见”的断言。
  2. 增加“过程层默认折叠”的断言。
  3. 增加“审批只显示轻提示 + overlay 仍可用”的断言。
  4. 更新主聊天视觉截图基线。
- 完成标准：主聊天重构不依赖人工肉眼长期守回归。
- 验证方式：测试和视觉截图对比通过。

---

## 7. P4 共性交互增强

### [ ] TASK-CHAT-FIX-030 新增 history / pagination composable

- 目标：给两个 chat 的长会话加载历史提供统一能力。
- 涉及文件：
  - [web/src/composables/useChatHistoryPager.js](/Users/lizhongxuan/Desktop/aiops-codex/web/src/composables/useChatHistoryPager.js)
  - [ChatPage.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/pages/ChatPage.vue)
  - [ProtocolConversationPane.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/components/protocol-workspace/ProtocolConversationPane.vue)
- 前置依赖：TASK-CHAT-FIX-011
- 实施动作：
  1. 定义 `loadingOlder`、`hasOlder`、`loadOlder`。
  2. 在顶部接近阈值时触发加载。
  3. 提供 `loading older messages`、`failed to load older messages`、`start of session` 三种 sentinel。
- 完成标准：长会话不再全量渲染并且能回看更早内容。
- 验证方式：手动和自动化都能触发顶部加载。

### [ ] TASK-CHAT-FIX-031 新增 turn 级虚拟滚动能力

- 目标：解决长线程渲染成本过高的问题。
- 涉及文件：
  - [web/src/composables/useVirtualTurnList.js](/Users/lizhongxuan/Desktop/aiops-codex/web/src/composables/useVirtualTurnList.js)
  - [ChatPage.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/pages/ChatPage.vue)
  - [ProtocolConversationPane.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/components/protocol-workspace/ProtocolConversationPane.vue)
- 前置依赖：TASK-CHAT-FIX-024、TASK-CHAT-FIX-016
- 实施动作：
  1. 以 turn 为单位建立虚拟窗口。
  2. 只挂载视口附近的 turn。
  3. 为高度波动较大的 process fold 保留缓冲区。
  4. 在 prepend 历史后进行锚点补偿。
- 完成标准：长会话滚动流畅度明显提升。
- 验证方式：手动滚动不卡顿，自动化中 DOM 数量受控。

### [ ] TASK-CHAT-FIX-032 在两个 chat 中插入 compact boundary

- 目标：未来做历史压缩时，不让用户莫名其妙丢上下文。
- 涉及文件：
  - [ChatPage.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/pages/ChatPage.vue)
  - [ProtocolConversationPane.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/components/protocol-workspace/ProtocolConversationPane.vue)
  - [SessionHistoryDrawer.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/components/SessionHistoryDrawer.vue)
- 前置依赖：TASK-CHAT-FIX-030
- 实施动作：
  1. 设计 compact boundary 的最小文案。
  2. 在历史被总结或分页截断时插入边界。
  3. 提供查看完整历史的入口。
- 完成标准：历史压缩行为可感知、可追溯。
- 验证方式：视觉和交互都可测试。

### [ ] TASK-CHAT-FIX-033 为 Omnibar 增加大块粘贴缓冲

- 目标：避免大段日志、代码、命令粘贴时误拆分或误提交。
- 涉及文件：
  - [Omnibar.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/components/Omnibar.vue)
  - [web/src/composables/usePasteAssist.js](/Users/lizhongxuan/Desktop/aiops-codex/web/src/composables/usePasteAssist.js)
- 前置依赖：无
- 实施动作：
  1. 识别大块文本粘贴。
  2. 粘贴期间短暂缓冲，避免和回车事件串扰。
  3. 保持 follow-up 模式和 stop/send 状态不回退。
- 完成标准：长日志和大块代码粘贴稳定。
- 验证方式：输入器测试覆盖 paste 行为。

### [ ] TASK-CHAT-FIX-034 为 Omnibar 增加图片 / 路径识别

- 目标：让真实使用中的截图和文件路径更自然地进入输入器流程。
- 涉及文件：
  - [Omnibar.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/components/Omnibar.vue)
  - [web/src/composables/usePasteAssist.js](/Users/lizhongxuan/Desktop/aiops-codex/web/src/composables/usePasteAssist.js)
- 前置依赖：TASK-CHAT-FIX-033
- 实施动作：
  1. 识别拖入或粘贴的本地图片。
  2. 识别拖入路径与多文件路径列表。
  3. 给出轻量提示，而不是直接把内容糊成一大串文本。
- 完成标准：图片和路径输入体验明显更稳。
- 验证方式：手动拖拽和粘贴场景验证通过。

### [ ] TASK-CHAT-FIX-035 为 Omnibar 增加焦点恢复提示

- 目标：降低用户切回窗口后的输入迷失感。
- 涉及文件：
  - [Omnibar.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/components/Omnibar.vue)
  - [web/src/composables/usePasteAssist.js](/Users/lizhongxuan/Desktop/aiops-codex/web/src/composables/usePasteAssist.js)
- 前置依赖：TASK-CHAT-FIX-033
- 实施动作：
  1. 在重新聚焦时检测是否存在待处理图片或粘贴提示。
  2. 用轻量 hint 告诉用户当前可以继续输入或上传。
  3. 保证 hint 不抢主线程视觉重心。
- 完成标准：焦点恢复后的体验更有引导性。
- 验证方式：聚焦恢复场景可重复验证。

### [ ] TASK-CHAT-FIX-036 优化 `ChoiceCard` 与结构化追问

- 目标：减少自由文本追问带来的使用成本。
- 涉及文件：
  - [ChoiceCard.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/components/ChoiceCard.vue)
  - [ChatPage.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/pages/ChatPage.vue)
  - [ProtocolConversationPane.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/components/protocol-workspace/ProtocolConversationPane.vue)
- 前置依赖：TASK-CHAT-FIX-024、TASK-CHAT-FIX-016
- 实施动作：
  1. 推荐选项永远放前面。
  2. 每个选项补一句结果说明。
  3. 保留一个补充输入入口，但不要求用户从零组织答案。
- 完成标准：结构化提问比现在更省心。
- 验证方式：选择题型交互更明确且不影响原协议。

### [ ] TASK-CHAT-FIX-037 统一消息与 composer 的视觉节奏

- 目标：在结构完成后，再做最后一轮视觉收口。
- 涉及文件：
  - [MessageCard.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/components/MessageCard.vue)
  - [Omnibar.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/components/Omnibar.vue)
  - [ChatTurnGroup.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/components/chat/ChatTurnGroup.vue)
  - [ChatProcessFold.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/components/chat/ChatProcessFold.vue)
  - [ProtocolTurnGroup.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/components/protocol-workspace/ProtocolTurnGroup.vue)
  - [ProtocolProcessFold.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/components/protocol-workspace/ProtocolProcessFold.vue)
- 前置依赖：P2、P3、P4 结构任务基本完成
- 实施动作：
  1. 统一 assistant 文本线程样式，让正文更像文本而不是卡片。
  2. 统一 user bubble 的宽度、圆角、留白。
  3. 统一 process fold 的轻量层级。
  4. 统一 composer 高度、圆角、工具条密度。
- 完成标准：两个 chat 视觉语言一致，但不会被改回“卡片堆栈”。
- 验证方式：视觉截图对比达到目标。

---

## 8. P4A MCP UI 卡片与控制面板

### [ ] TASK-CHAT-FIX-043 定义 `McpUiCard` 数据契约

- 目标：让后续所有 MCP 图表卡和控制面板卡都有统一的输入结构与落位规则。
- 涉及文件：
  - [web/src/lib/mcpUiCardModel.js](/Users/lizhongxuan/Desktop/aiops-codex/web/src/lib/mcpUiCardModel.js)
  - [web/src/lib/chatTurnFormatter.js](/Users/lizhongxuan/Desktop/aiops-codex/web/src/lib/chatTurnFormatter.js)
- 前置依赖：TASK-CHAT-FIX-004、TASK-CHAT-FIX-005
- 实施动作：
  1. 定义最小字段：`id`、`source`、`mcpServer`、`uiKind`、`placement`、`title`、`summary`、`freshness`、`scope`、`visual`、`actions`。
  2. 定义 `uiKind` 范围：`readonly_summary`、`readonly_chart`、`action_panel`、`form_panel`。
  3. 定义 `placement` 范围：`inline_final`、`inline_action`、`side_panel`、`drawer`、`modal`。
  4. 定义 action 元数据：`intent`、`mutation`、`approvalMode`、`confirmText`、`payloadSchema`。
- 完成标准：前端对 MCP UI 的承接不再依赖后续逐卡硬编码。
- 验证方式：给一份 mock payload 可以稳定产出统一 view model。

### [ ] TASK-CHAT-FIX-043A 定义 `McpBundle` 数据契约

- 目标：让“聚合监控面板”和“聚合控制面板”成为一等概念，而不是几张松散卡片的偶然组合。
- 涉及文件：
  - [web/src/lib/mcpUiCardModel.js](/Users/lizhongxuan/Desktop/aiops-codex/web/src/lib/mcpUiCardModel.js)
  - [web/src/lib/chatTurnFormatter.js](/Users/lizhongxuan/Desktop/aiops-codex/web/src/lib/chatTurnFormatter.js)
- 前置依赖：TASK-CHAT-FIX-043
- 实施动作：
  1. 定义 `bundleKind`：`monitor_bundle`、`remediation_bundle`。
  2. 定义 bundle 基础字段：`bundleId`、`subject`、`summary`、`sections`、`freshness`。
  3. 为 `monitor_bundle` 定义 section 标准：`overview`、`trends`、`alerts`、`changes`、`topology`。
  4. 为 `remediation_bundle` 定义 section 标准：`root_cause`、`impact`、`recommended_actions`、`control_panels`、`validation_panels`。
- 完成标准：后续“redis 面板”“nginx 故障修复面板”都能走统一 bundle 协议。
- 验证方式：一份 middleware 监控 mock 和一份 RCA mock 都能稳定产出 bundle model。

### [ ] TASK-CHAT-FIX-043B 定义 MCP payload 归一化适配层

- 目标：保证本地 MCP、远端 MCP、host MCP、workspace MCP 的结果最终都进入同一套 bundle / card model。
- 涉及文件：
  - [web/src/lib/mcpUiCardModel.js](/Users/lizhongxuan/Desktop/aiops-codex/web/src/lib/mcpUiCardModel.js)
  - [web/src/lib/mcpUiPayloadAdapter.js](/Users/lizhongxuan/Desktop/aiops-codex/web/src/lib/mcpUiPayloadAdapter.js)
- 前置依赖：TASK-CHAT-FIX-043
- 实施动作：
  1. 定义统一 adapter，把不同来源的 MCP payload 归一化。
  2. 统一处理 `source`、`scope`、`freshness`、`actions`、`errors`。
  3. 页面层只接收 normalized 结果，不直接面对原始 payload。
- 完成标准：chat 层不需要知道“这是哪一路 MCP 回来的”。
- 验证方式：多种来源的 mock payload 能归到同一 model。

### [ ] TASK-CHAT-FIX-044 新增 `McpUiCardHost.vue`

- 目标：统一把结构化 MCP payload 渲染成具体卡片组件。
- 涉及文件：
  - [web/src/components/mcp/McpUiCardHost.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/components/mcp/McpUiCardHost.vue)
- 前置依赖：TASK-CHAT-FIX-043
- 实施动作：
  1. 按 `uiKind` 分发到具体子卡片。
  2. 按 `placement` 控制默认布局形态。
  3. 统一暴露 `action`、`detail`、`refresh` 等事件。
  4. 统一错误态、空态、过期态的基础框架。
- 完成标准：页面层不需要逐处 `if/else` 渲染 MCP 卡片。
- 验证方式：同一 host 能承接不同类型的 MCP UI payload。

### [ ] TASK-CHAT-FIX-044A 新增 `McpBundleHost.vue`

- 目标：把聚合 bundle 作为主展示单元渲染，而不是把多张卡片平铺在聊天正文里。
- 涉及文件：
  - [web/src/components/mcp/McpBundleHost.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/components/mcp/McpBundleHost.vue)
- 前置依赖：TASK-CHAT-FIX-043A、TASK-CHAT-FIX-044
- 实施动作：
  1. 按 `bundleKind` 分发到 `monitor bundle` 或 `remediation bundle` 视图。
  2. 支持 section 化渲染，每个 section 内再挂 `McpUiCardHost`。
  3. 控制默认展开级别，避免 bundle 一上来过长。
  4. 暴露 `action`、`open-detail`、`pin` 等统一事件。
- 完成标准：用户在 chat 内看到的是“聚合面板”，不是几张散卡。
- 验证方式：bundle host 可以稳定承接 4-8 张卡的组合展示。

### [ ] TASK-CHAT-FIX-044B 新增 `GenericMcpActionCard.vue`

- 目标：即使前端还没有专门适配某个 MCP action，也能安全展示、审批和执行。
- 涉及文件：
  - [web/src/components/mcp/GenericMcpActionCard.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/components/mcp/GenericMcpActionCard.vue)
  - [web/src/components/mcp/McpUiCardHost.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/components/mcp/McpUiCardHost.vue)
- 前置依赖：TASK-CHAT-FIX-044
- 实施动作：
  1. 显示 action 名、目标对象、关键参数、权限路径。
  2. mutation 类 action 仍然提供审批入口。
  3. 只读类 action 仍然提供执行 / 刷新入口。
  4. 缺少专用 renderer 时自动退回 fallback 卡。
- 完成标准：新 MCP action 不会因为前端未适配而直接失效或绕过权限。
- 验证方式：未知 action mock 仍能形成可操作且可审批的卡片。

### [ ] TASK-CHAT-FIX-045 实现只读监控卡组件

- 目标：先完成最常用的监控查看型 UI。
- 涉及文件：
  - [web/src/components/mcp/McpKpiStripCard.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/components/mcp/McpKpiStripCard.vue)
  - [web/src/components/mcp/McpTimeseriesChartCard.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/components/mcp/McpTimeseriesChartCard.vue)
  - [web/src/components/mcp/McpStatusTableCard.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/components/mcp/McpStatusTableCard.vue)
- 前置依赖：TASK-CHAT-FIX-044
- 实施动作：
  1. v1 采用轻量 SVG 图表，不引入大图表库。
  2. 每张卡都必须显示标题、summary、时间范围、freshness、scope。
  3. 统一空态、无数据、过期、加载失败的表现。
  4. 控制默认高度和信息密度，避免把聊天线程挤爆。
- 完成标准：主聊天和协议工作台都能安全展示只读监控结果。
- 验证方式：典型 metrics MCP mock 数据可以落成图表卡。

### [ ] TASK-CHAT-FIX-045A 实现 `McpMonitorBundleCard.vue`

- 目标：让用户问“我想知道 xxx 中间件情况”时，直接看到一个聚合监控工作台。
- 涉及文件：
  - [web/src/components/mcp/McpMonitorBundleCard.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/components/mcp/McpMonitorBundleCard.vue)
- 前置依赖：TASK-CHAT-FIX-044A、TASK-CHAT-FIX-045
- 实施动作：
  1. 定义固定 section：概览、趋势、异常、变更、依赖。
  2. 将 KPI strip、时序图、短表格组合到统一 bundle 布局。
  3. 在 bundle 头部显示中间件名、环境、freshness、summary。
  4. 给“查看完整面板”提供 drawer / modal 入口。
- 完成标准：用户看到的是一个“redis 当前情况面板”而不是零散监控片段。
- 验证方式：redis / nginx / kafka 这类 mock 都能落成聚合面板。

### [ ] TASK-CHAT-FIX-046 实现可变更控制面板卡组件

- 目标：让用户可以在 chat 内直接完成常见修复操作，而不是跳去独立页面。
- 涉及文件：
  - [web/src/components/mcp/McpControlPanelCard.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/components/mcp/McpControlPanelCard.vue)
  - [web/src/components/mcp/McpActionFormCard.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/components/mcp/McpActionFormCard.vue)
- 前置依赖：TASK-CHAT-FIX-044
- 实施动作：
  1. 把控制面板和表单面板分开，避免一个组件同时承担展示和复杂表单。
  2. 卡片内明确显示目标对象、当前状态、操作后果、权限路径。
  3. 支持最小交互形态：按钮、选择项、输入项、确认说明。
  4. 为 destructive / mutation 操作预留二次确认区。
- 完成标准：控制卡既能操作，又不会看起来像危险按钮墙。
- 验证方式：典型 “重启服务 / 修改阈值 / 静音告警” mock 能正确渲染。

### [ ] TASK-CHAT-FIX-046A 实现 `McpRemediationBundleCard.vue`

- 目标：在完成根因定位后，自动聚合出本次故障最相关的控制面板和验证面板。
- 涉及文件：
  - [web/src/components/mcp/McpRemediationBundleCard.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/components/mcp/McpRemediationBundleCard.vue)
- 前置依赖：TASK-CHAT-FIX-044A、TASK-CHAT-FIX-046
- 实施动作：
  1. 头部显示根因摘要、影响范围、置信度。
  2. 展示 `recommended actions`，按优先顺序排列。
  3. 展示聚合后的控制面板区，而不是单个按钮散落。
  4. 展示 `validation panels`，供操作后立即复看指标。
- 完成标准：用户定位完问题后，不需要再切去多个页面找修复入口。
- 验证方式：典型“发布后错误率抬升”“连接池耗尽”“实例异常”都能产出对应 remediation bundle。

### [ ] TASK-CHAT-FIX-046B 为 remediation bundle 增加 recent-activity strip

- 目标：修复执行过程中只显示最近几条关键活动，避免再次刷屏。
- 涉及文件：
  - [web/src/components/mcp/McpRemediationBundleCard.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/components/mcp/McpRemediationBundleCard.vue)
  - [web/src/lib/mcpUiCardModel.js](/Users/lizhongxuan/Desktop/aiops-codex/web/src/lib/mcpUiCardModel.js)
- 前置依赖：TASK-CHAT-FIX-046A
- 实施动作：
  1. 定义 `lastActivity` 和 `recentActivities` 字段。
  2. 默认只展示最后一步和最近 3 到 5 条 activity。
  3. 详细日志继续走 process fold 或 evidence modal。
  4. action 完成后 recent-activity strip 自动收成最终摘要。
- 完成标准：后台修复过程不再把 chat 正文刷成执行日志面板。
- 验证方式：长修复链路里正文仍然保持简洁。

### [ ] TASK-CHAT-FIX-047 把 MCP action 接进现有审批链路

- 目标：保证控制面板内的变更动作不会绕开你现有的 approval 体系。
- 涉及文件：
  - [ChatPage.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/pages/ChatPage.vue)
  - [ProtocolWorkspacePage.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/pages/ProtocolWorkspacePage.vue)
  - [ProtocolApprovalRail.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/components/protocol-workspace/ProtocolApprovalRail.vue)
  - [ProtocolEventTimeline.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/components/protocol-workspace/ProtocolEventTimeline.vue)
- 前置依赖：TASK-CHAT-FIX-046
- 实施动作：
  1. 只读 action 直接走卡片内执行。
  2. mutation action 统一发起审批请求。
  3. 审批状态进入现有 approval rail / overlay。
  4. action 提交、审批、执行、成功/失败都记录进 timeline。
- 完成标准：MCP 控制面板不会形成第二套平行审批系统。
- 验证方式：mutation action 从卡片点击到审批完成路径连通。

### [ ] TASK-CHAT-FIX-047A 新增 `McpBundleResolver` 与 preset registry

- 目标：把“根据用户想看的中间件自动聚合监控面板”和“根因后自动聚合控制面板”做成规则层，而不是页面层硬编码。
- 涉及文件：
  - [web/src/lib/mcpBundleResolver.js](/Users/lizhongxuan/Desktop/aiops-codex/web/src/lib/mcpBundleResolver.js)
  - [web/src/lib/mcpBundlePresetRegistry.js](/Users/lizhongxuan/Desktop/aiops-codex/web/src/lib/mcpBundlePresetRegistry.js)
- 前置依赖：TASK-CHAT-FIX-043A
- 实施动作：
  1. 定义 `middleware/service -> monitor bundle preset` 的映射。
  2. 定义 `root cause type -> remediation bundle preset` 的映射。
  3. 支持 scope 解析：服务、环境、集群、主机。
  4. 输出 bundle 所需的 section 配置和卡片组合。
- 完成标准：问 redis、nginx、kafka 时能稳定命中对应聚合面板，而不是零散卡片。
- 验证方式：preset registry 能对典型中间件和根因类型给出稳定 bundle。

### [ ] TASK-CHAT-FIX-048 把 MCP UI card 接进 turn formatter

- 目标：让 MCP UI card 成为 turn 的一等公民，而不是聊天正文里的特殊 case。
- 涉及文件：
  - [web/src/lib/chatTurnFormatter.js](/Users/lizhongxuan/Desktop/aiops-codex/web/src/lib/chatTurnFormatter.js)
  - [web/src/lib/mcpUiCardModel.js](/Users/lizhongxuan/Desktop/aiops-codex/web/src/lib/mcpUiCardModel.js)
- 前置依赖：TASK-CHAT-FIX-043、TASK-CHAT-FIX-044
- 实施动作：
  1. 在 turn model 中增加 `resultAttachments`、`actionSurfaces`、`workspaceSurfaces`。
  2. 只读图表卡优先进入 `resultAttachments`。
  3. 当前可操作控制卡优先进入 `actionSurfaces`。
  4. 长驻监控面板优先进入 `workspaceSurfaces`。
- 完成标准：MCP UI 卡片进入统一的 turn 信息架构。
- 验证方式：formatter 输出里可以区分普通消息和 MCP surface。

### [ ] TASK-CHAT-FIX-048A 把 `McpBundle` 接进 turn formatter

- 目标：让 monitor bundle 和 remediation bundle 成为 turn 的核心结果区域，而不是附件中的附件。
- 涉及文件：
  - [web/src/lib/chatTurnFormatter.js](/Users/lizhongxuan/Desktop/aiops-codex/web/src/lib/chatTurnFormatter.js)
  - [web/src/lib/mcpBundleResolver.js](/Users/lizhongxuan/Desktop/aiops-codex/web/src/lib/mcpBundleResolver.js)
- 前置依赖：TASK-CHAT-FIX-047A、TASK-CHAT-FIX-048
- 实施动作：
  1. 在 turn model 中增加 `resultBundles`、`actionBundles`。
  2. 用户监控查询优先产出 `monitor_bundle`。
  3. 根因定位结果优先产出 `remediation_bundle`。
  4. bundle 与普通 `resultAttachments` 同时存在时，bundle 优先级更高。
- 完成标准：turn 层面明确支持“聚合面板优先于零散卡片”。
- 验证方式：formatter 输出能稳定将问监控和 RCA 场景提升为 bundle。

### [ ] TASK-CHAT-FIX-049 协议工作台接入 MCP surfaces

- 目标：让协议工作台成为 MCP 图表和控制面板的主承载场景。
- 涉及文件：
  - [ProtocolConversationPane.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/components/protocol-workspace/ProtocolConversationPane.vue)
  - [ProtocolWorkspacePage.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/pages/ProtocolWorkspacePage.vue)
  - [ProtocolEvidenceModal.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/components/protocol-workspace/ProtocolEvidenceModal.vue)
- 前置依赖：TASK-CHAT-FIX-047、TASK-CHAT-FIX-048
- 实施动作：
  1. 小型结果图表卡进入当前 turn 的最终结果区。
  2. 长驻监控与控制面板进入右栏或 drawer，不回流正文。
  3. 明细大图、长表格、多步表单进入 evidence modal。
  4. action 完成后把结果回写到当前 turn、timeline、evidence。
- 完成标准：协议工作台能自然承载“查指标 -> 看图 -> 点操作 -> 审批 -> 回写结果”的闭环。
- 验证方式：监控和修复场景能在一个 workspace 中完成。

### [ ] TASK-CHAT-FIX-049A 协议工作台优先接入 `monitor_bundle` 与 `remediation_bundle`

- 目标：让协议工作台成为“中间件工作台”的首个落地场景。
- 涉及文件：
  - [ProtocolConversationPane.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/components/protocol-workspace/ProtocolConversationPane.vue)
  - [ProtocolWorkspacePage.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/pages/ProtocolWorkspacePage.vue)
- 前置依赖：TASK-CHAT-FIX-045A、TASK-CHAT-FIX-046A、TASK-CHAT-FIX-048A
- 实施动作：
  1. 监控查询 turn 中优先展示 `monitor_bundle`。
  2. RCA 完成后优先展示 `remediation_bundle`。
  3. bundle 内的控制动作继续串到 approval rail 和 timeline。
  4. validation panels 操作后自动刷新。
- 完成标准：协议工作台能直接承接“问某个中间件情况”和“查明故障后直接修”的完整闭环。
- 验证方式：用户不必跳去多个监控页和控制页完成同一件事。

### [ ] TASK-CHAT-FIX-050 主聊天接入 MCP surfaces

- 目标：让主聊天也能承接结果图表和轻控制卡，但不变成 dashboard 页面。
- 涉及文件：
  - [ChatPage.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/pages/ChatPage.vue)
  - [web/src/components/chat/ChatTurnGroup.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/components/chat/ChatTurnGroup.vue)
- 前置依赖：TASK-CHAT-FIX-047、TASK-CHAT-FIX-048
- 实施动作：
  1. 把结果图表卡挂在当前 turn 的 final 区下方。
  2. 把当前可操作控制卡挂在 turn 的 action surface 区。
  3. 保持主聊天轻量，不在正文里长期堆叠大图和长表。
  4. 给“查看完整监控面板”提供 drawer 或 modal 入口。
- 完成标准：主聊天可以处理轻量监控和操作，但仍然像 chat，不像控制台首页。
- 验证方式：用户在单线程对话中也能完成一次“看图 + 执行修复”。

### [ ] TASK-CHAT-FIX-050A 主聊天接入 bundle 简化版

- 目标：让主聊天也能承接聚合面板，但保持更轻量的 chat 气质。
- 涉及文件：
  - [ChatPage.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/pages/ChatPage.vue)
  - [web/src/components/chat/ChatTurnGroup.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/components/chat/ChatTurnGroup.vue)
- 前置依赖：TASK-CHAT-FIX-045A、TASK-CHAT-FIX-046A、TASK-CHAT-FIX-048A
- 实施动作：
  1. 主聊天只默认展示 monitor bundle 的摘要 section 和 1-2 个关键 section。
  2. remediation bundle 默认展示根因 + 推荐操作 + 1 组验证 panel。
  3. 其他 section 收到 drawer / modal。
  4. 保持 bundle 可展开，但不让单线程聊天变成满屏 dashboard。
- 完成标准：主聊天符合“边回答边给聚合面板”的需求，但仍然像对话。
- 验证方式：单线程场景中既能看聚合面板，又不会压垮消息阅读。

### [ ] TASK-CHAT-FIX-051 升级全局 `app-mcp-drawer`

- 目标：把当前空壳的 `Skills & MCP` 抽屉升级成真正的 MCP surface 容器。
- 涉及文件：
  - [App.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/App.vue)
  - [store.js](/Users/lizhongxuan/Desktop/aiops-codex/web/src/store.js)
- 前置依赖：TASK-CHAT-FIX-043、TASK-CHAT-FIX-044
- 实施动作：
  1. 把抽屉定位从静态列表升级成 `MCP surfaces / pinned dashboards`。
  2. 展示启用中的 MCP、常驻监控面板、最近操作面板。
  3. 提供“固定到 drawer / 从 drawer 移除”的最小能力。
  4. 让 `metrics`、`host-logs` 这类 MCP 有统一入口，而不是分散在各页面各自实现。
- 完成标准：全局 MCP 抽屉成为统一监控与工具面板容器。
- 验证方式：用户可以在任一 chat 中打开并复用常驻 MCP 面板。

### [ ] TASK-CHAT-FIX-051A 支持 bundle 固定与复用

- 目标：让用户已经打开过的中间件 bundle 可以固定到全局 drawer，避免重复问同一句。
- 涉及文件：
  - [App.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/App.vue)
  - [store.js](/Users/lizhongxuan/Desktop/aiops-codex/web/src/store.js)
- 前置依赖：TASK-CHAT-FIX-051
- 实施动作：
  1. 支持把 `monitor_bundle` 固定到 drawer。
  2. 支持记录最近使用的 remediation bundle。
  3. 支持从聊天中的 bundle 一键跳到全局常驻面板。
- 完成标准：用户不用重复在多个对话里反复索取同一个中间件面板。
- 验证方式：固定后的 bundle 可跨 chat 复用。

### [ ] TASK-CHAT-FIX-052 为 MCP UI 卡片补专项测试与截图

- 目标：给图表卡和控制面板卡建立专项回归保护。
- 涉及文件：
  - [web/tests/chat-ui-visual.spec.js](/Users/lizhongxuan/Desktop/aiops-codex/web/tests/chat-ui-visual.spec.js)
  - [web/tests/protocol-chat-ui.spec.js](/Users/lizhongxuan/Desktop/aiops-codex/web/tests/protocol-chat-ui.spec.js)
  - [web/tests/screenshots](/Users/lizhongxuan/Desktop/aiops-codex/web/tests/screenshots)
- 前置依赖：TASK-CHAT-FIX-045 至 TASK-CHAT-FIX-051
- 实施动作：
  1. 测只读图表卡的标题、summary、freshness、scope 是否完整。
  2. 测控制面板卡的 mutation action 是否正确触发审批。
  3. 测大图 / 长表 / 表单是否走 modal 或 drawer，而不是撑爆正文。
  4. 更新 MCP 相关视觉截图基线。
- 完成标准：MCP UI 卡片上线后不容易因为样式或权限改动而回退。
- 验证方式：专项自动化测试和截图对比通过。

### [ ] TASK-CHAT-FIX-052A 为 bundle 场景补专项测试

- 目标：确保真正的“聚合监控面板”和“聚合修复面板”行为不会回退成单卡拼接。
- 涉及文件：
  - [web/tests/chat-ui-visual.spec.js](/Users/lizhongxuan/Desktop/aiops-codex/web/tests/chat-ui-visual.spec.js)
  - [web/tests/protocol-chat-ui.spec.js](/Users/lizhongxuan/Desktop/aiops-codex/web/tests/protocol-chat-ui.spec.js)
- 前置依赖：TASK-CHAT-FIX-045A 至 TASK-CHAT-FIX-051A
- 实施动作：
  1. 测“我想知道 redis 情况”是否产出 `monitor_bundle`，而不是离散监控卡。
  2. 测根因定位完成后是否自动产出 `remediation_bundle`。
  3. 测 remediation bundle 的 mutation action 是否正确触发审批与 validation panel 刷新。
  4. 更新 bundle 场景截图基线。
- 完成标准：bundle 是产品主形态，而不是文档概念。
- 验证方式：专项测试和截图都能证明“聚合”确实发生了。

### [ ] TASK-CHAT-FIX-052B 为 payload normalization / fallback / recent-activity 补专项测试

- 目标：把 Claude Code 那三条高价值工程约束真正测住。
- 涉及文件：
  - [web/tests/protocol-chat-ui.spec.js](/Users/lizhongxuan/Desktop/aiops-codex/web/tests/protocol-chat-ui.spec.js)
  - [web/tests/chat-ui-visual.spec.js](/Users/lizhongxuan/Desktop/aiops-codex/web/tests/chat-ui-visual.spec.js)
- 前置依赖：TASK-CHAT-FIX-043B、TASK-CHAT-FIX-044B、TASK-CHAT-FIX-046B
- 实施动作：
  1. 测不同来源的 MCP payload 是否归一化成同一种 bundle / card model。
  2. 测未知 MCP action 是否落到 `GenericMcpActionCard`，且审批路径仍可用。
  3. 测 remediation bundle 是否默认只显示 recent activities，而不是全量过程。
- 完成标准：MCP 体系的可扩展性和克制感有自动化回归保护。
- 验证方式：专项测试覆盖归一化、fallback、recent-activity 三条约束。

---

## 9. P5 测试与发布收口

### [ ] TASK-CHAT-FIX-038 为滚动与 unread 行为补专项测试

- 目标：防止滚动体验在后续迭代中退化。
- 涉及文件：
  - [web/tests/ChatPage.spec.js](/Users/lizhongxuan/Desktop/aiops-codex/web/tests/ChatPage.spec.js)
  - [web/tests/protocol-chat-ui.spec.js](/Users/lizhongxuan/Desktop/aiops-codex/web/tests/protocol-chat-ui.spec.js)
- 前置依赖：TASK-CHAT-FIX-018、TASK-CHAT-FIX-027、TASK-CHAT-FIX-030、TASK-CHAT-FIX-031
- 实施动作：
  1. 测“滚离底部后不自动吸底”。
  2. 测“有新 turn 时出现 unread pill”。
  3. 测“点击 pill 回到底部”。
  4. 测“prepend 历史后视口不跳”。
- 完成标准：滚动行为有专项回归保护。
- 验证方式：自动化测试稳定通过。

### [ ] TASK-CHAT-FIX-039 为输入器增强补专项测试

- 目标：把 paste、图片、路径识别这些容易回退的能力测住。
- 涉及文件：
  - [web/tests/ChatPage.spec.js](/Users/lizhongxuan/Desktop/aiops-codex/web/tests/ChatPage.spec.js)
  - [web/tests/chat-ui-visual.spec.js](/Users/lizhongxuan/Desktop/aiops-codex/web/tests/chat-ui-visual.spec.js)
- 前置依赖：TASK-CHAT-FIX-033、TASK-CHAT-FIX-034、TASK-CHAT-FIX-035
- 实施动作：
  1. 测大块粘贴不误提交。
  2. 测粘贴图片时有正确提示或处理路径。
  3. 测拖入路径时不变成脏文本。
- 完成标准：输入器改造具备专项保护。
- 验证方式：自动化与手动验证结果一致。

### [ ] TASK-CHAT-FIX-040 更新视觉截图基线

- 目标：为两个 chat 的最终视觉状态保留明确基线。
- 涉及文件：
  - [web/tests/screenshots](/Users/lizhongxuan/Desktop/aiops-codex/web/tests/screenshots)
  - [web/tests/chat-ui-visual.spec.js](/Users/lizhongxuan/Desktop/aiops-codex/web/tests/chat-ui-visual.spec.js)
  - [web/tests/protocol-chat-ui.spec.js](/Users/lizhongxuan/Desktop/aiops-codex/web/tests/protocol-chat-ui.spec.js)
- 前置依赖：TASK-CHAT-FIX-037
- 实施动作：
  1. 更新主聊天截图。
  2. 更新协议工作台截图。
  3. 补一张“运行中 process fold”截图。
  4. 补一张“未读 pill + divider”截图。
- 完成标准：视觉变化可以被稳定追踪。
- 验证方式：截图测试通过且人工审阅无明显退化。

### [ ] TASK-CHAT-FIX-041 跑一轮完整 smoke

- 目标：在交付前验证结构、交互、性能没有明显断裂。
- 涉及文件：
  - [web/tests/ChatPage.spec.js](/Users/lizhongxuan/Desktop/aiops-codex/web/tests/ChatPage.spec.js)
  - [web/tests/ProtocolWorkspacePage.spec.js](/Users/lizhongxuan/Desktop/aiops-codex/web/tests/ProtocolWorkspacePage.spec.js)
  - [web/tests/protocol-ux-fixes.spec.js](/Users/lizhongxuan/Desktop/aiops-codex/web/tests/protocol-ux-fixes.spec.js)
- 前置依赖：TASK-CHAT-FIX-038 至 TASK-CHAT-FIX-040
- 实施动作：
  1. 跑关键单测。
  2. 跑两个页面的核心 UI 测试。
  3. 过一轮手工路径：简单问答、读文件、等待审批、失败、长线程回看。
- 完成标准：没有明显阻塞性回归。
- 验证方式：测试结果和人工检查清单都通过。

### [ ] TASK-CHAT-FIX-042 发布前清理旧分支逻辑

- 目标：防止新结构上线后，旧分支和死代码继续干扰维护。
- 涉及文件：
  - [ChatPage.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/pages/ChatPage.vue)
  - [ProtocolConversationPane.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/components/protocol-workspace/ProtocolConversationPane.vue)
  - [protocolWorkspaceVm.js](/Users/lizhongxuan/Desktop/aiops-codex/web/src/lib/protocolWorkspaceVm.js)
  - [web/src/lib/chatTurnFormatter.js](/Users/lizhongxuan/Desktop/aiops-codex/web/src/lib/chatTurnFormatter.js)
- 前置依赖：TASK-CHAT-FIX-041
- 实施动作：
  1. 删除不再使用的旧 computed、旧 props、旧模板分支。
  2. 删除只服务旧卡片流的样式。
  3. 确认新旧逻辑没有并存造成双渲染。
- 完成标准：代码树回到可维护状态。
- 验证方式：全量搜索确认没有遗留死分支和废弃 props。

---

## 10. 推荐实施顺序

不要并行乱做，推荐严格按下面顺序推进：

1. 先做 P0，冻结样本和验收矩阵。
2. 再做 P1，先把共享 formatter 和规则层做出来。
3. 再做 P4A 的基础部分，先把 `McpUiCard` 协议和 `McpUiCardHost` 定下来。
4. 先落协议工作台 P2，因为它最适合先承接 MCP 图表和控制面板。
5. 再做主聊天 P3，把 `visibleCards` 切到 `formattedTurns`，同时接入轻量 MCP surface。
6. 再做 P4，把滚动、历史、输入器增强补齐。
7. 最后做 P5，跑测试、更新截图、清理旧逻辑。

---

## 11. 风险清单

### [!] RISK-CHAT-FIX-001 先改视觉不改结构

- 风险：页面可能更好看，但交互和层级依然不专业。
- 规避：严格按 “formatter -> turn -> scroll -> visual” 顺序推进。

### [!] RISK-CHAT-FIX-002 主聊天和协议工作台强行共用完全相同的页面输出

- 风险：主聊天的 terminal/approval/history 需求和协议工作台的右栏需求不同，强行统一会让代码变僵。
- 规避：只共用基础原语，不强推完全一致的页面结构。

### [!] RISK-CHAT-FIX-003 一次性引入虚拟滚动过早

- 风险：在 turn 结构没稳定前就上 virtualization，排查成本会很高。
- 规避：等 P2、P3 稳定后再做 P4 的虚拟滚动。

### [!] RISK-CHAT-FIX-004 输入器增强与现有快捷键冲突

- 风险：paste 缓冲和图片识别可能影响回车发送、Cmd+Enter、follow-up 模式。
- 规避：输入器增强要配专项测试，不和结构重构混在同一个大 patch。

### [!] RISK-CHAT-FIX-005 MCP 控制面板绕开现有审批链路

- 风险：用户可能直接在卡片里执行高风险操作，导致权限和审计断裂。
- 规避：所有 mutation action 统一复用现有 approval 流程，不允许新开平行执行通道。

### [!] RISK-CHAT-FIX-006 监控图表没有 freshness / scope，导致用户误判

- 风险：用户可能看不出图是旧数据、哪台主机、哪个服务、哪个时间范围。
- 规避：每张图表卡强制展示 freshness、scope、time range 和一句 summary。

### [!] RISK-CHAT-FIX-007 过早引入大图表库

- 风险：在 UI surface 协议没稳定前先引入图表库，会把后续交互和布局锁死。
- 规避：v1 先用轻量 SVG，等卡片协议和落位稳定后再评估图表库。

---

## 12. 最后的交付判断

只有下面这些都满足，才算这轮真正完成：

- [ ] 两个 chat 都改成了 turn 级主线程
- [ ] 过程层可以折叠，并且折叠头部有 summary / live hint / elapsed
- [ ] 审批、plan、background agents、timeline 都回到了正确区域
- [ ] 用户上滑后不会被强制吸底，且看得到未读提示
- [ ] 长线程支持历史加载或至少具备可落地的分页入口
- [ ] 输入器对真实使用场景更稳
- [ ] MCP 图表卡和控制面板卡能在 chat 内自然出现、操作、审批、回写结果
- [ ] 自动化测试和视觉截图都已更新
