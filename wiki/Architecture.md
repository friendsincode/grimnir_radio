# Architecture Overview

Grimnir Radio uses a modern, scalable architecture designed for broadcast automation with multi-instance support.

## System Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                     Control Plane Cluster                   │
│  ┌────────────┐  ┌────────────┐  ┌────────────┐          │
│  │ Instance 1 │  │ Instance 2 │  │ Instance 3 │          │
│  │ (Leader)   │  │ (Follower) │  │ (Follower) │          │
│  └─────┬──────┘  └─────┬──────┘  └─────┬──────┘          │
│        │                │                │                   │
│        └────────────────┴────────────────┘                   │
│                         │                                     │
└─────────────────────────┼─────────────────────────────────────┘
                          │
           ┌──────────────┼──────────────┐
           │              │              │
           ▼              ▼              ▼
    ┌───────────┐  ┌───────────┐  ┌────────────┐
    │PostgreSQL │  │   Redis   │  │Media Engine│
    │  Primary  │  │Event Bus │  │  (gRPC)    │
    └───────────┘  └───────────┘  └─────┬──────┘
           │                              │
           │                              ▼
           │                       ┌──────────────┐
           │                       │  GStreamer   │
           │                       │  Pipelines   │
           │                       └──────┬───────┘
           │                              │
           └──────────────────────────────┼─────────┐
                                          │         │
                                          ▼         ▼
                                    ┌─────────┐ ┌─────────┐
                                    │Icecast2 │ │  S3/    │
                                    │Streams  │ │Storage  │
                                    └─────────┘ └─────────┘
```

## Core Components

### 1. Control Plane (`grimnirradio`)

The main application binary handling all business logic and API requests.

**Responsibilities:**
- **HTTP REST API** - Station, media, schedule management
- **Scheduler** - Generates schedule entries from clocks/smart blocks
- **Executor** - Per-station goroutines managing playback state
- **Priority System** - 5-tier priority ladder for content sources
- **Event Bus** - Pub/sub for distributed coordination
- **Authentication** - JWT-based auth with RBAC
- **WebSocket** - Real-time event streaming to clients

**Key Services:**
```go
internal/
├── api/           # HTTP handlers
├── scheduler/     # Schedule generation
├── executor/      # Playback execution
├── priority/      # Priority management
├── smartblock/    # Rule-based playlists
├── auth/          # Authentication
└── eventbus/      # Event distribution
```

### 2. Media Engine (`mediaengine`)

Separate process handling all audio processing via GStreamer.

**Responsibilities:**
- **gRPC Server** - Control interface (port 9091)
- **Pipeline Management** - GStreamer pipeline lifecycle
- **DSP Processing** - Loudness, AGC, compression, limiting
- **Crossfades** - Cue-aware seamless transitions
- **Live Input** - Harbor-style DJ input with handover
- **Telemetry** - Real-time audio levels, LUFS, underruns

**Why Separate Process?**
- **Isolation**: GStreamer crashes don't affect control plane
- **Resource Limits**: Can constrain CPU/memory per engine
- **Scaling**: Run multiple engines for different stations
- **Development**: Restart media engine without losing API state

### 3. Database Layer

**Supported Databases:**
- **PostgreSQL** (recommended) - Full features, best performance
- **MySQL 8+** - Alternative for existing MySQL infrastructure
- **SQLite** - Development and single-instance deployments

**Data Models:**
```
stations         # Station configuration
media_items      # Media library metadata
smart_blocks     # Rule-based playlist definitions
clocks           # Hour templates
schedule_entries # Materialized schedule
mounts           # Output stream endpoints
users            # User accounts (JWT auth)
priority_sources # Active priority sources
executor_states  # Per-station playback state
```

### 4. Event Bus

**Purpose**: Distributed coordination for multi-instance deployments

**Implementations:**
1. **In-Memory** - Single instance, development
2. **Redis Pub/Sub** - Multi-instance, simple setup
3. **NATS JetStream** - Multi-instance, enterprise-grade

**Event Types:**
- `schedule_update` - New schedule entries generated
- `priority.emergency` - Emergency content inserted
- `priority.override` - Manual override activated
- `live.handover` - DJ handover initiated
- `webstream.failover` - Webstream source failed over
- `dj_connect` / `dj_disconnect` - DJ connection events
- `now_playing` - Track metadata updates

### 5. Storage Backends

**Media File Storage:**

**Option A: Filesystem**
```
/media/
  ├── station-1/
  │   ├── media-id-1/
  │   └── media-id-2/
  └── station-2/
      └── media-id-3/
