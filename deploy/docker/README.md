# AIOps Codex Docker 部署指南

## 架构概览

```
                    ┌─────────────────────────────────────────┐
                    │         ai-server 容器                   │
                    │                                          │
 浏览器 ──HTTP──►  │  ai-server (Go)                          │
                    │      │                                   │
                    │      │ exec + stdio pipe (JSON-RPC)      │
                    │      ▼                                   │
                    │  codex app-server (Rust)                 │
                    │      │                                   │
                    │      │ HTTPS                             │
                    │      ▼                                   │
                    │  OpenAI API                              │
                    ├──────────────────────────────────────────┤
                    │  端口:                                    │
                    │    8080  → HTTP + WebSocket + 前端        │
                    │    18090 → gRPC (host-agent 接入)         │
                    └──────────┬───────────────────────────────┘
                               │ gRPC 双向流
                    ┌──────────▼───────────────────────────────┐
                    │  host-agent 容器 (每台目标主机一个)         │
                    │  host-agent (Go) → 终端/执行/文件操作      │
                    └──────────────────────────────────────────┘
```

ai-server 和 codex app-server 打在同一个镜像里，原因是 ai-server
通过 `exec.Command("codex", "app-server")` 把 codex 作为子进程拉起，
两者通过 stdio pipe 通信，必须在同一个进程空间。

## 镜像里有什么

ai-server.Dockerfile 是 4 阶段多阶段构建：

```
Stage 1 (frontend)     : Node 22 → npm ci → npm run build → web/dist/
Stage 2 (backend)      : Go 1.26 → go build → ai-server 二进制
Stage 3 (codex)        : curl 下载 GitHub Release → codex 二进制
Stage 4 (final)        : debian:bookworm-slim + 三个产物合并
```

最终镜像内容：

```
/usr/local/bin/codex       ← Rust 静态二进制 (musl, ~50MB)
/usr/local/bin/ai-server   ← Go 静态二进制 (~15MB)
/app/web/dist/             ← Vue 3 前端构建产物
/data/                     ← 运行时数据目录
```

基础镜像是 `debian:bookworm-slim`，不需要 Node.js 运行时。
codex 是 musl 静态链接，ai-server 是 CGO_ENABLED=0，都是零依赖。

## 快速开始

### 1. 准备环境变量

```bash
cd deploy/docker
cp .env.example .env
```

编辑 `.env`：

```bash
# 必填: host-agent 接入 token
HOST_AGENT_BOOTSTRAP_TOKEN=your-secure-token-here

# 必填: OpenAI 认证 (三选一，见下方"认证方式"章节)
CODEX_API_KEY=sk-xxx
```

### 2. 构建并启动

```bash
# 构建镜像 (首次约 3-5 分钟)
docker compose build

# 启动
docker compose up -d

# 查看日志
docker compose logs -f ai-server

# 查看健康状态
docker compose ps
```

### 3. 访问

- 前端: http://127.0.0.1:18080
- 健康检查: http://127.0.0.1:18080/api/v1/healthz
- gRPC: 127.0.0.1:18090 (host-agent 接入)

## 认证方式

codex app-server 需要 OpenAI 认证才能工作。三种方式：

### 方式 1: API Key（推荐服务器环境）

最简单。在 `.env` 里设置：

```bash
CODEX_API_KEY=sk-proj-xxxx
```

或者在 docker-compose.yml 的 environment 里加：

```yaml
- CODEX_API_KEY=sk-proj-xxxx
```

codex 启动时会自动读取这个环境变量。

### 方式 2: 挂载 auth.json

先在本地机器上完成 codex 登录：

```bash
# 本地安装 codex
npm install -g @openai/codex

# 登录 (会打开浏览器)
codex login

# 登录成功后 auth.json 在这里:
ls ~/.codex/auth.json
```

然后把 auth.json 挂载进容器：

```yaml
# docker-compose.yml
services:
  ai-server:
    volumes:
      - ai-data:/data
      - ~/.codex/auth.json:/data/codex-home/auth.json:ro
```

### 方式 3: 前端 ChatGPT OAuth 登录

需要配置 OAuth 参数（适合有 ChatGPT Team/Enterprise 的场景）：

```yaml
environment:
  - GPT_OAUTH_CLIENT_ID=your-client-id
  - GPT_OAUTH_CLIENT_SECRET=your-client-secret
  - GPT_OAUTH_AUTH_URL=https://auth0.openai.com/authorize
  - GPT_OAUTH_TOKEN_URL=https://auth0.openai.com/oauth/token
  - GPT_OAUTH_REDIRECT_URL=http://your-server:18080/api/v1/auth/oauth/callback
  - GPT_OAUTH_ACCOUNT_ID=your-account-id
```

启动后在前端页面点击登录，走 OAuth 流程。

## codex 版本管理

### 指定 codex 版本

默认拉取 latest release。指定版本：

```bash
docker compose build --build-arg CODEX_VERSION=0.117.0-alpha.21
```

### 升级 codex

```bash
# 重新构建 (会拉取最新 release)
docker compose build --no-cache ai-server

# 重启
docker compose up -d ai-server
```

### 锁定版本（生产推荐）

在 docker-compose.yml 里固定版本：

```yaml
services:
  ai-server:
    build:
      args:
        CODEX_VERSION: "0.117.0-alpha.21"
```

## 多架构支持

Dockerfile 支持 amd64 和 arm64：

