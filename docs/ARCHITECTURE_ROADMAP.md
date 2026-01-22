# Grimnir Radio - Architecture Roadmap

**Version:** 0.0.1-alpha
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
| **Scheduler (Planner)** | ✓ Strong | `internal/scheduler` - Deterministic Smart Blocks, 48h rolling schedule |
| **Media Library** | ✓ Complete | `internal/media` - File ingest, tagging, analysis queue |
| **Multi-Station** | ✓ Complete | Isolated stations with separate scheduling |
| **PostgreSQL Store** | ✓ Complete | Via GORM with MySQL/SQLite support |
| **Authentication** | ✓ Complete | JWT with 15-min TTL, 3-tier RBAC |

### ⚠️ Partial Implementations

| Component | Status | Current State | Needed |
|-----------|--------|---------------|--------|
| **Media Engine Control** | ⚠️ Basic | `internal/playout` with basic GStreamer | gRPC interface, graph control, telemetry |
| **Station Controller** | ⚠️ Merged | Combined with scheduler | Separate executor process/goroutine |
| **DSP Pipeline** | ⚠️ Minimal | Basic playback only | Loudness, AGC, compressor, limiter, ducking |
| **Live Failover** | ⚠️ Basic | Authorization + handover events | Priority ladder, automatic failover |
| **Observability** | ⚠️ Stubs | Health checks, metric placeholders | Full Prometheus metrics, distributed tracing |

### ❌ Missing Components

| Component | Status | Priority | Impact |
|-----------|--------|----------|--------|
| **gRPC Media Engine Interface** | Not Started | **High** | Needed for proper control separation |
| **Planner/Executor Split** | Not Started | **High** | Core architectural requirement |
| **Redis/NATS Event Bus** | Not Started | **Medium** | Required for multi-instance |
| **Priority System** | Not Started | **High** | Emergency/live/automation tiers |
| **Recording Sink** | Not Started | **Low** | Compliance/archival feature |
| **Graph-Based DSP** | Not Started | **High** | Professional audio quality |
| **Telemetry Channel** | Not Started | **Medium** | Media engine→Go monitoring |

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

### Phase 4A: Foundation Refactoring (Current → MVP 1)

**Goal:** Align with design brief core principles

**Duration:** 4-6 weeks

**Tasks:**

1. **Split Scheduler → Planner + Executor** (Week 1-2)
   - [ ] Rename `internal/scheduler` → `internal/planner`
   - [ ] Create `internal/executor` package
   - [ ] Planner generates time-ordered event timeline
   - [ ] Executor polls timeline, sends commands to media engine
   - [ ] Per-station executor goroutines with state machines
   - [ ] Update `cmd/grimnirradio/main.go` to start executor pool

2. **Design gRPC Media Engine Interface** (Week 1-2)
   - [ ] Create `proto/mediaengine.proto`
   - [ ] Define service:
     ```protobuf
     service MediaEngine {
       rpc LoadGraph(GraphConfig) returns (GraphHandle);
       rpc Play(PlayRequest) returns (PlayResponse);
       rpc Stop(StopRequest) returns (StopResponse);
       rpc Fade(FadeRequest) returns (FadeResponse);
       rpc InsertEmergency(InsertRequest) returns (InsertResponse);
       rpc RouteLive(RouteRequest) returns (RouteResponse);
       rpc StreamTelemetry(TelemetryRequest) returns (stream Telemetry);
     }
     ```
   - [ ] Generate Go stubs: `protoc --go_out=. --go-grpc_out=. proto/mediaengine.proto`
   - [ ] Create `internal/mediaengine` client package

3. **Implement Priority System** (Week 2-3)
   - [ ] Define priority enum in `internal/models`:
     - `PriorityEmergency = 0` (EAS alerts)
     - `PriorityLiveOverride = 1` (Manual DJ takeover)
     - `PriorityLiveScheduled = 2` (Scheduled live shows)
     - `PriorityAutomation = 3` (Normal playout)
     - `PriorityFallback = 4` (Emergency fallback audio)
   - [ ] Executor state machine honors priority
   - [ ] API endpoints for emergency insert: `POST /api/v1/playout/emergency-insert`
   - [ ] API endpoints for priority override: `POST /api/v1/playout/override`

