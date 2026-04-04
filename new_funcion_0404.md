# 2026-04-04 新功能设计方案

本文面向当前 `aiops-codex` 代码库，整理 9 个新增特性的设计方案。目标不是只给概念，而是尽量落到当前已经存在的 chat、workspace、审批、MCP、skill、runner、agent profile 体系中，形成后续可以直接拆任务实施的方案。

---

## 0. 默认假设与边界

本次设计默认采用下面 5 个假设：

1. “审核管理页面”里的“审核”主要指当前系统里的 `approval request / approval decision / approval grant`，不是第三方工单系统审批。
2. “历史工作台”已经以 `workspace / 主 Agent 会话` 为核心，这次新功能也继续以主 Agent workspace 作为协作对象。
3. “UI 卡片管理”指当前 chat / workspace / MCP surface 体系里的结构化 UI 卡片，而不是任意页面组件。
4. “针对主机 agent 的功能命令进行限制，具体常用信息接口”里，“聚体”按“具体”理解：即减少自由 shell，优先暴露结构化只读接口。
5. Coroot 会以独立服务部署，AIOps Code 通过反向代理、只读 token 或内部网络访问方式接入。

如果后续你希望其中任意一条改掉，比如：

- UI 卡片管理还要覆盖普通后台页面卡片
- Coroot 不是嵌入，而是只拉取 API 数据
- 审核授权不按主机级，而要按主机组 / 环境级

则需要同步调整下面的资源模型。

---

## 1. 当前代码库里已经可复用的基础

### 1.1 审批与审计

当前系统已经有这两类能力：

- `internal/server/server.go`
  - 已有 `auditApprovalRequested()`、`auditApprovalLifecycleEvent()`，并会记录 `sessionId / threadId / turnId / hostId / command / cwd / operator / approvalDecision`。
- `internal/model/types.go`
  - 已有 `ApprovalRequest`、`ApprovalGrant`。
- `internal/store/memory.go`
  - 已支持 `AddApproval()`、`ResolveApproval()`、`AddApprovalGrant()`。

但当前也有一个关键限制：

- 现在的 `ApprovalGrant` 是 session 级、内存级的，不能满足“按主机查看已授权命令、下次直接通过”的长期管理诉求。

这意味着：

`审核管理页面可以复用现有审批和 audit 数据源，但授权列表必须升级为独立持久化资源。`

### 1.2 MCP / Skill / Script 已有 CRUD

Runner 控制面已经有可复用的资源模型和 API：

- `pkg/runner/server/api/skill_handler.go`
- `pkg/runner/server/api/mcp_handler.go`
- `pkg/runner/server/api/script_handler.go`
- `pkg/runner/server/store/skillstore/model.go`
- `pkg/runner/server/store/mcpstore/model.go`
- `pkg/runner/scriptstore/model.go`

说明：

- Skill、MCP、Script 已经有列表、详情、创建、修改、删除的最小管理能力。
- 新功能不需要从零做资源层，更多是扩字段、扩页面、扩关联关系。

### 1.3 Host Agent 权限模型已经存在

当前主机 agent 并不是完全裸奔：

- `internal/model/types.go`
  - 已有 `AgentCommandPermissions`、`AgentCapabilityPermissions`
- `internal/server/agent_profile_policy.go`
  - 已有 `evaluateCommandPolicyForHost()`、`ensureWritableRootsForHost()`

当前已经能按这些维度控：

- command execution 是否启用
- `allowSudo`
- `allowShellWrapper`
- 默认审批模式
- category policy
- writable roots
- capability 开关，如 terminal / fileChange / webSearch / approval / multiAgent 等

这意味着：

`第 4 项“限制 host agent 功能命令”不需要推翻重来，而应该从“profile 级策略”升级为“产品化策略中心 + 结构化接口优先”。`

### 1.4 Chat / Workspace 侧的 MCP UI surface 已经搭好基础协议

前面这轮 chat 改造已经落下来了：

- `web/src/lib/mcpUiCardModel.js`
- `web/src/lib/mcpUiPayloadAdapter.js`
- `web/src/lib/mcpBundleResolver.js`
- `web/src/components/mcp/McpUiCardHost.vue`
- `web/src/components/mcp/McpBundleHost.vue`

当前已支持的核心类型：

- `readonly_summary`
- `readonly_chart`
- `action_panel`
- `form_panel`
- `monitor_bundle`
- `remediation_bundle`

