# design-0323-concrete-plan

## 0. 文档定位

本文不是另一套独立架构，而是 [design-0323.md](/Users/lizhongxuan/Desktop/aiops-codex/design-0323.md) 的“工程定版选型”。

它重点回答三件事：

- 每一层到底推荐用什么协议
- 关键组件直接选什么技术栈
- V1 应该按什么顺序落地

与 `codex app-server` 事件流、审批卡片、PlanCard、session grant 有关的细节，在 [design-0323-app-server-approval.md](/Users/lizhongxuan/Desktop/aiops-codex/design-0323-app-server-approval.md) 和 [design-0323-ui-authz.md](/Users/lizhongxuan/Desktop/aiops-codex/design-0323-ui-authz.md) 中做专项细化。

## 1. 最终推荐方案

目标能力是：

- 网页显示在线主机列表
- 点击某台主机后打开可交互终端
- 页面有 Codex 聊天框
- 用户在聊天框里指定主机并下发任务
- Codex 通过受控方式在远程主机执行命令
- 整个过程支持审批、审计、回显、断线重连

推荐不要走“浏览器直接 SSH 到主机”这条路线，也不要把 `codex app-server` 的本地 shell 强行替换成远程 shell。

推荐采用四层架构：

1. 浏览器层：主机列表 + Chat UI + Web Terminal
2. `ai-server`：主业务后端，也就是你的业务 `App-Server`，负责会话编排、终端代理、审批、审计、与 Codex 桥接
3. `codex app-server`：负责 Codex 会话、线程、审批事件、流式消息
4. `Host Agent`：部署在每台远程主机上的 Go 轻量客户端，主动向 `ai-server` 发起双向 `gRPC` 长连接，提供真实 PTY 能力

## 2. 协议选型

### 2.1 Browser <-> ai-server

使用：

- `HTTPS`：页面、REST API、鉴权
- `WebSocket`：终端实时输入输出、聊天流、审批事件推送

原因：

- 终端天然是低延迟双向流
- 主机状态、日志、审批卡片都适合服务端主动推送
- 浏览器对 WebSocket 支持最成熟

### 2.2 ai-server <-> Host Agent

`V1` 直接推荐：

- `gRPC bidirectional streaming`

推荐把所有控制与输出都复用到同一条长连接里，消息帧建议包括：

- `register`
- `heartbeat`
- `pty/open`
- `pty/input`
- `pty/resize`
- `pty/signal`
- `pty/close`
- `exec/start`
- `exec/cancel`
- `stream/output`
- `stream/exit`

原因：

- 远程主机主动出站，适合跨 VPC、NAT 和防火墙场景
- `gRPC` 天然适合做双向流、心跳、重连和流式输出
- 比浏览器风格的 `WSS` 自定义协议更适合 Go 写的主机代理
- 后续加 mTLS、版本协商、流控和 observability 更自然

### 2.3 ai-server <-> codex app-server

推荐：

- `stdio` 上跑 `JSON-RPC 2.0(JSONL)`

不推荐作为默认首选：

- `WebSocket` 模式

原因：

- `codex app-server` 官方默认传输就是 `stdio`
- 官方文档里 `websocket` 传输仍标为 experimental
- `ai-server` 与 `codex app-server` 通常部署在同机或同 Pod，`stdio` 最稳

### 2.4 Codex <-> 远程主机能力暴露方式

推荐：

- 把远程主机能力做成 `MCP tools` 或 `app-server dynamic tools`

优先顺序建议：

1. `MCP tools` 作为稳定主方案
2. `dynamic tools` 仅在你确认接受 experimental API 时采用

不要做：

- 让 Codex 误以为自己在控制本地 shell

正确做法是：

- `V1` 让 Codex 只调用两个物理隔离工具：
  - `execute_readonly_query`
  - `execute_system_mutation`
- `ai-server` 再把请求转成发给目标主机 `Host Agent` 的 gRPC 控制消息
- 输出流回到 `ai-server`
- `ai-server` 把摘要和结果再返回给 Codex

## 3. 开源项目选型

