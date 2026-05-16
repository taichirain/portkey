# Portkey — Kong-like API Gateway

Portkey is an API Gateway inspired by Kong, built with a **Control Plane / Data Plane** separation architecture. Designed for local testability on a single machine, with the ability to scale to multi-instance production deployments.

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                       Control Plane                          │
│  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────────┐  │
│  │ Admin    │ │ Auth     │ │ Publisher│ │  Validator   │  │
│  │ API      │ │ Service  │ │ (NATS)  │ │              │  │
│  └──────────┘ └──────────┘ └──────────┘ └──────────────┘  │
└────────────────────────┬────────────────────────────────────┘
                         │
┌────────────────────────▼────────────────────────────────────┐
│                        Data Plane                            │
│  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────────┐  │
│  │ Proxy    │ │ Plugin   │ │ Rate     │ │   Health     │  │
│  │ (HTTP/WS │ │ Chain    │ │ Limit    │ │   Check      │  │
│  │ /gRPC)   │ │          │ │          │ │              │  │
│  └──────────┘ └──────────┘ └──────────┘ └──────────────┘  │
└─────────────────────────────────────────────────────────────┘
```

## Repository Layout

```
portkey/
├── cmd/                    # Entrypoints
├── internal/
│   ├── app/                # Dependency assembly and startup
│   ├── config/             # Configuration loading
│   ├── control/            # Control Plane logic
│   ├── data/               # Data Plane logic
│   ├── domain/             # Domain models and core rules
│   └── platform/           # PostgreSQL, Redis, logging, HTTP
├── configs/                # Dev and deployment configs
├── dashboard/              # React admin SPA
├── migrations/             # Database migrations
├── tests/                  # Integration and e2e tests
└── docs/                   # Design documents
```

## Current Implementation

| Area | Status | Details |
|------|--------|---------|
| Proxy | ✅ | HTTP Reverse Proxy, WebSocket, gRPC |
| Plugin System | ✅ | Chain runtime, dynamic loading, key-auth plugin |
| Rate Limiting | ✅ | Local (in-memory) + Redis distributed |
| Health Checks | ✅ | Active/passive, configurable intervals |
| Admin API | ✅ | Service, Route, Upstream, Consumer CRUD |
| Auth | ✅ | JWT, RBAC, API key management |
| Dashboard | ⚡ Skeleton | React + Ant Design SPA |
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

## Tech Stack

| Layer | Technology |
|-------|-----------|
| Language | Go 1.26 |
| Proxy | net/http, gorilla/websocket, gRPC |
| Cache | Redis (go-redis) |
| Database | PostgreSQL (lib/pq) |
| Dashboard | React 18 + Ant Design + Vite |
| Testing | Go testing + Playwright (e2e) |

## Design Philosophy

1. **Control/Data Plane Separation** — Admin operations never block the proxy hot path; proxy failures never affect admin availability.
2. **Plugin as Architecture** — Auth, rate limiting, logging, and other features are chain-loaded plugins. The core remains extensible.
3. **Domain-Driven** — `internal/domain` holds pure business rules with zero framework or infrastructure dependencies.
4. **Single-Machine Testable** — Full testability without Kubernetes. Lowers the cognitive overhead of development.

## License

[MIT](LICENSE) © 2026 Walker