说明：

- “UI 卡片管理”
- “Coroot 监控嵌入”
- “监控里嵌 AI”
- “自动生成 skills 和 UI 卡片”

这 4 项都应该基于这套协议继续扩，而不是再平行造一套卡片体系。

### 1.5 Runner 体系已经有脚本、工作流、环境、Agent、Run 基础设施

可复用模块：

- `pkg/runner/workflow/model.go`
- `pkg/runner/server/api/dashboard_handler.go`
- `pkg/runner/server/api/environment_handler.go`
- `pkg/runner/server/api/run_handler.go`
- `pkg/runner/server/service/*`

说明：

- “runner 脚本配置管理”
- “可以演练的沙盒环境”
- “自动化生成功能”

都可以直接建立在 runner control plane 上，不需要再去别处建一套执行系统。

---

## 2. 总体设计原则

这 9 个特性不要各自独立长，而应统一挂到 4 类产品能力下：

1. `Governance`
   - 审核管理
   - 授权列表
   - 主机 agent 限制策略
2. `Catalog`
   - Skill 管理
   - MCP 管理
   - UI 卡片管理
   - Runner 脚本配置管理
3. `Observability`
   - Coroot 接入
   - 监控卡片与聚合 bundle
   - 监控内 AI
4. `Automation / Lab`
   - 沙盒环境
   - 自动生成 skills / UI 卡片

统一原则如下：

- 所有能力都要能被 chat / workspace / runner control plane 共用。
- 先建资源模型，再建页面。
- 任何“可直接执行命令”的能力，都必须经过统一审批 / 审计 / 授权策略。
- 所有图表与控制面板都先走结构化 UI card，不允许直接堆 raw JSON。
- 所有生成式能力都必须输出“草稿资源”，而不是直接静默发布上线。

---

## 3. 特性一：审核管理页面

### 3.1 目标

解决两个问题：

1. 清楚记录：
   - 用户在哪个工作台或会话
   - 在什么时间
   - 对哪台主机
   - 审核了什么命令或文件变更
   - 最终是通过、拒绝、自动放行还是策略自动通过
2. 管理“主机级授权列表”：
   - 选定某台主机后，看到当前哪些命令已授权
   - 后续相同命令可直接通过
   - 支持撤销授权

### 3.2 当前现状

当前可复用：

- 审批申请、审批决策、auto-approve 已有审计日志
- session 里已有 `ApprovalRequest`
- session 内已有 `ApprovalGrant`

当前不足：

- 没有审批管理页面
- 没有可查询的结构化审核索引
- grant 不是 host 级持久化资源
- 无法直接回答“某条命令为什么被免审”

### 3.3 方案概述

新增两类独立资源：

1. `ApprovalAuditRecord`
   - 审核事件流水
2. `ApprovalGrantRecord`
   - 主机级授权白名单

二者关系：

- 审核流水记录“发生过什么”
- 授权记录表示“当前允许什么”

### 3.4 数据模型

#### 3.4.1 ApprovalAuditRecord

```json
{
  "id": "audit-approval-001",
  "event": "approval.requested | approval.decision | approval.auto_accepted",
  "sessionId": "session-xxx",
  "sessionKind": "single_host | workspace",
  "threadId": "thread-xxx",
  "turnId": "turn-xxx",
  "workspaceSessionId": "workspace-xxx",
  "hostId": "web-01",
  "hostName": "web-01",
  "operator": "user@example.com",
  "approvalId": "approval-xxx",
  "approvalType": "command | file_change | remote_command | remote_file_change",
  "toolName": "commandExecution | execute_system_mutation",
  "command": "sudo nginx -s reload",
  "cwd": "/etc/nginx",
  "filePath": "/etc/nginx/nginx.conf",
  "decision": "accept | reject | decline | auto_accept",
  "status": "pending | accepted | rejected | accepted_for_session_auto | accepted_by_policy_auto",
  "grantMode": "none | session | host",
  "fingerprint": "command|web-01|/etc/nginx|sudo nginx -s reload",
  "startedAt": "2026-04-04T10:20:30Z",
  "endedAt": "2026-04-04T10:21:02Z",
  "createdAt": "2026-04-04T10:21:02Z",
  "meta": {}
}
```

#### 3.4.2 ApprovalGrantRecord

