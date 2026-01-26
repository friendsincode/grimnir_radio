# Docker Build Optimization

## Current Pain Points

1. **Go compilation from scratch** - No build cache, recompiles all dependencies every time
2. **Large GStreamer packages** - ~200MB+ of packages downloaded and installed
3. **No pre-built images** - Every user must build locally
4. **Two separate builds** - grimnir and mediaengine share dependencies but build separately

## Quick Wins (Immediate)

### 1. Enable BuildKit Cache Mounts

Add to Dockerfile build stage:

```dockerfile
# Build binary with cache mounts
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-w -s" \
    -o grimnirradio \
    ./cmd/grimnirradio
```

Enable BuildKit in docker-compose or shell:
```bash
export DOCKER_BUILDKIT=1
docker compose build
```

### 2. Parallel Builds

Build both images simultaneously:
```bash
docker compose build --parallel
```

### 3. Use BuildKit in docker-quick-start.sh

```bash
export DOCKER_BUILDKIT=1
export COMPOSE_DOCKER_CLI_BUILD=1
```

## Medium-Term (v1.1)

### 1. Pre-built Base Image

Create `Dockerfile.base` with GStreamer pre-installed:

```dockerfile
FROM ubuntu:22.04
ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update && apt-get install -y \
    gstreamer1.0-tools \
    gstreamer1.0-plugins-base \
    gstreamer1.0-plugins-good \
    gstreamer1.0-plugins-bad \
    gstreamer1.0-plugins-ugly \
    gstreamer1.0-libav \
    libshout3 \
    ca-certificates curl \
    && rm -rf /var/lib/apt/lists/*
```

Publish to ghcr.io:
```bash
docker build -f Dockerfile.base -t ghcr.io/friendsincode/grimnir-base:latest .
docker push ghcr.io/friendsincode/grimnir-base:latest
```

Then Dockerfile.mediaengine becomes:
```dockerfile
FROM ghcr.io/friendsincode/grimnir-base:latest
COPY --from=builder /build/mediaengine /usr/local/bin/mediaengine
# ... rest of setup
```

**Savings:** ~2-3 minutes per build (no apt-get)

### 2. Pre-built Application Images

Publish complete images to GitHub Container Registry:

```yaml
# .github/workflows/docker-publish.yml
on:
  push:
    tags: ['v*']

jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: docker/build-push-action@v5
        with:
          push: true
          tags: ghcr.io/friendsincode/grimnir:${{ github.ref_name }}
          platforms: linux/amd64,linux/arm64
```

### 3. --pull Flag for Quick Start

Add to docker-quick-start.sh:
```bash
if [ "$USE_PREBUILT" = true ]; then
    # Pull pre-built images instead of building
    docker compose pull
else
    docker compose build --parallel
fi
```

## Long-Term (v2.0+)

### 1. Single Combined Image Option

For simpler deployments, offer an all-in-one image:
- grimnir + mediaengine in one container
- Supervisor or s6-overlay to manage processes
- Trade-off: Less isolation, but simpler

### 2. Nix-based Builds

Reproducible builds with perfect caching:
```nix
{
  dockerImage = pkgs.dockerTools.buildImage {
    name = "grimnir";
    contents = [ grimnir-binary gstreamer ];
  };
}
```

### 3. Binary Releases

For non-Docker users:
- GitHub Releases with pre-built binaries
- Linux amd64, arm64
- macOS amd64, arm64 (for dev)
- Users install GStreamer via package manager

## Build Time Comparison

| Method | First Build | Subsequent |
|--------|-------------|------------|
| Current (no cache) | ~5-8 min | ~5-8 min |
| With BuildKit cache | ~5-8 min | ~1-2 min |
| Pre-built base image | ~3-4 min | ~1-2 min |
| Pre-built app images | ~30 sec | ~30 sec |

## Implementation Priority

1. **Now:** Enable BuildKit caches in Dockerfiles
2. **v1.1:** Publish pre-built images to ghcr.io
3. **v1.1:** Add `--pull` flag to quick-start script
4. **v2.0:** Consider combined image option
