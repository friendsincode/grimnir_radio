/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

// Loads the real internal/web/static/js/dirty-form.js inside a vm sandbox with
// enough of a DOM to drive form events. Same approach as harness.mjs: no
// package.json, no node_modules, just node:test and node:vm.

import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { dirname, resolve } from 'node:path';
import vm from 'node:vm';

const here = dirname(fileURLToPath(import.meta.url));
const DIRTY_FORM_JS = resolve(here, '../../internal/web/static/js/dirty-form.js');

// makeField builds one form control. Pass checked for a checkbox or radio, and
// options for a select.
export function makeField({ name, value = '', type = 'text', checked = false, multiple = false, options = null }) {
    const field = { name, value, type, checked, multiple };
    if (options) {
        field.options = options.map((o) =>
            (typeof o === 'string' ? { value: o, selected: false } : { value: o.value, selected: !!o.selected }));
    }
    return field;
}

// makeForm builds a form whose elements are the given fields. Events do not
// bubble from the fields in this stub, so tests edit a value and then fire the
// event on the form itself, which is what a real bubbling input event looks
// like to a listener bound on the form.
export function makeForm(fields = [], attrs = {}) {
    const listeners = {};
    return {
        elements: fields,
        dataset: {},
        ...attrs,
        addEventListener(type, fn) { (listeners[type] ||= []).push(fn); },
        removeEventListener(type, fn) {
            if (listeners[type]) listeners[type] = listeners[type].filter((f) => f !== fn);
        },
        dispatchEvent(type, ev = {}) { (listeners[type] || []).forEach((f) => f({ type, ...ev })); },
        listenerCount(type) { return (listeners[type] || []).length; },
    };
}

// loadDirtyForm evaluates dirty-form.js and hands back its internals plus the
// document and window stubs, so a test can fire DOMContentLoaded or
// beforeunload exactly as a browser would.
export function loadDirtyForm({ forms = [] } = {}) {
    const documentStub = {
        _listeners: {},
        addEventListener(type, fn) { (this._listeners[type] ||= []).push(fn); },
        removeEventListener() {},
        dispatchEvent(type, ev = {}) { (this._listeners[type] || []).forEach((f) => f({ type, ...ev })); },
        querySelectorAll(sel) { return sel === 'form[data-dirty-guard]' ? forms : []; },
    };

    const windowStub = {
        _listeners: {},
        addEventListener(type, fn) { (this._listeners[type] ||= []).push(fn); },
        removeEventListener() {},
        dispatchEvent(type, ev = {}) { (this._listeners[type] || []).forEach((f) => f({ type, ...ev })); },
    };

    const sandbox = {
        window: windowStub,
        document: documentStub,
        console: { log() {}, warn() {}, error() {} },
    };
    sandbox.globalThis = sandbox;

    vm.createContext(sandbox);
    // Top-level function and class declarations in a script do become globals,
    // but re-exporting through var keeps this robust if the file ever moves to
    // const arrow functions.
    const src = readFileSync(DIRTY_FORM_JS, 'utf8')
        + '\n;var __serializeForm = serializeForm;'
        + '\n;var __guardForm = guardForm;'
        + '\n;var __anyFormDirty = anyFormDirty;'
        + '\n;var __initDirtyFormGuards = initDirtyFormGuards;'
        + '\n;var __DirtyFormGuard = DirtyFormGuard;';
    vm.runInContext(src, sandbox, { filename: 'dirty-form.js' });

    return {
        sandbox,
        document: documentStub,
        window: windowStub,
        serializeForm: sandbox.__serializeForm,
        guardForm: sandbox.__guardForm,
        anyFormDirty: sandbox.__anyFormDirty,
        initDirtyFormGuards: sandbox.__initDirtyFormGuards,
        DirtyFormGuard: sandbox.__DirtyFormGuard,
        // Fires beforeunload the way a browser does and reports whether the
        // page tried to block the navigation.
        fireBeforeUnload() {
            let prevented = false;
            const ev = {
                type: 'beforeunload',
                preventDefault() { prevented = true; },
                returnValue: undefined,
            };
            (windowStub._listeners.beforeunload || []).forEach((f) => f(ev));
            return { prevented, returnValue: ev.returnValue };
        },
    };
}
