# Portkey — Kong-like API Gateway

Portkey 是一个类 Kong 的 API Gateway，按 **Control Plane / Data Plane** 分离设计。面向单机可完整测试，后续可扩展到多实例生产部署。

## Architecture

```
┌─────────────────────────────────────────────────────────┐
│                    Control Plane                         │
│  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────┐  │
│  │ Admin    │ │ Auth     │ │ Publisher │ │ Validator│  │
│  │ API      │ │ Service  │ │ (NATS)   │ │          │  │
│  └──────────┘ └──────────┘ └──────────┘ └──────────┘  │
└────────────────────────┬────────────────────────────────┘
                         │
┌────────────────────────▼────────────────────────────────┐
│                      Data Plane                          │
│  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────┐  │
│  │ Proxy    │ │ Plugin   │ │ Rate     │ │ Health   │  │
│  │ (HTTP/WS │ │ Chain    │ │ Limit    │ │ Check    │  │
│  │ /gRPC)   │ │          │ │          │ │          │  │
│  └──────────┘ └──────────┘ └──────────┘ └──────────┘  │
└─────────────────────────────────────────────────────────┘
```

## Repository Layout

```
portkey/
├── cmd/                    # 启动入口
├── internal/
│   ├── app/                # 启动装配层（control/data/all）
│   ├── config/             # 配置加载与解析
│   ├── control/            # Control Plane 业务
│   ├── data/               # Data Plane 业务
│   ├── domain/             # 领域模型与核心规则
│   └── platform/           # PostgreSQL、Redis、日志等基础设施
├── configs/                # 本地开发与部署配置
├── dashboard/              # React 管理后台 SPA
├── migrations/             # 数据库迁移
├── tests/                  # 集成 / e2e 测试
└── docs/                   # 设计文档
```

## Layer Rules

- `cmd/` — 只做启动参数解析和模式选择，不写业务逻辑
- `internal/app/` — 负责组装依赖和启动服务
- `internal/domain/` — 领域对象、规则和不依赖外部系统的纯逻辑
- `internal/control/` — 管理面能力，不接代理热路径
- `internal/data/` — 数据面代理能力，不做配置 CRUD
- `internal/platform/` — 技术基础设施实现，不承载业务语义
- `dashboard/` — 前后端分离，不嵌入 Go 二进制

## Current Implementation

| Area | Status | Details |
|------|--------|---------|
| Proxy | ✅ | HTTP Reverse Proxy, WebSocket, gRPC |
| Plugin System | ✅ | Plugin chain runtime, dynamic loading, auth plugins (key-auth) |
| Rate Limiting | ✅ | Local (in-memory) + Redis distributed, per-consumer/route |
| Health Checks | ✅ | Active/passive, configurable intervals and thresholds |
| Admin API | ✅ | Service, Route, Upstream, Consumer CRUD |
| Auth | ✅ | JWT, RBAC, API key management |
| Dashboard | ⚡ Skeleton | React + Ant Design SPA, basic layout |
| Traffic Split | 🔄 Planned | Blue-green, canary, weighted routing |

## Quick Start

```bash
# Prerequisites: Go 1.26+, PostgreSQL, Redis

# Build
go build ./cmd/portkey

# Run (Control Plane + Data Plane)
./portkey

# Dashboard (separate terminal)
cd dashboard && npm install && npm run dev
```

> 完整使用指南见 [docs/USER_GUIDE.md](docs/USER_GUIDE.md)

## Tech Stack

| Layer | Technology |
|-------|-----------|
| Language | Go 1.26 |
| Proxy | net/http reverse proxy, gorilla/websocket, gRPC |
| Cache | Redis (go-redis) |
| Database | PostgreSQL (lib/pq) |
| Dashboard | React 18 + Ant Design + Vite |
| Testing | Go testing + Playwright (e2e) |

## Design Philosophy

1. **Control/Data Plane 分离** — 管理操作不阻塞代理路径，代理故障不影响管理可用性
2. **插件即架构** — 认证、限流、日志等功能以插件形式链式加载，核心可扩展
3. **领域驱动** — internal/domain 承载纯业务规则，不依赖任何框架或基础设施
4. **单机可测** — 无需 Kubernetes 即可本地完整测试，降低开发心智负担

## License

[MIT](LICENSE) © 2026 Walker
