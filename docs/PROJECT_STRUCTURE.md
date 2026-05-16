# Portkey 项目目录设计

> 日期：2026-05-03 | 状态：Draft

这份文档只回答一个问题：

**代码应该放哪，为什么放这里。**

---

## 1. 顶层目录

```text
.
├── api/
├── cmd/
├── configs/
├── dashboard/
├── deploy/
├── docs/
├── internal/
├── migrations/
├── scripts/
└── tests/
```

说明：

- `api/`：放接口契约，不放业务实现
- `cmd/`：放可执行入口
- `configs/`：放本地和部署配置模板
- `dashboard/`：前端 SPA
- `deploy/`：部署资源
- `docs/`：产品、架构、路线图文档
- `internal/`：Go 主体代码
- `migrations/`：数据库迁移文件
- `scripts/`：辅助脚本
- `tests/`：跨模块测试

---

## 2. Go 代码分层

```text
internal/
├── app/
├── config/
├── control/
├── data/
├── domain/
└── platform/
```

### 2.1 `internal/app/`

启动装配层，只负责把模块接起来。

```text
internal/app/
├── all/
├── control/
└── data/
```

建议内容：

- 读取配置
- 初始化 logger / db / redis / telemetry
- 构造 control plane / data plane runtime
- 管理启动和优雅关闭

不要放：

- 路由匹配算法
- 发布逻辑
- repository SQL 细节

### 2.2 `internal/config/`

放配置结构体、默认值、环境变量解析、配置文件加载。

建议内容：

- `config.go`
- `loader.go`
- `validate.go`

### 2.3 `internal/domain/`

放最核心的业务模型和规则，不依赖 PostgreSQL、Redis、HTTP 框架。

```text
internal/domain/
├── admin/
├── audit/
├── consumer/
├── credential/
├── plugin/
├── revision/
├── route/
├── service/
├── target/
└── upstream/
```

每个目录建议包含：

- 实体定义
- 值对象
- 核心校验规则
- 与该实体强相关的领域方法

不要放：

- SQL
- HTTP handler
- gin/chi/echo 之类框架对象

### 2.4 `internal/control/`

Control Plane 专属代码。

```text
internal/control/
├── api/
├── auth/
├── audit/
├── publish/
├── repository/
├── service/
└── validate/
```

职责划分：

- `api/`：Admin API handler、request/response DTO、路由注册
- `auth/`：管理员登录、token、RBAC
- `audit/`：审计记录写入与查询
- `publish/`：snapshot 构建、revision 生成、发布与回滚
- `repository/`：面向 PostgreSQL 的读写实现
- `service/`：控制面应用服务，编排 use case
- `validate/`：配置合法性校验器

### 2.5 `internal/data/`

Data Plane 专属代码。

```text
internal/data/
├── auth/
├── balancer/
├── health/
├── observability/
├── plugin/
├── proxy/
├── ratelimit/
├── router/
└── snapshot/
```

职责划分：

- `auth/`：JWT / API Key 在数据面的执行逻辑
- `balancer/`：target 选择、重试、熔断协作点
- `health/`：主动/被动健康检查
- `observability/`：metrics、trace、access log
- `plugin/`：插件接口、注册、作用域合并、执行链
- `proxy/`：反向代理主链路
- `ratelimit/`：本地限流和 Redis 限流
- `router/`：host/path/method/header 匹配
- `snapshot/`：revision 拉取、watch、构建、原子切换

### 2.6 `internal/platform/`

技术基础设施，不携带具体业务语义。

```text
internal/platform/
├── httpserver/
├── logging/
├── postgres/
├── redis/
└── telemetry/
```

适合放：

- DB client 初始化
- Redis client 初始化
- HTTP server 封装
- logger 封装
- telemetry 初始化

不适合放：

- route CRUD
- revision 发布
- upstream 选择规则

---

## 3. 前端目录

```text
dashboard/
├── public/
└── src/
```

等前端正式初始化后，建议继续拆成：

```text
dashboard/src/
├── app/
├── components/
├── features/
├── pages/
├── routes/
├── services/
├── styles/
└── types/
```

原则：

- 页面级逻辑进 `pages/`
- 按业务域拆功能进 `features/`
- API 请求统一进 `services/`
- 通用组件不和页面耦合

---

## 4. 迁移、脚本、测试

### 4.1 `migrations/postgres/`

只放 PostgreSQL migration：

- 建表
- 索引
- 约束
- 回滚脚本

### 4.2 `scripts/`

```text
scripts/
├── ci/
└── dev/
```

建议：

- `dev/` 放本地启动、初始化、生成脚本
- `ci/` 放 lint、test、build 相关脚本

### 4.3 `tests/`

```text
tests/
├── e2e/
├── integration/
└── testdata/
```

边界：

- `integration/`：测 CP、DP 与 PostgreSQL/Redis 的协作
- `e2e/`：测从 Dashboard/API 到 DP 代理的完整链路
- `testdata/`：样例配置、fixture、mock payload

---

## 5. 命名约束

建议尽早统一这几条：

- 不要创建 `utils/` 作为兜底垃圾桶目录
- 不要把控制面和数据面都塞进 `internal/server/`
- 不要把 SQL model 直接当 domain model 用
- 不要让 `handler -> repository` 直接穿透，应该经过 `service`
- 不要把插件、路由、代理全部混在一个 package 里

---

## 6. 当前推荐开发顺序

先补这几个最小文件最划算：

1. `go.mod`
2. `cmd/portkey/main.go`
3. `internal/config/config.go`
4. `internal/app/control/app.go`
5. `internal/app/data/app.go`
6. `internal/app/all/app.go`
7. `configs/config.example.yaml`
8. `migrations/postgres/0001_init.sql`

这样后面每个 milestone 都有稳定落点，不需要边写边重构目录。
