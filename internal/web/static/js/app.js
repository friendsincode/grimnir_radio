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
        // Get token from cookie
        const cookies = document.cookie.split(';');
        for (const cookie of cookies) {
            const [name, value] = cookie.trim().split('=');
            if (name === 'grimnir_token') {
                return value;
            }
        }
        return null;
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

    playLive(url, stationName) {
        this.play({
            url: url,
            title: stationName || 'Live Stream',
            artist: 'LIVE',
            artwork: null,
            id: null,
            type: 'live'
        });
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
        this.currentTrack = null;
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
    }

    onLoadedMetadata() {
        if (this.durationEl && this.audio.duration && !this.isLive) {
            this.durationEl.textContent = this.formatTime(this.audio.duration);
        } else if (this.durationEl && this.isLive) {
            this.durationEl.textContent = 'LIVE';
        }
    }

    onError(e) {
        console.error('Audio error:', e, this.audio.error);
        showToast('Failed to play audio', 'error');
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
                playlistIndex: this.playlistIndex
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
                    this.audio.src = this.currentTrack.url;
                    if (data.time && !this.isLive) {
                        this.audio.currentTime = data.time;
                    }
                    this.updateUI();
                    this.show();
                    // Don't auto-play on restore - user must click
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
