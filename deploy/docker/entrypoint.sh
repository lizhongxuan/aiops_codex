#!/bin/bash
set -e

# codex OAuth 回调服务器绑定 127.0.0.1:1455 (loopback only)
# Docker 端口映射流量到达容器的 eth0，不是 loopback
# 用 socat 监听 0.0.0.0:14550 (容器 eth0 可达)，转发到 127.0.0.1:1455
socat TCP-LISTEN:14550,bind=0.0.0.0,fork,reuseaddr TCP:127.0.0.1:1455 &

exec /usr/local/bin/ai-server "$@"