## 3.1 Web Terminal

生产方案直接选：

- `xterm.js`

建议同时使用的 addon：

- `@xterm/addon-fit`
- `@xterm/addon-web-links`
- `@xterm/addon-search`
- `@xterm/addon-webgl`

可选：

- `@xterm/addon-serialize`，用于断线恢复时保留终端缓冲区

原因：

- 这是事实标准，VS Code、Theia 等都在用
- 对 `bash`、`vim`、`tmux`、curses 程序兼容性好
- IME / CJK 支持成熟
- 可控性远高于“整包式 web terminal 产品”

结论：

- Web Terminal 选 `xterm.js`
- 不要把 `ttyd` / `wetty` / `gotty` 当最终终端内核

## 3.2 PTY 实现

如果 `Host Agent` 用 Go：

- `github.com/creack/pty`

如果只是讨论 Node 侧的 PTY 方案：

- `microsoft/node-pty`

推荐判断：

- 既然主机侧已经明确是 Go 写的 `Host Agent`，优先 `Go + creack/pty`
- `node-pty` 只适合作为参考，不再是主路径

我更推荐：

- `Host Agent` 用 Go
- `ai-server` 可以用 Go 或 Node

原因：

- Go 更适合做单文件部署、长连接、心跳、流式代理、跨平台发包
- 远程主机代理通常更希望是静态二进制、低依赖、低运维

## 3.3 哪些开源项目不建议作为最终主方案

### `ttyd`

适合：

- 快速 PoC
- 临时把某个命令暴露到网页

不适合：

- 多主机注册中心
- Codex 工具调用编排
- 审批 / 审计 / 权限模型
- 多租户远程控制平台

### `wetty`

适合：

- 浏览器 SSH 登录页

不适合：

- 做你整个平台的远程控制核心层

原因：

- 它本质上更偏“网页 SSH 客户端”
- 而你的目标是“Codex 驱动的主机控制平台”

### `webssh2`

适合：

- 如果你只想先做“浏览器手工 SSH 到机器”

不适合：

- Codex 通过结构化工具安全控制远程主机

## 4. 具体模块职责

### 4.1 `Host Agent`

部署在每台远程主机。

职责：

- 主动向 `ai-server` 发起双向 `gRPC` 长连接
- 打开真实 PTY
- 收发输入输出
- 执行一次性命令
- 上报退出码
- 断线重连
- 心跳保活

关键实现点：

- 每个输出 chunk 带 `seq`
- 支持 `resume_from_seq`
- `exec/start` 底层也尽量走 PTY，不要直接裸 `exec.Command`
- 支持 `cwd`、`env`、`timeout_sec`
- 启动时先 `register`，随后在同一条长连接中持续 `heartbeat`
- 默认普通权限运行，高危操作通过 `sudo` 策略单独处理

### 4.2 `ai-server`

职责：

- 用户登录与 RBAC
- 主机注册中心
- WebSocket 网关
- gRPC Agent Gateway
- Terminal Broker
- Approval Center
- Audit Log
- Codex Gateway
- Remote Tools Adapter

核心子模块建议：

- `host-registry`
- `agent-gateway`
- `terminal-broker`
- `codex-gateway`
- `remote-tools`
- `approval-audit`

### 4.3 `codex app-server`

职责只保留为：

- 线程管理
- 会话历史
- 流式 agent 事件
- 审批请求
- 工具调用编排

不要让它直接管理远程主机连接。

## 5. 远程工具设计

不要给模型一个万能命令工具。

`V1` 推荐模型侧只注册两个物理隔离工具：

### 5.1 `execute_readonly_query`

用于：

- 只读探测
- 默认免审
- 命中只读校验后直接执行

典型动作：

- `df`
- `ps`
- `ss`
- `ls`
- `cat`
- `grep`
- `tail`

### 5.2 `execute_system_mutation`

用于：

- 任何状态变更
- 默认拦截审批

典型动作：

- 安装软件
- 重启服务
- 写文件
- 删除文件
- 执行变更脚本

原因：

