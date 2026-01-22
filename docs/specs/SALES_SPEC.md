# Grimnir Radio — Sales Spec

**Version:** 0.0.1-alpha

This document describes Grimnir Radio's value proposition, target customers, and capabilities. Features are marked as **✓ IMPLEMENTED** or **⏳ PLANNED**.

---

## One-Liner

Professional radio automation where Go owns the control plane and a dedicated media engine owns real-time audio—deterministic scheduling, seamless live handover, and consistent sound without Liquidsoap scripting complexity.

---

## Current Status

**Alpha Release (0.0.1-alpha)** - Core control plane and scheduling implemented, playout integration in progress. NOT production-ready.

**What Works Today:**
- Smart scheduling with rule-based playlist generation (deterministic, reproducible)
- 48-hour rolling schedule automation with clock templates
- Multi-station/multi-mount architecture with isolation
- JWT authentication with role-based access (admin, manager, DJ)
- Media upload and metadata management
- HTTP JSON API with WebSocket events for real-time updates
- PostgreSQL/MySQL/SQLite database support

**What's Coming:**
- **Process separation**: API Gateway, Planner, Executor Pool, Media Engine (separate binaries)
- **Media engine**: GStreamer-based with gRPC control interface (not embedded)
- **Priority system**: 5-tier ladder (Emergency > Live > Scheduled > Automation > Fallback)
- **DSP pipeline**: Graph-based loudness normalization, AGC, compression, limiting
- **Event bus**: Redis/NATS for multi-instance support and fault isolation
- **Webstream relay**: Failover chains for external streams
- **Migration tools**: One-command import from AzuraCast/LibreTime
- **WebDJ**: Browser-based DJ control panel

---

## Target Customers

### Primary Markets ✓

**Community Radio Stations**
- Independent and community stations upgrading from manual or legacy tools
- Campus and low-power FM/online stations needing modern scheduling
- Need: Reliable automation without enterprise pricing
- Pain point: Current tools (LibreTime, AzuraCast) are complex or unstable

**Streaming-Only Stations**
- Online-only broadcasters prioritizing sound consistency and uptime
- Podcast networks expanding into live streaming
- Need: Simple, deterministic scheduling
- Pain point: Liquidsoap scripting is too complex for non-developers

**Content Networks**
- Syndicating shows across multiple stations/mounts
- Managing multiple related stations from single control plane
- Need: Multi-tenant architecture, efficient media management
- Pain point: Running separate instances for each station is costly

### ⏳ Future Markets

**Commercial Broadcasters** (requires production hardening)
- Small-to-medium commercial stations
- Broadcast groups managing multiple markets
- Need: Enterprise-grade uptime, compliance reporting
- Pain point: Enterprise solutions are expensive; open-source lacks support

**Educational Institutions**
- College radio training programs
- High school media programs
- Need: Easy-to-learn interface, educational focus
- Pain point: Professional tools have steep learning curves

---

## Problems We Solve

### ✓ Currently Solving

**Complex Scheduling = Wasted Time**
- **Problem:** Manual playlist creation takes hours each week
- **Solution:** Smart Blocks with rule-based generation (genre, mood, BPM, separation rules)
- **Benefit:** 15-minute music block generated in < 200ms; deterministic and repeatable

**Unpredictable Automation = On-Air Mistakes**
- **Problem:** Schedule gaps, unexpected tracks, violated rotation rules
- **Solution:** 48-hour rolling schedule with validation; clock templates with stopsets
- **Benefit:** See exactly what will play; modify schedule entries in real-time

**Multi-Station Complexity = Operational Overhead**
- **Problem:** Managing multiple stations requires separate tools and workflows
- **Solution:** Multi-station architecture with per-station isolation
- **Benefit:** Single login, unified media library, station-specific scheduling

**No API = Limited Integration**
- **Problem:** Can't build custom tools or integrate with existing systems
- **Solution:** Complete RESTful JSON API with WebSocket events
- **Benefit:** Build custom dashboards, mobile apps, third-party integrations

