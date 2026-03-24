# design-0323-ui-authz

## 1. 已确认的 V1 决策

本项目当前阶段直接定死以下边界：

- 卡片 UI 固定为 7 种
- 不使用 `mcp-ui`
- 不给模型暴露万能命令工具
- 模型侧只注册 2 个物理隔离的远程执行工具：
  - `execute_readonly_query`
  - `execute_system_mutation`
- 渐进式授权先只做 2 档：
  - `Approve_Once`
  - `Approve_Command_For_Session`

这套设计的目标不是“一步到位覆盖所有运维动作”，而是先把：

- 聊天流
- 审批流
- 主机执行
- 会话级免打扰授权

这四条主线做稳。

## 2. 七种卡片 UI

V1 只保留下面 7 种卡片，不再继续扩类型。

### 2.1 `MessageCard`

用途：

- 用户消息
- Assistant 普通回复
- 简短说明文本

字段：

- `message_id`
- `role`
- `content`
- `created_at`
- `host_id?`

UI 要点：

- 保持聊天主视图简洁
- 支持 Markdown
- 如果当前回合绑定了主机，显示 `host-A` 这类 host pill

### 2.2 `PlanCard`

用途：

- 展示 Codex 当前回合的执行计划
- 展示任务清单的完成进度
- 让用户看到“接下来准备做什么”

字段：

- `plan_id`
- `title`
- `host_id?`
- `items[]`
- `status`
- `updated_at`

其中 `items[]` 的每一项建议包含：

- `item_id`
- `text`
- `status`

状态建议：

- `pending`
- `in_progress`
- `completed`

UI 要点：

- 同一轮任务里尽量复用同一张 `PlanCard`
- `completed` 的任务项使用中横线划掉
- `in_progress` 的任务项高亮显示
- 不要把每次计划更新都渲染成一张新卡片，应该原位更新

推荐渲染效果：

- 未完成：普通文本
- 进行中：加粗或强调色
- 已完成：删除线

### 2.3 `StepCard`

用途：

- 展示工具执行过程
- 展示“正在等待审批”“已自动批准”“执行完成”这类中间态

字段：

- `step_id`
- `tool_name`
- `host_id`
- `status`
- `summary`
- `args_preview`
- `started_at`
- `ended_at?`

状态建议：

- `running`
- `waiting_approval`
- `auto_approved`
- `completed`
- `failed`
- `cancelled`

### 2.4 `ResultCard`

用途：

- 展示结构化结果
- 展示命令摘要
- 展示错误摘要

字段：

- `result_id`
- `title`
- `host_id`
- `summary`
- `kv_rows[]`
- `stdout_excerpt?`
- `stderr_excerpt?`
- `exit_code?`

UI 要点：

- 默认展示摘要
- 原始输出折叠
- 适合承接 `df`、`systemctl status`、`ss -lntp` 这类结果

### 2.5 `CommandApprovalCard`

用途：

- 审批任何“命令型系统变更”

字段：

- `approval_id`
- `host_id`
- `tool_name`
- `command_preview`
- `cwd`
- `risk_level`
- `reason`
- `session_grant_option`
- `status`

按钮与交互：

- 单选项：
  - `仅允许执行本次命令`
  - `允许此命令在当前会话中免审`
- 操作按钮：
  - `Approve`
  - `Decline`

### 2.6 `FileChangeApprovalCard`

用途：

- 审批任何“文件写入 / patch / 覆盖配置”类变更

字段：

- `approval_id`
- `host_id`
- `tool_name`
- `file_path`
- `change_summary`
- `diff_preview`
- `risk_level`
- `reason`
- `session_grant_option`
- `status`

按钮与交互：

- 单选项：
  - `仅允许执行本次变更`
  - `允许此类文件变更在当前会话中免审`
- 操作按钮：
  - `Approve`
  - `Decline`

### 2.7 `ChoiceCard`

用途：

- 承接通用 `tool/requestUserInput`
- 例如选择环境、选择是否继续、选择发布策略

字段：

- `request_id`
- `title`
- `question`
- `options[]`
- `status`

UI 要点：

