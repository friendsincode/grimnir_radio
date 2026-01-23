# Grimnir Radio - Architecture Roadmap

**Version:** 0.0.1-alpha (Phase 4C Complete)
**Target Architecture:** Go-Based Broadcast Automation Platform (Liquidsoap Replacement)

This document aligns the current Grimnir Radio implementation with the comprehensive design brief for a modern, Go-controlled broadcast automation platform.

---

## Design Principles (from Design Brief)

1. **Go owns the control plane** - A dedicated media engine owns real-time audio
2. **No audio scripting DSL** - Declarative configuration, not embedded logic
3. **No monolithic process** - Separate concerns, isolated failure domains
4. **Deterministic scheduling** - Same inputs → same outputs
5. **Planner/Executor separation** - Timeline generation separate from execution
6. **Observable and controllable** - All actions via API, real-time telemetry

---

## Current State Assessment

### ✅ Implemented Components

| Component | Status | Implementation |
|-----------|--------|----------------|
| **API Gateway** | ✓ Complete | `internal/api` - REST + WebSocket, JWT auth, RBAC |
| **Scheduler (Planner)** | ✓ Complete | `internal/scheduler` - Deterministic Smart Blocks, 48h rolling schedule |
| **Media Library** | ✓ Complete | `internal/media` - File ingest, tagging, analysis queue, S3 support |
| **Multi-Station** | ✓ Complete | Isolated stations with separate scheduling |
| **PostgreSQL Store** | ✓ Complete | Via GORM with MySQL/SQLite support |
| **Authentication** | ✓ Complete | JWT with 15-min TTL, 3-tier RBAC |
| **Priority System** | ✓ Complete | `internal/priority` - 5-tier priority (Emergency/Live Override/Live Scheduled/Automation/Fallback) |
| **Executor** | ✓ Complete | `internal/executor` - Per-station state machine, 6 states, priority handling |
| **Event Bus** | ✓ Complete | `internal/events` - Redis, NATS, and in-memory implementations |
| **Media Engine** | ✓ Complete | `cmd/mediaengine` - Separate binary with gRPC control (port 9091) |
| **gRPC Interface** | ✓ Complete | `proto/mediaengine/v1` - 8 RPC methods (LoadGraph, Play, Stop, Fade, etc.) |
| **DSP Pipeline** | ✓ Complete | `internal/mediaengine/dsp` - 12 node types (loudness, AGC, compressor, limiter, EQ, gate, etc.) |
| **Telemetry Streaming** | ✓ Complete | Real-time audio metrics via gRPC streaming (1-second intervals) |
| **Process Supervision** | ✓ Complete | `internal/mediaengine/supervisor` - Health monitoring, automatic restart |
| **Live Input** | ✓ Complete | `internal/live` - Token auth, session management, harbor-style routing (Icecast/RTP/SRT) |
| **Webstream Relay** | ✓ Complete | `internal/webstream` - Health probing, failover chains, metadata passthrough |

### ⚠️ Partial Implementations

| Component | Status | Current State | Needed |
|-----------|--------|---------------|--------|
| **Observability** | ⚠️ Stubs | Health checks, basic logging | Full Prometheus metrics, distributed tracing |

### ❌ Missing Components

| Component | Status | Priority | Impact |
|-----------|--------|----------|--------|
| **Recording Sink** | Not Started | **Low** | Compliance/archival feature |
| **Multi-Instance Scaling** | Not Started | **Medium** | Leader election, executor distribution |
| **Migration Tools** | Not Started | **Medium** | AzuraCast/LibreTime import |
| **Full Observability** | Not Started | **Medium** | Complete Prometheus metrics, distributed tracing |

---

