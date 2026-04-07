# Claude Code 学习笔记 × 本项目对照分析：深度审核版

> 对前一版分析报告的每条建议做严格审核：是否真的有用、改了会不会引入新问题、投入产出比是否合理。
> 判定标准：✅ 值得做 / ⚠️ 有条件地值得

---

## 一、Prompt 优化（零代码逻辑改动）

### 1.1 行为约束注入（知识点 22）

**原建议**：在 SystemPrompt.Content 中添加运维行为约束（最小变更、先检查后操作、失败先诊断等）。

**审核判定：⚠️ 有条件地值得，但需要注意 3 个问题**

问题 A — prompt 膨胀与 Codex 的 DeveloperInstructions 传递机制冲突：
`renderMainAgentDeveloperInstructions()` 的输出同时传给 `thread/start` 和 `turn/start`。
thread 创建时传一次（`ensureThreadWithSpec`），之后每个 turn 又传一次（`requestTurnWithSpec`）。
如果 SystemPrompt.Content 从 3 行膨胀到 20 行，每个 turn 的 `developerInstructions` 参数都会变大。
Codex（OpenAI Responses API）对 `developerInstructions` 的处理方式是每 turn 覆盖，不是追加。
所以膨胀的 prompt 不会累积，但会增加每次 API 调用的 token 消耗。

问题 B — 约束可能与 Codex 自身的行为指令冲突：
Codex 内部已经有自己的 system prompt（包括工具使用规范、安全约束等）。
你注入的 "失败后先诊断" 可能和 Codex 内部的 "遇到错误自动重试" 产生矛盾。
没有办法预测 Codex 内部 prompt 的内容，所以存在不可控的指令冲突风险。

问题 C — 约束的粒度问题：
"最小变更原则" 对编码 agent 有意义，但对运维 agent 可能过于保守。
运维场景中，用户说 "修复 nginx 502" 时，agent 可能需要同时检查配置、重启服务、查看上游——这不是 "最小变更"。
建议改为运维特化的约束，而不是照搬 Claude Code 的编码约束。

**修正建议**：只添加 3 条运维特化约束，控制在 100 字以内：
```
修改配置前先备份原文件。
命令失败后先查日志再决定下一步，不要盲目重试。
不同主机的路径和服务名可能不同，不要假设。
```

---

### 1.2 输出效率指令（知识点 24）

**原建议**：在 turn 级 prompt 中添加输出格式要求。

**审核判定：⚠️ 有条件地值得，但有一个隐藏问题**

隐藏问题 — turnScoped 分支的实际作用：
`renderMainAgentDeveloperInstructions(profile, hostID, true)` 中 `turnScoped=true` 时，
当前只多加了一行 `"Summarize execution results clearly for the web UI."`。
如果在这里加更多格式指令，它们会出现在每个 turn 的 `developerInstructions` 中，
但不会出现在 thread 创建时的指令中。这意味着：
- 第一个 turn 之前，Codex 不知道这些格式要求
- 如果 Codex 在 thread 级别缓存了 developerInstructions，turn 级别的覆盖可能不生效

**修正建议**：格式指令应该放在 thread 级别（`turnScoped=false` 的分支），而不是 turn 级别。
这样 Codex 从 thread 创建开始就知道输出格式要求。

---

## 二、Hook 系统简化（知识点 4）

**原建议**：在 handleCodexNotification 中插入 hook 调用点。

**审核判定：⚠️ 有条件地值得，但复杂度被严重低估**

问题 A — hook 执行的时序问题：
`handleCodexNotification` 是同步处理 Codex 推送的事件。
如果 hook 需要执行异步操作（比如 "命令执行后触发告警"），
它不能阻塞 notification 处理，否则会延迟后续事件的处理。
需要一个异步 hook 执行器，这不是 "小改动"。

问题 B — hook 失败的影响：
如果 PreToolUse hook 失败了（比如 "检查磁盘空间" 的命令超时），
应该阻止工具执行还是忽略 hook 失败？
Claude Code 的 hook 是在进程内同步执行的，失败处理简单。
本项目的 hook 可能涉及远程主机操作，失败模式复杂得多。

问题 C — 当前审批系统已经覆盖了核心场景：
`handleCodexServerRequest` 中的审批流已经实现了 PreToolUse 的核心功能（执行前检查+用户确认）。
PostToolUse 的审计日志已经通过 `auditApprovalLifecycleEvent` 实现。
Stop Hook 的 "自动追加" 功能可以通过更简单的方式实现（在 `handleMissionTurnCompleted` 中添加逻辑）。

**修正建议**：不要建通用 hook 系统。只在 `handleMissionTurnCompleted` 中添加一个 "auto-followup" 检查，
用于 mission 完成后自动汇总。这是 hook 系统 80% 的价值，但只需要 20% 的复杂度。

