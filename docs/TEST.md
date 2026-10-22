# 测试与验收

CI 使用 PostgreSQL 16、Redis 7、Go 1.26、Node 20 和 Chromium。数据库测试与浏览器 E2E 使用两个独立数据库，避免前者留下的配置影响发布验证。完整门禁见 `.github/workflows/ci.yml`。

## Go

```bash
go build ./...
go vet ./...
go test ./... -count=1
```

上面的默认测试包含不依赖 PostgreSQL 的代理、插件、限流、快照及故障场景测试；Redis 可用时还会执行多 DP 共享限流测试。

数据库集成测试需要先把 `migrations/postgres/0001` 到 `0005` 按顺序应用到空的 `portkey_test` 数据库，再执行：

```bash
TEST_DB_HOST=127.0.0.1 TEST_DB_PORT=5432 TEST_DB_USER=postgres \
TEST_DB_PASSWORD=portkeytest TEST_DB_NAME=portkey_test \
go test -tags=integration ./tests/integration -count=1
```

## Dashboard E2E

在另一个空数据库 `portkey_e2e` 中应用相同迁移，并用它配置 `PORTKEY_E2E_CONFIG`。安装前端依赖和 Chromium 后运行：

```bash
cd dashboard && npm ci && npx playwright install chromium && cd ..
PORTKEY_E2E_CONFIG=configs/config.ci.yaml bash dashboard/e2e/run-tests.sh \
  e2e/milestone9.spec.ts e2e/milestone12.spec.ts \
  e2e/milestone13.spec.ts e2e/milestone16.spec.ts
```

脚本会启动并清理本机测试后端与 Vite 服务。M16 用例覆盖从 Dashboard 创建灰度策略、发布 revision、观察 DP 切换、回滚并观察 DP 恢复。

## 三档压测

```bash
DURATION=10s CONCURRENT=20 bash tests/load/run_load_test.sh
```

脚本分别运行无插件、认证、认证加本地限流三档，并将结果写入 `tests/load/results/`。这些结果用于比较本机三档相对开销，不能直接当成生产容量指标。

2026-10-22 本机样例基线（每档 5 秒、并发 20、限流阈值 10000/分钟，关闭测试 logger）：

| 模式 | QPS | 平均延迟 | 成功请求 |
|---|---:|---:|---:|
| 无插件 | 2531 | 7.69 ms | 12669 |
| Key Auth | 2274 | 8.52 ms | 11385 |
| Key Auth + 本地限流 | 1944 | 9.97 ms | 9732 |

正式容量基线仍需在固定硬件和部署配置下重复测量，并记录波动范围。
