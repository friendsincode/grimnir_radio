# Installation Guide

Grimnir Radio offers multiple installation methods to suit different environments and use cases.

## Installation Methods Overview

| Method | Best For | Pros | Cons |
|--------|----------|------|------|
| **Docker Compose** | Production, quick start | Easy setup, isolated, portable | Requires Docker |
| **Nix (Full Module)** | NixOS systems | Reproducible, declarative | NixOS only |
| **Nix (Basic)** | Any Linux/macOS | Reproducible builds | Manual config |
| **From Source** | Development, custom builds | Full control, latest code | Complex setup |

---

## Docker Compose Installation

### Prerequisites

- Docker 20.10+ and Docker Compose 2.0+
- 2GB RAM minimum
- 10GB disk space for media

### Interactive Quick Start

The easiest method with automatic configuration:

```bash
git clone https://github.com/friendsincode/grimnir_radio.git
cd grimnir_radio
./scripts/docker-quick-start.sh
```

**Features:**
- Automatic port conflict detection
- Three deployment modes (Quick, Custom, Production)
- External storage configuration
- Multi-instance setup support

### Manual Docker Compose

For custom deployments:

```bash
# Clone repository
git clone https://github.com/friendsincode/grimnir_radio.git
cd grimnir_radio

# Configure environment
cp .env.example .env
# Edit .env with your settings

# Start services
docker-compose up -d

# Check status
docker-compose ps

# View logs
docker-compose logs -f grimnirradio
```

### Docker Compose Configuration

**Basic Stack** (`docker-compose.yml`):
- Grimnir Radio (control plane)
- PostgreSQL (metadata database)
- Redis (event bus, caching)
- Icecast2 (streaming server)

**Environment Variables** (`.env`):
```bash
# HTTP API
GRIMNIR_HTTP_PORT=8080

# Database
GRIMNIR_DB_BACKEND=postgres
GRIMNIR_DB_DSN=postgres://grimnir:password@postgres:5432/grimnir_radio?sslmode=disable

# Redis
GRIMNIR_REDIS_ADDR=redis:6379

# Storage
GRIMNIR_MEDIA_ROOT=/media
# Or S3:
GRIMNIR_S3_BUCKET=my-media
GRIMNIR_S3_REGION=us-east-1

# Security
GRIMNIR_JWT_SIGNING_KEY=your-secret-key-here
```

See [Configuration Guide](Configuration) for all options.

---

## Nix Installation

### Nix Package Manager

Required for all Nix installation methods:

```bash
# Install Nix (single-user)
sh <(curl -L https://nixos.org/nix/install) --no-daemon

# Or multi-user (recommended)
sh <(curl -L https://nixos.org/nix/install) --daemon

# Enable flakes (required)
mkdir -p ~/.config/nix
echo "experimental-features = nix-command flakes" >> ~/.config/nix/nix.conf
```

### Method 1: NixOS Module (Recommended for NixOS)

Full turn-key installation with automatic service management:

**`/etc/nixos/configuration.nix`:**
```nix
{
  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    grimnir-radio.url = "github:friendsincode/grimnir_radio";
  };

  outputs = { self, nixpkgs, grimnir-radio }: {
    nixosConfigurations.myhost = nixpkgs.lib.nixosSystem {
      modules = [
        grimnir-radio.nixosModules.default
        {
          services.grimnir-radio = {
            enable = true;
            httpPort = 8080;
            mediaPath = "/var/lib/grimnir-radio/media";

            # Optional: Use PostgreSQL
            database = {
              enable = true;
              createDatabase = true;
            };

            # Optional: Use Redis
            redis = {
              enable = true;
            };

            # Optional: Use Icecast
            icecast = {
              enable = true;
              password = "hackme";
            };
          };
        }
      ];
    };
  };
}
```

Apply configuration:
```bash
sudo nixos-rebuild switch
```

**What This Provides:**
- Automatic systemd services for both binaries
- Auto-configured PostgreSQL with database/user creation
- Auto-configured Redis for event bus
- Auto-configured Icecast2 streaming server
- Automatic firewall rules
- Security hardening (PrivateTmp, ProtectSystem, etc.)
- Automatic service dependencies and ordering

### Method 2: Nix Profile (Any Linux/macOS)

Install binaries to your user profile:

```bash
# Install
nix profile install github:friendsincode/grimnir_radio

# Run
grimnirradio serve
```

**Manual configuration required:**
- PostgreSQL/MySQL database setup
- Redis installation (optional, for multi-instance)
- Icecast2 installation
- Environment variables

### Method 3: Nix Run (Try Without Installing)

```bash
# Run directly without installing
nix run github:friendsincode/grimnir_radio

# Run media engine
nix run github:friendsincode/grimnir_radio#mediaengine
```

### Nix Development Environment

For development:

```bash
# Enter development shell
nix develop github:friendsincode/grimnir_radio

# Or clone and develop
git clone https://github.com/friendsincode/grimnir_radio.git
cd grimnir_radio
nix develop

# Build
make build

# Run tests
make test
```

