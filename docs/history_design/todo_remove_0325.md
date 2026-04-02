# 远程主机管理实现任务清单

基于 `design_remove_0325.md` 对“远程主机管理能力”的设计目标，将落地工作整理为以下 3 个阶段与横切治理项。以下状态基于设计文档与当前仓库中的相关实现初步判断，后续可按真实开发进度继续更新。

状态说明：
- `[x]` 已具备或已完成
- `[ ]` 未完成
- `（部分完成）` 表示已有链路或局部实现，但还未做到稳定的端到端体验

## 0. 当前已具备基线（按设计文档与现有代码初判）

- [x] 0.1 `host-agent` 可主动注册到 `ai-server`，远程主机可在前端展示在线状态。
- [x] 0.2 页面已具备进入远程主机终端的入口。
- [x] 0.3 `ai-server` 已注册 `execute_readonly_query`、`execute_system_mutation` 等远程动态工具。
- [x] 0.4 远程文件工具已具备基础骨架：`list_remote_files`、`read_remote_file`、`search_remote_files`。
- [x] 0.5 远程命令审批与远程文件变更审批卡已具备基础实现。
- [x] 0.6 需要补一组稳定的 smoke / 回归用例，固化当前可用能力，避免后续改造破坏现有链路。

## Phase A: 远程命令管理可用

目标：
- 远程主机上线后，聊天默认操作选中的目标主机。
- 远程只读命令可跑，远程变更命令可审批，远程终端可进入。

验收：
- 用户选中一台 Linux 主机后，可以直接查看 CPU / 磁盘 / 服务状态并看到输出。
- 有风险的远程命令会弹审批，审批后能继续执行。

### 1. 主机上下文强绑定（`web` / `internal/server` / `internal/store` / `internal/codex`）

- [x] 1.1 前端主机选择器切换时，把当前 `hostId` 显式绑定到当前会话 / 线程，而不是只做视觉筛选。
- [x] 1.2 `ai-server` 在线程或会话维度持久化当前目标主机；切换主机时清空旧绑定或切到新线程。
- [x] 1.3 `codex app-server` 的线程提示词与动态工具入参都要强制携带远程主机语义，防止回落到本地 `commandExecution` / `fileChange`。
- [x] 1.4 Host Selector、命令卡、审批卡、终端页统一展示当前目标主机的 `hostName / hostId`。

### 2. 远程只读查询闭环（`internal/server/dynamic_tools.go` / `internal/server/remote_exec.go` / `cmd/host-agent`）

- [x] 2.1 固化 `execute_readonly_query` 的 allowlist / 只读校验，确保 `systemctl status`、`journalctl`、`grep`、`tail`、`find` 等命令可用，带副作用命令被拒绝。
- [x] 2.2 统一 `host-agent` 回传结构，至少稳定包含 `stdout / stderr / exitCode / timeout / cancelled / error`。
- [x] 2.3 只读执行结果在前端展示“过程态 + 摘要态”，避免退化成纯黑盒 shell 文本。
- [x] 2.4 增加 CPU、磁盘、服务状态、日志检查等典型 smoke case。

### 3. 远程变更命令审批闭环（`internal/server` / `internal/store` / `cmd/host-agent`）

- [x] 3.1 审批请求必须记录 `host / command / cwd / reason / fingerprint` 等关键字段。
- [x] 3.2 支持 `accept / reject / accept_session` 后从安全点继续执行，不因审批造成线程卡死。
- [x] 3.3 远程 mutation 成功时收敛成摘要，失败时保留完整输出与 `exitCode`。
- [x] 3.4 “停止任务”要真正传递到远程执行取消链路，而不只是停止前端动画。

### 4. 远程终端一致性与资源回收（`web` / `internal/server` / `cmd/host-agent`）

- [x] 4.1 终端页必须与当前 `selected host` 强绑定，避免聊天和终端指向不同机器。
- [x] 4.2 长时间终端会话与命令执行都支持中断、超时与会话回收。
- [x] 4.3 `host-agent` 断连或心跳超时时，要明确反馈到 UI，并阻止静默回退到本地执行。
- [x] 4.4 增加端到端验收脚本：选主机 -> 只读排查 -> 审批执行 -> 进入同主机终端。

## Phase B: 远程主机体验对齐本地

目标：
- 文件浏览与文件搜索过程反馈完整。
- 审批队列、停止任务、命令卡片、错误卡片与 UI 过程摘要的体验接近本地 Codex。

验收：
- 用户主观感受已经接近“在本地 Codex 中操作远程 Linux 主机”的节奏。

### 5. 过程反馈协议化（`internal/server/server.go` / `web/src/components/ThinkingCard.vue` / `web/src/components/ProcessLineCard.vue`）

- [x] 5.1 统一远程活动信号为 `browsing / searching / executing / waiting_approval / finalizing / completed` 等有限集合。
- [x] 5.2 顶部同一时刻只高亮一个当前态，忽略重复刷新的相同状态，避免刷屏。
- [x] 5.3 下方过程行只保留轻量摘要，如 `已浏览 3 个文件`、`已搜索 2 次`、`已处理 1 个命令`。
- [x] 5.4 为文件浏览、文件搜索、网页搜索、命令执行补齐“开始 / 完成”成对事件。

### 6. 审批工作台与队列（`web/src/pages/ChatPage.vue` / `internal/server`，部分完成）

- [x] 6.1 固化“输入区覆盖式审批工作台”为唯一活跃审批入口，不再让审批卡在聊天流刷屏。
- [x] 6.2 支持审批队列，一次只呈现一个待处理审批，请求按顺序续跑。
- [x] 6.3 审批拒绝后允许用户继续输入“换个方式做”，而不是阻塞当前线程。
- [x] 6.4 审批完成后在聊天流写入中性纪要，并支持“本会话记住”语义。

