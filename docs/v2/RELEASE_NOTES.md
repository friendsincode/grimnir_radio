# Grimnir Radio 2.0.0 release notes

2.0.0 is the first stable release of the high-availability (HA) architecture. It folds eleven alpha tags & ten release candidates (`v2.0.0-alpha.1` through `v2.0.0-rc.10`) into one shippable line. The last v1 release was `v1.40.9`. v2 runs against the same database & keeps every v1 binary, so you adopt it on your own schedule.

Upgrade guide: [`docs/UPGRADING.md`](../UPGRADING.md). Breaking changes & build requirements: [`docs/v2/BREAKING_CHANGES.md`](BREAKING_CHANGES.md). Full HA cutover runbook: [`docs/v2/UPGRADE.md`](UPGRADE.md). Architecture: [`docs/v2/ARCHITECTURE.md`](ARCHITECTURE.md).

## The short version

v1 ran two binaries on one box: the control plane (`grimnirradio`) & the media engine (`mediaengine`). v2 keeps both unchanged & adds two runtime binaries plus operator tooling, so one deployment can lose a node without dropping a listener. If you run a single station box & don't need failover, the v1-style single-node layout still runs on the 2.0 images. You pull the new images, restart, & the migrations apply themselves.

Nothing from v1 was removed. Every v1 env var, API endpoint, database table, & binary behaves as it did on `v1.40.9`. The breaking changes are about how you build & deploy, not about what the running system does; they're listed in `BREAKING_CHANGES.md`.

## New binaries & tooling

| Binary | Purpose | First shipped | Published image |
|---|---|---|---|
| `cmd/edge-encoder` | Ingests PCM-over-RTP from N media engines, sample-aligned input switching on engine failure, serves HTTP/ICY + HLS to listeners. Built on go-gst CGo bindings. | `v2.0.0-alpha.3` | none yet; build from source |
| `cmd/grimnir-fanout` | Accepts one DJ over Harbor / RTP / SRT / WebRTC & duplicates the audio as PCM-over-RTP to every media engine. Flips the engine-side mixer branch over gRPC. Built on go-gst CGo bindings. | `v2.0.0-alpha.7` | `ghcr.io/friendsincode/grimnir_fanout` |
| `cmd/grimnir-deploy` | Operator CLI for every mutating cluster operation. Writes an `audit_log` row on start & completion. Every subcommand carries `--dry-run` & `--help`. | `v2.0.0-alpha.5` | none; build from source |
| `cmd/alertmanager-ntfy` | Loopback sidecar that turns Alertmanager webhooks into ntfy.sh pushes across three severity tiers. | `v2.0.0-alpha.6` | none; build from source |

The two v1 binaries (`cmd/grimnirradio`, `cmd/mediaengine`) keep their roles & ship as `ghcr.io/friendsincode/grimnir_radio` & `ghcr.io/friendsincode/grimnir_mediaengine`. They gained HA env vars (listed below) that default to off.

## Zero-loss failover

The reason the topology grew from one box to two: a single node can die without a listener noticing.

- The edge encoder buffers a sample-aligned mix from both media engines. Lose one engine & the switch to the other is inaudible.
- Keepalived floats two VRRP VIPs (listener + DJ). When the active node drops, DJ sessions reconnect against the surviving fan-out in under 5s.
- The production cutover is one nginx `upstream` rewrite & one reload. Rollback is the same reload pointing back at v1.
- Postgres physical replication plus a pgbackrest WAL archive to MinIO rebuilds a region from cold in under an hour.

## New API endpoints

Two unauthenticated endpoints drive the custom JS player on the public listen page:

- `GET /api/v1/stations/<id>/streams` returns the ordered `StationStream` list, HQ first & LQ second. Handler: `internal/api/streams.go`.
- `POST /api/v1/listener-events` takes anonymous reconnect telemetry (`play`, `stop`, `reconnect`, `degrade`, `upgrade`, `exhausted`). It rate-limits to 10 requests/min/IP in-process & logs the IP without storing it. Handler: `internal/api/listener_events.go`.

The authenticated v1 API surface is unchanged.

## New gRPC surface

`proto/mediaengine/v1/liveinput.proto` adds `LiveInputControl.SetLiveInput(active bool)`, which lets the fan-out mix a DJ in or out of the always-on engine-side `audiomixer` branch. The engine reads `x-grimnir-source-addr` & `x-grimnir-session-id` from gRPC metadata so the audit log can trace which DJ session caused a given mixer transition.

## New database tables

- `deploy_history` records one row per `grimnir-deploy deploy`: `tag`, `previous_tag`, `phase` (`started` / `complete` / `failed`), `started_at`, `completed_at`, `operator`, `reason`.
- `audit_log` records one row per mutating subcommand: `operator`, `subcommand`, `args`, `started_at`, `duration_ms`, `outcome`.

