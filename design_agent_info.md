# Agent Profile 页面设计方案

## 1. 目标

这个页面用于统一管理 Agent 的静态配置，不展示运行时状态。

页面聚焦的配置项只有：

- `System Prompt`
- `命令权限 / Command Permissions`
- `能力权限 / Capability Permissions`
- `Skills`
- `MCP`
- `主 agent 的基础运行参数`
  - `model`
  - `reasoningEffort`
  - `approvalPolicy`
  - `sandboxMode`

不在这页里展示：

- 当前主机是否在线
- 当前 thread / turn / session
- host heartbeat / last error / agent version
- 当前执行中的临时上下文

## 2. 多 Agent Profile 模型

第一版不再把页面理解成“一个全局 prompt 配置”，而是收口成“多个 Agent Profile 的统一配置入口”。

第一版最少支持两个静态 profile：

- `main-agent`
- `host-agent-default`

两类 profile 的公共配置维度完全一致：

- `System Prompt`
- `Command Permissions`
- `Capability Permissions`
- `Skills`
- `MCP`

第一版暂不开放：

- 单 host-agent 覆盖配置
- session 级覆盖
- 临时 prompt patch

## 3. 技术路线决策

第一版采用 `todo_agent_info.md` 里的方案 B：

- `main-agent`：真实消费 `System Prompt / 权限 / model / reasoning / approval / sandbox`
- `host-agent`：暂时仍是执行守护进程，不升级为真正的本地 LLM runtime
- `host-agent-default profile`：先接统一 schema、持久化、页面编辑与 API；实际 runtime 消费放到后续阶段，只映射到执行策略边界

这意味着：

- 页面上 `main-agent` 与 `host-agent-default` 看起来是同构 profile
- 但第一版只有 `main-agent` 真正接入 prompt 和运行参数
- `host-agent-default` 先完成“建模一致、配置可存、接口可读写”，不假装已经具备本地推理能力

## 4. 页面入口

入口放在主页面左下角设置按钮弹出菜单中。

设置菜单第一版包含：

- `通用设置`
- `Agent Profile`

点击后进入独立路由：

- `/settings/agent`

这个页面需要脱离主聊天工作区壳层，不再渲染会话、终端、主机卡片等运行态 UI。

## 5. 页面结构

页面采用三栏布局：

### 5.1 左侧：Profile 导航

- Profile 列表
- 当前选中状态
- 返回主工作区入口

第一版固定展示：

- `主 agent`
- `host-agent 默认`

### 5.2 中间：配置表单

按区块组织：

1. `Profile 概览`
2. `System Prompt`
3. `Command Permissions`
4. `Capability Permissions`
5. `Skills`
6. `MCP`

### 5.3 右侧：生效预览

展示：

- `最终 system prompt`
- `命令权限摘要`
- `capability 摘要`
- `已启用 skills`
- `已启用 MCP`
- `main-agent` 的 runtime 参数摘要

## 6. 配置项细节

### 6.1 Profile 概览

- `Profile Name`
- `Profile ID`
- `Profile Type`
- `Description`

### 6.2 Main-Agent Runtime

第一版只对 `main-agent` 真正生效：

- `model`
- `reasoningEffort`
- `approvalPolicy`
- `sandboxMode`

`host-agent-default` 也允许编辑同样字段，但第一版只存储，不由 host-agent runtime 真正消费。

### 6.3 System Prompt

- 大文本编辑区
- `notes`
- `preview`
- 恢复默认
- dirty state 提示

建议的内容结构仍然是：

- 角色定义
- 执行原则
- 安全约束
- 输出风格
- 工具使用规则

### 6.4 Command Permissions

字段：

- `enabled`
- `defaultMode`
- `allowShellWrapper`
- `allowSudo`
- `defaultTimeoutSeconds`
- `allowedWritableRoots`
- `categoryPolicies`

命令类别：

- `system_inspection`
- `service_read`
- `network_read`
- `file_read`
- `service_mutation`
- `filesystem_mutation`
- `package_mutation`

权限档位：

- `allow`
- `approval_required`
- `readonly_only`
- `deny`

### 6.5 Capability Permissions

能力集合：

- `commandExecution`
- `fileRead`
- `fileSearch`
- `fileChange`
- `terminal`
- `webSearch`
- `webOpen`
- `approval`
- `multiAgent`
- `plan`
- `summary`

状态集合：

- `enabled`
- `approval_required`
- `disabled`

### 6.6 Skills

字段：

- `id`
- `name`
- `description`
- `source`
- `enabled`
- `activationMode`

### 6.7 MCP

字段：

- `id`
- `name`
- `type`
- `source`
- `enabled`
- `permission`
- `requiresExplicitUserApproval`

## 7. 保存与校验

后端必须做强校验：

- `system prompt` 非空且限制长度
- 命令类别和模式必须是合法枚举
- capability 状态必须是合法枚举
- writable roots 不能为空路径

高风险能力不能只靠 prompt 约束，仍然要靠 server/执行层兜底。

## 8. 第一版完成标准

第一版的完成定义是：

- 可以在页面里切换 `main-agent` / `host-agent-default`
- 可以编辑并保存两类 profile 的静态配置
- `main-agent` 新 thread / 新 turn 会按 profile 生效
- `host-agent-default` 已有统一 schema、存储和接口，但 runtime 真实消费后置
- 页面不混入主机运行态信息
