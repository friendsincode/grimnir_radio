# Grimnir Radio — Software Engineering Spec

## Context & Goals
- Deliver a reliable, deterministic radio automation control plane and playout
- Outperform legacy stacks in scheduling accuracy, live handover, and loudness consistency
- Keep the system simple to operate and extend (Go monolith + GStreamer)

## Non-Goals
- Replace Liquidsoap DSP graph complexity; we use GStreamer pipelines
- Heavy frontend frameworks (server-rendered HTML + small JS are fine)

## Architecture Overview
- Go monolith exposes HTTP JSON API and WebSocket events
- Subsystems: scheduler, smart blocks, analyzer, playout, live, media, events bus
- Playout: one GStreamer pipeline per mount (file/webstream/live inputs → Icecast/Shoutcast sinks)
- Database: Postgres (reference), MySQL, SQLite (dev/single node)
- Object storage: S3-compatible or filesystem for media

## Modules & Responsibilities
- `scheduler`: builds rolling plans from clocks and Smart Blocks; persists schedule
- `smartblock`: evaluates rules, quotas, separation windows, energy curves; deterministic via seeds
- `playout`: cue-aware crossfades, normalization, live override ladder, relay failover, and webstream inputs
- `analyzer`: ingest jobs, loudness and cue extraction, waveform previews
- `api`: HTTP surface for auth, media, programming, control, analytics, and webhooks
- `telemetry`: health, metrics, logs
- `db`: connection management, migrations

## Data Model Summary
- Entities: users, roles, stations, mounts, media, tags, playlists, rulesets, clocks, schedules, plays
- Webstreams: named remote stream endpoints (URL, protocol, headers, reconnect/backoff, health state, optional fallback URL chain)
- Analysis: loudness/cues stored with media; recent plays memory supports separation rules
- JSON-capable fields hold rule definitions and clock shapes

## API Surface (v1)
- Auth: login, refresh
- Stations/Mounts: list/create, encoder presets
- Media: upload via signed URL, metadata, waveform and analysis read
- Smart Blocks: CRUD, materialize (seed + target duration)
- Clocks: CRUD, simulate
- Schedule: list, refresh rolling window, patch entries
- Live: authorize, handover
- Playout: reload, skip, stop
- Webstreams: CRUD endpoints, health status, and schedule webstream slots into clocks or direct schedule entries. Payloads support fallback URL chains and timing controls (preflight/grace windows).
- Analytics: now playing, spins
- Webhooks: track start, pipeline health changes

## Configuration (env)
- Prefer `GRIMNIR_*` with backward-compatible `RLM_*` fallback
- `GRIMNIR_ENV` (default `development`)
- `GRIMNIR_HTTP_BIND` (`0.0.0.0`), `GRIMNIR_HTTP_PORT` (`8080`)
- `GRIMNIR_DB_BACKEND` (`postgres`|`mysql`|`sqlite`), `GRIMNIR_DB_DSN` (required)
- `GRIMNIR_MEDIA_ROOT` (default `./media`)
- `GRIMNIR_OBJECT_STORAGE_URL` (optional)
- `GRIMNIR_GSTREAMER_BIN` (default `gst-launch-1.0`)
- `GRIMNIR_SCHEDULER_LOOKAHEAD_MINUTES` (default `48`)
- `GRIMNIR_JWT_SIGNING_KEY` (required)
- `GRIMNIR_METRICS_BIND` (default `127.0.0.1:9000`)
- Webstream-specific:
- `GRIMNIR_WEBSTREAM_ALLOWED_SCHEMES` (default `http,https`) — comma-separated
- `GRIMNIR_WEBSTREAM_CONNECT_TIMEOUT_MS` (default `5000`)
- `GRIMNIR_WEBSTREAM_READ_TIMEOUT_MS` (default `15000`)
- `GRIMNIR_WEBSTREAM_RECONNECT_BACKOFF_MS` (default `1000`)
- `GRIMNIR_WEBSTREAM_PREFLIGHT_MS` (default `3000`) — early connect before slot start to validate readiness
- `GRIMNIR_WEBSTREAM_GRACE_MS` (default `5000`) — allowed time after slot start before failover
- `GRIMNIR_WEBSTREAM_FALLBACK_LIMIT` (default `3`) — max alternates tried before local programming fallback

## Observability
- Logs: zerolog, structured with request/job IDs
- Metrics: `/metrics` with `grimnir_radio_metrics` placeholders
- Health: `/healthz` and `/api/v1/health`

## Security
- JWT-based auth with optional OIDC
- RBAC: admin, manager, DJ
- Route-level middleware for auth and roles

## Performance Targets
- Schedule build within 500ms for a 1-hour window (warm cache)
- Playout underruns: zero in nominal cases; live handover < 3s
- Analyzer throughput: parallel workers bounded; backpressure via job queue
- Webstream start/handback: < 3s join, < 2s retry backoff on transient errors; failover to backup URL within grace window

## Acceptance Criteria
- Deterministic Smart Block materialization given identical seed and inputs
- 48h rolling schedule persists and reconciles on media changes
- Live handover honors priority ladder with smooth transitions
- Loudness normalization enforced at playout per configured target
- Scheduled Webstream slot plays, recovers on disconnects (bounded retries, graceful fallback)
- If the primary webstream is not available by the end of the grace window, automatically connect to the first backup URL (e.g., alternate Icecast session), iterating the fallback chain before returning to local programming.

## Test Strategy
- Unit: rule evaluation, scheduler slotting, API validation
- Integration: SQLite-backed runs for end-to-end scheduling/playlisting
- Smoke: playout with fixture audio for 1 hour, continuity assertions
- Load: scheduler/API under concurrent changes; analyzer job throughput

