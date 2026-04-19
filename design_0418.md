# AIOps Codex Tool Prompt Refactor And Prompt-Debt Deletion Design

## 背景

`aiops-codex` 当前所有 tool 的行为不是由单一 prompt 决定，而是由多层文本和运行时策略叠加出来的：

- tool schema 描述层
- host / thread developer instructions 层
- `BuildSystemPrompt()` 的全局系统提示层
- workspace `TurnPolicy + PromptEnvelope` 层
- agent loop 运行时 repair / nudge 层

这种分层最初是为了解决不同问题：

- tool schema 负责暴露能力
- host / runtime prompt 负责环境约束
- turn policy 负责 lane、证据和审批
- loop repair 负责临场纠偏

问题不是“有多层”本身，而是这些层已经发生了职责漂移。尤其是 `web_search` 和“实时/行情/快照”类问题，现在同一条规则被 3-4 层重复表达，且方向一致地推向“少搜、快答、少来源”。这会直接影响所有 tool 的整体行为稳定性。

参考对比：

- Claude Code 的 `WebSearch` prompt 主要强调“获取最新信息 + 最终必须带 Sources”，见 [claude code/tools/WebSearchTool/prompt.ts](/Users/lizhongxuan/Desktop/aiops-codex/claude%20code/tools/WebSearchTool/prompt.ts:5)
- Claude Code 的默认 system prompt 是一个统一 registry：静态 section + 动态 section，见 [claude code/constants/prompts.ts](/Users/lizhongxuan/Desktop/aiops-codex/claude%20code/constants/prompts.ts:444) 和 [claude code/constants/systemPromptSections.ts](/Users/lizhongxuan/Desktop/aiops-codex/claude%20code/constants/systemPromptSections.ts:1)
- Claude Code 的 tool prompt 归 tool 自己维护，并在构造 API tool schema 时调用 `tool.prompt()` 写入 description，而不是再重复塞进 session prompt，见 [claude code/utils/api.ts](/Users/lizhongxuan/Desktop/aiops-codex/claude%20code/utils/api.ts:139)
- 原生 Codex 更依赖 first-class tool item / protocol event，不依赖多层业务 prose 去驱动搜索收口

因此，本方案不是“把所有 prompt 合成一段”，而是：

- 参考 Claude Code 统一 prompt assembly 方式，保留必要分层
- 收紧每层职责
- 删除重复和失效代码
- 把高价值约束从 prose 下沉到结构化 policy

## 当前问题

### 1. 同一条规则跨层重复

当前至少有三处对“行情 / 价格 / 指数 / 最新 / 实时”做了重复约束：

- [`internal/server/dynamic_tools.go`](/Users/lizhongxuan/Desktop/aiops-codex/internal/server/dynamic_tools.go:706)
- [`internal/agentloop/session.go`](/Users/lizhongxuan/Desktop/aiops-codex/internal/agentloop/session.go:361)
- [`internal/agentloop/loop.go`](/Users/lizhongxuan/Desktop/aiops-codex/internal/agentloop/loop.go:659)

重复内容包括：

- 紧凑快照格式
- 1-2 个来源
- 给出核心信息后就停止
- 有“足够市场快照数据”后立刻回答

### 2. 规则职责错位

现在有三类本应分开的约束混在一起：

- tool 能力约束：某个 tool 能做什么
- turn 决策约束：这轮是否必须继续搜、证据够不够
- answer 风格约束：最终回答长什么样

结果是：

- tool prompt 在管 answer style
- system prompt 在管 citation count
- runtime nudge 在管 early stop

### 3. 结构化 policy 太弱，prose 太强

`TurnPolicy` 当前只有：

- `RequiredTools`
- `RequiredEvidenceKinds`
- `MinimumEvidenceCount`
- `RequiredNextTool`

见 [`internal/model/types.go`](/Users/lizhongxuan/Desktop/aiops-codex/internal/model/types.go:334)

但真实业务需要的规则其实是：

- 至少几个独立来源
- 是否必须引用来源
- 哪类问题允许早停
- answer style 是 compact snapshot 还是 normal answer

这些现在没有结构化字段，只能散落在 prompt prose 里。

### 4. 部分 prompt builder 已经是死代码

[`internal/server/prompt_builder.go`](/Users/lizhongxuan/Desktop/aiops-codex/internal/server/prompt_builder.go) 里这几段已经不参与实际 workspace prompt 组装，只在测试里自测：

