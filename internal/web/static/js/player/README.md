# Grimnir JS Player

Vanilla JS ES module that wraps `<audio>` with a reconnect-aware lifecycle, multi-URL auto-degrade, MediaSession integration, & anonymous telemetry. Shipped in v2.0.0-alpha.10 (Track B-3, see `docs/superpowers/plans/2026-06-06-custom-js-player.md`).

No build step. No framework. Modern browsers parse the module as-is.

## Files

- `player.js` — the module. Exports one function: `createPlayer(containerEl, stationId, options)`.
- `player.test.html` — manual test harness. Open it locally, point it at a station ID, kill the upstream to see reconnect tiers fire.

## Quick use

```html
<div id="my-player" data-station-id="11111111-1111-1111-1111-111111111111"></div>
<script type="module">
  import { createPlayer } from '/static/js/player/player.js';
  const el = document.getElementById('my-player');
  createPlayer(el, el.dataset.stationId, { stationName: 'My Station' });
</script>
```

The module fetches `/api/v1/stations/<id>/streams` on construct, renders its own minimal play/pause/quality/status DOM inside the container, & owns the `<audio>` lifecycle from there.

## API

```js
const player = createPlayer(containerEl, stationIdOrUrl, options);
```

`stationIdOrUrl` is either a UUID (module fetches `/api/v1/stations/<id>/streams`) or a full `https://...` URL (single-stream mode; no auto-degrade).

`options`:

| key | default | what |
|---|---|---|
| `streams` | (none) | Pre-supply the ordered list & skip the fetch. Array of `{url, label, priority}`. |
| `stationId` | (none) | Telemetry station ID when arg 2 is a URL. |
| `stationName` | `Grimnir Radio` | Used for MediaSession metadata (lock-screen title). |
| `autoplay` | `false` | Play on construct. Most browsers block this without a user gesture. |
| `apiBase` | `''` | Prefix for `/api/v1/...` calls. Set this for cross-origin embeds. |
| `onEscalate` | (none) | Called when the player steps HQ -> LQ; arg includes attempt count & from/to streams. |
| `onStateChange` | (none) | Called on every state transition. |
| `onStreamChange` | (none) | Called when activeIndex moves (degrade, upgrade, or listener pick). |
| `onExhausted` | (none) | Called once when every URL has failed. |

Returned methods: `play()`, `pause()`, `stop()`, `retry()`, `destroy()`, `getState()`, `getReconnectCount()`, `getActiveStream()`, `getStreams()`, `setActiveIndex(i)`.

## States

The container element carries `data-grimnir-state="..."` so operators can style each state in CSS without touching JS:

- `stopped` — idle, not playing
- `playing` — audio flowing
- `reconnecting` — recycle-on-failure in progress
- `unavailable` — every URL in the list failed; retry button visible

A second attribute, `data-grimnir-reconnect-ui="progress|text"`, signals the tiered indicator:

| time since disconnect | UI |
|---|---|
| 0-500ms | silent; nothing painted |
| 500ms-3s | thin progress bar |
| 3+ attempts in 30s | "Reconnecting..." text flashed for ~1s, then back to progress bar |

If the player has more than one stream in the list, the 3+ attempts trigger also steps down to the next URL & emits a `degrade` telemetry event. A background HEAD probe runs every 60s against the higher-preference URL while degraded; on 2xx, the player upgrades back & emits `upgrade`.

## Telemetry

Every player POSTs anonymous events to `/api/v1/listener-events`. Event types: `play`, `stop`, `reconnect` (with `duration_ms`), `degrade`, `upgrade`, `exhausted`. The handler logs the request IP for rate-limiting (10/min/IP) but never stores it. Opt out with `data-grimnir-no-telemetry="1"` on the container.

The module prefers `navigator.sendBeacon` so events still flush on page unload. It falls back to `fetch` with `keepalive: true`.

## Why recycle the `<audio>` element

When a stream connection drops mid-play (proxy reset, ICY header truncated, encoder restart), HTML5 `<audio>` latches onto the failed connection & won't retry on its own. The only reliable browser-portable recovery is to remove the element, build a fresh one pointing at the same URL, & call `play()`. That's what `onFailure` -> `recycleToActive` does. See the listener handlers near the top of `player.js` for the full event list (`error`, `stalled`, `waiting`, `ended`).

## Manual testing

1. Run the control plane locally with a station that has at least one StationStream row.
2. Open `player.test.html` & edit the `stationId` constant near the top.
3. Click Play. Watch the network tab; you'll see the HEAD probe fire every 60s once you've stepped down.
4. Kill the upstream encoder to trigger reconnects; the tiered UI takes 500ms to show, then escalates.

## Browser support

ES modules, MediaSession, fetch, sendBeacon — every shipping browser since 2021. No transpile needed. The module uses no top-level await, no dynamic imports, no decorators.

## Related endpoints

- `GET /api/v1/stations/<id>/streams` — returns the ordered stream list.
- `POST /api/v1/listener-events` — accepts the anonymous telemetry payloads.

Both are public (no auth) so the browser can hit them directly.
