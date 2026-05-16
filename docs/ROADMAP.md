# Portkey API Gateway — Roadmap

> 版本：v0.2 | 日期：2026-05-03 | 状态：Draft

---

## 1. 原则

这份 roadmap 按 **实现依赖顺序** 排列，不按开发周期估算排列。

目标只有两个：

- 先把 **Control Plane / Data Plane** 的边界做对
- 再逐层叠加代理能力、插件能力和管理能力

---

## 2. 总览

1. Milestone 0：工程骨架
2. Milestone 1：配置模型与 PostgreSQL
3. Milestone 2：Control Plane 最小闭环
4. Milestone 3：Data Plane 快照消费
5. Milestone 4：核心代理链路
6. Milestone 5：插件系统 v1
7. Milestone 6：认证能力
8. Milestone 7：限流能力
9. Milestone 8：上游可靠性
10. Milestone 9：Dashboard 最小可用版
11. Milestone 10：测试、压测与故障验证

---

## 3. Milestones

### Milestone 0：工程骨架

**Deliverables**

- 仓库目录结构定型
- `portkey control`
- `portkey data`
- `portkey all`
- 基础日志、错误码、request id
- `/health`、`/ready`

**Done 标准**

- 三种启动模式都能正常启动
- Control Plane 和 Data Plane 端口独立
- 本地可以用空配置跑通健康检查

**测试方式**

- 手工 smoke：分别启动 `control`、`data`、`all`
- 检查端口监听是否符合预期
- 请求 `/health`、`/ready`，确认返回成功

**依赖**

- 无

---

### Milestone 1：配置模型与 PostgreSQL

**Deliverables**

- PostgreSQL schema
- migrations
- domain model
- repository 接口与实现

最小表：

- `services`
- `routes`
- `upstreams`
- `targets`
- `consumers`
- `credentials`
- `plugins`
- `admins`
- `audit_logs`
- `config_revisions`

**Done 标准**

- 核心实体能完成 CRUD
- 唯一约束、外键、基本校验生效
- 写操作能留下审计日志

**测试方式**

- 集成测试：连 PostgreSQL 跑核心 repository CRUD
- 插入重复键、非法外键，确认约束报错
- 执行一次写操作，确认 `audit_logs` 有记录

**依赖**

- Milestone 0

---

### Milestone 2：Control Plane 最小闭环

**Deliverables**

- 管理员登录
- Admin API
- 配置校验器
- revision 发布流程
- revision 查询与回滚入口

最小 API 范围：

- routes
- services
- upstreams
- targets
- consumers
- plugins
- revisions

**Done 标准**

- 能通过 API 创建配置对象
- 能执行 `validate -> snapshot -> publish`
- 能标记 active revision
- 发布失败不会破坏当前 active revision

**测试方式**

- 集成测试：走一次最小 Admin API 创建配置并发布 revision
- 构造一份非法配置，确认发布失败且 active revision 不变
- 手工 smoke：查询 revision 列表并执行一次回滚

**依赖**

- Milestone 1

---

### Milestone 3：Data Plane 快照消费

**Deliverables**

- DP 启动时全量拉取快照
- DP watch revision 更新
- 本地构建只读内存快照
- 原子切换
- 失败保留旧版本

**Done 标准**

- CP 发布新 revision 后，DP 无需重启即可生效
- 非法快照不会替换在线配置
- DP 能暴露当前 revision 状态

**测试方式**

- 集成测试：启动 CP + DP，发布新 revision，确认 DP 自动切换
- 注入坏快照，确认 DP 继续服务旧 revision
- 手工 smoke：查看 DP 当前 revision 状态接口

**依赖**

- Milestone 2

---

### Milestone 4：核心代理链路

**Deliverables**

- HTTP 反向代理
- 路由匹配
- `service -> upstream -> target` 解析
- Round Robin
- 基础 access log
- 基础 metrics
- trace id 透传

路由匹配最小支持：

- path
- method
- host
- header

