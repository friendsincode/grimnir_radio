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

Prefer `GRIMNIR_*` prefix (falls back to `RLM_*` for compatibility). The v2 binary set reads ~90 env vars across seven categories. The canonical operator-facing template is `.env.example` (v1 surface preserved at `.env.v1.example`); per-binary READMEs are the deep reference.

Legend in the tables below: **Binary** = which binary reads it (CP = control plane `cmd/grimnirradio`, ME = media engine, EE = edge encoder, FO = fan-out, DP = `grimnir-deploy`, AB = `alertmanager-ntfy`). **Req** = required for that binary's role; **opt** = has a working default.

### Substrate (shared by every binary in a region)

| Variable | Binary | Default | Req | Description |
|---|---|---|---|---|
| `GRIMNIR_DB_DSN` (alias `RLM_DB_DSN`) | CP, DP | empty | yes | Postgres connection string. v2 requires Postgres 16+ in HA. |
| `GRIMNIR_REDIS_ADDR` (alias `RLM_REDIS_ADDR`) | CP, ME, FO, DP | `localhost:6379` | yes (HA) | Redis address for leader election, event bus, NetClock lease, fan-out session replication. |
| `GRIMNIR_REDIS_PASSWORD` (alias `RLM_REDIS_PASSWORD`, `REDIS_PW`) | CP, ME, DP | empty | depends | Redis auth. `grimnir-deploy` accepts `REDIS_PW` as a third fallback. |
| `GRIMNIR_REDIS_DB` | CP | `0` | opt | Logical Redis DB index. |
| `GRIMNIR_ENV` (alias `RLM_ENV`) | CP | `development` | opt | Environment band; affects dev-mode fallbacks (e.g. TURN credential check). |
| `GRIMNIR_REGION` | CP, DP | `default` | yes (HA) | Drives ntfy topic naming & NetClock Redis lease keys. |
| `GRIMNIR_INSTANCE_ID` (alias `RLM_INSTANCE_ID`) | CP | empty | yes (multi-instance) | Unique per node; used by leader election. |
| `GRIMNIR_LEADER_ELECTION_ENABLED` (alias `RLM_LEADER_ELECTION_ENABLED`) | CP | `false` | yes (HA) | Enables Redis-backed leader election. |
| `GRIMNIR_DB_BACKEND` (alias `RLM_DB_BACKEND`) | CP | `postgres` | opt | `postgres`, `mysql`, or `sqlite`. |

### Control plane (`cmd/grimnirradio`)

