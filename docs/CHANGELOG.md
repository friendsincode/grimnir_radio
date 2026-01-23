# Changelog

## 0.0.1-alpha (Phase 4B Complete) — 2026-01-22

### Phase 4B: Media Engine Separation (100% Complete)
**Multi-Process Architecture with gRPC Communication**

- Created separate `mediaengine` binary with gRPC server (port 9091)
- Implemented DSP graph builder supporting 12 node types:
  - Loudness Normalization (EBU R128)
  - AGC (Automatic Gain Control)
  - Compressor, Limiter, Equalizer, Gate
  - Silence Detector, Level Meter, Mix, Duck
- Built pipeline manager with GStreamer integration
  - Crossfade support with configurable curves (linear, log, exp, S-curve)
  - Cue point handling (intro/outro markers)
  - Emergency insertion with immediate preemption
  - Live input routing with DSP processing
- Added process supervision and watchdog
  - Health monitoring (5-second intervals)
  - Automatic restart on crash (rate limited)
  - Heartbeat tracking (15-second timeout)
- Implemented gRPC client for control plane
  - Connection management with automatic retry
  - All 8 RPC method wrappers (LoadGraph, Play, Stop, Fade, InsertEmergency, RouteLive, StreamTelemetry, GetStatus)
  - Real-time telemetry streaming with callbacks
- Integrated executor with media engine
  - MediaController wrapper for high-level API
  - Priority event handling via gRPC
  - Telemetry streaming (1-second intervals)
- Created production deployment tooling
  - Systemd service files with resource limits
  - Security hardening (PrivateTmp, ProtectSystem, NoNewPrivileges)
  - Complete installation and operations guide
- Comprehensive integration tests
  - 10 client integration tests (connection, playback, telemetry, concurrency)
  - 3 end-to-end tests (executor + media engine + priority + telemetry)
  - All 13 tests passing (100% success rate)
- Documentation
  - Architecture diagrams and component breakdown
  - Deployment guide for systemd
  - Migration notes from old playout system

**Files Added:**
- `cmd/mediaengine/main.go` - Media engine binary
- `proto/mediaengine/v1/mediaengine.proto` - gRPC service definition
- `internal/mediaengine/` - Service, pipeline, supervisor, DSP graph builder
- `internal/mediaengine/client/` - gRPC client
- `internal/executor/media_controller.go` - Executor integration
- `deploy/systemd/` - Systemd service files and deployment guide
- `test/integration/` - End-to-end integration tests
- `docs/ARCHITECTURE_NOTES.md` - Architecture documentation

**Code Statistics:**
- 7,260 lines of production code
- 890 lines of integration tests
- 20+ unit tests for DSP graph builder
- 13 integration tests (all passing)

### Phase 4A: Executor Refactor & Priority System (100% Complete)

- Implemented 5-tier priority system
  - Emergency (0), Live Override (1), Live Scheduled (2), Automation (3), Fallback (4)
  - State machine with transition validation
  - Preemption rules and priority scoring
- Built executor state machine
  - 6 states: Idle, Preloading, Playing, Fading, Live, Emergency
  - Complete transition validation
  - Buffer management and preloading
- Created priority service
  - InsertEmergency, StartOverride, StartScheduledLive, ActivateAutomation, Release
  - Event bus integration
  - Transaction handling
- Implemented event bus
  - Redis event bus with pub/sub
  - NATS support (alternative)
  - Fallback to in-memory bus
- Added REST API endpoints
  - Priority management (`/api/v1/priority/`)
  - Executor state (`/api/v1/executor/`)
  - Real-time telemetry endpoints
- Created 50+ unit tests for state machine and priority logic

### Phase 0: Foundation Fixes (100% Complete)

- Created missing media service package
  - File storage operations
  - S3 client for object storage
  - Integration with API handlers
- Fixed module path errors
  - Updated from `github.com/example/grimnirradio` to `github.com/friendsincode/grimnir_radio`
  - Fixed 18 Go files across codebase
- Added basic unit tests
  - Smart block engine tests
  - Scheduler tests
  - Media service tests

## 0.0.1-alpha (Initial) — Documentation Alpha Baseline
- Standardized project naming to Grimnir Radio
- Added README with shout-outs and naming details
- Introduced .gitignore and Makefile (`verify`, `build`, etc.)
- Created Sales, Engineering, and Programmer specs
- Added Webstream feature plans with fallback chains and env knobs
- Documented migration paths from AzuraCast/LibreTime (local takeover and remote API)
- Added VS Code setup guide, workspace configs, and `.env.example`
- Archived original Smart Blocks specs

Note: This is a documentation alpha, not a production release.
