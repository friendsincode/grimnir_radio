# Grimnir Radio — Programmer's Spec

**Version:** 0.0.1-alpha
**Architecture:** Go-Based Broadcast Automation Platform (Liquidsoap Replacement)

This document is for developers working on Grimnir Radio. It clearly separates **✓ IMPLEMENTED** features from **⏳ PLANNED** features and provides practical guidance for working with the codebase.

---

## Design Philosophy

### Core Principle
**Go owns the control plane. A dedicated media engine owns real-time audio.**

### What This Means for Developers
- **No audio DSL**: Configuration is declarative (YAML/JSON), not scripting
- **Separation of concerns**: Planner generates timeline, executor executes it, media engine plays audio
- **gRPC for control**: Media engine controlled via gRPC, not CLI glue scripts
- **API-first**: All operations exposed via HTTP/gRPC APIs
- **Isolated failures**: One component crash doesn't take down the whole system

---

## Repository Layout

### ✓ IMPLEMENTED Packages

```
grimnir_radio/
├── cmd/
│   └── grimnirradio/          # Main binary ✓
│       └── main.go            # Entry point, service bootstrap
│
├── internal/
│   ├── api/                   # HTTP+WebSocket API ✓
│   │   └── api.go             # REST endpoints, WebSocket handling
│   ├── analyzer/              # Media analysis service ✓ (partial)
│   │   └── analyzer.go        # LUFS analysis, cue points (basic)
│   ├── auth/                  # API key auth + RBAC ✓
│   │   ├── apikey.go          # API key generation/validation
│   │   └── middleware.go      # Auth middleware
│   ├── clock/                 # Clock compilation ✓
│   │   └── clock.go           # Hour template processing
│   ├── config/                # Configuration loading ✓
│   │   └── config.go          # Env var parsing, validation
│   ├── db/                    # Database management ✓
│   │   └── db.go              # GORM setup, migrations
│   ├── events/                # Event bus ✓ (in-memory, will be replaced)
│   │   └── bus.go             # Pub/sub event bus
│   ├── live/                  # Live input services ✓
│   │   └── live.go            # Authorization, handover triggering
│   ├── logging/               # Structured logging ✓
│   │   └── logger.go          # Zerolog setup
│   ├── media/                 # Media service ✓
│   │   └── media.go           # File storage, upload handling
│   ├── models/                # Data models ✓
│   │   └── models.go          # GORM entities
│   ├── playout/               # GStreamer pipeline mgmt ✓ (basic)
│   │   └── manager.go         # Pipeline lifecycle
│   ├── scheduler/             # Schedule builder ✓ (will become planner)
│   │   └── scheduler.go       # Timeline generation
│   ├── smartblock/            # Rule engine ✓
│   │   └── engine.go          # Smart Block materialization
│   ├── storage/               # Object storage ✓
│   │   └── storage.go         # S3-compatible or filesystem
│   └── telemetry/             # Metrics/health ✓ (partial)
│       └── telemetry.go       # Health checks, metric stubs
│
├── docs/                      # Documentation
│   ├── API_REFERENCE.md       # Complete API docs ✓
│   ├── ARCHITECTURE_ROADMAP.md # Implementation roadmap ✓
│   └── specs/                 # Specification documents
│       ├── ENGINEERING_SPEC.md
│       ├── PROGRAMMERS_SPEC.md (this file)
│       └── SALES_SPEC.md
│
├── go.mod                     # Go dependencies ✓
├── go.sum
├── Makefile                   # Build targets ✓
└── VERSION                    # Version file (0.0.1-alpha)
```

### ⏳ PLANNED Packages (New Architecture)

