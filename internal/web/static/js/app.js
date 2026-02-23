/**
 * Grimnir Radio - Application JavaScript
 */

// Theme Management
function setTheme(theme) {
    document.documentElement.setAttribute('data-bs-theme', theme);
    localStorage.setItem('grimnir-theme', theme);

    // Update theme CSS link
    const themeLink = document.querySelector('link[href*="/static/css/themes/"]');
    if (themeLink) {
        themeLink.href = `/static/css/themes/${theme}.css`;
    }

    // Close the theme modal if it's open
    const themeModal = document.getElementById('themeModal');
    if (themeModal) {
        const bsModal = bootstrap.Modal.getInstance(themeModal);
        if (bsModal) {
            // Wait for modal to fully hide before cleanup
            themeModal.addEventListener('hidden.bs.modal', function cleanup() {
                themeModal.removeEventListener('hidden.bs.modal', cleanup);
                // Clean up any stale modal backdrops after modal is fully hidden
                document.querySelectorAll('.modal-backdrop').forEach(el => el.remove());
                document.body.classList.remove('modal-open');
                document.body.style.removeProperty('overflow');
                document.body.style.removeProperty('padding-right');
            }, { once: true });
            bsModal.hide();
        } else {
            // No modal instance, just clean up directly
            document.querySelectorAll('.modal-backdrop').forEach(el => el.remove());
            document.body.classList.remove('modal-open');
            document.body.style.removeProperty('overflow');
            document.body.style.removeProperty('padding-right');
        }
    }

    // Save to server for cross-device sync (if logged in)
    if (window.htmx && window.GRIMNIR_WS_TOKEN) {
        htmx.ajax('POST', '/api/v1/preferences/theme', {
            values: { theme: theme },
            swap: 'none',
            headers: { 'Authorization': 'Bearer ' + window.GRIMNIR_WS_TOKEN }
        }).catch(() => {
            // Ignore errors - theme is saved locally anyway
        });
    }
}

// Initialize theme from localStorage
(function() {
    const savedTheme = localStorage.getItem('grimnir-theme');
    if (savedTheme) {
        setTheme(savedTheme);
    }
})();

// WebSocket Connection Manager
class GrimnirWebSocket {
    constructor() {
        this.ws = null;
        this.reconnectAttempts = 0;
        this.maxReconnectAttempts = 10;
        this.reconnectDelay = 5000;
        this.listeners = new Map();
    }

    connect(types = ['schedule_update', 'now_playing', 'health']) {
        const token = this.getToken();
        if (!token) {
            console.log('No auth token, skipping WebSocket connection');
            return;
        }

        const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
        const url = `${protocol}//${window.location.host}/api/v1/events?types=${types.join(',')}&token=${token}`;

        this.ws = new WebSocket(url);

        this.ws.onopen = () => {
            console.log('WebSocket connected');
            this.reconnectAttempts = 0;
            this.dispatch('connected', {});
        };

        this.ws.onmessage = (event) => {
            try {
                const data = JSON.parse(event.data);
                if (data.type === 'ping') {
                    return; // Ignore keepalive
                }
                this.dispatch(data.type, data.payload);
            } catch (e) {
                console.error('WebSocket message parse error:', e);
            }
        };

        this.ws.onclose = (event) => {
            console.log('WebSocket closed:', event.code, event.reason);
            this.dispatch('disconnected', { code: event.code, reason: event.reason });
            this.scheduleReconnect();
        };

        this.ws.onerror = (error) => {
            console.error('WebSocket error:', error);
            this.dispatch('error', { error });
        };
    }

    scheduleReconnect() {
        if (this.reconnectAttempts >= this.maxReconnectAttempts) {
            console.log('Max reconnection attempts reached');
            return;
        }

        this.reconnectAttempts++;
        const delay = this.reconnectDelay * Math.pow(1.5, this.reconnectAttempts - 1);
        console.log(`Reconnecting in ${delay}ms (attempt ${this.reconnectAttempts})`);

        setTimeout(() => this.connect(), delay);
    }

    on(event, callback) {
        if (!this.listeners.has(event)) {
            this.listeners.set(event, []);
        }
        this.listeners.get(event).push(callback);
    }

    off(event, callback) {
        const callbacks = this.listeners.get(event);
        if (callbacks) {
            const index = callbacks.indexOf(callback);
            if (index > -1) {
                callbacks.splice(index, 1);
            }
        }
    }

    dispatch(event, data) {
        const callbacks = this.listeners.get(event);
        if (callbacks) {
            callbacks.forEach(cb => cb(data));
        }
    }

    getToken() {
        // Get WS token from global variable (set by server in template)
        // This is a short-lived token specifically for WebSocket auth
        return window.GRIMNIR_WS_TOKEN || null;
    }

    disconnect() {
        if (this.ws) {
            this.ws.close();
            this.ws = null;
        }
    }
}

// Global WebSocket instance
window.grimnirWS = new GrimnirWebSocket();

// Initialize WebSocket on authenticated pages
function getStationSelectorElement() {
    return document.getElementById('stationSelector') || document.querySelector('select[name="station_id"]');
}

// Render server UTC timestamps in the user's local timezone for designated elements.
// Elements must include: class="js-local-datetime" data-utc="RFC3339"
function grimnirUpdateLocalDateTimes(root) {
    const scope = root || document;

    const pad2 = (n) => String(n).padStart(2, '0');
    const formatLocalYYYYMMDDHHMMSS = (d) => (
        `${d.getFullYear()}-${pad2(d.getMonth() + 1)}-${pad2(d.getDate())} ${pad2(d.getHours())}:${pad2(d.getMinutes())}:${pad2(d.getSeconds())}`
    );

    scope.querySelectorAll('.js-local-datetime[data-utc]').forEach((el) => {
        const utc = el.getAttribute('data-utc');
        if (!utc) return;
        const d = new Date(utc);
        if (isNaN(d.getTime())) return;
        el.textContent = formatLocalYYYYMMDDHHMMSS(d);
        if (!el.getAttribute('title')) el.setAttribute('title', `UTC: ${utc}`);
    });
}

window.grimnirUpdateLocalDateTimes = grimnirUpdateLocalDateTimes;

document.addEventListener('DOMContentLoaded', () => {
    grimnirUpdateLocalDateTimes(document);

    if (document.body.classList.contains('dashboard-layout')) {
        window.grimnirWS.connect();

        // Fetch initial now-playing state
        fetchNowPlaying();
        // Keep header now-playing fresh even if websocket events are missed.
        window._grimnirNowPlayingPoll = setInterval(fetchNowPlaying, 15000);

        // Always resolve now-playing from the currently selected station.
        // WebSocket events may include updates for other stations.
        window.grimnirWS.on('now_playing', () => {
            fetchNowPlaying();
        });

        window.grimnirWS.on('schedule_update', (data) => {
            // Trigger calendar refresh if on schedule page
            if (window.calendar) {
                window.calendar.refetchEvents();
            }
        });

        window.grimnirWS.on('health', (data) => {
            updateHealthStatus(data);
        });

        const nowPlaying = document.getElementById('nowPlaying');
        if (nowPlaying) {
            nowPlaying.style.cursor = 'pointer';
            nowPlaying.title = 'Play current station audio';
            nowPlaying.addEventListener('click', () => {
                playSelectedStationAudio();
            });
        }

        const dashboardPlayLink = document.getElementById('dashboardPlayLink');
        if (dashboardPlayLink) {
            dashboardPlayLink.addEventListener('click', (e) => {
                e.preventDefault();
                playSelectedStationAudio();
            });
        }

        // Refresh header now-playing when station context changes.
        const stationSelect = getStationSelectorElement();
        if (stationSelect) {
            stationSelect.addEventListener('change', () => {
                setTimeout(fetchNowPlaying, 150);
                // Keep audio stream and dashboard station context aligned.
                if (window.globalPlayer?.isLive) {
                    playSelectedStationAudio();
                }
            });
        }

        window.addEventListener('resize', () => updateDashboardNowPlayingMarquee());
    }
});

function getSelectedStationInfo() {
    const stationSelect = getStationSelectorElement();
    if (!stationSelect || !stationSelect.value) return null;

    const option = stationSelect.options[stationSelect.selectedIndex];
    return {
        stationId: stationSelect.value,
        stationName: option?.textContent?.trim() || 'Live Stream'
    };
}

