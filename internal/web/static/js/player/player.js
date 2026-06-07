// player.js — Grimnir Radio listener player (Chunks 2 + 3)
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
// Exhaustion: when every URL in the list has failed in turn, the player calls
// `onExhausted` (the test harness paints a "temporarily unavailable" panel
// with a Retry button — Chunk 4 builds the prod UI on the same hook).
//
// Public surface
// --------------
//   const player = createPlayer(containerEl, stationIdOrUrl, {
//     // Pre-supplied list short-circuits the fetch (useful in tests):
//     streams: [{url, label, priority, ...}, ...],
//     onEscalate:    (info)   => { /* informational; fires AFTER auto-degrade */ },
//     onStateChange: (state)  => { /* "playing" | "reconnecting" | "stopped" | "unavailable" */ },
//     onStreamChange:(stream) => { /* fires when the active URL changes */ },
//     onExhausted:   ()       => { /* every URL failed; prompt the user */ },
//     autoplay: true,
//   });
//   player.play();              // user-gesture entry point
//   player.pause();
//   player.stop();              // tears down completely
//   player.retry();             // restart from priority 1 after exhaustion
//   player.getState();
//   player.getReconnectCount();
//   player.getActiveStream();   // current {url, label, ...} or null

const RECONNECT_WINDOW_MS = 30_000;
const RECONNECT_ESCALATE_AT = 3;

// Events that mean "the stream just died or stalled hard enough to need a
// fresh socket." `waiting` fires for normal buffering too, so we debounce it
// before treating it as a failure (see WAITING_GRACE_MS).
const WAITING_GRACE_MS = 1_500;

// Background HEAD probe cadence while degraded.
const UPGRADE_PROBE_INTERVAL_MS = 60_000;