```
internal/
├── planner/                   # ⭐ Renamed from scheduler
│   └── planner.go             # Pure timeline generator
├── executor/                  # ⭐ NEW - Per-station execution
│   ├── executor.go            # State machine, timeline execution
│   ├── statemachine.go        # State transitions
│   └── priority.go            # Priority-based source management
├── mediaengine/               # ⭐ NEW - gRPC client
│   ├── client.go              # gRPC client wrapper
│   ├── commands.go            # LoadGraph, Play, Stop, Fade, etc.
│   └── telemetry.go           # Telemetry stream consumer
├── priority/                  # ⭐ NEW - Priority system
│   ├── priority.go            # 5-tier priority definitions
│   └── resolver.go            # Conflict resolution
├── dsp/                       # ⭐ NEW - DSP configuration
│   ├── graph.go               # Graph builder
│   └── nodes.go               # Node definitions (loudness, AGC, etc.)
├── eventbus/                  # ⭐ Replacement for events
│   ├── redis.go               # Redis Pub/Sub adapter
│   └── nats.go                # NATS adapter
└── migration/                 # ⭐ NEW - Import tools
    ├── azuracast.go           # AzuraCast backup import
    └── libretime.go           # LibreTime backup import

cmd/
└── mediaengine/               # ⭐ NEW - Separate binary
    └── main.go                # Media engine gRPC server
```

---

## Build & Run

### ✓ IMPLEMENTED

**Prerequisites:**
- Go 1.22+
- PostgreSQL 12+ / MySQL 8+ / SQLite 3
- GStreamer 1.0 (for playout)

**Build:**
```bash
make build              # Builds ./grimnirradio
# OR
go build ./cmd/grimnirradio
```

**Run:**
```bash
./grimnirradio
```

**Test:**
```bash
make test               # Run tests with race detector
# OR
go test -race ./...
```

**Verify (CI):**
```bash
make verify             # tidy, fmt, vet, lint, test
```

### ⏳ PLANNED (New Architecture)

**Build multiple binaries:**
```bash
make build-all          # Builds grimnirradio + mediaengine
# OR
go build ./cmd/grimnirradio
go build ./cmd/mediaengine
```

**Run with media engine:**
```bash
# Terminal 1: Start media engine (one per station)
./mediaengine --grpc-port=9091 --station-id=<uuid>

# Terminal 2: Start main API/control plane
export GRIMNIR_MEDIA_ENGINE_GRPC=localhost:9091
./grimnirradio
```

---

## Environment Variables

### ✓ IMPLEMENTED (prefer `GRIMNIR_*`)

**Core Configuration:**
```bash
GRIMNIR_ENV=development                    # development | production
GRIMNIR_HTTP_BIND=0.0.0.0                  # HTTP bind address
GRIMNIR_HTTP_PORT=8080                     # HTTP port
GRIMNIR_DB_BACKEND=postgres                # postgres | mysql | sqlite
GRIMNIR_DB_DSN="postgres://..."           # Database connection string (required)
GRIMNIR_MEDIA_ROOT=./media                 # Media storage path
GRIMNIR_OBJECT_STORAGE_URL=                # S3 URL (optional)
GRIMNIR_GSTREAMER_BIN=gst-launch-1.0       # GStreamer binary path
GRIMNIR_SCHEDULER_LOOKAHEAD_MINUTES=48     # Schedule horizon (48 hours)
GRIMNIR_JWT_SIGNING_KEY=secret             # Session signing key (required)
GRIMNIR_METRICS_BIND=127.0.0.1:9000        # Metrics endpoint
```

**Legacy Compatibility:**
- `RLM_*` variants accepted as fallback (for backward compatibility)

### ⏳ PLANNED Configuration

**Event Bus:**
```bash
GRIMNIR_EVENT_BUS_BACKEND=redis            # redis | nats | memory
GRIMNIR_REDIS_URL=redis://localhost:6379   # Redis connection
GRIMNIR_NATS_URL=nats://localhost:4222     # NATS connection
```

**Media Engine:**
```bash
GRIMNIR_MEDIA_ENGINE_GRPC=localhost:9091   # gRPC endpoint
GRIMNIR_MEDIA_ENGINE_TLS=true              # Use TLS for gRPC
```

**Webstream (future):**
```bash
GRIMNIR_WEBSTREAM_ALLOWED_SCHEMES=http,https
GRIMNIR_WEBSTREAM_CONNECT_TIMEOUT_MS=5000
GRIMNIR_WEBSTREAM_PREFLIGHT_MS=3000
GRIMNIR_WEBSTREAM_GRACE_MS=5000
GRIMNIR_WEBSTREAM_FALLBACK_LIMIT=3
```

