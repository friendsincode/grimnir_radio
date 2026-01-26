# Grimnir Radio v2.0.0 - MediaEngine Migration

## Overview

Move GStreamer audio processing from the grimnir container to a dedicated mediaengine container via gRPC.

**Why:** If GStreamer crashes currently, it kills the API. Separation improves reliability and allows independent scaling of audio processing.

## Current vs Target Architecture

| Component | v1.x (Current) | v2.0 (Target) |
|-----------|----------------|---------------|
| grimnir | Go API + GStreamer | Go API only |
| mediaengine | Exists, unused | All audio via gRPC |
| Communication | exec.Command | gRPC client |

### Current Flow (v1.x)
```
grimnir container
├── HTTP API
├── Scheduler
├── Director
└── GStreamer (exec.Command)
    └── Pipelines managed in-process
```

### Target Flow (v2.0)
```
grimnir container          mediaengine container
├── HTTP API               ├── gRPC Server (9091)
├── Scheduler              ├── GStreamer pipelines
├── Director               └── Audio processing
└── MediaEngine gRPC Client
    └── Play/Stop/Status calls
```

## Implementation Plan

### 1. Add MediaEngine Client to Server

**`internal/config/config.go`** - Add configuration:
```go
MediaEngineAddr string  // "mediaengine:9091"
```

Load from environment:
```go
MediaEngineAddr: getEnvAny([]string{"GRIMNIR_MEDIA_ENGINE_ADDR"}, "mediaengine:9091"),
```

**`internal/server/server.go`** - Initialize client:
```go
import "github.com/friendsincode/grimnir_radio/internal/mediaengine/client"

// In initDependencies():
mediaClient, err := client.New(s.cfg.MediaEngineAddr, s.logger)
if err != nil {
    return fmt.Errorf("create mediaengine client: %w", err)
}
s.DeferClose(func() error { return mediaClient.Close() })

s.director = playout.NewDirector(..., mediaClient, ...)  // pass client
```

### 2. Update Director

**`internal/playout/director.go`** - Replace local pipelines with gRPC:

```go
// OLD (current):
d.playout.EnsurePipelineWithDualOutput(ctx, mount, launch, hqHandler, lqHandler)

// NEW (v2.0):
d.mediaClient.Play(ctx, &pb.PlayRequest{
    StationId: stationID,
    MountId:   mountID,
    Source:    &pb.Source{Type: pb.SOURCE_TYPE_MEDIA, Path: mediaPath},
})
```

Director changes:
- Remove `manager *Manager` field, add `mediaClient *client.Client`
- Remove all `buildBroadcastPipeline` and `buildDualBroadcastPipeline` methods
- Replace `manager.EnsurePipeline*` calls with `mediaClient.Play`
- Replace `manager.StopPipeline` calls with `mediaClient.Stop`

### 3. Remove GStreamer from Main Dockerfile

**`Dockerfile`** - Delete lines 33-39:
```dockerfile
# REMOVE these lines:
    gstreamer \
    gstreamer-tools \
    gst-plugins-base \
    gst-plugins-good \
    gst-plugins-bad \
    gst-plugins-ugly \
    libshout \
```

### 4. Update Docker Compose

**`docker-compose.yml`**:
- Verify `GRIMNIR_MEDIA_ENGINE_ADDR: mediaengine:9091` exists in grimnir environment (already present as `GRIMNIR_MEDIA_ENGINE_GRPC_ADDR`)
- Remove `shm_size` from grimnir if present (no longer needs shared memory for GStreamer)

### 5. Delete Unused Files

After migration, these files become unused:
- `internal/playout/manager.go` - Local pipeline management
- `internal/playout/pipeline.go` - GStreamer process wrapper

## Files Summary

| File | Action |
|------|--------|
| `internal/config/config.go` | Add MediaEngineAddr field |
| `internal/server/server.go` | Init mediaengine client, pass to Director |
| `internal/playout/director.go` | Replace local pipelines with gRPC calls |
| `internal/playout/manager.go` | Delete (unused after migration) |
| `internal/playout/pipeline.go` | Delete (unused after migration) |
| `Dockerfile` | Remove GStreamer packages |
| `docker-compose.yml` | Verify env var, remove shm_size from grimnir |

## Verification

```bash
# Start services
docker compose up -d

# Check mediaengine health
docker compose exec mediaengine mediaengine health

# Test stream
curl -s http://localhost:8080/live/main | head -c 1000 | xxd

# Check logs for gRPC communication
docker compose logs -f grimnir mediaengine
```

## Rollback Plan

If issues occur:
1. Revert Dockerfile to include GStreamer packages
2. Revert director.go to use local Manager
3. Re-add manager.go and pipeline.go if deleted
4. Remove mediaengine client initialization from server.go

## Also in v2.0

### Open Source Dependency Audit

Create exhaustive documentation of all open source packages used:
- Go dependencies (from go.mod)
- NPM packages (frontend)
- System packages (Alpine APK in Dockerfiles)
- GStreamer plugins
- Runtime dependencies (PostgreSQL, Redis, Icecast, etc.)

For each package, document:
- Package name and version
- License type (MIT, Apache-2.0, GPL, LGPL, etc.)
- Link to license on GitHub/source repo
- Usage in Grimnir Radio

Output: `docs/THIRD_PARTY_LICENSES.md`

## Future Work (Post v2.0)

These are planned for later versions:
- DSP graph UI (visual audio processing)
- Crossfade with cue points
- Multi-engine scaling (multiple mediaengine instances)
- Live input routing UI
- Audio normalization presets via mediaengine