**Done 标准**

- 请求可通过 DP 正常代理到上游
- 不同 route 能正确匹配到对应 service
- 多 target 下轮询行为正确
- 基础日志和指标可观测

**测试方式**

- e2e 测试：请求经过 DP 到 mock upstream
- 准备两到三个 route，验证 path/method/host/header 匹配
- 准备两个 target，连续请求确认轮询分布
- 检查 access log 与 metrics 暴露是否存在

**依赖**

- Milestone 3

---

### Milestone 5：插件系统 v1

**Deliverables**

- 插件接口
- 插件注册表
- 插件作用域合并
- `OnRequest`
- `OnResponse`
- `OnError`

作用域顺序：

`global -> service -> route -> consumer`

**Done 标准**

- 请求阶段插件可短路请求
- 响应阶段插件真实挂接到代理回包链路
- 同名插件覆盖规则稳定可测
- 能输出本次请求的 effective plugin set

**测试方式**

- 集成测试：用测试插件验证 `OnRequest`、`OnResponse`、`OnError`
- 构造同名插件在不同作用域挂载，验证覆盖顺序
- e2e 测试：确认短路响应和响应改写都发生在真实代理链路上

**依赖**

- Milestone 4

---

### Milestone 6：认证能力

**Deliverables**

- `jwt-auth`
- `key-auth`
- consumer / credential 关联
- 敏感字段脱敏
- secret 存储策略

**Done 标准**

- JWT 和 API Key 能按 route 或 global 启用
- 非法请求返回正确状态码
- Dashboard 和 API 不回显完整 secret
- 认证结果可传递到后续插件和日志

**测试方式**

- e2e 测试：覆盖 JWT 成功、失败、缺失三条路径
- e2e 测试：覆盖 API Key 成功、失败、缺失三条路径
- 集成测试：检查 API 返回和日志输出不泄漏完整 secret

**依赖**

- Milestone 5

---

### Milestone 7：限流能力

**Deliverables**

- 本地限流
- Redis 分布式限流
- 标准限流响应头

最小限流维度：

- consumer
- route
- ip

**Done 标准**

- 单 DP 场景下本地限流行为正确
- 多 DP 场景下 Redis 限流结果一致
- 超限返回 429
- 指标能反映限流命中次数

**测试方式**

- 集成测试：单 DP 验证 consumer、route、ip 三种维度
- 集成测试：双 DP + Redis，验证限流结果一致
- e2e 测试：确认超限返回 429 和标准响应头
- 检查 metrics 中有限流命中计数

**依赖**

- Milestone 5
- Redis 接入

---

### Milestone 8：上游可靠性

**Deliverables**

- 主动健康检查
- 被动健康检查
- 重试
- target 粒度熔断

**Done 标准**

- 异常 target 会被摘除
- 恢复后的 target 可以重新接流量
- 重试只在允许的条件下发生
- 熔断状态可观测

**测试方式**

- e2e 测试：一个 target 正常、一个 target 故障，确认故障节点被摘除
- 恢复故障 target，确认其能重新接收流量
- 构造可重试与不可重试请求，确认重试条件正确
- 检查熔断状态和健康状态是否可观测

**依赖**

- Milestone 4

---

### Milestone 9：Dashboard 最小可用版

**Deliverables**

- 登录页
- route 管理页
- service / upstream / target 管理页
- consumer 管理页
- plugin 管理页
- revision 发布页
- 基础监控页

**Done 标准**

- 不用手调 API，也能完成完整配置发布
- 能看到当前 active revision 和 DP revision 状态
- 能完成基础配置变更与发布操作

**测试方式**

- 手工 smoke：从登录开始完成一次完整配置创建与发布
- 手工 smoke：查看 active revision 与 DP revision 状态
- 最少补一条 e2e：验证核心页面能完成发布主链路

**依赖**

- Milestone 2
- Milestone 3

---

### Milestone 10：测试、压测与故障验证

**Deliverables**