- 每个选项是清晰的单选按钮
- 不承担高风险命令审批主流程
- 更偏“让用户补充信息”

## 3. 为什么明确不用 `mcp-ui`

这里直接定结论：

- V1 不使用 `mcp-ui`

原因：

- 你现在最核心的是聊天流、计划卡片、审批卡片、工具状态回传
- 这些卡片都和 `codex app-server` 的事件流强绑定
- 审批卡片属于安全关键 UI，必须完全受你自己的前端状态机控制
- 引入 `mcp-ui` 会多一套 Widget 生命周期和动作回传语义，当前阶段收益不大

所以当前推荐实现是：

- 全部 7 种卡片都由 Vue 原生组件实现
- 数据统一由 `ai-server` 通过 WebSocket 推送
- 前端本地只做状态归并和交互反馈

## 4. Tool Segregation 设计

## 4.1 核心原则

永远不要给模型一个：

- `run_linux_command`

这样的万能接口。

模型在 Function Calling 或 MCP Tool 阶段就必须看到明确分离的两个工具：

- `execute_readonly_query`
- `execute_system_mutation`

这不是“命名优化”，而是物理隔离设计的一部分。

## 4.2 模型侧只注册两个独立工具

### 工具 A：`execute_readonly_query`

给模型的描述：

- 仅用于执行只读的系统探测命令，例如 `df`、`ps`、`ss`、`ls`、`cat`、`grep`、`tail`。
- 绝对不能包含任何修改系统状态的动作。
- 如果需要修改、删除、安装、重启、kill、写文件，必须使用 `execute_system_mutation`。

默认策略：

- 默认免审
- 命中安全校验后自动执行

推荐 Schema：

```json
{
  "type": "object",
  "properties": {
    "host_id": { "type": "string" },
    "program": { "type": "string" },
    "args": {
      "type": "array",
      "items": { "type": "string" }
    },
    "cwd": { "type": "string" },
    "timeout_sec": { "type": "integer", "minimum": 1, "maximum": 120 },
    "reason": { "type": "string" }
  },
  "required": ["host_id", "program", "args", "reason"]
}
```

说明：

- 即使是只读工具，也尽量不用自由文本 `command`
- 优先使用 `program + args`
- 这样更容易做 allowlist 和审计

### 工具 B：`execute_system_mutation`

给模型的描述：

- 用于执行任何会改变系统状态的操作。
- 包括安装软件、重启服务、写文件、删除文件、执行脚本、发送信号等。

默认策略：

- 默认拦截
- 需要审批
- 未获批前不执行

推荐 Schema：

```json
{
  "type": "object",
  "properties": {
    "host_id": { "type": "string" },
    "mode": {
      "type": "string",
      "enum": ["command", "file_change"]
    },
    "program": { "type": "string" },
    "args": {
      "type": "array",
      "items": { "type": "string" }
    },
    "cwd": { "type": "string" },
    "file_path": { "type": "string" },
    "patch_text": { "type": "string" },
    "timeout_sec": { "type": "integer", "minimum": 1, "maximum": 600 },
    "reason": { "type": "string" }
  },
  "required": ["host_id", "mode", "reason"]
}
```

说明：

- `mode=command` 时，承接命令变更型执行
- `mode=file_change` 时，承接文件改动型执行
- 这样可以和 `CommandApprovalCard` / `FileChangeApprovalCard` 直接对应

## 4.3 两个工具如何映射到前端卡片

在真正执行工具前，如果 Codex 先给出了计划，则前端应先展示一张 `PlanCard`。

`PlanCard` 的更新原则是：

- 同一个 `plan_id` 只维护一张卡片
- 后续进度变化只更新 `items[]` 的状态
- 某个任务完成后，对该行文本直接加删除线
- 不额外插入新的“计划完成”卡片

### `execute_readonly_query`

典型链路：

1. 模型调用工具
2. `ai-server` 通过安全校验
3. 通过 `ai-server <-> Host Agent` 的 gRPC 长连接直接下发
4. 前端展示：
   - `PlanCard`
   - `StepCard(running)`
   - `ResultCard`

### `execute_system_mutation(mode=command)`

