# Grimnir Radio - Nix Installation Guide

**Version:** 1.0.0
**Last Updated:** 2026-01-22

This guide covers all three ways to deploy and develop Grimnir Radio using Nix.

---

## Quick Start

### Prerequisites

Install Nix with flakes enabled:

```bash
# Install Nix (if not already installed)
curl --proto '=https' --tlsv1.2 -sSf -L https://install.determinate.systems/nix | sh -s -- install

# Or use the official installer
sh <(curl -L https://nixos.org/nix/install) --daemon

# Enable flakes (if not already enabled)
mkdir -p ~/.config/nix
echo "experimental-features = nix-command flakes" >> ~/.config/nix/nix.conf
```

---

## Three Deployment Flavors

### 1. Basic: Just the Binaries (For Nerds)

**Use case:** You know what you're doing. You have PostgreSQL, Redis, and Icecast2 already running. You just want the Grimnir Radio binaries.

#### Install and Run

```bash
# Run directly without installing
nix run github:friendsincode/grimnir_radio

# Run media engine
nix run github:friendsincode/grimnir_radio#mediaengine

# Install to profile
nix profile install github:friendsincode/grimnir_radio

# Now you can run from anywhere
grimnirradio serve
mediaengine --grpc-port 9091
```

#### Build from local source

```bash
git clone https://github.com/friendsincode/grimnir_radio
cd grimnir_radio

# Build both binaries
nix build

# Result in ./result/bin/
./result/bin/grimnirradio --version
./result/bin/mediaengine --version

# Install to profile
nix profile install .
```

#### What You Need to Provide

- **PostgreSQL**: Database for metadata and schedules
- **Redis**: Event bus and leader election
- **Icecast2**: Streaming server (optional, for live output)
- **Configuration**: Environment variables or config file

**Example configuration:**

```bash
export DATABASE_URL="postgres://user:pass@localhost:5432/grimnir"
export REDIS_URL="redis://localhost:6379/0"
export MEDIA_ENGINE_GRPC_ADDR="localhost:9091"
export JWT_SECRET="your-secret-key-here"
export MEDIA_STORAGE_PATH="/var/lib/grimnir/media"

# Start media engine
mediaengine --grpc-port 9091 &

# Start control plane
grimnirradio serve
```

---

### 2. Full: Turn-Key Installation (White Glove Treatment)

**Use case:** You want everything installed and configured automatically. PostgreSQL, Redis, Icecast2, Grimnir Radio - all managed by NixOS.

#### Requirements

- NixOS system (or VM running NixOS)
- Root access for system configuration

#### Add to NixOS Configuration

Edit `/etc/nixos/configuration.nix`:

```nix
{ config, pkgs, ... }:

{
  imports = [
    # ... your other imports
  ];

  # Add Grimnir Radio flake input (in flake.nix if using flakes)
  # Or use fetchGit for non-flake systems
  inputs.grimnir-radio.url = "github:friendsincode/grimnir_radio";

  # Enable Grimnir Radio with full stack
  services.grimnir-radio = {
    enable = true;

    # HTTP configuration
    httpBind = "0.0.0.0";
    httpPort = 8080;

    # Database (auto-configured)
    enableDatabase = true;
    databaseUrl = "postgres://grimnir:grimnir@localhost:5432/grimnir?sslmode=disable";

    # Redis (auto-configured)
    enableRedis = true;
    redisUrl = "redis://localhost:6379/0";

    # Icecast (auto-configured)
    enableIcecast = true;
    icecastPassword = "CHANGE_ME_IN_PRODUCTION";

    # JWT secret (CHANGE THIS!)
    jwtSecret = "your-super-secret-jwt-key-change-this-in-production";

    # Media storage
    mediaStoragePath = "/var/lib/grimnir-radio/media";

    # Tracing (optional)
    tracingEnabled = false;
    tracingSampleRate = 0.1;
    otlpEndpoint = "localhost:4317";

    # User/group
    user = "grimnir";
    group = "grimnir";
  };

  # Optional: Nginx reverse proxy with TLS
  services.nginx = {
    enable = true;
    recommendedProxySettings = true;
    recommendedTlsSettings = true;
    recommendedOptimisation = true;

    virtualHosts."radio.example.com" = {
      enableACME = true;
      forceSSL = true;
      locations."/" = {
        proxyPass = "http://localhost:8080";
        proxyWebsockets = true; # For WebSocket events
      };
    };
  };

  # Open firewall ports
  networking.firewall.allowedTCPPorts = [ 80 443 8000 8080 ];
}
```

#### Rebuild System

```bash
# Rebuild NixOS configuration
sudo nixos-rebuild switch

# Check service status
systemctl status grimnir-radio
systemctl status grimnir-mediaengine
systemctl status postgresql
systemctl status redis-grimnir
systemctl status icecast
```

