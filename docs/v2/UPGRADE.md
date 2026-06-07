# Grimnir Radio v2 upgrade runbook

This is the master operator runbook for migrating a v1.x deployment (single VM, single binary pair, local-disk media) to the v2 HA topology (two proxmox VMs, four binaries per node, VRRP-floated VIPs, shared Postgres + Redis, R2-backed media, ntfy alerting). It walks every phase from "v1 in prod today" to "v2 cutover complete & v1 decommissioned".

Read it once end-to-end before starting. Every phase has a rollback paragraph; if the verification step at the end of the phase fails, you roll back that phase, not the whole upgrade.

Architecture reference: `docs/v2/ARCHITECTURE.md`. Per-runbook depth: `docs/runbooks/index.md`. Parent design (876 lines): `docs/superpowers/plans/2026-06-01-ha-zero-loss-failover-design.md`.

## Pre-flight: what v2 actually gives you

- Zero-listener-drop failover when one VM (or one binary on one VM) dies. Edge encoder buffers a sample-aligned mix from both media engines; loss of one is invisible to listeners.
- DJ failover when the fan-out on the active VM dies. Keepalived floats the DJ VIP; sessions reconnect in under 5s against the surviving fan-out.
- A single nginx reload on the edge VPS is the cutover; rollback is the same reload pointing back at v1.
- Shared media in R2 means either VM can serve any file without filesystem sync.
- Postgres physical replication + pgbackrest WAL archive to R2 means you can rebuild a region from cold in under an hour.

If you don't need any of that, stay on v1. v2 doubles the operator surface area.

---

## Phase 0a: operator-pending checklist

These are external dependencies. Until each is checked off, don't start Phase 1.

- [ ] **Cloudflare R2 buckets created**, one per region:
  - `grimnir-media-<region>` for media objects
  - `grimnir-backups-<region>` for pgbackrest WAL + base backups
  - API token scoped to both, with `Object Read & Write`. Record the Account ID, Access Key ID, Secret Access Key, and Endpoint URL (looks like `https://<account-id>.r2.cloudflarestorage.com`).
- [ ] **ntfy VPS reachable & topics provisioned**, one set per region:
  - `grimnir-region-<region>-page` (Tier-2 page)
  - `grimnir-audit-<region>` (Tier-1 audit trail)
  - `grimnir-region-<region>-rollback` (Tier-3 page-and-rollback)
  - Publisher tokens for each. The receiver tokens (operator-side) live on the phones / desktops that should buzz.