async function playSelectedStationAudio() {
    if (!window.globalPlayer || typeof window.globalPlayer.switchToStation !== 'function') return;

    const station = getSelectedStationInfo();
    if (!station) return;

    try {
        const headers = {};
        if (window.GRIMNIR_WS_TOKEN) {
            headers['Authorization'] = 'Bearer ' + window.GRIMNIR_WS_TOKEN;
        }

        const response = await fetch(`/api/v1/stations/${station.stationId}/mounts`, { headers });
        if (!response.ok) return;

        const mounts = await response.json();
        if (!Array.isArray(mounts) || mounts.length === 0) return;

        // Prefer highest bitrate mount as default.
        const sorted = [...mounts].sort((a, b) => (b.bitrate || 0) - (a.bitrate || 0));
        const mount = sorted[0];
        if (!mount?.name) return;

        window.globalPlayer.switchToStation(station.stationId, `/live/${mount.name}`, station.stationName);
    } catch (e) {
        console.debug('Failed to start station audio from now-playing header:', e);
    }
}

// Fetch current now-playing state from API
async function fetchNowPlaying() {
    try {
        // Get station ID from the page (station selector)
        const stationSelect = getStationSelectorElement();
        const stationId = stationSelect?.value;
        if (!stationId) return;

        const response = await fetch(`/api/v1/analytics/now-playing?station_id=${stationId}`);
        if (response.ok) {
            const data = await response.json();
            updateNowPlaying(data);
        }
    } catch (e) {
        console.log('Failed to fetch now-playing:', e);
    }
}

// Update now playing display
function updateNowPlaying(data) {
    const container = document.getElementById('nowPlaying');
    if (!container) return;

    const line = buildNowPlayingLine(data);
    if (line) {
        const icon = document.createElement('i');
        icon.className = 'bi bi-music-note text-success';

        const trackWrap = document.createElement('span');
        trackWrap.className = 'dashboard-now-playing-track';

        const trackText = document.createElement('span');
        trackText.className = 'dashboard-now-playing-text';
        trackText.textContent = line;
        trackText.title = line;

        trackWrap.appendChild(trackText);
        container.replaceChildren(icon, trackWrap);
        scheduleNowPlayingMarqueeUpdate();
    } else {
        container.innerHTML = '<i class="bi bi-music-note"></i><span class="text-body-secondary">Nothing playing</span>';
    }

    // Update on-air status
    const statusBadge = document.getElementById('onAirStatus');
    if (statusBadge) {
        if (data && data.status === 'playing') {
            statusBadge.className = 'badge on-air';
            statusBadge.innerHTML = '<i class="bi bi-record-circle me-1"></i>ON AIR';
        } else {
            statusBadge.className = 'badge bg-secondary';
            statusBadge.innerHTML = '<i class="bi bi-record-circle me-1"></i>OFF AIR';
        }
    }
}

function buildNowPlayingLine(data) {
    if (!data || typeof data !== 'object') return '';

    let artist = String(data.artist || '').trim();
    let title = String(data.title || '').trim();

    if (!artist && title.includes(' - ')) {
        const parts = title.split(' - ');
        if (parts.length >= 2) {
            artist = parts.shift().trim();
            title = parts.join(' - ').trim();
        }
    }

    if (data.is_live_dj === true) return 'Live DJ';
    if (title && artist) return `${artist} - ${title}`;
    if (title) return title;
    if (artist) return artist;

    const mountID = String(data.mount_id || '').trim();
    if (mountID) return `Mount: ${mountID}`;

    return '';
}

function scheduleNowPlayingMarqueeUpdate() {
    requestAnimationFrame(() => {
        requestAnimationFrame(() => {
            updateDashboardNowPlayingMarquee();
        });
    });
    setTimeout(updateDashboardNowPlayingMarquee, 60);
}

function updateDashboardNowPlayingMarquee() {
    const wrap = document.querySelector('#nowPlaying .dashboard-now-playing-track');
    const text = document.querySelector('#nowPlaying .dashboard-now-playing-text');
    if (!wrap || !text) return;

    wrap.classList.remove('is-scrolling');
    wrap.style.removeProperty('--np-scroll-distance');
    wrap.style.removeProperty('--np-scroll-duration');

    const overflow = text.scrollWidth - wrap.clientWidth;
    if (overflow <= 6) return;

    const travel = overflow + 20;
    const duration = Math.max(10, Math.min(40, Math.round(travel / 12)));
    wrap.style.setProperty('--np-scroll-distance', `${travel}px`);
    wrap.style.setProperty('--np-scroll-duration', `${duration}s`);
    wrap.classList.add('is-scrolling');
}

document.body.addEventListener('htmx:afterSwap', () => {
    scheduleNowPlayingMarqueeUpdate();
});

// Update health status indicators
function updateHealthStatus(data) {
    // Update any health indicators on the page
    const healthIndicators = document.querySelectorAll('[data-health-mount]');
    healthIndicators.forEach(el => {
        const mountId = el.dataset.healthMount;
        if (data.mount_id === mountId) {
            el.className = data.healthy ? 'badge bg-success' : 'badge bg-danger';
            el.textContent = data.healthy ? 'Healthy' : 'Error';
        }
    });
}

// Keyboard shortcuts
document.addEventListener('keydown', (e) => {
    // Ignore if typing in input
    if (e.target.tagName === 'INPUT' || e.target.tagName === 'TEXTAREA') {
        return;
    }

    // Ctrl+K - Quick search
    if (e.ctrlKey && e.key === 'k') {
        e.preventDefault();
        const searchModal = bootstrap.Modal.getOrCreateInstance(document.getElementById('searchModal'));
        searchModal.show();
        setTimeout(() => document.getElementById('globalSearch')?.focus(), 100);
        return;
    }

    // ? - Help
    if (e.key === '?') {
        const shortcutsModal = bootstrap.Modal.getOrCreateInstance(document.getElementById('shortcutsModal'));
        shortcutsModal.show();
        return;
    }

    // G-key navigation
    if (e.key === 'g' && !window.gKeyPressed) {
        window.gKeyPressed = true;
        setTimeout(() => { window.gKeyPressed = false; }, 500);
        return;
    }

    if (window.gKeyPressed) {
        switch (e.key) {
            case 'd': window.location.href = '/dashboard'; break;
            case 'm': window.location.href = '/dashboard/media'; break;
            case 's': window.location.href = '/dashboard/schedule'; break;
            case 'l': window.location.href = '/dashboard/live'; break;
            case 'a': window.location.href = '/dashboard/analytics'; break;
        }
        window.gKeyPressed = false;
    }
});

// Toast notifications
function showToast(message, type = 'info') {
    const container = document.getElementById('toastContainer');
    if (!container) return;

    const icons = {
        success: 'bi-check-circle',
        error: 'bi-exclamation-circle',
        warning: 'bi-exclamation-triangle',
        info: 'bi-info-circle'
    };

    const toast = document.createElement('div');
    toast.className = 'toast show';
    toast.setAttribute('role', 'alert');
    toast.innerHTML = `
        <div class="toast-header bg-${type === 'error' ? 'danger' : type}">
            <i class="bi ${icons[type]} me-2"></i>
            <strong class="me-auto">Notification</strong>
            <button type="button" class="btn-close" data-bs-dismiss="toast"></button>
        </div>
        <div class="toast-body">${message}</div>
    `;

    container.appendChild(toast);

    // Auto-remove after 5 seconds
    setTimeout(() => {
        toast.classList.remove('show');
        setTimeout(() => toast.remove(), 300);
    }, 5000);
}

// HTMX event handlers
document.body.addEventListener('htmx:afterRequest', (e) => {
    // Handle errors
    if (e.detail.failed) {
        const xhr = e.detail.xhr;
        let message = 'An error occurred';

        try {
            const response = JSON.parse(xhr.responseText);
            message = response.error || response.message || message;
        } catch {
            if (xhr.status === 401) {
                message = 'Session expired. Please log in again.';
                setTimeout(() => window.location.href = '/login', 1000);
            } else if (xhr.status === 403) {
                message = 'Access denied';
            } else if (xhr.status === 404) {
                message = 'Not found';
            }
        }

        showToast(message, 'error');
    }
});

