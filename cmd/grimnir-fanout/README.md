# grimnir-fanout

Live-input fan-out for the Grimnir HA architecture. Accepts a single DJ
connection over one of four protocols (Harbor TCP, raw RTP, SRT, WebRTC) &
duplicates the audio as PCM-over-RTP toward every running media engine, so the
lockstep executor survives an engine failover mid-broadcast.

## What it does

A DJ connects once. The fan-out terminates the protocol (TLS, Opus decode,
SRT handshake, WebRTC ICE/DTLS, whatever the source brings), normalises the
PCM to 44.1 kHz S16 stereo, & ships an identical RTP-L16 stream to engine A
& engine B over `multiudpsink`. Both engines mix the live PCM into their
output through an always-on `audiomixer` branch, so a media-engine restart
or VRRP failover loses zero DJ samples on the wire from the listener's
perspective.

Detail flow:

1. DJ source connects to one of the four ingresses (see "Port assignments").
2. Per-ingress glue authenticates the connection via gRPC against the control
   plane's `DJAuth` service (`internal/grimnirfanout/auth_grpc.go`), with a
   500-entry LRU + 5-min TTL cache & event-bus revocation.
3. On accept, a `Session` is opened & a per-session GStreamer pipeline is
   spawned. The pipeline normalises sample rate / channel count & terminates
   in `rtpL16pay → multiudpsink` with both engine RTP ports as targets.
4. The fan-out fires `SetLiveInput(true)` over gRPC against each media
   engine's `LiveInputControl` service. Engine pipelines hold an always-on
   `audiomixer` branch fed by `udpsrc` on `GRIMNIR_LIVE_INPUT_PORT`; flipping
   the boolean controls whether DJ PCM gets mixed into the output.
5. When the DJ disconnects (clean RTCP BYE, TCP close, SRT-level disconnect,
   WebRTC peer-connection close), the session terminator calls
   `SetLiveInput(false)`, drains the pipeline, & releases the slot.

Two fan-out instances run side-by-side with shared session state replicated
through Redis (`internal/grimnirfanout/replication.go`). If the active
fan-out dies mid-broadcast, the DJ's reconnect attempt lands on the peer
with auth + session metadata already in cache.

## Architecture in the v2 HA story

The v2 HA path has three audio-plane binaries on each node:

- `grimnir-mediaengine` produces PCM. With `GRIMNIR_HA_PCM_RTP_ENABLED=true`
  every engine fans its output PCM to every edge encoder over RTP-L16.
- `grimnir-fanout` (this binary) inverts the direction: one DJ in, N engines
  out. Engines mix the DJ PCM into the same output pipeline that's already
  serving scheduled content.
- `edge-encoder` consumes engine PCM, switches between engines on health
  loss, & is the only listener-facing process.

Without fan-out a DJ has to reconnect on every engine failover, because the
DJ source talks to exactly one engine. With fan-out the DJ talks to one IP
(the VIP) & the audio reaches both engines in lockstep; an engine restart
or kernel panic is invisible to the DJ & inaudible to the listener.

See `docs/superpowers/plans/2026-06-05-live-input-fan-out.md` for the
implementation plan, & `cmd/edge-encoder/README.md` for the listener side.

## Port assignments

| Port | Protocol | Purpose |
|---|---|---|
| 8000 | TCP | Harbor (Icecast `SOURCE` / HTTP `PUT`) ingress |
| 5006 | UDP | Raw RTP-L16 ingress (FFmpeg, OBS, hardware encoder) |
| 1935 | UDP | SRT listener |
| 8004 | TCP | WebRTC HTTP signalling (whip-style POST + ICE) |
| 9093 | TCP | gRPC server (`GrimnirFanout.GetStatus` + future ops) |
| 8003 | TCP | HTTP `/healthz` |
| 9193 | TCP | Prometheus `/metrics` |

Defaults come from `internal/grimnirfanout/config.go`; override via env vars
below. Port 9192 is reserved for edge-encoder; 9193 keeps the per-binary
metrics ports adjacent for ops convenience.

## Environment variables

The fan-out reads `FANOUT_*` first, then falls back to `RLM_FANOUT_*` for
parity with the rest of the repo. All ports are decimal ints; booleans accept
`1/true/yes/on` & `0/false/no/off`.

