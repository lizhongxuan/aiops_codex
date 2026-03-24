# MVP Quickstart

本文对应 [desgin_mvp_0324.md](/Users/lizhongxuan/Desktop/aiops-codex/desgin_mvp_0324.md) 和 [todo_mvp_0324.md](/Users/lizhongxuan/Desktop/aiops-codex/todo_mvp_0324.md) 的当前实现。

## 1. 目录说明

- `cmd/ai-server`: 业务服务端，负责 HTTP API、WebSocket、Codex app-server 桥接、Agent gRPC 接入
- `cmd/host-agent`: Go 版 Host Agent
- `internal/codex`: `codex app-server` 的 stdio JSON-RPC 客户端
- `internal/server`: HTTP / WS / gRPC 服务实现
- `web`: Vue 3 前端
- `scripts/smoke_mvp_0324.mjs`: 本地 MVP 冒烟脚本
- `deploy/sql/mvp_0324_schema.sql`: MVP 数据库存储 schema
- `proto/agent.proto`: Host Agent gRPC 协议定义
- `internal/agentrpc/agent.pb.go` / `internal/agentrpc/agent_grpc.pb.go`: checked-in 的 gRPC 代码

## 2. 关键配置项

当前实现已支持这些环境变量：

- `CODEX_APP_SERVER_PATH`
- `AIOPS_HTTP_ADDR`
- `AIOPS_GRPC_ADDR`
- `DEFAULT_WORKSPACE`
- `APP_SESSION_SECRET`
- `APP_SESSION_TTL`
- `APP_STATE_PATH`
- `APP_AUDIT_LOG_PATH`
- `HOST_AGENT_BOOTSTRAP_TOKEN`
- `AGENT_HEARTBEAT_TIMEOUT`
- `GPT_OAUTH_CLIENT_ID`
- `GPT_OAUTH_CLIENT_SECRET`
- `GPT_OAUTH_AUTH_URL`
- `GPT_OAUTH_TOKEN_URL`
- `GPT_OAUTH_REDIRECT_URL`
- `GPT_OAUTH_SCOPES`
- `GPT_OAUTH_USERINFO_URL`
- `GPT_OAUTH_ACCOUNT_ID`
- `GPT_OAUTH_PLAN_TYPE`
- `FRONTEND_REDIRECT_URL`

说明：

- 默认工作区仍然是 `~/.aiops_codex/`
- 当前实现会把稳定会话态落盘到 `APP_STATE_PATH`，包括 GPT 登录态、thread 映射、主机选择和主机在线信息
- 当前实现会把关键审计事件按 JSONL 落盘到 `APP_AUDIT_LOG_PATH`
- `APP_SESSION_SECRET` 已用于签名会话 cookie，避免前端直接伪造 session ID
- `APP_SESSION_TTL` 默认 30 天，用于让浏览器刷新或重开后仍能恢复同一个业务 session
- 当前内存 store 已区分 `web session` 与 `codex auth session`，为后续数据库化保留了边界
- `AGENT_HEARTBEAT_TIMEOUT` 用于将超时未上报心跳的 Agent 自动标记为离线

## 3. 本地启动

### 3.1 启动前端

```bash
cd web
npm install
npm run dev
```

默认地址：

- `http://127.0.0.1:5173`

### 3.2 启动 ai-server

```bash
cd /Users/lizhongxuan/Desktop/aiops-codex
mkdir -p .data/workspace .tools/go-build .tools/gomodcache

AIOPS_HTTP_ADDR=127.0.0.1:18080 \
AIOPS_GRPC_ADDR=127.0.0.1:18090 \
DEFAULT_WORKSPACE=$PWD/.data/workspace \
HOST_AGENT_BOOTSTRAP_TOKEN=change-me \
GOCACHE=$PWD/.tools/go-build \
GOMODCACHE=$PWD/.tools/gomodcache \
go run ./cmd/ai-server
```

说明：

- 生产默认工作区是 `~/.aiops_codex/`
- 上面把工作区改到 `.data/workspace`，只是为了当前开发环境更容易本地验证

### 3.3 启动 Host Agent

```bash
cd /Users/lizhongxuan/Desktop/aiops-codex

AIOPS_SERVER_GRPC_ADDR=127.0.0.1:18090 \
AIOPS_AGENT_BOOTSTRAP_TOKEN=change-me \
AIOPS_AGENT_HOST_ID=agent-local \
AIOPS_AGENT_HOSTNAME=agent-local \
GOCACHE=$PWD/.tools/go-build \
GOMODCACHE=$PWD/.tools/gomodcache \
go run ./cmd/host-agent
```

## 4. 当前已验证

- Go 代码通过 `go test ./...`
- Vue 前端通过 `npm run build`
- `ai-server` 可成功启动 HTTP + gRPC
- `/api/v1/healthz` 可返回 `codexAlive` / `codexLastExit`
- `chatgpt` 登录启动接口可返回 `authUrl`
- GPT 登录成功、失败、刷新恢复三条链路已通过 `scripts/smoke_mvp_0324.mjs`
- `server-local` 对话、命令审批、文件审批、工作区写入边界已通过 `scripts/smoke_mvp_0324.mjs`
- `Approve for session` 已通过 `scripts/smoke_mvp_0324.mjs`
- `host-agent` 可成功注册到 `ai-server`
- `/api/v1/state` 可看到 `server-local`、在线 Agent、`lastActivityAt`
- `host-agent` 离线后可切换为 `offline`，重连后可恢复 `online`
- `internal/store` 已补 `MarkStaleHosts` 和 session grant 的自动化测试
- 审计日志会落盘到 `.data/ai-audit.log`

## 5. 当前未完成

- 数据库运行时仍未接入，当前只是补了 `deploy/sql/mvp_0324_schema.sql`
- 远程主机执行
