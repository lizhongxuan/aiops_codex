#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CODEX_BIN="${CODEX_APP_SERVER_PATH:-codex}"
CODEX_HOME_DIR="${CODEX_HOME:-$HOME/.codex}"

require_cmd() {
  local cmd="$1"
  if ! command -v "$cmd" >/dev/null 2>&1; then
    echo "Missing required command: $cmd" >&2
    exit 1
  fi
}

require_cmd go
require_cmd "$CODEX_BIN"

if [[ -z "${CODEX_API_KEY:-}" && ! -f "$CODEX_HOME_DIR/auth.json" ]]; then
  cat >&2 <<EOF
Missing Codex authentication.

Set CODEX_API_KEY, or ensure this file exists:
  $CODEX_HOME_DIR/auth.json
EOF
  exit 1
fi

mkdir -p "$ROOT_DIR/.data" "$ROOT_DIR/.gocache"

export GOCACHE="${GOCACHE:-$ROOT_DIR/.gocache}"
export AIOPS_HTTP_ADDR="${AIOPS_HTTP_ADDR:-127.0.0.1:18080}"
export AIOPS_GRPC_ADDR="${AIOPS_GRPC_ADDR:-127.0.0.1:18090}"
export FRONTEND_REDIRECT_URL="${FRONTEND_REDIRECT_URL:-http://127.0.0.1:5173/}"
export APP_STATE_PATH="${APP_STATE_PATH:-$ROOT_DIR/.data/ai-server-state.json}"
export APP_AUDIT_LOG_PATH="${APP_AUDIT_LOG_PATH:-$ROOT_DIR/.data/ai-audit.log}"

cd "$ROOT_DIR"

echo "Starting ai-server"
echo "  HTTP: $AIOPS_HTTP_ADDR"
echo "  gRPC: $AIOPS_GRPC_ADDR"
echo "  Frontend redirect: $FRONTEND_REDIRECT_URL"

exec go run ./cmd/ai-server