// Handle page content swaps (SPA navigation)
document.body.addEventListener('htmx:afterSwap', (e) => {
    // Always update any local datetime placeholders in swapped content.
    try {
        grimnirUpdateLocalDateTimes(e.detail?.target || document);
    } catch (_) {
        // non-fatal
    }

    // Only handle page-level swaps
    if (e.detail.target.id === 'page-content') {
        // Re-initialize Bootstrap tooltips
        const tooltips = document.querySelectorAll('[data-bs-toggle="tooltip"]');
        tooltips.forEach(el => {
            const existing = bootstrap.Tooltip.getInstance(el);
            if (existing) existing.dispose();
            new bootstrap.Tooltip(el);
        });

        // Re-initialize upload zones if any
        document.querySelectorAll('.upload-zone').forEach(initUploadZone);

        // Trigger any page-specific initialization
        document.dispatchEvent(new CustomEvent('grimnir:pageLoad'));

        // Re-run any inline scripts in the new content
        // Note: HTMX handles <script> tags in swapped content automatically

        // Update document title from the page content if available
        const titleEl = document.querySelector('[data-page-title]');
        if (titleEl) {
            document.title = titleEl.dataset.pageTitle + ' - Grimnir Radio';
        }

        // Close any open modals
        const openModals = document.querySelectorAll('.modal.show');
        openModals.forEach(modal => {
            const instance = bootstrap.Modal.getInstance(modal);
            if (instance) instance.hide();
        });

        // Scroll to top
        window.scrollTo(0, 0);
    }
});

document.body.addEventListener('htmx:beforeRequest', (e) => {
    // Add loading indicator
    const target = e.detail.elt;
    if (target.dataset.loadingText) {
        target.dataset.originalText = target.textContent;
        target.textContent = target.dataset.loadingText;
        target.disabled = true;
    }
});

document.body.addEventListener('htmx:afterRequest', (e) => {
    // Remove loading indicator
    const target = e.detail.elt;
    if (target.dataset.originalText) {
        target.textContent = target.dataset.originalText;
        target.disabled = false;
        delete target.dataset.originalText;
    }
});

// File upload handling with progress
function initUploadZone(element) {
    const input = element.querySelector('input[type="file"]');
    const progress = element.querySelector('.progress');
    const progressBar = element.querySelector('.progress-bar');

    element.addEventListener('dragover', (e) => {
        e.preventDefault();
        element.classList.add('dragover');
    });

    element.addEventListener('dragleave', () => {
        element.classList.remove('dragover');
    });

    element.addEventListener('drop', (e) => {
        e.preventDefault();
        element.classList.remove('dragover');

        const files = e.dataTransfer.files;
        if (files.length > 0) {
            handleFiles(files);
        }
    });

    element.addEventListener('click', () => {
        input.click();
    });

    input.addEventListener('change', () => {
        if (input.files.length > 0) {
            handleFiles(input.files);
        }
    });

    function handleFiles(files) {
        Array.from(files).forEach(file => {
            uploadFile(file);
        });
    }

    function uploadFile(file) {
        const formData = new FormData();
        formData.append('file', file);

        if (progress) {
            progress.classList.remove('d-none');
        }

        const xhr = new XMLHttpRequest();
        xhr.open('POST', '/dashboard/media/upload');

        xhr.upload.addEventListener('progress', (e) => {
            if (e.lengthComputable && progressBar) {
                const percent = (e.loaded / e.total) * 100;
                progressBar.style.width = percent + '%';
                progressBar.textContent = Math.round(percent) + '%';
            }
        });

        xhr.addEventListener('load', () => {
            if (progress) {
                progress.classList.add('d-none');
                progressBar.style.width = '0%';
            }

            if (xhr.status >= 200 && xhr.status < 300) {
                showToast(`${file.name} uploaded successfully`, 'success');
                // Refresh media list
                htmx.trigger(document.body, 'mediaUploaded');
            } else {
                showToast(`Failed to upload ${file.name}`, 'error');
            }
        });

        xhr.addEventListener('error', () => {
            if (progress) {
                progress.classList.add('d-none');
            }
            showToast(`Failed to upload ${file.name}`, 'error');
        });

        xhr.send(formData);
    }
}

// Initialize upload zones
document.addEventListener('DOMContentLoaded', () => {
    document.querySelectorAll('.upload-zone').forEach(initUploadZone);
});

// Global Audio Player - Floating & Draggable
class GlobalPlayer {
    constructor() {
        this.audio = new Audio();
        this.currentTrack = null;
        this.playlist = [];
        this.playlistIndex = 0;
        this.isLive = false;
        this.isLiveDJ = false;
        this.isMinimized = false;

        this.webrtcEnabled = true;
        this.liveTransport = localStorage.getItem('grimnir-live-transport') || 'http';
        this.peerConnection = null;
        this.signalingWs = null;
        this.useWebRTC = false;  // Currently using WebRTC vs HTTP

        // Drag state
        this.isDragging = false;
        this.dragOffset = { x: 0, y: 0 };

        // DOM elements
        this.container = document.getElementById('globalPlayer');
        this.playPauseBtn = document.getElementById('playerPlayPause');
        this.playIcon = document.getElementById('playerPlayIcon');
        this.prevBtn = document.getElementById('playerPrev');
        this.nextBtn = document.getElementById('playerNext');
        this.closeBtn = document.getElementById('playerClose');
        this.minimizeBtn = document.getElementById('playerMinimize');
        this.dragHandle = document.getElementById('playerDragHandle');
        this.progressBar = document.getElementById('globalPlayerProgressBar');
        this.progressContainer = document.getElementById('globalPlayerProgress');
        this.volumeSlider = document.getElementById('playerVolume');
        this.titleEl = document.getElementById('playerTitle');
        this.artistEl = document.getElementById('playerArtist');
        this.artworkEl = document.getElementById('playerArtwork');
        this.currentTimeEl = document.getElementById('playerCurrentTime');
        this.durationEl = document.getElementById('playerDuration');
        this.stationSelector = document.getElementById('playerStationSelector');
        this.stationMenu = document.getElementById('playerStationMenu');
        this.transportSelector = document.getElementById('playerTransportSelector');
        this.transportMenu = document.getElementById('playerTransportMenu');
        this.titleAutoScrollRaf = null;
        this.titleAutoScrollPausedUntil = 0;
        this.titleScrollPxPerSecond = 10;
        this.titleDragActive = false;
        this.titlePointerStartX = 0;
        this.titleScrollStart = 0;
        this.artistAutoScrollRaf = null;

        // Cached stations for quick switching
        this.publicStations = [];

        this.init();
    }

    init() {
        if (!this.container) return;

        // Set up audio event listeners
        this.audio.addEventListener('play', () => this.onPlay());
        this.audio.addEventListener('pause', () => this.onPause());
        this.audio.addEventListener('ended', () => this.onEnded());
        this.audio.addEventListener('timeupdate', () => this.onTimeUpdate());
        this.audio.addEventListener('loadedmetadata', () => this.onLoadedMetadata());
        this.audio.addEventListener('error', (e) => this.onError(e));
        this.audio.addEventListener('stalled', () => this.onStalled());
        this.audio.addEventListener('waiting', () => this.onWaiting());
        this.audio.addEventListener('playing', () => this.onPlaying());

        // Preload setting for better buffering
        this.audio.preload = 'auto';

        // Set up control listeners
        this.playPauseBtn?.addEventListener('click', () => this.togglePlayPause());
        this.prevBtn?.addEventListener('click', () => this.prev());
        this.nextBtn?.addEventListener('click', () => this.next());
        this.closeBtn?.addEventListener('click', () => this.close());
        this.minimizeBtn?.addEventListener('click', () => this.toggleMinimize());

        // Progress bar seeking
        this.progressContainer?.addEventListener('click', (e) => this.seek(e));

        // Volume control
        this.volumeSlider?.addEventListener('input', (e) => {
            this.audio.volume = e.target.value / 100;
            localStorage.setItem('grimnir-volume', e.target.value);
        });

        // Restore volume from localStorage
        const savedVolume = localStorage.getItem('grimnir-volume');
        if (savedVolume) {
            this.audio.volume = savedVolume / 100;
            if (this.volumeSlider) this.volumeSlider.value = savedVolume;
        }

        // Set up drag functionality
        this.initDrag();

        // Restore position from localStorage
        this.restorePosition();

        // Restore last playing track from sessionStorage
        this.restoreState();

        // Recalculate marquee when viewport changes
        window.addEventListener('resize', () => this.updateTitleMarquee());
        window.addEventListener('load', () => {
            this.scheduleTitleMarqueeUpdate();
            this.scheduleArtistMarqueeUpdate();
        });
        if (document.fonts && document.fonts.ready) {
            document.fonts.ready.then(() => {
                this.scheduleTitleMarqueeUpdate();
                this.scheduleArtistMarqueeUpdate();
            }).catch(() => {});
        }
        this.initTitleScrollControls();

        // Save state when navigating away
        window.addEventListener('beforeunload', () => {
            this.saveState();
        });

        // Also save state periodically for live streams
        setInterval(() => {
            if (this.currentTrack && !this.audio.paused) {
                this.saveState();
            }
        }, 5000);

        // Load public stations for the station selector
        this.loadPublicStations();
    }

