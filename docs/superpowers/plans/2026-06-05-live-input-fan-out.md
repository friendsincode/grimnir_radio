# Live-Input Fan-Out Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

> **Status:** Complete. 12 chunks (Chunk 0 spike + Chunks 1-11). Written 2026-06-05 incrementally per `feedback_brainstorming_incremental_save.md`. **Not yet executed — that's a separate multi-session effort.**

**Goal:** Build a new `cmd/grimnir-fanout/` Go binary that terminates one of four live-input protocols (Harbor TCP, WebRTC, RTP, SRT) per DJ session, normalizes the audio to NetClock-aligned PCM-over-RTP, and fans it out to both media engines in the region — preserving the lockstep-executor story during live shows.

**Architecture:** Standalone process (not fused with the edge encoder; per Q-E1=2 from the 2026-06-01 brainstorm). Two fan-out instances per region behind a DJ-facing keepalived VIP; one active, one warm. Active instance writes session state (auth context, mix bus, codec) to Redis hash `dj:session:<session-id>` on every state change; peer reads on takeover so a DJ reconnect after fan-out failure picks up without re-auth (Q-E2=C + Q-E3=A). Per-session pipeline shape: `<protocol terminator> → decoder → resampler@44.1kHz/stereo → NetClock timestamp stamp → rtpL16pay → multiudpsink clients=engine1:port,engine2:port`. The clock binding uses `internal/gstnet/` (from Track A step 5); protocol termination uses gst-launch subprocess for Harbor/RTP/SRT and `pion/webrtc/v4` (already in `go.mod`) for WebRTC.

**Tech Stack:** Go 1.24, go-gst CGo (existing dependency from Track A step 4), `internal/gstnet/` (NetClock wrapper from Track A step 5), `github.com/pion/webrtc/v4` (existing dep used by `internal/webrtc/broadcaster.go`), `github.com/pion/rtp` (existing), `github.com/haivision/srtgo` (new dep for SRT; depends on `libsrt-dev`), Redis (existing dep for leadership election).

**Issue:** TBD — file when first chunk merges.

**Parent design:** `docs/superpowers/plans/2026-06-01-ha-zero-loss-failover-design.md` Section 2.5 (full fan-out internals locked); Section 9.1 Track A step 6.

**Builds on:**
- Track A step 4 (edge encoder + PCM transport, shipped v2.0.0-alpha.3) — established the go-gst CGo build path + multiudpsink RTP-L16 pattern.
- Track A step 5 (NetClock engine sync, shipped v2.0.0-alpha.4) — provides `internal/gstnet/` for clock binding.

**Decisions locked 2026-06-01 brainstorm (referenced as Q-E1..Q-E3):**

| Q | Decision | Rationale |
|---|---|---|
| Q-E1 | **2** — standalone process, NOT fused with edge encoder | Outbound (encoder) and inbound (fan-out) have completely different code surfaces and failure modes; an encoder OOM shouldn't kill DJ sessions |
| Q-E2 | **C** — Redis session-state replication so DJ reconnect after failover survives without re-auth | TCP socket can't migrate but everything else can; the no-re-auth-on-reconnect property is worth the complexity |
| Q-E3 | **A** — Redis hash per session, written on state change (NOT on every audio frame) | Small low-frequency data; Redis already a dep; trivial to debug |

**Honest scope:** 12 chunks. **Estimated 5-7 calendar weeks at solo pace.** Same order of magnitude as the edge encoder (Track A step 4) which took ~3 calendar weeks of focused engineering in a single push (2026-06-03 to 2026-06-05) with substantial subagent leverage. With four protocols to terminate (Harbor, WebRTC, RTP, SRT) instead of one (PCM-RTP), expect more chunks per protocol but each individually smaller.