- 单元测试
- 集成测试
- 压测脚本
- 故障场景验证清单

必须覆盖的验证：

- CP 发布 revision，DP 拉取并切换
- 认证、限流、日志、指标在真实请求上生效
- PostgreSQL 不可用时 CP 行为
- Redis 不可用时限流降级行为
- DP 收到坏快照时的保护行为

**Done 标准**

- 核心路径有自动化测试
- 压测结果能反映无插件、认证、限流三档差异
- 关键失败场景下系统行为可预期

**测试方式**

- 统一跑单元、集成、e2e 测试作为发布前门槛
- 运行压测脚本，输出三档基线结果
- 按故障清单逐条验证并记录预期行为

**依赖**

- Milestone 4 之后可逐步推进
- Milestone 9 完成后做完整收口

---

## 4. 推荐实现顺序

1. 工程骨架
2. PostgreSQL schema 与 repository
3. CP CRUD 与 revision 发布
4. DP 快照拉取与原子切换
5. HTTP proxy、router、Round Robin
6. 插件系统
7. JWT / Key Auth
8. 限流
9. 健康检查 / 重试 / 熔断
10. Dashboard
11. 测试与压测

---

## 5. 暂缓项

- 更复杂的流量治理细节
- Dashboard 体验打磨
- 更完整的观测与运维视图

这些不作为 `v0.2` 阻塞项，放到后续版本继续收口。

### v0.3 规划

如果 `Milestone 11-15` 已经开发完成，`v0.3` 建议继续沿用 milestone 编号，按两个步骤推进：

#### Milestone 16：控制面与观测收口

**Deliverables**

- Dashboard 灰度配置、发布、回滚主链路补齐
- active revision / DP revision 状态展示
- 灰度命中、revision 切换、回滚结果的日志与指标补齐
- 关键失败场景的可视化与排障信息补齐

**Done 标准**

- 不用手调 API，也能在 Dashboard 完成灰度创建、发布、回滚
- 能明确看到当前 active revision、DP 当前 revision、最近一次切换结果
- 灰度命中和回滚结果可以通过日志或指标定位
- 发布失败、坏快照、非法规则等场景下系统行为可观察

**测试方式**

- e2e 测试：覆盖 Dashboard 灰度创建、发布、回滚主链路
- 集成测试：覆盖 revision 切换、失败保护、状态查询
- 手工 smoke：查看 revision 状态、灰度命中和回滚结果

**依赖**

- Milestone 15

---

#### Milestone 17：复杂流量治理收口

**Deliverables**

- 在现有灰度能力上补更复杂的规则组合
- 明确与认证、限流、健康检查的协同行为
- 保证复杂规则下的流量走向仍可预测、可回滚

**Done 标准**

- 复杂规则组合下流量结果稳定可解释
- 复杂治理规则与现有插件、认证、限流不冲突
- 出现异常节点或回滚操作时，流量结果仍然确定

**测试方式**

- e2e 测试：覆盖复杂规则组合与回滚场景
- 集成测试：覆盖治理规则与健康检查、限流、认证的组合行为
- 手工 smoke：验证复杂规则的命中结果与回滚结果

**依赖**

- Milestone 16

---

## 6. 里程碑验收口径

只有满足下面四条，才算 v0.2 主链路成立：

1. CP 能把配置发布成 revision
2. DP 能安全应用 revision，而不是直接改运行时对象
3. 请求能经过真实代理链路，并正确执行 request/response 插件
4. 单机环境下能完整验证管理、发布、代理、观测闭环

---

## 7. 最小测试原则

为了保证“每走一步都能验收”，每个 milestone 默认至少满足下面三条：

1. 至少有一个明确的外部验收结果，不接受只看内部代码结构
2. 至少有一种最小验证方式：单测、集成测试、e2e 或手工 smoke
3. 进入下一个 milestone 前，上一阶段主路径必须能重复验证

这里不追求过早建立复杂测试体系，先保证每一步都有可重复的验收出口。