4. **Add Telemetry Channel** (Week 3-4)
   - [ ] Implement telemetry stream in executor
   - [ ] Parse GStreamer bus messages → structured events
   - [ ] Metrics: `buffer_depth_samples`, `dropout_count`, `cpu_usage_percent`, `loudness_lufs`
   - [ ] Publish to event bus (in-memory for now, Redis/NATS in Phase 4B)
   - [ ] WebSocket clients can subscribe to telemetry events

5. **Update API for New Architecture** (Week 4)
   - [ ] Document executor state machine states in API_REFERENCE.md
   - [ ] Add telemetry event documentation
   - [ ] Update playout control endpoints to use executor
   - [ ] Add priority system endpoints

6. **Testing & Documentation** (Week 5-6)
   - [ ] Unit tests for planner/executor split
   - [ ] Integration tests for priority system
   - [ ] Update all docs to reflect new architecture
   - [ ] Performance benchmarks for scheduler → executor flow

**Deliverables:**
- ✓ Clean planner/executor separation
- ✓ Priority-based playout control
- ✓ Telemetry stream from media engine
- ✓ gRPC interface design (implementation in Phase 4B)

---

### Phase 4B: Media Engine Implementation (MVP 1 → MVP 2)

**Goal:** Replace basic GStreamer with graph-based DSP pipeline

**Duration:** 6-8 weeks

**Tasks:**

1. **Implement gRPC Media Engine Server** (Week 1-3)
   - [ ] Create `cmd/mediaengine` binary (separate process)
   - [ ] Implement gRPC service from Phase 4A proto
   - [ ] GStreamer graph builder from `GraphConfig`
   - [ ] Per-station pipeline management
   - [ ] Bidirectional communication: Go ↔ Media Engine

2. **Build DSP Graph System** (Week 2-4)
   - [ ] Graph nodes:
     - **Decode:** ffmpeg/GStreamer decoders
     - **Loudness:** EBU R128 / ATSC A/85 normalization
     - **AGC:** Automatic Gain Control
     - **Compressor:** Dynamic range compression
     - **Limiter:** True peak limiting
     - **Ducking:** Microphone over music
     - **Silence Detector:** Dead air detection
   - [ ] Node configuration via protobuf messages
   - [ ] Dynamic graph reconfiguration (no restarts)

3. **Multiple Output Isolation** (Week 4-5)
   - [ ] One decode/DSP chain → multiple encoder forks
   - [ ] Outputs: Icecast, HLS, DASH, SRT, Recording
   - [ ] Output failure isolation (one output crash ≠ all crash)
   - [ ] Per-output health monitoring

4. **Live Input Integration** (Week 5-6)
   - [ ] Live inputs: Icecast source, RTP, SRT, WebRTC
   - [ ] Seamless switching: automation → live → automation
   - [ ] Crossfade configuration per priority level
   - [ ] Automatic failback on live source disconnect

5. **Recording Sink** (Week 6-7)
   - [ ] Continuous recording to timestamped files
   - [ ] Rotation policy (keep N days, max size)
   - [ ] Aircheck export API: `GET /api/v1/stations/{id}/recordings?start=...&end=...`
   - [ ] Background worker for recording archival to S3

6. **Integration & Testing** (Week 7-8)
   - [ ] End-to-end: API → Executor → gRPC → Media Engine → Output
   - [ ] Stress test: 10 stations, 3 outputs each, 24-hour run
   - [ ] Failure injection: kill outputs, kill media engine, network failures
   - [ ] Performance profiling

**Deliverables:**
- ✓ gRPC-controlled media engine (separate process)
- ✓ Graph-based DSP pipeline (loudness, AGC, compressor, limiter)
- ✓ Multiple outputs per station with isolation
- ✓ Live input with automatic failover
- ✓ Recording sink for compliance

---

### Phase 4C: Observability & Multi-Instance (MVP 2 → MVP 3)

**Goal:** Production-grade monitoring and horizontal scaling

**Duration:** 4-6 weeks

**Tasks:**

1. **Replace Event Bus with Redis/NATS** (Week 1-2)
   - [ ] Replace `internal/events.Bus` with Redis Pub/Sub or NATS
   - [ ] Config: `GRIMNIR_EVENT_BUS_BACKEND=redis|nats|memory`
   - [ ] Backward compatibility: memory bus for single-node
   - [ ] Event serialization (JSON or protobuf)
   - [ ] Update WebSocket handler to subscribe via Redis/NATS

