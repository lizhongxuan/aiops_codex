# 主 Agent -> 子 Agent 调度器设计（MVP，优化版）

## 1. 背景

根据 [final-verdict.md](/Users/lizhongxuan/Desktop/aiops-codex/docs/final-verdict.md) 的结论，当前项目的基础设施已经足够完整：

- `ai-server` 已经有 Codex `thread/start`、`turn/start`、审批、会话、快照、WebSocket、host-agent gRPC 流
- `host-agent` 已经能执行命令、回传输出、终端接入、文件操作、心跳与重连
- `web` 已经有普通聊天页、审批卡片、终端页和“协作工作台”的原型 UI

当前真正缺的不是 transport，而是 `ai-server` 内部的一层可靠调度器：

- 用户请求进入后，谁来做规划
- 规划会话如何把任务拆给多个主机
- 子 Agent 如何绑定到具体主机
- 子 Agent 的回执、审批、完成情况如何被可靠收集
- 后期主机规模达到几百上千台时，如何保护单个 Codex `app-server` 不被并发线程和 turn 打崩

这份文档只设计第一阶段要先落地的最小闭环：

**用户在协作工作台会话里给“主 Agent”下需求，`ai-server` 创建独立的 PlannerSession 做规划，调度器为目标主机创建逻辑 Worker，受预算控制地启动子 Agent，收集进度 / 审批 / 结果，再以前台投影的方式回投到协作工作台会话。**

## 2. 第一阶段目标

### 2.1 要实现的能力

1. 单机对话页会话、协作工作台会话、PlannerSession、WorkerSession 都有独立会话身份。
2. PlannerSession 和每个 WorkerSession 都有独立工作区。
3. PlannerSession 能以结构化方式把多个子任务交给调度器。
4. 调度器为每个目标主机创建或复用一个逻辑 Worker。
5. 一个 mission 内，同一台主机只对应一个逻辑 Worker；同一主机上的多个任务串行进入这个 Worker。
6. 调度器能收集子 Agent 的进度、审批、结果和失败信息。
7. 调度器把这些信息投影成协作工作台会话里的 UI 卡片，供聊天流、弹窗和抽屉显示。
8. 即使 mission 覆盖几百上千台主机，单个 Codex `app-server` 也只承受受控数量的活跃 thread / turn。
9. 单机对话页保持原有“选定主机后直接对话”的模型，不被 orchestrator 路径污染。

### 2.2 明确不做的事

第一阶段不做下面这些，以免范围失控：

- 不做通用 DAG 引擎
- 不做 playbook / runner 执行闭环
- 不做子 Agent 之间直接通信
- 不做完整的任务回放系统
- 不做跨 mission 的经验沉淀
- 不做“所有主机同时激活 AI 子会话”的激进并发模型

一句话：

**先把可靠的 fan-out / fan-in + workspace 隔离 + app-server 背压跑通，再做 DAG、planner 自动重规划和 runner。**

## 3. 术语

### 3.1 SingleHostSession

用户在“页面一：单机对话框”里看到的普通会话。

- 面向用户
- 选定某个主机后直接与该主机会话交互
- 继续沿用当前单 host 排障 / 执行路径
- 不参与 mission fan-out / fan-in
- 仍然是用户想直接和某台主机继续聊天时的最终落点

### 3.2 WorkspaceSession

用户在“页面二：协作工作台对话框”里看到的主 Agent 会话。

- 面向用户
- 是主 Agent 的前台投影，不是后台 PlannerSession 本体
- 接收用户输入
- 展示计划摘要、派发摘要、审批镜像、结果汇总
- 默认不直接暴露 Planner 原始 tool payload 和内部轨迹

### 3.3 PlannerSession

PlannerSession 是 `ai-server` 为某个 mission 创建的独立内部会话，也是一个独立 Codex thread。

- 不直接面向用户
- 负责理解请求、读取监控 MCP / skills / 经验包、拆任务、生成 plan / DAG、提交结构化派发
- 有独立 thread
- 有独立本地 workspace
- 不直接跨主机执行命令

### 3.4 WorkerSession

WorkerSession 是绑定到某一台具体主机的独立内部会话，也是一个独立 Codex thread。

- 一个 mission 内，一台主机最多一个逻辑 WorkerSession
- 该会话内部可串行执行多个任务
- 有独立 thread
- 有独立工作区
- 不直接和其他 WorkerSession 通信

### 3.5 host-agent

host-agent 是远端主机上的 Go 进程。

- 提供 exec / terminal / file 操作
- 通过 gRPC 双向流连接 `ai-server`
- 是执行层，不是推理层

### 3.6 调度器

调度器运行在 `ai-server` 内部，是 Go 层状态机。

