# 两个 Chat 对话框对齐 Codex / Claude Code UX 的修复方案

这份文档把之前分散在“协议工作台 Chat 修复”“两个 chat 共用 UX 规则”“Claude Code 可借鉴经验”里的内容重新收敛成一份统一方案。

这次整理后，原先几处容易冲突的点已经统一为下面 3 条：

- 范围不再只写“协议工作台”，而是同时覆盖主聊天 [ChatPage.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/pages/ChatPage.vue) 和协议工作台 [ProtocolConversationPane.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/components/protocol-workspace/ProtocolConversationPane.vue)。
- 架构不要求两边强行共用一套完全相同的 formatter 输出，但要求共用同一套 `turn / process / final` 语义模型与折叠规则。
- 执行优先级不再分成两版，统一为“先结构、再交互、再性能、最后视觉”。

当前没有发现阻塞性的文档冲突，也没有必须先问你的前置问题。下面默认采用一个合理假设：

`共享基础能力可以共用，页面级 view model 允许分别适配。`

---

## 1. 目标

这份方案统一回答 4 个问题：

- Codex 风格里，“已处理”的消息为什么能折叠
- 怎么判断哪些消息该折叠，哪些绝不能折叠
- 为什么它的数据看起来总是特别整齐、特别好扫读
- 你项目里已有的几种 UI 卡片，应该如何重新落位到两个 chat 里
- 后续引入 MCP 监控图表卡、控制面板卡时，应该怎么进入 chat，而不是重新回到“原始卡片堆叠”

核心结论保持不变，但范围扩大成两个 chat：

`不要继续直接渲染 raw cards。先把消息整理成 turn 级 view model，再决定哪些内容进主线程、哪些内容进折叠层、哪些内容进侧栏或 dock。`

---

## 2. 适用范围与收敛原则

### 2.1 适用页面

[ChatPage.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/pages/ChatPage.vue)

- 主聊天页
- 当前特点是卡片流、顶部 activity summary、ThinkingCard、plan dock、approval overlay、terminal dock 并存

[ProtocolConversationPane.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/components/protocol-workspace/ProtocolConversationPane.vue)

- 协议工作台聊天区
- 当前特点是消息已经比主聊天干净，但仍然只是 `normalizedMessages + statusCard`

[ProtocolWorkspacePage.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/pages/ProtocolWorkspacePage.vue)

- 协议工作台整体容器
- 负责把 conversation、approval、plan、background agents、timeline 拼到一个工作台

### 2.2 统一收敛原则

- 两个 chat 都要从“原始卡片流”改成“turn 级线程”
- 两个 chat 都要遵守同一套折叠判定、摘要生成、排序和滚动规则
- 两个 chat 不必输出完全一样的 UI，但必须让用户感知到同一种产品语言
- 主聊天优先强化历史、长线程和输入体验
- 协议工作台优先强化过程折叠、阻塞态和右栏分层
- MCP UI 卡片必须走结构化 UI surface，不允许以 raw JSON 或长 markdown 伪装成“普通消息”

### 2.3 这次整理后已经消掉的冲突

- 原文标题只写协议工作台，但后文已经讨论两个 chat；现在统一为“两个 chat”
- 原文有两版执行优先级；现在只保留一版统一路线
- 原文一部分内容默认 plan/agents 只属于协议工作台；现在改成“共享原则相同，但具体呈现不同”
- 原文结论和优先级重复；现在把“结论”和“实施顺序”拆清楚

---

## 3. 基于当前仓库已经能确认的事实

### 3.1 协议工作台已经有半套正确的数据层

[protocolWorkspaceVm.js](/Users/lizhongxuan/Desktop/aiops-codex/web/src/lib/protocolWorkspaceVm.js)

- `resolveProtocolWorkspaceCards` 已经能围绕最近一条用户消息切 mission 范围
- `buildProtocolConversationItems` 已经在清理 assistant 文案
- `buildProtocolBackgroundAgents` 已经把 host agent 过程抽成独立数据
- `buildProtocolApprovalItems`、`buildProtocolEventItems`、`buildProtocolPlanCardModel` 已经在做右栏和 widget 所需的结构化数据

这说明协议工作台缺的不是“有没有 view model”，而是：

`缺少 turn formatter 这一层，把 conversation / process / final 串成真正的聊天线程。`

### 3.2 主聊天已经有大量可复用状态，但结构仍然偏“卡片拼盘”

[ChatPage.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/pages/ChatPage.vue)

- 已经有 `activitySummary`、`currentReadingLine`、`currentSearchLine` 这类过程摘要原料
- 已经有 `showThinking`、`thinkingPhase`、`thinkingHint` 这类 thinking 状态
- 已经有 `activePlanCard`、`activeApprovalCard`、`latestTerminalCard` 这类运行态控件
- 但当前消息流仍然主要依赖 `visibleCards`

这说明主聊天的问题不是没有状态，而是：

