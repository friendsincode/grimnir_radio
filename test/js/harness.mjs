/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

// Loads the real internal/web/static/js/app.js inside a vm sandbox with a
// minimal browser stub, so the player's reconnect state machine can be tested
// as it actually ships. Deliberately dependency-free: no package.json, no
// node_modules, just node:test and node:vm.

import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { dirname, resolve } from 'node:path';
import vm from 'node:vm';

const here = dirname(fileURLToPath(import.meta.url));
const APP_JS = resolve(here, '../../internal/web/static/js/app.js');

function makeElement(id = '') {
    const listeners = {};
    return {
        id,
        style: {},
        dataset: {},
        textContent: '',
        innerHTML: '',
        value: '',
        offsetWidth: 100,
        clientWidth: 100,
        scrollWidth: 100,
        classList: {
            _s: new Set(),
            add(...c) { c.forEach((x) => this._s.add(x)); },
            remove(...c) { c.forEach((x) => this._s.delete(x)); },
            replace(a, b) { this._s.delete(a); this._s.add(b); },
            toggle(c) { this._s.has(c) ? this._s.delete(c) : this._s.add(c); },
            contains(c) { return this._s.has(c); },
        },
        addEventListener(type, fn) { (listeners[type] ||= []).push(fn); },
        removeEventListener(type, fn) {
            if (listeners[type]) listeners[type] = listeners[type].filter((f) => f !== fn);
        },
        dispatchEvent(type, ev = {}) { (listeners[type] || []).forEach((f) => f({ type, ...ev })); },
        querySelector() { return null; },
        querySelectorAll() { return []; },
        replaceChildren() {},
        appendChild() {},
        removeChild() {},
        remove() {},
        setAttribute() {},
        removeAttribute() {},
        getAttribute() { return null; },
        getBoundingClientRect() { return { left: 0, top: 0, width: 100, height: 20 }; },
        closest() { return null; },
        focus() {},
        click() {},
        insertAdjacentHTML() {},
    };
}

// A stand-in for HTMLAudioElement covering exactly the surface the player uses.
// playResult lets a test make play() reject, which is how a failed reconnect
// looks to the player.
class FakeAudio {
    constructor() {
        this.paused = true;
        this.currentTime = 0;
        this.duration = NaN;
        this.readyState = 4;
        this.volume = 1;
        this.src = '';
        this.srcObject = null;
        this.preload = '';
        this.crossOrigin = null;
        this._listeners = {};
        this.playCalls = 0;
        this.playResult = () => Promise.resolve();
    }
    addEventListener(type, fn) { (this._listeners[type] ||= []).push(fn); }
    removeEventListener(type, fn) {
        if (this._listeners[type]) this._listeners[type] = this._listeners[type].filter((f) => f !== fn);
    }
    emit(type, ev = {}) { (this._listeners[type] || []).forEach((f) => f({ type, ...ev })); }
    play() {
        this.playCalls++;
        const r = this.playResult();
        return Promise.resolve(r).then(
            () => { this.paused = false; },
            (e) => { throw e; },
        );
    }
    pause() { this.paused = true; }
    load() {}
    removeAttribute(a) { if (a === 'src') this.src = ''; }
    setAttribute() {}
}

// Builds the sandbox, evaluates app.js in it, and returns the sandbox plus a
// freshly constructed player.
export function loadPlayer({ elements = {} } = {}) {
    const els = {};
    const getEl = (id) => {
        if (id in elements && elements[id] === null) return null;
        return (els[id] ||= makeElement(id));
    };

    const body = makeElement('body');
    const documentStub = {
        body,
        documentElement: makeElement('html'),
        visibilityState: 'visible',
        _listeners: {},
        addEventListener(type, fn) { (this._listeners[type] ||= []).push(fn); },
        removeEventListener() {},
        dispatchEvent(type, ev = {}) { (this._listeners[type] || []).forEach((f) => f({ type, ...ev })); },
        getElementById: getEl,
        querySelector: () => null,
        querySelectorAll: () => [],
        createElement: () => makeElement(),
        cookie: '',
    };

    const storage = () => {
        const m = new Map();
        return {
            getItem: (k) => (m.has(k) ? m.get(k) : null),
            setItem: (k, v) => m.set(k, String(v)),
            removeItem: (k) => m.delete(k),
            clear: () => m.clear(),
        };
    };

    const windowStub = {
        _listeners: {},
        addEventListener(type, fn) { (this._listeners[type] ||= []).push(fn); },
        removeEventListener() {},
        dispatchEvent(type, ev = {}) { (this._listeners[type] || []).forEach((f) => f({ type, ...ev })); },
        GRIMNIR_WEBRTC: { enabled: false },
        matchMedia: () => ({ matches: false, addEventListener() {}, addListener() {} }),
    };

    const sandbox = {
        window: windowStub,
        document: documentStub,
        navigator: { onLine: true, userAgent: 'test' },
        localStorage: storage(),
        sessionStorage: storage(),
        location: { href: 'http://test/', pathname: '/', search: '', hostname: 'test', protocol: 'http:' },
        console: { log() {}, warn() {}, error() {}, debug() {}, info() {} },
        Audio: FakeAudio,
        WebSocket: class { constructor() { this.readyState = 0; } send() {} close() {} },
        fetch: () => Promise.resolve({ ok: true, json: () => Promise.resolve({}), text: () => Promise.resolve('') }),
        Math,
        JSON,
        Promise,
    };

    // Timers and Date are read through getters so node:test's mock.timers, which
    // swaps them on the test realm's globalThis, is visible inside the sandbox.
    // Every id is tracked so dispose() can stop the polling loops app.js starts
    // at load; without that the test process never exits.
    const timeouts = new Set();
    const intervals = new Set();
    Object.defineProperties(sandbox, {
        setTimeout: { get: () => (fn, ms, ...a) => { const id = globalThis.setTimeout(fn, ms, ...a); timeouts.add(id); return id; } },
        clearTimeout: { get: () => (id) => { timeouts.delete(id); return globalThis.clearTimeout(id); } },
        setInterval: { get: () => (fn, ms, ...a) => { const id = globalThis.setInterval(fn, ms, ...a); intervals.add(id); return id; } },
        clearInterval: { get: () => (id) => { intervals.delete(id); return globalThis.clearInterval(id); } },
        Date: { get: () => globalThis.Date },
        requestAnimationFrame: { get: () => (fn) => { const id = globalThis.setTimeout(fn, 16); timeouts.add(id); return id; } },
        cancelAnimationFrame: { get: () => (id) => { timeouts.delete(id); return globalThis.clearTimeout(id); } },
    });
    const dispose = () => {
        timeouts.forEach((id) => globalThis.clearTimeout(id));
        intervals.forEach((id) => globalThis.clearInterval(id));
        timeouts.clear();
        intervals.clear();
    };
    sandbox.window.localStorage = sandbox.localStorage;
    sandbox.window.sessionStorage = sandbox.sessionStorage;
    sandbox.window.document = documentStub;
    sandbox.globalThis = sandbox;
    sandbox.self = sandbox;

    vm.createContext(sandbox);
    // `class X {}` at script top level is a lexical binding and never becomes a
    // property of the global object, so re-export it through `var` to reach it.
    const src = readFileSync(APP_JS, 'utf8') + '\n;var __GlobalPlayer = GlobalPlayer;';
    vm.runInContext(src, sandbox, { filename: 'app.js' });

    const player = new sandbox.__GlobalPlayer();
    return { sandbox, player, document: documentStub, window: windowStub, elements: els, dispose };
}

export { makeElement, FakeAudio };
