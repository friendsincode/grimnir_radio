/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

// Exercises the global player's live-stream recovery against the real app.js.
// The behaviour under test is what keeps a listener on air through a drop, and
// none of it is reachable from Go tests.

import test from 'node:test';
import assert from 'node:assert/strict';
import { advance, goLive, withPlayer } from './harness.mjs';

// ---------------------------------------------------------------------------
// intent
// ---------------------------------------------------------------------------

test('a deliberate pause stops every automatic recovery path', async () => {
    await withPlayer(async ({ player }) => {
        goLive(player);
        player.startLiveWatchdog();

        player.audio.paused = false;
        player.togglePlayPause(); // user pauses
        assert.equal(player.wantsLivePlayback, false, 'pause must clear listener intent');

        const before = player.audio.playCalls;
        player.onError({});
        player.onEnded();
        player.onStalled();
        player.onSuspend();
        player.checkLiveProgress();
        await advance(60000);

        assert.equal(player.audio.playCalls, before, 'a paused player must never reconnect itself');
    });
});

test('explicit play restores intent', async () => {
    await withPlayer(async ({ player }) => {
        goLive(player);
        player.togglePlayPause();
        assert.equal(player.wantsLivePlayback, false);
        player.audio.paused = true;
        player.togglePlayPause();
        assert.equal(player.wantsLivePlayback, true, 'pressing play must re-arm recovery');
    });
});

// ---------------------------------------------------------------------------
// the events that already existed
// ---------------------------------------------------------------------------

test('an error on a live stream reconnects', async () => {
    await withPlayer(async ({ player }) => {
        goLive(player);
        player.onError({});
        await advance(1000);
        assert.ok(player.audio.playCalls > 0, 'error must trigger a reconnect');
    });
});

test('a live stream reporting ended reconnects instead of stopping', async () => {
    await withPlayer(async ({ player }) => {
        goLive(player);
        player.onEnded();
        await advance(1000);
        assert.ok(player.audio.playCalls > 0, 'ended on a live stream means the source dropped');
    });
});

test('a brief stall that recovers on its own is left alone', async () => {
    await withPlayer(async ({ player }) => {
        goLive(player);
        player.onStalled();
        // Recovers well inside the 8s stall window.
        await advance(2000);
        player.onPlaying();
        await advance(20000);
        assert.equal(player.audio.playCalls, 0, 'an ordinary rebuffer must not churn the connection');
    });
});

test('a stall that does not recover reconnects', async () => {
    await withPlayer(async ({ player }) => {
        goLive(player);
        player.audio.readyState = 1; // still not ready when the timer fires
        player.onStalled();
        await advance(9000);
        assert.ok(player.audio.playCalls > 0, 'a stall past the window must reconnect');
    });
});

// ---------------------------------------------------------------------------
// progress watchdog
// ---------------------------------------------------------------------------

test('a frozen currentTime reconnects even with no media event', async () => {
    await withPlayer(async ({ player }) => {
        goLive(player);
        player.startLiveWatchdog();
        // The element claims to be playing and fires nothing at all, but the
        // clock never moves. This is silence that looks healthy. Needs to reach
        // a watchdog tick beyond the 12s no-progress threshold.
        await advance(20000);
        assert.ok(player.audio.playCalls > 0, 'frozen playback must be detected by the watchdog');
    });
});

test('advancing playback is never interrupted', async () => {
    await withPlayer(async ({ player }) => {
        goLive(player);
        player.startLiveWatchdog();
        for (let i = 0; i < 12; i++) {
            await advance(5000);
            player.audio.currentTime += 5;
        }
        assert.equal(player.audio.playCalls, 0, 'healthy playback must be left alone');
    });
});

test('the watchdog ignores a paused player', async () => {
    await withPlayer(async ({ player }) => {
        goLive(player);
        player.startLiveWatchdog();
        player.audio.paused = true;
        await advance(60000);
        assert.equal(player.audio.playCalls, 0);
    });
});

// ---------------------------------------------------------------------------
// tab wake and network
// ---------------------------------------------------------------------------

test('a tab returning to the foreground with dead audio reconnects', async () => {
    await withPlayer(async ({ player, document }) => {
        goLive(player);
        player.audio.paused = true; // socket died while hidden
        document.visibilityState = 'visible';
        document.dispatchEvent('visibilitychange');
        await advance(1000);
        assert.ok(player.audio.playCalls > 0, 'a woken tab must re-check the stream');
    });
});

test('the network coming back triggers an immediate retry', async () => {
    await withPlayer(async ({ player, window }) => {
        goLive(player);
        player.audio.paused = true;
        window.dispatchEvent('online');
        await advance(1000);
        assert.ok(player.audio.playCalls > 0, 'online must not wait out the backoff');
    });
});

// ---------------------------------------------------------------------------
// persistence
// ---------------------------------------------------------------------------

test('reconnection keeps trying well past the old 20-attempt ceiling', async () => {
    await withPlayer(async ({ player }) => {
        goLive(player);
        player.audio.playResult = () => Promise.reject(new Error('refused'));

        player.onError({});
        for (let i = 0; i < 60; i++) await advance(20000);

        assert.ok(
            player.reconnectAttempts > 20,
            `expected to still be retrying past 20 attempts, got ${player.reconnectAttempts}`,
        );
        assert.ok(player.audio.playCalls > 20, `expected >20 play attempts, got ${player.audio.playCalls}`);
    });
});

test('backoff is capped so retries never stop and never spin', async () => {
    await withPlayer(async ({ player }) => {
        goLive(player);
        player.audio.playResult = () => Promise.reject(new Error('refused'));
        player.onError({});
        for (let i = 0; i < 40; i++) await advance(20000);

        const calls = player.audio.playCalls;
        await advance(16000); // one capped interval
        assert.ok(player.audio.playCalls > calls, 'retries must continue at the capped interval');
        assert.ok(player.audio.playCalls - calls <= 3, 'capped backoff must not busy-loop');
    });
});

// ---------------------------------------------------------------------------
// teardown
// ---------------------------------------------------------------------------

test('closing the player stops the watchdog and every pending retry', async () => {
    await withPlayer(async ({ player }) => {
        goLive(player);
        player.startLiveWatchdog();
        player.onError({});

        player.close();
        const before = player.audio.playCalls;
        await advance(120000);

        assert.equal(player.audio.playCalls, before, 'a closed player must not resurrect itself');
        assert.equal(player.liveWatchdogTimer, null, 'watchdog interval must be cleared');
        assert.equal(player.wantsLivePlayback, false);
    });
});

test('non-live media is untouched by any of it', async () => {
    await withPlayer(async ({ player }) => {
        player.currentTrack = { url: '/archive/1/stream', title: 'Podcast', type: 'media' };
        player.isLive = false;
        player.audio.paused = false;
        player.startLiveWatchdog();

        player.onStalled();
        player.onSuspend();
        await advance(120000);

        assert.equal(player.audio.playCalls, 0, 'recovery must not fire for on-demand media');
    });
});