`这些状态还没有被编排成统一的 turn 级体验。`

### 3.3 你仓库里已经有一批“格式化得不错”的基础函数

[workspaceViewModel.js](/Users/lizhongxuan/Desktop/aiops-codex/web/src/lib/workspaceViewModel.js)

- `compactText`
- `formatTime`
- `formatShortTime`
- `objectRows`
- `buildWorkspaceHostRows`
- `buildWorkspaceStepItems`
- `buildWorkspaceLiveTimeline`

这类函数已经说明方向是对的：

`先做 normalize / ordered rows / 场景排序 / 条数截断，再谈视觉。`

### 3.4 仓库里已经有 MCP catalog 和入口，但还没有 MCP UI surface

[store.js](/Users/lizhongxuan/Desktop/aiops-codex/web/src/store.js)

- 已经有 `MCP_CATALOG`
- 已经有 `metrics`、`host-files`、`host-logs` 这类 MCP 能力定义

[App.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/App.vue)

- 顶部已经有 `Skills & MCP` 入口
- 右侧 `app-mcp-drawer` 目前基本还是空壳

[web/package.json](/Users/lizhongxuan/Desktop/aiops-codex/web/package.json)

- 当前没有图表库依赖

这三点组合起来的含义很明确：

`后续完全可以把 MCP UI 卡片接进来，但必须先定义统一的 UI card 协议；图表 v1 应优先走轻量 SVG，而不是急着先引入大图表库。`

---

## 4. 当前为什么还“不像 Codex”

### 4.1 主线程还没有 turn 结构

Codex 风格的核心不是“卡片更轻”，而是每一轮都能被用户自然读成：

- 用户说了什么
- 过程中做了哪些事
- 最终得到了什么结论

而你现在两个 chat 都还没有稳定落实成：

`user -> process -> final`

### 4.2 过程层还没有被定义成低频阅读层

现在的问题不是过程太多，而是过程没有自己的层级。

结果就是：

- 该折叠的 trace 直接进入正文
- 该放 dock 的 plan / agents 仍然容易和正文混在一起
- 该进右栏或 modal 的证据，有时仍然在主线程里抢视觉

### 4.3 滚动与历史体验还不够专业

从现状代码看：

- 两个 chat 都还偏向“有新内容就自动吸底”
- 长会话还没有 turn 级虚拟滚动和 prepend 锚点补偿
- 用户离开后回来，也没有 away summary

这会直接影响真实使用体感，即使 CSS 再漂亮也不像成熟产品。

---

## 5. 统一的信息架构

### 5.1 一个 turn 的标准结构

推荐两个 chat 都统一为同一套 turn 语义：

```json
{
  "id": "turn-001",
  "status": "running | completed | failed | waiting_approval | waiting_input | aborted",
  "elapsedLabel": "已处理 1m 03s",
  "userMessage": {},
  "processGroup": {
    "visible": true,
    "collapsedByDefault": true,
    "summary": "已浏览 2 个文件，执行 1 次搜索，检查 1 台主机",
    "liveHint": "现在搜索 nginx upstream",
    "items": []
  },
  "finalMessage": {},
  "blockingState": {},
  "widgets": {}
}
```

关键点只有 3 个：

- 主线程的最小渲染单位是 `turn`
- 可折叠的是 `processGroup`
- 不是一条条 `ProcessLineCard` 各自决定命运

### 5.2 允许共享原语，但页面适配可以不同

这次文档整理后明确采用这个架构边界：

- 共用的部分：
  - turn 切分
  - 语义分类
  - process summary
  - 折叠策略
  - unread / scroll 规则
  - thinking 简化规则
- 页面适配的部分：
  - 主聊天的 terminal dock、approval overlay、history drawer
  - 协议工作台的 approval rail、event timeline、evidence modal、background agents card

也就是说，不要求两个页面完全共用一个超大 formatter 文件，但必须共用一套统一逻辑。

---

## 6. “已处理消息为什么能折叠”

### 6.1 折叠的本质是“过程是二级信息”

Codex 风格里，过程不会被删掉，但会被降级为：

- 默认低频阅读
- 可快速展开
- 折叠时仍能感知任务在推进

所以“已处理”可折叠，不是为了藏信息，而是为了让第一眼阅读顺序更正确：

1. 先看用户问题
2. 再看当前阻塞或最终结论
3. 需要时再钻取过程

### 6.2 一个好的折叠头部必须同时给 3 类信息

- 时长：`已处理 1m 03s`
- 摘要：`已浏览 2 个文件，执行 1 次搜索`
- live hint：`现在搜索 nginx upstream`

如果只写“处理中”，用户会觉得空。
如果把所有细节平铺，用户会觉得乱。
高级感来自“信息密度刚好够判断，不需要先点开才能知道任务有没有推进”。

### 6.3 已完成与运行中要区别对待