**Out of scope for this plan** (deferred follow-ups):
- True session-bandwidth-aware codec negotiation (Opus VBR for WebRTC, etc.). Phase 1: codec is fixed per protocol.
- DJ-side mixing UI (talkover, send levels). Lives in WebDJ frontend; fan-out just receives the mixed audio.
- Multi-region DJ routing (e.g., DJ in US-East routes to US-East fan-out). Phase 2.
- Encrypted SRT (passphrase). Phase 1 assumes plaintext SRT or TLS at edge VPS.
- WebRTC ICE/STUN/TURN for restricted network DJ clients. Phase 1 assumes browser can directly reach the fan-out IP.

---

## File structure

| File | Status | Responsibility |
|---|---|---|
| `internal/grimnirfanout/spike/main.go` | Create then delete | Chunk 0 spike: verify a Harbor-style TCP receiver + decoder + multiudpsink pipeline can be driven from Go |
| `docs/superpowers/spikes/2026-06-05-fanout-spike.md` | Create | Spike report (proceed / redesign) |
| `cmd/grimnir-fanout/main.go` | Create | CLI entry point, signal handling, dependency wiring |
| `cmd/grimnir-fanout/main_test.go` | Create | Integration tests against the assembled binary |
| `internal/grimnirfanout/config.go` | Create | Env-var loading |
| `internal/grimnirfanout/config_test.go` | Create | Config + validation tests |
| `internal/grimnirfanout/session.go` | Create | DJ session state machine; per-session lifecycle |
| `internal/grimnirfanout/session_test.go` | Create | Session state machine tests |
| `internal/grimnirfanout/pipeline.go` | Create | Per-session GStreamer pipeline construction (go-gst, NetClock-bound) |
| `internal/grimnirfanout/pipeline_test.go` | Create | Pipeline tests |
| `internal/grimnirfanout/harbor.go` | Create | Harbor (Icecast-protocol TCP push) listener |
| `internal/grimnirfanout/harbor_test.go` | Create | |
| `internal/grimnirfanout/rtp.go` | Create | RTP UDP listener |
| `internal/grimnirfanout/rtp_test.go` | Create | |
| `internal/grimnirfanout/srt.go` | Create | SRT listener via srtgo |
| `internal/grimnirfanout/srt_test.go` | Create | |
| `internal/grimnirfanout/webrtc.go` | Create | WebRTC signaling + ingest via pion |
| `internal/grimnirfanout/webrtc_test.go` | Create | |
| `internal/grimnirfanout/auth.go` | Create | Token validation against control plane gRPC; local cache |
| `internal/grimnirfanout/auth_test.go` | Create | |
| `internal/grimnirfanout/replication.go` | Create | Redis session-state replication |
| `internal/grimnirfanout/replication_test.go` | Create | |
| `internal/grimnirfanout/grpc.go` | Create | gRPC service (GetStatus matching mediaengine convention) |
| `internal/grimnirfanout/grpc_test.go` | Create | |
| `internal/grimnirfanout/metrics.go` | Create | Prometheus metrics (active sessions, per-protocol counts, replication lag) |
| `proto/grimnirfanout/v1/grimnirfanout.proto` | Create | gRPC service definition |
| `internal/playout/director.go` | Modify | Add `rtpL16depay + audiomixer` to engine pipelines so they accept the live-input branch |
| `cmd/grimnirradio/main.go` | Possibly modify | Wire any new control-plane endpoints (session-creation API the WebDJ uses) |
| `Dockerfile.fanout` | Create | Build container (mirrors edge-encoder's pattern; needs `libsrt-dev`) |
| `deploy/grimnir-fanout.service` | Create | systemd unit |
| `.github/workflows/ci.yml` | Modify | Install `libsrt-dev` for SRT build |
| `Makefile` | Modify | Add `build-fanout` target |
| `CLAUDE.md` | Modify | Document the new binary + env vars |
| `internal/version/version.go` | Modify | Bump 2.0.0-alpha.4 → 2.0.0-alpha.5 |

**Decomposition principle:** one file per protocol (harbor.go, rtp.go, srt.go, webrtc.go) so each terminator can be developed and tested in isolation. Session + pipeline core stays small (< 300 lines each). Auth + replication get their own files because their failure modes are independent of the protocol path.

---

## Chunk 0: Spike — Harbor TCP termination → PCM → multiudpsink

Validate the GStreamer pipeline shape can drive a Harbor (Icecast-style) TCP receiver, decode whatever the DJ sends (MP3, AAC, Ogg), resample to 44.1kHz stereo PCM, and emit RTP-L16 to two destinations. Done correctly this is the canonical pattern for all four protocols (only the upstream element changes).

### Task 0.1: Set up spike

**Files:**
- Create then delete: `internal/grimnirfanout/spike/main.go`
- Create then delete: `internal/grimnirfanout/spike/run.sh`

**Context:**
The spike uses `gst-launch-1.0` only (no go-gst). Three processes:
1. **Fake DJ client**: `gst-launch-1.0 audiotestsrc freq=440 num-buffers=300 ! lamemp3enc ! tcpclientsink host=127.0.0.1 port=8000` — emits 6 seconds of MP3 over a TCP connection.
2. **Fan-out**: `gst-launch-1.0 tcpserversrc port=8000 ! decodebin ! audioconvert ! audioresample ! audio/x-raw,rate=44100,channels=2,format=S16BE ! rtpL16pay pt=10 mtu=1400 ! multiudpsink clients=127.0.0.1:15004,127.0.0.1:15005 sync=true`
3. **Two engine receivers**: each `gst-launch-1.0 udpsrc port=15004 ! application/x-rtp,... ! rtpL16depay ! audioconvert ! lamemp3enc ! filesink location=/tmp/spike-engine-N.mp3`

Run all three concurrently. After 6 seconds: assert both engine output files exist and contain non-trivial MP3 audio.

- [ ] **Step 1: Write the spike script**

`internal/grimnirfanout/spike/run.sh`:

```bash
#!/bin/bash
# Spike: validate fan-out pipeline shape — DJ MP3 over TCP → decode → PCM-RTP → 2 engines.
set -e
rm -f /tmp/spike-engine-A.mp3 /tmp/spike-engine-B.mp3

echo "=== Spawning engine receivers ==="
gst-launch-1.0 -q udpsrc port=15004 caps='application/x-rtp,media=audio,clock-rate=44100,encoding-name=L16,payload=10,channels=2' ! rtpjitterbuffer latency=80 ! rtpL16depay ! audioconvert ! lamemp3enc ! filesink location=/tmp/spike-engine-A.mp3 &
PID_A=$!
gst-launch-1.0 -q udpsrc port=15005 caps='application/x-rtp,media=audio,clock-rate=44100,encoding-name=L16,payload=10,channels=2' ! rtpjitterbuffer latency=80 ! rtpL16depay ! audioconvert ! lamemp3enc ! filesink location=/tmp/spike-engine-B.mp3 &
PID_B=$!
sleep 0.5

echo "=== Spawning fan-out (TCP server → PCM-RTP fan-out to 2 engines) ==="
gst-launch-1.0 -q tcpserversrc port=8000 ! decodebin ! audioconvert ! audioresample ! audio/x-raw,rate=44100,channels=2,format=S16BE ! rtpL16pay pt=10 mtu=1400 ! multiudpsink clients=127.0.0.1:15004,127.0.0.1:15005 sync=true &
PID_FO=$!
sleep 0.5

echo "=== Spawning fake DJ client (6s of MP3 over TCP) ==="
gst-launch-1.0 -q audiotestsrc freq=440 num-buffers=300 ! audioconvert ! lamemp3enc ! tcpclientsink host=127.0.0.1 port=8000

echo "=== DJ finished; waiting for engines to drain ==="
sleep 2
kill $PID_FO $PID_A $PID_B 2>/dev/null
wait 2>/dev/null

if [ ! -s /tmp/spike-engine-A.mp3 ] || [ ! -s /tmp/spike-engine-B.mp3 ]; then
    echo "FAIL: engine output empty"
    ls -l /tmp/spike-engine-*.mp3
    exit 1
fi

echo "=== SUCCESS ==="
ls -l /tmp/spike-engine-*.mp3
echo "Listen with: paplay /tmp/spike-engine-A.mp3"
```

- [ ] **Step 2: Run the spike**

```bash
chmod +x internal/grimnirfanout/spike/run.sh
./internal/grimnirfanout/spike/run.sh
```

- [ ] **Step 3: Verify both engine outputs contain valid audio**

```bash
ffprobe /tmp/spike-engine-A.mp3 2>&1 | grep -E "Duration|bitrate"
ffprobe /tmp/spike-engine-B.mp3 2>&1 | grep -E "Duration|bitrate"
paplay /tmp/spike-engine-A.mp3  # should hear 440 Hz sine for ~6s
```

Both files should have a non-trivial duration and the engines should produce IDENTICAL audio (within sample alignment).

- [ ] **Step 4: Document findings + delete spike**

Write `docs/superpowers/spikes/2026-06-05-fanout-spike.md` with PROCEED / REDESIGN decision based on observation.

```bash
rm -rf internal/grimnirfanout/spike/
git add docs/superpowers/spikes/2026-06-05-fanout-spike.md
git commit -m "spike: fan-out pipeline shape validated"
```

---

## Chunk 1: Fan-out binary scaffold (config + gRPC + healthz + signal handling)

Stand up `cmd/grimnir-fanout` with the same shape as `cmd/edge-encoder`: config from env, gRPC GetStatus, HTTP /healthz, clean shutdown on SIGINT/SIGTERM. No protocols yet (Chunks 3-6).

### Task 1.1: Proto + Status type + config loader

**Files:**
- Create: `proto/grimnirfanout/v1/grimnirfanout.proto`
- Create: `internal/grimnirfanout/config.go`
- Create: `internal/grimnirfanout/config_test.go`

**Env vars** (follow `EDGE_ENCODER_*` pattern; namespace `FANOUT_*` with `RLM_FANOUT_*` fallback):

| Variable | Default | Purpose |
|---|---|---|
| `FANOUT_BIND_ADDR` | `0.0.0.0` | gRPC + HTTP bind |
| `FANOUT_GRPC_PORT` | `9093` | gRPC (mediaengine=9091, edge=9092, fanout=9093) |
| `FANOUT_HTTP_PORT` | `8003` | /healthz |
| `FANOUT_METRICS_PORT` | `9193` | Prometheus |
| `FANOUT_HARBOR_PORT` | `8000` | TCP listen for Harbor protocol |
| `FANOUT_RTP_PORT` | `5006` | UDP listen for raw RTP ingest |
| `FANOUT_SRT_PORT` | `1935` | UDP listen for SRT |
| `FANOUT_WEBRTC_HTTP_PORT` | `8004` | HTTP signaling (POST /offer) |
| `FANOUT_ENGINE_A_RTP` | empty | `host:port` of engine A's RTP-L16 ingest (required) |
| `FANOUT_ENGINE_B_RTP` | empty | `host:port` of engine B's RTP-L16 ingest (optional; if empty, single-engine mode) |
| `FANOUT_NETCLOCK_ENABLED` | `false` | If true, bind pipelines to NetClock |
| `FANOUT_NETCLOCK_MASTER_ADDR` | empty | Where to dial for NetClock sync |
| `FANOUT_CONTROL_PLANE_GRPC` | empty | `host:port` of grimnirradio for auth token validation |
| `FANOUT_REDIS_ADDR` | empty | Redis for session-state replication; empty disables replication (single-instance mode) |
| `FANOUT_LOG_LEVEL` | `info` | |

Validation: `FANOUT_ENGINE_A_RTP=""` → error. Other fields optional.

- [ ] **Step 1-6**: failing test → impl → verify → make ci → commit

Reference: mirror `internal/edgeencoder/config.go` structure.

### Task 1.2: gRPC server (GetStatus)

**Files:**
- Create: `internal/grimnirfanout/grpc.go`
- Create: `internal/grimnirfanout/grpc_test.go`

`StatusResponse`: version, uptime_seconds, active_sessions, harbor_session_count, rtp_session_count, srt_session_count, webrtc_session_count, total_sessions_served, engine_a_reachable, engine_b_reachable.

Mirror `internal/edgeencoder/grpc.go` pattern: `StatusProvider` interface with a `Status() Status` method; gRPC server holds a reference, calls it per request.

- [ ] **Step 1-7**: proto → make proto → failing test → impl → make ci → commit

### Task 1.3: `cmd/grimnir-fanout/main.go` wiring

**Files:**
- Create: `cmd/grimnir-fanout/main.go`
- Create: `cmd/grimnir-fanout/main_test.go`

Bring up config, gRPC server, /healthz HTTP, signal-context-cancelling shutdown. Stub status provider until real session manager exists (Chunk 2).

- [ ] **Steps**: TDD; mirror `cmd/edge-encoder/main.go` structure

### Task 1.4: Makefile + CI workflow updates

**Files:**
- Modify: `Makefile` — add `build-fanout` target with `CGO_ENABLED=1`
- Modify: `.github/workflows/ci.yml` — install `libsrt-dev` (for Chunk 5; build it in advance to surface issues early)

- [ ] **Steps**: edit + manually verify YAML parses + `make build-fanout` produces binary

---

## Chunk 2: Session core + pipeline (no protocols yet)

The session is the load-bearing abstraction. Each DJ connection creates a `Session` with a unique ID; the session owns a GStreamer pipeline that:
1. Accepts decoded PCM from a protocol-specific source (Chunks 3-6 plug in)
2. NetClock-stamps it
3. RTP-L16 + multiudpsink to both engines

### Task 2.1: Session type + state machine

**Files:**
- Create: `internal/grimnirfanout/session.go`
- Create: `internal/grimnirfanout/session_test.go`

States: `Idle` → `Authenticating` → `Active` → `Ended`. Per-session: `ID`, `StartedAt`, `Protocol` (enum), `Pipeline` (Chunk 2.2 holds), `AuthClaims` (cached from Chunk 7).

Session manager (`SessionMgr`): map[sessionID]*Session, thread-safe add/remove/lookup, `Count()` accessor.

- [ ] **Steps**: TDD with synthetic sessions

### Task 2.2: GStreamer pipeline construction (go-gst, NetClock-bound)

**Files:**
- Create: `internal/grimnirfanout/pipeline.go`
- Create: `internal/grimnirfanout/pipeline_test.go`

Per-session pipeline shape: `appsrc name=audio_in caps=audio/x-raw,format=S16LE,rate=44100,channels=2 → audioconvert → audioresample → audio/x-raw,format=S16BE,rate=44100,channels=2 → rtpL16pay pt=10 mtu=1400 → multiudpsink clients=<engine list>`. NOTE: `appsrc` so the protocol terminators (Chunks 3-6) can push PCM in via Go.

When `FANOUT_NETCLOCK_ENABLED=true`: bind pipeline to `gstnet.NewNetClientClock` before SetState(PLAYING). Reuse `internal/gstnet` from Track A step 5.

- [ ] **Steps**: TDD — start pipeline with `multiudpsink` pointed at `127.0.0.1:65000` (which nothing listens on; multiudpsink will silently discard); verify it reaches PLAYING and accepts PCM via appsrc PushBuffer.

### Task 2.3: Wire SessionMgr into status provider

**Files:**
- Modify: `cmd/grimnir-fanout/main.go`
- Modify: `internal/grimnirfanout/grpc.go` (Status accessor reads SessionMgr.Count())

- [ ] **Steps**: TDD via gRPC client → assert ActiveSessions reflects manually-added sessions

---

## Chunk 3: Harbor (Icecast TCP) ingress

The most common DJ protocol; Mixxx/Butt/SAM Broadcaster all use Icecast/Shoutcast push.

### Task 3.1: Harbor listener

**Files:**
- Create: `internal/grimnirfanout/harbor.go`
- Create: `internal/grimnirfanout/harbor_test.go`

TCP listen on `FANOUT_HARBOR_PORT`. Per accepted connection:
1. Read HTTP-style request (Icecast uses `SOURCE /mount HTTP/1.0\r\nAuthorization: Basic ...\r\nContent-Type: audio/mpeg\r\n\r\n`)
2. Parse mount path, auth header
3. Call `Auth.Validate(token)` (Chunk 7); for now stub a fake validator that accepts everything
4. Construct Session via SessionMgr
5. Spawn a goroutine that reads encoded bytes from the TCP connection, pipes them through a per-session `gst-launch-1.0 fdsrc fd=0 ! decodebin ! audioconvert ! audio/x-raw,format=S16LE,rate=44100,channels=2 ! fdsink` subprocess, and pushes the resulting PCM bytes into the Session's appsrc.
6. On connection close: tear down subprocess + Session.

Why subprocess for decoding: matches existing mediaengine pattern; avoids decoding 5 formats (MP3, AAC, Ogg, Opus, FLAC) in CGo. Subprocess stdin/stdout buffering acceptable for live audio (<50ms).

- [ ] **Steps**: Integration test spawns harbor.go's listener, connects with `tcpclientsink` from gst-launch, asserts PCM arrives at the Session's appsrc within 1s.

### Task 3.2: Integration into main.go

- [ ] Wire `harbor.NewListener(...)` into `cmd/grimnir-fanout/main.go`'s run() when configured.

---

## Chunk 4: RTP ingress (raw RTP push from external broadcast tools)

External broadcast tools (some studio gear, custom integrations) push RTP directly.

### Task 4.1: RTP UDP listener

**Files:**
- Create: `internal/grimnirfanout/rtp.go`
- Create: `internal/grimnirfanout/rtp_test.go`

Listen on `FANOUT_RTP_PORT`. Accept RTP-Opus, RTP-AAC, RTP-PCM (L16). Pion's `rtp.Packet.Unmarshal` for header; route per-SSRC to a Session.

Authentication: tricky for stateless UDP. Phase 1: trust the source IP + a pre-configured shared secret in the first packet's RTP header extension. Document this as weak; refine in Chunk 7.

For each session, the decode chain depends on the RTP payload type. Same subprocess pattern as Harbor for decoding.

- [ ] **Steps**: TDD with synthetic RTP packets; integration test with `audiotestsrc ! rtpopuspay ! udpsink` as the source.

---

## Chunk 5: SRT ingress

SRT (Secure Reliable Transport) is increasingly common for remote DJ links. Requires `github.com/haivision/srtgo` Go binding + `libsrt-dev` system library.

### Task 5.1: SRT listener

**Files:**
- Create: `internal/grimnirfanout/srt.go`
- Create: `internal/grimnirfanout/srt_test.go`

`srtgo.Socket` listens on `FANOUT_SRT_PORT`. SRT streams Transport Stream (MPEG-TS) by convention; decode chain: `fdsrc fd=0 ! tsdemux ! aacparse ! avdec_aac ! audioconvert ! ...`.

- [ ] **Steps**: TDD; integration test using `srt-live-transmit` (system tool) as the source, audiotestsrc encoded as TS in a sidecar.

### Task 5.2: CI workflow already added libsrt-dev in Chunk 1.4

- [ ] Verify GHA passes with SRT chunk landed.

---

## Chunk 6: WebRTC ingress (browser-side WebDJ client)

The WebDJ console (browser) uses WebRTC to push audio to the fan-out. Most common protocol for in-house DJs.

### Task 6.1: WebRTC signaling + ingest

**Files:**
- Create: `internal/grimnirfanout/webrtc.go`
- Create: `internal/grimnirfanout/webrtc_test.go`

HTTP POST `/offer` endpoint accepts an SDP offer from the browser. Use `pion/webrtc/v4` to construct an answer; on data flow, capture Opus RTP packets via `track.OnRTP`; depay → Opus decoder (subprocess `gst-launch-1.0 fdsrc fd=0 ! opusparse ! opusdec ! audioconvert ! fdsink`) → PCM → Session.

Pion already used by `internal/webrtc/broadcaster.go` (outbound). This is the inbound counterpart.

- [ ] **Steps**: Unit test for signaling math; integration test using the existing `internal/webrtc` outbound client to push to the fan-out's inbound.

### Task 6.2: Wire into main.go + ensure `FANOUT_WEBRTC_HTTP_PORT` shares the same mux as /healthz OR runs its own listener

- [ ] **Steps**: deploy decision; document in the commit message

---

## Chunk 7: Auth via control plane gRPC + local cache

Every protocol's session creation calls `Auth.Validate(token, mount)`. The validator dials the control plane gRPC, caches positive results for the token's lifetime.

### Task 7.1: Auth client

**Files:**
- Create: `internal/grimnirfanout/auth.go`
- Create: `internal/grimnirfanout/auth_test.go`

- gRPC client to `cmd/grimnirradio`'s existing live-session-token endpoint (per `internal/auth/`).
- LRU cache keyed by token; TTL = token expiry from the response.
- `Validate(ctx, token, mount string) (claims AuthClaims, err error)`.

- [ ] **Steps**: TDD with mock gRPC server.

### Task 7.2: Token revocation event subscription

- The control plane publishes revocation events on the existing event bus (Redis pub/sub OR NATS per `internal/eventbus/`).
- Fan-out subscribes; on revoke event, evict the cached token and terminate any active session using it.

- [ ] **Steps**: TDD with fake event bus.

### Task 7.3: Replace stub validators in Chunks 3-6 with the real Auth

- [ ] **Steps**: thread auth through; integration tests still pass.

---

## Chunk 8: Redis session-state replication (failover continuity)

Per Q-E2=C + Q-E3=A: active fan-out writes `dj:session:<id>` Redis hash on state changes; peer reads on takeover.

### Task 8.1: Replication writer

**Files:**
- Create: `internal/grimnirfanout/replication.go`
- Create: `internal/grimnirfanout/replication_test.go`

On every Session state change (started, codec negotiated, auth claims cached, mix bus state changed): serialize the relevant fields to JSON and `HSET dj:session:<id> ...`. TTL 60s, refreshed every 5s.

- [ ] **Steps**: TDD with `miniredis` (in-memory Redis for tests; existing pattern from `internal/eventbus`)

### Task 8.2: Replication reader (peer takeover)

On fan-out startup, scan `dj:session:*` keys. For each session whose `last_active_at` is recent (< 30s ago) AND has no local Session: treat as a "warm" pending takeover. When the DJ's reconnect arrives (carries session-id in the protocol-specific way), look up the Redis state, restore claims/codec/mix bus, accept the connection.

- [ ] **Steps**: TDD with two SessionMgr instances backed by the same miniredis; first writes state, second reads via "takeover" code path.

### Task 8.3: Wire replication into Session lifecycle

- [ ] **Steps**: hook the writer into Session.SetState; hook the reader into protocol acceptor paths

---

## Chunk 9: Engine-side pipeline mixing

For the live input to actually become part of the engine's output PCM, the engine must mix it with scheduled-content PCM. Currently engine pipelines (`internal/playout/director.go`) don't have a live-input branch.

### Task 9.1: Add live-input branch to dual-bitrate pipeline

**Files:**
- Modify: `internal/playout/director.go` — `buildDualBroadcastPipeline()`

Add a parallel branch: `udpsrc port=<FANOUT_OUTPUT_PORT> ! rtpjitterbuffer latency=80 ! rtpL16depay ! audioconvert ! audio/x-raw,rate=44100,channels=2 ! audiomixer.sink_1` where `audiomixer` is inserted between the existing `audioresample` and the tee. The scheduled content connects to `audiomixer.sink_0`.

When live audio is silent (or no input arriving), `audiomixer` outputs whichever input is louder (the scheduled content). When live audio arrives, it's mixed into the output bus.

Priority-aware ducking: lookup `internal/priority/` for the live-override behavior.

- [ ] **Steps**: pipeline-string unit test (existing director_pcm_rtp_test.go pattern); integration test with real RTP traffic from a mock fan-out.

### Task 9.2: Same for webstream pipeline

- [ ] Mirror the dual-bitrate change.

### Task 9.3: Config flag

Add `GRIMNIR_LIVE_INPUT_ENABLED` + `GRIMNIR_LIVE_INPUT_FANOUT_ADDR` env vars. Default off; existing single-instance behavior preserved.

- [ ] Steps + tests.

---

## Chunk 10: E2E integration test

The load-bearing validation. Spawn:
1. Two fake engine receivers (gst-launch with udpsrc+rtpjitterbuffer+rtpL16depay+filesink)
2. The fan-out binary
3. A WebDJ-style WebRTC client (using pion as the source)

Inject:
- DJ connects, sends audio for 3s → both engine sinks receive identical PCM
- Fan-out crashes (kill the binary); restart it → DJ reconnects → state restored from Redis, audio resumes
- DJ disconnects → engine sinks stop receiving live input within 200ms

### Task 10.1: Write the E2E test

**Files:**
- Create: `internal/grimnirfanout/e2e_integration_test.go` (build tag `integration`)

- [ ] **Steps**: TDD; runs ~30s; same shape as the edge encoder E2E test

---

## Chunk 11: Docs + deploy + version bump v2.0.0-alpha.5

### Task 11.1: cmd/grimnir-fanout/README.md
### Task 11.2: deploy/grimnir-fanout.service systemd unit
### Task 11.3: CLAUDE.md update — document the new binary + env vars + architectural role
### Task 11.4: Dockerfile.fanout (mirror edge-encoder's Dockerfile, add libsrt-dev)
### Task 11.5: Version bump to v2.0.0-alpha.5, tag, push

---

## Acceptance for the plan as a whole

- Chunk 0 spike report exists, decision = PROCEED
- `cmd/grimnir-fanout` binary builds + runs + exposes gRPC GetStatus
- All four protocols (Harbor, RTP, SRT, WebRTC) accept DJ connections and produce PCM-RTP at the engines
- Two fan-out instances + Redis: a DJ reconnect after killing the active fan-out lands on the peer with auth/state restored
- Engine-side mixing: live audio audibly overrides scheduled content during live shows (per priority ladder)
- E2E test green: full lifecycle (connect → audio flows → failover → audio resumes) passes
- v2.0.0-alpha.5 tagged on origin
- `make ci` exits 0 throughout

## Out of scope (deferred to follow-up issues)

- TLS for SRT, WebRTC, Harbor — TLS terminates at the edge VPS in production; fan-out serves plaintext within the trust boundary
- DJ-side mixing UI (talkover, send levels, ducking control) — lives in WebDJ frontend (separate plan)
- Multi-region DJ routing (DJ in US-East reaches US-East fan-out) — phase 2
- WebRTC ICE/STUN/TURN for restricted networks — phase 1 assumes direct reachability
- Listener-event telemetry from fan-out — separate plan

## Estimated effort

- Chunk 0 (spike): 1 day
- Chunk 1 (scaffold): 2 days
- Chunk 2 (session + pipeline core): 3 days
- Chunk 3 (Harbor): 2-3 days
- Chunk 4 (RTP): 2 days
- Chunk 5 (SRT): 3-4 days (new dep, new build complexity)
- Chunk 6 (WebRTC): 3-4 days
- Chunk 7 (Auth): 3 days
- Chunk 8 (Replication): 4 days
- Chunk 9 (Engine mixing): 2-3 days
- Chunk 10 (E2E): 2 days
- Chunk 11 (docs + tag): 1 day

**Total: 28-33 working days = 6-7 calendar weeks at solo pace.** Matches the Section 9.7 estimate.

