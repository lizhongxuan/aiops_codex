# Bifrost vs Codex Runtime 差距分析报告

## 概述

本报告基于对 `codex/codex-rs/` 源码的深入分析，对比 `internal/agentloop/` + `internal/bifrost/` 的当前实现，识别出要完美替换 Codex Runtime 还缺少的功能。

tasks.md 中 19 个任务全部标记为 `[x]` 完成，核心 agent loop、bifrost gateway、context compression、tool registry、workspace 管理等基础架构已就位。但 Codex Runtime 的功能远比 tasks.md 覆盖的范围更广。

---

## 一、tasks.md 闭环检查

### 已闭环的功能（实现完整）

| 模块 | 状态 | 说明 |
|------|------|------|
| Bifrost Provider 接口 | ✅ | provider.go 定义完整 |
| OpenAI Provider | ✅ | openai.go 含流式 |
| Anthropic Provider | ✅ | anthropic.go 含格式转换 |
| Ollama Provider | ✅ | ollama.go 走 OpenAI 兼容 |
| Credential Pool | ✅ | credential.go 轮转+冷却 |
| 错误恢复 R1-R5 | ✅ | retry.go + fallback.go |
| 成本追踪 | ✅ | usage.go |
| Context Manager | ✅ | context.go 含 Sanitize |
| Context Compressor L1-L5 | ✅ | compact.go 五层体系 |
| Tool Registry | ✅ | tools.go + tools_remote/coroot/workspace |
| Session 管理 | ✅ | session.go |
| Agent Loop 主循环 | ✅ | loop.go RunTurn |
| 流式消费 | ✅ | stream.go |
| 并行 tool call | ✅ | loop.go shouldParallelizeToolBatch |
| Workspace 多 Session | ✅ | workspace.go |
| Config 扩展 | ✅ | config.go Bifrost 字段 |
| server.go 集成 | ✅ | useBifrost 开关 |
| Codex 依赖移除 | ✅ | internal/codex/ 已删除 |

### 存在风险的闭环项

| 项目 | 风险 | 说明 |
|------|------|------|
| L4 自动压缩 | ⚠️ 中 | Codex 的 compact prompt 很简洁（"Create a handoff summary"），你的 9 维结构化摘要更复杂，但 Codex 实际使用的是远程 compact task（`run_compact_task`），会调用专门的 API 端点做压缩，不只是简单的 LLM 调用 |
| 审批系统 | ⚠️ 低 | 基础审批流完整，但缺少 Codex 的 `ExecPolicy` 细粒度策略引擎（Starlark 规则文件） |
| 流式推送 | ⚠️ 低 | stream.go 有 StreamObserver 接口，但需确认与 WebSocket 的实际集成是否完整 |

---

## 二、Codex 源码中存在但项目缺失的功能

### 🔴 高优先级（核心功能缺失）

#### 1. Subagent / Multi-Agent 系统
**Codex 实现**: `core/src/agent/control.rs` + `tools/handlers/multi_agents.rs`
- 支持 `spawn_agent`、`send_input`、`wait_agent`、`close_agent`、`resume_agent` 五个工具
- 支持 agent 树形结构（parent → children）
- 支持 agent 间通信（inter-agent communication）
- 支持 agent fork（从当前对话分叉出子 agent）
- 支持 agent 深度限制
- 支持 agent 状态订阅
- 支持 agent 配置继承（sandbox、approval policy、provider 等）

**你的项目**: workspace.go 只有 planner/worker 两层，没有通用的 subagent 系统。

**差距**: 这是 Codex 最强大的能力之一。缺少它意味着无法实现并行子任务分解、多 agent 协作、agent 专业化分工。

#### 2. MCP (Model Context Protocol) 本地 Server 管理
**Codex 实现**: `codex-mcp/` 模块
- STDIO MCP server 启动和管理
- Streamable HTTP MCP server 连接
- MCP 工具自动发现和注册
- MCP 工具审批模板
- MCP server 连接管理器（重连、超时）
- OAuth / Bearer Token 认证
- MCP 资源读取（`mcp_resource.rs`）

**你的项目**: tools_coroot.go 硬编码了 7 个 Coroot 工具，没有通用的 MCP 框架。

