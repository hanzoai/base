# syntax=docker/dockerfile:1
#
# Hanzo Base — single Go binary, admin SPA embedded via //go:embed.
#
# The React admin SPA lives at ui-react/dist/ and is embedded by the Go
# binary at compile time (ui-react/embed.go uses //go:embed all:dist).
# The committed ui-react/dist is the source of truth for CI builds —
# rebuild it locally with `pnpm --dir ui-react build` before tagging.
FROM golang:1.26.4-alpine AS builder
RUN apk add --no-cache git ca-certificates tzdata
WORKDIR /build
# Private cross-org modules (hanzoai/*, luxfi/* — luxfi/zap forward bridge) are
# fetched via authenticated git, bypassing the public proxy. gh_token is the
# shared docker-build.yml BuildKit secret; no-op when absent (local/dev).
ENV GOPRIVATE=github.com/hanzoai/*,github.com/luxfi/*,github.com/zap-proto/*
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=secret,id=gh_token \
    if [ -s /run/secrets/gh_token ]; then \
      git config --global url."https://x-access-token:$(cat /run/secrets/gh_token)@github.com/".insteadOf "https://github.com/"; \
    fi && \
    go mod download
COPY . .

# Per SCALE_STANDARD.md §2 — every Go production Dockerfile that
# emits JSON to a client builds with GOEXPERIMENT=jsonv2. Verified
# -12% time / -23% allocs on the edge POST roundtrip vs encoding/json
# v1 (json_bench_test.go in hanzoai/zip).
ARG GO_EXPERIMENT=jsonv2
ENV GOEXPERIMENT=${GO_EXPERIMENT}

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
    CMD curl -f http://localhost:8090/healthz || exit 1
ENTRYPOINT ["/app/base"]
CMD ["serve", "--http=0.0.0.0:8090", "--dir=/data", "--migrationsDir=/migrations", "--hooksDir=/hooks"]