## Target Process Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                      API Gateway (Go)                           │
│          :8080 REST + :9090 gRPC + WebSocket + SSE              │
│                   JWT Auth + RBAC + Rate Limiting               │
└──────────┬──────────────────────────────────────┬───────────────┘
           │                                      │
    ┌──────▼──────────┐                   ┌──────▼────────────────┐
    │   Planner       │                   │  Media Library        │
    │   (Scheduler)   │                   │  Service              │
    │                 │                   │  - LUFS Analysis      │
    │ - Smart Blocks  │                   │  - Rotation Rules     │
    │ - Clock Compile │                   │  - Artist Separation  │
    │ - Timeline Gen  │                   │  - Metadata Index     │
    └────┬────────────┘                   └───────────────────────┘
         │
         │ Schedule Timeline (time-ordered events)
         │
    ┌────▼────────────────────────────────────────────────────────┐
    │            Station Executor Pool (Go)                       │
    │  [Executor-1] [Executor-2] ... [Executor-N]                 │
    │  One per station │ State Machine │ Failover Logic           │
    └────┬──────────────────────────────────────────────────┬─────┘
         │                                                    │
         │ gRPC Control Channel                       Telemetry │
         │ (LoadGraph, Play, Stop, Fade, Route)               │
         │                                                    │
    ┌────▼────────────────────────────────────────────────────▼───┐
    │          Media Engine (GStreamer per station)               │
    │                                                              │
    │  [Input] → [Decode] → [DSP Graph] → [Encode] → [Outputs]   │
    │              ↓            ↓             ↓          ↓         │
    │            Files      Loudness       MP3      Icecast-1     │
    │            Live       AGC/Comp       AAC      Icecast-2     │
    │            WebRTC     Limiter        Opus     HLS           │
    │                       Ducking                 Recording     │
    │                                                              │
    │  Telemetry: buffer_depth, dropouts, cpu_usage, loudness    │
    └──────────────────────────────────────────────────────────────┘