- `running`：过程层默认展开，但只展示轻量摘要和关键最新项
- `planning` / `thinking`：可展开，但默认以轻提示为主
- `waiting_approval`：正文只保留状态提示，审批主体进入固定审批区
- `completed`：过程层默认折叠
- `failed`：错误常驻展开，过程层可折叠
- `aborted`：显示终止结果，过程层折叠

---

## 7. 怎么判断哪些要折叠

### 7.1 判定不是只看 `card.type`

正确做法是两步：

1. 先做语义分类
2. 再做 UI 落位

推荐语义分类至少区分：

- `user`
- `assistant_final`
- `assistant_progress`
- `thinking`
- `process_read`
- `process_search`
- `process_list`
- `process_command`
- `plan`
- `background_agent`
- `approval`
- `choice`
- `error`
- `system_notice`
- `internal_routing`

### 7.2 必须常驻展开的内容

- 用户消息
- assistant 最终回答
- `ErrorCard`
- `CommandApprovalCard`
- `FileChangeApprovalCard`
- `ChoiceCard`
- 当前真正阻塞用户决策的状态

原因很简单：

`这些不是过程痕迹，而是当前结果或当前决策点。`

### 7.3 默认应该折叠进 process group 的内容

- `ProcessLineCard`
- 文件浏览痕迹
- 搜索痕迹
- 命令执行痕迹
- 已完成的 thinking
- 已完成的后台 worker 中间态
- 非关键 `NoticeCard`
- 只读型工具调用摘要
- 内部路由说明

### 7.4 直接过滤不展示的内容

- 纯内部 route / dispatch 判定
- 带 route 元信息的 JSON
- 只说明内部机制、但对用户没有决策价值的系统文案

这里协议工作台已经做了一部分：

[protocolWorkspaceVm.js](/Users/lizhongxuan/Desktop/aiops-codex/web/src/lib/protocolWorkspaceVm.js)

- `isSystemRoutingMessage`
- `cleanAssistantMessageText`

主聊天后续也应该补上同类清洗逻辑。

---

## 8. 为什么数据会显得“格式化得特别好”

真正让数据看起来高级的，不是圆角，而是下面这 5 步。

### 8.1 先做 normalize

- 文本先 `compactText`
- 时间统一 `formatTime` / `formatShortTime`
- host 标签统一一个 resolver
- 同一语义永远用同一种短语

原则：

`同一类信息不要出现三种说法。`

### 8.2 再做 clean

- 删内部路由说明
- 删 route JSON
- 把实现侧术语转成人类能读的产品语言

例如协议工作台已经在把 `Planner` 统一成 `主 Agent`，这就是对的。

### 8.3 对象不要直接 dump，先转 ordered rows

你当前仓库里已经有好例子：

[workspaceViewModel.js](/Users/lizhongxuan/Desktop/aiops-codex/web/src/lib/workspaceViewModel.js)

- `objectRows`

[protocolWorkspaceVm.js](/Users/lizhongxuan/Desktop/aiops-codex/web/src/lib/protocolWorkspaceVm.js)

- `buildTaskBindingRows`
- `buildApprovalAnchorRows`

最值得保留的策略是：

- 先定义 `orderedKeys`
- 关键字段优先显示
- 兜底字段再按补充信息展示

### 8.4 列表排序必须按场景

统一规则建议如下：

- 聊天主线程：按 turn 的自然顺序
- turn 内部：`user -> process -> final`
- approval：激活项优先，再按到期或创建时间
- background agents：`running > waiting_approval > queued > idle`
- plan steps：按 step index，不按时间
- timeline：内部先按最新排序截断，再在展示层反转成从旧到新

### 8.5 长列表必须截断，但必须有 drill-down 入口

- 主线程只负责摘要
- 右栏只负责关键变化
- 深层 transcript、terminal output、审批上下文统一进 modal 或 drawer

协议工作台已有正确方向：

[ProtocolEvidenceModal.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/components/protocol-workspace/ProtocolEvidenceModal.vue)

后续应该继续强化，而不是把深层信息灌回主线程。

---

## 9. 现有卡片应该如何重新落位

### 9.1 主线程正文

- `MessageCard`
  - 只承载用户消息和 assistant 最终回答
- `ThinkingCard`
  - 只承载当前活跃 turn 的轻状态
- `ErrorCard`
  - 必须可见
- `ChoiceCard`
  - 必须可见

### 9.2 过程折叠层

建议新增：

- `ChatProcessFold`
- `ProtocolProcessFold`

或先做一个共享内核，再包两层页面壳。

用途：

- 展示 `已处理 xx`
- 展示 `summary + live hint`
- 展示可展开的 process items

### 9.3 composer 上方 dock / widget

- [PlanCard.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/components/PlanCard.vue)
  - 主聊天里收敛成轻量 plan widget
- [ProtocolInlinePlanWidget.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/components/protocol-workspace/ProtocolInlinePlanWidget.vue)
  - 协议工作台继续放在 composer 上方