- `planModeSection()`
- `toolPromptsSection()`
- `requestApprovalToolPrompt()`
- `explicitExecutionSection()`

实际 `buildWorkspacePromptEnvelope()` 只用了：

- `staticSystemPromptSection()`
- `developerInstructionsSection()`
- `intentClarificationSection()`

见 [`internal/server/turn_policy.go`](/Users/lizhongxuan/Desktop/aiops-codex/internal/server/turn_policy.go:356)

这类代码已经构成明确的提示词债务。

### 5. 缺少统一 prompt assembly 骨架

Claude Code 的关键不是“prompt 更长”，而是它有一条非常清晰的组装链：

- 默认 system prompt 由 section registry 统一拼接
- 动态 section 与静态 section 明确分开
- tool 自己拥有 tool prompt
- custom / append / override 是明确的后置覆盖层

见：

- [claude code/constants/prompts.ts](/Users/lizhongxuan/Desktop/aiops-codex/claude%20code/constants/prompts.ts:444)
- [claude code/utils/systemPrompt.ts](/Users/lizhongxuan/Desktop/aiops-codex/claude%20code/utils/systemPrompt.ts:17)

而 `aiops-codex` 当前是：

- 一部分规则在 `BuildSystemPrompt()`
- 一部分规则在 `PromptEnvelope`
- 一部分规则在 host developer instructions
- 一部分规则在 tool schema description
- 一部分规则在 loop repair

结果是同一个约束会“东一块，西一块”地出现。

### 6. “刷新后搜索记录消失”不是 prompt 问题

这点必须单列说明：

- prompt 分层重构可以修复“过早收口、搜索不足、引用不足”
- 但“搜索过程刷新后不见了”属于 tool 历史项不够 durable 的问题

因此本设计聚焦 prompt 与 dead code；搜索过程持久化仍应继续走 lifecycle / history item 路线。

## 设计目标

### 目标

- 统一所有 tool prompt 的职责边界
- 删除重复、失效、只在测试中存活的 prompt 代码
- 将高价值规则从 prose 下沉到结构化 `TurnPolicy`
- 让 `web_search`、`query_ai_server_state`、`readonly_host_inspect`、mutation 类工具都遵守同一 prompt 架构
- 减少运行时 answer-now nudge 对 tool 行为的干扰

### 非目标

- 本方案不直接实现搜索历史持久化
- 本方案不重写 tool lifecycle / projection 架构
- 本方案不要求前端改版
- 本方案不删除所有 legacy 会话恢复代码；只定义其删除窗口

## Claude Code 参考模型

本次重构不直接照搬 Claude Code 的实现细节，但应借鉴它的分层思想。

Claude Code 可以概括成 4 个层次：

### A. Default System Prompt Sections

由统一的 section registry 生成：

- 静态 section
- 动态 section
- 带缓存和 cache-break 标记的 section

见：

- [claude code/constants/prompts.ts](/Users/lizhongxuan/Desktop/aiops-codex/claude%20code/constants/prompts.ts:444)
- [claude code/constants/systemPromptSections.ts](/Users/lizhongxuan/Desktop/aiops-codex/claude%20code/constants/systemPromptSections.ts:1)

这个层负责：

- 身份
- 通用安全
- 通用做事原则
- env / language / MCP / output style 等 session 级上下文

### B. Tool-Owned Prompt

每个 tool 通过 `tool.prompt()` 维护自己的 description。

见：

- [claude code/Tool.ts](/Users/lizhongxuan/Desktop/aiops-codex/claude%20code/Tool.ts:518)
- [claude code/utils/api.ts](/Users/lizhongxuan/Desktop/aiops-codex/claude%20code/utils/api.ts:139)

这意味着：

- tool 的使用说明归 tool 自己负责
- system prompt 不再重复解释每个 tool
- tool schema 和 tool description 的变更边界清晰

### C. Effective Prompt Overrides

Claude Code 把 `custom / append / override / agent-specific prompt` 放在统一的 effective prompt builder 里，而不是散落在多个入口。

见：

- [claude code/utils/systemPrompt.ts](/Users/lizhongxuan/Desktop/aiops-codex/claude%20code/utils/systemPrompt.ts:17)

### D. Runtime / Session Attachments

动态上下文以 section 或 attachment 形式注入，而不是把一次性业务规则写死进全局 system prompt。

这使它更容易做到：

- session 变化归 session section
- tool 变化归 tool prompt
- agent 变化归 effective prompt override

## 目标分层