    initTitleScrollControls() {
        if (!this.titleEl) return;

        const startDrag = (clientX) => {
            if (!this.titleEl.classList.contains('scrollable')) return;
            this.titleDragActive = true;
            this.titlePointerStartX = clientX;
            this.titleScrollStart = this.titleEl.scrollLeft;
            this.titleEl.classList.add('dragging-title');
            this.pauseTitleAutoScroll(2000);
        };

        const onDrag = (clientX) => {
            if (!this.titleDragActive) return;
            const dx = clientX - this.titlePointerStartX;
            this.titleEl.scrollLeft = this.titleScrollStart - dx;
        };

        const endDrag = () => {
            if (!this.titleDragActive) return;
            this.titleDragActive = false;
            this.titleEl.classList.remove('dragging-title');
            this.pauseTitleAutoScroll(1500);
        };

        this.titleEl.addEventListener('mousedown', (e) => {
            if (e.button !== 0) return;
            startDrag(e.clientX);
            e.preventDefault();
        });
        document.addEventListener('mousemove', (e) => onDrag(e.clientX));
        document.addEventListener('mouseup', endDrag);

        this.titleEl.addEventListener('touchstart', (e) => {
            if (!e.touches || e.touches.length === 0) return;
            startDrag(e.touches[0].clientX);
        }, { passive: true });
        document.addEventListener('touchmove', (e) => {
            if (!this.titleDragActive || !e.touches || e.touches.length === 0) return;
            onDrag(e.touches[0].clientX);
            e.preventDefault();
        }, { passive: false });
        document.addEventListener('touchend', endDrag);
        document.addEventListener('touchcancel', endDrag);

        this.titleEl.addEventListener('wheel', (e) => {
            if (!this.titleEl.classList.contains('scrollable')) return;
            const delta = Math.abs(e.deltaX) > Math.abs(e.deltaY) ? e.deltaX : e.deltaY;
            if (!delta) return;
            this.pauseTitleAutoScroll(1400);
            this.titleEl.scrollLeft += delta;
            e.preventDefault();
        }, { passive: false });
    }

    pauseTitleAutoScroll(ms = 1500) {
        this.titleAutoScrollPausedUntil = Date.now() + ms;
    }

    stopTitleAutoScroll() {
        if (this.titleAutoScrollRaf) {
            cancelAnimationFrame(this.titleAutoScrollRaf);
            this.titleAutoScrollRaf = null;
        }
    }

    startTitleAutoScroll() {
        if (!this.titleEl) return;

        this.stopTitleAutoScroll();
        let lastTs = 0;
        let holdUntil = Date.now() + 1200;
        let resetPending = false;

        const tick = (ts) => {
            this.titleAutoScrollRaf = requestAnimationFrame(tick);

            const maxScroll = this.titleEl.scrollWidth - this.titleEl.clientWidth;
            if (maxScroll <= 4 || !this.titleEl.classList.contains('scrollable')) {
                return;
            }

            if (!lastTs) {
                lastTs = ts;
                return;
            }

            const now = Date.now();
            if (this.titleDragActive || now < this.titleAutoScrollPausedUntil || now < holdUntil) {
                lastTs = ts;
                return;
            }

            if (resetPending) {
                this.titleEl.scrollLeft = 0;
                resetPending = false;
                holdUntil = now + 1000;
                lastTs = ts;
                return;
            }

            const dt = (ts - lastTs) / 1000;
            lastTs = ts;
            const next = this.titleEl.scrollLeft + (dt * this.titleScrollPxPerSecond);

            if (next >= maxScroll) {
                this.titleEl.scrollLeft = maxScroll;
                holdUntil = now + 2200;
                resetPending = true;
                return;
            }

            this.titleEl.scrollLeft = next;
        };

        this.titleAutoScrollRaf = requestAnimationFrame(tick);
    }

    async loadPublicStations() {
        try {
            const response = await fetch('/api/v1/public/stations');
            if (response.ok) {
                const stations = await response.json();
                this.publicStations = this.applyPreferredStationOrder(stations);
                this.updateTransportMenu();
                this.updateStationMenu();
            }
        } catch (e) {
            console.debug('Failed to load public stations:', e);
        }
    }

    applyPreferredStationOrder(stations) {
        if (!Array.isArray(stations)) return [];
        const preferred = Array.isArray(window.GRIMNIR_STATION_ORDER) ? window.GRIMNIR_STATION_ORDER : [];
        if (preferred.length === 0) return stations;

        const rank = new Map();
        preferred.forEach((id, idx) => rank.set(id, idx));
        return [...stations].sort((a, b) => {
            const ra = rank.has(a.id) ? rank.get(a.id) : Number.MAX_SAFE_INTEGER;
            const rb = rank.has(b.id) ? rank.get(b.id) : Number.MAX_SAFE_INTEGER;
            if (ra !== rb) return ra - rb;
            return (a.name || '').localeCompare(b.name || '');
        });
    }

    updateTransportMenu() {
        if (!this.transportMenu) return;

        const transport = this.getLiveTransport();
        this.transportMenu.innerHTML = `
            <li>
                <a class="dropdown-item small ${transport === 'http' ? 'active' : ''}" href="#"
                   onclick="globalPlayer.setLiveTransport('http'); return false;">
                    <i class="bi bi-check2 ${transport === 'http' ? '' : 'invisible'} me-1"></i>Standard (HTTP)
                </a>
            </li>
            <li>
                <a class="dropdown-item small ${transport === 'webrtc' ? 'active' : ''}" href="#"
                   onclick="globalPlayer.setLiveTransport('webrtc'); return false;">
                    <i class="bi bi-check2 ${transport === 'webrtc' ? '' : 'invisible'} me-1"></i>Low Latency (WebRTC)
                </a>
            </li>
        `;
    }

    updateStationMenu() {
        if (!this.stationMenu) return;

        if (this.publicStations.length === 0) {
            this.stationMenu.innerHTML = '<li><span class="dropdown-item-text text-body-secondary small">No stations available</span></li>';
            return;
        }

        let html = '';
        for (const station of this.publicStations) {
            if (station.mounts && station.mounts.length > 0) {
                html += `<li><h6 class="dropdown-header">${this.escapeHtml(station.name)}</h6></li>`;
                for (const mount of station.mounts) {
                    const quality = mount.bitrate ? ` (${mount.bitrate}kbps)` : '';
                    html += `<li><a class="dropdown-item small" href="#" onclick="globalPlayer.switchToStation('${station.id}', '${mount.url}', '${this.escapeHtml(station.name)}'); return false;">
                        <i class="bi bi-play-fill me-1"></i>${this.escapeHtml(mount.name)}${quality}
                    </a></li>`;
                }
            }
        }

        if (html) {
            this.stationMenu.innerHTML = html;
        } else {
            this.stationMenu.innerHTML = '<li><span class="dropdown-item-text text-body-secondary small">No stations available</span></li>';
        }
    }

    escapeHtml(str) {
        if (!str) return '';
        return str.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
    }

    switchToStation(stationId, mountUrl, stationName) {
        // Hard-reset current playback state before switching streams.
        this.stopMetadataPolling();
        this.stopLiveTimeTicker();
        this.audio.pause();
        this.audio.removeAttribute('src');
        this.audio.srcObject = null;
        this.audio.load();
        this.closeWebRTC();

        this.playLive(mountUrl, stationName, stationId, {
            transport: this.getLiveTransport()
        });
    }

    setLiveTransport(mode) {
        const next = mode === 'webrtc' ? 'webrtc' : 'http';
        this.liveTransport = next;
        localStorage.setItem('grimnir-live-transport', next);
        this.updateTransportMenu();
    }

    getLiveTransport() {
        return this.liveTransport === 'webrtc' ? 'webrtc' : 'http';
    }

    initDrag() {
        if (!this.dragHandle || !this.container) return;

        this.dragHandle.addEventListener('mousedown', (e) => this.startDrag(e));
        this.dragHandle.addEventListener('touchstart', (e) => this.startDrag(e), { passive: false });

        document.addEventListener('mousemove', (e) => this.onDrag(e));
        document.addEventListener('touchmove', (e) => this.onDrag(e), { passive: false });

        document.addEventListener('mouseup', () => this.endDrag());
        document.addEventListener('touchend', () => this.endDrag());
    }

    startDrag(e) {
        if (e.target.closest('button')) return; // Don't drag when clicking buttons

        this.isDragging = true;
        this.container.classList.add('dragging');

        const rect = this.container.getBoundingClientRect();
        const clientX = e.touches ? e.touches[0].clientX : e.clientX;
        const clientY = e.touches ? e.touches[0].clientY : e.clientY;

        this.dragOffset = {
            x: clientX - rect.left,
            y: clientY - rect.top
        };

        e.preventDefault();
    }

