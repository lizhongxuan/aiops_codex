`12# Claude Code 源码学习笔记：可借鉴的知识点

> 基于 Claude Code v2.1.88 源码（4756 个源文件）的深度分析。按"可迁移性"分类，每个知识点标注源码位置和核心思路。

---

## 一、Agent 架构层

### 1. ReAct 循环工程化

**源码**: `src/query.ts` — `queryLoop()`

核心是一个 `while(true)` 循环，每轮经历 7 个阶段：

```
上下文预处理 → 附件注入 → API调用(流式) → 错误恢复 → 工具执行 → 后处理 → 循环决策
```

关键设计：
- `needsFollowUp` 标志控制循环是否继续（模型输出了 tool_use 就继续）
- `StreamingToolExecutor`：边接收流式响应边执行工具，不等模型输出完
- 并行工具调用：无依赖的工具同时执行，有依赖的串行
- 每轮迭代的 state 用一个对象传递，continue 时整体替换

**可借鉴**：任何 agent 系统都需要这种结构化的循环设计，而不是简单的递归调用。

### 2. 多级错误恢复

**源码**: `src/query.ts` 第 1000-1300 行

```
prompt_too_long 错误
  → 尝试1: Context Collapse drain（释放暂存上下文）
  → 尝试2: Reactive Compact（紧急全量压缩）
  → 尝试3: 截断最旧消息重试
  → 报错给用户

max_output_tokens 错误
  → 尝试1: 升级限制 8K → 64K
  → 尝试2: 注入恢复消息 "Resume directly, break into smaller pieces"
  → 最多重试 N 次后报错

模型过载
  → 自动切换到 fallback model
```

熔断机制：连续 3 次自动压缩失败后停止重试（`MAX_CONSECUTIVE_AUTOCOMPACT_FAILURES = 3`）。源码注释提到曾有 1,279 个会话出现 50+ 次连续失败，每天浪费约 25 万次 API 调用。

**可借鉴**：生产级 agent 必须有分层错误恢复 + 熔断器，否则一个错误就能让系统陷入无限循环。

### 3. 子 Agent 委托

**源码**: `src/tools/AgentTool/runAgent.ts`

主 agent 可以 spawn 子 agent 处理子任务：

```
主 Agent
  ├── AgentTool("explore") → Explore Agent（只读，搜索代码库）
  ├── AgentTool("plan")    → Plan Agent（只读，制定计划）
  └── AgentTool("custom")  → 用户自定义 Agent
```

关键设计：
- 独立上下文：子 agent 有自己的消息历史、readFileState、agentId
- 上下文继承：可以 fork 父 agent 的消息历史（`forkContextMessages`），共享 prompt cache
- 工具过滤：子 agent 可以有不同的工具集（Explore agent 只有读取类工具）
- CLAUDE.md 裁剪：只读 agent 跳过 CLAUDE.md 注入，节省 token
- 独立 transcript：每个子 agent 有自己的会话记录文件

**可借鉴**：子 agent 的上下文隔离 + 选择性继承是关键。完全隔离浪费 cache，完全共享会污染上下文。

### 4. Hook 系统

**源码**: `src/utils/hooks.ts`

6 种 hook 类型：
- PreToolUse / PostToolUse — 工具调用前后
- PreCompact / PostCompact — 压缩前后
- Stop — 模型停止输出时
- SessionStart — 会话开始时

Stop hooks 实现了"外部监督者模式"：即使模型认为任务完成了，外部逻辑可以强制它继续。

**可借鉴**：hook 系统让 agent 的行为可被外部控制和扩展，是实现 human-in-the-loop 的关键基础设施。

---

## 二、上下文管理层

### 5. 五层上下文防爆体系

**源码**: 分布在多个文件

```
L1 源头截断 (toolLimits.ts, toolResultStorage.ts)
  单工具: 50K 字符 / 100K tokens
  单消息聚合: 200K 字符
  超限 → 写磁盘 + 返回预览