```

**Option B: S3-Compatible**
- AWS S3
- MinIO (self-hosted)
- DigitalOcean Spaces
- Backblaze B2
- Wasabi

**Configuration:**
```bash
# Filesystem
GRIMNIR_MEDIA_ROOT=/var/lib/grimnir-radio/media

# S3
GRIMNIR_S3_BUCKET=my-media
GRIMNIR_S3_REGION=us-east-1
GRIMNIR_S3_ACCESS_KEY_ID=...
GRIMNIR_S3_SECRET_ACCESS_KEY=...
```

---

## Data Flow

### Schedule Generation Flow

```
1. Clock Definition (Admin)
   ↓
2. Scheduler (Every 30s tick)
   - Looks ahead 48 hours
   - Compiles clock slots into schedule entries
   ↓
3. Smart Block Materialization
   - Executes rules against media library
   - Selects tracks matching criteria
   ↓
4. Schedule Entry Creation
   - Writes entries to database
   - Publishes schedule_update event
   ↓
5. Executors Receive Event
   - Update internal schedule cache
   - Preload next track if needed
```

### Playback Flow

```
1. Executor Monitors Schedule
   - Checks for upcoming entries (5 min lookahead)
   ↓
2. Executor Sends Play Command
   - Checks priority system for preemption
   - Sends gRPC LoadGraph + Play to media engine
   ↓
3. Media Engine Processes Audio
   - Loads media file (filesystem or S3)
   - Applies DSP graph (loudness, AGC, etc.)
   - Crossfades with previous track (if configured)
   ↓
4. Stream Output
   - Encodes to MP3/AAC/OGG (configurable)
   - Sends to Icecast2 mount point
   ↓
5. Telemetry Feedback
   - Media engine streams telemetry via gRPC
   - Executor updates state (levels, position, metadata)
   - WebSocket clients receive now_playing events
```

### Priority System Flow

```
Priority Tiers (highest to lowest):
0. Emergency      - Station alerts, breaking news
1. Live Override  - Manual DJ takeover
2. Live Scheduled - Scheduled live shows
3. Automation     - Regular scheduled content
4. Fallback       - Dead air prevention

Priority Change Flow:
1. Priority Source Inserted
   - API call or scheduled event
   ↓
2. Priority Service Evaluates
   - Compares new priority to current
   - Determines if preemption needed
   ↓
3. Event Published (if preemption)
   - priority.emergency or priority.override
   ↓
4. Executors Receive Event
   - Trigger crossfade to new source
   - Update internal state
   ↓
5. Media Engine Switches
   - Fades out current track
   - Routes new source through DSP
   ↓
6. Priority Source Released
   - When completed, publishes priority.released
   - Executor returns to previous priority tier
```

---

## Scaling Patterns

### Single Instance (Development)

```
┌─────────────────┐
│  grimnirradio   │──── Local filesystem
└────────┬────────┘
         │
    ┌────┴─────┐
    │ SQLite DB│
    └──────────┘
```

- In-memory event bus
- SQLite database
- Local filesystem storage
- No external dependencies

### Small Production (1-3 Stations)

```
┌─────────────────┐
│  grimnirradio   │──── S3 (optional)
└────────┬────────┘
         │
    ┌────┴─────┬──────────┐
    │PostgreSQL│   Redis  │
    └──────────┴──────────┘
```

- Single control plane instance
- PostgreSQL for reliability
- Redis for future scaling
- S3 for distributed media access

### Multi-Instance (10+ Stations)

```
┌────────┐  ┌────────┐  ┌────────┐
│ API #1 │  │ API #2 │  │ API #3 │
│(Leader)│  │        │  │        │
└───┬────┘  └───┬────┘  └───┬────┘
    │           │           │
    └───────────┴───────────┘
                │
    ┌───────────┴───────────┬──────────┐
    │                       │          │
