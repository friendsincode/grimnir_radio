# Migration API Guide

This guide covers using the REST API endpoints to import data from AzuraCast and LibreTime.

## Table of Contents

- [Overview](#overview)
- [Authentication](#authentication)
- [API Endpoints](#api-endpoints)
- [Examples](#examples)
- [Job Status](#job-status)
- [Error Handling](#error-handling)

## Overview

The Migration API allows you to trigger and monitor data migrations from other broadcast automation systems. Migrations run as background jobs and provide real-time progress updates.

**Supported Sources:**
- AzuraCast (via backup file)
- LibreTime (via database connection)

**Key Features:**
- Background job execution
- Real-time progress tracking
- Dry-run mode for preview
- Job cancellation support
- Statistics reporting

## Authentication

All migration endpoints require admin authentication. You must include a valid JWT token in the `Authorization` header.

```bash
# Login to get token
curl -X POST http://localhost:8080/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{
    "email": "admin@example.com",
    "password": "your-password",
    "station_id": "station-uuid"
  }'

# Response includes access_token
{
  "access_token": "eyJhbGciOiJIUzI1NiIs...",
  "user": {...}
}
```

Use the token in subsequent requests:

```bash
export TOKEN="eyJhbGciOiJIUzI1NiIs..."
curl -H "Authorization: Bearer $TOKEN" http://localhost:8080/api/v1/migrations
```

## API Endpoints

### Start AzuraCast Import

**POST** `/api/v1/migrations/azuracast`

Import data from an AzuraCast backup file.

**Request Body:**
```json
{
  "backup_path": "/path/to/azuracast-backup.tar.gz",
  "dry_run": false,
  "skip_media": false,
  "media_copy_method": "copy"
}
```

**Parameters:**
- `backup_path` (string, required): Path to AzuraCast backup tar.gz file
- `dry_run` (boolean, optional): Preview import without making changes (default: false)
- `skip_media` (boolean, optional): Skip importing media files (default: false)
- `media_copy_method` (string, optional): How to handle media files: `copy`, `symlink`, or `none` (default: `copy`)

**Response:** `202 Accepted`
```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "type": "azuracast",
  "status": "pending",
  "progress": 0,
  "total_steps": 10,
  "current_step": 0,
  "step_name": "",
  "dry_run": false,
  "started_at": "2026-01-22T10:00:00Z",
  "stats": {
    "stations_imported": 0,
    "mounts_imported": 0,
    "media_imported": 0,
    "playlists_imported": 0,
    "schedules_imported": 0,
    "users_imported": 0,
    "errors_encountered": 0
  }
}
```

---

### Start LibreTime Import

**POST** `/api/v1/migrations/libretime`

Import data from a LibreTime PostgreSQL database.

**Request Body:**
```json
{
  "database_dsn": "postgres://airtime:password@localhost/airtime",
  "dry_run": false,
  "skip_media": false,
  "media_copy_method": "copy"
}
```

**Parameters:**
- `database_dsn` (string, required): PostgreSQL connection string
- `dry_run` (boolean, optional): Preview import without making changes (default: false)
- `skip_media` (boolean, optional): Skip importing media files (default: false)
- `media_copy_method` (string, optional): How to handle media files: `copy`, `symlink`, or `none` (default: `copy`)

**Response:** `202 Accepted`
```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "type": "libretime",
  "status": "pending",
  "progress": 0,
  "total_steps": 10,
  "current_step": 0,
  "step_name": "",
  "dry_run": false,
  "started_at": "2026-01-22T10:00:00Z",
  "stats": {...}
}
```

---

### List All Migrations

**GET** `/api/v1/migrations`

List all migration jobs (past and current).

**Response:** `200 OK`
```json
{
  "migrations": [
    {
      "id": "550e8400-e29b-41d4-a716-446655440000",
      "type": "azuracast",
      "status": "completed",
      "progress": 100,
      "total_steps": 10,
      "current_step": 10,
      "step_name": "Import completed",
      "dry_run": false,
      "started_at": "2026-01-22T10:00:00Z",
      "completed_at": "2026-01-22T10:05:30Z",
      "stats": {
        "stations_imported": 3,
        "mounts_imported": 5,
        "media_imported": 1250,
        "playlists_imported": 15,
        "schedules_imported": 24,
        "users_imported": 8,
        "errors_encountered": 2
      }
    }
  ],
  "count": 1
}
```

---

### Get Migration Status

**GET** `/api/v1/migrations/{id}`

Get the current status of a specific migration job.

**Response:** `200 OK`
```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "type": "azuracast",
  "status": "running",
  "progress": 45,
  "total_steps": 10,
  "current_step": 5,
  "step_name": "Importing media",
  "dry_run": false,
  "started_at": "2026-01-22T10:00:00Z",
  "stats": {
    "stations_imported": 3,
    "mounts_imported": 5,
    "media_imported": 562,
    "playlists_imported": 0,
    "schedules_imported": 0,
    "users_imported": 0,
    "errors_encountered": 0
  }
}
```

**Status Values:**
- `pending`: Job queued but not started
- `running`: Currently executing
- `completed`: Successfully finished
- `failed`: Error occurred

---

### Cancel Migration

**DELETE** `/api/v1/migrations/{id}`

Cancel a running or pending migration job.

**Response:** `200 OK`
```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "type": "azuracast",
  "status": "failed",
  "error": "cancelled by user",
  "completed_at": "2026-01-22T10:03:00Z",
  ...
}
```

## Examples

### Complete AzuraCast Import Workflow

```bash
# 1. Login and get token
TOKEN=$(curl -s -X POST http://localhost:8080/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"admin@example.com","password":"admin","station_id":"station-1"}' \
  | jq -r '.access_token')

# 2. Start import (dry-run first to preview)
JOB_ID=$(curl -s -X POST http://localhost:8080/api/v1/migrations/azuracast \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "backup_path": "/data/azuracast-backup.tar.gz",
    "dry_run": true,
    "skip_media": false,
    "media_copy_method": "copy"
  }' | jq -r '.id')

echo "Migration job started: $JOB_ID"

# 3. Monitor progress
while true; do
  STATUS=$(curl -s -H "Authorization: Bearer $TOKEN" \
    "http://localhost:8080/api/v1/migrations/$JOB_ID" | jq -r '.status')

  PROGRESS=$(curl -s -H "Authorization: Bearer $TOKEN" \
    "http://localhost:8080/api/v1/migrations/$JOB_ID" | jq -r '.progress')

  STEP=$(curl -s -H "Authorization: Bearer $TOKEN" \
    "http://localhost:8080/api/v1/migrations/$JOB_ID" | jq -r '.step_name')

  echo "[$PROGRESS%] $STATUS - $STEP"

  if [ "$STATUS" = "completed" ] || [ "$STATUS" = "failed" ]; then
    break
  fi

  sleep 2
done

# 4. Get final stats
curl -s -H "Authorization: Bearer $TOKEN" \
  "http://localhost:8080/api/v1/migrations/$JOB_ID" | jq '.stats'
```

### LibreTime Import with Media Symlinks

```bash
# Import LibreTime data with symlinks (faster, requires shared filesystem)
curl -X POST http://localhost:8080/api/v1/migrations/libretime \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "database_dsn": "postgres://airtime:password@localhost:5432/airtime",
    "dry_run": false,
    "skip_media": false,
    "media_copy_method": "symlink"
  }'
```

### Dry-Run to Preview Changes

```bash
# Preview what would be imported without making any changes
curl -X POST http://localhost:8080/api/v1/migrations/azuracast \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "backup_path": "/data/backup.tar.gz",
    "dry_run": true
  }'
```

## Job Status

### Progress Tracking

The `progress` field is a percentage (0-100) representing overall completion.

The `current_step` and `total_steps` fields show granular progress:
- Step 1/10: Extracting backup archive
- Step 2/10: Opening database
- Step 3/10: Importing stations
- Step 4/10: Importing mounts
- Step 5/10: Importing media
- Step 6/10: Importing playlists
- Step 7/10: Importing schedules
- Step 8/10: Importing users
- Step 10/10: Import completed

### Real-Time Updates

Poll the status endpoint every 1-2 seconds for real-time progress:

```bash
watch -n 2 "curl -s -H 'Authorization: Bearer $TOKEN' \
  http://localhost:8080/api/v1/migrations/$JOB_ID | jq '.'"
```

## Error Handling

### Common Errors

**400 Bad Request**
```json
{
  "error": "backup_path is required"
}
```
*Solution:* Provide all required fields in request body

**401 Unauthorized**
```json
{
  "error": "unauthorized"
}
```
*Solution:* Include valid JWT token in Authorization header

**403 Forbidden**
```json
{
  "error": "forbidden"
}
```
*Solution:* Only admin users can trigger migrations

**404 Not Found**
```json
{
  "error": "migration not found"
}
```
*Solution:* Check migration ID is correct

### Migration Failures

When a migration fails, the `error` field contains details:

```json
{
  "id": "...",
  "status": "failed",
  "error": "connect to libretime db: connection refused",
  "stats": {
    "errors_encountered": 5
  }
}
```

**Common failure causes:**
- Backup file not found or corrupted
- Database connection refused
- Insufficient disk space for media files
- Permission denied on media directories
- Invalid database credentials

### Partial Success

A migration may complete with errors. Check the `errors_encountered` field:

```json
{
  "status": "completed",
  "stats": {
    "stations_imported": 3,
    "media_imported": 1200,
    "errors_encountered": 15
  }
}
```

Review logs for details on which items failed to import.

## Best Practices

1. **Always run dry-run first** to preview changes and identify issues
2. **Use symlinks for large media libraries** when possible (same filesystem)
3. **Monitor disk space** before importing large media collections
4. **Test database credentials** before starting LibreTime imports
5. **Keep backup files** until migration is verified successful
6. **Run migrations during off-peak hours** to minimize performance impact
7. **Cancel stuck jobs** if progress stalls for more than 5 minutes

## Limitations

- Migration jobs are stored in memory and lost on server restart
- Only admin users can trigger migrations
- One migration can run per source at a time
- Passwords cannot be migrated (users must reset passwords)
- Some source-specific features may not have direct equivalents

## See Also

- [CLI Migration Guide](../README.md#migrations) - Using command-line tools
- [AzuraCast Import Details](./AZURACAST_MIGRATION.md)
- [LibreTime Import Details](./LIBRETIME_MIGRATION.md)
- [API Authentication](./API_AUTH.md)
