# edge-encoder

Edge encoder for the Grimnir HA architecture. Ingests raw PCM via RTP from two media engines, performs sample-aligned input switching when the active source dies, and serves the encoded result to HTTP/ICY listeners and (optionally) HLS via S3.

## What it does

- Binds two UDP ports (default `:5004` and `:5005`) for incoming RTP-L16 stereo 44.1kHz PCM from media engines
- GStreamer pipeline: `udpsrc → rtpjitterbuffer → rtpL16depay → audioconvert → leaky-queue → input-selector → audioconvert → tee → lamemp3enc → appsink (HTTP /live)` plus optional `tee → aacenc → hlssink2 → S3` branch
- Per-input health monitored via two signals (combined with AND): pad-probe packet arrival (100ms window) AND gRPC `GetStatus` poll on the engine (3 consecutive failures = unhealthy)
- Switcher with 100ms hysteresis flips `input-selector` when the active input goes unhealthy and the other is healthy
- Output: HTTP/ICY at `/live` (default port `:8001`), HLS segments + manifest pushed to S3 if enabled

## Environment variables

| Variable | Default | Purpose |
|---|---|---|
| `EDGE_ENCODER_BIND_ADDR` | `0.0.0.0` | gRPC + HTTP bind address |
| `EDGE_ENCODER_GRPC_PORT` | `9092` | gRPC port for `GetStatus` |
| `EDGE_ENCODER_HTTP_PORT` | `8001` | HTTP/ICY listener port (serves `/live` and `/healthz`) |
| `EDGE_ENCODER_METRICS_PORT` | `9192` | Prometheus metrics endpoint (reserved; not yet wired) |
| `EDGE_ENCODER_RTP_PORT_A` | `5004` | UDP port for engine A's RTP-L16 |
| `EDGE_ENCODER_RTP_PORT_B` | `5005` | UDP port for engine B's RTP-L16 |
| `EDGE_ENCODER_ENGINE_A_GRPC` | empty | `host:port` of engine A's gRPC; empty disables health subscription (pure packet-arrival health) |
| `EDGE_ENCODER_ENGINE_B_GRPC` | empty | same for engine B |
| `EDGE_ENCODER_OUTPUT_FORMAT` | `mp3` | `mp3` or `aac` for the main HTTP/ICY output |
| `EDGE_ENCODER_OUTPUT_BITRATE_KBPS` | `128` | Encoder bitrate |
| `EDGE_ENCODER_HLS_ENABLED` | `false` | If true, also emit HLS segments to S3 |
| `EDGE_ENCODER_HLS_S3_BUCKET` | empty | S3 bucket name (required when HLS enabled) |
| `EDGE_ENCODER_HLS_S3_REGION` | `us-east-1` | S3 region |
| `EDGE_ENCODER_HLS_S3_ENDPOINT` | empty | Custom S3 endpoint (for MinIO etc.) |
| `EDGE_ENCODER_HLS_S3_USE_PATH_STYLE` | `false` | Path-style addressing (MinIO needs `true`) |
| `EDGE_ENCODER_HLS_SEGMENT_DIR` | `/tmp/grimnir-hls` | Local staging dir for segments before S3 upload |
| `EDGE_ENCODER_LOG_LEVEL` | `info` | Log level |

AWS credentials come from the standard AWS SDK chain (env vars, IAM role, shared config).

## Build

Requires GStreamer 1.20+ development headers + plugin packs.

**Debian/Ubuntu:**
```bash
sudo apt-get install libgstreamer1.0-dev \
    gstreamer1.0-plugins-base gstreamer1.0-plugins-good \
    gstreamer1.0-plugins-ugly gstreamer1.0-plugins-bad
```

**Arch:**
```bash
sudo pacman -S gstreamer gst-plugins-base gst-plugins-good gst-plugins-ugly gst-plugins-bad
```

Then:
```bash
make build-edge-encoder
```

Note: edge-encoder uses **go-gst CGo bindings** (`github.com/go-gst/go-gst`) rather than the `gst-launch-1.0` subprocess pattern used by the rest of grimnir. This is deliberate: runtime control of `input-selector` requires programmatic property access, which `gst-launch` can't provide.

## Run

Single-engine smoke test (works without a peer; left input is the only feed):

```bash
EDGE_ENCODER_RTP_PORT_A=5004 EDGE_ENCODER_HTTP_PORT=8001 ./edge-encoder
```

Then in another terminal:

```bash
gst-launch-1.0 audiotestsrc freq=440 ! audioconvert \
  ! audio/x-raw,format=S16BE,rate=44100,channels=2 \
  ! rtpL16pay pt=10 ! udpsink host=127.0.0.1 port=5004 sync=true
```

Listen:

```bash
ffplay http://localhost:8001/live
```

## Architecture references

- Design: `docs/superpowers/plans/2026-06-01-ha-zero-loss-failover-design.md` Section 2 (audio path)
- Implementation plan: `docs/superpowers/plans/2026-06-03-edge-encoder-pcm-transport.md`
- Spike validation: `docs/superpowers/spikes/2026-06-03-pcm-switching-spike.md`
