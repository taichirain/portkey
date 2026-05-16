# Portkey Milestone 11：Traffic Split Design

> 版本：v0.2 | 日期：2026-05-04 | 状态：Draft

---

## 1. 目标

Milestone 11 只做最小可用的灰度发布能力。

范围：

- header 灰度
- weight 灰度

不在第一版范围内：

- consumer 灰度
- sticky 灰度
- 多次递归灰度
- 复杂流量治理

验收重点：

- 规则能进入 revision 并由 DP 原子生效
- Dashboard 或 Admin API 能完成创建、发布、回滚
- 命中结果和回滚结果可被观察到

---

## 2. 核心结论

1. 灰度是 `route` 命中后的流量决策，不属于 route matcher 本身
2. 第一版灰度目标只允许切到另一个 `service`
3. 灰度规则必须进入 revision snapshot，由 DP 原子应用
4. 第一版只做 `header + weight`
5. `service-scoped plugin` 继续绑定 `original service`
6. `weight` 只做请求级概率命中，不做 sticky
7. 一次请求最多命中一条灰度规则，不允许递归重写

---

## 3. 请求链路

推荐执行顺序：

1. route 匹配
2. 解析灰度规则
3. 计算 `effective service / upstream / balancer`
4. 构建插件链
5. 执行认证、限流、代理、重试、健康检查

说明：

- 灰度只改变流量最终落点
- route 归属和 service 作用域语义仍按原始 route 保持稳定

---

## 4. 配置模型

新增独立资源：

- `TrafficPolicy`

最小字段：

- `id`
- `name`
- `route_id`
- `priority`
- `type`
- `match_config`
- `target_service_id`
- `enabled`
- `tags`

约束：

- `type` 只允许 `header` / `weight`，`consumer` 先保留模型定义但不进入第一版实现验收
- 同一 `route` 下 `priority` 唯一
- `target_service_id` 必须存在
- `target_service_id` 不能等于 source route 当前绑定的 service

---

## 5. 规则类型

### Header

```json
{
  "type": "header",
  "match_config": {
    "header": "X-Canary",
    "value": "beta"
  }
}
```

规则：

- header key 大小写不敏感
- value 精确匹配
- 不做正则、前缀、包含

### Weight

```json
{
  "type": "weight",
  "match_config": {
    "percentage": 10
  }
}
```

规则：

- `percentage` 范围 `1-100`
- 按请求级概率命中
- 不做 sticky

---

## 6. 控制面与发布

Traffic Policy 进入正常配置发布流程：

1. 写入 PostgreSQL
2. 配置校验
3. 构建 snapshot
4. 生成 revision
5. 发布 revision
6. DP 拉取并原子切换

说明：

- 修改 `traffic_policies` 表不会立刻影响在线流量
- 在线真相仍然是 DP 当前 active snapshot

---

## 7. 数据模型

建议新增表：

- `traffic_policies`

建议字段：

| 列名 | 类型 | 约束 |
|---|---|---|
| `id` | `uuid` | `pk` |
| `name` | `varchar(255)` | `not null` |
| `route_id` | `uuid` | `not null fk routes(id)` |
| `priority` | `integer` | `not null` |
| `type` | `varchar(32)` | `not null` |
| `match_config` | `jsonb` | `not null` |
| `target_service_id` | `uuid` | `not null fk services(id)` |
| `enabled` | `boolean` | `not null default true` |
| `tags` | `text[]` | `not null default '{}'` |
| `created_at` | `timestamptz` | `not null default now()` |
| `updated_at` | `timestamptz` | `not null default now()` |

建议索引：

- unique `(route_id, priority)`
- index `(route_id)`
- index `(target_service_id)`

---

## 8. Snapshot 口径

revision snapshot 新增：

- `traffic_policies`

示例：

```json
{
  "traffic_policies": [
    {
      "id": "3d3d2f4f-30e0-4c5d-a5d8-cf84d61d6f71",
      "name": "canary-by-header",
      "route_id": "4c0ef5df-930a-4f47-8b13-3d6c677c54cb",
      "priority": 100,
      "type": "header",
      "match_config": {
        "header": "X-Canary",
        "value": "beta"
      },
      "target_service_id": "29f234ea-5169-4d4e-bc4f-ae848e2f6571",
      "enabled": true,
      "tags": [
        "canary"
      ]
    }
  ]
}
```

决策：

- snapshot 允许保留 disabled policies
- DP build 时只装载 `enabled=true` 的规则

---

## 9. DP 运行时结构

DP 内存建议组织为：

- `route_id -> ordered policies`

要求：

- 每个 route 下按 `priority asc` 排序
- 只保留启用规则
- 命中第一条规则后立即停止

建议在运行时区分：

- `original service`
- `effective service`
- `original upstream`
- `effective upstream`
- `traffic policy hit`

这样日志和观测才不会混。

---

## 10. API 口径

最小 API：

- `POST /api/v1/traffic-policies`
- `GET /api/v1/traffic-policies?id={id}`
- `GET /api/v1/routes/{route_id}/traffic-policies`
- `GET /api/v1/traffic-policies?page={page}&page_size={page_size}`
- `PUT /api/v1/traffic-policies?id={id}`
- `DELETE /api/v1/traffic-policies?id={id}`

统一响应格式继续沿用现有 Control Plane 风格：

```json
{
  "success": true,
  "data": {}
}
```

错误格式：

```json
{
  "success": false,
  "error": {
    "message": "Invalid request body",
    "code": "INVALID_REQUEST"
  }
}
```

API 返回只保留 canonical 字段：

- 返回 `target_service_id`
- 不返回 `target_service_name`

---

## 11. 已决策项

- 第一版不做 consumer 灰度
- 第一版不做 sticky
- `service-scoped plugin` 绑定 `original service`
- snapshot 保留 disabled policies
- API 不带目标 service 摘要

---

## 12. 验收口径

Milestone 11 至少要满足：

1. header policy 能命中灰度 service
2. weight policy 能按概率命中灰度 service
3. 发布 revision 后 DP 自动生效
4. 回滚 revision 后灰度规则失效
5. 日志或指标能区分 `original service` 与 `effective service`
6. 非法 policy 不会进入有效 revision
