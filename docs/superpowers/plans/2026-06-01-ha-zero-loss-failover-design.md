# HA + Zero-Loss Failover + Rolling Updates — Architecture Design

> **Status:** Complete. All nine sections brainstormed and approved. Sections 1–7 from the 2026-06-01 session (recovered from transcript `c2d5a308-40b1-4d4a-8d04-337c98f23c16.jsonl`); Section 2.5 (fan-out internals), Section 8 (observability/runbooks/security/backup), Section 9 (build sequence/cutover/decommission), and the truncation patches in Sections 4/5/6/7 added on 2026-06-03.
>
> **For agentic workers:** This is a *design* document, not an implementation plan. Implementation plans for each subsystem are separate per Approach 4 (see Section 9). Do not implement against this doc directly — write per-subsystem plans first via `/superpowers:writing-plans`, in the order specified in Section 9.1.

---

## Goal

Bring Grimnir Radio to a state where:

1. Listeners experience **no silence and no forced reconnect** when an engine fails or when the system is updated.
2. **Rolling software updates** can ship without taking the site offline.
3. The architecture supports **multiple instances per region**, with a path to multiple regions later.
4. A **single-instance + local-disk** deployment remains a first-class supported mode.

Driver: 25+ daily users on RLMRadio (`.11` single-host docker-compose); reboots for upgrades visibly hurt the listener experience.

---

## Decisions log (Q1–Q12 + approach)

| Topic | Decision | Source |
|---|---|---|
| Q1: Failover experience for listeners | **A** — Seamless: no silence, no disconnect. | User msg #67, #69 |
| Q2: Region meaning | **A+D** — Geographic regions across mixed infra (own VPS + colo + cloud), **2 instances per region**. | User msg #77 |
| Q3: First-phase scope | **B-shaped** — Design for 3+ regions, ship **1 region, 2 instances** first. | User msg #82 |
| Q4: Active/Passive/Edge-relay | **C** — Active/Active with relay layer in front. | User msg #87, #89 |
| Q5: Database HA | **A+C** — Postgres primary+replica + pgbouncer; **distributed SQL (D) deferred as tech debt** for 3+ regions. | User msg #104 |
| Q6: Media storage | **Hybrid (D)** — S3-compatible source of truth + local read-through cache; **single-instance + local-disk must remain first-class**. | User msg #109 |
| Q7: Encoding / engine sync | **B** for live HTTP/ICY — engines emit PCM, edge encoder owns encode, sample-aligned switching. **D** for HLS — segment-based, atomic at boundary. | User msg #133 |
| Q8: Live DJ input under dual engines | **A** — Live input fans out to both engines through a regional fan-out node. | User msg #138 |
| Q9: Listener entry point HA | **D** — VIP + keepalived + 2 edge encoders (Approach A baseline) + **custom JS player on the web embed with tight reconnect-resume** for the bulk listener experience. External URL listeners get the baseline VIP/reconnect behavior. | User msg #167 |
| Q10: Per-region deployment platform | **C for phase 1** — docker-compose per node + keepalived + a real deploy script. K8s/Helm files stay in-tree for self-hosters; not the supported path. | User msg #175 |
| Q11: DB migration discipline | **A** — Expand/contract enforced as a hard rule, documented in `CLAUDE.md`, CI lint flags suspicious migrations. | User msg #180 |
| Q12: Deploy trigger | **B+** — Auto-deploy on tag push with per-environment policy (`auto` / `window` / `manual`) + emergency pause toggle + tag-suffix conventions (`-hold`, `-hotfix`). | User msg #185, #190 |
| Phasing | **Approach 4 — Parallel stack, one cutover.** Build complete new HA stack alongside current prod; integrate as built; one DNS/VIP cutover at end. Old single-instance stays warm 2–4 weeks as rollback target. | User msg #202, #207 |

**Cultural constraints** (load-bearing for the design):

