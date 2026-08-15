/**
 * Grimnir Radio - Shared confirm and feedback helpers
 *
 * Three patterns lived side by side: hx-confirm (browser dialog), raw
 * confirm(), and alert() dumping backend text at the user. This file gives all
 * of them one surface.
 *
 *   const ok = await grimnirConfirm({ message: 'Delete this track?' });
 *   grimnirNotify('Track deleted', 'success');
 *   grimnirNotifyError(response);            // reads the body, escapes it
 *
 * Every hx-confirm on the page is routed through the same modal by the
 * htmx:confirm handler at the bottom, so those call sites need no edit.
 */

// escapeHtml makes server text safe to place in innerHTML. showToast in app.js
// interpolates its message, and the error paths feed it raw response bodies, so
// escaping is not optional here.
function escapeHtml(value) {
    return String(value == null ? '' : value)
        .replace(/&/g, '&amp;')
        .replace(/</g, '&lt;')
        .replace(/>/g, '&gt;')
        .replace(/"/g, '&quot;')
        .replace(/'/g, '&#39;');
}

// SEVERE_PATTERN marks copy that describes an irreversible or bulk action.
// Those get the red button and a wording that names the consequence.
const SEVERE_PATTERN = /\ball\b|permanent|cannot be undone|irreversible|\bpurge\b|\bwipe\b/i;

// confirmSeverity picks the button styling from the question, unless the caller
// or the element said otherwise.
function confirmSeverity(question, override) {
    if (override) return override;
    return SEVERE_PATTERN.test(question || '') ? 'danger' : 'warning';
}

const CONFIRM_MODAL_ID = 'grimnirConfirmModal';

// confirmModalElement builds the modal once and reuses it.
function confirmModalElement() {
    let el = document.getElementById(CONFIRM_MODAL_ID);
    if (el) return el;

    el = document.createElement('div');
    el.id = CONFIRM_MODAL_ID;
    el.className = 'modal fade';
    el.tabIndex = -1;
    el.setAttribute('aria-hidden', 'true');
    el.innerHTML = `
        <div class="modal-dialog modal-dialog-centered">
          <div class="modal-content">
            <div class="modal-header">
              <h5 class="modal-title" data-role="title"></h5>
              <button type="button" class="btn-close" data-bs-dismiss="modal" aria-label="Close"></button>
            </div>
            <div class="modal-body" data-role="message"></div>
            <div class="modal-footer">
              <button type="button" class="btn btn-secondary" data-bs-dismiss="modal" data-role="cancel"></button>
              <button type="button" class="btn" data-role="confirm"></button>
            </div>
          </div>
        </div>`;
    document.body.appendChild(el);
    return el;
}

// grimnirConfirm asks the question in a modal and resolves true only if the
// user picks the confirm button. Dismissing, the close button, Escape and the
// backdrop all resolve false.
function grimnirConfirm(options) {
    const opts = typeof options === 'string' ? { message: options } : (options || {});
    const message = opts.message || 'Are you sure?';
    const severity = confirmSeverity(message, opts.severity);
    const title = opts.title || (severity === 'danger' ? 'This cannot be undone' : 'Please confirm');
    const confirmLabel = opts.confirmLabel || (severity === 'danger' ? 'Delete' : 'Continue');
    const cancelLabel = opts.cancelLabel || 'Cancel';

    // Without Bootstrap the modal cannot open, and silently resolving false
    // would swallow the action. Fall back to the browser dialog instead.
    if (typeof bootstrap === 'undefined' || !bootstrap.Modal) {
        return Promise.resolve(confirm(message));
    }

    const el = confirmModalElement();
    el.querySelector('[data-role="title"]').textContent = title;
    el.querySelector('[data-role="message"]').innerHTML = escapeHtml(message).replace(/\n/g, '<br>');
    el.querySelector('[data-role="cancel"]').textContent = cancelLabel;

    const confirmBtn = el.querySelector('[data-role="confirm"]');
    confirmBtn.textContent = confirmLabel;
    confirmBtn.className = `btn btn-${severity === 'danger' ? 'danger' : 'warning'}`;

    const modal = bootstrap.Modal.getOrCreateInstance(el);

    return new Promise((resolve) => {
        let answered = false;

        const onConfirm = () => {
            answered = true;
            modal.hide();
            resolve(true);
        };
        // hidden.bs.modal covers every dismissal path, so a user who presses
        // Escape or clicks the backdrop gets the same answer as Cancel.
        const onHidden = () => {
            confirmBtn.removeEventListener('click', onConfirm);
            el.removeEventListener('hidden.bs.modal', onHidden);
            if (!answered) resolve(false);
        };

        confirmBtn.addEventListener('click', onConfirm);
        el.addEventListener('hidden.bs.modal', onHidden);
        modal.show();
    });
}

// grimnirNotify is the one success/error surface. It delegates to showToast
// when app.js is present and escapes the message either way.
function grimnirNotify(message, type = 'info') {
    const safe = escapeHtml(message);
    if (typeof showToast === 'function') {
        showToast(safe, type);
        return;
    }
    // app.js absent (public pages). Say it plainly rather than dropping it.
    const container = document.getElementById('toastContainer') || document.body;
    const note = document.createElement('div');
    note.className = `alert alert-${type === 'error' ? 'danger' : type} position-fixed`;
    note.style.cssText = 'top: 20px; right: 20px; z-index: 9999; min-width: 200px;';
    note.innerHTML = safe;
    container.appendChild(note);
    setTimeout(() => note.remove(), 5000);
}

// grimnirNotifyError replaces alert('Error: ' + text). It accepts a Response, an
// Error, or a string, and never puts a raw body into the page unescaped.
function grimnirNotifyError(source, fallback = 'Something went wrong') {
    if (source && typeof source.text === 'function') {
        return Promise.resolve(source.text())
            .then((text) => grimnirNotify(errorText(text, source, fallback), 'error'))
            .catch(() => grimnirNotify(fallback, 'error'));
    }
    grimnirNotify(errorText(source, null, fallback), 'error');
    return Promise.resolve();
}

// errorText pulls the most useful sentence out of whatever the caller had.
// A JSON body's error/message field beats the raw body; a raw body beats the
// status line; the fallback beats an empty string.
function errorText(value, response, fallback) {
    if (value && typeof value === 'object' && value.message) return value.message;

    const raw = typeof value === 'string' ? value.trim() : '';
    if (raw) {
        try {
            const parsed = JSON.parse(raw);
            if (parsed && (parsed.error || parsed.message)) return parsed.error || parsed.message;
        } catch {
            // Not JSON; the body itself is the message.
        }
        return raw;
    }

    if (response && response.status) return `${fallback} (HTTP ${response.status})`;
    return fallback;
}

// data-confirm replaces inline onsubmit="return confirm(...)". Those cannot go
// async, so the submit is stopped, the question asked, and the form submitted
// again with the guard released.
const CONFIRMED_FLAG = 'grimnirConfirmed';

function guardSubmit(e) {
    const form = e.target;
    if (!form || !form.getAttribute) return;

    const question = form.getAttribute('data-confirm');
    if (!question) return;

    // Second pass, after the user agreed.
    if (form.dataset && form.dataset[CONFIRMED_FLAG] === 'true') {
        delete form.dataset[CONFIRMED_FLAG];
        return;
    }

    e.preventDefault();
    grimnirConfirm({
        message: question,
        severity: form.getAttribute('data-confirm-severity'),
        confirmLabel: form.getAttribute('data-confirm-label'),
    }).then((ok) => {
        if (!ok) return;
        if (form.dataset) form.dataset[CONFIRMED_FLAG] = 'true';
        if (typeof form.requestSubmit === 'function') {
            form.requestSubmit();
        } else {
            form.submit();
        }
    });
}

// Route every hx-confirm through the same modal. htmx fires this before it
// would show the browser dialog; preventDefault stops that and issueRequest
// resumes once the user agrees.
if (typeof document !== 'undefined' && document.addEventListener) {
    document.addEventListener('submit', guardSubmit, true);

    document.addEventListener('htmx:confirm', (e) => {
        if (!e.detail || !e.detail.question) return;
        e.preventDefault();

        const el = e.detail.elt;
        const severity = el && el.getAttribute ? el.getAttribute('data-confirm-severity') : null;
        const label = el && el.getAttribute ? el.getAttribute('data-confirm-label') : null;

        grimnirConfirm({ message: e.detail.question, severity, confirmLabel: label })
            .then((ok) => { if (ok) e.detail.issueRequest(true); });
    });
}

if (typeof window !== 'undefined') {
    window.grimnirConfirm = grimnirConfirm;
    window.grimnirNotify = grimnirNotify;
    window.grimnirNotifyError = grimnirNotifyError;
    window.grimnirEscapeHtml = escapeHtml;
}
