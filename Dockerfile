# syntax=docker/dockerfile:1
#
# Hanzo Base — single Go binary, admin SPA embedded via //go:embed.
#
# The React admin SPA lives at ui-react/dist/ and is embedded by the Go
# binary at compile time (ui-react/embed.go uses //go:embed all:dist).
# The committed ui-react/dist is the source of truth for CI builds —
# rebuild it locally with `pnpm --dir ui-react build` before tagging.
FROM public.ecr.aws/docker/library/golang:1.26.5-alpine AS builder
RUN apk add --no-cache git ca-certificates tzdata
WORKDIR /build
# Every module in this graph is public, so the download is the proxy verified
# against go.sum — no credential, no direct git.
#
# go.sum holds the PROXY hashes, and fetching direct is what silently picks up a
# moved tag:
#
#   verifying github.com/luxfi/vm@v1.3.1/go.mod: checksum mismatch
#     downloaded: h1:uViH3COP8hh…   (direct git — a re-cut tag)
#     go.sum:     h1:6YR/uFV2Fo…    (== sum.golang.org, byte for byte)
#
# luxfi does force-move tags, which is real, but that is an argument FOR the
# proxy: it serves the immutable copy sum.golang.org signed, so re-cutting a tag
# can no longer change what this image is built from without the checksum saying
# so.
#
# GOPRIVATE=github.com/hanzoai/* sat here, forcing hanzoai/* direct and
# unverified on the claim that they were private. Four of them — ltx, lz4,
# replicate, tygoja — had been moved to another org and the builder could not
# read them at all, which is what broke this image. A public module cannot
# depend on a private one, so those four went back to hanzoai public; with
# nothing private left in the graph, the token and the two knobs have no job.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .

# Per SCALE_STANDARD.md §2 — every Go production Dockerfile that
# emits JSON to a client builds with GOEXPERIMENT=jsonv2. Verified
# -12% time / -23% allocs on the edge POST roundtrip vs encoding/json
# v1 (json_bench_test.go in zap-proto/zip).
ARG GO_EXPERIMENT=jsonv2
ENV GOEXPERIMENT=${GO_EXPERIMENT}

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build \
    -ldflags="-s -w -X github.com/hanzoai/base.Version=$(git describe --tags --always 2>/dev/null || echo 'dev')" \
    -o /build/base \
    ./examples/base/main.go

FROM public.ecr.aws/docker/library/alpine:3.21
# sqlite (CLI) + python3: CTO directive — every pod ships the sqlite3 CLI and a
# python3 runtime for debugging the embedded store. Base is a CGO_ENABLED=0
# pure-Go build (modernc sqlite) with per-value app encryption, so the on-disk
# DB is a STANDARD SQLite file — the plaintext sqlite3 CLI opens it directly
# (unlike IAM's SQLCipher store, which needs the codec build). Runtime only, no
# build toolchain.
RUN apk add --no-cache ca-certificates tzdata curl sqlite python3 \
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