L2 去重 (FileReadTool)
  文件 hash 缓存，未变文件返回 stub:
  "File unchanged since last read..."

L3 微压缩 (compact/microCompact.ts)
  每轮清理旧的读取类工具结果
  两条路径：cache_edits（热缓存）/ 直接清空（冷缓存）

L4 自动压缩 (compact/autoCompact.ts)
  ~83% 窗口利用率触发
  9 维结构化摘要

L5 兜底 (compact/compact.ts)
  压缩请求本身也超了 → 按轮次丢弃最旧的重试
  最多 3 次
```

**可借鉴**：分层防御是核心思想。大多数对话只用到 L1-L3，永远不需要全量压缩。每层的成本递增，触发频率递减。

### 6. 工具结果预算系统

**源码**: `src/utils/toolResultStorage.ts`

两级预算 + 决策冻结：

```typescript
type ContentReplacementState = {
  seenIds: Set<string>           // 见过的所有 tool_use_id
  replacements: Map<string, string>  // 被替换的 id → 替换后的内容
}
```

每个 tool_result 的命运在第一次被看到时冻结：
- 被替换了 → 之后每轮用缓存的替换内容重新应用（`mustReapply`）
- 没被替换 → 之后永远不会被替换（`frozen`）
- 从未见过 → 有资格被新替换（`fresh`）

**为什么冻结？** 为了 prompt cache。如果第 5 轮的结果在第 8 轮突然被替换，第 5-7 轮的缓存前缀全部失效。

**可借鉴**：任何需要 prompt cache 的系统都应该保证消息内容的稳定性。决策冻结是一个通用模式。

### 7. 压缩摘要的意图保持

**源码**: `src/services/compact/prompt.ts`

9 维结构化摘要：

```
1. Primary Request and Intent — 用户的所有显式请求
2. Key Technical Concepts — 技术概念、框架
3. Files and Code Sections — 文件和代码片段（含完整代码）
4. Errors and fixes — 错误和修复方式
5. Problem Solving — 问题解决过程
6. All user messages — 所有非工具结果的用户消息（原文列出）
7. Pending Tasks — 待办任务
8. Current Work — 当前正在做的工作
9. Optional Next Step — 下一步计划（要求逐字引用原文）
```

防漂移设计：
- `<analysis>` 草稿纸提高质量但不进入最终上下文
- 第 9 维要求逐字引用最近对话，打断"传话游戏"效应
- 压缩后注入续接指令："Resume directly — do not acknowledge the summary"
- transcript 路径作为外部记忆保底

**可借鉴**：任何需要长对话摘要的系统都应该用结构化模板 + 原文引用来防止意图漂移。

### 8. Prompt Cache 优化策略

**源码**: `src/constants/prompts.ts`, `src/utils/api.ts`

```
System Prompt 分区:
  ┌─────────────────────────────┐
  │ 静态区 (scope: 'global')     │ ← 跨用户可缓存
  │ 身份、工具说明、行为规范      │
  ├─ DYNAMIC_BOUNDARY ──────────┤
  │ 动态区                       │ ← 每会话不同
  │ 语言、MCP指令、环境信息       │
  └─────────────────────────────┘

Context 注入位置:
  - systemContext (git status) → 追加到 system prompt 末尾（会话级缓存）
  - userContext (CLAUDE.md) → 作为第一条伪用户消息（不影响 system prompt 缓存）
