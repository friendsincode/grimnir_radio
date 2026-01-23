# Changelog

## 0.0.1-alpha (Phase 4C Complete) — 2026-01-22

### Phase 4C: Live Input & Webstream Relay (100% Complete)
**Harbor-Style Live Input and HTTP Stream Failover**

#### Live Input System
- Implemented live DJ session management with database persistence
  - Token-based authentication (32-byte cryptographically random tokens)
  - One-time use token validation
  - Session lifecycle tracking (connected, active, disconnected)
- Created live authorization service
  - `GenerateToken()` - Create authorization tokens for DJs
  - `AuthorizeSource()` - Validate tokens before connection
  - `HandleConnect()` - Start live session with priority integration
  - `HandleDisconnect()` - End live session and clean up
  - `GetActiveSessions()` - List all active DJ connections
- Added 6 REST API endpoints for live management
  - `POST /api/v1/live/tokens` - Generate authorization token
  - `POST /api/v1/live/authorize` - Validate token
  - `POST /api/v1/live/connect` - Start live session
  - `DELETE /api/v1/live/sessions/{id}` - Disconnect session
  - `GET /api/v1/live/sessions` - List active sessions
  - `GET /api/v1/live/sessions/{id}` - Get session details
- Implemented harbor-style live input in media engine
  - Icecast-compatible source client input (souphttpsrc)
  - RTP input over UDP
  - SRT (Secure Reliable Transport) input
  - WebRTC placeholder (future implementation)
- Integrated with priority system
  - Live override sessions (priority 1)
  - Live scheduled sessions (priority 2)
  - Automatic priority transitions on connect/disconnect
- Event bus integration
  - `dj.connect` - DJ connection events
  - `dj.disconnect` - DJ disconnection events

#### Webstream Relay System
- Created webstream model with failover chain support
  - Primary → Backup → Backup2 URL progression
  - Health check configuration (interval, timeout, method)
  - Failover settings (enabled, grace period, auto-recovery)
  - Buffer and reconnect settings
  - Metadata passthrough and override
- Implemented webstream service with health monitoring
  - CRUD operations for webstream configurations
  - Background health check workers (one per webstream)
  - Automatic health checker lifecycle management
  - Preflight connection checks
  - Manual failover and primary reset
- Built health check algorithm
  - HTTP HEAD/GET probes with configurable timeout
  - 3-tier health status: healthy → degraded → unhealthy
  - Consecutive failure tracking (degraded after 1, failover after 3)
  - Redirect handling (up to 3 redirects)
- Implemented failover logic
  - Test backup URL before switching
  - Grace window before failover
  - Skip unhealthy backups automatically
  - Auto-recovery to primary when healthy
- Added webstream player to media engine
  - GStreamer souphttpsrc for HTTP/Icecast streams
  - ICY metadata extraction (iradio-mode)
  - Configurable buffer size (max-size-time)
  - Fade-in support on webstream start
  - DSP graph routing for processing
- Created 7 REST API endpoints for webstream management
  - `GET /api/v1/webstreams` - List webstreams
  - `POST /api/v1/webstreams` - Create webstream
  - `GET /api/v1/webstreams/{id}` - Get webstream
  - `PUT /api/v1/webstreams/{id}` - Update webstream
  - `DELETE /api/v1/webstreams/{id}` - Delete webstream
  - `POST /api/v1/webstreams/{id}/failover` - Manual failover
  - `POST /api/v1/webstreams/{id}/reset` - Reset to primary
- Event bus integration
  - `webstream.failover` - Automatic/manual failover events
  - `webstream.recovered` - Auto-recovery to primary events

#### Scheduler Integration
- Added `SlotTypeWebstream` to clock slot types
- Updated scheduler to create webstream schedule entries
- Integrated webstream playback with playout director
  - Load webstream configuration from database
  - Build GStreamer pipeline with current URL
  - Respect failover state and health status
  - Publish now playing events with webstream metadata
  - Schedule automatic stop at entry end time

**Files Added:**
- `internal/models/live.go` - Live session model
- `internal/live/service.go` - Live authorization and session management
- `internal/api/live.go` - Live API handlers (6 endpoints)
- `internal/mediaengine/live.go` - Live input manager
- `internal/models/webstream.go` - Webstream model with failover
- `internal/webstream/service.go` - Webstream service
- `internal/webstream/health_checker.go` - Background health check workers
- `internal/mediaengine/webstream.go` - Webstream player
- `internal/api/webstream.go` - Webstream API handlers (7 endpoints)

**Files Modified:**
- `proto/mediaengine/v1/mediaengine.proto` - Added LiveInputType enum
- `internal/models/models.go` - Added SlotTypeWebstream
- `internal/scheduler/service.go` - Handle webstream slots
- `internal/playout/director.go` - Webstream playback integration
- `internal/server/server.go` - Webstream service initialization
- `internal/db/migrate.go` - LiveSession and Webstream migrations

**Code Statistics:**
- ~1,400 lines for live input system
- ~1,200 lines for webstream relay system
- ~200 lines for scheduler integration
- 13 new REST API endpoints
- 4 new event types

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
