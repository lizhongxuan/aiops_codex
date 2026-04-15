# Claude Code Coordinator 模块分析

> 源码路径: `claude code/coordinator/`

## 概述

Coordinator 是 Claude Code 的**会话模式管理器**，负责判断当前会话处于哪种运行模式（普通模式 vs coordinator 模式），并为不同模式注入对应的 system prompt 和用户上下文。

## 文件清单

| 文件 | 职责 |
|------|------|
| `coordinatorMode.ts` | 唯一文件，包含模式判断、prompt 生成、上下文注入 |

## 核心函数

### `isCoordinatorMode(): boolean`
判断当前是否处于 coordinator 模式。依赖 feature flag (`COORDINATOR_MODE`) 和 session 配置。

### `isScratchpadGateEnabled(): boolean`
判断 scratchpad（草稿板）功能是否开启，用于 coordinator 模式下的中间推理。

### `matchSessionMode(mode: string): boolean`
匹配当前 session 的运行模式字符串，支持多种模式标识。

### `getCoordinatorUserContext(context): string`
为 coordinator 模式生成用户上下文信息，包含当前工作目录、项目信息等。

### `getCoordinatorSystemPrompt(): string`
生成 coordinator 模式专用的 system prompt，指导模型以"协调者"角色运行——分解任务、分配给子 agent、汇总结果。

## 设计模式

Coordinator 模式的核心思想是**任务分解与委派**：
1. 用户提交复杂任务
2. Coordinator 分析任务，拆分为子任务
3. 每个子任务委派给独立的 Agent（通过 AgentTool）
4. Coordinator 汇总各 Agent 的结果，返回给用户

这与你项目中 `internal/orchestrator/` 的设计理念一致，但 Claude Code 的实现更轻量——只是通过 prompt engineering 来实现协调，而非硬编码的工作流。

## 与你项目的对应关系

| Claude Code | aiops-codex | 说明 |
|-------------|-------------|------|
| `coordinatorMode.ts` | `internal/orchestrator/orchestrator.go` | 任务编排入口 |
| `getCoordinatorSystemPrompt()` | agent loop 的 system prompt 构建 | 模式切换 |
| AgentTool 子 agent 调度 | subagent 机制 | 多 agent 协作 |

## 可借鉴的设计

1. **模式切换通过 prompt 而非代码分支**：不同模式只是 system prompt 不同，agent loop 代码不变
2. **Feature flag 控制**：新模式通过 feature flag 灰度发布
3. **Scratchpad 中间推理**：coordinator 可以在草稿板上做中间推理，不直接暴露给用户