### 7. 命令卡片与错误卡片统一（`web/src/components` / `internal/server`）

- [x] 7.1 远程 `CommandCard` 与本地展示规则统一：运行中流式输出，成功折叠，失败展开。
- [x] 7.2 长命令或脚本增加 shell 类型识别与折叠显示，避免撑爆布局。
- [x] 7.3 错误类型至少区分：审批拒绝、agent 断连、OS 权限不足、命令退出失败、网络超时、终端断开。
- [x] 7.4 错误卡片要明确展示失败原因、是否可重试、是否建议换方案。

### 8. 结构化远程文件浏览与搜索（`internal/server/remote_files.go` / `web/src/components`，部分完成）

- [x] 8.1 将 `list_remote_files`、`read_remote_file`、`search_remote_files` 的结果稳定渲染为结构化文件卡，而不是退化成 shell 文本。
- [x] 8.2 前端支持文件名高亮、hover 绝对路径、文件列表与普通正文分离。
- [x] 8.3 补齐点击文件名打开远程文件预览的交互。
- [x] 8.4 搜索结果展示 `path / line / snippet`，并在时间线摘要中汇总文件数与命中数。

### 9. 超时、停止与重试体验（`web` / `internal/server` / `cmd/host-agent`）

- [x] 9.1 网络或心跳超时时给出重试提醒与当前 host 状态，而不是长时间停留在“正在思考”。
- [x] 9.2 远程执行卡住时自动落成 `failed / aborted`，不能伪装为成功。
- [x] 9.3 前端重连后可恢复对当前 turn、approval、active host 的感知。
- [x] 9.4 为 `stop / timeout / reconnect / agent offline` 场景补回归测试。

## Phase C: 远程文件修改对齐本地

目标：
- 支持结构化远程 `file_change`。
- 支持 diff、审批卡、文件预览与结果回看。

验收：
- 用户可以像本地 Codex 一样，让系统修改远程配置文件并审批确认。

### 10. 结构化远程文件修改协议（`internal/server/dynamic_tools.go` / `cmd/host-agent`，部分完成）

- [x] 10.1 固化 `execute_system_mutation(mode=file_change)` 参数协议：`path / content / write_mode / reason / host`。
- [x] 10.2 明确“审批前不落盘、审批后才写入”的执行边界，并保证一次审批只对应一次实际文件写入。
- [x] 10.3 远程写文件失败时返回明确的权限、路径、IO 错误，不吞掉错误细节。
- [x] 10.4 为覆盖写、追加写、目标文件不存在等场景补自动化测试。

### 11. 文件修改审批卡与 diff 体验（`web` / `internal/server`）

- [x] 11.1 `FileChangeApprovalCard` 展示目标主机、目标路径、写入方式、变更原因和摘要 diff。
- [x] 11.2 `FileChangeCard` 保留 before / after 或 diff 结果，便于后续回看。
- [x] 11.3 审批通过后再真正落盘，并把执行结果同步到聊天流与审计日志。
- [x] 11.4 完成典型场景验收：读取 nginx 配置 -> 生成修改 -> 审批 -> 落盘 -> 重启服务 -> 回显结果。

### 12. 远程文件体验补齐到接近本地（`web` / `internal/server`）

- [x] 12.1 远程文件修改后的回看、重开预览、再次搜索能力要与本地 `fileChange` 节奏对齐。
- [x] 12.2 聊天流中的“已修改 N 个文件”摘要要来自真实远程 `file_change` 结果。
- [x] 12.3 UI 文案、卡片样式、失败反馈尽量与本地 Codex 一致。
- [x] 12.4 为多文件连续修改场景补交互与回归用例。

## 横切治理项

### 13. 安全与审计（`internal/config` / `internal/server` / `internal/store`）

- [x] 13.1 固化只读与变更分层规则：只读快路径默认免审批，mutation 默认审批。
- [x] 13.2 审批与执行记录至少包含 `sessionId / threadId / turnId / hostId / hostName / operator / toolName / command 或 filePath / cwd / approvalDecision / startedAt / endedAt / status / exitCode`。
- [x] 13.3 `host-agent` 维持最小信任模型：bootstrap token、来源限制、固定 host identity。
- [x] 13.4 为开发态 `0.0.0.0:18090` 与生产态内网 / VPN / TLS 配置补明显提示与文档约束。

### 14. Docker 化接入与运维文档（`deploy/docker` / `cmd/host-agent` / `cmd/ai-server`）

- [x] 14.1 `deploy/docker` 已具备 `ai-server` / `host-agent` Dockerfile 与示例配置骨架。
- [x] 14.2 校验 `host-agent` 镜像满足 shell、pseudo-TTY、回连 `ai-server` 的最低运行要求。
- [x] 14.3 整理并核对 `AIOPS_SERVER_GRPC_ADDR`、`AIOPS_AGENT_HOST_ID`、`AIOPS_AGENT_HOSTNAME`、`AIOPS_AGENT_BOOTSTRAP_TOKEN` 等必填环境变量。
- [x] 14.4 补一份 Linux 主机容器化部署、升级与排障说明，以及最小 smoke 检查步骤。

### 15. 测试与最终验收（全链路）

- [x] 15.1 将设计文档第 10 章的 9 条验收标准映射为可执行检查项和自动化 smoke。
- [x] 15.2 补单元测试：host binding、审批续跑、timeout / cancel、error mapping、file search / file change。
- [x] 15.3 补端到端测试：选主机、只读查询、审批 mutation、停止任务、终端一致性、文件浏览、文件修改。
- [x] 15.4 每个阶段结束后输出一次 gap list，确认是否具备进入下一 Phase 的条件。
