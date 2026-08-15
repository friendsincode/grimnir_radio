/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

// Exercises the silent half of live recovery: the standby connection that is
// built while the dying element still has buffered audio to play, so the
// listener never hears the handover. The audible fallback paths are covered in
// player_reconnect.test.mjs.

import test from 'node:test';
import assert from 'node:assert/strict';
import { advance, goLive, withPlayer } from './harness.mjs';

// Brings a standby up to the point where it is playing and ready to take over.
async function standbyReady(player) {
    player.onStalled();
    await advance(1500); // past the 1.2s prewarm delay
    assert.ok(player.standbyAudio, 'expected a standby connection to be open');
    player.standbyAudio.emit('playing');
    return player.standbyAudio;
}

// ---------------------------------------------------------------------------
// opening the standby
// ---------------------------------------------------------------------------

test('a persistent stall opens a standby connection without touching the live one', async () => {
    await withPlayer(async ({ player }) => {
        goLive(player);
        const live = player.audio;

        player.onStalled();
        await advance(1500);

        assert.ok(player.standbyAudio, 'a stall past the prewarm delay must open a standby');
        assert.notEqual(player.standbyAudio, live, 'the standby must be its own element');
        assert.equal(live.playCalls, 0, 'the live element must not be reloaded while it still has audio');
        assert.equal(player.standbyAudio.playCalls, 1, 'the standby must actually start');
    });
});

test('the standby starts muted so autoplay policy cannot refuse it', async () => {
    await withPlayer(async ({ player }) => {
        goLive(player);
        player.onStalled();
        await advance(1500);

        assert.equal(player.standbyAudio.muted, true, 'a standby that is not muted can be blocked');
        assert.equal(player.standbyAudio.volume, 0, 'a standby must be inaudible until it is promoted');
    });
});

test('a stall that clears before the prewarm delay costs no second connection', async () => {
    await withPlayer(async ({ player }) => {
        goLive(player);
        player.onStalled();
        await advance(500);
        player.onPlaying();
        await advance(5000);

        assert.equal(player.standbyAudio, null, 'a 500ms rebuffer must not open a connection to the station');
    });
});

test('a recovered stream releases the standby it no longer needs', async () => {
    await withPlayer(async ({ player }) => {
        goLive(player);
        const spare = await standbyReady(player);

        player.onPlaying(); // the original connection came back on its own

        assert.equal(player.standbyAudio, null, 'the spare must be dropped');
        assert.equal(spare.paused, true, 'the spare connection must be closed, not left counting as a listener');
        assert.equal(spare.src, '', 'the spare must release its stream URL');
    });
});

test('an unused standby is dropped before the server could count it as a listener', async () => {
    await withPlayer(async ({ player }) => {
        goLive(player);
        player.startLiveWatchdog();
        const spare = await standbyReady(player);

        // The original connection quietly keeps delivering: currentTime advances
        // and no further stall event ever fires, so nothing else would notice the
        // spare is redundant.
        for (let i = 0; i < 4; i++) {
            await advance(5000);
            player.audio.currentTime += 5;
        }

        assert.equal(player.standbyAudio, null, 'a redundant standby must not be held open');
        assert.equal(spare.paused, true, 'the redundant connection must be closed');
        // 10s of sustained delivery is what makes the broadcast server count a
        // connection (establishSeconds), and the drop happens at 8s.
        assert.ok(player.standbyIdleDropMs < 10000, 'the drop must beat the establishment threshold');
    });
});

test('WebRTC playback never opens a standby', async () => {
    await withPlayer(async ({ player }) => {
        goLive(player);
        player.useWebRTC = true;

        player.onStalled();
        await advance(5000);

        assert.equal(player.standbyAudio, null, 'there is no URL to reopen for a WebRTC session');
    });
});

// ---------------------------------------------------------------------------
// the handover
// ---------------------------------------------------------------------------

test('a ready standby waits while the live element still has audio to play', async () => {
    await withPlayer(async ({ player }) => {
        goLive(player);
        const live = player.audio;
        await standbyReady(player); // live element is still !paused, readyState 4

        assert.equal(player.audio, live, 'audio that is still playing must not be cut short');
        assert.ok(player.standbyAudio, 'the standby must be held, not discarded');
        assert.equal(player.standbyReady, true);
    });
});

test('the standby takes over the moment the live buffer runs dry', async () => {
    await withPlayer(async ({ player }) => {
        goLive(player);
        const live = player.audio;
        const spare = await standbyReady(player);

        live.readyState = 1; // buffer exhausted: this is where silence would start
        player.onWaiting();

        assert.equal(player.audio, spare, 'the standby must become the live element');
        assert.equal(live.paused, true, 'the dead connection must be closed');
        assert.equal(live.src, '', 'the dead connection must release its URL');
        assert.equal(spare.playCalls, 1, 'the standby was already playing and must not be restarted');
    });
});

test('a handover restores full volume and never advertises itself', async () => {
    await withPlayer(async ({ player }) => {
        goLive(player);
        player.audio.volume = 0.4;
        const spare = await standbyReady(player);

        player.audio.readyState = 1;
        player.onWaiting();
        await advance(500); // let the fade finish

        assert.equal(player.audio, spare);
        assert.equal(spare.muted, false, 'the promoted element must be audible');
        assert.ok(Math.abs(spare.volume - 0.4) < 0.001, `volume=${spare.volume}, want the listener's 0.4`);
        assert.equal(player.reconnectAttempts, 0, 'a silent handover must not count as a reconnect');
        assert.equal(player.isReconnecting, false);
    });
});