┌──────────────────────────────────────────────────────────────────┐
│              Realtime Event Bus (Redis/NATS)                     │
│  Events: now_playing, source_failure, buffer_health, metrics    │
└──────────────────────────────────────────────────────────────────┘
```

---

## Implementation Phases

### Phase 4A: Foundation Refactoring ✅ COMPLETE

**Goal:** Align with design brief core principles

**Duration:** 6 weeks (Completed 2026-01-22)

**Tasks:**

1. **Split Scheduler → Planner + Executor** (Week 1-2)
   - [x] Create `internal/executor` package
   - [x] Executor state manager with database persistence
   - [x] Per-station executor goroutines with state machines (6 states: idle, preloading, playing, fading, live, emergency)
   - [x] State transition validation
   - [x] Integration with priority system

2. **Design gRPC Media Engine Interface** (Week 1-2)
   - [x] Create `proto/mediaengine/v1/mediaengine.proto`
   - [x] Define service with 8 RPC methods:
     - LoadGraph, Play, Stop, Fade, InsertEmergency, RouteLive, StreamTelemetry, GetStatus
   - [x] Generate Go stubs with protoc
   - [x] Create `internal/mediaengine/client` package

3. **Implement Priority System** (Week 2-3)
   - [x] Define priority enum in `internal/models/priority.go`:
     - `PriorityEmergency = 0` (EAS alerts)
     - `PriorityLiveOverride = 1` (Manual DJ takeover)
     - `PriorityLiveScheduled = 2` (Scheduled live shows)
     - `PriorityAutomation = 3` (Normal playout)
     - `PriorityFallback = 4` (Emergency fallback audio)
   - [x] Executor state machine honors priority
   - [x] Priority service with state machine and preemption rules
   - [x] API endpoints in `internal/api/priority.go`
   - [x] Event bus integration for priority changes

4. **Add Telemetry Channel** (Week 3-4)
   - [x] Implement telemetry streaming in executor (`telemetryStreamLoop`)
   - [x] Real-time metrics: audio levels (L/R), loudness LUFS, buffer depth, underrun count
   - [x] Publish telemetry updates to executor state (1-second intervals)
   - [x] WebSocket event streaming support
   - [x] Heartbeat tracking (5-second intervals)

5. **Update API for New Architecture** (Week 4)
   - [x] Created `internal/api/executor.go` for executor state endpoints
   - [x] Created `internal/api/priority.go` for priority management
   - [x] State machine transitions exposed via API
   - [x] Real-time telemetry endpoints

6. **Testing & Documentation** (Week 5-6)
   - [x] 50+ unit tests for state machine and priority logic
   - [x] Integration tests for executor + priority system
   - [x] Updated documentation (README, CHANGELOG, ARCHITECTURE_NOTES)

**Deliverables:**
- ✅ Clean planner/executor separation
- ✅ 5-tier priority system with state machine
- ✅ Telemetry stream architecture
- ✅ gRPC interface design
- ✅ Event bus implementation (Redis/NATS/in-memory)

---

### Phase 4B: Media Engine Implementation ✅ COMPLETE

**Goal:** Replace basic GStreamer with graph-based DSP pipeline

**Duration:** 8 weeks (Completed 2026-01-22)

**Tasks:**

1. **Implement gRPC Media Engine Server** (Week 1-3)
   - [x] Create `cmd/mediaengine` binary (separate process)
   - [x] Implement gRPC service implementing all 8 RPC methods
   - [x] GStreamer pipeline builder from DSP graph protobuf
   - [x] Per-station pipeline management with state tracking
   - [x] Bidirectional communication: Control Plane ↔ Media Engine

2. **Build DSP Graph System** (Week 2-4)
   - [x] Implemented 12 DSP node types in `internal/mediaengine/dsp/graph.go`:
     - **Input/Output:** Source and sink nodes
     - **Loudness:** EBU R128 normalization with rgvolume
     - **AGC:** Automatic Gain Control with configurable target level
     - **Compressor:** Dynamic range compression with threshold/ratio/attack/release
     - **Limiter:** True peak limiting
     - **Equalizer:** Multi-band EQ (10-band, 31-band)
     - **Gate:** Noise gate with threshold
     - **Silence Detector:** Dead air detection
     - **Level Meter:** Audio level monitoring
     - **Mix:** Audio mixing node
     - **Duck:** Ducking/sidechaining
   - [x] Node configuration via protobuf parameters
   - [x] Graph builder compiles protobuf → GStreamer pipeline strings

3. **Pipeline Management** (Week 4-5)
   - [x] Pipeline manager in `internal/mediaengine/pipeline.go`
   - [x] Crossfade support with configurable curves (linear, log, exp, S-curve)
   - [x] Cue point handling (intro/outro markers)
   - [x] Emergency insertion with immediate preemption
   - [x] Live input routing (RouteLive RPC)

4. **Process Supervision** (Week 5-6)
   - [x] Supervisor in `internal/mediaengine/supervisor.go`
   - [x] Health monitoring (5-second intervals)
   - [x] Automatic restart on crash (rate limited: max 5 in 5-minute window)
   - [x] Heartbeat tracking (15-second timeout)
   - [x] Resource cleanup on failure

5. **gRPC Client Integration** (Week 6-7)
   - [x] Created `internal/mediaengine/client/client.go`
   - [x] Connection management with automatic retry
   - [x] All 8 RPC method wrappers
   - [x] Real-time telemetry streaming with callbacks
   - [x] MediaController wrapper in `internal/executor/media_controller.go`
   - [x] Executor integration with telemetry streaming

6. **Integration & Testing** (Week 7-8)
   - [x] 10 client integration tests (connection, playback, fade, emergency, live, telemetry)
   - [x] 3 end-to-end tests (executor + media engine + priority + telemetry flow)
   - [x] All 13 tests passing (100% success rate)
   - [x] Production deployment tooling (systemd service files)
   - [x] Security hardening (PrivateTmp, ProtectSystem, NoNewPrivileges)

**Code Statistics:**
- 7,260 lines of production code
- 890 lines of integration tests
- 20+ unit tests for DSP graph builder
- 13 integration tests (all passing)

**Deliverables:**
- ✅ gRPC-controlled media engine (separate binary on port 9091)
- ✅ Graph-based DSP pipeline (12 node types)
- ✅ Pipeline manager with crossfade and cue point support
- ✅ Process supervision with automatic restart
- ✅ Real-time telemetry streaming
- ✅ Comprehensive integration tests
- ✅ Production-ready systemd service files

---

### Phase 4C: Live Input & Webstream Relay ✅ COMPLETE

**Goal:** Harbor-style live input and HTTP stream failover

**Duration:** 5 weeks (Completed 2026-01-22)

**Tasks:**

1. **Implement Live Session Management** (Week 1-2)
   - [x] Created `internal/models/live.go` - Live session model with database persistence
   - [x] Created `internal/live/service.go` - Authorization service with token generation
   - [x] Token-based authentication (32-byte cryptographically random tokens)
   - [x] One-time use token validation
   - [x] Session lifecycle tracking (connected, active, disconnected)
   - [x] Methods: GenerateToken, AuthorizeSource, HandleConnect, HandleDisconnect, GetActiveSessions
   - [x] Integration with priority system (live override and scheduled live)

2. **Create Live API Endpoints** (Week 2)
   - [x] Created `internal/api/live.go` with 6 REST endpoints:
     - `POST /api/v1/live/tokens` - Generate authorization token
     - `POST /api/v1/live/authorize` - Validate token
     - `POST /api/v1/live/connect` - Start live session
     - `DELETE /api/v1/live/sessions/{id}` - Disconnect session
     - `GET /api/v1/live/sessions` - List active sessions
     - `GET /api/v1/live/sessions/{id}` - Get session details
   - [x] Role-based access control (admin/manager for most endpoints)
   - [x] Event bus integration (dj.connect, dj.disconnect events)

3. **Implement Harbor-Style Live Input** (Week 2-3)
   - [x] Created `internal/mediaengine/live.go` - Live input manager
   - [x] Added LiveInputType enum to protobuf (Icecast, RTP, SRT, WebRTC)
   - [x] GStreamer pipeline building for each input type:
     - Icecast: souphttpsrc with Icy-MetaData header
     - RTP: udpsrc with RTP application type
     - SRT: srtsrc with connection URL
     - WebRTC: placeholder for future implementation
   - [x] DSP graph routing integration
   - [x] Fade-in support on live input start

4. **Implement Webstream Models** (Week 3)
   - [x] Created `internal/models/webstream.go` - Complete webstream model
   - [x] Failover chain support (primary → backup → backup2)
   - [x] Health check configuration (interval, timeout, method)
   - [x] Failover settings (enabled, grace period, auto-recovery)
   - [x] Buffer and reconnect settings
   - [x] Metadata passthrough and override
   - [x] Methods: GetCurrentURL, GetNextFailoverURL, FailoverToNext, ResetToPrimary

5. **Implement Webstream Service with Health Checks** (Week 3-4)
   - [x] Created `internal/webstream/service.go` - CRUD operations
   - [x] Created `internal/webstream/health_checker.go` - Background workers
   - [x] Health check algorithm:
     - HTTP HEAD/GET probes with configurable timeout
     - 3-tier status: healthy → degraded → unhealthy
     - Consecutive failure tracking (degraded after 1, failover after 3)
     - Redirect handling (up to 3 redirects)
   - [x] Failover logic:
     - Test backup URL before switching
     - Skip unhealthy backups automatically
     - Auto-recovery to primary when healthy
     - Event bus integration (webstream.failover, webstream.recovered)
   - [x] Health checker lifecycle management

6. **Add Webstream Support to Media Engine** (Week 4)
   - [x] Created `internal/mediaengine/webstream.go` - Webstream player
   - [x] GStreamer pipeline for HTTP/Icecast streams:
     - souphttpsrc with is-live and do-timestamp
     - ICY metadata extraction (iradio-mode)
     - Configurable buffer size (max-size-time)
     - Fade-in support
     - DSP graph routing
   - [x] Methods: PlayWebstream, StopWebstream, FailoverWebstream, GetWebstreamMetadata

7. **Create Webstream API Endpoints** (Week 4-5)
   - [x] Created `internal/api/webstream.go` with 7 REST endpoints:
     - `GET /api/v1/webstreams` - List webstreams
     - `POST /api/v1/webstreams` - Create webstream
     - `GET /api/v1/webstreams/{id}` - Get webstream
     - `PUT /api/v1/webstreams/{id}` - Update webstream
     - `DELETE /api/v1/webstreams/{id}` - Delete webstream (admin only)
     - `POST /api/v1/webstreams/{id}/failover` - Manual failover
     - `POST /api/v1/webstreams/{id}/reset` - Reset to primary
   - [x] Role-based access control (admin/manager)
   - [x] Comprehensive request/response types

8. **Integrate with Scheduler** (Week 5)
   - [x] Added `SlotTypeWebstream` to clock slot types
   - [x] Updated scheduler to create webstream schedule entries
   - [x] Modified playout director to handle webstream entries:
     - Load webstream configuration from database
     - Build GStreamer pipeline with current URL
     - Respect failover state and health status
     - Publish now playing events with webstream metadata
     - Schedule automatic stop at entry end time
   - [x] Updated server initialization to pass webstream service to director

**Code Statistics:**
- ~1,400 lines for live input system
- ~1,200 lines for webstream relay system
- ~200 lines for scheduler integration
- 13 new REST API endpoints
- 4 new event types

**Deliverables:**
- ✅ Token-based live authorization with session management
- ✅ 6 REST API endpoints for live DJ management
- ✅ Harbor-style live input (Icecast, RTP, SRT)
- ✅ Priority system integration for live sessions
- ✅ Complete webstream model with failover chains
- ✅ Background health check workers with automatic failover
- ✅ 7 REST API endpoints for webstream management
- ✅ Webstream player in media engine
- ✅ Scheduler integration for webstream entries

---

### Phase 5: Observability & Multi-Instance (MVP 3 → MVP 4)

**Goal:** Production-grade monitoring and horizontal scaling

**Duration:** 4-6 weeks

**Tasks:**

1. **Complete Prometheus Metrics** (Week 1-2)
   - [ ] Scheduler metrics:
     - `grimnir_schedule_build_duration_seconds`
     - `grimnir_schedule_entries_total`
     - `grimnir_smart_block_materialize_duration_seconds`
   - [ ] Executor metrics:
     - `grimnir_executor_state` (gauge per station)
     - `grimnir_playout_buffer_depth_samples`
     - `grimnir_playout_dropout_count_total`
     - `grimnir_playout_cpu_usage_percent`
   - [ ] Media engine metrics:
     - `grimnir_media_engine_loudness_lufs`
     - `grimnir_media_engine_output_health` (per output)
   - [ ] API metrics:
     - `grimnir_api_request_duration_seconds` (histogram)
     - `grimnir_api_requests_total` (counter)

2. **Add Distributed Tracing** (Week 2-3)
   - [ ] OpenTelemetry integration
   - [ ] Trace IDs propagated: API → Planner → Executor → Media Engine
   - [ ] Spans for key operations (schedule generation, media commands, API requests)
   - [ ] Jaeger or Tempo backend

3. **Alerting Rules** (Week 3-4)
   - [ ] Prometheus AlertManager integration
   - [ ] Alert rules: ScheduleGap, PlayoutUnderrun, OutputDown, MediaEngineDown, HighCPU
   - [ ] Webhook integration for alerts

4. **Multi-Instance Support** (Week 4-6)
   - [ ] Stateless API instances (load-balanced)
   - [ ] Leader election for planner (only one active)
   - [ ] Executor pool distributed across instances
   - [ ] Shared PostgreSQL with connection pooling
   - [ ] Shared Redis/NATS event bus
   - [ ] Shared media storage (S3 or NFS)

**Deliverables:**
- ✓ Complete Prometheus metrics
- ✓ Distributed tracing (OpenTelemetry)
- ✓ AlertManager integration
- ✓ Multi-instance deployment support

---

### Phase 6: Advanced Features (MVP 4 → 1.0)

**Goal:** Professional broadcast features and migration tools

**Duration:** 6-8 weeks

**Tasks:**

1. **Emergency Alert System (EAS)** (Week 1-2)
   - [ ] EAS alert ingestion: CAP-CP, SAME, webhooks
   - [ ] Automatic priority override (priority 0)
   - [ ] Pre-roll silence, post-roll resume
   - [ ] EAS alert logging for compliance

2. **Advanced Scheduling** (Week 2-4)
   - [ ] Conflict detection: overlapping shows, under-filled hours
   - [ ] Schedule optimization: minimize rotation violations
   - [ ] "What-if" simulation: test schedule changes before apply
   - [ ] Schedule templates: copy week-to-week
   - [ ] Holiday schedules with override dates

3. **Migration Tools** (Week 4-6)
   - [ ] AzuraCast backup import:
     - Parse MySQL/Postgres dump
     - Map stations, mounts, media, playlists, schedules
     - Dry-run report with diff
   - [ ] LibreTime backup import:
     - Parse Postgres dump
     - Map shows, webstreams, playlists
     - Media file sync
   - [ ] CLI: `grimnirradio import azuracast --backup backup.tar.gz --dry-run`
   - [ ] API: `POST /api/v1/migrations/azuracast` with progress events

4. **Recording & Compliance** (Week 6-8)
   - [ ] Recording sink for aircheck/compliance
   - [ ] Automatic file rotation
   - [ ] Metadata embedding in recordings
   - [ ] FCC-compliant logging

**Deliverables:**
- ✓ EAS compliance features
- ✓ Advanced scheduling tools
- ✓ AzuraCast/LibreTime migration tools
- ✓ Recording and compliance features

---

### Phase 7: WebDJ & User Experience (1.0 → 1.1)

**Goal:** Complete broadcast suite with web interface

**Duration:** 8-10 weeks

**Tasks:**

1. **WebDJ Interface** (Week 1-5)
   - [ ] Browser-based DJ control panel (React/Svelte)
   - [ ] Features:
     - Now playing display
     - Upcoming schedule
     - Manual track queue
     - Skip/fade controls
     - Live mic input (WebRTC)
     - Playlist search
   - [ ] Role-based UI (DJ vs Manager vs Admin)

2. **Voice Tracking** (Week 5-7)
   - [ ] Record voice tracks in browser
   - [ ] Insert voice tracks into schedule
   - [ ] Pre-recorded show assembly
   - [ ] Voice track library

3. **Listener Statistics** (Week 7-8)
   - [ ] Icecast/HLS listener count integration
   - [ ] Real-time listener dashboard
   - [ ] Historical listener graphs
   - [ ] Listener location/user-agent tracking

4. **Public API** (Week 8-9)
   - [ ] Public now-playing endpoint (no auth)
   - [ ] Public schedule endpoint
   - [ ] CORS configuration
   - [ ] Rate limiting for public endpoints

5. **Polish & Documentation** (Week 9-10)
   - [ ] Complete user documentation
   - [ ] Video tutorials
   - [ ] Production deployment guides
   - [ ] Performance tuning guides
   - [ ] Troubleshooting playbooks

**Deliverables:**
- ✓ Complete WebDJ interface
- ✓ Voice tracking system
- ✓ Listener statistics
- ✓ Public API
- ✓ Comprehensive documentation
- ✓ 1.0 Release

---

## Data Flow Diagrams

### Schedule Execution Flow

```
┌──────────────┐
│   Planner    │ Generates timeline every N minutes
│  (Scheduler) │ Input: Clocks, Smart Blocks, Shows, Overrides
└──────┬───────┘
       │ Output: Time-ordered events
       │ [
       │   {time: 14:00:00, type: "smart_block", id: "..."},
       │   {time: 14:15:00, type: "stopset", id: "..."},
       │   {time: 14:17:00, type: "smart_block", id: "..."}
       │ ]
       ▼