    onDrag(e) {
        if (!this.isDragging) return;

        const clientX = e.touches ? e.touches[0].clientX : e.clientX;
        const clientY = e.touches ? e.touches[0].clientY : e.clientY;

        let x = clientX - this.dragOffset.x;
        let y = clientY - this.dragOffset.y;

        // Constrain to viewport
        const rect = this.container.getBoundingClientRect();
        const maxX = window.innerWidth - rect.width;
        const maxY = window.innerHeight - rect.height;

        x = Math.max(0, Math.min(x, maxX));
        y = Math.max(0, Math.min(y, maxY));

        this.container.style.left = x + 'px';
        this.container.style.top = y + 'px';
        this.container.style.right = 'auto';
        this.container.style.bottom = 'auto';

        e.preventDefault();
    }

    endDrag() {
        if (!this.isDragging) return;
        this.isDragging = false;
        this.container.classList.remove('dragging');
        this.savePosition();
    }

    savePosition() {
        if (!this.container) return;
        const rect = this.container.getBoundingClientRect();
        localStorage.setItem('grimnir-player-position', JSON.stringify({
            x: rect.left,
            y: rect.top
        }));
    }

    restorePosition() {
        const pos = localStorage.getItem('grimnir-player-position');
        if (pos && this.container) {
            try {
                const { x, y } = JSON.parse(pos);
                // Verify position is still valid
                if (x >= 0 && x < window.innerWidth - 100 && y >= 0 && y < window.innerHeight - 100) {
                    this.container.style.left = x + 'px';
                    this.container.style.top = y + 'px';
                    this.container.style.right = 'auto';
                    this.container.style.bottom = 'auto';
                }
            } catch (e) {
                console.error('Failed to restore player position:', e);
            }
        }
    }

    toggleMinimize() {
        this.isMinimized = !this.isMinimized;
        this.container?.classList.toggle('minimized', this.isMinimized);
        if (this.minimizeBtn) {
            const icon = this.minimizeBtn.querySelector('i');
            if (icon) {
                icon.classList.toggle('bi-dash-lg', !this.isMinimized);
                icon.classList.toggle('bi-plus-lg', this.isMinimized);
            }
        }
        if (!this.isMinimized) {
            this.scheduleTitleMarqueeUpdate();
        }
    }

    play(track) {
        // track: { url, title, artist, artwork, id, type: 'media'|'live'|'playlist' }
        this.currentTrack = track;
        this.isLive = track.type === 'live';
        this.isLiveDJ = false;
        this.audio.src = track.url;
        this.audio.play().catch(e => console.error('Play error:', e));

        this.updateUI();
        this.show();
        this.saveState();
    }

    playMedia(id, title, artist, artworkUrl) {
        this.play({
            url: `/dashboard/media/${id}/stream`,
            title: title || 'Unknown Track',
            artist: artist || '',
            artwork: artworkUrl || `/dashboard/media/${id}/artwork`,
            id: id,
            type: 'media'
        });
    }

    playLive(url, stationName, stationId, options = {}) {
        // Guard against empty URLs
        if (!url) {
            console.warn('playLive called with empty URL');
            return;
        }

        // Check if this is already an LQ stream (user selected LQ quality)
        // Simple check: URL contains -lq in the path
        const isLQStream = url.includes('/live/') && url.includes('-lq');

        // Determine LQ URL for HTTP fallback (append -lq to mount name)
        // e.g., /live/main -> /live/main-lq
        // But don't double it if URL already ends with -lq
        let lqUrl = url;
        if (url.includes('/live/') && !isLQStream) {
            lqUrl = url.replace(/\/live\/([^/?]+)/, '/live/$1-lq');
        }

        this.currentTrack = {
            url: url,        // HQ URL (for reference)
            lqUrl: lqUrl,    // LQ URL (for HTTP streaming)
            title: stationName || 'Live Stream',
            artist: stationName || 'Connecting...',
            artwork: null,
            id: null,
            type: 'live',
            stationId: stationId,
            stationName: stationName || ''
        };
        this.isLive = true;
        this.isLiveDJ = false;
        this.updateUI();
        this.show();

        // If LQ stream selected, skip WebRTC and use HTTP directly
        if (isLQStream) {
            console.log('LQ stream selected, using HTTP directly');
            this.fallbackToHTTP(url);
            this.startMetadataPolling();
            return;
        }

        const transport = options.transport || this.getLiveTransport();
        const useWebRTCTransport = transport === 'webrtc';

        // Try WebRTC only when explicitly selected.
        if (useWebRTCTransport && this.webrtcEnabled && 'RTCPeerConnection' in window) {
            this.connectWebRTC(stationId).then(connected => {
                if (!connected) {
                    // Fall back to HTTP LQ streaming immediately
                    console.log('WebRTC failed, falling back to HTTP LQ streaming');
                    this.fallbackToHTTP(lqUrl);
                }
            }).catch(err => {
                console.log('WebRTC error, falling back to HTTP LQ:', err);
                this.fallbackToHTTP(lqUrl);
            });
        } else {
            // Default path: HTTP streaming
            this.fallbackToHTTP(lqUrl);
        }

        // Start fetching now-playing metadata
        this.startMetadataPolling();
    }

    fallbackToHTTP(url) {
        this.useWebRTC = false;
        this.closeWebRTC();

        // Guard against empty URLs
        if (!url) {
            console.warn('fallbackToHTTP called with empty URL');
            return;
        }

        // Fully reset audio element to avoid stale buffer issues
        this.audio.pause();
        this.audio.removeAttribute('src');
        this.audio.load();  // Reset internal state

        // Add cache-busting timestamp to prevent browser caching
        const cacheBustUrl = url.includes('?')
            ? `${url}&_t=${Date.now()}`
            : `${url}?_t=${Date.now()}`;

        this.audio.src = cacheBustUrl;
        this.audio.play().catch(e => console.error('HTTP play error:', e));
    }

    async connectWebRTC(stationId) {
        // Close any existing connection
        this.closeWebRTC();

        return new Promise((resolve) => {
            const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
            const qs = stationId ? `?station_id=${encodeURIComponent(stationId)}` : '';
            const signalingUrl = `${protocol}//${window.location.host}/webrtc/signal${qs}`;

            console.log('Connecting to WebRTC signaling:', signalingUrl);

            this.signalingWs = new WebSocket(signalingUrl);
            let connected = false;
            let timeoutId;

            // Timeout after 10 seconds
            timeoutId = setTimeout(() => {
                if (!connected) {
                    console.log('WebRTC connection timeout');
                    this.closeWebRTC();
                    resolve(false);
                }
            }, 10000);

            this.signalingWs.onopen = () => {
                console.log('WebRTC signaling connected');
            };

            this.signalingWs.onmessage = async (event) => {
                try {
                    const msg = JSON.parse(event.data);

                    switch (msg.type) {
                        case 'offer':
                            // Create peer connection and handle offer
                            const success = await this.handleWebRTCOffer(msg.sdp);
                            if (success) {
                                connected = true;
                                clearTimeout(timeoutId);
                                this.useWebRTC = true;
                                resolve(true);
                            } else {
                                clearTimeout(timeoutId);
                                resolve(false);
                            }
                            break;

                        case 'candidate':
                            if (this.peerConnection && msg.candidate) {
                                try {
                                    await this.peerConnection.addIceCandidate(msg.candidate);
                                } catch (e) {
                                    console.debug('Failed to add ICE candidate:', e);
                                }
                            }
                            break;

                        case 'error':
                            console.error('WebRTC signaling error:', msg.error);
                            clearTimeout(timeoutId);
                            resolve(false);
                            break;
                    }
                } catch (e) {
                    console.error('Failed to parse signaling message:', e);
                }
            };

            this.signalingWs.onerror = (error) => {
                console.log('WebRTC signaling error:', error);
                clearTimeout(timeoutId);
                resolve(false);
            };

            this.signalingWs.onclose = () => {
                console.log('WebRTC signaling closed');
                if (!connected) {
                    clearTimeout(timeoutId);
                    resolve(false);
                }
            };
        });
    }