- 接收 PlannerSession 的结构化派发请求
- 创建和管理 WorkerSession
- 分配 workspace
- 控制活跃 Worker 并发
- 收集 Worker 回执和审批
- 把 Worker 状态投影回 WorkspaceSession

## 4. 设计原则

### 4.1 调度在 Go 层

调度器必须在 Go 层掌握任务状态，而不是靠 LLM 自己“想起来”要给谁发消息。

原因：

- 状态可靠
- 审批可控
- 背压可控
- 重试可控
- 断连可恢复
- 便于 UI 投影

### 4.2 PlannerSession 必须独立于 WorkspaceSession

用户在协作工作台里看到的“主 Agent 会话”不等于后台真实主 Agent。

第一阶段就应该把二者拆开：

- WorkspaceSession 负责交互与展示
- PlannerSession 负责规划与派发

这样后面补自动重规划、任务回收、mission 级恢复时，不需要再把前台会话语义推翻重来。

### 4.3 协作工作台采用前台投影模式

协作工作台的主聊天流只展示摘要级信息。

- 默认只显示主 Agent 口径的摘要
- 点击摘要后，优先打开弹窗 / 抽屉查看结构化详情
- 计划详情默认展示结构化过程，原始 Planner 轨迹作为第二层入口
- 派发详情默认按 host 聚合，而不是按 DAG 节点平铺
- Worker 详情默认只读；如果用户要继续和某台主机交互，应跳转到 SingleHostSession

这样可以保证：

- 主聊天流不被内部调度日志刷爆
- 前台查看详情不额外放大 Codex turn 压力
- 主 Agent 的“对用户表达”和后台 Planner 的“内部规划过程”可以分别演进

### 4.4 监控 MCP / skills / 经验包默认由 PlannerSession 调用

为了避免上下文和权限边界混乱，默认约束如下：

- 协作工作台前台会话不直接调用监控 MCP / skills / 经验包
- PlannerSession 负责这些上下文获取
- PlannerSession 基于这些上下文生成 plan / DAG / dispatch
- 前台工作台只显示这些调用的摘要和命中结果

### 4.5 一主机一逻辑 Worker，不等于一开始就创建一千个活跃 thread

这条是后期规模化最关键的原则。

- mission 可以引用几百上千台 host
- 但调度器只应让有限数量的 WorkerSession 处于活跃 turn 状态
- 其余 host 只保留逻辑 Worker 和排队任务，不急着 materialize 成活跃 Codex thread

### 4.6 智能在 Codex 层

PlannerSession / WorkerSession 仍然是 Codex thread。

- 推理、计划、工具选择、结果总结由 Codex 做
- Go 只做生命周期管理、状态推进、预算控制、事件归档和 UI 投影

### 4.7 执行在 host-agent 层

实际执行继续复用现有：

- `sendAgentEnvelope`
- `remote exec`
- `remote file ops`
- `approval`
- `terminal`

调度器不重新发明执行器。

### 4.8 工作区是默认根，不是唯一可改范围

PlannerSession 和 WorkerSession 都需要独立工作区。

但这里的工作区语义是：

- 默认 `cwd`
- 产物沉淀目录
- 草稿 / 临时文件目录
- prompt 中的默认操作根

它不是说子 Agent 只能动工作区里的文件。运维场景下修改 `/etc/nginx`、`/var/log` 仍然是刚需，继续复用现有审批流即可。

## 5. 复用现有能力

第一阶段实现时，应尽量复用现有代码，而不是另起一套系统。

### 5.1 现有 Codex 能力

当前已经有：

- `thread/start`
- `turn/start`
- `turn/interrupt`
- `handleCodexNotification`
- transcript 持久化

可直接用于创建 PlannerSession / WorkerSession 的 thread。

### 5.2 现有远程主机能力

当前已经有：

- `agentConnection`
- `sendAgentEnvelope`
- `handleAgentExecOutput`
- `handleAgentExecExit`
- `remote files`
- `terminal`

WorkerSession 不需要任何新的 host transport。

### 5.3 现有会话和卡片能力

当前已经有：

- `store.SessionState`
- `store.SetSelectedHost`
- `store.SetThread`
- `store.SetTurn`
- `store.UpsertCard`
- `broadcastSnapshot`

调度器第一阶段继续复用卡片系统做 UI 投影。

### 5.4 现有审批能力

当前已经有：

- `ApprovalRequest`
- `ApprovalGrant`
- 远程命令审批
- 文件变更审批
- 审批决策后继续执行

调度器不新增第二套审批机制，只做 **审批镜像和路由**。

## 6. 总体方案

第一阶段采用下面这条主链路：

