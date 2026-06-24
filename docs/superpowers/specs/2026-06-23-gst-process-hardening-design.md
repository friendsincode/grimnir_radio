# GStreamer process hardening & overload regression tests (v2)

Date: 2026-06-23
Status: design, pending review
Scope: v2 branch only. No v1 changes.

## Problem

Two related failure modes on the v2 audio path.

**Zombie accumulation.** Every `gst-launch` subprocess and every go-gst binary calls `gst_init`, which forks `gst-plugin-scan` to build the plugin registry. With no persistent, writable registry cache the scan re-forks on every spawn, and the dead scans pile up as zombies because the container runs no PID-1 reaper. The v1 monolith showed ~2300 such zombies in production. No Dockerfile or compose file sets `GST_REGISTRY` today, and no `exec.Command` site sets `cmd.Env`, so the cache lever is unused.

**Overload from un-released decoders.** Rapid user actions (queueing, previewing, DJ peers connecting and dropping) spawn subprocess decoders that can leak if a teardown path is missed. No metric exposes how many decoders or pipelines are live, and no test drives a burst of actions and asserts resources return to baseline.

### The four live subprocess spawn sites in v2

The control-plane broadcast pipeline (`internal/playout/pipeline.go`) is in-process go-gst and is not a spawner. The subprocess `gst-launch` spawners that remain are:

1. `internal/playout/crossfade.go:159` (control-plane binary). Uses process groups (`processgroup.go`) + `stop()`.
2. `internal/grimnirfanout/harbor_sink.go:77` (fan-out binary). `End()` calls `Wait()`.
3. `internal/grimnirfanout/ingress_webrtc.go:375` (fan-out binary). Reaped in `tearDownPeer`, but reachability is buggy (below).
4. `internal/mediaengine/gstreamer.go:169` (media-engine binary). **Live in v2**, reached via `cmd/mediaengine/main.go:183/210` → `service.go` `PipelineManager` → `pipeline.go` → `NewGStreamerProcess`. It has **no process-group isolation** (no `Setpgid`) and uses a bare `gp.cmd.Process.Kill()` at `gstreamer.go:238/272`, so its `gst-launch` grandchildren can orphan. This was missed in the first draft; it is in scope.

## Goals

- Stop `gst-plugin-scan` zombies at the source so they don't generate, not just reap them after.
- Guarantee teardown on every release path for all four subprocess spawners.
- Make live resource counts observable (Prometheus) so overload is visible before it bites.
- Add regression tests that drive a burst of user actions and fail if subprocess/pipeline counts don't return to baseline.

## Non-goals

- No runtime rejection, eviction, or queueing of user requests. Behavior at high load is "bound + log": the work is allowed, but it is guaranteed to be released and is visible.
- No changes to the v1.40.x line.
- No change to the in-process go-gst happy path beyond adding metrics.

## Approach

### A. Registry cache (removes the zombie source)

Point `GST_REGISTRY` at a persistent, writable path for both the long-lived go-gst binaries and the spawned `gst-launch` subprocesses (inject via `cmd.Env`). Warm it once at process start so `gst-plugin-scan` runs and every later `gst_init` reads the cache.

- One small helper resolves the cache path (env override, default under a writable state dir), ensures it exists and is writable (fail loudly at boot otherwise), and returns the env pairs to inject on every spawn.
- Accuracy note: `gst-plugin-scan` re-forks only if a plugin's mtime/size changes. In a fixed container image the plugin set is immutable, so the scan runs effectively once. Document that assumption rather than asserting "exactly once" unconditionally.
- Each app-node compose service that runs gst (`docker-compose.yml` control plane + media engine, `docker-compose.fanout.yml` fan-out) mounts the cache path as a named volume.

### B. Spawn hygiene / guaranteed teardown (all four sites)

Reuse the existing `internal/playout/processgroup.go` (`newPipelineProcessGroup` / `killProcessGroup`) rather than inventing new packages. Promote the "spawn in a process group + guaranteed kill + Wait, idempotent" pattern into one small shared helper (a single file, ~30 lines) and apply it to all four sites:

- `crossfade.go`: already grouped; add a `defer` guard so a panic mid-`Play()` still reaps.
- `harbor_sink.go`: ensure `End()` always `Wait()`s, including when `Begin()` half-failed.
- `ingress_webrtc.go`: the leak is reachability, not `tearDownPeer` itself (which is already idempotent and nil-safe). Two gaps: (a) `attachTrack` can `startOpusDecoder()` before the peer is inserted into `ing.peers` (at `handleOffer` end), so an error inside `attachTrack` calls `removePeer`, finds `!ok`, and returns without reaping the live decoder; (b) a peer that stalls in `Connecting` never hits the `Failed|Closed|Disconnected` states that trigger teardown. Fix: tear the decoder down on any `attachTrack` error path regardless of map membership, and add a connection-establishment timeout that forces teardown.
- `gstreamer.go` (media engine): add process-group isolation and route `Stop()`/`Kill()` through the shared helper so grandchildren can't orphan.