- [ProtocolBackgroundAgentsCard.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/components/protocol-workspace/ProtocolBackgroundAgentsCard.vue)
  - 保持在 composer 上方，不回流正文

### 9.4 右栏或固定工作区

- [ProtocolApprovalRail.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/components/protocol-workspace/ProtocolApprovalRail.vue)
- [ProtocolEventTimeline.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/components/protocol-workspace/ProtocolEventTimeline.vue)

规则：

- 审批不要混进正文
- 关键事件不要复制整段过程日志
- 正文只保留“当前等待审批”的轻状态

### 9.5 modal / drawer

- transcript
- terminal output
- 详细证据
- 审批上下文
- 长列表详情

原则：

`深层信息有入口，但不回灌主线程。`

### 9.6 MCP UI card host

建议新增一层统一承接器：

- `McpUiCardHost`

用途：

- 把 MCP 返回的结构化 UI payload 映射成真正的前端卡片
- 控制它到底进入：
  - turn 的最终结果区
  - 当前可操作区
  - 右栏 / drawer
  - modal

原则：

`MCP UI 卡片是结构化 surface，不是普通聊天消息，也不是 process log。`

---

## 10. MCP UI 卡片如何进入两个 chat

### 10.1 先把 MCP UI 卡片定义成“结构化结果面板”

后续如果要把监控图表、控制面板、操作表单放进 chat，最重要的不是先画图，而是先把它和普通消息分开。

推荐统一把它定义成：

- `assistant final` 的结构化附件
- 或 `action surface`
- 或 `workspace surface`

而不是：

- 一大段 markdown
- 一坨 JSON
- 一条伪装成消息的卡片块

### 10.2 监控图表卡和控制面板卡不是一类东西

建议至少拆成下面 4 类：

- `readonly_summary`
  - KPI、状态条、短表格、异常摘要
- `readonly_chart`
  - 时序图、分布图、sparkline、状态 heatmap
- `action_panel`
  - 重启服务、扩容、静音告警、回滚、清理缓存、切换开关
- `form_panel`
  - 修改阈值、修改副本数、调整开关参数、补充操作说明

这 4 类的共同点是都来自 MCP，但它们的落位完全不同。

### 10.3 你这个需求的关键不是“单卡”，而是“聚合 bundle”

按你现在这条需求，目标并不是：

- 用户问一句
- 我回一张 CPU 图
- 再回一张错误率图
- 再回一个重启按钮

这还是“分散卡片思维”。

真正符合你需求的形态应该是两种 bundle：

- `monitor_bundle`
  - 面向“我想知道 xxx 中间件情况”
  - 自动聚合这个中间件相关的监控页面和关键观察面板
- `remediation_bundle`
  - 面向“服务出问题，已经完成根因定位”
  - 自动聚合和这次故障最相关的控制面板、修复动作、变更入口

也就是说，chat 里真正出现的应该优先是：

`一个聚合面板`

而不是：

`几张松散的小卡片`

### 10.4 用户问监控时，要直接出现 monitor bundle

比如用户说：

- `我想知道 xxx 中间件的情况`
- `看看 redis 现在怎么样`
- `nginx 最近有没有异常`

这时系统不应该只返回数据摘要，而应该：

1. 先识别用户想看的对象
   - 中间件类型
   - 服务名
   - 集群 / 环境 / 主机范围
2. 再命中一个 `monitor bundle preset`
3. 自动聚合相关 MCP 数据源
4. 直接生成一个监控 UI 卡片组

这个 `monitor bundle` 里建议至少分成固定几个 section：

- `健康概览`
  - 当前状态、严重告警、核心 KPI
- `关键趋势`
  - QPS / latency / error / saturation / backlog 等时序图
- `依赖与拓扑`
  - 上下游、节点分布、实例状态
- `近期异常`
  - 告警、错误日志、抖动时段、失败操作
- `最近变更`
  - 发布、配置变更、扩缩容、重启记录

这样用户问的是“redis 怎么样”，看到的就不再是离散数据点，而是一个“redis 当前监控工作台”。

### 10.5 根因定位后，要直接出现 remediation bundle

如果系统已经做完根因定位，比如判断出：

- 副本异常
- 连接池耗尽
- 某实例 CPU 打满
- 最近配置变更有风险
- 某次发布后错误率抬升

那下一步就不该只给一段文本建议。

更符合你需求的做法是直接出现一个 `remediation bundle`，自动聚合：

- 当前根因摘要
- 受影响范围
- 建议优先操作
- 对应的控制面板
- 必要时的审批入口
- 操作后的验证面板

这个 `remediation bundle` 里建议至少包含：

- `Root Cause`
  - 一句话根因判断 + 置信度
- `Impact`
  - 影响的主机 / 服务 / 流量 / 错误面
- `Recommended Actions`
  - 建议优先顺序