- [ ] **Edge VPS root access**, with nginx already terminating TLS for the public hostname (e.g. `<public-hostname>`). The cutover is a single `nginx -s reload` after the `upstream` block is rewritten.
- [ ] **Two proxmox VMs provisioned** in the same L2 segment, each with:
  - 4 vCPU, 8 GB RAM, 80 GB disk (more if you have lots of media in the read-through cache)
  - Ubuntu 24.04 LTS
  - A management IP and a free IP available for each VIP (listener + DJ)
  - Docker + docker compose plugin installed
  - `keepalived` package available via apt (don't install yet; Phase 5 does that)
- [ ] **A free Postgres host**, either a third VM or managed (e.g. Cloudflare Hyperdrive, Neon, or a Crunchy instance). Postgres 16+. Both VMs must reach it on TCP 5432.
- [ ] **A free Redis host** reachable from both VMs on TCP 6379. Either a third VM or managed.

Substrate sizing & decision rationale: `docs/superpowers/plans/2026-06-06-pgbackrest-config.md`.

## Phase 0b: substrate verification

Before the v2 binaries touch the substrate, prove the substrate is real & meets the contract.

```bash
# From each proxmox VM:
nc -zv <pg-host> 5432
nc -zv <redis-host> 6379

# From the operator workstation (you'll need psql installed):
psql "postgres://grimnir:$PG_PASSWORD@<pg-host>:5432/postgres" -tAc "SELECT version();"
# Expect: PostgreSQL 16.x or later

# Confirm pgbackrest is configured & can write to R2:
ssh <pg-host> sudo -u postgres pgbackrest --stanza=grimnir check
# Expect: "stanza-create command end: completed successfully"

# Confirm Redis works:
redis-cli -h <redis-host> ping
# Expect: PONG
```

If any of those fail, fix the substrate before continuing. v2 binaries assume the substrate is correct & will not start helpful error messages otherwise.

**Rollback**: nothing's been changed yet. Walk away.

---

## Phase 1: install grimnir-deploy on the operator workstation

`grimnir-deploy` is the single CLI for every mutating operation in v2. It runs from the operator workstation, talks to both proxmox VMs over SSH, and writes audit rows to Postgres for every action.

```bash
# Clone the repo on the operator workstation:
git clone https://github.com/friendsincode/grimnir_radio.git
cd grimnir_radio
git checkout v2.0.0-rc.1

# Build the binary:
go build -o /usr/local/bin/grimnir-deploy ./cmd/grimnir-deploy

# Configure it:
mkdir -p ~/.config/grimnir-deploy
cat > ~/.config/grimnir-deploy/config.yml <<'EOF'
region: prod-us
nodes:
  - host: <node-a-ip>
    user: <ssh-user>
    name: node-a
  - host: <node-b-ip>
    user: <ssh-user>
    name: node-b
postgres_dsn: postgres://grimnir:$PG_PASSWORD@<pg-host>:5432/grimnir?sslmode=require
redis_addr: <redis-host>:6379
ntfy:
  url: https://ntfy.grimnir.example
  topic_audit: grimnir-audit-prod-us
  topic_page: grimnir-region-prod-us-page
EOF

# Verify it can talk to the substrate:
grimnir-deploy verify
```

The verify output is a per-host, per-component table. Every cell should be OK or N/A. If anything is RED, fix the underlying problem before continuing; verify is the canary.

Per-subcommand reference: `docs/runbooks/index.md`. Each subcommand carries `--help` with the inline procedure & writes an audit row on start + completion.

**Rollback**: `rm /usr/local/bin/grimnir-deploy && rm -rf ~/.config/grimnir-deploy`. No state has been written anywhere.

---

## Phase 2: install Prometheus, Alertmanager, Grafana

The observability stack lives outside the HA nodes (typically on the same VM as Postgres, or a fourth small VM). It scrapes both nodes & all four binaries.

```bash
# On the observability host:
sudo apt-get install -y prometheus alertmanager
# Or use the Docker images; either works.

# Copy the provisioning configs from this repo:
sudo install -m 0644 ops/prometheus/prometheus.yml /etc/prometheus/prometheus.yml
sudo install -d -m 0755 /etc/prometheus/rules
sudo install -m 0644 ops/prometheus/rules/*.yml /etc/prometheus/rules/
sudo install -m 0644 ops/alertmanager/config.yml /etc/alertmanager/alertmanager.yml

# Validate the rules locally before reloading:
make prometheus-validate

# Reload:
sudo systemctl restart prometheus alertmanager
```

`ops/prometheus/README.md` lists every metric scraped per binary. `docs/observability/README.md` covers the topology end-to-end (what scrapes what, where alerts go, how secrets resolve).

For Grafana, the dashboard JSON & datasource provisioning live in `ops/grafana/`. Drop them under `/etc/grafana/provisioning/` (datasources) and `/var/lib/grafana/dashboards/` (dashboards), then restart Grafana. The HA dashboard ID is `grimnir-ha-overview`.

Wire the Alertmanager-to-ntfy bridge (`cmd/alertmanager-ntfy`) as a loopback sidecar. The full config is in `docs/observability/README.md § alertbridge`. The bridge gives you three severity tiers; Tier-3 pages AND auto-rollbacks (see `internal/grimnirdeploy/autorollback/`).

**Verify the stack**:

```bash
curl -s http://<obs-host>:9090/-/healthy            # Prometheus
curl -s http://<obs-host>:9093/-/healthy            # Alertmanager
curl -s http://<obs-host>:3000/api/health           # Grafana
# Send a test alert through the bridge:
amtool alert add test_alert severity=info team=grimnir summary="cutover-test"
# You should see a Tier-1 ntfy land on the audit topic within 30s.
```

**Rollback**: stop the three services. They don't write to Postgres; nothing on the substrate cares.

---

## Phase 3: A3 deploy to proxmox + 1 week soak

A3 means "two nodes, alpha 3 binaries, fan-out & VIP not yet wired". The point of this phase is to prove the binaries run side-by-side against shared Postgres + Redis without the fancy HA wiring. If something's broken at this layer it's easier to find without keepalived in the middle.

```bash
# On each proxmox VM, install the v2 docker compose stack:
ssh <ssh-user>@<node-a-ip>
git clone https://github.com/friendsincode/grimnir_radio.git /srv/docker/grimnir_radio
cd /srv/docker/grimnir_radio
git checkout v2.0.0-rc.1
cp docker-compose.v2.example.yml docker-compose.override.yml
# Edit docker-compose.override.yml: point DB_DSN, REDIS_ADDR at the substrate.
# Don't set HA_PCM_RTP_ENABLED yet; this phase runs both nodes independent.

./grimnir up -d
./grimnir ps   # expect grimnir-radio, grimnir-mediaengine, grimnir-edge-encoder, grimnir-fanout all running
./grimnir logs -f grimnir-radio   # confirm migrations ran clean; expect "scheduler started"

# Repeat on <node-b-ip>.
```

Now both VMs are running v2 binaries against the same Postgres. Listeners can hit either VM directly (without the VIP) on port 8001. Stream a song from both & confirm audio is identical.

**Soak**: let it run for 1 week. Watch the Grafana dashboard. The acceptance gate is:

- Zero unplanned restarts of any container on either node
- Zero `5xx` from `/api/v1/*` outside known maintenance windows
- Zero `grimnir_scheduler_overlap_errors_total` increases
- Replication lag < 5s sustained

If the soak passes, continue to Phase 4. If it fails, capture logs, file an issue, and stay on v1.

**Rollback**: `./grimnir down` on both VMs. v1 in prod is untouched; this phase ran in parallel against the same DB but only as a passive consumer.

---

## Phase 4: media migration to R2

Up to this point both VMs have been reading media from local disk (the legacy `/srv/data/grimnir_radio/media-data` volume). HA requires shared media; the cutover is to Cloudflare R2.

Follow `docs/runbooks/migrate-media-to-r2.md` end-to-end. The summary:

1. `rclone sync` the local volume up to the R2 bucket (90 min for ~100k files).
2. Flip `GRIMNIR_MEDIA_BACKEND=s3` plus the R2 env vars on both VMs.
3. `./grimnir up -d` to restart with the new backend.
4. Verify a known media item plays from each VM.

The on-disk path stays as a read-through cache (`GRIMNIR_MEDIA_ROOT` is still required). Cache misses fetch from R2; cache hits stay local.

**Rollback**: flip `GRIMNIR_MEDIA_BACKEND=fs` & restart. The local volume is still authoritative until you delete it. Don't delete it until Phase 8 completes.

---

## Phase 5: keepalived + grimnir-fanout

This phase wires the two VRRP VIPs (listener + DJ) & turns on the live-input fan-out.

**Keepalived**: follow `docs/runbooks/keepalived-install.md`. The runbook covers the per-region VIP table, the install steps on both nodes, the health-check scripts, and how to confirm the VIP can actually float (drop priority on the master and watch the backup take over within 3s).

**grimnir-fanout**: it's already running from Phase 3 (it's one of the four containers in the v2 compose stack). What you're doing here is enabling the engine-side mixer branch & turning on cross-node session replication:

```bash
# On both VMs, add these to docker-compose.override.yml under the mediaengine env:
GRIMNIR_HA_PCM_RTP_ENABLED=true
GRIMNIR_HA_PCM_RTP_TARGETS=<node-a-ip>:5004,<node-b-ip>:5004
GRIMNIR_NETCLOCK_ENABLED=true
GRIMNIR_NETCLOCK_REGION=prod-us
GRIMNIR_NETCLOCK_MASTER_ADDR=<node-a-ip>:9094

# And these to the grimnir-fanout env:
FANOUT_ENGINE_A_RTP=<node-a-ip>:5005
FANOUT_ENGINE_B_RTP=<node-b-ip>:5005
FANOUT_REDIS_ADDR=<redis-host>:6379
FANOUT_NETCLOCK_ENABLED=true
FANOUT_NETCLOCK_MASTER_ADDR=<node-a-ip>:9094

./grimnir up -d
```

The grimnir-fanout README (`cmd/grimnir-fanout/README.md`) has the full env table. The engine-side mixer branch design is in `docs/superpowers/plans/2026-06-05-live-input-fan-out.md`.

**Verify**:

```bash
# Listener VIP works:
curl -I http://<listener-vip>:8001/stream
# DJ VIP works:
curl http://<dj-vip>:8003/healthz
# Drop priority on node A, watch node B take over:
ssh <ssh-user>@<node-a-ip> sudo systemctl stop keepalived
# Within 3s, node B is master for both VIPs. Listener stream stays connected.
ssh <ssh-user>@<node-a-ip> sudo systemctl start keepalived
# Priority returns; node A takes back the VIPs.
```

**Rollback**: `sudo systemctl stop keepalived` on both nodes; flip `GRIMNIR_HA_PCM_RTP_ENABLED=false` & restart. Phase 3 topology returns.