典型链路：

1. 模型调用工具
2. `ai-server` 标记为待审批
3. 前端展示：
   - `PlanCard`
   - `StepCard(waiting_approval)`
   - `CommandApprovalCard`
4. 用户批准后执行
5. `ai-server` 再通过 gRPC 长连接把动作发给目标 `Host Agent`
6. 前端再展示：
   - `StepCard(running/completed)`
   - `ResultCard`

### `execute_system_mutation(mode=file_change)`

典型链路：

1. 模型调用工具
2. `ai-server` 标记为待审批
3. 前端展示：
   - `PlanCard`
   - `StepCard(waiting_approval)`
   - `FileChangeApprovalCard`
4. 用户批准后执行
5. `ai-server` 再通过 gRPC 长连接把动作发给目标 `Host Agent`
6. 展示结果

## 4.4 物理隔离要落在三层

### 第一层：模型声明隔离

模型只看到两个不同工具名，语义边界明确。

### 第二层：后端路由隔离

`ai-server` 内部必须是两条独立执行路径：

- `readonly-dispatcher`
- `mutation-dispatcher`

不要共用一个 `executeRemoteCommand()` 再靠布尔开关分支。

### 第三层：主机执行器隔离

Host Agent 内部也分开：

- `readonly-runner`
- `mutation-runner`

职责分别是：

- `readonly-runner` 只接受 allowlist 程序
- `mutation-runner` 只有拿到审批通过令牌时才可执行

## 4.5 App-Server / ai-server 的最后一道防线

即使你已经拆成两个工具，也必须防止模型“犯傻”。

也就是：

- 模型可能把 `rm -rf /tmp/a` 塞进 `execute_readonly_query`

所以 `execute_readonly_query` 在真正执行前必须过一个严格校验器。

推荐校验步骤：

1. 校验 `program` 是否在只读 allowlist
2. 校验 `args` 中是否出现明显变更型关键字
3. 拒绝 `sudo`
4. 拒绝 shell 解释器作为 `program`
5. 拒绝重定向、here-doc、命令替换等 shell 语法
6. 校验路径是否落在允许读取范围

推荐的只读 allowlist 起步集合：

- `df`
- `du`
- `free`
- `uptime`
- `ps`
- `ss`
- `netstat`
- `ls`
- `cat`
- `grep`
- `head`
- `tail`
- `find`
- `systemctl`
- `journalctl`

对 `systemctl` / `journalctl` 这类命令要再做子命令限制。

例如：

- 允许：`systemctl status nginx`
- 拒绝：`systemctl restart nginx`

如果校验失败：

- 不要偷偷转去执行 `execute_system_mutation`
- 直接拒绝执行，并把错误返回模型：
  - `This request is not read-only. Use execute_system_mutation instead.`

这样模型会在下一步改用正确工具。

## 4.6 Host Agent 内的执行器设计

### `readonly-runner`

约束：

- 普通用户执行
- 禁止 `sudo`
- 禁止 shell 模式
- 仅支持 `execve(program, args)` 风格执行
- 超时更短，例如默认 `15-30s`

### `mutation-runner`

约束：

- 必须带审批通过令牌
- 默认普通用户执行
- 涉及提权时只能走白名单 helper
- 记录完整审计日志

推荐不要做：

- 直接让模型生成 `sudo bash -lc '...'`

推荐做：

- 先由 `ai-server` 把可提权操作翻译成受控 helper
- 或者只允许有限的 sudoers 白名单命令

## 5. 渐进式授权设计

## 5.1 V1 只做两档授权

当前阶段只支持：

- `Approve_Once`
- `Approve_Command_For_Session`

不要一开始做太多粒度。

原因：

- 粒度一多，前端和策略引擎会迅速复杂化
- 先把“本次”与“本会话同命令免审”跑通，价值已经很高

## 5.2 前端审批卡片怎么设计

`CommandApprovalCard` 和 `FileChangeApprovalCard` 的底部统一采用：

- 一个单选组
  - `仅允许执行本次命令`
  - `允许此命令在当前会话中免审`
- 两个操作按钮
  - `Approve`
  - `Decline`

