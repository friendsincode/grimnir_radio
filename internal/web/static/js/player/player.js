// player.js — Grimnir Radio listener player (Chunks 2 + 3 + 4 + 5)
//
// Vanilla JS ES module. No build step. No framework.
//
// Architecture
// ------------
// Wraps an HTML5 <audio> element. On error/stalled/waiting/ended, the ONLY
// reliable way to "reconnect" an audio element is to detach it & build a fresh
// one pointing at the same URL — browsers latch onto the failed connection
// otherwise & won't retry on their own. We do exactly that.
//
// Failure detection uses a 30-second sliding window. If more than 3 reconnect
// attempts pile up inside that window, the player auto-degrades to the next
// stream in the ordered list (HQ -> LQ -> ...). The list is fetched at
// construct time from `GET /api/v1/stations/<id>/streams`.
//
// Recovery: while degraded, a background HEAD probe runs against the next
// higher-preference URL every 60s. On 2xx, the player switches back via the
// same recycle path.
//
// Exhaustion: when every URL in the list has failed in turn, the player paints
// a "Stream unavailable" panel inside the container & fires `onExhausted`.
//
// UI (Chunk 4)
// ------------
// The module renders minimal HTML inside `containerEl`:
//   - play/pause button         [data-grimnir-role="toggle"]
//   - status text region        [data-grimnir-role="status"]
//   - thin progress bar         [data-grimnir-role="progress"]
//   - quality label             [data-grimnir-role="label"]
//   - quality <select>          [data-grimnir-role="quality"]
// The container element carries [data-grimnir-state="..."] so operator CSS can
// target every state. Reconnect UI is tiered:
//   - <500ms          silent
//   - 500ms..3s       progress bar visible
//   - 3+ attempts     "Reconnecting..." text for ~1s, then resume silently
//
// Telemetry (Chunk 5)
// -------------------
// Anonymous events POST to `/api/v1/listener-events` for: play, stop,
// reconnect (with duration_ms), degrade, upgrade, exhausted. Uses sendBeacon
// when available; falls back to fetch keepalive. Opt out by setting
// `data-grimnir-no-telemetry="1"` on the container element.
//
// Public surface
// --------------
//   const player = createPlayer(containerEl, stationIdOrUrl, {
//     streams: [{url, label, priority, ...}, ...], // optional pre-supply
//     stationId:   string,    // for telemetry when arg 2 is a URL
//     stationName: string,    // for MediaSession metadata
//     onEscalate, onStateChange, onStreamChange, onExhausted,
//     autoplay: false,
//     apiBase: '',
//   });
//   player.play() / pause() / stop() / retry() / destroy();
//   player.getState() / getReconnectCount() / getActiveStream() / getStreams();
//   player.setActiveIndex(i);    // listener picks a quality (auto-pin)

const RECONNECT_WINDOW_MS = 30_000;
const RECONNECT_ESCALATE_AT = 3;
const WAITING_GRACE_MS = 1_500;
const UPGRADE_PROBE_INTERVAL_MS = 60_000;

// UI thresholds for the tiered reconnect indicator (Chunk 4).
const RECONNECT_UI_SILENT_MS = 500;
const RECONNECT_TEXT_FLASH_MS = 1_000;

const TELEMETRY_PATH = '/api/v1/listener-events';

function looksLikeUrl(s) {
  return typeof s === 'string' && /^https?:\/\//i.test(s);
}

