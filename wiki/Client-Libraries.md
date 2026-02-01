# Client Libraries

Grimnir Radio provides client libraries for easy integration with external applications.

## Python Client

A full-featured Python client is available for all API operations.

### Installation

```bash
# Install dependencies
pip install requests

# Download the client
curl -O https://raw.githubusercontent.com/friendsincode/grimnir_radio/main/docs/api/examples/python/grimnir_client.py
```

Or copy `grimnir_client.py` from the repository directly into your project.

### Quick Start

```python
from grimnir_client import GrimnirClient

# Initialize with your API key (get it from your profile page)
client = GrimnirClient("https://your-instance.com", api_key="gr_your-api-key")

# Get stations
stations = client.get_stations()
for station in stations:
    print(f"{station['name']}: {station['id']}")

# Get now playing
np = client.get_now_playing(station_id)
print(f"Now Playing: {np['title']} by {np['artist']}")
```

### Station Management

```python
# List all stations
stations = client.get_stations()

# Get specific station
station = client.get_station(station_id)

# Create a station
new_station = client.create_station(
    name="My Radio Station",
    description="Community radio broadcasting",
    timezone="America/New_York"
)
```

### Media Operations

```python
# Upload media file
media = client.upload_media(
    station_id=station_id,
    file_path="/path/to/song.mp3",
    title="Song Title",
    artist="Artist Name"
)

# Get media item
media = client.get_media(media_id)

# List media for station
media_list = client.list_media(station_id)
```

### Smart Blocks

```python
# Create a smart block
block = client.create_smart_block(
    station_id=station_id,
    name="Rock Playlist",
    rules=[
        {"field": "genre", "operator": "equals", "value": "Rock"}
    ],
    limit=20,
    sort_by="random"
)

# Materialize (generate) a smart block
tracks = client.materialize_smart_block(block_id, duration_minutes=60)
```

### Playlists

```python
# Create playlist
playlist = client.create_playlist(
    station_id=station_id,
    name="Morning Show",
    tracks=[media_id_1, media_id_2, media_id_3]
)

# Get playlist
playlist = client.get_playlist(playlist_id)
```

### Schedule

```python
# Get upcoming schedule
schedule = client.get_schedule(station_id, hours=24)

# Refresh schedule
client.refresh_schedule(station_id)

# Update schedule entry
client.update_schedule_entry(
    entry_id=entry_id,
    starts_at="2026-02-01T14:00:00Z"
)
```

### Playout Control

```python
# Skip current track
client.skip_track(station_id)

# Stop playout
client.stop_playout(station_id)
```

### Analytics

```python
# Get now playing (no auth required)
np = client.get_now_playing(station_id)

# Get listener count (no auth required)
listeners = client.get_listeners(station_id)

# Get spin report (requires auth)
spins = client.get_spins(station_id, since="2026-01-01")
```

### Live Sessions

```python
# Generate DJ authorization token
token = client.generate_live_token(station_id)

# Get active live sessions
sessions = client.get_live_sessions(station_id)

# Disconnect a live session
client.disconnect_live_session(session_id)
```

### Webstreams

```python
# Create webstream with failover
webstream = client.create_webstream(
    station_id=station_id,
    name="Remote DJ",
    urls=[
        "http://primary.example.com:8000/stream",
        "http://backup.example.com:8000/stream"
    ],
    health_check_interval_ms=5000
)

# Get webstream health
health = client.get_webstream(webstream_id)
print(f"Health: {health['health_status']}")

# Manual failover
client.failover_webstream(webstream_id)

# Reset to primary
client.reset_webstream(webstream_id)
```

### Error Handling

```python
from grimnir_client import GrimnirClient, GrimnirAPIError

client = GrimnirClient("https://your-instance.com", api_key="gr_your-api-key")

try:
    stations = client.get_stations()
except GrimnirAPIError as e:
    print(f"API Error: {e.status_code} on {e.endpoint}")
    print(f"Message: {e.message}")
```

## OpenAPI / Swagger

For other languages, you can generate a client from the OpenAPI specification:

```bash
# Download the OpenAPI spec
curl -O https://raw.githubusercontent.com/friendsincode/grimnir_radio/main/api/openapi.yaml

# Generate client using openapi-generator
# Example for TypeScript
openapi-generator-cli generate \
  -i openapi.yaml \
  -g typescript-fetch \
  -o ./grimnir-client-ts

# Example for Go
openapi-generator-cli generate \
  -i openapi.yaml \
  -g go \
  -o ./grimnir-client-go
```

See [OpenAPI Generator](https://openapi-generator.tech/) for all supported languages.

## curl Examples

For quick testing or shell scripts:

```bash
# Set your API key (get it from your profile page in the web dashboard)
API_KEY="gr_your-api-key-here"

# Get stations
curl https://your-instance.com/api/v1/stations \
  -H "X-API-Key: $API_KEY"

# Get now playing (no auth required)
curl https://your-instance.com/api/v1/analytics/now-playing?station_id=UUID

# Upload media
curl -X POST https://your-instance.com/api/v1/media/upload \
  -H "X-API-Key: $API_KEY" \
  -F "file=@song.mp3" \
  -F "station_id=UUID"

# Skip current track
curl -X POST https://your-instance.com/api/v1/playout/skip \
  -H "X-API-Key: $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"station_id": "UUID"}'
```

## Rate Limits

- Default: 100 requests/minute per user
- Media uploads: 10/minute
- Contact administrator for increased limits

## Public Endpoints

These endpoints do not require authentication:

- `GET /health` - Health check
- `GET /public/stations` - List public stations
- `GET /analytics/now-playing` - Get currently playing track
- `GET /analytics/listeners` - Get listener count

## Support

- **API Documentation**: [API Reference](API-Reference)
- **OpenAPI Spec**: [api/openapi.yaml](https://github.com/friendsincode/grimnir_radio/blob/main/api/openapi.yaml)
- **Issues**: [GitHub Issues](https://github.com/friendsincode/grimnir_radio/issues)