    async handleWebRTCOffer(offer) {
        try {
            // Build ICE servers from server-provided config
            const iceServers = [];

            // Use server-provided STUN/TURN config if available
            const webrtcCfg = window.GRIMNIR_WEBRTC || {};

            // Add STUN server
            if (webrtcCfg.stunUrl) {
                iceServers.push({ urls: webrtcCfg.stunUrl });
            } else {
                // Fallback to Google STUN
                iceServers.push({ urls: 'stun:stun.l.google.com:19302' });
            }

            // Add TURN server if configured
            if (webrtcCfg.turnUrl) {
                iceServers.push({
                    urls: webrtcCfg.turnUrl,
                    username: webrtcCfg.turnUsername || '',
                    credential: webrtcCfg.turnPassword || ''
                });
            }

            const config = {
                iceServers,
                // Force relay to test TURN server (remove after testing)
                // iceTransportPolicy: 'relay'
            };

            console.log('WebRTC ICE config:', JSON.stringify(config, null, 2));

            this.peerConnection = new RTCPeerConnection(config);

            // Set up audio element to receive the track
            this.peerConnection.ontrack = (event) => {
                console.log('WebRTC received audio track');
                if (event.streams && event.streams[0]) {
                    this.audio.srcObject = event.streams[0];
                    this.audio.play().then(() => {
                        console.log('WebRTC audio playing');
                        if (this.artistEl) {
                            this.setSecondaryText(this.getLiveSecondaryText());
                        }
                    }).catch(e => console.error('WebRTC play error:', e));
                }
            };

            // Send ICE candidates to server
            this.peerConnection.onicecandidate = (event) => {
                if (event.candidate && this.signalingWs?.readyState === WebSocket.OPEN) {
                    this.signalingWs.send(JSON.stringify({
                        type: 'candidate',
                        candidate: event.candidate.toJSON()
                    }));
                }
            };

            // Monitor connection state
            this.peerConnection.onconnectionstatechange = () => {
                const state = this.peerConnection?.connectionState;
                console.log('WebRTC connection state:', state);

                if (state === 'connected') {
                    this._webrtcReconnectAttempts = 0;
                    if (this.artistEl && this.currentTrack) {
                        this.setSecondaryText(this.getLiveSecondaryText());
                    }
                } else if (state === 'failed' || state === 'disconnected') {
                    // WebRTC failed - fall back to HTTP immediately (no retries)
                    if (this.currentTrack && this.isLive) {
                        console.log('WebRTC connection failed, falling back to HTTP LQ immediately');
                        this.fallbackToHTTP(this.currentTrack.lqUrl || this.currentTrack.url);
                    }
                }
            };

            // Set remote description (the offer)
            await this.peerConnection.setRemoteDescription(new RTCSessionDescription(offer));

            // Create and send answer
            const answer = await this.peerConnection.createAnswer();
            await this.peerConnection.setLocalDescription(answer);

            // Send answer to server
            if (this.signalingWs?.readyState === WebSocket.OPEN) {
                this.signalingWs.send(JSON.stringify({
                    type: 'answer',
                    sdp: this.peerConnection.localDescription
                }));
            }

            return true;
        } catch (e) {
            console.error('WebRTC offer handling failed:', e);
            this.closeWebRTC();
            return false;
        }
    }

    closeWebRTC() {
        if (this.peerConnection) {
            this.peerConnection.close();
            this.peerConnection = null;
        }
        if (this.signalingWs) {
            this.signalingWs.close();
            this.signalingWs = null;
        }
        // Clear srcObject if it was set
        if (this.audio.srcObject) {
            this.audio.srcObject = null;
        }
        this.useWebRTC = false;
    }

    startMetadataPolling() {
        // Clear any existing interval
        if (this.metadataInterval) {
            clearInterval(this.metadataInterval);
        }

        // Fetch immediately
        this.fetchNowPlayingMetadata();

        // Then poll every 15 seconds
        this.metadataInterval = setInterval(() => {
            if (this.isLive && !this.audio.paused) {
                this.fetchNowPlayingMetadata();
            }
        }, 15000);
    }

    stopMetadataPolling() {
        if (this.metadataInterval) {
            clearInterval(this.metadataInterval);
            this.metadataInterval = null;
        }
    }

    async fetchNowPlayingMetadata() {
        if (!this.isLive || !this.currentTrack) return;

        try {
            // Try to get station ID from the track or from the page
            let stationId = this.currentTrack.stationId;
            if (!stationId) {
                // Try to get from station selector
                const stationSelect = getStationSelectorElement();
                stationId = stationSelect?.value;
            }
            if (!stationId) {
                // Try to get from data attribute on body
                stationId = document.body.dataset.stationId;
            }
            if (!stationId) {
                // Try to find any station ID on the page
                const stationEl = document.querySelector('[data-station-id]');
                stationId = stationEl?.dataset.stationId;
            }

            // Fetch from the API
            const url = stationId
                ? `/api/v1/analytics/now-playing?station_id=${stationId}`
                : '/api/v1/analytics/now-playing';

            const response = await fetch(url);
            if (!response.ok) return;

            const data = await response.json();

            if (data && typeof data === 'object') {
                const displayTitle = this.buildLiveNowPlayingTitle(data);
                if (!displayTitle) return;

                // Update player UI with the track info
                this.setTitle(displayTitle);

                // Second line in global player is station name.
                const stationName = this.resolveCurrentStationName();
                const artistText = this.getLiveSecondaryText();
                this.setSecondaryText(artistText);

                this.isLiveDJ = data.is_live_dj === true || data.source_type === 'live' || data.type === 'live';
                if (this.container) {
                    this.container.classList.toggle('is-on-air', this.isLive);
                    this.container.classList.toggle('is-live-dj', this.isLive && this.isLiveDJ);
                    this.container.classList.remove('is-live');
                }

                // Update artwork if media_id is available
                if (this.artworkEl && data.media_id) {
                    const artworkUrl = `/archive/${data.media_id}/artwork`;
                    this.artworkEl.innerHTML = `<img src="${artworkUrl}" alt="" onerror="this.parentElement.innerHTML='<i class=\\'bi bi-broadcast\\'></i>'">`;
                }

                // Store track timing info for local time updates
                if (data.started_at && data.ended_at) {
                    this._trackStarted = new Date(data.started_at);
                    this._trackEnded = new Date(data.ended_at);
                    this._trackDuration = Math.floor((this._trackEnded - this._trackStarted) / 1000);
                }

                // Also update the current track object
                this.currentTrack.title = displayTitle;
                this.currentTrack.artist = artistText;
                if (stationName) this.currentTrack.stationName = stationName;
                this.currentTrack.mediaId = data.media_id;

                // Start local time ticker if not already running
                this.startLiveTimeTicker();
            }
        } catch (e) {
            console.debug('Failed to fetch now-playing metadata:', e);
        }
    }

    buildLiveNowPlayingTitle(data) {
        const artist = (data.artist || '').toString().trim();
        const title = (data.title || '').toString().trim();

        if (data.is_live_dj === true || data.source_type === 'live' || data.type === 'live') {
            return title || 'Live DJ';
        }
        if (artist && title) return `${artist} - ${title}`;
        if (title) return title;
        if (artist) return artist;
        return '';
    }

    resolveCurrentStationName() {
        if (this.currentTrack?.stationName) return this.currentTrack.stationName;

        const selected = getSelectedStationInfo();
        if (selected?.stationName) return selected.stationName;

        const stationId = this.currentTrack?.stationId;
        if (stationId && Array.isArray(this.publicStations)) {
            const station = this.publicStations.find((s) => s.id === stationId);
            if (station?.name) return station.name;
        }

        return '';
    }

    getLiveSecondaryText() {
        return this.resolveCurrentStationName() || 'On-Air';
    }

    setSecondaryText(text) {
        if (!this.artistEl) return;
        this.artistEl.textContent = (text || '').toString();
        this.scheduleArtistMarqueeUpdate();
    }

    stopArtistAutoScroll() {
        if (this.artistAutoScrollRaf) {
            cancelAnimationFrame(this.artistAutoScrollRaf);
            this.artistAutoScrollRaf = null;
        }
    }

    scheduleArtistMarqueeUpdate() {
        requestAnimationFrame(() => {
            requestAnimationFrame(() => {
                this.updateArtistMarquee();
            });
        });
        setTimeout(() => this.updateArtistMarquee(), 60);
    }