---

## 三、性能瓶颈（原报告遗漏）

### 3.1 Snapshot 全量推送

`broadcastSnapshot()` 每次都序列化完整的 Snapshot（包含所有 Cards、Approvals、Hosts），
通过 WebSocket 推送给所有连接的客户端。在以下场景中这是严重问题：
- 长对话（50+ Cards）：每次 delta 事件都推送全量 Cards
- 命令输出流式追加（`item/commandExecution/outputDelta`）：每个 delta 都触发全量推送
- 多客户端连接：每个客户端都收到完整 snapshot

### 3.2 outputDelta 广播风暴

每个 outputDelta 事件都调用 `broadcastSnapshot(sessionID)`。
如果一个命令输出 1000 行，就会触发 1000 次全量 snapshot 推送。
`throttledBroadcast` 只用在了 workspace route thread 上，普通 session 没有节流。

### 3.3 Session transcript 全量序列化

`SaveSessionTranscript()` 每次都 `json.MarshalIndent` 整个 Cards 数组。
如果 Cards 中包含大量命令输出，这个操作的 I/O 开销很大。
而且 `scheduleSessionPersistence` 的防抖只有一个 timer，
高频 Card 更新时可能导致频繁的全量写入。

---

## 最终修改建议

经过三轮分析（对照 → 审核 → 收敛），以下是最终建议。分为"现在就做"和"排期做"两档。

### 现在就做

#### ① outputDelta 广播节流

改动最小、收益最大的一条。

**改法**：`internal/server/server.go` — `handleCodexNotification()` 中 `item/commandExecution/outputDelta` 和 `item/fileChange/outputDelta` 两个 case，把 `a.broadcastSnapshot(sessionID)` 替换为 `a.throttledBroadcast(sessionID)`。

**风险**：极低。`throttledBroadcast` 已在 workspace route 生产使用。命令输出流式展示从逐字符变为批量刷新，体感更流畅。

---

#### ② 运维行为约束注入

**改法**：`internal/model/types.go` — `defaultAgentProfile()` 中 Main Agent 的 `SystemPrompt.Content` 追加 3 行：

```
Back up config files before modifying them.
When a command fails, check logs and status before retrying.
Do not assume paths or service names are the same across different hosts.
```

**风险**：低。增加约 30 token。与 Codex 内部指令冲突的理论风险存在，但这 3 条都是事实性约束，不太可能矛盾。上线后观察 1-2 周。

---

#### ③ 输出格式指令（thread 级别）

**改法**：`internal/server/server.go` — `renderMainAgentDeveloperInstructions()` 中，在 `contextLines`（`turnScoped=false` 通用分支）追加：

```go
contextLines = append(contextLines,
    "Lead with conclusions, then supporting details.",
    "For multi-host results, use structured format (tables or lists).",
    "Show only key lines from command output, not the full dump.",
)
```

**风险**：低。增加约 30 token。放在 thread 级别确保 Codex 从 thread 创建起就知道格式要求。

---

### 排期做

#### ④ Snapshot 中 Card.Output 截断

**改法**：
- `internal/model/types.go` — Card 增加 `OutputTruncated bool` 和 `FullOutputSize int`
- `internal/server/server.go` — `snapshot()` 中截断 Output 到 4KB，标记截断状态
- 新增 `GET /api/v1/cards/{id}/output` API 返回完整 Output
- 前端 TerminalCard/CodeCard 适配按需加载

**风险**：低。完整 Output 仍在内存中，只是推送时截断。前端需要适配。

---

#### ⑤ Mission 完成后自动汇总

**改法**：`internal/server/orchestrator_integration.go` — `handleMissionTurnCompleted()` 中，mission 状态变为 completed 时自动向 workspace session 发送汇总请求。加 `autoSummaryAttempted` 标记，只尝试一次。

**风险**：中。汇总 turn 本身也可能失败，需要确保不会无限重试。

---

#### ⑥ Session transcript 改为追加写入

**改法**：`SaveSessionTranscript()` 改为 JSONL 追加写入，只写新增/变更的 Card。

**风险**：中。Card 有更新操作（`UpdateCard`），JSONL 需要处理同 ID 多条记录取最后一条的逻辑，加载时需要 merge。

---

#### ⑦ 前端连续失败限流

**改法**：`web/src/pages/ChatPage.vue` — `sendMessage()` 中记录连续失败次数，连续 3 次后禁用发送按钮 30 秒，提示"连接不稳定，请稍后重试"。

**风险**：低。纯前端改动。

---

*①②③ 可在一个 PR 中完成，改动量约 10 行代码。④-⑦ 按需排期。*
