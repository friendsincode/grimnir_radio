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

    // Update server-side preference via HTMX if available
    if (window.htmx) {
        htmx.ajax('POST', '/api/v1/preferences/theme', {
            values: { theme: theme },
            swap: 'none'
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
document.addEventListener('DOMContentLoaded', () => {
    if (document.body.classList.contains('dashboard-layout')) {
        window.grimnirWS.connect();

        // Fetch initial now-playing state
        fetchNowPlaying();

        // Update on-air status
        window.grimnirWS.on('now_playing', (data) => {
            updateNowPlaying(data);
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
    }
});

// Fetch current now-playing state from API
async function fetchNowPlaying() {
    try {
        // Get station ID from the page (station selector)
        const stationSelect = document.querySelector('select[name="station_id"]');
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

    if (data && data.title) {
        container.innerHTML = `
            <i class="bi bi-music-note text-success"></i>
            <span class="text-truncate" style="max-width: 200px;" title="${data.artist} - ${data.title}">
                ${data.artist} - ${data.title}
            </span>
        `;
    } else {
        container.innerHTML = `
            <i class="bi bi-music-note"></i>
            <span class="text-body-secondary">Nothing playing</span>
        `;
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
        this.isMinimized = false;

        // WebRTC state
        this.webrtcEnabled = true;  // Try WebRTC first for live streams
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
    }

    play(track) {
        // track: { url, title, artist, artwork, id, type: 'media'|'live'|'playlist' }
        this.currentTrack = track;
        this.isLive = track.type === 'live';
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

    playLive(url, stationName, stationId) {
        this.currentTrack = {
            url: url,
            title: stationName || 'Live Stream',
            artist: 'Connecting...',
            artwork: null,
            id: null,
            type: 'live',
            stationId: stationId
        };
        this.isLive = true;
        this.updateUI();
        this.show();

        // Try WebRTC first for lower latency
        if (this.webrtcEnabled && 'RTCPeerConnection' in window) {
            this.connectWebRTC().then(connected => {
                if (!connected) {
                    // Fall back to HTTP streaming
                    console.log('WebRTC failed, falling back to HTTP streaming');
                    this.fallbackToHTTP(url);
                }
            }).catch(err => {
                console.log('WebRTC error, falling back to HTTP:', err);
                this.fallbackToHTTP(url);
            });
        } else {
            // WebRTC not available, use HTTP streaming
            this.fallbackToHTTP(url);
        }

        // Start fetching now-playing metadata
        this.startMetadataPolling();
    }

    fallbackToHTTP(url) {
        this.useWebRTC = false;
        this.closeWebRTC();
        this.audio.src = url;
        this.audio.play().catch(e => console.error('HTTP play error:', e));
    }

    async connectWebRTC() {
        // Close any existing connection
        this.closeWebRTC();

        return new Promise((resolve) => {
            const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
            const signalingUrl = `${protocol}//${window.location.host}/webrtc/signal`;

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
            // Create peer connection with STUN server
            const config = {
                iceServers: [
                    { urls: 'stun:stun.l.google.com:19302' }
                ]
            };

            this.peerConnection = new RTCPeerConnection(config);

            // Set up audio element to receive the track
            this.peerConnection.ontrack = (event) => {
                console.log('WebRTC received audio track');
                if (event.streams && event.streams[0]) {
                    this.audio.srcObject = event.streams[0];
                    this.audio.play().then(() => {
                        console.log('WebRTC audio playing');
                        if (this.artistEl) {
                            this.artistEl.textContent = 'LIVE (WebRTC)';
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
                        const artistText = this.currentTrack.artist || 'LIVE';
                        this.artistEl.textContent = artistText + ' (WebRTC)';
                    }
                } else if (state === 'failed' || state === 'disconnected') {
                    // Try to reconnect via WebRTC first (track changes cause brief disconnects)
                    if (this.currentTrack && this.isLive) {
                        this._webrtcReconnectAttempts = (this._webrtcReconnectAttempts || 0) + 1;
                        if (this._webrtcReconnectAttempts <= 3) {
                            console.log(`WebRTC reconnecting (attempt ${this._webrtcReconnectAttempts})...`);
                            if (this.artistEl) this.artistEl.textContent = 'Reconnecting...';
                            // Short delay then reconnect
                            setTimeout(() => {
                                if (this.currentTrack && this.isLive) {
                                    this.connectWebRTC().then(connected => {
                                        if (!connected && this._webrtcReconnectAttempts >= 3) {
                                            console.log('WebRTC reconnect failed, falling back to HTTP');
                                            this.fallbackToHTTP(this.currentTrack.url);
                                        }
                                    });
                                }
                            }, 500);
                        } else {
                            console.log('WebRTC reconnect failed, falling back to HTTP');
                            this.fallbackToHTTP(this.currentTrack.url);
                        }
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
                const stationSelect = document.querySelector('select[name="station_id"]');
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

            if (data && data.title) {
                // Update player UI with the track info
                if (this.titleEl) this.titleEl.textContent = data.title;

                // Show artist and album if available
                let artistText = data.artist || '';
                if (data.album) {
                    artistText += artistText ? ` • ${data.album}` : data.album;
                }
                if (!artistText) artistText = 'LIVE';
                // Add connection type indicator
                const connectionType = this.useWebRTC ? ' (WebRTC)' : '';
                if (this.artistEl) this.artistEl.textContent = artistText + connectionType;

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
                this.currentTrack.title = data.title;
                this.currentTrack.artist = data.artist || 'LIVE';
                this.currentTrack.mediaId = data.media_id;

                // Start local time ticker if not already running
                this.startLiveTimeTicker();
            }
        } catch (e) {
            console.debug('Failed to fetch now-playing metadata:', e);
        }
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
        this.currentTrack = null;
        this.isLive = false;
        this.stopMetadataPolling();
        this.stopLiveTimeTicker();
        this.hide();
        sessionStorage.removeItem('grimnir-player-state');
    }

    show() {
        if (this.container) {
            this.container.style.display = 'block';
            document.body.classList.add('player-active');
        }
    }

    hide() {
        if (this.container) {
            this.container.style.display = 'none';
            document.body.classList.remove('player-active');
        }
    }

    updateUI() {
        if (!this.currentTrack) return;

        // Update track info
        if (this.titleEl) this.titleEl.textContent = this.currentTrack.title;
        if (this.artistEl) this.artistEl.textContent = this.currentTrack.artist;

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
            this.container.classList.toggle('is-live', this.isLive);
        }

        // Update prev/next button visibility
        if (this.prevBtn) this.prevBtn.style.visibility = this.playlist.length > 0 ? 'visible' : 'hidden';
        if (this.nextBtn) this.nextBtn.style.visibility = this.playlist.length > 0 ? 'visible' : 'hidden';
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
            this.artistEl.textContent = 'Buffering...';
        }
    }

    onWaiting() {
        // Buffering - show subtle indicator
        if (this.isLive && this.artistEl && this.currentTrack) {
            // Store current text if not already buffering
            if (!this.artistEl.textContent.includes('Buffering')) {
                this._savedArtist = this.artistEl.textContent;
            }
            this.artistEl.textContent = 'Buffering...';
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

        // Restore artist text if we were showing buffering
        if (this.isLive && this.artistEl) {
            if (this._savedArtist) {
                // Append connection type indicator
                const connectionType = this.useWebRTC ? ' (WebRTC)' : '';
                this.artistEl.textContent = this._savedArtist + connectionType;
                this._savedArtist = null;
            } else if (this.artistEl.textContent === 'Connecting...') {
                // Initial connection complete
                const connectionType = this.useWebRTC ? 'LIVE (WebRTC)' : 'LIVE';
                this.artistEl.textContent = connectionType;
            }
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
            this.artistEl.textContent = 'Reconnecting...';
        }

        this.reconnectTimer = setTimeout(() => {
            if (!this.isLive || !this.currentTrack) {
                this.isReconnecting = false;
                return;
            }

            // Get base URL without cache busters
            let baseUrl = this.currentTrack.url.split('?')[0];
            // Preserve nobuffer param if switching quality
            if (this.currentTrack.url.includes('nobuffer=1')) {
                baseUrl += '?nobuffer=1&_t=' + Date.now();
            } else {
                baseUrl += '?_t=' + Date.now();
            }

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
                    if (this.artistEl) this.artistEl.textContent = 'Connection lost - click play to retry';
                    this.reconnectAttempts = 0;
                }
            });
        }, delay);
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

                    // Auto-resume if it was playing
                    if (data.wasPlaying) {
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
            e.detail.issueRequest();
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