**差距**: 无法动态加载第三方 MCP server，无法扩展工具生态。

#### 3. Apply Patch（文件变更工具）
**Codex 实现**: `apply_patch.rs` + `codex-rs/apply-patch/`
- 统一的文件变更格式（unified diff）
- 支持文件新增、删除、修改、移动
- 安全检查（assess_patch_safety）
- 与 sandbox 策略集成
- 与审批系统集成
- 转换为 protocol FileChange 用于 UI 展示

**你的项目**: tools_remote.go 有 `write_file` 但没有 patch/diff 级别的文件变更。

**差距**: 缺少结构化的文件变更能力，无法做精确的 diff 预览和审批。

#### 4. Turn Diff Tracker（变更追踪）
**Codex 实现**: `turn_diff_tracker.rs`
- 每个 turn 开始时记录文件基线（git blob SHA1）
- turn 结束时生成 unified diff
- 支持 git root 发现和相对路径
- 支持文件模式检测（regular/symlink/executable）

**你的项目**: 无对应实现。

**差距**: 无法在 turn 结束时展示"这一轮 AI 改了什么"的 diff 视图。

#### 5. Skills 系统
**Codex 实现**: `skills.rs` + `codex-rs/skills/` + `codex-rs/core-skills/`
- SKILL.md 文件发现和加载
- 显式触发（`$skill-name`）
- 隐式触发（根据 description 匹配命令）
- Skill 依赖解析（环境变量等）
- Skill 注入到 system prompt
- Skill 作用域（user/repo/system/admin）
- Skill 遥测

**你的项目**: 无对应实现。

**差距**: 无法通过 skill 扩展 agent 的专业能力。

---

### 🟡 中优先级（增强功能缺失）

#### 6. Hook Runtime（钩子系统）
**Codex 实现**: `hook_runtime.rs`
- `session_start` 钩子
- `pre_tool_use` 钩子
- `post_tool_use` 钩子
- `user_prompt_submit` 钩子
- 钩子可注入额外上下文到对话
- 钩子可修改/拦截工具调用

**你的项目**: 无对应实现（Kiro 有自己的 hook 系统，但 agent loop 内部没有）。

**差距**: 无法在 agent loop 内部实现自定义拦截和注入逻辑。

#### 7. Guardian（安全守卫）
**Codex 实现**: `guardian/` 模块
- 独立的安全审查 agent
- 对工具调用进行风险评估
- 支持 MCP 工具审批注解
- 支持命令审批和 execve 审批
- 审查结果可阻止工具执行

**你的项目**: 审批系统只有简单的 approve/reject，没有 AI 驱动的安全审查。

**差距**: 缺少自动化的安全风险评估层。

#### 8. ExecPolicy（执行策略引擎）
**Codex 实现**: `exec_policy.rs` + `codex-rs/execpolicy/`
- Starlark 规则文件定义命令执行策略
- 支持 allow/deny/prompt 三种决策
- 支持策略动态修正（amendment）
- 支持网络策略规则
- 支持策略文件热加载

**你的项目**: 审批策略是简单的字符串配置（`ApprovalPolicy`），没有规则引擎。

**差距**: 无法实现细粒度的命令白名单/黑名单策略。

#### 9. Environment Context（环境上下文注入）
**Codex 实现**: `environment_context.rs`
- 每个 turn 注入 cwd、shell、日期、时区、网络策略、subagent 状态
- XML 格式序列化
- turn 间差异检测（只注入变化的部分）

**你的项目**: session.go 的 BuildSystemPrompt 只在初始化时构建，不会每 turn 更新环境上下文。

**差距**: AI 无法感知运行时环境变化（如 cwd 切换、时间变化）。

#### 10. File Watcher（文件监控）
**Codex 实现**: `file_watcher.rs`
- 监控工作区文件变更
- 支持递归/非递归监控
- 事件去重和节流
- 多订阅者模式

**你的项目**: 无对应实现。

**差距**: 无法感知用户在 agent 运行期间手动修改的文件。

#### 11. Memory / Session Persistence（记忆系统）
**Codex 实现**: `memories/` 模块 + `memory_trace.rs`
- 会话记忆持久化到磁盘
- 记忆摘要生成（通过 LLM）
- 跨会话记忆引用
- 记忆合并和去重
- 两阶段记忆处理（phase1 + phase2）

