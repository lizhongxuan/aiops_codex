# 协作工作台 UI V2 设计方案

## 整体布局

```
┌──────────────────────────────────────────────────────────────────┐
│ 工作台    [刷新]                    [终端] [agent-local ●] [GPT] │
├────────────────────────────────────────┬─────────────────────────┤
│                                        │ ⚠ 待审批决策 (4)        │
│  (聊天消息流，跟单机对话一致)            │                         │
│                                        │  ┌ web-02 ─────── ⏱ ┐  │
│  用户消息靠右，AI 回复靠左              │  │ 执行命令:            │  │
│  时间戳、头像、气泡样式复用 ChatPage    │  │ systemctl restart   │  │
│                                        │  │ [详情][授权][拒绝]   │  │
│                                        │  │ [✓ 同意执行]         │  │
│                                        │  └────────────────────┘  │
│                                        │                         │
│                                        │  ┌ web-01 ─────── ⏱ ┐  │
│                                        │  │ ...                │  │
│                                        │  └────────────────────┘  │
│                                        │                         │
│                                        ├─────────────────────────┤
│                                        │ 实时事件           [⟳]  │
│                                        │                         │
│                                        │ ● 10:21 Planner 生成   │
│                                        │   执行计划 plan-v3      │
│                                        │ ● 10:22 Dispatcher 下发 │
│                                        │   accepted 15, queued 10│
│                                        │ ● 10:23 web-01, web-02  │
│                                        │   等待审批              │
│                                        │ ● 系统运行中 15台 完成8 │
├────────────────────────────────────────┤                         │
│ ▸ 共 4 个任务，已完成 0 个        [展开]│                         │
│  ○ 1. 梳理现有 orchestrator 投影...    │                         │
│  ○ 2. 实现后端 richer read models...   │                         │
│  ○ 3. 更新 ProtocolPage...             │                         │
│  ○ 4. 补后端/前端测试并跑验证...       │                         │
│                                        │                         │
│ ▸ 2 background agents              [v]│                         │
│  🟢 web-01 (worker) running       Open │                         │
│  🟡 web-02 (worker) awaiting      Open │                         │
│                                        │                         │
│ ┌────────────────────────────────────┐ │                         │
│ │ Ask for follow up changes or @...  │ │                         │
│ │                                    │ │                         │
│ │ + GPT 5.4 v  超高 v        🎤 ⏹  │ │                         │
│ └────────────────────────────────────┘ │                         │
└────────────────────────────────────────┴─────────────────────────┘
```

## 一、左侧：聊天区

### 1.1 消息流

完全复用 ChatPage 的消息渲染逻辑和样式：
- 用户消息靠右，带头像和时间戳
- AI 回复靠左，带机器人图标和时间戳
- CardItem 组件复用（MessageCard、ErrorCard、NoticeCard 等）
- 审批卡片、命令卡片等也复用现有组件

不再在聊天流里内嵌"前台投影入口"、"派发给子 agent"、"完成情况"等结构化卡片组。
这些信息改为：
- 计划步骤 → 底部任务 checklist
- 运行中的 host agent → 底部 background agents
- 审批 → 右侧待审批面板
- 事件 → 右侧实时事件

### 1.2 底部区域（输入框上方）

输入框上方有两个可折叠区域：

#### 任务 checklist

```
▸ 共 4 个任务，已完成 0 个                              [展开/收起]
  ○ 1. 梳理现有 orchestrator 投影与 workspace 卡片生成链路
  ○ 2. 实现后端 richer read models
  ● 3. 更新 ProtocolPage（进行中）
  ○ 4. 补后端/前端测试并跑验证
```

- 数据来源：PlannerSession 生成的计划步骤
- 状态：○ 未开始、● 进行中、✓ 已完成、✗ 失败
- 默认展开，可点击标题行折叠
- 折叠后只显示摘要行："共 4 个任务，已完成 2 个"

#### Background agents

```
▸ 2 background agents (@ to tag agents)                [展开/收起]
  🟢 web-01 (worker) is running                            Open
  🟡 web-02 (worker) is awaiting instruction                Open
```

- 数据来源：当前 mission 中正在运行的 HostWorker
- 状态颜色：🟢 running、🟡 awaiting/waiting_approval、🔴 failed、⚪ queued
- "Open" 按钮点击后打开该 host 的详情抽屉（只读）
- 默认折叠，有活跃 agent 时自动展开
- 折叠后只显示摘要行："2 background agents"

### 1.3 输入框

复用 ChatPage 的 Omnibar 组件样式，加上 Codex App 风格的底部工具栏：

```
┌──────────────────────────────────────────────────────┐
│ Ask for follow up changes or @ to tag an agent       │
├──────────────────────────────────────────────────────┤
│ +   GPT 5.4 ▾   超高 ▾                      🎤  ⏹  │
└──────────────────────────────────────────────────────┘
```

- 输入框单行，输入时自动撑高（max 4 行）
- 底部工具栏：+ 按钮、模型选择、推理等级、麦克风、发送/停止
- @ 输入时弹出 agent 选择器（列出当前 mission 的 host worker）

## 二、右侧面板

右侧面板分上下两个区域，不用 tab 切换，直接堆叠。

### 2.1 待审批决策

