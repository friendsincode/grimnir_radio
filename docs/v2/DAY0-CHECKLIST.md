# v2.0.0 cutover — Day 0 operator checklist

Print this. Tick boxes as you go. Don't skip the rollback note at the end of every phase; the v2 design assumes you stop & roll back if a phase's verify fails.

This checklist tracks `docs/v2/UPGRADE.md` 1-to-1 but compresses it into a single sheet with copy-pasteable commands. Read UPGRADE.md once end-to-end first; this is the field reference.

**Version pinned for this cutover**: `v2.0.0-rc.5` (commit `709f4ce`). If the tag in UPGRADE.md says `rc.1`, that's stale; use `rc.5`.

**Pre-deploy audit**: `docs/v2/2026-06-08-pre-deploy-audit.md`. Open it now & confirm the one blocker (B-1, version-string drift) is patched in your local UPGRADE.md before you start.

---

## Pre-flight (do these BEFORE touching prod)

These are external; until each ticks, don't start Phase 0b.

- [ ] **Cloudflare R2 account exists**; payment method on file
- [ ] **R2 bucket #1 created**: `grimnir-media-<region>` in your primary R2 region
- [ ] **R2 bucket #2 created**: `grimnir-backups-<region>` in a different R2 region (DR)
- [ ] **R2 bucket #3 created**: `grimnir-hls-<region>` for HLS segment offload (edge-encoder writes here)
- [ ] **R2 API token created**, scoped to those three buckets, with `Object Read & Write`. Record in password manager:
  - Account ID
  - Access Key ID
  - Secret Access Key
  - Endpoint URL (`https://<account-id>.r2.cloudflarestorage.com`)
- [ ] **ntfy VPS provisioned**; small (1 vCPU / 1 GB RAM is plenty); hostname recorded
- [ ] **ntfy installed** via `apt install ntfy` or the official Docker image
- [ ] **Caddy or nginx** in front of ntfy doing TLS termination
- [ ] **3 ntfy topics created** for the region (replace `<region>`):
  - `grimnir-audit-<region>` — Tier-1 audit trail (every mutating subcommand posts here)
  - `grimnir-region-<region>-page` — Tier-2 alerts
  - `grimnir-region-<region>-rollback` — Tier-3 auto-rollback page
- [ ] **Publisher tokens** for each of the three topics, recorded in password manager
- [ ] **Receiver tokens** on the operator phone(s) + desktop(s) that should buzz
- [ ] **Postgres host** provisioned; Postgres 16+; reachable from both VMs on TCP 5432
- [ ] **pgbackrest installed** on the Postgres host & configured to write to the `grimnir-backups-<region>` R2 bucket
- [ ] **Redis host** provisioned; reachable from both VMs on TCP 6379
- [ ] **Two proxmox VMs** provisioned in the same L2 segment:
  - 4 vCPU, 8 GB RAM, 80 GB disk
  - Ubuntu 24.04 LTS
  - Management IP + a free IP for each VIP (listener + DJ)
  - Docker + docker compose plugin installed
  - `keepalived` apt package available (do not install yet)
- [ ] **Edge VPS root access** confirmed; nginx already terminating TLS for the public hostname
- [ ] **SSH key** generated on operator workstation: `ssh-keygen -t ed25519 -f ~/.ssh/grimnir-deploy-ed25519 -C "grimnir-deploy"`
- [ ] **SSH key authorized** on both proxmox VMs under the `<ssh-user>` (or your) user
- [ ] **`grimnir-deploy` binary** built from rc.5 & on `$PATH`: `go build -o /usr/local/bin/grimnir-deploy ./cmd/grimnir-deploy`
- [ ] **`/etc/grimnir/secrets.env`** created on each VM with 0600 perms, containing the credentials enumerated in `.env.example`
- [ ] **`.env`** copied to `/srv/docker/grimnir_radio/.env` on each VM (use `.env.example` as the template; the v1 surface is preserved at `.env.v1.example` for reference)
- [ ] **VRRP VIPs allocated**: listener VIP IP & DJ VIP IP recorded, both inside the same L2 segment as the two VMs
- [ ] **DNS plan**: edge VPS upstream block already supports a swap from `<v1-prod-host>:8081` to `<listener-vip>:8001`

---

## Phase 0b — Substrate verification

