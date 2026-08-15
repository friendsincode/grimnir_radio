/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

import test from 'node:test';
import assert from 'node:assert/strict';
import { loadUIFeedback } from './ui_feedback_harness.mjs';

test('escapeHtml neutralises markup from a response body', () => {
    const ctx = loadUIFeedback();
    assert.equal(
        ctx.escapeHtml('<img src=x onerror="alert(1)">'),
        '&lt;img src=x onerror=&quot;alert(1)&quot;&gt;',
    );
});

test('escapeHtml handles null and undefined', () => {
    const ctx = loadUIFeedback();
    assert.equal(ctx.escapeHtml(null), '');
    assert.equal(ctx.escapeHtml(undefined), '');
});

test('a plain question gets the warning styling', () => {
    const ctx = loadUIFeedback();
    assert.equal(ctx.confirmSeverity('Delete this track?'), 'warning');
});

test('a bulk or irreversible question gets the danger styling', () => {
    const ctx = loadUIFeedback();
    assert.equal(ctx.confirmSeverity('DELETE 3 playlist(s) and ALL tracks'), 'danger');
    assert.equal(ctx.confirmSeverity('This cannot be undone'), 'danger');
    assert.equal(ctx.confirmSeverity('Purge the orphan records?'), 'danger');
});

test('an explicit severity beats the inferred one', () => {
    const ctx = loadUIFeedback();
    assert.equal(ctx.confirmSeverity('Delete this track?', 'danger'), 'danger');
});

test('confirming resolves true and shows the modal', async () => {
    const ctx = loadUIFeedback();
    const answer = ctx.grimnirConfirm('Delete this track?');

    assert.equal(ctx.shown.length, 1, 'the modal should open');
    ctx.modal().role('confirm').dispatchEvent('click');

    assert.equal(await answer, true);
});

test('dismissing resolves false', async () => {
    const ctx = loadUIFeedback();
    const answer = ctx.grimnirConfirm('Delete this track?');

    // Escape, the backdrop and the close button all end in hidden.bs.modal.
    ctx.modal().dispatchEvent('hidden.bs.modal');

    assert.equal(await answer, false);
});

test('the modal is built once and reused', async () => {
    const ctx = loadUIFeedback();

    const first = ctx.grimnirConfirm('Delete one?');
    const el = ctx.modal();
    el.role('confirm').dispatchEvent('click');
    await first;

    const second = ctx.grimnirConfirm('Delete two?');
    assert.equal(ctx.modal(), el, 'a second confirm should reuse the same element');
    ctx.modal().role('confirm').dispatchEvent('click');
    await second;
});

test('a reused modal does not answer twice', async () => {
    const ctx = loadUIFeedback();

    const first = ctx.grimnirConfirm('Delete one?');
    ctx.modal().role('confirm').dispatchEvent('click');
    assert.equal(await first, true);

    // The click listener from the first round must be gone, otherwise a later
    // dismissal would resolve the wrong answer.
    const second = ctx.grimnirConfirm('Delete two?');
    ctx.modal().dispatchEvent('hidden.bs.modal');
    assert.equal(await second, false);
});

test('the question text is escaped into the modal body', async () => {
    const ctx = loadUIFeedback();
    const answer = ctx.grimnirConfirm('Delete <b>everything</b>?');

    assert.equal(ctx.modal().role('message').innerHTML, 'Delete &lt;b&gt;everything&lt;/b&gt;?');
    ctx.modal().dispatchEvent('hidden.bs.modal');
    await answer;
});

test('without bootstrap it falls back to the browser dialog', async () => {
    const ctx = loadUIFeedback({ bootstrap: false });

    const answer = await ctx.grimnirConfirm('Delete this track?');

    assert.equal(answer, true);
    assert.deepEqual(ctx.confirmCalls, ['Delete this track?'], 'the action must not be silently dropped');
});

test('hx-confirm is routed through the modal, not the browser dialog', async () => {
    const ctx = loadUIFeedback();
    const hook = ctx.fireHtmxConfirm('Delete this playlist?');

    assert.equal(hook.prevented, true, 'htmx must not show its own dialog');
    assert.equal(hook.issuedCount(), 0, 'nothing should fire before the user answers');

    ctx.modal().role('confirm').dispatchEvent('click');
    await Promise.resolve();
    await Promise.resolve();

    assert.equal(hook.issuedCount(), 1);
});

