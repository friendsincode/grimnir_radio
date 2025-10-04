# [Archived] Grimnir Radio Upgrade Programming Spec

## Context
- Original spec targets Grimnir Radio, a Go control plane that drives deterministic scheduling, audio analysis, and GStreamer playout for Icecast/Shoutcast mounts.
- Repository already contains package stubs for scheduler, smart blocks, playout, analyzer, and API but several behaviors are unfinished or loosely defined.
- Goal of this document is to turn the Grimnir Radio product brief into actionable engineering direction and a prioritized backlog for the next delivery cycle.

## Goals
- Deliver a reliable 48-hour rolling schedule with reproducible Smart Block output and hard-stop alignment to clocks.
- Provide API and WebSocket surfaces that frontends and automation scripts can depend on.
- Guarantee consistent loudness and cue metadata across ingestion, scheduling, and playout.
- Support PostgreSQL, MySQL, and SQLite backends with a common migration story and shared query abstractions.

## Non-Goals
- Liquidsoap integration or alternative DSP graph beyond GStreamer pipelines.
- Full podcast CMS replacement (basic feeds only).
- Mobile native apps; responsive web is in-scope but native shells arrive later.

## Current Gaps Observed
- Smart Block engine lacks persistence of rule evaluations and quota memory across mounts.
- Scheduler module does not yet materialize hourly plans or respect stopsets defined in clocks.
- Analyzer package has placeholders for loudness/cue extraction but no concrete job orchestration.
- Playout service needs lifecycle management per mount, crossfade policy handling, and telemetry hooks.
- API layer is missing request validation, RBAC enforcement, and pagination/filters expected by the spec.
- No WebDJ studio, voice-tracking tools, or remote relay management—key parity features.
- Compliance reporting limited to spins; lacks affidavits, royalty summaries, and automated exports.
- Listener experience (requests, podcasts, archives) and media ops (watch folders, bulk edits) remain unspecified.
- Ghost CMS integration absent; no webhook listener to import articles with embedded audio or calendar metadata.

## Architecture Plan

### Service Topology
- `cmd/grimnirradio` boots a monolith that wires config, logging, database connections, object storage clients, and subsystems.
- Each subsystem (`scheduler`, `playout`, `analyzer`, `events`, `live`) registers with a shared event bus to publish state and consume commands.
- Graceful shutdown honors `context.Context` propagation with per-component `Stop()` hooks to drain queues and flush metrics.

### Scheduler & Programming Services
- Maintain a per-station goroutine that assembles schedules in rolling windows (default 48h) and persists to `schedules` table.
- Use `clock.Manager` to expand clock templates into hourly slots; scheduler allocates Smart Blocks, hard elements, and ads.
- Model shows/programs with recurrence rules, seasonal calendars, and host ownership to drive permissions.
- Support voice-track slots that reference prerecorded elements with intro/outro alignment.
- Invoke `smartblock.Engine` with deterministic seeds derived from mount + hour to enable replays.
- Persist plan decisions (track UUID, expected start, cues, energy) and update when live overrides occur.
- Provide reconciliation loop to re-fill when media fails validation or new programming is added.

### Smart Block Engine Enhancements
- Expand rule evaluation to cover quotas, separation, diversity, and fallbacks with configurable penalties.
- Maintain recent play memory by station/mount via `recent_plays` table or in-memory cache backed by DB snapshot.
- Introduce scoring heuristics (beam search width, penalty weights) as config defaults; expose metrics to tune.
- Add dry-run endpoint returning candidate sequences and debug info for UI rule builder.
- Support energy curves and segues tailored for voice-tracked breaks and live-to-auto transitions.

### Analyzer Pipeline
- Implement ingestion queue backed by `analysis_jobs` table with worker goroutines limited by `cfg.Analyzer.MaxConcurrent`.
- Use GStreamer CLI (`gst-launch` or Go bindings) or ffmpeg as a fallback to compute loudness, peak, tempo, and spectral tags.
- Store results in `media_analysis` rows and update media records with cues, intro/outro, and computed loudness offsets.
- Emit analyzer events for UI progress and to trigger scheduler refresh when new assets become playable.
- Provide watch-folder scanners and remote upload ingestion with checksum validation.
- Generate waveform previews and loudness profiles for WebDJ cueing.

### Playout & Resilience Engine
- One pipeline per mount controlled by `playout.Manager`; handles pre-buffering of upcoming tracks and crossfades.
- Build GStreamer graph with configurable sources (file, stream, live input) and sinks (Icecast/Shoutcast).
- Implement transition policies (standard crossfade, back-to-back, hard stop) honoring cue points and loudness adjustments.
- Integrate live override ladder (Emergency > Live DJ > Scheduled > Fallback) with preemption and fallback cues.
- Support remote relay inputs with health monitoring, automatic failover, and fallback playlists.
- Publish now playing, overruns, underruns, and pipeline health over the event bus and WebSocket.
- Enable optional hot-standby nodes with state replication and stream failover.