---

## Phase 6: cutover dry-runs

Before the real cutover, rehearse the nginx swap on the edge VPS into a staging hostname.

```bash
# On the edge VPS:
sudo cp /etc/nginx/sites-available/<public-hostname> /etc/nginx/sites-available/staging.<public-hostname>
# Edit staging.<public-hostname>:
#   server_name staging.<public-hostname>;
#   upstream grimnir { server <listener-vip>:8001; }
sudo ln -s /etc/nginx/sites-available/staging.<public-hostname> /etc/nginx/sites-enabled/
sudo nginx -t
sudo nginx -s reload

# Confirm staging is serving from v2:
curl -I https://staging.<public-hostname>/stream
# Should hit the v2 listener VIP.
```

Stream from staging for 24h. Watch the Grafana dashboard. If the dry-run is clean, continue.

**Rollback**: remove the staging vhost & reload nginx. v1 is still in production for the real hostname.

---

## Phase 7: the cutover

The actual production cutover is one nginx upstream change & one reload.

```bash
# On the edge VPS:
sudo vi /etc/nginx/sites-available/<public-hostname>
# Change:
#   upstream grimnir { server <v1-prod-host>:8081; }   # v1
# To:
#   upstream grimnir { server <listener-vip>:8001; }   # v2
sudo nginx -t
sudo nginx -s reload
```

That's it. New listener connections land on the v2 listener VIP. In-flight v1 connections drain naturally as listeners reconnect.

**Verify within 60s**:

```bash
# From the operator workstation:
curl -sI https://<public-hostname>/stream | head -5
# X-Served-By should now be the v2 edge-encoder.

grimnir-deploy verify
# Every cell OK. listener_connections gauge climbing on the v2 side.
```

If anything is wrong, **rollback is the same reload pointing the upstream back at v1**:

```bash
sudo vi /etc/nginx/sites-available/<public-hostname>   # restore old upstream
sudo nginx -s reload
```

You're back on v1 within 10s. v2 keeps running; you triage the failure offline.

---

## Phase 8: post-cutover validation & v1 deprecation

Let the v2 stack run for 1 week against production traffic. Acceptance gate:

- Zero unplanned VRRP transitions
- Zero rows in `deploy_history` with phase = `failed`
- Listener reconnect rate (from the JS player telemetry on `/api/v1/listener-events`) under the pre-cutover baseline
- Postgres replication lag < 5s sustained
- pgbackrest backup-drill passes (`grimnir-deploy backup-drill --region=prod-us --drill-host=<scratch-vm>`)

If all five hold, decommission v1:

```bash
ssh <ssh-user>@<v1-prod-host>
cd /srv/docker/grimnir_radio
./grimnir down
# Don't delete the volume yet. Wait another week. Then:
sudo rm -rf /srv/data/grimnir_radio/{media-data,postgres-data,media-data.pre-r2-backup}
```

**Rollback at Phase 8 is the same as Phase 7**: nginx reload back to v1's address. Until you delete the v1 volumes, v1 is recoverable.

---

## Per-phase rollback summary

| Phase | What you did | Rollback |
|---|---|---|
| 0 | Provisioning | Nothing to roll back |
| 1 | Installed grimnir-deploy | Delete the binary & config |
| 2 | Stood up observability | `systemctl stop prometheus alertmanager grafana-server` |
| 3 | A3 deploy alongside v1 | `./grimnir down` on both VMs |
| 4 | Flipped media to R2 | `GRIMNIR_MEDIA_BACKEND=fs` & restart |
| 5 | Enabled keepalived + fan-out HA | Stop keepalived; disable PCM-RTP env |
| 6 | Staging cutover dry-run | Remove staging vhost; reload nginx |
| 7 | Production cutover | Nginx upstream back to v1; reload |
| 8 | v1 decommission | Don't `rm -rf` the volumes for 1 wk |

## When in doubt

- The verify command is always safe. Run it before & after every phase.
- Every mutating subcommand has `--dry-run`. Use it.
- Every subcommand writes an `audit_log` row & posts to the audit ntfy topic. If you can't remember what you did 20 minutes ago, the audit log can.
- `docs/runbooks/index.md` is the 3am-page directory. Each row maps a symptom to a subcommand & a long-form runbook.

If the upgrade goes sideways in a way no runbook covers, stop. Hold the position. Capture the state. File an issue. The v2 stack is designed so that "halted partway through Phase N" is a stable state; nothing requires the upgrade to complete on a deadline.
