# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Grimnir Radio is a broadcast automation system built in Go 1.24. It uses a **two-binary architecture**:

- **Control Plane (`cmd/grimnirradio`)**: HTTP API server, scheduler, executor, authentication
- **Media Engine (`cmd/mediaengine`)**: GStreamer-based audio processing with gRPC control interface

The control plane communicates with the media engine via gRPC for low-latency audio control.

## Common Commands

```bash
# Build both binaries
make build

# Run tests with race detector
make test

# Run a single test
go test -v -run TestName ./path/to/package

# Full verification (tidy, fmt, vet, lint, test)
make verify

# CI check (verify + fmt-check)
make ci

# Integration tests
go test -v -tags=integration ./...

# E2E browser tests (go-rod)
make test-e2e

# Quick route verification (no browser)
make test-routes

# Format code
make fmt

# Generate protobuf code
make proto
```

### Development

```bash
# Start dev database and redis
make dev-stack

# Run control plane (requires dev-stack)
make run-control

# Run media engine
make run-media
```

## Architecture

### Key Directories

- `internal/api/` - HTTP REST handlers (26+ endpoints)
- `internal/scheduler/` - Schedule generation (30-second tick, 48h rolling window)
- `internal/executor/` - Per-station state machine (Idle→Preloading→Playing→Fading→Live/Emergency)
- `internal/priority/` - 5-tier priority ladder (Emergency→Live Override→Live Scheduled→Automation→Fallback)
- `internal/mediaengine/` - gRPC client and DSP graph configuration
- `internal/playout/` - Director and playback management
- `internal/eventbus/` - Event bus abstractions (Redis/NATS/in-memory)
- `internal/auth/` - JWT validation, 3-tier RBAC
- `internal/models/` - GORM data models
- `internal/media/` - Media storage service (filesystem and S3 backends)
- `internal/webstream/` - HTTP stream relay with failover
- `internal/live/` - Live DJ input (Icecast, RTP, SRT, WebRTC)
- `internal/migration/` - Import tools for AzuraCast and LibreTime
- `proto/mediaengine/v1/` - Protobuf definitions for media engine gRPC

### Data Flow

1. HTTP API receives requests → JWT auth → API handlers
2. Scheduler compiles smart blocks into timeline
3. Executor runs per-station state machines
4. gRPC commands sent to Media Engine
5. GStreamer pipelines process audio
6. Telemetry streams back via gRPC
7. Events broadcast via Redis/NATS to WebSocket clients

### Media Path Handling

**Critical**: Media paths in the database must be **relative** (e.g., `station_id/ab/cd/file.audio`), not absolute.

- `internal/media/storage_fs.go`: `Store()` returns relative path, `Delete()` joins with rootDir
- `internal/playout/director.go`: Joins relative paths with `mediaRoot` before sending to media engine
- The media engine receives **absolute paths** via gRPC

If you see path errors like `/var/lib/grimnir/media/var/lib/grimnir/media/...`, the database contains absolute paths that need fixing via `migrations/002_fix_media_paths.sql`.

### Smart Blocks & Scheduling

Smart blocks are rule-based playlist generators with rotation rules and artist separation. Clock templates define hour-level scheduling with slot compilation. The scheduler materializes smart blocks into concrete timeline entries on a 30-second tick.

### DSP Processing

The media engine supports 12+ DSP node types: Loudness Normalize, AGC, Compressor, Limiter, EQ, Gate, Silence Detector, Ducking, etc. These are configured as a processing graph via the `LoadGraph` gRPC method.

### Multi-Instance Deployment

For horizontal scaling, the system uses Redis-based leader election with CRC32 consistent hashing (500 virtual nodes) for executor distribution. Event bus can be Redis or NATS.

## Key Technologies

- **Go 1.24** with Chi v5 router and GORM
- **gRPC/Protobuf** for media engine control
- **GStreamer** for audio processing (external dependency)
- **PostgreSQL** (primary), MySQL, SQLite (dev)
- **Redis** for leader election and event bus
- **go-rod** for E2E browser testing
- **OpenTelemetry** for tracing, **Prometheus** for metrics

