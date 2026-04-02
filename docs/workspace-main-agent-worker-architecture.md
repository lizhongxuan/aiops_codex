# 协作工作台主 Agent / Worker 架构说明

本文件对应 `todo_delete_session.md` 中“删除 Planner Session”的重构目标，用来冻结当前阶段的后端职责划分、前台产品概念和兼容策略。

## 1. 产品概念冻结

前台只保留以下概念：

- `主 Agent Session`
- `子 Agent / host-agent`
- `计划小控件`
- `右侧审批列表`
- `证据弹框`

前台不再暴露以下术语：

- `PlannerSession`
- `影子 session`
- `Planner -> AI`
- `Planner -> Host`
- `planner trace`

## 2. 后端目标架构

当前阶段统一为三层：

1. `WorkspaceSession / 主 Agent Session`
   - 唯一用户可见会话
   - 简单状态问题直接回复
   - 复杂任务在当前 workspace 会话内生成简短计划
   - 直接调用 `query_ai_server_state` 与 `orchestrator_dispatch_tasks`
   - 持续向前台回投摘要级运行态

2. `Mission / Go 调度器`
   - 保存 mission / task / worker / approval / result 状态
   - 负责 host 级分配、并发、预算、失败收敛、取消、历史回放
   - 不再为新 mission 创建独立 planner session

3. `WorkerSession`
   - 按 host 执行具体任务
   - 负责 remote exec / terminal / file change
   - 产生审批并把结果回传给主 Agent

## 3. 请求分流规则

工作台消息分为三类：

1. `状态问题`
   - 直接读取 ai-server 当前投影
   - 不创建 mission turn
   - 不派发 worker

2. `单主机只读问题`
   - 在当前 `WorkspaceSession` 上直接起主 Agent turn
   - 允许读取目标 host 状态
   - 不允许直接做 mutation

3. `复杂任务或高风险任务`
   - 在当前 `WorkspaceSession` 上直接起主 Agent turn
   - 主 Agent 先生成简短 plan
   - 再调用 dispatch 把 step 派发到 worker

## 4. 审批边界

- 审批永远只来自 `WorkerSession`
- 主 Agent 不承担审批执行角色
- 主对话只显示审批摘要，例如：
  - `有 1 条审批等待处理`
  - `web-02 正在等待审批`
- 实际审批动作统一放在右侧审批列表

## 5. 兼容策略

本轮是“删除 PlannerSession 的主链路实现”，不是“一次性删除所有历史 planner 字段”。

兼容策略分两期：

### 兼容期

- 旧持久化数据中的 `plannerSessionId / plannerThreadId / planner trace` 允许继续被加载
- legacy planner mission 如果仍在运行，会在启动恢复时被明确收敛为失败
- 历史接口允许读取旧 planner 相关字段，但不会继续对外生成新的 planner 概念

### 清理期

- 等真实链路稳定后，再进一步删除：
  - planner 兼容索引
  - planner 常量
  - planner legacy failover 分支
  - 历史测试中的 planner 夹具

## 6. 当前未完全删除的 legacy 兼容点

以下内容当前仍保留，但只服务兼容和恢复，不属于新主链路：

- `Mission.PlannerSessionID / PlannerThreadID`
- `SessionKindPlanner`
- `RuntimePresetPlannerInternal`
- `MissionByPlannerSession(...)`
- legacy planner mission reconcile / failover

这些兼容点存在的原因是：

- 避免旧 JSON / 历史 mission 直接无法加载
- 保证老数据恢复时能明确失败，而不是静默卡死

## 7. 当前验收口径

当前只要满足下面条件，就视为主链路完成去 planner 化：

- 新复杂任务不再创建 `PlannerSession`
- `WorkspaceSession` 直接承担 planning + dispatch
- 前台不再展示 planner 概念
- 审批继续由 worker 触发并在右侧统一处理
- 历史 legacy planner 数据不会阻塞启动或导致恢复卡死
