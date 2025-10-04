# Grimnir Radio — Programmer's Spec

## Repository Layout
- `cmd/grimnirradio`: main entrypoint binary
- `internal/api`: HTTP+WebSocket API
- `internal/analyzer`: media analysis jobs (loudness, cues)
- `internal/clock`: clock compilation
- `internal/db`: DB connect/close, migrations
- `internal/events`: event bus
- `internal/live`: live input services
- `internal/media`: media service helpers
- `internal/playout`: GStreamer pipeline manager and director
- `internal/scheduler`: rolling schedule builder
- `internal/smartblock`: rule engine
- `internal/telemetry`: metrics/health

## Build & Run
- Go 1.22+
- Build: `go build ./cmd/grimnirradio`
- Run: `./grimnirradio`

## Environment Variables (prefer `GRIMNIR_*`)
- `GRIMNIR_ENV` = `development` | `production`
- `GRIMNIR_HTTP_BIND` = `0.0.0.0` (default)
- `GRIMNIR_HTTP_PORT` = `8080` (default)
- `GRIMNIR_DB_BACKEND` = `postgres` | `mysql` | `sqlite`
- `GRIMNIR_DB_DSN` = DSN string (required)
- `GRIMNIR_MEDIA_ROOT` = `./media` (default)
- `GRIMNIR_OBJECT_STORAGE_URL` = S3 or empty
- `GRIMNIR_GSTREAMER_BIN` = `gst-launch-1.0` (default)
- `GRIMNIR_SCHEDULER_LOOKAHEAD_MINUTES` = `48` (default)
- `GRIMNIR_JWT_SIGNING_KEY` = secret (required)
- `GRIMNIR_METRICS_BIND` = `127.0.0.1:9000` (default)
- Compatibility fallback: corresponding `RLM_*` are accepted

## Local Development
- Database: use SQLite for quick start (`GRIMNIR_DB_BACKEND=sqlite`, `GRIMNIR_DB_DSN=file:dev.sqlite?_foreign_keys=on`)
- Postgres example: `postgres://user:pass@localhost:5432/grimnir?sslmode=disable`
- Migrations: applied on startup via `internal/db.Migrate`

## Logging & Error Handling
- Zerolog, console writer in development; Info level by default, Debug in `development`
- Return structured errors from services; HTTP uses consistent error JSON helpers

## Testing
- Unit tests: `go test ./...`
- Focus on `smartblock`, `scheduler`, and `api` packages
- Consider fixture media for playout smoke tests

## API Quickstart
- Health: `curl http://localhost:8080/api/v1/health`
- Login: `POST /api/v1/auth/login` → `{access_token}`
- Authenticated routes use `Authorization: Bearer <token>`
 - Stations, mounts, media, smart-blocks, clocks, schedule, live, playout, analytics, webhooks under `/api/v1`
 - Webstreams (planned): CRUD under `/api/v1/webstreams`, health probe, and schedule references via clock slot `type: "webstream"`. Payloads support fallback URL chains and timing controls.

## Migration Tooling (Planned)
- Goal: import AzuraCast/LibreTime backups (stations, mounts, media metadata, playlists, basic schedules)
- CLI examples:
  - `./grimnirradio import azuracast --backup /path/azuracast_backup.tar.gz --dry-run`
  - `./grimnirradio import libretime --backup /path/libretime_backup.tar.gz --apply`
- API (admin-only):
  - `POST /api/v1/migrations/azuracast` (multipart upload or URL); responds with job ID
  - `GET /api/v1/migrations/{jobID}` for status; server-sent events or WebSocket for progress
- Mapping highlights:
  - Stations/mounts → `stations`, `mounts` with encoder presets
  - Playlists → static playlists or Smart Block rules (best effort)
  - Media → copy or reference existing paths; compute checksum; enqueue analysis if cues/loudness missing
  - Schedules → clocks + scheduled entries; unsupported constructs flagged for manual review
- Config knobs:
  - `GRIMNIR_IMPORT_MEDIA_ROOT`, `GRIMNIR_IMPORT_BATCH_SIZE`, `GRIMNIR_IMPORT_DRY_RUN`
- Cutover:
  - Validate on staging mount; use webstream relay for per-mount switchover; define grace window + fallback

### Same-Server Takeover
- Detect AzuraCast/LibreTime installs; stop services; import DB; move media/configs; translate ports and mounts
- Command: `./grimnirradio migrate local --detect --apply` (use `--dry-run` to preview)

### Cross-Server API Import
- Pull via API: stations, mounts, users, media metadata, playlists, schedules; sync media via rsync/S3/HTTP
- Commands:
  - `./grimnirradio migrate remote --source azuracast --url <URL> --token <TOKEN> --apply`
  - `./grimnirradio migrate remote --source libretime --url <URL> --token <TOKEN> --apply`
- Fallback to remote DB/backup when an API is incomplete

## Adding a Feature
- Define interfaces in the relevant `internal/*` package
- Wire into `internal/server` for dependency injection and route mounting
- Expose metrics and logs; update specs and README if behavior changes

## Feature: Webstream (Planned)
- Goal: relay external HTTP/ICY streams as scheduled items and on-demand inputs
- GStreamer: `souphttpsrc location=<URL> is-live=true ! decodebin ! audioconvert ! audioresample ! queue ! <normalization/crossfade chain>`
- Metadata: read ICY/HTTP headers for `StreamTitle`; emit now-playing events when present
- Health: timeouts, bounded reconnect with backoff; fallback to local programming when unavailable
- Scheduler: support `webstream` clock slot with URL and duration; optional `fallback_urls[]` for alternates (e.g., backup Icecast session); insert into rolling plan
- API: CRUD `/webstreams`; schedule slot payload `{ type: "webstream", payload: { url, headers?, timeout_ms?, fallback_urls?: [string], preflight_ms?, grace_ms? } }`
- Failover Flow: begin preflight `preflight_ms` before slot; if not connected by `grace_ms` after slot start, connect to first fallback URL; iterate until success or exhausted, then return to local programming
- Config: allow-list schemes (`http,https`), connect/read timeouts, backoff, preflight/grace/fallback limits; see Engineering Spec env vars

## Style & Conventions
- Keep functions small and focused; prefer explicit types over `any`
- Avoid global state; pass context; use `context.Context` for long-running tasks
- Avoid one-letter variable names; keep naming consistent and meaningful

## Troubleshooting
- No audio output: check GStreamer binary path and encoder presets
- Live handover flaky: inspect event bus logs and `/metrics`
- Schedule gaps: verify clocks, rules, and analysis status; run schedule refresh
