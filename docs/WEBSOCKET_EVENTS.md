# WebSocket Event Streaming

Grimnir Radio provides real-time event streaming via WebSocket for monitoring live sessions, webstream failover, playback state, and system health.

## Connection

### Endpoint
```
GET /api/v1/events?types=<event_types>
```

### Authentication
Requires JWT authentication via:
- Query parameter: `?token=<jwt_token>`
- Or HTTP header: `Authorization: Bearer <jwt_token>`

### Example Connection
```javascript
const token = "your_jwt_token_here";
const ws = new WebSocket(`wss://radio.example.com/api/v1/events?types=now_playing,live.handover&token=${token}`);

ws.onmessage = (event) => {
    const data = JSON.parse(event.data);
    console.log('Event:', data.type, data.payload);
};
```

## Available Event Types

### Playback Events

#### `now_playing`
Triggered when a new track or source starts playing.

**Payload (Media):**
```json
{
    "type": "now_playing",
    "payload": {
        "station_id": "uuid",
        "mount_id": "uuid",
        "media_id": "uuid",
        "title": "Song Title",
        "artist": "Artist Name",
        "album": "Album Name",
        "duration": 180.5,
        "starts_at": "2026-01-22T10:30:00Z"
    }
}
```

**Payload (Webstream):**
```json
{
    "type": "now_playing",
    "payload": {
        "station_id": "uuid",
        "mount_id": "uuid",
        "webstream_id": "uuid",
        "webstream_name": "BBC Radio 1",
        "url": "http://stream.example.com/stream.mp3",
        "health_status": "healthy"
    }
}
```

### Live Session Events

#### `live.handover`
Triggered when a DJ takes control from automated playout.

**Payload:**
```json
{
    "type": "live.handover",
    "payload": {
        "station_id": "uuid",
        "session_id": "uuid",
        "user_id": "uuid",
        "username": "DJ Mike",
        "priority": 1,
        "transition_type": "faded",
        "handover_at": "2026-01-22T14:00:00Z"
    }
}
```

**Transition Types:**
- `immediate`: Instant cut (preempt)
- `faded`: Crossfade transition
- `delayed`: Waits for track boundary

**Priority Levels:**
- `1`: Live override (manual DJ takeover)
- `2`: Scheduled live show

#### `live.released`
Triggered when a DJ releases control back to automation.

**Payload:**
```json
{
    "type": "live.released",
    "payload": {
        "station_id": "uuid",
        "session_id": "uuid",
        "user_id": "uuid",
        "username": "DJ Mike",
        "released_at": "2026-01-22T16:00:00Z"
    }
}
```

#### `dj_connect`
Triggered when a DJ connects to a live source.

**Payload:**
```json
{
    "type": "dj_connect",
    "payload": {
        "station_id": "uuid",
        "mount_id": "uuid",
        "session_id": "uuid",
        "username": "DJ Sarah",
        "source_ip": "192.168.1.100"
    }
}
```

#### `dj_disconnect`
Triggered when a DJ disconnects from a live source.

**Payload:**
```json
{
    "type": "dj_disconnect",
    "payload": {
        "station_id": "uuid",
        "session_id": "uuid",
        "username": "DJ Sarah",
        "duration_seconds": 3600.5
    }
}
```

### Webstream Events

#### `webstream.failover`
Triggered when a webstream fails over to a backup URL.

**Payload:**
```json
{
    "type": "webstream.failover",
    "payload": {
        "webstream_id": "uuid",
        "webstream_name": "BBC Radio 1",
        "failed_url": "http://primary.example.com/stream",
        "failover_url": "http://backup.example.com/stream",
        "reason": "connection_timeout",
        "timestamp": "2026-01-22T12:30:00Z"
    }
}
```

#### `webstream.recovered`
Triggered when a webstream recovers to the primary URL.

**Payload:**
```json
{
    "type": "webstream.recovered",
    "payload": {
        "webstream_id": "uuid",
        "webstream_name": "BBC Radio 1",
        "recovered_url": "http://primary.example.com/stream",
        "timestamp": "2026-01-22T12:35:00Z"
    }
}
```

### Priority Events

#### `priority.emergency`
Triggered when emergency broadcast content is inserted.

**Payload:**
```json
{
    "type": "priority.emergency",
    "payload": {
        "station_id": "uuid",
        "media_id": "uuid",
        "priority": 0,
        "source_type": "emergency",
        "inserted_at": "2026-01-22T15:00:00Z"
    }
}
```

#### `priority.override`
Triggered when a manual override is activated.

**Payload:**
```json
{
    "type": "priority.override",
    "payload": {
        "station_id": "uuid",
        "priority": 1,
        "source_type": "live",
        "source_id": "uuid",
        "transition_type": "preempt"
    }
}
```

#### `priority.released`
Triggered when a priority source is released.

**Payload:**
```json
{
    "type": "priority.released",
    "payload": {
        "station_id": "uuid",
        "previous_priority": 1,
        "new_priority": 3,
        "released_at": "2026-01-22T15:30:00Z"
    }
}
```

### System Events

#### `health`
Periodic health status updates and crossfade notifications.

**Payload:**
```json
{
    "type": "health",
    "payload": {
        "station_id": "uuid",
        "mount_id": "uuid",
        "event": "crossfade",
        "previous_media": "uuid",
        "current_media": "uuid",
        "timestamp": "2026-01-22T10:35:00Z"
    }
}
```

#### `schedule_update`
Triggered when the schedule is refreshed or modified.

**Payload:**
```json
{
    "type": "schedule_update",
    "payload": {
        "station_id": "uuid",
        "entries_added": 15,
        "timestamp": "2026-01-22T11:00:00Z"
    }
}
```

## Subscribing to Multiple Events

You can subscribe to multiple event types by providing a comma-separated list:

```
GET /api/v1/events?types=now_playing,live.handover,live.released,webstream.failover,webstream.recovered
```

### Default Events
If no `types` parameter is provided, the connection defaults to:
- `now_playing`
- `health`

## Heartbeat

The server sends a ping every 15 seconds to keep the connection alive:

```json
{"type":"ping"}
```

Clients should handle ping messages and may respond with pong (optional).

## Error Handling

### Connection Errors
If the WebSocket connection fails authentication:
```
HTTP 401 Unauthorized
```

### Event Subscription Errors
Invalid event types are silently ignored. Only valid event types receive subscriptions.

### Reconnection Strategy
Clients should implement exponential backoff for reconnection:
1. First retry: 1 second
2. Second retry: 2 seconds
3. Third retry: 4 seconds
4. Max retry: 30 seconds

```javascript
let retryDelay = 1000;
const maxRetryDelay = 30000;

