# Orchestrator 手动 Smoke 清单

本文用于验证 `WorkspaceSession -> PlannerSession -> WorkerSession -> host-agent` 的真实 UI 闭环。  
适用范围是已经完成 orchestrator 相关后端和前端联动的环境，重点检查工作台交互、恢复逻辑和多主机调度是否符合预期。

## 1. 前置条件

- `ai-server`、前端、`host-agent` 均已启动。
- 至少准备 2 台在线主机，建议 4 台以上，便于观察排队与分批推进。
- 当前用户已能在 Web 端正常进入工作台页和单机聊天页。
- 后端日志可读，前端页面可刷新。
- 如果需要验证审批流程，确保目标主机上存在能触发审批的命令或文件操作。

## 2. Smoke 步骤

### 2.1 新建 `WorkspaceSession`

操作步骤：

1. 在左侧会话入口新建工作台会话。
2. 确认页面进入 `/protocol` 或对应工作台页面。
3. 发送一个明确的多主机运维任务，例如“检查 4 台主机上的 nginx 状态并给出差异”。

预期结果：

- 新会话类型是 `workspace`。
- 页面出现 Mission 相关卡片或工作台摘要流，而不是普通单机会话消息流。
- 后端创建或复用 `PlannerSession`，但前端不会直接暴露内部 session。

记录点：

- workspace session id
- mission id
- planner session id

### 2.2 Planner Dispatch 多主机 Worker

操作步骤：

1. 让 planner 解析任务并派发到多个 host。
2. 观察工作台是否出现 host 维度的分发卡片或进度卡片。
3. 检查至少 2 台 host 是否各自进入 worker 执行。

预期结果：

- planner 能生成结构化 dispatch。
- worker 按 host 维度启动，而不是全部挤在一个 session。
- 同一 host 的重复任务会优先复用队列，不应同时起多个互斥 worker turn。

记录点：

- 目标 host 列表
- 实际启动的 worker 数量
- 是否出现排队

### 2.3 多主机 Worker 执行

操作步骤：

1. 让一部分 worker 完成简单只读操作，例如状态检查或目录查看。
2. 让另一部分 worker 执行需要审批或较慢的操作。
3. 观察卡片是否按 host 分别更新。

预期结果：

- 每台主机的进度独立更新。
- 工作台能看到任务分批推进，而不是一口气全部完成。
- worker 完成后，mission 卡片链会继续推进到下一阶段。

记录点：

- worker 完成顺序
- 是否有重复进度卡
- 是否有 host 状态混淆

### 2.4 `approval / choice` 镜像

操作步骤：

1. 在 worker 侧触发审批请求。
2. 在工作台上点击审批卡片并选择同意或拒绝。
3. 如果出现 choice 卡片，在工作台直接提交答案。

预期结果：

- 工作台上的审批/选择操作会准确路由回原始 internal session。
- worker 侧状态会同步变化，不需要手动切回内部 session。
- 审批通过或拒绝后，原 worker turn 能继续或结束。

记录点：

- approval id / choice id
- 选择的决策
- 返回到的 worker session id

### 2.5 跳转到单机对话

操作步骤：

1. 在工作台中打开带 `hostId` 的详情卡片或 worker 只读卡片。
2. 点击“切到单机对话”或等效入口。
3. 观察是否自动进入该 host 对应的 `SingleHostSession`。

预期结果：

- 页面路由切换到普通聊天页。
- 当前 host 名称和地址与点击的 worker 卡片一致。
- 单机会话可以继续发起交互，不会影响 workspace 的内部 session 结构。

记录点：

- 跳转前的 host 信息
- 跳转后的 chat session

### 2.6 `mission stop`

操作步骤：

1. 在工作台执行 stop。
2. 观察 planner 和 worker 是否都被中断。
3. 刷新页面确认 mission 终态。

预期结果：

- mission 进入 `cancelled` 或等价终态。
- 所有未完成 worker 任务被清理。
- 工作台投影会出现停止或取消相关卡片。

记录点：

- stop 发起时间
- 被取消的 worker 数
- mission 终态

### 2.7 `host offline / restart reconcile`

操作步骤：

1. 让某台 host 断开或模拟离线。
2. 刷新或等待后端重连检测。
3. 重启 `ai-server`，验证恢复后的 reconcile。

预期结果：

- 离线 host 对应的 worker 任务会被标记为失败或不可用。
- 工作台能看到 host unavailable 或恢复失败的投影卡片。
- 重启后不会把已经失败的 worker 误恢复成运行中。

记录点：

- 离线 host id
- 重启前后 mission 状态
- reconcile 结果

### 2.8 预算 / 排队观察点

操作步骤：

1. 用明显超过预算的 host 数量发起任务，例如 16 台以上。
2. 设置较小的 mission budget，观察前 N 个 worker 是否先启动。
3. 继续推进完成若干 worker，检查后续 queued task 是否被释放。

预期结果：

- 同一时间只会激活预算允许范围内的 worker。
- 剩余任务应处于排队或待启动状态。
- 已完成 worker 释放预算后，后续 worker 会继续启动。

记录点：

- budget 值
- 初始激活数
- 队列长度变化
- 释放预算后的补位情况

## 3. 验收标准

- `WorkspaceSession` 创建后能进入工作台页。
- planner 能稳定派发多主机任务。
- 多主机 worker 可以分批执行，不会互相串台。
- `approval / choice` 在工作台上可直接处理并准确回路。
- 可以从工作台的 host 详情跳到对应单机对话。
- `mission stop` 能清掉未完成任务并收敛到终态。
- `host offline / restart reconcile` 能正确标记失败并恢复状态。
- 预算与队列行为符合 `M12` 里定义的调度语义。

## 4. 验收记录模板

```text
日期：
环境版本：
前端构建版本：
后端 commit：
测试人：

workspace session id：
mission id：
planner session id：
host 列表：
budget：

步骤 2.1 结果：
步骤 2.2 结果：
步骤 2.3 结果：
步骤 2.4 结果：
步骤 2.5 结果：
步骤 2.6 结果：
步骤 2.7 结果：
步骤 2.8 结果：

问题列表：
1.
2.

最终结论：
- 通过 / 不通过
- 是否可进入下一批次：
```