参考 Claude Code，`aiops-codex` 的目标不应是“3+1 层 prose”，而应是“统一 prompt assembly 骨架 + 明确的 4 类来源”。

### 总体骨架

1. `Default Prompt Sections`
2. `Tool-Owned Prompt`
3. `Turn Policy Attachment`
4. `Runtime Repair`

其中 `Default Prompt Sections` 内部再区分静态 section 和动态 section。

### Layer 1: Default Prompt Sections

职责：

- agent 身份
- 通用安全边界
- 通用回答风格
- 通用证据原则
- 环境 / host / session / lane 共有上下文

文件归属：

- 新增统一 registry 文件，建议：
  - `internal/server/prompt_sections.go`
  - `internal/server/prompt_assembler.go`
- 现有内容迁移来源：
  - [`internal/agentloop/session.go`](/Users/lizhongxuan/Desktop/aiops-codex/internal/agentloop/session.go:304)
  - [`internal/server/prompt_builder.go`](/Users/lizhongxuan/Desktop/aiops-codex/internal/server/prompt_builder.go:28)
  - [`internal/server/server.go`](/Users/lizhongxuan/Desktop/aiops-codex/internal/server/server.go:5012)

必须保留：

- 基于证据回答
- 不泄露内部实现
- 工具输出只摘要关键行

必须移除：

- 行情快照专用格式
- 1-2 来源数量限制
- “给完就停”之类问题特定规则

建议 section 结构参考 Claude Code：

- `staticSystemSection`
- `workflowSection`
- `safetySection`
- `toolUsageSection`
- `environmentSection`
- `languageOrOutputStyleSection`

但不再让 section 里承载某个具体 tool 的长说明。

### Layer 2: Tool-Owned Prompt

职责：

- tool 的能力、输入、输出、失败语义
- host / path / shell / approval 的硬约束

文件归属：

- tool JSON schema：[`internal/server/dynamic_tools.go`](/Users/lizhongxuan/Desktop/aiops-codex/internal/server/dynamic_tools.go)
- 新增建议：
  - `internal/server/tool_prompts.go`
  - 或者各 tool handler / tool spec 旁边定义 `Prompt()` / `Description()`

必须保留：

- remote host 必须带 `host=...`
- remote command 无 shell wrapper
- local/remote 文件路径约束
- 哪类工具是 readonly / mutation / approval

必须移除：

- 市场快照 answer style
- 来源条数限制
- “不要展开分析”这种产品文风规则

关键原则参考 Claude Code：

- tool 如何用，只在 tool 自己那里解释一次
- session/system prompt 不重复解释 tool
- `web_search/open_page/find_in_page` 作为一个搜索工具族，各自有自己的 tool prompt，但不再在 system prompt 重复定义“回答必须 1-2 来源”

### Layer 3: Turn Policy Attachment

职责：

- 本轮是否必须用 tool
- 本轮需要的证据类型和数量
- 本轮 answer style
- 是否允许结束

文件归属：

- [`internal/server/turn_policy.go`](/Users/lizhongxuan/Desktop/aiops-codex/internal/server/turn_policy.go)
- [`internal/model/types.go`](/Users/lizhongxuan/Desktop/aiops-codex/internal/model/types.go:334)

新增结构化字段建议：

```go
type TurnPolicy struct {
    // 现有字段保留
    IntentClass           string
    Lane                  string
    RequiredTools         []string
    RequiredEvidenceKinds []string
    MinimumEvidenceCount  int
    RequiredNextTool      string

    // 新增字段
    MinimumIndependentSources int      `json:"minimumIndependentSources,omitempty"`
    RequireSourceAttribution  bool     `json:"requireSourceAttribution,omitempty"`
    PreferredAnswerStyle      string   `json:"preferredAnswerStyle,omitempty"` // normal | compact_snapshot | plan | verify
    AllowEarlyStop            bool     `json:"allowEarlyStop,omitempty"`
    EvidenceDiversityRules    []string `json:"evidenceDiversityRules,omitempty"`

    // 新增的通用合同字段
    KnowledgeFreshness        string   `json:"knowledgeFreshness,omitempty"`       // stable | external | realtime
    EvidenceContract          string   `json:"evidenceContract,omitempty"`         // none | external_facts | sourced_snapshot | execution_evidence
    AnswerContract            string   `json:"answerContract,omitempty"`           // normal | sourced_facts | sourced_snapshot | plan | verify
    FreshnessDeadline         string   `json:"freshnessDeadline,omitempty"`        // now | today | this_week | explicit_date
    RequiredCitationKinds     []string `json:"requiredCitationKinds,omitempty"`    // url | domain | page
}
```

