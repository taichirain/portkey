# Portkey 监控页设计文档

> 版本：v0.1 | 日期：2026-05-06 | 状态：Draft

---

## 1. 现状分析

### 1.1 当前 Dashboard 不是监控页

当前 `Dashboard.tsx` 承担的是**管理概览**职责，展示内容为：

- 配置对象计数（service / route / upstream / target / consumer / plugin）
- 最近路由与服务列表
- 当前 active revision 信息
- 快捷操作入口

它不包含任何运行时指标、健康状态或 DP 实例信息。

### 1.2 后端已有的监控相关数据

DP 侧（`proxy.go`）：

| 数据 | 位置 | 现状 |
|------|------|------|
| 请求数 | `Metrics.requestsTotal` | 累计计数，自进程启动 |
| 活跃连接 | `Metrics.requestsActive` | 当前值 |
| 延迟累计 | `Metrics.responseLatency` | 累计微秒，无分位数 |
| 错误数 | `Metrics.errorsTotal` | 累计计数 |
| `/metrics` 端点 | `data/app.go:185` | 返回上述 4 个字段 |
| `/revision` 端点 | `data/app.go:197` | 返回当前 revision ID + 配置统计 |
| `/health` `/ready` | `data/app.go:173,179` | 简单状态字符串 |

DP 侧健康检查（`health/types.go`）：

| 数据 | 现状 |
|------|------|
| per-target 健康状态 | `HealthManager` 内存维护，无 HTTP 暴露 |
| 熔断器状态 | `CircuitBreakerState`（closed/open/half-open） |
| 连续错误/成功数 | `TargetHealth` 内部计数 |
| 主动/被动健康检查配置 | `UpstreamHealthConfig` |

DP 侧限流（`ratelimit/`）：

- 限流命中发生在插件层面，没有独立计数器暴露给外部。

DP 侧流量策略（M17 新增）：

- `PolicyMatchDetails` 结构化日志，仅写入 access log，无 HTTP API 暴露。

### 1.3 关键缺陷

1. **没有时间窗口**：只有累计计数，无法回答"最近 5 分钟 QPS 是多少"
2. **没有分位数**：延迟只有累加值，无法看 p50 / p95 / p99
3. **没有 per-route / per-service 维度**：所有请求混在一起
4. **CP 不感知 DP 实例**：CP 不知道有多少 DP 在运行，无法代理 DP 指标
5. **CP 没有监控 API**：前端无法从 CP 获取任何运行时监控数据
6. **健康检查数据未暴露**：`HealthManager` 全在内存，外部无法查看

---

## 2. 设计目标

1. 不引入外部依赖（Prometheus / Grafana / OpenTelemetry），纯内置实现
2. DP 自己维护滑动窗口指标，CP 代理聚合
3. 前端独立监控页 `/monitoring`，覆盖流量、健康、DP 状态三个维度
4. 与现有 access log、M17 的 PolicyMatchDetails 形成互补，不重复

---

## 3. 整体架构

```
                   ┌─────────────────────────────────┐
                   │           Dashboard              │
                   │  GET /monitoring                 │
                   └────────────┬────────────────────┘
                                │
                   ┌────────────▼────────────────────┐
                   │      Control Plane (CP)          │
                   │                                  │
                   │  GET /api/v1/monitoring/metrics   │
                   │  GET /api/v1/monitoring/health    │
                   │  GET /api/v1/monitoring/dp-status │
                   │                                  │
                   │  dp_instances: [...]              │
                   └──┬──────────┬──────────┬────────┘
                      │          │          │
              ┌───────▼──┐ ┌────▼──────┐ ┌─▼────────┐
              │  DP-1    │ │  DP-2    │ │  DP-3    │
              │ /metrics │ │ /metrics │ │ /metrics │
              │ /health  │ │ /health  │ │ /health  │
              │  detail  │ │  detail  │ │  detail  │
              └──────────┘ └──────────┘ └──────────┘
```

CP 通过 HTTP 轮询各 DP 实例，聚合后返回给前端。

---

## 4. DP 侧变更

### 4.1 Metrics 滑动窗口

扩展 `internal/data/proxy/proxy.go` 中的 `Metrics` 结构体：

