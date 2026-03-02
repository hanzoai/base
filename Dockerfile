# syntax=docker/dockerfile:1
FROM node:20-alpine AS ui-builder
WORKDIR /ui
COPY ui/package.json ui/package-lock.json ./
RUN npm ci
COPY ui/ .
RUN npm run build

FROM golang:1.26-alpine AS builder
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
COPY --from=builder /build/ui/dist /app/public
RUN mkdir -p /data /migrations /hooks && chown -R hanzo:hanzo /app /data /migrations /hooks
USER hanzo
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD curl -f http://localhost:8080/api/health || exit 1
ENTRYPOINT ["/app/base"]
CMD ["serve", "--http=0.0.0.0:8080", "--dir=/data", "--migrationsDir=/migrations", "--hooksDir=/hooks"]
