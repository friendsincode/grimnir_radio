# Grimnir Radio — Sales Spec

**Version:** 1.3.1

This document describes Grimnir Radio's value proposition, target customers, and capabilities. Features are marked as **✓ IMPLEMENTED** or **⏳ PLANNED**.

---

## Production Deployment

Grimnir Radio powers **[rlmradio.xyz](https://rlmradio.xyz)**, a community radio station honoring the legacy of Grimnir who dedicated his work to the community.

---

## One-Liner

Professional radio automation where Go owns the control plane and a dedicated media engine owns real-time audio—deterministic scheduling, seamless live handover, and consistent sound without Liquidsoap scripting complexity.

---

## Current Status

**Production Release (1.3.1)** - Full-featured broadcast automation system. Production-ready.

**Core Features (All Implemented):**
- Smart scheduling with rule-based playlist generation (deterministic, reproducible)
- 48-hour rolling schedule automation with clock templates
- Multi-station/multi-mount architecture with isolation
- API key authentication with role-based access (admin, manager, DJ)
- Media upload and metadata management
- HTTP JSON API with WebSocket events for real-time updates
- PostgreSQL/MySQL/SQLite database support
- **Two-binary architecture**: Control plane + separate media engine
- **GStreamer-based media engine** with gRPC control interface
- **5-tier priority system** (Emergency > Live Override > Live Scheduled > Automation > Fallback)
- **Graph-based DSP pipeline**: Loudness normalization, AGC, compression, limiting
- **Redis/NATS event bus** for multi-instance support and fault isolation
- **Webstream relay** with failover chains for external streams
- **Migration tools**: One-command import from AzuraCast/LibreTime
- **Live DJ input** with token-based authorization (Icecast, RTP, SRT)
- **Full observability**: Prometheus metrics, OpenTelemetry tracing, alerting
- **Horizontal scaling** with consistent hashing and leader election
- **Turn-key deployment**: Docker Compose, Kubernetes, Nix
- **Audit logging** for sensitive operations

**What's Coming:**
- **WebDJ**: Browser-based DJ control panel
- **Emergency Alert System (EAS)** integration
- **Advanced scheduling**: Conflict detection, templates

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

### ✓ Additional Solved Problems

**Liquidsoap Complexity = Development Bottleneck**
- **Problem:** Liquidsoap scripting (OCaml-based DSL) requires specialized expertise; hard to debug and maintain
- **Solution:** Go control plane + separate media engine with gRPC interface; declarative YAML configuration (no scripting DSL)
- **Benefit:** Easier to understand, extend, and maintain; standard programming tools work

**Inconsistent Loudness = Listener Fatigue**
- **Problem:** Volume jumps between tracks harm listener experience
- **Solution:** Graph-based DSP pipeline with loudness normalization (EBU R128/ATSC A/85); analysis at ingest, enforcement at playout
- **Benefit:** Consistent volume across entire broadcast; professional sound quality

**Fragile Live Handovers = Dead Air**
- **Problem:** DJ transitions fail, causing embarrassing gaps; no priority system for emergency content
- **Solution:** 5-tier priority ladder (Emergency > Live Override > Live Scheduled > Automation > Fallback) with seamless crossfades
- **Benefit:** Sub-3-second transitions; emergency content always takes priority; no dead air

**Monolithic Failures = Total Outage**
- **Problem:** Single component crash takes down entire broadcast
- **Solution:** Process separation (Control Plane + Media Engine) with isolated failure domains; one process crash doesn't kill others
- **Benefit:** Improved reliability; graceful degradation; easier debugging

**Migration from Legacy = Downtime Risk**
- **Problem:** Switching platforms means hours of manual data entry
- **Solution:** One-command import from AzuraCast/LibreTime backups; dry-run preview with diff report
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
- API key authentication with role-based access (admin, manager, DJ)
- **vs. Legacy Systems:** Can build custom frontends, mobile apps, integrations

**Cloud or On-Prem Flexibility**
- SQLite for single-node (community stations, testing)
- PostgreSQL/MySQL for production multi-station deployments
- S3-compatible object storage or filesystem
- **vs. Cloud-Only:** No vendor lock-in; run anywhere

### ✓ Additional Advantages

**Process Separation Architecture**
- Control Plane and Media Engine run as separate processes
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
- API key authentication (configurable expiration up to 1 year)
- Role-based access control (RBAC):
  - **Admin:** Full system access
  - **Manager:** Station management, programming, analytics
  - **DJ:** Media upload, playout skip
- API keys managed via web dashboard profile page
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

### ✓ IMPLEMENTED (Post-1.0)

**Process Architecture**
- Control Plane (HTTP/WebSocket/gRPC server, authentication, routing, scheduling)
- Media Engine (separate binary, GStreamer-based, gRPC controlled)
- Executor Pool (per-station goroutines, state management)
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

**Observability**
- Complete Prometheus metrics (latency, throughput, errors)
- Distributed tracing (OpenTelemetry)
- Performance profiling endpoints
- Alerting integration (AlertManager)
- Schedule gap detection alerts
- Pipeline health monitoring

**User Management**
- User CRUD API (admin)
- Audit logging for sensitive operations
- Per-user activity tracking

**Deployment Options**
- Docker Compose (turn-key with Icecast2)
- Kubernetes manifests
- Nix flakes (Basic, Full NixOS, Dev flavors)
- Systemd service files

### ⏳ PLANNED

**Smart Block Enhancements**
- Energy curves (ramp up/down energy over hour)
- Advanced scoring algorithms (weighted multi-factor)
- Mood-based transitions
- Time-of-day awareness

**User Management Enhancements**
- Password reset flow
- Optional OIDC/SSO integration

**WebDJ Interface**
- Browser-based DJ control panel
- Live streaming from browser
- Playlist management
- Voice tracking

**Emergency Alert System (EAS)**
- EAS message parsing
- Automatic broadcast interruption
- Compliance logging

---

## Packaging & Deployment

### ✓ IMPLEMENTED

**Two-Binary Architecture**
- `cmd/grimnirradio` → `./grimnirradio` (Control Plane)
- `cmd/mediaengine` → `./mediaengine` (Media Engine)
- All migrations embedded
- gRPC communication between processes
- Separate failure domains (one crash ≠ total outage)

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

**Deployment Options**
- **Nix (Recommended)**: Reproducible builds, three flavors (Basic, Full NixOS, Dev)
- **Docker Compose**: Turn-key with intelligent port detection, external storage/database support
- **Kubernetes**: Full manifests with process separation
- **Systemd**: Service files for bare metal deployment
- Environment variable configuration
- GStreamer 1.0 required (plugins: base, good, ugly)

**High Availability**
- Load-balanced API instances
- Leader election for executor distribution
- Consistent hashing (CRC32, 500 virtual nodes)
- Event bus via Redis Pub/Sub or NATS
- Shared PostgreSQL with replication
- Shared media storage (S3/NFS)

### ⏳ FUTURE

**Cloud Deployment**
- One-click AWS/GCP/Azure deployment templates
- Managed database integration guides
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

### "Is it production-ready?"

**Response:**
- **Yes:** Version 1.3.1 is production-ready and powers [rlmradio.xyz](https://rlmradio.xyz)
- **What works:** All core features including scheduling, smart blocks, API, authentication, two-process architecture, 5-tier priority system, live input, webstream relay, migration tools, and full observability
- **What's coming:** WebDJ interface, EAS integration, advanced scheduling

**When to Consider:**
- 24/7 community or commercial broadcast
- Migration from AzuraCast or LibreTime
- Multi-station hosting with centralized control

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
- API key authentication with configurable expiration (up to 1 year)
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

### ✓ IMPLEMENTED (Post-1.0)

**Enhanced Security:**
- Audit logging for sensitive operations (priority, live, API keys, webstreams)
- API key authentication with configurable expiration

### ⏳ PLANNED

**Enhanced Security:**
- Optional OIDC/SSO integration
- Rate limiting on public endpoints
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

### ✓ Production Deployment

**Prerequisites:**
- Migration from existing system (using import tools)
- Complete playout pipeline validation
- Monitoring and alerting configured

**Rollout:**
- Per-mount cutover (test before switching)
- Webstream relay for gradual transition
- Rollback plan to existing system
- 1-week validation period

### ✓ Multi-Station Rollout

1. Onboard one station completely
2. Add additional stations incrementally
3. Centralize media library management
4. Build custom integrations via API

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

### ✓ COMPLETE - All Planned Phases

**Phase 0: Foundation Fixes** ✓
- Core control plane, API, authentication
- Smart Blocks and scheduling
- Multi-station/multi-mount
- Media management

**Phase 4A: Executor & Priority System** ✓
- 5-tier priority ladder implementation
- State machine for priority resolution
- Executor state tracking

**Phase 4B: Media Engine Separation** ✓
- Separate `mediaengine` binary
- gRPC interface (protobuf definitions)
- Graph-based DSP pipeline (loudness, AGC, compression, limiting)
- Telemetry stream to control plane
- Process supervision and restart

**Phase 4C: Live Input & Webstream Relay** ✓
- Live handover with priority system (harbor-style)
- Webstream relay with failover chains
- Sub-3-second transitions

**Phase 5: Observability & Multi-Instance** ✓
- Redis Pub/Sub and NATS event bus
- Prometheus metrics, OpenTelemetry tracing
- Leader election with consistent hashing
- Multi-instance executor coordination

**Phase 6: Production Readiness** ✓
- Migration tools (AzuraCast/LibreTime import)
- Docker Compose and Kubernetes deployment
- Load testing and performance optimization

**Phase 7: Nix Integration** ✓
- Reproducible builds via Nix flakes
- Three deployment flavors (Basic, Full NixOS, Dev)

### ⏳ FUTURE PHASES

**Phase 8: WebDJ & Advanced Features**
- Browser-based DJ control panel
- Emergency Alert System (EAS) integration
- Advanced scheduling (conflict detection, templates)

See `docs/ARCHITECTURE_ROADMAP.md` for detailed implementation history.

---

## Summary

Grimnir Radio is a **production-ready** radio automation platform powering [rlmradio.xyz](https://rlmradio.xyz). The architectural vision is realized: **Go owns the control plane, a dedicated media engine owns real-time audio.** All planned phases are complete, including the two-process architecture, 5-tier priority system, graph-based DSP, live input, webstream relay, migration tools, and full observability.

**Best suited for:**
- Community radio stations seeking modern, reliable automation
- Streaming-only stations prioritizing sound consistency and uptime
- Content networks managing multiple stations from a single control plane
- Organizations wanting to migrate from AzuraCast or LibreTime

**Choose Grimnir Radio if you:**
- Value API-first design and automation
- Want deterministic, predictable scheduling with reproducible results
- Prefer maintainable architecture (Go + gRPC + GStreamer) over Liquidsoap scripting complexity
- Need process isolation and fault tolerance
- Plan to build custom integrations or mobile apps
- Want to replace Liquidsoap with declarative configuration

**Consider alternatives if you:**
- Need WebDJ browser-based streaming (coming soon)
- Require Emergency Alert System integration (coming soon)
- Require unlimited DSP customization (Liquidsoap may be better fit)
- Require commercial support contracts (not available yet)

---

## Contact & Next Steps

- **Live Deployment:** [rlmradio.xyz](https://rlmradio.xyz)
- **Repository:** [github.com/friendsincode/grimnir_radio](https://github.com/friendsincode/grimnir_radio)
- **Documentation:** `docs/api/README.md`, `docs/specs/`
- **Issues:** GitHub Issues