这里不应再走“市场关键词命中 -> 特殊 policy”这条路。参考 Claude Code，[`WebSearchTool/prompt.ts`](/Users/lizhongxuan/Desktop/aiops-codex/claude%20code/tools/WebSearchTool/prompt.ts:5) 的核心是：

- 只声明 `web_search` 用于 up-to-date information
- 搜索后答案必须带 `Sources:`
- 不通过行业词表去驱动“搜几个站”或“何时停”

因此 `TurnPolicy` 应该改成**通用合同驱动**，而不是**领域词表驱动**。

对于“今天 A 股行情”“查看上证指数最新走势”“React 文档最新地址”“今天北京天气”这类问题，应统一落成：

- `RequiredTools = ["web_search"]`
- `KnowledgeFreshness = "realtime"` 或 `external`
- `EvidenceContract = "sourced_snapshot"` 或 `external_facts`
- `MinimumEvidenceCount >= 2`
- `MinimumIndependentSources >= 2`
- `RequireSourceAttribution = true`
- `AnswerContract = "sourced_snapshot"` 或 `sourced_facts`
- `AllowEarlyStop = false`

关键点是：

- 这套规则来自“用户是否要求**当前/外部/可能变化的事实**”，而不是来自“A股/指数”这些领域词
- `compact_snapshot` 只是某类答案合同的一种渲染形态，不应该与具体行业词绑定
- `detectWorkspaceTurnSignals()` 不再维护市场专用词表，而应识别更通用的 freshness / external-verification / sourced-answer 信号

这里要尽量模仿 Claude Code 的“动态 section / attachment”思路：

- 将本轮约束作为 attachment 注入
- 不要把这类约束写进全局 system prompt 常量
- 不要把这类约束写进 tool prompt

### Layer 4: Runtime Repair

职责：

- 只做缺失前置条件的最小纠偏
- 不改写主策略
- 不施加业务风格

文件归属：

- [`internal/agentloop/loop.go`](/Users/lizhongxuan/Desktop/aiops-codex/internal/agentloop/loop.go)

允许保留的 repair：

- 缺 required tool 时提醒继续调用
- 缺审批时提醒进入审批链
- 缺 evidence 时提醒继续收集证据

必须删除的 repair：

- “你已经有足够市场快照数据，立即回答”
- 任何针对具体垂类问题的 answer-now 文案

## 统一组装方案

参考 Claude Code，`aiops-codex` 应形成一条唯一的 prompt 组装链：

1. `DefaultPromptSections()`
2. `ResolveDynamicSections(session, host, policy)`
3. `AssemblePromptEnvelope()`
4. `BuildEffectiveSystemPrompt()`
5. `AttachToolSchemasWithToolOwnedPrompts()`
6. `AppendRuntimeRepairMessage()` 仅在必要时执行

建议新增接口：

```go
type PromptSection struct {
    Name      string
    Content   string
    Static    bool
    CacheHint string
}

type ToolPromptSpec struct {
    Name        string
    Description string
}
```

建议新增函数：

- `defaultPromptSections()`
- `dynamicPromptSections(sessionID, hostID, policy)`
- `buildEffectivePrompt(sections []PromptSection)`
- `toolPromptSpec(name string) ToolPromptSpec`

## 新的规则归属矩阵

| 规则 | 归属层 | 是否允许 prose | 备注 |
| --- | --- | --- | --- |
| tool 参数/host/path/approval 硬约束 | Tool-Owned Prompt | 是 | 以 capability 为主 |
| 是否必须先搜 | Turn Policy | 否，优先结构化 | `RequiredTools` |
| 至少几个来源 | Turn Policy | 否，优先结构化 | `MinimumIndependentSources` |
| 是否必须带来源 | Turn Policy | 否，优先结构化 | `RequireSourceAttribution` |
| 回答合同是什么 | Turn Policy | 否，优先结构化 | `AnswerContract` / `PreferredAnswerStyle` |
| 知识是否必须是当前/外部的 | Turn Policy | 否，优先结构化 | `KnowledgeFreshness` / `EvidenceContract` |
| 通用回答风格 | Default Prompt Sections | 是 | 不带业务特例 |
| 缺少前置条件时提醒继续 | Runtime Repair | 是 | 只做最小修复 |
| “搜够了赶紧答” | 不保留 | 否 | 应删除 |