**Migration Tools (future):**
```bash
GRIMNIR_IMPORT_MEDIA_ROOT=./media
GRIMNIR_IMPORT_BATCH_SIZE=500
GRIMNIR_IMPORT_DRY_RUN=false
```

---

## Local Development

### Quick Start with SQLite

```bash
# 1. Set environment variables
export GRIMNIR_DB_BACKEND=sqlite
export GRIMNIR_DB_DSN="file:dev.sqlite?_foreign_keys=on"
export GRIMNIR_JWT_SIGNING_KEY="dev-secret-change-in-production"  # For web session cookies

# 2. Run migrations (automatic on startup)
./grimnirradio

# 3. Test API
curl http://localhost:8080/api/v1/health
```

### PostgreSQL Development Setup

```bash
# 1. Start PostgreSQL (Docker)
docker run -d \
  --name grimnir-postgres \
  -e POSTGRES_PASSWORD=password \
  -e POSTGRES_DB=grimnir \
  -p 5432:5432 \
  postgres:15

# 2. Set environment variables
export GRIMNIR_DB_BACKEND=postgres
export GRIMNIR_DB_DSN="postgres://postgres:password@localhost:5432/grimnir?sslmode=disable"
export GRIMNIR_JWT_SIGNING_KEY="dev-secret"  # For web session cookies

# 3. Run
./grimnirradio
```

### Development Workflow

```bash
# 1. Make changes to code

# 2. Format and vet
make fmt
make vet

# 3. Run tests
make test

# 4. Run full verification (before commit)
make verify

# 5. Build and test locally
make build
./grimnirradio
```

---

## API Documentation

### ✓ IMPLEMENTED Endpoints

See `docs/API_REFERENCE.md` for complete documentation with schemas and examples.

**Quick Reference:**

**Authentication:**
```
# API uses X-API-Key header (generate keys from profile page)
# Web dashboard uses session cookies (login via /login page)
```

**Stations:**
```
GET  /api/v1/stations               # List stations
POST /api/v1/stations               # Create station (admin, manager)
GET  /api/v1/stations/{id}          # Get station
```

**Mounts:**
```
GET  /api/v1/stations/{id}/mounts   # List mounts
POST /api/v1/stations/{id}/mounts   # Create mount (admin, manager)
```

**Media:**
```
POST /api/v1/media/upload           # Upload audio (admin, manager, dj)
GET  /api/v1/media/{id}             # Get media details
```

**Smart Blocks:**
```
GET  /api/v1/smart-blocks           # List smart blocks
POST /api/v1/smart-blocks           # Create (admin, manager)
POST /api/v1/smart-blocks/{id}/materialize  # Generate playlist
```

**Clocks:**
```
GET  /api/v1/clocks                 # List clocks
POST /api/v1/clocks                 # Create (admin, manager)
POST /api/v1/clocks/{id}/simulate   # Preview schedule
```

**Schedule:**
```
GET  /api/v1/schedule               # Get upcoming entries
POST /api/v1/schedule/refresh       # Rebuild (admin, manager)
PATCH /api/v1/schedule/{id}         # Modify entry (admin, manager)
```

**Live:**
```
POST /api/v1/live/authorize         # Authorize live source
POST /api/v1/live/handover          # Trigger live takeover (admin, manager)
```

**Playout:**
```
POST /api/v1/playout/reload         # Restart pipeline (admin, manager)
POST /api/v1/playout/skip           # Skip track (admin, manager, dj)
POST /api/v1/playout/stop           # Stop (admin, manager)
```

**Analytics:**
```
GET  /api/v1/analytics/now-playing  # Current track
GET  /api/v1/analytics/spins        # Play history (admin, manager)
```

**Events:**
```
GET  /api/v1/events                 # WebSocket stream
```

### ⏳ PLANNED Endpoints (New Architecture)

**Priority Management:**
```
POST   /api/v1/priority/emergency   # Emergency takeover (priority 0)
POST   /api/v1/priority/override    # Manual override (priority 1)
GET    /api/v1/priority/sources     # List active sources
DELETE /api/v1/priority/sources/{id} # Remove source
```

**Executor State:**
```
GET /api/v1/executor/states         # List all executor states
GET /api/v1/executor/states/{stationID}  # Get state for station
```

