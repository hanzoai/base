# syntax=docker/dockerfile:1
FROM node:20-alpine AS ui-builder
RUN corepack enable && corepack prepare pnpm@latest --activate
WORKDIR /ui
COPY ui/package.json ui/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile
COPY ui/ .
RUN pnpm build

FROM golang:1.24-alpine AS builder
RUN apk add --no-cache git ca-certificates tzdata
WORKDIR /build
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
COPY --from=ui-builder /ui/dist ./ui/dist
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build \
    -ldflags="-s -w -X github.com/hanzoai/base.Version=$(git describe --tags --always 2>/dev/null || echo 'dev')" \
    -o /build/base \
    ./examples/base/main.go

FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata curl \
    && addgroup -S hanzo && adduser -S hanzo -G hanzo
WORKDIR /app
COPY --from=builder /build/base /app/base
RUN mkdir -p /data /migrations /hooks /app/public && chown -R hanzo:hanzo /app /data /migrations /hooks
USER hanzo
EXPOSE 8090
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD curl -f http://localhost:8090/api/health || exit 1
ENTRYPOINT ["/app/base"]
CMD ["serve", "--http=0.0.0.0:8090", "--dir=/data", "--migrationsDir=/migrations", "--hooksDir=/hooks"]
