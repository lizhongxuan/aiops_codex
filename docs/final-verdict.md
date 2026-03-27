# 最终结论：不迁移，继续在当前项目上开发

## 为什么不再摇摆

看完你的代码后，答案很清楚了。

你已经有一套完整的、跑通了的架构：

```
ai-server (Go)
  ├── Codex app-server 桥接 (stdio JSON-RPC)
  ├── HTTP API + WebSocket (前端通信)
  ├── gRPC 双向流 (host-agent 接入)
  ├── 会话管理 (多 session, 持久化, transcript)
  ├── 审批流 (command/file approval + grant)
  ├── 终端管理 (本地 + 远程, WebSocket 转发)
  ├── 远程命令执行 (exec, 带超时/取消)
  ├── 远程文件操作 (list/read/search/write)
  ├── 主机管理 (注册/心跳/离线检测)
  ├── 审计日志 (JSONL)
  └── 安全 (TLS mTLS, bootstrap token, CIDR, host allowlist)

host-agent (Go)
  ├── gRPC 双向流连接 ai-server
  ├── 终端 (open/input/resize/signal/close)
  ├── 命令执行 (start/cancel, stdout 回传)
  ├── 文件操作 (list/read/search/write)
  ├── 心跳 (10s 间隔)
  └── 自动重连 (3s 退避)

web (Vue 3)
  ├── 聊天界面
  ├── 终端 (xterm.js)
  ├── 审批卡片
  └── 多会话管理
```

这就是你自己的 "Node Host" 模式。跟 OpenClaw 的 Node Host 本质一样，
但你用的是 gRPC 双向流而不是 WebSocket，而且你已经有：
- 远程终端（OpenClaw 的 system.run）
- 远程文件操作（OpenClaw 没有的）
- 命令审批（OpenClaw 的 exec-approvals）
- 主机注册/心跳/离线检测（OpenClaw 的 device pairing）
- TLS + mTLS + CIDR 白名单（比 OpenClaw 的安全模型更完整）

**你已经走在 OpenClaw 前面了，在运维这个垂直场景上。**

迁移到 OpenClaw 意味着：
1. 扔掉 3900+ 行已验证的 Go 服务端代码
2. 扔掉 gRPC 双向流换成 WebSocket（降级）
3. 扔掉 mTLS 换成 token auth（降级）
4. 扔掉远程文件操作能力（OpenClaw Node Host 没有）
5. 受制于 OpenClaw 的单进程 Node.js Gateway
6. 受制于 OpenClaw 的个人助手设计哲学

这不是改造，这是倒退。

---

## 你现在缺什么，怎么补

你当前项目的核心缺口不在底层基础设施，而在上层运维智能逻辑。
按优先级排：

### P0: 多会话 Agent 调度（你说的"主 Agent 拆任务给子 Agent"）

当前状态：一个 Codex app-server 进程，一个 thread 对应一个会话。
没有"主 Agent 规划 → 子 Agent 执行"的调度层。

要做的事：

```
internal/orchestrator/
  ├── planner.go        # 接收任务，调用 Codex 做规划，输出 DAG
  ├── dag.go            # DAG 数据结构 + 状态机
  ├── dispatcher.go     # 把 DAG 节点分发到对应 host 的 session
  └── collector.go      # 收集子任务结果，推进 DAG
```

实现思路：
- planner 用一个独立的 Codex thread，system prompt 注入规划能力
- 每个子任务创建一个新的 Codex thread，绑定目标 host
- thread 之间通过 orchestrator 在 Go 层协调，不需要 LLM 层的 A2A
- 这比 OpenClaw 的 sessions_send 更可控——你在 Go 层做调度，
  不依赖 LLM 自己决定什么时候发消息给谁

### P1: Playbook / Pipeline 引擎（你说的 Runner）

当前状态：只有 Codex 驱动的自由执行，没有确定性多步骤流程。

要做的事：

```
internal/runner/
  ├── playbook.go       # YAML playbook 解析
  ├── executor.go       # 步骤执行器（调用 host-agent 的 exec）
  ├── approval.go       # 审批门控（复用现有审批流）
  ├── batch.go          # 批量分发（并行 + 分批 + 限速）
  └── failover.go       # 失败回退到 Agent
```

你已经有 remote exec 的完整实现，playbook 引擎就是在上面加一层
YAML 解析 + 步骤编排 + 审批门控。工作量不大。

### P1: Coroot 集成

```
internal/coroot/
  ├── client.go         # Coroot REST API 客户端
  ├── query.go          # 指标查询
  └── rca.go            # RCA 报告生成
```

然后在 Codex 的 dynamic tools 里注册 coroot 相关工具，
让 AI 能直接调用。你已经有 `internal/server/dynamic_tools.go`，
这个扩展点是现成的。

### P2: 记忆系统

```
internal/memory/
  ├── host_profile.go   # 主机画像（Markdown 文件）
  ├── experience.go     # 运维经验库
  └── search.go         # SQLite + sqlite-vec 向量搜索
```

每个 host 一个目录，存画像和运维日志。
向量搜索用 SQLite，不需要外部数据库。

### P2: 经验沉淀

Agent 成功完成运维后，自动提炼经验 → 存入经验库 → 关联 playbook。
这个可以后做，先把前面的跑通。

---

## 你提到的"AI 系统能针对某个需求搭建 AI 工程"

这个更大的愿景，你当前的架构完全能支撑：

```
用户提需求: "我要一个能自动修复 nginx 的运维系统"
    │
    ▼
主 Agent (Codex thread, planner prompt)
    │  理解需求 → 生成 DAG:
    │    1. 查询 Coroot 获取 nginx 相关告警和指标
    │    2. 检索经验库匹配已有 playbook
    │    3. 如果有 playbook → 用 runner 执行
    │    4. 如果没有 → 拆分子任务给对应 host agent
    │    5. 收集结果 → 沉淀经验
    │
    ▼
orchestrator (Go 层调度)
    │  创建子 thread → 绑定 host → 注入 host 画像
    │
    ├──→ Host Agent web01: Codex thread + SSH 终端
    ├──→ Host Agent web02: Codex thread + SSH 终端
    └──→ Runner: 批量执行 playbook 到 N 台机器
```

关键点：**调度在 Go 层，智能在 Codex 层，执行在 host-agent 层。**
三层分离，每层做自己擅长的事。

这比 OpenClaw 的"全靠 LLM 自己 sessions_send 来协调"要靠谱得多。
LLM 负责思考和决策，Go 负责可靠的调度和状态管理。

---

## 总结

不迁移。你的项目在运维场景上已经比 OpenClaw 更完整。
接下来的工作是在现有基础上加 orchestrator + runner + coroot + memory 四个模块。
预计 6-8 周能跑通完整闭环。
