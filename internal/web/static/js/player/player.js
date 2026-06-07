// player.js — Grimnir Radio listener player (Chunk 2: base reconnect path)
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
// attempts pile up inside that window, we call the `onEscalate` hook so the
// caller can degrade to a lower-quality URL (wired in Chunk 3).
//
// Public surface
// --------------
//   const player = createPlayer(containerEl, streamUrl, {
//     onEscalate: () => { /* multi-URL degrade — Chunk 3 */ },
//     onStateChange: (state) => { /* "playing" | "reconnecting" | "stopped" */ },
//     autoplay: true,
//   });
//   player.play();      // user-gesture entry point (browsers require it)
//   player.pause();
//   player.stop();      // tears down completely
//   player.getState();  // current state string
//   player.getReconnectCount();  // size of current sliding window

const RECONNECT_WINDOW_MS = 30_000;
const RECONNECT_ESCALATE_AT = 3;

// Events that mean "the stream just died or stalled hard enough to need a
// fresh socket." `waiting` fires for normal buffering too, so we debounce it
// before treating it as a failure (see WAITING_GRACE_MS).
const FAILURE_EVENTS = ['error', 'stalled', 'ended'];
const WAITING_EVENT = 'waiting';
const WAITING_GRACE_MS = 1_500;

export function createPlayer(containerEl, streamUrl, options = {}) {
  if (!containerEl) {
    throw new Error('createPlayer: containerEl is required');
  }
  if (!streamUrl) {
    throw new Error('createPlayer: streamUrl is required');
  }

  const onEscalate = typeof options.onEscalate === 'function' ? options.onEscalate : null;
  const onStateChange = typeof options.onStateChange === 'function' ? options.onStateChange : null;
  const autoplay = options.autoplay === true;

  let audio = null;           // current <audio> element
  let waitingTimer = null;    // pending "waiting → treat as failure" timer
  let reconnectAttempts = []; // timestamps (ms since epoch) of recent reconnects
  let state = 'stopped';      // "stopped" | "playing" | "reconnecting"
  let destroyed = false;
  let userPaused = false;     // true if user explicitly hit pause

  function setState(next) {
    if (state === next) return;
    state = next;
    if (onStateChange) {
      try { onStateChange(state); } catch (e) { console.error('onStateChange threw', e); }
    }
  }

  function clearWaitingTimer() {
    if (waitingTimer !== null) {
      clearTimeout(waitingTimer);
      waitingTimer = null;
    }
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
    const el = document.createElement('audio');
    el.preload = 'none';
    el.crossOrigin = 'anonymous';
    // Don't set `controls` — the container page owns the UI. Listeners get a
    // styled button + label from Chunk 4; the bare <audio> is invisible.
    el.src = streamUrl;

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
    // prune anything outside the 30s sliding window
    const cutoff = now - RECONNECT_WINDOW_MS;
    reconnectAttempts = reconnectAttempts.filter((t) => t >= cutoff);
    return reconnectAttempts.length;
  }

  function onWaiting() {
    // `waiting` fires on every normal rebuffer. Give it a grace period — if
    // playback resumes inside the window, the `playing` handler cancels this.
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
    // Browser-initiated pauses (mid-stream stall on some mobile builds) look
    // identical to user pauses at the event level. We trust `userPaused` to
    // tell them apart — set by the public pause() method.
    if (userPaused) {
      setState('stopped');
    }
  }

  function onFailure(evt) {
    if (destroyed || userPaused) return;

    setState('reconnecting');

    const count = recordReconnectAttempt();
    console.info(
      `grimnir-player: reconnect (${count}/${RECONNECT_ESCALATE_AT} in ${RECONNECT_WINDOW_MS / 1000}s) trigger=${evt && evt.type}`,
    );

    if (count >= RECONNECT_ESCALATE_AT) {
      // The escalation hook owns the next move (Chunk 3: pick a fallback URL
      // & build a fresh player). We still recycle the current element so the
      // caller has a clean slate if it wants to retry the same URL.
      if (onEscalate) {
        try {
          onEscalate({
            attempts: count,
            windowMs: RECONNECT_WINDOW_MS,
            currentUrl: streamUrl,
          });
        } catch (e) {
          console.error('grimnir-player: onEscalate threw', e);
        }
      }
      // reset the window so we don't keep escalating on every subsequent event
      reconnectAttempts = [];
    }

    // Recycle: detach the failed element & build a fresh one. This is the
    // load-bearing line of the whole module.
    detachCurrentAudio();
    audio = buildAudio();
    const playPromise = audio.play();
    if (playPromise && typeof playPromise.catch === 'function') {
      playPromise.catch((err) => {
        // Autoplay can be blocked even after the user already pressed play —
        // mobile Safari especially. We surface it as another failure so the
        // sliding-window logic can decide whether to escalate.
        console.warn('grimnir-player: post-recycle play() rejected', err);
      });
    }
  }

  // Public methods --------------------------------------------------------

  function play() {
    if (destroyed) return Promise.reject(new Error('player destroyed'));
    userPaused = false;
    if (!audio) {
      audio = buildAudio();
    }
    const p = audio.play();
    // Browsers return a Promise from play() since ~2017; old paths returned
    // undefined. Normalize so callers can always `.catch`.
    return p && typeof p.then === 'function' ? p : Promise.resolve();
  }

  function pause() {
    userPaused = true;
    if (audio) audio.pause();
    setState('stopped');
  }

  function stop() {
    userPaused = true;
    detachCurrentAudio();
    setState('stopped');
  }

  function destroy() {
    destroyed = true;
    detachCurrentAudio();
    reconnectAttempts = [];
  }

  function getState() { return state; }
  function getReconnectCount() {
    const cutoff = Date.now() - RECONNECT_WINDOW_MS;
    return reconnectAttempts.filter((t) => t >= cutoff).length;
  }

  // Optional autoplay path. Browsers will reject this without a user gesture;
  // the caller is responsible for retrying via play() after a click.
  if (autoplay) {
    audio = buildAudio();
    const p = audio.play();
    if (p && typeof p.catch === 'function') {
      p.catch((err) => console.info('grimnir-player: autoplay blocked', err && err.name));
    }
  }

  return {
    play,
    pause,
    stop,
    destroy,
    getState,
    getReconnectCount,
  };
}
