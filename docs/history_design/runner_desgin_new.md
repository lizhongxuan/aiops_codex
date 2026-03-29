# Runner-First 智能运维执行引擎设计文档

## 1. 文档定位

本文给出一套面向 AIOps 平台的 `runner-first` 设计方案。

目标不是再造一个通用工作流玩具，而是建设一个真正适合智能运维场景的执行引擎：

- 以 `runner` 作为确定性、多主机、可批量分发的执行平面。
- 借鉴 Lobster 的优点，重点吸收“审批门、可恢复、确定性运行时、安全边界”。
- 与主 Agent、黑板 DAG、Coroot、Memory、经验包库形成闭环。
- 支持“1000 台安装 nginx”这类大规模任务，也支持“Runner 失败后回退到子 Agent 智能修复”。

本文同时说明如何使用该引擎，以及它和市面常见引擎工具相比的优点。

---

## 2. 背景与核心判断

你的目标架构不是单纯的自动化脚本执行器，而是一个完整的智能运维平台：

- 主 Agent 负责理解需求、拆解任务、生成黑板 DAG、调度执行。
- 子 Agent 负责单主机诊断和修复。
- Runner 负责确定性、多主机、批量、高效率任务。
- Coroot 负责监控、拓扑、RCA。
- Memory 负责经验沉淀与命中。

因此，Runner 的角色应当是：

> AIOps 平台中的确定性执行引擎，而不是孤立存在的 YAML 解释器。

这意味着设计重点不是“语法够不够炫”，而是下面 6 件事：

1. 能稳定执行。
2. 能批量分发。
3. 能审批和恢复。
4. 能观测和审计。
5. 能和 AI 调度层配合。
6. 能把经验沉淀为可重复执行的 Playbook。

---

## 3. 设计目标

### 3.1 核心目标

- 面向主机运维场景，提供多步骤、可编排、可批量分发的执行能力。
- 让运维经验可以从 Agent 的自由操作沉淀为可审核、可重放、可回滚的 Workflow / Playbook。
- 让主 Agent 可以根据任务类型，在 `Runner` 与 `子 Agent` 之间智能决策。
- 让失败的 Runner 任务具备可观察、可审批、可恢复、可回退到 Agent 的能力。

### 3.2 非目标

- 不把 Runner 设计成通用业务系统的 Saga / 事务编排平台。
- 不把 Runner 设计成只依赖 Kubernetes 的引擎。
- 不让 Runner 直接承担 AI 推理或复杂故障归因职责。
- 不要求第一版就覆盖所有生态集成。

---

## 4. 总体思路

### 4.1 Runner 与 Agent 的分工

推荐坚持这条主线：

- 确定性任务由 Runner 处理。
- 不确定性、诊断性、探索性任务由 Agent 处理。
- Runner 失败时，把上下文回传给主 Agent，再决定是否拉起子 Agent 接管。

一句话总结：

> Runner 负责“高效率地做确定的事”，Agent 负责“有弹性地处理不确定的事”。

### 4.2 Runner-First 的原因

以 Runner 为主，而不是以 Lobster 为主，原因很明确：

- 你的场景天然是多主机、多目标、多批次，不是单用户助手里的本地工具链。
- 你的核心诉求是批量执行和失败回退到 Agent，这更像运维执行平面，而不是单次 deterministic tool pipeline。
- 你需要 server、API、run center、events、metrics、agent registry、workflow store，这些都更符合 Runner 的演进方向。
- Lobster 的审批和可恢复非常优秀，但它更像“高质量工作流 runtime 思想来源”，不是你平台的主底座。

因此，建议采用：

> 架构主线以 Runner 为主，运行时语义借鉴 Lobster。

---

## 5. 设计原则

### 5.1 确定性优先

只要一个任务已经沉淀成 Playbook，就应尽量避免再交给 AI 自由发挥。

### 5.2 审批是运行时能力，不是 UI 附件

审批不能只是页面上点一下按钮。
审批必须进入状态机，影响执行流转，并具备 resume token、审计记录、超时和拒绝路径。

### 5.3 工作流是数据，执行是状态机

Workflow YAML / Script / Agent / Environment 都应是可存储、可版本化、可审计的资源。
运行时状态应明确为 FSM，而不是散落在日志里的隐含状态。