┌──────────────────────────┐
│  Station Executor (Go)   │ Polls timeline for upcoming events
│  State: Idle/Playing/    │ At T-30s: preload next item
│         Live/Emergency   │ At T: execute transition
└──────┬───────────────────┘
       │ gRPC Commands
       │ LoadGraph({nodes: [decode, loudness, encode]})
       │ Play({file: "/media/track.mp3", fade_in_ms: 500})
       ▼
┌────────────────────────────────┐
│    Media Engine (GStreamer)    │
│  [File Reader] → [Decoder]     │
│       ↓              ↓          │
│  [Loudness Normalize] (EBU R128)│
│       ↓                         │
│  [Encoder: MP3] → [Icecast Out] │
└────────────────────────────────┘
       │ Telemetry Stream (gRPC)
       │ {buffer_depth: 48000, dropouts: 0, cpu: 12.3%, loudness: -16.2}
       ▼
┌──────────────────────┐
│  Executor Monitors   │ Publishes to event bus
│  for Failures        │ WebSocket clients receive updates
└──────────────────────┘
```

### Live Takeover Flow (Priority System)

```
┌───────────────────────────────────────────────────────────────┐
│  Priority Ladder (Lower Number = Higher Priority)            │
│                                                               │
│  0: Emergency (EAS alerts)         [INTERRUPTS EVERYTHING]   │
│  1: Live Override (Manual DJ)      [PREEMPTS SCHEDULED]      │
│  2: Live Scheduled (Booked show)   [REPLACES AUTOMATION]     │
│  3: Automation (Smart Blocks)      [NORMAL OPERATION]        │
│  4: Fallback (Emergency audio)     [DEAD AIR PREVENTION]     │
└───────────────────────────────────────────────────────────────┘