1. 用户在 WorkspaceSession 里给主 Agent 下需求。
2. `ai-server` 创建 mission。
3. `ai-server` 为该 mission 创建独立的 PlannerSession，并分配 planner workspace。
4. PlannerSession 读取监控 MCP / skills / 经验包，生成 plan / DAG，并通过结构化动态工具把任务提交给调度器。
5. 调度器为每个 host 创建或复用一个逻辑 Worker。
6. 调度器只在预算允许时，才把逻辑 Worker 激活成活跃 WorkerSession turn。
7. 活跃 WorkerSession 在各自主机上复用现有远程工具执行。
8. 调度器通过权威 hook + snapshot fallback 收集进度、审批、回复和完成情况。
9. 调度器把这些变化写回 WorkspaceSession 卡片和协作工作台读模型。
10. 用户如果想深入某台主机，只读查看 WorkerSession 详情；如果要继续互动，则跳转到 SingleHostSession。

这个过程里：

- SingleHostSession 保持原路径，不走 mission orchestrator
- WorkspaceSession 不直接承担规划和 fan-out
- PlannerSession 和 WorkerSession 都是内部 session
- 主机多不代表同时活跃的 AI thread 多
- 调度器是唯一中枢
- host-agent 继续只做执行和输出回传

## 7. 核心对象模型

第一阶段建议在 `internal/orchestrator/` 下定义如下对象。

### 7.1 Mission

```go
type Mission struct {
    ID                  string
    WorkspaceSessionID  string
    PlannerSessionID    string
    PlannerThreadID     string
    Title               string
    Summary             string
    Status              string
    ProjectionMode      string // front_projection
    CreatedAt           string
    UpdatedAt           string
    Workers             map[string]*HostWorker    // key: hostID
    Tasks               map[string]*TaskRun
    Workspaces          map[string]*WorkspaceLease // key: leaseID
    Events              []RelayEvent

    GlobalActiveBudget  int
    MissionActiveBudget int
}
```

说明：

- `WorkspaceSessionID` 指协作工作台里用户看到的前台主 Agent 会话
- `PlannerSessionID` / `PlannerThreadID` 是独立规划会话
- `ProjectionMode` 第一阶段固定为 `front_projection`
- `Workers` 负责把 host 绑定到逻辑 Worker
- `Workspaces` 管理 planner / worker 的工作区租约
- `GlobalActiveBudget` / `MissionActiveBudget` 用于 app-server 负载保护

### 7.2 TaskRun

```go
type TaskRun struct {
    ID             string
    MissionID      string
    HostID         string
    WorkerHostID   string
    SessionID      string
    ThreadID       string
    Title          string
    Instruction    string
    Constraints    []string
    Status         string
    ExternalNodeID string
    Attempt        int
    CreatedAt      string
    UpdatedAt      string
    LastError      string
    LastReply      string
    ApprovalState  string
}
```

任务状态建议：

- `queued`
- `ready`
- `dispatching`
- `running`
- `waiting_approval`
- `completed`
- `failed`
- `cancelled`

说明：

- 第一阶段还不做通用 DAG，所以不用真的实现节点依赖
- `ExternalNodeID` 只是给未来 planner / DAG 兼容预留，不参与当前状态推进

### 7.3 HostWorker

```go
type HostWorker struct {
    MissionID      string
    HostID         string
    SessionID      string
    ThreadID       string
    WorkspaceID    string
    ActiveTaskID   string
    QueueTaskIDs   []string
    Status         string
    LastSeenAt     string
    IdleSince      string
}
```

说明：

- 一台主机在一个 mission 内只有一个逻辑 Worker
- `HostWorker` 表示 host 绑定的 mailbox / actor，不只是 session 引用
- 同一主机多个 task 放进 `QueueTaskIDs` 串行执行
- Worker 可以逻辑存在但暂时没有活跃 turn

### 7.4 WorkspaceLease

```go
type WorkspaceLease struct {
    ID          string
    MissionID   string
    SessionID   string
    HostID      string
    Kind        string // planner | worker
    LocalPath   string
    RemotePath  string
    Status      string
    CreatedAt   string
    UpdatedAt   string
}
```

说明：

- PlannerSession 一定有 `Kind=planner` 的 workspace
- 每个 HostWorker 一定有 `Kind=worker` 的 workspace
- `LocalPath` 用于本地产物和审计
- `RemotePath` 用于远程默认 `cwd` 和临时产物

### 7.5 RelayEvent

```go
type RelayEvent struct {
    ID             string
    MissionID      string
    TaskID         string
    HostID         string
    SessionID      string
    Type           string
    Status         string
    Summary        string
    Detail         string
    ApprovalID     string
    SourceCardID   string
    CreatedAt      string
}
```

事件类型建议：

- `dispatch`
- `progress`
- `approval_requested`
- `approval_resolved`
- `reply`
- `completed`
- `failed`
- `cancelled`

## 8. 工作区模型

这是这版设计相对旧版最大的补充。

### 8.1 Planner 工作区

PlannerSession 默认使用本地工作区：

