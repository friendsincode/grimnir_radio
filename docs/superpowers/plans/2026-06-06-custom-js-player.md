# Custom JS Player + Reconnect-Resume (Track B-3)

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship the listener-facing browser player that delivers the "no audible reconnect" promise of Q9=D from the HA design: tight reconnect-resume on transient HTTP/ICY drops, multi-URL auto-degrade (HQ → LQ → retry HQ) on persistent failures, subtle UI during reconnects, anonymous reconnect telemetry to the control plane.

**Architecture:** Vanilla JS ES module wrapping HTML5 `<audio>`. No framework. Listens for `error`/`stalled`/`waiting`/`ended`; detaches the current `<audio>` and creates a fresh one pointing at the same URL on failure (the only reliable way to "reconnect" an `<audio>` element). After 3 consecutive failures within 30s, steps down through a list of URLs (HQ → LQ) fetched at page load from `/api/v1/stations/<station-id>/streams`. Background `HEAD` request every 60s while on LQ to step back up.

**Tech Stack:** Vanilla JS (ES modules), HTML5 `<audio>`, MediaSession API for browser-level media controls (lock-screen play/pause). No build step required (modern browsers parse ES modules directly). Backend additions: one new control-plane endpoint (`GET /api/v1/stations/<id>/streams`) + one telemetry endpoint (`POST /api/v1/listener-events`).

**Issue:** TBD — file when first chunk merges.

**Parent design:** Section 5 of `docs/superpowers/plans/2026-06-01-ha-zero-loss-failover-design.md` ("Custom JS player with reconnect-resume").

**Estimated scope:** 6 chunks. **2-3 calendar weeks at solo pace.**

---

## File structure

| File | Status | Responsibility |
|---|---|---|
| `internal/web/static/js/player/player.js` | Create | The reconnect-aware player module |
| `internal/web/static/js/player/telemetry.js` | Create | Anonymous reconnect event posting |
| `internal/web/static/js/player/player.test.html` | Create | Browser-side test harness (manual; opens locally) |
| `internal/web/templates/embed/player.html` | Create | The minimal HTML embed that pages load |
| `internal/web/templates/embed/player_demo.html` | Create | Standalone demo page |
| `internal/api/streams.go` | Create | `GET /api/v1/stations/<id>/streams` |
| `internal/api/streams_test.go` | Create | |
| `internal/api/listener_events.go` | Create | `POST /api/v1/listener-events` (anonymous, IP-from-socket) |
| `internal/api/listener_events_test.go` | Create | |
| `internal/models/listener_event.go` | Create | `ListenerEvent` model + migration |
| `migrations/00X_listener_events.sql` | Create | Expand-only |
| Test fixtures + integration tests via go-rod (browser tests, existing pattern) | Create | |

## Chunks

### Chunk 1: Backend streams endpoint

The player needs to know which URLs to try. `GET /api/v1/stations/<id>/streams` returns the ordered list (HQ first, LQ second; future: HLS as a third option).

Response:
```json
{
  "streams": [
    {"url": "https://<public-hostname>/main/hq", "format": "mp3", "bitrate_kbps": 128, "label": "HQ"},
    {"url": "https://<public-hostname>/main/lq", "format": "mp3", "bitrate_kbps": 64,  "label": "LQ"}
  ]
}
```

- [ ] Task 1.1: model + handler with TDD
- [ ] Task 1.2: integration test using existing `internal/api/api_test.go` patterns

### Chunk 2: Player module — base reconnect path

Single-URL player. Listens for `error|stalled|waiting|ended`; on any of those, recycles the `<audio>` element.

- [ ] Task 2.1: `player.js` skeleton — module exports `createPlayer(containerEl, streamUrl)`
- [ ] Task 2.2: event-listener wiring; recycle-on-failure logic
- [ ] Task 2.3: reconnect counter + sliding-window detection (3-in-30s → escalation hook stubbed)
- [ ] Task 2.4: browser test harness — manual page that loads the module + lets a human kill the stream upstream

### Chunk 3: Multi-URL auto-degrade

- [ ] Task 3.1: at construct time, fetch `/api/v1/stations/<id>/streams`; store the ordered list
- [ ] Task 3.2: degrade path — on 3-reconnect-in-30s trigger, step to next URL
- [ ] Task 3.3: upgrade path — background `HEAD` against the higher-preference URL every 60s while degraded
- [ ] Task 3.4: exhaustion state — all URLs failed → show "Stream temporarily unavailable" + manual retry button; stop auto-reconnect

### Chunk 4: UI state during reconnect (subtle)

- [ ] Task 4.1: silent reconnect < 500ms (no UI change)
- [ ] Task 4.2: 500ms-3s — thin progress bar + spinner on play button
- [ ] Task 4.3: 3+ attempts → flash "Reconnecting..." text for ~1s, then resume on next URL silently
- [ ] Task 4.4: All-URLs-exhausted error state
- [ ] Task 4.5: Active stream label (HQ / LQ) visible but de-emphasized; manual quality dropdown

### Chunk 5: MediaSession + listener-event telemetry

- [ ] Task 5.1: wire MediaSession API (lock screen, OS notification controls)
- [ ] Task 5.2: anonymous reconnect events POST to `/api/v1/listener-events` (no listener ID; server uses request socket IP for rate limiting only, not storage)
- [ ] Task 5.3: `data-grimnir-no-telemetry="1"` attribute opt-out
- [ ] Task 5.4: ListenerEvent model + migration + handler (Chunk 1 wired the route shell; this task wires the payload)
- [ ] Task 5.5: events surfaced in operator dashboard (handle in B-4 observability)

### Chunk 6: Integration + docs + ship

- [ ] Task 6.1: end-to-end test using go-rod (existing browser-test pattern in `test/e2e/`)
- [ ] Task 6.2: replace existing player embed in `internal/web/templates/` (find current embed; swap in new module)
- [ ] Task 6.3: README in `internal/web/static/js/player/README.md`
- [ ] Task 6.4: CLAUDE.md note about the player + the two new endpoints
- [ ] Task 6.5: version bump on whatever release line this rides

## Acceptance

- A listener whose connection blips for < 500ms hears no UI change and no audio glitch
- A listener whose primary stream fails 3× in 30s gets stepped down to LQ silently
- A listener whose HQ stream recovers gets stepped back up within 60s
- A listener with both streams down sees the "temporarily unavailable" UI
- Reconnect events appear in the operator dashboard (post B-4)
- E2E browser test verifies the recycle-on-error path

## Out of scope

- Service Worker for offline cache (real complexity for marginal benefit)
- Media Source Extensions / MSE (phase 1 doesn't need it; the recycle pattern is enough)
- HLS player support (separate plan; HLS players have their own reconnect built in)
- Adaptive bitrate based on observed bandwidth (custom for ICY; phase 1.5 at earliest)
- DRM / playback restrictions (not part of grimnir's model)

## Estimated effort

- Chunk 1 (backend endpoints): 1-2 days
- Chunk 2 (base reconnect): 2-3 days
- Chunk 3 (multi-URL): 2-3 days
- Chunk 4 (UI state): 2 days
- Chunk 5 (MediaSession + telemetry): 2-3 days
- Chunk 6 (integration): 2 days

**Total: 11-15 working days = 2-3 calendar weeks at solo pace.**

## Filed

2026-06-06 as part of the full v2 plan-writing pass. Execution is a separate effort.