### ⏳ Planned Solutions

**Liquidsoap Complexity = Development Bottleneck**
- **Problem:** Liquidsoap scripting (OCaml-based DSL) requires specialized expertise; hard to debug and maintain
- **Solution (planned):** Go control plane + separate media engine with gRPC interface; declarative YAML configuration (no scripting DSL)
- **Benefit:** Easier to understand, extend, and maintain; standard programming tools work

**Inconsistent Loudness = Listener Fatigue**
- **Problem:** Volume jumps between tracks harm listener experience
- **Solution (planned):** Graph-based DSP pipeline with loudness normalization (EBU R128/ATSC A/85); analysis at ingest, enforcement at playout
- **Benefit:** Consistent volume across entire broadcast; professional sound quality

**Fragile Live Handovers = Dead Air**
- **Problem:** DJ transitions fail, causing embarrassing gaps; no priority system for emergency content
- **Solution (planned):** 5-tier priority ladder (Emergency > Live Override > Live Scheduled > Automation > Fallback) with seamless crossfades
- **Benefit:** Sub-3-second transitions; emergency content always takes priority; no dead air

**Monolithic Failures = Total Outage**
- **Problem:** Single component crash takes down entire broadcast
- **Solution (planned):** Process separation (API Gateway, Planner, Executor Pool, Media Engine) with isolated failure domains; one process crash doesn't kill others
- **Benefit:** Improved reliability; graceful degradation; easier debugging

**Migration from Legacy = Downtime Risk**
- **Problem:** Switching platforms means hours of manual data entry
- **Solution (planned):** One-command import from AzuraCast/LibreTime backups; dry-run preview with diff report
- **Benefit:** Per-mount cutover with zero/low downtime; validated migration

---

## Key Differentiators

### ✓ Current Advantages

**Deterministic Smart Blocks**
- Clock-aware, rule-driven programming with predictable results
- Same seed + rules = same playlist (reproducible for testing/validation)
- Separation windows prevent artist/title repeats (configurable minutes)
- Quota enforcement (e.g., max 2 tracks per artist per hour)
- **vs. AzuraCast:** More flexible rules; deterministic generation
- **vs. LibreTime:** Better performance; clearer rule syntax

**Clean Go Control Plane**
- Go-based control plane (API, scheduling, database) separate from audio processing
- Clean HTTP+WebSocket API with complete documentation
- Multi-database support (PostgreSQL, MySQL, SQLite)
- Single or multi-instance deployment (with Redis/NATS event bus)
- **vs. AzuraCast:** Clearer separation of concerns; easier to extend
- **vs. Custom Liquidsoap:** No scripting required; proper API surface; maintainable codebase

**Modern API-First Design**
- RESTful JSON API for all operations
- WebSocket events for real-time updates (now-playing, health, schedule changes)
- JWT authentication with role-based access (admin, manager, DJ)
- **vs. Legacy Systems:** Can build custom frontends, mobile apps, integrations

**Cloud or On-Prem Flexibility**
- SQLite for single-node (community stations, testing)
- PostgreSQL/MySQL for production multi-station deployments
- S3-compatible object storage or filesystem
- **vs. Cloud-Only:** No vendor lock-in; run anywhere

### ⏳ Planned Advantages

**Process Separation Architecture**
- API Gateway, Planner, Executor Pool, Media Engine run as separate processes
- Isolated failure domains: one component crash doesn't kill broadcast
- gRPC control interface between Go control plane and media engine
- Event bus (Redis/NATS) for multi-instance coordination
- **vs. Monoliths:** Better reliability; easier debugging; graceful degradation
- **vs. Liquidsoap:** Clear separation of concerns; maintainable components

**Priority-Based Handover**
- 5-tier priority ladder (Emergency 0 > Live Override 1 > Live Scheduled 2 > Automation 3 > Fallback 4)
- Automatic priority resolution with state machine
- Sub-3-second transitions without underruns or dead air
- Emergency content always takes precedence
- **vs. LibreTime:** More reliable; explicit priority handling; better DJ experience
- **vs. Manual Switching:** Automated failover; no operator error