Both follow the expand-only migration discipline in `docs/MIGRATIONS.md`. No v1 table was renamed, dropped, or had a column removed, so v1.40.9 code still reads a 2.0-migrated database.

## New env vars

Roughly 90 env vars span seven categories. The canonical operator template is `.env.example`; the v1 surface is preserved at `.env.v1.example`. Every HA var defaults to off, so a v1 config keeps working untouched. Full tables live in `CLAUDE.md` & the per-binary READMEs. The HA-relevant additions:

- Media engine: `GRIMNIR_HA_PCM_RTP_ENABLED`, `GRIMNIR_HA_PCM_RTP_TARGETS`, `GRIMNIR_NETCLOCK_ENABLED`, `GRIMNIR_NETCLOCK_PORT`, `GRIMNIR_NETCLOCK_REGION`, `GRIMNIR_NETCLOCK_MASTER_ADDR`, `GRIMNIR_LIVE_INPUT_*`.
- Control plane: `GRIMNIR_REGION`, `GRIMNIR_MEDIA_BACKEND`, the `GRIMNIR_S3_*` set, `GRIMNIR_VRRP_VIPS`, the `GRIMNIR_NTFY_*` set, `GRIMNIR_SECRETS_BACKEND`, the `VAULT_*` set, `GRIMNIR_GRPC_ADDR` (DJAuth gRPC, default port 9095).
- Fan-out: `FANOUT_ENGINE_A_RTP` / `FANOUT_ENGINE_B_RTP`, the `FANOUT_*_PORT` set, `FANOUT_CONTROL_PLANE_GRPC`, `FANOUT_REDIS_ADDR`, `FANOUT_NETCLOCK_*`.
- Auto-rollback observer: `GRIMNIR_PROMETHEUS_URL`, `GRIMNIR_DEPLOY_AUTOROLLBACK_ENABLED`, `GRIMNIR_DEPLOY_AUTOROLLBACK_PROM_URL`, `GRIMNIR_DEPLOY_AUTOROLLBACK_TICK`.

## New metrics & alerts

`internal/metrics/` ships HA Prometheus metrics with per-binary registries; the rules & their unit tests live in `ops/prometheus/rules/`, validated by `make prometheus-validate`. Key gauges & counters:

- `grimnir_pg_replication_lag_seconds` (from `internal/dbhealth/`)
- `grimnir_vrrp_master_count` & `grimnir_vrrp_state` (from `internal/vrrphealth/`, the split-brain detector)
- `grimnir_listener_reconnects_total` (from the JS player telemetry endpoint)
- `grimnir_deploy_autorollback_triggers_total`

Alerts route through `internal/alertbridge/` (the `cmd/alertmanager-ntfy` sidecar) to ntfy across three tiers: Tier-1 audit, Tier-2 page, Tier-3 page-and-rollback. Tier-3 flips the `deploy_history` row to `failed` & wakes the auto-rollback observer in `internal/grimnirdeploy/autorollback/`.

## Object-storage media backend

`internal/media/` ships an S3 backend alongside the filesystem backend, selected with `GRIMNIR_MEDIA_BACKEND=s3`. The defaults suit MinIO (`region=auto`, `path_style=true`). The same backend also works against AWS S3. Migration steps are in `docs/runbooks/migrate-media-to-minio.md`; the rationale is in `docs/OBJECT_STORAGE_EXPERIMENTAL.md`. The on-disk path stays as a read-through cache, so `GRIMNIR_MEDIA_ROOT` is still required even with the S3 backend.

## Custom JS player

The listener-facing browser player is a vanilla JS ES module at `internal/web/static/js/player/player.js`. No framework & no build step. It wraps `<audio>`, recycles the element on `error` / `stalled` / `waiting` / `ended`, & steps from HQ to LQ after 3 failures inside 30s. A background HEAD probe every 60s recovers to HQ when the upstream returns. MediaSession metadata wires up lock-screen play/pause. The legacy `GlobalPlayer` in `internal/web/static/js/app.js` still drives cross-page dashboard playback; the new module owns the public `/listen` page & the `/embed/player?station=<id>` widget. API: `internal/web/static/js/player/README.md`.

## What was removed

Nothing. v2 is additive against the v1 codebase. The v1 binary pair ships in 2.0.0 & deploys on its own, without any HA wiring, for as long as you want.

## Known gaps

- The in-browser WebDJ interface is planned, not built. Live DJ ingest works through Harbor / RTP / SRT / WebRTC clients; there's no browser DJ UI yet.
- EAS (Emergency Alert System) integration is planned.
- The Vault secrets backend is contract-tested but not production-soaked. The `.env` backend is the default for that reason.

These are tracked for v2.1+.
