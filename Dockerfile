# Hanzo Base - PocketBase Fork
# Multi-stage build for minimal production image

# Build stage
FROM golang:1.24-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git ca-certificates tzdata

WORKDIR /build

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-s -w -X github.com/hanzoai/base.Version=$(git describe --tags --always 2>/dev/null || echo 'dev')" \
    -o /build/base \
    ./examples/base/main.go

# Production stage
FROM alpine:3.19

# Install runtime dependencies
RUN apk add --no-cache ca-certificates tzdata curl

# Create non-root user
RUN addgroup -S hanzo && adduser -S hanzo -G hanzo

WORKDIR /app

# Copy binary from builder
COPY --from=builder /build/base /app/base

# Copy UI if exists
COPY --from=builder /build/ui/dist /app/hz_public

# Create data directories
RUN mkdir -p /pb_data /pb_migrations /pb_hooks && \
    chown -R hanzo:hanzo /app /pb_data /pb_migrations /pb_hooks

USER hanzo

# Expose default port
EXPOSE 8080

# Environment variables
ENV PB_ENCRYPTION_KEY=""

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD curl -f http://localhost:8080/api/health || exit 1

# Default command
ENTRYPOINT ["/app/base"]
CMD ["serve", "--http=0.0.0.0:8080", "--dir=/pb_data", "--migrationsDir=/pb_migrations", "--hooksDir=/pb_hooks"]
