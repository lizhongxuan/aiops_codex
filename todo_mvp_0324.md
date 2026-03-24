# todo_mvp_0324

## 0. 文档定位

本文是 [desgin_mvp_0324.md](/Users/lizhongxuan/Desktop/aiops-codex/desgin_mvp_0324.md) 的实施任务清单。

目标不是展开长期平台能力，而是把 MVP 拆成一组可以直接排期、执行、验收的任务。

## 1. MVP 交付目标

- [x] 网页支持配置并登录 GPT 账号
- [x] 网页支持使用 Codex 操作服务端本机
- [x] 默认工作区固定为 `~/.aiops_codex/`
- [x] 服务端接入 `codex app-server`
- [x] 网页支持最小聊天与审批卡片流
- [x] 提供 Go 版 `Host Agent`
- [x] `Host Agent` 可以通过 `gRPC` 长连接接入服务端
- [x] 页面可以展示 `server-local` 和 Agent 在线状态

## 2. 任务拆分原则

- [x] 所有写操作默认限制在 `~/.aiops_codex/`
- [x] MVP 不做 Codex 控制远程 Agent 主机
- [x] MVP 不做完整 Web Terminal
- [x] MVP 不做复杂授权矩阵
- [x] 审批先只支持 `Approve once` 和 `Decline`

## 3. Phase 0: 项目骨架与基础能力

- [x] 确认服务端项目结构，划分 `web`、`ai-server`、`agent` 三个模块目录
- [x] 确认前端技术栈为 `Vue 3`
- [x] 确认服务端语言与运行方式
- [x] 确认 `codex app-server` 的启动方式与部署方式
- [x] 增加 MVP 配置项文档
- [x] 增加本地开发启动说明

建议至少包含这些配置项：

- [x] `CODEX_APP_SERVER_PATH`
- [x] `GPT_OAUTH_CLIENT_ID`
- [x] `GPT_OAUTH_CLIENT_SECRET`
- [x] `GPT_OAUTH_REDIRECT_URL`
- [x] `APP_SESSION_SECRET`
- [x] `HOST_AGENT_BOOTSTRAP_TOKEN`
- [x] `DEFAULT_WORKSPACE=~/.aiops_codex/`

## 4. Phase 1: GPT 登录打通

### 4.1 前端任务

- [x] 增加 `Connect GPT` 登录入口
- [x] 增加 GPT 登录状态展示区域
- [x] 增加已登录账号信息展示
- [x] 增加重新连接按钮
- [x] 增加退出登录按钮
- [x] 未登录时限制进入 Codex 会话页面

### 4.2 服务端任务

- [x] 实现 GPT OAuth 发起接口
- [x] 实现 GPT OAuth 回调接口
- [x] 校验 OAuth 回调参数和错误分支
- [x] 将 `idToken` / `accessToken` 写入服务端 session store
- [x] 建立 `web_sessions` 与 `codex_auth_sessions` 关联关系
- [x] 封装 `account/login/start(type=chatgptAuthTokens)` 调用
- [x] 处理 `account/login/completed`
- [x] 处理 `account/updated(authMode=chatgptAuthTokens)`
- [x] 将登录结果同步回前端

### 4.3 验收任务

- [ ] 用户完成 GPT 登录后，页面显示已连接状态
- [x] 页面刷新后仍能恢复登录态
- [ ] 登录失败时页面能展示明确错误信息
- [x] 服务端不会把 GPT token 直接暴露给前端

## 5. Phase 2: codex app-server 接入

### 5.1 服务端进程管理

- [x] 封装 `codex app-server` 的启动逻辑
- [x] 封装 `codex app-server` 的健康检查逻辑
- [x] 封装 `stdio JSON-RPC` 双向收发
- [x] 实现 app-server 生命周期管理
- [x] 实现异常退出后的日志记录

### 5.2 会话管理

- [x] 设计网页会话到 Codex thread 的映射
- [x] 支持为当前用户创建或恢复 thread
- [x] 保存 `thread_id`
- [x] 保存当前选中的执行目标
- [x] 保存最近一次会话时间

### 5.3 验收任务

- [x] 服务端可成功启动 `codex app-server`
- [x] 服务端可完成初始化握手
- [ ] 前端发出的消息可进入 Codex thread
- [ ] Codex 返回的消息可流式展示到页面

## 6. Phase 3: server-local 本机执行闭环

### 6.1 工作区初始化

- [x] 服务启动时检查 `~/.aiops_codex/` 是否存在
- [x] 目录不存在时自动执行 `mkdir -p ~/.aiops_codex`
- [x] 将默认工作区写入运行配置

### 6.2 thread/start 初始化参数

- [x] `cwd` 固定为 `~/.aiops_codex/`
- [x] `sandboxPolicy.type` 设为 `workspaceWrite`
- [x] `sandboxPolicy.writableRoots` 限制为 `["~/.aiops_codex/"]`
- [x] `approvalPolicy` 设为 `on-request` 或更保守值

### 6.3 本机执行链路

- [x] 支持用户选择 `server-local`
- [x] 将主机选择信息注入 Codex 会话上下文
- [ ] 跑通只读命令执行
- [ ] 跑通 `~/.aiops_codex/` 下文件创建和修改
- [ ] 跑通命令输出回传
- [ ] 跑通文件变更结果回传

### 6.4 安全边界

- [x] 禁止默认写入 `~/.aiops_codex/` 之外的目录
- [x] 禁止默认使用 root 权限
- [x] 禁止默认开放全盘写权限
- [x] 增加关键操作日志

### 6.5 验收任务