Run from each proxmox VM. Each command must pass before you continue.

```bash
# Postgres reachable & version OK
nc -zv pg.example.internal 5432
psql "postgres://grimnir:$GRIMNIR_PW@pg.example.internal:5432/postgres" -tAc "SELECT version();"
#   expect: PostgreSQL 16.x or later

# pgbackrest can write to R2
ssh pg.example.internal sudo -u postgres pgbackrest --stanza=grimnir check
#   expect: "stanza-create command end: completed successfully"

# Redis reachable & auth OK
redis-cli -h redis.example.internal -a "$REDIS_PW" ping
#   expect: PONG

# R2 bucket reachable (use rclone or aws s3 ls with --endpoint-url)
aws s3 ls s3://grimnir-media-<region> --endpoint-url=https://<account-id>.r2.cloudflarestorage.com
#   expect: empty (or whatever's already there)
```

- [ ] All four substrate checks pass on node-a
- [ ] All four substrate checks pass on node-b

**Rollback**: nothing changed. Walk away. Fix substrate before retry.

---

## Phase 1 — Install grimnir-deploy

```bash
# On the operator workstation:
git clone https://github.com/friendsincode/grimnir_radio.git
cd grimnir_radio
git checkout v2.0.0-rc.5
go build -o /usr/local/bin/grimnir-deploy ./cmd/grimnir-deploy

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
postgres_dsn: postgres://grimnir:<GRIMNIR_PW>@pg.example.internal:5432/grimnir?sslmode=require
redis_addr: redis.example.internal:6379
ntfy:
  url: https://ntfy.example.com
  topic_audit: grimnir-audit-prod-us
  topic_page: grimnir-region-prod-us-page
EOF

# Export the deploy-tool env vars
export GRIMNIR_DEPLOY_PEER_HOST=<node-b-ip>
export GRIMNIR_DEPLOY_PEER_SSH_USER=<ssh-user>
export GRIMNIR_DEPLOY_PEER_SSH_KEY=~/.ssh/grimnir-deploy-ed25519
export GRIMNIR_DEPLOY_OPERATOR=$USER

# Verify
grimnir-deploy verify
```

- [ ] `grimnir-deploy verify` returns a per-host, per-component table
- [ ] Every cell is `OK` or `N/A`; nothing RED
- [ ] Audit ntfy got a Tier-1 ping for the verify invocation

**Rollback**: `rm /usr/local/bin/grimnir-deploy && rm -rf ~/.config/grimnir-deploy`. No state was written.

---

## Phase 2 — Prometheus, Alertmanager, Grafana

On the observability host (typically the Postgres VM or a fourth small VM):

```bash
sudo apt-get install -y prometheus alertmanager
sudo install -m 0644 ops/prometheus/prometheus.yml /etc/prometheus/prometheus.yml
sudo install -d -m 0755 /etc/prometheus/rules
sudo install -m 0644 ops/prometheus/rules/*.yml /etc/prometheus/rules/
sudo install -m 0644 ops/alertmanager/config.yml /etc/alertmanager/alertmanager.yml

make prometheus-validate                  # validates rules locally before reload
sudo systemctl restart prometheus alertmanager
```

For Grafana:

```bash
sudo install -d -m 0755 /etc/grafana/provisioning/datasources
sudo install -m 0644 ops/grafana/provisioning/datasources/*.yml /etc/grafana/provisioning/datasources/
sudo install -d -m 0755 /etc/grafana/provisioning/dashboards
sudo install -m 0644 ops/grafana/provisioning/dashboards/*.yml /etc/grafana/provisioning/dashboards/
sudo install -d -m 0755 /var/lib/grafana/dashboards
sudo install -m 0644 ops/grafana/dashboards/*.json /var/lib/grafana/dashboards/
sudo systemctl restart grafana-server
```

Then wire the alertmanager-ntfy bridge as a loopback sidecar (full config in `docs/observability/README.md § alertbridge`).

Verify:

```bash
curl -s http://<obs-host>:9090/-/healthy        # Prometheus
curl -s http://<obs-host>:9093/-/healthy        # Alertmanager
curl -s http://<obs-host>:3000/api/health       # Grafana
amtool alert add test_alert severity=info team=grimnir summary="cutover-test"
```