2. **Complete Prometheus Metrics** (Week 2-3)
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

3. **Add Distributed Tracing** (Week 3-4)
   - [ ] OpenTelemetry integration
   - [ ] Trace IDs propagated: API → Planner → Executor → Media Engine
   - [ ] Spans for key operations:
     - Schedule generation
     - Smart block materialization
     - Media engine command execution
     - API request handling
   - [ ] Jaeger or Tempo backend

4. **Alerting Rules** (Week 4-5)
   - [ ] Prometheus AlertManager integration
   - [ ] Alert rules:
     - `ScheduleGap` - No entries in next 1 hour
     - `PlayoutUnderrun` - Dropout count > threshold
     - `OutputDown` - Output health = 0 for > 60s
     - `MediaEngineDown` - gRPC connection lost
     - `HighCPU` - Media engine CPU > 80% for 5 minutes
   - [ ] Webhook integration for alerts

5. **Multi-Instance Support** (Week 5-6)
   - [ ] Stateless API instances (load-balanced)
   - [ ] Leader election for planner (only one active)
   - [ ] Executor pool distributed across instances
   - [ ] Shared PostgreSQL with connection pooling
   - [ ] Shared Redis/NATS event bus
   - [ ] Shared media storage (S3 or NFS)

6. **Deployment Guides** (Week 6)
   - [ ] Docker Compose: full stack (API + media engine + postgres + redis + icecast)
   - [ ] Kubernetes manifests with multi-replica deployment
   - [ ] Systemd service files for traditional deployment
   - [ ] Production hardening checklist

**Deliverables:**
- ✓ Redis/NATS event bus (multi-instance ready)
- ✓ Complete Prometheus metrics
- ✓ Distributed tracing (OpenTelemetry)
- ✓ AlertManager integration
- ✓ Multi-instance deployment support

---

### Phase 5: Advanced Features (MVP 3 → MVP 4)

**Goal:** Professional broadcast features

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

3. **Webstream Relay with Failover** (Week 4-6)
   - [ ] Schedule external HTTP/ICY streams in clocks
   - [ ] Health probing: periodic HEAD requests
   - [ ] Fallback URL chains: primary → backup → local
   - [ ] Preflight connection before slot start
   - [ ] Grace window failover (auto-switch if primary fails)
   - [ ] Metadata passthrough (ICY StreamTitle)

4. **Migration Tools** (Week 6-8)
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

**Deliverables:**
- ✓ EAS compliance features
- ✓ Advanced scheduling tools
- ✓ Webstream relay with automatic failover
- ✓ AzuraCast/LibreTime migration tools

---

### Phase 6: WebDJ & User Experience (MVP 4 → 1.0)

**Goal:** Complete broadcast suite

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

### Phase 4A (Foundation)
- [ ] Planner generates timeline in < 500ms for 48-hour window
- [ ] Executor transitions between tracks in < 100ms
- [ ] Priority system tested with emergency inserts
- [ ] Telemetry stream delivers metrics every 1 second

### Phase 4B (Media Engine)
- [ ] 10 stations × 3 outputs = 30 concurrent outputs for 24 hours
- [ ] Zero dropouts in nominal conditions
- [ ] Output failure isolated (1 output crash doesn't affect others)
- [ ] Live input failover completes in < 3 seconds

### Phase 4C (Observability)
- [ ] All metrics exported to Prometheus
- [ ] Distributed traces show end-to-end request flow
- [ ] Alerts fire correctly for test failure scenarios
- [ ] Multi-instance deployment scales to 3 API replicas

### Phase 5 (Advanced)
- [ ] AzuraCast migration imports 1000-track library in < 5 minutes
- [ ] Webstream failover completes within grace window (< 5s)
- [ ] EAS alerts interrupt automation immediately (< 500ms)

### Phase 6 (1.0)
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

**Key Takeaway:** Grimnir Radio has strong foundations (API, auth, scheduling). The next phases focus on proper control plane separation, professional audio quality, and production-grade reliability.

**Estimated Timeline:** 24-30 weeks (6-8 months) from current state to 1.0 release.

**Next Steps:**
1. Review and approve this roadmap
2. Begin Phase 4A: Foundation Refactoring
3. Set up project tracking (GitHub Projects, milestones)
4. Establish weekly progress reviews
