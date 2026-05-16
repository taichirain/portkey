# Milestone 10: 故障场景验证清单

## 概述

本文档定义了 Portkey API Gateway 在各种故障场景下的预期行为和验证步骤。

---

## 验证状态总览

| 验证项 | 状态 | 说明 |
|--------|------|------|
| 集成测试 | ✅ | 17 个自动化测试全部通过 |
| 压测脚本 | ✅ | 可运行，支持 plain/auth/ratelimit 三档 |
| PostgreSQL 故障 | ✅ | 自动化测试覆盖 |
| PostgreSQL 恢复 | ✅ | 新增自动化测试覆盖 |
| Redis 故障 | ✅ | 自动化测试覆盖 |
| Redis 恢复 | ✅ | 新增自动化测试覆盖 |
| 无效快照 | ✅ | 自动化测试覆盖 |
| 核心包单元测试 | ⚠️ | 部分包无独立单元测试（集成测试覆盖核心逻辑） |

---

## 验证场景总览

| 序号 | 场景 | 组件 | 优先级 | 自动化测试 | 验证状态 |
|------|------|------|--------|------------|----------|
| 1 | CP 发布 revision，DP 拉取并切换 | CP + DP | P0 | ✅ | ✅ |
| 2 | 认证、限流在真实请求上生效 | DP | P0 | ✅ | ✅ |
| 3 | PostgreSQL 不可用时 CP 行为 | CP | P0 | ✅ | ✅ |
| 3.1 | PostgreSQL 恢复后服务自动恢复 | CP | P0 | ✅ (新增) | ✅ |
| 4 | Redis 不可用时限流降级行为 | DP | P0 | ✅ | ✅ |
| 4.1 | Redis 恢复后限流自动恢复 | DP | P0 | ✅ (新增) | ✅ |
| 5 | DP 收到坏快照时的保护行为 | DP | P0 | ✅ | ✅ |

---

## 详细验证清单

### 场景 1: CP 发布 revision，DP 拉取并切换

**目的**: 验证配置发布流程的端到端正确性

**前置条件**:
- CP 服务正常运行
- DP 服务正常运行
- CP 和 DP 网络连通

**测试步骤**:

| 步骤 | 操作 | 预期结果 | 验证状态 | 自动化测试 |
|------|------|----------|----------|------------|
| 1.1 | 在 CP 创建新的 Service 和 Route | 配置成功保存到数据库 | ✅ | 集成测试覆盖 |
| 1.2 | 发布新的 revision | CP 返回 200 OK，revision 标记为 active | ✅ | 集成测试覆盖 |
| 1.3 | 等待 DP 轮询周期 | DP 日志显示 "检测到新的 revision" | ✅ | `TestM10_CP_DP_RevisionPublishAndPull` |
| 1.4 | 向 DP 发送请求验证新配置 | 请求按新配置路由 | ✅ | 集成测试覆盖 |
| 1.5 | 并发请求过程中更新配置 | 所有请求成功，无 500 错误 | ✅ | `TestM10_DP_SnapshotSwitch_Atomic` |

**预期行为**:
- DP 定期轮询 CP 的 `/api/v1/public/active-revision` 接口
- 检测到新 revision 后，DP 原子性切换配置
- 配置切换过程中请求处理不中断
- 无效 JSON 被拒绝，保留上一个有效配置

**相关代码**:
- `internal/data/consumer/consumer.go` - DP 拉取快照逻辑
- `internal/control/publisher/publisher.go` - CP 发布逻辑

---

### 场景 2: 认证、限流、日志、指标在真实请求上生效

**目的**: 验证插件链在真实请求流量下的正确性

**前置条件**:
- DP 服务正常运行
- 已配置认证和限流插件

**测试步骤**:

#### 2.1 认证插件 (key-auth)

| 步骤 | 操作 | 预期结果 | 验证状态 | 自动化测试 |
|------|------|----------|----------|------------|
| 2.1.1 | 发送不带 API Key 的请求 | 返回 401 Unauthorized | ✅ | `TestM10_AuthRateLimit_Combined` |
| 2.1.2 | 发送带无效 API Key 的请求 | 返回 401 Unauthorized | ✅ | `TestM10_AuthRateLimit_Combined` |
| 2.1.3 | 发送带有效 API Key 的请求 | 返回 200 OK，请求被转发 | ✅ | `TestM10_AuthRateLimit_Combined` |
| 2.1.4 | 验证请求头中的凭据被隐藏 | 后端服务看不到 API Key | - | 需手动验证 |

#### 2.2 限流插件 (rate-limit)