### 5.4 多主机是第一等公民

不是“先做单机，再勉强扩展到多机”，而是一开始就把 inventory、group、batch、rolling、max-failures 作为核心语义。

### 5.5 与 AI 解耦但可协同

Runner 不应依赖 AI 才能运行，但必须方便 AI 调用、观察、审批、失败回退、经验沉淀。

### 5.6 安全默认开启

审批、输出限制、凭据隔离、能力白名单、执行超时、可追踪审计应为默认能力。

---

## 6. 参考 Lobster 后应吸收的关键优点

Runner 主线不变，但必须吸收 Lobster 这 5 个核心优点：

### 6.1 审批门

对会产生副作用的步骤提供显式审批能力：

- step 进入 `waiting_approval`
- 返回 `resume_token`
- 用户 / 主 Agent / 策略引擎执行 `approve` 或 `reject`
- 原 run 从断点继续，而不是整条链重跑

### 6.2 可恢复

运行时必须支持：

- 断点恢复
- 审批后恢复
- 服务重启后恢复
- 失败后 retry from checkpoint

### 6.3 确定性 envelope

每个 step / host 执行结果都应有统一 envelope：

- status
- stdout
- stderr
- outputs
- changed
- diff
- error
- metadata

### 6.4 安全 runtime

运行时应强制：

- timeout
- max output bytes
- action allowlist
- credentials scoping
- 审批门控

### 6.5 显式数据流

变量传递不要长期依赖 `stdout` 解析导出，应当升级为 typed outputs：

- `steps.<id>.outputs.<key>`
- `steps.<id>.result.stdout`
- `steps.<id>.result.json`

---

## 7. 目标架构

### 7.1 逻辑分层

```text
Web / API / CLI
    |
    v
Runner Control Plane
    |- Workflow Service
    |- Script Service
    |- Agent Registry
    |- Run Service
    |- Approval Service
    |- Event Stream / Metrics / Audit
    |
    v
Runner Engine
    |- Planner-independent Workflow FSM
    |- Strategy Scheduler
    |- Dispatcher
    |- Module Runtime
    |- Checkpoint / Resume
    |
    +--> Runner Agent(s)
    |       |- Local / Remote Execution
    |       |- Capability Filter
    |       |- Output Streaming
    |       |- Waiting Token Cache
    |
    +--> Fallback to AIOps Agent Orchestrator
            |- 主 Agent
            |- Blackboard DAG
            |- 子 Agent
            |- Coroot
            |- Memory
```

### 7.2 组件说明

#### 7.2.1 Runner Control Plane

职责：

- 提供 Workflow / Script / Agent / Environment / Run 的 API 与 UI
- 维护运行状态、审批、事件流、审计、指标
- 将 workflow 交给 engine 执行

#### 7.2.2 Runner Engine

职责：

- 解释 Workflow
- 做目标解析、策略调度、批次分发、状态流转
- 执行 check / apply / rollback / verify
- 管理审批、恢复、失败重试

#### 7.2.3 Runner Agent

职责：

- 作为远程执行平面
- 支持本地 agent 内执行、远程节点执行、能力白名单
- 回传 stdout / stderr / typed outputs
- 维护 waiting token 与 task 映射

#### 7.2.4 AIOps Orchestrator

职责：

- 当 Runner 无法处理或任务失败时，由主 Agent 接管
- 根据 Coroot 与 Memory 决定是否需要启动子 Agent
- 将 Agent 成功经验重新沉淀为新的 Workflow / Script / ExpPack

---

## 8. 资源模型

Runner 中至少需要 7 类资源：

### 8.1 Workflow

定义执行编排本身。

### 8.2 Script

定义可复用脚本内容，例如：

- shell script
- python script
- future: sql / powershell / k8s snippets

### 8.3 Agent

定义执行节点：

- address
- token / mTLS identity
- capabilities
- tags / env / region / role
- heartbeat / last error

### 8.4 Environment

定义变量、密钥引用、默认策略。

### 8.5 Run

定义一次 workflow 执行实例。

### 8.6 Approval

定义审批请求与决策记录。

### 8.7 Artifact

定义执行产物：

