# Grimnir Radio v2 architecture

Single-page reference for the v2 HA topology. Depth: `docs/superpowers/plans/2026-06-01-ha-zero-loss-failover-design.md` (876 lines).

## The picture

```
                                    Listeners (public internet)
                                              |
                                              v
                              +-----------------------------+
                              |   Edge VPS (TLS terminator) |
                              |   nginx reverse-proxy       |
                              |   server_name <public-hostname>  |
                              +--------------+--------------+
                                             | upstream -> listener VIP
                                             v
                                  +----------+----------+
                                  | listener VIP        |
                                  | (VRRP, port 8001)   |
                                  +----------+----------+
                                  floats between node A/B
                                             |
                +----------------------------+----------------------------+
                |                                                         |
                v                                                         v
   +----------------------------+                           +----------------------------+
   |  proxmox VM: node A         |                          |  proxmox VM: node B         |
   |  <node-a-ip>                |  <----- PCM RTP ----->   |  <node-b-ip>                |
   |                             |  (multiudpsink, both     |                             |
   |  +-----------------------+  |   engines emit to both   |  +-----------------------+  |
   |  | edge-encoder          |  |   edge encoders;         |  | edge-encoder          |  |
   |  | (go-gst, port 8001)   |<-+   NetClock keeps them    |  | (go-gst, port 8001)   |  |
   |  | HTTP/ICY + HLS        |  |   sample-aligned)        |  | HTTP/ICY + HLS        |  |
   |  +----------+------------+  |                          |  +----------+------------+  |
   |             | mixed PCM     |                          |             | mixed PCM     |
   |             v               |                          |             v               |
   |  +-----------------------+  |                          |  +-----------------------+  |
   |  | mediaengine           |  |                          |  | mediaengine           |  |
   |  | (go-gst, gRPC :9091)  |  |                          |  | (go-gst, gRPC :9091)  |  |
   |  | DSP graph + mixer     |  |                          |  | DSP graph + mixer     |  |
   |  | branch (DJ input)     |  |                          |  | branch (DJ input)     |  |
   |  +----^------------------+  |                          |  +----^------------------+  |
   |       | gRPC LoadGraph/Play |                          |       | gRPC LoadGraph/Play |
   |  +----+------------------+  |                          |  +----+------------------+  |
   |  | grimnirradio (control)|  |  <--- shared Postgres +  |  | grimnirradio (control)|  |
   |  | HTTP API :8081        |  |       Redis (leader      |  | HTTP API :8081        |  |
   |  | scheduler + executor  |  |       election +         |  | scheduler + executor  |  |
   |  +----^------------------+  |       event bus) ----->  |  +----^------------------+  |
   |       | gRPC DJAuth         |                          |       | gRPC DJAuth         |
   |  +----+------------------+  |                          |  +----+------------------+  |
   |  | grimnir-fanout        |  |                          |  | grimnir-fanout        |  |
   |  | Harbor :8000          |  |  <---- session repl ---> |  | Harbor :8000          |  |
   |  | RTP :5006             |  |        (Redis pub/sub)   |  | RTP :5006             |  |
   |  | SRT :1935             |  |                          |  | SRT :1935             |  |
   |  | WebRTC :8004          |  |                          |  | WebRTC :8004          |  |
   |  +----^------------------+  |                          |  +----^------------------+  |
   |       | PCM RTP -> A & B    |                          |       | PCM RTP -> A & B    |
   +-------+---------------------+                          +-------+---------------------+
           |                                                        |
           +--------------------- DJ VIP ---------------------------+
                              (VRRP, ports 8000/5006/1935/8004)
                                       ^
                                       |
                              DJ clients (Internet)

   Off-cluster substrate:
   +--------------------------+   +--------------------------+   +--------------------------+
   | Postgres 16+             |   | Redis                    |   | MinIO, own VM            |
   | physical replication +   |   | leader election +        |   | media bucket             |
   | pgbackrest WAL to MinIO  |   | event bus + VRRP state   |   | backups bucket (pgbackrest)|
   +--------------------------+   +--------------------------+   +--------------------------+

   Observability:
   +--------------------------+   +--------------------------+   +--------------------------+
   | Prometheus               |   | Alertmanager + ntfy      |   | Grafana                  |
   | scrapes all 4 binaries   |-> | bridge (3 severity tiers)|-> | grimnir-ha-overview      |
   | on both nodes (8 targets)|   | -> ntfy VPS              |   | dashboard                |
   +--------------------------+   +--------------------------+   +--------------------------+
```

## Two-page summary

### The four binaries (per node)

**`grimnirradio` (control plane)**. HTTP REST API on :8081. Owns the scheduler (30s tick, 48h rolling window), the executor (per-station state machine), JWT auth & RBAC, the media-storage service, the webstream relay, and the DJAuth gRPC server that the fan-out queries to authorize incoming DJ sessions. Both nodes run it; Redis-based leader election decides which one owns each station's executor via CRC32 consistent hashing (500 virtual nodes).

**`mediaengine`**. gRPC server on :9091. Holds the GStreamer pipeline per station (`go-gst` programmatic bindings). The pipeline holds an always-on `audiomixer` branch with a `udpsrc -> rtpL16depay` input on `GRIMNIR_LIVE_INPUT_PORT`; the fan-out flips the DJ branch's mixer pad volume via `LiveInputControl.SetLiveInput`. When `GRIMNIR_HA_PCM_RTP_ENABLED=true`, the mixed output is emitted as raw L16 PCM over RTP via `multiudpsink` to every edge encoder in `GRIMNIR_HA_PCM_RTP_TARGETS`. When `GRIMNIR_NETCLOCK_ENABLED=true`, the pipeline binds to a region-wide NetClock master so both nodes' PCM samples are wall-clock-aligned.