```bash
# 构建 amd64 (默认)
docker compose build

# 构建 arm64 (如部署到 ARM 服务器)
docker compose build --build-arg TARGETARCH=arm64

# 或者用 buildx 同时构建两个架构
docker buildx build \
  --platform linux/amd64,linux/arm64 \
  -f deploy/docker/ai-server.Dockerfile \
  -t aiops-codex:latest \
  --push .
```

## 数据持久化

所有运行时数据在 `/data` volume 里：

```
/data/
├── workspace/              ← codex 工作区 (agent 读写文件的地方)
├── codex-home/             ← CODEX_HOME (auth.json, config.toml, memories/)
│   ├── auth.json           ← 认证信息
│   ├── config.toml         ← codex 配置
│   └── memories/           ← agent 记忆
├── ai-server-state.json    ← 会话/主机/认证状态持久化
└── ai-audit.log            ← 审计日志 (JSONL)
```

备份：

```bash
# 备份整个数据目录
docker compose exec ai-server tar czf - /data > backup.tar.gz

# 或者直接备份 volume
docker run --rm -v ai-data:/data -v $(pwd):/backup \
  debian:bookworm-slim tar czf /backup/ai-data-backup.tar.gz /data
```

## host-agent 部署

### Docker 方式（测试环境）

docker-compose.yml 里已经包含一个 `host-agent-local` 服务用于测试。

### 二进制方式（生产环境，推荐）

host-agent 应该直接装在目标主机上，不要装在容器里，
否则它只能操作容器内部而不是宿主机。

```bash
# 在目标主机上构建
docker build -f deploy/docker/host-agent.Dockerfile -o type=local,dest=./out .

# 或者直接 go build
CGO_ENABLED=0 go build -o host-agent ./cmd/host-agent

# 运行
AIOPS_SERVER_GRPC_ADDR=ai-server-ip:18090 \
AIOPS_AGENT_HOST_ID=web-01 \
AIOPS_AGENT_HOSTNAME=web-01 \
AIOPS_AGENT_BOOTSTRAP_TOKEN=your-token \
AIOPS_AGENT_LABELS=env=prod,role=web \
./host-agent
```

### systemd 服务（Linux 生产环境）

```ini
# /etc/systemd/system/aiops-host-agent.service
[Unit]
Description=AIOps Host Agent
After=network.target

[Service]
Type=simple
User=ops
ExecStart=/usr/local/bin/host-agent
Restart=always
RestartSec=3
Environment=AIOPS_SERVER_GRPC_ADDR=ai-server-ip:18090
Environment=AIOPS_AGENT_HOST_ID=web-01
Environment=AIOPS_AGENT_HOSTNAME=web-01
Environment=AIOPS_AGENT_BOOTSTRAP_TOKEN=your-token
Environment=AIOPS_AGENT_LABELS=env=prod,role=web

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl enable --now aiops-host-agent
```

## 生产环境安全加固

### 启用 TLS

```yaml
# docker-compose.yml
services:
  ai-server:
    volumes:
      - ai-data:/data
      - ./certs:/certs:ro
    environment:
      - HOST_AGENT_SECURITY_PROFILE=production
      - AIOPS_GRPC_TLS_CERT_FILE=/certs/server.pem
      - AIOPS_GRPC_TLS_KEY_FILE=/certs/server-key.pem
      - AIOPS_GRPC_TLS_CLIENT_CA_FILE=/certs/ca.pem
      - HOST_AGENT_ALLOWED_HOST_IDS=web-01,web-02,db-01
      - HOST_AGENT_ALLOWED_CIDRS=10.0.0.0/8,172.16.0.0/12
```

host-agent 端对应配置：

```bash
AIOPS_AGENT_TLS_CA_FILE=/certs/ca.pem
AIOPS_AGENT_TLS_CERT_FILE=/certs/agent.pem
AIOPS_AGENT_TLS_KEY_FILE=/certs/agent-key.pem
AIOPS_AGENT_TLS_SERVER_NAME=aiops-server.internal
```

### 更换 bootstrap token

```bash
# 生成安全 token
openssl rand -hex 32

# 支持多 token 轮转
HOST_AGENT_BOOTSTRAP_TOKENS=new-token-2026-04,old-token-2026-03
```

## 故障排查

### codex app-server 没启动

```bash
# 查看日志
docker compose logs ai-server | grep codex

# 进容器检查
docker compose exec ai-server codex --version
docker compose exec ai-server codex app-server --help

# 检查健康
curl http://127.0.0.1:18080/api/v1/healthz
# 如果 codexAlive: false，说明 codex 子进程没跑起来
```

常见原因：
- 没有认证信息（设置 CODEX_API_KEY 或挂载 auth.json）
- codex 版本太旧不支持 app-server 子命令
- 网络不通（codex 需要访问 api.openai.com）

### host-agent 连不上

```bash
# 检查 gRPC 端口
docker compose exec ai-server curl -v telnet://127.0.0.1:18090

# 检查 host-agent 日志
docker compose logs host-agent-local

# 检查主机列表
curl http://127.0.0.1:18080/api/v1/state | jq '.hosts'
```

常见原因：
- bootstrap token 不匹配
- gRPC 端口没暴露或被防火墙拦截
- TLS 配置不一致

## 文件清单

```
deploy/docker/
├── ai-server.Dockerfile       ← ai-server + codex + 前端 (多阶段构建)
├── host-agent.Dockerfile      ← host-agent (独立镜像)
├── docker-compose.yml         ← 编排文件
├── .env.example               ← 环境变量模板
├── ai-server.env.example      ← ai-server 完整环境变量参考
├── host-agent.env.example     ← host-agent 完整环境变量参考
└── README.md                  ← 本文档
```