- stdout / stderr chunk files
- diff
- rendered files
- backup files
- RCA snapshot

---

## 9. Workflow DSL 设计

### 9.1 目标

DSL 既要适合大规模运维，也要适合主 Agent 或工程师人工编辑。

要求：

- 人类可读
- AI 易生成
- 可静态校验
- 可版本化
- 可扩展

### 9.2 顶层结构

建议的目标结构如下：

```yaml
version: v1alpha1
name: install-nginx-batch
description: 批量安装并验证 nginx
labels:
  domain: nginx
  class: batch-install
owners:
  - sre

inventory:
  groups:
    web:
      hosts: [web01, web02, web03]
      vars:
        pkg_name: nginx
  hosts:
    web01:
      address: agent://web01
    web02:
      address: agent://web02
    web03:
      address: agent://web03

env_ref: prod-linux

plan:
  mode: manual-approve
  strategy: rolling
  batch_size: 50
  max_parallel: 100
  max_failures: 10
  on_batch_failure: pause
  on_host_failure: continue

steps:
  - id: precheck
    name: 采集系统信息
    targets: [web]
    action: shell.run
    args:
      script_ref: collect_linux_baseline
    outputs:
      - os_release
      - kernel

  - id: install
    name: 安装 nginx
    targets: [web]
    action: package.install
    approvals:
      required: true
      reason: 批量安装软件包会修改目标主机状态
    args:
      name: ${pkg_name}
      state: present

  - id: enable
    name: 启动并设置开机自启
    targets: [web]
    action: service.ensure
    depends_on: [install]
    args:
      name: nginx
      enabled: true
      state: started

  - id: verify
    name: 校验服务健康
    targets: [web]
    action: http.check
    depends_on: [enable]
    args:
      url: http://127.0.0.1/
      expect_status: 200

handlers:
  - id: rollback-nginx
    action: package.install
    args:
      name: nginx
      state: absent
```

### 9.3 顶层字段

- `version`: DSL 版本
- `name`: workflow 名称
- `description`: 描述
- `labels`: 标签，便于 Memory / 搜索 / 策略匹配
- `owners`: 所有者
- `inventory`: 主机、分组、变量
- `env_ref`: 环境变量集合
- `plan`: 运行策略
- `steps`: 主要执行链
- `handlers`: 补偿 / 通知 / 回滚动作
- `tests`: 可选静态校验与预演

### 9.4 plan 语义

#### 9.4.1 mode

- `auto`: 自动运行
- `manual-approve`: 整个 run 或关键步骤需要审批
- `policy-gated`: 由策略引擎自动判断是否需要审批

#### 9.4.2 strategy

- `sequential`: 串行
- `parallel`: 所有目标并行
- `rolling`: 滚动发布
- `canary`: 金丝雀
- `sharded`: 分片

#### 9.4.3 批量控制

- `batch_size`
- `max_parallel`
- `max_failures`
- `failure_threshold_percent`
- `cooldown_sec`

### 9.5 step 语义

建议 step 支持：

- `id`
- `name`
- `targets`
- `action`
- `args`
- `depends_on`
- `when`
- `loop`
- `timeout`
- `retries`
- `continue_on_error`
- `approvals`
- `verify`
- `rollback`
- `outputs`
- `notify`

### 9.6 输出模型

建议不再把变量传递长期绑定在 `BOPS_EXPORT:` 上，而改成统一输出：

```yaml
outputs:
  collect:
    os_release: ${result.json.os_release}
    kernel: ${result.json.kernel}
```

执行时统一持久化：

- `result.status`
- `result.stdout`
- `result.stderr`
- `result.changed`
- `result.diff`
- `result.outputs`
- `result.error`

### 9.7 tests 语义

建议引入：

- schema validate
- inventory resolve test
- dry-run / check-mode test
- capability test
- policy test

---

## 10. 运行时状态机设计

### 10.1 Run 状态

建议引入以下 run 状态：

- `draft`
- `queued`
- `preparing`
- `waiting_approval`
- `running`
- `paused`
- `resuming`
- `partial_failed`
- `failed`
- `canceled`
- `interrupted`
- `success`

### 10.2 Step 状态

