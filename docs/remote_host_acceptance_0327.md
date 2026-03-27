# 远程主机验收映射（2026-03-27）

本文把 `design_remove_0325.md` 第 10 章的 9 条最终验收标准，映射到当前仓库里可执行的 smoke / 自动化检查项，便于后续按同一套标准回归。

## 自动化入口

- 远程 host smoke：`GOCACHE=$PWD/.tools/go-build GOMODCACHE=$PWD/.tools/gomodcache go run ./scripts/smoke_remote_host_0327.go`
- 远程 exec / cancel / host binding 单测：`GOCACHE=$PWD/.tools/go-build GOMODCACHE=$PWD/.tools/gomodcache go test ./cmd/host-agent ./internal/server ./internal/store`

## smoke 环境变量

- `AIOPS_BASE_URL`：默认 `http://127.0.0.1:18080`
- `AIOPS_REMOTE_HOST_ID`：可选；不传时自动选择第一台在线且可执行的远程主机
- `AIOPS_REMOTE_CPU_COMMAND`：默认 `uptime`
- `AIOPS_REMOTE_DISK_COMMAND`：默认 `df -h /`
- `AIOPS_REMOTE_SERVICE_COMMAND`：默认 `systemctl status ssh --no-pager`
- `AIOPS_REMOTE_LOG_COMMAND`：默认 `journalctl -n 20 --no-pager`

如果远程主机的服务名不是 `ssh`，或日志命令需要调整，覆盖对应环境变量即可。

## 验收映射

| 设计标准 | 可执行检查项 | 自动化覆盖 |
| --- | --- | --- |
| 1. 选中远程主机后，聊天默认只操作该主机 | `POST /api/v1/host/select` 后检查 `selectedHostId` 与 runtime host 绑定 | `scripts/smoke_remote_host_0327.go` |
| 2. 远程只读操作能稳定执行并有过程反馈 | 在同一远程 host 上执行 CPU / 磁盘 / 服务 / 日志 4 条只读命令，并校验命令卡落到目标 host | `scripts/smoke_remote_host_0327.go` |
| 3. 远程 mutation 默认走审批，审批后能继续执行，不会卡死 | 触发 `remote_command` 审批，接受后等待对应命令卡完成 | `scripts/smoke_remote_host_0327.go` |
| 4. 停止任务可以真正中断远程执行 | stop 时向 agent 发送 `exec/cancel`，并把命令卡稳定落为 `cancelled` | `go test ./internal/server` 中的 `TestMarkTurnInterruptedSendsRemoteCancelAndKeepsCancelledCard` |
| 5. 远程终端与聊天执行目标一致 | 选中 host 后创建同 host 终端 session，并通过 WebSocket 收到 `ready/exit` | `scripts/smoke_remote_host_0327.go` |
| 6. 远程错误不会伪装成成功 | 非零退出、权限错误、断连、timeout、cancel 都映射到明确状态 | `go test ./cmd/host-agent ./internal/server` |
| 7. 用户不会长时间面对“正在思考”而无过程反馈 | smoke 中要求命令卡和审批卡在执行期出现；单测保住 stalled turn 自动失败 | `scripts/smoke_remote_host_0327.go` + `go test ./internal/server` |
| 8. 远程文件浏览与远程文件修改逐步接近本地 Codex 卡片体验 | 文件搜索结果结构化卡、远程 file change 审批/落盘链路保底 | `go test ./internal/server`（当前偏保底，不代表体验已对齐） |
| 9. 网络超时会出现重试提醒 | timeout / host timeout 状态需要被结构化回传并在 UI 可区分 | `go test ./internal/server`（当前覆盖状态映射，重试提醒体验仍待补齐） |

## 当前解读

- 这份映射解决的是“标准如何执行检查”的问题，因此 `15.1` 可以按文档与脚本落地视为完成。
- 其中第 7、8、9 条还存在体验层 gap，自动化已能发现回归，但不代表体验目标已经完全达到。