| 步骤 | 操作 | 预期结果 | 验证状态 | 自动化测试 |
|------|------|----------|----------|------------|
| 2.2.1 | 发送 N 个请求 (N < limit) | 全部返回 200 OK | ✅ | `TestM10_RateLimit_Headers` |
| 2.2.2 | 验证响应头包含限流信息 | `X-RateLimit-Limit`, `X-RateLimit-Remaining`, `X-RateLimit-Reset` | ✅ | `TestM10_RateLimit_Headers` |
| 2.2.3 | 发送超过 limit 的请求 | 第 limit+1 个请求返回 429 Too Many Requests | ✅ | `TestM10_AuthRateLimit_Combined` |
| 2.2.4 | 验证 429 响应包含 Retry-After | 响应头有 `Retry-After` | - | 需手动验证 |

#### 2.3 认证 + 限流组合

| 步骤 | 操作 | 预期结果 | 验证状态 | 自动化测试 |
|------|------|----------|----------|------------|
| 2.3.1 | 不同 consumer 并发发送请求 | 限流按 consumer 独立计数 | ✅ | `TestM10_ConcurrentRequests_WithAuthAndRateLimit` |
| 2.3.2 | 验证请求日志 | 日志包含 consumer_id、route_id 等上下文 | ✅ | `TestM10_RequestLogging` |

**相关代码**:
- `internal/data/plugin/auth/key_auth.go` - 认证插件
- `internal/data/ratelimit/plugin.go` - 限流插件
- `internal/data/plugin/chain.go` - 插件执行链

---

### 场景 3: PostgreSQL 不可用时 CP 行为

**目的**: 验证 CP 在数据库故障时的优雅降级

**前置条件**:
- CP 服务正常运行
- 可控制 PostgreSQL 服务状态

**测试步骤**:

| 步骤 | 操作 | 预期结果 | 验证状态 | 自动化测试 |
|------|------|----------|----------|------------|
| 3.1 | 停止 PostgreSQL 服务 | 数据库连接中断 | ✅ | 模拟测试 |
| 3.2 | 向 CP 发送读请求 (GET /api/v1/services) | 返回 503 Service Unavailable | ✅ | `TestM10_CP_PostgresUnavailable_ReadOperations` |
| 3.3 | 向 CP 发送写请求 (POST /api/v1/services) | 返回 503 Service Unavailable | ✅ | `TestM10_CP_PostgresUnavailable_WriteOperations` |
| 3.4 | 验证错误响应格式 | JSON 响应包含 `error` 和 `message` 字段 | ✅ | `TestM10_CP_PostgresUnavailable_ReadOperations` |
| 3.5 | 恢复 PostgreSQL 服务 | 数据库连接恢复 | ✅ | 模拟测试 |
| 3.6 | 重新发送请求 | 返回 200 OK | ✅ | `TestM10_CP_PostgresRecovered_Operations` (新增) |

**预期行为**:
- CP 返回适当的 HTTP 错误码 (503)
- 错误响应包含有意义的错误信息
- CP 进程不崩溃
- 数据库恢复后服务自动恢复正常

**相关代码**:
- `internal/platform/postgres/postgres.go` - 数据库连接
- `internal/control/api/handler/*` - API 处理器

---

### 场景 4: Redis 不可用时限流降级行为

**目的**: 验证限流插件在 Redis 故障时的优雅降级

**前置条件**:
- DP 服务正常运行
- 已配置 Redis 限流策略
- 可控制 Redis 服务状态

**测试步骤**:

| 步骤 | 操作 | 预期结果 | 验证状态 | 自动化测试 |
|------|------|----------|----------|------------|
| 4.1 | 停止 Redis 服务 | Redis 连接中断 | ✅ | 模拟测试 |
| 4.2 | 发送请求 | 请求仍然被处理 (不返回 500) | ✅ | `TestM10_RateLimit_RedisUnavailable_FallbackToLocal` |
| 4.3 | 验证日志 | 日志包含 "Redis is unavailable, falling back to local limiter" | ✅ | 代码逻辑已验证 |
| 4.4 | 验证限流功能 | 本地限流生效 (可能跨实例不一致) | ✅ | `TestM10_RateLimit_LocalOnly_AlwaysWorks` |
| 4.5 | 恢复 Redis 服务 | Redis 连接恢复 | ✅ | 模拟测试 |
| 4.6 | 发送更多请求 | Redis 限流恢复正常 | ✅ | `TestM10_RateLimit_RedisRecovered_ResumeNormal` (新增) |

**预期行为**:
- Redis 不可用时自动降级到本地限流
- 请求不会因为 Redis 故障而失败
- 日志记录降级事件
- Redis 恢复后自动恢复 Redis 限流（每次请求先尝试 Redis，失败则降级）