**`edge-encoder`**. HTTP listener-facing endpoint on :8001 (ICY + HLS). Receives PCM-over-RTP from N media engines, runs a sample-aligned switcher, encodes to the listener-facing codecs. Switches inputs on engine failure without a glitch because the inputs are sample-aligned via NetClock. Uses `go-gst` CGo bindings (the only binary that has to; `gst-launch` can't do runtime input switching).

**`grimnir-fanout`**. Live DJ ingress on per-protocol ports (Harbor 8000, RTP 5006, SRT 1935, WebRTC 8004). One DJ at a time per station; the fan-out duplicates the PCM stream toward every media engine via `multiudpsink`, so both engines' mixers see the same DJ audio. Authorizes incoming sessions by calling the control plane's DJAuth gRPC with an LRU+TTL cache + event-bus revocation. Replicates session state cross-node via Redis pub/sub so a DJ reconnecting after a VIP flip lands on a node that knows about them. Uses `go-gst` CGo bindings.

### The VIPs

Two VRRP VIPs per region, each backed by a single binary on the active node:

- **Listener VIP** -> `edge-encoder` port 8001. Health check: `curl /healthz` on the local edge-encoder. If it fails, keepalived drops priority & the VIP floats to the other node within 3s.
- **DJ VIP** -> `grimnir-fanout` ports 8000/5006/1935/8004. Health check: `curl /healthz` on the local fan-out. Same failover behavior.

The control plane's `internal/vrrphealth/` poller reads VRRP state from Redis & exports `grimnir_vrrp_master_count` (split-brain detector) + `grimnir_vrrp_state`. The keepalived `notify.sh` writes state transitions to Redis on every event. Keepalived install: `docs/runbooks/keepalived-install.md`.

### The substrate

Three things have to live outside the HA nodes & be reachable from both:

- **Postgres 16+**. Authoritative for every scheduling, media-metadata, audit, deploy-history, and live-DJ row. Physical replication + pgbackrest WAL archive to MinIO means cold-rebuild of a region in under an hour (`docs/runbooks/restore-from-backup.md`, `docs/runbooks/cold-start-region.md`).
- **Redis**. Three jobs: leader election (control-plane executors), event bus (cross-instance fan-out + revocation + VRRP state), and the emergency-pause / deploy-policy keys that `grimnir-deploy` respects.
- **MinIO**. Self-hosted S3-compatible object storage on a dedicated VM (`192.168.195.24:9000`). Two buckets per region; one for media objects (`GRIMNIR_MEDIA_BACKEND=s3`), one for pgbackrest backups. It runs on the LAN, so media & backup traffic stays internal with no per-GB egress bill.

### The edge VPS (external entry point, `192.168.195.1`)

One small VPS in front, running nginx as the TLS terminator. It reverse-proxies `<public-hostname>` to the listener VIP. This external entry point is where the HA cutover & failover are realized: the v1-to-v2 cutover is a single change to the `upstream` block & one `nginx -s reload`, and rollback is the same edit in reverse. All listener traffic enters through `192.168.195.1`, so it is the single switch that points the public at v2 or back at v1.

### Observability

Prometheus scrapes all four binaries on both nodes (8 scrape targets per region). Alertmanager routes alerts through `cmd/alertmanager-ntfy` to ntfy with three tiers: Tier-1 audit, Tier-2 page, Tier-3 page-and-rollback. Tier-3 also flips the most recent `deploy_history` row to `failed` & invokes the auto-rollback observer in `internal/grimnirdeploy/autorollback/`, which uses a soak-window Prometheus poller (listener reconnects / 5xx rate / page-and-rollback alerts) to decide whether to rollback automatically.

Provisioning lives in `ops/prometheus/`, `ops/alertmanager/`, `ops/grafana/`. `make prometheus-validate` runs promtool against the rules + tests. Topology details: `docs/observability/README.md`.

### Deploy & operator surface

Every mutating operation goes through `cmd/grimnir-deploy`. It runs from the operator workstation, talks to both nodes over SSH, writes `audit_log` rows on start & completion, and posts to the audit ntfy topic. Every subcommand carries `--dry-run` + `--help`. The 3am-page index is `docs/runbooks/index.md`; it maps symptom -> subcommand -> long-form runbook.

The expand-only migration discipline (`docs/MIGRATIONS.md`) is enforced by `cmd/migration-lint` in `make ci`. v(N) & v(N+1) must run side-by-side against the same database during a rolling deploy; any destructive schema operation needs the `-- migration-contract:` annotation & a stated reason.

### What's the same as v1

The control plane API surface is unchanged. The media engine's gRPC API is a superset (added `LiveInputControl`, nothing removed). The data model is a superset (added `deploy_history` + `audit_log`, nothing removed or renamed). A v1.31.1 binary will start cleanly against the v2 database & vice versa.

### What's different from v1

- Four binaries per node, not two
- Two VIPs in front, not direct DNS to a single VM
- Shared Postgres + Redis required, not embedded sqlite + local Redis
- Media in MinIO, not on local disk (local disk becomes a read-through cache)
- Every mutating op via `grimnir-deploy`, not direct `docker compose`
- ntfy alerting wired by default
- Sample-aligned cross-node PCM mixing via NetClock + multiudpsink