## 文件级重构方案

### 1. `internal/server/dynamic_tools.go`

处理方式：

- 保留 tool schema
- 删除 `localThreadDeveloperInstructions()` 中的市场快照段落
- 保留 `web_search` 必须用于实时信息的规则，但只写成 capability/usage，不写 answer style

进一步目标：

- 后续把 `localThreadDeveloperInstructions()` 中的 tool-by-tool prose 拆出，迁到 `tool_prompts.go`
- 让 host developer instructions 只保留 host context，不再夹带 tool 使用长说明

删改目标：

- 删除 [`internal/server/dynamic_tools.go:728`](/Users/lizhongxuan/Desktop/aiops-codex/internal/server/dynamic_tools.go:728) 那段完整的 compact snapshot prose

### 2. `internal/agentloop/session.go`

处理方式：

- `BuildSystemPrompt()` 只保留通用输出规范
- 删除针对“行情 / 价格 / 指数 / 今日 / 最新 / 实时”的专用 bullet

删改目标：

- 删除 [`internal/agentloop/session.go:373`](/Users/lizhongxuan/Desktop/aiops-codex/internal/agentloop/session.go:373) 到 [`internal/agentloop/session.go:377`](/Users/lizhongxuan/Desktop/aiops-codex/internal/agentloop/session.go:377) 的快照型问题专用规则

### 3. `internal/server/turn_policy.go`

处理方式：

- 扩展 `TurnPolicy`
- 把 freshness / external facts / sourced answer contract 写成结构化字段
- `workspaceLaneInstructions()` 只保留 lane 约束，不再承担业务 answer style
- `buildWorkspacePromptEnvelope()` 只负责 attachment 组装，不再承担 section 文案定义

新增重点：

- `MinimumIndependentSources`
- `RequireSourceAttribution`
- `PreferredAnswerStyle`
- `AllowEarlyStop`
- `KnowledgeFreshness`
- `EvidenceContract`
- `AnswerContract`

### 4. `internal/agentloop/loop.go`

处理方式：

- 删除 market-specific answer-now nudge
- 保留 generic missing-evidence / missing-required-tool repair

删改目标：

- 删除 `compactSnapshotAnswerNudge`
- 删除 `queryNeedsCompactSnapshotAnswer()`
- 删除 `sessionMarketSnapshotToolBase()`
- 删除 `sessionHasCompactSnapshotAnswerNudge()`
- 删除 `shouldNudgeCompactSnapshotAnswer()`
- 删除相关 `market_snapshot_*` metadata 写入和测试断言

### 5. `internal/server/prompt_builder.go`

处理方式：

- 不再让它继续当“东拼西凑的 section 堆”
- 重命名并重构成 Claude Code 风格的 section registry
- 保留仍被 `PromptEnvelope` 使用的 section
- 删除未被任何运行时代码引用、仅存活于测试中的 section builder

立即删除目标：

- `planModeSection()`
- `toolPromptsSection()`
- `requestApprovalToolPrompt()`
- `explicitExecutionSection()`

连带删除：

- 对应的 [`internal/server/prompt_builder_test.go`](/Users/lizhongxuan/Desktop/aiops-codex/internal/server/prompt_builder_test.go) 死测试

新增替代：

- `prompt_sections.go`
- `prompt_assembler.go`

这两个文件分别负责：

- section 定义
- section 解析与组装

### 6. `internal/server/session_runtime.go`

处理方式：

- 现有 ReAct 主路径继续保留
- 标记 legacy prompt builder 为“兼容期后删除”

暂不立即删除，但应列入第二阶段清理：

- `workspaceRouteThreadConfigHash()`
- `workspaceReadonlyThreadConfigHash()`
- `workspaceOrchestrationThreadConfigHash()`
- `buildWorkspaceRouteThreadStartSpec()`
- `buildWorkspaceRouteTurnStartSpec()`
- `buildWorkspaceReadonlyThreadStartSpec()`
- `buildWorkspaceReadonlyTurnStartSpec()`
- `buildWorkspaceOrchestrationThreadStartSpec()`
- `buildWorkspaceOrchestrationTurnStartSpec()`

这些代码现在仍被 rollback / 历史恢复 / 兼容测试引用，见 [`internal/server/session_runtime.go`](/Users/lizhongxuan/Desktop/aiops-codex/internal/server/session_runtime.go:84) 和 [`internal/server/orchestrator_integration.go`](/Users/lizhongxuan/Desktop/aiops-codex/internal/server/orchestrator_integration.go:512)。

