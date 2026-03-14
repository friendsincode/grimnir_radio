/**
 * WebDJ MIDI Controller Support
 *
 * Supports Web MIDI API for class-compliant USB DJ controllers.
 * Compatible with Chrome, Edge, Chromium, Brave, and any Blink-based browser.
 *
 * No drivers required for class-compliant devices (most modern DJ gear).
 * Firefox and Safari: the MIDI section remains visible with a browser notice.
 */

class WebDJMIDI {
    constructor() {
        this.midiAccess = null;
        this.inputs = new Map();
        this.outputs = new Map();
        this.activeProfile = null;
        this.learnMode = null; // { paramId } | null
        this.customMappings = new Map(); // paramId -> mapping object
        this.profiles = this._buildProfiles();
    }

    async init() {
        if (typeof navigator.requestMIDIAccess !== 'function') {
            this._setStatus('unsupported', 'MIDI: Chrome/Brave/Edge only');
            this._showBrowserNotice();
            return;
        }

        try {
            this.midiAccess = await navigator.requestMIDIAccess({ sysex: false });
            this.midiAccess.onstatechange = () => this._enumerateDevices();
            this._setStatus('ready', 'NO MIDI');
            this._enumerateDevices();
        } catch (err) {
            this._setStatus('denied', 'MIDI DENIED');
            console.warn('[WebDJMIDI] Access denied:', err);
        }
    }

    // ---------------------------------------------------------------
    // Device enumeration
    // ---------------------------------------------------------------

    _enumerateDevices() {
        this.inputs.clear();
        this.outputs.clear();

        for (const input of this.midiAccess.inputs.values()) {
            this.inputs.set(input.id, input);
            input.onmidimessage = (e) => this.handleMIDIMessage(e);
        }
        for (const output of this.midiAccess.outputs.values()) {
            this.outputs.set(output.id, output);
        }

        if (this.inputs.size > 0) {
            const firstInput = this.inputs.values().next().value;
            const detected = this._detectProfile(firstInput.name);
            if (detected) {
                this.activeProfile = detected;
                this._setStatus('active', this._truncate(firstInput.name, 22));
            } else {
                this._setStatus('connected', this._truncate(firstInput.name, 22));
            }
        } else {
            this.activeProfile = null;
            this._setStatus('ready', 'NO MIDI');
        }

        this._renderDeviceList();
    }

    _detectProfile(deviceName) {
        for (const profile of this.profiles) {
            if (profile.match && profile.match.test(deviceName)) {
                return profile;
            }
        }
        return null;
    }

    // ---------------------------------------------------------------
    // Message handling
    // ---------------------------------------------------------------

    handleMIDIMessage(event) {
        if (!event.data || event.data.length < 2) return;
        const [status, data1, data2 = 0] = event.data;
        const type = status & 0xF0;
        const channel = status & 0x0F;

        // MIDI learn intercepts the next message
        if (this.learnMode) {
            if (type === 0x90 && data2 > 0) {
                this._saveLearned(this.learnMode.paramId, 'note', channel, data1);
            } else if (type === 0xB0) {
                this._saveLearned(this.learnMode.paramId, 'cc', channel, data1);
            }
            return;
        }

        const action = this._resolveAction(type, channel, data1);
        if (action) {
            this.dispatchAction(action, data2, type);
        }
    }

    _resolveAction(type, channel, data1) {
        // Custom (learned) mappings take priority
        for (const [paramId, mapping] of this.customMappings) {
            if (this._matchesMapping(mapping, type, channel, data1)) {
                return paramId;
            }
        }
        // Active profile
        if (this.activeProfile) {
            for (const mapping of this.activeProfile.mappings) {
                if (this._matchesMapping(mapping, type, channel, data1)) {
                    return mapping.action;
                }
            }
        }
        return null;
    }

    _matchesMapping(mapping, type, channel, data1) {
        if (mapping.type === 'note') {
            return (type === 0x90 || type === 0x80) &&
                   mapping.channel === channel &&
                   mapping.note === data1;
        }
        if (mapping.type === 'cc') {
            return type === 0xB0 &&
                   mapping.channel === channel &&
                   mapping.cc === data1;
        }
        return false;
    }

    // ---------------------------------------------------------------
    // Action dispatch → Alpine component
    // ---------------------------------------------------------------

