# Grimnir Radio Streaming Guide

**Version:** 1.0
**Last Updated:** 2026-01-28

This guide covers audio streaming configuration, the web player, WebRTC setup, and troubleshooting.

---

## Table of Contents

- [Overview](#overview)
- [Streaming Architecture](#streaming-architecture)
- [Player Features](#player-features)
- [Reverse Proxy Configuration](#reverse-proxy-configuration)
- [WebRTC Configuration](#webrtc-configuration)
- [Troubleshooting](#troubleshooting)
- [Performance Tuning](#performance-tuning)

---

## Overview

Grimnir Radio supports two streaming methods:

| Method | Latency | Quality | Browser Support | Use Case |
|--------|---------|---------|-----------------|----------|
| **HTTP Streaming** | 5-15 seconds | Configurable (LQ/HQ) | All browsers | Default, most compatible |
| **WebRTC** | < 1 second | High quality | Modern browsers | Low-latency live broadcasts |

The web player automatically selects the best method:
1. Attempts WebRTC for HQ low-latency streaming
2. Falls back to HTTP LQ streaming if WebRTC fails
3. Users can manually select LQ streams to save bandwidth

---

## Streaming Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                    Grimnir Radio Server                      │
│                                                              │
│  ┌──────────────┐    ┌──────────────┐    ┌──────────────┐  │
│  │   Director   │───▶│  GStreamer   │───▶│  Broadcast   │  │
│  │  (Scheduler) │    │  Pipelines   │    │   Server     │  │
│  └──────────────┘    └──────────────┘    └──────────────┘  │
│                                                │             │
│                                    ┌───────────┴───────────┐│
│                                    │                       ││
│                              ┌─────▼─────┐          ┌──────▼─────┐
│                              │   HTTP    │          │   WebRTC   │
│                              │  /live/*  │          │  /webrtc/  │
│                              └───────────┘          └────────────┘
└─────────────────────────────────────────────────────────────┘
                                    │                       │
                         ┌──────────▼──────────┐   ┌────────▼────────┐
                         │   Reverse Proxy     │   │   STUN/TURN     │
                         │   (nginx/Traefik)   │   │   Servers       │
                         └──────────▼──────────┘   └─────────────────┘
                                    │
                         ┌──────────▼──────────┐
                         │    Web Browser      │
                         │   (Global Player)   │
                         └─────────────────────┘
```

### Stream Endpoints

| Endpoint | Description | Format |
|----------|-------------|--------|
| `/live/{mount}` | High quality HTTP stream | MP3/AAC |
| `/live/{mount}-lq` | Low quality HTTP stream (64kbps) | MP3 |
| `/webrtc/signal` | WebRTC signaling WebSocket | Opus |
| `/stream/{station}/{mount}` | Icecast proxy (if configured) | Various |

---

## Player Features

### Global Player

The web player (`app.js` - `GlobalPlayer` class) provides:

- **Persistent playback** across page navigation
- **Draggable/minimizable** floating player widget
- **Volume memory** (saved to localStorage)
- **Now-playing metadata** (polled every 15 seconds)
- **Progress tracking** for live streams
- **Station switcher** dropdown for multi-station setups
- **Automatic reconnection** with exponential backoff

### Playback Logic

```javascript
// Simplified flow:
1. User clicks Play on a stream
2. If HQ selected and WebRTC available:
   a. Connect to /webrtc/signal
   b. Establish peer connection
   c. On success: play via WebRTC (shows "LIVE (WebRTC)")
   d. On failure: fall back to HTTP LQ stream
3. If LQ selected:
   a. Connect directly to HTTP stream
   b. Play via HTML5 Audio element
```

### Quality Selection

Users can select stream quality on the Listen page:

- **HQ (High Quality)**: Attempts WebRTC first, falls back to HTTP HQ
- **LQ (Low Quality)**: Direct HTTP streaming at lower bitrate

The player shows the connection type:
- `LIVE` - HTTP streaming
- `LIVE (WebRTC)` - WebRTC streaming
- `Buffering...` - Temporary buffer underrun
- `Reconnecting...` - Connection recovery in progress

---

## Reverse Proxy Configuration

Proper proxy configuration is **critical** for streaming. The proxy must:

1. **Disable buffering** for audio streams
2. **Support long-lived connections** (streams run indefinitely)
3. **Pass WebSocket upgrades** for WebRTC signaling
4. **Preserve chunked transfer encoding**

### Nginx Configuration

```nginx
upstream grimnir_backend {
    server localhost:8080;
    keepalive 64;
}

server {
    listen 443 ssl http2;
    server_name radio.example.com;

    # TLS configuration
    ssl_certificate /etc/letsencrypt/live/radio.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/radio.example.com/privkey.pem;

    # Default location - standard proxying
    location / {
        proxy_pass http://grimnir_backend;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;

        # WebSocket support (for events, WebRTC signaling)
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";

        # General streaming support
        proxy_buffering off;
        proxy_cache off;
    }

    # Audio streams - special handling
    location ~ ^/(live|stream)/ {
        proxy_pass http://grimnir_backend;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;

        # CRITICAL: Disable ALL buffering for streaming
        proxy_buffering off;
        proxy_cache off;
        proxy_request_buffering off;

        # Long timeout for stream connections (24 hours)
        proxy_read_timeout 86400s;
        proxy_send_timeout 86400s;

        # Chunked transfer encoding
        chunked_transfer_encoding on;

        # Prevent response header buffering
        proxy_http_version 1.1;
    }

    # WebRTC signaling endpoint
    location /webrtc/ {
        proxy_pass http://grimnir_backend;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_set_header Host $host;
        proxy_read_timeout 86400s;
    }
}
```

### Traefik Configuration

```yaml
# traefik.yml or docker-compose labels
http:
  routers:
    grimnir:
      rule: "Host(`radio.example.com`)"
      service: grimnir
      middlewares:
        - streaming

  middlewares:
    streaming:
      buffering:
        maxRequestBodyBytes: 0
        maxResponseBodyBytes: 0
        retryExpression: "false"

  services:
    grimnir:
      loadBalancer:
        servers:
          - url: "http://grimnir:8080"
        healthCheck:
          path: /healthz
          interval: 10s
```

### Caddy Configuration

```
radio.example.com {
    reverse_proxy localhost:8080 {
        # Disable buffering for streaming
        flush_interval -1

        # Long timeouts for streams
        transport http {
            read_timeout 0
            write_timeout 0
        }
    }
}
```

### Apache Configuration

```apache
<VirtualHost *:443>
    ServerName radio.example.com

    ProxyPreserveHost On
    ProxyPass / http://localhost:8080/
    ProxyPassReverse / http://localhost:8080/

    # Disable buffering for streams
    <Location ~ "^/(live|stream)/">
        SetEnv proxy-sendchunked 1
        SetEnv proxy-sendcl 0
        ProxyPassReverse http://localhost:8080/
    </Location>

    # WebSocket support
    RewriteEngine On
    RewriteCond %{HTTP:Upgrade} websocket [NC]
    RewriteCond %{HTTP:Connection} upgrade [NC]
    RewriteRule ^/?(.*) "ws://localhost:8080/$1" [P,L]
</VirtualHost>
```

---

## WebRTC Configuration

WebRTC provides ultra-low latency streaming (< 1 second) but requires additional configuration.

### Environment Variables

```bash
# Enable WebRTC
GRIMNIR_WEBRTC_ENABLED=true

# RTP port for audio (UDP)
GRIMNIR_WEBRTC_RTP_PORT=5004

# STUN server (required for NAT traversal)
GRIMNIR_WEBRTC_STUN_URL=stun:stun.l.google.com:19302

# TURN server (optional, for restrictive firewalls)
GRIMNIR_WEBRTC_TURN_URL=turn:turn.example.com:3478
GRIMNIR_WEBRTC_TURN_USERNAME=grimnir
GRIMNIR_WEBRTC_TURN_PASSWORD=your-turn-password
```

### Firewall Rules

WebRTC requires these ports:

| Port | Protocol | Direction | Purpose |
|------|----------|-----------|---------|
| 443 | TCP | Inbound | HTTPS/WSS for signaling |
| 5004 | UDP | Inbound | RTP audio (configurable) |
| 49152-65535 | UDP | Both | ICE candidate negotiation |

```bash
# UFW example
sudo ufw allow 443/tcp
sudo ufw allow 5004/udp
sudo ufw allow 49152:65535/udp
```

### TURN Server Setup (Optional)

For users behind restrictive NATs/firewalls, you need a TURN server:

```bash
# Install coturn
sudo apt install coturn

# /etc/turnserver.conf
listening-port=3478
fingerprint
lt-cred-mech
user=grimnir:your-password
realm=radio.example.com
server-name=radio.example.com
```

### Verifying WebRTC

1. Open browser developer tools (F12)
2. Go to the Listen page
3. Click HQ play button
4. Check console for:
   - `Connecting to WebRTC signaling: wss://...`
   - `WebRTC connection state: connected`
   - Player shows `LIVE (WebRTC)`

If WebRTC fails, the console will show:
- `WebRTC connection timeout` or `WebRTC failed, falling back to HTTP LQ streaming`

---

## Troubleshooting

### Stream Won't Play

**Symptoms:** Click play, nothing happens or immediate stop

**Causes & Solutions:**

1. **Proxy buffering enabled**
   ```bash
   # Check nginx config
   grep -r "proxy_buffering" /etc/nginx/
   # Should show "proxy_buffering off" for stream locations
   ```

2. **No audio being broadcast**
   ```bash
   # Check if director is running
   curl http://localhost:8080/healthz
   # Check logs for playout activity
   docker logs grimnir-radio 2>&1 | grep -i playout
   ```

3. **Browser autoplay policy**
   - Most browsers require user interaction before playing audio
   - The player handles this, but may show a "click to play" state

### Buffering / Choppy Audio

**Symptoms:** Audio plays but with gaps, stuttering, or "Buffering..." message

**Causes & Solutions:**

1. **Network congestion**
   - Switch to LQ stream (lower bandwidth)
   - Check server bandwidth with `iftop` or similar

2. **Proxy buffering (partial)**
   ```nginx
   # Ensure ALL buffering is disabled
   proxy_buffering off;
   proxy_cache off;
   proxy_request_buffering off;
   ```

3. **Server overloaded**
   - Check CPU/memory usage
   - Consider horizontal scaling

### ResponseController Flush Errors

**Symptoms:** Log spam showing `ResponseController flush failed error="feature not supported"`

**Cause:** Reverse proxy doesn't support HTTP/2 ResponseController flushing

**Solutions:**

1. Ensure `proxy_buffering off` is set (see nginx config above)
2. The error is logged once per connection and doesn't affect playback
3. Add streaming-specific location block in nginx config

### WebRTC Connection Fails

**Symptoms:** Console shows "WebRTC connection timeout" or "falling back to HTTP"

**Causes & Solutions:**

1. **WebRTC not enabled**
   ```bash
   # Verify environment variable
   echo $GRIMNIR_WEBRTC_ENABLED  # Should be "true"
   ```

2. **Firewall blocking UDP**
   ```bash
   # Test UDP port
   nc -uzv your-server 5004
   ```

3. **Missing STUN/TURN server**
   - Verify STUN URL is reachable
   - For restrictive networks, configure TURN server

4. **Browser compatibility**
   - WebRTC requires modern browsers
   - Safari may have issues; Chrome/Firefox work best

### No Metadata / Wrong Song Info

**Symptoms:** Player shows "LIVE" but no track title/artist

**Causes & Solutions:**

1. **No now-playing data**
   ```bash
   # Check now-playing API
   curl "http://localhost:8080/api/v1/analytics/now-playing?station_id=YOUR_STATION_ID"
   ```

2. **Station ID not set**
   - Verify station ID is passed to `playLive()` function

3. **Metadata polling interval**
   - Player polls every 15 seconds
   - Wait a moment after stream starts

### Audio Out of Sync (Multiple Tabs)

**Symptoms:** Same stream in different tabs has different timing

**This is expected** - each HTTP connection buffers independently. WebRTC provides more consistent timing across connections.

---

## Performance Tuning

### Reduce Latency (HTTP)

```bash
# Lower buffer size (trade-off: more CPU)
GRIMNIR_BROADCAST_BUFFER_SIZE=2048  # bytes, default 4096
```

### Increase Listener Capacity

```bash
# Increase max connections
GRIMNIR_HTTP_MAX_CONNECTIONS=10000

# Use connection pooling in reverse proxy
upstream grimnir_backend {
    server localhost:8080;
    keepalive 256;
}
```

### Optimize for Mobile

- Default to LQ streams for mobile users
- Consider lower bitrates (64kbps) for mobile
- Test on actual devices, not just browser emulation

### CDN Integration

For large-scale deployments, consider CDN:

1. Configure CDN to pull from origin
2. Set appropriate cache headers (no-cache for live)
3. Use CDN's edge locations for geographic distribution

---

## Quick Reference

### Stream URLs

| URL Pattern | Description |
|-------------|-------------|
| `/live/main` | HQ stream for "main" mount |
| `/live/main-lq` | LQ stream for "main" mount |
| `/live/main?_t=123` | Cache-busted stream (for reconnection) |

### Player JavaScript API

```javascript
// Play a live stream
globalPlayer.playLive('/live/main', 'Station Name', 'station-uuid');

// Play a media file
globalPlayer.playMedia('media-uuid', 'Title', 'Artist');

// Toggle play/pause
globalPlayer.togglePlayPause();

// Close player
globalPlayer.close();

// Check connection type
globalPlayer.useWebRTC  // true if using WebRTC
```

### Health Checks

```bash
# Server health
curl http://localhost:8080/healthz

# Check active streams
curl http://localhost:8080/api/v1/analytics/now-playing

# Prometheus metrics
curl http://localhost:9000/metrics | grep grimnir_broadcast
```

---

**Document Version:** 1.0
**Last Updated:** 2026-01-28
