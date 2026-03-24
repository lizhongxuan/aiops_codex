# design-0323

## 0. 文档定位

本文是总设计文档，负责定义：

- 总体架构
- 模块职责
- 协议边界
- 安全与实施阶段

其中与 `codex app-server` 的事件流、审批卡片、前端交互细节有关的内容，在 [design-0323-app-server-approval.md](/Users/lizhongxuan/Desktop/aiops-codex/design-0323-app-server-approval.md) 中做专项细化。

## 1. 方案目标

建设一个面向 AI 运维的远程控制系统，包含三个核心模块：

- `Host Agent`：部署在远程主机上的 Go 轻量客户端，启动后主动向业务 `App-Server` 发起双向 `gRPC` 长连接，提供真实 PTY 能力，支持输入、输出回显、窗口缩放、信号中断、命令执行与日志流回传。
- `ai-server`：自建服务端，也就是你的业务 `App-Server`，负责主机接入、Web 控制台、终端转发、审批编排、审计、与 Codex 的协议桥接。
- `codex app-server`：作为 Codex 会话引擎，负责聊天、上下文、审批事件、账号状态、流式消息和工具调用编排。

目标效果：

1. 用户打开网页后，可以看到在线主机列表。
2. 用户点击某台主机后，可以像本地终端一样操作远程主机，并实时看到输出。
3. 页面上有 Codex 聊天框，用户给出运维指令后，Codex 可以通过受控方式操作远程主机。
4. 所有高风险操作都有审批、审计和权限边界。

## 2. 关键结论

### 2.1 页面应由 `ai-server` 自建

`codex app-server` 的定位是“把 Codex 嵌入你自己的产品”，它提供的是协议、认证、会话历史、审批与事件流，不是一个可直接复用的 Web 控制台。因此本方案默认：

- 终端页面、主机列表、审批卡片、聊天框，全部由 `ai-server` 提供。
- `codex app-server` 作为后端会话引擎，以 `stdio JSON-RPC` 形式接入。

### 2.2 远程主机控制不建议直接复用 Codex 本地 Shell

本项目的远程执行链路不是“Codex 直接在服务端本机跑 shell”，而是：

- Codex 通过工具调用请求远程操作。
- `ai-server` 把请求转成发给 `Host Agent` 的 gRPC 控制消息。
- `Host Agent` 在远程主机上执行，并把输出流回。

因此推荐把“远程主机终端能力”封装成一个自定义 MCP 工具集，而不是试图把 `codex app-server` 的本地 shell 执行直接替换成远程 PTY。

### 2.3 审批事件要按 `codex app-server` 原生协议实现

需要修正一个关键点：原生审批不是简单依赖一个泛化的 `turn/item` 卡片，而是由 `codex app-server` 发起明确的服务器请求。

- 命令执行审批：`item/commandExecution/requestApproval`
- 文件变更审批：`item/fileChange/requestApproval`
- MCP / App 工具侧效审批：`tool/requestUserInput`
- 审批完成确认：`serverRequest/resolved`

如果把远程终端暴露为 MCP 工具，那么最贴近原生体验的做法是基于 `tool/requestUserInput` 渲染审批卡片。

### 2.4 “网页里让每个用户原生登录自己的 ChatGPT 账号”有实现风险

这是本方案里最需要提前说明的点。

官方文档表明：

- `codex app-server` 支持三种认证模式：`apikey`、`chatgpt`、`chatgptAuthTokens`
- 浏览器登录模式 `chatgpt` 返回的 `authUrl` 使用本地回调，`app-server` 自己托管 callback
- 登录缓存默认保存在 `~/.codex/auth.json` 或系统 keyring，不是保存在 `config.toml`

这意味着：

- 如果 `codex app-server` 运行在你的服务器上，而用户在自己的浏览器里访问网页，那么 `chatgpt` 这种“本地回调登录”天然不适合直接做多租户 Web 登录。
- 你可以做每用户隔离的 `CODEX_HOME` 和独立进程，但浏览器回调链路依然是一个风险点。

因此推荐：

- `V1`：先用平台 API Key 或平台托管的服务账号跑通 AI 运维。
- `V2`：如果后续强需求是“每个网页用户使用自己的 Codex/ChatGPT 身份”，优先评估 `chatgptAuthTokens` 模式或用户自有 App-Server/Agent 方案，而不是直接依赖服务器侧 `chatgpt` 浏览器回调。

## 3. 总体架构