export function createPlayer(containerEl, stationIdOrUrl, options = {}) {
  if (!containerEl) {
    throw new Error('createPlayer: containerEl is required');
  }
  if (!stationIdOrUrl) {
    throw new Error('createPlayer: stationId (or url) is required');
  }

  const onEscalate = typeof options.onEscalate === 'function' ? options.onEscalate : null;
  const onStateChange = typeof options.onStateChange === 'function' ? options.onStateChange : null;
  const onStreamChange = typeof options.onStreamChange === 'function' ? options.onStreamChange : null;
  const onExhausted = typeof options.onExhausted === 'function' ? options.onExhausted : null;
  const autoplay = options.autoplay === true;
  const apiBase = typeof options.apiBase === 'string' ? options.apiBase : '';
  const stationName = typeof options.stationName === 'string' ? options.stationName : 'Grimnir Radio';

  // Station ID for telemetry. When arg 2 is a URL we have no station; caller
  // can still provide one via options.stationId so the events POST.
  const telemetryStationId = looksLikeUrl(stationIdOrUrl)
    ? (typeof options.stationId === 'string' ? options.stationId : '')
    : stationIdOrUrl;

  // Telemetry opt-out: honors data-grimnir-no-telemetry="1" on the container.
  const telemetryDisabled =
    containerEl.getAttribute && containerEl.getAttribute('data-grimnir-no-telemetry') === '1';

  let streams = [];
  let activeIndex = 0;
  let streamsLoaded = false;
  let streamsLoadPromise = null;
  // pinnedIndex disables auto-degrade/upgrade when set (listener picked a
  // specific quality via the dropdown).
  let pinnedIndex = null;

  if (Array.isArray(options.streams) && options.streams.length > 0) {
    streams = options.streams.slice();
    streamsLoaded = true;
  } else if (looksLikeUrl(stationIdOrUrl)) {
    streams = [{ url: stationIdOrUrl, label: 'stream', priority: 1 }];
    streamsLoaded = true;
  } else {
    streamsLoadPromise = fetchStreams(apiBase, stationIdOrUrl).then((list) => {
      if (destroyed) return;
      streams = list;
      streamsLoaded = true;
      activeIndex = 0;
      renderQualityOptions();
      updateUI();
    }).catch((err) => {
      console.error('grimnir-player: stream list fetch failed', err);
    });
  }

  let audio = null;
  let waitingTimer = null;
  let reconnectAttempts = [];
  let state = 'stopped';
  let destroyed = false;
  let userPaused = false;
  let upgradeTimer = null;

  // Reconnect-UI tracking (Chunk 4).
  let disconnectStartedAt = 0;     // ms timestamp when the latest reconnect cycle began
  let reconnectUiTimer = null;     // setTimeout handle for the progress-bar reveal
  let reconnectTextTimer = null;   // setTimeout handle for clearing the "Reconnecting..." flash

  function setState(next) {
    if (state === next) return;
    state = next;
    containerEl.setAttribute('data-grimnir-state', state);
    if (onStateChange) {
      try { onStateChange(state); } catch (e) { console.error('onStateChange threw', e); }
    }
    updateUI();
  }

  function activeStream() {
    if (!streamsLoaded || streams.length === 0) return null;
    return streams[activeIndex] || null;
  }

  function activeUrl() {
    const s = activeStream();
    return s ? s.url : null;
  }

  function fireStreamChange() {
    updateUI();
    if (!onStreamChange) return;
    try { onStreamChange(activeStream()); } catch (e) { console.error('onStreamChange threw', e); }
  }

  function clearWaitingTimer() {
    if (waitingTimer !== null) { clearTimeout(waitingTimer); waitingTimer = null; }
  }
  function clearReconnectUiTimers() {
    if (reconnectUiTimer !== null) { clearTimeout(reconnectUiTimer); reconnectUiTimer = null; }
    if (reconnectTextTimer !== null) { clearTimeout(reconnectTextTimer); reconnectTextTimer = null; }
  }
  function stopUpgradeProbe() {
    if (upgradeTimer !== null) { clearInterval(upgradeTimer); upgradeTimer = null; }
  }
  function startUpgradeProbe() {
    stopUpgradeProbe();
    if (activeIndex <= 0 || pinnedIndex !== null) return;
    upgradeTimer = setInterval(probeForUpgrade, UPGRADE_PROBE_INTERVAL_MS);
  }

  function detachCurrentAudio() {
    if (!audio) return;
    clearWaitingTimer();
    try {
      audio.removeEventListener('error', onFailure);
      audio.removeEventListener('stalled', onFailure);
      audio.removeEventListener('ended', onFailure);
      audio.removeEventListener('waiting', onWaiting);
      audio.removeEventListener('playing', onPlaying);
      audio.removeEventListener('pause', onPause);
      audio.pause();
      audio.removeAttribute('src');
      audio.load();
    } catch (e) {
      console.warn('grimnir-player: detach failed', e);
    }
    if (audio.parentNode === containerEl) {
      containerEl.removeChild(audio);
    }
    audio = null;
  }

  function buildAudio() {
    const url = activeUrl();
    if (!url) return null;
    const el = document.createElement('audio');
    el.preload = 'none';
    el.crossOrigin = 'anonymous';
    el.src = url;
    el.addEventListener('error', onFailure);
    el.addEventListener('stalled', onFailure);
    el.addEventListener('ended', onFailure);
    el.addEventListener('waiting', onWaiting);
    el.addEventListener('playing', onPlaying);
    el.addEventListener('pause', onPause);
    containerEl.appendChild(el);
    return el;
  }

  function recordReconnectAttempt() {
    const now = Date.now();
    reconnectAttempts.push(now);
    const cutoff = now - RECONNECT_WINDOW_MS;
    reconnectAttempts = reconnectAttempts.filter((t) => t >= cutoff);
    return reconnectAttempts.length;
  }

  function onWaiting() {
    clearWaitingTimer();
    waitingTimer = setTimeout(() => {
      waitingTimer = null;
      onFailure({ type: 'waiting-timeout' });
    }, WAITING_GRACE_MS);
  }

  function onPlaying() {
    clearWaitingTimer();
    // Reconnect succeeded — emit a reconnect telemetry event with duration if we
    // were previously in the reconnecting state. duration_ms = time from
    // disconnect to recovery.
    if (state === 'reconnecting' && disconnectStartedAt > 0) {
      const duration = Date.now() - disconnectStartedAt;
      sendTelemetry('reconnect', duration);
    }
    disconnectStartedAt = 0;
    clearReconnectUiTimers();
    setState('playing');
  }

  function onPause() {
    if (userPaused) setState('stopped');
  }

  function degradeOrExhaust() {
    if (activeIndex + 1 >= streams.length) return false;
    activeIndex += 1;
    reconnectAttempts = [];
    fireStreamChange();
    return true;
  }

  function enterUnavailable() {
    detachCurrentAudio();
    stopUpgradeProbe();
    clearReconnectUiTimers();
    setState('unavailable');
    sendTelemetry('exhausted');
    if (onExhausted) {
      try { onExhausted(); } catch (e) { console.error('onExhausted threw', e); }
    }
  }

  function recycleToActive() {
    detachCurrentAudio();
    audio = buildAudio();
    if (!audio) return;
    const p = audio.play();
    if (p && typeof p.catch === 'function') {
      p.catch((err) => console.warn('grimnir-player: post-recycle play() rejected', err));
    }
  }

  function onFailure(evt) {
    if (destroyed || userPaused) return;

    if (!streamsLoaded) {
      beginReconnectUi();
      setState('reconnecting');
      return;
    }

    beginReconnectUi();
    setState('reconnecting');

    const count = recordReconnectAttempt();
    const cur = activeStream();
    console.info(
      `grimnir-player: reconnect (${count}/${RECONNECT_ESCALATE_AT} in ${RECONNECT_WINDOW_MS / 1000}s) trigger=${evt && evt.type} stream=${cur && cur.label}`,
    );

    if (count >= RECONNECT_ESCALATE_AT) {
      // Tier 3 UI: flash "Reconnecting..." text for ~1s, then proceed silently.
      flashReconnectingText();

      // Pinned listeners stay on their chosen URL even after 3 failures — we
      // just keep cycling that URL and surfacing the reconnect text.
      if (pinnedIndex === null) {
        const advanced = degradeOrExhaust();
        if (!advanced) {
          enterUnavailable();
          return;
        }
        sendTelemetry('degrade');
        if (onEscalate) {
          try {
            onEscalate({
              attempts: count, windowMs: RECONNECT_WINDOW_MS,
              from: cur, to: activeStream(),
            });
          } catch (e) { console.error('grimnir-player: onEscalate threw', e); }
        }
        startUpgradeProbe();
      }
    }
    recycleToActive();
  }

  async function probeForUpgrade() {
    if (destroyed || userPaused) return;
    if (pinnedIndex !== null) { stopUpgradeProbe(); return; }
    if (activeIndex <= 0) { stopUpgradeProbe(); return; }
    const target = streams[activeIndex - 1];
    if (!target) { stopUpgradeProbe(); return; }
    try {
      const resp = await fetch(target.url, { method: 'HEAD', cache: 'no-store' });
      if (!resp.ok) return;
    } catch (e) { return; }
    activeIndex -= 1;
    reconnectAttempts = [];
    fireStreamChange();
    if (activeIndex <= 0) stopUpgradeProbe();
    console.info(`grimnir-player: upgrade -> ${activeStream() && activeStream().label}`);
    sendTelemetry('upgrade');
    recycleToActive();
  }

  // --- Reconnect UI tiers (Chunk 4) -------------------------------------

  function beginReconnectUi() {
    if (disconnectStartedAt === 0) {
      disconnectStartedAt = Date.now();
    }
    clearReconnectUiTimers();
    // Tier 1: silent for the first 500ms. We do not touch the DOM beyond the
    // [data-grimnir-state="reconnecting"] attribute that setState() will set.
    // Tier 2: at 500ms, reveal the progress bar.
    reconnectUiTimer = setTimeout(() => {
      reconnectUiTimer = null;
      if (state === 'reconnecting') {
        containerEl.setAttribute('data-grimnir-reconnect-ui', 'progress');
        updateUI();
      }
    }, RECONNECT_UI_SILENT_MS);
  }

  function flashReconnectingText() {
    containerEl.setAttribute('data-grimnir-reconnect-ui', 'text');
    updateUI();
    if (reconnectTextTimer !== null) clearTimeout(reconnectTextTimer);
    reconnectTextTimer = setTimeout(() => {
      reconnectTextTimer = null;
      // Drop back to progress-bar mode so the rest of the cycle stays subtle.
      if (state === 'reconnecting') {
        containerEl.setAttribute('data-grimnir-reconnect-ui', 'progress');
      } else {
        containerEl.removeAttribute('data-grimnir-reconnect-ui');
      }
      updateUI();
    }, RECONNECT_TEXT_FLASH_MS);
  }

  // --- DOM scaffolding (Chunk 4) ---------------------------------------

  let uiRoot = null;
  let toggleBtn = null;
  let statusEl = null;
  let progressEl = null;
  let labelEl = null;
  let qualitySel = null;
  let retryBtn = null;

  function renderShell() {
    // Build once. The <audio> elements live as siblings of uiRoot inside
    // containerEl so detachCurrentAudio() can still remove them by parentNode.
    uiRoot = document.createElement('div');
    uiRoot.setAttribute('data-grimnir-role', 'ui-root');

    toggleBtn = document.createElement('button');
    toggleBtn.type = 'button';
    toggleBtn.setAttribute('data-grimnir-role', 'toggle');
    toggleBtn.textContent = 'Play';
    toggleBtn.addEventListener('click', () => {
      if (state === 'unavailable') { retry(); return; }
      if (state === 'playing' || state === 'reconnecting') pause();
      else play();
    });

    statusEl = document.createElement('span');
    statusEl.setAttribute('data-grimnir-role', 'status');

    progressEl = document.createElement('div');
    progressEl.setAttribute('data-grimnir-role', 'progress');
    progressEl.setAttribute('aria-hidden', 'true');

    labelEl = document.createElement('span');
    labelEl.setAttribute('data-grimnir-role', 'label');

    qualitySel = document.createElement('select');
    qualitySel.setAttribute('data-grimnir-role', 'quality');
    qualitySel.addEventListener('change', () => {
      const v = qualitySel.value;
      if (v === 'auto') {
        pinnedIndex = null;
        return;
      }
      const i = parseInt(v, 10);
      if (Number.isFinite(i) && i >= 0 && i < streams.length) setActiveIndex(i);
    });

    retryBtn = document.createElement('button');
    retryBtn.type = 'button';
    retryBtn.setAttribute('data-grimnir-role', 'retry');
    retryBtn.textContent = 'Retry';
    retryBtn.addEventListener('click', () => retry());

    uiRoot.appendChild(toggleBtn);
    uiRoot.appendChild(labelEl);
    uiRoot.appendChild(qualitySel);
    uiRoot.appendChild(statusEl);
    uiRoot.appendChild(progressEl);
    uiRoot.appendChild(retryBtn);
    containerEl.appendChild(uiRoot);

    renderQualityOptions();
    containerEl.setAttribute('data-grimnir-state', state);
    updateUI();
  }

  function renderQualityOptions() {
    if (!qualitySel) return;
    qualitySel.innerHTML = '';
    const auto = document.createElement('option');
    auto.value = 'auto';
    auto.textContent = 'Auto';
    qualitySel.appendChild(auto);
    streams.forEach((s, i) => {
      const o = document.createElement('option');
      o.value = String(i);
      o.textContent = s.label || `Stream ${i + 1}`;
      qualitySel.appendChild(o);
    });
    qualitySel.value = pinnedIndex === null ? 'auto' : String(pinnedIndex);
  }

  function updateUI() {
    if (!uiRoot) return;
    // Toggle button text.
    if (toggleBtn) {
      if (state === 'unavailable') toggleBtn.textContent = 'Retry';
      else if (state === 'playing' || state === 'reconnecting') toggleBtn.textContent = 'Pause';
      else toggleBtn.textContent = 'Play';
    }
    // Status text.
    if (statusEl) {
      const reconnectUi = containerEl.getAttribute('data-grimnir-reconnect-ui');
      if (state === 'unavailable') {
        statusEl.textContent = 'Stream unavailable';
      } else if (state === 'reconnecting' && reconnectUi === 'text') {
        statusEl.textContent = 'Reconnecting...';
      } else {
        statusEl.textContent = '';
      }
    }
    // Active label (de-emphasized — operators style via CSS).
    if (labelEl) {
      const s = activeStream();
      labelEl.textContent = s && s.label ? s.label : '';
    }
    // Retry button only visible in the unavailable state.
    if (retryBtn) {
      retryBtn.setAttribute('data-grimnir-hidden', state === 'unavailable' ? '0' : '1');
    }
    // Quality select reflects current selection.
    if (qualitySel) {
      const desired = pinnedIndex === null ? 'auto' : String(pinnedIndex);
      if (qualitySel.value !== desired) qualitySel.value = desired;
    }
  }

  // --- MediaSession (Chunk 5.1) ----------------------------------------

  function wireMediaSession() {
    if (typeof navigator === 'undefined' || !navigator.mediaSession) return;
    try {
      if (typeof MediaMetadata !== 'undefined') {
        navigator.mediaSession.metadata = new MediaMetadata({
          title: stationName,
          artist: 'Grimnir Radio',
        });
      }
      navigator.mediaSession.setActionHandler('play', () => { play(); });
      navigator.mediaSession.setActionHandler('pause', () => { pause(); });
    } catch (e) {
      console.warn('grimnir-player: MediaSession wiring failed', e);
    }
  }

  // --- Telemetry (Chunk 5.2/5.3) ---------------------------------------

  function sendTelemetry(eventType, durationMs) {
    if (telemetryDisabled) return;
    if (!telemetryStationId) return;
    const s = activeStream();
    if (!s) return;
    const payload = {
      event_type: eventType,
      station_id: telemetryStationId,
      stream_label: s.label || 'unknown',
    };
    if (typeof durationMs === 'number' && durationMs >= 0) {
      payload.duration_ms = Math.round(durationMs);
    }
    const url = `${apiBase}${TELEMETRY_PATH}`;
    const json = JSON.stringify(payload);
    try {
      // sendBeacon is best-effort; survives page unload. It uses
      // text/plain by default, but the handler only cares about the JSON body.
      if (typeof navigator !== 'undefined' &&
          typeof navigator.sendBeacon === 'function' &&
          typeof Blob !== 'undefined') {
        const blob = new Blob([json], { type: 'application/json' });
        const ok = navigator.sendBeacon(url, blob);
        if (ok) return;
      }
      // Fallback: fetch with keepalive so the request survives a quick unload.
      fetch(url, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: json,
        keepalive: true,
      }).catch((e) => console.warn('grimnir-player: telemetry POST failed', e));
    } catch (e) {
      console.warn('grimnir-player: telemetry threw', e);
    }
  }

  // --- Public methods --------------------------------------------------

  function play() {
    if (destroyed) return Promise.reject(new Error('player destroyed'));
    userPaused = false;

    if (!streamsLoaded && streamsLoadPromise) {
      return streamsLoadPromise.then(() => {
        if (destroyed || userPaused) return;
        if (streams.length === 0) { enterUnavailable(); return; }
        sendTelemetry('play');
        if (!audio) audio = buildAudio();
        if (!audio) return;
        const p = audio.play();
        return p && typeof p.then === 'function' ? p : undefined;
      });
    }

    if (streams.length === 0) { enterUnavailable(); return Promise.resolve(); }
    sendTelemetry('play');
    if (!audio) audio = buildAudio();
    if (!audio) return Promise.resolve();
    const p = audio.play();
    return p && typeof p.then === 'function' ? p : Promise.resolve();
  }

  function pause() {
    userPaused = true;
    if (audio) audio.pause();
    stopUpgradeProbe();
    clearReconnectUiTimers();
    containerEl.removeAttribute('data-grimnir-reconnect-ui');
    setState('stopped');
    sendTelemetry('stop');
  }

  function stop() {
    userPaused = true;
    detachCurrentAudio();
    stopUpgradeProbe();
    clearReconnectUiTimers();
    containerEl.removeAttribute('data-grimnir-reconnect-ui');
    setState('stopped');
    sendTelemetry('stop');
  }

  function destroy() {
    destroyed = true;
    detachCurrentAudio();
    stopUpgradeProbe();
    clearReconnectUiTimers();
    reconnectAttempts = [];
    if (uiRoot && uiRoot.parentNode === containerEl) {
      containerEl.removeChild(uiRoot);
    }
    uiRoot = null;
  }

  function retry() {
    if (destroyed) return Promise.reject(new Error('player destroyed'));
    activeIndex = pinnedIndex !== null ? pinnedIndex : 0;
    reconnectAttempts = [];
    userPaused = false;
    fireStreamChange();
    setState('stopped');
    return play();
  }

  // setActiveIndex pins the player to a specific stream (listener choice via
  // the quality dropdown). Disables auto-degrade/upgrade until reset to auto.
  function setActiveIndex(i) {
    if (i < 0 || i >= streams.length) return;
    pinnedIndex = i;
    activeIndex = i;
    reconnectAttempts = [];
    stopUpgradeProbe();
    fireStreamChange();
    if (state === 'playing' || state === 'reconnecting') {
      recycleToActive();
    }
  }

  function getState() { return state; }
  function getReconnectCount() {
    const cutoff = Date.now() - RECONNECT_WINDOW_MS;
    return reconnectAttempts.filter((t) => t >= cutoff).length;
  }
  function getActiveStream() { return activeStream(); }
  function getStreams() { return streams.slice(); }

  // --- Init ------------------------------------------------------------

  renderShell();
  wireMediaSession();

  if (autoplay) {
    if (streamsLoaded) {
      if (streams.length > 0) {
        sendTelemetry('play');
        audio = buildAudio();
        if (audio) {
          const p = audio.play();
          if (p && typeof p.catch === 'function') {
            p.catch((err) => console.info('grimnir-player: autoplay blocked', err && err.name));
          }
        }
      }
    } else if (streamsLoadPromise) {
      streamsLoadPromise.then(() => {
        if (destroyed || userPaused || streams.length === 0) return;
        sendTelemetry('play');
        audio = buildAudio();
        if (!audio) return;
        const p = audio.play();
        if (p && typeof p.catch === 'function') {
          p.catch((err) => console.info('grimnir-player: autoplay blocked', err && err.name));
        }
      });
    }
  }

  return {
    play, pause, stop, destroy, retry,
    getState, getReconnectCount, getActiveStream, getStreams,
    setActiveIndex,
  };
}

async function fetchStreams(apiBase, stationId) {
  const url = `${apiBase}/api/v1/stations/${encodeURIComponent(stationId)}/streams`;
  const resp = await fetch(url, { cache: 'no-store' });
  if (!resp.ok) throw new Error(`streams fetch ${resp.status}`);
  const body = await resp.json();
  if (!body || !Array.isArray(body.streams)) throw new Error('streams response shape unexpected');
  return body.streams;
}
