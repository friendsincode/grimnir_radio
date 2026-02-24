# Streaming Update: Built-in Harbor Source Receiver

> Eliminate the Icecast server dependency by building a native source receiver into the control plane.

## Problem

DJs using BUTT and Mixxx need the Icecast source protocol to stream live audio. Currently, Grimnir requires a standalone Icecast server as a middleman: DJs push audio to Icecast, then the media engine pulls from it via `souphttpsrc`. This adds deployment complexity, an extra container, and an unnecessary network hop.

## Solution

Build a **harbor** — an HTTP server built into the control plane that directly accepts Icecast-compatible source connections, decodes the audio to raw PCM, and feeds it into the existing encoder pipeline. The Icecast source protocol is just HTTP PUT with Basic auth — no proprietary code or licensing concerns.

## Architecture

```
BUTT/Mixxx
    │  PUT /live.mp3
    │  Authorization: Basic base64(source:<token>)
    │  Content-Type: audio/mpeg
    │  [continuous audio stream]
    ▼
┌─────────────────────────────────────┐
│  Harbor HTTP Server (:8088)         │  ← new, in control plane
│  1. Parse Basic auth → extract token│
│  2. Resolve mount path → station    │
│  3. live.Service.HandleConnect()    │
│  4. Start GStreamer decoder process │
│  5. io.Copy(decoderStdin, req.Body) │
└──────────┬──────────────────────────┘
           │ raw S16LE PCM (stdout)
           ▼
┌──────────────────────────────────────┐
│  Existing Encoder Pipeline (fdsrc)   │  ← already exists
│  fdsrc fd=0 → tee → lamemp3enc → HQ │
│                   → lamemp3enc → LQ  │
│                   → opusenc → WebRTC │
└──────────────────────────────────────┘
```

## Key Design Decisions

1. **Harbor lives in the control plane** — has direct DB access (token validation), event bus (DJ connect/disconnect events), and playout director (encoder pipeline stdin). No new gRPC plumbing needed.

2. **Audio piped via stdin/stdout** — follows the existing `pcmCrossfadeSession` pattern: a GStreamer decoder reads compressed audio from stdin, outputs raw S16LE PCM to stdout, which is written to the encoder pipeline's stdin. No CGo, no named pipes, no TCP port allocation.

3. **Token-based auth via HTTP Basic** — BUTT/Mixxx send `Authorization: Basic base64(source:password)`. We use `source` as username (standard convention, ignored) and the generated token as password. Same token system the web UI already uses.

4. **Both PUT and SOURCE methods** — modern clients use HTTP PUT, legacy clients (older BUTT versions) use the proprietary `SOURCE` method. We support both. SOURCE requires HTTP connection hijacking.

## Implementation Plan

### Issue 1: Harbor config and package skeleton
- Add `GRIMNIR_HARBOR_ENABLED`, `GRIMNIR_HARBOR_BIND`, `GRIMNIR_HARBOR_PORT`, `GRIMNIR_HARBOR_MAX_SOURCES` to `internal/config/config.go`
- Create `internal/harbor/` package with `Server` struct, `ListenAndServe()`, `Shutdown(ctx)`
- Wire into `internal/server/server.go` startup/shutdown

**Files:** `internal/config/config.go`, `internal/harbor/server.go`, `internal/server/server.go`

### Issue 2: Icecast source protocol handler
- Implement `handleSource(w, r)` HTTP handler
- Accept PUT and SOURCE methods (405 for others)
- Parse `Authorization: Basic` header, extract token from password field
- Resolve mount from URL path (e.g., `/live.mp3` → DB lookup `Mount` by name)
- For SOURCE method: hijack TCP connection, send `HTTP/1.0 200 OK\r\n\r\n`
- For PUT method: send 200, flush headers, read from `r.Body`
- Parse Ice-* metadata headers (`Ice-Name`, `Ice-Description`, `Ice-Genre`, `Ice-Bitrate`, `Content-Type`, `User-Agent`)
- Enforce max concurrent sources limit
- Track active connections in `conns map[string]*SourceConnection`

**Files:** `internal/harbor/server.go`, `internal/harbor/metadata.go`