### WebDJ Studio
- Browser-based studio using WebRTC for low-latency capture, with fallback to Opus over secure websockets.
- Provide cart wall, playlist bin, and library search filtered by station/show permissions.
- Real-time meter bridge, headphone/cue routing, and configurable audio devices per user profile.
- Integrate push-to-talk, profanity delay, and emergency stop tied into playout override ladder.

### Voice Tracking Workflow
- Inline recorder launches from schedule slots, capturing mic audio with automatic leveling and noise gating.
- Allow edit/review of takes, attach to specific breaks, and schedule automatic insertion with cue alignment.
- Approval flow for producers to review/approve before tracks publish to on-air log.
- Support remote talent by syncing required beds/imaging via lightweight download package.

### Listener Experience & Archives
- REST endpoints for listener requests with rate limiting, approval queues, and audit trails.
- Generate podcast feeds and downloadable show archives from scheduled programs and airchecks.
- Provide shareable now-playing widgets, embed player API, and webhook triggers for third-party bots.

### Ghost CMS Integration
- Register webhook endpoint to receive Ghost publish/update events with signature validation.
- Parse article metadata and custom tags to detect embedded audio (MP3/Opus URLs) and scheduling directives (one-off or repeating).
- Create or update calendar entries that map to Smart Blocks or hard elements, including optional recurring show templates.
- Attach article body and hero art as show notes for podcasts and listener archives.
- Support manual override to approve/reject imports and map articles to specific stations or mounts.

### Compliance & Reporting Enhancements
- Automate generation of show affidavits, royalty split summaries, and market-specific exports (SoundExchange, SOCAN, PRS, etc.).
- Track ad play confirmation with digital signatures for traffic systems.
- Provide retention policies and secure exports for legal hold scenarios.

### Clock & Stopset Handling
- Formalize `clock.Template` to support hard elements, Smart Blocks, stopsets, and padding instructions.
- Provide simulation API to render a clock hour with sample media so programmers can validate timings.
- Ensure scheduler reserves slots for hard IDs, promos, ads, and handles tolerance logic around Smart Blocks.

### API & Experience Layer
- Use `net/http` with `chi` or `gorilla/mux`; apply middleware for logging, auth, rate limiting.
- Add JSON request/response schemas with validation (e.g., `github.com/go-playground/validator/v10`).
- Provide resource endpoints: media, smart blocks, clocks, schedules, live, playout control, analytics, and webhooks configuration.
- Extend API for WebDJ studio (session auth, cart management, mic controls), listener requests, and podcast feeds.
- Implement Ghost webhook handlers with signature verification, article parsing, and optional approval queue.
- WebSocket channel streams now playing, schedule updates, job status, WebDJ state, and system health events.
- Version API under `/v1` and document contract in OpenAPI for future client generation.

### Data Access Layer
- Centralize DB interactions in `internal/db` using `sqlc` or handcrafted queries with `database/sql` + `sqlx`.
- Provide repository interfaces for testing: `MediaStore`, `ScheduleStore`, `RuleStore`, `PlayHistoryStore`.
- Implement migrations using `golang-migrate` with drivers for Postgres, MySQL, SQLite; align with `docs/sql` schema files.
- Add context timeouts and retry logic for transient errors.
- Model multi-tenant station isolation (quotas, storage limits) and compliance audit tables.
- Introduce tables for external content imports (Ghost articles, assets, schedule directives) with idempotency keys.

### Storage & Media Handling
- Support S3-compatible object storage with signed upload URLs; fallback to local filesystem for dev.
- Validate uploaded media headers before queuing analysis; store waveform previews for UI.
- Manage quarantine bucket/folder for rejected media with reasons available via API.
- Add watch-folder ingestion, remote uploader agents, and bulk metadata editing workflow.
- Provide archive/export tooling for compliance (pull audio + logs per show).
- Support importing remote media referenced in Ghost articles with checksum verification and deduplication.

### Auth & RBAC
- JWT validation middleware supporting OIDC issuer configuration.
- Role + station scoping enforced at repository layer to prevent data leakage between stations.
- Admin endpoints to manage users, roles, service tokens.
- Extend roles for WebDJ, show producer, and compliance auditor personas.
- Support per-show ACLs for media bins and voice-track approvals.
- Provide scoped API keys or service accounts for Ghost webhook calls with audit logging.

### Observability & Ops
- Structured logging via `zap` or `zerolog` with correlation IDs and subsystem fields.
- Metrics exported via Prometheus: scheduler lag, analyzer queue depth, playout buffer health, live handovers.
- Tracing instrumentation for Smart Block generation and major API calls using OpenTelemetry.
- Health endpoints (`/healthz`, `/readyz`) with dependency checks (DB, object store, GStreamer availability).
- Real-time dashboards for WebDJ latency, relay health, and failover events.
- Monitor Ghost webhook success rates, import latencies, and article-to-schedule conversion metrics.

### Deployment & Configuration
- Configuration via environment variables with optional YAML/JSON file overlay.
- Support Docker deployment with containers for server, DB, optional object storage emulator.
- Provide systemd unit template for bare metal installs.
- Document hardware sizing guidance for single station and multi-station deployments.
- Provide HA deployment recipes (active/standby playout, redundant encoders, backup scheduler).
- Expose configuration for Ghost webhook secrets, publish event filters, and content tagging rules.