#### What Gets Installed

1. **PostgreSQL** - Database with `grimnir` database and user created
2. **Redis** - Event bus and caching
3. **Icecast2** - Streaming server on port 8000
4. **Grimnir Radio Media Engine** - GStreamer-based audio engine
5. **Grimnir Radio Control Plane** - API and scheduler
6. **systemd services** - Automatic startup and restart
7. **User/group** - Dedicated `grimnir` system user
8. **Firewall rules** - Ports opened automatically

#### Access Points

- **API**: http://localhost:8080
- **Icecast**: http://localhost:8000
- **Logs**: `journalctl -u grimnir-radio -f`

#### Initial Setup

```bash
# Create admin user (via API or CLI tool if available)
curl -X POST http://localhost:8080/api/v1/users \
  -H "Content-Type: application/json" \
  -d '{
    "username": "admin",
    "email": "admin@example.com",
    "password": "changeme",
    "role": "admin"
  }'

# Login and get JWT token
curl -X POST http://localhost:8080/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{
    "username": "admin",
    "password": "changeme"
  }'
```

---

### 3. Dev: Development Environment (For Hacking)

**Use case:** You want to contribute to Grimnir Radio or customize it. Full development environment with all tools.

#### Enter Development Shell

```bash
git clone https://github.com/friendsincode/grimnir_radio
cd grimnir_radio

# Enter development shell
nix develop

# You'll see a welcome message:
# 🎙️  Grimnir Radio Development Environment
#
# Available commands:
#   make build          - Build both binaries
#   make test           - Run tests
#   ...
```

#### What's Included

The development shell provides:

**Go Development:**
- Go 1.22+
- gopls (LSP server)
- gotools (gofmt, goimports, etc.)
- go-tools (staticcheck, etc.)

**Protocol Buffers:**
- protoc compiler
- protoc-gen-go
- protoc-gen-go-grpc

**GStreamer (Media Engine):**
- GStreamer 1.0
- All plugin packages (base, good, bad, ugly, libav)
- Development tools (gst-inspect, gst-launch)
- pkg-config for building

**Infrastructure:**
- PostgreSQL (for local DB)
- Redis (for event bus)
- Icecast (for streaming)
- Docker Compose (for containerized stack)

**Kubernetes Tools:**
- kubectl
- k9s (terminal UI)

**Build & Test Tools:**
- GNU Make
- Git
- jq, yq (YAML/JSON processing)
- curl
- k6 (load testing)

#### Development Workflow

```bash
# Enter dev shell
nix develop

# Generate protobuf code
make proto

# Build binaries
make build

# Run tests
make test

# Run control plane (requires database)
make run-control

# Run media engine
make run-media

# Run full stack with Docker Compose
make dev-stack

# Stop stack
make dev-stop
```

#### Database Setup

```bash
# Option 1: Use Docker (included in dev shell)
docker run -d \
  --name grimnir-postgres \
  -e POSTGRES_USER=grimnir \
  -e POSTGRES_PASSWORD=grimnir \
  -e POSTGRES_DB=grimnir \
  -p 5432:5432 \
  postgres:15-alpine

# Option 2: Use local PostgreSQL
createuser grimnir
createdb -O grimnir grimnir

# Set environment variable
export DATABASE_URL="postgres://grimnir:grimnir@localhost:5432/grimnir?sslmode=disable"
```

#### Redis Setup

```bash
# Option 1: Use Docker
docker run -d --name grimnir-redis -p 6379:6379 redis:7-alpine

# Option 2: Use local Redis (included in dev shell)
redis-server --daemonize yes

# Set environment variable
export REDIS_URL="redis://localhost:6379/0"
```

#### IDE Integration

**VSCode:**

The dev shell works seamlessly with VSCode. Just open the project:

```bash
nix develop --command code .
```

VSCode will detect the Go environment and use the tools from the Nix shell.

**GoLand/IntelliJ:**

Set the Go SDK path to the one from the Nix shell:

```bash
nix develop --command which go
# Use this path in GoLand settings
```

#### Direnv Integration (Automatic Shell Activation)

```bash
# Install direnv
nix profile install nixpkgs#direnv

# Create .envrc in project root
echo "use flake" > .envrc

# Allow direnv
direnv allow

# Now the dev shell activates automatically when you cd into the project!
cd ~/projects/grimnir_radio  # Dev shell auto-activates
```

---

## Advanced Usage

### Custom Package Builds

```nix
# flake.nix overlay
{
  outputs = { self, nixpkgs, ... }:
    let
      # Custom Grimnir Radio with specific Go version
      customGrimnir = pkgs.grimnir-radio.override {
        go = pkgs.go_1_21;
      };
    in
    {
      packages.x86_64-linux.custom = customGrimnir;
    };
}
```

