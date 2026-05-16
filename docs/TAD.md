# Portkey API Gateway — 技术架构文档 (TAD)

> 版本：v0.2 | 日期：2026-05-03 | 状态：Draft | 文档地位：唯一技术真相来源

---

## 1. 目标

Portkey v0.2 按 **Kong-like** 的思路设计：

- **控制面 (Control Plane, CP)** 负责配置、鉴权、审计、配置分发
- **数据面 (Data Plane, DP)** 负责代理流量，不直接修改配置
- **Dashboard 前后端分离**，前端是独立 SPA，不嵌入网关二进制

这套架构 **可以在单机上完整测试**。本地开发时只是在一台机器上同时运行 CP、DP、PostgreSQL、Redis；逻辑边界仍然按分离设计，不做“先揉成一团，后面再拆”的路线。

---

## 2. 架构总览

### 2.1 逻辑拓扑

```text
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
                     v
                Upstream Services
```

### 2.2 运行模式

同一个仓库、同一个 Go 二进制，支持三种模式：

- `portkey control`：只启动控制面
- `portkey data`：只启动数据面
- `portkey all`：本地开发模式，同时启动 CP 和 DP

`all` 模式仅用于本地测试和单机演示，不改变 CP/DP 的职责边界。

---

## 3. 核心设计原则

1. **配置写入只发生在 CP**
2. **DP 只消费只读快照，不直接读写业务配置表**
3. **请求热路径不依赖数据库**
4. **配置变更以版本化快照发布，不做零散对象级热更新**
5. **响应阶段能力必须接入代理真实回包链路**
6. **管理面和流量面分端口、分鉴权、分故障域**

---

## 4. 控制面设计

### 4.1 职责

控制面负责：

- Admin API
- Dashboard 后端接口
- 管理员认证与 RBAC
- 配置校验
- 配置持久化
- 审计日志
- 版本化快照生成
- 向 DP 分发配置

控制面不负责代理业务流量。

### 4.2 配置源

`PostgreSQL` 作为唯一权威配置源，存储：

- services
- routes
- upstreams
- targets
- consumers
- credentials
- plugins
- admins
- audit_logs
- config_revisions

原因：

- 多实例 CP 场景下，SQLite 不适合作为共享权威源
- 事务、锁、唯一约束、回滚、审计链路都更自然
- 本地测试也可以用单机 PostgreSQL 或 Docker Compose 跑起来

### 4.3 配置发布模型

配置变更流程固定为：

`write config -> validate -> build snapshot -> create revision -> publish`

关键约束：

- 每次发布生成一个全量快照版本 `revision`
- DP 只接受“完整、可反序列化、可校验”的快照
- 发布失败不影响当前在线版本
- 支持把某个旧版本重新设为 active

### 4.4 配置分发

v0.2 采用 **CP 主动提供 watch 流 + DP 本地原子切换**：

- DP 启动时先拉取一次完整快照
- 之后与 CP 建立 watch 连接，监听新 revision
- 收到新快照后先本地构建内存结构
- 构建成功后通过原子指针整体替换
- 构建失败则继续使用旧快照，并上报错误

分发协议可以是：

- gRPC stream，或
- HTTP long-poll / SSE

v0.2 不强绑协议，但要保留“全量拉取 + 增量通知”的接口形态。

---

## 5. 数据面设计

### 5.1 职责

数据面只负责：

- TLS 终止
- 路由匹配
- 插件执行
- 上游选择
- 反向代理
- 重试
- 主动/被动健康检查
- 熔断
- 指标、访问日志、trace

数据面不提供配置 CRUD。

### 5.2 数据面本地状态

DP 只保留以下运行态：

- 当前生效的只读配置快照
- 上游连接池
- 健康检查状态
- 熔断器状态
- 本地限流器状态
- 指标缓存

其中：

- **配置快照** 来源于 CP
- **健康检查 / 熔断 / 本地限流** 属于运行态，不回写配置表

### 5.3 请求链路

```text
Client
  -> TLS
  -> Route Match
  -> Resolve Effective Plugins
  -> Request Plugins
  -> Upstream Select
  -> Retry / Circuit Breaker / Passive HC
  -> Reverse Proxy
  -> Response Plugins
  -> Access Log / Metrics / Trace
  -> Client
```

关键约束：

- `Request Plugins` 与 `Response Plugins` 是两段真实链路，不是文档概念
- `Response Plugins` 必须挂在代理回包阶段，例如 `ModifyResponse` / response wrapper
- 熔断器放在 **balancer/transport** 层，按 `target` 粒度统计
- 被动健康检查基于真实上游返回结果，不做独立插件

---

## 6. 配置模型

### 6.1 核心实体

- `Service`：上游服务抽象
- `Route`：请求匹配规则，绑定一个 Service
- `Upstream`：负载均衡组
- `Target`：上游实际节点
- `Consumer`：调用方身份
- `Credential`：Consumer 的认证凭证
- `Plugin`：挂载在不同作用域的扩展能力

### 6.2 Service 与 Upstream 边界

为避免语义打架，v0.2 规定：