```text
┌──────────────────────────────────────────────────────────────┐
│                         Browser                               │
│  主机列表 / Web Terminal / 审批卡片 / Codex Chat             │
└───────────────┬───────────────────────────┬──────────────────┘
                │                           │
                │ WebSocket / HTTPS         │ stdio JSON-RPC
                │                           │
┌───────────────▼───────────────────────────▼──────────────────┐
│                          ai-server                            │
│                                                              │
│  1. Web 控制台                                                │
│  2. 主机注册与连接管理                                        │
│  3. gRPC Agent Gateway                                        │
│  4. 终端会话代理 / 输出广播                                   │
│  5. 审批中心 / 审计                                           │
│  6. Codex 网关                                                │
│  7. MCP Remote Tools Gateway                                  │
└───────────────┬───────────────────────────┬──────────────────┘
                │                           │
                │ outbound gRPC stream      │ local stdio JSON-RPC
                │                           │
┌───────────────▼──────────────┐   ┌────────▼──────────────────┐
│          Host Agent          │   │     codex app-server      │
│  运行在远程主机               │   │  会话/认证/审批/事件流     │
│  Go + PTY + gRPC stream      │   │  工具编排 / 对话状态       │
└──────────────────────────────┘   └───────────────────────────┘
```

## 4. 模块设计

### 4.1 `Host Agent`

职责：

- 主动向 `ai-server` 发起双向 `gRPC` 长连接，避免服务端反向打洞。
- 在远程主机上创建真实 PTY。
- 提供交互式终端和短命令执行两种能力。
- 把 stdout/stderr 以流式方式实时上送。
- 支持断线重连、会话恢复、心跳保活。

建议能力：

- `Connect`：建立双向 gRPC 流。
- `register`：主机注册，提交 `host_id`、主机名、OS、标签、版本、能力集。
- `heartbeat`：应用层心跳或 keepalive。
- `pty/open`：创建交互式 PTY，会返回 `session_id`。
- `pty/input`：写入键盘输入。
- `pty/resize`：更新 cols/rows。
- `pty/signal`：发送 `SIGINT` / `SIGTERM`。
- `pty/close`：关闭 PTY。
- `exec/start`：执行一次性命令，底层仍建议走 PTY，保证安装类命令的真实输出。
- `exec/cancel`：取消命令。
- `stream/output`：按序号回传输出块。
- `stream/exit`：回传退出码、结束时间、耗时。

实现建议：

- Linux/macOS 统一基于 PTY 实现；Windows 后续再补 ConPTY。
- 所有输出分块附带 `seq`，支持断线补拉。
- 所有命令执行默认带 `cwd`、`env`、`timeout_sec`、`rows/cols`。
- 所有控制消息与输出都尽量复用同一条 gRPC 长连接。
- 不在客户端保存长期用户态 token，使用短期接入令牌 + 设备指纹/mTLS。

### 4.2 `ai-server`

职责：

- 对外提供网页控制台和 WebSocket 接口。
- 对内维护与所有 `Host Agent` 的双向 gRPC 长连接池。
- 维护主机在线状态、会话状态、用户与主机授权关系。
- 代理浏览器终端输入输出。
- 代理 `codex app-server` 的 JSON-RPC。
- 暴露给 Codex 的远程运维工具集。
- 承接审批流、审计流和日志留存。

建议拆分为 5 个内部子模块：

- `host-registry`：主机注册、心跳、租约、标签筛选。
- `agent-gateway`：维护 gRPC 长连接、版本协商、断线重连和流控。
- `terminal-broker`：终端创建、流式输出广播、重连恢复。
- `codex-gateway`：维护浏览器到 `codex app-server` 的会话映射。
- `remote-tools`：把 Codex 工具调用翻译为发给 `Host Agent` 的控制消息。
- `approval-audit`：审批卡片、审批结果、命令审计、操作留痕。

### 4.3 `codex app-server`

职责：

- 提供线程、回合、流式消息、审批、认证和工具调用编排。
- 给前端输出接近原生 Codex 的对话体验。
- 负责工具调用时的原生事件序列。

在本方案中的使用方式：

- 不作为页面提供者。
- 不直接接远程主机。
- 作为“AI 会话内核”，由 `ai-server` 做协议桥接。

## 5. 推荐落地方案

### 5.1 V1 推荐方案：`ai-server + logical per-user Codex session mapping + Remote MCP Tools`

这是最稳妥、最容易做出来的版本。

这里的 `per-user Codex session` 指的是：

- 每个网页用户都有独立的线程、回合、审批上下文和主机选择上下文

它不默认等于：