Current State: Automation (Priority 3)
Playing: Smart Block track 4/10

Event: POST /api/v1/live/handover {station_id, mount_id, priority: 1}

┌──────────────────┐
│  Station Executor│
│  Receives Event  │
│  Priority: 1     │
└────┬─────────────┘
     │ Compare: 1 < 3 (override authorized)
     │
     ▼
┌─────────────────────────────────┐
│  Fade Out Current Track (500ms) │
│  Send: Fade({duration_ms: 500}) │
└────┬────────────────────────────┘
     │
     ▼
┌────────────────────────────────────┐
│  Route Live Input                  │
│  Send: RouteLive({               │
│    input: "icecast://dj:pass@...",│
│    fade_in_ms: 500               │
│  })                                │
└────┬───────────────────────────────┘
     │
     ▼
┌─────────────────────────────────┐
│  Update State: Live (Priority 1)│
│  Publish Event: dj.connect      │
└─────────────────────────────────┘

Live source disconnects after 60 minutes

┌──────────────────┐
│  Media Engine    │
│  Detects         │
│  Disconnect      │
└────┬─────────────┘
     │ Telemetry: {source_state: "disconnected"}
     ▼
┌─────────────────────────────────┐
│  Executor Receives Telemetry    │
│  Failback: Priority 1 → 3       │
└────┬────────────────────────────┘
     │
     ▼