**DSP Graphs:**
```
GET    /api/v1/dsp-graphs           # List DSP configurations
POST   /api/v1/dsp-graphs           # Create DSP graph
POST   /api/v1/dsp-graphs/{id}/apply # Apply to station
```

**Webstreams:**
```
GET  /api/v1/webstreams             # List webstreams
POST /api/v1/webstreams             # Create webstream
GET  /api/v1/webstreams/{id}/health # Health check
```

**Migrations:**
```
POST /api/v1/migrations/azuracast   # Import AzuraCast backup
POST /api/v1/migrations/libretime   # Import LibreTime backup
GET  /api/v1/migrations/{jobID}     # Status
```

---

## Architecture Overview

### Process Architecture (Target)

```
┌─────────────────────────────────────────┐
│         API Gateway (Go)                │  Port 8080/9090
│  HTTP REST + gRPC + WebSocket + SSE     │  Process: grimnirradio
└──────────┬──────────────────────────────┘
           │
    ┌──────┴───────┬──────────────────────┐
    │              │                      │
┌───▼─────┐  ┌─────▼──────┐  ┌───────────▼────┐
│ Planner │  │  Media     │  │  Background    │
│         │  │  Library   │  │  Workers       │
│ Timeline│  │  Analysis  │  │  Transcoding   │
└───┬─────┘  └────────────┘  └────────────────┘
    │
    │ Schedule Timeline
    ▼
┌──────────────────────────────────────────────┐
│        Executor Pool (goroutines)            │
│  [Exec-1] [Exec-2] ... [Exec-N]              │
│  One per station │ State Machine             │
└──────┬───────────────────────────────────────┘
       │
       │ gRPC Commands
       ▼
┌─────────────────────────────────────────────┐
│    Media Engine (separate process)          │  Port 9091
│    GStreamer pipeline + gRPC server         │  Process: mediaengine
│    Decode → DSP → Encode → Outputs          │
└─────────────────────────────────────────────┘
```

### Component Interaction

**Schedule Generation Flow:**
1. Planner generates timeline every N minutes
2. Timeline persisted to `schedule_entries` table
3. Executor polls timeline for upcoming events
4. At T-30s: Executor preloads next item
5. At T: Executor sends gRPC `Play` command to media engine
6. Media engine starts playback, streams telemetry back

**Priority Override Flow:**
1. User POSTs to `/api/v1/priority/override`
2. API creates `priority_sources` entry
3. Executor detects higher priority source
4. Executor sends `Fade` command to media engine (current track)
5. Executor sends `RouteLive` command (new source)
6. State machine transitions to Live state

---

## Database Schema

### ✓ IMPLEMENTED Tables

See `internal/models/models.go` for complete definitions.

**Core Tables:**
- `users` - Authentication (id, email, password_hash, role)
- `stations` - Station definitions (id, name, timezone)
- `mounts` - Output streams (id, station_id, url, format, bitrate)
- `encoder_presets` - GStreamer encoders (id, name, format, options)
- `media_items` - Audio files (id, title, artist, duration, path, loudness_lufs, cue_points)
- `tags` - Metadata labels (id, name)
- `media_tag_links` - Many-to-many media↔tags
- `smart_blocks` - Rule definitions (id, station_id, rules, sequence)
- `clock_hours` - Hour templates (id, station_id, name)
- `clock_slots` - Clock elements (id, clock_hour_id, type, payload)
- `schedule_entries` - Materialized schedule (id, station_id, starts_at, ends_at, source_type, metadata)
- `play_history` - Played tracks (id, station_id, media_id, started_at, ended_at)
- `analysis_jobs` - Analyzer work queue (id, media_id, status)

### ⏳ PLANNED Tables

**`executor_states`** - Runtime executor state
```sql
CREATE TABLE executor_states (
  id UUID PRIMARY KEY,
  station_id UUID NOT NULL,
  state VARCHAR(32) NOT NULL,  -- idle, preloading, playing, fading, live, emergency
  current_priority INT NOT NULL,  -- 0-4
  current_source_id UUID,
  current_source_type VARCHAR(32),  -- media, live, webstream, fallback
  buffer_depth INT,  -- samples
  last_heartbeat TIMESTAMP NOT NULL,
  metadata JSONB
);
```

