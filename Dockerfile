# syntax=docker/dockerfile:1
# Build stage
FROM golang:1.24-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git ca-certificates tzdata

WORKDIR /build

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Version can be passed as build arg (from CI) or derived from git
ARG VERSION=""

# Build binary with optimizations (BuildKit cache for faster rebuilds)
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    VERSION_VAL="${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo 'dev')}" && \
    VERSION_VAL="${VERSION_VAL#v}" && \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-w -s -X github.com/friendsincode/grimnir_radio/internal/version.Version=${VERSION_VAL}" \
    -o grimnirradio \
    ./cmd/grimnirradio

# Runtime stage
FROM alpine:3.19

# Install runtime dependencies
RUN apk add --no-cache \
    ca-certificates \
    tzdata \
    curl \
    su-exec \
    ffmpeg \
    gstreamer \
    gstreamer-tools \
    gst-plugins-base \
    gst-plugins-good \
    gst-plugins-bad \
    gst-plugins-ugly \
    libshout \
    && addgroup -S grimnir \
    && adduser -S -G grimnir grimnir

# Copy binary from builder
COPY --from=builder /build/grimnirradio /usr/local/bin/grimnirradio

# Copy license and notices into image for distribution compliance
RUN mkdir -p /usr/share/licenses/grimnir-radio/third_party/licenses
COPY --from=builder /build/LICENSE /usr/share/licenses/grimnir-radio/LICENSE
COPY --from=builder /build/THIRD_PARTY_NOTICES.md /usr/share/licenses/grimnir-radio/THIRD_PARTY_NOTICES.md
COPY --from=builder /build/third_party/go-licenses.csv /usr/share/licenses/grimnir-radio/third_party/go-licenses.csv
COPY --from=builder /build/third_party/licenses/ /usr/share/licenses/grimnir-radio/third_party/licenses/

# Copy entrypoint script
COPY docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh
RUN chmod +x /usr/local/bin/docker-entrypoint.sh

# Create necessary directories
RUN mkdir -p /var/lib/grimnir/media \
    && mkdir -p /etc/grimnir \
    && chown -R grimnir:grimnir /var/lib/grimnir

# Expose ports
EXPOSE 8080 9000

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD curl -f http://localhost:8080/healthz || exit 1

# Set working directory
WORKDIR /var/lib/grimnir

# Run via entrypoint (handles permission fixing and user switching)
ENTRYPOINT ["/usr/local/bin/docker-entrypoint.sh"]
CMD ["/usr/local/bin/grimnirradio", "serve"]