```go
type Metrics struct {
    // ---- 原有累计计数（保留，用于 /metrics 兼容） ----
    requestsTotal   int64
    requestsActive  int64
    responseLatency int64
    errorsTotal     int64

    // ---- 新增：滑动窗口 ----
    // 12 个桶，每个桶覆盖 5 秒，总计 60 秒窗口
    requestBuckets  [12]int64
    errorBuckets    [12]int64
    latencyBuckets  [12]int64  // 微秒累计
    bucketTimestamps [12]int64 // 每桶对应的 unix 秒
    currentBucket   int32

    // ---- 新增：状态码分布 ----
    status2xx int64
    status3xx int64
    status4xx int64
    status5xx int64

    // ---- 新增：功能计数器 ----
    rateLimitedTotal int64
    policyHitTotal   int64
    startedAt        time.Time
}
```

**桶轮转逻辑**（每次请求完成时调用）：

```go
func (m *Metrics) recordRequest(statusCode int, latency time.Duration, rateLimited, policyHit bool) {
    now := time.Now().Unix()
    bucket := int32(now/5) % 12

    // 如果跨越了桶，清理过期桶
    if atomic.LoadInt32(&m.currentBucket) != bucket {
        atomic.StoreInt32(&m.currentBucket, bucket)
        atomic.StoreInt64(&m.requestBuckets[bucket], 0)
        atomic.StoreInt64(&m.errorBuckets[bucket], 0)
        atomic.StoreInt64(&m.latencyBuckets[bucket], 0)
        atomic.StoreInt64(&m.bucketTimestamps[bucket], now)
    }

    atomic.AddInt64(&m.requestBuckets[bucket], 1)
    atomic.AddInt64(&m.latencyBuckets[bucket], latency.Microseconds())

    if statusCode >= 500 {
        atomic.AddInt64(&m.errorBuckets[bucket], 1)
    }

    // 状态码分布
    switch {
    case statusCode >= 200 && statusCode < 300:
        atomic.AddInt64(&m.status2xx, 1)
    case statusCode >= 300 && statusCode < 400:
        atomic.AddInt64(&m.status3xx, 1)
    case statusCode >= 400 && statusCode < 500:
        atomic.AddInt64(&m.status4xx, 1)
    default:
        atomic.AddInt64(&m.status5xx, 1)
    }

    if rateLimited {
        atomic.AddInt64(&m.rateLimitedTotal, 1)
    }
    if policyHit {
        atomic.AddInt64(&m.policyHitTotal, 1)
    }
}
```

**QPS 计算**：

```go
func (m *Metrics) calculateQPS() float64 {
    var total int64
    now := time.Now().Unix()
    for i := 0; i < 12; i++ {
        ts := atomic.LoadInt64(&m.bucketTimestamps[i])
        if now-ts <= 60 {
            total += atomic.LoadInt64(&m.requestBuckets[i])
        }
    }
    return float64(total) / 60.0
}
```

### 4.2 增强 `/metrics` 端点

修改 `internal/app/data/app.go` 中的 `/metrics` handler：

```
GET /metrics

Response:
{
    // 原有字段（保持兼容）
    "requests_total": 12345,
    "requests_active": 3,
    "response_latency_us": 285000000,
    "errors_total": 67,

    // 新增字段
    "qps_1m": 45.2,
    "error_rate_1m": 0.012,
    "avg_latency_ms_1m": 23.5,
    "p99_latency_ms_1m": 120.0,
    "status_distribution": {
        "2xx": 12000,
        "3xx": 10,
        "4xx": 250,
        "5xx": 67
    },
    "rate_limited_total": 12,
    "policy_hit_total": 89,
    "uptime_seconds": 3600,
    "current_revision_id": "abc-123",
    "current_revision_version": "v1.2.0"
}
```

### 4.3 新增 `/health/detail` 端点

新增 DP 端点，暴露 `HealthManager` 中的完整健康数据：

```
GET /health/detail

Response:
{
    "status": "healthy",
    "dp_revision_id": "abc-123",
    "upstreams": [
        {
            "id": "uuid-1",
            "name": "user-service",
            "targets": [
                {
                    "target": "10.0.0.1",
                    "port": 8080,
                    "status": "healthy",
                    "circuit_state": "closed",
                    "consecutive_errors": 0,
                    "consecutive_successes": 15,
                    "successes": 1500,
                    "failures": 3,
                    "timeouts": 1,
                    "last_checked_at": "2026-05-06T01:00:00Z",
                    "last_success_at": "2026-05-06T01:00:00Z",
                    "last_failure_at": "2026-05-06T00:58:30Z"
                },
                {
                    "target": "10.0.0.2",
                    "port": 8080,
                    "status": "unhealthy",
                    "circuit_state": "open",
                    "consecutive_errors": 5,
                    "consecutive_successes": 0,
                    "successes": 1200,
                    "failures": 25,
                    "timeouts": 8,
                    "last_checked_at": "2026-05-06T00:59:55Z",
                    "last_success_at": "2026-05-06T00:55:00Z",
                    "last_failure_at": "2026-05-06T00:59:55Z"
                }
            ]
        }
    ]
}
```