- `Route` 必须绑定 `Service`
- `Service` 只描述上游访问策略，不直接存 `host/port`
- `Service` 必须绑定一个 `Upstream`
- `Upstream` 负责算法、target、健康检查、哈希策略

也就是说，**不再保留“Service 既能直连 host/port，又能关联 Upstream”这套双模型**。

### 6.3 路由规则

v0.2 支持：

- path prefix
- method
- host
- header
- path param

优先级规则：

1. 精确路径
2. 参数路径
3. 前缀路径
4. 通配路径
5. 同级再按显式 `priority`

### 6.4 插件挂载作用域

v0.2 支持四级挂载：

- global
- service
- route
- consumer

为控制复杂度，v0.2 **不支持多父级组合挂载**，例如：

- route + consumer
- service + consumer

这类组合先不做。

### 6.5 生效配置合并

请求进入 DP 后，按以下顺序合并插件配置：

`global -> service -> route -> consumer`

约束：

- 同一作用域内，同名插件最多一份
- 后层覆盖前层同名插件配置
- 最终得到本次请求的 `effective plugin set`

这套规则要写进实现和测试，不允许靠约定猜。

---

## 7. 插件系统

### 7.1 插件阶段

v0.2 只保留三段：

- `OnRequest`
- `OnResponse`
- `OnError`

但不是所有能力都做成插件：

- 认证、限流、改写、CORS、日志、指标：适合插件
- 熔断、重试、负载均衡、被动健康检查：归核心代理能力

这点必须明确，不然后面边界会越来越乱。

### 7.2 插件接口要求

插件至少要能拿到：

- request
- response metadata
- route
- service
- consumer
- selected upstream target
- shared vars
- trace / request id

否则很多插件能力会因为上下文不全无法实现。

### 7.3 插件加载方式

v0.2 只支持两种：

- **内置编译插件**：默认方案
- **进程外插件**：保留扩展口，后续可做 gRPC / WASM

`Go .so plugin` 不作为主路线。原因很简单：版本兼容性和部署可维护性太差。

---

## 8. 存储与共享状态

### 8.1 权威存储

- `PostgreSQL`：配置、审计、版本

### 8.2 共享运行态

- `Redis`：仅用于确实需要跨 DP 共享的状态

典型包括：

- 分布式限流计数
- 会话 / 登录态
- DP 心跳或租约信息

### 8.3 不采用的东西

v0.2 明确不引入：

- BadgerDB 配置缓存
- DP 本地持久化配置副本
- “SQLite + Redis + Badger + 内存快照”四层混合状态

理由：配置系统最怕状态源过多。v0.2 要先把“单一真相来源 + DP 原子快照”跑稳。

---

## 9. 管理面安全

### 9.1 Admin API

- 开发环境默认监听 `127.0.0.1:8001`
- 生产环境必须放在内网或受入口鉴权保护
- 所有变更接口禁止使用 `GET`
- 所有写操作记录审计日志

### 9.2 Dashboard

- 前端独立部署，调用 Admin API
- 使用 cookie session 或 OIDC，不在浏览器长期保存高权限 token
- 必须启用 CSRF 防护
- 所有展示到 UI 的敏感字段默认脱敏

### 9.3 Secret 管理

以下数据不允许明文回显：

- API Key
- JWT secret
- Basic Auth password
- 第三方 webhook token

存储策略：

- 数据库存密文，CP 启动时加载主密钥解密
- 或数据库只存 secret reference，真实密钥来自环境变量 / Vault

`audit_logs` 不能保存完整 secret 原文。

---

## 10. 可观测性

最小可观测性要求：

- 访问日志
- Prometheus 指标
- OpenTelemetry trace
- DP 健康状态与当前 revision

核心指标：

- request count
- request duration
- upstream status
- retry count
- circuit state
- rate limit reject count
- config apply success / failure

---

## 11. 本地测试拓扑

单机测试推荐直接跑四个组件：

```text
Dashboard   :3000
Control API :8001
Data Plane  :8080
PostgreSQL  :5432
Redis       :6379
```

验证顺序：

1. 在 CP 创建 route/service/upstream/plugin
2. CP 发布 revision
3. DP 拉到新 revision 并原子切换
4. 请求打到 `:8080`
5. 在 Dashboard/Prometheus 观察效果

这已经足够验证 Kong-like 架构，不需要一开始就上多机。

---

## 12. v0.2 范围

v0.2 只做最小正确架构，不追求功能大而全。

### 必做

- CP/DP 分离
- PostgreSQL 权威配置源
- DP 内存快照 + revision 切换
- HTTP 反向代理
- 路由匹配
- Round Robin
- request/response 插件链
- JWT / Key Auth
- 本地限流 + Redis 分布式限流
- 管理员登录
- 审计日志

### 暂缓

- gRPC 代理
- WebSocket
- 复杂 RBAC
- 灰度发布策略
- 多父级插件挂载
- 动态加载 `.so` 插件
- 多 CP 高可用

---

## 13. 结论

Portkey v0.2 的方向是：

- **像 Kong 一样做 CP/DP 分离**
- **像现代 Go 网关一样保持实现简单**
- **单机能测，后面能拆**

先把边界做对，再谈功能扩张。