### Testing Strategy
- Unit tests covering rule evaluation, scheduler slotting, and API validation.
- Integration tests using ephemeral SQLite database and fake object storage; target core schedule scenarios.
- End-to-end smoke test that spins up playout pipeline with fixture media and asserts audio continuity for one hour.
- Load testing harness for scheduler and API to ensure responsiveness under concurrent programming changes.
- Acceptance tests for WebDJ latency, voice-track timing, and relay failover recovery.
- Contract tests for Ghost webhook ingestion, article parsing edge cases, and recurring schedule generation.

### Risks & Mitigations
- GStreamer integration complexity: start with minimal pipeline, add feature flags for advanced DSP.
- Multi-database support increasing QA surface: treat PostgreSQL as reference, add CI matrix for MySQL and SQLite.
- Deterministic scheduling vs. dynamic updates: persist seeds and provide replay endpoints for debugging.
- Browser audio API variance impacting WebDJ: use WebRTC fallback and provide hardware diagnostics.

## Implementation Task List

### Phase 0 – Readiness
- (P0) Audit `docs/sql/*.sql` against existing `internal/db` queries; align naming and add migrations scaffold.
- (P0) Define configuration struct and default loader covering DB, storage, analyzer, scheduler, and playout settings.

### Phase 1 – Control Plane & Data Contracts
- (P0) Implement database connection manager with health checks and context-aware query helpers.
- (P1) Scaffold repository interfaces and adapters for media, schedules, rules, and plays.
- (P1) Build OpenAPI spec skeleton and serve swagger JSON for `/v1` endpoints.
- (P1) Add structured logging and Prometheus registry wiring in `cmd/grimnirradio`.

### Phase 2 – Smart Blocks & Scheduling
- (P0) Complete `internal/smartblock` rule evaluation for quotas, separation, diversity, and fallbacks.
- (P0) Implement deterministic seed strategy and persistence of block decisions.
- (P0) Expand scheduler to materialize 48h rolling plans, respecting clocks, hard elements, and stopsets.
- (P1) Expose dry-run and simulation APIs for Smart Blocks and clocks.
- (P1) Add reconciliation loop for media availability changes and schedule drift handling.

### Phase 3 – Analyzer & Media Pipeline
- (P0) Implement analyzer worker pool, job lifecycle, and integration with GStreamer/ffmpeg tooling.
- (P0) Persist analysis results (loudness, cues, tempo) and trigger scheduler refresh events.
- (P0) Ship watch-folder scanners and remote uploader agent workflow with checksum validation.
- (P1) Generate waveform previews and loudness profiles for UI and WebDJ clients.
- (P1) Expose quarantine management UI/API for rejected assets with retry pipeline.

### Phase 4 – Playout & Live Control
- (P0) Build playout manager with per-mount pipelines, cue-aware crossfades, and loudness normalization.
- (P0) Implement live override ladder with manual and automatic handover commands.
- (P0) Add remote relay ingestion, health probes, and automatic failover routing.
- (P1) Emit real-time events for now playing, underruns, relay state, and pipeline health over WebSocket.
- (P1) Provide playout control endpoints (skip, reload, start, stop) with RBAC checks.
- (P1) Introduce hot-standby playout nodes with state replication hooks.

### Phase 5 – WebDJ, Voice Tracking & Listener Experience
- (P0) Implement WebDJ session management, WebRTC media pipeline, and cart wall controls.
- (P0) Build inline voice-track recorder/editor with approval workflow and schedule integration.
- (P1) Deliver library search, crates, and permissions for WebDJ and remote talent.
- (P1) Launch listener request API with moderation queue and rate limiting.
- (P1) Generate podcast feeds and downloadable show archives from airchecks.
- (P2) Add profanity delay, talkback/cue routing, and remote talent package sync.
- (P0) Stand up Ghost webhook endpoint with signature verification and ingest pipeline.
- (P1) Parse Ghost article metadata for embedded audio and schedule directives; create calendar entries.
- (P1) Provide approval UI/API for imported articles and conflict resolution with existing schedules.
- (P2) Automate recurring schedule handling based on Ghost tagging conventions and reminder notifications.

### Phase 6 – Observability, Security, and Polish
- (P0) Finish JWT/OIDC authentication flow and RBAC enforcement middleware.
- (P0) Add health, readiness, and metrics endpoints plus tracing instrumentation across subsystems.
- (P1) Harden API validation, error handling, and pagination/filter patterns.
- (P2) Document disaster recovery, backup strategy, and upgrade procedure in docs.

### Sustaining & Follow-Up
- (P1) Create regression test suite for scheduler determinism and audio continuity.
- (P1) Establish migration testing matrix for PostgreSQL/MySQL/SQLite.
- (P1) Expand compliance exports for regional variants and maintain legal retention tests.
- (P1) Build load and latency benchmarks for WebDJ and remote relay workflows.
- (P1) Monitor Ghost ingestion success metrics and iterate on tagging contracts with publishers.
- (P2) Evaluate addition of CockroachDB or other horizontally scalable stores if demand arises.