┌───▼────┐          ┌───────▼───┐  ┌──▼────┐
│Postgres│          │   Redis   │  │  S3   │
│Replica │          │  Cluster  │  │Bucket │
└────────┘          └───────────┘  └───────┘
```

- **Load Balancer** (nginx/haproxy) distributes HTTP requests
- **Leader Election** via Redis - only leader runs scheduler
- **Executor Distribution** - stations hashed to specific instances
- **Redis Cluster** for event bus reliability
- **S3** for shared media access across instances
- **PostgreSQL Replication** for database HA

---

## Security Architecture

### Authentication

**JWT-Based:**
```
Client                 Control Plane
  │                         │
  ├──── POST /auth/login ────▶
  │                         │
  ◀─── JWT Token ───────────┤
  │                         │
  ├──── API Request ────────▶
  │    (Authorization: Bearer TOKEN)
  │                         │
  ◀─── Response ────────────┤
```

**Roles:**
- **Admin**: Full system access
- **Manager**: Station management
- **DJ**: Live broadcasting only

### Network Security

**Recommended Firewall Rules:**
```
# Public (clients)
80/tcp    - HTTP redirect to HTTPS
443/tcp   - HTTPS (API)
8000/tcp  - Icecast2 streams

# Internal (between services)
5432/tcp  - PostgreSQL (control plane ↔ database)
6379/tcp  - Redis (control plane ↔ Redis)
9091/tcp  - gRPC (control plane ↔ media engine)
```

**TLS Configuration:**
- API: Use reverse proxy (nginx/traefik) for TLS termination
- Database: Enable SSL connections (`sslmode=require`)
- Redis: Use AUTH password
- Icecast: Enable HTTPS for streams

---

## Performance Considerations

### Database Optimization

**Indexes:**
```sql
-- Schedule lookups
CREATE INDEX idx_schedule_station_time ON schedule_entries(station_id, starts_at);

-- Media queries
CREATE INDEX idx_media_station ON media_items(station_id);
CREATE INDEX idx_media_genre ON media_items(genre);

-- Smart block rules
CREATE INDEX idx_smart_blocks_station ON smart_blocks(station_id);
```

**Connection Pooling:**
```bash
GRIMNIR_DB_MAX_OPEN_CONNS=25
GRIMNIR_DB_MAX_IDLE_CONNS=10
GRIMNIR_DB_CONN_MAX_LIFETIME=300s
```

### GStreamer Tuning

**Buffer Configuration:**
```bash
MEDIAENGINE_BUFFER_SIZE=2097152      # 2MB buffer
MEDIAENGINE_BUFFER_MIN=65536         # 64KB minimum
MEDIAENGINE_BUFFER_MAX=10485760      # 10MB maximum
```

**CPU Affinity:**
- Pin media engine to specific CPU cores for consistent performance
- Avoid CPU core sharing with control plane

### Redis Tuning

**Memory Management:**
```redis
maxmemory 256mb
maxmemory-policy allkeys-lru
```

**Persistence:**
```redis
save ""                  # Disable RDB snapshots
appendonly yes           # Enable AOF for events
appendfsync everysec     # Balance durability/performance
```

---

## Monitoring & Observability

### Metrics (Prometheus)

**Exposed at:** `http://localhost:9000/metrics`

**Key Metrics:**
```
grimnir_scheduler_generation_duration_seconds
grimnir_executor_state_changes_total
grimnir_media_engine_connection_status
grimnir_api_request_duration_seconds
grimnir_db_query_duration_seconds
grimnir_event_bus_publishes_total
grimnir_priority_transitions_total
```

### Tracing (OpenTelemetry)

**Enable:**
```bash
GRIMNIR_TRACING_ENABLED=true
GRIMNIR_OTLP_ENDPOINT=localhost:4317
GRIMNIR_TRACING_SAMPLE_RATE=0.1
```

**Traces:**
- API request spans
- Schedule generation spans
- Smart block execution spans
- gRPC call spans

### Logging

**Structured JSON Logs:**
```json
{
  "level": "info",
  "time": "2026-01-23T03:00:00Z",
  "component": "scheduler",
  "station_id": "abc123",
  "message": "schedule generation complete",
  "duration_ms": 45
}
```

**Log Levels:**
- `debug`: Verbose, development only
- `info`: Normal operations
- `warn`: Recoverable errors, degraded state
- `error`: Errors requiring attention

---

## Further Reading

- [Configuration Guide](Configuration) - Detailed configuration options
- [Multi-Instance Setup](Multi-Instance) - Horizontal scaling guide
- [Observability](Observability) - Monitoring and alerting
- [Performance Tuning](Performance-Tuning) - Optimization guide
- [Engineering Spec](Engineering-Spec) - Deep technical details