**`priority_sources`** - Active priority sources
```sql
CREATE TABLE priority_sources (
  id UUID PRIMARY KEY,
  station_id UUID NOT NULL,
  mount_id UUID NOT NULL,
  priority INT NOT NULL,  -- 0=emergency, 1=live_override, 2=live_scheduled, 3=automation, 4=fallback
  source_type VARCHAR(32) NOT NULL,
  source_id UUID NOT NULL,
  starts_at TIMESTAMP NOT NULL,
  ends_at TIMESTAMP,  -- NULL = indefinite
  active BOOLEAN NOT NULL DEFAULT true,
  metadata JSONB
);
```

**`webstreams`** - External stream definitions
```sql
CREATE TABLE webstreams (
  id UUID PRIMARY KEY,
  station_id UUID NOT NULL,
  name VARCHAR(255) NOT NULL,
  url TEXT NOT NULL,  -- primary URL
  fallback_urls JSONB,  -- array of backup URLs
  headers JSONB,  -- HTTP headers
  health_state VARCHAR(32) NOT NULL,  -- healthy, degraded, down
  last_health_check TIMESTAMP,
  metadata JSONB,
  created_at TIMESTAMP NOT NULL,
  updated_at TIMESTAMP NOT NULL
);
```

**`dsp_graphs`** - DSP configurations
```sql
CREATE TABLE dsp_graphs (
  id UUID PRIMARY KEY,
  station_id UUID NOT NULL,
  name VARCHAR(255) NOT NULL,
  description TEXT,
  nodes JSONB NOT NULL,  -- ordered array of DSP nodes
  created_at TIMESTAMP NOT NULL,
  updated_at TIMESTAMP NOT NULL
);
```

---

## Testing

### ✓ IMPLEMENTED

**Run tests:**
```bash
make test
# OR
go test -race ./...
```

**Coverage:**
```bash
go test -cover ./...
# OR
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

**Focus areas with existing tests:**
- Smart Block rule evaluation (`internal/smartblock`)
- Schedule generation (`internal/scheduler`)
- API endpoint validation (`internal/api`)
- API key auth (`internal/auth`)

### ⏳ PLANNED Testing

**Unit Tests (to be added):**
- Planner timeline generation
- Executor state machine transitions
- Priority resolution logic
- DSP graph builder
- gRPC command serialization

**Integration Tests:**
```bash
# End-to-end test with mock media engine
go test -tags=integration ./test/integration/...
```

**Stress Tests:**
```bash
# 24-hour continuous run with 10 stations
go test -tags=stress -timeout=24h ./test/stress/...
```

**Chaos Tests:**
```bash
# Random component failures
go test -tags=chaos ./test/chaos/...
```

---

## Code Style & Conventions

### Go Best Practices

**Function naming:**
- Exported functions: `PascalCase`
- Unexported functions: `camelCase`
- Avoid one-letter variables (except loop indices, common abbreviations)

**Error handling:**
```go
// ✓ GOOD
result, err := DoSomething()
if err != nil {
    return fmt.Errorf("do something failed: %w", err)
}

// ✗ BAD
result, _ := DoSomething()  // Don't ignore errors
```

**Context propagation:**
```go
// ✓ GOOD - Pass context as first parameter
func ProcessSchedule(ctx context.Context, stationID string) error {
    // ...
}

// ✗ BAD - Context stored in struct
type Service struct {
    ctx context.Context  // Don't do this
}
```

**Use defer for cleanup:**
```go
// ✓ GOOD
file, err := os.Open("file.txt")
if err != nil {
    return err
}
defer file.Close()

// Process file...
```

### Project Conventions

**Logging:**
```go
// Use structured logging with Zerolog
logger.Info().
    Str("station_id", stationID).
    Str("track_id", trackID).
    Msg("starting track playback")
```

**Configuration:**
```go
// Use config package for env vars
cfg := config.Load()
if cfg.DBBackend == "postgres" {
    // ...
}
```

**Database queries:**
```go
// Use GORM, never raw SQL unless absolutely necessary
var station models.Station
if err := db.Where("id = ?", stationID).First(&station).Error; err != nil {
    return err
}
```

---

## Adding a New Feature

### Step-by-Step Guide

**1. Define interfaces** (`internal/<package>/interface.go`)
```go
package executor

