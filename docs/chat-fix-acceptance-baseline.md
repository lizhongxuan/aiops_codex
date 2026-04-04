# 两个 Chat 基线与统一验收矩阵

更新日期：2026-04-03

这份文档用于冻结当前两个 chat 的真实输入面、回归样本和统一验收矩阵，避免后续继续靠“看起来像 Codex/Claude Code”做主观判断。

## 1. 当前状态来源盘点

### 1.1 主聊天

页面主入口：[ChatPage.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/pages/ChatPage.vue)

直接依赖的原始状态：

- `store.snapshot.cards`
  - 原始聊天卡片流
  - `PlanCard` 来源
  - `TerminalCard` 来源
  - MCP `actionSurfaces / resultBundles` 来源
- `store.snapshot.approvals`
  - 原生审批队列
- `store.snapshot.kind`
  - 会话类型判断
- `store.snapshot.selectedHostId`
  - 终端面板与审批目标主机上下文
- `store.snapshot.auth.connected`
  - 输入器可用态
- `store.snapshot.config.codexAlive`
  - 输入器与 banner 状态
- `store.sessionList`
  - history sentinel / session history 入口
- `store.selectedHost`
  - 终端能力、执行能力、离线态、显示文案
- `store.runtime.turn`
  - `active / phase / hostId`
- `store.runtime.activity`
  - `viewedFiles / searchedWebQueries / searchedContentQueries`
- `store.runtime.codex`
  - reconnect / stopped / retry banner
- `store.loading / store.sending / store.errorMessage / store.noticeMessage / store.canSend / store.canOpenTerminal`

页面本地状态：

- `showThinking / thinkingPhase / thinkingHint / preferredThinkingPhase`
- `approvalFollowupMode / authCardCollapsed`
- `localMcpApprovals`
- `activeMcpSurface / mcpPinnedSurfaces / isMcpDrawerOpen`
- `terminalDockVisible / terminalDockHeight / terminalDockSessionLive`
- `composerMessage`

关键派生链路：

1. `store.snapshot.cards -> visibleCards -> streamCards`
2. `streamCards + store.runtime.activity + store.runtime.turn -> formatMainChatTurns() -> mainChatTurns`
3. `mainChatTurns -> useChatHistoryPager() -> pagedStreamEntries`
4. `pagedStreamEntries -> useChatScrollState() -> unread divider / unread pill`
5. `pagedStreamEntries -> useVirtualTurnList() -> virtualizedStreamEntries`
6. `store.snapshot.cards -> activePlanCard / latestTerminalCard / pendingApprovalCards`

主聊天里当前的语义分层：

- 最终消息：`formatMainChatTurns()` 产出的 `finalMessage`
- 过程痕迹：`mainChatActiveProcess`、共享 formatter 的 `processItems`
- 阻塞态：原生审批 overlay、synthetic MCP approval overlay、reconnect/stopped banner
- 深层面板：terminal dock、session history drawer、MCP surface drawer

### 1.2 协议工作台

页面主入口：[ProtocolWorkspacePage.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/pages/ProtocolWorkspacePage.vue)

VM 入口：[protocolWorkspaceVm.js](/Users/lizhongxuan/Desktop/aiops-codex/web/src/lib/protocolWorkspaceVm.js)

线程组件入口：[ProtocolConversationPane.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/components/protocol-workspace/ProtocolConversationPane.vue)

直接依赖的原始状态：

- `store.snapshot.kind / store.snapshot.sessionId`
  - workspace 会话判定与 history reset key
- `store.snapshot.cards`
  - conversation / process / plan / notice / choice / MCP surface 统一数据源
- `store.snapshot.approvals`
  - 原生审批队列
- `store.snapshot.hosts`
  - host rows、background agents、worker context
- `store.snapshot.selectedHostId`
  - evidence host 上下文
- `store.snapshot.auth.connected`
  - composer 可用态
- `store.snapshot.config.codexAlive`
  - composer 可用态
