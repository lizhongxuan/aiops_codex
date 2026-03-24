# desgin_mvp_0324

## 0. 文档定位

本文定义一个最小可运行的 MVP。

目标不是一次性做完整的“多主机 AI 运维平台”，而是先跑通下面 3 个用户可见能力：

1. 网页上可以配置并登录 GPT 账号
2. 用户可以在页面中使用 Codex 操作服务端本机，默认工作区为 `~/.aiops_codex/`
3. 提供一个 Go 编写的 `Host Agent`，它可以通过 `gRPC` 连接到服务端并完成注册

本文优先考虑：

- 能尽快做出来
- 技术路径明确
- 与现有设计文档不冲突

本文不追求：

- 完整的远程多主机 Codex 控制
- 完整的远程文件 patch 流程
- 完整的多租户凭证隔离
- 完整的审批体系和细粒度授权矩阵

## 1. MVP 范围

## 1.1 In Scope

- 网页登录 GPT 账号
- 网页聊天框接入 `codex app-server`
- Codex 在服务端本机执行命令和文件操作
- 默认工作区为 `~/.aiops_codex/`
- 网页展示基本聊天流、计划、步骤、审批、结果
- Go 版 `Host Agent` 主动向服务端建立双向 `gRPC` 长连接
- 服务端展示 Agent 在线状态

## 1.2 Out of Scope

- 让 Codex 通过 `Host Agent` 真正控制远程主机
- 多主机批量执行
- 复杂的 remote file patch / remote diff 展示
- 远程 sudo 分级授权
- 每用户独立 `codex app-server` 进程
- 每用户独立 `CODEX_HOME`

## 2. MVP 核心结论

### 2.1 GPT 账号登录采用“外部管理 Token”模式

MVP 不采用 `codex app-server` 自己的 `chatgpt` 浏览器登录流作为网页主路径。

原因是：

- 官方 `chatgpt` 登录流会返回一个 `authUrl`
- 该 `authUrl` 的回调默认指向 app-server 本地 callback
- 这个模式更适合本地桌面 / CLI，不适合普通 Web 页面直接承接

因此 MVP 推荐：

- 网页侧先完成 GPT OAuth
- 业务服务端拿到 `idToken` / `accessToken`
- 再通过 `account/login/start(type=chatgptAuthTokens)` 注入给 `codex app-server`

这意味着：

- 网页登录 GPT 账号是支持的
- 但 OAuth 生命周期由你的业务 `App-Server` 管，而不是交给 `codex app-server` 自己托管

### 2.2 MVP 先只让 Codex 操作“服务端本机”

这一步是故意收敛范围。

MVP 中，Codex 的执行目标不是远程 Agent 主机，而是服务端本机。

好处：

- 实现最短路径
- 可以直接复用 `codex app-server` 的本地 command / file 工具
- 审批事件直接使用原生：
  - `item/commandExecution/requestApproval`
  - `item/fileChange/requestApproval`

### 2.3 `Host Agent` 在 MVP 中只负责“接入打通”

MVP 中的 `Host Agent` 重点不是让 Codex 控制远程主机，而是先证明下面链路成立：

- Go Agent 启动
- 主动向服务端发起双向 `gRPC` 长连接
- 完成 `register`
- 周期 `heartbeat`
- 服务端能显示在线状态

也就是：

- `Host Agent` 在 MVP 中先是“主机接入能力”
- 远程执行和 Codex remote tools 放到下一阶段

## 3. MVP 架构

```text
Browser
  ├─ GPT OAuth Login
  ├─ Codex Chat
  ├─ Plan / Step / Approval / Result Cards
  └─ Host List
        │
        │ HTTPS + WebSocket
        ▼
Business App-Server (ai-server)
  ├─ Web API / WS Gateway
  ├─ GPT OAuth Callback / Session
  ├─ Codex Gateway
  ├─ Local Host Executor
  ├─ gRPC Agent Gateway
  └─ Host Registry
        │                        │
        │ stdio JSON-RPC         │ gRPC bidi stream
        ▼                        ▼
  codex app-server           Host Agent
                                 │
                                 └─ runs on remote host
```

## 4. 组件职责

## 4.1 Browser

职责：

- 展示 GPT 登录状态
- 展示主机列表
- 展示 Codex 聊天流
- 展示计划卡片、步骤卡片、审批卡片、结果卡片
- 选择执行目标

MVP 里的执行目标建议固定两类：

- `server-local`
- `agent-hosts` 仅展示在线状态，不支持 Codex 执行

## 4.2 Business App-Server

职责：