实现位置：在 `internal/data/health/` 包中新增 `HealthDetailHandler`，复用
`HealthManager.GetAllTargetHealth()` 和 `snapshot.Services` / `snapshot.Upstreams`
中的名称映射。

---

## 5. CP 侧变更

### 5.1 DP 实例配置

在 `configs/config.yaml` 中增加 `dp_instances` 段：

```yaml
dp_instances:
  - name: "dp-primary"
    url: "http://127.0.0.1:8001"
  - name: "dp-secondary"
    url: "http://127.0.0.1:8002"
```

加载到 `internal/config/config.go` 中：

```go
type DPInstance struct {
    Name string `yaml:"name"`
    URL  string `yaml:"url"`
}

type Config struct {
    // ... 原有字段
    DPInstances []DPInstance `yaml:"dp_instances"`
}
```

这是最小实现方式。后续可替换为 DP 注册 + 心跳续约机制。

### 5.2 新增 `/api/v1/monitoring/*` 端点

在 `internal/app/control/app.go` 中注册：

```go
protected.HandleFunc("/api/v1/monitoring/metrics", handleMonitoringMetrics)
protected.HandleFunc("/api/v1/monitoring/health", handleMonitoringHealth)
protected.HandleFunc("/api/v1/monitoring/dp-status", handleMonitoringDPStatus)
```

#### `GET /api/v1/monitoring/metrics`

CP 轮询所有 DP 的 `/metrics`，聚合返回：

```json
{
    "aggregated": {
        "qps_1m": 135.6,
        "error_rate_1m": 0.008,
        "avg_latency_ms_1m": 25.3,
        "requests_active": 9,
        "requests_total": 37000,
        "errors_total": 201,
        "rate_limited_total": 36,
        "policy_hit_total": 267,
        "status_distribution": {
            "2xx": 36000,
            "3xx": 30,
            "4xx": 750,
            "5xx": 201
        }
    },
    "per_dp": [
        {
            "name": "dp-primary",
            "url": "http://127.0.0.1:8001",
            "online": true,
            "qps_1m": 67.8,
            "error_rate_1m": 0.005,
            "avg_latency_ms_1m": 22.1,
            "requests_active": 4,
            "current_revision_id": "abc-123",
            "uptime_seconds": 7200
        },
        {
            "name": "dp-secondary",
            "url": "http://127.0.0.1:8002",
            "online": true,
            "qps_1m": 67.8,
            "error_rate_1m": 0.011,
            "avg_latency_ms_1m": 28.5,
            "requests_active": 5,
            "current_revision_id": "abc-123",
            "uptime_seconds": 3600
        }
    ],
    "active_revision": {
        "id": "abc-123",
        "version": "v1.2.0",
        "published_at": "2026-05-06T00:30:00Z"
    }
}
```

**聚合规则**：

- `qps_1m`：各 DP 求和
- `error_rate_1m`：`(各 DP 错误数之和) / (各 DP 请求数之和)`
- `avg_latency_ms_1m`：各 DP 加权平均（按请求数加权）
- `requests_active`：各 DP 求和
- `status_distribution`：各 DP 求和
- `online`：对 DP 的 `/health` 做 HTTP 请求，超时 2 秒视为离线

**DP 版本不一致检测**：

如果某个 DP 的 `current_revision_id` 与 CP 的 `active_revision.id` 不同，
在该 DP 条目中增加 `"revision_mismatch": true` 字段，前端用橙色高亮。

#### `GET /api/v1/monitoring/health`

转发各 DP 的 `/health/detail`，聚合返回：

```json
{
    "dp_instances": [
        {
            "name": "dp-primary",
            "online": true,
            "upstreams": [ ... ]
        },
        {
            "name": "dp-secondary",
            "online": true,
            "upstreams": [ ... ]
        }
    ]
}
```

#### `GET /api/v1/monitoring/dp-status`