- `store.sessionList`
  - 自动切换最近 workspace / 查看完整历史
- `store.runtime.turn`
  - `active / phase / hostId`
- `store.runtime.codex`
  - runtime 状态与 toolbar 文案
- `store.loading / store.sending / store.errorMessage / store.noticeMessage`

页面本地状态：

- `composerDraft`
- `actionNotice / actionTone`
- `selectedHostId / selectedStepId / selectedApprovalId / selectedMessageId`
- `selectedMcpSurface`
- `evidenceOpen / evidenceTab / evidenceSource`
- `workspaceBootstrapBusy / workspaceBootstrapAttempted`
- `localMcpApprovals / localMcpEvents`

VM 派生输出：

- `hostRows`
- `planCardModel`
- `choiceCards`
- `approvalItems`
- `backgroundAgents`
- `eventItems`
- `conversationStatusCard`
- `formattedTurns`
- `activeProcessTurnId`
- `statusBanner`
- `canStopCurrentMission`
- `nextSendStartsNewMission`

`ProtocolConversationPane` 当前真实输入面：

- `formattedTurns`
- `backgroundAgents`
- `choiceCards`
- `statusCard`
- `draft / sending / busy / primaryActionOverride`
- `historyResetKey`

组件内派生链路：

1. `formattedTurns -> useChatHistoryPager() -> visibleStreamItems`
2. `visibleStreamItems -> useChatScrollState() -> unread divider / unread pill`
3. `visibleStreamItems -> useVirtualTurnList() -> renderedTurns`
4. `backgroundAgents -> composer widgets`
5. `choiceCards -> protocol choice stack`

协议工作台里当前的语义分层：

- 最终消息：`formattedTurns[].finalMessage`
- 过程痕迹：`formattedTurns[].processItems`
- 阻塞态：approval rail、status banner、conversation status card
- 深层信息：evidence modal、event timeline、global MCP drawer

## 2. 冻结的 6 组基础验收样本

这 6 组是基础回归样本，后续不再改名字，只扩内容。

| 样本 | 目标 | 主聊天证据 | 协议工作台证据 |
| --- | --- | --- | --- |
| S1 简单问答已完成 | turn 正确切分，最终回答直接可读 | [ChatPage.spec.js](/Users/lizhongxuan/Desktop/aiops-codex/web/tests/ChatPage.spec.js) 中 completed turn 默认折叠、最终回答仍可见 | [ProtocolWorkspacePage.spec.js](/Users/lizhongxuan/Desktop/aiops-codex/web/tests/ProtocolWorkspacePage.spec.js) 中 completed turn 折叠、final answer 可见 |
| S2 读取/搜索/浏览后完成回答 | 文件/搜索痕迹进入 process fold，不回灌正文 | [chatTurnFormatter.spec.js](/Users/lizhongxuan/Desktop/aiops-codex/web/tests/chatTurnFormatter.spec.js) 的 active process / route JSON 清洗断言 | [ProtocolWorkspacePage.spec.js](/Users/lizhongxuan/Desktop/aiops-codex/web/tests/ProtocolWorkspacePage.spec.js) 的 process fold 与 evidence modal 断言 |
| S3 等待审批 | 正文只保留轻阻塞提示，审批主体进 overlay/rail | [ChatPage.spec.js](/Users/lizhongxuan/Desktop/aiops-codex/web/tests/ChatPage.spec.js) 的 pending approvals stay out of thread | [protocol-fixture-ui.spec.js](/Users/lizhongxuan/Desktop/aiops-codex/web/tests/protocol-fixture-ui.spec.js) 的 approval boundary smoke |
| S4 失败或停止 | 错误原因、停止提示、重新开始路径清晰 | [ChatPage.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/pages/ChatPage.vue) 的 reconnect/stopped banner 链路与相关页面测试 | [ProtocolWorkspacePage.spec.js](/Users/lizhongxuan/Desktop/aiops-codex/web/tests/ProtocolWorkspacePage.spec.js) 的 stopped mission / restart hint 场景 |
| S5 后台 agent / 计划投影 / 结构化追问 | plan、background agents、choice 不回流正文 | [ChatPage.spec.js](/Users/lizhongxuan/Desktop/aiops-codex/web/tests/ChatPage.spec.js) 的 composer dock plan widget | [ProtocolWorkspacePage.spec.js](/Users/lizhongxuan/Desktop/aiops-codex/web/tests/ProtocolWorkspacePage.spec.js) 的 background agents、plan projection、choice stack 场景 |
| S6 长线程回访 | unread、history、分页、虚拟滚动稳定 | [ChatPage.spec.js](/Users/lizhongxuan/Desktop/aiops-codex/web/tests/ChatPage.spec.js) 的 unread pill、history sentinel、virtualization | [protocol-fixture-ui.spec.js](/Users/lizhongxuan/Desktop/aiops-codex/web/tests/protocol-fixture-ui.spec.js) 的 history boundary、virtualization、focus hint |