- 承接网页请求和 WebSocket
- 管理 GPT OAuth 登录态
- 启动并维护 `codex app-server`
- 把网页消息转成 `turn/start`
- 把 app-server 事件流转成前端卡片数据
- 在本机执行 Codex 产生的命令和文件改动
- 维护与 `Host Agent` 的双向 `gRPC` 长连接
- 维护在线主机注册表

## 4.3 codex app-server

职责：

- 管理 Codex 会话和线程
- 维护 GPT 账号状态
- 处理聊天、计划、审批、流式输出
- 对本机命令和文件操作发出原生审批请求

MVP 中它不负责：

- 远程 Agent 管理
- 远程主机执行

## 4.4 Host Agent

职责：

- 启动后主动连接业务 `App-Server`
- 发送注册信息
- 周期心跳
- 维持在线状态

MVP 中它不负责：

- 被 Codex 调用执行远程命令
- 远程 PTY 打开
- 远程文件操作

这些能力保留到 Phase 2。

## 5. GPT OAuth 设计

## 5.1 推荐模式

MVP 推荐模式：

- 网页完成 GPT OAuth
- 业务 `App-Server` 保存用户 Web 会话和 GPT Token
- `App-Server` 调用 `codex app-server`：
  - `account/login/start(type=chatgptAuthTokens, idToken, accessToken)`

原因：

- 对 Web 场景最可控
- 不依赖 `codex app-server` 本地 callback 直接暴露给用户浏览器
- 更适合后续和你自己的登录体系整合

## 5.2 不推荐的 MVP 模式

不推荐把下面这条路作为 MVP 主路径：

- 网页直接触发 `codex app-server` 的 `chatgpt` 浏览器登录流

原因：

- `authUrl` 的回调指向 app-server 本地 callback
- 对普通 Web 页面来说路由链路不自然
- 调试成本高

## 5.3 最小登录流程

1. 用户打开网页
2. 点击 `Connect GPT`
3. 浏览器走 GPT OAuth
4. 业务 `App-Server` 拿到 `idToken` / `accessToken`
5. `App-Server` 调 `account/login/start`
6. `codex app-server` 返回：
   - `account/login/completed`
   - `account/updated(authMode=chatgptAuthTokens)`
7. 网页显示已登录状态

## 5.4 登录态存储

MVP 建议：

- GPT token 存服务端 session store
- 浏览器只持有业务 session cookie
- 不把 GPT token 暴露给前端业务代码

建议表：

- `web_user_sessions`
- `codex_auth_sessions`

## 6. 服务端本机执行设计

## 6.1 目标主机定义

MVP 里内置一个逻辑主机：

- `server-local`

它表示：

- 当前业务 `App-Server` 所在机器

## 6.2 默认工作区

MVP 默认工作区为：

- `~/.aiops_codex/`

启动时建议：

- 服务端进程确保该目录存在
- 不存在则自动创建

```bash
mkdir -p ~/.aiops_codex
```

## 6.3 Codex 会话初始化

当用户在网页选择 `server-local` 并发起对话时，`ai-server` 调 `thread/start` 时建议使用：

- `cwd = ~/.aiops_codex/`
- `sandboxPolicy.type = workspaceWrite`
- `sandboxPolicy.writableRoots = ["~/.aiops_codex/"]`
- `approvalPolicy = on-request` 或更保守策略

这样可以保证：

- 命令在服务端本机执行
- 文件写权限默认限制在 `~/.aiops_codex/`
- 读系统信息类命令仍可运行

## 6.4 MVP 中允许的操作

MVP 允许 Codex 在服务端本机做：

- 查看系统状态
- 运行只读命令
- 在 `~/.aiops_codex/` 下读写文件
- 执行需要审批的命令

典型例子：

- `pwd`
- `ls`
- `df -h`
- `ps`
- 在 `~/.aiops_codex/` 下创建脚本或配置文件

## 6.5 MVP 中不建议做的事

- 默认让 Codex 直接操作 `/etc`、`/usr/local` 等系统目录
- 默认让 Codex 具备 root 权限
- 默认打开全盘写权限

## 7. 审批设计

因为 MVP 的执行目标是服务端本机，所以审批直接复用 `codex app-server` 原生事件：

- `item/commandExecution/requestApproval`
- `item/fileChange/requestApproval`

MVP 不强求复杂授权矩阵，先支持：

- `Approve once`
- `Decline`

如果你想保留一点延展空间，也可以在 UI 上预留：

- `Approve for session`

但建议 V1 不做复杂 grant 存储逻辑。

## 8. 网页 UI 最小范围

MVP 页面建议只保留 4 个主区域：

### 8.1 GPT 登录区

显示：

- 是否已连接 GPT
- 当前账号邮箱或状态
- 重新连接 / 退出按钮