test('a live stream reporting ended hands over silently instead of reconnecting', async () => {
    await withPlayer(async ({ player }) => {
        goLive(player);
        const live = player.audio;
        const spare = await standbyReady(player);

        player.onEnded();

        assert.equal(player.audio, spare, 'ended must be answered by the standby, not a visible reconnect');
        assert.equal(live.playCalls, 0, 'the dead element must never be reloaded once a standby is up');
    });
});

test('an error hands over silently instead of reconnecting', async () => {
    await withPlayer(async ({ player }) => {
        goLive(player);
        const spare = await standbyReady(player);

        player.onError({});

        assert.equal(player.audio, spare);
    });
});

test('the frozen-playback watchdog prefers the standby over a visible reconnect', async () => {
    await withPlayer(async ({ player }) => {
        goLive(player);
        const live = player.audio;
        player.startLiveWatchdog();

        // currentTime never moves and no media event fires. One 5s watchdog tick
        // with no movement opens the standby; the handover follows at the 6s
        // half-timeout tick rather than the full 12s, because a silent swap is
        // cheaper than more dead air.
        await advance(7000);
        assert.ok(player.standbyAudio, 'the watchdog must open a standby before it gives up on the element');
        player.standbyAudio.emit('playing');
        const spare = player.standbyAudio;

        await advance(5000);

        assert.equal(player.audio, spare, 'the watchdog must hand over to the standby');
        assert.equal(live.playCalls, 0, 'the frozen element must not be reloaded when a standby exists');
    });
});

test('the promoted element keeps receiving the events that drive recovery', async () => {
    await withPlayer(async ({ player }) => {
        goLive(player);
        const spare = await standbyReady(player);
        player.audio.readyState = 1;
        player.onWaiting();
        assert.equal(player.audio, spare);

        // A second drop, now on the promoted element. Its own events must reach
        // the state machine or recovery would work exactly once.
        spare.paused = false;
        spare.readyState = 4;
        spare.currentTime = 20;
        player.noteLiveProgress();
        spare.emit('stalled');
        await advance(1500);

        assert.ok(player.standbyAudio, 'the promoted element must be able to open its own standby');
        assert.notEqual(player.standbyAudio, spare);
    });
});

test('a handover blocked by autoplay policy falls back to a direct reconnect', async () => {
    await withPlayer(async ({ player }) => {
        goLive(player);
        const spare = await standbyReady(player);

        player.audio.readyState = 1;
        player.onWaiting();
        assert.equal(player.audio, spare);

        // Safari's answer to unmuting without a gesture: the element pauses.
        spare.paused = true;
        await advance(1200);

        assert.equal(player.standbySwapBlocked, true, 'a blocked swap must not be retried all session');
        assert.ok(spare.playCalls > 1, 'the visible reconnect path must take over');
    });
});

// ---------------------------------------------------------------------------
// what the listener sees
// ---------------------------------------------------------------------------

// The second line is written into a child span the element stub discards, so
// read the calls rather than the DOM.
function watchSecondaryText(player) {
    const seen = [];
    const real = player.setSecondaryText.bind(player);
    player.setSecondaryText = (text) => {
        seen.push(String(text ?? ''));
        real(text);
    };
    return seen;
}

test('a short rebuffer never says Buffering', async () => {
    await withPlayer(async ({ player }) => {
        goLive(player);
        const seen = watchSecondaryText(player);

        player.audio.readyState = 1;
        player.onWaiting();
        await advance(750);
        player.audio.readyState = 4;
        player.onPlaying();
        await advance(5000);

        assert.ok(
            !seen.some((t) => t.includes('Buffering')),
            `a 750ms rebuffer must stay off the display, saw ${JSON.stringify(seen)}`,
        );
    });
});

test('a rebuffer long enough to hear does say Buffering', async () => {
    await withPlayer(async ({ player }) => {
        goLive(player);
        const seen = watchSecondaryText(player);

        player.audio.paused = true;
        player.audio.readyState = 1;
        player.onWaiting();
        await advance(1500);

        assert.ok(
            seen.some((t) => t.includes('Buffering')),
            `silence past the delay must be labelled, saw ${JSON.stringify(seen)}`,
        );
    });
});

test('a silent handover never shows Reconnecting', async () => {
    await withPlayer(async ({ player }) => {
        goLive(player);
        const spare = await standbyReady(player);
        const seen = watchSecondaryText(player);

        player.audio.readyState = 1;
        player.onWaiting();
        await advance(2000);

        assert.equal(player.audio, spare);
        assert.ok(
            !seen.some((t) => t.includes('Reconnecting') || t.includes('Buffering')),
            `a handover must be invisible, saw ${JSON.stringify(seen)}`,
        );
    });
});

test('switching station closes the standby aimed at the old one', async () => {
    await withPlayer(async ({ player }) => {
        goLive(player);
        const spare = await standbyReady(player);

        player.playLive('https://rlmradio.xyz/live/other', 'Other Station', 'station-2');

        assert.equal(player.standbyAudio, null, 'a standby for the old mount must not survive the switch');
        assert.equal(spare.paused, true);
    });
});

test('closing the player closes the standby connection too', async () => {
    await withPlayer(async ({ player }) => {
        goLive(player);
        const spare = await standbyReady(player);

        player.close();

        assert.equal(player.standbyAudio, null);
        assert.equal(spare.paused, true, 'a closed player must leave no connection open to the station');
    });
});

test('a deliberate pause closes the standby connection too', async () => {
    await withPlayer(async ({ player }) => {
        goLive(player);
        const spare = await standbyReady(player);

        player.audio.paused = false;
        player.togglePlayPause(); // explicit pause

        assert.equal(player.standbyAudio, null);
        assert.equal(spare.paused, true);
    });
});
