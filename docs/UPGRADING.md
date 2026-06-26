# Upgrading Grimnir Radio v1.40.x to 2.0.0

This is the entry point for moving a v1 deployment to 2.0.0. Read [`docs/v2/BREAKING_CHANGES.md`](v2/BREAKING_CHANGES.md) first; it's short & it tells you whether anything in your setup needs attention before you start. The feature inventory is in [`docs/v2/RELEASE_NOTES.md`](v2/RELEASE_NOTES.md).

There are two paths, & most people want the first one.

- **Single-node**: you run one box, you don't need failover, & you want the 2.0 features without the HA topology. Pull the new images, restart, done. Covered in full below.
- **High availability**: you want zero-listener-drop failover across two nodes with VRRP VIPs, shared Postgres + Redis, & MinIO-backed media. That's a multi-day project with its own runbook: [`docs/v2/UPGRADE.md`](v2/UPGRADE.md). The overview is at the bottom of this page.

The last v1 release was `v1.40.9`. Everything below assumes you're on the v1.40.x line.

## Single-node upgrade

Nothing in v2 removed v1 behavior, & the migrations only add tables & columns. So v1.40.9 code can still read the database after you migrate, which is what makes the rollback at the end a 30-second operation.

### 1. Back up the database & media

Take the backup before you touch anything. From the deploy directory (`/srv/docker/grimnir_radio` on a standard install):

```bash
# Database dump:
docker exec grimnir-postgres pg_dump -U grimnir grimnir > grimnir-pre-2.0-$(date +%F).sql

# Media volume: skip this if you already store media in MinIO/S3.
# Otherwise copy the host path that backs the media-data volume, e.g.:
sudo cp -a /srv/data/grimnir_radio/media-data /srv/data/grimnir_radio/media-data.pre-2.0
```

### 2. Pin the image tags

`:latest` now tracks the 2.0 line, so pin the version in `docker-compose.override.yml` to control when you move:

```yaml
services:
  grimnir:
    image: ghcr.io/friendsincode/grimnir_radio:v2.0.0
  mediaengine:
    image: ghcr.io/friendsincode/grimnir_mediaengine:v2.0.0
```

### 3. Pull & restart

Use the `./grimnir` wrapper, not raw `docker compose`; the wrapper orders the compose files correctly.

```bash
./grimnir pull
./grimnir up -d
```

The control plane runs the migrations on startup. They're expand-only, so they add the `deploy_history` & `audit_log` tables & leave every v1 table alone.

### 4. Verify

```bash
# Migrations ran & the scheduler came up:
./grimnir logs grimnir | grep -E "version=|scheduler started" | tail -5

# The API answers (8080 is the in-container default; use whatever host port you mapped):
curl -sI http://127.0.0.1:8080/api/v1/stations | head -1   # expect 401 (auth required) or 200

# Containers are healthy:
./grimnir ps
```

Then open the public listen page & confirm a station plays. The `version=` line in the logs should read 2.0.0.

### 5. Roll back if you need to

Point the tags back at v1.40.9 & restart. The 2.0 schema additions are invisible to v1 code, so no database downgrade is needed.

```bash
# In docker-compose.override.yml, set both images back to :v1.40.9, then:
./grimnir pull
./grimnir up -d
```

Keep the pre-2.0 database dump & the `media-data.pre-2.0` copy until 2.0 has run clean for a week. After that, delete them.

## What you get without going HA

The single-node 2.0 stack gives you the parts of v2 that don't need a second node:

- The custom JS player on `/listen` & the `/embed/player?station=<id>` widget, with automatic HQ-to-LQ fallback & recovery.
- The S3 media backend (`GRIMNIR_MEDIA_BACKEND=s3`), if you'd rather serve media from object storage (self-hosted MinIO) than local disk.
- The two public player endpoints (`/api/v1/stations/<id>/streams` & `/api/v1/listener-events`).

The edge encoder, fan-out, VRRP failover, & `grimnir-deploy` are HA features. They sit idle until you wire them, so they cost you nothing on a single node.

## High-availability upgrade

The HA path turns one box into two nodes that survive each other's failure. It adds the edge encoder & fan-out binaries, keepalived VIPs, an external Postgres 16 + Redis, self-hosted MinIO on its own VM for media & backups, ntfy alerting, & a Prometheus/Alertmanager/Grafana stack. The HA cutover & failover are handled at the external entry point (the edge VPS, `192.168.195.1`): a single nginx `upstream` rewrite there points listeners at v2 or back at v1.

Don't improvise it. [`docs/v2/UPGRADE.md`](v2/UPGRADE.md) is the phase-by-phase runbook, & each phase has its own rollback paragraph. The shape:

| Phase | What happens |
|---|---|
| 0 | Provision MinIO, ntfy, the edge VPS, two nodes, external Postgres 16 + Redis; verify the substrate |
| 1 | Build & configure `grimnir-deploy` on the operator workstation |
| 2 | Stand up Prometheus, Alertmanager, Grafana, & the ntfy bridge |
| 3 | Deploy the v2 stack on both nodes alongside v1; soak 1 week |
| 4 | Migrate media to MinIO (`GRIMNIR_MEDIA_BACKEND=s3`) |
| 5 | Enable keepalived VIPs & the fan-out HA wiring |
| 6 | Rehearse the nginx cutover against a staging hostname |
| 7 | Cut over: one `upstream` rewrite + `nginx -s reload` |
| 8 | Soak 1 week, then decommission v1 |

The acceptance gate at each soak is in the runbook: zero unplanned restarts, zero `5xx` on `/api/v1/*`, replication lag under 5s, & no `failed` rows in `deploy_history`. If a phase fails verification, you roll back that phase, not the whole upgrade. A deployment halted partway through any phase is a stable state.