## 3. MCP 专项样本

MCP 不是最初 6 组基础样本的一部分，但已经成为当前产品的正式验收项，单独冻结为专项样本。

| 样本 | 目标 | 主聊天证据 | 协议工作台证据 |
| --- | --- | --- | --- |
| M1 监控 bundle 聚合展示 | 问“某中间件情况”时展示聚合 bundle，而不是散卡 | [chat-fixture-ui.spec.js](/Users/lizhongxuan/Desktop/aiops-codex/web/tests/chat-fixture-ui.spec.js) 的 aggregated monitor bundle 场景 | [protocol-fixture-ui.spec.js](/Users/lizhongxuan/Desktop/aiops-codex/web/tests/protocol-fixture-ui.spec.js) 的 bundle host 场景 |
| M2 控制面板触发审批 | mutation action 进入审批链路，不直接执行 | [ChatPage.spec.js](/Users/lizhongxuan/Desktop/aiops-codex/web/tests/ChatPage.spec.js) 的 synthetic MCP approval overlay | [ProtocolWorkspacePage.spec.js](/Users/lizhongxuan/Desktop/aiops-codex/web/tests/ProtocolWorkspacePage.spec.js) 的 synthetic MCP approval rail / timeline |
| M3 remediation bundle recent activity | 修复面板默认露 recent activity，不刷全量日志 | [chat-ui-visual.spec.js](/Users/lizhongxuan/Desktop/aiops-codex/web/tests/chat-ui-visual.spec.js) 的 bundle drawer heavy 基线 | [protocol-fixture-ui.spec.js](/Users/lizhongxuan/Desktop/aiops-codex/web/tests/protocol-fixture-ui.spec.js) 的 remediation bundle 场景 |

## 4. 统一验收矩阵