| Variable | Default | Purpose |
|---|---|---|
| `FANOUT_BIND_ADDR` | `0.0.0.0` | Bind address shared by every listener |
| `FANOUT_GRPC_PORT` | `9093` | gRPC control surface |
| `FANOUT_HTTP_PORT` | `8003` | `/healthz` |
| `FANOUT_METRICS_PORT` | `9193` | Prometheus `/metrics` |
| `FANOUT_HARBOR_PORT` | `8000` | Icecast-compatible ingress |
| `FANOUT_RTP_PORT` | `5006` | Raw RTP-L16 ingress |
| `FANOUT_SRT_PORT` | `1935` | SRT listener |
| `FANOUT_WEBRTC_HTTP_PORT` | `8004` | WebRTC signalling |
| `FANOUT_ENGINE_A_RTP` | *required* | `host:port` of engine A's PCM RTP ingress (e.g., `<node-a-ip>:5104`) |
| `FANOUT_ENGINE_B_RTP` | empty | `host:port` of engine B; empty for single-engine deployments |
| `FANOUT_CONTROL_PLANE_GRPC` | empty | Control-plane gRPC for DJ auth. When empty the binary boots in `AcceptAllAuthenticator` mode (dev only — every DJ token is accepted) |
| `FANOUT_REDIS_ADDR` | empty | Redis for cross-fanout session replication. When empty, sessions are single-node only |
| `FANOUT_NETCLOCK_ENABLED` | `false` | Bind pipelines to the region's NetClock master so engine-side mix output is sample-aligned across both engines |
| `FANOUT_NETCLOCK_MASTER_ADDR` | empty | `host:port` of the NetClock master. Required when `FANOUT_NETCLOCK_ENABLED=true` |
| `FANOUT_LOG_LEVEL` | `info` | zerolog level |

`FANOUT_ENGINE_A_RTP` is the only required field. Boot fails with a clear
error if it's unset.

## Build

Requires GStreamer 1.20+ dev headers, `libsrt-dev` for the SRT ingress, &
the standard plugin packs.

Debian/Ubuntu:

```bash
sudo apt-get install libgstreamer1.0-dev libsrt-openssl-dev \
    gstreamer1.0-plugins-base gstreamer1.0-plugins-good \
    gstreamer1.0-plugins-ugly gstreamer1.0-plugins-bad
```

Arch:

```bash
sudo pacman -S gstreamer gst-plugins-base gst-plugins-good \
    gst-plugins-ugly gst-plugins-bad srt
```

Then:

```bash
go build -o bin/grimnir-fanout ./cmd/grimnir-fanout
```

Like `edge-encoder`, this binary uses **go-gst CGo bindings**, not the
`gst-launch-1.0` subprocess pattern used by the rest of grimnir. Runtime
control of the per-session pipeline (mute, drain, switch input) needs
programmatic property access.

## Run

Dev rig (single engine, no auth, accepts any DJ token):

```bash
FANOUT_ENGINE_A_RTP=127.0.0.1:5104 \
FANOUT_HARBOR_PORT=8000 \
FANOUT_RTP_PORT=5006 \
./bin/grimnir-fanout
```

Production HA (both engines, auth on, Redis replication):

```bash
FANOUT_ENGINE_A_RTP=<node-a-ip>:5104 \
FANOUT_ENGINE_B_RTP=<node-b-ip>:5104 \
FANOUT_CONTROL_PLANE_GRPC=<control-plane-host>:9090 \
FANOUT_REDIS_ADDR=10.10.0.20:6379 \
FANOUT_NETCLOCK_ENABLED=true \
FANOUT_NETCLOCK_MASTER_ADDR=<node-a-ip>:9094 \
./bin/grimnir-fanout
```

Quick Harbor smoke test against a running fan-out:

```bash
ffmpeg -re -i loop.mp3 -c:a libmp3lame -b:a 128k \
  -content_type audio/mpeg -f mp3 \
  icecast://source:test-token@127.0.0.1:8000/dj
```

Both engines should see PCM arrive on their `GRIMNIR_LIVE_INPUT_PORT` & the
fan-out should log a `session_started` event with the DJ's auth subject.

## Operations

- `/healthz` returns 200 once every listener has bound.
- `/metrics` serves the binary's Prometheus registry (`FanoutRegistry` in
  `internal/metrics/registry.go`, namespace `grimnir_fanout_*`). Per-protocol
  counters & auth-cache gauges land here as they're wired up.
- gRPC `GetStatus` returns the live session list, uptime, & last-seen
  timestamps per engine target. Useful from the control plane's deploy
  pre-check.
- When the fan-out is down see `docs/runbooks/fanout-down.md`.
