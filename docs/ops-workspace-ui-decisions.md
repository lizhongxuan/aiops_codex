# AIOps Workspace UI 设计决策

## 背景

这份文档基于以下已存在上下文整理：

- 规划来源：[docs/final-verdict.md](/Users/lizhongxuan/Desktop/aiops-codex/docs/final-verdict.md)
- 架构图来源：[docs/aiops-architecture.puml](/Users/lizhongxuan/Desktop/aiops-codex/docs/aiops-architecture.puml)
- 当前产品壳子与视觉语言：[web/src/App.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/App.vue)、[web/src/style.css](/Users/lizhongxuan/Desktop/aiops-codex/web/src/style.css)
- 当前主机模型：[internal/model/types.go](/Users/lizhongxuan/Desktop/aiops-codex/internal/model/types.go)
- 当前主机选择交互：[web/src/components/HostModal.vue](/Users/lizhongxuan/Desktop/aiops-codex/web/src/components/HostModal.vue)

说明：

- 当前没有提供具体 Figma frame / node 链接，因此这里先把“现有 Codex app 的设计语言”当作目标视觉基线。
- Figma 侧已生成一张用于对齐信息架构和调度链路的 FigJam 图，后续可以在此基础上继续细化视觉稿。

## 1. 现有 Codex app 视觉语言提炼

### 1.1 设计基调

- 整体是浅色工作台，不走 marketing dashboard 路线，而是偏“工程操作台”。
- 左侧导航固定，主画布纯白，信息靠卡片、边框、状态点和小尺寸文字来分层。
- 交互重心在顶部 pill 和底部 omnibar，不靠重色按钮抢视觉。
- 动效轻，主要是 hover、阴影、blur、淡入，不做夸张过渡。

### 1.2 可复用视觉 token

- 背景色：
  - sidebar `#eceff3`
  - canvas `#ffffff`
  - drawer `#f8fafc`
- 文字：
  - 主文字 `#0f172a`
  - 次文字 `#64748b`
  - 元信息 `#9ca3af`
- 边框：
  - 主边框 `#e2e8f0`
  - 卡片边框 `#e5e7eb`
- 主强调色：
  - primary `#2563eb`
  - hover `#1d4ed8`
- 圆角：
  - 卡片圆角 `16px`
  - 输入容器 `18px - 20px`
  - pill `9999px`

### 1.3 组件语法

- 状态统一用 `dot + 文本` 或 `badge + 文本`，不要大面积红绿背景。
- 卡片阴影偏轻，强调“层级”而不是“漂浮感”。
- 主机 ID、路径、命令、版本号统一 monospace。
- 页面顶部工具栏继续沿用 header pill 语法，不改成传统后台 tab bar。
- 关键操作放在右上 header 和内容区的局部 toolbar，避免全屏工具条。

## 2. 新工作台的信息架构

目标不是新增三个彼此孤立的页面，而是在现有工作台里扩展成四类主视图：

1. `会话 / Chat Workspace`
2. `主机管理 / Hosts`
3. `经验包库 / Experience Packs`
4. `主 Agent 协议 / Protocol Workspace`

推荐左侧导航结构：

- 会话
- 主机
- 经验包
- 协议工作台
- 终端

推荐顶部全局控件：

- 主机范围选择器：单主机 / 主机组 / 全局 Fleet
- 连接状态：WS、Codex、Orchestrator、Runner
- 快捷动作：新建任务、批量执行、打开终端
- 当前上下文 pill：当前 scope、当前运行策略、审批模式

## 3. 共享布局原则

### 3.1 页面壳子

所有新页面都复用现有三段式结构：

- 左侧固定导航
- 顶部 sticky header
- 中间滚动主画布

不建议引入“新的独立后台模板”，否则会破坏当前 Codex app 的连续性。

### 3.2 主机范围先于页面内容

多主机场景下，页面最重要的不是筛选器，而是“当前控制范围”。

因此每个新页面都应在主标题下方增加 `Scope Bar`：

- 当前选中：`server-local` / `web-cluster` / `prod-all`
- 范围类型：单机 / 分组 / Fleet
- 在线情况：在线数 / 离线数 / 锁定数
- 执行策略：串行 / 并行 / 分批
- 审批模式：逐条审批 / 批次审批 / 会话记忆

### 3.3 右侧详情抽屉

当前项目已经有 modal / drawer 的语言，因此新页面优先采用：

- 中间列表或图谱负责总览
- 右侧 drawer 负责细节

不要把所有细节直接铺在主画布里。

## 4. 页面一：主机管理

### 4.1 页面目标

让用户完成三件事：

1. 看清当前主机池状态
2. 快速切换控制范围
3. 对主机执行单机或批量动作

### 4.2 页面布局

推荐三段结构：

1. 顶部 `KPI Strip`
2. 中部 `主机清单`
3. 右侧 `主机详情 Drawer`

### 4.3 KPI Strip

