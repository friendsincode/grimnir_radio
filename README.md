# Grimnir Radio

Version: 0.0.1-alpha (Phase 4C Complete)

Grimnir Radio is a modern radio automation system built in Go, featuring a multi-process architecture with separated control plane and media engine, live DJ input, and HTTP stream relay with automatic failover.

## Architecture

Grimnir Radio uses a **two-binary architecture** for process isolation:

- **Control Plane (`grimnirradio`)**: HTTP API, database, scheduling, priority management, authentication
- **Media Engine (`mediaengine`)**: GStreamer pipelines, DSP processing, audio telemetry via gRPC

Communication between components uses gRPC for low-latency, reliable control of audio operations.

## Current Status

**Phase 4C Complete (100%)** - Live Input & Webstream Relay
- ✅ Token-based live DJ authorization with session management
- ✅ Harbor-style live input (Icecast, RTP, SRT)
- ✅ Priority system integration for live sessions
- ✅ Webstream relay with automatic health checks
- ✅ Failover chain progression (primary → backup with auto-recovery)
- ✅ 13 new REST API endpoints (6 live, 7 webstream)
- ✅ Scheduler integration for webstream entries

**Phase 4B Complete (100%)** - Media Engine Separation
- ✅ Separate media engine binary with gRPC interface
- ✅ DSP graph builder (12 node types: loudness, AGC, compressor, limiter, etc.)
- ✅ Pipeline manager with crossfade and cue point support
- ✅ Process supervision with automatic restart
- ✅ Real-time telemetry streaming
- ✅ 13 comprehensive integration tests (all passing)

See `docs/ARCHITECTURE_ROADMAP.md` for implementation timeline and `docs/ARCHITECTURE_NOTES.md` for architecture details.

## Naming

- Canonical name: Grimnir Radio
- Go module: `github.com/friendsincode/grimnir_radio`
- Binaries: `cmd/grimnirradio` (control plane), `cmd/mediaengine` (media engine)
- Env vars: prefer `GRIMNIR_*` (falls back to `RLM_*` for compatibility)

## Docs

- Sales spec: `docs/specs/SALES_SPEC.md`
- Engineering spec: `docs/specs/ENGINEERING_SPEC.md`
- Programmer's spec: `docs/specs/PROGRAMMERS_SPEC.md`
- Archived specs: `docs/olderspecs/`
- First-time setup (Ubuntu/Debian + VS Code): `docs/SETUP_VSCODE.md`

## Changelog

- See `docs/CHANGELOG.md` for version history.

## Quick Start

### Build

```bash
# Build control plane
go build -o grimnirradio ./cmd/grimnirradio

# Build media engine
go build -o mediaengine ./cmd/mediaengine

# Or use Makefile
make build
```

### Run

```bash
# Start media engine (must start first)
./mediaengine

# In another terminal, start control plane
./grimnirradio
```

### Test

```bash
# Unit tests
make test

# Integration tests
go test -v -tags=integration ./...

# Verify all code
make verify
```

## Development

- **Verify code**: `make verify` (tidy, fmt, vet, optional lint, test)
- **Build binaries**: `make build` (outputs `./grimnirradio` and `./mediaengine`)
- **Generate protobuf**: `make proto` (requires protoc and protoc-gen-go)
- **Integration tests**: `go test -v -tags=integration ./...`

## Deployment

See `deploy/systemd/README.md` for production deployment with systemd.

Key files:
- `deploy/systemd/grimnirradio.service` - Control plane service
- `deploy/systemd/mediaengine.service` - Media engine service

## Features

### Implemented ✅
- HTTP REST API with JWT authentication (26 endpoints total)
- Smart Block engine (rule-based playlist generation)
- Clock hour templates with slot compilation
- Scheduler service (30-second tick, materializes smart blocks)
- 5-tier priority system (Emergency → Live Override → Live Scheduled → Automation → Fallback)
- Per-station executor with state machine
- Event bus (Redis/NATS/in-memory pub/sub)
- WebSocket event streaming
- GStreamer-based media engine with gRPC control
- DSP processing graph (loudness, compression, limiting, EQ, etc.)
- Real-time audio telemetry
- Process supervision and watchdog
- Multi-database support (PostgreSQL, MySQL, SQLite)
- **Live DJ input** with token-based authorization (Icecast, RTP, SRT)
- **Webstream relay** with automatic health checks and failover chains
- Scheduler integration for webstream playback

### In Progress 🚧
- Horizontal scaling / multi-instance support (Phase 5)
- Full observability (Prometheus metrics, OpenTelemetry tracing)

### Planned 📋
- Migration tools (AzuraCast, LibreTime import)
- Emergency Alert System (EAS) integration
- WebDJ interface
- Advanced scheduling features (conflict detection, templates)

## Shout-Outs

Special shout-out to Sound Minds, Hal, Vince, MooseGirl, doc mike, Grammy Mary, Flash Somebody, Cirickle, and everyone else trying to keep RLM alive.

Grimnir, may your dream live on in this project.