轻量端点，仅返回 DP 实例列表与版本状态：

```json
{
    "cp_active_revision_id": "abc-123",
    "cp_active_revision_version": "v1.2.0",
    "dp_instances": [
        {
            "name": "dp-primary",
            "url": "http://127.0.0.1:8001",
            "online": true,
            "revision_id": "abc-123",
            "revision_mismatch": false,
            "uptime_seconds": 7200,
            "config_stats": {
                "services_count": 5,
                "routes_count": 12,
                "upstreams_count": 3,
                "traffic_policies_count": 8
            }
        }
    ]
}
```

### 5.3 前端 API 层

在 `dashboard/src/services/api.ts` 中新增：

```typescript
// ---- Monitoring ----

async getMonitoringMetrics(): Promise<MonitoringMetricsResponse> {
    const response = await this.client.get<MonitoringMetricsResponse>('/monitoring/metrics');
    return response.data as MonitoringMetricsResponse;
}

async getMonitoringHealth(): Promise<MonitoringHealthResponse> {
    const response = await this.client.get<MonitoringHealthResponse>('/monitoring/health');
    return response.data as MonitoringHealthResponse;
}

async getDPStatus(): Promise<DPStatusResponse> {
    const response = await this.client.get<DPStatusResponse>('/monitoring/dp-status');
    return response.data as DPStatusResponse;
}
```

---

## 6. 前端监控页设计

### 6.1 路由注册

`dashboard/src/App.tsx` 中增加：

```tsx
<Route path="monitoring" element={<MonitoringPage />} />
```

`dashboard/src/layouts/MainLayout.tsx` 菜单中增加"监控"入口。

### 6.2 页面布局

```
┌─────────────────────────────────────────────────────────────┐
│  监控概览                                      [刷新] [自动]│
├──────────┬──────────┬──────────┬──────────┬────────────────┤
│  当前QPS │  错误率   │  平均延迟 │  活跃连接 │  DP 在线/总数  │
│  135.6   │  0.8%    │  25ms    │    9     │    2 / 2      │
├──────────┴──────────┴──────────┴──────────┴────────────────┤
│                                                             │
│  DP 实例状态                                                │
│  ┌─────────────┬────────┬────────┬───────┬────────┬──────┐ │
│  │ 实例名       │ 状态   │ QPS    │ 延迟  │ 版本   │ 运行  │ │
│  ├─────────────┼────────┼────────┼───────┼────────┼──────┤ │
│  │ dp-primary  │ 🟢在线 │ 67.8   │ 22ms  │ v1.2.0 │ 2h   │ │
│  │ dp-secondary│ 🟢在线 │ 67.8   │ 28ms  │ v1.2.0 │ 1h   │ │
│  └─────────────┴────────┴────────┴───────┴────────┴──────┘ │
│                                                             │
├────────────────────────────┬────────────────────────────────┤
│  状态码分布                 │  功能计数器                    │
│                            │                                │
│  2xx  █████████████ 97.3%  │  限流命中:     36              │
│  3xx  ▏             0.1%   │  流量策略命中: 267             │
│  4xx  ██            2.0%   │                                │
│  5xx  ▏             0.5%   │  当前活跃:     9              │
│                            │  累计请求:     37,000          │
├────────────────────────────┴────────────────────────────────┤
│                                                             │
│  健康检查详情                                               │
│  ┌────────────────────────────────────────────────────────┐ │
│  │ user-service (upstream)                                │ │
│  ├─────────────┬──────────┬──────────┬─────────┬─────────┤ │
│  │ Target      │ 健康状态 │ 熔断状态 │ 连续错误 │ 成功率  │ │
│  ├─────────────┼──────────┼──────────┼─────────┼─────────┤ │
│  │ 10.0.0.1    │ ✅健康   │ closed   │ 0       │ 99.8%   │ │
│  │ :8080       │          │          │         │         │ │
│  ├─────────────┼──────────┼──────────┼─────────┼─────────┤ │
│  │ 10.0.0.2    │ ❌不健康 │ 🔶open   │ 5       │ 0.0%    │ │
│  │ :8080       │          │          │         │         │ │
│  └─────────────┴──────────┴──────────┴─────────┴─────────┘ │
│                                                             │
│  ┌────────────────────────────────────────────────────────┐ │
│  │ order-service (upstream)                               │ │
│  │ ...                                                    │ │
│  └────────────────────────────────────────────────────────┘ │
│                                                             │
└─────────────────────────────────────────────────────────────┘
```

