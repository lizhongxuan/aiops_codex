#
# codex app-server 打包说明
# ========================
#
# codex 是 OpenAI 开源的 Rust 项目 (https://github.com/openai/codex)
# `codex app-server` 是 codex 二进制的一个子命令，启动后通过 stdio
# 提供双向 JSON-RPC 协议，供 IDE/Web 等客户端驱动 agent 循环。
#
# codex 二进制有三种获取方式（本 Dockerfile 提供全部三种，按需选用）：
#
#   方案 A: 从 GitHub Release 下载预编译的 musl 静态二进制（推荐，最快）
#   方案 B: 从源码编译 Rust 项目
#   方案 C: 通过 npm install -g @openai/codex 安装（需要 Node.js 运行时）
#
# 默认使用方案 A。如需切换，修改 Stage 3 即可。
#

# ============================================================
# Stage 1: 构建前端 (Vue 3)
# ============================================================
FROM node:22-bookworm-slim AS frontend

WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY web/ ./
RUN npm run build

# ============================================================
# Stage 2: 构建 ai-server (Go)
# ============================================================
FROM golang:1.26-bookworm AS backend

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal
COPY proto ./proto

ARG TARGETOS=linux
ARG TARGETARCH=amd64

RUN CGO_ENABLED=0 \
    GOOS=${TARGETOS} \
    GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" -o /out/ai-server ./cmd/ai-server

# ============================================================
# Stage 3: 获取 codex 二进制
# ============================================================
#
# ---- 方案 A: 从 GitHub Release 下载预编译静态二进制 (推荐) ----
#
# codex 用 Rust 编译，Release 产物是 musl 静态链接的单文件二进制，
# 零运行时依赖，直接放进任何 Linux 镜像就能跑。
#
# 产物命名规则:
#   x86_64:  codex-x86_64-unknown-linux-musl
#   arm64:   codex-aarch64-unknown-linux-musl
#
# 打包在 tar.gz 里，解压后就是单个可执行文件。
#
FROM debian:bookworm-slim AS codex-download