## Release Plan
- Phase 1: control plane, DB manager, basic API, logging/metrics
- Phase 2: Smart Blocks + scheduler rolling plans
- Phase 3: analyzer pipeline and media ops
- Phase 4: playout manager, live control, and webstream inputs (scheduleable webstream slots with fallback chains)
- Phase 5: WebDJ/voice tracking basics, listener experience
- Phase 6: observability hardening, RBAC, and upgrade docs

## Risks & Mitigations
- GStreamer complexity → feature flags; minimal viable pipeline first
- Multi-DB surface → treat Postgres as reference; CI matrix for MySQL/SQLite
- Determinism vs live changes → persist seeds; reconciliation loop and replay endpoints
- Upstream webstream instability → health checks, bounded reconnects, and automatic fallback to local programming

## Migration & Import (AzuraCast, LibreTime)
- Objectives: import stations, mounts, media metadata, playlists, and basic schedules from AzuraCast and LibreTime with minimal downtime
- Inputs:
  - AzuraCast backup: `.tar.gz` containing DB dump (MySQL or Postgres), `stations/` configs, `media/` per station
  - LibreTime backup: DB dump (Postgres), `storage/` media, and liquidsoap/encoder configs
- Process:
  1) Extract archive and detect platform + schema version
  2) Parse DB dump; map entities to Grimnir schema (stations, mounts, users, playlists)
  3) Media import: hash by path + checksum; de-duplicate; enqueue analysis for missing cues/loudness
  4) Playlist mapping: convert to static playlists or Smart Block seed rules when feasible; flag complex rules for manual review
  5) Schedule mapping: translate shows to clocks and scheduled entries; preserve hard elements where possible
  6) Dry-run mode: report intended creates/updates, conflicts, and unsupported features
  7) Apply mode: transactional import; resumable on failure
- Cutover Strategy:
  - Stage on a non-public mount; validate with webstream relay
  - Per-mount switchover with grace window; fallback to original source if required
- Tooling (planned):
  - CLI: `grimnirradio import azuracast --backup <path> [--station <id>] [--dry-run]`
  - CLI: `grimnirradio import libretime --backup <path> [--station <id>] [--dry-run]`
  - API (admin): `/api/v1/migrations/{azuracast|libretime}` with upload or URL to archive; emits progress events
- Config:
  - `GRIMNIR_IMPORT_MEDIA_ROOT` (default `./media`)
  - `GRIMNIR_IMPORT_BATCH_SIZE` (default `500`)
  - `GRIMNIR_IMPORT_DRY_RUN` (bool)
- Observability:
  - Metrics: items scanned/imported/errored; duration per phase
  - Events: import started/progress/chunk-complete/completed/failed, with station scope
- Acceptance:
  - Imports run in dry-run with full diff preview
  - At least 95% of common AzuraCast/LibreTime fields mapped automatically for typical setups
  - No data loss for stations, mounts, media paths, and playlists; unsupported features surfaced explicitly

### Same-Server Takeover (Local)
- Detect: inspect Docker (compose) or system services to find AzuraCast/LibreTime footprints
- Quiesce: stop source services safely (configurable) and take a consistent DB snapshot
- Import: parse DB and configs in place; move/copy media to `GRIMNIR_IMPORT_MEDIA_ROOT`; translate encoder presets
- Map: stations → stations, mounts → mounts, playlists → static or Smart Block rules, schedules → clocks + entries
- Validate: dry-run report; start Grimnir on staging mount; smoke test
- Switch: rebind ports or update reverse proxy; enable production mounts; keep rollback hook to re-enable source

### Cross-Server Migration (Remote API / Remote DB)
- API connectors: AzuraCast and LibreTime endpoints for stations, media, playlists, schedules, users (where available)
- Remote DB fallback: if API is limited, read a remote DB snapshot or backup archive
- Media sync: rsync/S3/HTTP copy with checksums; resumable; incremental sync until cutover
- Scheduling: export shows/playlists; convert to clocks and scheduled entries; gaps flagged for manual review
- Cutover: configure webstream relay to test; per-mount final switch with grace and fallback

### CLI Examples
- Local takeover: `grimnirradio migrate local --detect --apply` (or `--dry-run`)
- Remote via API: `grimnirradio migrate remote --source azuracast --url https://azuracast.example --token $TOKEN --apply`
- Remote via backup: `grimnirradio import azuracast --backup /backups/azuracast.tar.gz --apply`


## Webstream Feature
- Purpose: allow relaying external HTTP/ICY streams, either ad-hoc or scheduled in clocks
- Scheduling: add `webstream` slot type with URL and optional headers; optional `fallback_urls[]` for alternates (e.g., backup Icecast mount); direct schedule entries supported
- Playout: GStreamer graph e.g., `souphttpsrc location=... ! decodebin ! audioconvert ! audioresample ! level ! queue ! …` into mount pipeline
- Health: periodic ICY/HTTP checks, metadata passthrough (title/artist), retry with backoff; emit events on state changes
- Failover: preflight connection begins `GRIMNIR_WEBSTREAM_PREFLIGHT_MS` before slot; if not ready by `GRIMNIR_WEBSTREAM_GRACE_MS` after start, auto-failover to next `fallback_url`; after `GRIMNIR_WEBSTREAM_FALLBACK_LIMIT` attempts, revert to local programming
- API: CRUD (`/webstreams`), schedule slot references, health endpoint; RBAC-managed. Payload supports `{ url, headers?, fallback_urls?, preflight_ms?, grace_ms?, retry? }`