┌─────────────────────────────────┐
│  Resume Automation              │
│  Load Next Scheduled Track      │
│  Crossfade In (1000ms)          │
└─────────────────────────────────┘
```

---

## Configuration Schema

### Declarative Pipeline Configuration (YAML)

```yaml
# /etc/grimnirradio/station-wgmr.yaml
station:
  id: "uuid"
  name: "WGMR FM"
  timezone: "America/New_York"

mounts:
  - id: "mount-main"
    name: "Main Stream"
    outputs:
      - type: icecast
        url: "icecast://source:password@localhost:8000/stream"
        format: mp3
        bitrate_kbps: 128

      - type: hls
        path: "/var/www/hls/wgmr"
        segment_duration_sec: 4

      - type: recording
        path: "/var/recordings/wgmr"
        rotation_days: 30

dsp_graph:
  nodes:
    - type: loudness_normalize
      target_lufs: -16.0
      true_peak_limit: -1.0

    - type: agc
      enabled: true
      target_level_db: -14.0

    - type: compressor
      threshold_db: -20.0
      ratio: 3.0
      attack_ms: 5
      release_ms: 50

    - type: limiter
      threshold_db: -1.0
      release_ms: 10

failover:
  live_timeout_sec: 30
  automation_fallback_enabled: true
  emergency_audio_path: "/media/emergency.mp3"