| Variable | Default | Req | Description |
|---|---|---|---|
| `GRIMNIR_HTTP_BIND` / `GRIMNIR_HTTP_PORT` (RLM_* aliases) | `0.0.0.0` / `8080` | opt | HTTP listener. |
| `GRIMNIR_GRPC_ADDR` | empty | opt | Combined `host:port` override for the control-plane gRPC server (DJAuth). Wins over the split bind/port pair. Port 9095 is chosen so it does not collide with media-engine gRPC (9091) or NetClock master (9094). |
| `GRIMNIR_GRPC_BIND` / `GRIMNIR_GRPC_PORT` | `0.0.0.0` / `9095` | opt | Split bind/port for DJAuth gRPC. Port `0` disables. |
| `GRIMNIR_JWT_SIGNING_KEY` (alias `RLM_JWT_SIGNING_KEY`) | empty | yes | JWT signing secret. Must match across nodes in a region. |
| `GRIMNIR_BASE_URL` (alias `RLM_BASE_URL`) | empty | opt | External URL used in webhook payloads, etc. |
| `GRIMNIR_MEDIA_ROOT` (alias `RLM_MEDIA_ROOT`) | `./media` | yes | Filesystem media path. Still required when backend=`s3` (read-through cache). |
| `GRIMNIR_MEDIA_BACKEND` | `fs` | opt | `fs` (default) or `s3`. `s3` requires `GRIMNIR_S3_BUCKET`. |
| `GRIMNIR_S3_BUCKET` (alias `S3_BUCKET`) | empty | yes (s3) | Bucket name. |
| `GRIMNIR_S3_ENDPOINT` (alias `S3_ENDPOINT`) | empty | opt | Endpoint URL; e.g., `https://<account-id>.r2.cloudflarestorage.com` for R2. Empty defaults to AWS S3. |
| `GRIMNIR_S3_REGION` (alias `AWS_REGION`) | `auto` | opt | Set to `us-east-1` etc. for AWS S3. |
| `GRIMNIR_S3_ACCESS_KEY` (aliases `GRIMNIR_S3_ACCESS_KEY_ID`, `AWS_ACCESS_KEY_ID`) | empty | yes (s3) | Access key. |
| `GRIMNIR_S3_SECRET_KEY` (aliases `GRIMNIR_S3_SECRET_ACCESS_KEY`, `AWS_SECRET_ACCESS_KEY`) | empty | yes (s3) | Secret key. |
| `GRIMNIR_S3_PATH_STYLE` (alias `S3_USE_PATH_STYLE`) | `true` | opt | `true` for R2 & MinIO; `false` for AWS virtual-host. |
| `GRIMNIR_S3_PUBLIC_BASE_URL` (alias `S3_PUBLIC_BASE_URL`) | empty | opt | CDN base URL (e.g., a Cloudflare custom hostname in front of R2). |
| `GRIMNIR_MEDIA_ENGINE_GRPC_ADDR` (alias `MEDIA_ENGINE_GRPC_ADDR`) | `mediaengine:9091` | yes | Loopback in v2; each VM runs its own engine. |
| `GRIMNIR_SCHEDULER_LOOKAHEAD_MINUTES` (alias `RLM_*`) | `168` (h, not min) | opt | Lookahead horizon for schedule materialization. |
| `GRIMNIR_METRICS_BIND` (alias `RLM_*`) | `127.0.0.1:9000` | opt | Prometheus metrics bind. |
| `GRIMNIR_MAX_UPLOAD_SIZE_MB` (alias `RLM_*`) | `0` (endpoint defaults) | opt | Global multipart upload cap. |
| `GRIMNIR_TRACING_ENABLED`, `GRIMNIR_OTLP_ENDPOINT`, `GRIMNIR_TRACING_SAMPLE_RATE` (RLM_* aliases) | `false` / `localhost:4317` / `1.0` | opt | OTLP tracing. |
| `GRIMNIR_HARBOR_ENABLED` + `GRIMNIR_HARBOR_PORT`, `GRIMNIR_HARBOR_BIND`, `GRIMNIR_HARBOR_HOST`, `GRIMNIR_HARBOR_MOUNT_PREFIX`, `GRIMNIR_HARBOR_SSL`, `GRIMNIR_HARBOR_MAX_SOURCES`, `GRIMNIR_HARBOR_PUBLIC_PORT` (HARBOR_* aliases) | off / 8088 / 0.0.0.0 / — / — / false / 10 / 0 | opt | Built-in source receiver (legacy v1 path; v2 uses fan-out). |
| `GRIMNIR_WEBRTC_ENABLED`, `GRIMNIR_WEBRTC_RTP_PORT`, `GRIMNIR_WEBRTC_STUN_URL`, `GRIMNIR_WEBRTC_TURN_URL`, `GRIMNIR_WEBRTC_TURN_USERNAME`, `GRIMNIR_WEBRTC_TURN_PASSWORD` (WEBRTC_* aliases) | true / 5004 / Google STUN / empty / empty / empty | opt | Legacy WebRTC ingest. In production, TURN_URL set requires the username+password pair. |

### Media engine (`cmd/mediaengine`)

| Variable | Default | Req | Description |
|---|---|---|---|
| `GRIMNIR_HA_PCM_RTP_ENABLED` (alias `RLM_*`) | `false` | yes (HA) | Emit raw L16 PCM via RTP to fan-out / edge-encoder downstream. |
| `GRIMNIR_HA_PCM_RTP_TARGETS` (alias `RLM_*`) | empty | yes (HA) | Comma-separated `host:port` list; `multiudpsink` duplicates the stream. |
| `GRIMNIR_NETCLOCK_ENABLED` (alias `RLM_*`) | `false` | yes (HA) | Bind pipelines to the region's shared GstNetClock. |
| `GRIMNIR_NETCLOCK_PORT` (alias `RLM_*`) | `9094` | opt | TCP port the master serves clock time on. |
| `GRIMNIR_NETCLOCK_REGION` (alias `RLM_*`) | empty | yes (NetClock on) | Region key for the Redis lease `grimnir-netclock-master-<region>`. |
| `GRIMNIR_NETCLOCK_MASTER_ADDR` (alias `RLM_*`) | empty | opt | Slaves dial this `host:port`. Future: auto-discovered via Redis. |
| `GRIMNIR_LIVE_INPUT_ENABLED` (alias `RLM_*`) | `false` | yes (fan-out wired) | Enables the always-on engine-side audiomixer branch for fan-out PCM ingest. |
| `GRIMNIR_LIVE_INPUT_PORT` (alias `RLM_*`) | `5008` | opt | Engine's `udpsrc` port for fan-out's PCM-over-RTP. |
| `GRIMNIR_LIVE_INPUT_FANOUT_ADDR` (alias `RLM_*`) | empty | yes (live-input on) | `host:port` of the fan-out the engine accepts gRPC `SetLiveInput` from. |
| `GRIMNIR_GSTREAMER_BIN` (alias `RLM_*`) | `gst-launch-1.0` | opt | Pipeline spawner binary. |
| `MEDIAENGINE_LOG_LEVEL` | `info` | opt | Engine log level. |