```text
<default-workspace>/missions/<missionID>/planner
```

用途：

- 规划草稿
- 汇总产物
- mission 级临时文件

### 8.2 Worker 工作区

每个 HostWorker 有一份自己的工作区租约：

本地路径：

```text
<default-workspace>/missions/<missionID>/hosts/<hostID>
```

远程路径：

```text
<AIOPS_AGENT_WORKSPACE_ROOT>/<missionID>/<hostID>
```

建议默认环境变量：

```text
AIOPS_AGENT_WORKSPACE_ROOT=~/.aiops_codex/workspaces
```

### 8.3 MVP 的远程工作区准备方式

第一阶段不需要马上给 host-agent 加新 RPC。

可以先这样做：

1. 调度器为 HostWorker 分配 `RemotePath`
2. 在第一次启动该 Worker 前，用现有远程只读 / 变更流程确保目录存在
3. 后续 WorkerSession 默认把这个路径作为 `cwd`

第二阶段再考虑新增：

- `workspace/prepare`
- `workspace/cleanup`

这样的显式 host-agent RPC。

### 8.4 工作区与系统文件的关系

需要明确：

- workspace 是默认根
- 不是唯一可访问根
- 访问系统文件仍然允许
- 对系统状态变更仍然必须走现有审批

## 9. PlannerSession 和调度器的接口

第一阶段不要靠解析 PlannerSession 的自然语言来派发任务，太脆弱。

### 9.1 PlannerSession 的工具域

PlannerSession 默认拥有以下上下文能力：

- 监控 MCP
- 经验包 / playbook 检索
- skills
- `orchestrator_dispatch_tasks`

WorkspaceSession 默认不直接拥有这些工具。

也就是说：

- 用户在协作工作台里输入需求
- `ai-server` 把需求转交给 PlannerSession
- PlannerSession 调用监控 MCP / skills / 经验包
- PlannerSession 生成 plan / DAG / dispatch
- WorkspaceSession 只接收摘要投影

这样可以保证：

- 工具调用责任边界清晰
- 监控上下文只在 PlannerSession 聚合
- 前台会话不被内部 tool 细节污染

建议新增一个动态工具：

`orchestrator_dispatch_tasks`

### 9.2 工具输入

建议 schema：

```json
{
  "missionTitle": "发布前 web 集群 nginx 巡检与自修复",
  "summary": "先对 web 集群做配置校验和健康检查，只对 web-08 的 reload 申请审批。",
  "tasks": [
    {
      "taskId": "task-web-08",
      "hostId": "web-08",
      "title": "reload 前门禁",
      "instruction": "检查 nginx 配置；若通过则申请本次 reload 审批；然后汇总 reload 前后探针。",
      "constraints": [
        "审批只作用于当前命令",
        "保留 reload 前后摘要"
      ],
      "externalNodeId": "node-exec-web08"
    }
  ]
}
```

### 9.3 工具输出

建议返回：

```json
{
  "missionId": "mission-xxx",
  "accepted": 128,
  "queued": 872,
  "activated": 16,
  "workers": [
    {
      "hostId": "web-08",
      "sessionId": "sess-xxx",
      "status": "active"
    },
    {
      "hostId": "web-09",
      "sessionId": "",
      "status": "queued"
    }
  ]
}
```

### 9.4 为什么一定要返回 queued / activated

因为大规模场景下：

- mission 可以有 1000 台 host
- 但活跃 Worker 可能只有 16 或 32 个

PlannerSession 必须知道：

- 任务被接受了多少
- 当前真正被激活了多少
- 其余多少在等待调度

这样它不会误以为所有子 Agent 都已经同时开始工作。

## 10. 子 Agent 启动方式

调度器在收到 task 后，为目标 host 做如下步骤：

1. 查找或创建 HostWorker
2. 分配或复用 Worker workspace
3. 若没有内部 session，则创建 WorkerSession
4. `store.SetSelectedHost(workerSessionID, hostID)`
5. 在预算允许时调用现有 `ensureThread`
6. 拼接 Worker prompt
7. 调用现有 `requestTurn`

### 10.1 WorkerSession 创建策略

不建议把 WorkerSession 挂到普通浏览器会话列表中。

第一阶段建议：

- 继续复用 `store.EnsureSession(sessionID)`
- 但 session 元数据要标记 `kind=worker`、`visible=false`
- mission 对 WorkerSession 的归属关系保存在 orchestrator 自己的 `Mission.Workers`
- Web 普通“历史会话”列表默认不显示这些 orchestrated internal sessions

这样可以避免破坏现有会话系统，同时保留后续可调试性。

### 10.2 Worker 指令包装

调度器发给 WorkerSession 的输入，不应该只是原始任务文本，而应该是包装后的执行指令：

