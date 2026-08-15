/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

// Loads the real internal/web/static/js/ui-feedback.js in a vm sandbox with a
// DOM stub rich enough to build the confirm modal and a Bootstrap stub that
// records show/hide. Dependency-free, same as harness.mjs.

import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { dirname, resolve } from 'node:path';
import vm from 'node:vm';

const here = dirname(fileURLToPath(import.meta.url));
const UI_FEEDBACK_JS = resolve(here, '../../internal/web/static/js/ui-feedback.js');

// makeNode is a DOM node stub. querySelector resolves against a registry the
// node fills in when innerHTML is assigned, which is how the modal template
// finds its data-role parts.
function makeNode(tag = 'div') {
    const listeners = {};
    const roles = {};
    const node = {
        tagName: tag,
        id: '',
        className: '',
        tabIndex: 0,
        style: { cssText: '' },
        textContent: '',
        children: [],
        _attrs: {},
        get innerHTML() { return this._innerHTML || ''; },
        set innerHTML(html) {
            this._innerHTML = html;
            // Register a stub for each data-role in the markup so querySelector
            // can hand back a node the code can write to.
            const seen = html.match(/data-role="([a-z]+)"/g) || [];
            seen.forEach((m) => {
                const role = m.slice(11, -1);
                roles[role] ||= makeNode('div');
            });
        },
        setAttribute(k, v) { this._attrs[k] = String(v); },
        getAttribute(k) { return k in this._attrs ? this._attrs[k] : null; },
        removeAttribute(k) { delete this._attrs[k]; },
        querySelector(sel) {
            const m = /^\[data-role="([a-z]+)"\]$/.exec(sel);
            if (m) return (roles[m[1]] ||= makeNode('div'));
            return null;
        },
        querySelectorAll() { return []; },
        appendChild(child) { this.children.push(child); return child; },
        removeChild(child) { this.children = this.children.filter((c) => c !== child); },
        remove() {},
        addEventListener(type, fn) { (listeners[type] ||= []).push(fn); },
        removeEventListener(type, fn) {
            if (listeners[type]) listeners[type] = listeners[type].filter((f) => f !== fn);
        },
        dispatchEvent(type, ev = {}) { (listeners[type] || []).forEach((f) => f({ type, ...ev })); },
        listenerCount(type) { return (listeners[type] || []).length; },
        role(name) { return (roles[name] ||= makeNode('div')); },
    };
    return node;
}

// loadUIFeedback evaluates ui-feedback.js. Pass bootstrap: false to test the
// path where the modal library is missing.
export function loadUIFeedback({ bootstrap = true, withShowToast = true } = {}) {
    const byId = {};
    const body = makeNode('body');

    const documentStub = {
        body,
        _listeners: {},
        addEventListener(type, fn) { (this._listeners[type] ||= []).push(fn); },
        removeEventListener() {},
        dispatchEvent(type, ev = {}) { (this._listeners[type] || []).forEach((f) => f({ type, ...ev })); },
        getElementById(id) { return byId[id] || null; },
        createElement: (tag) => makeNode(tag),
        querySelectorAll: () => [],
    };

    // The real code appends the modal to body and finds it again by id on the
    // next call, so mirror that registration here.
    const originalAppend = body.appendChild.bind(body);
    body.appendChild = (child) => {
        if (child && child.id) byId[child.id] = child;
        return originalAppend(child);
    };

    const shown = [];
    const hidden = [];
    const modalInstances = new Map();
    const bootstrapStub = {
        Modal: {
            getOrCreateInstance(el) {
                if (!modalInstances.has(el)) {
                    modalInstances.set(el, {
                        show() { shown.push(el); },
                        // A real hide triggers hidden.bs.modal; tests rely on
                        // that to observe the dismissal path.
                        hide() { hidden.push(el); el.dispatchEvent('hidden.bs.modal'); },
                    });
                }
                return modalInstances.get(el);
            },
        },
    };

    const toasts = [];
    const confirmCalls = [];

    const sandbox = {
        document: documentStub,
        console: { log() {}, warn() {}, error() {} },
        setTimeout: (fn, ms) => globalThis.setTimeout(fn, ms),
        clearTimeout: (id) => globalThis.clearTimeout(id),
        Promise,
        JSON,
        String,
        // A stand-in for the browser dialog, used only when bootstrap is gone.
        confirm: (msg) => { confirmCalls.push(msg); return true; },
    };
    sandbox.window = sandbox;
    sandbox.globalThis = sandbox;
    if (bootstrap) sandbox.bootstrap = bootstrapStub;
    if (withShowToast) sandbox.showToast = (message, type) => toasts.push({ message, type });

    vm.createContext(sandbox);
    const src = readFileSync(UI_FEEDBACK_JS, 'utf8')
        + '\n;var __errorText = errorText;'
        + '\n;var __confirmSeverity = confirmSeverity;'
        + '\n;var __confirmModalElement = confirmModalElement;';
    vm.runInContext(src, sandbox, { filename: 'ui-feedback.js' });

    return {
        sandbox,
        document: documentStub,
        body,
        toasts,
        shown,
        hidden,
        confirmCalls,
        grimnirConfirm: sandbox.grimnirConfirm,
        grimnirNotify: sandbox.grimnirNotify,
        grimnirNotifyError: sandbox.grimnirNotifyError,
        escapeHtml: sandbox.grimnirEscapeHtml,
        errorText: sandbox.__errorText,
        confirmSeverity: sandbox.__confirmSeverity,
        // The modal the code built, once it exists.
        modal() { return byId.grimnirConfirmModal || null; },
        // Builds a form carrying data-confirm and records its resubmissions.
        makeConfirmForm(question, attrs = {}) {
            const form = makeNode('form');
            form.setAttribute('data-confirm', question);
            Object.entries(attrs).forEach(([k, v]) => form.setAttribute(k, v));
            form.dataset = {};
            form.submitted = 0;
            form.requestSubmit = () => {
                form.submitted++;
                // requestSubmit fires submit again, which is what the
                // already-confirmed flag has to survive.
                documentStub.dispatchEvent('submit', {
                    target: form,
                    preventDefault() { form.blockedTwice = true; },
                });
            };
            return form;
        },
        // Fires a submit the way the capturing document listener sees it.
        fireSubmit(form) {
            let prevented = false;
            documentStub.dispatchEvent('submit', {
                target: form,
                preventDefault() { prevented = true; },
            });
            return { prevented };
        },
        // Fires htmx's pre-request confirm hook and reports what the page did.
        fireHtmxConfirm(question, { elt = null } = {}) {
            let prevented = false;
            let issued = 0;
            documentStub.dispatchEvent('htmx:confirm', {
                detail: {
                    question,
                    elt,
                    issueRequest() { issued++; },
                },
                preventDefault() { prevented = true; },
            });
            return { prevented, issuedCount: () => issued };
        },
    };
}

export { makeNode };
