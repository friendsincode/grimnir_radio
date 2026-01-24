# Getting Started with Grimnir Radio

Get Grimnir Radio running in minutes with Docker, Nix, or from source.

## Quick Start with Docker

The fastest way to try Grimnir Radio:

```bash
# Clone the repository
git clone https://github.com/friendsincode/grimnir_radio.git
cd grimnir_radio

# Run the interactive setup script
./scripts/docker-quick-start.sh
```

The script will guide you through:
1. **Deployment mode** - Quick Start, Custom, or Production
2. **Port configuration** - Automatic port conflict detection
3. **Storage setup** - Media storage path
4. **Database configuration** - Built-in or external PostgreSQL
5. **Service startup** - Automatic container orchestration

### What Gets Installed

- **Grimnir Radio** control plane (HTTP API on port 8080)
- **PostgreSQL** database for metadata
- **Redis** for event bus and caching
- **Icecast2** streaming server (port 8000)

Access the API at `http://localhost:8080`

## Quick Start with Nix

For NixOS users or those wanting reproducible builds:

```bash
# Run directly (no installation)
nix run github:friendsincode/grimnir_radio

# Or install to your profile
nix profile install github:friendsincode/grimnir_radio
grimnirradio serve
```

See [Nix Installation](Nix-Installation) for the full NixOS module setup.

## Quick Start from Source

For development or custom builds:

```bash
# Prerequisites: Go 1.24+, GStreamer 1.0, PostgreSQL

# Clone and build
git clone https://github.com/friendsincode/grimnir_radio.git
cd grimnir_radio
go build -o grimnirradio ./cmd/grimnirradio

# Set up database
createdb grimnir_radio

# Configure environment
cp .env.example .env
# Edit .env with your settings

# Run
./grimnirradio serve
```

## First Station Setup

Once Grimnir Radio is running, create your first station:

### 1. Create an Admin User

```bash
# Using the CLI
./grimnirradio user create \
  --email admin@example.com \
  --password yourpassword \
  --role admin
```

### 2. Get an API Token

```bash
curl -X POST http://localhost:8080/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{
    "email": "admin@example.com",
    "password": "yourpassword"
  }'
```

Save the returned JWT token for subsequent requests.

### 3. Create a Station

```bash
curl -X POST http://localhost:8080/api/v1/stations \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "My Radio Station",
    "description": "Community radio broadcasting",
    "timezone": "America/New_York"
  }'
```

### 4. Create a Mount Point

```bash
curl -X POST http://localhost:8080/api/v1/mounts \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "station_id": "STATION_ID",
    "name": "main",
    "url": "http://localhost:8000/live",
    "format": "mp3",
    "bitrate": 128
  }'
```

### 5. Upload Media

```bash
curl -X POST http://localhost:8080/api/v1/media \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -F "station_id=STATION_ID" \
  -F "file=@/path/to/song.mp3"
```

### 6. Create a Smart Block

```bash
curl -X POST http://localhost:8080/api/v1/smartblocks \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "station_id": "STATION_ID",
    "name": "Rock Music",
    "description": "All rock tracks",
    "rules": {
      "genre": "rock",
      "limit": 50
    }
  }'
```

### 7. Create a Clock

```bash
curl -X POST http://localhost:8080/api/v1/clocks \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "station_id": "STATION_ID",
    "name": "Hourly Rotation",
    "duration": 3600,
    "slots": [
      {
        "minute": 0,
        "duration": 180,
        "type": "smartblock",
        "smartblock_id": "SMARTBLOCK_ID"
      }
    ]
  }'
```

## Next Steps

- **[Configure](Configuration)** - Customize settings for your environment
- **[Architecture](Architecture)** - Understand the system components
- **[API Reference](API-Reference)** - Explore the full API
- **[Production Deployment](Production-Deployment)** - Deploy to production
- **[Migration Guide](Migration-Guide)** - Import from AzuraCast/LibreTime

## Common First-Run Issues

### Port Already in Use

If ports 8080 (API) or 8000 (Icecast) are in use:

```bash
# Docker: Edit docker-compose.yml ports
services:
  grimnirradio:
    ports:
      - "8081:8080"  # Map to different host port

# Or use the quick-start script which auto-detects conflicts
./scripts/docker-quick-start.sh
```

### Database Connection Failed

Ensure PostgreSQL is running and credentials are correct:

```bash
# Check connection
psql -h localhost -U grimnir -d grimnir_radio

# Docker: Check database service
docker-compose ps postgres
docker-compose logs postgres
```

### GStreamer Not Found

For source installations, ensure GStreamer is installed:

```bash
# Ubuntu/Debian
sudo apt-get install gstreamer1.0-tools gstreamer1.0-plugins-base \
  gstreamer1.0-plugins-good gstreamer1.0-plugins-bad \
  gstreamer1.0-plugins-ugly

# macOS
brew install gstreamer gst-plugins-base gst-plugins-good \
  gst-plugins-bad gst-plugins-ugly

# Fedora
sudo dnf install gstreamer1 gstreamer1-plugins-base \
  gstreamer1-plugins-good gstreamer1-plugins-bad-free \
  gstreamer1-plugins-ugly-free
```

## Getting Help

- **Documentation**: Browse this wiki for detailed guides
- **Issues**: [Report bugs](https://github.com/friendsincode/grimnir_radio/issues)
- **Logs**: Check `docker-compose logs` or application logs for errors
- **Troubleshooting**: See [Troubleshooting Guide](Troubleshooting)