### Multi-Instance Setup

Deploy multiple instances with different configurations:

```nix
{ config, ... }:
{
  # Instance 1: Main station
  services.grimnir-radio-main = {
    enable = true;
    httpPort = 8080;
    mediaEngineGrpcAddr = "localhost:9091";
    # ...
  };

  # Instance 2: Backup station
  services.grimnir-radio-backup = {
    enable = true;
    httpPort = 8081;
    mediaEngineGrpcAddr = "localhost:9092";
    # ...
  };
}
```

### Cross-Compilation

Build for different architectures:

```bash
# Build for ARM64 (Raspberry Pi, etc.)
nix build --system aarch64-linux

# Build for macOS (control plane only, media engine is Linux-only)
nix build --system x86_64-darwin .#grimnir-radio
```

---

## Troubleshooting

### Flakes Not Working

```bash
# Enable flakes permanently
mkdir -p ~/.config/nix
echo "experimental-features = nix-command flakes" >> ~/.config/nix/nix.conf

# Restart nix-daemon (on NixOS)
sudo systemctl restart nix-daemon
```

### vendorHash Mismatch

When building from source, you might see:

```
error: hash mismatch in fixed-output derivation
```

**Fix:**

```bash
# Get the correct hash
nix build --impure --expr '(builtins.getFlake "git+file://$(pwd)").packages.x86_64-linux.grimnir-radio.goModules'

# Copy the hash from the error and update nix/package.nix
vendorHash = "sha256-...";
```

### GStreamer Plugins Not Found

If media engine can't find GStreamer plugins:

```bash
# Check plugin path
echo $GST_PLUGIN_SYSTEM_PATH_1_0

# List available plugins
gst-inspect-1.0

# Verify wrapper is working
ldd $(which mediaengine)
```

### Service Won't Start

```bash
# Check logs
journalctl -u grimnir-radio -n 100
journalctl -u grimnir-mediaengine -n 100

# Check database connection
sudo -u grimnir psql -h localhost -U grimnir -d grimnir -c "SELECT version();"

# Check Redis connection
redis-cli ping

# Check if ports are available
sudo netstat -tlnp | grep -E ':(8080|9091|6379|5432|8000)'
```

### Permission Denied

```bash
# Check media storage ownership
sudo chown -R grimnir:grimnir /var/lib/grimnir-radio

# Check file permissions
ls -la /var/lib/grimnir-radio
```

---

## Migration from Docker/Bare Metal

### From Docker Compose

```bash
# Export current data
docker exec grimnir-postgres pg_dump -U grimnir grimnir > backup.sql

# Import to NixOS PostgreSQL
sudo -u postgres psql grimnir < backup.sql

# Copy media files
rsync -av /var/lib/docker/volumes/grimnir_media/ /var/lib/grimnir-radio/media/
```

### From Bare Metal

```bash
# Export PostgreSQL
pg_dump -U grimnir grimnir > backup.sql

# Import to NixOS
sudo -u postgres psql grimnir < backup.sql

# Copy media
rsync -av /old/path/to/media/ /var/lib/grimnir-radio/media/
```

---

## Performance Tuning

### PostgreSQL

```nix
services.postgresql = {
  settings = {
    shared_buffers = "256MB";
    effective_cache_size = "1GB";
    work_mem = "16MB";
    maintenance_work_mem = "128MB";
  };
};
```

### Redis

```nix
services.redis.servers.grimnir = {
  maxmemory = "256mb";
  maxmemoryPolicy = "allkeys-lru";
};
```

### Grimnir Radio

```nix
systemd.services.grimnir-radio.serviceConfig = {
  MemoryMax = "2G";
  CPUQuota = "200%";
};
```

---

## Uninstalling

### NixOS Module

```bash
# Remove from configuration.nix
# services.grimnir-radio.enable = false;

# Rebuild
sudo nixos-rebuild switch

# Remove data (optional)
sudo rm -rf /var/lib/grimnir-radio
```

### Profile Installation

```bash
# Remove from profile
nix profile remove grimnir-radio

# Garbage collect
nix-collect-garbage -d
```

---

## Support

- **GitHub Issues**: https://github.com/friendsincode/grimnir_radio/issues
- **Documentation**: https://github.com/friendsincode/grimnir_radio/tree/main/docs
- **Nix Discourse**: https://discourse.nixos.org (tag: grimnir-radio)

---

## Contributing

See [CONTRIBUTING.md](../CONTRIBUTING.md) for development workflow and guidelines.

All three flavors (Basic, Full, Dev) use the same source code and build process, ensuring consistency across deployment methods.