```text
你是一个绑定到 host=web-08 的 WorkerSession。
你不是直接对用户回复；你的结果会由调度器回投给 WorkspaceSession。
只在当前主机范围内行动。
默认工作区：
...
任务目标：
...
约束：
...
输出要求：
1. 简要说明做了什么
2. 当前状态（completed / waiting_approval / failed）
3. 关键命令与关键结果摘要
```

## 11. Codex app-server 负载保护与背压

这是这版设计的另一个核心补充。

### 11.1 基本原则

整个 `ai-server` 后面只维护一个共享的 Codex `app-server` 进程。

不做：

- 一台 host 一个 app-server 进程
- 一台 host 一个永久活跃 thread
- 1000 台 host 同时 `turn/start`

而是做：

- 一个 mission 可以有很多逻辑 Worker
- 但只有有限数量的 Worker 处于活跃 turn 状态

### 11.2 两层预算

建议至少有两层预算：

1. 全局预算 `GlobalActiveBudget`
2. 单 mission 预算 `MissionActiveBudget`

推荐默认值：

- `GlobalActiveBudget`: 32
- `MissionActiveBudget`: 8 或 16

这不是写死的数字，而是第一阶段保守值。

额外约定：

- PlannerSession 活跃时也要占用预算
- 调度器应预留少量全局余量给用户普通会话，不能把全部预算都给 mission worker

### 11.3 速率限制

除了活跃预算，还需要三类速率保护：

1. `ThreadCreateRateLimit`
2. `TurnStartRateLimit`
3. `PendingRequestBudget`

建议第一阶段至少这样处理：

- `thread/start` 不允许瞬时为数百个 host 同时发起
- `turn/start` 用 token bucket 或简单定时器限速
- 当 Codex client 的 pending request 达到阈值时，dispatcher 停止继续激活新的 worker

原因：

- 压垮 `app-server` 的不只是“同时活跃多少个 worker”
- 还有“单位时间内创建了多少 thread”
- 以及“同时挂了多少未完成 RPC”

### 11.4 调度方式

dispatcher 不要在 `Dispatch()` 里立刻把所有 task 都变成活跃 turn。

应该是：

1. 全部 task 先进 mission queue
2. 只有满足预算的 host 才进入 `ready -> dispatching -> running`
3. 同一 host 始终 single-flight
4. 某个 Worker 完成、失败、等待审批或被取消后，再释放预算
5. 调度器继续激活下一批 ready host

### 11.5 lazy materialization

逻辑 Worker 不等于活跃 Codex thread。

建议：

- HostWorker 可以先只有 `HostID + QueueTaskIDs`
- 只有在它要真正执行第一条 task 时，才创建 thread
- 长时间 idle 的 Worker thread 可以回收

这样 mission 即使覆盖 1000 台 host，也不是 1000 个 thread 同时在线。

### 11.6 thread 回收策略

建议：

- Worker thread 空闲超过一定 TTL 时自动清理
- mission 结束后统一回收所有内部 thread
- thread 丢失时允许按现有逻辑自动重建

### 11.7 为什么这比“直接一主机一 thread 常驻”更合理

因为真正压垮 `app-server` 的不是 mission 里出现了多少 host，而是：

- 同时有多少活跃 turn
- 同时有多少工具调用
- 单位时间创建了多少 thread / turn
- 同时有多少通知和审批交互

所以需要控制的是 **活跃度**，不是 mission 声明了多少 host。

## 12. 结果收集设计

第一阶段的 collector 仍然追求简单，但不能只靠 snapshot diff。

### 12.1 两级事件源

collector 应该有两级事件源：

一级是权威 hook：

- turn started / completed / failed / aborted
- approval requested / resolved
- remote exec started / exit
- host offline / unavailable

二级是 snapshot fallback：

- Assistant 回复摘要
- 主动补齐 UI 所需状态
- 容错恢复

### 12.2 为什么不能只靠 `broadcastSnapshot`

只靠 `broadcastSnapshot -> diff` 有三个问题：

- snapshot 是 UI 读模型，不是领域事件
- 卡片多了以后 diff 成本会越来越高
- 某些事件本来就已经在 server hook 中被明确识别过，再二次推断容易重复

因此第一阶段最稳的做法是：

- 权威状态变化直接打给 orchestrator
- snapshot 只负责补摘要和兜底

### 12.3 去重状态

建议维护：

```go
type WorkerSeenState struct {
    LastTurnPhase      string
    SeenCardIDs        map[string]string
    SeenApprovalStatus map[string]string
    LastReplyCardID    string
}
```

只有状态发生变化时才产生新的 `RelayEvent`。

## 13. 协作工作台前台投影设计

调度器收集到的子会话事件，不应直接塞原始终端输出回 WorkspaceSession。

WorkspaceSession 只投影摘要级卡片和结构化详情入口。

### 13.1 建议卡片类型