- `pending`
- `ready`
- `waiting_approval`
- `running`
- `paused`
- `success`
- `failed`
- `skipped`
- `rollback_running`
- `rollback_success`
- `rollback_failed`

### 10.3 Host 状态

- `pending`
- `running`
- `success`
- `failed`
- `timed_out`
- `canceled`
- `waiting_approval`
- `fallback_to_agent`

### 10.4 审批状态

- `requested`
- `approved`
- `rejected`
- `expired`
- `superseded`

### 10.5 恢复机制

每个 run 在以下节点打 checkpoint：

- step 开始前
- step 审批前
- batch 开始前
- batch 完成后
- rollback 开始前

checkpoint 最少包含：

- run_id
- workflow revision
- current step id
- current batch index
- per-host status
- outputs snapshot
- waiting token

---

## 11. 审批与恢复设计

### 11.1 审批为什么必须是一等能力

运维场景里，下面这些动作不能只靠 UI 弹窗：

- 安装 / 卸载软件
- 改配置文件
- 重启服务
- 批量执行危险命令
- 跨环境推广

因此审批必须体现在：

- DSL 里可声明
- 引擎里可阻塞
- API 里可查询和决策
- 审计里可追踪
- run state 可恢复

### 11.2 审批模型

建议每个可审批步骤生成一个 Approval Request：

- `approval_id`
- `run_id`
- `step_id`
- `batch_id`
- `targets`
- `reason`
- `requested_by`
- `requested_at`
- `expires_at`
- `resume_token`
- `status`
- `decision_by`
- `decision_at`
- `decision_comment`

### 11.3 审批接口

建议新增：

- `GET /api/v1/runs/{id}/approvals`
- `POST /api/v1/runs/{id}/approvals/{approval_id}/approve`
- `POST /api/v1/runs/{id}/approvals/{approval_id}/reject`
- `POST /api/v1/runs/{id}/resume`

### 11.4 恢复策略

审批通过后：

- 不重跑已完成 step
- 从 `waiting_approval` 的下一个安全点继续
- 对当前 batch 中未执行主机继续执行
- 对已成功主机保持成功状态

这部分正是建议借鉴 Lobster 的核心。

---

## 12. 执行策略设计

### 12.1 sequential

适用于：

- 单机恢复
- 高风险数据库操作
- 人工值守流程

### 12.2 parallel

适用于：

- 纯采集
- 低风险批量操作

### 12.3 rolling

适用于：

- 服务升级
- 配置变更
- systemd reload

### 12.4 canary

适用于：

- 新版本发布
- 新规则 / 新配置试投产

### 12.5 fallback to agent

这是本设计最关键的 AIOps 差异点。

当某个 host 失败时：

1. Runner 记录失败上下文。
2. Runner 发出 `host_failed` 事件。
3. 主 Agent 读取 run 上下文、Coroot 指标、主机画像。
4. 主 Agent 拉起对应 host 的子 Agent。
5. 子 Agent 接管单机诊断与修复。
6. 成功经验可回写为新的 Script / Workflow / ExpPack。

---

## 13. 模块系统设计

### 13.1 模块接口

建议把现有 `Check/Apply/Rollback` 接口真正接入执行路径：

- `Check`: 生成 diff，判断是否变更
- `Apply`: 执行变更
- `Rollback`: 失败补偿

### 13.2 MVP 内置模块

建议优先内置：

- `cmd.run`
- `shell.run`
- `script.shell`
- `script.python`
- `file.write`
- `file.template`
- `package.install`
- `service.ensure`
- `process.check`
- `http.check`
- `wait.until`
- `wait.event`
- `assert.equals`
- `assert.contains`

### 13.3 运维模块要点

运维模块需要标准化输出：

- `changed`
- `before`
- `after`
- `diff`
- `evidence`
- `safe_to_retry`
- `rollback_supported`

### 13.4 AI 友好性

模块 schema 要足够清晰，便于主 Agent 自动生成 Workflow：

- 参数可枚举
- 默认值明确
- 风险级别明确
- 审批建议明确

---

## 14. Server 侧能力设计

Runner Server 需要具备下面这些能力：

### 14.1 Workflow 管理

- Workflow CRUD
- label / tag / owner 检索
- validate
- dry-run
- version compare

### 14.2 Script 管理