- 每个用户一个独立 `codex app-server` 进程
- 每个用户一套独立 OpenAI 凭证

核心思路：

1. `ai-server` 自建页面。
2. 浏览器连接 `ai-server`，获得 Web Terminal 和 Codex Chat。
3. `ai-server` 通过 `stdio` 维护与 `codex app-server` 的 JSON-RPC 长生命周期会话。
4. `V1` 的模型侧工具固定为两类：
   - `execute_readonly_query`
   - `execute_system_mutation`
5. Codex 通过工具调用来控制远程主机。
6. 高风险工具调用走原生审批事件。

优点：

- 与 `codex app-server` 当前官方协议匹配度高。
- 审批可以走原生 `tool/requestUserInput`。
- 多主机、多会话管理清晰。
- 不需要篡改 Codex 内建 shell 语义。

缺点：

- Codex 看到的是“远程工具”而不是“本地 shell”。
- 文件改动不会天然复用 `fileChange` 的本地补丁体验，除非你进一步实现远程文件工具和 diff 展示。

### 5.2 V2 进阶方案：增强远程工程操作体验

在 V1 跑通后，可以继续补：

- 在 `execute_system_mutation` 内部细化 `command` / `file_change` 两种 mode。
- 再逐步向模型暴露更高层的结构化工具，例如 `restart_service`、`package_install`、`read_tail_log`。
- 主机分组批量执行。
- 操作前快照与回滚。
- 远程 sudo 能力的分级审批。

## 6. 核心流程

### 6.1 远程主机接入流程

1. 运维人员在目标主机安装并启动 `Host Agent`。
2. `Host Agent` 使用预置 bootstrap token 主动向 `ai-server` 发起双向 `gRPC` 长连接。
3. `ai-server` 校验 token、设备指纹、版本与主机策略。
4. 注册成功后，主机进入在线列表，并持续发送心跳。
5. 用户在页面看到主机在线状态和基础信息。

### 6.2 网页终端流程

1. 用户在页面选择一台主机。
2. 前端向 `ai-server` 请求 `terminal/open`。
3. `ai-server` 通过 gRPC 流给对应 `Host Agent` 下发 `pty/open`。
4. `Host Agent` 创建 shell，开始回传输出流。
5. 前端键盘输入通过 `ai-server` 转发给 `Host Agent`。
6. 输出流实时广播到前端终端组件。
7. 浏览器缩放窗口时，前端触发 `terminal/resize`。

### 6.3 Codex 驱动远程运维流程

1. 用户在聊天框中输入：“帮我在 host-A 上安装 nginx 并验证端口监听”。
2. 前端把消息发给 `ai-server`。
3. `ai-server` 转发到 `codex app-server` 的 `turn/start`。
4. Codex 推理后决定调用 `execute_readonly_query` 或 `execute_system_mutation`。
5. 如果是 `execute_system_mutation`，`codex app-server` 发出审批请求。
6. `ai-server` 先检查当前浏览器会话里是否已经存在匹配的 session grant。
7. 如果未命中授权，前端在聊天流里渲染审批卡片。
8. 用户点击 `Approve` 后，`ai-server` 把审批结果回传给 `codex app-server`，并按需要写入会话授权。
9. `codex app-server` 继续发起工具调用。
10. `ai-server` 将工具调用通过 gRPC 流下发给目标主机的 `Host Agent`。
11. `Host Agent` 执行命令并回传输出。
12. `ai-server` 一边把输出写入审计日志，一边把结果返回给 Codex。
13. Codex 整理执行结果，在聊天框中给出结论和下一步建议。

## 7. 原生审批机制设计

### 7.1 推荐实现

如果远程终端能力通过 MCP/App Tool 暴露给 Codex，则审批以 `tool/requestUserInput` 为主。

页面表现建议：

- 聊天流中除了普通消息，还应支持 `PlanCard`、`StepCard`、`ResultCard`、审批卡片。
- 在聊天流中插入一张审批卡片，不要做全局弹窗。
- 卡片内展示：
  - 主机名
  - 工具名
  - 高亮命令
  - 工作目录
  - 风险说明
  - 申请来源（Codex / 用户）
- 操作按钮：
  - `Approve`
  - `Decline`
  - 可选 `Approve for Session`

### 7.2 协议流

#### A. 开始会话

- `thread/start` 或 `turn/start` 时，不要把 `approvalPolicy` 设为 `never`
- 推荐默认：
  - `approvalPolicy = "on-request"` 或更保守策略
  - 对高风险工具始终开启审批

#### B. 触发审批

