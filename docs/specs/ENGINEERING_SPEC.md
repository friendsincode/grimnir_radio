# Grimnir Radio — Software Engineering Spec

**Version:** 1.3.1
**Architecture:** Go-Based Broadcast Automation Platform (Liquidsoap Replacement)

This document describes the architecture, design decisions, and technical specifications for Grimnir Radio, a modern broadcast automation system that combines the strengths of AzuraCast and LibreTime while replacing Liquidsoap with a more reliable, observable, and controllable media pipeline.

## Production Deployment

Grimnir Radio powers **[rlmradio.xyz](https://rlmradio.xyz)**, demonstrating production-grade reliability for community radio broadcasting.

---

## Design Principles

### Core Principle
**Go owns the control plane. A dedicated media engine owns real-time audio.**

### Key Tenets
1. **No audio scripting DSL** - Declarative configuration, not embedded logic
2. **No monolithic process** - Separate concerns, isolated failure domains
3. **Planner/Executor separation** - Timeline generation separate from execution
4. **Observable and controllable** - All actions via API, real-time telemetry
5. **Deterministic scheduling** - Same inputs → same outputs
6. **Suitable for 24/7 unattended operation**

---

## Context & Goals

### ✓ IMPLEMENTED Goals (All Complete)
- Deliver a reliable, deterministic radio automation control plane
- Support multiple databases (PostgreSQL, MySQL, SQLite)
- API key authentication with RBAC (web dashboard uses session cookies)
- Event-driven architecture for inter-service communication
- Multi-station/multi-mount architecture
- API-first design (REST + WebSocket + gRPC)
- GStreamer-based media engine with graph-based DSP pipeline
- Sample-accurate timing with professional DSP (loudness normalization, AGC, compression)
- Multiple outputs per station with isolated failure domains
- Live broadcasting with 5-tier priority-based source management
- Production-grade observability (Prometheus metrics, OpenTelemetry tracing, alerting)
- Webstream relay with automatic failover
- Migration tools for AzuraCast and LibreTime
- Horizontal scaling with consistent hashing and leader election
- Turn-key deployment (Docker Compose, Kubernetes, Nix)
- Audit logging for sensitive operations

---

## High-Level Architecture

```
┌──────────────────────────────────────────────────────────────┐
│                    API Gateway (Go)                          │
│         :8080 REST + :9090 gRPC + WebSocket + SSE            │
│           API Key Auth + RBAC + Rate Limiting                │
└───────┬──────────────────────────────────┬───────────────────┘
        │                                  │
   ┌────▼─────────────┐            ┌──────▼────────────────┐
   │   Planner        │            │  Media Library        │
   │  (Scheduler)     │            │   Service             │
   │                  │            │  - LUFS Analysis      │
   │ - Smart Blocks   │            │  - Rotation Rules     │
   │ - Clock Compile  │            │  - Artist Separation  │
   │ - Timeline Gen   │            │  - Metadata Index     │
   └────┬─────────────┘            └───────────────────────┘
        │
        │ Schedule Timeline (time-ordered events)
        │
   ┌────▼──────────────────────────────────────────────────┐
   │         Station Executor Pool (Go)                    │
   │  [Executor-1] [Executor-2] ... [Executor-N]           │
   │  One per station │ State Machine │ Priority Logic     │
   └────┬──────────────────────────────────────┬───────────┘
        │                                      │
        │ gRPC Control Channel          Telemetry Stream
        │ (LoadGraph, Play, Stop, Fade)       │
        │                                      │
   ┌────▼──────────────────────────────────────▼───────────┐
   │       Media Engine (GStreamer per station)            │
   │                                                        │
   │  [Input] → [Decode] → [DSP Graph] → [Encode] → [Out] │
   │              ↓           ↓             ↓         ↓    │
   │           Files      Loudness        MP3    Icecast-1 │
   │           Live       AGC/Comp        AAC    Icecast-2 │
   │           WebRTC     Limiter         Opus   HLS       │
   │                      Ducking                Recording  │
   │                                                        │
   │  Telemetry: buffer_depth, dropouts, cpu, loudness     │
   └────────────────────────────────────────────────────────┘

┌────────────────────────────────────────────────────────────┐
│         Realtime Event Bus (Redis/NATS)                   │
│  Events: now_playing, source_failure, buffer_health        │
└────────────────────────────────────────────────────────────┘
```

---

## Process Architecture

### Design Decision: Multiple Processes, Not Monolith

**Why?**
- Scheduler crash ≠ audio stop
- UI crash ≠ broadcast stop
- Output failure ≠ playout failure
- Easier to scale horizontally
- Clearer observability boundaries

**Processes:**

1. **API Gateway** (Go) - Port 8080/9090
   - Single entry point for all API traffic
   - Authentication and authorization
   - Request routing
   - WebSocket/SSE connections

2. **Planner** (Go) - Background service
   - Generates time-ordered schedule timelines
   - Runs periodically (every 5 minutes or on-demand)
   - Deterministic Smart Block materialization
   - Clock compilation
   - Conflict detection

3. **Executor Pool** (Go) - One goroutine per station
   - Polls planner for upcoming events
   - Executes timeline at precise times
   - State machine per station
   - Sends gRPC commands to media engine
   - Monitors telemetry from media engine

4. **Media Engine** (GStreamer) - One process per station
   - Graph-based audio pipeline
   - gRPC server for control commands
   - Telemetry stream back to executor
   - Multiple outputs per instance
   - Isolated failure domains

5. **Background Workers** (Go) - Job queue processors
   - Media analysis (LUFS, cue points, waveform)
   - Transcoding
   - Recording export
   - Backup tasks

---

## Modules & Responsibilities

### ✓ IMPLEMENTED Modules

**`cmd/grimnirradio`**
- Main binary entry point
- Bootstraps all services
- Graceful shutdown orchestration

**`internal/api`**
- REST endpoints (stations, media, smart blocks, clocks, schedule, playout, analytics)
- WebSocket event streaming
- API key middleware
- RBAC enforcement
- Request/response validation

**`internal/scheduler`** (will be renamed → `internal/planner`)
- Smart Block rule evaluation
- Clock template compilation
- 48-hour rolling schedule generation
- Deterministic materialization (seeded random)
- Quota enforcement, separation windows

**`internal/models`**
- GORM data models for all entities
- JSON field support for flexible metadata
- Relationship definitions

**`internal/db`**
- Connection management via GORM
- Automatic migrations on startup
- Multi-backend support (PostgreSQL, MySQL, SQLite)

**`internal/auth`**
- API key generation and validation (SHA256 hashed)
- Claims structure (user_id, roles, station_id)
- RBAC middleware

**`internal/events`**
- Pub/sub event bus abstraction
- Event types: now_playing, health, schedule.update, dj.connect, audit events
- Supports in-memory, Redis Pub/Sub, and NATS backends

**`internal/media`**
- File storage abstraction (filesystem or S3)
- Upload handling (multipart)
- Path generation per station/media ID

**`internal/analyzer`**
- Job queue for media analysis
- Loudness analysis (LUFS, ReplayGain)
- Cue point detection (intro/outro)
- BPM extraction
- Waveform extraction

**`internal/live`**
- Live source authorization
- Handover triggering via events

**`internal/playout`**
- Director for playback management
- Coordinates with executor and media engine
- Handles track transitions and crossfades

**`internal/config`**
- Environment variable loading
- Validation and defaults
- Dual naming support (GRIMNIR_* and RLM_*)

**`internal/logging`**
- Zerolog configuration
- Development vs production formatting
- Request ID propagation

**`internal/telemetry`**
- Health check endpoints
- Metrics placeholders

**`internal/storage`**
- Object storage abstraction (S3-compatible or filesystem)

### ✓ IMPLEMENTED Modules (Post-1.0)

**`internal/executor`**
- Per-station execution goroutines
- State machine: Idle → Loading → Playing → Fading → Live → Error → Stopped
- Priority-based source management
- gRPC client to media engine
- Telemetry monitoring and failover logic

**`internal/mediaengine`**
- gRPC client for media engine control
- Command wrappers: LoadGraph, Play, Stop, Fade, InsertEmergency, RouteLive
- Telemetry stream consumer

**`internal/priority`**
- Priority tier definitions and logic
- Source priority: Emergency (0) → Live Override (1) → Live Scheduled (2) → Automation (3) → Fallback (4)
- Conflict resolution

**`internal/mediaengine/dsp`**
- DSP graph configuration builder
- Node definitions: loudness, AGC, compressor, limiter, ducking, silence detector
- GStreamer pipeline templates

**`internal/eventbus`**
- Redis Pub/Sub and NATS adapters
- Multi-instance support with leader election
- Event serialization (JSON)

**`cmd/mediaengine`**
- Separate media engine binary
- gRPC server implementation
- GStreamer graph builder and manager
- Per-station pipeline isolation
- Telemetry publisher

---

## Data Model

### ✓ IMPLEMENTED Entities

All entities currently exist in `internal/models/models.go`:

- **users**: Authentication accounts (id, email, password_hash, role)
- **stations**: Station definitions with timezone support
- **mounts**: Output streams with encoder configuration
- **encoder_presets**: GStreamer encoder templates
- **media_items**: Audio files with metadata, analysis results, cue points
- **tags**: Metadata labels for categorization
- **media_tag_links**: Many-to-many media↔tags
- **smart_blocks**: Rule definitions with filters, quotas, separation
- **clock_hours**: Hour templates with slots
- **clock_slots**: Clock elements (smart_block, hard_item, stopset)
- **schedule_entries**: Materialized schedule with time windows
- **play_history**: Played tracks for analytics and separation rules
- **analysis_jobs**: Analyzer work queue

### ✓ IMPLEMENTED Entities (Post-1.0)

**`executor_states`** - Executor runtime state (`internal/models/priority.go`)
```go
type ExecutorState struct {
    ID            string    `gorm:"type:uuid;primaryKey"`
    StationID     string    `gorm:"type:uuid;index"`
    State         StateEnum `gorm:"type:varchar(32)"` // idle, preloading, playing, fading, live, emergency
    CurrentPriority int     // 0-4
    CurrentSourceID string   `gorm:"type:uuid"`
    CurrentSourceType string `gorm:"type:varchar(32)"` // media, live, webstream, fallback
    BufferDepth   int      // samples
    LastHeartbeat time.Time
    Metadata      map[string]any `gorm:"type:jsonb"`
}
```

**`priority_sources`** - Active sources with priority (`internal/models/priority.go`)
```go
type PrioritySource struct {
    ID         string `gorm:"type:uuid;primaryKey"`
    StationID  string `gorm:"type:uuid;index"`
    MountID    string `gorm:"type:uuid;index"`
    Priority   int    // 0=emergency, 1=live_override, 2=live_scheduled, 3=automation, 4=fallback
    SourceType string `gorm:"type:varchar(32)"`
    SourceID   string `gorm:"type:uuid"`
    StartsAt   time.Time
    EndsAt     *time.Time // nil = indefinite
    Active     bool
    Metadata   map[string]any `gorm:"type:jsonb"`
}
```

**`webstreams`** - External stream definitions (`internal/models/webstream.go`)
```go
type Webstream struct {
    ID            string         `gorm:"type:uuid;primaryKey"`
    StationID     string         `gorm:"type:uuid;index"`
    Name          string         `gorm:"index"`
    URL           string         // primary URL
    FallbackURLs  []string       `gorm:"type:jsonb"` // backup URLs
    Headers       map[string]string `gorm:"type:jsonb"`
    HealthState   string         `gorm:"type:varchar(32)"` // healthy, degraded, down
    LastHealthCheck time.Time
    Metadata      map[string]any `gorm:"type:jsonb"`
}
```

**`dsp_graphs`** - DSP configurations (`internal/mediaengine/dsp/graph.go` - runtime only, not persisted)
```go
type DSPGraph struct {
    ID          string         `gorm:"type:uuid;primaryKey"`
    StationID   string         `gorm:"type:uuid;index"`
    Name        string         `gorm:"index"`
    Description string         `gorm:"type:text"`
    Nodes       []DSPNode      `gorm:"type:jsonb"` // ordered list of DSP nodes
    CreatedAt   time.Time
    UpdatedAt   time.Time
}

type DSPNode struct {
    Type   string         `json:"type"`   // loudness, agc, compressor, limiter, ducking, silence
    Config map[string]any `json:"config"` // node-specific parameters
}
```

**`migration_jobs`** - Import job tracking (`internal/migration/types.go`)
```go
type MigrationJob struct {
    ID         string `gorm:"type:uuid;primaryKey"`
    Source     string `gorm:"type:varchar(32)"` // azuracast, libretime
    Status     string `gorm:"type:varchar(32)"` // pending, running, complete, failed
    Progress   int    // 0-100
    ItemsTotal int
    ItemsDone  int
    ItemsFailed int
    Errors     []string `gorm:"type:jsonb"`
    CreatedAt  time.Time
    UpdatedAt  time.Time
}
```

---

## API Surface (v1)

### ✓ IMPLEMENTED Endpoints

All currently implemented endpoints remain as-is. See `docs/API_REFERENCE.md` for full documentation.

**Summary:**
- Auth: API key via X-API-Key header (keys managed in profile page)
- Stations: list, create, get
- Mounts: list, create
- Media: upload, get
- Smart Blocks: list, create, materialize
- Clocks: list, create, simulate
- Schedule: list, refresh, update entry
- Live: authorize, handover
- Playout: reload, skip, stop
- Analytics: now-playing, spins
- Webhooks: track-start
- Events: WebSocket stream
- Health: health checks

### ✓ IMPLEMENTED Endpoints (Post-1.0)

**Priority Management:** (`internal/api/priority.go`)
```
POST   /api/v1/priority/emergency                # Emergency takeover (priority 0)
POST   /api/v1/priority/override                 # Manual override (priority 1)
DELETE /api/v1/priority/{sourceID}               # Release priority source
GET    /api/v1/priority/current                  # Get current priority
GET    /api/v1/priority/active                   # List active sources
```

**Executor State:** (`internal/api/executor.go`)
```
GET    /api/v1/executor/states                   # List all executor states
GET    /api/v1/executor/states/{stationID}       # Get executor state for station
GET    /api/v1/executor/telemetry/{stationID}    # Get real-time telemetry
GET    /api/v1/executor/health                   # Health check for all executors
```

**Webstreams:** (`internal/api/api.go`)
```
GET    /api/v1/webstreams                        # List webstreams
POST   /api/v1/webstreams                        # Create webstream
GET    /api/v1/webstreams/{id}                   # Get webstream
PUT    /api/v1/webstreams/{id}                   # Update webstream
DELETE /api/v1/webstreams/{id}                   # Delete webstream
POST   /api/v1/webstreams/{id}/failover          # Trigger failover
POST   /api/v1/webstreams/{id}/reset             # Reset to primary
```

**Migrations:** (`internal/api/migration.go`)
```
POST   /api/v1/migrations                        # Create migration job
POST   /api/v1/migrations/{jobID}/start          # Start migration job
GET    /api/v1/migrations/{jobID}                # Get migration status
GET    /api/v1/migrations                        # List all migrations
```

### ⏳ PLANNED Endpoints

**DSP Graph Management API:** (graphs exist internally but no HTTP API yet)
```
GET    /api/v1/dsp-graphs                        # List DSP graphs
POST   /api/v1/dsp-graphs                        # Create DSP graph
GET    /api/v1/dsp-graphs/{graphID}              # Get DSP graph
PATCH  /api/v1/dsp-graphs/{graphID}              # Update DSP graph
DELETE /api/v1/dsp-graphs/{graphID}              # Delete DSP graph
POST   /api/v1/dsp-graphs/{graphID}/apply        # Apply graph to station
```

**Media Engine Control (internal gRPC, not HTTP):**
```
rpc LoadGraph(GraphConfig) returns (GraphHandle)
rpc Play(PlayRequest) returns (PlayResponse)
rpc Stop(StopRequest) returns (StopResponse)
rpc Fade(FadeRequest) returns (FadeResponse)
rpc InsertEmergency(InsertRequest) returns (InsertResponse)
rpc RouteLive(RouteRequest) returns (RouteResponse)
rpc StreamTelemetry(TelemetryRequest) returns (stream Telemetry)
```

---

## Scheduling Model

### Key Properties
- **Deterministic**: Same inputs → same outputs
- **Event-sourced**: State machine with recorded transitions
- **Planner/Executor separation**: Timeline generation ≠ execution
- **Offline simulation**: "What will play at time T?" without affecting live system

### Scheduling Entities

**Station** - Top-level broadcast entity
- Timezone-aware
- Independent schedule timeline
- Isolated failure domain

**Show** - Scheduled program block
- Fixed time slot
- Can contain tracks, webstreams, live sources
- Priority: live_scheduled (2)

**Rotation** - Smart Block automation
- Rule-based track selection
- Separation windows, quotas
- Priority: automation (3)

**Track** - Individual media item
- File-based audio
- Analyzed metadata (LUFS, cue points, BPM)

**Live Source** - External input
- Icecast/Shoutcast mount, RTP, SRT, WebRTC
- Authorization required
- Priority: live_scheduled (2) or live_override (1)

**Override** - Manual intervention
- Emergency insert
- DJ takeover
- Priority: emergency (0) or live_override (1)

**Webstream** - External HTTP/ICY stream
- Health monitoring
- Fallback URL chains
- Scheduled in clocks or as live sources

### Timeline Generation

**Planner Process:**
1. Load clock templates for station
2. Compile hour-by-hour structure
3. Materialize Smart Blocks (deterministic seeded generation)
4. Insert scheduled shows/overrides
5. Validate: no gaps, no overlaps, separation rules honored
6. Persist timeline to `schedule_entries` table
7. Publish `schedule.refresh` event

**Executor Process:**
1. Poll timeline for upcoming events (T-60s lookahead)
2. At T-30s: preload next item (download, decode, analyze)
3. At T-5s: check priority (is there a higher-priority source?)
4. At T: execute transition (fade out current, fade in next)
5. Monitor telemetry (buffer health, dropouts)
6. On failure: escalate to next priority level

---

## Media Engine Requirements

### Design Goal
**Replace Liquidsoap with a more reliable, observable, and controllable media pipeline.**

### Core Responsibilities
1. **Decode** → **DSP** → **Mix** → **Encode** → **Output**
2. Sample-accurate timing
3. Multiple outputs from one playout
4. Live input ingest
5. Graph-based pipeline (not scripting)

### Recommended Implementation: GStreamer

**Why GStreamer?**
- Battle-tested (VLC, Pitivi, many pro tools use it)
- Dynamic graph reconfiguration (no restarts)
- Extensive plugin ecosystem
- C library with Go bindings available
- LGPL license

**Alternative: FFmpeg**
- Simpler but less flexible
- Filter graph syntax more rigid
- Better for transcoding, less ideal for live mixing

**Alternative: Custom Rust/C++ Engine**
- Maximum control
- Significant development effort
- Consider for later optimization phase

### Audio Pipeline

**Graph Structure:**

```
[Input Sources] ──┬──> [Decoder] ──> [Resampler] ──> [DSP Chain] ──> [Encoder Fork] ──┬──> [Output 1: Icecast]
                  │                                                                    ├──> [Output 2: HLS]
                  │                                                                    ├──> [Output 3: Recording]
                  │                                                                    └──> [Output N: ...]
                  └──> [Live Input Mixer]
```

**DSP Chain Nodes:**

1. **Loudness Normalization** (EBU R128 / ATSC A/85)
   - Target LUFS: -16.0 (configurable)
   - True peak limit: -1.0 dBTP
   - Integrated loudness measurement

2. **AGC (Automatic Gain Control)**
   - Maintains consistent signal level
   - Target: -14.0 dB (configurable)
   - Attack/release times

3. **Compressor**
   - Dynamic range compression
   - Threshold: -20.0 dB
   - Ratio: 3:1 (configurable)
   - Attack: 5ms, Release: 50ms

4. **Limiter**
   - Brick-wall limiting
   - Threshold: -1.0 dB
   - Prevents clipping
   - Look-ahead: 5ms

5. **Ducking**
   - Microphone over music
   - Sidechain input from live mic
   - Configurable duck amount (-12 dB typical)

6. **Silence Detection**
   - Dead air detection
   - Threshold: -40 dB for 5 seconds
   - Triggers failover to fallback audio

### Input Sources

**File Inputs:**
- MP3, AAC, FLAC, OGG, WAV
- Seek support for cue points
- Gapless playback

**Live Inputs:**
- Icecast/Shoutcast source client
- RTP (RFC 3550)
- SRT (Secure Reliable Transport)
- WebRTC (browser-based DJ)
- ALSA/JACK/PipeWire (local hardware)

### Output Formats

**Streaming:**
- Icecast (MP3, AAC, Opus)
- HLS (HTTP Live Streaming)
- DASH (Dynamic Adaptive Streaming over HTTP)
- SRT push

**Recording:**
- Continuous recording to timestamped files
- Rotation policy (keep N days, max size)
- Format: MP3 or FLAC (configurable)

### Output Isolation

**Critical Requirement:** One output failure ≠ all outputs fail

**Implementation:**
- Each output is a separate GStreamer branch
- Branch failure handled gracefully
- Telemetry reports per-output health
- Main pipeline continues if one output drops

---

## Control Plane Requirements

### All Actions Controllable via API

**No CLI glue scripts. No manual GStreamer command editing.**

All media engine control happens through:
1. HTTP API (user-facing)
2. gRPC API (executor → media engine)

### Real-Time Event Stream

**Event Types:**
- `now_playing` - Track metadata, started_at, duration
- `source_failure` - Live source disconnected, webstream down
- `buffer_health` - Buffer depth in samples, dropout count
- `loudness_metrics` - Current LUFS, integrated LUFS, true peak
- `silence_detected` - Dead air alert
- `output_health` - Per-output status (up, degraded, down)
- `priority_change` - Priority source activated/deactivated

**Transport:**
- WebSocket (for browsers)
- SSE (Server-Sent Events, for simple clients)
- Redis Pub/Sub (for Go services)

### Graceful Reloads

**No Restarts Required For:**
- Schedule changes
- Smart Block rule updates
- Clock modifications
- DSP graph changes (graceful crossfade to new graph)
- Configuration updates (except network binds)

**Restart Required For:**
- Binary updates
- Database connection changes
- Major GStreamer pipeline architecture changes

---

## Priority System

### 5-Tier Priority Ladder

```
Priority 0: Emergency         [INTERRUPTS EVERYTHING]
  ↓         EAS alerts, system failure fallback
Priority 1: Live Override     [PREEMPTS SCHEDULED]
  ↓         Manual DJ takeover, manager override
Priority 2: Live Scheduled    [REPLACES AUTOMATION]
  ↓         Booked live shows
Priority 3: Automation        [NORMAL OPERATION]
  ↓         Smart Blocks, scheduled tracks
Priority 4: Fallback          [DEAD AIR PREVENTION]
              Emergency audio loop
```

### Priority Rules

1. **Lower number = higher priority**
2. **Active priority source preempts all higher numbers**
3. **Equal priority → first in time wins**
4. **Priority 0 interrupts immediately (< 500ms)**
5. **Priority 1-2 fade out current (configurable fade time)**
6. **Priority 3-4 wait for natural transition**

### State Transitions

```
Current State: Automation (Priority 3)
Playing: Track 4 of 10 from Smart Block

Event: Emergency Alert (Priority 0)
  ↓
Executor: Immediate stop
  ↓
Send: InsertEmergency(audio: "/alerts/eas.mp3")
  ↓
Media Engine: Stop current, play alert, telemetry: {priority: 0}
  ↓
Alert finishes
  ↓
Resume: Automation (Priority 3) at Track 5
```

### API Examples

**Emergency Takeover:**
```bash
POST /api/v1/priority/emergency
{
  "station_id": "uuid",
  "mount_id": "uuid",
  "audio_file": "/alerts/eas.mp3",
  "duration_seconds": 30
}
```

**Live Override:**
```bash
POST /api/v1/priority/override
{
  "station_id": "uuid",
  "mount_id": "uuid",
  "priority": 1,
  "source_type": "live_icecast",
  "source_url": "icecast://dj:password@localhost:8000/live"
}
```

---

## Observability & Reliability

### Metrics (Prometheus)

**Planner Metrics:**
```
grimnir_planner_build_duration_seconds{station_id}
grimnir_planner_entries_generated_total{station_id}
grimnir_planner_smart_block_materialize_duration_seconds{block_id}
grimnir_planner_separation_violations_total{station_id}
```

**Executor Metrics:**
```
grimnir_executor_state{station_id, state}              # Gauge: current state
grimnir_executor_transition_duration_seconds{station_id}
grimnir_executor_priority_changes_total{station_id, from_priority, to_priority}
```

**Media Engine Metrics:**
```
grimnir_media_buffer_depth_samples{station_id}
grimnir_media_dropouts_total{station_id}
grimnir_media_cpu_usage_percent{station_id}
grimnir_media_loudness_lufs{station_id}                # Current integrated LUFS
grimnir_media_true_peak_dbtp{station_id}
grimnir_media_output_health{station_id, output_id}     # 0=down, 1=up
```

**API Metrics:**
```
grimnir_api_request_duration_seconds{endpoint, method}
grimnir_api_requests_total{endpoint, method, status}
grimnir_api_active_websockets{event_type}
```

### Logging

**Structured Logging (Zerolog):**
- JSON format in production
- Console format in development
- Fields: `timestamp`, `level`, `message`, `station_id`, `request_id`, `user_id`, `component`

**Log Levels:**
- `debug`: Detailed execution flow
- `info`: Normal operations (schedule refresh, track start, live connect)
- `warn`: Recoverable issues (webstream down, buffer underrun)
- `error`: Failures requiring attention (executor crash, database error)

**Correlation:**
- `request_id`: Traces HTTP request → planner → executor → media engine
- `station_id`: Isolates logs per station
- `span_id` / `trace_id`: Distributed tracing (OpenTelemetry)

### Failure Domains

**Critical Principle:** Failures must be isolated

| Component Fails | What Stops | What Continues |
|-----------------|------------|----------------|
| API Gateway     | HTTP requests | Playout, executor, media engine |
| Planner         | Schedule updates | Current playout, executor follows existing timeline |
| Executor (one station) | That station's playout | All other stations |
| Media Engine (one station) | That station's outputs | All other stations |
| Output (one stream) | That stream | All other outputs on same station |
| Database        | Writes (reads from cache) | Playout from memory timeline |
| Redis/NATS      | New event subscriptions | Existing subscriptions, playout |

**Recovery Strategies:**
- **Planner crash**: Executor continues with last-known timeline until planner restarts
- **Executor crash**: Media engine continues current track; executor restarts and resumes
- **Media Engine crash**: Executor detects via gRPC failure, restarts media engine, resumes from last known position
- **Output failure**: Executor logs error, continues with other outputs
- **Database failure**: Executor/planner use last-known state from memory; retry connection with exponential backoff

---

## Configuration

### Declarative Configuration (YAML)

**Design Principle:** No embedded logic or scripting DSL. Configuration is data, not code.

**Example: Station Configuration**

```yaml
# /etc/grimnirradio/stations/wgmr.yaml
station:
  id: "uuid"
  name: "WGMR FM"
  timezone: "America/New_York"

mounts:
  - id: "mount-main"
    name: "Main Stream 128k MP3"
    outputs:
      - type: icecast
        url: "icecast://source:password@localhost:8000/stream"
        format: mp3
        bitrate_kbps: 128
        channels: 2
        sample_rate: 44100

      - type: hls
        path: "/var/www/hls/wgmr"
        segment_duration_sec: 4
        playlist_size: 6

      - type: recording
        path: "/var/recordings/wgmr"
        format: mp3
        bitrate_kbps: 192
        rotation_days: 30
        max_size_gb: 100

dsp_graph:
  nodes:
    - type: loudness_normalize
      config:
        target_lufs: -16.0
        true_peak_limit_dbtp: -1.0
        integrated_window_sec: 3.0

    - type: agc
      config:
        enabled: true
        target_level_db: -14.0
        max_gain_db: 12.0
        attack_ms: 10
        release_ms: 100

    - type: compressor
      config:
        threshold_db: -20.0
        ratio: 3.0
        knee_db: 6.0
        attack_ms: 5
        release_ms: 50
        makeup_gain_db: 2.0

    - type: limiter
      config:
        threshold_db: -1.0
        lookahead_ms: 5
        release_ms: 10

    - type: silence_detector
      config:
        threshold_db: -40.0
        duration_sec: 5.0
        action: failover_to_fallback

failover:
  live_timeout_sec: 30
  automation_fallback_enabled: true
  fallback_audio_path: "/media/fallback/emergency.mp3"
  fallback_audio_loop: true

priorities:
  emergency: 0
  live_override: 1
  live_scheduled: 2
  automation: 3
  fallback: 4
```

**Configuration Loading:**
- Environment variables override config files
- Config files validated on load (schema validation)
- Invalid config prevents startup (fail-fast)
- Config changes trigger graceful reload (no downtime)

---

## Data Stores

### PostgreSQL (Primary)
**What:** Persistent, transactional data
- Users, stations, mounts, media, smart blocks, clocks, schedules, play history
- Connection pooling via GORM
- Migrations via embedded SQL files

### MySQL (Supported)
**What:** Alternative to PostgreSQL
- All same data as PostgreSQL
- Testing via CI matrix

### SQLite (Development/Single-Node)
**What:** Embedded database for simplicity
- Same schema as PostgreSQL/MySQL
- Foreign keys enabled
- Not recommended for production multi-station

### Redis (Multi-Instance)
**What:** Realtime event bus, ephemeral state
- Pub/Sub for events (now_playing, source_failure, buffer_health, etc.)
- Executor state cache (executor_states table mirrored)
- WebSocket subscription management
- TTL-based cleanup

### NATS (Alternative to Redis)
**What:** Realtime event bus, more scalable
- Subject-based routing
- JetStream for persistence (optional)
- Lower latency than Redis for high-throughput

### Object Storage (S3-Compatible)
**What:** Media files, recordings, backups
- S3, MinIO, Wasabi, Backblaze B2
- Fallback to local filesystem
- Path structure: `s3://bucket/stations/{station_id}/media/{media_id}.mp3`

---

## Security

### ✓ IMPLEMENTED

- API key authentication (SHA256 hashed, configurable expiration up to 1 year)
- Web dashboard session cookies (JWT-based)
- RBAC with three roles: admin, manager, dj
- Route-level middleware for auth and role enforcement
- Bcrypt password hashing (cost 10)
- SQL injection protection via GORM parameterization
- Request context for auth claims propagation
- Audit logging for sensitive operations (priority, live sessions, API keys, webstreams, schedule changes)
- gRPC control interface between control plane and media engine
- No direct HTTP access to media engine (internal only)

### ⏳ PLANNED

**Enhanced Security:**
- Optional OIDC/OAuth2 integration for SSO
- Rate limiting on public endpoints (per IP, per user)
- IP allowlisting for admin endpoints
- TLS for gRPC media engine connections (mutual authentication)

---

## Performance Targets

### ⏳ PLANNED (Not Yet Benchmarked)

**Planner:**
- Schedule build: < 500ms for 1-hour window (warm cache)
- Smart block materialization: < 200ms for 15-minute block
- Clock compilation: < 100ms per clock

**Executor:**
- State transition: < 100ms (fade commands excluded)
- Priority override: < 500ms (emergency), < 2s (live)
- Telemetry processing: < 10ms per event

**Media Engine:**
- Buffer depth: 2-5 seconds audio (configurable)
- Dropout rate: 0 in nominal conditions
- CPU usage: < 20% per station (typical hardware)
- Latency (file → output): < 100ms

**API:**
- Request p95 latency: < 100ms (excluding long-running operations)
- WebSocket event delivery: < 10ms
- Concurrent WebSocket clients: 100+ per instance

**System:**
- Support 10+ stations per instance (typical VPS)
- 3 outputs per station = 30 concurrent outputs
- 24/7 operation without restarts

---

## Acceptance Criteria

### ✓ IMPLEMENTED (All Complete)

**Core:**
- Deterministic Smart Block materialization given identical seed and inputs
- 48h rolling schedule persists and reconciles on media changes
- API key authentication with role-based access control
- Multi-database support (PostgreSQL, MySQL, SQLite)
- Event bus pub/sub with WebSocket streaming
- Media upload with analysis job queuing
- Schedule refresh and manual entry updates
- API health checks functional

**Planner/Executor Split:**
- Planner generates timeline independently of executor
- Executor polls timeline and executes at precise times
- Executor continues with last timeline if planner crashes

**Priority System:**
- Emergency (0) interrupts immediately (< 500ms)
- Live override (1) preempts automation with fade
- Priorities enforced correctly under all scenarios
- State machine tested with all priority transitions

**Media Engine:**
- gRPC control interface functional
- Graph-based DSP pipeline configurable via protobuf
- Multiple outputs isolated (one failure ≠ all fail)
- Telemetry stream delivers metrics

**Audio Quality:**
- Loudness normalization enforced (EBU R128/ATSC A/85)
- Crossfades smooth (no clicks/pops)
- Live input failover < 3 seconds

**Observability:**
- All metrics exported to Prometheus
- Distributed traces show end-to-end flow (OpenTelemetry)
- Alerts configured for critical scenarios

**Multi-Instance:**
- Stateless API instances scale horizontally
- Redis/NATS event bus for inter-instance communication
- Leader election via consistent hashing (CRC32, 500 virtual nodes)

### ⏳ PLANNED

**Customizable Landing Page:**
- Landing page editor in admin dashboard
- Custom branding (logo, colors, background images)
- Configurable widget layout (now playing, schedule, recent tracks)
- Template system for station operators

**WebDJ:**
- Browser-based live streaming
- Voice tracking support

**EAS Integration:**
- Emergency Alert System message parsing
- Automatic broadcast interruption

---

## Test Strategy

### ✓ IMPLEMENTED

- Unit tests: rule evaluation, scheduler slotting, API validation
- Race detector enabled in tests
- Test coverage tracking via `go test -cover`

### ⏳ PLANNED

**Unit Tests:**
- Planner: clock compilation, Smart Block materialization, conflict detection
- Executor: state machine transitions, priority logic
- Media Engine client: gRPC command serialization
- Priority: source comparison, conflict resolution

**Integration Tests:**
- End-to-end: API → Planner → Executor → Media Engine (mock) → Output
- Database: test all queries with PostgreSQL/MySQL/SQLite
- Event bus: Redis/NATS pub/sub under load
- Auth: API key generation, validation, expiry, revocation

**Stress Tests:**
- 10 stations, 3 outputs each, 24-hour continuous run
- Schedule updates during playout
- Priority overrides during playback
- Output failures and recovery
- Media engine restarts

**Chaos Engineering:**
- Kill executor (random station) - expect: station resumes, others unaffected
- Kill media engine (random station) - expect: executor restarts media engine
- Kill database - expect: system continues with cached timeline
- Network partition - expect: executors operate independently, reconcile on recovery
- Slow disk I/O - expect: buffering handles delays, no dropouts

**Performance Benchmarks:**
- Go benchmarks for critical paths (schedule build, smart block generation)
- Media engine: measure CPU per station, buffer depth stability
- API: load test with concurrent requests (100+ RPS)

---

## Release Plan

### ✓ COMPLETE - All Phases Through 1.0

**Phase 0: Foundation Fixes** ✓
- Core Go control plane setup
- DB manager with migrations
- Basic API endpoints
- Logging/metrics infrastructure
- Authentication and RBAC

**Phase 4A: Executor & Priority System** ✓
- Split scheduler → planner + executor
- gRPC media engine interface design
- Priority system (5-tier ladder)
- Telemetry channel (media engine → Go)

**Phase 4B: Media Engine Separation** ✓
- gRPC media engine server (separate process)
- Graph-based DSP pipeline
- Multiple outputs with isolation
- Live input integration (Icecast, RTP, SRT)
- Real-time audio telemetry

**Phase 4C: Live Input & Webstream Relay** ✓
- Harbor-style live input with authorization
- Webstream relay with failover chains
- Health monitoring and automatic reconnect

**Phase 5: Observability & Multi-Instance** ✓
- Redis Pub/Sub and NATS event bus
- Complete Prometheus metrics
- Distributed tracing (OpenTelemetry)
- AlertManager integration
- Consistent hashing and leader election

**Phase 6: Production Readiness** ✓
- AzuraCast/LibreTime migration tools
- Docker Compose deployment
- Kubernetes manifests
- Load testing and performance optimization
- Production deployment guides

**Phase 7: Nix Integration** ✓
- Reproducible builds via Nix flakes
- Three deployment flavors (Basic, Full NixOS, Dev)

### ⏳ FUTURE PHASES

**Phase 8: WebDJ & Advanced Features**
- WebDJ interface
- Voice tracking
- Emergency Alert System (EAS)
- Advanced scheduling (conflict detection)

**Current Version:** 1.3.1 (Production)

---

## Technology Stack

### ✓ IMPLEMENTED

**Core:**
- Go 1.24
- Chi v5 (HTTP router)
- GORM (ORM with PostgreSQL/MySQL/SQLite)
- Zerolog (structured logging)
- nhooyr.io/websocket (WebSocket)
- golang-jwt/jwt/v5 (web session cookies)
- google/uuid (UUID generation)
- golang.org/x/crypto (bcrypt)
- gRPC (`google.golang.org/grpc`) - media engine control
- Protobuf (`google.golang.org/protobuf`) - message serialization
- Redis client (`github.com/go-redis/redis/v9`) - event bus, leader election
- NATS client (`github.com/nats-io/nats.go`) - alternative event bus
- OpenTelemetry (`go.opentelemetry.io/otel`) - distributed tracing
- Prometheus client (`github.com/prometheus/client_golang`) - metrics

**External:**
- GStreamer 1.0 with plugins: base, good, bad, ugly (audio processing)
- PostgreSQL 12+ / MySQL 8+ / SQLite 3 (database)
- Icecast 2.4+ / Shoutcast 2+ (streaming servers)
- S3-compatible object storage (optional)
- Redis (event bus, leader election)
- NATS (alternative event bus)

**Observability:**
- Prometheus (metrics collection)
- Grafana (dashboards)
- Jaeger or Tempo (distributed tracing)
- AlertManager (alerting)

**Media Engine:**
- Separate `mediaengine` binary
- GStreamer 1.0 with plugins: base, good, bad, ugly
- gRPC server for control interface

---

## Non-Goals

Per the design brief, these are explicitly **out of scope**:

❌ **No embedded audio scripting language** - No Liquidsoap DSL, no Lua, no JavaScript audio scripting
❌ **No per-output encoders duplicating work** - One decode/DSP chain → multiple encoder forks
❌ **No monolithic single process** - Separate API, planner, executor, media engine processes
❌ **No CLI glue scripts** - All control via API (HTTP or gRPC)
❌ **No restart for schedule changes** - Dynamic reconfiguration only

---

## Summary

Grimnir Radio is a production-ready, Go-controlled broadcast automation platform that **replaces Liquidsoap** with a more reliable, observable, and controllable media pipeline. The system follows the principle of **"Go owns the control plane, a dedicated media engine owns real-time audio"** with clean separation of concerns, isolated failure domains, and no audio scripting DSL.

**Current State:** Production release (v1.3.1) powering [rlmradio.xyz](https://rlmradio.xyz). All planned phases complete.

**Implemented:**
- Two-binary architecture (grimnirradio + mediaengine)
- 5-tier priority system with state machine
- Graph-based DSP pipeline (loudness, AGC, compression, limiting)
- Live input with token-based authorization (Icecast, RTP, SRT)
- Webstream relay with automatic failover
- Full observability (Prometheus, OpenTelemetry, alerting)
- Horizontal scaling with consistent hashing and leader election
- Migration tools for AzuraCast and LibreTime
- Audit logging for sensitive operations
- Turn-key deployment (Docker, Kubernetes, Nix)

**Future Work:** Customizable landing page editor, WebDJ interface, Emergency Alert System (EAS), advanced scheduling features.