type Executor interface {
    Start(ctx context.Context) error
    Stop(ctx context.Context) error
    GetState(stationID string) (State, error)
}
```

**2. Implement business logic**
```go
package executor

type service struct {
    planner Planner
    mediaEngine MediaEngineClient
    logger zerolog.Logger
}

func (s *service) Start(ctx context.Context) error {
    // Implementation...
}
```

**3. Write unit tests**
```go
package executor_test

func TestExecutorStart(t *testing.T) {
    // Test implementation...
}
```

**4. Wire into main** (`cmd/grimnirradio/main.go`)
```go
// Initialize service
executorSvc := executor.New(planner, mediaEngine, logger)

// Start service
if err := executorSvc.Start(ctx); err != nil {
    log.Fatal(err)
}
```

**5. Add API endpoints** (`internal/api/api.go`)
```go
func (a *API) handleExecutorState(w http.ResponseWriter, r *http.Request) {
    stationID := chi.URLParam(r, "stationID")
    state, err := a.executor.GetState(stationID)
    // ... handle response
}
```

**6. Update documentation**
- Add endpoint to `docs/API_REFERENCE.md`
- Update this spec with feature status
- Add to `CHANGELOG.md`

---

## gRPC Media Engine Interface

### ⏳ PLANNED

**Protocol Buffer Definition** (`proto/mediaengine.proto`):
```protobuf
syntax = "proto3";
package mediaengine;

service MediaEngine {
  // Load audio processing graph
  rpc LoadGraph(GraphConfig) returns (GraphHandle);

  // Playback control
  rpc Play(PlayRequest) returns (PlayResponse);
  rpc Stop(StopRequest) returns (StopResponse);
  rpc Fade(FadeRequest) returns (FadeResponse);

  // Priority sources
  rpc InsertEmergency(InsertRequest) returns (InsertResponse);
  rpc RouteLive(RouteRequest) returns (RouteResponse);

  // Telemetry stream (server → client)
  rpc StreamTelemetry(TelemetryRequest) returns (stream Telemetry);
}

message GraphConfig {
  repeated DSPNode nodes = 1;
  repeated Output outputs = 2;
}

message DSPNode {
  string type = 1;  // loudness, agc, compressor, limiter, ducking, silence
  map<string, string> config = 2;
}

message PlayRequest {
  string file_path = 1;
  int32 fade_in_ms = 2;
  optional CuePoint cue_in = 3;
  optional CuePoint cue_out = 4;
}

message Telemetry {
  int64 timestamp_ms = 1;
  int32 buffer_depth_samples = 2;
  int32 dropout_count = 3;
  float cpu_usage_percent = 4;
  float loudness_lufs = 5;
  float true_peak_dbtp = 6;
  map<string, OutputHealth> output_health = 7;
}
```

**Generate Go code:**
```bash
protoc --go_out=. --go-grpc_out=. proto/mediaengine.proto
```

**Client usage** (`internal/executor/executor.go`):
```go
// Connect to media engine
conn, err := grpc.Dial("localhost:9091", grpc.WithInsecure())
if err != nil {
    return err
}
defer conn.Close()

client := mediaengine.NewMediaEngineClient(conn)

// Load DSP graph
graphCfg := &mediaengine.GraphConfig{
    Nodes: []*mediaengine.DSPNode{
        {Type: "loudness", Config: map[string]string{"target_lufs": "-16.0"}},
        {Type: "limiter", Config: map[string]string{"threshold_db": "-1.0"}},
    },
}
handle, err := client.LoadGraph(ctx, graphCfg)
if err != nil {
    return err
}

// Play track
playReq := &mediaengine.PlayRequest{
    FilePath: "/media/track.mp3",
    FadeInMs: 500,
}
resp, err := client.Play(ctx, playReq)
if err != nil {
    return err
}

// Subscribe to telemetry
telemetryStream, err := client.StreamTelemetry(ctx, &mediaengine.TelemetryRequest{})
if err != nil {
    return err
}

for {
    telemetry, err := telemetryStream.Recv()
    if err != nil {
        break
    }
    // Process telemetry...
}
```

---

## Troubleshooting

### Common Issues

**Database connection errors:**
```bash
# Check DSN format
echo $GRIMNIR_DB_DSN