```

**可借鉴**：把不变的内容和变化的内容分开，是 prompt cache 优化的核心原则。

---

## 三、工具系统层

### 9. 工具类型系统

**源码**: `src/Tool.ts`

每个工具通过 `buildTool()` 定义，声明丰富的元数据：

```typescript
interface Tool {
  name: string
  call(input, context): Promise<Output>
  checkPermissions(input): PermissionResult
  isReadOnly(input): boolean        // 影响微压缩行为
  isDestructive?(input): boolean    // 影响权限检查
  maxResultSizeChars: number        // 影响落盘阈值
  isConcurrencySafe(input): boolean // 影响并行执行
  prompt(options): string           // 动态生成工具描述
  renderToolUseMessage(input): ReactNode  // UI 渲染
}
```

**可借鉴**：工具不只是一个函数，它需要丰富的元数据来支持权限控制、上下文管理、UI 渲染等横切关注点。

### 10. 权限模式

**源码**: `src/utils/permissions/`

四种模式：
- `default`：每次确认
- `acceptEdits`：自动接受文件编辑
- `bypassPermissions`：全部跳过（仅沙箱环境）
- `plan`：只读规划模式

Bash 工具有白名单/黑名单：`git status` 自动允许，`rm -rf /` 永远拒绝。

**可借鉴**：权限不是二元的（允许/拒绝），而是一个频谱。不同场景需要不同的权限粒度。

### 11. 工具结果落盘

**源码**: `src/utils/toolResultStorage.ts`

```
工具执行 → 结果 > 阈值?
  ├── 否 → 原样返回
  └── 是 → persistToolResult()
            ├── 写入 /session/tool-results/{id}.txt (flag: 'wx' 排他创建)
            ├── 生成 2KB 预览（在换行符边界截断）
            └── 返回: "<persisted-output>Output too large...Preview:...</persisted-output>"
```

空结果处理：注入 `(ToolName completed with no output)` 标记，防止模型误判 turn boundary。

**可借鉴**：大数据不要塞进上下文，写磁盘 + 给模型一个"指针"让它按需加载。

---

## 四、终端 UI 层

### 12. 自研 Ink 渲染引擎

**源码**: `src/ink/`

基于 React + Yoga 的终端 UI 框架（Ink 的深度 fork）：
- `RawAnsi` 组件：直接渲染 ANSI 转义序列，绕过解析
- `NoSelect` 组件：标记区域不可选择
- WeakMap 缓存渲染结果，remount 时 O(1) 查找
- 进度消息原地替换（替换最后一条而非 append），防止消息数组爆炸

**可借鉴**：React 不只能做 Web UI，用 react-reconciler 可以渲染到任何目标（终端、Canvas、PDF...）。

### 13. 全屏模式的消息管理

**源码**: `src/screens/REPL.tsx`

```typescript
if (isCompactBoundaryMessage(newMessage)) {
  if (isFullscreenEnvEnabled()) {
    // 保留上一次压缩后的消息用于滚动回看
    setMessages(old => [...getMessagesAfterCompactBoundary(old), newMessage])
  } else {
    // 普通模式：直接清空
    setMessages(() => [newMessage])
  }
  setConversationId(randomUUID())  // 刷新 React key 触发重渲染
}
```

**可借鉴**：UI 状态和 API 状态可以不同。UI 保留历史用于回看，API 只用压缩后的消息。

---

## 五、构建系统层

### 14. Feature Flag 编译期消除

**源码**: `build.ts`

90+ 个 feature flag 通过 `bun:bundle` 的 `feature()` API 在构建时静态替换：

```typescript
import { feature } from 'bun:bundle'

// 构建时替换为 true/false，未启用的分支被完全消除
const coordinatorModule = feature('COORDINATOR_MODE')
  ? require('./coordinator/coordinatorMode.js')
  : null
```

等价于 webpack 的 `DefinePlugin`，但更优雅。

**可借鉴**：大型项目用编译期 feature flag 比运行时 if/else 更好——不需要的代码完全不存在于产物中。

### 15. System Prompt 分区缓存

**源码**: `src/constants/systemPromptSections.ts`

```typescript
// 可缓存的段
systemPromptSection('memory', () => loadMemoryPrompt())

