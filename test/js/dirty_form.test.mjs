/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

import test from 'node:test';
import assert from 'node:assert/strict';
import { loadDirtyForm, makeForm, makeField } from './form_harness.mjs';

// A stand-in for the station settings form: a few text fields, a checkbox and a
// select, which is the shape all three guarded forms share.
function settingsForm() {
    return makeForm([
        makeField({ name: 'name', value: 'RLM Radio' }),
        makeField({ name: 'description', value: 'the station' }),
        makeField({ name: 'public', type: 'checkbox', checked: true }),
        makeField({ name: 'timezone', value: 'America/Chicago', options: ['America/Chicago', 'UTC'] }),
    ]);
}

test('an untouched form does not block navigation', () => {
    const form = settingsForm();
    const ctx = loadDirtyForm({ forms: [form] });
    ctx.initDirtyFormGuards();

    assert.equal(ctx.anyFormDirty(), false);
    assert.equal(ctx.fireBeforeUnload().prevented, false);
});

test('an edited field blocks navigation', () => {
    const form = settingsForm();
    const ctx = loadDirtyForm({ forms: [form] });
    ctx.initDirtyFormGuards();

    form.elements[0].value = 'RLM Radio Two';
    form.dispatchEvent('input');

    assert.equal(ctx.anyFormDirty(), true);
    const { prevented, returnValue } = ctx.fireBeforeUnload();
    assert.equal(prevented, true);
    assert.equal(returnValue, '');
});

test('typing a value back to its original clears the warning', () => {
    const form = settingsForm();
    const ctx = loadDirtyForm({ forms: [form] });
    ctx.initDirtyFormGuards();

    form.elements[0].value = 'RLM Radio Two';
    form.dispatchEvent('input');
    assert.equal(ctx.anyFormDirty(), true);

    form.elements[0].value = 'RLM Radio';
    form.dispatchEvent('input');
    assert.equal(ctx.anyFormDirty(), false, 'restoring the value should not leave the form dirty');
});

test('toggling a checkbox counts as an edit', () => {
    const form = settingsForm();
    const ctx = loadDirtyForm({ forms: [form] });
    ctx.initDirtyFormGuards();

    form.elements[2].checked = false;
    form.dispatchEvent('change');

    assert.equal(ctx.anyFormDirty(), true);
});

test('a successful htmx save re-baselines the form', () => {
    const form = settingsForm();
    const ctx = loadDirtyForm({ forms: [form] });
    ctx.initDirtyFormGuards();

    form.elements[1].value = 'a new description';
    form.dispatchEvent('input');
    assert.equal(ctx.anyFormDirty(), true);

    form.dispatchEvent('htmx:afterRequest', { detail: { successful: true } });

    assert.equal(ctx.anyFormDirty(), false);
    assert.equal(ctx.fireBeforeUnload().prevented, false);
});

test('a failed htmx save leaves the changes guarded', () => {
    const form = settingsForm();
    const ctx = loadDirtyForm({ forms: [form] });
    ctx.initDirtyFormGuards();

    form.elements[1].value = 'a new description';
    form.dispatchEvent('input');
    form.dispatchEvent('htmx:afterRequest', { detail: { successful: false } });

    assert.equal(ctx.anyFormDirty(), true, 'a rejected save must not clear the unsaved-changes guard');
});

test('editing again after a save is guarded once more', () => {
    const form = settingsForm();
    const ctx = loadDirtyForm({ forms: [form] });
    ctx.initDirtyFormGuards();

    form.elements[0].value = 'first edit';
    form.dispatchEvent('input');
    form.dispatchEvent('htmx:afterRequest', { detail: { successful: true } });
    assert.equal(ctx.anyFormDirty(), false);

    form.elements[0].value = 'second edit';
    form.dispatchEvent('input');
    assert.equal(ctx.anyFormDirty(), true);
});

test('a native submit releases the guard', () => {
    const form = settingsForm();
    const ctx = loadDirtyForm({ forms: [form] });
    ctx.initDirtyFormGuards();

    form.elements[0].value = 'changed';
    form.dispatchEvent('input');
    form.dispatchEvent('submit');

    assert.equal(ctx.anyFormDirty(), false, 'submitting is a deliberate navigation');
    assert.equal(ctx.fireBeforeUnload().prevented, false);
});

test('one dirty form among several blocks navigation', () => {
    const clean = settingsForm();
    const dirty = makeForm([makeField({ name: 'title', value: 'Morning Clock' })]);
    const ctx = loadDirtyForm({ forms: [clean, dirty] });
    ctx.initDirtyFormGuards();

    dirty.elements[0].value = 'Evening Clock';
    dirty.dispatchEvent('input');

    assert.equal(ctx.anyFormDirty(), true);
});