- Script CRUD
- language / tag
- render
- checksum / revision

### 14.3 Agent 管理

- register
- update
- heartbeat
- capability
- offline detection
- probe

### 14.4 Run Center

- submit
- cancel
- retry failed hosts
- resume
- list
- detail
- events
- approvals

### 14.5 Dashboard / Metrics

- queue depth
- run success rate
- average duration
- host failure ratio
- approval wait time
- fallback-to-agent count

### 14.6 审计

必须记录：

- 谁提交了 run
- 谁审批了步骤
- 哪些主机被修改
- 执行了哪些模块
- 输出与 artifact 在哪里

---

## 15. Agent 设计

### 15.1 Agent 模式

建议保留两种模式：

- `embedded agent-server`: 轻量、直接
- `host runner agent`: 部署到目标环境，负责本地执行

### 15.2 Agent 元数据

至少包括：

- `id`
- `address`
- `status`
- `token` 或 mTLS identity
- `capabilities`
- `tags`
- `env`
- `region`
- `last_beat_at`
- `last_error`

### 15.3 Agent 能力边界

Agent 必须支持 capability filter，例如：

- 某 agent 只能执行 `shell.run`
- 某 agent 禁止 `package.install`
- 某 agent 只能在生产读、在测试写

### 15.4 Agent 的 waiting token

现有实现已经有 waiting token 的缓存入口，后续可直接扩展成审批恢复能力。

---

## 16. 与主 Agent / Blackboard / Coroot / Memory 的集成

### 16.1 与主 Agent 的关系

主 Agent 不直接替代 Runner。

主 Agent 负责：

- 任务理解
- 任务分类
- 选择 Runner 还是子 Agent
- 查看 Runner 状态
- 审批建议
- 失败回退决策

### 16.2 与 Blackboard 的关系

Blackboard 维护平台级 DAG；
Runner 维护一次 Workflow 的执行 FSM。

两者关系：

- Blackboard 是“任务级”
- Runner FSM 是“执行级”

主 Agent 可以把 Runner Run 作为 Blackboard 中的一个节点。

### 16.3 与 Coroot 的关系

Coroot 参与三个阶段：

- 执行前：确认告警、影响范围、目标组件
- 执行中：验证修复指标是否恢复
- 执行后：形成 RCA 对账和闭环

### 16.4 与 Memory 的关系

Memory 参与三个阶段：

- 执行前：匹配已有经验包、命中已有 Workflow
- 执行中：注入目标主机画像与风险提示
- 执行后：总结经验，提升命中率

### 16.5 经验沉淀闭环

建议形成如下闭环：

```text
子 Agent 自由修复成功
    -> 主 Agent 归纳步骤
    -> 生成人工可审的 Script / Workflow 草稿
    -> 审核通过后进入 Runner Store
    -> 下次优先命中 Runner Playbook
```

---

## 17. 如何使用

下面给出推荐使用方式。

### 17.1 注册执行节点

1. 部署 runner agent 到各目标环境。
2. 为 agent 配置 token 或 mTLS。
3. 在 Runner Server 中注册 agent。
4. 为 agent 标记 capability、环境、区域、风险级别。

### 17.2 创建可复用脚本

1. 将通用脚本保存为 Script。
2. 标记语言、标签、描述、版本。
3. 在 Workflow 中通过 `script_ref` 引用。

### 17.3 创建 Workflow

1. 填写 inventory、plan、steps。
2. 为有副作用的步骤设置 approval。
3. 为关键步骤定义 verify 和 rollback。
4. 通过 validate / dry-run 做预检。

### 17.4 提交 Run

方式一：Web UI

- 在 Run Center 选择 Workflow
- 填写 vars
- 选择目标环境
- 点击 Run

方式二：API

```bash
curl -X POST http://runner.example/api/v1/runs \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{
    "workflow_name": "install-nginx-batch",
    "vars": {
      "pkg_name": "nginx"
    },
    "triggered_by": "planner-agent",
    "idempotency_key": "incident-20260326-nginx-install"
  }'
```

### 17.5 查看执行过程

- `GET /api/v1/runs/{id}`
- `GET /api/v1/runs/{id}/events`
- `GET /api/v1/runs/{id}/events/history`
- `GET /metrics`