### 8.2 Host 列表

至少展示：

- `server-local`
- 已连接的 `Host Agent`

其中：

- `server-local` 支持被 Codex 操作
- `Host Agent` 在 MVP 里只显示在线状态

### 8.3 Codex Chat

至少支持这些卡片：

- `MessageCard`
- `PlanCard`
- `StepCard`
- `CommandApprovalCard`
- `FileChangeApprovalCard`
- `ResultCard`

### 8.4 可选日志区

MVP 可以不做完整 Web Terminal。

更轻的做法是：

- 在结果卡片或步骤卡片里展示命令输出摘要

如果你想稍微增强体验，再加一个只读日志面板即可。

## 9. Host Agent MVP 设计

## 9.1 技术栈

- 语言：`Go`
- 连接协议：`gRPC bidirectional streaming`
- PTY 相关依赖：本阶段可先不接 PTY，只保留连接框架

## 9.2 MVP 能力范围

最小能力只做：

- `Connect`
- `register`
- `heartbeat`
- `ping`

建议注册字段：

- `host_id`
- `hostname`
- `os`
- `arch`
- `agent_version`
- `labels`

## 9.3 服务端能力

业务 `App-Server` 至少要支持：

- 接受 Agent 长连接
- 校验 bootstrap token
- 保存在线状态
- 展示最近心跳时间

## 9.4 不在 MVP 中实现

下面这些先不做：

- `pty/open`
- `pty/input`
- `exec/start`
- 远程文件读写
- Codex -> Host Agent 远程执行

这些统一放到 Phase 2。

## 10. 数据模型

MVP 建议的最小表结构：

- `web_users`
- `web_sessions`
- `codex_auth_sessions`
- `codex_threads`
- `approval_requests`
- `hosts`
- `host_agent_connections`

关键字段建议：

- `hosts.kind`: `server_local | agent`
- `hosts.status`: `online | offline`
- `approval_requests.status`: `pending | approved | declined`

## 11. 实施顺序

### Phase 1: GPT 登录打通

- 网页增加 `Connect GPT`
- 服务端完成 GPT OAuth 回调
- 服务端调用 `account/login/start(type=chatgptAuthTokens)`
- 页面展示已登录状态

### Phase 2: 服务端本机 Codex 操作

- 创建默认工作区 `~/.aiops_codex/`
- 接入 `codex app-server`
- 跑通聊天
- 跑通本机命令审批
- 跑通本机文件变更审批

### Phase 3: Host Agent 接入

- 编写 Go 版 `Host Agent`
- 跑通 `gRPC` 长连接
- 跑通注册与心跳
- 页面展示 Agent 在线状态

## 12. 风险与说明

### 12.1 GPT OAuth 是 MVP 最大的不确定点

这里的关键假设是：

- 业务 `App-Server` 能拿到可用于 `chatgptAuthTokens` 模式的 `idToken` / `accessToken`

如果这条链路短期内拿不下来，建议开发阶段先加一个临时 fallback：

- `API Key login`

但产品目标仍然保持 GPT OAuth。

### 12.2 默认工作区是最小安全边界

把 `cwd` 和写权限收敛到 `~/.aiops_codex/`，是 MVP 最重要的安全边界之一。

### 12.3 Host Agent 在 MVP 中不是执行器

MVP 中不要把 `Host Agent` 目标定成“已经能被 Codex 控制远程主机”。

它的任务只是：

- 把 gRPC 接入打通
- 把主机注册模型建立起来

## 13. 一句话总结

这个 MVP 的最小闭环是：

- 网页登录 GPT 账号
- 网页里的 Codex 可以操作服务端本机，默认工作区 `~/.aiops_codex/`
- Go 写的 `Host Agent` 能通过 gRPC 连上服务端并出现在主机列表里

这 3 件事跑通后，再进入下一阶段：

- 让 Codex 通过 `Host Agent` 控制远程主机

## 14. 参考资料

- OpenAI Codex Auth: https://developers.openai.com/codex/auth
- OpenAI Codex App Server: https://developers.openai.com/codex/app-server
- 现有总纲设计：[design-0323.md](/Users/lizhongxuan/Desktop/aiops-codex/design-0323.md)
- 现有工程定版：[design-0323-concrete-plan.md](/Users/lizhongxuan/Desktop/aiops-codex/design-0323-concrete-plan.md)
- 审批交互专项：[design-0323-app-server-approval.md](/Users/lizhongxuan/Desktop/aiops-codex/design-0323-app-server-approval.md)
- UI 与授权专项：[design-0323-ui-authz.md](/Users/lizhongxuan/Desktop/aiops-codex/design-0323-ui-authz.md)
