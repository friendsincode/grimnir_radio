# Architecture Notes

## Media Engine Separation (Phase 4B)

### Overview
As of Phase 4B, Grimnir Radio uses a multi-process architecture with separate control plane and media engine binaries.

### Process Architecture

```
┌─────────────────────────────────────┐
│   Control Plane (grimnirradio)     │
│                                      │
│  ┌──────────┐      ┌──────────────┐│
│  │Scheduler │─────▶│  Executor    ││
│  └──────────┘      │  (per-station││
│                     │   goroutine) ││
│  ┌──────────┐      └──────┬───────┘│
│  │Priority  │             │        │
│  │Service   │─────────────┘        │
│  └──────────┘             │        │
│                           │        │
│                      ┌────▼────┐   │
│                      │  Media  │   │
│                      │Controller  │
│                      └────┬────┘   │
└───────────────────────────┼────────┘
                            │ gRPC
                            │ (port 9091)
┌───────────────────────────┼────────┐
│   Media Engine Process    │        │
│                      ┌────▼────┐   │
│                      │  gRPC   │   │
│                      │ Service │   │
│                      └────┬────┘   │
│                           │        │
│                      ┌────▼────────┐
│                      │  Pipeline   │
│                      │  Manager    │
│                      └────┬────────┘
│                           │        │
│                   ┌───────▼────────┐
│                   │  GStreamer     │
│                   │  Pipelines     │
│                   └────────────────┘
└─────────────────────────────────────┘
```

### Component Responsibilities

**Control Plane (`grimnirradio`):**
- HTTP API endpoints
- Database management (PostgreSQL/MySQL/SQLite)
- Schedule generation
- Priority management (5-tier system)
- Per-station executors
- Event bus (Redis/NATS/in-memory)
- Authentication & authorization

**Media Engine (`mediaengine`):**
- gRPC server (default port: 9091)
- GStreamer pipeline management
- DSP graph processing (12 node types)
- Audio telemetry streaming
- Process supervision & health monitoring
- Crossfade & cue point handling
- Live input routing

### Communication Flow

1. **Playback Initiation:**
   ```
   Scheduler → Executor → MediaController → [gRPC] → MediaEngine → GStreamer
   ```

2. **Telemetry Updates:**
   ```
   GStreamer → MediaEngine → [gRPC Stream] → MediaController → Executor State
   ```

3. **Priority Changes:**
   ```
   Priority Service → Event Bus → Executor → MediaController → [gRPC] → MediaEngine
   ```

### Migration from Old Architecture

The old playout system (`internal/playout/pipeline.go`) launched GStreamer processes directly. This has been superseded by:

- **Old:** `PlayoutManager` → Direct `gst-launch` execution
- **New:** `Executor` → `MediaController` → gRPC → `MediaEngine` → GStreamer

### Configuration

**Control Plane Environment Variables:**
- `MEDIAENGINE_GRPC_ADDR`: Media engine address (default: `localhost:9091`)

**Media Engine Environment Variables:**
- `MEDIAENGINE_GRPC_BIND`: Bind address (default: `0.0.0.0`)
- `MEDIAENGINE_GRPC_PORT`: gRPC port (default: `9091`)
- `MEDIAENGINE_LOG_LEVEL`: Log level (default: `info`)
- `GSTREAMER_BIN`: GStreamer binary path (default: `gst-launch-1.0`)

### Benefits of Separation

1. **Process Isolation:** GStreamer crashes don't bring down the control plane
2. **Resource Management:** Media processing in separate cgroup/container
3. **Scalability:** Multiple media engines for different stations
4. **Debugging:** Clear separation of audio pipeline from business logic
5. **Upgrades:** Can restart media engine without disrupting API/database

### Deployment

See `deploy/systemd/` for service files:
- `grimnirradio.service` - Control plane
- `mediaengine.service` - Media engine

The media engine should start before the control plane and both should be supervised by systemd.

### Future Enhancements

- Multiple media engines per control plane (station sharding)
- Media engine pools with load balancing
- Remote media engines for distributed deployment
- WebRTC integration for browser-based live input