```
⚠ 待审批决策 (4)                                        0 待处理
```

标题栏：
- 左侧：⚠ 图标 + "待审批决策" + 数量徽标
- 右侧：待处理计数

每个审批卡片：

```
┌─────────────────────────────────────────────────┐
│ 🖥 web-02                              ⏱ 04:59 │
│ ● 执行命令:                                      │
│   systemctl restart nginx                        │
│                                                   │
│ [详情]  [授权]  [拒绝]  [✓ 同意执行]              │
└─────────────────────────────────────────────────┘
```

- 主机名 + 倒计时（审批超时计时器，可选）
- 执行命令显示（带绿色圆点表示待执行）
- 四个按钮：
  - 详情：打开审批详情弹窗（命令全文、执行理由、终端上下文）
  - 授权：授权免审（蓝色按钮）
  - 拒绝：拒绝执行（橙色/红色按钮）
  - 同意执行：批准当前命令（绿色按钮，带 ✓ 图标，最醒目）

按钮颜色：
- 详情：灰色/白色（次要操作）
- 授权：蓝色
- 拒绝：橙红色
- 同意执行：绿色（主操作）

无待审批时显示：
```
✅ 当前没有待审批操作。
```

### 2.2 实时事件

```
实时事件                                              [⟳ 刷新]
```

时间线列表，每条事件：

```
● 10:21:35  Planner 生成执行计划 plan-v3
● 10:22:12  Dispatcher 下发任务：accepted 15, activated 5, queued 10
● 10:23:45  web-01, web-02 等待审批                    ← 橙色圆点
● 10:34:xx  系统运行中  共 15 个主机任务，已完成 8 ▲   ← 底部汇总
```

- 圆点颜色：蓝色（信息）、绿色（完成）、橙色（待审批/警告）、红色（失败）
- 最新事件在上面
- 底部有一个固定的汇总行："系统运行中 共 N 个主机任务，已完成 M"
- 汇总行可展开查看按状态分组的主机列表

## 三、去掉的东西

以下当前 ProtocolPage 的元素在 V2 中去掉：

1. ~~Host Agents 卡片列表~~（右侧面板）→ 改为底部 background agents 折叠列表
2. ~~命令通知面板~~（右侧面板）→ 改为右侧"实时事件"时间线
3. ~~"前台投影入口"卡片组~~（聊天流内）→ 去掉
4. ~~"主 Agent 派发给子 agent"卡片列表~~（聊天流内）→ 去掉
5. ~~"子 agent 完成情况"卡片列表~~（聊天流内）→ 去掉
6. ~~标题区的 pill 标签~~（Fleet·web-cluster、前台投影、当前命令放行）→ 精简到顶部导航栏
7. ~~自定义的 protocol-composer-box 输入框~~→ 复用 ChatPage 的 Omnibar

## 四、保留的弹窗/抽屉

以下弹窗/抽屉保留，但样式跟随 V2 调整：

1. 计划详情弹窗（点击任务 checklist 的某个步骤展开）
2. Host 详情抽屉（点击 background agent 的 "Open" 按钮）
3. 审批详情弹窗（点击审批卡片的"详情"按钮）
4. Planner 原始轨迹弹窗（从计划详情里进入）

## 五、数据流

```
PlannerSession 生成计划
  → 任务 checklist 数据
  → 通过 orchestrator 投影到 WorkspaceSession

Dispatcher 分发任务
  → background agents 列表数据
  → 实时事件时间线数据

WorkerSession 执行
  → 审批请求 → 右侧待审批面板
  → 进度/完成 → 实时事件时间线
  → AI 回复摘要 → 聊天流（作为 AI 消息）

用户操作
  → 聊天输入 → WorkspaceSession → PlannerSession
  → 审批决策 → 路由到 WorkerSession 原始审批接口
  → @ tag agent → 直接给指定 HostWorker 发消息
```

## 六、与 ChatPage 的复用关系

ProtocolPage V2 应该尽量复用 ChatPage 的组件：

| ChatPage 组件 | ProtocolPage V2 复用方式 |
|---|---|
| 消息流渲染（stream-row、CardItem） | 直接复用 |
| Omnibar（输入框） | 直接复用，加 @ agent 选择器 |
| ThinkingCard | 直接复用 |
| ErrorCard | 直接复用 |
| NoticeCard | 直接复用 |
| ApprovalCard（如果有） | 不在聊天流里用，改为右侧面板 |

新增的组件：
- `ApprovalPanel.vue`：右侧待审批面板
- `EventStream.vue`：右侧实时事件时间线
- `TaskChecklist.vue`：底部任务 checklist
- `BackgroundAgents.vue`：底部 background agents 列表

## 七、实现步骤

1. 重写 ProtocolPage.vue 的模板结构（两栏布局：左聊天+右面板）
2. 左侧聊天区复用 ChatPage 的消息渲染逻辑
3. 新增 ApprovalPanel 组件（右上）
4. 新增 EventStream 组件（右下）
5. 新增 TaskChecklist 组件（输入框上方）
6. 新增 BackgroundAgents 组件（输入框上方）
7. 复用 ChatPage 的 Omnibar 输入框
8. 去掉旧的 Host Agents 面板、命令通知面板、内嵌卡片组
9. 调整弹窗/抽屉样式