- [ ] Prometheus healthy
- [ ] Alertmanager healthy
- [ ] Grafana healthy & dashboards loaded (`grimnir-ha-overview` visible)
- [ ] Test alert lands on the audit ntfy topic within 30s

**Rollback**: `systemctl stop prometheus alertmanager grafana-server`. They write nothing the substrate cares about.

---

## Phase 3 — A3 deploy to proxmox + 1 week soak

The goal: prove the v2 binaries run against the shared substrate without the fancy HA wiring. Failures here are easier to diagnose without keepalived in the middle.

On each VM:

```bash
ssh <ssh-user>@<node-a-ip>
sudo mkdir -p /srv/docker/grimnir_radio
sudo chown <ssh-user>:<ssh-user> /srv/docker/grimnir_radio
git clone https://github.com/friendsincode/grimnir_radio.git /srv/docker/grimnir_radio
cd /srv/docker/grimnir_radio
git checkout v2.0.0-rc.5

# Copy the v1 single-VM template; do NOT set HA_PCM_RTP_ENABLED yet
cp docker-compose.override.yml.example docker-compose.override.yml
# Edit docker-compose.override.yml: point DB_DSN, REDIS_ADDR at the substrate
# (or rely on the .env file you placed in Pre-flight)

./grimnir up -d
./grimnir ps                # expect grimnir-radio, grimnir-mediaengine running
./grimnir logs -f grimnir-radio
#   expect: "scheduler started" + clean migration logs
```

Repeat on `<node-b-ip>`. Stream a song from each VM directly (no VIP) on port `8001` (edge-encoder default) or `8080` (control-plane HTTP).

Soak for 1 week. Acceptance gates:

- [ ] Zero unplanned container restarts on either VM
- [ ] Zero 5xx from `/api/v1/*` outside maintenance windows
- [ ] Zero `grimnir_scheduler_overlap_errors_total` increases
- [ ] Postgres replication lag < 5s sustained (Grafana panel: HA Overview → Replication)

**Rollback**: `./grimnir down` on both VMs. v1 in prod is untouched; Phase 3 was a passive consumer.

---

## Phase 4 — Media migration to R2

Follow `docs/runbooks/migrate-media-to-r2.md` end-to-end. Summary:

```bash
# On one of the VMs (the one with the v1 media volume):
rclone sync /srv/data/grimnir_radio/media-data r2:grimnir-media-prod-us \
  --transfers 16 --checkers 32 --progress
# Expect ~90 min for 100k files at ~50 GB; longer for bigger libraries.

# On BOTH VMs, edit /srv/docker/grimnir_radio/.env:
GRIMNIR_MEDIA_BACKEND=s3
GRIMNIR_S3_BUCKET=grimnir-media-prod-us
GRIMNIR_S3_REGION=auto
GRIMNIR_S3_ENDPOINT=https://<account-id>.r2.cloudflarestorage.com
GRIMNIR_S3_ACCESS_KEY=<R2-KEY>
GRIMNIR_S3_SECRET_KEY=<R2-SECRET>
GRIMNIR_S3_PATH_STYLE=true

./grimnir up -d              # on both VMs
```

- [ ] rclone sync completes with exit 0
- [ ] Both VMs restart cleanly with `GRIMNIR_MEDIA_BACKEND=s3`
- [ ] A known media item plays from each VM (test: pick a track from the library, hit play, verify audio)
- [ ] R2 bandwidth visible in Cloudflare dashboard (proves cache misses are fetching from R2)

**Rollback**: flip `GRIMNIR_MEDIA_BACKEND=fs` & restart. Local volume is still authoritative until you delete it; **don't delete until Phase 8 acceptance gates pass**.

---

## Phase 5 — keepalived + grimnir-fanout HA wiring

**Keepalived**: follow `docs/runbooks/keepalived-install.md`. Per-region VIP table, install steps, health-check scripts.

```bash
# On both VMs:
sudo apt-get install -y keepalived
sudo install -m 0644 ops/keepalived/keepalived-listener.conf /etc/keepalived/
sudo install -m 0644 ops/keepalived/keepalived-dj.conf /etc/keepalived/
sudo install -m 0755 ops/keepalived/check-edge.sh /etc/keepalived/
sudo install -m 0755 ops/keepalived/check-fanout.sh /etc/keepalived/
sudo install -m 0755 ops/keepalived/notify.sh /etc/keepalived/
# Edit the .conf files for VIP, priorities (node-a higher), and unicast_peer
sudo systemctl enable --now keepalived
```