```json
{
  "id": "grant-host-web-01-001",
  "hostId": "web-01",
  "hostScope": "host",
  "grantType": "command | file_change",
  "fingerprint": "command|web-01|/etc/nginx|sudo nginx -s reload",
  "command": "sudo nginx -s reload",
  "cwd": "/etc/nginx",
  "grantRoot": "/etc/nginx",
  "createdFromApprovalId": "approval-xxx",
  "createdFromSessionId": "workspace-xxx",
  "createdBy": "user@example.com",
  "createdAt": "2026-04-04T10:21:02Z",
  "expiresAt": "",
  "status": "active | revoked | expired",
  "reason": "用户授予 host 级免审",
  "meta": {}
}
```

### 3.5 页面设计

新增页面：`审核管理`

页面结构：

1. 顶部统计区
   - 今日审批数
   - 待审批数
   - 自动通过数
   - 已授权命令数
2. 左侧筛选区
   - 时间范围
   - 会话类型：单机会话 / 协作工作台
   - 主机
   - 操作人
   - 决策结果
   - 工具类型
3. 主表：审核流水
   - 时间
   - 工作台 / 会话
   - 主机
   - 命令 / 文件
   - 审核人
   - 决策
   - 是否形成授权
4. 详情抽屉
   - 上下文消息
   - turn / thread
   - 命令、cwd、filePath
   - 审核前原因
   - 审核后结果
5. 授权列表 Tab
   - 按主机切换
   - 展示当前 host 级授权命令
   - 支持撤销 / 续期 / 临时禁用

### 3.6 接口设计

建议新增 API：

- `GET /api/v1/approval-audits`
- `GET /api/v1/approval-audits/{id}`
- `GET /api/v1/approval-grants`
- `POST /api/v1/approval-grants`
- `POST /api/v1/approval-grants/{id}/revoke`
- `POST /api/v1/approval-grants/{id}/disable`
- `POST /api/v1/approval-grants/{id}/enable`

### 3.7 后端实现建议

新增 store：

- `internal/store/approval_audit_store.go`
- `internal/store/approval_grant_store.go`

或者在 runner control plane 里新增：

- `pkg/runner/server/store/approvalstore/*`

推荐做法：

- 审计流水由现有 `auditApprovalLifecycleEvent()` 同时写 JSONL 和结构化 store
- 授权记录从“accept_session / host grant”路径显式创建
- host 切换不应清掉 host 级 grant
- session auto grant 与 host grant 要严格区分

### 3.8 风险与注意点

- host 级授权必须谨慎，建议默认支持 TTL
- 高风险命令如 `rm -rf`、`sudo su`、`iptables -F` 不应允许被长期 host grant
- host grant 应与 command fingerprint 强绑定，避免模糊命令扩权

### 3.9 实施优先级

优先级：`P0`

这是治理能力的基础，后续所有控制面板、Coroot 修复动作、runner 自动化都依赖它。

---

## 4. 特性二：UI 卡片管理

### 4.1 目标

让系统能明确回答 4 个问题：

1. 当前支持哪些 UI 卡片
2. 每种卡片的功能与作用是什么
3. 这些卡片如何触发
4. 如何新增和修改卡片定义

### 4.2 当前现状

当前已经有 renderer 和协议，但没有管理面：

- 已有 `McpUiCardHost`、`McpBundleHost`
- 已有具体卡片：
  - `McpSummaryCard`
  - `McpKpiStripCard`
  - `McpTimeseriesChartCard`
  - `McpStatusTableCard`
  - `McpControlPanelCard`
  - `McpActionFormCard`
  - `McpMonitorBundleCard`
  - `McpRemediationBundleCard`
  - `GenericMcpActionCard`

当前不足：

- 没有“卡片定义中心”
- 卡片触发关系散落在 formatter / bundle resolver / preset registry 中
- 业务人员无法通过后台增改卡片元数据

### 4.3 方案概述

新增资源：`UICardDefinition`

把卡片定义分成两层：

1. `Renderer Layer`
   - 前端真实组件
2. `Definition Layer`
   - 元数据、触发条件、展示位置、schema、作用说明

### 4.4 数据模型