- [ ] 页面可让 Codex 在 `server-local` 执行 `pwd`
- [ ] 页面可让 Codex 在 `server-local` 执行 `ls`
- [ ] 页面可让 Codex 在 `~/.aiops_codex/` 下创建文件
- [ ] 命令和文件变更都能正确显示结果

## 7. Phase 4: 审批流 MVP

### 7.1 服务端任务

- [x] 监听 `item/commandExecution/requestApproval`
- [x] 监听 `item/fileChange/requestApproval`
- [x] 将审批事件归一化成前端卡片数据
- [x] 实现批准接口
- [x] 实现拒绝接口
- [x] 实现审批请求状态存储

### 7.2 前端任务

- [x] 实现 `CommandApprovalCard`
- [x] 实现 `FileChangeApprovalCard`
- [x] 卡片展示命令或文件变更摘要
- [x] 支持 `Approve once`
- [x] 支持 `Decline`
- [x] 用户点击后更新卡片状态

### 7.3 验收任务

- [ ] 命令审批请求能在页面弹出卡片
- [ ] 文件变更审批请求能在页面弹出卡片
- [ ] 用户批准后任务继续执行
- [ ] 用户拒绝后任务终止并展示结果

## 8. Phase 5: 网页最小 UI

### 8.1 页面结构

- [x] 增加 GPT 登录区
- [x] 增加 Host 列表区
- [x] 增加 Codex Chat 区
- [x] 增加可选日志区或结果摘要区

### 8.2 卡片组件

- [x] 实现 `MessageCard`
- [x] 实现 `PlanCard`
- [x] 实现 `StepCard`
- [x] 实现 `ResultCard`
- [x] 实现 `CommandApprovalCard`
- [x] 实现 `FileChangeApprovalCard`

### 8.3 PlanCard 交互

- [x] 支持展示任务清单
- [x] 支持进行中任务高亮
- [x] 支持已完成任务用中横线划掉
- [x] 同一轮对话复用同一张 `PlanCard`

### 8.4 Host 列表

- [x] 展示固定主机 `server-local`
- [x] 展示已连接 Agent 主机
- [x] 展示主机在线状态
- [x] 明确区分“可执行”和“仅在线展示”

### 8.5 验收任务

- [ ] 用户可在单页面内完成登录、选主机、发消息、审批、看结果
- [ ] 卡片流顺序清晰
- [ ] 未登录和已登录状态切换正确

## 9. Phase 6: Host Agent MVP

### 9.1 gRPC 协议与接口

- [x] 定义 Agent `register` 消息
- [x] 定义 `heartbeat` 消息
- [x] 定义 `ping` 消息
- [x] 定义服务端返回的 `ack` 结构
- [ ] 生成 `Go` gRPC 代码

建议注册字段至少包含：

- [x] `host_id`
- [x] `hostname`
- [x] `os`
- [x] `arch`
- [x] `agent_version`
- [x] `labels`

### 9.2 Agent 客户端实现

- [x] 初始化 Agent 配置加载
- [x] 实现 bootstrap token 读取
- [x] 实现到服务端的 gRPC 长连接
- [x] 启动后自动发送 `register`
- [x] 定时发送 `heartbeat`
- [x] 支持基础 `ping`
- [x] 增加重连机制
- [x] 增加本地日志输出

### 9.3 服务端接入

- [x] 增加 gRPC Agent Gateway
- [x] 校验 Agent bootstrap token
- [x] 保存 Agent 注册信息
- [x] 保存最近心跳时间
- [x] 维护在线状态
- [x] 将在线状态同步给前端 Host 列表

### 9.4 验收任务

- [x] Agent 启动后可出现在 Host 列表
- [x] 页面可显示最近心跳时间
- [x] Agent 断开后状态可切换为离线
- [x] Agent 重连后状态可恢复为在线

## 10. Phase 7: 数据表与存储

- [ ] 创建 `web_users`
- [ ] 创建 `web_sessions`
- [ ] 创建 `codex_auth_sessions`
- [ ] 创建 `codex_threads`
- [ ] 创建 `approval_requests`
- [ ] 创建 `hosts`
- [ ] 创建 `host_agent_connections`

关键字段补充：

- [ ] `hosts.kind`
- [ ] `hosts.status`
- [ ] `approval_requests.status`
- [ ] `approval_requests.request_type`
- [ ] `codex_threads.thread_id`
- [ ] `codex_auth_sessions.auth_mode`

## 11. 联调与测试清单

- [ ] 测试 GPT 登录成功流程
- [ ] 测试 GPT 登录失败流程
- [ ] 测试页面刷新后的会话恢复
- [ ] 测试 `server-local` 对话链路
- [ ] 测试命令审批流程
- [ ] 测试文件变更审批流程
- [ ] 测试 `~/.aiops_codex/` 写入边界
- [x] 测试 Agent 注册成功流程
- [ ] 测试 Agent 心跳超时流程
- [x] 测试 Agent 重连流程

## 12. MVP Definition of Done

- [ ] 网页可以完成 GPT 登录
- [ ] 登录后的用户可以发起 Codex 对话
- [ ] Codex 可以在 `server-local` 上工作
- [x] 默认工作区固定为 `~/.aiops_codex/`
- [ ] 命令审批和文件审批可在页面完成
- [x] 页面能显示最小卡片流
- [x] Go 版 `Host Agent` 可以通过 gRPC 连上服务端
- [x] 页面可看到 `server-local` 与 Agent 在线状态

## 13. Phase 2 之后再做

- [ ] Codex 通过 `Host Agent` 控制远程主机
- [ ] 远程 PTY 和 Web Terminal
- [ ] 远程文件 patch / diff
- [ ] 多主机批量执行
- [ ] 更细粒度授权矩阵
- [ ] `Approve for session`
- [ ] 更完整的审计体系