**grimnir-fanout**: it's been running since Phase 3. Now enable the HA wiring. Add to `/srv/docker/grimnir_radio/.env` on **both** VMs:

```
# Mediaengine HA:
GRIMNIR_HA_PCM_RTP_ENABLED=true
GRIMNIR_HA_PCM_RTP_TARGETS=<node-a-ip>:5004,<node-b-ip>:5004
GRIMNIR_NETCLOCK_ENABLED=true
GRIMNIR_NETCLOCK_REGION=prod-us
GRIMNIR_NETCLOCK_MASTER_ADDR=<node-a-ip>:9094

# Fanout HA:
FANOUT_ENGINE_A_RTP=<node-a-ip>:5005
FANOUT_ENGINE_B_RTP=<node-b-ip>:5005
FANOUT_REDIS_ADDR=redis.example.internal:6379
FANOUT_NETCLOCK_ENABLED=true
FANOUT_NETCLOCK_MASTER_ADDR=<node-a-ip>:9094

# DJ auth: known caveat — see audit W-1
# FANOUT_CONTROL_PLANE_GRPC=<control-plane-host>:9090
```

**Caveat on `FANOUT_CONTROL_PLANE_GRPC`** (from pre-deploy audit W-1): the control plane's DJAuth gRPC server is still stubbed in rc.5. If you set this, fan-out's auth client will get `ErrDJAuthGRPCNotWired` from the server & reject every DJ token. For now, leave it **unset**; the fan-out boots in `AcceptAllAuthenticator` mode (logs a warning) until a follow-up release lands the server side. Gate DJ traffic on a private network ACL or stay on the v1 DJ ingress path until then.

Restart both VMs with the new compose stack:

```bash
./grimnir down
docker compose -f docker-compose.yml -f docker-compose.fanout.yml up -d
./grimnir ps             # expect grimnir-radio, grimnir-mediaengine, grimnir-edge-encoder, grimnir-fanout
```

Verify:

```bash
curl -I http://<listener-vip>:8001/stream         # 200 OK
curl http://<dj-vip>:8003/healthz                 # ok

# VRRP failover smoke test:
ssh <ssh-user>@<node-a-ip> sudo systemctl stop keepalived
# Within 3s: VIPs move to node-b. Listener stream stays connected.
ssh <ssh-user>@<node-a-ip> sudo systemctl start keepalived
# Priorities return; node-a takes VIPs back.
```

- [ ] Listener VIP serves audio
- [ ] DJ VIP `/healthz` returns ok
- [ ] VRRP failover smoke test completes; no listener gap audible
- [ ] Grafana `Audio Pipeline` dashboard shows both engines emitting PCM-RTP

**Rollback**: `sudo systemctl stop keepalived` on both nodes; flip `GRIMNIR_HA_PCM_RTP_ENABLED=false` & restart. You're back to Phase 3 topology.

---

## Phase 6 — Cutover dry-run on staging hostname

```bash
# On the edge VPS:
sudo cp /etc/nginx/sites-available/<public-hostname> \
        /etc/nginx/sites-available/staging.<public-hostname>
sudo vi /etc/nginx/sites-available/staging.<public-hostname>
#   change: server_name staging.<public-hostname>;
#   change: upstream grimnir { server <listener-vip>:8001; }
sudo ln -s /etc/nginx/sites-available/staging.<public-hostname> /etc/nginx/sites-enabled/
sudo nginx -t
sudo nginx -s reload

curl -I https://staging.<public-hostname>/stream     # 200 OK, X-Served-By v2 edge-encoder
```

Stream from staging for 24h. Watch Grafana.

- [ ] Staging hostname serves audio from the v2 listener VIP
- [ ] No 5xx in nginx access log for staging vhost over 24h
- [ ] Listener counts on v2 increase as listeners discover staging

**Rollback**: `sudo rm /etc/nginx/sites-enabled/staging.<public-hostname> && sudo nginx -s reload`. Production hostname untouched.

---

## Phase 7 — The actual cutover

One config change. One reload.

