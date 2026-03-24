# design-0323-app-server-approval

## 0. 文档定位

本文不是一份新的总体架构方案，而是 [design-0323.md](/Users/lizhongxuan/Desktop/aiops-codex/design-0323.md) 的专项细化文档。

它重点展开三件事：

- `ai-server` 如何与 `codex app-server` 交互
- 审批请求如何进入前端卡片
- 用户决议如何回传并继续执行

## 1. 先说结论

在你的方案里，`codex app-server` 不负责远程主机连接本身，它负责三件事：

- 维护 Codex 会话和线程
- 把 Agent 工作过程以事件流形式推给客户端
- 在需要用户确认时，向客户端发起审批请求

你自己的 `ai-server` 负责：

- 作为业务 `App-Server` 维护与浏览器、Codex、Host Agent 三侧连接
- 启动并维护 `codex app-server` 进程连接
- 把浏览器输入转成 JSON-RPC 请求
- 把 app-server 的事件流转成前端可消费的 WS 消息
- 在用户点击审批按钮后，把审批结果回传给 app-server
- 如果是远程主机工具，再把工具调用通过 gRPC 流转到目标主机的 `Host Agent`

所以从实现角度看：

- `codex app-server` 是“Agent Runtime + 审批事件源”
- `ai-server` 是“协议桥接器 + 业务编排层”
- 浏览器是“展示层 + 审批交互层”

## 2. 交互分成两类

这部分非常关键。

### 2.1 本地内建工具型交互

如果 Codex 使用的是它自己的内建工具，例如：

- 本地 command execution
- 本地 file change

那么 app-server 会直接发这些审批事件：

- `item/commandExecution/requestApproval`
- `item/fileChange/requestApproval`

这种模式更像“Codex 控制它自己所在机器的 shell 和文件系统”。

### 2.2 远程工具型交互

如果你做的是“Codex 控制远程主机”，推荐不是让 Codex 去调用本地 shell，而是让它调用你暴露给它的远程工具，例如：

- `execute_readonly_query`
- `execute_system_mutation(mode=command)`
- `execute_system_mutation(mode=file_change)`

这时更贴近的交互是：

- `mcpToolCall` 或 `dynamicToolCall`
- 有副作用时，app-server 通过 `tool/requestUserInput` 请求用户审批

对你的项目来说，主路径应该是第二种。

## 3. ai-server 如何连接 codex app-server

推荐方式：

- `ai-server` 启动子进程：`codex app-server`
- 通过子进程的 `stdin/stdout` 收发 JSON-RPC 一行一条消息

原因：

- 这是 app-server 默认传输方式
- 部署最稳
- 不需要把 app-server 单独暴露成外网服务

基本流程：

1. `ai-server` 启动 `codex app-server`
2. 建立 stdout 监听
3. 发送 `initialize`
4. 发送 `initialized`
5. 发送 `thread/start`
6. 发送 `turn/start`
7. 持续读取 `item/*`、`turn/*`、`thread/*`、审批请求等通知

## 4. 一个最小会话流程

### 4.1 初始化连接

`ai-server -> codex app-server`

```json
{
  "method": "initialize",
  "id": 1,
  "params": {
    "clientInfo": {
      "name": "ai_server",
      "title": "AIOps Web Console",
      "version": "0.1.0"
    }
  }
}
```

然后：

```json
{
  "method": "initialized",
  "params": {}
}
```

### 4.2 创建线程

```json
{
  "method": "thread/start",
  "id": 2,
  "params": {
    "model": "gpt-5.4",
    "cwd": "/srv/empty",
    "approvalPolicy": "unlessTrusted",
    "sandboxPolicy": {
      "type": "workspaceWrite",
      "writableRoots": ["/srv/empty"],
      "networkAccess": false
    },
    "serviceName": "aiops-web"
  }
}
```

这里的 `cwd` 和 `sandboxPolicy` 只是 Codex 运行时自己的上下文约束，不等于远程主机工作目录。

### 4.3 发起一次用户提问