test('cancelling an hx-confirm never issues the request', async () => {
    const ctx = loadUIFeedback();
    const hook = ctx.fireHtmxConfirm('Delete this playlist?');

    ctx.modal().dispatchEvent('hidden.bs.modal');
    await Promise.resolve();
    await Promise.resolve();

    assert.equal(hook.issuedCount(), 0);
});

test('an element with no question is left alone', () => {
    const ctx = loadUIFeedback();
    const hook = ctx.fireHtmxConfirm(null);

    assert.equal(hook.prevented, false, 'requests without hx-confirm must not be intercepted');
});

test('a data-confirm form does not submit until the user agrees', async () => {
    const ctx = loadUIFeedback();
    const form = ctx.makeConfirmForm('Delete this recording permanently?');

    const { prevented } = ctx.fireSubmit(form);
    assert.equal(prevented, true, 'the submit must be held');
    assert.equal(form.submitted, 0);

    ctx.modal().role('confirm').dispatchEvent('click');
    await Promise.resolve();
    await Promise.resolve();

    assert.equal(form.submitted, 1);
});

test('cancelling a data-confirm form never submits it', async () => {
    const ctx = loadUIFeedback();
    const form = ctx.makeConfirmForm('Delete this recording permanently?');

    ctx.fireSubmit(form);
    ctx.modal().dispatchEvent('hidden.bs.modal');
    await Promise.resolve();
    await Promise.resolve();

    assert.equal(form.submitted, 0);
});

test('the confirmed resubmission is not intercepted again', async () => {
    const ctx = loadUIFeedback();
    const form = ctx.makeConfirmForm('Purge selected duplicate files?');

    ctx.fireSubmit(form);
    ctx.modal().role('confirm').dispatchEvent('click');
    await Promise.resolve();
    await Promise.resolve();

    assert.equal(form.submitted, 1);
    assert.notEqual(form.blockedTwice, true, 'a confirmed form must not ask again and stall');
});

test('a form without data-confirm submits untouched', () => {
    const ctx = loadUIFeedback();
    const form = ctx.makeConfirmForm('');
    form.removeAttribute('data-confirm');

    assert.equal(ctx.fireSubmit(form).prevented, false);
});

test('notify escapes the message before it reaches the toast', () => {
    const ctx = loadUIFeedback();
    ctx.grimnirNotify('<script>steal()</script>', 'error');

    assert.equal(ctx.toasts.length, 1);
    assert.equal(ctx.toasts[0].type, 'error');
    assert.match(ctx.toasts[0].message, /&lt;script&gt;/);
});

test('notifyError reads a response body and shows it as a toast', async () => {
    const ctx = loadUIFeedback();
    await ctx.grimnirNotifyError({ status: 500, text: () => Promise.resolve('disk full') });

    assert.equal(ctx.toasts[0].message, 'disk full');
    assert.equal(ctx.toasts[0].type, 'error');
});

test('notifyError prefers a JSON error field over the raw body', async () => {
    const ctx = loadUIFeedback();
    await ctx.grimnirNotifyError({ status: 400, text: () => Promise.resolve('{"error":"playlist is in use"}') });

    assert.equal(ctx.toasts[0].message, 'playlist is in use');
});

test('notifyError falls back to the status when the body is empty', async () => {
    const ctx = loadUIFeedback();
    await ctx.grimnirNotifyError({ status: 502, text: () => Promise.resolve('   ') }, 'Bulk delete failed');

    assert.equal(ctx.toasts[0].message, 'Bulk delete failed (HTTP 502)');
});

test('notifyError survives a body that cannot be read', async () => {
    const ctx = loadUIFeedback();
    await ctx.grimnirNotifyError({ status: 500, text: () => Promise.reject(new Error('aborted')) }, 'Delete failed');

    assert.equal(ctx.toasts[0].message, 'Delete failed');
});

test('notifyError accepts a thrown Error', async () => {
    const ctx = loadUIFeedback();
    await ctx.grimnirNotifyError(new Error('NetworkError when attempting to fetch'));

    assert.equal(ctx.toasts[0].message, 'NetworkError when attempting to fetch');
});

test('an HTML error body cannot inject markup through the toast', async () => {
    const ctx = loadUIFeedback();
    await ctx.grimnirNotifyError({ status: 500, text: () => Promise.resolve('<img src=x onerror=alert(1)>') });

    assert.doesNotMatch(ctx.toasts[0].message, /<img/, 'the raw tag must never reach innerHTML');
});