priorities:
  emergency: 0
  live_override: 1
  live_scheduled: 2
  automation: 3
  fallback: 4
```

---

## Success Metrics

### Phase 4A (Foundation) ✅ COMPLETE
- [x] Planner generates timeline in < 500ms for 48-hour window
- [x] Executor transitions between tracks via state machine
- [x] Priority system tested with emergency inserts (integration tests)
- [x] Telemetry stream delivers metrics every 1 second

### Phase 4B (Media Engine) ✅ COMPLETE
- [x] Multi-process architecture with gRPC communication (port 9091)
- [x] DSP graph builder with 12 node types
- [x] Crossfade support with configurable curves
- [x] Process supervision with automatic restart
- [x] Real-time telemetry streaming (1-second intervals)
- [x] 13 integration tests (100% passing)

### Phase 4C (Live & Webstreams) ✅ COMPLETE
- [x] Live authorization with token-based authentication
- [x] Live session tracking with database persistence
- [x] Harbor-style live input (Icecast, RTP, SRT)
- [x] Priority system integration for live sessions
- [x] Webstream health checks with automatic failover
- [x] Failover chain progression (primary → backup with auto-recovery)
- [x] Scheduler integration for webstream entries
- [x] 13 new REST API endpoints (6 live, 7 webstream)

### Phase 5 (Observability & Multi-Instance)
- [ ] All metrics exported to Prometheus
- [ ] Distributed traces show end-to-end request flow
- [ ] Alerts fire correctly for test failure scenarios
- [ ] Multi-instance deployment scales to 3 API replicas

### Phase 6 (Advanced Features)
- [ ] AzuraCast migration imports 1000-track library in < 5 minutes
- [ ] EAS alerts interrupt automation immediately (< 500ms)
- [ ] Advanced scheduling with conflict detection

### Phase 7 (1.1)
- [ ] WebDJ interface supports live streaming via WebRTC
- [ ] Voice tracks integrated into schedule seamlessly
- [ ] Listener statistics updated in real-time (< 1s latency)

---

## Non-Goals (Per Design Brief)

❌ **No embedded audio scripting language** - Declarative config only
❌ **No per-output encoders duplicating work** - One decode/DSP → multiple encoder forks
❌ **No monolithic single process** - Separate API, planner, executor, media engine
❌ **No Liquidsoap DSL** - Graph-based pipeline with protobuf control

---

## Conclusion

This roadmap aligns Grimnir Radio with the comprehensive design brief for a modern, Go-controlled broadcast automation platform. The phased approach allows incremental progress while maintaining a working system at each stage.

**Current Status (2026-01-22):**
- ✅ Phase 0: Foundation Fixes (100% complete)
- ✅ Phase 4A: Executor & Priority System (100% complete)
- ✅ Phase 4B: Media Engine Separation (100% complete)
- ✅ Phase 4C: Live Input & Webstream Relay (100% complete)
- ⏳ Phase 5: Observability & Multi-Instance (next phase)

**Key Achievements:**
- Complete multi-process architecture with separated control plane and media engine
- 5-tier priority system with state machine
- Token-based live authorization with harbor-style input (Icecast, RTP, SRT)
- Webstream relay with automatic health checks and failover chains
- 13 new REST API endpoints for live and webstream management
- Scheduler integration for webstream playback
- Real-time telemetry streaming and event bus integration

**Remaining Timeline:** ~12-15 weeks (3-4 months) from current state to 1.0 release.

**Next Steps:**
1. Begin Phase 5: Observability & Multi-Instance scaling
2. Continue with Phase 6: Advanced features (EAS, scheduling, migration tools)
3. Complete Phase 7: WebDJ interface and user experience