**Professional DSP Pipeline**
- Graph-based DSP (not scripting): Decode → Loudness → AGC → Compressor → Limiter → Encode → Outputs
- EBU R128 and ATSC A/85 loudness normalization
- True peak limiting to prevent clipping
- Per-output isolation (one failure ≠ all outputs fail)
- Telemetry stream for real-time monitoring
- **vs. AzuraCast:** Professional broadcast-grade audio processing
- **vs. Liquidsoap:** Declarative configuration (YAML) instead of scripting DSL

**Effortless Migration**
- Import AzuraCast/LibreTime backups with single command
- Dry-run preview with diff report
- Per-mount cutover (test before switching)
- **vs. Manual Migration:** Hours → minutes; validated import

**Webstream Relay with Failover**
- Schedule external HTTP/ICY streams in clocks
- Health monitoring with automatic reconnect
- Backup URL chains for redundancy (e.g., primary DJ stream → backup Icecast → local programming)
- **vs. Manual Streams:** Automatic failover; no dead air

---

## Core Capabilities

### ✓ IMPLEMENTED

**Scheduling & Programming**
- 48-hour rolling window (configurable lookahead)
- Clock templates with slots (smart blocks, hard items, stopsets)
- Clock simulation (preview before committing)
- Schedule refresh API for manual rebuilds
- Schedule entry updates (change times, metadata, mount)
- Real-time WebSocket events for schedule changes

**Smart Blocks (Intelligent Playlists)**
- Rule-based track selection (genre, mood, artist, language, BPM, year, explicit, tags)
- Filter operators: includes, excludes, between, equals
- Quota enforcement: min/max counts per field
- Separation windows: artist/title/album/label repeat prevention (minutes)
- Deterministic generation via seeded random (test/validate playlists)
- Dry-run materialize for testing (seed + duration → track list)

**Media Management**
- Multipart file upload (admin, manager, DJ roles)
- Metadata fields: title, artist, album, genre, mood, label, language, BPM, year
- Tag system for categorization (many-to-many)
- Analysis job queue (loudness, cue points, waveform)
- Analysis states: pending, running, complete, failed
- Storage: filesystem or S3-compatible object storage

**Authentication & Access Control**
- JWT-based authentication (15-minute token TTL)
- Role-based access control (RBAC):
  - **Admin:** Full system access
  - **Manager:** Station management, programming, analytics
  - **DJ:** Media upload, playout skip
- Token refresh endpoint for session extension
- Bcrypt password hashing

**Multi-Station Architecture**
- Isolated stations with separate scheduling
- Multiple mounts per station (different formats/bitrates)
- Station-specific timezone support
- Per-station media libraries

**Live Input**
- Live source authorization API
- Handover triggering (manager/admin)
- Event bus integration for DJ connect/disconnect

**Playout Control**
- Pipeline reload (restart with new GStreamer command)
- Skip current track (DJ, manager, admin)
- Stop playout (manager, admin)
- Basic GStreamer pipeline management

**Analytics & Reporting**
- Now-playing tracking (most recent play)
- Spins report (play history grouped by artist/title)
- Play history with timestamps and metadata
- Time-range filtering for reports

**Events & Real-Time**
- WebSocket endpoint with event type filtering
- Event types: now_playing, health, schedule.update, dj.connect
- Ping/pong for connection health
- Webhook ingestion for external systems

**Health & Monitoring**
- Health check endpoints (/healthz, /api/v1/health)
- Structured logging (Zerolog, JSON in production)
- Request ID propagation
- Metrics endpoint placeholders

### ⏳ PLANNED