test('a multi-select tracks every selected option', () => {
    const form = makeForm([
        makeField({
            name: 'days',
            multiple: true,
            options: [{ value: 'mon', selected: true }, { value: 'tue', selected: false }],
        }),
    ]);
    const ctx = loadDirtyForm({ forms: [form] });
    ctx.initDirtyFormGuards();

    assert.equal(ctx.anyFormDirty(), false);
    form.elements[0].options[1].selected = true;
    form.dispatchEvent('change');
    assert.equal(ctx.anyFormDirty(), true);
});

test('buttons and file inputs are not treated as state', () => {
    const ctx = loadDirtyForm();
    const form = makeForm([
        makeField({ name: 'save', type: 'submit', value: 'Save' }),
        makeField({ name: 'upload', type: 'file', value: '' }),
        makeField({ name: 'name', value: 'kept' }),
    ]);

    assert.equal(ctx.serializeForm(form), 'name=kept');
});

test('an unnamed control is ignored', () => {
    const ctx = loadDirtyForm();
    const form = makeForm([makeField({ name: '', value: 'x' }), makeField({ name: 'y', value: '1' })]);

    assert.equal(ctx.serializeForm(form), 'y=1');
});

test('DOMContentLoaded attaches the guards', () => {
    const form = settingsForm();
    const ctx = loadDirtyForm({ forms: [form] });

    assert.equal(form.listenerCount('input'), 0, 'nothing should attach before the DOM is ready');
    ctx.document.dispatchEvent('DOMContentLoaded');
    assert.equal(form.listenerCount('input'), 1);
});

test('a form is not guarded twice when init runs again', () => {
    const form = settingsForm();
    const ctx = loadDirtyForm({ forms: [form] });

    ctx.initDirtyFormGuards();
    ctx.initDirtyFormGuards();

    assert.equal(form.listenerCount('input'), 1, 'htmx swaps can re-run init; the form must attach once');
});

// The smart-block and clock forms hydrate their fields from JS and fire
// synthetic change events while doing it. isTrusted is false for those, and a
// form nobody typed into must never prompt.
test('JS hydration does not arm the guard', () => {
    const form = settingsForm();
    const ctx = loadDirtyForm({ forms: [form] });
    ctx.initDirtyFormGuards();

    form.elements[0].value = 'restored from existingRules';
    form.dispatchEvent('change', { isTrusted: false });

    assert.equal(ctx.anyFormDirty(), false, 'a synthetic change event is not a user edit');
    assert.equal(ctx.fireBeforeUnload().prevented, false);
});

test('a user edit after hydration still arms the guard', () => {
    const form = settingsForm();
    const ctx = loadDirtyForm({ forms: [form] });
    ctx.initDirtyFormGuards();

    form.elements[0].value = 'hydrated';
    form.dispatchEvent('change', { isTrusted: false });
    ctx.window.dispatchEvent('load');
    assert.equal(ctx.anyFormDirty(), false);

    form.elements[0].value = 'typed by hand';
    form.dispatchEvent('input', { isTrusted: true });
    assert.equal(ctx.anyFormDirty(), true);
});

test('load re-snapshots an untouched form so a revert reads clean', () => {
    const form = settingsForm();
    const ctx = loadDirtyForm({ forms: [form] });
    ctx.initDirtyFormGuards();

    // Hydration lands after DOMContentLoaded.
    form.elements[0].value = 'hydrated late';
    ctx.window.dispatchEvent('load');

    form.elements[0].value = 'user edit';
    form.dispatchEvent('input', { isTrusted: true });
    assert.equal(ctx.anyFormDirty(), true);

    form.elements[0].value = 'hydrated late';
    form.dispatchEvent('input', { isTrusted: true });
    assert.equal(ctx.anyFormDirty(), false, 'the post-hydration value is the one to compare against');
});

test('load does not discard an edit already in progress', () => {
    const form = settingsForm();
    const ctx = loadDirtyForm({ forms: [form] });
    ctx.initDirtyFormGuards();

    form.elements[0].value = 'typed before load fired';
    form.dispatchEvent('input', { isTrusted: true });
    ctx.window.dispatchEvent('load');

    assert.equal(ctx.anyFormDirty(), true, 'a slow page load must not swallow a real edit');
});

test('detach stops the form from blocking navigation', () => {
    const form = settingsForm();
    const ctx = loadDirtyForm({ forms: [form] });
    const guard = ctx.guardForm(form);

    form.elements[0].value = 'changed';
    form.dispatchEvent('input');
    assert.equal(ctx.anyFormDirty(), true);

    guard.detach();
    assert.equal(ctx.anyFormDirty(), false);
    assert.equal(form.listenerCount('input'), 0);
});