function connect() {
    const ws = new WebSocket(wsUrl);

    ws.onclose = () => {
        console.log(`Connection closed. Reconnecting in ${retryDelay}ms...`);
        setTimeout(connect, retryDelay);
        retryDelay = Math.min(retryDelay * 2, maxRetryDelay);
    };

    ws.onopen = () => {
        console.log('Connected');
        retryDelay = 1000; // Reset on successful connection
    };
}
```

## Example: Live Session Monitor

Monitor live DJ sessions with automatic reconnection:

```javascript
class LiveSessionMonitor {
    constructor(apiUrl, token) {
        this.apiUrl = apiUrl;
        this.token = token;
        this.ws = null;
        this.retryDelay = 1000;
        this.maxRetryDelay = 30000;
    }

    connect() {
        const wsUrl = `${this.apiUrl}/events?types=live.handover,live.released,dj_connect,dj_disconnect&token=${this.token}`;
        this.ws = new WebSocket(wsUrl);

        this.ws.onopen = () => {
            console.log('Live session monitor connected');
            this.retryDelay = 1000;
        };

        this.ws.onmessage = (event) => {
            const data = JSON.parse(event.data);

            if (data.type === 'ping') return;

            switch (data.type) {
                case 'live.handover':
                    this.onHandover(data.payload);
                    break;
                case 'live.released':
                    this.onRelease(data.payload);
                    break;
                case 'dj_connect':
                    this.onDJConnect(data.payload);
                    break;
                case 'dj_disconnect':
                    this.onDJDisconnect(data.payload);
                    break;
            }
        };

        this.ws.onclose = () => {
            console.log(`Connection closed. Reconnecting in ${this.retryDelay}ms...`);
            setTimeout(() => this.connect(), this.retryDelay);
            this.retryDelay = Math.min(this.retryDelay * 2, this.maxRetryDelay);
        };

        this.ws.onerror = (error) => {
            console.error('WebSocket error:', error);
        };
    }