### Issue 3: GStreamer decoder for live source audio
- Build decoder pipeline: `fdsrc fd=0 ! decodebin ! audioconvert ! audioresample ! audio/x-raw,format=S16LE,rate=44100,channels=2 ! fdsink fd=1`
- Decoder reads compressed audio (MP3, Ogg, AAC, Opus) from stdin
- Decoder writes raw S16LE PCM to stdout
- Uses `decodebin` for automatic format detection
- Goroutine pipes decoder stdout → encoder stdin
- Clean shutdown on context cancellation

**Files:** `internal/harbor/decoder.go`

### Issue 4: Playout director live source injection
- Add `Director.InjectLiveSource(ctx, stationID, mountID) (encoderIn io.WriteCloser, release func(), err error)`
  - Pauses the `pcmCrossfadeSession` for the mount (stops current decoder, keeps encoder pipeline running)
  - Returns the encoder's stdin `io.WriteCloser` for the harbor decoder to write into
  - Returns a `release` callback that resumes automation when the DJ disconnects
- Add `pcmCrossfadeSession.Pause()` — stops current decoder, returns `encoderIn`
- Add `pcmCrossfadeSession.Resume()` — re-enables the session for automation

**Files:** `internal/playout/director.go`, `internal/playout/crossfade.go`

### Issue 5: End-to-end harbor connection lifecycle
- Wire the full flow: auth → connect → decode → inject → stream → disconnect → release
- `handleSource` calls `live.Service.HandleConnect()` after auth succeeds (triggers priority handover)
- `io.Copy(decoderStdin, audioSource)` blocks until DJ disconnects (TCP close / EOF)
- On disconnect: close decoder, call `live.Service.HandleDisconnect()`, call `release()` to resume automation
- Publish `EventDJConnect` / `EventDJDisconnect` events (already handled by live service)
- Handle edge cases: DJ reconnect, encoder pipeline not running, station not active

**Files:** `internal/harbor/server.go`

### Issue 6: Dashboard UI updates for harbor mode
- When harbor is enabled, show harbor host:port in Connection Info card instead of Icecast
- Update BUTT guide: Address=harbor host, Port=harbor port, Mount=/live.mp3, User=source, Password=token
- Update Mixxx guide similarly
- Show both harbor and Icecast info if both are configured
- Pass `HarborEnabled`, `HarborHost`, `HarborPort` to template data

**Files:** `internal/web/pages_live.go`, `internal/web/templates/pages/dashboard/live/dashboard.html`

### Issue 7: Docker and deployment updates
- Add harbor env vars to `docker-compose.yml` grimnir service
- Expose port 8088 in docker-compose.override.yml
- Add nginx proxy config example for harbor (proxy_buffering off, client_max_body_size 0)
- Icecast container becomes optional (can be removed from compose if harbor is enabled)

**Files:** `docker-compose.yml`, `docker-compose.override.yml`

### Issue 8: Harbor tests
- Unit tests for Basic auth parsing (valid, invalid, missing, malformed base64)
- Unit tests for mount path resolution
- Unit tests for SOURCE vs PUT method handling
- Unit tests for Ice-* header parsing
- Unit tests for max sources enforcement
- Integration test for connection lifecycle (connect → stream → disconnect)

**Files:** `internal/harbor/server_test.go`, `internal/harbor/decoder_test.go`

## Compatibility

| Software | Method | Content-Type | Status |
|----------|--------|-------------|--------|
| BUTT | PUT | audio/mpeg, audio/ogg | Supported |
| Mixxx | SOURCE/PUT | audio/mpeg, audio/ogg | Supported |
| OBS | PUT | audio/mpeg | Supported |
| ffmpeg | PUT | any audio/* | Supported |
| Liquidsoap | PUT | any audio/* | Supported |

## Configuration

```env
GRIMNIR_HARBOR_ENABLED=true       # Enable built-in source receiver
GRIMNIR_HARBOR_BIND=0.0.0.0       # Bind address
GRIMNIR_HARBOR_PORT=8088           # Listen port (DJs connect here)
GRIMNIR_HARBOR_MAX_SOURCES=10      # Max concurrent source connections
```

## Migration Path

1. Enable harbor alongside Icecast (both work simultaneously)
2. Update DJ connection guides to point at harbor
3. Once verified, remove Icecast container from docker-compose
4. Reclaim the port and resources
