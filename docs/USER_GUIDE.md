# Portkey API Gateway — 用户指南

> 版本：v0.2 | 面向：使用者和本地开发者

---

## 目录

1. [产品概述](#1-产品概述)
2. [快速开始](#2-快速开始)
3. [配置文件说明](#3-配置文件说明)
4. [运行模式](#4-运行模式)
5. [配置工作流](#5-配置工作流)
6. [Admin API 使用指南](#6-admin-api-使用指南)
7. [Dashboard 使用指南](#7-dashboard-使用指南)
8. [插件系统](#8-插件系统)
9. [监控与可观测性](#9-监控与可观测性)
10. [故障场景](#10-故障场景)
11. [开发与测试](#11-开发与测试)
12. [常见问题](#12-常见问题)

---

## 1. 产品概述

### 1.1 什么是 Portkey

Portkey 是一个 **Kong-like 的 API Gateway**，面向个人项目和小团队场景。它把网关拆成两个逻辑平面：

- **Control Plane（控制面）**：负责配置管理、鉴权、审计和配置下发。不处理业务流量。
- **Data Plane（数据面）**：负责代理实际请求。不直接修改配置，只消费只读快照。

二者可以在同一台机器上运行（`all` 模式），也可以分开独立部署。

### 1.2 核心功能

| 功能 | 说明 |
|------|------|
| HTTP 反向代理 | 将客户端请求转发至上游服务 |
| 路由匹配 | 按 path / method / host / header 匹配路由 |
| 负载均衡 | 支持 Round Robin，多 target 轮询分发 |
| 认证鉴权 | JWT Auth、Key Auth |
| 限流 | 本地限流 + Redis 分布式限流 |
| 插件扩展 | 内置 request / response / error 三段插件 |
| 配置版本化 | 每次发布生成唯一 revision，支持回滚 |
| 可观测性 | 访问日志、Prometheus 指标、OpenTelemetry trace |
| 管理面 | Admin API + Dashboard SPA |

### 1.3 架构拓扑

```
                    +----------------------+
                    |      Dashboard       |
                    |   React SPA (:3000)  |
                    +----------+-----------+
                               |
                               v
                    +----------------------+
                    |    Control Plane     |
                    |   Admin API (:8001)  |
                    | - Auth / RBAC        |
                    | - Config Validate    |
                    | - Snapshot Builder   |
                    | - DP Config Watch    |
                    +----+------------+----+
                         |            |
                         v            v
                 +-----------+    +--------+
                 |PostgreSQL |    | Redis  |
                 | config DB |    | shared |
                 +-----------+    | state  |
                                  +--------+

Client -> +----------------------+
          |      Data Plane      |
          |   Proxy API (:8080)  |
          | - Route Match        |
          | - Plugin Chain       |
          | - LB / Retry         |
          | - Health / Metrics   |
          +----------+-----------+
                     |
                Upstream Services
```

---

## 2. 快速开始

### 2.1 前置依赖

- Go 1.26+
- PostgreSQL 15+
- Redis 6+（分布式限流可选）
- Node.js 18+（Dashboard 可选）

### 2.2 编译

```bash
cd portkey

# 编译二进制
go build -o bin/portkey ./cmd/portkey

# 确认编译成功
./bin/portkey --help
```

### 2.3 启动最小环境

```bash
# 1. 启动 PostgreSQL（Docker 一键）
docker run -d --name portkey-pg \
  -e POSTGRES_USER=postgres \
  -e POSTGRES_PASSWORD=postgres \
  -e POSTGRES_DB=portkey \
  -p 5432:5432 \
  postgres:16

# 2. 初始化数据库
psql -h 127.0.0.1 -U postgres -d portkey -f migrations/postgres/0001_init.sql
psql -h 127.0.0.1 -U postgres -d portkey -f migrations/postgres/0002_add_upstream_to_services.sql
# 根据需要执行后续迁移

# 3. 准备配置文件
cp configs/config.example.yaml configs/config.yaml

# 4. 一键启动（all 模式，同时启动 CP + DP）
./bin/portkey --mode all --config configs/config.yaml
```

启动成功后，你应该能看到三个端口：

| 组件 | 端口 | 作用 |
|------|------|------|
| Control Plane | `127.0.0.1:8001` | Admin API |
| Data Plane | `127.0.0.1:8080` | 代理入口 |
| Dashboard | `127.0.0.1:3000` | 管理界面（需额外启动） |

### 2.4 启动 Dashboard

```bash
cd dashboard
npm install
npm run dev
# Dashboard 访问 http://localhost:3000
```

### 2.5 快速验证

```bash
# 检查 CP 健康状态
curl http://127.0.0.1:8001/health

# 检查 DP 健康状态
curl http://127.0.0.1:8080/health

# 检查 DP readiness
curl http://127.0.0.1:8080/ready
```

---

## 3. 配置文件说明

默认配置文件 `configs/config.yaml` 结构如下：

```yaml
control:
  host: 127.0.0.1          # CP 监听地址，生产环境建议内网 IP
  port: 8001                # CP 端口
  db:                       # PostgreSQL 配置
    host: 127.0.0.1
    port: 5432
    user: postgres
    password: ""            # 生产环境建议用环境变量注入
    database: portkey
    sslmode: disable        # 生产环境建议 enable

data:
  host: 127.0.0.1           # DP 监听地址
  port: 8080                # DP 代理端口
  control_url: http://127.0.0.1:8001  # CP 地址，DP 从这里拉取配置
  redis:                    # Redis 配置（分布式限流需要）
    host: 127.0.0.1
    port: 6379
    password: ""
    db: 0

logging:
  level: info               # 日志级别：debug / info / warn / error
  development: true         # 开发模式输出友好格式，生产建议 false

dp_instances:               # 监控页需要的 DP 实例列表
  - name: "dp-primary"
    url: "http://127.0.0.1:8001"
```

### 配置建议

- **开发环境**：`development: true`，日志可读性强
- **生产环境**：`host` 改用内网 IP，`sslmode: enable`，密码通过环境变量注入
- **多 DP 场景**：在 `dp_instances` 中列出所有 DP 实例，CP 会轮询聚合数据

---

## 4. 运行模式

同一个二进制支持三种模式：

### 4.1 模式对比

| 模式 | 命令 | 启动内容 | 适用场景 |
|------|------|---------|---------|
| `control` | `portkey control` | 只启动控制面 | 生产分离部署 |
| `data` | `portkey data` | 只启动数据面 | 生产分离部署 |
| `all` | `portkey all` | 同时启动 CP + DP | 本地开发、单机演示 |

### 4.2 生产部署推荐拓扑

```
# 机器 A — 控制面 + 数据库
portkey --mode control --config configs/config.yaml

# 机器 B / C — 数据面实例
portkey --mode data --config configs/data-config.yaml
```

### 4.3 优雅关闭

Portkey 在收到 `SIGINT` / `SIGTERM` 信号时执行优雅关闭，会等待进行中的请求完成后再退出。你可以用 Ctrl+C 或 `kill` 命令正常停止。

---

## 5. 配置工作流

Portkey 的配置变更统一走版本化发布流程，不零散下发。

### 5.1 完整工作流

```
编辑配置 -> 校验合法性 -> 生成快照 -> 创建 revision -> 发布 -> DP 拉取生效
```

### 5.2 核心概念

| 实体 | 说明 | 例子 |
|------|------|------|
| **Service** | 上游服务抽象，描述访问策略 | `user-service` |
| **Route** | 请求匹配规则，绑定一个 Service | `GET /api/users/*` |
| **Upstream** | 负载均衡组，管理一组 target | `user-upstream` |
| **Target** | 上游实际节点（IP:Port） | `10.0.0.1:8080` |
| **Consumer** | 调用方身份 | `mobile-app` |
| **Credential** | Consumer 的认证凭证 | JWT Secret / API Key |
| **Plugin** | 扩展能力 | `jwt-auth`, `rate-limiting` |
| **Revision** | 一次发布的完整配置快照 | `rev-abc123` |

### 5.3 Service 与 Upstream 的关系

```
Route → Service → Upstream → [Target A, Target B]
```

- Route 必须绑定一个 Service
- Service 不直接存 host:port，必须绑定一个 Upstream
- Upstream 负责负载均衡算法和 target 管理
- 这种设计避免 Kong 早期 "service 既能直连又能关联 upstream" 的双模型问题

### 5.4 配置示例：暴露一个上游服务

通过 Admin API 完成配置：

```bash
# 1. 登录获取 token
curl -s -X POST http://127.0.0.1:8001/api/v1/login \
  -H "Content-Type: application/json" \
  -d '{"username":"admin","password":"admin123"}' | jq
# 保存返回的 token

TOKEN="your-token-here"

# 2. 创建 Upstream
curl -X POST http://127.0.0.1:8001/api/v1/upstreams \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name":"my-upstream","algorithm":"round-robin"}'

# 3. 创建 Target
curl -X POST http://127.0.0.1:8001/api/v1/targets \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"upstream_id":"<upstream-id>","target":"httpbin.org","port":80,"weight":1}'

# 4. 创建 Service
curl -X POST http://127.0.0.1:8001/api/v1/services \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name":"httpbin","upstream_id":"<upstream-id>"}'

# 5. 创建 Route
curl -X POST http://127.0.0.1:8001/api/v1/routes \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name":"httpbin-route",
    "service_id":"<service-id>",
    "paths":["/httpbin"],
    "methods":["GET","POST"],
    "strip_path":true
  }'

# 6. 发布 Revision
curl -X POST http://127.0.0.1:8001/api/v1/revisions \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{}'
```

### 5.5 验证代理生效

```bash
# 请求经过 DP 转发到 httpbin
curl http://127.0.0.1:8080/httpbin/get

# 如果 strip_path=true 且 target 是 httpbin.org:80
# 实际转发到 http://httpbin.org/get
```

### 5.6 回滚

```bash
# 查看 revision 列表
curl http://127.0.0.1:8001/api/v1/revisions \
  -H "Authorization: Bearer $TOKEN"

# 回滚到指定 revision
curl -X POST http://127.0.0.1:8001/api/v1/revisions/<revision-id>/rollback \
  -H "Authorization: Bearer $TOKEN"
```

回滚后，DP 会自动拉取旧 revision 并原子切换，不影响当前在线流量。

---

## 6. Admin API 使用指南

### 6.1 接口概览

| 资源 | 端点 | 说明 |
|------|------|------|
| 登录 | `POST /api/v1/login` | 获取管理 token |
| 路由 | `GET/POST/PUT/DELETE /api/v1/routes` | 路由 CRUD |
| 服务 | `GET/POST/PUT/DELETE /api/v1/services` | 服务 CRUD |
| 上游 | `GET/POST/PUT/DELETE /api/v1/upstreams` | 上游 CRUD |
| 目标 | `GET/POST/PUT/DELETE /api/v1/targets` | 目标 CRUD |
| 消费者 | `GET/POST/PUT/DELETE /api/v1/consumers` | 消费者 CRUD |
| 插件 | `GET/POST/PUT/DELETE /api/v1/plugins` | 插件 CRUD |
| 凭据 | `GET/POST/PUT/DELETE /api/v1/credentials` | 认证凭据 CRUD |
| Revision | `GET/POST /api/v1/revisions` | 发布与查询 |
| 监控指标 | `GET /api/v1/monitoring/metrics` | 聚合指标 |
| 监控健康 | `GET /api/v1/monitoring/health` | DP 健康详情 |
| DP 状态 | `GET /api/v1/monitoring/dp-status` | DP 实例状态 |
| 审计日志 | `GET /api/v1/audit-logs` | 操作审计 |

### 6.2 鉴权说明

- Admin API 开发环境默认监听 `127.0.0.1`，不对外暴露
- 写操作（POST/PUT/DELETE）需要 Bearer Token
- 所有写操作自动记录审计日志
- 敏感字段（API Key、JWT Secret 等）不回显原文

### 6.3 插件挂载

插件可以挂载在四级作用域：

```
global → service → route → consumer
```

- 后层覆盖前层同名插件配置
- 同一作用域内同名插件最多一份
- 最终得到当前请求的 `effective plugin set`

```bash
# 全局挂载：对所有请求生效
curl -X POST http://127.0.0.1:8001/api/v1/plugins \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name":"access-log",
    "scope":"global",
    "config":{
      "format":"json"
    }
  }'

# 挂载到某个 route
curl -X POST http://127.0.0.1:8001/api/v1/plugins \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name":"key-auth",
    "scope":"route",
    "scope_id":"<route-id>",
    "config":{
      "key_names":["apikey"]
    }
  }'
```

---

## 7. Dashboard 使用指南

Dashboard 是独立 SPA，前后端分离，不嵌入 Go 二进制。

### 7.1 启动

```bash
cd dashboard
npm install
npm run dev
# 浏览器打开 http://localhost:3000
```

### 7.2 页面功能

| 页面 | 功能 |
|------|------|
| 登录页 | 管理员登录 |
| 概览页 | 配置对象统计、active revision 信息 |
| 路由管理 | 创建、编辑、删除路由 |
| 服务管理 | 创建、编辑、删除服务 |
| 上游管理 | 创建、编辑上游及 target |
| 消费者管理 | 创建、编辑消费者和凭据 |
| 插件管理 | 查看和挂载插件 |
| Revision 页 | 查看 revision 列表、创建发布、回滚 |
| 监控页 | QPS、错误率、延迟、DP 实例状态 |

### 7.3 典型操作流程

1. 打开 Dashboard，登录
2. 进入「上游」→ 创建 Upstream 和 Target
3. 进入「服务」→ 创建 Service，绑定 Upstream
4. 进入「路由」→ 创建 Route，绑定 Service
5. 进入「插件」→ 挂载认证或限流插件
6. 进入「Revision」→ 点击「发布」按钮
7. 验证代理和监控页效果

---

## 8. 插件系统

### 8.1 插件阶段

每个插件可以挂载到三个阶段：

```
OnRequest → [反向代理] → OnResponse → 返回客户端
                              ↓
                           OnError
```

| 阶段 | 触发时机 | 典型用途 |
|------|---------|---------|
| `OnRequest` | 收到请求后、代理上游之前 | 认证、限流、请求改写 |
| `OnResponse` | 上游返回响应后 | 响应改写、CORS |
| `OnError` | 代理出错时 | 错误格式化、日志 |

### 8.2 v0.2 内置插件

| 插件 | 类型 | 说明 |
|------|------|------|
| `jwt-auth` | OnRequest | JWT 认证 |
| `key-auth` | OnRequest | API Key 认证 |
| `rate-limiting` | OnRequest | 限流（本地/Redis） |
| `request-transformer` | OnRequest | 请求头/体改写 |
| `response-transformer` | OnResponse | 响应头/体改写 |
| `cors` | OnResponse | CORS 跨域 |
| `access-log` | OnResponse | 访问日志 |
| `prometheus` | OnResponse | Prometheus 指标 |

### 8.3 不属于插件的核心能力

以下能力由网关核心实现，不能通过插件绕过：

- 负载均衡
- 重试
- 熔断
- 主动健康检查
- 被动健康检查

---

## 9. 监控与可观测性

### 9.1 运行时指标

DP 暴露以下端点：

| 端点 | 访问频率 | 内容 |
|------|---------|------|
| `GET /metrics` | 按需 | QPS、延迟、错误率、状态码分布、限流命中 |
| `GET /health` | 每 5 秒 | 简单健康状态字符串 |
| `GET /health/detail` | 每 30 秒 | 每个 upstream/target 的健康详情、熔断状态 |
| `GET /revision` | 按需 | 当前 revision ID、配置统计 |

### 9.2 CP 聚合监控

CP 轮询所有 DP 实例的监控端点，提供聚合数据：

| 端点 | 内容 |
|------|------|
| `GET /api/v1/monitoring/metrics` | 聚合 QPS、错误率、延迟、各 DP 详情 |
| `GET /api/v1/monitoring/health` | 各 DP 中每个 target 的健康状态 |
| `GET /api/v1/monitoring/dp-status` | DP 实例列表、版本一致性检测 |

### 9.3 审计日志

所有配置写操作（创建/修改/删除）自动记录审计日志，包含：

- 操作人
- 操作时间
- 操作类型
- 操作前后状态
- 请求 IP

```bash
# 查询审计日志
curl http://127.0.0.1:8001/api/v1/audit-logs \
  -H "Authorization: Bearer $TOKEN"
```

### 9.4 访问日志

数据面自动记录每次请求的访问日志，包含：

- 请求方法、路径、状态码
- 延迟
- 匹配的 route / service
- 认证消费者
- trace ID

---

## 10. 故障场景

### 10.1 PostgreSQL 不可用时

- Control Plane 的写操作（创建/修改配置）会失败
- 已经生效的 DP 流量不受影响，继续使用内存中的最后一份快照
- CP 恢复后，正常写入配置并重新发布即可

### 10.2 Redis 不可用时

- 分布式限流降级为本地限流（限流精度降低但不中断服务）
- 不影响正常代理流量
- Redis 恢复后自动重新连接

### 10.3 DP 收到无效快照

- DP 会拒绝应用，保持旧 revision 继续服务
- 上报错误到日志
- 需要检查 CP 端配置合法性

### 10.4 发布失败

- 不影响当前 active revision
- 当前在线流量继续使用旧版本
- 修复配置后重新发布即可

---

## 11. 开发与测试

### 11.1 快速运行测试

```bash
# 运行不需要数据库的单元/集成测试
go test ./tests/integration -run "TestM2|TestM3|TestM4|TestM5|TestM8|TestM10|TestM12" -v
```

### 11.2 运行需要 PostgreSQL 的测试

```bash
go test -tags=integration ./tests/integration -run "TestM13|TestMultiCP" -v
```

### 11.3 人工 Smoke 测试

完整冒烟链路：

1. 启动 PostgreSQL、Redis、CP、DP
2. 登录 Admin API 或 Dashboard
3. 创建 Upstream → Target → Service → Route
4. 挂载 JWT 或 Key Auth 插件
5. 发布 Revision
6. 请求 DP 代理端口，验证路由和认证生效
7. 查看监控指标和审计日志

### 11.4 压测

```bash
# 三档压测：plain（无插件）、auth（认证）、ratelimit（限流）
go run tests/benchmark/benchmark.go
```

---

## 12. 常见问题

### Q: Control Plane 和 Data Plane 必须分开部署吗？

不必。本地开发可以用 `portkey --mode all` 在一台机器上同时运行。生产环境建议分开部署以隔离故障域。

### Q: 为什么配置变更要走 revision 发布，不能直接热更新？

这是 Portkey 的核心设计决策。版本化发布保证：

- 变更可回滚
- DP 只消费完整快照，避免部分更新风险
- 发布失败不影响在线流量

### Q: Service 和 Upstream 有什么区别？

Service 是上游服务的逻辑抽象（"用户服务"），Upstream 是负载均衡组（具体有哪些节点、用啥算法）。Service 必须绑定 Upstream，不能直连 host:port。

### Q: 支持多 Control Plane 吗？

v0.2 以单 CP 为主，但 PostgreSQL 作为权威配置源天然支持多 CP 扩展。后续版本会补全多 CP 高可用方案（已有迁移文件 `0005_multi_cp_high_availability.sql` 预留了基础结构）。

### Q: 支持 gRPC 和 WebSocket 代理吗？

v0.2 暂不支持。代码结构已预留了扩展口（`internal/data/proxy/grpc.go` 和 `websocket.go`），会在后续版本补齐。

### Q: 日志和指标会占用大量磁盘吗？

访问日志和指标保留在进程内，不持久化到磁盘。建议生产环境配合外部日志收集系统（如 Loki、ELK）和时序数据库（如 Prometheus + Grafana）使用。

---

## 附录：端口汇总

| 组件 | 默认端口 | 说明 |
|------|---------|------|
| Control Plane | 8001 | Admin API |
| Data Plane | 8080 | 代理入口 |
| Dashboard | 3000 | 管理界面 |
| PostgreSQL | 5432 | 配置数据库 |
| Redis | 6379 | 共享状态 |

## 附录：文件位置

| 文件/目录 | 用途 |
|-----------|------|
| `cmd/portkey/main.go` | 启动入口 |
| `configs/config.yaml` | 运行配置 |
| `configs/config.example.yaml` | 配置模板 |
| `configs/config.test.yaml` | 测试配置 |
| `migrations/postgres/` | 数据库迁移 |
| `dashboard/` | 前端 SPA |
| `docs/` | 产品、架构、设计文档 |
| `tests/` | 集成测试、e2e 测试、测试数据 |
| `scripts/` | 开发与 CI 脚本 |