```json
{
  "id": "mcp-timeseries-chart",
  "name": "时序图卡片",
  "kind": "readonly_chart",
  "renderer": "McpTimeseriesChartCard",
  "bundleSupport": ["monitor_bundle"],
  "placementDefaults": ["inline_final", "drawer"],
  "summary": "展示指标时间序列趋势",
  "capabilities": ["view", "refresh", "open-detail"],
  "triggerTypes": ["mcp_tool_result", "bundle_section", "preset"],
  "inputSchema": {},
  "actionSchema": {},
  "editableFields": ["title", "summary", "scope", "freshness", "visual", "actions"],
  "status": "active",
  "version": 1,
  "createdAt": "",
  "updatedAt": ""
}
```

### 4.5 页面设计

新增页面：`UI 卡片管理`

页面结构：

1. 卡片类型总览
   - 只读卡
   - 图表卡
   - 操作卡
   - 表单卡
   - bundle 卡
2. 列表页
   - 名称
   - kind
   - renderer
   - 默认 placement
   - 触发方式
   - 当前状态
3. 详情页
   - 功能描述
   - 作用范围
   - 输入 schema
   - 可执行 action
   - 适用 bundle
   - 示例 payload
4. 编辑器
   - 元数据表单
   - JSON schema 编辑
   - 预览区域
5. 触发调试器
   - 选择工具输出 / bundle preset / mock payload
   - 实时预览卡片

### 4.6 当前支持的卡片分类

建议在页面里明确展示：

#### 4.6.1 只读信息卡

- `McpSummaryCard`
  - 用途：文字摘要、说明、告警结论
  - 触发：只读 MCP 结果、bundle 概述区
- `McpKpiStripCard`
  - 用途：核心指标横向摘要
  - 触发：`readonly_summary` 且带 `kpis`
- `McpTimeseriesChartCard`
  - 用途：时序趋势
  - 触发：`readonly_chart` 且 `visual.kind != table`
- `McpStatusTableCard`
  - 用途：状态表、实例列表、错误分布
  - 触发：`readonly_chart` 且 `visual.kind = table/status_table`

#### 4.6.2 操作卡

- `McpControlPanelCard`
  - 用途：执行可审批操作
  - 触发：`action_panel`
- `McpActionFormCard`
  - 用途：带参数的操作表单
  - 触发：`form_panel`
- `GenericMcpActionCard`
  - 用途：未知 action 的兜底
  - 触发：无专用 renderer 的 mutation/read action

#### 4.6.3 聚合卡

- `McpMonitorBundleCard`
  - 用途：把某个中间件 / 服务的概览、趋势、告警、变更、依赖聚在一起
- `McpRemediationBundleCard`
  - 用途：根因、建议动作、控制面板、验证面板聚合

### 4.7 支持新增与修改的边界

建议第一版支持：

- 修改定义元数据
- 新增 preset / trigger mapping
- 调整 placement default
- 调整 bundle section 归属
- 调整 schema 和示例

第一版不建议支持：

- 直接在页面里可视化拖拽生成复杂 Vue renderer

建议做法：

- 后台新增的是 `definition`
- 真正新增前端 renderer 仍走代码 PR
- 但可以通过 “generic schema + existing renderer” 复用出大量卡片

### 4.8 接口设计

- `GET /api/v1/ui-cards`
- `GET /api/v1/ui-cards/{id}`
- `POST /api/v1/ui-cards`
- `PUT /api/v1/ui-cards/{id}`
- `DELETE /api/v1/ui-cards/{id}`
- `POST /api/v1/ui-cards/{id}/preview`

### 4.9 实施优先级

优先级：`P1`

它是 Coroot 卡片、自动生成卡片、监控内 AI 的前置目录能力。

---

## 5. 特性三：接入 Coroot 监控，并生成可直接查询的 skills / MCP

### 5.1 目标

把 Coroot 从“外部监控系统”变成当前产品里的第一等监控源，做到：

1. 页面可嵌入 Coroot 监控
2. chat / workspace 可直接查询 Coroot 数据
3. 可把 Coroot 数据聚合成监控 bundle
4. 可生成 skills 或 MCP tools 供 AI 直接调用

### 5.2 方案拆成三层

#### 第一层：Coroot Embed Layer

用于“把页面嵌进来”。

#### 第二层：Coroot Data Layer

用于“把 Coroot 数据结构化取出来”。

#### 第三层：Coroot AI Layer

用于“让 AI 能问、能解释、能触发操作”。

### 5.3 接入架构

建议新增模块：

```text
internal/coroot/
  client.go
  auth.go
  services.go
  incidents.go
  metrics.go
  topology.go
  rca.go
```