展示 4 到 6 个轻量指标卡：

- 在线主机数
- 离线主机数
- 可执行主机数
- 支持终端主机数
- Agent 版本漂移数
- 最近 5 分钟心跳异常数

风格上延续白底卡片 + 小标题 + 大数字，不做大色块告警屏。

### 4.4 主机清单

清单主视图优先用 table，而不是 card wall。原因是主机管理的核心是对比和批量操作。

推荐列：

- 复选框
- 主机名
- Host ID
- 状态
- 类型 kind
- OS / Arch
- Capabilities
- Labels
- Last heartbeat
- Quick actions

字段直接对齐现有模型：

- `id`
- `name`
- `kind`
- `status`
- `executable`
- `terminalCapable`
- `os`
- `arch`
- `agentVersion`
- `labels`
- `lastHeartbeat`

### 4.5 交互

- 单击行：更新当前 scope 为该主机，并打开详情 drawer。
- 双击行：直接进入终端。
- 顶部复选后出现批量操作条：
  - 批量打标签
  - 加入主机组
  - 批量执行 playbook
  - 交给主 Agent 生成任务
  - 导出主机清单

### 4.6 详情 Drawer

分 4 个分组：

- 基础信息
  - host name / id / kind / status / labels
- 能力信息
  - executable / terminalCapable / os / arch / agentVersion
- 实时状态
  - last heartbeat / 最近连接错误 / 当前锁定状态
- 快捷动作
  - 进入终端
  - 设为当前上下文
  - 发起诊断
  - 查看最近会话

### 4.7 关键状态

- 空状态：没有主机时显示 bootstrap 引导，而不是只有 “No hosts available”
- 离线状态：行级灰化，但保留“查看详情”
- 不可执行状态：允许查看，不允许发送任务
- 版本漂移：用 warning badge，不要直接标红

## 5. 页面二：经验包库管理

### 5.1 页面目标

让经验从“历史执行结果”沉淀为“可组合、可复用、可绑定主机群”的资产。

这个页面不是单纯知识库，而是介于：

- 运维经验
- Playbook
- Host Profile
- Planner prompt context

之间的组合库。

### 5.2 页面布局

推荐 `左筛选 + 中列表 + 右详情` 的资源库结构。

左侧筛选：

- 场景
- 适用 OS
- 风险级别
- 来源
- 版本
- 评分 / 置信度

中间主列表：

- table 和 card 双视图切换
- 默认 table，便于筛选和排序

右侧详情：

- 经验包摘要
- 版本链路
- 绑定 playbook
- 适用主机画像
- 触发条件

### 5.3 列表字段建议

- 名称
- 场景标签
- 当前版本
- 来源
- 适用范围
- 最近使用时间
- 关联 playbook 数
- 成功率
- 维护状态

### 5.4 详情页内容块

- 概要
  - 这个经验包解决什么问题
- 前置条件
  - OS、服务、依赖、权限要求
- 核心步骤
  - 可以是结构化步骤摘要，不直接铺长 YAML
- 关联资源
  - playbooks
  - host profiles
  - memory entries
- 版本演进
  - v1 -> v2 -> v3
- 风险与审批
  - 是否需要高权限
  - 是否必须逐机审批

### 5.5 关键动作

- 加载到主 Agent
- 附加到主机组
- 创建新版本
- 对比版本
- 回滚到旧版本
- 发布 / 下线

### 5.6 设计重点

- 经验包不是 markdown 文件列表，所以列表项必须突出“可执行性”和“适用性”。
- 版本链路建议用横向 timeline，而不是下拉纯文本。
- “绑定到主机组”应该是一等动作，因为它会直接影响后续 planner 选包。

## 6. 页面三：主 Agent - 多子 Agent 工作协议页

### 6.1 页面目标

这不是简单聊天页升级版，而是“任务编排可视化工作台”。

用户要能同时看到：

- 自己给主 Agent 的需求
- 主 Agent 生成的 DAG
- 子 Agent 被分配到哪些主机
- 每个节点当前状态
- 审批在哪里卡住
- 结果如何被回收并沉淀为经验

### 6.2 页面布局

推荐三栏主结构：

1. 左栏 `用户对话 + 任务上下文`
2. 中栏 `黑板 DAG`
3. 右栏 `子 Agent / Host lanes`

底部仍然保留当前项目的 omnibar 交互，但升级为：

- 自然语言输入
- 可插入主机范围
- 可插入经验包
- 可插入 playbook

### 6.3 左栏：用户对话与任务上下文

沿用当前聊天页的语言，但做两点增强：

- 当前任务范围固定显示
  - 主机组
  - 执行策略
  - 审批模式
- 最近输入产物可折叠显示
  - planner brief
  - chosen packs
  - selected playbooks

### 6.4 中栏：黑板 DAG

这是协议页的主视觉中心。

节点类型建议分为：