RUN apt-get update && apt-get install -y --no-install-recommends curl ca-certificates \
    && rm -rf /var/lib/apt/lists/*

# 可通过 build arg 指定版本，默认 latest
ARG CODEX_VERSION=latest
ARG TARGETARCH=amd64

# 映射 docker TARGETARCH → codex release 命名
# amd64 → x86_64-unknown-linux-musl
# arm64 → aarch64-unknown-linux-musl
RUN set -eux; \
    case "${TARGETARCH}" in \
      amd64) ARCH="x86_64" ;; \
      arm64) ARCH="aarch64" ;; \
      *) echo "unsupported arch: ${TARGETARCH}" && exit 1 ;; \
    esac; \
    BINARY_NAME="codex-${ARCH}-unknown-linux-musl"; \
    if [ "${CODEX_VERSION}" = "latest" ]; then \
      DOWNLOAD_URL="https://github.com/openai/codex/releases/latest/download/${BINARY_NAME}.tar.gz"; \
    else \
      DOWNLOAD_URL="https://github.com/openai/codex/releases/download/${CODEX_VERSION}/${BINARY_NAME}.tar.gz"; \
    fi; \
    echo "Downloading codex from: ${DOWNLOAD_URL}"; \
    curl -fSL "${DOWNLOAD_URL}" -o /tmp/codex.tar.gz; \
    tar -xzf /tmp/codex.tar.gz -C /tmp/; \
    mv /tmp/${BINARY_NAME} /usr/local/bin/codex; \
    chmod +x /usr/local/bin/codex; \
    rm -f /tmp/codex.tar.gz; \
    /usr/local/bin/codex --version

#
# ---- 方案 B: 从源码编译 (如果你需要定制或用未发布版本) ----
#
# 取消下面的注释，注释掉方案 A 即可:
#
# FROM rust:1.82-bookworm AS codex-build
# RUN apt-get update && apt-get install -y --no-install-recommends \
#     musl-tools pkg-config libssl-dev \
#     && rm -rf /var/lib/apt/lists/*
# RUN rustup target add x86_64-unknown-linux-musl
# WORKDIR /src
# RUN git clone --depth 1 https://github.com/openai/codex.git .
# WORKDIR /src/codex-rs
# RUN cargo build --release --target x86_64-unknown-linux-musl --bin codex
# RUN cp target/x86_64-unknown-linux-musl/release/codex /usr/local/bin/codex
#
# 然后把下面 Stage 4 的 COPY --from=codex-download 改成 COPY --from=codex-build
#

#
# ---- 方案 C: npm 安装 (需要 Node.js 运行时，镜像会大很多) ----
#
# FROM node:22-bookworm-slim AS codex-npm
# RUN npm install -g @openai/codex@latest
#
# 注意: npm 包内部也是下载对应平台的预编译二进制，
# 但安装后 codex 二进制在 node_modules 里，需要 Node.js 运行时。
# 最终镜像需要换成 node:22-bookworm-slim 基础镜像。
# 不推荐，因为白白多了 ~200MB 的 Node.js 运行时。
#

# ============================================================
# Stage 4: 最终运行镜像
# ============================================================
FROM debian:bookworm-slim

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
       bash ca-certificates tzdata git curl socat \
    && rm -rf /var/lib/apt/lists/* \
    && useradd --system --create-home --uid 10001 aiops

# codex 二进制 (Rust 静态编译，零运行时依赖)
COPY --from=codex-download /usr/local/bin/codex /usr/local/bin/codex

# ai-server 二进制 (Go 静态编译)
COPY --from=backend /out/ai-server /usr/local/bin/ai-server

# 前端构建产物
COPY --from=frontend /src/web/dist /app/web/dist

# 启动脚本
COPY deploy/docker/entrypoint.sh /app/entrypoint.sh

WORKDIR /app

RUN mkdir -p /data/workspace /data/codex-home \
    && chown -R aiops:aiops /data /app \
    && chmod +x /app/entrypoint.sh

USER aiops

# codex app-server 运行时说明:
#
# ai-server 启动时会执行:
#   exec.Command(CODEX_APP_SERVER_PATH, "app-server")
#
# 这会启动 codex 的 app-server 子命令，通过 stdio 提供 JSON-RPC 协议。
# codex app-server 需要:
#   1. CODEX_HOME 目录 (存放 auth.json, config.toml, memories 等)
#   2. 认证信息 (API Key 或 ChatGPT OAuth token)
#
# 认证方式:
#   - API Key: 设置 CODEX_API_KEY 环境变量，codex 会自动使用
#   - ChatGPT OAuth: 通过前端登录流程，ai-server 会调用
#     account/login/start JSON-RPC 方法驱动 OAuth
#   - 预置 auth.json: 把 ~/.codex/auth.json 挂载到 /data/codex-home/auth.json
#
ENV HOME=/home/aiops \
    CODEX_HOME=/data/codex-home \
    CODEX_APP_SERVER_PATH=/usr/local/bin/codex \
    AIOPS_HTTP_ADDR=0.0.0.0:8080 \
    AIOPS_GRPC_ADDR=0.0.0.0:18090 \
    DEFAULT_WORKSPACE=/data/workspace \
    APP_STATE_PATH=/data/ai-server-state.json \
    APP_AUDIT_LOG_PATH=/data/ai-audit.log \
    HOST_AGENT_BOOTSTRAP_TOKEN=change-me \
    LANG=C.UTF-8

EXPOSE 8080 18090 1455

HEALTHCHECK --interval=15s --timeout=5s --start-period=10s --retries=3 \
    CMD curl -sf http://127.0.0.1:8080/api/v1/healthz || exit 1

ENTRYPOINT ["/app/entrypoint.sh"]