See [Development Guide](Development) for more details.

---

## From Source Installation

### Prerequisites

**Required:**
- Go 1.24 or later
- GStreamer 1.0 with plugins (base, good, bad, ugly)
- PostgreSQL 12+ (or MySQL 8+, SQLite 3.35+)
- Git

**Optional:**
- Redis 6+ (for multi-instance)
- NATS 2.9+ (alternative to Redis)
- Icecast2 (for streaming)

### System Dependencies

**Ubuntu/Debian:**
```bash
sudo apt-get update
sudo apt-get install -y \
  golang-1.24 \
  gstreamer1.0-tools \
  gstreamer1.0-plugins-base \
  gstreamer1.0-plugins-good \
  gstreamer1.0-plugins-bad \
  gstreamer1.0-plugins-ugly \
  gstreamer1.0-libav \
  libgstreamer1.0-dev \
  libgstreamer-plugins-base1.0-dev \
  postgresql-client \
  redis-server
```

**Fedora/RHEL:**
```bash
sudo dnf install -y \
  golang \
  gstreamer1 \
  gstreamer1-plugins-base \
  gstreamer1-plugins-good \
  gstreamer1-plugins-bad-free \
  gstreamer1-plugins-ugly-free \
  gstreamer1-libav \
  gstreamer1-devel \
  gstreamer1-plugins-base-devel \
  postgresql \
  redis
```

**macOS:**
```bash
brew install go gstreamer gst-plugins-base gst-plugins-good \
  gst-plugins-bad gst-plugins-ugly postgresql redis
```

### Build from Source

```bash
# Clone repository
git clone https://github.com/friendsincode/grimnir_radio.git
cd grimnir_radio

# Build control plane
go build -o grimnirradio ./cmd/grimnirradio

# Build media engine (optional, requires GStreamer)
go build -o mediaengine ./cmd/mediaengine

# Install (optional)
sudo cp grimnirradio /usr/local/bin/
sudo cp mediaengine /usr/local/bin/
```

### Database Setup

**PostgreSQL:**
```bash
# Create user and database
sudo -u postgres psql -c "CREATE USER grimnir WITH PASSWORD 'password';"
sudo -u postgres psql -c "CREATE DATABASE grimnir_radio OWNER grimnir;"

# Test connection
psql -h localhost -U grimnir -d grimnir_radio
```

**MySQL:**
```bash
# Create user and database
mysql -u root -p <<EOF
CREATE DATABASE grimnir_radio CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
CREATE USER 'grimnir'@'localhost' IDENTIFIED BY 'password';
GRANT ALL PRIVILEGES ON grimnir_radio.* TO 'grimnir'@'localhost';
FLUSH PRIVILEGES;
EOF
```

**SQLite:**
```bash
# SQLite creates database automatically, just configure path
mkdir -p /var/lib/grimnir-radio
```

### Configuration

```bash
# Copy environment template
cp .env.example .env

# Edit configuration
nano .env
```

**Minimum required settings:**
```bash
GRIMNIR_DB_BACKEND=postgres
GRIMNIR_DB_DSN=postgres://grimnir:password@localhost:5432/grimnir_radio?sslmode=disable
GRIMNIR_JWT_SIGNING_KEY=$(openssl rand -base64 32)
GRIMNIR_MEDIA_ROOT=/var/lib/grimnir-radio/media
```

### Run

```bash
# Run control plane
./grimnirradio serve

# In another terminal, run media engine (if built separately)
./mediaengine
```

### Systemd Service (Linux)

Create `/etc/systemd/system/grimnir-radio.service`:
```ini
[Unit]
Description=Grimnir Radio Control Plane
After=network.target postgresql.service redis.service

[Service]
Type=simple
User=grimnir
WorkingDirectory=/opt/grimnir-radio
EnvironmentFile=/etc/grimnir-radio/config.env
ExecStart=/usr/local/bin/grimnirradio serve
Restart=on-failure
RestartSec=5s

[Install]
WantedBy=multi-user.target
```

Enable and start:
```bash
sudo systemctl daemon-reload
sudo systemctl enable grimnir-radio
sudo systemctl start grimnir-radio
sudo systemctl status grimnir-radio
```

---

## Post-Installation

### Verify Installation

```bash
# Check API health
curl http://localhost:8080/health

# Expected response:
# {"status":"ok","version":"1.0.0"}

# Check logs
# Docker:
docker-compose logs grimnirradio

# Systemd:
journalctl -u grimnir-radio -f

# Direct:
# Check console output
```

### Create Admin User

```bash
./grimnirradio user create \
  --email admin@example.com \
  --password yourpassword \
  --role admin
```

### Next Steps

- [Getting Started](Getting-Started) - Create your first station
- [Configuration](Configuration) - Customize settings
- [Production Deployment](Production-Deployment) - Production checklist

## Upgrading

See [Upgrading Guide](Upgrading) for version upgrade instructions.

## Troubleshooting

See [Troubleshooting Guide](Troubleshooting) for common issues and solutions.