    dispatchAction(action, value, msgType) {
        const app = this._getAlpineApp();
        if (!app) return;

        const norm = value / 127;  // 0-1
        const isOff = (msgType === 0x80) || (msgType === 0x90 && value === 0);

        switch (action) {
            case 'play_a':       if (!isOff) app.togglePlay('a'); break;
            case 'play_b':       if (!isOff) app.togglePlay('b'); break;
            case 'stop_a':       if (!isOff) app.stopDeck('a');  break;
            case 'stop_b':       if (!isOff) app.stopDeck('b');  break;
            case 'volume_a':     app.setVolume('a', norm);        break;
            case 'volume_b':     app.setVolume('b', norm);        break;
            case 'crossfader':   app.setCrossfader(norm);         break;
            case 'master_volume': app.setMasterVolume(norm);      break;
            case 'pitch_a':      app.setPitch('a', (norm - 0.5) * 16); break;
            case 'pitch_b':      app.setPitch('b', (norm - 0.5) * 16); break;
            case 'eq_a_hi':
                app.deckA.eqHigh = (norm - 0.5) * 24;
                app.setEQ('a');
                break;
            case 'eq_a_mid':
                app.deckA.eqMid = (norm - 0.5) * 24;
                app.setEQ('a');
                break;
            case 'eq_a_lo':
                app.deckA.eqLow = (norm - 0.5) * 24;
                app.setEQ('a');
                break;
            case 'eq_b_hi':
                app.deckB.eqHigh = (norm - 0.5) * 24;
                app.setEQ('b');
                break;
            case 'eq_b_mid':
                app.deckB.eqMid = (norm - 0.5) * 24;
                app.setEQ('b');
                break;
            case 'eq_b_lo':
                app.deckB.eqLow = (norm - 0.5) * 24;
                app.setEQ('b');
                break;
            default: {
                const m = action.match(/^cue_([ab])_(\d)$/);
                if (m && !isOff) app.handleCue(m[1], parseInt(m[2]));
            }
        }

        this._updateLEDs(app);
    }

    // ---------------------------------------------------------------
    // LED feedback
    // ---------------------------------------------------------------

    _updateLEDs(app) {
        if (!this.activeProfile?.leds) return;
        const output = this.outputs.values().next().value;
        if (!output) return;

        const send = (ledKey, on) => {
            const led = this.activeProfile.leds[ledKey];
            if (!led) return;
            const status = on ? (0x90 | led.channel) : (0x80 | led.channel);
            try { output.send([status, led.note, on ? 127 : 0]); } catch (_) {}
        };

        send('play_a', app.deckA.state === 'playing');
        send('play_b', app.deckB.state === 'playing');
    }

    // ---------------------------------------------------------------
    // MIDI Learn
    // ---------------------------------------------------------------

    startLearn(paramId) {
        this.endLearn(); // cancel any prior learn
        this.learnMode = { paramId };
        const btn = document.querySelector(`[data-learn="${paramId}"]`);
        if (btn) btn.classList.add('dj2-midi-learn-active');
    }

    endLearn() {
        if (!this.learnMode) return;
        const btn = document.querySelector(`[data-learn="${this.learnMode.paramId}"]`);
        if (btn) btn.classList.remove('dj2-midi-learn-active');
        this.learnMode = null;
    }

    _saveLearned(paramId, type, channel, noteOrCC) {
        const mapping = type === 'note'
            ? { type: 'note', channel, note: noteOrCC }
            : { type: 'cc', channel, cc: noteOrCC };
        this.customMappings.set(paramId, mapping);
        this.saveProfile();
        this.endLearn();
        this._renderLearnList(); // refresh UI badge
    }

    clearMapping(paramId) {
        this.customMappings.delete(paramId);
        this.saveProfile();
        this._renderLearnList();
    }

    clearAllMappings() {
        this.customMappings.clear();
        this.saveProfile();
        this._renderLearnList();
    }

    // ---------------------------------------------------------------
    // Profile persistence (localStorage)
    // ---------------------------------------------------------------

    saveProfile() {
        const data = {};
        for (const [k, v] of this.customMappings) {
            data[k] = v;
        }
        try { localStorage.setItem('webdj_midi_mappings', JSON.stringify(data)); } catch (_) {}
    }

    loadProfile() {
        try {
            const raw = localStorage.getItem('webdj_midi_mappings');
            if (raw) {
                const data = JSON.parse(raw);
                for (const [k, v] of Object.entries(data)) {
                    this.customMappings.set(k, v);
                }
            }
        } catch (_) {}
    }