- `observe`
- `analyze`
- `plan`
- `execute`
- `verify`
- `fallback-runner`
- `learn`

每个节点卡片展示：

- 节点标题
- 目标主机 / 主机组
- 当前状态
- 依赖关系
- 审批标记
- 运行摘要

状态颜色建议：

- pending：灰
- queued：蓝灰
- running：蓝
- waiting_approval：橙
- blocked：黄
- failed：红
- completed：绿

图形建议：

- DAG 走浅边框卡片，不做深色流程图
- 当前执行路径高亮
- 失败节点可以展开“失败原因 + fallback 路径”

### 6.5 右栏：子 Agent / Host Lanes

右栏按 host 或 host group 分 lane：

- lane 标题：主机名 / 组名
- lane 状态：在线、忙碌、锁定、审批中
- 子 Agent 卡片：当前任务、最近输出、已执行命令数、文件改动数

点击 lane 可展开：

- 当前 session
- 终端入口
- 审批队列
- 最新结果

### 6.6 协议页核心交互

用户输入需求后，页面按以下节奏演进：

1. 展示主 Agent 的规划摘要
2. 中栏生成 DAG 草图
3. DAG 节点按依赖关系进入 queued / running
4. 右栏生成对应子 Agent lanes
5. 审批出现时，中栏节点和右栏 lane 同时高亮
6. 结果回收后，出现任务总结和经验沉淀建议

### 6.7 为什么不能只做聊天流

因为你的目标不是“让用户看文本输出”，而是“让用户控制多主机、多会话、多步骤执行”。

所以协议页必须把三条线同时可视化：

- 任务依赖线
- 主机执行线
- 审批与失败线

## 7. 多主机控制设计

### 7.1 多主机控制不是 modal

当前 `HostModal` 只适合单主机切换。进入多主机场景后，需要升级为 `Scope Bar + Scope Drawer`：

- 默认显示当前范围 pill
- 点击后打开范围选择器
- 可选：
  - 单主机
  - 主机组
  - 动态筛选结果
  - 全量 fleet

### 7.2 多主机执行策略

在协议页和主机页都要有明确执行策略可见：

- 串行
- 完全并行
- 分批并行
- 失败即停
- 允许部分失败

这些策略不应该藏在设置页。

### 7.3 主机组概念

建议前端先引入一级实体 `Host Group`，即使后端第一期只是软分组。

主机组最少包含：

- 名称
- 描述
- labels 选择器
- 主机数量
- 风险级别
- 默认执行策略

### 7.4 审批模式

多主机下审批必须区分：

- 单主机单步审批
- 同类命令批次审批
- 会话内记忆审批

UI 上建议作为一个显式 pill 放在 header 中。

## 8. 建议路由与前端模块

推荐新增路由：

- `/hosts`
- `/experience-packs`
- `/protocol`

推荐新增页面组件：

- `web/src/pages/HostsPage.vue`
- `web/src/pages/ExperiencePacksPage.vue`
- `web/src/pages/ProtocolPage.vue`

推荐新增共享组件：

- `HostScopeBar.vue`
- `MetricStrip.vue`
- `HostInventoryTable.vue`
- `HostDetailDrawer.vue`
- `PackLibraryTable.vue`
- `PackVersionTimeline.vue`
- `DagBoard.vue`
- `DagNodeCard.vue`
- `AgentLane.vue`
- `ExecutionPolicyPill.vue`

推荐 store 拆分方向：

- `hostCenter`
- `packLibrary`
- `protocolWorkspace`

## 9. 页面与后端能力映射

### 9.1 主机管理页

直接消费现有主机模型与主机切换能力。

### 9.2 经验包库页

对应未来的：

- `internal/memory/experience.go`
- `internal/runner/playbook.go`
- 向量搜索

### 9.3 协议页

对应未来的：

- `internal/orchestrator/planner.go`
- `internal/orchestrator/dag.go`
- `internal/orchestrator/dispatcher.go`
- `internal/orchestrator/collector.go`

协议页 UI 不应该等待所有后端完成后再设计，而应该先把视图骨架定下来，反向约束后端输出形态。

## 10. 建议实现顺序

### Phase 1

- 扩展左侧导航
- 新增 `HostsPage`
- 把 `HostModal` 升级为 `HostScopeBar`

### Phase 2

- 新增 `ProtocolPage`
- 先用假数据渲染 DAG 与 Agent lanes
- 跑通主 Agent 规划摘要 + 节点状态流转

### Phase 3

- 新增 `ExperiencePacksPage`
- 打通经验包列表、详情、绑定动作

### Phase 4

- 将审批卡、结果汇总、终端入口统一挂接到协议页
- 打通多主机执行策略与批量动作

## 11. 一句话设计结论

这三个页面不应被设计成传统后台里的三张 CRUD 页，而应该共同组成一个“以主机范围为上下文、以 DAG 为核心、以经验沉淀为闭环”的浅色工程工作台。
