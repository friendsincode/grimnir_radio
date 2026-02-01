# Grimnir Radio API Documentation

Grimnir Radio provides a RESTful API for programmatic access to all station management, scheduling, and playback features.

## Quick Start

### Base URL

```
https://your-instance.com/api/v1
```

### Authentication

Most endpoints require JWT Bearer token authentication:

```bash
# Login to get a token
curl -X POST https://your-instance.com/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email": "user@example.com", "password": "yourpassword"}'

# Response:
# {"token": "eyJ...", "user": {...}}

# Use the token in subsequent requests
curl https://your-instance.com/api/v1/stations \
  -H "Authorization: Bearer eyJ..."
```

### Public Endpoints (No Auth Required)

- `GET /health` - Health check
- `GET /public/stations` - List public stations
- `GET /analytics/now-playing` - Get currently playing track
- `GET /analytics/listeners` - Get listener count

## API Specification

The full API is documented in OpenAPI 3.0 format:

- **OpenAPI Spec**: [`/api/openapi.yaml`](../../api/openapi.yaml)
- **Interactive Docs**: Use [Swagger UI](https://swagger.io/tools/swagger-ui/) or [Redoc](https://redocly.github.io/redoc/) to view

### View with Swagger UI

```bash
# Using Docker
docker run -p 8081:8080 -e SWAGGER_JSON=/api/openapi.yaml \
  -v $(pwd)/api:/api swaggerapi/swagger-ui

# Then open http://localhost:8081
```

## Client Libraries

### Python

A full-featured Python client is available:

```python
from grimnir_client import GrimnirClient

# Initialize and login
client = GrimnirClient("https://your-instance.com")
client.login("user@example.com", "password")

# Get stations
stations = client.get_stations()
for station in stations:
    print(f"{station['name']}: {station['id']}")

# Get now playing
np = client.get_now_playing(station_id)
print(f"Now Playing: {np['title']} by {np['artist']}")
```

See [`examples/python/grimnir_client.py`](examples/python/grimnir_client.py) for the full client library.

**Installation:**

```bash
pip install requests
# Then copy grimnir_client.py to your project
```

## Common Operations

### Get Station Schedule

```bash
curl "https://your-instance.com/api/v1/schedule?station_id=UUID&hours=24" \
  -H "Authorization: Bearer TOKEN"
```

### Upload Media

```bash
curl -X POST https://your-instance.com/api/v1/media/upload \
  -H "Authorization: Bearer TOKEN" \
  -F "file=@/path/to/song.mp3" \
  -F "station_id=UUID"
```

### Skip Current Track

```bash
curl -X POST https://your-instance.com/api/v1/playout/skip \
  -H "Authorization: Bearer TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"station_id": "UUID"}'
```

### Create a Smart Block

```bash
curl -X POST https://your-instance.com/api/v1/smart-blocks \
  -H "Authorization: Bearer TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "station_id": "UUID",
    "name": "Rock Playlist",
    "rules": [
      {"field": "genre", "operator": "equals", "value": "Rock"}
    ],
    "limit": 20,
    "sort_by": "random"
  }'
```

### Subscribe to Real-time Events (SSE)

```javascript
const eventSource = new EventSource(
  'https://your-instance.com/api/v1/events?types=now_playing,listener_update',
  { headers: { 'Authorization': 'Bearer TOKEN' } }
);

eventSource.onmessage = (event) => {
  const data = JSON.parse(event.data);
  console.log('Event:', data);
};
```

## Role-Based Access

| Role | Permissions |
|------|-------------|
| `dj` | Skip tracks, view media/playlists |
| `manager` | All DJ permissions + create playlists, smart blocks, schedule |
| `admin` | All manager permissions + station settings, user management |
| `platform_admin` | Full system access, all stations |

## Rate Limits

- Default: 100 requests/minute per user
- Media uploads: 10/minute
- Contact administrator for increased limits

## Error Handling

All errors return JSON with an `error` field:

```json
{
  "error": "error_code",
  "message": "Human readable message"
}
```

Common error codes:
- `unauthorized` - Invalid or missing token
- `forbidden` - Insufficient permissions
- `not_found` - Resource not found
- `validation_error` - Invalid request data

## Webhooks

Configure webhooks to receive notifications:

- `track_start` - When a new track starts playing
- `track_end` - When a track finishes
- `live_start` - When a DJ goes live
- `live_end` - When a DJ session ends

## Support

- GitHub Issues: https://github.com/friendsincode/grimnir_radio/issues
- Documentation: https://github.com/friendsincode/grimnir_radio/wiki