浏览器输入：

- `host_id = host-A`
- 文本：`帮我检查 nginx 是否运行，不在的话帮我启动`

`ai-server` 应把“用户文本 + 主机选择上下文”一起传给 Codex。

推荐做法：

- 在 `turn/start.input` 里给一条普通文本
- 再拼一条系统约束或 developer 指令，明确告诉 Codex 当前目标主机是 `host-A`
- 并告诉它只能通过 `execute_*` 工具操作该主机

示例：

```json
{
  "method": "turn/start",
  "id": 3,
  "params": {
    "threadId": "thr_123",
    "input": [
      {
        "type": "text",
        "text": "当前用户选择的目标主机是 host-A。你只能通过 execute_* 工具操作该主机。用户请求：帮我检查 nginx 是否运行，不在的话帮我启动。"
      }
    ]
  }
}
```

## 5. app-server 返回给 ai-server 的事件流

在 `turn/start` 之后，app-server 不会只回一个最终答案，它会持续推事件。

最重要的事件类型有：

- `thread/started`
- `turn/started`
- `item/started`
- `item/agentMessage/delta`
- `item/completed`
- `turn/completed`

在你的网页里，一般这样映射：

- `item/agentMessage/delta` -> 聊天流的流式打字效果
- `item/started` / `item/completed` -> 会话时间线里的步骤卡片
- `turn/completed` -> 本轮对话结束

如果本轮中 Codex 先给出一个执行计划，推荐由 `ai-server` 归一化成：

- `plan.created`
- `plan.updated`

也就是：

- 第一次出现计划时创建一张 `PlanCard`
- 后续只更新同一个 `plan_id` 下的任务项状态
- 某个任务完成后，在同一张卡片里把该任务文本加删除线

## 6. 审批到底是怎么发生的

### 6.1 本地 shell / 文件变更审批

如果 Codex 调的是它自己的本地 command/file 工具，消息顺序是：

1. `item/started`
2. `item/commandExecution/requestApproval` 或 `item/fileChange/requestApproval`
3. 客户端提交审批结果
4. `serverRequest/resolved`
5. `item/completed`

这更适合“Codex 改本地仓库、跑本地命令”的场景。

### 6.2 远程主机工具审批

如果你走的是推荐路径，也就是 Codex 调 `execute_system_mutation` 这类工具，那么更合理的审批方式是：

1. Codex 计划调用 `execute_system_mutation`
2. app-server 产生一个工具调用项，例如 `mcpToolCall`
3. 对该工具存在副作用时，app-server 发 `tool/requestUserInput`
4. `ai-server` 将该请求转成前端审批卡片
5. `ai-server` 先查当前会话是否存在有效授权
6. 如果命中会话授权，直接自动批准，并插入 `StepCard(auto_approved)`
7. 如果未命中，再展示审批卡片，用户点击 `Approve` 或 `Decline`
8. `ai-server` 把审批结果回复给 app-server
9. app-server 继续该回合，真正发起工具调用
10. `ai-server` 执行远程工具并把结果返回给 app-server
11. app-server 输出最终 assistant 结论

这条链路和你的“远程主机”架构是匹配的。

## 7. 用户在网页上到底看到什么

推荐界面不是弹窗优先，而是“聊天流中的审批卡片”。

### 7.1 用户视觉上会看到五类内容

#### 第一类：普通聊天消息

例如：

- “我准备检查 host-A 上的 nginx 服务状态。”
- “我需要执行一条有副作用的命令，请你确认。”

#### 第二类：计划卡片

也就是 `PlanCard`。

例如：

- 读取 nginx 服务状态
- 如果未运行则启动服务
- 验证 80 端口监听

展示规则：

- 未完成任务正常显示
- 当前进行中的任务高亮
- 已完成任务用中横线划掉

#### 第三类：步骤卡片

例如：

- 正在调用 `execute_readonly_query`
- 正在等待审批
- 已执行完成，退出码 0

#### 第四类：结果卡片

也就是 `ResultCard`。