- 模型从一开始就知道“只读”和“变更”是两种不同工具
- 免审 / 必审边界清楚
- 后端更容易做执行器隔离、审计统计和会话级授权

`V2` 再在 `execute_system_mutation` 内部继续细化结构化动作，或者继续向模型暴露更高层专用工具，例如：

- `restart_service`
- `package_install`
- `write_file_patch`
- `read_tail_log`

## 6. 审批与审计

推荐规则：

- 只读命令默认免审批
- 有副作用命令必须审批
- `execute_system_mutation` 默认需要审批
- `sudo`、软件安装、服务重启、文件写入、网络变更必须高等级审批

审批卡片至少展示：

- 主机名
- 工具名
- 命令或结构化参数
- 工作目录
- 风险等级
- 发起来源

审计至少记录：

- `user_id`
- `thread_id`
- `host_id`
- `tool_name`
- `command`
- `cwd`
- `approved_by`
- `start_time`
- `end_time`
- `exit_code`
- `output_digest`

## 7. 推荐技术栈

### 7.1 我最推荐的一套

- 前端：`Vue 3 + xterm.js`
- `ai-server`：`Go`
- `Host Agent`：`Go + creack/pty`
- Codex 集成：`codex app-server` 通过 `stdio JSON-RPC`
- 数据库：`PostgreSQL`
- 缓存/会话：`Redis`
- 实时通道：`WebSocket`

适合原因：

- `Go` 很适合业务 App-Server 和主机代理两端
- `xterm.js` 是最稳的浏览器终端方案
- `codex app-server` 用 `stdio` 最贴近官方默认用法

### 7.2 备选方案

如果你团队 Node 更强，也可以：

- 前端：`Vue 3 + xterm.js`
- `ai-server`：`Node.js / NestJS`
- `Host Agent`：仍然建议保持 `Go + creack/pty`

但我仍然建议：

- `Host Agent` 不要改成 Node

## 8. 为什么不建议“直接 SSH”

如果你的目标只是“网页手工登 SSH”，那后端代连 SSH 可以做。

但你现在的目标更大：

- 要让 Codex 代执行
- 要审批
- 要审计
- 要多主机
- 要 NAT 友好
- 要断线重连
- 要后续支持文件操作、服务管理、批量执行

这种情况下，直接 SSH 有几个问题：

- 服务端必须能直连每台机器
- 凭据管理更重
- 会话恢复难做
- Codex 工具层和 SSH channel 耦合太深
- 审计粒度容易失控

所以：

- `MVP` 可以容忍后端 SSH 代理
- 生产版应尽快切到 `Host Agent` 主动外连 + gRPC 长连接模型

## 9. MVP 落地顺序

### 第一阶段

- 前端接入 `xterm.js`
- `ai-server` 建好主机注册和主机列表
- `Host Agent` 跑通 `register/heartbeat`
- 跑通网页终端：`pty/open`、`input`、`resize`、`close`

### 第二阶段

- `ai-server` 接入 `codex app-server`
- 把 `execute_readonly_query` 和 `execute_system_mutation` 作为第一批工具接给 Codex
- 审批卡片接 `tool/requestUserInput`
- 聊天框里支持选择 `host_id`

### 第三阶段

- 增加 `remote_file_read/write`
- 增加 `remote_service_*`
- 增加审计检索
- 增加断线恢复和历史回放

### 第四阶段

- 多主机批量执行
- sudo 分级授权
- 回滚/快照
- 主机标签与策略模板

## 10. 一句话结论

如果你要做的是“Codex 控制远程主机”的生产级网页系统，建议直接定为：

- 浏览器终端：`xterm.js`
- 远程执行协议：`Host Agent -> ai-server` 的双向 `gRPC` 长连接
- Codex 集成协议：`codex app-server` 的 `stdio JSON-RPC`
- 远程主机接入方式：Go 写的 `Host Agent` 主动回连
- 工具模型：`MCP tools` 为主，必要时再评估 `dynamic tools`
- 不把 `ttyd` / `wetty` / `webssh2` 当最终核心，只把它们当参考或 PoC