### 6.3 交互细节

**自动刷新**：
- 默认关闭，点击"自动"按钮开启
- 开启后每 10 秒轮询 `/api/v1/monitoring/metrics`
- 健康检查详情每 30 秒刷新一次（变化频率低，不需要高频）
- 用户离开页面时停止轮询

**DP 版本不一致告警**：
- DP 行的"版本"列显示为橙色 `<Tag color="warning">v1.1.0</Tag>`
- 同行增加 Tooltip：「该 DP 实例的配置版本落后于 CP 活跃版本」

**DP 离线处理**：
- DP 行的"状态"列显示为红色 `<Tag color="error">离线</Tag>`
- QPS / 延迟列显示为 `-`
- 聚合指标中排除离线 DP

**空状态**：
- 未配置 `dp_instances` 时，显示提示：「未配置 DP 实例，请在 config.yaml 中添加 dp_instances」

### 6.4 类型定义

在 `dashboard/src/types/index.ts` 中新增：

```typescript
// ---- Monitoring Types ----

export interface DPMetrics {
    name: string;
    url: string;
    online: boolean;
    qps_1m: number;
    error_rate_1m: number;
    avg_latency_ms_1m: number;
    requests_active: number;
    requests_total: number;
    errors_total: number;
    rate_limited_total: number;
    policy_hit_total: number;
    current_revision_id: string;
    revision_mismatch?: boolean;
    uptime_seconds: number;
    status_distribution: {
        '2xx': number;
        '3xx': number;
        '4xx': number;
        '5xx': number;
    };
}

export interface MonitoringMetricsResponse {
    aggregated: Omit<DPMetrics, 'name' | 'url' | 'online' | 'current_revision_id'
        | 'revision_mismatch' | 'uptime_seconds'>;
    per_dp: DPMetrics[];
    active_revision: {
        id: string;
        version: string;
        published_at: string;
    };
}

export interface TargetHealthDetail {
    target: string;
    port: number;
    status: 'healthy' | 'unhealthy' | 'degraded' | 'unknown';
    circuit_state: 'closed' | 'open' | 'half-open';
    consecutive_errors: number;
    consecutive_successes: number;
    successes: number;
    failures: number;
    timeouts: number;
    last_checked_at: string;
    last_success_at: string;
    last_failure_at: string;
}

export interface UpstreamHealthDetail {
    id: string;
    name: string;
    targets: TargetHealthDetail[];
}

export interface DPHealthDetail {
    name: string;
    online: boolean;
    upstreams: UpstreamHealthDetail[];
}

export interface MonitoringHealthResponse {
    dp_instances: DPHealthDetail[];
}

export interface DPStatusInstance {
    name: string;
    url: string;
    online: boolean;
    revision_id: string;
    revision_mismatch: boolean;
    uptime_seconds: number;
    config_stats: {
        services_count: number;
        routes_count: number;
        upstreams_count: number;
        traffic_policies_count: number;
    };
}

export interface DPStatusResponse {
    cp_active_revision_id: string;
    cp_active_revision_version: string;
    dp_instances: DPStatusInstance[];
}
```

### 6.5 权限

监控页使用新权限 `monitoring:read`，默认分配给所有角色（含 viewer）。
在 RBAC 中不需要新增 `monitoring:write`，因为监控页是只读的。

---

## 7. 改动文件清单

| 文件 | 变更类型 | 说明 |
|------|----------|------|
| `internal/data/proxy/proxy.go` | 修改 | Metrics 结构体扩展 + 滑动窗口 + recordRequest |
| `internal/app/data/app.go` | 修改 | `/metrics` 返回增强 + 新增 `/health/detail` |
| `internal/data/health/` | 新增文件 | `HealthDetailHandler`，暴露完整健康数据 |
| `internal/config/config.go` | 修改 | 新增 `DPInstance` / `dp_instances` 配置项 |
| `internal/app/control/app.go` | 修改 | 注册 `/api/v1/monitoring/*` 端点 |
| `internal/control/api/handler/` | 新增文件 | `monitoring.go`，CP 代理聚合逻辑 |
| `dashboard/src/pages/Monitoring.tsx` | 新增 | 监控页组件 |
| `dashboard/src/services/api.ts` | 修改 | 新增 3 个监控 API 方法 |
| `dashboard/src/types/index.ts` | 修改 | 新增监控相关类型定义 |
| `dashboard/src/App.tsx` | 修改 | 新增 `/monitoring` 路由 |
| `dashboard/src/layouts/MainLayout.tsx` | 修改 | 菜单新增"监控"入口 |