    updateArtistMarquee() {
        if (!this.artistEl) return;

        this.stopArtistAutoScroll();
        this.artistEl.scrollLeft = 0;

        const initialMax = this.artistEl.scrollWidth - this.artistEl.clientWidth;
        if (initialMax <= 4) return;

        let lastTs = 0;
        let holdUntil = Date.now() + 900;
        let resetPending = false;

        const tick = (ts) => {
            this.artistAutoScrollRaf = requestAnimationFrame(tick);
            const max = this.artistEl.scrollWidth - this.artistEl.clientWidth;
            if (max <= 4) return;
            if (!lastTs) {
                lastTs = ts;
                return;
            }

            const now = Date.now();
            if (now < holdUntil) {
                lastTs = ts;
                return;
            }

            if (resetPending) {
                this.artistEl.scrollLeft = 0;
                resetPending = false;
                holdUntil = now + 800;
                lastTs = ts;
                return;
            }

            const dt = (ts - lastTs) / 1000;
            lastTs = ts;
            const next = this.artistEl.scrollLeft + (dt * 10);

            if (next >= max) {
                this.artistEl.scrollLeft = max;
                holdUntil = now + 1700;
                resetPending = true;
                return;
            }

            this.artistEl.scrollLeft = next;
        };

        this.artistAutoScrollRaf = requestAnimationFrame(tick);
    }

    startLiveTimeTicker() {
        // Stop existing ticker
        if (this._liveTimeTicker) {
            clearInterval(this._liveTimeTicker);
        }

        // Update time display every second
        this._liveTimeTicker = setInterval(() => {
            if (!this.isLive || !this._trackStarted || !this._trackDuration) return;

            const now = new Date();
            const elapsed = Math.floor((now - this._trackStarted) / 1000);

            if (elapsed >= 0 && elapsed <= this._trackDuration) {
                if (this.currentTimeEl) this.currentTimeEl.textContent = this.formatTime(elapsed);
                if (this.durationEl) this.durationEl.textContent = this.formatTime(this._trackDuration);
                // Update progress bar
                const pct = Math.min(100, (elapsed / this._trackDuration) * 100);
                if (this.progressBar) this.progressBar.style.width = pct + '%';
            } else if (elapsed > this._trackDuration) {
                // Track should have ended, fetch new metadata
                this.fetchNowPlayingMetadata();
            }
        }, 1000);
    }

    stopLiveTimeTicker() {
        if (this._liveTimeTicker) {
            clearInterval(this._liveTimeTicker);
            this._liveTimeTicker = null;
        }
    }

    playPlaylist(items, startIndex = 0) {
        // items: [{ id, title, artist }]
        this.playlist = items;
        this.playlistIndex = startIndex;
        if (items.length > 0) {
            const item = items[startIndex];
            this.playMedia(item.id, item.title, item.artist);
        }
    }

    togglePlayPause() {
        if (this.audio.paused) {
            this.audio.play().catch(e => console.error('Play error:', e));
        } else {
            this.audio.pause();
        }
    }

    prev() {
        if (this.playlist.length > 0 && this.playlistIndex > 0) {
            this.playlistIndex--;
            const item = this.playlist[this.playlistIndex];
            this.playMedia(item.id, item.title, item.artist);
        } else if (this.audio.currentTime > 3) {
            this.audio.currentTime = 0;
        }
    }

    next() {
        if (this.playlist.length > 0 && this.playlistIndex < this.playlist.length - 1) {
            this.playlistIndex++;
            const item = this.playlist[this.playlistIndex];
            this.playMedia(item.id, item.title, item.artist);
        }
    }

    seek(e) {
        if (this.isLive || !this.audio.duration) return;
        const rect = this.progressContainer.getBoundingClientRect();
        const pct = (e.clientX - rect.left) / rect.width;
        this.audio.currentTime = pct * this.audio.duration;
    }

    close() {
        this.audio.pause();
        this.audio.src = '';
        this.closeWebRTC();  // Clean up WebRTC connection
        this.stopTitleAutoScroll();
        this.stopArtistAutoScroll();
        this.currentTrack = null;
        this.isLive = false;
        this.isLiveDJ = false;
        this.stopMetadataPolling();
        this.stopLiveTimeTicker();
        this.hide();
        sessionStorage.removeItem('grimnir-player-state');
    }

    show() {
        if (this.container) {
            this.container.style.display = 'block';
            document.body.classList.add('player-active');
            // Recalculate after becoming visible; hidden elements report 0 widths.
            this.scheduleTitleMarqueeUpdate();
        }
    }

    hide() {
        if (this.container) {
            this.container.style.display = 'none';
            document.body.classList.remove('player-active');
            this.stopTitleAutoScroll();
            this.stopArtistAutoScroll();
        }
    }

    updateUI() {
        if (!this.currentTrack) return;

        // Update track info
        this.setTitle(this.currentTrack.title);
        if (this.artistEl) {
            if (this.isLive) {
                this.setSecondaryText(this.getLiveSecondaryText());
            } else {
                this.setSecondaryText(this.currentTrack.artist);
            }
        }

        // Update artwork
        if (this.artworkEl) {
            if (this.currentTrack.artwork) {
                this.artworkEl.innerHTML = `<img src="${this.currentTrack.artwork}" alt="" onerror="this.parentElement.innerHTML='<i class=\\'bi bi-music-note-beamed\\'></i>'">`;
            } else {
                this.artworkEl.innerHTML = '<i class="bi bi-broadcast"></i>';
            }
        }

        // Update live indicator
        if (this.container) {
            this.container.classList.toggle('is-on-air', this.isLive);
            this.container.classList.toggle('is-live-dj', this.isLive && this.isLiveDJ);
            this.container.classList.remove('is-live');
        }

        // Update prev/next button visibility
        if (this.prevBtn) this.prevBtn.style.visibility = this.playlist.length > 0 ? 'visible' : 'hidden';
        if (this.nextBtn) this.nextBtn.style.visibility = this.playlist.length > 0 ? 'visible' : 'hidden';

        // Show station selector for live streams.
        if (this.stationSelector) {
            this.stationSelector.style.display = this.isLive ? 'block' : 'none';
        }
        if (this.transportSelector) {
            this.transportSelector.style.display = this.isLive ? 'block' : 'none';
        }
    }

    onPlay() {
        if (this.playIcon) {
            this.playIcon.classList.remove('bi-play-fill');
            this.playIcon.classList.add('bi-pause-fill');
        }
    }

    onPause() {
        if (this.playIcon) {
            this.playIcon.classList.remove('bi-pause-fill');
            this.playIcon.classList.add('bi-play-fill');
        }
    }

    onEnded() {
        // For live streams, ignore "ended" events - stream shouldn't end
        if (this.isLive) {
            return;
        }

        if (this.playlist.length > 0 && this.playlistIndex < this.playlist.length - 1) {
            this.next();
        } else {
            this.onPause();
            if (this.progressBar) this.progressBar.style.width = '100%';
        }
    }

    onTimeUpdate() {
        if (this.isLive || !this.audio.duration) return;

        const pct = (this.audio.currentTime / this.audio.duration) * 100;
        if (this.progressBar) this.progressBar.style.width = pct + '%';
        if (this.currentTimeEl) this.currentTimeEl.textContent = this.formatTime(this.audio.currentTime);
        // Note: Don't reset reconnect counter here - let onPlaying handle it
    }

    onLoadedMetadata() {
        if (this.durationEl && this.audio.duration && !this.isLive) {
            this.durationEl.textContent = this.formatTime(this.audio.duration);
        } else if (this.durationEl && this.isLive) {
            this.durationEl.textContent = 'LIVE';
        }
        // Note: Don't reset reconnect counter here - let onPlaying handle it after stable playback
    }

    onError(e) {
        // Ignore errors during cleanup/stop
        if (!this.currentTrack) return;

        // For non-live content, show error
        if (!this.isLive) {
            showToast('Failed to play audio', 'error');
        }
        // For live streams, just stop - user can click play to retry
    }

    onStalled() {
        // Just show buffering indicator, don't auto-reconnect
        if (this.isLive && this.artistEl && this.currentTrack) {
            if (!this.artistEl.textContent.includes('Buffering')) {
                this._savedArtist = this.artistEl.textContent;
            }
            this.setSecondaryText('Buffering...');
        }
    }

    onWaiting() {
        // Buffering - show subtle indicator
        if (this.isLive && this.artistEl && this.currentTrack) {
            // Store current text if not already buffering
            if (!this.artistEl.textContent.includes('Buffering')) {
                this._savedArtist = this.artistEl.textContent;
            }
            this.setSecondaryText('Buffering...');
        }
    }

    onPlaying() {
        // Playback resumed - clear any stalled timer and restore UI
        if (this.stalledTimer) {
            clearTimeout(this.stalledTimer);
            this.stalledTimer = null;
        }

        // Mark successful connection time for cooldown
        this.lastSuccessfulConnect = Date.now();

        // Clear reconnecting flag immediately
        this.isReconnecting = false;
        this.reconnectAttempts = 0;

        // Restore second line after buffering/reconnect
        if (this.isLive && this.artistEl) {
            this.setSecondaryText(this.getLiveSecondaryText());
            this._savedArtist = null;
        }
    }