配套新增：

- `internal/server/coroot_proxy.go`
- `internal/server/coroot_tools.go`

### 5.4 页面嵌入方案

优先建议：`服务端反向代理 + iframe/embed view`

原因：

- 可以统一处理认证
- 可以避免浏览器跨域和 cookie 问题
- 可以做只读模式限制

建议新增页面：

- `监控总览`
- `Coroot 服务视图`
- `Coroot 埋点 / 告警 / RCA 详情`

同时支持在 chat/workspace 内通过 drawer/modal 打开：

- `CorootEmbedPanel`

### 5.5 Chat / Workspace 内的映射

当用户输入：

- “我想知道 nginx 中间件的情况”
- “看下 redis 最近有没有异常”
- “为什么 web-01 负载这么高”

系统应优先调用：

1. Coroot 查询 API
2. bundle resolver
3. 输出 `monitor_bundle`

而不是只吐 markdown 指标文本。

### 5.6 Skills 与 MCP 方案

建议两条都做，但分工不同：

#### Skills

适合：

- 固定场景 prompt 封装
- 用户语言理解增强
- 把 Coroot 查询步骤固化成操作套路

建议内置 skill：

- `coroot-service-overview`
- `coroot-incident-summary`
- `coroot-rca-explain`
- `coroot-verify-recovery`

#### MCP

适合：

- 真正结构化查询
- 工具参数校验
- 被主 Agent / workspace / card generator 统一调用

建议 MCP tool：

- `coroot.list_services`
- `coroot.service_overview`
- `coroot.service_metrics`
- `coroot.service_alerts`
- `coroot.topology`
- `coroot.incident_timeline`
- `coroot.rca_report`

### 5.7 Coroot 与 UI 卡片的映射

建议默认映射：

- `service_overview` -> `readonly_summary`
- `service_metrics` -> `readonly_chart`
- `service_alerts` -> `status_table`
- `topology` -> `monitor_bundle.dependencies`
- `rca_report` -> `remediation_bundle.root_cause`

### 5.8 风险与注意点

- Coroot 页面嵌入要处理认证和 CSP
- 不建议一开始直接暴露 Coroot 全部原始页面导航
- 建议优先封装“服务视图”和“RCA 视图”

### 5.9 实施优先级

优先级：`P0-P1`

这是监控产品化的关键入口。

---

## 6. 特性四：限制 Host Agent 功能命令，并提供具体常用信息接口

### 6.1 目标

从“让 host agent 拿 shell 自由发挥”升级为：

- 默认优先走结构化只读接口
- 原始命令执行更严格
- mutation 要么审批，要么走 runner / control panel

### 6.2 当前现状

现有 profile 权限能控制很多事，但还偏底层：

- 面向 agent profile
- 面向 capability 和 command category
- 对业务使用者不够直观

### 6.3 方案概述

新增一层：`Host Agent Capability Gateway`

把 host agent 能力分成三类：

1. `Structured Read API`
   - 常用只读信息接口
2. `Controlled Mutation API`
   - 明确的可审批动作
3. `Raw Shell Escape Hatch`
   - 最后兜底，但默认更严

### 6.4 建议的常用信息接口目录

建议第一批做成结构化工具：

- `host.summary`
  - OS、uptime、load、cpu、mem、disk
- `host.process.top`
  - Top N 进程
- `host.service.status`
  - systemd 服务状态
- `host.journal.tail`
  - 服务日志 tail
- `host.file.exists`
  - 文件存在与元数据
- `host.file.read`
  - 文件读取
- `host.file.search`
  - 关键字搜索
- `host.network.listeners`
  - 监听端口
- `host.network.connections`
  - 当前连接
- `host.package.version`
  - 软件版本
- `host.nginx.status`
  - Nginx 状态与配置摘要
- `host.mysql.summary`
  - MySQL 基础状态
- `host.redis.summary`
  - Redis 基础状态
- `host.jvm.summary`
  - JVM heap / thread / gc 摘要

### 6.5 限制策略

建议默认策略：

- 只读信息接口：默认允许
- 原始 `remote_command`：
  - 只读命令可审批后执行
  - mutation 命令默认更严
- `sudo`：
  - 默认禁用，必须按 host profile 显式放开
- shell wrapper：
  - 默认仅在主机组白名单中允许

### 6.6 产品侧表现

在 agent profile 页面和未来的“主机策略中心”里显示：

