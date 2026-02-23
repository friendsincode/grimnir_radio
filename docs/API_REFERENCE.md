# Grimnir Radio API Reference

Version: 1.17.11

Base URL: `http://localhost:8080/api/v1`

This document describes all implemented API endpoints. For the full OpenAPI specification, see `api/openapi.yaml`.

## Table of Contents

- [Authentication](#authentication)
- [Stations](#stations)
- [Mounts](#mounts)
- [Media](#media)
- [Smart Blocks](#smart-blocks)
- [Clocks](#clocks)
- [Schedule](#schedule)
- [Live Input](#live-input)
- [Playout Control](#playout-control)
- [Analytics](#analytics)
- [Webhooks](#webhooks)
- [Events (WebSocket)](#events-websocket)
- [Health](#health)
- [⏳ Planned Endpoints (Future Architecture)](#-planned-endpoints-future-architecture)
  - [Priority Management](#priority-management)
  - [Executor State](#executor-state)
  - [DSP Graphs](#dsp-graphs)
  - [Webstreams](#webstreams)
  - [Migrations](#migrations)
- [Data Models](#data-models)
- [Error Responses](#error-responses)

---

## Authentication

### POST /auth/login

Authenticate with email/password and receive a JWT access token.

**Request Body:**
```json
{
  "email": "admin@example.com",
  "password": "password123",
  "station_id": "optional-station-uuid"
}
```

**Response (200 OK):**
```json
{
  "access_token": "eyJhbGciOiJIUzI1NiIs...",
  "expires_in": 900
}
```

**Token TTL:** 15 minutes (900 seconds)

**Error Codes:**
- `credentials_required` (400) - Missing email or password
- `invalid_credentials` (401) - Wrong email or password

**Example:**
```bash
curl -X POST http://localhost:8080/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"admin@example.com","password":"password123"}'
```

---

### POST /auth/refresh

Refresh an existing JWT token to extend the session.

**Authentication:** Required (Bearer token)

**Request Body:** Empty

**Response (200 OK):**
```json
{
  "access_token": "eyJhbGciOiJIUzI1NiIs...",
  "expires_in": 900
}
```

**Example:**
```bash
curl -X POST http://localhost:8080/api/v1/auth/refresh \
  -H "Authorization: Bearer $TOKEN"
```

---

## Stations

### GET /stations

List all stations.

**Authentication:** Required

**Response (200 OK):**
```json
[
  {
    "id": "uuid",
    "name": "WGMR FM",
    "description": "Community radio",
    "timezone": "America/New_York",
    "created_at": "2024-01-01T00:00:00Z",
    "updated_at": "2024-01-01T00:00:00Z"
  }
]
```

**Example:**
```bash
curl http://localhost:8080/api/v1/stations \
  -H "Authorization: Bearer $TOKEN"
```

---

### POST /stations

Create a new station.

**Authentication:** Required (admin, manager)

**Request Body:**
```json
{
  "name": "WGMR FM",
  "description": "Community radio station",
  "timezone": "America/New_York"
}
```

**Response (201 Created):**
```json
{
  "id": "uuid",
  "name": "WGMR FM",
  "description": "Community radio station",
  "timezone": "America/New_York",
  "created_at": "2024-01-01T00:00:00Z",
  "updated_at": "2024-01-01T00:00:00Z"
}
```

**Validation:**
- `name` is required
- `timezone` defaults to "UTC" if not provided

**Error Codes:**
- `name_required` (400) - Name field is missing

---

### GET /stations/:stationID

Get details for a specific station.

**Authentication:** Required

**URL Parameters:**
- `stationID` - Station UUID

**Response (200 OK):**
```json
{
  "id": "uuid",
  "name": "WGMR FM",
  "description": "Community radio",
  "timezone": "America/New_York",
  "created_at": "2024-01-01T00:00:00Z",
  "updated_at": "2024-01-01T00:00:00Z"
}
```

**Error Codes:**
- `not_found` (404) - Station does not exist

---

### PATCH /stations/:stationID

**Status:** NOT IMPLEMENTED

Update station details.

**Authentication:** Required (admin, manager)

---

## Mounts

### GET /stations/:stationID/mounts

List all mounts for a station.

**Authentication:** Required

**URL Parameters:**
- `stationID` - Station UUID

**Response (200 OK):**
```json
[
  {
    "id": "uuid",
    "station_id": "uuid",
    "name": "Main Stream",
    "url": "icecast://server:8000/stream",
    "format": "mp3",
    "bitrate": 128,
    "channels": 2,
    "sample_rate": 44100,
    "encoder_preset_id": "",
    "created_at": "2024-01-01T00:00:00Z",
    "updated_at": "2024-01-01T00:00:00Z"
  }
]
```

---

### POST /stations/:stationID/mounts

Create a new mount for a station.

**Authentication:** Required (admin, manager)

**URL Parameters:**
- `stationID` - Station UUID

**Request Body:**
```json
{
  "station_id": "optional-override",
  "name": "Main Stream",
  "url": "icecast://server:8000/stream",
  "format": "mp3",
  "bitrate_kbps": 128,
  "channels": 2,
  "sample_rate": 44100,
  "encoder_preset": {}
}
```

**Response (201 Created):**
```json
{
  "id": "uuid",
  "station_id": "uuid",
  "name": "Main Stream",
  "url": "icecast://server:8000/stream",
  "format": "mp3",
  "bitrate": 128,
  "channels": 2,
  "sample_rate": 44100,
  "encoder_preset_id": "",
  "created_at": "2024-01-01T00:00:00Z",
  "updated_at": "2024-01-01T00:00:00Z"
}
```

**Required Fields:**
- `name`
- `url`
- `format`

**Error Codes:**
- `missing_required_fields` (400) - Required field is missing

---

## Media

### POST /media/upload

Upload an audio file with metadata.

**Authentication:** Required (admin, manager, dj)

**Request:** multipart/form-data

**Form Fields:**
- `file` (required) - Audio file binary
- `station_id` (optional) - Defaults to user's station from JWT
- `title` (optional) - Track title
- `artist` (optional) - Artist name
- `album` (optional) - Album name
- `duration_seconds` (optional) - Duration in seconds (float)

**Response (201 Created):**
```json
{
  "id": "uuid",
  "station_id": "uuid",
  "title": "Example Song",
  "artist": "Example Artist",
  "album": "Example Album",
  "duration_seconds": 180.5,
  "analysis_state": "pending",
  "analysis_job_id": "uuid",
  "filename": "song.mp3"
}
```

**Max File Size:** 128 MB

**Analysis:** Media analysis is automatically queued after upload. Check `/media/:mediaID` for analysis results.

**Error Codes:**
- `invalid_multipart` (400) - Invalid multipart form data
- `file_required` (400) - No file uploaded
- `station_id_required` (400) - Station ID missing
- `invalid_duration` (400) - Duration format invalid
- `media_store_failed` (500) - File storage failed
- `analysis_queue_error` (500) - Failed to enqueue analysis

**Example:**
```bash
curl -X POST http://localhost:8080/api/v1/media/upload \
  -H "Authorization: Bearer $TOKEN" \
  -F "file=@song.mp3" \
  -F "title=Example Song" \
  -F "artist=Example Artist" \
  -F "station_id=$STATION_ID"
```

---

### GET /media/:mediaID

Get media item details and analysis results.

**Authentication:** Required

**URL Parameters:**
- `mediaID` - Media UUID

**Response (200 OK):**
```json
{
  "id": "uuid",
  "station_id": "uuid",
  "title": "Example Song",
  "artist": "Example Artist",
  "album": "Example Album",
  "duration": 180500000000,
  "path": "/media/station-uuid/media-uuid.mp3",
  "storage_key": "",
  "genre": "Rock",
  "mood": "Energetic",
  "label": "Independent",
  "language": "en",
  "explicit": false,
  "loudness_lufs": -14.5,
  "replay_gain": 0.0,
  "bpm": 120.0,
  "year": 2024,
  "tags": [],
  "cue_points": {
    "intro_end": 5.2,
    "outro_in": 175.3
  },
  "waveform": null,
  "analysis_state": "complete",
  "created_at": "2024-01-01T00:00:00Z",
  "updated_at": "2024-01-01T00:00:00Z"
}
```

**Analysis States:**
- `pending` - Waiting for analysis
- `running` - Analysis in progress
- `complete` - Analysis finished
- `failed` - Analysis failed

**Error Codes:**
- `not_found` (404) - Media item does not exist

---

## Smart Blocks

Smart Blocks are rule-based intelligent playlist generators.

### GET /smart-blocks

List all smart blocks, optionally filtered by station.

**Authentication:** Required

**Query Parameters:**
- `station_id` (optional) - Filter by station UUID

**Response (200 OK):**
```json
[
  {
    "id": "uuid",
    "station_id": "uuid",
    "name": "Morning Rock",
    "description": "Upbeat rock for morning drive",
    "rules": {
      "filters": [
        {
          "field": "genre",
          "operator": "includes",
          "value": ["Rock", "Alternative"]
        }
      ],
      "quotas": [],
      "separation": {
        "artist_minutes": 60,
        "title_minutes": 240
      }
    },
    "sequence": {
      "mode": "weighted",
      "weights": {}
    },
    "created_at": "2024-01-01T00:00:00Z",
    "updated_at": "2024-01-01T00:00:00Z"
  }
]
```

---

### POST /smart-blocks

Create a new smart block.

**Authentication:** Required (admin, manager)

**Request Body:**
```json
{
  "station_id": "uuid",
  "name": "Morning Rock",
  "description": "Upbeat rock for morning drive",
  "rules": {
    "filters": [
      {
        "field": "genre",
        "operator": "includes",
        "value": ["Rock", "Alternative"]
      },
      {
        "field": "bpm",
        "operator": "between",
        "value": [100, 140]
      }
    ],
    "quotas": [
      {
        "field": "artist",
        "min": 0,
        "max": 2
      }
    ],
    "separation": {
      "artist_minutes": 60,
      "title_minutes": 240,
      "album_minutes": 30,
      "label_minutes": 0
    }
  },
  "sequence": {
    "mode": "weighted",
    "weights": {
      "genre": 1.0,
      "mood": 0.5
    }
  }
}
```

**Response (201 Created):**
```json
{
  "id": "uuid",
  "station_id": "uuid",
  "name": "Morning Rock",
  "description": "Upbeat rock for morning drive",
  "rules": { ... },
  "sequence": { ... },
  "created_at": "2024-01-01T00:00:00Z",
  "updated_at": "2024-01-01T00:00:00Z"
}
```

**Required Fields:**
- `station_id`
- `name`

**Rule Structure:**
- **filters**: Include/exclude rules for media selection
  - Supported operators: `includes`, `excludes`, `between`, `equals`
  - Supported fields: `genre`, `mood`, `artist`, `language`, `bpm`, `year`, `explicit`, `tags`
- **quotas**: Min/max counts per field (e.g., max 2 tracks per artist)
- **separation**: Minimum minutes between repeats (artist/title/album/label)

**Sequence Modes:**
- `weighted`: Score-based selection with field weights
- `random`: Random selection (default if not specified)

---

### POST /smart-blocks/:blockID/materialize

Generate a playlist from a smart block using its rules.

**Authentication:** Required

**URL Parameters:**
- `blockID` - Smart Block UUID

**Request Body:**
```json
{
  "station_id": "uuid",
  "mount_id": "optional-uuid",
  "seed": 1234567890,
  "duration_ms": 900000
}
```

**Request Fields:**
- `station_id` (required) - Station UUID
- `seed` (optional) - Random seed for deterministic generation (defaults to timestamp)
- `duration_ms` (optional) - Target duration in milliseconds (defaults to 900000 = 15 minutes)
- `mount_id` (optional) - Mount UUID for context

**Response (200 OK):**
```json
{
  "tracks": [
    {
      "media_id": "uuid",
      "title": "Example Song",
      "artist": "Example Artist",
      "duration_ms": 180500
    }
  ],
  "total_duration_ms": 900000,
  "seed": 1234567890,
  "warnings": [],
  "unresolved": false
}
```

**Error Codes:**
- `block_id_required` (400) - Block ID missing
- `station_id_required` (400) - Station ID missing
- `unresolved` (409) - Could not fill duration with available tracks matching rules
- `materialize_failed` (500) - Generation failed

**Notes:**
- Generation is deterministic: same seed + rules + media pool = same playlist
- If insufficient tracks match rules, returns 409 with `ErrUnresolved`

---

## Clocks

Clock templates define hour-by-hour programming structure.

### GET /clocks

List all clocks for a station.

**Authentication:** Required

**Query Parameters:**
- `station_id` (required) - Station UUID

**Response (200 OK):**
```json
[
  {
    "id": "uuid",
    "station_id": "uuid",
    "name": "Morning Drive",
    "slots": [
      {
        "id": "uuid",
        "clock_hour_id": "uuid",
        "position": 0,
        "offset": 0,
        "type": "smart_block",
        "payload": {
          "smart_block_id": "uuid",
          "duration_ms": 900000
        }
      },
      {
        "id": "uuid",
        "clock_hour_id": "uuid",
        "position": 1,
        "offset": 900000000000,
        "type": "stopset",
        "payload": {
          "duration_ms": 120000
        }
      }
    ],
    "created_at": "2024-01-01T00:00:00Z",
    "updated_at": "2024-01-01T00:00:00Z"
  }
]
```

**Slot Types:**
- `smart_block` - Generate music from smart block rules
- `hard_item` - Play specific media item
- `stopset` - Commercial/promo break

---

### POST /clocks

Create a new clock template.

**Authentication:** Required (admin, manager)

**Request Body:**
```json
{
  "station_id": "uuid",
  "name": "Morning Drive",
  "slots": [
    {
      "position": 0,
      "offset_ms": 0,
      "type": "smart_block",
      "payload": {
        "smart_block_id": "uuid",
        "duration_ms": 900000
      }
    },
    {
      "position": 1,
      "offset_ms": 900000,
      "type": "stopset",
      "payload": {
        "duration_ms": 120000
      }
    }
  ]
}
```

**Response (201 Created):**
```json
{
  "id": "uuid",
  "station_id": "uuid",
  "name": "Morning Drive",
  "slots": [ ... ],
  "created_at": "2024-01-01T00:00:00Z",
  "updated_at": "2024-01-01T00:00:00Z"
}
```

**Required Fields:**
- `station_id`
- `name`

**Slot Payload Formats:**

**smart_block:**
```json
{
  "smart_block_id": "uuid",
  "duration_ms": 900000
}
```

**hard_item:**
```json
{
  "media_id": "uuid"
}
```

**stopset:**
```json
{
  "duration_ms": 120000,
  "playlist_id": "optional-uuid"
}
```

---

### POST /clocks/:clockID/simulate

Preview schedule generation for a clock.

**Authentication:** Required

**URL Parameters:**
- `clockID` - Clock UUID

**Query Parameters:**
- `minutes` (optional) - Simulation window in minutes (default: 60)

**Response (200 OK):**
```json
[
  {
    "slot_position": 0,
    "starts_at": "2024-01-01T00:00:00Z",
    "ends_at": "2024-01-01T00:15:00Z",
    "type": "smart_block",
    "tracks": [
      {
        "media_id": "uuid",
        "title": "Example Song",
        "artist": "Example Artist"
      }
    ]
  }
]
```

**Error Codes:**
- `clock_id_required` (400) - Clock ID missing
- `simulate_failed` (500) - Simulation failed

---

## Schedule

The schedule is the materialized, time-ordered list of what will play.

### GET /schedule

Get upcoming schedule entries for a station.

**Authentication:** Required

**Query Parameters:**
- `station_id` (required) - Station UUID
- `hours` (optional) - Lookahead window in hours (default: 6)

**Response (200 OK):**
```json
[
  {
    "id": "uuid",
    "station_id": "uuid",
    "mount_id": "uuid",
    "starts_at": "2024-01-01T00:00:00Z",
    "ends_at": "2024-01-01T00:03:30Z",
    "source_type": "media",
    "source_id": "uuid",
    "metadata": {
      "title": "Example Song",
      "artist": "Example Artist",
      "album": "Example Album"
    },
    "created_at": "2024-01-01T00:00:00Z",
    "updated_at": "2024-01-01T00:00:00Z"
  }
]
```

**Source Types:**
- `media` - Music/audio file
- `live` - Live DJ input
- `stopset` - Commercial break
- `webstream` - External stream (planned, not yet implemented)

---

### POST /schedule/refresh

Rebuild the rolling schedule for a station.

**Authentication:** Required (admin, manager)

**Request Body:**
```json
{
  "station_id": "uuid"
}
```

**Response (202 Accepted):**
```json
{
  "status": "refresh_queued"
}
```

**Notes:**
- Refresh runs asynchronously
- Rebuilds next 48 hours (configurable via `GRIMNIR_SCHEDULER_LOOKAHEAD_MINUTES`)

---

### PATCH /schedule/:entryID

Modify an existing schedule entry.

**Authentication:** Required (admin, manager)

**URL Parameters:**
- `entryID` - Schedule Entry UUID

**Request Body (all fields optional):**
```json
{
  "starts_at": "2024-01-01T00:05:00Z",
  "ends_at": "2024-01-01T00:08:30Z",
  "mount_id": "uuid",
  "metadata": {
    "title": "Updated Title",
    "custom_field": "value"
  }
}
```

**Response (200 OK):**
```json
{
  "id": "uuid",
  "station_id": "uuid",
  "mount_id": "uuid",
  "starts_at": "2024-01-01T00:05:00Z",
  "ends_at": "2024-01-01T00:08:30Z",
  "source_type": "media",
  "source_id": "uuid",
  "metadata": { ... },
  "created_at": "2024-01-01T00:00:00Z",
  "updated_at": "2024-01-01T00:00:10Z"
}
```

**Behavior:**
- If `starts_at` is changed without `ends_at`, duration is preserved
- Emits `schedule.update` event on the event bus

**Error Codes:**
- `not_found` (404) - Entry does not exist
- `invalid_starts_at` (400) - Invalid timestamp format
- `invalid_ends_at` (400) - Invalid timestamp format

---

## Live Input

### POST /live/authorize

Authorize a live source to connect to a mount.

**Authentication:** Required

**Request Body:**
```json
{
  "station_id": "uuid",
  "mount_id": "uuid",
  "token": "optional-auth-token"
}
```

**Response (200 OK):**
```json
{
  "authorized": true
}
```

**Notes:**
- Authorization logic is service-specific
- Used by Icecast/Shoutcast for DJ authentication

---

### POST /live/handover

Trigger live DJ takeover for a mount.

**Authentication:** Required (admin, manager)

**Request Body:**
```json
{
  "station_id": "uuid",
  "mount_id": "uuid"
}
```

**Response (200 OK):**
```json
{
  "status": "handover_initiated"
}
```

**Behavior:**
- Publishes `dj.connect` event to the event bus
- Playout manager switches from scheduled content to live input

---

## Playout Control

### POST /playout/reload

Reload (restart) the GStreamer pipeline for a mount.

**Authentication:** Required (admin, manager)

**Request Body:**
```json
{
  "station_id": "uuid",
  "mount_id": "uuid",
  "launch": "gst-launch command string"
}
```

**Response (200 OK):**
```json
{
  "status": "reloaded"
}
```

**Notes:**
- Stops existing pipeline if running
- Starts new pipeline with provided GStreamer launch string
- 15-second timeout for pipeline startup

**Error Codes:**
- `mount_and_launch_required` (400) - Missing required fields
- `pipeline_start_failed` (500) - Pipeline failed to start

---

### POST /playout/skip

Skip the currently playing track.

**Authentication:** Required (admin, manager, dj)

**Request Body:**
```json
{
  "station_id": "uuid",
  "mount_id": "uuid"
}
```

**Response (200 OK):**
```json
{
  "status": "skipped"
}
```

**Behavior:**
- Stops current pipeline
- Publishes `now_playing` event with `skipped: true`
- Next scheduled track will play

---

### POST /playout/stop

Stop playout for a mount.

**Authentication:** Required (admin, manager)

**Request Body:**
```json
{
  "mount_id": "uuid"
}
```

**Response (200 OK):**
```json
{
  "status": "stopped"
}
```

**Error Codes:**
- `mount_id_required` (400) - Mount ID missing
- `stop_failed` (500) - Stop operation failed

---

## Analytics

### GET /analytics/now-playing

Get the currently playing track for a station.

**Authentication:** Required

**Query Parameters:**
- `station_id` (required) - Station UUID

**Response (200 OK):**
```json
{
  "id": "uuid",
  "station_id": "uuid",
  "mount_id": "uuid",
  "media_id": "uuid",
  "artist": "Example Artist",
  "title": "Example Song",
  "album": "Example Album",
  "label": "Independent",
  "started_at": "2024-01-01T00:00:00Z",
  "ended_at": "0001-01-01T00:00:00Z",
  "transition": "crossfade",
  "metadata": {}
}
```

**Notes:**
- Returns most recent play history entry
- `ended_at` is zero-time if currently playing
- Returns empty object `{}` if nothing has played yet

---

### GET /analytics/spins

Get play history (rotation report) for a station.

**Authentication:** Required (admin, manager)

**Query Parameters:**
- `station_id` (required) - Station UUID
- `since` (optional) - RFC3339 timestamp (defaults to 30 days ago)

**Response (200 OK):**
```json
[
  {
    "artist": "Example Artist",
    "title": "Example Song",
    "count": 42
  },
  {
    "artist": "Another Artist",
    "title": "Another Song",
    "count": 38
  }
]
```

**Notes:**
- Groups by artist and title
- Ordered by play count descending
- Useful for DMCA reporting and rotation analysis

---

## Webhooks

### POST /webhooks/track-start

Ingest track change events from external systems.

**Authentication:** Required (admin)

**Request Body (flexible):**
```json
{
  "station_id": "uuid",
  "mount_id": "uuid",
  "title": "Example Song",
  "artist": "Example Artist",
  "custom_field": "value"
}
```

**Response (202 Accepted):**
```json
{
  "status": "received"
}
```

**Behavior:**
- Accepts any JSON payload
- Publishes to `now_playing` event on the event bus
- Useful for integration with external automation systems

---

## Events (WebSocket)

### GET /events

Subscribe to real-time events via WebSocket.

**Authentication:** Required

**Query Parameters:**
- `types` (optional) - Comma-separated event types (defaults to `now_playing,health`)

**Example Event Types:**
- `now_playing` - Track changes
- `health` - System health changes
- `schedule.update` - Schedule modifications
- `dj.connect` - Live DJ connections

**Connection:**
```javascript
const ws = new WebSocket('ws://localhost:8080/api/v1/events?types=now_playing,health');
ws.onmessage = (event) => {
  const data = JSON.parse(event.data);
  console.log(data.type, data.payload);
};
```

**Message Format:**
```json
{
  "type": "now_playing",
  "payload": {
    "station_id": "uuid",
    "mount_id": "uuid",
    "title": "Example Song",
    "artist": "Example Artist"
  }
}
```

**Ping Messages:**
```json
{
  "type": "ping"
}
```

**Notes:**
- Server sends ping every 15 seconds
- Connection auto-closes on error or context cancellation

---

## Health

### GET /api/v1/health

API health check endpoint.

**Authentication:** Not required

**Response (200 OK):**
```json
{
  "status": "ok"
}
```

---

## ⏳ Planned Endpoints (Future Architecture)

The following endpoints are **NOT YET IMPLEMENTED** but are planned as part of the multi-process architecture (API Gateway, Planner, Executor Pool, Media Engine). See `docs/ARCHITECTURE_ROADMAP.md` for implementation timeline.

---

### Priority Management

These endpoints implement the 5-tier priority ladder: Emergency (0) > Live Override (1) > Live Scheduled (2) > Automation (3) > Fallback (4).

#### POST /api/v1/priority/emergency

Insert emergency content with highest priority (0). Preempts all other content immediately.

**Authentication:** Required (admin only)

**Request Body:**
```json
{
  "station_id": "uuid",
  "source_type": "media" | "webstream",
  "source_id": "uuid",
  "crossfade_ms": 500
}
```

**Response (200 OK):**
```json
{
  "priority_id": "uuid",
  "state": "active",
  "priority_level": 0,
  "started_at": "timestamp"
}
```

**Behavior:**
- Immediately preempts current content (any priority level)
- Fades out current track over `crossfade_ms`
- Starts emergency content
- Publishes `priority.emergency` event to event bus
- Returns to previous priority level when emergency content ends

---

#### POST /api/v1/priority/override

Start a live override (priority 1). Used for unscheduled live content that should preempt automation.

**Authentication:** Required (admin, manager)

**Request Body:**
```json
{
  "station_id": "uuid",
  "source_type": "live" | "webstream",
  "source_id": "uuid",
  "crossfade_ms": 2000
}
```

**Response (200 OK):**
```json
{
  "priority_id": "uuid",
  "state": "active",
  "priority_level": 1,
  "started_at": "timestamp"
}
```

**Behavior:**
- Preempts automation (priority 3) and fallback (priority 4)
- Does NOT preempt emergency (priority 0) or live scheduled (priority 2)
- Crossfades out current track
- Publishes `priority.override` event

---

#### GET /api/v1/priority/current

Get current priority state for all stations or a specific station.

**Authentication:** Required

**Query Parameters:**
- `station_id` (optional) - Filter by station

**Response (200 OK):**
```json
{
  "states": [
    {
      "station_id": "uuid",
      "current_priority": 3,
      "current_source_type": "media",
      "current_source_id": "uuid",
      "active_priority_id": "uuid",
      "state": "playing" | "fading" | "preloading",
      "buffer_depth_samples": 4096,
      "last_heartbeat": "timestamp"
    }
  ]
}
```

---

#### DELETE /api/v1/priority/override/:priority_id

Release a live override, returning to scheduled content.

**Authentication:** Required (admin, manager)

**Response (200 OK):**
```json
{
  "status": "released",
  "returned_to_priority": 2
}
```

**Behavior:**
- Crossfades from override back to scheduled content
- Publishes `priority.released` event
- State machine transitions to next highest priority

---

### Executor State

Monitor and control per-station executor state.

#### GET /api/v1/executor/states

Get all executor states across all stations.

**Authentication:** Required (admin, manager)

**Response (200 OK):**
```json
{
  "executors": [
    {
      "id": "uuid",
      "station_id": "uuid",
      "state": "idle" | "preloading" | "playing" | "fading" | "live" | "emergency",
      "current_priority": 3,
      "current_source_id": "uuid",
      "current_source_type": "media",
      "buffer_depth_samples": 4096,
      "last_heartbeat": "timestamp",
      "metadata": {
        "now_playing": {...},
        "next_scheduled": {...}
      }
    }
  ]
}
```

**States:**
- `idle` - No content playing
- `preloading` - Loading next track into buffer
- `playing` - Currently playing content
- `fading` - Crossfade in progress
- `live` - Live input active
- `emergency` - Emergency content active

---

#### GET /api/v1/executor/states/:station_id

Get executor state for a specific station.

**Authentication:** Required

**Response (200 OK):**
```json
{
  "id": "uuid",
  "station_id": "uuid",
  "state": "playing",
  "current_priority": 3,
  "current_source_id": "uuid",
  "current_source_type": "media",
  "buffer_depth_samples": 4096,
  "last_heartbeat": "timestamp",
  "telemetry": {
    "peak_level_dbfs": -6.2,
    "rms_level_dbfs": -12.5,
    "loudness_lufs": -16.1,
    "true_peak_dbtp": -1.2,
    "underruns": 0
  }
}
```

---

### DSP Graphs

Manage graph-based DSP pipeline configurations for media engine.

#### GET /api/v1/dsp-graphs

List all DSP graph configurations.

**Authentication:** Required

**Response (200 OK):**
```json
{
  "graphs": [
    {
      "id": "uuid",
      "station_id": "uuid",
      "name": "Default Broadcast Chain",
      "description": "Standard loudness + compression + limiting",
      "nodes": [
        {
          "type": "loudness_normalize",
          "config": {
            "target_lufs": -16.0,
            "true_peak_limit_dbtp": -1.0
          }
        },
        {
          "type": "agc",
          "config": {
            "target_level_db": -14.0,
            "attack_ms": 5,
            "release_ms": 100
          }
        },
        {
          "type": "compressor",
          "config": {
            "threshold_db": -18.0,
            "ratio": 3.0,
            "attack_ms": 5,
            "release_ms": 50
          }
        },
        {
          "type": "limiter",
          "config": {
            "ceiling_dbtp": -1.0,
            "attack_ms": 1,
            "release_ms": 100
          }
        }
      ],
      "created_at": "timestamp",
      "updated_at": "timestamp"
    }
  ]
}
```

---

#### POST /api/v1/dsp-graphs

Create a new DSP graph configuration.

**Authentication:** Required (admin, manager)

**Request Body:**
```json
{
  "station_id": "uuid",
  "name": "Custom Broadcast Chain",
  "description": "Heavy compression for consistent sound",
  "nodes": [
    {
      "type": "loudness_normalize",
      "config": {
        "target_lufs": -16.0,
        "true_peak_limit_dbtp": -1.0
      }
    }
  ]
}
```

**Response (201 Created):**
```json
{
  "id": "uuid",
  "station_id": "uuid",
  "name": "Custom Broadcast Chain",
  "nodes": [...],
  "created_at": "timestamp"
}
```

**Supported Node Types:**
- `loudness_normalize` - EBU R128/ATSC A/85 loudness normalization
- `agc` - Automatic Gain Control
- `compressor` - Dynamic range compression
- `limiter` - True peak limiting
- `ducking` - Side-chain ducking for voice-overs
- `silence_detector` - Detect and alert on silence

---

#### GET /api/v1/dsp-graphs/:id

Get a specific DSP graph configuration.

**Authentication:** Required

**Response (200 OK):**
```json
{
  "id": "uuid",
  "station_id": "uuid",
  "name": "Graph Name",
  "description": "Description",
  "nodes": [...],
  "created_at": "timestamp",
  "updated_at": "timestamp"
}
```

---

#### PUT /api/v1/dsp-graphs/:id

Update a DSP graph configuration.

**Authentication:** Required (admin, manager)

**Request Body:**
```json
{
  "name": "Updated Name",
  "description": "Updated description",
  "nodes": [...]
}
```

**Response (200 OK):**
```json
{
  "id": "uuid",
  "station_id": "uuid",
  "name": "Updated Name",
  "nodes": [...],
  "updated_at": "timestamp"
}
```

**Notes:**
- Updating an active graph requires media engine reload
- Changes are NOT applied to running pipelines until reload

---

#### DELETE /api/v1/dsp-graphs/:id

Delete a DSP graph configuration.

**Authentication:** Required (admin)

**Response (200 OK):**
```json
{
  "status": "deleted"
}
```

**Error Codes:**
- `graph_in_use` (409) - Cannot delete graph currently in use

---

### Webstreams

Manage external HTTP/ICY stream sources with failover support.

#### GET /api/v1/webstreams

List all webstream configurations.

**Authentication:** Required

**Response (200 OK):**
```json
{
  "webstreams": [
    {
      "id": "uuid",
      "station_id": "uuid",
      "name": "DJ Remote Stream",
      "urls": [
        "http://primary-dj.example.com:8000/stream",
        "http://backup-dj.example.com:8000/stream"
      ],
      "health_check_interval_ms": 5000,
      "retry_limit": 3,
      "grace_window_ms": 10000,
      "preflight": true,
      "metadata_passthrough": true,
      "created_at": "timestamp",
      "updated_at": "timestamp"
    }
  ]
}
```

---

#### POST /api/v1/webstreams

Create a new webstream configuration.

**Authentication:** Required (admin, manager)

**Request Body:**
```json
{
  "station_id": "uuid",
  "name": "DJ Remote Stream",
  "urls": [
    "http://primary-dj.example.com:8000/stream",
    "http://backup-dj.example.com:8000/stream"
  ],
  "health_check_interval_ms": 5000,
  "retry_limit": 3,
  "grace_window_ms": 10000,
  "preflight": true,
  "metadata_passthrough": true
}
```

**Response (201 Created):**
```json
{
  "id": "uuid",
  "station_id": "uuid",
  "name": "DJ Remote Stream",
  "urls": [...],
  "created_at": "timestamp"
}
```

**Configuration:**
- `urls` - Ordered list of stream URLs (first = primary, rest = failovers)
- `health_check_interval_ms` - How often to probe stream health
- `retry_limit` - Max retries before failover
- `grace_window_ms` - Time to wait before switching back to primary
- `preflight` - Test connection before going live
- `metadata_passthrough` - Extract and use ICY StreamTitle

---

#### GET /api/v1/webstreams/:id

Get a specific webstream configuration.

**Authentication:** Required

**Response (200 OK):**
```json
{
  "id": "uuid",
  "station_id": "uuid",
  "name": "DJ Remote Stream",
  "urls": [...],
  "health_check_interval_ms": 5000,
  "current_url_index": 0,
  "last_health_check": "timestamp",
  "health_status": "ok" | "degraded" | "failed",
  "created_at": "timestamp",
  "updated_at": "timestamp"
}
```

---

#### PUT /api/v1/webstreams/:id

Update a webstream configuration.

**Authentication:** Required (admin, manager)

**Request Body:**
```json
{
  "name": "Updated Name",
  "urls": [...],
  "health_check_interval_ms": 10000
}
```

**Response (200 OK):**
```json
{
  "id": "uuid",
  "name": "Updated Name",
  "urls": [...],
  "updated_at": "timestamp"
}
```

---

#### DELETE /api/v1/webstreams/:id

Delete a webstream configuration.

**Authentication:** Required (admin)

**Response (200 OK):**
```json
{
  "status": "deleted"
}
```

**Error Codes:**
- `webstream_in_use` (409) - Cannot delete webstream currently scheduled or playing

---

### Migrations

Import data from AzuraCast and LibreTime installations.

#### POST /api/v1/migrations/azuracast

Import from an AzuraCast installation.

**Authentication:** Required (admin only)

**Request Body:**
```json
{
  "source_type": "backup" | "api",
  "backup_path": "/path/to/azuracast-backup.tar.gz",
  "api_url": "https://azuracast.example.com",
  "api_key": "azuracast-api-key",
  "dry_run": true,
  "station_mapping": {
    "azuracast_station_id": "grimnir_station_id"
  }
}
```

**Response (200 OK):**
```json
{
  "migration_id": "uuid",
  "state": "pending" | "running" | "complete" | "failed",
  "dry_run": true,
  "preview": {
    "stations": 2,
    "mounts": 4,
    "media_items": 1523,
    "playlists": 12,
    "schedule_entries": 48
  },
  "started_at": "timestamp"
}
```

**Migration Process:**
1. If `dry_run: true`, generates preview without applying changes
2. Validates backup structure and API connectivity
3. Creates migration job tracked by `migration_id`
4. Imports data transactionally (rollback on failure)
5. Publishes `migration.progress` events via WebSocket

---

#### POST /api/v1/migrations/libretime

Import from a LibreTime installation.

**Authentication:** Required (admin only)

**Request Body:**
```json
{
  "source_type": "backup" | "database",
  "backup_path": "/path/to/libretime-backup.tar.gz",
  "db_connection_string": "postgresql://user:pass@host/libretime",
  "dry_run": true,
  "station_mapping": {
    "default": "grimnir_station_id"
  }
}
```

**Response (200 OK):**
```json
{
  "migration_id": "uuid",
  "state": "pending",
  "dry_run": true,
  "preview": {
    "stations": 1,
    "shows": 15,
    "media_items": 892,
    "playlists": 8,
    "schedule_entries": 24
  },
  "started_at": "timestamp"
}
```

**Notes:**
- LibreTime shows are converted to clock templates where possible
- Smart playlists are converted to Smart Blocks (with rule translation)
- Complex scheduling rules may require manual review

---

#### GET /api/v1/migrations/:id

Get migration job status and results.

**Authentication:** Required (admin)

**Response (200 OK):**
```json
{
  "migration_id": "uuid",
  "state": "complete",
  "dry_run": false,
  "started_at": "timestamp",
  "completed_at": "timestamp",
  "results": {
    "stations_imported": 2,
    "mounts_imported": 4,
    "media_imported": 1523,
    "media_skipped": 12,
    "playlists_imported": 12,
    "schedule_entries_imported": 48
  },
  "errors": [],
  "warnings": [
    "Complex rule in playlist 'Evening Mix' requires manual review"
  ]
}
```

**States:**
- `pending` - Queued, not started
- `running` - In progress
- `complete` - Finished successfully
- `failed` - Error occurred

---

## Data Models

### User
```go
{
  "id": "uuid",
  "email": "user@example.com",
  "role": "admin" | "manager" | "dj",
  "created_at": "timestamp",
  "updated_at": "timestamp"
}
```

**Roles:**
- `admin` - Full access
- `manager` - Station management, programming, analytics
- `dj` - Media upload, playout skip

---

### Station
```go
{
  "id": "uuid",
  "name": "Station Name",
  "description": "Description text",
  "timezone": "America/New_York",
  "created_at": "timestamp",
  "updated_at": "timestamp"
}
```

---

### Mount
```go
{
  "id": "uuid",
  "station_id": "uuid",
  "name": "Mount Name",
  "url": "icecast://server:8000/mount",
  "format": "mp3" | "aac" | "ogg",
  "bitrate": 128,
  "channels": 2,
  "sample_rate": 44100,
  "encoder_preset_id": "uuid",
  "created_at": "timestamp",
  "updated_at": "timestamp"
}
```

---

### MediaItem
```go
{
  "id": "uuid",
  "station_id": "uuid",
  "title": "Song Title",
  "artist": "Artist Name",
  "album": "Album Name",
  "duration": 180500000000,  // nanoseconds
  "path": "/media/station/file.mp3",
  "storage_key": "",
  "genre": "Rock",
  "mood": "Energetic",
  "label": "Record Label",
  "language": "en",
  "explicit": false,
  "loudness_lufs": -14.5,
  "replay_gain": 0.0,
  "bpm": 120.0,
  "year": 2024,
  "tags": [],
  "cue_points": {
    "intro_end": 5.2,
    "outro_in": 175.3
  },
  "waveform": null,
  "analysis_state": "complete",
  "created_at": "timestamp",
  "updated_at": "timestamp"
}
```

**Analysis States:** `pending`, `running`, `complete`, `failed`

---

### SmartBlock
```go
{
  "id": "uuid",
  "station_id": "uuid",
  "name": "Block Name",
  "description": "Description",
  "rules": {
    "filters": [...],
    "quotas": [...],
    "separation": {...}
  },
  "sequence": {
    "mode": "weighted" | "random",
    "weights": {...}
  },
  "created_at": "timestamp",
  "updated_at": "timestamp"
}
```

---

### ClockHour
```go
{
  "id": "uuid",
  "station_id": "uuid",
  "name": "Clock Name",
  "slots": [
    {
      "id": "uuid",
      "clock_hour_id": "uuid",
      "position": 0,
      "offset": 0,  // nanoseconds
      "type": "smart_block" | "hard_item" | "stopset",
      "payload": {...}
    }
  ],
  "created_at": "timestamp",
  "updated_at": "timestamp"
}
```

---

### ScheduleEntry
```go
{
  "id": "uuid",
  "station_id": "uuid",
  "mount_id": "uuid",
  "starts_at": "timestamp",
  "ends_at": "timestamp",
  "source_type": "media" | "live" | "stopset",
  "source_id": "uuid",
  "metadata": {...},
  "created_at": "timestamp",
  "updated_at": "timestamp"
}
```

---

### PlayHistory
```go
{
  "id": "uuid",
  "station_id": "uuid",
  "mount_id": "uuid",
  "media_id": "uuid",
  "artist": "Artist Name",
  "title": "Song Title",
  "album": "Album Name",
  "label": "Record Label",
  "started_at": "timestamp",
  "ended_at": "timestamp",
  "transition": "crossfade" | "cut" | "fade",
  "metadata": {...}
}
```

---

## ⏳ Planned Data Models (Future Architecture)

The following data models are **NOT YET IMPLEMENTED** but are planned as part of the multi-process architecture.

---

### ExecutorState

Per-station executor state tracking. Managed by the Executor Pool, exposed via API.

```go
{
  "id": "uuid",
  "station_id": "uuid",
  "state": "idle" | "preloading" | "playing" | "fading" | "live" | "emergency",
  "current_priority": 0 | 1 | 2 | 3 | 4,
  "current_source_id": "uuid",
  "current_source_type": "media" | "live" | "webstream" | "fallback",
  "buffer_depth_samples": 4096,
  "last_heartbeat": "timestamp",
  "metadata": {
    "now_playing": {...},
    "next_scheduled": {...}
  },
  "telemetry": {
    "peak_level_dbfs": -6.2,
    "rms_level_dbfs": -12.5,
    "loudness_lufs": -16.1,
    "true_peak_dbtp": -1.2,
    "underruns": 0
  },
  "created_at": "timestamp",
  "updated_at": "timestamp"
}
```

**States:**
- `idle` - No content playing
- `preloading` - Loading next track into buffer
- `playing` - Currently playing scheduled content
- `fading` - Crossfade in progress
- `live` - Live input active (priority 1 or 2)
- `emergency` - Emergency content active (priority 0)

**Priority Levels:**
- `0` - Emergency (highest priority, immediate preemption)
- `1` - Live Override (unscheduled live content)
- `2` - Live Scheduled (scheduled live shows)
- `3` - Automation (normal scheduled content)
- `4` - Fallback (lowest priority, used when nothing else available)

---

### PrioritySource

Tracks active priority sources for the priority ladder.

```go
{
  "id": "uuid",
  "station_id": "uuid",
  "priority_level": 0 | 1 | 2 | 3 | 4,
  "source_type": "media" | "live" | "webstream" | "fallback",
  "source_id": "uuid",
  "state": "active" | "fading_in" | "fading_out" | "released",
  "crossfade_ms": 2000,
  "started_at": "timestamp",
  "ends_at": "timestamp",
  "preempted_source_id": "uuid",
  "metadata": {...},
  "created_at": "timestamp",
  "updated_at": "timestamp"
}
```

**Behavior:**
- Created when priority source becomes active
- Updated as state transitions occur
- `preempted_source_id` points to the source that was interrupted
- State machine: active → fading_out → released

---

### DSPGraph

Graph-based DSP pipeline configuration for media engine.

```go
{
  "id": "uuid",
  "station_id": "uuid",
  "name": "Default Broadcast Chain",
  "description": "Standard loudness + compression + limiting",
  "nodes": [
    {
      "type": "loudness_normalize",
      "config": {
        "target_lufs": -16.0,
        "true_peak_limit_dbtp": -1.0,
        "measurement_window_ms": 400
      }
    },
    {
      "type": "agc",
      "config": {
        "target_level_db": -14.0,
        "attack_ms": 5,
        "release_ms": 100,
        "max_gain_db": 12.0
      }
    },
    {
      "type": "compressor",
      "config": {
        "threshold_db": -18.0,
        "ratio": 3.0,
        "attack_ms": 5,
        "release_ms": 50,
        "knee_db": 6.0
      }
    },
    {
      "type": "limiter",
      "config": {
        "ceiling_dbtp": -1.0,
        "attack_ms": 1,
        "release_ms": 100
      }
    }
  ],
  "active": true,
  "created_at": "timestamp",
  "updated_at": "timestamp"
}
```

**Supported Node Types:**
- `loudness_normalize` - EBU R128/ATSC A/85 loudness normalization
- `agc` - Automatic Gain Control (slow-acting leveling)
- `compressor` - Dynamic range compression
- `limiter` - True peak limiting (brick-wall)
- `ducking` - Side-chain ducking for voice-overs
- `silence_detector` - Detect and alert on silence (deadair detection)

**Graph Execution:**
- Nodes execute in order (serial DSP chain)
- Each node outputs to next node's input
- Final output goes to encoder(s)

---

### Webstream

External HTTP/ICY stream source with failover support.

```go
{
  "id": "uuid",
  "station_id": "uuid",
  "name": "DJ Remote Stream",
  "description": "Primary DJ stream with backup",
  "urls": [
    "http://primary-dj.example.com:8000/stream",
    "http://backup-dj.example.com:8000/stream"
  ],
  "health_check_interval_ms": 5000,
  "retry_limit": 3,
  "grace_window_ms": 10000,
  "preflight": true,
  "metadata_passthrough": true,
  "current_url_index": 0,
  "last_health_check": "timestamp",
  "health_status": "ok" | "degraded" | "failed",
  "created_at": "timestamp",
  "updated_at": "timestamp"
}
```

**Configuration:**
- `urls` - Ordered list of stream URLs (first = primary, rest = failovers)
- `health_check_interval_ms` - How often to probe stream health (default: 5000ms)
- `retry_limit` - Max connection retries before failover (default: 3)
- `grace_window_ms` - Time to wait before switching back to primary after failover (default: 10000ms)
- `preflight` - Test connection before going live (default: true)
- `metadata_passthrough` - Extract and use ICY StreamTitle metadata (default: true)

**Health Status:**
- `ok` - Primary stream healthy
- `degraded` - Using failover stream
- `failed` - All streams failed

**Failover Behavior:**
1. Primary fails → retry `retry_limit` times
2. If still failing → switch to next URL in list
3. Continue down chain until successful connection
4. After `grace_window_ms` of stability, probe primary
5. If primary healthy, switch back

---

### MigrationJob

Tracks import jobs from AzuraCast/LibreTime.

```go
{
  "id": "uuid",
  "source_platform": "azuracast" | "libretime",
  "source_type": "backup" | "api" | "database",
  "dry_run": false,
  "state": "pending" | "running" | "complete" | "failed",
  "progress_percent": 75,
  "current_step": "Importing media metadata",
  "started_at": "timestamp",
  "completed_at": "timestamp",
  "results": {
    "stations_imported": 2,
    "mounts_imported": 4,
    "media_imported": 1523,
    "media_skipped": 12,
    "playlists_imported": 12,
    "schedule_entries_imported": 48
  },
  "errors": [],
  "warnings": [
    "Complex rule in playlist 'Evening Mix' requires manual review"
  ],
  "created_by_user_id": "uuid",
  "created_at": "timestamp",
  "updated_at": "timestamp"
}
```

**States:**
- `pending` - Queued, not started
- `running` - In progress
- `complete` - Finished successfully
- `failed` - Error occurred

**Progress Events:**
- Publishes `migration.progress` events via WebSocket
- Updates `progress_percent` and `current_step` as work proceeds
- Final results available in `results` object

---

## Error Responses

All errors follow this format:

```json
{
  "error": "error_code"
}
```

### Common Error Codes

**400 Bad Request:**
- `invalid_json` - Request body is not valid JSON
- `credentials_required` - Email and password are required
- `station_id_required` - Station ID is missing
- `name_required` - Name field is required
- `missing_required_fields` - One or more required fields missing
- `invalid_multipart` - Invalid multipart form data
- `file_required` - File upload is required
- `invalid_duration` - Duration format is invalid
- `media_id_required` - Media ID is missing
- `block_id_required` - Block ID is missing
- `clock_id_required` - Clock ID is missing
- `entry_id_required` - Entry ID is missing
- `mount_id_required` - Mount ID is missing
- `station_and_mount_required` - Both station and mount IDs required
- `mount_and_launch_required` - Both mount ID and launch command required
- `invalid_starts_at` - Invalid timestamp format for starts_at
- `invalid_ends_at` - Invalid timestamp format for ends_at

**401 Unauthorized:**
- `invalid_credentials` - Email or password is incorrect
- `unauthorized` - Not authenticated or invalid token

**403 Forbidden:**
- `insufficient_role` - User role lacks permission for this action

**404 Not Found:**
- `not_found` - Resource does not exist

**409 Conflict:**
- `unresolved` - Smart block could not generate sufficient tracks

**500 Internal Server Error:**
- `db_error` - Database operation failed
- `token_issue_failed` - JWT token generation failed
- `media_store_failed` - File storage failed
- `analysis_queue_error` - Failed to enqueue analysis job
- `materialize_failed` - Smart block materialization failed
- `simulate_failed` - Clock simulation failed
- `refresh_failed` - Schedule refresh failed
- `update_failed` - Update operation failed
- `authorize_failed` - Authorization check failed
- `pipeline_start_failed` - GStreamer pipeline failed to start
- `stop_failed` - Stop operation failed

**501 Not Implemented:**
- `not_implemented` - Endpoint exists but is not yet implemented

---

## Authentication

All authenticated endpoints require a `Authorization: Bearer <token>` header.

**Token Acquisition:**
1. POST to `/api/v1/auth/login` with email/password
2. Store the returned `access_token`
3. Include in subsequent requests: `Authorization: Bearer <token>`
4. Refresh before expiry using `/api/v1/auth/refresh`

**Token Expiration:** 15 minutes

**Role-Based Access Control:**

| Endpoint | Admin | Manager | DJ |
|----------|-------|---------|-----|
| GET /stations | ✓ | ✓ | ✓ |
| POST /stations | ✓ | ✓ | |
| POST /mounts | ✓ | ✓ | |
| POST /media/upload | ✓ | ✓ | ✓ |
| POST /smart-blocks | ✓ | ✓ | |
| POST /clocks | ✓ | ✓ | |
| POST /schedule/refresh | ✓ | ✓ | |
| PATCH /schedule/:id | ✓ | ✓ | |
| POST /live/handover | ✓ | ✓ | |
| POST /playout/reload | ✓ | ✓ | |
| POST /playout/skip | ✓ | ✓ | ✓ |
| POST /playout/stop | ✓ | ✓ | |
| GET /analytics/spins | ✓ | ✓ | |
| POST /webhooks/* | ✓ | | |

---

## Rate Limiting

Currently not implemented. May be added in future versions.

---

## Versioning

API version is specified in the URL path: `/api/v1/...`

Breaking changes will increment the version number.

---

## Support

For issues and feature requests, see the project repository.