- `Control Panels`
  - 重启、扩容、摘流、回滚、静音告警、修改阈值、刷新配置
- `Validation Panels`
  - 操作后需要立刻复看的指标和日志

用户体验上，应该是：

- 先看到为什么坏
- 紧接着就看到能做什么
- 做完以后立刻看到验证面板

而不是再跳去几个不同页面找按钮。

### 10.6 放在哪里，不能只看“是不是卡片”

推荐直接按下面的规则落位：

- 直接支撑当前回答结论的 KPI / 小图表
  - 进入当前 turn 的 `finalMessage` 下方，作为 `result attachment`
- 一个中等复杂度的 `monitor_bundle`
  - 进入当前 turn 的 `final attachment` 主区
- 一个当前需要立即操作的 `remediation_bundle`
  - 进入当前 turn 的 `action surface` 主区
- 持续运行中的监控面板
  - 进入右栏或全局 `MCP drawer`
- 当前用户需要立即处理的问题控制面板
  - 进入当前 turn 的 `action surface`
  - 在协议工作台里优先靠近 approval / plan 区
- 明细大图、长表格、多步表单
  - 进入 modal / drawer

一句话：

`图表可以是结果证据，控制面板可以是操作入口，但都不应该退化成正文里的大块杂糅内容。`

### 10.7 只读卡和可变更卡必须分权限路径

监控类卡片里，最容易出问题的是“展示”和“操作”混在一起。

建议强制区分：

- 只读动作
  - 刷新
  - 切换时间范围
  - 查看日志
  - 展开详情
- 变更动作
  - 重启
  - 扩缩容
  - 修改阈值
  - 静音 / 恢复告警
  - 执行修复脚本

规则：

- 只读动作可以直接在卡片内执行
- 变更动作必须经过你现有的 approval 链路
- 执行完成后，要把结果回写成当前 turn 的过程项、最终状态和 bundle 验证区，而不是只在卡片内悄悄变化

### 10.8 建议的数据契约

推荐后续为 MCP UI 卡片单独定义一份结构化协议，例如：

```json
{
  "id": "mcp-ui-001",
  "source": "mcp",
  "mcpServer": "metrics",
  "uiKind": "readonly_chart",
  "placement": "inline_final",
  "title": "CPU 使用率（5m）",
  "summary": "web-01 在 10:24 出现峰值 92%",
  "freshness": {
    "capturedAt": "2026-04-03T10:25:00+08:00",
    "ttlSec": 30
  },
  "scope": {
    "hostId": "web-01",
    "service": "nginx",
    "timeRange": "5m"
  },
  "visual": {},
  "actions": []
}
```

这里最关键的字段不是 `visual`，而是：

- `uiKind`
- `placement`
- `freshness`
- `scope`
- `actions`

因为它们决定这个卡片到底怎么显示、能不能点、会不会过期、是否应该走审批。

如果要支持你说的“聚合监控面板”和“聚合控制面板”，还应该再往上包一层 bundle 契约，例如：

```json
{
  "bundleId": "middleware-redis-prod",
  "bundleKind": "monitor_bundle",
  "subject": {
    "type": "middleware",
    "name": "redis",
    "env": "prod"
  },
  "summary": "redis-prod 当前存在连接数抖动，但错误率仍可控",
  "sections": [
    { "kind": "overview", "cards": [] },
    { "kind": "trends", "cards": [] },
    { "kind": "alerts", "cards": [] },
    { "kind": "changes", "cards": [] }
  ]
}
```

根因定位后的控制 bundle 也类似，只是 `bundleKind` 换成 `remediation_bundle`，并增加：

- `rootCause`
- `confidence`
- `recommendedActions`
- `validationPanels`

### 10.9 监控图表必须有“可解释层”

后续你做监控图表卡时，不要只画图。

每张图至少要同时带：

- 标题
- 一句 summary
- 时间范围
- 数据新鲜度
- 目标范围
  - 主机 / 服务 / 集群 / 环境

不要让用户自己盯着线图猜：

- 这是哪台机器
- 这是多长时间
- 这张图是不是过期了
- 主 Agent 想让我从图里看出什么
- 这张图属于哪个 bundle 的哪个 section

### 10.10 控制面板卡必须有“操作边界层”

控制面板不是按钮拼盘。

每张可变更卡都应该明确展示：

- 影响对象
  - 哪台主机 / 哪个服务 / 哪个环境
- 当前状态
  - 当前副本数、当前开关值、当前告警状态
- 操作后果
  - 是否会重启、是否会短暂中断、是否会扩大影响面
- 权限路径
  - 直接执行 / 二次确认 / 审批

如果这 4 类信息缺失，用户就会把它当成“危险按钮墙”。

### 10.11 chat 内的 action 执行后，应该怎么回写

推荐统一行为：

