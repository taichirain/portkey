# ===== Stage 1: Build Go binary =====
FROM golang:1.26-alpine AS builder

WORKDIR /app

# Cache dependencies
COPY go.mod go.sum ./
RUN go mod download

# Build
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /portkey ./cmd/portkey

# ===== Stage 2: Build Dashboard =====
FROM node:20-alpine AS dashboard-builder

WORKDIR /app
COPY dashboard/package.json dashboard/package-lock.json ./
RUN npm ci
COPY dashboard/ .
RUN npm run build

# ===== Stage 3: Runtime =====
FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

# Go binary
COPY --from=builder /portkey .

# Config
COPY configs/config.example.yaml configs/config.yaml

# Dashboard static files
COPY --from=dashboard-builder /app/dist /app/dashboard/dist

EXPOSE 8001 8080

ENTRYPOINT ["/app/portkey"]
CMD ["--mode", "all", "--config", "/app/configs/config.yaml"]