对于远程执行工具：

1. Codex 决定调用 `execute_system_mutation`
2. `codex app-server` 发出 `tool/requestUserInput`
3. `ai-server` 先检查当前会话是否已有匹配的 session grant
4. 如果未命中授权，再把审批请求转成前端审批卡片

对于 Codex 自身命令/文件能力：

- 命令审批事件：`item/commandExecution/requestApproval`
- 文件审批事件：`item/fileChange/requestApproval`

#### C. 审批回传

1. 前端点击 `Approve`
2. 前端向 `ai-server` 发送审批决议
3. `ai-server` 把 JSON-RPC 响应透传给 `codex app-server`
4. `codex app-server` 返回 `serverRequest/resolved`
5. 随后继续工具调用或把条目标记为 declined/cancelled

### 7.3 审批策略建议

- 默认所有远程副作用操作都审批：
  - 软件安装
  - 包管理
  - 服务启动/停止/重启
  - 文件写入
  - sudo 命令
  - 网络访问
- 可免审批的只读动作：
  - `uname -a`
  - `ps`
  - `top`/`htop` 替代查询
  - `systemctl status`
  - `journalctl -n`
  - 文件读取

### 7.4 审计字段

每次审批至少记录：

- `approval_id`
- `thread_id`
- `turn_id`
- `host_id`
- `user_id`
- `tool_name`
- `command_preview`
- `decision`
- `approver`
- `requested_at`
- `resolved_at`
- `exit_code`

## 8. 登录与账号管理设计

### 8.1 官方能力边界

根据官方协议，`codex app-server` 当前支持：

- `apikey`
- `chatgpt`
- `chatgptAuthTokens`

并且：

- 登录缓存默认在 `~/.codex/auth.json` 或系统 keyring
- `config.toml` 主要用于配置策略，例如 `forced_login_method`
- `chatgpt` 浏览器登录流依赖 `authUrl` 和本地 callback

因此，原需求里“token 保存在 `config.toml`”这一点需要改成：

- 凭证与缓存归 `CODEX_HOME` 下的 `auth.json` 或 keyring
- 策略配置归 `config.toml`

### 8.2 推荐方案 A：平台托管账号或 API Key

适用场景：

- 先把系统做出来。
- 用户主要关注 AI 运维能力，不要求每个人必须绑定自己 OpenAI 身份。

实现方式：

- `ai-server` 统一维护一个或少量 `codex app-server` 实例。
- 使用 API Key 登录 `codex app-server`。
- 平台侧 RBAC 决定谁能操作哪些主机。

优点：

- 简单。
- 稳定。
- 最适合 V1。

缺点：

- 不是“每人自己的 ChatGPT/Codex 身份”。

### 8.3 推荐方案 B：每用户隔离 `CODEX_HOME` + 独立 `app-server` 进程

适用场景：

- 你确实需要每个网页用户拥有自己的 Codex 会话和凭证隔离。

隔离原则：

- 每个用户一个 `CODEX_HOME`，例如 `/data/codex/users/<user_id>/`
- 每个用户一个 `codex app-server` 进程
- 每个用户一个独立监听端口
- 进程空闲超时自动回收

目录结构建议：

```text
/data/codex/users/<user_id>/
  |- config.toml
  |- auth.json
  |- logs/
  |- threads/
```

但要特别说明：

- 如果走 `chatgpt` 浏览器登录，`authUrl` 的回调默认是 app-server 本地 callback。
- 在“用户浏览器访问你的网页，app-server 运行在服务器”的架构下，这个流程未必天然可用。

所以方案 B 内部分两条路：

- `B1`：每用户 API Key，最稳。
- `B2`：每用户外部托管 ChatGPT token，再通过 `chatgptAuthTokens` 注入。

### 8.4 不推荐直接作为 V1 采用的方案

不建议一开始就强依赖“服务器侧 `chatgpt` 浏览器回调登录 + 多租户网页代理”，原因有三点：

1. 回调目标是 app-server 本地地址，不天然适配 Web SaaS 场景。
2. 多用户并发时，账号隔离、回调路由、超时恢复都会变复杂。
3. 一旦登录失败，定位问题会同时涉及浏览器、反向代理、回调端口和凭证缓存。

## 9. 远程工具设计

### 9.1 建议工具集