    applyProfile(profileName) {
        const profile = this.profiles.find(p => p.name === profileName);
        if (profile) {
            this.activeProfile = profile;
            this._setStatus('active', this._truncate(profileName, 22));
        }
    }

    // ---------------------------------------------------------------
    // Status indicator
    // ---------------------------------------------------------------

    _setStatus(state, label) {
        const dot = document.getElementById('midi-status-dot');
        const lbl = document.getElementById('midi-device-name');
        if (!dot || !lbl) return;

        dot.className = 'dj2-midi-dot';
        switch (state) {
            case 'unsupported':
            case 'denied':
                dot.classList.add('dj2-midi-dot--off');
                break;
            case 'ready':
                dot.classList.add('dj2-midi-dot--ready');
                break;
            case 'connected':
                dot.classList.add('dj2-midi-dot--connected');
                break;
            case 'active':
                dot.classList.add('dj2-midi-dot--active');
                break;
        }
        lbl.textContent = label;
    }

    _showBrowserNotice() {
        const notice = document.getElementById('midi-browser-notice');
        if (notice) notice.style.display = '';
        const panel = document.getElementById('midi-learn-section');
        if (panel) panel.style.display = 'none';
    }

    // ---------------------------------------------------------------
    // UI rendering
    // ---------------------------------------------------------------