- Liquidsoap is the anti-pattern, not the reference. Never propose it; never justify by "that's how broadcast shops have done it for 20 years." Design from first principles, optimize for listener UX. (User msg #114, #116)

---

## Section 1 of 9 — System topology

A **region** is a set of co-located nodes joined by low-latency LAN (same datacenter, same colo cage, same VPC, etc.). Each region operates independently and serves its own listeners.

A region has these elements:

- **Two HA nodes**, each running a complete `grimnir` stack: control plane, media engine, edge encoder, live-input fan-out, keepalived. Stateless or near-stateless per node; persistent state lives in the regional services below.
- **One regional Postgres cluster**: primary + streaming replica + pgbouncer (3 processes; pgbouncer co-located with each grimnir control plane is fine). For phase 1 these run on the two HA nodes (cross-host replication); for harder uptime guarantees, the DB pair can move to dedicated DB hosts later without app changes.
- **One regional object store**: MinIO cluster (2+ nodes) or rented S3-compatible (Cloudflare R2 recommended; zero egress fees matter for streaming media downloads). Source of truth for media files. Per-node `media-cache` volume holds hot reads.
- **One listener-facing VIP** held by keepalived, floats between the two HA nodes. This is what `<public-hostname>` DNS resolves to (or the regional CNAME does).
- **One DJ-facing endpoint** for live inputs (Harbor/WebRTC/RTP/SRT). Same VIP or a separate one; same keepalived setup.

A region is self-contained: kill the entire region's network link and the region keeps serving its local listeners until the database write availability matters. Inter-region traffic is for DB replication (phase 2) and operator access only.

**Single-instance deployment stays supported**: collapsed to one node, no replica, no VIP, no fan-out, local-disk storage. Same binaries, different config. The HA topology is the superset.

**Open question answered by default in the spec, push back if wrong**: keepalived runs on the HA nodes themselves rather than dedicated load-balancer boxes. For 2-node regions this is the simplest, fewest-machines arrangement. If you'd rather have dedicated LB hosts, say so.

> User approval: msg #220 "no looks good."

---

## Section 2 of 9 — Audio path

This is the technical heart. Conceptually: engines produce raw audio, the edge encoder owns the listener-facing encoded stream, the listener never sees an engine directly.

**Engine output: raw PCM over RTP (L16 stereo 44.1kHz)**

Each media engine's GStreamer pipeline terminates in an `rtpL16pay` + `udpsink` instead of an encoder. The output goes to the local edge encoder on the same host AND the peer host's edge encoder. Bandwidth: 1.41 Mbps stereo PCM per engine per destination, so 2 engines × 2 relays = 4 PCM streams totaling ~5.6 Mbps of intra-region traffic. Trivial on any LAN.

Compressed transport (Opus, FLAC) was considered and rejected: any decode at the relay adds latency and Opus is lossy, defeating the point of letting the relay own the encode. Raw PCM is right when both ends share a LAN.

**Clock sync: GStreamer NetClock**

One engine in the region is elected `time master` (uses the leader-election lock from `internal/leadership/`, separate key from the scheduler leader); the other slaves to it. Sub-millisecond sync on a LAN, GStreamer-native, no external dependency. Phase 2 cross-region: each region has its own time master, no inter-region clock sync attempted. Phase 3 upgrade path noted as PTP (IEEE 1588) for cases that need it.

**Edge encoder internals (new component, lives in mediaengine repo or new `internal/edgeencoder/`)**

GStreamer pipeline per output stream:

```
appsrc(engine-A-pcm) ─┐
                      ├─ input-selector ─ audioconvert ─ encoder ─ HTTP source
appsrc(engine-B-pcm) ─┘                  │
                                         └─ tee ─ HLS muxer ─ S3 segment writer
```

- Per-input jitter buffer of 80ms absorbs network jitter without adding audible latency.
- `input-selector` switches between inputs at a `running-time` boundary; because both inputs share a clock, the switch is sample-aligned (zero discontinuity in the PCM going into the encoder).
- Encoder runs once and never restarts on a switch. The encoded byte stream the listener sees is continuous regardless of which engine is feeding it.
- A `tee` branches the post-switch PCM to an HLS muxer that writes segments to S3 (or local disk for single-instance mode). This supports the Q9-D "premium listener" HLS path; basic phase 1 can skip HLS and add it later.

**Switch decision logic**

The edge encoder watches each input's RTP packet arrival and the engine's gRPC health channel. An input is healthy when: packets arrived in the last 100ms AND the engine reports `serving`. On the active input going unhealthy, switch immediately to the survivor. On both unhealthy: emit silence (or a configured fallback file), raise an alert.

**Listener-facing protocols**

- **HTTP/ICY** (MP3, AAC) — primary listener path, served by the edge encoder's HTTP source. Existing Go broadcast server code in `cmd/mediaengine` gets adapted to consume from the new encoder pipeline.
- **HLS** — segments live in shared object store; manifest served by either node's edge encoder, segments pulled by listener from S3/CDN.

> User approval: msg #220 "no looks good."

---

## Section 2.5 of 9 — Live input fan-out (inbound media gateway)

This is the inbound analog of the edge encoder: a single DJ connection terminates at a regional endpoint and the audio is replicated to both media engines so the lockstep-executor story (Section 3) continues to hold during live shows.

### Topology

A standalone `grimnir-fanout` binary runs on each HA node, alongside (but not in the same process as) the edge encoder. The two fan-out instances sit behind a DJ-facing VIP held by keepalived (the second VIP per region, separate from the listener-facing one — Section 1). At any moment one instance is "active" (holds the VIP), the other is "warm" (running, hot-mirroring session state from Redis, ready to take over).

Standalone process (not fused with the edge encoder) so the inbound and outbound paths cannot cross-contaminate. The two have very different code surfaces — outbound is real-time fan-out of finished encoded bytes to thousands of listener sockets; inbound is a small number of authenticated DJ sessions with protocol negotiation, codec decoding, and auth. Keeping them in separate processes means an encoder OOM doesn't kill DJ sessions and a codec bug in inbound doesn't drop listener streams.

### Protocols accepted from DJs

Same four as the existing live-input layer in `internal/live/`:

- **Harbor** (Icecast-protocol TCP push, MP3/AAC/Ogg encoded)
- **WebRTC** (browser-side WebDJ client primarily; SDP negotiation + DTLS-SRTP)
- **RTP** (raw RTP push from external broadcast tools)
- **SRT** (Secure Reliable Transport, for higher-quality remote DJ links)

Per-protocol decoders convert each into the same normalized internal representation: stereo PCM at 44.1kHz, timestamped against the regional NetClock.

### Pipeline shape per session

```
DJ client ──(Harbor/WebRTC/RTP/SRT)──> grimnir-fanout
                                            │
                                            ├─ protocol terminator + decoder
                                            ├─ resampler → 44.1kHz stereo PCM
                                            ├─ NetClock timestamp stamp
                                            └─ rtpL16pay + udpsink ─┬─> engine A
                                                                    └─> engine B
```

Both engines receive byte-identical RTP-L16 streams with NetClock-aligned timestamps. Each engine has an `rtpL16depay + audiomixer` element in its pipeline that mixes the live DJ input into its outgoing PCM at the appropriate priority tier (per `internal/priority/`, live override sits above scheduled). Because timestamps are NetClock-aligned, the mixed PCM coming out of engine A and engine B is sample-aligned and the edge encoder's `input-selector` continues working exactly as Section 2 describes.

### Clock binding

`grimnir-fanout` is a NetClock slave (same as the engines — Section 2). It binds to the regional NetClock master process for timestamp synthesis. If the NetClock master is down, the fan-out logs WARN and falls back to system monotonic clock — engines will detect the timestamp drift and switch to last-known-good per the Section 7 divergence-detection row. Operationally this should never happen; flagged here for completeness.

### Auth

DJ session tokens are issued by the control plane (existing `internal/auth/` flow). On connection, fan-out validates the token by calling the local control plane gRPC (`localhost:9090`), which checks against the DB. Token claims (mount permission, station permission, expiry) are cached in the fan-out for the token's lifetime to avoid per-frame validation. Token revocation: control plane publishes a revocation event on the existing event bus; fan-out subscribes and evicts cached tokens immediately.

### Session state replication (option C from Q-E2, mechanism A from Q-E3)

Per active DJ session, the active fan-out writes a Redis hash `dj:session:<session-id>` with these fields:

- `auth_token_claims_json` (cached claim set, so peer doesn't re-validate against DB on takeover)
- `mount_id`, `station_id`, `started_at`
- `codec_state_json` (sample rate negotiated, channel mapping, framing)
- `mix_bus_state_json` (send levels, routing, duck state, talkover gain)
- `last_active_at` (heartbeat, updated every 5s)
- `last_state_seq` (monotonic; peer uses to detect stale data)

Writes are triggered by DJ state-change actions, not by every audio frame. Typical write rate: < 1/second per session. Redis hash TTL: 60 seconds, refreshed on every state write by the active fan-out. If the active fan-out crashes or is partitioned, the peer takes the VIP, sees incoming DJ reconnect attempts, looks up the session by `session-id` (carried in the reconnect handshake), finds the state, resumes routing.

**What the DJ experiences on failover:** the TCP/WebRTC socket dies (TCP cannot migrate between hosts — this is physics). DJ client reconnects to the same VIP, which now resolves to the peer. Peer's fan-out finds the session state in Redis, accepts the reconnect without a second auth round-trip, restores the codec / mix bus state, resumes routing to both engines. Total visible gap: TCP/WebRTC reconnect time (typically 500ms–2s depending on client). No re-authentication, no re-config push, no lost routing state.

### Engine-side integration

Each engine's GStreamer pipeline gains:

- One `rtpL16depay` per fan-out source (just one, since both fan-outs feed identical RTP streams — see "active/warm" topology above)
- One `audiomixer` element with two inputs: the scheduled-content bus (existing) and the live-input bus
- Priority-aware ducking based on `internal/priority/` (live override pre-empts scheduled)

This is additive to the existing engine pipeline; no breaking change to the engine's existing API.

### Health check (byte-flow aware, per the Section 7 lesson)

`grimnir-fanout` exposes `/healthz` that returns 200 when:

- Process is up, NetClock binding is healthy, control plane gRPC is reachable, AND
- Either: there are zero active sessions (legitimately idle), OR at least one active session has produced an RTP packet to engines in the last 2 seconds

If a session is "active" per its socket state but produces no audio, the byte-flow check fails and `/healthz` returns 503 — which trips the keepalived VIP failover. This is the exact "process alive but not producing bytes" failure mode the Section 7 split-brain row exists to catch.

### Single-instance deployment

Collapsed to one `grimnir-fanout` process. No DJ-facing VIP (the single host's IP is the DJ endpoint). No Redis session replication (state lives only in-memory). On `grimnir-fanout` restart, in-flight DJ sessions drop and the DJ must reconnect — acceptable for single-instance mode since the whole point of that mode is "no HA."

### Failure modes specific to the fan-out

| Failure | Detection | Automatic response | Manual step |
|---|---|---|---|
| Fan-out process crash | Container exits non-zero; keepalived `vrrp_script` fails within 1s | VIP floats to peer; DJ clients reconnect; peer resumes session from Redis state | Investigate crash via core dump / logs; file issue if repeatable |
| NetClock binding lost | NetClock client error metric; timestamp-drift watchdog at engines | Fan-out logs WARN and uses system monotonic clock; engines detect drift and switch to last-known-good per Section 7 | Restart NetClock master; verify fan-out re-binds |
| Redis unreachable during steady state | Redis client error rate | Active fan-out continues serving DJ sessions in degraded mode (no state replication; failover would lose session state). Logged at WARN. | Restore Redis; warm-instance becomes useful again on next state write |
| Control plane gRPC down (auth validation impossible) | Auth call timeout | New DJ connections fail with 503; existing sessions continue using cached claims until token expiry | Restore control plane; DJs reconnect successfully once it's back |
| Both fan-out instances down | DJ VIP is unreachable | DJs cannot connect. Schedule continues playing per the engines. | Recover at least one fan-out node |

> User approval: 2026-06-02 conversation. Approved standalone process (Q-E1=2), session-state replication via Redis hashes (Q-E2=C, Q-E3=A); explicit acknowledgement that "C is a bitch" but the no-re-auth-on-reconnect property is worth the complexity.

---

## Section 3 of 9 — Control plane HA

Both nodes run a `grimnir` control plane. Both serve API requests. Leader election (Redis lock, already implemented in `internal/leadership/`) divides work between them.

**Per-request work (every instance):**

- HTTP API, JWT validation, RBAC checks
- WebSocket connections from admin UI clients (each instance subscribes to the Redis/NATS event bus and pushes events to its own connected clients)
- Static asset serving, metrics endpoint, health endpoints
- Read-only queries against the DB (via local pgbouncer)

JWT auth is already stateless, so a listener-facing LB can round-robin admin UI requests across both instances without sticky sessions. WebSocket connections are sticky for their lifetime (a single TCP connection), but if an instance dies the browser reconnects to the survivor and resubscribes.

**Leader-only work (one instance at a time):**

- Scheduler (`internal/scheduler/`, already gated by `leader_aware.go`)
- Orphan reaper, health-check loops, syndication delivery, webhook delivery
- Any cron-style background work

The leader writes its scheduled decisions into the DB (the timeline tables, as today). It does not drive engines directly across the network.

**How the two media engines stay coordinated**

Both control planes run the executor (the per-station state machine in `internal/executor/`) against the shared DB. Each control plane drives **only its co-located media engine** over local gRPC (`localhost:9091`). The two executors read the same scheduled timeline and produce the same `play` / `crossfade` / `stop` commands at the same wall-clock moments. Combined with the shared NetClock in Section 2, the two engines stay in lockstep without any cross-instance gRPC traffic on the audio control path.

This requires executor decisions to be deterministic given identical inputs: same timeline rows + same wall clock = same gRPC commands. The current executor design is broadly this shape; the spec calls out an audit task to verify and patch any non-deterministic spots (random tiebreakers, timestamp-of-decision recorded in memory not DB, etc.). If non-determinism is found and can't be fixed cheaply, the fallback is a leader-only executor that fans commands out to both engines, which adds cross-instance gRPC but is straightforwardly correct.

**Service discovery**

Each control plane targets `localhost:9091` for its media engine. There's no inter-node gRPC for the audio control path. Inter-node coordination is purely via Postgres and Redis.

**Instance identity**

`GRIMNIR_INSTANCE_ID` is the hostname or a stable per-host UUID. Used for leader-election ownership, telemetry labels, and the eventual deploy script's per-node drain commands.

**What does not change**

- `internal/executor/` API shape
- gRPC API to the media engine
- Auth flow
- Event bus contract

---

The interesting risk here is the deterministic-executor assumption. Push back if you remember non-determinism in the executor that'd break the lockstep approach.

> User approval: msg #226 "looks right, move to section 4"

---

## Section 4 of 9 — Database, cache, and object storage

### Postgres

Per-region cluster: one **primary**, one **streaming replica**, **pgbouncer** in front of each control plane (transaction-pooling mode).

- Async streaming replication (sync replication is configurable; default async because the latency cost on every write is not worth it for this workload).
- For phase 1 the DB pair runs on the two HA nodes (primary on node-1, replica on node-2). Cheap, saves hosts. Cross-host so a single host failure doesn't lose both. Tradeoff: a 2-host outage is total. Acceptable for 25-user phase 1; the spec calls out "move DB to dedicated hosts" as a phase 1.5 option without app changes.
- Failover is **manual for phase 1**: on primary failure, an operator runs a documented promote-replica runbook, repoints pgbouncer config, restarts pgbouncer, replica gets rebuilt against the new primary. Honest cost: if the primary dies at 03:00 and nobody's awake, write availability stops until promotion (reads keep working from the replica via a read-only pgbouncer pool). For ~25 daily users on a music-broadcast workload this is acceptable — the audio path keeps playing because it's already-scheduled content that the executor has cached.
- **Phase 1.5 upgrade** explicitly documented: drop in Patroni (or repmgr) for automated failover. The pgbouncer-in-front design means the app code never changes.
- pgbouncer's `server_reset_query` and short `server_lifetime` smooth over a primary swap so existing app connections survive.

### Schema migration discipline (expand/contract, codified)

Every schema change is split into three releases minimum:

1. **Expand**: only ADD columns/tables/indexes. Old code keeps working.
2. **Backfill + dual-write**: app writes to old + new shape, backfill job populates new shape from existing rows.
3. **Contract**: a later release (after every region is on the dual-write code) drops the old shape.

Enforced by:
- A new section in `CLAUDE.md` documenting the rule.
- A CI lint that scans `migrations/*.sql` for `DROP COLUMN`, `RENAME`, `ALTER TYPE`, etc. and fails the build unless the PR includes a `migration-contract: <reason>` annotation.
- Migration template files updated to nudge toward the pattern.

The lint can be Go (just regex over the new migration files in the PR diff) or a small shell script. Either way it lives in `make ci`.

### Object storage

S3-compatible source of truth + local read-through cache. Code path already exists in `internal/media/`; the new work is operational.

- **HA prod**: per-region MinIO cluster (2-4 nodes for erasure-coded durability) OR rented (Cloudflare R2 strongly recommended; egress-free pricing turns "every listener pull = bandwidth cost" into a non-problem). Both grimnir + media-engine pairs point at the same regional bucket.
- **Local cache** stays at `/var/lib/mediaengine/cache`. Populated on first read, LRU eviction at a configurable size cap (default 80% of partition). Cache survives container restarts (volume-mounted).

### Cache warming: bounded-eager prefetch (zero-downtime requirement)

Pure lazy caching breaks the zero-downtime promise: if a cold node declares `serving` and joins the edge encoder as a PCM source, then the peer dies before the cache warms, the cold node has to pull from S3 mid-track and listeners hear silence. That's exactly the failure mode the architecture exists to prevent.

The engine therefore uses **bounded-eager** warming at startup:

- At process start the engine reads the **next N minutes of the station timeline** (default `N = 10`, configurable via `GRIMNIR_CACHE_PREFETCH_HORIZON_SEC`) and pre-pulls all referenced media files from S3 into the local cache.
- The engine reports gRPC `health.Check = SERVING` **only after** the bounded prefetch completes. A file that is missing or fails to pull is logged at WARN with a structured event, but does not block `SERVING` — that's a data integrity problem the orphan reaper handles separately, not a startup-time problem.
- After `SERVING`, a background goroutine continues pulling beyond the prefetch horizon, walking the timeline forward as the scheduler materializes new entries. Rate-limited so it doesn't saturate the S3 link.
- Outside the prefetch horizon (e.g., a hard-item entry the operator inserts 30 minutes out), reads are lazy: first read pulls from S3, subsequent reads are local.

Sizing: a typical music station produces ~3 tracks per 10 minutes at ~5–8 MB each = ~25 MB to pre-pull. That's ~2 seconds on a LAN to a co-located MinIO and ~15–30 seconds over a slow WAN link to Cloudflare R2. Either way the engine is fully ready to feed listeners without stalling if it becomes the sole PCM source the moment after `SERVING`.

Single-instance + local-disk mode: the prefetch step is a no-op (the cache and the source of truth are the same directory). Engine reports `SERVING` after the usual GStreamer pipeline init.

> User approval (cache warming): 2026-06-02 conversation. Approved bounded-eager design, configurable horizon, 10-minute default.

---

## Section 5 of 9 — Listener entry point and DJ-facing endpoint

### The VIP + keepalived pattern

Per region: one virtual IP (VIP) for listener traffic, one VIP for DJ ingest (Harbor / WebRTC signaling / RTP / SRT). Both held by keepalived (VRRP) and float between the two HA nodes.

- **VRRP unicast peering** between the two hosts (not multicast — multicast is broken on most clouds and most VPS providers). Each host's keepalived config lists the peer's IP explicitly.
- **Process-aware health checks** on the holding host: keepalived runs a `vrrp_script` every second that checks the edge encoder's HTTP port (`curl localhost:8000/healthz`) AND the live-input fan-out's port. Process death → script fails → VRRP priority drops below peer → peer takes the VIP within ~3 seconds.
- **Host failure**: VRRP peer stops seeing advertisements, takes VIP within `advert_int × 3` (default 3s).
- **Split-brain protection**: a brief shared lease in Redis (separate key from leader election) gates which host "may hold the VIP." If a host can't write to Redis, it preemptively releases the VIP. Belt-and-suspenders against network partition double-holding.

### TLS and the existing edge VPS

The current `<edge-vps>` edge VPS reverse-proxies `<public-hostname>` to `.11:8081`. This pattern stays. For HA:

- Edge VPS keeps terminating TLS for `<public-hostname>`.
- Edge VPS reverse-proxies to the regional VIP (over WireGuard / ZeroTier / public IP, depending on the region).
- Certs stay on the edge VPS (one cert-bot, one place); regional VIPs only serve HTTP within the trust boundary.
- Phase 2 turns the edge VPS into a geo-router (or replaces it with GeoDNS / Anycast); the regional VIPs and TLS-termination split stays the same.

### Listener experience per failure mode (the honest table)

| Failure | Listener experience |
|---|---|
| Media engine process dies | Nothing. Edge encoder switches PCM sources at a sample boundary. |
| Media engine host dies (taking that engine + the relay on it) | Listeners on that host's relay get a TCP RST, reconnect to the surviving relay via the VIP (now floated to the survivor); 1-3s gap. |
| Edge encoder process dies, host alive | Keepalived health-check fails within 1s, VIP floats to peer; listeners reconnect to the new holder; 1-3s gap. |
| Both HA nodes die | Listeners get connection refused. Phase 2 cross-region failover would help; phase 1 = region-down. |
| Postgres primary dies | Listener audio unaffected (executor has the schedule cached and continues playing); write availability paused until manual promote. |
| Redis dies | Existing leader keeps holding (lease-based), no listener impact. New leader election waits until Redis returns. |

### Custom JS player with reconnect-resume (web embed)

A **best-effort tight-reconnect player** wraps the HTML5 `<audio>` element on every Grimnir-served listener page. Lives as a small JS bundle in `internal/web/static/js/player/` and is referenced from the existing listener templates. No framework dependencies; vanilla JS, ES modules.

**Reconnect mechanism (audio element recycling)**

The browser's `<audio>` element does not expose a clean "reconnect to the same stream URL" API: setting `src` to the same value is sometimes a no-op, and the element retains stalled-decoder state from before the disconnect. The reliable approach is to detach the existing element entirely and replace it.

- Listen for `error`, `stalled`, `waiting`, and `ended` events on the current `<audio>`.
- On any of those, detach the current `<audio>` element from the DOM, create a fresh one pointing at the current URL with a small starting buffer (`preload="auto"`), wire up the same event listeners, and attach it. The browser opens a new TCP connection to the stream URL; if the VIP has floated to the surviving node, this new connection lands on the new holder transparently.
- Track reconnect attempts in a sliding window. If 3 reconnects happen within 30 seconds, escalate to multi-URL fallback (below).

**Multi-URL fallback (auto-degrade)**

At page load the player fetches `GET /api/v1/stations/<station-id>/streams` and receives a list of stream URLs ordered from highest to lowest preference:

```json
{
  "streams": [
    {"url": "https://<public-hostname>/main/hq", "format": "mp3", "bitrate_kbps": 128, "label": "HQ"},
    {"url": "https://<public-hostname>/main/lq", "format": "mp3", "bitrate_kbps": 64,  "label": "LQ"}
  ]
}
```

The player starts on `streams[0]` (HQ). On the third reconnect failure within 30 seconds, it silently steps down to `streams[1]` (LQ) — replace `<audio>` element, point at new URL, audio resumes within ~500ms. Every 60 seconds while on a degraded stream, the player makes a background `HEAD` request against the higher-preference URL; on success (200), it steps back up to HQ.

The auto-degrade is silent in normal operation. If the player exhausts all stream URLs without success, it shows a "Stream temporarily unavailable" message with a manual retry button and stops auto-reconnecting until the listener clicks retry.

**UI state during reconnect**

The reconnect indicator is intentionally subtle so a one-second blip doesn't alarm listeners:

- Reconnect within 500ms of any event: no UI change, no indicator. Audio resumes inside the human attention threshold for "something happened."
- Reconnect taking 500ms–3s: thin progress bar at the top of the player; play/pause button shows a small spinner overlay; no modal, no error text.
- Reconnect failing past 3 attempts (about to step down to LQ): briefly flash a "Reconnecting..." text for ~1s, then resume audio on the LQ URL silently.
- All URLs exhausted: "Stream temporarily unavailable — Retry" with a button.

The active stream label (HQ / LQ) is shown in the player UI but visually de-emphasized; listeners can manually pick a quality from a dropdown but don't have to. Manual selection overrides auto-degrade for the rest of the session.

**Telemetry (anonymous reconnect events)**

The player POSTs a small anonymous event payload to `POST /api/v1/listener-events` on:

- Successful reconnect (with `attempt_count`, `time_to_resume_ms`)
- Auto-degrade to LQ (with `failed_attempts`, `previous_label`)
- Auto-upgrade back to HQ
- All-URLs-exhausted

Payload contains no listener identity, no IP (the server records it from the request socket, but the event itself is anonymous). Used by the control plane to surface reconnect-rate spikes in operator dashboards — a real failure that's masked by the player's reconnect logic will still show up as elevated reconnect rate, so operators know about it.

**Telemetry opt-out**: a `data-grimnir-no-telemetry="1"` attribute on the player embed disables event posting entirely. Default is opt-in (events sent).

**Service Worker, MediaSession, MSE**

Phase 1 does not use MSE (Media Source Extensions) or a Service Worker for stream caching — both add real complexity and the reconnect-via-fresh-element approach gets us most of the way to seamless. MediaSession API is wired up for browser-level media controls (lock-screen play/pause, OS notification) but doesn't affect reconnect logic.

> User approval: 2026-06-03 conversation. Approved multi-URL auto-degrade (Q-B1=B); subtle UI; anonymous reconnect telemetry to control plane.

---

## Section 6 of 9 — Rolling update flow

### The deploy script (per-region, lives on each node)

The deploy script is a versioned Go binary (lives in `cmd/grimnir-deploy/` or similar) — not a bash script — so it gets the same CI/test treatment as the rest of the codebase. It runs on either of the two HA nodes and orchestrates the peer.

**Inputs:** target tag (`vX.Y.Z`), region name, optional flags (`--force-policy`, `--dry-run`, `--rollback`).

**Pre-flight gates (any failure aborts before touching services):**

1. Read **emergency-pause** key from Redis. If set, abort with the pause message.
2. Read **deploy policy** for the region (`auto`, `window`, `manual`). If `window`, check current time falls inside the configured cron window. If `manual`, require an explicit `--go` flag.
3. Read **tag suffix conventions**: `-hold` skips auto entirely, `-hotfix` overrides `window`.
4. Verify the target image exists in the registry and pulls cleanly on both nodes.
5. Verify both nodes are currently healthy. Refuse to roll if one is already down (that's a recovery scenario, not a rolling update).
6. Verify leader election lease is held by one of the two nodes; note which one.

**Rolling sequence:**

1. **Pick first node**: the non-leader (so the leadership swap happens once, not twice).
2. **Drain first node**:
   - Set `vrrp_script` to fail on first node → keepalived drops VRRP priority → VIP floats to peer within 3s.
   - Send `SIGTERM` to grimnir control plane, edge encoder, fan-out, media engine on first node, in that order, with 30s grace for each (configurable). Active listener connections get a chance to drain on the relay; ones that don't drain within grace get cut and reconnect via the VIP to the surviving node.
   - Wait for the leader-election lease, if it was on this node, to expire and be claimed by peer.
3. **Run DB migrations** from the new image (each release ships its migrations; the deploy script runs `grimnir migrate` from the new image against the primary). Because of expand/contract discipline these are non-destructive — the old code on the peer keeps working against the migrated schema.
4. **Start new version on first node** (`docker compose up -d` with the new tag pinned).
5. **Wait for health**: `/healthz` returns 200, gRPC `health.Check` for media engine returns SERVING, edge encoder's PCM input from local engine is producing packets. 60s timeout (configurable).
6. **Restore VRRP priority on first node** → keepalived holds an active/active arrangement; the VIP stays where the peer has it, but the first node is back in the cluster as a hot peer.
7. **Verify**: edge encoder on peer is now receiving PCM from BOTH engines (the upgraded local one AND the peer's not-yet-upgraded one). Sample-aligned switching keeps working across versions because the PCM-over-RTP protocol is version-stable.
8. **Repeat steps 2-7 on the second node.**
9. **Post-flight**: both nodes healthy on new version, leader on one of them, listener-facing VIP held by either node. The script runs a fixed verification list before declaring success:
   - Both control planes report `/healthz` 200 with the new version in the response.
   - Both media engines respond gRPC `health.Check = SERVING`.
   - Both edge encoders report bytes-flowing on their listener output in the last 5 seconds.
   - Both fan-out instances pass their byte-flow-aware `/healthz` (or return 200 with "no active sessions" if idle).
   - DJ-facing VIP and listener-facing VIP are each held by exactly one node (no split-hold, no unheld).
   - Postgres replication lag is < 5 seconds.
   - Leader-election lease is held by exactly one of the two control planes.
   - A canary "play" request through the API on each control plane returns the expected response.
10. **Soak period**: the script then waits a configurable **deploy soak window** (default 5 minutes) before exiting non-zero on any new alert. Soak failures during this window trigger automatic rollback (see below). After the soak passes, the script exits 0 and writes the deploy record to Postgres (`deploy_history` table: tag, started_at, completed_at, operator, soak_outcome).

### Mid-roll failure (automatic rollback during the deploy itself)

If step 5 (wait-for-health) on the first node fails, the script automatically reverts that node to its previous image tag, waits for it to return to health, restores its VRRP priority, and exits non-zero with the captured logs. The peer node is untouched and still serving on the old version, so listener experience is unaffected. The cluster ends in its starting state (both nodes on the previous version) with a recorded failed-deploy entry in `deploy_history`.

If step 5 fails on the **second** node, the cluster is now split-version (first node on new, second still on old) — which is a deliberately supported steady state because of expand/contract migration discipline. The script attempts to revert the second node to the previous tag first; if that also fails, it reverts the **first** node back to the previous tag (which it knows still works because that's where the cluster started), restoring full-cluster-old-version state. An alert pages the operator either way.

### Post-deploy rollback (issues noticed after the deploy completed)

When issues surface minutes or hours after a successful deploy, the rollback path is **`grimnir-deploy --rollback`** rather than re-running the deploy script with the previous tag.

The `--rollback` flag:

- Auto-detects the previous successful tag from `deploy_history` (no need for the operator to remember it).
- Requires an explicit `--reason="..."` annotation; the reason is recorded in `deploy_history` and posted to the alerting channel (Slack/PagerDuty/whatever the region's alerting target is).
- Uses shorter grace periods (15 s vs 30 s drain) because rollbacks are usually urgent.
- Runs the same drain/restart/verify sequence in reverse-tag direction.
- **Checks the rollback eligibility window** (default 4 hours; configurable per region):
  - If less than 4 hours since the deploy: proceed normally.
  - If more than 4 hours: refuse to roll back unless `--force-aged-rollback` is also passed. Aged rollbacks are dangerous because contract migrations may have shipped between then and now, and the previous version's code may be incompatible with the contracted schema. The script names the suspect contract migrations from `migrations/*.sql` history in its refusal message so the operator can assess.
- Refuses outright if the rollback would cross any `migration-contract` annotated migration, regardless of age, unless `--force-through-contract-migration` is passed AND the operator has documented in `--reason` why this is safe.

### Migration handling during rollback

Migrations are not "rolled back." Expand-only migrations remain in place (no data loss; old code keeps working against the wider schema). Contract migrations that have already run cannot be cleanly reversed in a streaming-listener-serving context — those data columns are already gone. The contract-boundary refusal above exists specifically to prevent the deploy script from confidently rolling backward through a contract.

If rolling back past a contract is genuinely required (e.g., a contract migration is itself the cause of the incident), the operational path is: stop the cluster, restore the database from the most recent pre-contract backup, redeploy the previous tag. This is a documented runbook procedure, not an automated path — and it is the only path in this design that involves listener-visible downtime. Phase 1 accepts this as the explicit edge case.

### `deploy_history` table

A new tracking table in the regional Postgres:

```
deploy_history (
  id            uuid primary key,
  region        text,
  tag           text,         -- e.g., "v1.42.0"
  previous_tag  text,         -- what was running before this deploy
  started_at    timestamptz,
  completed_at  timestamptz,
  operator      text,         -- who ran the script
  outcome       text,         -- "success", "rolled_back_mid_roll", "rollback", "soak_failed"
  reason        text,         -- required for rollbacks
  soak_outcome  text,         -- "passed", "failed", "skipped"
  failure_log   text          -- captured logs when outcome != success
)
```

This table is the single source of truth for "what version is on what region, and how did it get there." Used by the rollback flow, the alerting layer, and operator dashboards.

> User approval: 2026-06-03 conversation. Approved dedicated `--rollback` flag with rollback-eligibility window and contract-boundary refusal (Q-C1=C); 4-hour default window configurable per region.

---

## Section 7 of 9 — Failure modes, detection, recovery

Section 5 covered the listener-facing impact of common failures. Section 7 covers the operational reality: how each failure gets detected, what triggers recovery, what manual steps remain, and the less-obvious cases.

### Detection layer

- **keepalived `vrrp_script`** runs every 1s on each node; failure drops VRRP priority. Drives VIP placement.
- **Health checks** on every component: control plane `/healthz`, media engine gRPC `health.Check`, edge encoder `/healthz`, fan-out `/healthz`, pgbouncer `SHOW STATS` query, MinIO `/minio/health/ready`. Probed by Prometheus on a 10s scrape and by keepalived on a 1s loop where it matters for VIP.
- **PCM input liveness** at the edge encoder: each `appsrc` for an engine input runs a 100ms watchdog. No packets → mark input unhealthy → switch to the surviving input within one buffer period.
- **Postgres replication lag** monitored via the standard `pg_stat_replication.replay_lsn` vs. `pg_current_wal_lsn` query, alerted at >5s lag, paged at >30s.
- **Leader-election lease** publishes its TTL to Redis; a separate watcher alerts if the lease churns more than once per minute (indicates an unhealthy leader or network flapping).

### Non-obvious failure modes

| Failure | Detection | Automatic response | Manual step |
|---|---|---|---|
| Postgres replica falls behind > 30s | Prometheus alert | None | Investigate WAL throughput, replica I/O; rebuild replica if catastrophic |
| Postgres primary slow (high commit latency) but alive | App slowness; commit-time metric | None automatic | Check `pg_stat_activity`, kill long-running queries, consider promote |
| Network partition between HA nodes | Both lose VRRP advertisements from peer; both lose pgbouncer connection to peer | Both nodes try to take VIP → Redis-lease split-brain gate (Section 5) allows only one. Leader election lease expires → new leader elected when partition heals. | Resolve network partition; verify VIP holder; verify replication caught up |
| NetClock master engine drifts or loses sync | RTP timestamp delta between engines exceeds threshold at the edge encoder | Encoder logs warning; falls back to last-known-good input until both look sane again | Investigate master engine; restart NetClock service if needed |
| MinIO / R2 outage | Media reads start failing; cache absorbs hot path | Engines play from cache; uploads queue locally with retry | Wait for storage recovery; replay queued uploads |
| Cache corruption on one node | gRPC errors when engine reads a cached file | Engine deletes corrupt cache entry, re-pulls from S3 | None usually; investigate underlying disk |
| Disk full on one node | All writes fail; container restart loop | keepalived health-check fails; VIP floats; node drains itself | Free disk; restart stack |
| DJ socket dies during a live show | Fan-out sees TCP close; signals both engines | Engines fall back to scheduled content via existing executor path | DJ reconnects via the surviving fan-out endpoint; live show resumes with a brief gap (TCP reconnect time, typically 1–3s) |
| Both engines healthy but producing diverging audio (silent NetClock corruption, encoder bug, race condition) | Edge encoder compares RTP timestamps + audio fingerprint between inputs every 1s; divergence beyond a threshold raises an alert and pins to last-known-good input | Edge encoder logs divergence; pins to one input; raises pager-grade alert | **Treat as a code bug, not an ops scenario.** Root-cause with both engines' pipeline graph + NetClock logs; fix in code; add a regression test that exercises the specific divergence shape |
| Edge encoder process OOM / memory leak (long-running encoder slowly grows; container restarts) | Container memory usage metric (Prometheus); restart counter | systemd / docker restart-policy brings encoder back; keepalived health-check fails briefly during restart so VIP floats to peer; listeners on that node reconnect | **Treat as a code bug requiring fix, not just monitoring.** Investigate the leak source (often a GStreamer ref-count problem); add a memory-growth integration test in CI; consider periodic graceful recycle as a stopgap only while a real fix is being written |
| Deploy script halts mid-roll (first-node health-check fails after new-version start, deploy aborts) | Deploy script exits non-zero; cluster is now split-version (first node on new, second node on old) | Deploy script automatically reverts first node to previous image tag, waits for health, restores VIP weight; cluster returns to homogeneous old version | Operator investigates the health-check failure from logs the script wrote; decides whether to retry with `--force-policy=manual`, file an issue, or pin the old version. Rollback path is documented in `docs/RUNBOOK_DEPLOY.md` (created in Section 8) |
| HLS segment write fails (S3 timeout / quota exhausted / bucket misconfig) | HLS muxer error metric; S3 client error rate; segment-age check (newest segment >2× target duration) | Edge encoder logs error; HLS listeners see manifest stall and either rebuffer or reconnect; ICY listeners unaffected | Investigate S3 health; check quota; verify bucket policy; HLS path may need a degraded fallback (serve last good manifest until storage recovers) |
| Listener VIP "stuck" on a node whose edge encoder is internally broken (split-brain: keepalived thinks it's up because the process is alive, but the encoder isn't producing bytes) | Process liveness ≠ functional health. **Detection requires a deeper probe than `pidof`.** Keepalived `vrrp_script` must check the encoder's `/healthz` endpoint, AND that endpoint must verify recent byte-flow to broadcast clients (not just "I'm running") | Encoder `/healthz` returns 503 when byte-flow stalls > 5s; keepalived priority drops; VIP floats to peer | Confirm health-endpoint logic is in fact byte-flow-aware (not just `return 200`); this is exactly the kind of detection bug that causes listener silence under "monitoring is green" conditions |
| Engine OOM during a live show (engine on the holding-DJ-route node dies) | Container memory metric; engine restart counter | Per Q8/A the DJ feed fans out to both engines, so the surviving engine keeps producing PCM with the live audio mixed in; edge encoder switches to survivor at the sample boundary; listeners unaffected | **Treat as a code bug.** Engine OOM during live shows means we have a leak in the live-input mixing path; add a long-running soak test to CI that streams hours of live input and asserts bounded memory growth |

**Note on rows marked "code bug":** Three of the rows above (engine divergence, encoder OOM, engine OOM during live show) describe failures that should not occur in correctly-written code. They're listed for completeness — if they happen, we want detection — but the primary response is to **fix the underlying bug and add a regression test**, not to accept these as recurring operational events. Operationally tolerating these failures normalizes silent corruption, which is the opposite of zero-downtime.

> User approval: 2026-06-02 conversation. Approved all six additional rows; emphasized that OOM/divergence categories are software-quality work in addition to ops detection.

---

## Section 8 of 9 — Observability, runbooks, security, backup

### 8.1 Observability & alerting

**Stack**: Prometheus (metrics) + OpenTelemetry (tracing) per CLAUDE.md, with self-hosted **ntfy.sh** as the paging target.

**Three-tier alerting**:

| Tier | Destination | Examples | Response |
|---|---|---|---|
| **Notify** | Chat channel (Slack or equivalent) | Replication lag > 5s, cache hit rate < 80%, soft warnings | Operator reviews during daytime |
| **Page** | ntfy.sh → phone push notification | VIP partition, primary DB down, both engines unhealthy, soak-window alert | Operator wakes up, runs the relevant runbook subcommand |
| **Page + auto-rollback** | ntfy.sh + `grimnir-deploy` triggers automatic rollback | Listener-reconnect-rate spike during deploy soak window (5 min after a deploy completes per Section 6), edge encoder byte-flow drops to zero on both nodes within soak window | Auto-rollback fires before the operator is fully awake; the page exists to inform what happened |

**On-call**: solo for phase 1 (just you). Section 8.3 secret management is designed so a second operator can be added cleanly by appending an age key — no architectural change needed.

**Page-grade target details**: self-hosted ntfy.sh on a small VPS (does not need to be in any Grimnir region; the alerting target failing should not correlate with Grimnir incidents). Topic per region (`grimnir-region-<name>`). Phone subscribes to all topics. Tokens scoped per-region so a leak doesn't expose other regions.

**Metrics that drive alerting** (concrete list for the implementation plan to wire up):

- `grimnir_listener_reconnect_rate_per_5min` — spike during soak window triggers tier-3 auto-rollback.
- `grimnir_edge_encoder_bytes_per_second{node}` — both nodes hitting zero in soak window triggers tier-3.
- `grimnir_postgres_replication_lag_seconds` — > 5s tier-1, > 30s tier-2.
- `grimnir_vrrp_holder_count{vip}` — should always equal 1; 0 or 2 is tier-2.
- `grimnir_engine_health{node}` — both unhealthy in same region is tier-2.
- `grimnir_pcm_input_packets_per_second{engine,source}` — drop below threshold tier-1 (informational; engine internally switches).
- `grimnir_deploy_history_failed_count` — increment triggers tier-2.
- `grimnir_redis_unreachable_seconds` — > 60s tier-2.
- `grimnir_cache_hit_rate_per_hour` — < 80% tier-1 (informational).

**Dashboards**: one per region, one cross-region. Stored as code in `dashboards/` (Grafana JSON or whatever Prometheus stack uses). Versioned alongside the alerting rules.

### 8.2 Operational runbooks

Runbooks are **first-class subcommands of the `grimnir-deploy` Go binary** with thin markdown index in `docs/runbooks/` for 3am "what subcommand do I even run?" lookup.

**Required subcommands** (each gets its own implementation plan):

| Subcommand | Purpose |
|---|---|
| `grimnir-deploy promote-replica` | Promote Postgres replica to primary; repoint pgbouncer; rebuild new replica from old primary |
| `grimnir-deploy drain --node=N` | Drain a node: VRRP priority drop, graceful service stop, leader hand-off, verify peer healthy |
| `grimnir-deploy emergency-pause` | Set Redis emergency-pause key; subsequent deploys abort with the pause message until cleared |
| `grimnir-deploy emergency-resume` | Clear emergency-pause; auto-deploys resume per region policy |
| `grimnir-deploy cold-start-region --region=R` | Bring up a freshly-built region from scratch; verifies all components come up healthy in dependency order |
| `grimnir-deploy restore --from=BACKUP_ID [--target-time=TS]` | Restore Postgres from pgbackrest (Section 8.4); handles base + WAL replay with progress output |
| `grimnir-deploy recover-partition` | Recover from a network partition: verifies VIP holder, replication state, leader lease; surfaces conflicts for operator decision |
| `grimnir-deploy verify` | Read-only health probe across the entire cluster; no changes; intended for incident triage |
| `grimnir-deploy backup-drill --region=R` | Run a backup-restore drill against a staging copy (Section 8.4); reports actual measured RTO/RPO |

Every subcommand supports `--dry-run`, has a `--help` describing the procedure, writes an audit log entry (see 8.3), and posts an ntfy notification on completion.

The markdown index in `docs/runbooks/index.md` is a table: symptom → subcommand → short description. Operator opens it at 3am, finds the symptom, runs the named subcommand. The subcommand's `--help` and inline prompts carry the rest.

### 8.3 Security & access

**Who can deploy**: solo on-call today. Access is gated by SSH access to either HA node in the region (no separate deploy server in phase 1) plus possession of the deploy age key (Section 8.3 secrets, below) plus the operator's auth in Vault when Vault is the active backend.

**Secret management**: pluggable backend interface in `internal/secrets/`. Phase 1 ships two implementations: `.env` file (default, always-supported baseline matching the single-instance-+-local-disk philosophy from Q6) and **HashiCorp Vault**. Backend selected per-region via config (`GRIMNIR_SECRETS_BACKEND=env|vault`).

Secrets the system manages:

- MinIO/R2 credentials per region
- keepalived VRRP auth password per region (rotated per region; not shared cross-region)
- NetClock auth shared secret
- JWT signing key (existing; replicated to both nodes)
- pgbouncer / Postgres role credentials
- Redis password (session-state + leader election + emergency-pause; same Redis, one password)
- ntfy.sh per-topic tokens
- pgbackrest repository credentials (for the cross-region backup destination)
- Vault root token (where applicable; recovery use only)

Rotation is a documented procedure per secret in `docs/runbooks/rotate-<secret>.md`. The deploy script's `--rollback` flag refuses to roll back through a credential rotation by default (operator must `--force-through-rotation` after confirming the previous version can still authenticate).

**Audit log**: `audit_log` table in the regional Postgres; every `grimnir-deploy` subcommand writes a row on invocation AND on completion. Schema:

```
audit_log (
  id            uuid primary key,
  ts            timestamptz not null,
  operator      text not null,             -- from auth context
  source_ip     text not null,             -- from SSH session or local invocation
  subcommand    text not null,             -- e.g., "deploy", "rollback", "promote-replica"
  args_json     jsonb not null,            -- captured (secrets redacted)
  phase         text not null,             -- "started", "completed", "failed"
  outcome       text,                      -- populated on completion
  duration_ms   bigint,                    -- populated on completion
  notes         text                       -- free-form, used for --reason annotations
)
```

Every operator action **also** posts an ntfy notification to a dedicated `grimnir-audit-<region>` topic. The phone gets a notification on every cluster mutation. In solo-operator mode this is the simplest "I didn't do that, someone else is in the system" detector — any audit notification not corresponding to an action you took is a security event.

The ntfy audit topic is separate from the page topic so audit noise doesn't desensitize you to actual pages.

**Network segmentation**: WireGuard mesh for **in-region** traffic (Postgres replication, Redis, NetClock RTP, gRPC, internal HTTP between control planes and engines). Public exposure limited to:

- Listener VIP: TCP 80/443 (HTTP/ICY, served via the existing edge VPS TLS terminator)
- DJ VIP: TCP and UDP ports for Harbor/WebRTC/RTP/SRT, authenticated per-protocol
- SSH: 22 on each HA node, key-only, key-pinning, ZeroTier-only (see below)

ZeroTier remains for **cross-region operator access** (ssh-jump pattern in memory notes) and for whatever cross-region coordination phase 2 adds. ZeroTier is NOT a dependency for in-region cluster traffic — if the ZeroTier control plane is down, the cluster keeps operating; only operator SSH access is affected.

Firewall: deny-all-default on each HA node; explicit allow rules for the public listener and DJ ports, ZeroTier SSH, and the WireGuard interface from the peer node. Rules managed by the `grimnir-deploy cold-start-region` subcommand (idempotent).

### 8.4 Backup & restore

**Tool**: `pgbackrest` for Postgres backup/restore. Standard, mature, parallel restore, S3-compatible object store integration, retention policies, integrity verification on each backup.

**Strategy**: **continuous WAL archiving + weekly base backups**.

- WAL is shipped from the primary to the backup destination(s) every **30 seconds** (`archive_timeout = 30s`) — bounding worst-case data loss to under a minute.
- Full base backup runs weekly during the lowest-traffic window per region (configurable via region config, default Sunday 04:00 local).
- Differential base backups daily; full once weekly. pgbackrest handles the chain.

**Destination — hybrid (same-region fast restore + cross-region disaster resilience)**:

- **Same-region**: a dedicated `grimnir-backup-<region>` bucket in the same regional object store as `grimnir-media-<region>`. Restore from here is fast (no egress).
- **Cross-region**: a dedicated `grimnir-backup-<region>-dr` bucket in a different region's object store. pgbackrest writes to both targets in parallel. R2 egress is free, so the cost is storage in two places — accepted.

Restore picks the closest healthy backup automatically; cross-region is reserved for "the whole region is gone."

**Retention** (configurable per region; defaults follow):

- Same-region: 30 days of differentials + 4 weekly base backups (about a month of restore-ability).
- Cross-region: 90 days of differentials + 12 weekly base backups (longer, since the disaster case is the only reason to reach for these).

**RTO / RPO targets** (phase 1):

- **RPO < 5 minutes** — driven by `archive_timeout = 30s` plus restore replay slack. The honest expectation is the WAL push interval (~30s) with rare worst cases at the push retry interval.
- **RTO < 2 hours** for same-region restore, < 4 hours for cross-region disaster restore.

These are phase-1 targets; tighten after first real-incident data and the first drill.

**Drill cadence**: quarterly. The `grimnir-deploy backup-drill` subcommand stands up a staging Postgres on a non-production host, restores the latest backup, measures actual base-restore + WAL-replay time, reports measured RTO + RPO, posts results to the audit ntfy topic. A drill failure (RTO over target, integrity check fails, etc.) is a tier-2 page-grade event.

**Media (S3/R2) backup**: object versioning enabled on the media bucket (R2 supports this; MinIO supports this). Deleted or overwritten objects are recoverable for 30 days. Separate from the Postgres backup path; uses the object store's native versioning rather than an external tool.

**Vault backup (when used)**: Vault's own raft storage snapshots, written to the same cross-region backup bucket as pgbackrest, daily. Documented restore procedure in the rotate-vault runbook.

> User approval: 2026-06-03 conversation. Approved three-tier alerting + ntfy.sh (Q-F2=C); solo on-call (Q-F2a); `.env` + Vault pluggable backends (Q-F4); audit via DB + ntfy (Q-F5=C); WireGuard in-region + ZeroTier cross-region (Q-F6=C); pgbackrest WAL archiving (Q-F7=B); hybrid same-region + cross-region backup destination (Q-F8=C); moderate RPO/RTO with quarterly drill (Q-F9=B).

---

## Section 9 of 9 — Build sequence, cutover, decommission

Approach 4 = stand up the complete new HA stack in parallel with current single-host prod (`<v1-prod-host>` keeps serving listeners throughout). One cutover at the end via the edge VPS reverse-proxy upstream swap. Old stack stays warm for 2–4 weeks as rollback target.

### 9.1 Build sequence — two parallel tracks

**Track A (backend stack — sequential, each step depends on the prior):**

1. **Database HA**: Postgres primary + streaming replica + pgbouncer on the two new hosts. Fresh `grimnir` DB schema. WireGuard mesh between the two nodes for replication.
2. **Object storage**: regional bucket on R2 (recommended) or MinIO cluster. `internal/media/` configured for the S3 backend. Local `/var/lib/mediaengine/cache` on each node.
3. **Two control planes + leader election ON**: both `grimnir` control planes running, leader election validated, scheduler runs on the leader, both serve API and WebSocket. No engines yet — control plane runs against an empty schedule.
4. **Single media engine + edge encoder + PCM transport**: bring up ONE media engine on node-1, build the edge encoder (`internal/edgeencoder/`), wire engine PCM → encoder. Validate: scheduled content plays end-to-end through the new path with one engine.
5. **Second media engine + NetClock + sample-aligned switching**: bring up engine on node-2, NetClock master/slave, both feeding the edge encoder. **This is the seamless-failover moment of truth** — verify with engine-kill drills that the listener stream survives an engine death with no audible glitch.
6. **Live-input fan-out** (`grimnir-fanout` standalone process per Section 2.5): both fan-out instances, DJ-facing VIP via keepalived, Redis session-state replication, validate with WebDJ end-to-end through the new path.
7. **Listener-facing VIP via keepalived**: VRRP between the two nodes for the listener IP, byte-flow-aware health-check, validate with edge-encoder-kill drills.

**Track B (independent — runs in parallel with Track A, ships when needed):**

- **Track B-1 (day 1, before any Track A schema work)**: Expand/contract migration discipline documented in `CLAUDE.md`, CI lint added to `make ci`. Applies to every Track A migration from PR #1.
- **Track B-2 (early, before Track A step 8)**: `grimnir-deploy` Go binary scaffold + deploy script with deploy policy and emergency pause. Exercised against the new stack as Track A progresses (every Track A step that touches a service gets driven through `grimnir-deploy`, not by hand).
- **Track B-3 (before Track A step 7 ships)**: Custom JS player with reconnect-resume and multi-URL auto-degrade. Deployable to the new stack's listener UI ahead of cutover.
- **Track B-4 (continuous, throughout Track A)**: Observability stack — Prometheus targets, ntfy.sh on a separate VPS, dashboards, alerting rules, audit log table, `internal/secrets/` interface with `.env` + Vault backends.
- **Track B-5 (before Track A step 1)**: pgbackrest configured against the new DB and the cross-region backup destination. Each Track A milestone is preceded by a base backup so we always have a recovery point during the build.
- **Track B-6 (parallel with Track A step 6+)**: Runbook subcommands of `grimnir-deploy` (promote-replica, drain, cold-start-region, restore, recover-partition, verify, backup-drill). Each subcommand validates against the partial-stack state as it lands.

### 9.2 Acceptance gates per Track A step

Each Track A step is "done" when ALL of:

- The subsystem passes its own integration test suite (added in the implementation plan for that step).
- A `grimnir-deploy verify` against the partial-stack reports the subsystem healthy.
- An audit log entry exists in the new stack's audit_log table for the completion.
- A snapshot backup is taken (pgbackrest) before moving to the next step.

A step is **not** "done" until the prior step's failure-injection drill (e.g., kill the primary DB and verify pgbouncer + replica behavior at step 1) passes against the partial stack. Failure-injection drills are part of the implementation plan for each step, not a separate later activity.

### 9.3 Continuous data sync mechanism (running throughout Track A)

The new DB and storage need to be a current mirror of prod by cutover day. Two independent sync streams run continuously from the moment Track A step 1 (new DB) is up:

- **Postgres logical replication** from old prod → new DB. The new DB is a subscriber. All tables, including play history. Drift monitored via `pg_stat_subscription.lag_bytes`; alert if it grows beyond a configurable threshold (default 100MB lag = roughly minutes of drift). The new DB's primary is read-only-from-app-perspective for the entire build — only the logical-replication writes happen. Real writes from the new control planes go to a `grimnir_staging` schema; the prod-replica schema is left alone.
- **`rclone sync --checksum`** from old `media-data` dir → new S3 bucket. Runs on a 60-second loop. Inotify-based on the old host for faster delta detection where possible. The new media engine cache pre-warms naturally from the S3 bucket as schedule materializes.

Both sync streams instrumented for lag. Tier-1 alert if either lag exceeds threshold.

### 9.4 Cutover plan

T-7 days: feature freeze on prod. Only critical-fix releases ship to old prod from here forward.

T-1 day: announce maintenance window (30-second listener-write pause expected; no audio impact expected).

T-0 (cutover):

1. Verify `grimnir-deploy verify` is green on the new stack.
2. Verify Postgres replication lag and rclone lag are both at zero (or near it; bounded by sub-second).
3. **Stop writes on old prod**: take the old control plane to read-only mode for ~30 seconds. Scheduled audio continues playing; listeners unaffected.
4. **Final delta apply**: logical replication catches up to the new DB (verified by replication lag = 0). Final rclone pass to catch any in-flight uploads.
5. **Promote the new DB**: rename `grimnir_staging` → `grimnir`, drop the logical replication subscription. New DB is now the source of truth.
6. **Swap edge VPS reverse-proxy upstream** from `<v1-prod-host>:8081` to the new regional listener VIP. nginx config reload (atomic; existing TCP connections to the edge VPS keep their backend until they reconnect; new connections hit the new stack).
7. **Resume writes on the new stack**: new control planes go from read-only to read-write. Scheduler resumes against the new DB. Schedule materializer picks up from the materialized state preserved through logical replication.
8. **Watch the success metrics** (see 9.5).

Total listener-visible gap: bounded by the edge VPS's nginx reload (sub-second) plus the TCP-reconnect time for listeners whose connections happened to land on the old upstream and got drained (1–3s, per the existing failure-mode table). Most listeners experience no gap because their TCP connection survives the nginx reload.

DJ-visible gap: any active DJ session reconnects when their upstream gets reloaded. Per Section 2.5, the reconnect picks up the session state from Redis with no re-auth — about 500ms–2s of audio gap. DJs who are mid-broadcast at cutover time get a heads-up notification at T-5min via the WebDJ client (separate event channel).

### 9.5 Cutover success bar + auto-revert triggers

After cutover, the `grimnir-deploy` process stays attached to a soak monitor for **30 minutes**. The auto-revert triggers (any one fires → automatic revert):

- Listener-reconnect-rate stays above +50% of pre-cutover baseline for any 15-minute window.
- HQ stream byte-flow drops to zero on both nodes simultaneously for > 30 seconds at any point.
- Any tier-2 page fires in the first 30 minutes.

**Auto-revert action**: edge VPS nginx config reloaded back to `<v1-prod-host>:8081`. Old stack resumes serving (it's still running, untouched). Old stack's Postgres still has all the writes up to T-0 (the replication stream pause means writes between T-0 and revert happened on the new stack only — that data is lost on revert).

**The data-loss-on-revert window is exactly the auto-revert window (30 min worst case).** Writes that happened on the new stack after cutover do not propagate back to the old stack. This is the explicit cost of the cutover-and-revert design. Documented and accepted; revert is for actual breakage scenarios, not for "I don't love how it looks." Manual override available: `grimnir-deploy revert-cutover --reason="..."` for after-the-soak revert.

**Soak passes**: `grimnir-deploy` writes a `cutover_success` row to `deploy_history` and exits 0. Manual operator decision to declare cutover successful (we wait at least one more 30-min window of all-green before formally declaring done).

### 9.6 Decommission of old stack

Old stack stays running and serving (nothing) for **2–4 weeks** after a successful cutover:

- Week 1: old stack is the documented quick-revert target. nginx config snippet kept ready to swap upstream back. No writes; old DB is dormant but intact.
- Weeks 2–3: confidence period. Operator monitors new stack for any latent issues. Old stack DB is frozen at its T-0 state.
- Week 4 (or sooner with operator confidence): formal decommission. Old `grimnir` container stack stopped. Final pg_dump of old DB stored in cross-region backup bucket with explicit `pre-cutover-final-` prefix and 1-year retention. Old `media-data` directory rsynced one last time to the cross-region backup bucket. Old host kept powered-on but services off for one more week; then services-uninstalled.

If a regression surfaces during the 4-week window that requires going back to old stack: edge VPS upstream swapped back, new stack data exported, manually merged into old DB, swapped forward again with a documented data-merge SOP. This is the painful path; documented but not automated because it should be exceedingly rare given the auto-revert during the 30-minute soak.

### 9.7 Resource estimates

Phase 1 incremental hosts needed:

| Resource | Need | Estimated cost |
|---|---|---|
| Two HA nodes (new) | Hetzner CX32 / OVH equivalent / your VPS preference; 4 vCPU, 8GB, 80GB SSD each | ~$15/mo each = ~$30/mo |
| Backup destination (cross-region) | Cloudflare R2 bucket | ~$0.015/GB/mo storage, zero egress |
| ntfy.sh host | Tiny VPS (1 vCPU, 1GB) on a different provider than your grimnir hosts | ~$5/mo |
| Vault (if used in phase 1) | Co-located on one of the HA nodes initially; dedicated VPS later | $0 phase 1 |
| WireGuard | No infrastructure cost (per-node config) | $0 |
| Cloudflare R2 for media | Sized to your current `media-data` consumption | ~$0.015/GB/mo storage, free egress |

Total infrastructure adds: ~$35/mo + storage scaled to media library size. Old stack stays at its existing cost during the build, gets decommissioned after cutover.

**Timeline (order of magnitude, no firm calendar commitments)**:

- Track B-1 (CI lint + expand/contract discipline): 1–2 days.
- Track A step 1 (Postgres HA): 1 week including failure drills.
- Track A step 2 (object storage + cache strategy): 3–5 days.
- Track A step 3 (control planes + leader election): 1 week.
- Track A step 4 (single engine + edge encoder + PCM): 2–3 weeks (the novel piece; the highest spike risk).
- Track A step 5 (second engine + NetClock + switching drill): 1–2 weeks.
- Track A step 6 (fan-out): 1–2 weeks.
- Track A step 7 (listener VIP): 3–5 days.
- Track B work overlapping: each adds 1–3 days but most happens within Track A windows.

End-to-end rough estimate: 8–12 weeks of focused engineering. Single-operator caveat: real calendar time depends on availability. Plan for 4–6 months calendar.

### 9.8 Open questions deferred to implementation

- **Executor determinism audit**: Section 3 names this as required. Needs a separate issue (filed alongside this design).
- **Specific MinIO topology if not R2**: 2-node vs 4-node erasure-coded; punt to Track A step 2's implementation plan.
- **WebDJ T-5min cutover-warning UI**: Section 9.4 mentions the heads-up notification; spec for the UI is part of Track B-3.
- **Single-instance + local-disk config-knob mechanism**: Section 1 promises this is supported; the actual config flag and its docker-compose shape get specified in Track A step 2's plan.

> User approval: 2026-06-03 conversation. Approved edge VPS reverse-proxy upstream swap (Q-G1=C); logical replication + rclone continuous sync (Q-G2=B); moderate auto-revert thresholds with 30-minute soak (Q-G3=B).

---

## Verification — does the design cover every decision?

This table maps each user decision to where it's addressed in the design.

| Decision | Reflected in | Status |
|---|---|---|
| Q1: Seamless failover (no silence, no disconnect) | Section 2 (sample-aligned PCM switch) + Section 5 (listener experience table) | Covered |
| Q2: 2 instances per region, mixed-infra | Section 1 (region definition) | Covered |
| Q3: Design for 3+ regions, ship 1 first | Section 1 + Section 9 (build sequence, decommission, resource estimates) | Covered |
| Q4: Active/Active with relay | Section 2 (edge encoder ingests both, switches) | Covered |
| Q5: Postgres primary+replica + pgbouncer; D as tech debt | Section 4 (DB); tech-debt issue filed as #232 | Covered |
| Q6: Hybrid storage, single-instance + local-disk first class | Section 4 (object storage + bounded-eager cache warming) + Section 1 (single-instance) | Covered |
| Q7: PCM-in-relay (B) for live HTTP/ICY + D for HLS | Section 2 (pipeline + tee + HLS branch) | Covered |
| Q8: Live input fan-out (A) | Section 2.5 (full fan-out internals: standalone process, NetClock binding, Redis session-state replication) | Covered |
| Q9: Custom JS player + tight reconnect | Section 5 (custom player spec: reconnect mechanism, multi-URL auto-degrade, UI state, telemetry) | Covered |
| Q10: docker-compose per node + keepalived + deploy script | Section 5 (VIP/keepalived) + Section 6 (deploy script + post-flight + rollback) | Covered |
| Q11: Expand/contract + CI lint | Section 4 (schema migration discipline) + Section 9 Track B-1 (phase-1 day-1 work) | Covered |
| Q12: B+ auto-deploy with policy + emergency pause + tag suffixes | Section 6 (pre-flight gates: emergency pause, deploy policy, tag suffix conventions) | Covered |
| Approach 4: Parallel stack, one cutover | Section 9 (build sequence, data-sync mechanism, cutover plan, success bar + auto-revert, decommission) | Covered |
| Cultural rule: don't propose Liquidsoap | Encoded in `feedback_liquidsoap.md` memory file | Covered (memory) |
| Single-instance + local-disk first class | Section 1 + Section 2.5 (single-instance fan-out collapse) + Section 4 (single-instance cache no-op) | Covered at design level; **config-knob mechanism specified in Track A step 2 implementation plan** (Section 9.8) |
| Deterministic executor audit task | Section 3 names it as required; Section 9.8 lists it as deferred to issue | Issue #233 filed |
| CI lint implementation for migrations | Section 4 names it as "Go regex over PR diff"; Section 9 Track B-1 schedules it day 1 | Issue #234 filed |
| Three-tier alerting + auto-rollback during soak | Section 8.1 (alerting tiers) + Section 6 (soak window auto-rollback) | Covered |
| Runbook execution model | Section 8.2 (`grimnir-deploy` subcommands) | Covered |
| Secret management (`.env` + Vault pluggable) | Section 8.3 (`internal/secrets/` interface + both backends) | Covered |
| Audit log + ntfy per operator action | Section 8.3 (`audit_log` table + ntfy audit topic) | Covered |
| Network segmentation (WireGuard in-region + ZeroTier cross-region) | Section 8.3 | Covered |
| pgbackrest WAL archiving with hybrid backup destination | Section 8.4 | Covered |
| RPO/RTO targets + quarterly drill | Section 8.4 | Covered |

### Open follow-ups (all filed as issues; implementation lands in per-subsystem plans)

1. **Issue #233** — Executor determinism audit (prerequisite for HA lockstep-engine architecture).
2. **Issue #234** — Expand/contract migration discipline + CI lint (Track B-1 day-1 work).
3. **Issue #232** — Distributed SQL engine for 3+ region deployments (deferred tech debt).

### Closed (this session)

- Distributed-SQL tech debt: issue **#232** filed 2026-06-02.
- Cache warming strategy (Section 4): bounded-eager with configurable horizon, 10-min default.
- Custom JS player full spec (Section 5).
- Deploy post-flight + rollback flow (Section 6): includes `--rollback` flag, eligibility window, contract-boundary refusal.
- Six additional failure-mode rows (Section 7), with explicit "code bug" annotations on OOM/divergence categories.
- Fan-out node internals as new Section 2.5.
- Section 8 in full (observability, runbooks, security, backup).
- Section 9 in full (build sequence under Approach 4, data-sync, cutover, decommission).

### Things explicitly out of scope (per the user)

- Liquidsoap. Not a fallback, not a reference, not a backup plan.
- Multicast VRRP (cloud-broken).
- Sync replication for Postgres in phase 1 (latency cost not worth it).
- Encoded engine-to-relay transport (Opus/FLAC) — raw PCM only on LAN.
- Cross-region clock sync — each region has its own NetClock master.

### What this design does NOT promise

- "Zero disconnect ever, including relay-host death." Section 9-D in the original Q&A is honest: TCP can't migrate. Web-embed listeners get sub-second reconnect via the custom JS player; external URL listeners get the 1–3s reconnect via VIP swap.
- "Zero gap on every failure." A 1–3s reconnect gap on listener side is the floor for the relay-host-death case. Everything else (engine-only failure, encoder process crash with host alive) is sample-aligned.
- "Auto-failover for the database." Phase 1 is manual promote. Phase 1.5 adds Patroni without app changes.
- "Multi-region in phase 1." Phase 1 is one region. Multi-region is phase 2.

---

## Next steps

1. User reviews this completed design end-to-end.
2. File the remaining two open follow-up issues (executor determinism audit, expand/contract CI lint).
3. Per-subsystem implementation plans via `/superpowers:writing-plans`, in the order specified in Section 9.1 (Track B-1 first, then Track A step 1, then parallel Track A + Track B work per the dependency notes).
4. First implementation plan: Track B-1 (expand/contract discipline + CI lint), since it gates every subsequent Track A migration.
