#!/usr/bin/env bash
set -euo pipefail

PROJECT_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
CONFIG_PATH="${PORTKEY_E2E_CONFIG:-$PROJECT_ROOT/configs/config.test.yaml}"
BACKEND_PID=""
FRONTEND_PID=""

cleanup() {
  if [[ -n "$FRONTEND_PID" ]]; then
    kill "$FRONTEND_PID" 2>/dev/null || true
    wait "$FRONTEND_PID" 2>/dev/null || true
  fi
  if [[ -n "$BACKEND_PID" ]]; then
    kill "$BACKEND_PID" 2>/dev/null || true
    wait "$BACKEND_PID" 2>/dev/null || true
  fi
}
trap cleanup EXIT

wait_for_health() {
  local url="$1"
  for _ in $(seq 1 30); do
    if curl -fsS "$url" >/dev/null 2>&1; then
      return 0
    fi
    sleep 1
  done
  return 1
}

cd "$PROJECT_ROOT"
go build -o /tmp/portkey-e2e cmd/portkey/main.go
/tmp/portkey-e2e -mode all -config "$CONFIG_PATH" > /tmp/portkey-e2e.log 2>&1 &
BACKEND_PID=$!

if ! wait_for_health http://127.0.0.1:8002/health; then
  cat /tmp/portkey-e2e.log
  exit 1
fi

cd "$PROJECT_ROOT/dashboard"
./node_modules/.bin/vite --host 127.0.0.1 --port 5173 --config vite.config.e2e.ts > /tmp/dashboard-e2e.log 2>&1 &
FRONTEND_PID=$!

if ! wait_for_health http://127.0.0.1:5173/; then
  cat /tmp/dashboard-e2e.log
  exit 1
fi

if [[ $# -eq 0 ]]; then
  set -- e2e/milestone9.spec.ts
fi
./node_modules/.bin/playwright test "$@" --project=chromium