- `execute_readonly_query(host_id, program, args[], cwd?, timeout_sec?, reason)`
- `execute_system_mutation(host_id, mode, program?, args[]?, file_path?, patch_text?, cwd?, timeout_sec?, reason)`
- `remote_terminal_open(host_id, shell?, cwd?, cols?, rows?)`
- `remote_terminal_write(session_id, data)`
- `remote_terminal_read(session_id, cursor?)`
- `remote_terminal_resize(session_id, cols, rows)`
- `remote_terminal_close(session_id)`

### 9.2 为什么要做高层工具

对 AI 运维来说，先把“只读”和“变更”拆成两个物理隔离工具，比“一个万能 shell 字符串”更可控：

- 更容易做审批。
- 更容易做审计。
- 更容易做参数校验。
- 更容易做权限收敛。

建议：

- `V1` 只注册 `execute_readonly_query` 和 `execute_system_mutation`。
- `V2` 再逐步把常见运维动作包装成更高层的结构化工具。

## 10. 安全设计

### 10.1 主机接入安全

- `Host Agent` 只主动出站，不开放入站端口。
- 接入使用短时 bootstrap token。
- 成功注册后切换为会话 token。
- 可选 mTLS。
- 每台主机绑定唯一 `host_id` 与指纹。

### 10.2 命令执行安全

- 默认非 root 运行 `Host Agent`。
- sudo 通过显式审批触发。
- 不传 root 密码，不在服务端保存 sudo 密码。
- 能结构化的动作不要直接落成任意 shell。

### 10.3 用户权限安全

- 页面登录用户与可操作主机做 RBAC 绑定。
- Codex 只能访问当前用户被授权的主机。
- 同一会话中的 host scope 必须明确。

### 10.4 日志与敏感信息

- 终端日志默认落审计，但支持敏感字段脱敏。
- 对 `Authorization`、`password`、`token`、私钥块做掩码。
- 审计日志与在线回显分层保存。

## 11. 数据模型建议

- `users`
- `hosts`
- `host_connections`
- `terminal_sessions`
- `terminal_output_chunks`
- `codex_sessions`
- `approval_requests`
- `tool_calls`
- `audit_logs`

关键字段建议：

- `hosts.status`：`online | offline | reconnecting`
- `terminal_sessions.status`：`opening | active | closed | failed`
- `approval_requests.status`：`pending | accepted | declined | canceled | expired`

## 12. 实施阶段

### Phase 1

- 完成 `Host Agent`
- 完成 `ai-server` 主机接入
- 完成网页终端
- 完成主机列表与在线状态

### Phase 2

- 接入 `codex app-server`
- 跑通聊天框
- 跑通 `execute_readonly_query`
- 跑通 `execute_system_mutation`
- 跑通审批卡片

### Phase 3

- 增加高层运维工具
- 增加审计与回放
- 增加会话恢复和批量执行
- 增加用户级隔离 `CODEX_HOME`

### Phase 4

- 评估 per-user 原生身份接入
- 评估 `chatgptAuthTokens`
- 增加更细粒度的审批策略和风险分级

## 13. 最终推荐

如果目标是“尽快做出能工作的 AI 运维平台”，推荐采用下面这条主线：

1. `ai-server` 自建页面。
2. `Host Agent` 负责真实远程终端与 gRPC 长连接。
3. `codex app-server` 只做会话引擎，不做页面。
4. 远程主机能力通过 MCP 工具暴露给 Codex。
5. 审批按 `codex app-server` 原生事件做，优先使用 `tool/requestUserInput`。
6. V1 使用平台 API Key 或平台托管身份。
7. 等系统跑稳后，再做 per-user 身份隔离。

## 14. 参考资料

- Codex App Server: https://developers.openai.com/codex/app-server
- Codex Authentication: https://developers.openai.com/codex/auth
- Codex Agent Approvals & Security: https://developers.openai.com/codex/agent-approvals-security
- Codex App Overview: https://developers.openai.com/codex/app

## 15. 与原始想法相比的修正点

- 不是复用 `codex app-server` 自带页面，而是由 `ai-server` 自建控制台。
- 原生审批不应只写成 `turn/item`，而应按 `item/commandExecution/requestApproval`、`item/fileChange/requestApproval`、`tool/requestUserInput` 实现。
- 凭证缓存不应写入 `config.toml`，而应放在 `~/.codex/auth.json` 或 keyring；`config.toml` 用于策略配置。
- 服务器侧多租户网页场景下，直接使用 `chatgpt` 本地回调登录存在实现风险，V1 不建议作为唯一主路径。

## 16. PlantUML 图示

- 总体架构图：`design-0323-architecture.puml`
- 用户通过网页让 Codex 在远程主机安装软件的流程图：`design-0323-install-flow.puml`
