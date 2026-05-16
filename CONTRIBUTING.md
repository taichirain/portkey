# Contributing to Portkey

Thanks for your interest in contributing!

## Development Setup

```bash
# Prerequisites
# - Go 1.26+
# - PostgreSQL
# - Redis

# Clone and build
git clone https://github.com/taichirain/portkey.git
cd portkey
go build ./cmd/portkey

# Dashboard (optional)
cd dashboard && npm install && npm run dev
```

## Project Structure

- `cmd/` — Entry points
- `internal/` — All application code
  - `control/` — Control Plane (Admin API, auth, config management)
  - `data/` — Data Plane (proxy, plugins, rate limiting, health checks)
  - `domain/` — Pure domain models and business rules
  - `platform/` — Infrastructure (PostgreSQL, Redis, HTTP)
- `dashboard/` — React SPA frontend

## Code Conventions

- **Go**: Standard `gofmt` formatting. Tests alongside implementation (`*_test.go`).
- **Dashboard**: TypeScript + React functional components + Ant Design.
- **Imports**: Use the module path `github.com/taichirain/portkey`.
- **Commit messages**: Clear and descriptive, prefixed by area (e.g., `feat:`, `fix:`, `docs:`).

## Pull Request Process

1. Fork the repo, create a feature branch from `master`
2. Run tests: `go test ./...`
3. Add tests for new functionality
4. Update docs if changing behavior
5. Submit a PR with a clear description

## Credits

🧑‍💻 **Walker** · 🤖 **DeepSeek**