| 维度 | 完成时应该看到什么 | 主聊天验证入口 | 协议工作台验证入口 |
| --- | --- | --- | --- |
| 结构 | 主线程按 `user / process / final` 渲染，而不是 raw cards 直出 | [ChatPage.spec.js](/Users/lizhongxuan/Desktop/aiops-codex/web/tests/ChatPage.spec.js)、[chat-ui-visual.spec.js](/Users/lizhongxuan/Desktop/aiops-codex/web/tests/chat-ui-visual.spec.js) | [ProtocolWorkspacePage.spec.js](/Users/lizhongxuan/Desktop/aiops-codex/web/tests/ProtocolWorkspacePage.spec.js)、[protocol-chat-ui.spec.js](/Users/lizhongxuan/Desktop/aiops-codex/web/tests/protocol-chat-ui.spec.js) |
| 折叠 | process 默认折叠或轻展开，final 始终可读 | [ChatPage.spec.js](/Users/lizhongxuan/Desktop/aiops-codex/web/tests/ChatPage.spec.js) completed turn 场景 | [ProtocolWorkspacePage.spec.js](/Users/lizhongxuan/Desktop/aiops-codex/web/tests/ProtocolWorkspacePage.spec.js) completed turn 场景 |
| 阻塞边界 | 审批、plan、background agents、timeline 不回灌正文 | [chat-fixture-ui.spec.js](/Users/lizhongxuan/Desktop/aiops-codex/web/tests/chat-fixture-ui.spec.js) dock and thread 场景 | [protocol-fixture-ui.spec.js](/Users/lizhongxuan/Desktop/aiops-codex/web/tests/protocol-fixture-ui.spec.js) approval/background agent boundary 场景 |
| 滚动 | 用户滚离底部后不被强制吸底，并看到 unread hint | [ChatPage.spec.js](/Users/lizhongxuan/Desktop/aiops-codex/web/tests/ChatPage.spec.js) unread pill 场景 | [ProtocolWorkspacePage.spec.js](/Users/lizhongxuan/Desktop/aiops-codex/web/tests/ProtocolWorkspacePage.spec.js) unread pill 场景 |
| 历史 | 有 compact boundary、加载更早消息、查看完整历史入口 | [ChatPage.spec.js](/Users/lizhongxuan/Desktop/aiops-codex/web/tests/ChatPage.spec.js) history sentinel 场景 | [protocol-fixture-ui.spec.js](/Users/lizhongxuan/Desktop/aiops-codex/web/tests/protocol-fixture-ui.spec.js) history boundary 场景 |
| 性能 | 长线程 DOM 数量受控，虚拟窗口不吞关键边界 | [ChatPage.spec.js](/Users/lizhongxuan/Desktop/aiops-codex/web/tests/ChatPage.spec.js) virtualization 场景 | [protocol-fixture-ui.spec.js](/Users/lizhongxuan/Desktop/aiops-codex/web/tests/protocol-fixture-ui.spec.js) virtualization 场景 |
| 输入器 | 大块粘贴、路径、图片、焦点恢复都稳定 | [chat-ui-visual.spec.js](/Users/lizhongxuan/Desktop/aiops-codex/web/tests/chat-ui-visual.spec.js)、[ChatPage.spec.js](/Users/lizhongxuan/Desktop/aiops-codex/web/tests/ChatPage.spec.js) | [protocol-fixture-ui.spec.js](/Users/lizhongxuan/Desktop/aiops-codex/web/tests/protocol-fixture-ui.spec.js) focus hint / artifact hint |
| MCP surface | bundle/card 自然进入 chat，支持 detail / pin / refresh / approval | [chat-fixture-ui.spec.js](/Users/lizhongxuan/Desktop/aiops-codex/web/tests/chat-fixture-ui.spec.js)、[ChatPage.spec.js](/Users/lizhongxuan/Desktop/aiops-codex/web/tests/ChatPage.spec.js) | [protocol-fixture-ui.spec.js](/Users/lizhongxuan/Desktop/aiops-codex/web/tests/protocol-fixture-ui.spec.js)、[ProtocolWorkspacePage.spec.js](/Users/lizhongxuan/Desktop/aiops-codex/web/tests/ProtocolWorkspacePage.spec.js) |
| 视觉 | 关键视觉基线稳定，可追踪回退 | [chat-ui-visual.spec.js](/Users/lizhongxuan/Desktop/aiops-codex/web/tests/chat-ui-visual.spec.js) | [protocol-chat-ui.spec.js](/Users/lizhongxuan/Desktop/aiops-codex/web/tests/protocol-chat-ui.spec.js)、[protocol-ux-fixes.spec.js](/Users/lizhongxuan/Desktop/aiops-codex/web/tests/protocol-ux-fixes.spec.js) |

## 5. 使用规则

- 后续再改 turn 结构、process fold、history、MCP surface 时，优先看这份文档，再看具体测试。
- 如果新增场景，不替换 `S1-S6 / M1-M3`，只允许在现有集合下追加子场景。
- 如果页面行为变化但矩阵条目无法解释，就说明实现已经偏离当前产品约束，应先更新设计文档再改代码。