- 用户点了控制面板卡的 action
- 该 action 创建当前 turn 的一个 `action run`
- action run 的过程进入 `processGroup`
- 审批进入现有 approval 区
- 成功或失败结果回写到：
  - 当前 turn 的最终结果区
  - 事件流
  - 证据面板
  - remediation bundle 的 validation panels

不要让 action 只在卡片内部切状态，否则用户会失去完整对话上下文。

### 10.12 还需要一个 bundle resolver / preset registry

如果没有这层，工程上最后还是会退化成“if redis 就塞这几张图”。

建议新增：

- `McpBundlePresetRegistry`
- `McpBundleResolver`

职责：

- 根据用户问题识别监控意图
- 根据服务 / 中间件类型匹配 preset
- 根据根因结果匹配 remediation preset
- 决定最终生成哪个 `monitor_bundle` / `remediation_bundle`

例如：

- `redis` -> 连接数、命中率、慢查询、内存、复制状态、近期告警
- `nginx` -> QPS、5xx、上游错误、连接数、reload 记录、实例健康
- `kafka` -> lag、ISR、broker 健康、topic 错误、最近 rebalance

### 10.13 跟当前仓库最适合的整合点

[App.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/App.vue)

- 现在空着的 `app-mcp-drawer` 很适合升级成：
  - pinned dashboards
  - 常驻监控面板
  - MCP surface 列表

[ProtocolApprovalRail.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/components/protocol-workspace/ProtocolApprovalRail.vue)

- 很适合继续承接 MCP 控制面板触发的变更审批

[ProtocolEventTimeline.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/components/protocol-workspace/ProtocolEventTimeline.vue)

- 很适合记录：
  - 图表刷新
  - action 提交
  - action 审批
  - action 成功 / 失败

[ProtocolEvidenceModal.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/components/protocol-workspace/ProtocolEvidenceModal.vue)

- 很适合承接：
  - 大图
  - 长表格
  - 图表明细
  - 操作前后 diff

### 10.14 图表实现策略

因为你当前前端没有图表库，所以建议明确分两步：

- v1
  - 自建轻量 SVG 图表组件
  - 只做 sparkline、line、bar、stacked status strip、KPI strip
  - 不做复杂缩放、拖拽、联动
- v2
  - 等 `McpUiCard` 协议、落位和交互稳定后
  - 再评估是否引入图表库

理由很现实：

- 现在最大的风险不是“图不够炫”
- 而是“还没定义好 UI surface，就先被图表库绑定实现方式”

### 10.15 MCP bundle 还必须满足 3 条工程约束

看完 Claude Code 这轮源码后，发现还有 3 条很值得直接吸收，否则后面 bundle 做着做着很容易变形。

第一条：

`远端 MCP 结果和本地 MCP 结果，必须先归一化成同一套前端模型，再渲染。`

第二条：

`就算前端还不认识某个新 MCP action，也必须有通用 fallback 卡片和审批兜底。`

第三条：

`后台修复动作不要刷全量过程，默认只展示最近几条 activity 和当前最后一步。`

这 3 条分别对应 Claude Code 里：

- `sdkMessageAdapter` 的 remote/local tool result 归一化
- `remotePermissionBridge` 的未知工具 stub + fallback permission request
- `LocalAgentTask` 的 `recentActivities / lastActivity`

如果你后面要让 MCP bundle 真正长期可扩展，这 3 条要写进一开始的设计，而不是等问题出现后再补。

---

## 11. 从 Claude Code 源码里最值得吸收的 UX 精华

这次整理后，把能直接借鉴的经验统一归成 8 类。

### 11.1 折叠组必须持续有摘要、hint、进度感

[CollapsedReadSearchContent.tsx](/Users/lizhongxuan/Desktop/aiops-codex/claude%20code/components/messages/CollapsedReadSearchContent.tsx)

- 折叠不是“藏起来”
- 折叠头部始终有可读 summary
- 运行中的组能显示最新 hint
- 计数应尽量稳定，不要流式抖动

### 11.2 折叠判定依赖语义白名单

[classifyForCollapse.ts](/Users/lizhongxuan/Desktop/aiops-codex/claude%20code/tools/MCPTool/classifyForCollapse.ts)

- 只读型 `search / read / list / get` 更适合折叠
- 会改变状态、产生关键结果、触发用户决策的动作不该默认折叠

### 11.3 thinking 只保留轻入口

[AssistantThinkingMessage.tsx](/Users/lizhongxuan/Desktop/aiops-codex/claude%20code/components/messages/AssistantThinkingMessage.tsx)

- thinking 默认不要长段铺开
- 当前轮只给“正在思考 / 查看过程”
- 详细 reasoning 进入 transcript 或 process fold

### 11.4 用户滚离底部后，不要强制吸底

[FullscreenLayout.tsx](/Users/lizhongxuan/Desktop/aiops-codex/claude%20code/components/FullscreenLayout.tsx)
[REPL.tsx](/Users/lizhongxuan/Desktop/aiops-codex/claude%20code/screens/REPL.tsx)

