/**
 * Landing Page Editor JavaScript
 * Handles the visual editor functionality for station landing pages.
 */

(function() {
    'use strict';

    // Editor state
    let config = {};
    let isDirty = false;
    let autoSaveTimer = null;

    // Initialize editor when DOM is ready
    document.addEventListener('DOMContentLoaded', function() {
        initEditor();
    });

    function initEditor() {
        // Load initial config from page
        const configEl = document.getElementById('landing-config');
        if (configEl) {
            try {
                config = JSON.parse(configEl.textContent || '{}');
            } catch (e) {
                console.error('Failed to parse landing page config:', e);
                config = {};
            }
        }

        // Setup auto-save
        setupAutoSave();

        // Setup preview communication
        setupPreviewMessaging();

        // Setup widget drag-and-drop
        setupDragAndDrop();

        console.log('Landing page editor initialized');
    }

    function setupAutoSave() {
        // Auto-save every 30 seconds if dirty
        setInterval(function() {
            if (isDirty) {
                saveDraft();
            }
        }, 30000);
    }

    function setupPreviewMessaging() {
        const previewFrame = document.getElementById('preview-frame');
        if (!previewFrame) return;

        // Listen for messages from preview iframe
        window.addEventListener('message', function(event) {
            if (event.data && event.data.type === 'preview-ready') {
                updatePreview();
            }
        });
    }

    function setupDragAndDrop() {
        // Initialize sortable for widget list if available
        const widgetList = document.getElementById('widget-list');
        if (widgetList && window.Sortable) {
            new Sortable(widgetList, {
                animation: 150,
                handle: '.drag-handle',
                onEnd: function() {
                    markDirty();
                    updatePreview();
                }
            });
        }
    }

    function markDirty() {
        isDirty = true;
        const saveBtn = document.getElementById('save-btn');
        if (saveBtn) {
            saveBtn.classList.add('btn-warning');
            saveBtn.classList.remove('btn-outline-secondary');
        }
    }

    function markClean() {
        isDirty = false;
        const saveBtn = document.getElementById('save-btn');
        if (saveBtn) {
            saveBtn.classList.remove('btn-warning');
            saveBtn.classList.add('btn-outline-secondary');
        }
    }

    function updatePreview() {
        const previewFrame = document.getElementById('preview-frame');
        if (previewFrame && previewFrame.contentWindow) {
            previewFrame.contentWindow.postMessage({
                type: 'config-update',
                config: config
            }, '*');
        }
    }

    function saveDraft() {
        const saveBtn = document.getElementById('save-btn');
        if (saveBtn) {
            saveBtn.disabled = true;
            saveBtn.innerHTML = '<i class="bi bi-hourglass-split me-1"></i>Saving...';
        }

        fetch('/dashboard/station/landing-page/save', {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json'
            },
            body: JSON.stringify({ config: config })
        })
        .then(response => response.json())
        .then(data => {
            if (data.status === 'saved') {
                markClean();
                showToast('Draft saved', 'success');
            } else {
                showToast('Failed to save draft', 'error');
            }
        })
        .catch(err => {
            console.error('Save failed:', err);
            showToast('Failed to save draft', 'error');
        })
        .finally(() => {
            if (saveBtn) {
                saveBtn.disabled = false;
                saveBtn.innerHTML = '<i class="bi bi-save me-1"></i>Save Draft';
            }
        });
    }

    function publish() {
        if (!confirm('Publish this landing page? It will be visible to the public.')) {
            return;
        }

        const publishBtn = document.getElementById('publish-btn');
        if (publishBtn) {
            publishBtn.disabled = true;
            publishBtn.innerHTML = '<i class="bi bi-hourglass-split me-1"></i>Publishing...';
        }

        fetch('/dashboard/station/landing-page/publish', {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json'
            },
            body: JSON.stringify({})
        })
        .then(response => response.json())
        .then(data => {
            if (data.status === 'published') {
                markClean();
                showToast('Landing page published!', 'success');
            } else {
                showToast('Failed to publish', 'error');
            }
        })
        .catch(err => {
            console.error('Publish failed:', err);
            showToast('Failed to publish', 'error');
        })
        .finally(() => {
            if (publishBtn) {
                publishBtn.disabled = false;
                publishBtn.innerHTML = '<i class="bi bi-globe me-1"></i>Publish';
            }
        });
    }

    function showToast(message, type) {
        // Simple toast notification
        const toast = document.createElement('div');
        toast.className = `alert alert-${type === 'success' ? 'success' : 'danger'} position-fixed`;
        toast.style.cssText = 'top: 20px; right: 20px; z-index: 9999; min-width: 200px;';
        toast.innerHTML = `<i class="bi bi-${type === 'success' ? 'check-circle' : 'exclamation-triangle'} me-2"></i>${message}`;
        document.body.appendChild(toast);
        setTimeout(() => toast.remove(), 3000);
    }

    // Expose functions globally
    window.LandingPageEditor = {
        saveDraft: saveDraft,
        publish: publish,
        updatePreview: updatePreview,
        markDirty: markDirty
    };
})();