## 删除计划

### 第一批：可立即删除

这批不需要兼容窗口，替换完成即可删：

1. `localThreadDeveloperInstructions()` 中的行情快照 prose
2. `BuildSystemPrompt()` 中的行情快照 prose
3. `compactSnapshotAnswerNudge` 及相关 helper / metadata
4. `prompt_builder.go` 中未参与实际 prompt assembly 的 section builder
5. 对应 dead tests

### 第一批补充：需要迁移后删除的“东一块西一块” prompt source

这批不是死代码，但需要拆散重组：

1. `renderMainAgentDeveloperInstructions()` 中的 tool prose
2. `localThreadDeveloperInstructions()` / `remoteThreadDeveloperInstructions()` 中的 tool prose
3. `BuildSystemPrompt()` 中的问题特定 answer style prose

目标不是简单删空，而是：

- host context 留在 host context
- tool 说明搬到 tool-owned prompt
- answer style 搬到 turn policy attachment

### 第二批：迁移完成后删除

这批代码虽然对新请求已无价值，但仍被兼容路径引用：

1. legacy workspace route / readonly / orchestration thread spec builder
2. legacy thread config hash helper
3. 依赖这些 builder 的兼容测试

删除前提：

- 历史 session replay 不再依赖旧 hash
- orchestrator reconcile 不再走 legacy route thread 恢复
- 兼容窗口和回滚策略明确结束

## 迁移步骤

### Phase 1: 先收权责，不删兼容路径

- 扩展 `TurnPolicy`
- 调整 `detectWorkspaceTurnSignals()`
- 删除 session / local thread / loop 中的 market prose 重复
- 用结构化 policy 驱动 compact snapshot 与 citation

### Phase 2: 删除立即可删的 prompt 债务

- 删除 `compactSnapshotAnswerNudge` 相关代码
- 删除 `prompt_builder.go` 死 section
- 更新对应测试

### Phase 3: 观察一轮真实回归

关键验证：

- 实时行情问题是否至少搜索 2 个独立来源
- 最终答案是否带来源
- 非实时问题是否没有被过度搜索
- 现有 workspace / approval / verify lane 是否未退化

### Phase 4: 清理 legacy prompt builder

- 先确认历史恢复链路已不依赖
- 再删除 `session_runtime.go` 中 legacy prompt builder 和 hash helper

## 测试与验收

### 后端

- `turn_policy` 单测：
  - 当前/外部事实问题命中 sourced policy
  - 至少要求 2 个独立来源
- `session` 单测：
  - 全局系统 prompt 不再包含 market snapshot prose
- `loop` 单测：
  - 不再注入 compact snapshot answer-now nudge
  - 仍能在缺 required tool / missing evidence 时继续 repair

### 前端 / 集成

- 搜索型问题最终答案出现后，搜索过程仍在 fold 中可见
- 刷新后若历史项存在，过程可重建
- 行情类答案必须带来源

## 风险

### 1. 只删 prose，不补统一 assembly，会导致新 prompt 再次散开

所以顺序必须是：

- 先补统一 prompt assembly 骨架
- 再补 `TurnPolicy`
- 再删重复 prose

### 2. 过度删除 legacy builder，会伤到历史会话恢复

所以 `session_runtime.go` 里的 legacy builder 不能和 prompt prose 一起立刻删。

### 3. 仅做 prompt 重构，无法单独修复“刷新后搜索记录消失”

这点必须在实施和验收时单独标注，避免误判。

## 最终结论

`aiops-codex` 的 tool prompt 分很多层，历史上是合理的；但当前已经出现明显的职责重复、规则串位和死代码堆积，必须重构。

建议落地原则：

- 参考 Claude Code，先统一 prompt assembly，再谈删减内容
- system prompt 改成 section registry，不再在多个文件里各写一段“大而全 developer instructions”
- tool 使用说明收回到 tool-owned prompt，不再混在 host developer instructions 里
- `TurnPolicy` 成为“是否继续搜、证据够不够、回答样式为何”的唯一权威
- 删除 market-specific answer-now nudge
- 删除未参与实际 prompt assembly 的 dead prompt builder
- 将 legacy workspace prompt builder 作为第二阶段兼容清理项

这会把“所有 tool 的提示词”从“历史叠加文本”改成更接近 Claude Code 的架构：

- 统一 section registry
- tool 自带 prompt
- turn policy 作为 attachment
- runtime 只做最小 repair
