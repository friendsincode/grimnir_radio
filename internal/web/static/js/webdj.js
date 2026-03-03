/**
 * WebDJ Console - Browser-based DJ mixing interface
 *
 * Copyright (C) 2026 Friends Incode
 * SPDX-License-Identifier: AGPL-3.0-or-later
 */

function webdjConsole() {
    return {
        // Session state
        sessionId: null,
        stationId: null,
        ws: null,
        connected: false,
        loading: false,

        // Deck A state
        deckA: {
            mediaId: '',
            title: '',
            artist: '',
            durationMs: 0,
            positionMs: 0,
            state: 'idle',
            bpm: null,
            pitch: 0,
            volume: 1.0,
            hotCues: [],
            eqHigh: 0,
            eqMid: 0,
            eqLow: 0
        },

        // Deck B state
        deckB: {
            mediaId: '',
            title: '',
            artist: '',
            durationMs: 0,
            positionMs: 0,
            state: 'idle',
            bpm: null,
            pitch: 0,
            volume: 1.0,
            hotCues: [],
            eqHigh: 0,
            eqMid: 0,
            eqLow: 0
        },

        // Mixer state
        mixer: {
            crossfader: 0.5,
            masterVolume: 1.0,
            cueSplit: false,
            cueMixLevel: 0.5,
            headphoneCueA: false,
            headphoneCueB: false,
            headphoneVol: 1.0
        },

        // Live broadcast state
        isLive: false,
        selectedMount: '',
        liveMountName: '',
        liveId: '',
        inputType: 'webrtc',

        // Library state
        libraryTracks: [],
        searchQuery: '',
        selectedPlaylist: '',

        // Waveform canvases
        waveformA: null,
        waveformB: null,

        // Position update timer
        positionTimer: null,

        init() {
            console.log('[WebDJ] init() called');

            // Get station ID from the webdj-app element
            const appElement = document.getElementById('webdj-app');
            this.stationId = appElement?.dataset.stationId || '';

            console.log('[WebDJ] Initialized with:', {
                stationId: this.stationId || '(empty)',
                hasAppElement: !!appElement,
                hasWSToken: !!window.GRIMNIR_WS_TOKEN
            });

            // Initialize waveform canvases
            this.$nextTick(() => {
                this.waveformA = document.getElementById('waveform-a');
                this.waveformB = document.getElementById('waveform-b');
            });

            // Load initial library
            this.searchLibrary();

            // Setup end session button
            document.getElementById('end-session-btn')?.addEventListener('click', () => {
                this.endSession();
            });

            // Cleanup on page unload
            window.addEventListener('beforeunload', () => {
                this.disconnect();
            });

            // Keyboard shortcuts
            document.addEventListener('keydown', (e) => this.handleKeyboard(e));

            console.log('[WebDJ] init() complete');
        },

        async startSession() {
            console.log('[WebDJ] startSession() called');

            try {
                this.loading = true;

                const stationId = this.getStationId();
                const authToken = this.getAuthToken();

                console.log('[WebDJ] Starting session with:', {
                    stationId: stationId || '(empty)',
                    hasAuthToken: !!authToken
                });

                if (!stationId) {
                    console.error('[WebDJ] No station ID available');
                    alert('No station selected. Please select a station first.');
                    this.loading = false;
                    return;
                }

                if (!authToken) {
                    console.error('[WebDJ] No auth token available');
                    alert('Authentication required. Please refresh the page.');
                    this.loading = false;
                    return;
                }

                const response = await fetch('/api/v1/webdj/sessions', {
                    method: 'POST',
                    headers: {
                        'Content-Type': 'application/json',
                        'Authorization': 'Bearer ' + authToken
                    },
                    body: JSON.stringify({
                        station_id: stationId
                    })
                });

                if (!response.ok) {
                    const errorText = await response.text();
                    console.error('[WebDJ] Session start failed:', response.status, errorText);
                    alert('Failed to start session: ' + errorText);
                    this.loading = false;
                    return;
                }

                const data = await response.json();
                console.log('[WebDJ] Session created:', data);
                this.sessionId = data.id;

                // Connect WebSocket
                this.connect();

                // Update UI
                document.getElementById('end-session-btn')?.classList.remove('d-none');

                // Load state from response
                this.loadState(data);

            } catch (error) {
                console.error('[WebDJ] Exception in startSession:', error);
                alert('Failed to start WebDJ session: ' + error.message);
            } finally {
                this.loading = false;
            }
        },

        async endSession() {
            if (!this.sessionId) return;

            if (!confirm('End this WebDJ session?')) return;

            try {
                await fetch(`/api/v1/webdj/sessions/${this.sessionId}`, {
                    method: 'DELETE',
                    headers: {
                        'Authorization': 'Bearer ' + this.getAuthToken()
                    }
                });

                this.disconnect();
                this.sessionId = null;
                document.getElementById('end-session-btn')?.classList.add('d-none');

            } catch (error) {
                console.error('Failed to end session:', error);
            }
        },

        connect() {
            if (this.ws) {
                this.ws.close();
            }

            const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
            const token = this.getAuthToken();
            const wsUrl = `${protocol}//${window.location.host}/api/v1/webdj/sessions/${this.sessionId}/ws?token=${encodeURIComponent(token)}`;

            this.ws = new WebSocket(wsUrl);

            this.ws.onopen = () => {
                this.connected = true;
                this.updateConnectionStatus('connected');
                console.log('WebDJ WebSocket connected');
            };

            this.ws.onclose = () => {
                this.connected = false;
                this.updateConnectionStatus('disconnected');
                console.log('WebDJ WebSocket disconnected');

                // Attempt reconnect after delay
                setTimeout(() => {
                    if (this.sessionId && !this.connected) {
                        this.connect();
                    }
                }, 3000);
            };

            this.ws.onerror = (error) => {
                console.error('WebSocket error:', error);
                this.updateConnectionStatus('error');
            };

            this.ws.onmessage = (event) => {
                try {
                    const msg = JSON.parse(event.data);
                    this.handleMessage(msg);
                } catch (error) {
                    console.error('Failed to parse message:', error);
                }
            };

            // Start position update timer
            this.startPositionTimer();
        },

        disconnect() {
            if (this.positionTimer) {
                clearInterval(this.positionTimer);
                this.positionTimer = null;
            }
            if (this.ws) {
                this.ws.close();
                this.ws = null;
            }
            this.connected = false;
        },

        updateConnectionStatus(status) {
            const el = document.getElementById('connection-status');
            if (!el) return;

            el.className = 'badge';
            switch (status) {
                case 'connected':
                    el.classList.add('bg-success');
                    el.textContent = 'Connected';
                    break;
                case 'disconnected':
                    el.classList.add('bg-secondary');
                    el.textContent = 'Disconnected';
                    break;
                case 'error':
                    el.classList.add('bg-danger');
                    el.textContent = 'Error';
                    break;
            }
        },

        handleMessage(msg) {
            switch (msg.type) {
                case 'initial_state':
                    this.loadStateFromWS(msg.data);
                    break;

                case 'deck_loaded':
                case 'deck_state':
                    this.updateDeckFromWS(msg.deck, msg.data);
                    break;

                case 'deck_position':
                    this.updateDeckPosition(msg.deck, msg.data.position_ms);
                    break;

                case 'deck_ejected':
                    this.resetDeck(msg.deck);
                    break;

                case 'deck_volume':
                    this.getDeck(msg.deck).volume = Number(msg.data.volume);
                    break;

                case 'deck_eq': {
                    const deck = this.getDeck(msg.deck);
                    deck.eqHigh = Number(msg.data.high);
                    deck.eqMid = Number(msg.data.mid);
                    deck.eqLow = Number(msg.data.low);
                    break;
                }

                case 'deck_pitch':
                    this.getDeck(msg.deck).pitch = Number(msg.data.pitch);
                    break;

                case 'cue_set':
                case 'cue_deleted':
                    this.refreshDeckState(msg.deck);
                    break;

                case 'mixer_crossfader':
                    this.mixer.crossfader = Number(msg.data.position);
                    break;

                case 'mixer_master_volume':
                    this.mixer.masterVolume = Number(msg.data.volume);
                    break;

                case 'mixer_cue_split':
                    this.mixer.cueSplit = !!msg.data.enabled;
                    break;

                case 'mixer_cue_mix_level':
                    this.mixer.cueMixLevel = Number(msg.data.level);
                    break;

                case 'mixer_headphone_cue':
                    if (msg.data.deck === 'a') {
                        this.mixer.headphoneCueA = !!msg.data.enabled;
                    } else if (msg.data.deck === 'b') {
                        this.mixer.headphoneCueB = !!msg.data.enabled;
                    }
                    break;

                case 'mixer_headphone_volume':
                    this.mixer.headphoneVol = Number(msg.data.volume);
                    break;

                case 'live_started':
                    this.isLive = true;
                    this.liveId = msg.data.live_id || '';
                    this.liveMountName = msg.data.mount_id || this.selectedMount;
                    console.log('WebDJ went live:', msg.data);
                    break;

                case 'live_stopped':
                    this.isLive = false;
                    this.liveId = '';
                    this.liveMountName = '';
                    console.log('WebDJ went off air');
                    break;

                case 'ping':
                    this.send({ action: 'pong' });
                    break;

                case 'error':
                    console.error('Server error:', msg.data);
                    break;
            }
        },

        loadState(data) {
            if (data.deck_a_state) {
                this.loadDeckState('a', data.deck_a_state);
            }
            if (data.deck_b_state) {
                this.loadDeckState('b', data.deck_b_state);
            }
            if (data.mixer_state) {
                this.loadMixerState(data.mixer_state);
            }
        },

        loadStateFromWS(data) {
            if (data.deck_a) {
                this.loadDeckState('a', data.deck_a);
            }
            if (data.deck_b) {
                this.loadDeckState('b', data.deck_b);
            }
            if (data.mixer) {
                this.loadMixerState(data.mixer);
            }
        },

        loadMixerState(state) {
            this.mixer.crossfader = Number(state.crossfader) || 0.5;
            this.mixer.masterVolume = Number(state.master_volume) ?? 1.0;
            this.mixer.cueSplit = !!state.cue_split;
            this.mixer.cueMixLevel = Number(state.cue_mix_level) || 0;
            this.mixer.headphoneCueA = !!state.headphone_cue_a;
            this.mixer.headphoneCueB = !!state.headphone_cue_b;
            this.mixer.headphoneVol = Number(state.headphone_vol) ?? 1.0;
        },

        loadDeckState(deckId, state) {
            const deck = this.getDeck(deckId);
            deck.mediaId = state.media_id || '';
            deck.title = state.title || '';
            deck.artist = state.artist || '';
            deck.durationMs = state.duration_ms || 0;
            deck.positionMs = state.position_ms || 0;
            deck.state = state.state || 'idle';
            deck.bpm = state.bpm || null;
            deck.pitch = Number(state.pitch) || 0;
            deck.volume = Number(state.volume) ?? 1.0;
            deck.hotCues = state.hot_cues || [];
            deck.eqHigh = Number(state.eq_high) || 0;
            deck.eqMid = Number(state.eq_mid) || 0;
            deck.eqLow = Number(state.eq_low) || 0;

            // Load waveform if track is loaded
            if (deck.mediaId) {
                this.loadWaveform(deckId, deck.mediaId);
            }
        },

        updateDeckFromWS(deckId, state) {
            this.loadDeckState(deckId, state);
        },

        updateDeckPosition(deckId, positionMs) {
            const deck = this.getDeck(deckId);
            deck.positionMs = positionMs;
        },

        resetDeck(deckId) {
            const deck = this.getDeck(deckId);
            deck.mediaId = '';
            deck.title = '';
            deck.artist = '';
            deck.durationMs = 0;
            deck.positionMs = 0;
            deck.state = 'idle';
            deck.bpm = null;
            deck.hotCues = [];
            this.clearWaveform(deckId);
        },

        getDeck(deckId) {
            return deckId === 'a' ? this.deckA : this.deckB;
        },

        send(data) {
            if (this.ws && this.ws.readyState === WebSocket.OPEN) {
                this.ws.send(JSON.stringify(data));
            }
        },

        // Deck controls
        async loadTrack(deckId, mediaId) {
            this.send({
                action: 'load',
                deck: deckId,
                data: { media_id: mediaId }
            });
        },

        togglePlay(deckId) {
            const deck = this.getDeck(deckId);
            if (deck.state === 'playing') {
                this.send({ action: 'pause', deck: deckId });
            } else {
                this.send({ action: 'play', deck: deckId });
            }
        },

        stopDeck(deckId) {
            this.send({ action: 'pause', deck: deckId });
            this.seekDeck(deckId, null, 0);
        },

        ejectDeck(deckId) {
            if (!confirm('Eject track from deck?')) return;
            this.send({ action: 'eject', deck: deckId });
        },

        seekDeck(deckId, event, positionMs = null) {
            const deck = this.getDeck(deckId);
            if (!deck.mediaId) return;

            if (positionMs === null && event) {
                const rect = event.target.getBoundingClientRect();
                const x = event.clientX - rect.left;
                const pct = x / rect.width;
                positionMs = Math.floor(pct * deck.durationMs);
            }

            this.send({
                action: 'seek',
                deck: deckId,
                data: { position_ms: positionMs }
            });
        },

        setVolume(deckId, volume) {
            this.send({
                action: 'volume',
                deck: deckId,
                data: { volume: parseFloat(volume) }
            });
        },

        setEQ(deckId) {
            const deck = this.getDeck(deckId);
            this.send({
                action: 'eq',
                deck: deckId,
                data: {
                    high: parseFloat(deck.eqHigh),
                    mid: parseFloat(deck.eqMid),
                    low: parseFloat(deck.eqLow)
                }
            });
        },

        setPitch(deckId, pitch) {
            this.send({
                action: 'pitch',
                deck: deckId,
                data: { pitch: parseFloat(pitch) }
            });
        },

        // Hot cues
        handleCue(deckId, cueId) {
            const deck = this.getDeck(deckId);
            const cue = deck.hotCues.find(c => c.id === cueId);

            if (cue) {
                // Jump to cue point
                this.seekDeck(deckId, null, cue.position_ms);
            } else {
                // Set new cue point
                this.send({
                    action: 'cue_set',
                    deck: deckId,
                    data: { cue_id: cueId, position_ms: deck.positionMs }
                });
            }
        },

        deleteCue(deckId, cueId) {
            this.send({
                action: 'cue_delete',
                deck: deckId,
                data: { cue_id: cueId }
            });
        },

        getCueClass(deckId, cueId) {
            const deck = this.getDeck(deckId);
            const hasCue = deck.hotCues.some(c => c.id === cueId);
            return hasCue ? 'btn-warning has-cue' : 'btn-outline-secondary';
        },

        // Mixer controls
        setCrossfader(position) {
            this.send({
                action: 'crossfader',
                data: { position: parseFloat(position) }
            });
        },

        setMasterVolume(volume) {
            this.send({
                action: 'master_volume',
                data: { volume: parseFloat(volume) }
            });
        },

        // Monitor/headphone controls
        setCueSplit(enabled) {
            this.send({
                action: 'cue_split',
                data: { enabled: enabled }
            });
        },

        setCueMixLevel(level) {
            this.send({
                action: 'cue_mix_level',
                data: { level: parseFloat(level) }
            });
        },

        setHeadphoneCue(deck, enabled) {
            this.send({
                action: 'headphone_cue',
                data: { deck: deck, enabled: enabled }
            });
        },

        setHeadphoneVolume(volume) {
            this.send({
                action: 'headphone_volume',
                data: { volume: parseFloat(volume) }
            });
        },

        // Live broadcast controls
        async toggleLive() {
            if (this.isLive) {
                await this.goOffAir();
            } else {
                await this.goLive();
            }
        },

        async goLive() {
            if (!this.selectedMount) {
                alert('Please select a mount to broadcast to.');
                return;
            }

            try {
                const response = await fetch(`/api/v1/webdj/sessions/${this.sessionId}/live`, {
                    method: 'POST',
                    headers: {
                        'Content-Type': 'application/json',
                        'Authorization': 'Bearer ' + this.getAuthToken()
                    },
                    body: JSON.stringify({
                        mount_id: this.selectedMount,
                        input_type: this.inputType
                    })
                });

                if (!response.ok) {
                    const error = await response.text();
                    throw new Error(error);
                }

                const data = await response.json();
                this.isLive = true;
                this.liveId = data.live_id || '';
                this.liveMountName = this.selectedMount;
                console.log('Went live:', data);

            } catch (error) {
                console.error('Failed to go live:', error);
                alert('Failed to go live: ' + error.message);
            }
        },

        async goOffAir() {
            if (!confirm('Go off air? This will stop your live broadcast.')) {
                return;
            }

            try {
                const response = await fetch(`/api/v1/webdj/sessions/${this.sessionId}/live`, {
                    method: 'DELETE',
                    headers: {
                        'Authorization': 'Bearer ' + this.getAuthToken()
                    }
                });

                if (!response.ok) {
                    const error = await response.text();
                    throw new Error(error);
                }

                this.isLive = false;
                this.liveId = '';
                this.liveMountName = '';
                console.log('Went off air');

            } catch (error) {
                console.error('Failed to go off air:', error);
                alert('Failed to go off air: ' + error.message);
            }
        },

        // Library
        async searchLibrary() {
            this.loading = true;
            try {
                const params = new URLSearchParams();
                if (this.searchQuery) params.set('q', this.searchQuery);
                if (this.selectedPlaylist) params.set('playlist_id', this.selectedPlaylist);

                const response = await fetch(`/dashboard/webdj/library/search?${params}`);
                if (!response.ok) throw new Error('Search failed');

                this.libraryTracks = await response.json();
            } catch (error) {
                console.error('Library search failed:', error);
            } finally {
                this.loading = false;
            }
        },

        loadToDeck(deckId) {
            document.querySelector('.library-search')?.focus();
        },

        startDrag(event, track) {
            event.dataTransfer.setData('text/plain', track.id);
            event.dataTransfer.effectAllowed = 'copy';
        },

        // Waveform
        async loadWaveform(deckId, mediaId) {
            try {
                const response = await fetch(`/api/v1/webdj/library/${mediaId}/waveform`, {
                    headers: {
                        'Authorization': 'Bearer ' + this.getAuthToken()
                    }
                });
                if (!response.ok) throw new Error('Failed to load waveform');

                const data = await response.json();
                this.drawWaveform(deckId, data);
            } catch (error) {
                console.error('Failed to load waveform:', error);
            }
        },

        drawWaveform(deckId, data) {
            const canvas = deckId === 'a' ? this.waveformA : this.waveformB;
            if (!canvas) return;

            const ctx = canvas.getContext('2d');
            const width = canvas.width;
            const height = canvas.height;
            const centerY = height / 2;

            ctx.clearRect(0, 0, width, height);

            const peaks = data.peak_left || [];
            if (peaks.length === 0) return;

            const step = peaks.length / width;
            const color = deckId === 'a' ? '#0d6efd' : '#dc3545';

            ctx.fillStyle = color;
            ctx.globalAlpha = 0.7;

            for (let x = 0; x < width; x++) {
                const i = Math.floor(x * step);
                const peak = peaks[i] || 0;
                const barHeight = peak * centerY;

                ctx.fillRect(x, centerY - barHeight, 1, barHeight * 2);
            }

            ctx.globalAlpha = 1;
        },

        clearWaveform(deckId) {
            const canvas = deckId === 'a' ? this.waveformA : this.waveformB;
            if (!canvas) return;

            const ctx = canvas.getContext('2d');
            ctx.clearRect(0, 0, canvas.width, canvas.height);
        },

        // Position timer for simulating playback progress
        startPositionTimer() {
            if (this.positionTimer) {
                clearInterval(this.positionTimer);
            }

            this.positionTimer = setInterval(() => {
                if (this.deckA.state === 'playing' && this.deckA.durationMs > 0) {
                    this.deckA.positionMs = Math.min(
                        this.deckA.positionMs + 100,
                        this.deckA.durationMs
                    );
                }
                if (this.deckB.state === 'playing' && this.deckB.durationMs > 0) {
                    this.deckB.positionMs = Math.min(
                        this.deckB.positionMs + 100,
                        this.deckB.durationMs
                    );
                }
            }, 100);
        },

        // Keyboard shortcuts
        handleKeyboard(e) {
            if (!this.sessionId) return;
            if (e.target.tagName === 'INPUT' || e.target.tagName === 'TEXTAREA') return;

            switch (e.key) {
                case 'q':
                    this.togglePlay('a');
                    break;
                case 'w':
                    this.togglePlay('b');
                    break;
                case '1': case '2': case '3': case '4':
                    this.handleCue('a', parseInt(e.key));
                    break;
                case '5': case '6': case '7': case '8':
                    this.handleCue('b', parseInt(e.key) - 4);
                    break;
            }
        },

        // Helpers
        formatMs(ms) {
            if (!ms || ms <= 0) return '0:00';
            const seconds = Math.floor(ms / 1000);
            const minutes = Math.floor(seconds / 60);
            const secs = seconds % 60;
            return `${minutes}:${secs.toString().padStart(2, '0')}`;
        },

        getAuthToken() {
            return window.GRIMNIR_WS_TOKEN || '';
        },

        getStationId() {
            return document.querySelector('[data-station-id]')?.dataset.stationId || this.stationId || '';
        },

        async refreshDeckState(deckId) {
            try {
                const response = await fetch(`/api/v1/webdj/sessions/${this.sessionId}`, {
                    headers: {
                        'Authorization': 'Bearer ' + this.getAuthToken()
                    }
                });
                if (response.ok) {
                    const data = await response.json();
                    if (deckId === 'a' && data.deck_a_state) {
                        this.loadDeckState('a', data.deck_a_state);
                    } else if (deckId === 'b' && data.deck_b_state) {
                        this.loadDeckState('b', data.deck_b_state);
                    }
                }
            } catch (error) {
                console.error('Failed to refresh deck state:', error);
            }
        }
    };
}