### C. PID-1 reaper (catch-all), and what the existing reaper does NOT cover

Add `init: true` (tini) to the v2 app-node services in compose. It reaps any straggler that reparents to PID 1. Not a Go `wait(-1)` subreaper: that races `os/exec`'s own `cmd.Wait()` (`wait4` on the specific PID) and yields spurious `ECHILD` ("no child processes") on the decoders we spawn deliberately.

Important: the existing orphan reaper (`internal/playout/reaper.go`) is control-plane-only (`server.go:648`) and matches **only** the broadcast pipeline signature `udpsink ... host=127.0.0.1` (`reaper.go:175`). It deliberately ignores crossfade, Harbor, WebRTC, and media-engine decoders. So the reaper is not the safety net for the four subprocess spawners; tini is. The spec adds one additive counter to the reaper but does not extend its matching.

### D. Observability (per-binary registries)

`internal/metrics` registries are per-binary (`registry.go`: `GrimnirRadioRegistry`, `MediaEngineRegistry`, `FanoutRegistry`, `EdgeEncoderRegistry`). The `type` label space is therefore partitioned across binaries, not one gauge:

- `grimnir_gst_subprocesses_active{type="crossfade"}` in `GrimnirRadioRegistry`.
- `grimnir_gst_subprocesses_active{type="harbor"|"webrtc"}` in `FanoutRegistry`.
- `grimnir_gst_subprocesses_active{type="mediaengine"}` in `MediaEngineRegistry`.
- `grimnir_gst_zombies` gauge in each of those three binaries, sampled every 30s by counting `Z`-state children of self via the same injectable `/proc` seam the reaper uses (`procRoot`), so it's unit-testable without real zombies.
- `grimnir_gst_orphans_reaped_total` counter incremented at the reaper's `syscall.Kill` success site (`reaper.go:85`). Additive, no behavior change.

Do not add a new `pipelines_active` gauge for the media engine: `internal/mediaengine/service.go:159` already sets `telemetry.MediaEngineActivePipelines`. Reuse it. A `pipelines_active{kind="fanout"}` gauge may be added in the fan-out binary if not already present.

Active-subprocess gauges increment at spawn and decrement inside the shared teardown helper, so they track the real count.

### E. Regression tests (the core deliverable)

Two tiers, kept distinct to avoid CI flake:

- **Unit tier (default):** keep the existing fakeable-spawner pattern (`crossfade_pump_test.go` injects `GStreamerBin: "true"` and observes fake `decoderProc`s; `reaper_test.go` uses an injectable fake `/proc`). Click-storm tests drive `Play()`/peer attach-detach/Harbor begin-end in bursts and assert the **active-subprocess gauge returns to baseline** and teardown callbacks fired. The zombie-count gauge is tested against a fake `/proc` via `procRoot`. These are new tests modeled on the existing ones, not in-place extensions.
- **Integration tier (`integration` build tag):** actually spawn `gst-launch` where available and count this process's real gst children by parent-PID + signature, asserting return-to-baseline via condition-based polling. Real-PID counting lives only here, never in the unit tier.
- Registry-warm test: point `GST_REGISTRY` at a temp dir, spawn twice, assert the cache file is created on the first spawn and its mtime is unchanged after the second (cache reused), rather than trying to count scan invocations.

All waiting is condition-based polling, never fixed sleeps. Runs under `-race`; gated by `make ci`.

## Risks & mitigations

- Registry path not writable in a read-only container → fail loudly at boot; document the volume mount per compose file.
- Subprocess counting in integration tests must match only this process's children, or shared CI flakes → match by parent PID + signature, integration tier only.
- tini changing signal forwarding could affect graceful shutdown → verify SIGTERM still drains pipelines cleanly in an integration test.

## Rollout

All behind v2. Metrics join the existing per-binary Prometheus scrape. Tests gate in `make ci`. Ships with the v2 cutover; no production deploy implied.

## Affected files (anticipated)

- New: one small shared spawn+reap helper (extends/relocates `internal/playout/processgroup.go`); a registry-cache env helper; test helpers.
- Edit: `internal/playout/crossfade.go`, `internal/grimnirfanout/harbor_sink.go`, `internal/grimnirfanout/ingress_webrtc.go`, `internal/mediaengine/gstreamer.go`, `internal/playout/reaper.go` (counter), `internal/metrics` (per-binary gauges), `docker-compose.yml` + `docker-compose.fanout.yml` (`init: true`, registry volume).
- Tests: click-storm + zombie-gauge unit tests, integration real-PID tests, registry-warm test.