例如：

- nginx 服务状态摘要
- 端口监听结果
- 安装结果与退出码

#### 第五类：审批卡片

卡片建议展示：

- 目标主机：`host-A`
- 工具名：`execute_system_mutation`
- 命令预览：`systemctl start nginx`
- 工作目录：`/`
- 风险等级：`medium` 或 `high`
- 原因：`需要启动服务以满足用户请求`
- 授权选项：
  - `仅允许执行本次命令`
  - `允许此命令在当前会话中免审`
- 按钮：`Approve`、`Decline`

如果是文件写入型工具，则展示：

- 目标文件路径
- 变更 diff 摘要
- 风险说明

## 8. 前端如何实现审批卡片

前端不要把审批当成一个全局模态框，而要把它当成聊天流中的一种“特殊 item”。

推荐前端状态模型：

- `messages[]`
- `plans[]`
- `timelineItems[]`
- `pendingApprovals[]`

其中 `plans[]` 的元素至少包含：

- `plan_id`
- `thread_id`
- `turn_id`
- `title`
- `items[]`
- `status`
- `updated_at`

`items[]` 的每一项至少包含：

- `item_id`
- `text`
- `status`

其中 `pendingApprovals[]` 的元素至少包含：

- `request_id`
- `thread_id`
- `turn_id`
- `item_id`
- `approval_type`
- `host_id`
- `tool_name`
- `command_preview`
- `cwd`
- `risk_level`
- `grant_mode`
- `command_fingerprint`
- `status`

当 `ai-server` 收到审批请求时：

1. 先持久化到数据库
2. 再通过浏览器 WebSocket 推送 `approval.created`
3. 前端把它渲染成聊天流卡片

用户点击按钮后：

1. 前端发 `approval.submit`
2. `ai-server` 校验用户权限
3. 如果用户选择 `允许此命令在当前会话中免审`，则写入 session grant
4. `ai-server` 回 app-server 审批结果
5. 更新数据库状态
6. 推送 `approval.resolved`

## 9. 后端实现原理

## 9.1 ai-server 内部建议拆成三类连接

### 会话 A：Browser Session

浏览器和 `ai-server` 的 WebSocket 连接。

负责：

- 用户输入
- 聊天流回传
- 审批卡片推送
- 终端日志推送

### 会话 B：Codex Session

`ai-server` 和 `codex app-server` 的 JSON-RPC 长连接。

负责：

- 启动线程
- 发起 turn
- 收事件流
- 回审批结果
- 执行工具回调

### 会话 C：Agent Session

`ai-server` 和每台远程主机 `Host Agent` 之间的双向 gRPC 长连接。

负责：

- 主机注册
- 心跳保活
- PTY 打开/关闭/输入/缩放
- 命令执行
- 日志输出回传

你的核心桥接工作，就是把这三类连接关联起来。

## 9.2 建议维护的映射关系

- `browser_session_id -> user_id`
- `thread_id -> browser_session_id`
- `thread_id -> selected_host_id`
- `host_id -> agent_connection_id`
- `plan_id -> thread_id`
- `approval_request_id -> thread_id`
- `tool_call_id -> host_id`

这样才能在用户点审批按钮时，准确找到要回给哪个 app-server 请求。

## 10. 审批通过后，远程命令是怎么真正执行的

这里要分清两层：

### 第一层：Codex 层“批准可以做”

用户点击 `Approve`，只是告诉 app-server：

- 这个工具调用现在可以继续

### 第二层：业务层“具体怎么做”

真正执行时，应该由你的 `ai-server` 把工具调用映射到远程主机。

例如：

1. Codex 决定调用 `execute_system_mutation`
2. app-server 把这次工具调用交给 `ai-server`
3. `ai-server` 检查审批状态是已通过
4. `ai-server` 找到 `host-A` 对应连接的 `Host Agent`
5. 下发：

```json
{
  "type": "exec/start",
  "request_id": "req_001",
  "host_id": "host-A",
  "command": ["bash", "-lc", "systemctl start nginx"],
  "cwd": "/",
  "timeout_sec": 60
}
```

