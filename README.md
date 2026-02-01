# Grimnir Radio

**Version:** 1.3.0

Grimnir Radio is a modern, production-ready broadcast automation system built in Go. It features a multi-process architecture with separated control plane and media engine, live DJ input, HTTP stream relay with automatic failover, horizontal scaling, and comprehensive observability.

## Architecture

Grimnir Radio uses a **two-binary architecture** for process isolation:

- **Control Plane (`grimnirradio`)**: HTTP API, database, scheduling, priority management, authentication
- **Media Engine (`mediaengine`)**: GStreamer pipelines, DSP processing, audio telemetry via gRPC

Communication between components uses gRPC for low-latency, reliable control of audio operations.

## Installation

Grimnir Radio can be installed in three ways:

### 1. Nix (Recommended)

```bash
# Basic: Just the binaries (you manage database/Icecast)
nix run github:friendsincode/grimnir_radio

# Full: Turn-key installation on NixOS (auto-configures everything)
# Add to your NixOS configuration.nix:
services.grimnir-radio.enable = true;

# Dev: Complete development environment
nix develop
```

See [`docs/NIX_INSTALLATION.md`](docs/NIX_INSTALLATION.md) for complete Nix guide (Basic, Full, Dev flavors).

### 2. Docker Compose (Turn-Key)

```bash
# Interactive deployment with smart port detection
./scripts/docker-quick-start.sh
```

**Features:**
- **Intelligent port detection**: Automatically finds available ports if defaults conflict
- **Three deployment modes**: Quick Start, Custom, Production
- **External storage support**: NAS/SAN mounts, custom paths
- **External database support**: RDS, Cloud SQL, ElastiCache
- **Multi-instance deployment**: 2-10 API instances with leader election
- **Configuration persistence**: Saves settings for reuse
- **Production-ready**: Validates paths, generates secure passwords

**Quick Start Guide:** [`docs/DOCKER_QUICK_START_GUIDE.md`](docs/DOCKER_QUICK_START_GUIDE.md)
**Full Documentation:** [`docs/DOCKER_DEPLOYMENT.md`](docs/DOCKER_DEPLOYMENT.md)

### 3. Kubernetes

```bash
kubectl apply -k kubernetes/
```

See [`kubernetes/README.md`](kubernetes/README.md) for complete K8s deployment guide.

### 4. Bare Metal

See [`docs/PRODUCTION_DEPLOYMENT.md`](docs/PRODUCTION_DEPLOYMENT.md) for manual installation.

## Production Status

**All planned phases complete (1.0 release):**

- ✅ **Phase 0**: Foundation Fixes
- ✅ **Phase 4A**: Executor & Priority System (5-tier priority ladder)
- ✅ **Phase 4B**: Media Engine Separation (gRPC, DSP, telemetry)
- ✅ **Phase 4C**: Live Input & Webstream Relay (harbor-style, failover)
- ✅ **Phase 5**: Observability & Multi-Instance (Prometheus, consistent hashing)
- ✅ **Phase 6**: Production Readiness (Docker, K8s, load testing, migrations)
- ✅ **Phase 7**: Nix Integration (reproducible builds, three deployment flavors)

See [`docs/ARCHITECTURE_ROADMAP.md`](docs/ARCHITECTURE_ROADMAP.md) for detailed implementation history.

## Naming

- Canonical name: Grimnir Radio
- Go module: `github.com/friendsincode/grimnir_radio`
- Binaries: `cmd/grimnirradio` (control plane), `cmd/mediaengine` (media engine)
- Env vars: prefer `GRIMNIR_*` (falls back to `RLM_*` for compatibility)

## API Documentation

Grimnir Radio provides a comprehensive REST API for integration with external applications.

- **OpenAPI/Swagger Spec**: [`api/openapi.yaml`](api/openapi.yaml)
- **API Guide**: [`docs/api/README.md`](docs/api/README.md)
- **Python Client**: [`docs/api/examples/python/grimnir_client.py`](docs/api/examples/python/grimnir_client.py)

### Quick Example (Python)

```python
from grimnir_client import GrimnirClient

# Initialize with your API key (get it from your profile page)
client = GrimnirClient("https://your-instance.com", api_key="gr_your-api-key")

# Get stations
stations = client.get_stations()

# Get now playing
np = client.get_now_playing(station_id)
print(f"Now Playing: {np['title']} by {np['artist']}")

# Upload media
media = client.upload_media(station_id, "/path/to/song.mp3")
```

### Quick Example (curl)

```bash
# Use your API key (get it from your profile page in the web dashboard)
API_KEY="gr_your-api-key-here"

# Get stations
curl https://your-instance.com/api/v1/stations \
  -H "X-API-Key: $API_KEY"

# Get now playing (no auth required)
curl https://your-instance.com/api/v1/analytics/now-playing
```

## Docs

- **API Documentation**: `docs/api/README.md`
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

### Recently Completed ✅
- Horizontal scaling with consistent hashing and leader election
- Full observability (Prometheus metrics, OpenTelemetry tracing, alerts)
- Turn-key Docker Compose deployment with Icecast2
- Migration tools (AzuraCast, LibreTime import) with media file transfer

### Planned 📋
- Emergency Alert System (EAS) integration
- WebDJ interface
- Advanced scheduling features (conflict detection, templates)

## Shout-Outs

Special shout-out to Sound Minds, Hal, Vince, MooseGirl, doc mike, Grammy Mary, Flash Somebody, Cirickle, and everyone else trying to keep RLM alive.

Grimnir, may your dream live on in this project.

## The Grimnir Principle

Grimnir did his work for the community, without gatekeeping and without asking for permission. This project exists because the community chose to respond in kind. Grimnir Radio is built to be shared, improved, and carried forward in the open, the same way Grimnir showed up for others. If you make it better and use it for the public, those improvements belong to the public too. That is not a condition. It is the point. This project uses the GNU Affero General Public License to make sure Grimnir's work, and the work done in his name, always stays with the community.

## License

Grimnir Radio is licensed under the **GNU Affero General Public License v3.0 or later (AGPL-3.0-or-later)**.

This means:
- You can use, modify, and distribute this software freely
- **If you run a modified version on a server, you must make the modified source code available to users**
- All derivative works must also be licensed under AGPL-3.0-or-later
- There is NO WARRANTY

See the [LICENSE](LICENSE) file for the full license text.

**AGPL Compliance:** If you modify Grimnir Radio and offer it as a network service (e.g., running a hosted instance), you must provide users with access to the complete corresponding source code, including all modifications.