- 记录 unread divider
- 新消息来了显示 `N new messages` pill
- 用户手动点 pill 再回到底部
- 不要在用户正在向上看时强制抢走滚动位置

### 11.5 长会话必须支持分页与虚拟滚动

[useVirtualScroll.ts](/Users/lizhongxuan/Desktop/aiops-codex/claude%20code/hooks/useVirtualScroll.ts)
[useAssistantHistory.ts](/Users/lizhongxuan/Desktop/aiops-codex/claude%20code/hooks/useAssistantHistory.ts)

- 只挂载视口附近内容
- 顶部滚动触发更早历史加载
- prepend 历史后做 scroll anchor 补偿

### 11.6 离开回来要有 away summary

[useAwaySummary.ts](/Users/lizhongxuan/Desktop/aiops-codex/claude%20code/hooks/useAwaySummary.ts)

- 用户离开一段时间后回来
- 如果期间任务已推进完成，应补一条简短总结

### 11.7 输入器要宽容对待大块粘贴、图片、路径

[usePasteHandler.ts](/Users/lizhongxuan/Desktop/aiops-codex/claude%20code/hooks/usePasteHandler.ts)
[useClipboardImageHint.ts](/Users/lizhongxuan/Desktop/aiops-codex/claude%20code/hooks/useClipboardImageHint.ts)

- 大块粘贴要缓冲
- 图片和路径要识别
- 聚焦恢复时给轻提示

### 11.8 会话 compact 后要保留可见边界

[CompactBoundaryMessage.tsx](/Users/lizhongxuan/Desktop/aiops-codex/claude%20code/components/messages/CompactBoundaryMessage.tsx)

- 历史被压缩后不能静默发生
- 要明确告诉用户“之前的历史被总结了”
- 同时给出查看完整历史入口

### 11.9 远端和本地工具结果要先归一化，再谈渲染

[sdkMessageAdapter.ts](/Users/lizhongxuan/Desktop/aiops-codex/claude%20code/remote/sdkMessageAdapter.ts)

Claude Code 很注意一件事：

- 远端来的 `tool_result`
- 本地产生的 `tool_result`

最后都要先变成同一类消息对象，再进入渲染和折叠逻辑。

这对你后面的 MCP bundle 特别重要，因为你未来很可能同时有：

- 本地 MCP
- 远端 host MCP
- workspace 聚合 MCP

设计上要提前规定：

`所有 MCP UI payload 先 normalize 成统一 bundle / card model，再进 ChatTurnFormatter。`

不要让页面层去区分“这是 metrics MCP”“这是 host-logs MCP”“这是远端 server 回来的”。

### 11.10 未知 MCP 工具也必须有 fallback 权限卡

[remotePermissionBridge.ts](/Users/lizhongxuan/Desktop/aiops-codex/claude%20code/remote/remotePermissionBridge.ts)

Claude Code 对未知远端工具的处理很稳：

- 就算本地没加载这个工具
- 也会先构造一个最小 stub
- 至少能显示工具名、关键输入
- 至少能进入 fallback permission request

这对你很有启发：

- 后续就算来了一个前端还没专门适配的 MCP 控制 action
- 也不能直接什么都不显示
- 更不能因为缺少专用 renderer 就绕开审批

正确策略应该是：

- 优先走专用 `McpUiCard`
- 否则退回 `GenericMcpActionCard`
- 仍然展示：
  - action 名
  - 目标对象
  - 关键参数
  - 权限路径
  - 审批入口

### 11.11 后台修复任务应该只显示最近活动摘要

[LocalAgentTask.tsx](/Users/lizhongxuan/Desktop/aiops-codex/claude%20code/tasks/LocalAgentTask/LocalAgentTask.tsx)
[BackgroundTaskStatus.tsx](/Users/lizhongxuan/Desktop/aiops-codex/claude%20code/components/tasks/BackgroundTaskStatus.tsx)

Claude Code 对后台 agent 的处理很克制：

- 保留 `lastActivity`
- 保留一个有限长度的 `recentActivities`
- 页脚只显示 summary pill
- 真要看详细 transcript 再进入任务视图

这特别适合你后面的 remediation bundle。

控制动作执行后，不应该在正文里刷：

- 每一步命令
- 每一次状态刷新
- 每一次 panel 局部变化

更合理的是只展示：

- 当前动作摘要
- 最后一条 activity
- 最近 3 到 5 条 activity
- 最终结果

这样 chat 仍然是 chat，不会再次退化成调试台。

---

## 12. 这些规则怎么分别落到两个 chat 里

### 12.1 两个 chat 都必须补的共性能力

- turn 级 formatter
- process fold
- `summary + live hint + elapsed`
- unread divider + `N 条新消息` pill
- prepend 历史锚点补偿
- thinking 轻入口
- 大块粘贴 / 图片 / 路径识别
- compact boundary
- `McpUiCardHost + placement rules`
- remote/local MCP payload normalization
- unknown MCP action fallback card
- remediation recent-activity strip