- 允许的结构化接口
- 禁止的 raw command 类别
- 当前生效策略来源：主 Agent / host-agent-default / host override

### 6.7 后端实现建议

在 `dynamic_tools.go` 现有远程能力基础上继续抽象：

- `query_host_summary`
- `query_service_status`
- `query_top_processes`
- `query_recent_logs`
- `query_network_state`

让 chat 优先命中这些结构化接口，而不是直接 shell。

### 6.8 实施优先级

优先级：`P0`

这是监控、审核、控制面板、安全治理的基础。

---

## 7. 特性五：接入 MCP、Skills，并支持列表管理

### 7.1 目标

把当前已有的 MCP / skill CRUD，从 runner 控制平面的“资源管理”升级成平台级“可启用、可测试、可绑定、可治理”的目录。

### 7.2 页面结构建议

新增统一入口：`能力中心`

其中包含 3 个 tab：

1. `Skills`
2. `MCP Servers`
3. `Bindings`

### 7.3 Skills 管理增强

在现有基础上补充字段：

- 分类
- 来源
- 版本
- 适用对象：主 Agent / host-agent / workspace
- 状态：草稿 / 启用 / 停用
- 依赖项：MCP / script / environment
- 最近命中次数

支持操作：

- 新建
- 编辑
- 预览
- 启停
- 绑定到 agent profile
- 生成测试 prompt

### 7.4 MCP 管理增强

在现有基础上补充字段：

- 分类：observability / files / logs / kubernetes / database / internal
- 认证方式
- 作用域
- 风险级别
- 默认权限：readonly / readwrite
- 需要显式审批
- 支持工具数量
- 最后探活时间

支持操作：

- 创建 / 修改 / 删除
- 启停
- 查看 tools
- 工具调用测试
- 绑定到 agent profile

### 7.5 Binding 管理

新增“能力绑定关系”：

- skill -> agent profile
- mcp -> agent profile
- skill -> workspace preset
- mcp -> workspace preset
- mcp / skill -> ui card trigger

### 7.6 实施优先级

优先级：`P1`

它是能力治理和自动生成能力的基础目录。

---

## 8. 特性六：在监控内嵌入 AI 功能

### 8.1 目标

用户在监控页面里，不需要跳回 chat 才能问问题，而是可以在监控上下文内直接：

- 让 AI 解释图表
- 让 AI 做异常归因
- 让 AI 给出下一步建议
- 直接生成工作台
- 直接生成 runner 执行草稿

### 8.2 方案概述

在监控页内新增：`AI 分析抽屉`

输入上下文自动包含：

- 当前服务 / 主机 / 集群
- 时间范围
- 当前图表 / bundle
- 告警状态
- 选中的 Coroot 面板

### 8.3 交互模式

建议提供 4 个固定动作：

1. `解释当前面板`
2. `定位异常原因`
3. `生成修复工作台`
4. `生成执行草稿`

以及一个自由输入框：

- “为什么 web-01 的 nginx QPS 掉了”

### 8.4 输出形态

AI 输出不能只给文本，应支持：

- 文字结论
- 监控 bundle 更新
- remediation bundle
- 新建 workspace
- 新建 runner workflow 草稿

### 8.5 技术实现

新增 `MonitorContext`：

```json
{
  "source": "coroot",
  "resourceType": "service",
  "resourceId": "nginx",
  "hostIds": ["web-01", "web-02"],
  "timeRange": "last_30m",
  "panels": [],
  "alerts": [],
  "topology": {}
}
```

将这个 context 注入：

- 主 Agent prompt
- workspace message metadata
- MCP bundle resolver

### 8.6 风险

- 监控内 AI 容易变成另一个独立 chat，必须控制范围
- 它应是“监控上下文增强器”，不是第二个主页聊天

### 8.7 实施优先级

优先级：`P1`

建议在 Coroot 接入和 monitor bundle 成型后再做。

---

## 9. 特性七：可以演练的沙盒环境

### 9.1 目标

为以下场景提供可反复演练的环境：

- 告警响应演练
- 常见中间件故障演练
- runner workflow 验证
- 新 skill / 新 UI 卡片 / 新控制面板联调

### 9.2 方案概述

新增资源：`LabEnvironment`

把沙盒环境做成可管理对象，而不是“临时找几台测试机”。

### 9.3 资源模型