// Looks-like-a-URL check used to keep the legacy `createPlayer(el, url, ...)`
// signature working while the rollout swaps callers over to station IDs.
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

  // Stream list resolution. Three input modes, in priority order:
  //   1. options.streams pre-supplied (tests, server-rendered embeds)
  //   2. first arg is a URL -> single-item list (legacy compat)
  //   3. first arg is a station id -> fetch /api/v1/stations/<id>/streams
  let streams = [];
  let activeIndex = 0;
  let streamsLoaded = false;
  let streamsLoadPromise = null;

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
    }).catch((err) => {
      console.error('grimnir-player: stream list fetch failed', err);
    });
  }

  let audio = null;             // current <audio> element
  let waitingTimer = null;      // pending "waiting → treat as failure" timer
  let reconnectAttempts = [];   // timestamps (ms since epoch) of recent reconnects
  let state = 'stopped';        // "stopped" | "playing" | "reconnecting" | "unavailable"
  let destroyed = false;
  let userPaused = false;       // true if user explicitly hit pause
  let upgradeTimer = null;      // setInterval handle for the HEAD probe

  function setState(next) {
    if (state === next) return;
    state = next;
    if (onStateChange) {
      try { onStateChange(state); } catch (e) { console.error('onStateChange threw', e); }
    }
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
    if (!onStreamChange) return;
    try { onStreamChange(activeStream()); } catch (e) { console.error('onStreamChange threw', e); }
  }

  function clearWaitingTimer() {
    if (waitingTimer !== null) {
      clearTimeout(waitingTimer);
      waitingTimer = null;
    }
  }

  function stopUpgradeProbe() {
    if (upgradeTimer !== null) {
      clearInterval(upgradeTimer);
      upgradeTimer = null;
    }
  }

  function startUpgradeProbe() {
    stopUpgradeProbe();
    // Nothing to upgrade to if we're already on priority 1.
    if (activeIndex <= 0) return;
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
      audio.load(); // forces the element to drop its current network resource
    } catch (e) {
      // best-effort teardown; never let it block the reconnect
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
    setState('playing');
  }

  function onPause() {
    if (userPaused) {
      setState('stopped');
    }
  }

  // degradeOrExhaust steps activeIndex to the next URL. Returns true if it
  // advanced, false if there's nothing left (exhausted).
  function degradeOrExhaust() {
    if (activeIndex + 1 >= streams.length) {
      return false;
    }
    activeIndex += 1;
    reconnectAttempts = [];
    fireStreamChange();
    return true;
  }

  function enterUnavailable() {
    detachCurrentAudio();
    stopUpgradeProbe();
    setState('unavailable');
    if (onExhausted) {
      try { onExhausted(); } catch (e) { console.error('onExhausted threw', e); }
    }
  }

  // Switch to whatever activeIndex currently points at. Used by both the
  // degrade path (after onFailure escalation) and the upgrade path (after a
  // successful HEAD probe).
  function recycleToActive() {
    detachCurrentAudio();
    audio = buildAudio();
    if (!audio) return;
    const playPromise = audio.play();
    if (playPromise && typeof playPromise.catch === 'function') {
      playPromise.catch((err) => {
        console.warn('grimnir-player: post-recycle play() rejected', err);
      });
    }
  }

  function onFailure(evt) {
    if (destroyed || userPaused) return;

    // Streams list still loading -> can't reconnect intelligently yet. Mark
    // reconnecting; play() will retry once the list arrives.
    if (!streamsLoaded) {
      setState('reconnecting');
      return;
    }

    setState('reconnecting');

    const count = recordReconnectAttempt();
    const cur = activeStream();
    console.info(
      `grimnir-player: reconnect (${count}/${RECONNECT_ESCALATE_AT} in ${RECONNECT_WINDOW_MS / 1000}s) trigger=${evt && evt.type} stream=${cur && cur.label}`,
    );

    if (count >= RECONNECT_ESCALATE_AT) {
      const advanced = degradeOrExhaust();
      if (!advanced) {
        enterUnavailable();
        return;
      }
      // Fire the informational hook AFTER the auto-degrade has picked the next
      // URL. Old semantics (caller decides where to go) are gone; the player
      // owns the decision now.
      if (onEscalate) {
        try {
          onEscalate({
            attempts: count,
            windowMs: RECONNECT_WINDOW_MS,
            from: cur,
            to: activeStream(),
          });
        } catch (e) {
          console.error('grimnir-player: onEscalate threw', e);
        }
      }
      // We just degraded; kick off the upgrade probe so HQ can come back.
      startUpgradeProbe();
    }

    recycleToActive();
  }

  // probeForUpgrade fires a HEAD against the next-higher-priority URL. On 2xx,
  // we step activeIndex back up & recycle. We probe one step at a time so a
  // transient HQ recovery doesn't yank a listener off a stable LQ.
  async function probeForUpgrade() {
    if (destroyed || userPaused) return;
    if (activeIndex <= 0) {
      stopUpgradeProbe();
      return;
    }
    const target = streams[activeIndex - 1];
    if (!target) {
      stopUpgradeProbe();
      return;
    }
    try {
      const resp = await fetch(target.url, { method: 'HEAD', cache: 'no-store' });
      if (!resp.ok) return;
    } catch (e) {
      return; // network error -> still down, try again next tick
    }
    activeIndex -= 1;
    reconnectAttempts = [];
    fireStreamChange();
    if (activeIndex <= 0) stopUpgradeProbe();
    console.info(`grimnir-player: upgrade -> ${activeStream() && activeStream().label}`);
    recycleToActive();
  }

  // Public methods --------------------------------------------------------

  function play() {
    if (destroyed) return Promise.reject(new Error('player destroyed'));
    userPaused = false;

    // If the streams list is still in-flight, defer play() until it lands.
    if (!streamsLoaded && streamsLoadPromise) {
      return streamsLoadPromise.then(() => {
        if (destroyed || userPaused) return;
        if (streams.length === 0) {
          enterUnavailable();
          return;
        }
        if (!audio) audio = buildAudio();
        if (!audio) return;
        const p = audio.play();
        return p && typeof p.then === 'function' ? p : undefined;
      });
    }

    if (streams.length === 0) {
      enterUnavailable();
      return Promise.resolve();
    }
    if (!audio) audio = buildAudio();
    if (!audio) return Promise.resolve();
    const p = audio.play();
    return p && typeof p.then === 'function' ? p : Promise.resolve();
  }

  function pause() {
    userPaused = true;
    if (audio) audio.pause();
    stopUpgradeProbe();
    setState('stopped');
  }

  function stop() {
    userPaused = true;
    detachCurrentAudio();
    stopUpgradeProbe();
    setState('stopped');
  }

  function destroy() {
    destroyed = true;
    detachCurrentAudio();
    stopUpgradeProbe();
    reconnectAttempts = [];
  }

  // retry: full reset after exhaustion. Walks back to priority 1 & restarts.
  function retry() {
    if (destroyed) return Promise.reject(new Error('player destroyed'));
    activeIndex = 0;
    reconnectAttempts = [];
    userPaused = false;
    fireStreamChange();
    setState('stopped');
    return play();
  }

  function getState() { return state; }
  function getReconnectCount() {
    const cutoff = Date.now() - RECONNECT_WINDOW_MS;
    return reconnectAttempts.filter((t) => t >= cutoff).length;
  }
  function getActiveStream() { return activeStream(); }
  function getStreams() { return streams.slice(); }

  // Autoplay path. Same defer-until-loaded rule as play().
  if (autoplay) {
    if (streamsLoaded) {
      if (streams.length > 0) {
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
    play,
    pause,
    stop,
    destroy,
    retry,
    getState,
    getReconnectCount,
    getActiveStream,
    getStreams,
  };
}

// fetchStreams hits the control-plane endpoint Chunk 1 shipped. Exposed
// (un-exported) so tests can stub the network via the `streams:` option.
async function fetchStreams(apiBase, stationId) {
  const url = `${apiBase}/api/v1/stations/${encodeURIComponent(stationId)}/streams`;
  const resp = await fetch(url, { cache: 'no-store' });
  if (!resp.ok) {
    throw new Error(`streams fetch ${resp.status}`);
  }
  const body = await resp.json();
  if (!body || !Array.isArray(body.streams)) {
    throw new Error('streams response shape unexpected');
  }
  return body.streams;
}