## Environment Variables

Prefer `GRIMNIR_*` prefix (falls back to `RLM_*` for compatibility). Key variables:

- `GRIMNIR_DB_DSN` - Database connection string
- `GRIMNIR_REDIS_ADDR` - Redis address for events/leadership
- `GRIMNIR_MEDIA_ENGINE_GRPC_ADDR` - Media engine gRPC address (default: localhost:9091)
- `GRIMNIR_MEDIA_ROOT` - Base directory for media files (e.g., /var/lib/grimnir/media)
- `GRIMNIR_JWT_SIGNING_KEY` - JWT signing secret

## Production Server Commands

**IMPORTANT**: On production servers, always use the `./grimnir` wrapper script, NOT direct `docker compose` commands. The wrapper handles the correct compose file ordering.

```bash
# All commands run from /srv/docker/grimnir_radio on the server
./grimnir up -d          # Start services
./grimnir down           # Stop services
./grimnir pull           # Pull latest images
./grimnir logs -f        # Follow logs
./grimnir ps             # Show status
./grimnir reset-db       # Reset database to fresh state (DESTRUCTIVE)
```

To reset the database manually:
```bash
./grimnir down
sudo rm -rf /srv/data/grimnir_radio/postgres-data
./grimnir up -d
```

## Docker Deployment

The `docker-compose.yml` uses these volume mounts:
- `media-data` → `/var/lib/grimnir/media` (inside container)
- `postgres-data` → `/var/lib/postgresql/data`

The `docker-compose.override.yml` (generated by `scripts/docker-quick-start.sh`) maps these to host paths like `/srv/data/grimnir_radio/media-data`.

**Important**: Files written to paths outside the mounted volumes (e.g., `/media/` instead of `/var/lib/grimnir/media/`) are stored in ephemeral container storage and will be lost on restart.

## Database Migrations (expand/contract discipline)

Rolling updates require v(N) and v(N+1) of the control plane to run side-by-side
against the same database during a deploy. Schema changes that work for one
version but break the other cause silent corruption or hard errors at the worst
moment. Every schema change must be split into three releases minimum:

1. **Expand**: ADD columns/tables/indexes only. Old code keeps working.
2. **Backfill + dual-write**: app writes to old + new shape; backfill populates new shape.
3. **Contract**: a later release (after every region is on the dual-write code) drops the old shape.

A "rename column" becomes 3 releases minimum.

**Enforced by `make ci`**: `cmd/migration-lint/` scans `migrations/*.sql` for
destructive operations (`DROP COLUMN`, `DROP TABLE`, `DROP INDEX`,
`RENAME COLUMN`, `RENAME TABLE`, `ALTER COLUMN ... TYPE`,
`ALTER COLUMN ... SET NOT NULL`, `TRUNCATE`) and fails the build unless the
migration includes a `-- migration-contract: <reason>` annotation. CI runs in
diff mode against `$BASE_REF` (set in the GitHub Actions workflow) so only
PR-changed files get linted; local `make migration-lint` lints everything.

**When to add the `-- migration-contract:` annotation**: when the destructive
operation is a legitimate contract phase of a multi-release sequence and the
release that wrote dual-format is already live in every region. The annotation
must name the original expand release and explain why it's safe now.

**See:** `docs/MIGRATIONS.md` for worked examples (add column, rename column,
drop column, narrow type). Use `migrations/TEMPLATE.sql` as the starting point
for any new migration.

## Versioning

Version is defined in `internal/version/version.go`. When bumping the version:

```bash
# 1. Update version in internal/version/version.go
# 2. Commit, tag, and push in one go:
git add -A && git commit -m "Message (vX.Y.Z)" && git tag -a vX.Y.Z -m "Version X.Y.Z" && git push origin main && git push origin vX.Y.Z
```

**CRITICAL**: EVERY version bump MUST include a git tag. No exceptions. Do this automatically without being asked. Tags trigger release builds and are used by the update checker.

## License

GNU AGPL v3.0-or-later. Modified network service deployments must publish source code.