    _renderDeviceList() {
        const list = document.getElementById('midi-device-list');
        if (!list) return;

        if (this.inputs.size === 0) {
            list.innerHTML = '<div class="dj2-midi-no-device">No MIDI devices connected</div>';
            return;
        }

        const items = [];
        for (const input of this.inputs.values()) {
            const escaped = input.name.replace(/'/g, "\\'");
            items.push(`
                <div class="dj2-midi-device-item">
                    <span class="dj2-midi-device-icon">&#x1F3B9;</span>
                    <span class="dj2-midi-device-name">${this._esc(input.name)}</span>
                    <button class="dj2-midi-mini-btn"
                            onclick="window.webdjMIDI.applyProfile('${escaped}')">
                        Map
                    </button>
                </div>
            `);
        }
        list.innerHTML = items.join('');

        // Populate profile selector
        const sel = document.getElementById('midi-profile-select');
        if (sel) {
            sel.innerHTML = this.profiles
                .map(p => `<option value="${this._esc(p.name)}">${this._esc(p.name)}</option>`)
                .join('');
            if (this.activeProfile) sel.value = this.activeProfile.name;
        }
    }

    _renderLearnList() {
        const container = document.getElementById('midi-learn-list');
        if (!container) return;

        const items = this.getLearnables().map(({ id, label }) => {
            const mapping = this.customMappings.get(id);
            let badge = '';
            if (mapping) {
                const desc = mapping.type === 'note'
                    ? `Ch${mapping.channel} N${mapping.note}`
                    : `Ch${mapping.channel} CC${mapping.cc}`;
                badge = `<span class="dj2-midi-mapped-badge">${desc}</span>
                         <button class="dj2-midi-mini-btn dj2-midi-mini-btn--clear"
                                 onclick="window.webdjMIDI.clearMapping('${id}')">✕</button>`;
            }
            return `
                <div class="dj2-midi-learn-row">
                    <span class="dj2-midi-learn-label">${this._esc(label)}</span>
                    <div class="dj2-midi-learn-controls">
                        ${badge}
                        <button class="dj2-midi-mini-btn dj2-midi-mini-btn--learn"
                                data-learn="${id}"
                                onclick="window.webdjMIDI.startLearn('${id}')">
                            LEARN
                        </button>
                    </div>
                </div>
            `;
        });
        container.innerHTML = items.join('');
    }

    // ---------------------------------------------------------------
    // Helpers
    // ---------------------------------------------------------------

    getLearnables() {
        return [
            { id: 'play_a',       label: 'Play/Pause Deck A' },
            { id: 'play_b',       label: 'Play/Pause Deck B' },
            { id: 'stop_a',       label: 'Stop Deck A' },
            { id: 'stop_b',       label: 'Stop Deck B' },
            { id: 'volume_a',     label: 'Volume Deck A' },
            { id: 'volume_b',     label: 'Volume Deck B' },
            { id: 'crossfader',   label: 'Crossfader' },
            { id: 'master_volume',label: 'Master Volume' },
            { id: 'pitch_a',      label: 'Pitch Deck A' },
            { id: 'pitch_b',      label: 'Pitch Deck B' },
            { id: 'eq_a_hi',      label: 'EQ Hi — Deck A' },
            { id: 'eq_a_mid',     label: 'EQ Mid — Deck A' },
            { id: 'eq_a_lo',      label: 'EQ Lo — Deck A' },
            { id: 'eq_b_hi',      label: 'EQ Hi — Deck B' },
            { id: 'eq_b_mid',     label: 'EQ Mid — Deck B' },
            { id: 'eq_b_lo',      label: 'EQ Lo — Deck B' },
            { id: 'cue_a_1',      label: 'Hot Cue A-1' },
            { id: 'cue_a_2',      label: 'Hot Cue A-2' },
            { id: 'cue_a_3',      label: 'Hot Cue A-3' },
            { id: 'cue_a_4',      label: 'Hot Cue A-4' },
            { id: 'cue_a_5',      label: 'Hot Cue A-5' },
            { id: 'cue_a_6',      label: 'Hot Cue A-6' },
            { id: 'cue_a_7',      label: 'Hot Cue A-7' },
            { id: 'cue_a_8',      label: 'Hot Cue A-8' },
            { id: 'cue_b_1',      label: 'Hot Cue B-1' },
            { id: 'cue_b_2',      label: 'Hot Cue B-2' },
            { id: 'cue_b_3',      label: 'Hot Cue B-3' },
            { id: 'cue_b_4',      label: 'Hot Cue B-4' },
            { id: 'cue_b_5',      label: 'Hot Cue B-5' },
            { id: 'cue_b_6',      label: 'Hot Cue B-6' },
            { id: 'cue_b_7',      label: 'Hot Cue B-7' },
            { id: 'cue_b_8',      label: 'Hot Cue B-8' },
        ];
    }

    _getAlpineApp() {
        const el = document.getElementById('webdj-app');
        if (!el) return null;
        try { return Alpine.$data(el); } catch (_) { return null; }
    }

    _truncate(str, n) {
        return str.length <= n ? str : str.slice(0, n - 1) + '…';
    }

    _esc(str) {
        return String(str || '')
            .replace(/&/g, '&amp;')
            .replace(/</g, '&lt;')
            .replace(/>/g, '&gt;')
            .replace(/"/g, '&quot;');
    }

    // ---------------------------------------------------------------
    // Built-in controller profiles
    // ---------------------------------------------------------------

    _buildProfiles() {
        const herculesMappings = (hotCueCount) => {
            const m = [
                { type: 'note', channel: 0, note: 0x0B, action: 'play_a' },
                { type: 'note', channel: 0, note: 0x0C, action: 'stop_a' },
                { type: 'note', channel: 1, note: 0x0B, action: 'play_b' },
                { type: 'note', channel: 1, note: 0x0C, action: 'stop_b' },
                { type: 'cc',   channel: 0, cc: 0x31, action: 'volume_a' },
                { type: 'cc',   channel: 1, cc: 0x31, action: 'volume_b' },
                { type: 'cc',   channel: 0, cc: 0x36, action: 'crossfader' },
                { type: 'cc',   channel: 0, cc: 0x37, action: 'master_volume' },
                { type: 'cc',   channel: 0, cc: 0x32, action: 'eq_a_hi' },
                { type: 'cc',   channel: 0, cc: 0x33, action: 'eq_a_mid' },
                { type: 'cc',   channel: 0, cc: 0x34, action: 'eq_a_lo' },
                { type: 'cc',   channel: 1, cc: 0x32, action: 'eq_b_hi' },
                { type: 'cc',   channel: 1, cc: 0x33, action: 'eq_b_mid' },
                { type: 'cc',   channel: 1, cc: 0x34, action: 'eq_b_lo' },
                { type: 'cc',   channel: 0, cc: 0x21, action: 'pitch_a' },
                { type: 'cc',   channel: 1, cc: 0x21, action: 'pitch_b' },
            ];
            for (let i = 0; i < hotCueCount; i++) {
                m.push({ type: 'note', channel: 0, note: i, action: `cue_a_${i + 1}` });
                m.push({ type: 'note', channel: 1, note: i, action: `cue_b_${i + 1}` });
            }
            return m;
        };

        const herculesLeds = {
            play_a: { note: 0x0B, channel: 0 },
            play_b: { note: 0x0B, channel: 1 },
        };

        const pioneerMappings = (hotCueCount) => {
            const m = [
                { type: 'note', channel: 0,  note: 0x0B, action: 'play_a' },
                { type: 'note', channel: 0,  note: 0x0C, action: 'stop_a' },
                { type: 'note', channel: 1,  note: 0x0B, action: 'play_b' },
                { type: 'note', channel: 1,  note: 0x0C, action: 'stop_b' },
                { type: 'cc',   channel: 0,  cc: 0x13, action: 'volume_a' },
                { type: 'cc',   channel: 1,  cc: 0x13, action: 'volume_b' },
                { type: 'cc',   channel: 0,  cc: 0x1F, action: 'crossfader' },
                { type: 'cc',   channel: 0,  cc: 0x07, action: 'master_volume' },
                { type: 'cc',   channel: 0,  cc: 0x17, action: 'eq_a_hi' },
                { type: 'cc',   channel: 0,  cc: 0x16, action: 'eq_a_mid' },
                { type: 'cc',   channel: 0,  cc: 0x15, action: 'eq_a_lo' },
                { type: 'cc',   channel: 1,  cc: 0x17, action: 'eq_b_hi' },
                { type: 'cc',   channel: 1,  cc: 0x16, action: 'eq_b_mid' },
                { type: 'cc',   channel: 1,  cc: 0x15, action: 'eq_b_lo' },
                { type: 'cc',   channel: 0,  cc: 0x0A, action: 'pitch_a' },
                { type: 'cc',   channel: 1,  cc: 0x0A, action: 'pitch_b' },
            ];
            for (let i = 0; i < hotCueCount; i++) {
                m.push({ type: 'note', channel: 7, note: i, action: `cue_a_${i + 1}` });
                m.push({ type: 'note', channel: 8, note: i, action: `cue_b_${i + 1}` });
            }
            return m;
        };

        const pioneerLeds = {
            play_a: { note: 0x0B, channel: 0 },
            play_b: { note: 0x0B, channel: 1 },
        };

        return [
            // --------------------------------------------------------
            // Generic MIDI — catches anything with basic CC-7 pattern
            // --------------------------------------------------------
            {
                name: 'Generic MIDI',
                match: null,
                mappings: [
                    { type: 'cc', channel: 0, cc: 0x07, action: 'volume_a' },
                    { type: 'cc', channel: 1, cc: 0x07, action: 'volume_b' },
                    { type: 'cc', channel: 0, cc: 0x08, action: 'crossfader' },
                ],
                leds: {},
            },

            // --------------------------------------------------------
            // Hercules DJControl Inpulse 200 (4 pads per deck)
            // --------------------------------------------------------
            {
                name: 'Hercules DJControl Inpulse 200',
                match: /hercules.*inpulse.*200/i,
                mappings: herculesMappings(4),
                leds: herculesLeds,
            },

            // --------------------------------------------------------
            // Hercules DJControl Inpulse 300 (6 pads per deck)
            // --------------------------------------------------------
            {
                name: 'Hercules DJControl Inpulse 300',
                match: /hercules.*inpulse.*300/i,
                mappings: herculesMappings(6),
                leds: herculesLeds,
            },

            // --------------------------------------------------------
            // Hercules DJControl Inpulse 500 (8 pads per deck)
            // --------------------------------------------------------
            {
                name: 'Hercules DJControl Inpulse 500',
                match: /hercules.*inpulse.*500/i,
                mappings: herculesMappings(8),
                leds: herculesLeds,
            },

            // --------------------------------------------------------
            // Pioneer DDJ-200 (4 pads per deck)
            // --------------------------------------------------------
            {
                name: 'Pioneer DDJ-200',
                match: /pioneer.*ddj.?200/i,
                mappings: pioneerMappings(4),
                leds: pioneerLeds,
            },

            // --------------------------------------------------------
            // Pioneer DDJ-FLX4 (8 pads per deck)
            // --------------------------------------------------------
            {
                name: 'Pioneer DDJ-FLX4',
                match: /pioneer.*ddj.?flx4/i,
                mappings: pioneerMappings(8),
                leds: pioneerLeds,
            },

            // --------------------------------------------------------
            // Numark Party Mix II
            // --------------------------------------------------------
            {
                name: 'Numark Party Mix II',
                match: /numark.*party.*mix/i,
                mappings: [
                    { type: 'note', channel: 0, note: 0x00, action: 'play_a' },
                    { type: 'note', channel: 1, note: 0x00, action: 'play_b' },
                    { type: 'cc',   channel: 0, cc: 0x07, action: 'volume_a' },
                    { type: 'cc',   channel: 1, cc: 0x07, action: 'volume_b' },
                    { type: 'cc',   channel: 2, cc: 0x07, action: 'crossfader' },
                    { type: 'note', channel: 0, note: 0x06, action: 'cue_a_1' },
                    { type: 'note', channel: 0, note: 0x07, action: 'cue_a_2' },
                    { type: 'note', channel: 0, note: 0x08, action: 'cue_a_3' },
                    { type: 'note', channel: 0, note: 0x09, action: 'cue_a_4' },
                    { type: 'note', channel: 1, note: 0x06, action: 'cue_b_1' },
                    { type: 'note', channel: 1, note: 0x07, action: 'cue_b_2' },
                    { type: 'note', channel: 1, note: 0x08, action: 'cue_b_3' },
                    { type: 'note', channel: 1, note: 0x09, action: 'cue_b_4' },
                ],
                leds: {
                    play_a: { note: 0x00, channel: 0 },
                    play_b: { note: 0x00, channel: 1 },
                },
            },

            // --------------------------------------------------------
            // Numark DJ2GO2
            // --------------------------------------------------------
            {
                name: 'Numark DJ2GO2',
                match: /numark.*dj2go/i,
                mappings: [
                    { type: 'note', channel: 0, note: 0x00, action: 'play_a' },
                    { type: 'note', channel: 1, note: 0x00, action: 'play_b' },
                    { type: 'cc',   channel: 0, cc: 0x07, action: 'volume_a' },
                    { type: 'cc',   channel: 1, cc: 0x07, action: 'volume_b' },
                    { type: 'cc',   channel: 0, cc: 0x08, action: 'crossfader' },
                    { type: 'note', channel: 0, note: 0x06, action: 'cue_a_1' },
                    { type: 'note', channel: 0, note: 0x07, action: 'cue_a_2' },
                    { type: 'note', channel: 1, note: 0x06, action: 'cue_b_1' },
                    { type: 'note', channel: 1, note: 0x07, action: 'cue_b_2' },
                ],
                leds: {
                    play_a: { note: 0x00, channel: 0 },
                    play_b: { note: 0x00, channel: 1 },
                },
            },

            // --------------------------------------------------------
            // Custom — MIDI learn only, no built-in mappings
            // --------------------------------------------------------
            {
                name: 'Custom',
                match: null,
                mappings: [],
                leds: {},
            },
        ];
    }
}

// ---------------------------------------------------------------
// Bootstrap on DOMContentLoaded
// ---------------------------------------------------------------

document.addEventListener('DOMContentLoaded', () => {
    window.webdjMIDI = new WebDJMIDI();
    window.webdjMIDI.loadProfile();
    window.webdjMIDI.init();

    // LED feedback: watch play button class changes via MutationObserver
    const deckObserver = new MutationObserver(() => {
        const app = window.webdjMIDI._getAlpineApp();
        if (app) window.webdjMIDI._updateLEDs(app);
    });

    // Attach observer after Alpine initialises the deck UI
    const attachObserver = () => {
        const btnA = document.querySelector('#webdj-app .dj2-deck-a .dj2-btn-play');
        const btnB = document.querySelector('#webdj-app .dj2-deck-b .dj2-btn-play');
        const opts = { attributes: true, attributeFilter: ['class'] };
        if (btnA) deckObserver.observe(btnA, opts);
        if (btnB) deckObserver.observe(btnB, opts);
    };
    setTimeout(attachObserver, 1200);

    // Wire profile selector change
    document.addEventListener('change', (e) => {
        if (e.target.id === 'midi-profile-select') {
            window.webdjMIDI.applyProfile(e.target.value);
        }
    });

    // Wire "clear all" button
    document.addEventListener('click', (e) => {
        if (e.target.id === 'midi-clear-all-btn') {
            if (confirm('Clear all MIDI learn mappings?')) {
                window.webdjMIDI.clearAllMappings();
            }
        }
        // Render learn list when the MIDI panel opens
        if (e.target.closest && e.target.closest('[data-midi-panel-toggle]')) {
            setTimeout(() => {
                window.webdjMIDI._renderLearnList();
                window.webdjMIDI._renderDeviceList();
            }, 50);
        }
    });
});