```bash
# On the edge VPS:
sudo vi /etc/nginx/sites-available/<public-hostname>
#   change upstream from:
#       upstream grimnir { server <v1-prod-host>:8081; }   # v1
#   to:
#       upstream grimnir { server <listener-vip>:8001; }   # v2
sudo nginx -t
sudo nginx -s reload
```

That's it. Verify within 60s:

```bash
curl -sI https://<public-hostname>/stream | head -5
#   expect: X-Served-By header indicating v2 edge-encoder
grimnir-deploy verify
#   expect: every cell OK
```

- [ ] Public hostname serves v2 audio
- [ ] `grimnir-deploy verify` green
- [ ] Grafana `Audio Pipeline` shows listener-count gauge climbing on the v2 side
- [ ] No spike in `nginx 5xx` log
- [ ] No spike in `grimnir_listener_disconnect_total{reason!="normal"}`

**Rollback (same reload)**:

```bash
sudo vi /etc/nginx/sites-available/<public-hostname>
#   restore upstream to: server <v1-prod-host>:8081;
sudo nginx -s reload
```

You're back on v1 within 10s. v2 keeps running. Triage the failure offline.

---

## Phase 8 — Post-cutover validation & v1 decommission

Let v2 run for 1 week. Acceptance gate (all five required):

- [ ] Zero unplanned VRRP transitions
- [ ] Zero rows in `deploy_history` with `phase = 'failed'`
- [ ] Listener reconnect rate under pre-cutover baseline
- [ ] Postgres replication lag < 5s sustained
- [ ] pgbackrest backup-drill passes: `grimnir-deploy backup-drill --region=prod-us --drill-host=<scratch-vm>`

If all five hold:

```bash
ssh <ssh-user>@<v1-prod-host>
cd /srv/docker/grimnir_radio
./grimnir down
# WAIT ANOTHER WEEK before destroying volumes. Then:
sudo rm -rf /srv/data/grimnir_radio/{media-data,postgres-data,media-data.pre-r2-backup}
```

- [ ] All five gates pass for 1 week
- [ ] v1 stack shut down on `<v1-prod-host>`
- [ ] Volumes preserved for 1 more week before deletion

**Rollback at Phase 8** = same as Phase 7 (nginx upstream back to v1). Until you `rm -rf` the volumes, v1 is recoverable.

---

## Per-phase rollback summary

| Phase | Rollback command |
|---|---|
| 0 | nothing to roll back |
| 1 | `rm /usr/local/bin/grimnir-deploy && rm -rf ~/.config/grimnir-deploy` |
| 2 | `sudo systemctl stop prometheus alertmanager grafana-server` |
| 3 | `./grimnir down` on both VMs |
| 4 | flip `.env` to `GRIMNIR_MEDIA_BACKEND=fs` & restart |
| 5 | `sudo systemctl stop keepalived` on both VMs; unset HA env; restart |
| 6 | remove staging vhost; `nginx -s reload` |
| 7 | restore nginx upstream to v1 IP; `nginx -s reload` |
| 8 | same as Phase 7; don't `rm -rf` v1 volumes for 1 week |

---

## When things go sideways

1. **Run `grimnir-deploy verify`** first. It's always safe & surfaces 90% of incidents in a per-component table.
2. **Read the audit ntfy topic**. Every mutating subcommand writes a Tier-1 audit row + ntfy post. If you can't remember what you ran 20 minutes ago, the audit log can.
3. **`--dry-run` everything** before re-running. Every subcommand supports it.
4. **`docs/runbooks/index.md`** is the symptom → subcommand → long-form runbook map. 3am-page friendly.
5. **Halted partway through Phase N is a stable state.** Hold position. Capture state. File an issue. Don't push forward on a deadline.

---

## Cross-references

- Architecture: `docs/v2/ARCHITECTURE.md`
- Release notes: `docs/v2/RELEASE_NOTES.md`
- Master runbook: `docs/v2/UPGRADE.md` (with B-1 patch applied)
- Per-subcommand: `docs/runbooks/index.md`
- Pre-deploy audit: `docs/v2/2026-06-08-pre-deploy-audit.md`
- v2 env template: `.env.example` (v1 reference preserved at `.env.v1.example`)
- Parent design (876 lines): `docs/superpowers/plans/2026-06-01-ha-zero-loss-failover-design.md`