```json
{
  "id": "lab-nginx-degrade",
  "name": "Nginx 性能下降演练环境",
  "type": "container | vm | mock",
  "status": "ready | running | dirty | resetting",
  "topology": {
    "hosts": ["lab-web-01", "lab-web-02", "lab-db-01"]
  },
  "scenarios": ["nginx-reload-failure", "high-load", "disk-full"],
  "runnerProfiles": ["lab-safe"],
  "corootConnected": true,
  "resetStrategy": "recreate",
  "createdAt": "",
  "updatedAt": ""
}
```

### 9.4 页面设计

新增：`沙盒实验室`

页面支持：

- 环境列表
- 场景模板
- 一键启动
- 一键注入故障
- 一键重置
- 绑定 runner profile / skill set / mcp preset
- 查看演练记录

### 9.5 三个阶段的落地建议

#### v1

Mock / fixture 级环境

- 前端可以跑 bundle、control panel、AI 分析流
- 不执行真实主机命令

#### v2

Container-based lab

- docker compose 或轻量 VM
- 真正跑 host agent / runner / coroot

#### v3

隔离远端 lab

- 独立网络
- 真正演练审批、修复、回滚

### 9.6 实施优先级

优先级：`P2`

先把治理和监控主链路打通，再做演练更合理。

---

## 10. 特性八：支持 Runner 脚本配置管理

### 10.1 目标

把当前 script 管理从“存脚本文本”升级为“脚本 + 参数 schema + 环境绑定 + 执行配置”的完整管理。

### 10.2 当前现状

当前已支持：

- Script CRUD
- Script render
- Environment 变量管理

当前不足：

- 没有脚本参数 schema
- 没有脚本配置实例
- 没有脚本与环境 / 主机组 / 审批策略的绑定

### 10.3 方案概述

新增资源：`ScriptConfigProfile`

用于描述：

- 这个脚本怎么填参数
- 默认值是什么
- 关联哪个 environment
- 需要什么审批
- 用在什么 inventory / runner profile 上

### 10.4 数据模型

```json
{
  "id": "reload-nginx-safe",
  "scriptName": "nginx-reload",
  "description": "生产环境安全 reload 配置",
  "argSchema": {
    "serviceName": { "type": "string", "required": true },
    "graceful": { "type": "boolean", "default": true }
  },
  "defaults": {
    "serviceName": "nginx",
    "graceful": true
  },
  "environmentRef": "prod-shared",
  "inventoryPreset": "web-cluster",
  "approvalPolicy": "required",
  "runnerProfile": "safe-prod",
  "status": "active",
  "createdAt": "",
  "updatedAt": ""
}
```

### 10.5 页面设计

新增：`脚本配置管理`

页面包括：

- 左侧脚本列表
- 中间配置实例列表
- 右侧配置编辑器与 dry-run 预览

支持：

- 参数 schema 编辑
- 默认值编辑
- 环境变量绑定
- dry run
- 审批策略绑定
- 生成 workflow step 草稿

### 10.6 与 UI 卡片 / AI 的关系

后续控制面板卡点击时，不直接拼 command，而是引用：

- `script + config profile`

这样能保证：

- 可复用
- 可审计
- 可生成
- 可回滚

### 10.7 实施优先级

优先级：`P1`

这是控制面板、runner 自动化、自动生成系统的重要中间层。

---

## 11. 特性九：设计一套自动生成 Skills 和 UI 卡片的系统

### 11.1 目标

让系统能从已有资源自动产出草稿：

- 从 MCP tool 生成 skill 草稿
- 从 runner script / config 生成 action card 草稿
- 从 Coroot 查询结果生成 monitor bundle preset
- 从审计与高频使用记录生成推荐卡片

### 11.2 核心原则

自动生成的对象默认都是：

- `draft`
- `待审核`
- `可预览`

绝不能自动上线生效。

### 11.3 架构概述

新增模块：`Generator Service`

输入：

- MCP tool schema
- Skill triggers
- Script metadata
- Script config profiles
- Coroot dashboard / query schema
- 审计和 usage analytics

输出：

- Skill draft
- UI card definition draft
- Bundle preset draft

### 11.4 生成链路

#### 11.4.1 从 MCP 生成 Skill

输入：

- MCP tool 名称、描述、参数 schema

输出：

- 推荐 skill 名
- 触发词
- prompt content
- 适配 agent 范围

