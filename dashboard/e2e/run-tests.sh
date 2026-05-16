#!/bin/bash
set -e

# Milestone 9 E2E 测试启动脚本
# 自动启动后端 + 前端，运行 Playwright 测试，然后清理

cd "$(dirname "$0")/.."
PROJECT_ROOT="$(cd .. && pwd)"

echo "=== Milestone 9 E2E 测试 ==="

# 1. 构建后端
echo "[1/5] 构建后端..."
cd "$PROJECT_ROOT"
go build -o /tmp/portkey-e2e cmd/portkey/main.go

# 2. 启动后端
echo "[2/5] 启动后端 (Control: 8002, Data: 8081)..."
/tmp/portkey-e2e -mode all -config configs/config.test.yaml > /tmp/portkey-e2e.log 2>&1 &
BACKEND_PID=$!
sleep 3

# 健康检查
if ! curl -sf http://127.0.0.1:8002/health > /dev/null; then
  echo "后端启动失败"
  cat /tmp/portkey-e2e.log
  exit 1
fi
echo "后端已就绪"

# 3. 启动前端
echo "[3/5] 启动前端开发服务器..."
cd "$PROJECT_ROOT/dashboard"
npx vite --host --port 5173 --config vite.config.e2e.ts > /tmp/dashboard-e2e.log 2>&1 &
FRONTEND_PID=$!
sleep 3

if ! curl -sf http://127.0.0.1:5173/ > /dev/null; then
  echo "前端启动失败"
  cat /tmp/dashboard-e2e.log
  kill $BACKEND_PID 2>/dev/null || true
  exit 1
fi
echo "前端已就绪"

# 4. 运行测试
echo "[4/5] 运行 Playwright E2E 测试..."
npx playwright test e2e/milestone9.spec.ts --project=chromium
TEST_EXIT=$?

# 5. 清理
echo "[5/5] 清理进程..."
kill $FRONTEND_PID 2>/dev/null || true
kill $BACKEND_PID 2>/dev/null || true
wait $FRONTEND_PID 2>/dev/null || true
wait $BACKEND_PID 2>/dev/null || true

echo "=== 测试结束 ==="
exit $TEST_EXIT