**你的项目**: Session 是纯内存的，重启后丢失。

**差距**: 无法实现 `resume` / `fork` 等会话恢复功能。

#### 12. Network Policy（网络策略）
**Codex 实现**: `network_policy_decision.rs` + `codex-rs/network-proxy/`
- 网络访问控制（allow/deny 域名）
- 网络代理
- 网络审批流程
- 策略动态修正

**你的项目**: 无对应实现。

**差距**: 无法控制 agent 的网络访问范围。

---

### 🟢 低优先级（锦上添花）

#### 13. Web Search 工具
**Codex 实现**: `web_search.rs`
- 搜索、打开页面、页面内查找
- 多查询支持

**你的项目**: 无对应实现。

#### 14. Tool Search（工具搜索）
**Codex 实现**: `tool_search.rs`
- BM25 全文搜索引擎
- 按名称、描述、参数搜索工具
- 当工具数量很多时帮助 AI 找到合适的工具

**你的项目**: 无对应实现。

#### 15. Commit Attribution（提交归属）
**Codex 实现**: `commit_attribution.rs`
- 自动在 git commit message 中添加 Co-authored-by trailer

**你的项目**: 无对应实现。

#### 16. JS REPL
**Codex 实现**: `tools/handlers/js_repl.rs` + `tools/js_repl/`
- 内置 JavaScript 运行时（V8）
- 支持在对话中执行 JS 代码

**你的项目**: 无对应实现。

#### 17. Fuzzy File Search
**Codex 实现**: `codex-rs/file-search/` + `app-server/src/fuzzy_file_search.rs`
- 模糊文件名搜索
- 支持会话式搜索（start/update/stop）

**你的项目**: tools_remote.go 有 `search_files` 但是通过远程主机 grep 实现。

#### 18. Realtime / WebRTC
**Codex 实现**: `codex-rs/realtime-webrtc/` + `realtime_context.rs`
- WebRTC 实时通信
- 实时对话上下文管理

**你的项目**: 无对应实现。

#### 19. Collaboration Mode
**Codex 实现**: `codex-rs/collaboration-mode-templates/`
- 多人协作模板

**你的项目**: 无对应实现。

#### 20. Connectors
**Codex 实现**: `codex-rs/connectors/`
- 外部服务连接器框架

**你的项目**: 无对应实现。

---

## 三、优先级建议

### 第一批（建议立即实施）

1. **Subagent 系统** — 这是 Codex 最核心的差异化能力，也是你 workspace 模式的自然升级
2. **MCP 框架** — 没有通用 MCP 支持，工具生态无法扩展
3. **Session Persistence** — 没有持久化就没有 resume/fork，用户体验断层

### 第二批（建议短期实施）

4. **Apply Patch + Turn Diff Tracker** — 文件变更的结构化展示和审批
5. **Environment Context 注入** — 让 AI 感知运行时环境变化
6. **ExecPolicy 规则引擎** — 细粒度的命令执行策略

### 第三批（建议中期实施）

7. **Skills 系统** — 可扩展的 agent 专业能力
8. **Hook Runtime** — agent loop 内部的拦截和注入
9. **Guardian 安全守卫** — AI 驱动的安全审查
10. **File Watcher** — 感知外部文件变更

### 第四批（按需实施）

11. Web Search、Tool Search、Commit Attribution、JS REPL 等

---

## 四、总结

tasks.md 的 19 个任务确实都已完成，**基础架构闭环没有问题**。Bifrost Gateway + Agent Loop + Context Compression + Tool Registry + Workspace 的核心链路是通的。

但要"完美替换 Codex Runtime"，还有约 **20 个功能模块** 需要移植，其中 **Subagent 系统、MCP 框架、Session Persistence** 是最关键的三个缺口。

从工程量估算：
- 第一批（3 个模块）：约 2-3 周
- 第二批（3 个模块）：约 1-2 周
- 第三批（4 个模块）：约 2-3 周
- 总计：约 5-8 周可达到 Codex Runtime 90%+ 的功能覆盖