    onHandover(payload) {
        console.log(`🎙️ ${payload.username} took over on station ${payload.station_id}`);
        console.log(`   Transition: ${payload.transition_type}, Priority: ${payload.priority}`);
    }

    onRelease(payload) {
        console.log(`👋 ${payload.username} released control`);
    }

    onDJConnect(payload) {
        console.log(`✅ ${payload.username} connected from ${payload.source_ip}`);
    }

    onDJDisconnect(payload) {
        const duration = Math.floor(payload.duration_seconds / 60);
        console.log(`❌ ${payload.username} disconnected after ${duration} minutes`);
    }

    disconnect() {
        if (this.ws) {
            this.ws.close();
        }
    }
}

// Usage
const monitor = new LiveSessionMonitor('wss://radio.example.com/api/v1', 'your_jwt_token');
monitor.connect();
```

## Example: Webstream Health Monitor

Monitor webstream health and failover events:

```javascript
const wsUrl = 'wss://radio.example.com/api/v1/events?types=webstream.failover,webstream.recovered';
const ws = new WebSocket(wsUrl + '&token=' + jwtToken);

ws.onmessage = (event) => {
    const data = JSON.parse(event.data);

    if (data.type === 'webstream.failover') {
        console.error(`⚠️ Webstream ${data.payload.webstream_name} failed over`);
        console.error(`   Failed URL: ${data.payload.failed_url}`);
        console.error(`   Using: ${data.payload.failover_url}`);
        console.error(`   Reason: ${data.payload.reason}`);

        // Alert operations team
        sendAlert('Webstream Failover', data.payload);
    }

    if (data.type === 'webstream.recovered') {
        console.log(`✅ Webstream ${data.payload.webstream_name} recovered`);
        console.log(`   URL: ${data.payload.recovered_url}`);
    }
};
```

## Performance Considerations

- **Bandwidth**: Each active WebSocket connection consumes minimal bandwidth (~1KB/event)
- **Connection Limit**: Default server limit is 10,000 concurrent WebSocket connections
- **Event Filtering**: Subscribe only to events you need to reduce bandwidth
- **Buffering**: Events are not buffered if a client disconnects

## Security

- **Authentication Required**: All WebSocket connections require valid JWT tokens
- **TLS Recommended**: Use `wss://` in production for encrypted connections
- **Token Refresh**: Refresh JWT tokens before expiration to maintain connection
- **Rate Limiting**: WebSocket connections are subject to rate limiting (100 connections per IP per minute)

## Troubleshooting

### Connection Refused
- Verify JWT token is valid and not expired
- Check that WebSocket endpoint is accessible
- Ensure firewall allows WebSocket connections (port 443 for wss://)

### Missing Events
- Verify event type spelling matches documentation
- Check that the event source (station, webstream, etc.) is active
- Confirm events are being published on the backend

### High Latency
- Check network conditions
- Reduce number of subscribed event types
- Verify server is not overloaded

## WebSocket Libraries

### JavaScript/TypeScript
- Native `WebSocket` API (browsers)
- `ws` package (Node.js)

### Python
- `websockets` library
- `socket.io-client` (Socket.IO)

### Go
- `nhooyr.io/websocket`
- `gorilla/websocket`

### Java
- Java 11+ native WebSocket client
- `tyrus-client` library

## Related Documentation

- [Live Handover API](./LIVE_HANDOVER_API.md)
- [Webstream Configuration](./WEBSTREAM_CONFIG.md)
- [Priority System](./PRIORITY_SYSTEM.md)
- [Authentication](./AUTHENTICATION.md)
