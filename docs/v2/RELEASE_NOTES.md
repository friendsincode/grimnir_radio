# Grimnir Radio v2.0.0-rc.1 release notes

`v2.0.0-rc.1` is the release candidate that consolidates the eleven alpha tags (v2.0.0-alpha.1 through v2.0.0-alpha.11) into a single shippable HA architecture. Start with `docs/v2/UPGRADE.md` for the operator runbook.

## What changed at a glance

v2 keeps the v1 control plane + media engine split & adds two more binaries (edge encoder + grimnir-fanout). The deployment moves from one VM running one binary pair to two VMs each running four binaries, with VRRP-floated VIPs in front, shared Postgres + Redis behind, and R2 for media + backups.

Nothing from v1 was removed. The v1 binary pair still runs against the same database; you can stay on v1 indefinitely. v2 is opt-in via the upgrade runbook.

## New binaries

| Binary | Purpose | Source |
|---|---|---|
| `cmd/edge-encoder` | Ingests PCM-over-RTP from N media engines, sample-aligned input switching, serves HTTP/ICY + HLS to listeners. Uses go-gst CGo bindings. | shipped in v2.0.0-alpha.3 |
| `cmd/grimnir-fanout` | Accepts a single DJ over Harbor / RTP / SRT / WebRTC; duplicates PCM-over-RTP to every media engine. Engine-side mixer branch flips via gRPC. Uses go-gst CGo bindings. | shipped in v2.0.0-alpha.7 |
| `cmd/grimnir-deploy` | Operator CLI for every mutating cluster operation. Writes audit rows on start & completion. Carries `--dry-run` & `--help` for every subcommand. | shipped in v2.0.0-alpha.5 |
| `cmd/alertmanager-ntfy` | Loopback sidecar that adapts Alertmanager webhooks to ntfy.sh with three severity tiers. | shipped in v2.0.0-alpha.6 |

The v1 binaries (`cmd/grimnirradio`, `cmd/mediaengine`) are unchanged in role but ship with new HA env vars (see below).

## New API endpoints

Two unauthenticated endpoints power the custom JS player on the public listen page:

- `GET /api/v1/stations/<id>/streams` — returns the ordered `StationStream` list (HQ first, LQ second). Handler: `internal/api/streams.go`.
- `POST /api/v1/listener-events` — anonymous reconnect telemetry (events: `play`, `stop`, `reconnect`, `degrade`, `upgrade`, `exhausted`). Process-local rate limit of 10/min/IP; the IP is logged but never stored. Handler: `internal/api/listener_events.go`.

The legacy authenticated API surface from v1 is unchanged.

## New gRPC surface

- `proto/mediaengine/v1/liveinput.proto` adds `LiveInputControl.SetLiveInput(active bool)` so the fan-out can flip the engine-side mixer branch in/out. The engine accepts `x-grimnir-source-addr` & `x-grimnir-session-id` as gRPC metadata so the audit log can trace which DJ session caused a given mixer transition.

## New database tables

- `deploy_history` — one row per `grimnir-deploy deploy` invocation. Columns include `tag`, `previous_tag`, `phase` (`started` / `complete` / `failed`), `started_at`, `completed_at`, `operator`, `reason`.
- `audit_log` — one row per mutating subcommand. Columns: `operator`, `subcommand`, `args`, `started_at`, `duration_ms`, `outcome`.

Both tables follow the expand-only migration discipline (`docs/MIGRATIONS.md`). No v1 tables were renamed, dropped, or had columns removed.

## New env vars

Grouped by binary; full table in `CLAUDE.md`.

**Media engine (HA path)**:

- `GRIMNIR_HA_PCM_RTP_ENABLED` (bool, default false) — emit raw L16 PCM via RTP to edge encoders
- `GRIMNIR_HA_PCM_RTP_TARGETS` (csv host:port) — required when HA enabled
- `GRIMNIR_NETCLOCK_ENABLED` (bool, default false) — bind pipelines to region-wide shared clock
- `GRIMNIR_NETCLOCK_PORT` (default 9094)
- `GRIMNIR_NETCLOCK_REGION` — required when NetClock enabled
- `GRIMNIR_NETCLOCK_MASTER_ADDR` — slaves dial this host:port

**Control plane (HA path)**:

- `GRIMNIR_REGION` — drives ntfy topic naming
- `GRIMNIR_MEDIA_BACKEND` — `fs` (default) or `s3`
- `GRIMNIR_S3_BUCKET` / `GRIMNIR_S3_ENDPOINT` / `GRIMNIR_S3_REGION` / `GRIMNIR_S3_ACCESS_KEY` / `GRIMNIR_S3_SECRET_KEY` / `GRIMNIR_S3_PATH_STYLE` / `GRIMNIR_S3_PUBLIC_BASE_URL`
- `GRIMNIR_VRRP_VIPS` — csv of VIP names the control plane polls from Redis
- `GRIMNIR_NTFY_URL` / `GRIMNIR_NTFY_TOKEN_PAGE` / `GRIMNIR_NTFY_TOKEN_AUDIT` / `GRIMNIR_NTFY_TOKEN_ROLLBACK`
- `GRIMNIR_SECRETS_BACKEND` — `env` (default) or `vault`
- `GRIMNIR_SECRETS_ENV_FILE`
- `VAULT_ADDR` / `VAULT_ROLE_ID` / `VAULT_SECRET_ID` — required when backend=vault

**Auto-rollback observer**:

- `GRIMNIR_PROMETHEUS_URL`
- `GRIMNIR_DEPLOY_AUTOROLLBACK_ENABLED` (bool, default true)
- `GRIMNIR_DEPLOY_AUTOROLLBACK_PROM_URL` — per-region override
- `GRIMNIR_DEPLOY_AUTOROLLBACK_TICK` (default 15s)

**grimnir-fanout**:

- `FANOUT_ENGINE_A_RTP` / `FANOUT_ENGINE_B_RTP`
- `FANOUT_HARBOR_PORT` / `FANOUT_RTP_PORT` / `FANOUT_SRT_PORT` / `FANOUT_WEBRTC_HTTP_PORT`
- `FANOUT_GRPC_PORT` / `FANOUT_HTTP_PORT` / `FANOUT_METRICS_PORT`
- `FANOUT_CONTROL_PLANE_GRPC`
- `FANOUT_REDIS_ADDR`
- `FANOUT_NETCLOCK_ENABLED` / `FANOUT_NETCLOCK_MASTER_ADDR`

Full per-binary env var table: `cmd/grimnir-fanout/README.md`, `cmd/edge-encoder/README.md`, `CLAUDE.md`.

## New metrics & alerts

The `internal/metrics/` package ships HA-specific Prometheus metrics with per-binary registries; the rules & tests live in `ops/prometheus/rules/`. `make prometheus-validate` runs promtool against them. Key additions:

- `grimnir_pg_replication_lag_seconds` (from `internal/dbhealth/`)
- `grimnir_vrrp_master_count` & `grimnir_vrrp_state` (from `internal/vrrphealth/`; split-brain detector)
- `grimnir_listener_reconnects_total` (from the JS player telemetry endpoint)
- `grimnir_deploy_autorollback_triggers_total`

Alerts route through the `internal/alertbridge/` adapter (`cmd/alertmanager-ntfy`) to ntfy with three tiers: Tier-1 audit, Tier-2 page, Tier-3 page-and-rollback. Tier-3 also flips the `deploy_history` row to `failed` & invokes the auto-rollback observer in `internal/grimnirdeploy/autorollback/`.

## New runbooks

Every operator procedure is documented in `docs/runbooks/`. The index (`docs/runbooks/index.md`) maps symptom -> subcommand -> long-form runbook:

| Runbook | Trigger |
|---|---|
| `deploy.md` | New release ready, or rollback after a bad deploy |
| `drain.md` / `drain-a-node.md` | Reboot a node or swap hardware |
| `promote-replica.md` | Primary Postgres degraded |
| `cold-start-region.md` | Bringing up a new region |
| `restore-from-backup.md` | Data corruption; restore from pgbackrest |
| `recover-partition.md` | Network partition recovering between HA nodes |
| `backup-drill.md` | Quarterly: verify backups actually restore |
| `emergency-pause.md` | Active incident; freeze all deploys |
| `verify.md` | Triage: is the cluster healthy right now? |
| `fanout-down.md` | DJ can't connect; live ingest unreachable |
| `keepalived-install.md` | Initial VIP failover bring-up per region |
| `migrate-media-to-r2.md` | One-time cutover from local disk to R2 |
| `secrets/rotation.md` | Rotate a secret (routine or post-leak) |
| `docs/v2/UPGRADE.md` | The master v1 -> v2 upgrade |

## Custom JS player

The listener-facing browser player is a vanilla JS ES module at `internal/web/static/js/player/player.js`. No framework, no build step. It wraps `<audio>`, recycles the element on `error`/`stalled`/`waiting`/`ended`, & steps HQ -> LQ after 3 failures inside 30s. A background HEAD probe every 60s recovers back to HQ when the upstream returns. MediaSession metadata wires up lock-screen play/pause. See `internal/web/static/js/player/README.md` for the API.

The legacy `GlobalPlayer` class in `internal/web/static/js/app.js` is still wired into the dashboard layout for cross-page playback; the new module only owns the public `/listen` page & the `/embed/player?station=<id>` widget.

## Object-storage backend

`internal/media/` ships an S3 backend in parallel with the filesystem backend. Selected via `GRIMNIR_MEDIA_BACKEND=s3`. Defaults are R2-friendly (`region=auto`, `path_style=true`). AWS S3 & MinIO are supported via the same backend; see `docs/runbooks/migrate-media-to-r2.md` for the migration steps & `docs/OBJECT_STORAGE_EXPERIMENTAL.md` for the broader rationale.

## What was removed

Nothing. v2 is additive against the v1 codebase. Every v1 env var, endpoint, table, & binary still works exactly as it did in v1.31.1. The v1 binary pair continues to ship in the v2.0.0-rc.1 release & can be deployed alone (without the v2 HA wiring) indefinitely.

## Where the docs live

- Operator-facing entry point: `docs/v2/UPGRADE.md`
- Architecture overview: `docs/v2/ARCHITECTURE.md`
- 3am-page runbook directory: `docs/runbooks/index.md`
- Observability topology: `docs/observability/README.md`
- Parent HA design (876 lines): `docs/superpowers/plans/2026-06-01-ha-zero-loss-failover-design.md`
- Per-feature design plans: `docs/superpowers/plans/2026-06-*`

## Known gaps & rc-blockers

- WebDJ interface is still planned, not built. Live DJ ingest works via Harbor / RTP / SRT / WebRTC clients, but there's no in-browser DJ UI.
- EAS (Emergency Alert System) integration is still planned.
- Vault backend has been contract-tested but not production-soaked. The `.env` backend is the default for that reason.

None of these block the rc; they're tracked for v2.1+.