- `MissionCard`
- `WorkerProgressCard`
- `WorkerApprovalCard`
- `WorkerCompletionCard`

### 13.2 主聊天流只展示摘要

主聊天流推荐只显示类似下面这些摘要：

- `主 Agent 已生成 3 步计划`
- `已派发给 web-08 / web-09`
- `web-08 当前 reload 等待审批`
- `web-09 健康检查已完成`

这些摘要都必须是可点击入口，而不是死文案。

### 13.3 详情承载形式

详情展示遵循下面的默认规则：

- 优先使用弹窗 / 抽屉，不在聊天流里大段展开
- 计划详情默认展示结构化过程
- 原始 Planner 轨迹作为第二层入口
- 派发详情默认按 host 聚合
- Worker 详情默认只读
- 需要继续和某台主机互动时，跳转到 SingleHostSession

一个重要约束是：

- 点开详情本质上读取 projector 生成的读模型
- 不应为了“看详情”再额外触发新的 Planner turn
- 不应为了“看详情”让大量 WorkerSession 同时活跃

### 13.4 卡片必须稳定 Upsert

第一阶段不要做“一个事件一张新卡”的刷屏模型。

建议：

- 每个 mission 一张 `MissionCard`
- 每个 host 一张 `WorkerProgressCard`
- 每个待审批对象一张 `WorkerApprovalCard`
- 每个 host 完成后收敛成一张 `WorkerCompletionCard`

卡片 ID 用确定性 key：

- `mission:<id>`
- `mission:<id>:host:<hostID>:progress`
- `mission:<id>:host:<hostID>:approval:<approvalID>`
- `mission:<id>:host:<hostID>:completion`

这样在 1000 台 host 时，主聊天流仍然是可控的。

### 13.5 协作工作台读模型对象

为了把“前台投影模式”正式落地，建议 projector 至少产出以下读模型对象。

#### PlanSummaryView

```json
{
  "label": "主 Agent 已生成 3 步计划",
  "caption": "前台工作台只展示摘要，详细过程放到计划详情抽屉里。",
  "tone": "info",
  "status": "已生成",
  "stepCount": 3,
  "plannerSessionId": "planner-sess-web-cluster"
}
```

#### PlanDetailView

```json
{
  "title": "PlannerSession 计划详情",
  "goal": "发布前对 web 集群做配置守卫、reload 风险收敛和健康验证。",
  "version": "plan-v3",
  "generatedAt": "19:43",
  "ownerSessionLabel": "主 Agent 工作台会话（前台投影）",
  "plannerSessionLabel": "planner-sess-web-cluster",
  "dagSummary": {
    "nodes": 8,
    "running": 2,
    "waitingApproval": 1,
    "queued": 2
  },
  "structured_process": [],
  "raw_planner_trace_ref": {
    "sessionId": "planner-sess-web-cluster",
    "threadId": "thread-planner-web-cluster"
  }
}
```

要求：

- `structured_process` 是默认展示层
- `raw_planner_trace_ref` 是第二层入口，不默认塞进主聊天流
- 计划详情里需要带 MCP / skills / 经验包命中摘要

#### DispatchSummaryView

```json
{
  "label": "已派发给 web-08 / web-09",
  "caption": "点击查看按主机组织的派发详情和任务约束。",
  "accepted": 8,
  "activated": 3,
  "queued": 2
}
```

#### DispatchHostDetailView

```json
{
  "hostId": "host-prod-08",
  "host": "web-08",
  "status": "等待审批",
  "request": {
    "title": "主 Agent 派发",
    "summary": "执行配置校验；若通过则申请本次 reload 审批，并回传摘要。",
    "constraints": [
      "审批只作用于当前命令",
      "必须保留 reload 前后输出摘要"
    ]
  },
  "events": []
}
```

要求：

- 必须按 host 聚合，不按 step 聚合
- 需要展示任务标题、instruction、constraints、当前状态和最近回执

#### WorkerReadonlyDetailView

```json
{
  "hostId": "host-prod-08",
  "mode": "readonly",
  "transcript": [],
  "terminal": {},
  "approval": {},
  "jumpTarget": {
    "type": "single_host_chat",
    "hostId": "host-prod-08"
  }
}
```

要求：

- 默认只读
- 展示 transcript、终端上下文、审批信息和结果摘要
- 不允许在抽屉里继续直接给子 agent 发消息
- 必须提供跳转到 SingleHostSession 的按钮或链接

## 14. 审批路由设计

审批仍然由 WorkerSession 拥有。

也就是说：

- 原始 `ApprovalRequest` 仍然挂在 WorkerSession 下
- WorkspaceSession 只展示镜像卡片

### 14.1 为什么不把审批迁到 WorkspaceSession

因为当前审批机制已经绑定：

- `sessionID`
- `threadID`
- `turnID`
- `approvalID`