**自动恢复机制说明** (`ratelimit/plugin.go:136-176`):
1. 每次请求先调用 `GetLimiter()` 获取限流器
2. 如果配置为 `redis` 策略且 `redisLimiter != nil`，返回 Redis 限流器
3. 如果 Redis 限流器执行失败，自动降级到本地限流器
4. 下次请求仍会先尝试 Redis 限流器，因此 Redis 恢复后自动恢复

**相关代码**:
- `internal/data/ratelimit/plugin.go:136-176` - Redis 降级和恢复逻辑
- `internal/data/ratelimit/redis.go` - Redis 限流器实现
- `internal/data/ratelimit/local.go` - 本地限流器实现

---

### 场景 5: DP 收到坏快照时的保护行为

**目的**: 验证 DP 在接收无效配置时的保护机制

**前置条件**:
- DP 服务正常运行
- 可控制 CP 返回的快照内容

**测试步骤**:

#### 5.1 无效 JSON 快照

| 步骤 | 操作 | 预期结果 | 验证状态 | 自动化测试 |
|------|------|----------|----------|------------|
| 5.1.1 | 配置 CP 返回无效 JSON | CP 返回损坏的快照数据 | ✅ | 模拟测试 |
| 5.1.2 | 触发 DP 拉取快照 | DP 日志显示 "构建配置快照失败" | ✅ | `TestM10_DP_InvalidJSON_Rejected` |
| 5.1.3 | 验证 DP 状态 | DP 保留上一个有效配置 | ✅ | `TestM10_DP_InvalidJSON_Rejected` |
| 5.1.4 | 发送请求 | 请求按旧配置正常处理 | ✅ | 集成测试覆盖 |

#### 5.2 引用不存在资源的快照

| 步骤 | 操作 | 预期结果 | 验证状态 | 自动化测试 |
|------|------|----------|----------|------------|
| 5.2.1 | 配置 CP 返回引用不存在 Service 的 Route | 快照包含无效引用 | ✅ | 模拟测试 |
| 5.2.2 | 触发 DP 拉取快照 | DP 记录警告日志 | ✅ | `TestM10_DP_SnapshotWithMissingReferences_LoggedButNotCrash` |
| 5.2.3 | 发送请求到无效 Route | 返回 404 或按可用配置处理 | ✅ | 集成测试覆盖 |

#### 5.3 空快照

| 步骤 | 操作 | 预期结果 | 验证状态 | 自动化测试 |
|------|------|----------|----------|------------|
| 5.3.1 | 配置 CP 返回空快照 (无 routes/services) | 快照数据为空 | ✅ | 模拟测试 |
| 5.3.2 | 触发 DP 拉取快照 | DP 记录 WARN 日志 "快照中没有任何配置" | ✅ | `TestM10_DP_EmptySnapshot_AcceptedWithWarning` |
| 5.3.3 | 验证 DP 状态 | 快照被接受，revision ID 被更新 | ✅ | `TestM10_DP_EmptySnapshot_AcceptedWithWarning` |
| 5.3.4 | 发送请求 | 返回 404 (无匹配路由) | ✅ | `TestM10_DP_EmptySnapshot_HandledGracefully` |

**重要说明**: 根据代码实际行为 (`consumer.go:432-451`)，`validateSnapshot` 函数只记录警告日志，不返回错误。因此：
- **无效 JSON**：JSON 解析失败 → 返回错误 → 快照被**拒绝**，保留上一个有效配置
- **空快照**：`validateSnapshot` 只记录 WARN → 返回 `nil` → 快照被**接受**
- **引用缺失**：`validateSnapshot` 只记录 WARN → 返回 `nil` → 快照被**接受**

**预期行为**:
- 无效 JSON 被拒绝，保留上一个有效配置
- DP 进程不崩溃
- 空快照被接受但记录警告
- 引用缺失被记录但不导致整体失败

**相关代码**:
- `internal/data/consumer/consumer.go:251-266` - 快照验证逻辑
- `internal/data/snapshot/snapshot.go:136-169` - 快照构建逻辑

---

## 自动化测试位置

### 集成测试

文件: `tests/integration/milestone10_test.go`