### 17.6 审批与恢复

1. step 进入 `waiting_approval`
2. UI 展示审批原因、目标主机、风险和 diff
3. 用户 approve / reject
4. run 使用 resume token 从 checkpoint 继续

### 17.7 失败回退到子 Agent

1. 某 batch 主机执行失败
2. Runner 将失败上下文回写 Blackboard
3. 主 Agent 读取 Coroot + Memory
4. 为失败主机启动子 Agent
5. 子 Agent 诊断修复
6. 成功后生成新的 Workflow / Script 草稿

---

## 18. 与市面工具的对比

下表从“你的目标场景”出发，而不是做纯功能罗列。

| 工具 | 强项 | 短板 | 本设计相对优势 |
| --- | --- | --- | --- |
| Lobster | 审批门、可恢复、确定性 pipeline、安全 runtime | 更像单助手中的工具链 runtime，不以多主机批量分发为核心 | 保留 Lobster 的审批/恢复思想，但把多主机批量执行做成第一等公民 |
| Ansible / AWX | inventory、job template、workflow visualizer、成熟生态 | 强依赖 playbook 生态，对 AI 调度、失败回退到 Agent 不友好 | 支持 AI-first 调度、agent fallback、可扩展模块运行时，而不是只围绕 playbook |
| Rundeck | Runbook UI、节点模型、远程执行、Runner | 更偏 runbook automation，中间层和 AI 编排结合较弱 | 把 run center、agent、AI fallback、memory、coroot 全部纳入统一架构 |
| Argo Workflows | DAG、并行、Kubernetes 原生 | 偏 K8s 批任务，不适合作为主机运维执行平面 | 不依赖 Kubernetes，把 host ops、agent registry、approvals 作为核心语义 |
| Temporal | durable execution、超强恢复语义 | 偏应用编排，不是主机运维执行引擎 | 吸收 durable 思想，但把 inventory、batch、host strategy、ops modules 做成一等能力 |
| StackStorm | 事件驱动、rules、workflow、action runner | 事件自动化强，但 AIOps 中的 agent fallback 与 memory 闭环不自然 | 保留事件触发思路，但以运维任务执行闭环为核心 |

### 18.1 对比 Lobster

Lobster 的优点非常值得借鉴：

- 审批门
- resume token
- 确定性运行时
- timeout / output cap / allowlist

但 Lobster 不是你的主底座，因为你的目标不是“一个助手里的多步骤工具链”，而是“一个可批量分发、可回退到 Agent 的运维执行平面”。

### 18.2 对比 Ansible / AWX

Ansible / AWX 的问题不在于它不好，而在于它太偏 playbook-first：

- 它很适合基础设施自动化
- 但不天然适合 AI planner -> runner -> agent fallback 这条链
- 也不天然具备“失败即拉起子 Agent 智能修复”的架构位

本设计的优势是：

- Workflow 不绑定某个现成执行器
- 能把 Agent 经验持续沉淀为新的 Workflow
- 能把 Coroot、Memory、Blackboard 直接纳入闭环

### 18.3 对比 Rundeck

Rundeck 的节点和 Runner 思路与你的场景很接近，尤其是远程环境调度。
但它更偏“中心化 Runbook Automation 平台”。

本设计的优势在于：

- 更 AI-native
- 更适合 host-level fallback to agent
- 更适合把知识沉淀到 Workflow / Script / ExpPack / Memory

### 18.4 对比 Argo Workflows

Argo 适合：

- K8s 批处理
- 容器化步骤
- DAG 强依赖编排

但你的问题核心是：

- 主机运维
- agent registry
- 审批
- 回退到子 Agent

因此本设计比 Argo 更贴近运维一线使用场景。

### 18.5 对比 Temporal

Temporal 最大的启发是“durable execution”。
这一点本设计必须学。

但 Temporal 太偏业务应用流程：

- 业务状态机
- 代码式 workflow
- 分布式应用恢复

而你的问题核心是 host ops。

本设计应该吸收 Temporal 的：

- checkpoint
- resume
- retry
- deterministic state machine

但不把系统做成通用业务流程平台。

### 18.6 对比 StackStorm

StackStorm 很强的地方在于：

- 规则触发
- 事件驱动
- action / workflow / runner 体系