### 12.2 主聊天优先补什么

[ChatPage.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/pages/ChatPage.vue)

- 把顶部 `activity-summary` 收进 turn 的 process fold
- 把 `showThinking` 改造成 turn 内轻状态，而不是独立漂浮区
- 把 `activePlanCard` 改造成 composer 上方 dock 的一部分
- 继续保留 approval overlay，但正文只显示轻阻塞提示
- 优先补 away summary、history 分页、虚拟滚动、回到底部 pill
- 监控图表优先作为 `final attachment`
- 当前可执行的控制面板优先作为当前 turn 的 `action surface`

### 12.3 协议工作台优先补什么

[ProtocolConversationPane.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/components/protocol-workspace/ProtocolConversationPane.vue)
[ProtocolWorkspacePage.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/pages/ProtocolWorkspacePage.vue)

- 把 `statusCard` 升级成真正的 `process fold`
- 折叠头部必须显示：
  - 已处理时长
  - 当前文件 / 搜索 / 命令 hint
  - 审批阻塞提示
- `ProtocolInlinePlanWidget` 和 `ProtocolBackgroundAgentsCard` 继续留在 composer 上方
- `ProtocolApprovalRail` 和 `ProtocolEventTimeline` 继续留在右侧，不回流正文
- 用户滚走后不再自动吸底
- 监控图表和常驻控制面板优先放在右栏 / MCP drawer / evidence，而不是正文无限堆叠

---

## 13. 统一后的实施优先级

这次整理后，只保留下面这一版优先级。

### P0. 先补共享结构层

- turn formatter
- 语义分类
- process summary
- collapse rules

### P1. 再补 MCP UI surface 协议和 host

- `McpUiCard` 数据契约
- `McpUiCardHost`
- `placement rules`
- 只读图表卡和控制面板卡的权限边界

### P2. 先让协议工作台长得对

原因不是它更重要，而是它现在数据层基础更好，改造成果也最明显。

优先做：

- `ProtocolTurnGroup`
- `ProtocolProcessFold`
- `statusCard -> process fold`
- 正文 / widget / 右栏边界彻底收紧
- MCP action 与 approval rail / timeline / evidence 串起来

### P3. 再把主聊天从卡片流改成线程

优先做：

- `visibleCards -> formattedTurns`
- `activity summary -> process fold`
- `thinking -> turn 内轻入口`
- `plan dock -> composer dock`
- 结果图表卡和当前 action surface 接进 turn

### P4. 再补交互与性能

- unread divider
- `N 条新消息` pill
- away summary
- 历史分页
- turn 级虚拟滚动
- prepend 锚点补偿

### P5. 最后补输入器、图表实现与视觉

- 大块粘贴 / 图片 / 路径识别
- 轻量 SVG 图表组件
- composer 尺寸和层级
- 消息排版
- 过程折叠的轻量视觉
- MCP 卡片视觉语言统一

顺序不要反。

如果先只改 CSS，最后大概率还是：

- 页面更好看一点
- 但信息结构依旧不专业
- 用户体验仍然像“调试卡片流”

---

## 14. 基线冻结与统一验收矩阵

当前实现已经补齐一份独立基线文档：

- [docs/chat-fix-acceptance-baseline.md](/Users/lizhongxuan/Desktop/aiops-codex/docs/chat-fix-acceptance-baseline.md)

这份基线文档解决了 3 件事：

- 盘点了主聊天和协议工作台当前真正依赖的状态来源
- 冻结了 6 组核心验收样本，并把 MCP bundle / action surface 作为扩展基线单列
- 把“结构 / 折叠 / 阻塞 / 滚动 / 输入器 / 性能 / MCP / 视觉”收敛成统一验收矩阵

后续如果继续改 turn formatter、approval 边界、history、virtualization 或 MCP surface，优先以这份基线文档为准，不再重新口头定义“这次算不算达标”。

---

## 15. 最终结论

这次整理后，文档已经没有实质性的范围冲突，结论也收敛成一句话：

`两个 chat 都应该从“原始卡片流”升级成“turn 级线程”；过程默认降级为可折叠层，结果与阻塞点始终优先；深层信息通过右栏、dock、modal、history 入口承接；后续的 MCP 图表卡和控制面板卡必须作为结构化 UI surface 接入，而不是回退成“消息里塞一大块数据”。`

如果按这个方案实施，两个 chat 会同时得到 4 个明显变化：

- 第一眼只看到结果和阻塞点，不再先看到过程噪音
- 过程不会消失，但会变成可折叠、可钻取、可感知推进的二级信息
- 长会话不会再因为自动吸底和全量渲染而显得“廉价”
- plan、agents、approval、timeline 会各回各位，不再像一堆卡片挤在一起
- 后续的监控图表与控制面板也能自然进入 chat，而不破坏整个对话结构