包含的测试:
| 测试函数 | 覆盖场景 |
|----------|----------|
| `TestM10_CP_DP_RevisionPublishAndPull` | CP 发布、DP 拉取 |
| `TestM10_DP_SnapshotSwitch_Atomic` | 配置原子切换 |
| `TestM10_AuthRateLimit_Combined` | 认证+限流组合 |
| `TestM10_RequestLogging` | 请求日志 |
| `TestM10_RateLimit_Headers` | 限流响应头 |
| `TestM10_CP_PostgresUnavailable_ReadOperations` | PostgreSQL 故障 (读) |
| `TestM10_CP_PostgresUnavailable_WriteOperations` | PostgreSQL 故障 (写) |
| `TestM10_CP_PostgresRecovered_Operations` | **PostgreSQL 恢复后服务自动恢复 (新增)** |
| `TestM10_RateLimit_RedisUnavailable_FallbackToLocal` | Redis 降级 |
| `TestM10_RateLimit_LocalOnly_AlwaysWorks` | 本地限流 |
| `TestM10_RateLimit_RedisRecovered_ResumeNormal` | **Redis 恢复后限流自动恢复 (新增)** |
| `TestM10_DP_InvalidJSON_Rejected` | 无效 JSON 被拒绝，保留上一个有效配置 |
| `TestM10_DP_EmptySnapshot_AcceptedWithWarning` | 空快照被接受但记录警告 |
| `TestM10_DP_SnapshotWithMissingReferences_LoggedButNotCrash` | 引用缺失被记录但不崩溃 |
| `TestM10_DP_EmptySnapshot_HandledGracefully` | 空快照优雅处理 |
| `TestM10_ConcurrentRequests_WithAuthAndRateLimit` | 并发稳定性 |
| `TestM10_HotUpdate_PluginConfiguration` | 配置热更新 |

### 压测脚本

文件: `tests/load/main.go`

支持三种测试模式:
1. `plain` - 无插件，纯转发
2. `auth` - 带认证插件
3. `ratelimit` - 带认证+限流插件

运行方式:
```bash
# 单模式运行
go run ./tests/load -mode=plain -duration=10s -concurrent=20

# 三档对比测试 (推荐)
bash tests/load/run_load_test.sh
```

---

## 核心包单元测试状态

### 有单元测试的包

| 包 | 测试文件 | 说明 |
|------|----------|------|
| `internal/data/ratelimit` | `ratelimit_test.go`, `plugin_test.go`, `redis_test.go` | ✅ 完整覆盖 |
| `internal/data/plugin/auth` | `key_auth_test.go`, `jwt_auth_test.go` | ✅ 认证插件测试 |
| `internal/domain/credential` | `credential_test.go` | ✅ 凭证测试 |
| `internal/data/snapshot` | `snapshot_test.go` | ✅ 快照测试 |
| `internal/control/api` | `response_format_test.go` | ⚠️ 仅响应格式测试 |

### 无独立单元测试的包

| 包 | 说明 | 覆盖方式 |
|------|------|----------|
| `internal/data/proxy` | HTTP 代理核心，压测对象 | ✅ 集成测试覆盖 (`TestM10_AuthRateLimit_Combined` 等) |
| `internal/control/api/handler` | CP API 处理器 | ⚠️ 无独立测试 (模拟测试覆盖行为) |
| `internal/control/repository` | 数据库操作层 | ⚠️ 无独立测试 |
| `internal/data/consumer` | DP 快照消费 | ✅ 集成测试覆盖 (`TestM10_CP_DP_RevisionPublishAndPull` 等) |

### control/api 覆盖率说明

- 仅 `response_format_test.go` 测试 API 响应格式
- Handler 逻辑通过集成测试中的模拟测试验证行为
- 建议：添加更多 Handler 单元测试

---

## 运行测试命令

### 单元测试
```bash
go test ./internal/...
```

### 集成测试
```bash
go test -tags=integration ./tests/integration/...
```

### 运行 Milestone 10 特定测试
```bash
go test -v ./tests/integration/... -run "TestM10_"
```

### 运行压测
```bash
# 单模式
go run ./tests/load -mode=plain -duration=5s -concurrent=10

# 三档对比
bash tests/load/run_load_test.sh
```

---

## 预期性能差异

压测三档模式应显示明显的性能差异:

| 模式 | QPS 预期 | 延迟预期 | 说明 |
|------|----------|----------|------|
| Plain (无插件) | 最高 | 最低 | 纯代理转发 |
| Auth (认证) | 中等 | 中等 | 需验证 API Key |
| RateLimit (认证+限流) | 最低 | 最高 | 需验证 + 限流计数 |

典型差异比例 (相对):
- Auth QPS ≈ Plain 的 80-90%
- RateLimit QPS ≈ Plain 的 60-80%

---

## 修订历史

| 版本 | 日期 | 修订内容 | 作者 |
|------|------|----------|------|
| 1.0 | 2026-05-04 | 初始版本 | - |
| 1.1 | 2026-05-04 | 修复压测脚本命名问题 (main.go) | - |
| 1.2 | 2026-05-04 | 修复测试数据构造问题 (service_id 匹配) | - |
| 1.3 | 2026-05-04 | 新增故障恢复测试 (PostgreSQL/Redis) | - |
| 1.4 | 2026-05-04 | 添加验证状态列和核心包测试状态说明 | - |
