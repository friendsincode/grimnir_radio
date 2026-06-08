# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Grimnir Radio is a broadcast automation system built in Go 1.24. v2 is a **four-binary HA architecture** running on two proxmox VMs with VRRP-floated VIPs in front, shared Postgres + Redis behind, & Cloudflare R2 for media + backups. v1's two-binary topology is still supported & v2 is opt-in.

**Start here for v2 operators**: [`docs/v2/UPGRADE.md`](docs/v2/UPGRADE.md). Architecture overview: [`docs/v2/ARCHITECTURE.md`](docs/v2/ARCHITECTURE.md). Release notes: [`docs/v2/RELEASE_NOTES.md`](docs/v2/RELEASE_NOTES.md).

The four binaries:

- **Control Plane (`cmd/grimnirradio`)**: HTTP API server, scheduler, executor, authentication, DJAuth gRPC server.
- **Media Engine (`cmd/mediaengine`)**: GStreamer-based audio processing with gRPC control interface. Always-on `audiomixer` branch for DJ input. Emits mixed PCM via RTP to N edge encoders when `GRIMNIR_HA_PCM_RTP_ENABLED=true`.
- **Edge Encoder (`cmd/edge-encoder`)**: Ingests raw PCM via RTP from N media engines, sample-aligned input switching on engine failure, serves HTTP/ICY + HLS to listeners. Uses **go-gst CGo bindings** (gst-launch subprocess can't do runtime input switching). See `cmd/edge-encoder/README.md`.
- **Fan-out (`cmd/grimnir-fanout`)**: Accepts a single DJ over Harbor / RTP / SRT / WebRTC & duplicates the audio as PCM-over-RTP toward every media engine. Engine pipelines hold an always-on `audiomixer` branch; fan-out flips engine-side `LiveInputControl.SetLiveInput(active)` to mix the DJ in/out. Also uses go-gst CGo bindings. See `cmd/grimnir-fanout/README.md`.

Plus the operator CLI: **`cmd/grimnir-deploy`** is the single entry point for every mutating cluster operation. Every subcommand writes an `audit_log` row & posts to ntfy. The 3am-page index is [`docs/runbooks/index.md`](docs/runbooks/index.md).

The control plane communicates with the media engine via gRPC for low-latency audio control. The edge encoder ingests PCM-over-RTP from each media engine and is the listener-facing endpoint in HA mode. The fan-out is the DJ-facing endpoint & talks to both engines so the cross-node mixer output stays aligned.

## Common Commands

```bash
# Build both binaries
make build

# Run tests with race detector
make test

# Run a single test
go test -v -run TestName ./path/to/package

# Full verification (tidy, fmt, vet, lint, test)
make verify

# CI check (verify + fmt-check)
make ci

# Integration tests
go test -v -tags=integration ./...

# E2E browser tests (go-rod)
make test-e2e

# Quick route verification (no browser)
make test-routes

# Format code
make fmt

# Generate protobuf code
make proto
```

### Development

```bash
# Start dev database and redis
make dev-stack

# Run control plane (requires dev-stack)
make run-control

# Run media engine
make run-media
```

## Architecture

### Key Directories

- `internal/api/` - HTTP REST handlers (26+ endpoints)
- `internal/scheduler/` - Schedule generation (30-second tick, 48h rolling window)
- `internal/executor/` - Per-station state machine (Idle→Preloading→Playing→Fading→Live/Emergency)
- `internal/priority/` - 5-tier priority ladder (Emergency→Live Override→Live Scheduled→Automation→Fallback)
- `internal/mediaengine/` - gRPC client and DSP graph configuration
- `internal/playout/` - Director and playback management
- `internal/eventbus/` - Event bus abstractions (Redis/NATS/in-memory)
- `internal/auth/` - JWT validation, 3-tier RBAC
- `internal/models/` - GORM data models
- `internal/media/` - Media storage service (filesystem and S3 backends)
- `internal/webstream/` - HTTP stream relay with failover
- `internal/live/` - Live DJ input (Icecast, RTP, SRT, WebRTC)
- `internal/migration/` - Import tools for AzuraCast and LibreTime
- `internal/metrics/` - HA-specific Prometheus metrics with per-binary registries. Add new HA metrics here; use `internal/telemetry/` for legacy/cross-binary shared metrics.
- `internal/notify/` - Self-hosted ntfy.sh client with three-tier severity (Tier1 audit / Tier2 page / Tier3 page-and-rollback). Per-region topic naming.
- `internal/secrets/` - Pluggable secret backend (`.env` baseline + Vault KV v2). Backend chosen via `GRIMNIR_SECRETS_BACKEND`. Contract-tested against both backends.
- `internal/alertbridge/` - Alertmanager webhook -> ntfy adapter. Runs as a loopback sidecar via `cmd/alertmanager-ntfy`.
- `internal/dbhealth/` - Postgres replication-lag probe; exports `grimnir_pg_replication_lag_seconds`.
- `internal/vrrphealth/` - VRRP master-count + state probe; exports `grimnir_vrrp_master_count` and `grimnir_vrrp_state` (split-brain detector).
- `internal/grimnirfanout/` - Per-protocol DJ ingress (Harbor, RTP, SRT, WebRTC), per-session GStreamer pipeline, DJ auth client w/ LRU+TTL cache + event-bus revocation, Redis-backed cross-instance session replication. Paired with `cmd/grimnir-fanout/`.
- `cmd/grimnir-fanout/` - Live-input fan-out binary. See its README for ports, env vars, & run examples.
- `internal/grimnirdeploy/autorollback/` - Soak-window Prometheus poller. Flips deploy verdict to Rollback when listener reconnects / 5xx rate / page-and-rollback alerts breach defaults.
- `ops/prometheus/`, `ops/alertmanager/`, `ops/grafana/` - HA observability provisioning. `make prometheus-validate` runs promtool against the rules + tests.
- `docs/observability/README.md` - HA observability topology: what scrapes what, where alerts go, how secrets resolve.
- `proto/mediaengine/v1/` - Protobuf definitions for media engine gRPC

### Data Flow

1. HTTP API receives requests → JWT auth → API handlers
2. Scheduler compiles smart blocks into timeline
3. Executor runs per-station state machines
4. gRPC commands sent to Media Engine
5. GStreamer pipelines process audio
6. Telemetry streams back via gRPC
7. Events broadcast via Redis/NATS to WebSocket clients

### Media Path Handling

**Critical**: Media paths in the database must be **relative** (e.g., `station_id/ab/cd/file.audio`), not absolute.

- `internal/media/storage_fs.go`: `Store()` returns relative path, `Delete()` joins with rootDir
- `internal/playout/director.go`: Joins relative paths with `mediaRoot` before sending to media engine
- The media engine receives **absolute paths** via gRPC

If you see path errors like `/var/lib/grimnir/media/var/lib/grimnir/media/...`, the database contains absolute paths that need fixing via `migrations/002_fix_media_paths.sql`.

### Smart Blocks & Scheduling

Smart blocks are rule-based playlist generators with rotation rules and artist separation. Clock templates define hour-level scheduling with slot compilation. The scheduler materializes smart blocks into concrete timeline entries on a 30-second tick.

### DSP Processing

The media engine supports 12+ DSP node types: Loudness Normalize, AGC, Compressor, Limiter, EQ, Gate, Silence Detector, Ducking, etc. These are configured as a processing graph via the `LoadGraph` gRPC method.

### Multi-Instance Deployment

For horizontal scaling, the system uses Redis-based leader election with CRC32 consistent hashing (500 virtual nodes) for executor distribution. Event bus can be Redis or NATS.

## Key Technologies

- **Go 1.24** with Chi v5 router and GORM
- **gRPC/Protobuf** for media engine control
- **GStreamer** for audio processing (external dependency)
- **PostgreSQL** (primary), MySQL, SQLite (dev)
- **Redis** for leader election and event bus
- **go-rod** for E2E browser testing
- **OpenTelemetry** for tracing, **Prometheus** for metrics

## Environment Variables

Prefer `GRIMNIR_*` prefix (falls back to `RLM_*` for compatibility). Key variables:

- `GRIMNIR_DB_DSN` - Database connection string
- `GRIMNIR_REDIS_ADDR` - Redis address for events/leadership
- `GRIMNIR_MEDIA_ENGINE_GRPC_ADDR` - Media engine gRPC address (default: localhost:9091)
- `GRIMNIR_GRPC_ADDR` - Combined `host:port` for the control-plane gRPC server (DJAuth service). Convenience override for the split `GRIMNIR_GRPC_BIND` + `GRIMNIR_GRPC_PORT` pair. Default: `0.0.0.0:9095`. The fan-out binary dials this via `FANOUT_CONTROL_PLANE_GRPC`. Port 9095 is chosen so it does not collide with media-engine gRPC (9091) or NetClock master (9094).
- `GRIMNIR_GRPC_BIND` - Bind address for the control-plane gRPC server. Default: `0.0.0.0`.
- `GRIMNIR_GRPC_PORT` - Port for the control-plane gRPC server. Default: `9095`. Set to `0` to disable.
- `GRIMNIR_MEDIA_ROOT` - Base directory for media files (e.g., /var/lib/grimnir/media). Still required when backend=`s3`; it's the on-disk read-through cache.
- `GRIMNIR_MEDIA_BACKEND` - `fs` (default) or `s3`. Selects the `internal/media/` backend the control plane uses. `s3` requires `GRIMNIR_S3_BUCKET`. The factory in `internal/media/service.go` dispatches on this value.
- `GRIMNIR_S3_BUCKET` - Bucket name. Required when `GRIMNIR_MEDIA_BACKEND=s3`.
- `GRIMNIR_S3_ENDPOINT` - Endpoint URL (e.g., `https://<account-id>.r2.cloudflarestorage.com` for R2). Empty defaults to AWS S3.
- `GRIMNIR_S3_REGION` - Default `auto` (R2 convention). Set to `us-east-1` etc. for AWS S3.
- `GRIMNIR_S3_ACCESS_KEY` - Access key. Legacy alias: `GRIMNIR_S3_ACCESS_KEY_ID`; falls back to `AWS_ACCESS_KEY_ID`.
- `GRIMNIR_S3_SECRET_KEY` - Secret key. Legacy alias: `GRIMNIR_S3_SECRET_ACCESS_KEY`; falls back to `AWS_SECRET_ACCESS_KEY`.
- `GRIMNIR_S3_PATH_STYLE` - Default `true` (R2 & MinIO friendly). Set `false` for AWS virtual-host addressing.
- `GRIMNIR_S3_PUBLIC_BASE_URL` - Optional CDN base URL (e.g., a Cloudflare custom hostname in front of R2). When unset, URLs are constructed from the endpoint.
- `GRIMNIR_JWT_SIGNING_KEY` - JWT signing secret
- `GRIMNIR_HA_PCM_RTP_ENABLED` - When true, media engine emits raw L16 PCM via RTP to the configured edge encoders (in addition to the legacy fdsink output). Required for the HA architecture (Track A step 4). Default: false.
- `GRIMNIR_HA_PCM_RTP_TARGETS` - Comma-separated list of `host:port` for PCM-RTP delivery. Required when HA enabled. Example: `<node-a-ip>:5004,<node-b-ip>:5004`. Each entry receives the same RTP stream via `multiudpsink`.
- `GRIMNIR_NETCLOCK_ENABLED` - When true, media engine pipelines bind to a region-wide shared clock (NetClock master/slave) so PCM samples emitted by N engines are aligned at the wall-clock level. Required for sample-aligned PCM switching at the edge encoder. Default: false. See Track A step 5.
- `GRIMNIR_NETCLOCK_PORT` - TCP port the master serves clock time on. Default: 9094.
- `GRIMNIR_NETCLOCK_REGION` - Region identifier; part of the Redis lease key `grimnir-netclock-master-<region>`. Required when NetClock enabled.
- `GRIMNIR_NETCLOCK_MASTER_ADDR` - Slaves dial this `host:port`. Optional; future versions will auto-discover via Redis.
- `GRIMNIR_VRRP_VIPS` - Comma-separated VIP names (e.g., `listener,dj`) the control plane polls out of Redis hash `grimnir:vrrp:<name>` to update the `grimnir_vrrp_holder_count` gauge. Empty disables the poller. Required when keepalived (Track A step 7) is installed; see `docs/runbooks/keepalived-install.md`.
- `GRIMNIR_REGION` - Region short name; defaults to `default`. Drives ntfy topic naming (`grimnir-region-<region>-page`, `grimnir-audit-<region>`).
- `GRIMNIR_NTFY_URL` - Self-hosted ntfy.sh base URL (e.g., `https://ntfy.grimnir.example`). When unset, `notify.FromEnv` returns a NopNotifier so dev binaries don't fail to start.
- `GRIMNIR_NTFY_TOKEN_PAGE` - Publisher token for the per-region page topic.
- `GRIMNIR_NTFY_TOKEN_AUDIT` - Publisher token for the per-region audit topic.
- `GRIMNIR_NTFY_TOKEN_ROLLBACK` - Publisher token for the per-region rollback topic.
- `GRIMNIR_SECRETS_BACKEND` - `env` (default) or `vault`. Selects the `internal/secrets/` backend.
- `GRIMNIR_SECRETS_ENV_FILE` - Path to the .env file (default `.env`). Used only when backend=`env`.
- `VAULT_ADDR`, `VAULT_ROLE_ID`, `VAULT_SECRET_ID` - Vault AppRole credentials. Required when backend=`vault`.
- `GRIMNIR_PROMETHEUS_URL` - Prometheus base URL the auto-rollback observer polls (e.g., `http://prometheus:9090`). Falls back from the more specific `GRIMNIR_DEPLOY_AUTOROLLBACK_PROM_URL`.
- `GRIMNIR_DEPLOY_AUTOROLLBACK_ENABLED` - `true` (default) or `false`/`0`/`no`/`off`. Disables the soak-window observer; tests use `false` to avoid needing a live Prometheus.
- `GRIMNIR_DEPLOY_AUTOROLLBACK_PROM_URL` - Override Prometheus URL for auto-rollback only (per-region differentiation).
- `GRIMNIR_DEPLOY_AUTOROLLBACK_TICK` - Poll interval during the soak window. Default `15s`.
- `FANOUT_ENGINE_A_RTP` - Required. `host:port` of media engine A's PCM RTP ingress; `grimnir-fanout` ships every session's PCM here via `multiudpsink`.
- `FANOUT_ENGINE_B_RTP` - `host:port` of engine B. Empty for single-engine deployments.
- `FANOUT_HARBOR_PORT` / `FANOUT_RTP_PORT` / `FANOUT_SRT_PORT` / `FANOUT_WEBRTC_HTTP_PORT` - Per-protocol DJ ingress ports. Defaults: 8000 / 5006 / 1935 / 8004.
- `FANOUT_GRPC_PORT` / `FANOUT_HTTP_PORT` / `FANOUT_METRICS_PORT` - Control surface ports. Defaults: 9093 / 8003 / 9193.
- `FANOUT_CONTROL_PLANE_GRPC` - Control-plane gRPC for `DJAuth` lookups. When empty the binary boots in `AcceptAllAuthenticator` mode (dev only).
- `FANOUT_REDIS_ADDR` - Redis for cross-fanout session replication. When empty, sessions are single-node only.
- `FANOUT_NETCLOCK_ENABLED` / `FANOUT_NETCLOCK_MASTER_ADDR` - Bind per-session pipelines to the region's NetClock master so engine mixer output is sample-aligned across both engines.
- See `cmd/grimnir-fanout/README.md` for the full table.

## Architectural note: engine-side live-input mixer (v2.0.0-alpha.7+)

Starting with v2.0.0-alpha.7 the media engine pipeline holds an always-on `audiomixer` branch with a `udpsrc → rtpL16depay` input on `GRIMNIR_LIVE_INPUT_PORT`. The fan-out's gRPC `LiveInputControl.SetLiveInput(active bool)` flips whether the DJ branch contributes to the mixer output; the crossfade is driven by the mixer's per-pad volume property rather than `input-selector`. The engine accepts the call with `x-grimnir-source-addr` & `x-grimnir-session-id` as gRPC metadata so the audit log can trace which DJ session caused a given mixer transition. See `proto/mediaengine/v1/liveinput.proto` & `internal/mediaengine/liveinput_controller.go`.

## Architectural note: custom JS player (v2.0.0-alpha.10+)

The listener-facing browser player is a vanilla JS ES module at `internal/web/static/js/player/player.js`. No framework, no build step. It wraps `<audio>`, recycles the element on `error`/`stalled`/`waiting`/`ended`, & steps HQ -> LQ after 3 failures inside 30s. A background HEAD probe every 60s recovers back to HQ when the upstream returns. MediaSession metadata wires up lock-screen play/pause. See `internal/web/static/js/player/README.md` for the full API.

The module talks to two public control-plane endpoints:

- `GET /api/v1/stations/<id>/streams` — returns the ordered `StationStream` list (HQ first, LQ second). Handler: `internal/api/streams.go`.
- `POST /api/v1/listener-events` — anonymous reconnect telemetry (events: `play`, `stop`, `reconnect`, `degrade`, `upgrade`, `exhausted`). Process-local rate limit of 10/min/IP; the IP is logged but never stored. Handler: `internal/api/listener_events.go`.

Both endpoints are unauthenticated so the browser can call them directly. The legacy `GlobalPlayer` class in `internal/web/static/js/app.js` is still wired into the dashboard layout for cross-page playback; the new module only owns the public `/listen` page & the `/embed/player?station=<id>` widget.

## Architectural note: programmatic GStreamer (v2.0.0-alpha.3+)

Starting with v2.0.0-alpha.3 the edge encoder (`cmd/edge-encoder`) uses go-gst CGo bindings instead of `gst-launch-1.0` subprocess. Starting with v2.0.0-alpha.4 the media engine (`cmd/grimnirradio` playout layer) also uses programmatic go-gst — pipeline strings in `internal/playout/director.go` are preserved unchanged, but the spawning layer (`internal/playout/pipeline.go`) is now `gst.NewPipelineFromString(...)` so `pipeline.ForceClock(...)` is callable. Build dependencies: `libgstreamer1.0-dev` + plugin packs (see `cmd/edge-encoder/README.md`).

## Production Server Commands

**IMPORTANT**: On production servers, always use the `./grimnir` wrapper script, NOT direct `docker compose` commands. The wrapper handles the correct compose file ordering.

```bash
# All commands run from /srv/docker/grimnir_radio on the server
./grimnir up -d          # Start services
./grimnir down           # Stop services
./grimnir pull           # Pull latest images
./grimnir logs -f        # Follow logs
./grimnir ps             # Show status
./grimnir reset-db       # Reset database to fresh state (DESTRUCTIVE)
```

To reset the database manually:
```bash
./grimnir down
sudo rm -rf /srv/data/grimnir_radio/postgres-data
./grimnir up -d
```

## Docker Deployment

The `docker-compose.yml` uses these volume mounts:
- `media-data` → `/var/lib/grimnir/media` (inside container)
- `postgres-data` → `/var/lib/postgresql/data`

The `docker-compose.override.yml` (generated by `scripts/docker-quick-start.sh`) maps these to host paths like `/srv/data/grimnir_radio/media-data`.

**Important**: Files written to paths outside the mounted volumes (e.g., `/media/` instead of `/var/lib/grimnir/media/`) are stored in ephemeral container storage and will be lost on restart.

## Database Migrations (expand/contract discipline)

Rolling updates require v(N) and v(N+1) of the control plane to run side-by-side
against the same database during a deploy. Schema changes that work for one
version but break the other cause silent corruption or hard errors at the worst
moment. Every schema change must be split into three releases minimum:

1. **Expand**: ADD columns/tables/indexes only. Old code keeps working.
2. **Backfill + dual-write**: app writes to old + new shape; backfill populates new shape.
3. **Contract**: a later release (after every region is on the dual-write code) drops the old shape.

A "rename column" becomes 3 releases minimum.

**Enforced by `make ci`**: `cmd/migration-lint/` scans `migrations/*.sql` for
destructive operations (`DROP COLUMN`, `DROP TABLE`, `DROP INDEX`,
`RENAME COLUMN`, `RENAME TABLE`, `ALTER COLUMN ... TYPE`,
`ALTER COLUMN ... SET NOT NULL`, `TRUNCATE`) and fails the build unless the
migration includes a `-- migration-contract: <reason>` annotation. CI runs in
diff mode against `$BASE_REF` (set in the GitHub Actions workflow) so only
PR-changed files get linted; local `make migration-lint` lints everything.

**When to add the `-- migration-contract:` annotation**: when the destructive
operation is a legitimate contract phase of a multi-release sequence and the
release that wrote dual-format is already live in every region. The annotation
must name the original expand release and explain why it's safe now.

**See:** `docs/MIGRATIONS.md` for worked examples (add column, rename column,
drop column, narrow type). Use `migrations/TEMPLATE.sql` as the starting point
for any new migration.

## Versioning

Version is defined in `internal/version/version.go`. When bumping the version:

```bash
# 1. Update version in internal/version/version.go
# 2. Commit, tag, and push in one go:
git add -A && git commit -m "Message (vX.Y.Z)" && git tag -a vX.Y.Z -m "Version X.Y.Z" && git push origin main && git push origin vX.Y.Z
```

**CRITICAL**: EVERY version bump MUST include a git tag. No exceptions. Do this automatically without being asked. Tags trigger release builds and are used by the update checker.

## License

GNU AGPL v3.0-or-later. Modified network service deployments must publish source code.
