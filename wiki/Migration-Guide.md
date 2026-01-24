# Grimnir Radio - Migration Guide

**Version:** 1.0
**Last Updated:** 2026-01-22

This guide covers migrating from AzuraCast and LibreTime to Grimnir Radio.

## Overview

Grimnir Radio provides migration tools to help you transition from existing broadcast automation systems:

- **AzuraCast**: Import from backup tarball (.tar.gz)
- **LibreTime**: Import from PostgreSQL database
- **Airtime**: Use LibreTime importer (compatible)

## Table of Contents

- [Quick Start](#quick-start)
- [AzuraCast Migration](#azuracast-migration)
- [LibreTime/Airtime Migration](#libretime-airtime-migration)
- [API-Based Migration](#api-based-migration)
- [What Gets Imported](#what-gets-imported)
- [Important Notes](#important-notes)
- [Troubleshooting](#troubleshooting)

---

## Quick Start

### Prerequisites

1. Grimnir Radio installed and database configured
2. For AzuraCast: Backup tarball file (.tar.gz)
3. For LibreTime: Database credentials and network access

### Basic Import (AzuraCast)

```bash
# Dry run (preview only)
grimnirradio import azuracast --backup /path/to/azuracast-backup.tar.gz --dry-run

# Full import
grimnirradio import azuracast --backup /path/to/azuracast-backup.tar.gz
```

### Basic Import (LibreTime)

```bash
# Dry run (preview only)
grimnirradio import libretime \
  --db-host localhost \
  --db-user airtime \
  --db-password secretpassword \
  --dry-run

# Full import
grimnirradio import libretime \
  --db-host localhost \
  --db-user airtime \
  --db-password secretpassword
```

---

## AzuraCast Migration

### Preparing the Backup

1. Log into your AzuraCast instance
2. Navigate to **Administration** → **Backups**
3. Create a new backup or download an existing one
4. Download the `.tar.gz` file to your Grimnir Radio server

### Import Options

```bash
grimnirradio import azuracast [OPTIONS]

Options:
  --backup string       Path to AzuraCast backup tarball (.tar.gz) (required)
  --skip-media         Skip media file import (stations/playlists only)
  --dry-run            Analyze backup without importing
```

### Examples

#### Preview Import
```bash
grimnirradio import azuracast \
  --backup /path/to/azuracast-backup-20260122.tar.gz \
  --dry-run
```

Output:
```
Import Preview:
  Stations:  3
  Media:     1250
  Playlists: 45

Run without --dry-run to perform the import.
```

#### Full Import
```bash
grimnirradio import azuracast \
  --backup /path/to/azuracast-backup-20260122.tar.gz
```

#### Import Without Media
```bash
grimnirradio import azuracast \
  --backup /path/to/azuracast-backup-20260122.tar.gz \
  --skip-media
```

### What Gets Imported (AzuraCast)

| Item | Status | Notes |
|------|--------|-------|
| Stations | ✅ Complete | All station configurations |
| Mounts | ⚠️ Partial | Mount points created, GStreamer configs may need adjustment |
| Media metadata | ✅ Complete | Title, artist, album, genre, duration |
| Media files | ⚠️ Detected | File paths stored in `import_path`, actual file copy not implemented yet |
| Playlists | ⚠️ Detected | Detected but import logic not fully implemented |
| Schedules | ❌ Not implemented | |
| Users | ❌ Not implemented | |

---

## LibreTime/Airtime Migration

### Database Access

Ensure you have network access to the LibreTime PostgreSQL database:

```bash
# Test connection
psql -h localhost -U airtime -d airtime -c "SELECT version();"
```

If connection fails:
1. Check PostgreSQL is listening on network interface (edit `postgresql.conf`)
2. Check `pg_hba.conf` allows connections from your IP
3. Restart PostgreSQL: `sudo systemctl restart postgresql`

### Import Options

```bash
grimnirradio import libretime [OPTIONS]

Options:
  --db-host string      LibreTime database host (default "localhost")
  --db-port int         LibreTime database port (default 5432)
  --db-name string      LibreTime database name (default "airtime")
  --db-user string      LibreTime database user (required)
  --db-password string  LibreTime database password
  --media-path string   Path to LibreTime media directory
  --skip-media          Skip media file import
  --skip-playlists      Skip playlist import
  --skip-schedules      Skip schedule/show import
  --dry-run             Analyze database without importing
```

### Examples

#### Preview Import
```bash
grimnirradio import libretime \
  --db-host 192.168.1.100 \
  --db-user airtime \
  --db-password secretpass \
  --dry-run
```

#### Full Import
```bash
grimnirradio import libretime \
  --db-host 192.168.1.100 \
  --db-port 5432 \
  --db-name airtime \
  --db-user airtime \
  --db-password secretpass
```

#### Import Metadata Only (No Media Files)
```bash
grimnirradio import libretime \
  --db-host localhost \
  --db-user airtime \
  --db-password secretpass \
  --skip-media
```

#### Import Without Schedules
```bash
grimnirradio import libretime \
  --db-host localhost \
  --db-user airtime \
  --db-password secretpass \
  --skip-schedules
```

### What Gets Imported (LibreTime)

| Item | Status | Notes |
|------|--------|-------|
| Station configuration | ✅ Complete | From `cc_pref` table (station name, description) |
| Media metadata | ✅ Complete | From `cc_files` (title, artist, album, genre, duration, bitrate, etc.) |
| Media files | ⚠️ Detected | File paths stored in `import_path`, actual file copy not implemented yet |
| Playlists | ✅ Complete | From `cc_playlist` and `cc_playlistcontents` with fade settings |
| Shows | ✅ Complete | Imported as Clock templates from `cc_show` |
| Show instances | ⚠️ Partial | Detected but schedule materialization not implemented |
| Users | ❌ Not implemented | |

### LibreTime Show Import

LibreTime shows are imported as **Clock templates** in Grimnir Radio. This means:

- Each LibreTime show becomes a Grimnir Clock with the same name and duration
- Show content is NOT automatically imported (you'll need to rebuild show templates)
- Show schedules must be manually recreated in Grimnir Radio

**Why this approach?**
- LibreTime and Grimnir have different scheduling models
- LibreTime shows can contain complex logic that doesn't map 1:1
- Gives you flexibility to redesign your schedule in Grimnir's more flexible system

---

## API-Based Migration

For automated workflows or web UI integration, use the REST API.

### Create Migration Job

```bash
curl -X POST http://localhost:8080/api/v1/migrations \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "source_type": "azuracast",
    "options": {
      "azuracast_backup_path": "/path/to/backup.tar.gz",
      "skip_media": false
    }
  }'
```

Response:
```json
{
  "job": {
    "id": "550e8400-e29b-41d4-a716-446655440000",
    "source_type": "azuracast",
    "status": "pending",
    "dry_run": false,
    "options": {...},
    "progress": {...},
    "created_at": "2026-01-22T10:00:00Z"
  }
}
```

### Start Migration Job

```bash
curl -X POST http://localhost:8080/api/v1/migrations/{job_id}/start \
  -H "Authorization: Bearer $TOKEN"
```

### Check Progress

```bash
curl http://localhost:8080/api/v1/migrations/{job_id} \
  -H "Authorization: Bearer $TOKEN"
```

Response:
```json
{
  "job": {
    "id": "550e8400-e29b-41d4-a716-446655440000",
    "status": "running",
    "progress": {
      "phase": "importing_media",
      "current_step": "Imported 500/1250 media files",
      "total_steps": 5,
      "completed_steps": 2,
      "percentage": 40,
      "media_total": 1250,
      "media_imported": 500
    }
  }
}
```

### List All Migration Jobs

```bash
curl http://localhost:8080/api/v1/migrations \
  -H "Authorization: Bearer $TOKEN"
```

### Cancel Running Job

```bash
curl -X POST http://localhost:8080/api/v1/migrations/{job_id}/cancel \
  -H "Authorization: Bearer $TOKEN"
```

### Delete Migration Job

```bash
curl -X DELETE http://localhost:8080/api/v1/migrations/{job_id} \
  -H "Authorization: Bearer $TOKEN"
```

### Real-Time Progress via WebSocket

Connect to the WebSocket events endpoint and subscribe to migration events:

```javascript
const ws = new WebSocket('ws://localhost:8080/api/v1/events?types=migration');

ws.onmessage = (event) => {
  const data = JSON.parse(event.data);

  if (data.type === 'migration') {
    console.log('Job:', data.payload.job_id);
    console.log('Status:', data.payload.status);
    console.log('Progress:', data.payload.progress);
  }
};
```

---

## What Gets Imported

### Common Items (Both Systems)

#### ✅ Fully Implemented
- **Station metadata**: Name, description, timezone
- **Media metadata**: Title, artist, album, genre, year, track number, duration
- **Playlist structures**: Playlist names, descriptions, items, ordering
- **Playlist item settings**: Fade in/out durations, positions

#### ⚠️ Partially Implemented
- **Media files**: File paths are imported to `import_path` field, but actual file copying/moving is not yet implemented
- **Schedule templates**: Shows/clocks are imported as templates, but schedule materialization is manual

#### ❌ Not Yet Implemented
- **User accounts**: Passwords cannot be migrated for security reasons
- **Live DJ sessions**: Historical data only
- **Listener statistics**: Historical data only
- **Webstream relays**: Configuration must be manually recreated

### AzuraCast Specific

| Feature | Status | Notes |
|---------|--------|-------|
| Multi-station setup | ✅ Complete | All stations imported |
| Custom fields | ❌ Not implemented | |
| Podcasts | ❌ Not implemented | |
| Remote relays | ❌ Not implemented | |
| Listener requests | ❌ Not implemented | Historical data only |

### LibreTime Specific

| Feature | Status | Notes |
|---------|--------|-------|
| Show templates | ✅ Complete | Imported as Clocks |
| Smart blocks | ⚠️ Manual | Need manual recreation in Grimnir |
| Podcast episodes | ❌ Not implemented | |
| Celery tasks | ❌ Not applicable | Different architecture |

---

## Important Notes

### Data Integrity

- **Always run a dry-run first** to preview what will be imported
- **Backup your Grimnir database** before running a migration
- **Verify imported data** after migration completes
- **Media files are not automatically copied** - file paths are stored in `import_path` field

### ID Mapping

The migration system tracks ID mappings between source and destination systems:

```json
{
  "mappings": {
    "station_1": {
      "old_id": "1",
      "new_id": "550e8400-e29b-41d4-a716-446655440000",
      "type": "station",
      "name": "My Radio Station"
    },
    "media_42": {
      "old_id": "42",
      "new_id": "660e8400-e29b-41d4-a716-446655440001",
      "type": "media",
      "name": "Song Title.mp3"
    }
  }
}
```

These mappings are stored in the migration job result and can be used for post-import processing.

### Post-Import Tasks

After migration, you'll need to:

1. **Verify stations** - Check station configurations match your requirements
2. **Media files** - Copy media files to Grimnir's media directory (manual step)
3. **Update file paths** - Update `path` field in `media_items` table to match new locations
4. **Recreate schedules** - Build new schedules using imported Clock templates
5. **Configure mounts** - Adjust GStreamer encoder settings if needed
6. **Test playback** - Verify audio output works correctly
7. **Create users** - Manually create user accounts (passwords can't be migrated)
8. **Configure live inputs** - Set up harbor/icecast live source endpoints

### Performance Considerations

- **Large libraries**: Imports with >10,000 media items may take 30+ minutes
- **Network speed**: LibreTime import speed depends on database connection speed
- **Database load**: Consider running migrations during off-peak hours
- **Progress tracking**: Use `--dry-run` first to estimate total time

---

## Troubleshooting

### "Backup file does not exist"

**Problem**: AzuraCast backup file not found

**Solutions**:
- Check file path is absolute (not relative)
- Verify file exists: `ls -lh /path/to/backup.tar.gz`
- Check file permissions: `chmod 644 /path/to/backup.tar.gz`

### "Failed to connect to LibreTime database"

**Problem**: Cannot connect to LibreTime PostgreSQL database

**Solutions**:
1. Test connection manually:
   ```bash
   psql -h hostname -U airtime -d airtime -c "SELECT version();"
   ```
2. Check PostgreSQL is listening on network:
   ```bash
   sudo netstat -an | grep 5432
   ```
3. Edit `/etc/postgresql/*/main/postgresql.conf`:
   ```
   listen_addresses = '*'
   ```
4. Edit `/etc/postgresql/*/main/pg_hba.conf`:
   ```
   host    airtime    airtime    0.0.0.0/0    md5
   ```
5. Restart PostgreSQL:
   ```bash
   sudo systemctl restart postgresql
   ```

### "Backup file must be a .tar.gz archive"

**Problem**: AzuraCast backup is wrong format

**Solutions**:
- Ensure backup was downloaded from AzuraCast (not another system)
- Check file extension is `.tar.gz`
- Verify file is valid: `tar -tzf backup.tar.gz | head`

### "Job not found"

**Problem**: Migration job ID doesn't exist

**Solutions**:
- List all jobs: `curl http://localhost:8080/api/v1/migrations`
- Check job ID is correct (UUID format)
- Job may have been deleted after completion

### "Media import not fully implemented"

**Warning**: This is expected behavior

**Explanation**:
- Media metadata is imported (title, artist, etc.)
- File paths are stored in `import_path` field
- Actual file copying must be done manually

**Manual file migration**:
```bash
# Example: Copy LibreTime media to Grimnir
rsync -av /srv/airtime/stor/ /var/lib/grimnir/media/

# Update database paths (example)
psql -d grimnir -c "
  UPDATE media_items
  SET path = '/var/lib/grimnir/media/' || SUBSTRING(import_path FROM '[^/]*$')
  WHERE import_path IS NOT NULL;
"
```

### "Validation failed"

**Problem**: Import validation errors

**Solutions**:
- Read validation error messages carefully
- For AzuraCast: Ensure backup file is valid and not corrupted
- For LibreTime: Verify database credentials and network access
- Run dry-run to see detailed validation results

### Import Fails Partway Through

**Problem**: Import stops with error mid-process

**Recovery steps**:
1. Check logs for error details:
   ```bash
   grep ERROR /var/log/grimnir/grimnir-radio.log | tail -50
   ```
2. Fix the underlying issue (disk space, permissions, etc.)
3. Delete the partial import:
   ```sql
   -- Identify stations created by failed import
   SELECT * FROM stations WHERE created_at > '2026-01-22 10:00:00';

   -- Delete if needed (use with caution!)
   DELETE FROM stations WHERE id = 'station-uuid';
   ```
4. Re-run import

---

## Support

For migration issues:

1. **Check logs**: `/var/log/grimnir/grimnir-radio.log`
2. **GitHub Issues**: https://github.com/friendsincode/grimnir_radio/issues
3. **Community Forum**: https://community.grimnir.radio

## Next Steps

After successful migration:

- Read [Multi-Instance](Multi-Instance) for scaling guidance
- Review [Observability](Observability) for monitoring setup
- Check [Production Deployment](Production-Deployment) for deployment best practices