**Process Architecture**
- API Gateway (HTTP/WebSocket server, authentication, routing)
- Planner (timeline generation, Smart Block materialization)
- Executor Pool (per-station goroutines, state management)
- Media Engine (separate binary, GStreamer-based, gRPC controlled)
- Event Bus (Redis Pub/Sub or NATS for inter-process communication)
- Isolated failure domains (one crash doesn't kill broadcast)

**Priority System**
- 5-tier priority ladder: Emergency (0) → Live Override (1) → Live Scheduled (2) → Automation (3) → Fallback (4)
- Automatic priority resolution with state machine
- Preemption rules (higher priority interrupts lower)
- Crossfade timing per priority level
- Priority source tracking and telemetry

**Media Engine (Separate Process)**
- GStreamer-based DSP pipeline (not embedded)
- gRPC control interface (protobuf-defined)
- Graph-based DSP: Decode → Loudness → AGC → Compressor → Limiter → Encode → Outputs
- EBU R128 and ATSC A/85 loudness normalization
- True peak limiting to prevent clipping
- Per-output isolation (multiple Icecast/HTTP outputs, independent failure)
- Bidirectional telemetry stream (levels, underruns, state)
- Declarative YAML configuration (no audio scripting DSL)
- Automatic restart on crash (supervised by control plane)

**Webstreams**
- Schedule external HTTP/ICY streams in clocks
- Health probing with bounded retry (exponential backoff)
- Fallback URL chains for redundancy
- Metadata passthrough (ICY StreamTitle)
- Preflight connection (validate before slot start)
- Grace window failover (auto-switch to backup if primary fails)

**Media Analysis**
- Complete loudness analysis (LUFS, ReplayGain)
- Automatic cue point detection (intro/outro)
- BPM extraction via beat detection
- Waveform preview generation
- Parallel worker pool with backpressure
- Resumable analysis jobs

**Migration & Import**
- AzuraCast backup import (CLI + API)
- LibreTime backup import (CLI + API)
- Dry-run mode with diff preview
- Station/mount/media/playlist mapping
- Per-mount cutover with rollback
- Progress tracking via WebSocket events

**Smart Block Enhancements**
- Energy curves (ramp up/down energy over hour)
- Historical play data integration (avoid recently played)
- Advanced scoring algorithms (weighted multi-factor)
- Mood-based transitions
- Time-of-day awareness

**Observability**
- Complete Prometheus metrics (latency, throughput, errors)
- Distributed tracing (OpenTelemetry)
- Performance profiling endpoints
- Alerting integration (AlertManager)
- Schedule gap detection alerts
- Pipeline health monitoring

**User Management**
- User CRUD API (admin)
- Password reset flow
- Optional OIDC/SSO integration
- Audit logging for sensitive operations
- Per-user activity tracking

**WebDJ Interface**
- Browser-based DJ control panel
- Live streaming from browser
- Playlist management
- Voice tracking

---

## Packaging & Deployment

### ✓ CURRENT

**Single Binary (Monolithic Phase)**
- `cmd/grimnirradio` → `./grimnirradio` (Go binary)
- All migrations embedded
- Basic GStreamer integration (control plane launches pipelines)

**Databases**
- PostgreSQL (preferred for production)
- MySQL (supported)
- SQLite (development/single node)

**Media Storage**
- Filesystem (simple deployments)
- S3-compatible (distributed/cloud)

**Platform Support**
- Linux first (x86_64 and ARM64)
- Runs on modern Linux distributions (Ubuntu 20.04+, Debian 11+)

**Deployment**
- systemd service
- Environment variable configuration
- GStreamer 1.0 required (plugins: base, good, ugly)

### ⏳ FUTURE

**Multi-Process Architecture**
- `grimnirradio` binary (API Gateway + Planner + Executor Pool)
- `mediaengine` binary (separate process, GStreamer-based)
- systemd service files for each process
- gRPC communication between processes
- Redis/NATS event bus for multi-instance coordination
- Separate failure domains (one crash ≠ total outage)

**Containerization**
- Docker images: `grimnirradio:latest` and `mediaengine:latest`
- Docker Compose for full stack (control plane + media engine + postgres + redis + icecast)
- Kubernetes manifests with process separation
- Helm charts with HA configuration

**High Availability**
- Load-balanced API Gateway instances
- Multiple Planner instances with leader election
- Executor Pool scaled across instances (via event bus)
- Shared PostgreSQL with replication
- Redis Sentinel or NATS cluster for event bus HA
- Shared media storage (S3/NFS)

**Cloud Deployment**
- One-click AWS/GCP/Azure deployment
- Managed database integration
- CDN integration for media delivery

---

## Migration Path

### ⏳ PLANNED - AzuraCast & LibreTime Import

**Supported Sources:**
- AzuraCast (v0.13+)
- LibreTime (v3.0+)

**What Migrates:**
- ✓ Stations, mounts, encoder presets
- ✓ Media metadata (title, artist, album, etc.)
- ✓ Playlists (converted to Smart Blocks where possible)
- ✓ Basic schedules (mapped to clocks + entries)
- ✓ DJ accounts (where available)
- ⚠ Complex rules flagged for manual review

**Migration Approaches:**

1. **Same-Server Takeover** (lowest risk)
   - Detect existing AzuraCast/LibreTime on same host
   - Stop source services, snapshot DB
   - Import in place, move media
   - Validate on staging mount
   - Switch ports/proxy when ready
   - Rollback to source if issues

2. **Cross-Server Migration** (zero downtime)
   - Connect to remote AzuraCast/LibreTime via API
   - Sync media incrementally (rsync/S3/HTTP)
   - Import metadata and schedules
   - Configure webstream relay to test
   - Per-mount final switch

3. **Backup Import** (offline migration)
   - Upload backup archive (.tar.gz)
   - Dry-run preview with diff report
   - Apply import transactionally
   - Resume on failure

**Tooling:**
- CLI: `grimnirradio import azuracast --backup /path/backup.tar.gz --dry-run`
- CLI: `grimnirradio import libretime --backup /path/backup.tar.gz --apply`
- API: `POST /api/v1/migrations/azuracast` with job tracking

**Cutover Strategy:**
- Stage on non-public mount
- Validate with webstream relay
- Per-mount switchover with grace window
- Fallback to original if needed

**Success Rate:** Target 95%+ field mapping for typical installations

---

## Success Metrics / ROI

### ✓ Current Measurable Benefits

**Time Savings:**
- Smart Block generation: < 200ms for 15-minute block (vs. 15+ minutes manual)
- Schedule preview: instant simulation (vs. waiting for playout to test)
- Multi-station management: single login vs. multiple dashboards

**Operational Reliability:**
- Deterministic scheduling: predictable playlists, no surprises
- 48-hour lookahead: catch issues before they air
- Real-time schedule updates: fix mistakes without restarts

**Developer Productivity:**
- Complete JSON API: build custom tools without reverse-engineering
- WebSocket events: real-time integrations without polling
- Single binary: simple deployment and updates

### ⏳ Planned Measurable Benefits

**Audio Quality:**
- Loudness drift < ±1 LU across entire hour (vs. ±5+ LU typical)
- Zero unexpected volume jumps
- Professional sound quality

**Uptime:**
- >= 99.9% playout uptime for single node
- Zero underruns in nominal cases
- Automatic failover (webstreams, pipeline restarts)

**Live Operations:**
- < 3 seconds mean live handover time (vs. 10+ seconds typical)
- Zero dead air during DJ transitions

**Migration Speed:**
- AzuraCast/LibreTime import: < 5 minutes for typical 1000-track library (vs. days of manual work)
- Zero data loss for stations, mounts, media metadata

---

## Competitive Positioning

### vs. LibreTime/AzuraCast

**Advantages:**
- ✓ **Simpler architecture:** Go monolith vs. PHP/Python + multiple services
- ✓ **Better API:** Complete RESTful JSON vs. limited/inconsistent
- ✓ **Deterministic scheduling:** Predictable Smart Blocks vs. random-only
- ✓ **Multi-database:** PostgreSQL/MySQL/SQLite vs. PostgreSQL-only
- ⏳ **Easier live handover:** (planned) Priority ladder vs. fragile manual switching
- ⏳ **Migration tools:** (planned) One-command import vs. manual

**Trade-offs:**
- **Maturity:** 0.0.1-alpha vs. years of production use
- **Community:** New project vs. established communities
- **Features:** Core implemented, many planned vs. feature-complete
- **Frontend:** Basic HTML vs. full web UI

**Target Audience:** Stations prioritizing API-first design, simplicity, deterministic scheduling over mature feature set

### vs. Custom Liquidsoap Stacks

**Advantages:**
- ✓ **No scripting DSL:** Declarative YAML configuration vs. Liquidsoap scripting language (OCaml-based)
- ✓ **Built-in scheduling:** Smart Blocks with deterministic generation vs. custom scheduling logic
- ✓ **Multi-station support:** Native architecture vs. multiple Liquidsoap instances
- ✓ **Easier to maintain:** Go codebase with standard tooling vs. OCaml + Liquidsoap expertise required
- ⏳ **Process separation:** (planned) Control plane separate from audio engine; isolated failure domains
- ⏳ **gRPC control:** (planned) Standard interface vs. custom protocol or CLI wrapping
- ⏳ **Proper API:** (planned) Complete HTTP/gRPC API vs. reverse-engineering Liquidsoap telnet/harbor
- ⏳ **Migration assistance:** (planned) Import existing configurations

**Trade-offs:**
- **DSP customization:** Graph-based GStreamer configuration vs. Liquidsoap's unlimited scripting
- **Niche features:** General-purpose broadcast automation vs. highly custom audio graph
- **Maturity:** New platform vs. Liquidsoap's decades of production use
- **Advanced operators:** Standard DSP operators vs. Liquidsoap's comprehensive operator library

**Design Philosophy:**
- **Go owns the control plane:** API, scheduling, database, business logic
- **Media engine owns real-time audio:** DSP graph, mixing, encoding, output
- **Declarative configuration:** YAML/JSON instead of scripting DSL
- **Standard protocols:** gRPC/HTTP instead of custom telnet/harbor interfaces

**Target Audience:** Stations wanting maintainable, API-driven automation over unlimited DSP flexibility; teams without Liquidsoap expertise; organizations prioritizing long-term maintainability

### vs. Commercial Solutions (PlayIt Live, SAM Broadcaster, etc.)

**Advantages:**
- ✓ **Open source:** Free vs. licensing fees
- ✓ **API-first:** Full automation vs. GUI-focused
- ✓ **Cloud or on-prem:** Flexible deployment vs. desktop-only
- ✓ **Modern stack:** Go vs. legacy Windows tech

**Trade-offs:**
- **Support:** Community vs. paid support
- **Maturity:** Alpha vs. decades of development
- **Features:** Core vs. comprehensive
- **Training:** Limited docs vs. extensive tutorials

**Target Audience:** Stations prioritizing cost, flexibility, API access over commercial support and mature feature set

---

## Objections & Responses

### "We already run AzuraCast/LibreTime"

**Response:**
- Integrate incrementally: use webstream relay to test Grimnir on non-public mount
- Migrate one station at a time (multi-station deployments)
- Use our migration tools (planned): import your backup with dry-run preview
- Keep existing system as fallback during transition

**When to Consider:**
- Frustrated with scheduling unpredictability
- Need better API for custom tools
- Want simpler architecture for easier maintenance
- Prioritize deterministic, reproducible scheduling

### "Is Go OK for audio?"

**Response:**
- **Go owns the control plane:** HTTP API, scheduling, database, business logic (where Go excels)
- **Media engine owns real-time audio:** Separate GStreamer-based process (battle-tested C library used by VLC, professional broadcast tools)
- **Process separation:** Go doesn't do real-time DSP; media engine runs independently with gRPC control interface
- **Right tool for each job:** Go for control/coordination, GStreamer for audio processing
- **Fault isolation:** Media engine crash doesn't kill control plane; automatic restart
- **Industry standard:** This architecture pattern is proven (e.g., Kubernetes controls, but doesn't run, containers)

### "Will our DJs learn it?"

**Response (current):**
- Complete JSON API documentation
- Simple concepts: Smart Blocks, Clocks, Schedules
- Role-based access: DJs get upload/skip permissions only

**Response (planned):**
- WebDJ interface for browser-based control
- Gradual adoption: start with scheduling, add live features later
- Training materials and examples

### "It's alpha software"

**Response (honest):**
- **Correct:** This is 0.0.1-alpha, not production-ready
- **What works:** Core scheduling, smart blocks, API, authentication
- **What's missing:** Complete playout integration, migration tools, WebDJ
- **Best for:** Testing, pilots, non-critical streams
- **Roadmap:** Production hardening in Phase 6

**When to Wait:**
- Critical 24/7 broadcast (wait for beta/1.0)
- Need migration tools immediately (wait for Phase 6)
- Want webstream support (wait for Phase 4)

### "We need commercial support"

**Response:**
- **Currently:** Community support via GitHub Issues
- **Future:** Consider commercial support offerings (TBD)
- **Alternative:** Hire consultant for setup/training
- **Self-service:** Complete API documentation, architecture specs

### "What if the project dies?"

**Response:**
- **Open source:** Fork and maintain yourself if needed
- **Simple architecture:** Go monolith easier to understand than complex multi-service stacks
- **Migration:** Use export tools to move to other platforms (planned)
- **Database:** Standard PostgreSQL/MySQL → portable

---

## Security & Compliance Highlights

### ✓ IMPLEMENTED

**Authentication & Access:**
- JWT-based with 15-minute token expiry
- Bcrypt password hashing (cost 10)
- Role-based access control (admin, manager, DJ)
- Route-level authorization enforcement

**Data Protection:**
- SQL injection protection (GORM parameterization)
- No passwords in logs
- Request ID tracking for audit

**Best Practices:**
- TLS via reverse proxy (nginx/caddy)
- Metrics endpoint localhost-only by default
- Database credentials via environment variables

### ⏳ PLANNED

**Enhanced Security:**
- Optional OIDC/SSO integration
- API key authentication for webhooks
- Rate limiting on public endpoints
- Audit logging for sensitive operations
- IP allowlisting for admin endpoints

**Compliance:**
- Aircheck recording for FCC compliance
- Spin count reports for BMI/ASCAP/SESAC
- Export formats for regulatory reporting

---

## Buying Path / Next Steps

### Evaluation (2-4 weeks)

**Week 1-2: Installation & Setup**
1. Deploy on test server (PostgreSQL recommended)
2. Create station, mount, test media upload
3. Build Smart Block, create clock, simulate schedule
4. Review API documentation, test endpoints
5. Set up GStreamer playout (basic)

**Week 3-4: Pilot**
1. Import 100-200 tracks with metadata
2. Build realistic clocks for daily schedule
3. Test schedule refresh and manual updates
4. Evaluate Smart Block rule flexibility
5. Test live handover (basic)

**Deliverables:**
- Working 48-hour schedule
- Smart Block playlists matching station format
- API integration proof-of-concept
- Performance assessment

### ⏳ Production Deployment (Planned for Beta/1.0)

**Prerequisites:**
- Migration from existing system (using import tools)
- Complete playout pipeline validation
- WebDJ setup for remote DJs
- Monitoring and alerting configured

**Rollout:**
- Per-mount cutover (test before switching)
- Webstream relay for gradual transition
- Rollback plan to existing system
- 1-week validation period

### ⏳ Multi-Station Rollout (Planned for 1.0+)

1. Onboard one station completely
2. Add additional stations incrementally
3. Centralize media library management
4. Build custom integrations via API
5. Train staff on WebDJ interface

---

## Pricing & Licensing

**License:** Open source (check repository for specific license)

**Cost:** Free (self-hosted)

**Support Options:**
- Community: GitHub Issues (free)
- Commercial support: To be determined (future)

**Infrastructure Costs (estimate for self-hosted):**
- VPS: $20-100/month (depends on size)
- PostgreSQL: Included or $10-50/month (managed)
- S3 storage: $5-20/month for 10,000 tracks
- Total: ~$35-170/month for typical station

**vs. Commercial:** $200-500/month typical for radio automation SaaS

---

## Roadmap Summary

### Phase 1-3: ✓ PARTIALLY COMPLETE (Current - 0.0.1-alpha)
- Core control plane, API, authentication
- Smart Blocks and scheduling
- Multi-station/multi-mount
- Basic playout controls
- Media management

### Phase 4A: ⏳ NEXT (Executor Refactor - Months 1-2)
- Priority system implementation (5-tier ladder)
- State machine for priority resolution
- In-memory event bus (foundation for Redis/NATS later)
- Executor state tracking

### Phase 4B: ⏳ (Media Engine Separation - Months 2-3)
- Separate `mediaengine` binary
- gRPC interface (protobuf definitions)
- Graph-based DSP pipeline (loudness, AGC, compression, limiting)
- Telemetry stream to control plane
- Process supervision and restart

### Phase 4C: ⏳ (Live & Webstreams - Month 4)
- Live handover with priority system
- Webstream relay with failover chains
- Crossfade enhancements (cue-aware)
- Sub-3-second transitions

### Phase 5: ⏳ (Multi-Instance & Scaling - Months 4-5)
- Redis Pub/Sub or NATS event bus
- Load-balanced API Gateway
- Leader election for Planner
- Shared PostgreSQL setup
- Multi-instance executor coordination

### Phase 6: ⏳ (Production Ready - Months 5-8)
- Migration tools (AzuraCast/LibreTime import)
- Complete observability (metrics, traces, logs)
- Performance optimization and profiling
- Production deployment guides
- WebDJ interface
- Beta release → 1.0

**Timeline:** 6-8 months from 0.0.1-alpha to production-ready 1.0 release

See `docs/ARCHITECTURE_ROADMAP.md` for detailed implementation plan.

---

## Summary

Grimnir Radio is a **promising but early-stage** radio automation platform with a clear architectural vision: **Go owns the control plane, a dedicated media engine owns real-time audio.** The core scheduling engine, Smart Blocks, and API are implemented and functional. The planned multi-process architecture (API Gateway, Planner, Executor Pool, Media Engine) with priority-based handover and graph-based DSP will provide production-grade reliability and sound quality.

**Best suited today:** Testing, pilots, non-critical streams, evaluating the architecture and API design

**Production readiness:** Expected in 6-8 months (beta/1.0) after completing process separation, priority system, and migration tools

**Choose Grimnir Radio if you:**
- Value API-first design and automation
- Want deterministic, predictable scheduling with reproducible results
- Prefer maintainable architecture (Go + gRPC + GStreamer) over Liquidsoap scripting complexity
- Need process isolation and fault tolerance (planned)
- Are comfortable with alpha-stage software and want to influence development direction
- Plan to build custom integrations or mobile apps
- Want to replace Liquidsoap with declarative configuration

**Wait for later releases if you:**
- Need 24/7 production-critical reliability today (wait for beta/1.0)
- Require complete webstream support with failover (Phase 4C)
- Need migration tools immediately (Phase 6)
- Want comprehensive WebDJ features (Phase 6)
- Require unlimited DSP customization (Liquidsoap may be better fit)
- Require commercial support (not available yet)

---

## Contact & Next Steps

- **Repository:** (check project README for repository URL)
- **Documentation:** `docs/API_REFERENCE.md`, `docs/specs/`
- **Issues:** GitHub Issues
- **Community:** (check project README for community links)