---

## 8. 测试策略

### 8.1 DP 侧

- **单元测试**：`proxy_test.go` 中验证滑动窗口桶轮转、QPS 计算、状态码分布
- **集成测试**：启动 DP，发送混合请求（200/404/500），验证 `/metrics` 返回值

### 8.2 CP 侧

- **集成测试**：启动 CP + DP（单实例），验证 `/api/v1/monitoring/metrics` 聚合正确
- **集成测试**：模拟 DP 离线（停止 DP 进程），验证 `online: false` 和聚合排除
- **集成测试**：模拟 DP 版本不一致（DP 拉取旧 revision），验证 `revision_mismatch: true`

### 8.3 前端

- **E2E 测试**（Playwright）：
  - 打开 `/monitoring`，验证 KPI 卡片渲染
  - 验证 DP 实例表格显示正确数据
  - 验证健康检查详情表格按 upstream 分组

---

## 9. 实现优先级

| 优先级 | 内容 | 估时 | 价值 |
|--------|------|------|------|
| P0 | DP Metrics 滑动窗口（proxy.go） | 2h | 高 |
| P0 | DP `/metrics` 增强返回 | 1h | 高 |
| P0 | CP `dp_instances` 配置 + 加载 | 1h | 高 |
| P0 | CP `/api/v1/monitoring/metrics` 代理聚合 | 2h | 高 |
| P0 | `Monitoring.tsx` 页面骨架（KPI + DP 表） | 3h | 高 |
| P1 | DP `/health/detail` 端点 | 2h | 中 |
| P1 | CP `/api/v1/monitoring/health` 代理 | 1h | 中 |
| P1 | 健康检查详情表格 | 2h | 中 |
| P1 | 自动刷新 + 离线/版本不一致处理 | 1h | 中 |
| P2 | 权限 `monitoring:read` | 0.5h | 低 |
| P2 | 状态码分布可视化（Ant Design Progress） | 1h | 低 |
| P2 | E2E 测试 | 2h | 低 |
| P3 | DP 注册 + 心跳续约机制（替换配置式） | 4h | 未来 |
| P3 | per-route / per-service 维度统计 | 4h | 未来 |
| P3 | Prometheus exporter（标准 /metrics 格式） | 4h | 未来 |
| P3 | OpenTelemetry 集成 | 8h | 未来 |

P0 + P1 合计约 15h，可交付最小可用监控页。
P2 合计约 3.5h，补齐体验。
P3 是后续版本的事。

---

## 10. 后续演进方向

### 10.1 Prometheus 兼容（v1.0+）

当监控数据需要对接 Grafana 等外部系统时，在 DP 上新增
`GET /metrics/prometheus` 端点，输出标准 Prometheus 文本格式：

```
# HELP portkey_requests_total Total number of requests
# TYPE portkey_requests_total counter
portkey_requests_total 12345

# HELP portkey_request_duration_seconds Request duration in seconds
# TYPE portkey_request_duration_seconds histogram
portkey_request_duration_seconds_bucket{le="0.01"} 8000
portkey_request_duration_seconds_bucket{le="0.05"} 11000
...
```

使用 `prometheus/client_golang` 库，与现有 atomic 计数器共存。

### 10.2 OpenTelemetry（v1.0+）

- 在 proxy.go 的请求链路中注入 OTel Span
- trace id 沿用现有的 `X-Trace-Id`，映射为 OTel TraceID
- 通过 OTLP exporter 推送到 Jaeger / Tempo

### 10.3 实时日志流（v1.1+）

- DP 新增 WebSocket 端点 `/ws/access-log`
- CP 代理转发到前端
- 前端展示最近 100 条访问日志的实时滚动列表
- 支持按 route / status / consumer 过滤

### 10.4 DP 注册机制（v1.0+）

替换配置式的 `dp_instances`：

1. DP 启动时 `POST /api/v1/public/dp-register`，上报 IP / 端口 / hostname
2. DP 定期 `POST /api/v1/public/dp-heartbeat`（间隔 10 秒）
3. CP 维护在线 DP 列表，超过 30 秒无心跳标记离线
4. CP 重启后 DP 自动重新注册