如果复制成第二份审批对象，容易造成状态分叉。

### 14.2 镜像策略

WorkspaceSession 展示 `WorkerApprovalCard`，卡片中保存：

- `missionId`
- `workerSessionID`
- `approvalID`
- `hostID`

用户在主页面点：

- `是`
- `授权免审`
- `否`

本质上还是把决策路由到 WorkerSession 原始审批接口。

## 15. 停止 / 取消策略

WorkspaceSession 停止 mission 时，调度器要负责 fan-out cancel。

### 15.1 mission cancel

行为：

- 标记 mission 为 `cancelled`
- 对所有活跃 WorkerSession 调用现有 `turn/interrupt`
- 对其远程 exec 复用现有 cancel 逻辑
- 清空尚未激活的 ready / queued task

### 15.2 单任务 cancel

第一阶段可以不开放 UI，但调度器内部要支持。

用途：

- 某个 host 卡住
- 审批被拒绝
- PlannerSession 需要重新调整某个 host 的任务

## 16. 持久化设计

第一阶段建议不要把 orchestrator 状态硬塞进现有 `store.persistentState`，避免一次性改动太大。

建议增加独立持久化文件：

```text
<state-dir>/orchestrator/orchestrator.json
```

保存内容：

- missions 当前态
- HostWorker 当前态
- task 当前态
- workspace lease
- 有界 relay events

而这些内容不重复保存：

- child session 全量 cards
- child session transcript
- 原始 approval 对象

因为这些仍由现有 `store` 负责。

### 16.1 有界事件保留

建议不要把 `RelayEvent` 无限制堆进 `Mission.Events`。

第一阶段就应至少做到：

- mission 内事件数上限
- 超限后只保留最近窗口

必要时再把详细时间线写成 JSONL。

### 16.2 重启恢复

server 重启后恢复逻辑：

1. 读取 orchestrator 状态
2. 恢复 mission -> planner / worker 关系
3. 对仍 active 的 WorkerSession 重新挂 collector
4. 对已无 thread / session 的任务标记 `failed` 或 `needs_reconcile`
5. 重新计算预算占用，不直接无脑恢复全部活跃 turn

## 17. 与现有代码的集成点

第一阶段建议只改这些地方。

### 17.1 `internal/server/server.go`

需要新增：

- `App.orchestrator *orchestrator.Manager`
- 在 `New(...)` 时初始化 orchestrator
- 在 mission 创建时创建 PlannerSession
- 在 `broadcastSnapshot(sessionID)` 后调用 `orchestrator.OnSnapshot`
- 在 turn / approval / remote exec 关键 hook 里调用 orchestrator
- 在协作工作台会话 stop 时调用 `orchestrator.CancelMission` 或 `CancelByWorkspaceSession`

### 17.2 `internal/server/dynamic_tools.go`

需要新增：

- `orchestrator_dispatch_tasks`

这个工具应该挂给 PlannerSession，而不是普通 WorkspaceSession。

### 17.3 `internal/server/handleApprovalDecision`

需要在审批决策成功后通知 orchestrator，便于同步镜像状态和预算释放。

### 17.4 `internal/orchestrator/*`

新增调度器实现。

### 17.5 `web`

第一阶段 Web 不需要先做一套新的 mission API。

只要：

- WorkspaceSession 能渲染新增卡片类型
- 协作工作台能从 WorkspaceSession / mission 读模型里取摘要
- 计划详情、派发详情、Worker 详情优先走弹窗 / 抽屉
- Worker 详情默认只读，不在抽屉里继续直接发消息
- Worker 详情允许跳转到 SingleHostSession，并携带 host 标识做主机选择桥接

就足够开始联调。

## 18. 模块拆分

第一阶段先实现这几个文件即可：

```text
internal/orchestrator/
  ├── manager.go        # mission 管理入口
  ├── state.go          # Mission / TaskRun / HostWorker / WorkspaceLease
  ├── dispatcher.go     # 创建 WorkerSession、启动子 turn、串行调度同主机任务
  ├── collector.go      # 监听 worker 状态，抽取进度 / 审批 / 完成结果
  ├── projector.go      # 把 relay event 投影到 WorkspaceSession 卡片和详情读模型
  ├── prompt.go         # Planner / Worker prompt 包装
  ├── workspace.go      # workspace lease 分配与准备
  └── store.go          # mission 状态持久化
```

## 19. 第一阶段推荐实现顺序

### Step 1: mission / worker / workspace 状态

先落：

- `Mission`
- `TaskRun`
- `HostWorker`
- `WorkspaceLease`
- `orchestrator store`

### Step 2: PlannerSession

落：

- mission 创建时拉起 PlannerSession
- planner workspace 分配
- planner 专用工具挂载

### Step 3: 派发工具

落：