但你的平台最终是：

- AI 驱动
- 运维为主
- Runner 与 Agent 协同

所以本设计更强调：

- 任务执行闭环
- 批量修复闭环
- 经验沉淀闭环

---

## 19. 本设计的核心优势

如果这套设计落地，它相对市面工具会有 8 个明显优势：

### 19.1 Runner 与 Agent 双引擎协同

不是“只有脚本分发”，也不是“只有 AI agent 自由发挥”，而是两套引擎协同。

### 19.2 批量执行与智能修复共存

Runner 负责规模，Agent 负责弹性。

### 19.3 审批与恢复是运行时能力

不是外挂式 UI，而是状态机内建能力。

### 19.4 AIOps 原生集成

天然接 Blackboard、Coroot、Memory、ExpPack。

### 19.5 对运维更友好

围绕 host、service、package、config、batch、rolling、failover 设计，而不是围绕容器批处理或业务事务。

### 19.6 不依赖单一生态

不被 Kubernetes、Ansible Playbook、单一脚本体系绑定。

### 19.7 Playbook 经验沉淀闭环

AI 能把自由修复经验持续转化为可审查、可批量复用的 Workflow。

### 19.8 更适合作为平台底座

它不是一个插件 runtime，而是一套平台级执行平面。

---

## 20. 落地路线图

### Phase 0: 收敛现有 Runner 基线

- 修复 engine API 漂移
- 恢复或删除失效的 `yaml_apply`
- 把现有 server / api / run center 收口稳定

### Phase 1: Runner 运行时增强

- 引入 approval state
- 引入 resume token
- 引入 checkpoint / interrupted recovery
- 实现 `wait.event`

### Phase 2: 多主机策略增强

- `parallel`
- `rolling`
- `canary`
- `batch_size`
- `max_failures`

### Phase 3: 模块体系增强

- `package.install`
- `service.ensure`
- `file.template`
- `http.check`
- `process.check`

### Phase 4: AIOps 集成

- Runner failure -> Blackboard
- Planner trigger fallback agent
- Coroot verify hook
- Memory / ExpPack 命中与沉淀

### Phase 5: 经验自动沉淀

- 子 Agent 成功修复 -> 生成 workflow draft
- 人工审核 -> 入库
- 主 Agent 下次优先命中 Runner

---

## 21. 最终建议

建议最终采用下面这条路线：

> 架构主线以 Runner 为主，运行时语义借鉴 Lobster，恢复语义借鉴 Temporal，批量与节点管理吸收 Rundeck / AWX 的长处，但整个平台必须围绕 AIOps 的 Runner + Agent 协同来设计。

再说得更直接一点：

- 不要把系统做成“Lobster 的大号版”。
- 不要把系统做成“Ansible/AWX 的 AI 包装层”。
- 不要把系统做成“Temporal 套壳主机运维”。

最优解是：

> 做一套以 Runner 为中心的智能运维执行平面，让它天然支持 AI 调度、审批恢复、批量执行、失败回退到 Agent、以及经验沉淀闭环。

这才是最符合你最终架构图的路线。

---

## 22. 参考资料

截至 2026-03-26，本文参考了以下官方资料：

- OpenClaw Lobster: https://docs.openclaw.ai/tools/lobster
- OpenClaw Cron vs Heartbeat: https://docs.openclaw.ai/cron-vs-heartbeat
- Ansible Automation Controller Workflows: https://docs.ansible.com/automation-controller/4.3/html/userguide/workflows.html
- Ansible Automation Controller Inventories: https://docs.ansible.com/automation-controller/4.4/html/userguide/inventories.html
- Rundeck Nodes Overview: https://docs.rundeck.com/docs/learning/getting-started/nodes-overview.html
- Rundeck Enterprise Runner: https://docs.rundeck.com/docs/administration/runner/runner-overview.html
- Argo Workflows DAG: https://argo-workflows.readthedocs.io/en/latest/walk-through/dag/
- Temporal Docs: https://docs.temporal.io/
- StackStorm Actions: https://docs.stackstorm.com/actions.html
- StackStorm Workflows: https://docs.stackstorm.com/workflows.html
- StackStorm Rules: https://docs.stackstorm.com/rules.html