    // Reconnection with exponential backoff
    reconnectLiveStream() {
        if (!this.isLive || !this.currentTrack) return;

        // Don't reconnect if audio is playing fine
        if (!this.audio.paused && this.audio.readyState >= 3) {
            this.reconnectAttempts = 0;
            this.isReconnecting = false;
            return;
        }

        // Prevent overlapping reconnection attempts
        if (this.isReconnecting) return;

        // Cancel any pending reconnect
        if (this.reconnectTimer) clearTimeout(this.reconnectTimer);

        // Initialize reconnect state
        if (!this.reconnectAttempts) this.reconnectAttempts = 0;
        this.reconnectAttempts++;
        this.isReconnecting = true;

        // Exponential backoff: 500ms, 1s, 2s, 4s, max 8s
        const delay = Math.min(500 * Math.pow(2, this.reconnectAttempts - 1), 8000);

        console.log(`Reconnecting in ${delay}ms (attempt ${this.reconnectAttempts})...`);

        // Update UI to show reconnecting state (no toast)
        if (this.artistEl && this.reconnectAttempts > 1) {
            this.setSecondaryText('Reconnecting...');
        }

        this.reconnectTimer = setTimeout(() => {
            if (!this.isLive || !this.currentTrack) {
                this.isReconnecting = false;
                return;
            }

            // Get LQ URL for HTTP reconnection (bandwidth friendly)
            const streamUrl = this.currentTrack.lqUrl || this.currentTrack.url;
            if (!streamUrl) {
                console.warn('No valid stream URL for reconnection');
                this.isReconnecting = false;
                return;
            }
            let baseUrl = streamUrl.split('?')[0];
            // Add cache buster
            baseUrl += '?_t=' + Date.now();

            this.audio.src = baseUrl;
            this.audio.play().then(() => {
                // Play command accepted - clear reconnecting flag
                // Counter will be reset by onPlaying after stable playback
                this.isReconnecting = false;
                if (this.artistEl) {
                    this.fetchNowPlayingMetadata(); // Refresh metadata
                }
            }).catch(err => {
                console.log('Reconnect attempt failed, will retry...');
                this.isReconnecting = false;
                // Don't show error, just try again
                if (this.reconnectAttempts < 20) { // Max 20 attempts (~2.5 min total)
                    this.reconnectLiveStream();
                } else {
                    // Only notify after many failed attempts
                    console.error('Stream connection lost after multiple attempts');
                    this.setSecondaryText('Connection lost - click play to retry');
                    this.reconnectAttempts = 0;
                }
            });
        }, delay);
    }

    setTitle(title) {
        if (!this.titleEl) return;

        const text = (title || '-').toString();

        let titleSpan = this.titleEl.querySelector('.title-scroll');
        if (!titleSpan) {
            titleSpan = document.createElement('span');
            titleSpan.className = 'title-scroll';
            this.titleEl.replaceChildren(titleSpan);
        }
        titleSpan.textContent = text;

        this.scheduleTitleMarqueeUpdate();
    }

    scheduleTitleMarqueeUpdate() {
        requestAnimationFrame(() => {
            requestAnimationFrame(() => {
                this.updateTitleMarquee();
            });
        });
        setTimeout(() => this.updateTitleMarquee(), 60);
    }

    updateTitleMarquee() {
        if (!this.titleEl) return;

        const titleSpan = this.titleEl.querySelector('.title-scroll');
        if (!titleSpan) return;
        if (this.titleEl.clientWidth <= 0) {
            setTimeout(() => this.updateTitleMarquee(), 60);
            return;
        }

        this.stopTitleAutoScroll();
        this.titleEl.classList.remove('scrollable');
        this.titleEl.scrollLeft = 0;

        const overflow = titleSpan.scrollWidth - this.titleEl.clientWidth;
        if (overflow <= 4) return;

        this.titleEl.classList.add('scrollable');
        this.startTitleAutoScroll();
    }

    formatTime(seconds) {
        const m = Math.floor(seconds / 60);
        const s = Math.floor(seconds % 60);
        return m + ':' + s.toString().padStart(2, '0');
    }

    saveState() {
        if (this.currentTrack) {
            sessionStorage.setItem('grimnir-player-state', JSON.stringify({
                track: this.currentTrack,
                time: this.audio.currentTime,
                playlist: this.playlist,
                playlistIndex: this.playlistIndex,
                wasPlaying: !this.audio.paused
            }));
        }
    }

    restoreState() {
        const state = sessionStorage.getItem('grimnir-player-state');
        if (state) {
            try {
                const data = JSON.parse(state);
                this.currentTrack = data.track;
                this.playlist = data.playlist || [];
                this.playlistIndex = data.playlistIndex || 0;

                if (this.currentTrack) {
                    this.isLive = this.currentTrack.type === 'live';
                    this.updateUI();
                    this.show();

                    // Auto-resume if it was playing and we have a valid URL
                    if (data.wasPlaying && this.currentTrack.url) {
                        if (this.isLive) {
                            // For live streams, use playLive to properly initialize WebRTC
                            this.playLive(
                                this.currentTrack.url,
                                this.currentTrack.title,
                                this.currentTrack.stationId
                            );
                        } else {
                            // For regular media, just set src and play
                            this.audio.src = this.currentTrack.url;
                            if (data.time) {
                                this.audio.currentTime = data.time;
                            }
                            this.audio.play().catch(e => {
                                console.debug('Auto-play blocked:', e);
                                this.onPause();
                            });
                        }
                    }
                }
            } catch (e) {
                console.error('Failed to restore player state:', e);
            }
        }
    }
}

// Initialize global player
let globalPlayer;
document.addEventListener('DOMContentLoaded', () => {
    globalPlayer = new GlobalPlayer();
    window.globalPlayer = globalPlayer;
});

// Legacy previewTrack function - now uses global player
function previewTrack(url, element, title, artist) {
    // Extract media ID from URL
    const match = url.match(/\/media\/([^/]+)\/stream/);
    const mediaId = match ? match[1] : null;

    // Get title/artist from the row if not provided
    if (!title && element) {
        const row = element.closest('tr');
        if (row) {
            const titleCell = row.querySelector('td:nth-child(3) strong');
            const artistCell = row.querySelector('td:nth-child(4)');
            title = titleCell?.textContent || 'Unknown';
            artist = artistCell?.textContent || '';
            if (artist === '-') artist = '';
        }
    }

    if (globalPlayer && mediaId) {
        globalPlayer.playMedia(mediaId, title, artist);
    } else if (globalPlayer) {
        globalPlayer.play({ url, title: title || 'Audio', artist: artist || '', type: 'media' });
    }
}

function stopPreview() {
    if (globalPlayer) {
        globalPlayer.close();
    }
}

// Confirm dialogs for destructive actions
document.addEventListener('htmx:confirm', (e) => {
    const message = e.detail.question;
    if (message) {
        e.preventDefault();
        if (confirm(message)) {
            // Tell htmx we've already confirmed, otherwise it will run its own
            // confirm() after this handler, causing a double prompt.
            e.detail.issueRequest(true);
        }
    }
});

// Time formatting utilities
function formatDuration(seconds) {
    const h = Math.floor(seconds / 3600);
    const m = Math.floor((seconds % 3600) / 60);
    const s = Math.floor(seconds % 60);

    if (h > 0) {
        return `${h}:${m.toString().padStart(2, '0')}:${s.toString().padStart(2, '0')}`;
    }
    return `${m}:${s.toString().padStart(2, '0')}`;
}

function formatTimeAgo(date) {
    const now = new Date();
    const diff = now - new Date(date);
    const seconds = Math.floor(diff / 1000);

    if (seconds < 60) return 'just now';
    if (seconds < 3600) return `${Math.floor(seconds / 60)}m ago`;
    if (seconds < 86400) return `${Math.floor(seconds / 3600)}h ago`;
    return `${Math.floor(seconds / 86400)}d ago`;
}

// Play archive track (public pages)
function playArchiveTrack(id, title, artist) {
    if (globalPlayer) {
        globalPlayer.play({
            url: `/archive/${id}/stream`,
            title: title || 'Unknown Track',
            artist: artist || '',
            artwork: `/archive/${id}/artwork`,
            id: id,
            type: 'media'
        });
    }
}

// Export utilities for use in inline scripts
window.grimnirUtils = {
    setTheme,
    showToast,
    formatDuration,
    formatTimeAgo,
    previewTrack,
    playArchiveTrack,
    get player() { return globalPlayer; }
};