# For PostgreSQL:
postgres://user:password@host:5432/dbname?sslmode=disable

# For MySQL:
user:password@tcp(host:3306)/dbname?parseTime=true

# For SQLite:
file:dev.sqlite?_foreign_keys=on
```

**API authentication failures:**
```bash
# Use API key from profile page (X-API-Key header)
curl http://localhost:8080/api/v1/stations \
  -H "X-API-Key: gr_your-api-key"

# Check key hasn't expired or been revoked
# Generate new key from Profile → API Keys in web dashboard
```

**GStreamer not found:**
```bash
# Check GStreamer binary
which gst-launch-1.0

# Or set custom path
export GRIMNIR_GSTREAMER_BIN=/usr/local/bin/gst-launch-1.0
```

**Schedule gaps:**
```bash
# Check clocks are defined
curl http://localhost:8080/api/v1/clocks?station_id=$STATION_ID \
  -H "X-API-Key: $API_KEY"

# Check smart blocks have matching media
curl http://localhost:8080/api/v1/smart-blocks \
  -H "X-API-Key: $API_KEY"

# Manually refresh schedule
curl -X POST http://localhost:8080/api/v1/schedule/refresh \
  -H "X-API-Key: $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"station_id":"$STATION_ID"}'
```

---

## Development Tools

### Recommended Editor Setup

**VS Code Extensions:**
- Go (golang.go) - Official Go extension
- REST Client (humao.rest-client) - API testing
- PostgreSQL (ckolkman.vscode-postgres) - Database management
- Protocol Buffers (zxh404.vscode-proto3) - Protobuf syntax

**VS Code Settings** (`.vscode/settings.json`):
```json
{
  "go.useLanguageServer": true,
  "go.lintTool": "golangci-lint",
  "go.lintOnSave": "package",
  "go.vetOnSave": "package",
  "go.buildOnSave": "package",
  "[go]": {
    "editor.formatOnSave": true,
    "editor.codeActionsOnSave": {
      "source.organizeImports": true
    }
  }
}
```

### Makefile Targets

```bash
make verify      # Full CI verification (tidy, fmt-check, vet, lint, test)
make build       # Build ./grimnirradio binary
make build-all   # Build all binaries (planned: + mediaengine)
make test        # Run tests with race detector
make fmt         # Format code with gofmt
make fmt-check   # Check formatting (CI)
make vet         # Run go vet
make lint        # Run golangci-lint (if installed)
make tidy        # Tidy go.mod
make clean       # Remove binaries
```

---

## Contributing Workflow

### Before Committing

```bash
# 1. Format code
make fmt

# 2. Run full verification
make verify

# 3. Test locally
make build
./grimnirradio

# 4. Test API endpoints
# (use REST client or curl)
```

### Commit Guidelines

**Commit message format:**
```
<type>: <subject>

<body>

Co-Authored-By: Claude Sonnet 4.5 <noreply@anthropic.com>
```

**Types:**
- `feat`: New feature
- `fix`: Bug fix
- `refactor`: Code refactoring
- `docs`: Documentation
- `test`: Tests
- `chore`: Build/tooling

**Example:**
```
feat: add priority system for live overrides

Implement 5-tier priority system:
- Emergency (0)
- Live Override (1)
- Live Scheduled (2)
- Automation (3)
- Fallback (4)

Includes executor state machine, priority resolver, and API endpoints.

Co-Authored-By: Claude Sonnet 4.5 <noreply@anthropic.com>
```

---

## Further Reading

- **API Reference:** `docs/API_REFERENCE.md` - Complete endpoint documentation
- **Engineering Spec:** `docs/specs/ENGINEERING_SPEC.md` - Architecture details
- **Sales Spec:** `docs/specs/SALES_SPEC.md` - Business perspective
- **Roadmap:** `docs/ARCHITECTURE_ROADMAP.md` - Implementation phases
- **Changelog:** `docs/CHANGELOG.md` - Version history

---

## Questions?

- Check existing documentation (docs/ directory)
- Review code comments in relevant packages
- Ask on GitHub Issues (for bugs/features)
- Refer to ENGINEERING_SPEC.md for architectural decisions