#### 11.4.2 从 Script / Runner Config 生成 UI 卡片

输入：

- script name
- arg schema
- approval policy
- inventory preset

输出：

- `action_panel` 或 `form_panel`
- 默认标题、参数表单、审批说明

#### 11.4.3 从 Coroot / 监控面板生成 Bundle

输入：

- 服务类型
- query schema
- topology / alerts / metrics

输出：

- `monitor_bundle preset`
- `remediation_bundle preset`

### 11.5 页面设计

新增：`生成工坊`

功能区：

1. 选择来源
   - MCP
   - Skill
   - Script
   - Coroot
2. 生成目标
   - Skill draft
   - UI card draft
   - Bundle preset
3. 预览
4. 校验
5. 发布为草稿

### 11.6 校验与发布流程

建议固定 4 步：

1. `Generate`
2. `Lint`
3. `Preview`
4. `Publish Draft`

其中 lint 要检查：

- schema 合法性
- placement 是否合理
- action 是否声明审批路径
- 是否缺失 scope / freshness / source

### 11.7 实施优先级

优先级：`P2`

必须等 MCP、skill、UI 卡片、script config 都先具备结构化目录后再做。

---

## 12. 这 9 个特性的依赖关系

建议按下面顺序实施：

### Phase 1：治理底座

1. 审核管理页面
2. Host Agent 限制与结构化常用信息接口

### Phase 2：目录底座

3. MCP / Skill 列表管理增强
4. UI 卡片管理
5. Runner 脚本配置管理

### Phase 3：监控主链路

6. Coroot 接入
7. 监控内嵌 AI

### Phase 4：自动化与演练

8. 沙盒环境
9. 自动生成 Skills / UI 卡片系统

---

## 13. 建议新增的页面与模块清单

### 13.1 前端页面

- `审核管理`
- `UI 卡片管理`
- `能力中心`
  - Skills
  - MCP
  - Bindings
- `监控总览 / Coroot`
- `沙盒实验室`
- `脚本配置管理`
- `生成工坊`

### 13.2 后端资源

- `approval_audits`
- `approval_grants`
- `ui_card_definitions`
- `ui_bundle_presets`
- `script_config_profiles`
- `lab_environments`
- `generator_jobs`

### 13.3 关键模块

- `internal/coroot/*`
- `approval grant store`
- `ui card store`
- `script config store`
- `generator service`
- `monitor context service`

---

## 14. 风险与边界提醒

### 14.1 最大风险不是技术，而是资源边界不清

最容易失控的点有 4 个：

1. 把 UI 卡片管理做成“任意组件平台”
2. 把监控内 AI 做成第二个首页聊天
3. 把 host grant 做成过宽的长期免审
4. 把自动生成系统做成直接上线系统

### 14.2 必须坚持的边界

- 卡片管理先管“定义”，不管“通用低代码组件平台”
- 监控内 AI 只围绕当前监控上下文
- host grant 以精确 fingerprint 为核心
- 自动生成只发布草稿，不直接启用

---

## 15. 最终建议

如果按投入产出比排序，最值得先做的是：

1. `审核管理页面 + host 级授权列表`
   - 这是治理闭环的基础
2. `Host Agent 限制 + 结构化常用信息接口`
   - 这是安全和产品手感的基础
3. `Coroot 接入 + monitor bundle`
   - 这是监控产品化的核心
4. `Runner 脚本配置管理`
   - 这是控制面板和自动化执行的桥梁
5. `UI 卡片管理`
   - 这是后续扩展能力的目录中心

从产品价值看，我建议优先级排序如下：

`审核治理 > Host Agent 能力收口 > Coroot 监控接入 > 脚本配置管理 > 卡片管理 > 能力中心 > 监控内 AI > 沙盒 > 自动生成系统`

---

## 16. 需要你后续拍板的 4 个产品决策

虽然这次我先按合理假设写完了方案，但真正开做前，建议你尽快拍板下面 4 个问题：

1. host 级授权是否允许设置长期有效，还是必须带 TTL
2. Coroot 是优先“嵌页面”，还是优先“结构化 API + 卡片”
3. UI 卡片管理是否只覆盖 MCP / 监控卡片，还是未来也覆盖普通后台卡片
4. 沙盒环境第一版是否接受“mock / container lab”，还是必须直接上真实隔离主机

这 4 个点不影响本次文档输出，但会直接影响后续排期和工作量。
