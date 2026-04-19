#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
WEB_DIR="$ROOT_DIR/web"

require_cmd() {
  local cmd="$1"
  if ! command -v "$cmd" >/dev/null 2>&1; then
    echo "Missing required command: $cmd" >&2
    exit 1
  fi
}

require_cmd node
require_cmd npm

cd "$WEB_DIR"

if [[ ! -d node_modules ]]; then
  if [[ -f package-lock.json ]]; then
    echo "Installing frontend dependencies with npm ci"
    npm ci
  else
    echo "Installing frontend dependencies with npm install"
    npm install
  fi
fi

echo "Building latest frontend code"
npm run build

echo "Starting web dev server on http://127.0.0.1:5173"
echo "Proxy target is configured in web/vite.config.js"

exec npm run dev -- --host 127.0.0.1