- `orchestrator_dispatch_tasks`
- `Manager.Dispatch`

做到 PlannerSession 能把结构化任务交给 Go 层。

### Step 4: WorkerSession 启动

落：

- HostWorker 创建 / 复用
- worker workspace 准备
- `ensureThread`
- `requestTurn`

做到任务真能被派发出去。

### Step 5: 预算与背压

落：

- `GlobalActiveBudget`
- `MissionActiveBudget`
- active worker 释放 / 下一批激活

做到大规模 host 下不会把 `app-server` 瞬间打满。

### Step 6: collector

落：

- server hook -> orchestrator
- `broadcastSnapshot -> orchestrator.OnSnapshot`
- 进度 / 审批 / 完成事件抽取

做到结果真能收上来。

### Step 7: WorkspaceSession 投影

落：

- `MissionCard`
- `WorkerApprovalCard`
- `WorkerCompletionCard`
- `PlanSummaryView`
- `PlanDetailView`
- `DispatchSummaryView`
- `DispatchHostDetailView`
- `WorkerReadonlyDetailView`

做到 UI 可见。

### Step 8: stop / cancel

落：

- mission cancel
- worker turn interrupt
- active exec cancel

## 20. 测试计划

第一阶段至少要有下面这些测试。

### 20.1 dispatcher 单测

- 派发单 host task 时会创建 HostWorker
- 同 host 多 task 时会复用同一个 HostWorker
- 离线 host 会拒绝派发
- worker 空闲后能正确释放预算

### 20.2 collector 单测

- 收到 worker snapshot 的新 card 时能生成 relay event
- 重复 snapshot 不会重复生成 event
- approval pending -> resolved 能正确转换
- server hook 事件和 snapshot fallback 不会重复记账

### 20.3 projector 单测

- dispatch event 会生成 mission card / worker progress card
- approval event 会生成 worker approval card
- completion event 会生成 worker completion card
- 同一 host 的 progress card 会被 Upsert 而不是刷出多张

### 20.4 预算测试

- mission 派发 1000 台 host 时，只激活前 N 个 worker
- 某个 worker 完成后，下一个 queued worker 会被激活
- 全局预算耗尽时，不会继续调用新的 `turn/start`
- `thread/start` / `turn/start` 的速率限制生效时，不会在短时间内打爆 Codex client pending request

### 20.5 集成测试

建议模拟：

1. WorkspaceSession 发起 mission
2. PlannerSession 派发 32 台 host
3. `MissionActiveBudget=4`
4. 其中 1 台进入审批
5. 审批通过后完成
6. 其他 host 分批推进
7. WorkspaceSession 最终能看到完整 mission / approval / completion 卡片链

### 20.6 前台投影交互验收

- 页面一 SingleHostSession 不受 orchestrator 影响，仍可直接与指定主机会话交互
- 页面二 WorkspaceSession 输入需求后，前台显示主 Agent 回复，但 MCP / skills / 经验包调用来自 PlannerSession
- 点击计划摘要时，优先打开弹窗 / 抽屉，而不是在聊天流内展开
- 计划详情默认展示结构化过程，且能继续点开原始 Planner 轨迹
- 点击派发摘要时，详情默认按 host 展开，能看见每台主机被派发的任务
- 点击某台主机详情时，默认只读，不允许在抽屉里直接发消息
- 从子任务详情可以跳到 SingleHostSession，并自动切换到对应主机

## 21. 后续扩展

这份设计故意把下一阶段留了口子。

第一阶段稳定后，再加：

### 21.1 planner 自动重规划

- 子任务完成后自动唤醒 PlannerSession
- PlannerSession 自动做收口总结或重规划

### 21.2 dag.go

新增节点依赖和批次推进：

- `ready`
- `blocked`
- `running`
- `completed`

### 21.3 runner

当某类任务已经沉淀成确定性步骤时：

- 不必每次都走 WorkerSession
- 可以直接走 playbook / pipeline

这也是后期处理“上千台 host 同类操作”时更合理的路径。

## 22. 结论

第一阶段不需要做一个“大而全 orchestrator 平台”。

只需要先补上这条最关键的闭环：

**WorkspaceSession 接收用户需求，`ai-server` 创建独立 PlannerSession 做规划，调度器按 host 创建逻辑 Worker，受预算控制地激活少量 WorkerSession，Worker 继续复用现有远程工具执行，调度器再把进度、审批和结果以前台投影的方式可靠地收回来。**

这样做的好处是：

- 和现有 `ai-server / host-agent / web` 完全兼容
- 改动范围可控
- 不会重做 transport / approval / session / exec
- 从第一阶段就把 PlannerSession 独立和 workspace 模型定下来
- 后期主机规模到几百上千台时，也不会因为“mission 里 host 多”就直接把 Codex `app-server` 压崩

这就是当前最值得先落地的 P0。