6. `Host Agent` 在远程主机执行
7. 输出流回 `ai-server`
8. `ai-server` 把摘要结果再交还给 app-server
9. app-server 生成最终回复

## 11. 为什么审批不应该只做在 ai-server

很多人容易想到：

- “既然远程执行是我自己做的，那我在 ai-server 拦一下就行了”

但更好的方式是：

- app-server 和 ai-server 两层都参与

原因：

### app-server 层审批

优点：

- Codex 自己知道它正在等待用户
- 聊天流状态是连贯的
- assistant 不会误以为命令已经执行
- 用户体验更像原生 Codex

### ai-server 层审批校验

优点：

- 即使前端伪造请求，也执行不了未审批操作
- 你的远程执行链路有独立安全防线
- 审计链更完整

结论：

- app-server 负责“交互语义上的审批”
- ai-server 负责“执行前的最终授权校验”

## 12. 推荐的审批 UI 文案

命令型审批卡片建议：

- 标题：`Codex wants to run a command on host-A`
- 副标题：`This action may change system state`
- 主体：
  - Tool: `execute_system_mutation`
  - Command: `systemctl restart nginx`
  - CWD: `/`
  - Reason: `Restart nginx to apply the updated configuration`
- 授权选项：
  - `Only this command`
  - `Allow this command for this session`
- 按钮：
  - `Approve`
  - `Decline`

文件变更型审批卡片建议：

- 标题：`Codex wants to modify a file on host-A`
- 主体：
  - File: `/etc/nginx/nginx.conf`
  - Summary: `Adds a server block listening on 80`
  - Diff preview: `...`
- 授权选项：
  - `Only this file change`
  - `Allow this file change for this session`
- 按钮：
  - `Approve`
  - `Decline`

## 13. 一个完整的推荐链路

以“帮我在 host-A 安装 nginx”为例：

1. 用户在网页选中 `host-A`
2. 用户在聊天框输入需求
3. 浏览器把消息和 `host_id` 发给 `ai-server`
4. `ai-server` 调 `turn/start`
5. Codex 决定调用 `execute_system_mutation`
6. app-server 发 `tool/requestUserInput`
7. `ai-server` 先查本地 session grant
8. 如果未命中，则生成审批卡片并推到前端
9. 用户点击 `Approve`
10. `ai-server` 把审批结果回给 app-server，并按需要写入 session grant
11. `ai-server` 执行远程工具，通过 gRPC 流转发到 `Host Agent`
12. `Host Agent` 在远程主机执行安装
13. 输出流同时进入：
    - Web Terminal
    - 审计日志
    - 工具结果摘要
14. `ai-server` 把结果回给 app-server
15. app-server 输出最终结论，例如：
    - “nginx 已安装完成，服务已启动，80 端口已监听”

## 14. 最实用的工程建议

### 建议 1

V1 不要直接做“让 app-server 原生 shell 审批去审批远程命令”。

因为那是本地 shell 语义，不是远程主机语义。

### 建议 2

V1 优先做：

- `MCP tools`
- `tool/requestUserInput`
- 聊天流审批卡片

这是和你目标最一致的一条路。

### 建议 3

审批卡片必须带：

- 主机信息
- 工具名
- 命令或 diff 预览
- 风险等级
- 审批结果
- 审批人

### 建议 4

真正执行前，`ai-server` 必须再查一次审批状态，不要只相信前端按钮。

## 15. 一句话总结

你可以把 `codex app-server` 理解成一个“会持续发事件、偶尔向客户端发起审批请求的 Agent 内核”。

网页真正要做的，不是简单调一个 HTTP API，而是：

- 用 `ai-server` 维护一条到 app-server 的长连接
- 把 app-server 的事件流翻译成聊天消息、步骤卡片、审批卡片
- 在用户点击审批后，把决策精确回传给对应的 server request
- 对远程执行再走你自己的 `Host Agent + gRPC` 链路
