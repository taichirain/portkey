#!/bin/bash

set -euo pipefail

cd "$(dirname "$0")/../.."

echo "========================================"
echo "Portkey 压测脚本 - 三档对比测试"
echo "========================================"
echo ""

DURATION=${DURATION:-10s}
CONCURRENT=${CONCURRENT:-20}

echo "测试配置:"
echo "  持续时间: $DURATION"
echo "  并发数:   $CONCURRENT"
echo ""

RESULTS_DIR="tests/load/results"
mkdir -p "$RESULTS_DIR"

echo "========================================"
echo "测试 1/3: 无插件模式 (Plain)"
echo "========================================"
go run ./tests/load/main.go \
    -duration="$DURATION" \
    -concurrent="$CONCURRENT" \
    -mode=plain \
    2>&1 | tee "$RESULTS_DIR/plain_result.txt"

echo ""
echo "========================================"
echo "测试 2/3: 认证模式 (Auth)"
echo "========================================"
go run ./tests/load/main.go \
    -duration="$DURATION" \
    -concurrent="$CONCURRENT" \
    -mode=auth \
    2>&1 | tee "$RESULTS_DIR/auth_result.txt"

echo ""
echo "========================================"
echo "测试 3/3: 认证+限流模式 (Auth + RateLimit)"
echo "========================================"
go run ./tests/load/main.go \
    -duration="$DURATION" \
    -concurrent="$CONCURRENT" \
    -mode=ratelimit \
    -ratelimit=10000 \
    2>&1 | tee "$RESULTS_DIR/ratelimit_result.txt"

echo ""
echo "========================================"
echo "测试完成！结果摘要"
echo "========================================"
echo ""
echo "结果文件位置: $(pwd)/tests/load/results/"
echo ""

for f in "$RESULTS_DIR/plain_result.txt" "$RESULTS_DIR/auth_result.txt" "$RESULTS_DIR/ratelimit_result.txt"; do
    if [ -f "$f" ]; then
        mode=$(basename "$f" _result.txt)
        echo "=== $mode ==="
        grep -E "(QPS|平均延迟|总请求数|成功请求)" "$f" || true
        echo ""
    fi
done