// 不可缓存的段（每轮可能变化）
DANGEROUS_uncachedSystemPromptSection(
  'mcp_instructions',
  () => getMcpInstructionsSection(mcpClients),
  'MCP servers connect/disconnect between turns'
)
```

**可借鉴**：把 prompt 的构建过程也做成声明式的，每段标注是否可缓存，系统自动优化。

---

## 六、数据与状态层

### 16. 会话持久化与恢复

**源码**: `src/utils/sessionStorage.ts`

- JSONL 格式的 transcript 文件
- `logicalParentUuid` 链接压缩前后的消息链
- `--resume` 恢复会话时重建完整消息链
- `reAppendSessionMetadata()`：确保用户设置的标题在 16KB 读取窗口内

**可借鉴**：会话持久化不只是"保存消息"，还需要处理压缩边界、消息链接、元数据位置等细节。

### 17. CLAUDE.md 发现机制

**源码**: `src/utils/claudemd.ts`

6 级优先级（从低到高）：

```
1. Managed  → /etc/claude-code/CLAUDE.md（全局管理员配置）
2. User     → ~/.claude/CLAUDE.md（用户私有全局指令）
3. Project  → CLAUDE.md, .claude/CLAUDE.md, .claude/rules/*.md
4. Local    → CLAUDE.local.md（私有项目指令，不提交）
5. AutoMem  → 自动记忆（跨会话持久化）
6. TeamMem  → 团队共享记忆
```

从 cwd 向上遍历到根目录，越近优先级越高。支持 `@include` 指令引用其他文件。

**可借鉴**：多级配置覆盖（全局 → 用户 → 项目 → 本地）是一个通用模式，类似 .gitconfig 的层级。

### 18. 极简状态管理

**源码**: `src/state/store.ts`

```typescript
function createStore<T>(initialState: T) {
  let state = initialState
  const listeners = new Set<() => void>()
  return {
    getState: () => state,
    setState: (fn: (prev: T) => T) => { state = fn(state); listeners.forEach(l => l()) },
    subscribe: (listener: () => void) => { listeners.add(listener); return () => listeners.delete(listener) },
  }
}
```

配合 React 18 的 `useSyncExternalStore` 使用。

**可借鉴**：不需要 Redux/Zustand，20 行代码就能实现一个类型安全的发布-订阅 store。

---

## 七、API 交互层

### 19. 多 Provider 统一接口

**源码**: `src/services/api/client.ts`

统一接口支持：
- Anthropic API（直连）
- AWS Bedrock
- Google Vertex
- Foundry

通过 `getAnthropicClient()` 工厂函数，根据环境变量自动选择 provider。

**可借鉴**：抽象 provider 层，让上层代码不关心具体用的是哪个 API。

### 20. Token 估算双轨制

**源码**: `src/services/tokenEstimation.ts`

```typescript
// 精确计数：调用 API
countMessagesTokensWithAPI(messages, tools)

// 粗略估算：字节数 / 4（JSON 文件 / 2），乘 4/3 安全系数
function roughTokenCountEstimation(content, bytesPerToken = 4) {
  return Math.round(content.length / bytesPerToken)
}

// 不同文件类型不同比率
function bytesPerTokenForFileType(ext) {
  switch (ext) {
    case 'json': return 2  // JSON 单字符 token 密度高
    default: return 4
  }
}
```

**可借鉴**：精确计数有网络开销，粗略估算快但要保守。两者结合使用。

### 21. MCP 协议实现

**源码**: `src/services/mcp/`

完整的 MCP client 实现：
- `InProcessTransport`：进程内通信（不走网络）
- `SdkControlTransport`：SDK 控制通道
- 指令 delta 机制：只发送变化的 MCP 指令，不重复发送
- OAuth 认证流程

**可借鉴**：MCP 是 AI 工具标准化的方向，理解其协议实现有助于构建兼容的工具生态。

---

## 八、提示词工程

### 22. 编码行为约束

**源码**: `src/constants/prompts.ts` — `getSimpleDoingTasksSection()`

最有价值的 5 条：

```
1. 最小变更原则
   "Don't add features, refactor code, or make improvements beyond what was asked."

2. 不要为假设的未来设计
   "Three similar lines of code is better than a premature abstraction."

3. 先读后改
   "Do not propose changes to code you haven't read."

4. 失败后先诊断再换策略
   "If an approach fails, diagnose why before switching tactics."

5. 记下重要信息
   "Write down any important information you might need later, as the original 
    tool result may be cleared later."
```

**可借鉴**：这些是 Anthropic 从大量真实用户反馈中总结的"模型常见毛病"的对症药。直接复用到 CLAUDE.md 或其他 AI 编码工具中。

### 23. 行动风险评估框架

**源码**: `src/constants/prompts.ts` — `getActionsSection()`

```
可逆的本地操作（编辑文件、跑测试）→ 自由执行
不可逆/影响共享系统的操作 → 先确认
用户批准一次 ≠ 批准所有场景
遇到障碍不要用破坏性操作绕过
```

**可借鉴**：给 agent 一个明确的"行动决策框架"，比简单说"小心点"有效得多。

### 24. 输出效率指令

**源码**: `src/constants/prompts.ts` — `getOutputEfficiencySection()`

外部版本（简洁）：
```
"Go straight to the point. Lead with the answer or action, not the reasoning."
```

内部版本（详细）：
```
"Write so they can pick back up cold: use complete, grammatically correct sentences 
without unexplained jargon. Expand technical terms."

"Use inverted pyramid when appropriate (leading with the action)."
```

**可借鉴**：内部版本的"假设用户已经离开并失去了上下文"是一个很好的写作原则。

---

## 九、可直接复用的设计模式速查表

| 模式 | 出处 | 一句话描述 |
|------|------|-----------|
| 决策冻结 | toolResultStorage | 第一次决策后永不改变，保证缓存稳定 |
| 草稿纸模式 | compact/prompt | `<analysis>` 提高质量但不进入最终输出 |
| 落盘+预览 | toolResultStorage | 大数据写磁盘，只给模型看摘要 |
| 逐字引用 | compact/prompt | 要求摘要包含原文引用，防止多次压缩漂移 |
| 熔断器 | autoCompact | 连续 N 次失败后停止重试 |
| 分层防御 | 五层压缩体系 | 每层只在前一层不够用时触发 |
| 静态/动态分区 | systemPromptSections | 不变的内容缓存，变化的内容隔离 |
| 恢复消息注入 | query.ts | 错误后注入"继续，不要道歉"的指令 |
| 空结果标记 | toolResultStorage | 空输出注入标记防止模型困惑 |
| memoize + 手动清缓存 | context.ts | 会话级缓存 + 特定事件触发清除 |
| 外部监督者 | stop hooks | 模型说完了但 hook 可以强制继续 |
| 上下文继承 | runAgent | 子 agent fork 父上下文，共享 cache |
| 流式并行执行 | StreamingToolExecutor | 边接收边执行，不等模型输出完 |

---

## 十、学习路线建议

按优先级排序：

1. **QueryEngine / query.ts** — 理解这个文件就理解了 Claude Code 的核心
2. **compact/** — 上下文管理是长对话 agent 的命脉
3. **Tool.ts + tools/** — 工具系统是 agent 能力的载体
4. **prompts.ts** — 提示词工程的最佳实践
5. **toolResultStorage.ts** — 大数据处理的精妙设计
6. **ink/** — 终端 UI 的 React 化
7. **services/api/** — 多 provider 统一接口
8. **services/mcp/** — MCP 协议实现

每个模块建议花 1-2 天深读，总共 2-3 周可以对整个架构有全面理解。

---

*本文基于 Claude Code v2.1.88 源码分析。源码从 npm 包的 source map 中还原，包含 4,756 个源文件、1,906 个核心源码文件。*