推荐默认选中：

- `仅允许执行本次命令`

避免误授予长时间权限。

## 5.3 Session Grant 如何存

`ai-server` 需要维护一张会话级授权表，例如 `session_command_grants`：

- `grant_id`
- `session_id`
- `user_id`
- `host_id`
- `tool_name`
- `grant_mode`
- `command_fingerprint`
- `file_scope?`
- `created_at`
- `expires_at`
- `created_from_approval_id`

其中：

- `grant_mode` 固定为 `Approve_Command_For_Session`
- `command_fingerprint` 是本次操作的归一化指纹

## 5.4 指纹怎么计算

不要直接拿原始命令字符串做比较。

推荐生成规则：

- `tool_name`
- `host_id`
- `mode`
- `program`
- 归一化后的 `args`
- 如果是文件变更，再加 `file_path`

例如：

```text
execute_system_mutation|host-A|command|systemctl|restart|nginx
```

或：

```text
execute_system_mutation|host-A|file_change|/etc/nginx/nginx.conf
```

这样同一会话内再次出现完全相同的动作时，可以自动放行。

## 5.5 自动批准链路

当 `execute_system_mutation` 到来时：

1. `ai-server` 先算出 `command_fingerprint`
2. 查询当前 `session_id` 下是否存在有效 grant
3. 如果命中：
   - 自动批准
   - 插入一个 `StepCard(auto_approved)`
   - 不再弹审批卡片
4. 如果未命中：
   - 创建审批记录
   - 推送 `CommandApprovalCard` 或 `FileChangeApprovalCard`

## 5.6 授权有效期

当前建议：

- grant 生命周期等于浏览器会话生命周期

也就是：

- 用户登出、会话过期、页面刷新后重建会话，都可以让 grant 失效

如果你想更稳一点，也可以加一个固定 TTL，例如：

- `30m`

但 V1 推荐逻辑上仍把它视作“当前会话授权”。

## 5.7 与 `codex app-server` 审批事件如何配合

推荐链路：

1. `codex app-server` 因 `execute_system_mutation` 进入待审批
2. `ai-server` 收到该审批请求
3. 先查本地 session grant
4. 如果命中：
   - 直接回 app-server `approved`
   - 前端只展示 `StepCard(auto_approved)`
5. 如果未命中：
   - 展示审批卡片
   - 等用户点击

这样可以同时满足：

- app-server 语义上知道“这次工具需要审批”
- 用户又不会被重复弹窗打扰

## 6. 前端状态模型

推荐前端统一维护：

- `messages[]`
- `plans[]`
- `steps[]`
- `results[]`
- `approvals[]`
- `sessionGrants[]`

其中 `plans[]` 至少包含：

- `plan_id`
- `thread_id`
- `turn_id`
- `title`
- `host_id?`
- `items[]`
- `status`
- `updated_at`

`items[]` 的每一项至少包含：

- `item_id`
- `text`
- `status`

其中 `approvals[]` 至少包含：

- `approval_id`
- `thread_id`
- `turn_id`
- `request_id`
- `host_id`
- `tool_name`
- `mode`
- `command_preview?`
- `file_path?`
- `diff_preview?`
- `risk_level`
- `grant_options`
- `status`

## 7. V1 最终推荐落地方式

如果现在就开始实现，推荐按下面顺序做：

1. 固定 7 种卡片的前端组件
2. `ai-server` 注册两个工具：
   - `execute_readonly_query`
   - `execute_system_mutation`
3. 先把 `readonly-runner` 和 `mutation-runner` 分开
4. 实现 `readonly` allowlist 校验器
5. 实现审批表和 `session_command_grants`
6. 接通：
   - `Approve_Once`
   - `Approve_Command_For_Session`

## 8. 一句话总结

V1 最稳的方案是：

- 7 种卡片全部自己写
- 不用 `mcp-ui`
- 模型只看到“只读工具”和“变更工具”两个物理隔离工具
- `readonly` 默认免审，但必须经过严格校验
- `mutation` 默认拦截
- 用户可把“当前命令在当前会话内免审”的信任范围授予系统