### Edge encoder (`cmd/edge-encoder`)

| Variable | Default | Req | Description |
|---|---|---|---|
| `EDGE_ENCODER_BIND_ADDR` | `0.0.0.0` | opt | gRPC + HTTP bind. |
| `EDGE_ENCODER_GRPC_PORT` | `9092` | opt | gRPC port for `GetStatus`. |
| `EDGE_ENCODER_HTTP_PORT` | `8001` | opt | HTTP/ICY listener (`/live`, `/healthz`). |
| `EDGE_ENCODER_METRICS_PORT` | `9192` | opt | Prometheus metrics endpoint (reserved). |
| `EDGE_ENCODER_RTP_PORT_A` / `EDGE_ENCODER_RTP_PORT_B` | `5004` / `5005` | opt | UDP ports for engine A & B RTP-L16. |
| `EDGE_ENCODER_ENGINE_A_GRPC` / `EDGE_ENCODER_ENGINE_B_GRPC` | empty | opt | `host:port` of each engine's gRPC; empty disables the health subscription (falls back to pure packet-arrival health). |
| `EDGE_ENCODER_OUTPUT_FORMAT` | `mp3` | opt | `mp3` or `aac`. |
| `EDGE_ENCODER_OUTPUT_BITRATE_KBPS` | `128` | opt | Encoder bitrate. |
| `EDGE_ENCODER_HLS_ENABLED` | `false` | opt | Also emit HLS segments to S3. |
| `EDGE_ENCODER_HLS_S3_BUCKET`, `_REGION`, `_ENDPOINT`, `_USE_PATH_STYLE` | empty / `us-east-1` / empty / `false` | yes (HLS on) | HLS S3 destination. Set USE_PATH_STYLE=true for MinIO. |
| `EDGE_ENCODER_HLS_SEGMENT_DIR` | `/tmp/grimnir-hls` | opt | Local staging dir before S3 upload. |
| `EDGE_ENCODER_LOG_LEVEL` | `info` | opt | Log level. |

Full table: `cmd/edge-encoder/README.md`.

### Fan-out (`cmd/grimnir-fanout`)

Variables namespaced `FANOUT_*` (legacy fallback `RLM_FANOUT_*`).

| Variable | Default | Req | Description |
|---|---|---|---|
| `FANOUT_ENGINE_A_RTP` | empty | yes | `host:port` of engine A PCM RTP ingress. |
| `FANOUT_ENGINE_B_RTP` | empty | opt | Engine B; empty = single-engine deployment. |
| `FANOUT_BIND_ADDR` | `0.0.0.0` | opt | Bind for HTTP & gRPC. |
| `FANOUT_GRPC_PORT` / `FANOUT_HTTP_PORT` / `FANOUT_METRICS_PORT` | `9093` / `8003` / `9193` | opt | Control surface ports. |
| `FANOUT_HARBOR_PORT` / `FANOUT_RTP_PORT` / `FANOUT_SRT_PORT` / `FANOUT_WEBRTC_HTTP_PORT` | `8000` / `5006` / `1935` / `8004` | opt | DJ ingress per protocol. |
| `FANOUT_CONTROL_PLANE_GRPC` | empty | yes (prod) | Dials the control plane's `DJAuth` gRPC. Empty boots in `AcceptAllAuthenticator` dev mode. |
| `FANOUT_REDIS_ADDR` | empty | yes (HA) | Cross-fanout session replication. |
| `FANOUT_NETCLOCK_ENABLED` / `FANOUT_NETCLOCK_MASTER_ADDR` | `false` / empty | yes (HA) | Bind per-session pipelines to the region's NetClock so engine mixer output is sample-aligned across both engines. |
| `FANOUT_LOG_LEVEL` | `info` | opt | Log level. |

Full table: `cmd/grimnir-fanout/README.md`.

### grimnir-deploy (operator workstation only)

| Variable | Default | Req | Description |
|---|---|---|---|
| `GRIMNIR_DEPLOY_DB_DSN` (falls back to `GRIMNIR_DB_DSN`) | empty | yes | Postgres DSN the deploy tool writes audit/history rows to. |
| `GRIMNIR_DEPLOY_REDIS_ADDR` (falls back to `GRIMNIR_REDIS_ADDR`) | empty | yes | Redis for the deploy lock. |
| `GRIMNIR_DEPLOY_REDIS_PASSWORD` (falls back to `GRIMNIR_REDIS_PASSWORD`, `REDIS_PW`) | empty | depends | Redis auth. |
| `GRIMNIR_DEPLOY_OPERATOR` (falls back to `USER`) | `unknown` | opt | Operator name written into audit rows. |
| `GRIMNIR_DEPLOY_POLICY` | `auto` | opt | `auto`, `manual`, `strict`. |
| `GRIMNIR_DEPLOY_WINDOW_CRON` | empty | opt | Cron expression restricting deploy windows. |
| `GRIMNIR_DEPLOY_SOAK_WINDOW` | `5m` | opt | Soak duration after node-a upgrade before node-b. |
| `GRIMNIR_DEPLOY_ROLLBACK_WINDOW` | `30m` | opt | How long after a deploy `grimnir-deploy rollback` accepts the request. |
| `GRIMNIR_DEPLOY_PEER_HOST` | empty | yes | SSH target for the peer VM. |
| `GRIMNIR_DEPLOY_PEER_SSH_USER` | `<ssh-user>` | opt | SSH user (operator chooses; the default at code level is whatever the operator configures via env). |
| `GRIMNIR_DEPLOY_PEER_SSH_PORT` | `22` | opt | SSH port. |
| `GRIMNIR_DEPLOY_PEER_SSH_KEY` | empty | yes | Path to private key (e.g., `~/.ssh/grimnir-deploy-ed25519`). |
| `GRIMNIR_DEPLOY_AUTOROLLBACK_ENABLED` | `true` | opt | Disables the soak-window observer (set `false`/`0`/`no`/`off`); tests use this. |
| `GRIMNIR_DEPLOY_AUTOROLLBACK_PROM_URL` (falls back to `GRIMNIR_PROMETHEUS_URL`) | empty | yes (auto-rollback on) | Prometheus base URL the observer polls. |
| `GRIMNIR_DEPLOY_AUTOROLLBACK_TICK` | `15s` | opt | Observer poll interval. |
| `GRIMNIR_LISTENER_VIP`, `GRIMNIR_DJ_VIP` | empty | opt | VIPs the partition-recovery runbook checks for split-brain. |

### Observability (`cmd/alertmanager-ntfy` + control-plane ntfy publishers)

| Variable | Default | Req | Description |
|---|---|---|---|
| `GRIMNIR_ALERTBRIDGE_ADDR` | `127.0.0.1:9095` | opt | Loopback HTTP bind for the Alertmanager-to-ntfy bridge. |
| `GRIMNIR_NTFY_URL` | empty | opt | ntfy.sh base URL. Empty returns a NopNotifier so dev binaries don't fail to start. |
| `GRIMNIR_NTFY_TOKEN_PAGE`, `_AUDIT`, `_ROLLBACK` | empty | yes (per-tier) | Publisher tokens. Topic names derive from `GRIMNIR_REGION`. |
| `GRIMNIR_PROMETHEUS_URL` | empty | opt | Prometheus base URL the auto-rollback observer polls. |
| `GRIMNIR_VRRP_VIPS` | empty | yes (keepalived) | Comma-separated VIP names (e.g., `listener,dj`) the control plane polls out of Redis hash `grimnir:vrrp:<name>` to update the `grimnir_vrrp_holder_count` gauge. See `docs/runbooks/keepalived-install.md`. |

### Secrets backend

| Variable | Default | Req | Description |
|---|---|---|---|
| `GRIMNIR_SECRETS_BACKEND` | `env` | opt | `env` (default) or `vault`. |
| `GRIMNIR_SECRETS_ENV_FILE` | `.env` | opt | Path to the .env file. Used only when backend=`env`. |
| `VAULT_ADDR`, `VAULT_ROLE_ID`, `VAULT_SECRET_ID` | empty | yes (vault) | Vault AppRole credentials. |

### Test-only & legacy aliases

`CI`, `E2E_HEADLESS`, `SKIP_BROWSER_TESTS`, `TEST_DB_DSN`, `GRIMNIR_COVERAGE_PROFILE`, `GRIMNIR_COVERAGE_TARGET` are read by tests; `SSH_CLIENT` and `USER` populate audit attribution. Legacy aliases (`ENVIRONMENT`, `LEADER_ELECTION_ENABLED`, `JWT_SIGNING_KEY`, `TRACING_ENABLED`, `OTLP_ENDPOINT`, `TRACING_SAMPLE_RATE`) still parse but emit a warning at startup; prefer the `GRIMNIR_*` form.

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
