# Systemd Service Installation Guide

This guide explains how to deploy Grimnir Radio using systemd on Linux systems.

## Prerequisites

- Linux system with systemd (Ubuntu 20.04+, Debian 11+, Fedora 34+, etc.)
- GStreamer 1.0 with required plugins installed
- PostgreSQL, MySQL, or SQLite database
- Redis or NATS (optional, for distributed event bus)

## System Dependencies

### Ubuntu/Debian
```bash
sudo apt update
sudo apt install -y \
    gstreamer1.0-tools \
    gstreamer1.0-plugins-base \
    gstreamer1.0-plugins-good \
    gstreamer1.0-plugins-bad \
    gstreamer1.0-plugins-ugly \
    gstreamer1.0-libav \
    ladspa-sdk \
    postgresql-client
```

### Fedora/RHEL
```bash
sudo dnf install -y \
    gstreamer1 \
    gstreamer1-plugins-base \
    gstreamer1-plugins-good \
    gstreamer1-plugins-bad-free \
    gstreamer1-plugins-ugly-free \
    gstreamer1-libav \
    ladspa \
    postgresql
```

## Installation Steps

### 1. Create System User

```bash
sudo useradd --system --no-create-home --shell /bin/false grimnir
```

### 2. Create Directory Structure

```bash
sudo mkdir -p /opt/grimnir/{control,mediaengine}/{bin,data}
sudo mkdir -p /opt/grimnir/media
sudo mkdir -p /etc/grimnir
sudo mkdir -p /var/log/grimnir
```

### 3. Build Binaries

```bash
# From the project root
go build -o grimnirradio ./cmd/grimnirradio
go build -o mediaengine ./cmd/mediaengine
```

### 4. Install Binaries

```bash
sudo cp grimnirradio /opt/grimnir/control/bin/
sudo cp mediaengine /opt/grimnir/mediaengine/bin/
sudo chmod +x /opt/grimnir/control/bin/grimnirradio
sudo chmod +x /opt/grimnir/mediaengine/bin/mediaengine
```

### 5. Set Permissions

```bash
sudo chown -R grimnir:grimnir /opt/grimnir
sudo chown -R grimnir:grimnir /var/log/grimnir
```

### 6. Configure Environment

Create `/etc/grimnir/mediaengine.env`:
```bash
MEDIAENGINE_GRPC_BIND=0.0.0.0
MEDIAENGINE_GRPC_PORT=9091
MEDIAENGINE_LOG_LEVEL=info
GSTREAMER_BIN=/usr/bin/gst-launch-1.0
```

Create `/etc/grimnir/grimnirradio.env`:
```bash
HTTP_BIND=0.0.0.0
HTTP_PORT=8080
DB_DRIVER=postgres
DB_DSN=host=localhost user=grimnir password=CHANGEME dbname=grimnir sslmode=disable
MEDIA_ENGINE_GRPC_ADDR=localhost:9091
MEDIA_STORAGE_PATH=/opt/grimnir/media
LOG_LEVEL=info
```

Create JWT secret:
```bash
openssl rand -hex 32 | sudo tee /etc/grimnir/jwt.secret
sudo chmod 600 /etc/grimnir/jwt.secret
sudo chown grimnir:grimnir /etc/grimnir/jwt.secret
```

### 7. Install Systemd Services

```bash
sudo cp deploy/systemd/mediaengine.service /etc/systemd/system/
sudo cp deploy/systemd/grimnirradio.service /etc/systemd/system/
sudo systemctl daemon-reload
```

### 8. Enable and Start Services

```bash
# Start media engine first
sudo systemctl enable mediaengine
sudo systemctl start mediaengine

# Then start control plane
sudo systemctl enable grimnirradio
sudo systemctl start grimnirradio
```

### 9. Verify Installation

```bash
# Check service status
sudo systemctl status mediaengine
sudo systemctl status grimnirradio

# View logs
sudo journalctl -u mediaengine -f
sudo journalctl -u grimnirradio -f

# Test HTTP API
curl http://localhost:8080/health
```

## Service Management

### Start Services
```bash
sudo systemctl start mediaengine
sudo systemctl start grimnirradio
```

### Stop Services
```bash
sudo systemctl stop grimnirradio
sudo systemctl stop mediaengine
```

### Restart Services
```bash
sudo systemctl restart mediaengine
sudo systemctl restart grimnirradio
```

### View Logs
```bash
# Real-time logs
sudo journalctl -u grimnirradio -f

# Last 100 lines
sudo journalctl -u grimnirradio -n 100

# Logs since boot
sudo journalctl -u grimnirradio -b
```

### Check Status
```bash
sudo systemctl status grimnirradio
sudo systemctl status mediaengine
```

## Upgrading

1. Stop services:
   ```bash
   sudo systemctl stop grimnirradio mediaengine
   ```

2. Backup current binaries:
   ```bash
   sudo cp /opt/grimnir/control/bin/grimnirradio /opt/grimnir/control/bin/grimnirradio.backup
   sudo cp /opt/grimnir/mediaengine/bin/mediaengine /opt/grimnir/mediaengine/bin/mediaengine.backup
   ```

3. Install new binaries:
   ```bash
   sudo cp grimnirradio /opt/grimnir/control/bin/
   sudo cp mediaengine /opt/grimnir/mediaengine/bin/
   ```

4. Restart services:
   ```bash
   sudo systemctl start mediaengine
   sudo systemctl start grimnirradio
   ```

## Troubleshooting

### Media Engine Won't Start

Check GStreamer installation:
```bash
gst-launch-1.0 --version
gst-inspect-1.0 --print-all | grep -i plugin
```

Check logs:
```bash
sudo journalctl -u mediaengine -n 50
```

### Control Plane Can't Connect to Media Engine

Verify media engine is running:
```bash
sudo systemctl status mediaengine
```

Check if gRPC port is open:
```bash
sudo netstat -tlnp | grep 9091
```

Test connection:
```bash
telnet localhost 9091
```

### Permission Denied Errors

Check file ownership:
```bash
ls -la /opt/grimnir/
ls -la /etc/grimnir/
```

Fix permissions:
```bash
sudo chown -R grimnir:grimnir /opt/grimnir
sudo chown -R grimnir:grimnir /etc/grimnir
```

### High CPU/Memory Usage

Check resource limits in service files and adjust:
- `MemoryLimit`
- `MemoryHigh`
- `CPUQuota`

Monitor with:
```bash
systemctl show mediaengine | grep -E '(Memory|CPU)'
```

## Security Considerations

1. **Firewall**: Only expose HTTP port externally, keep gRPC internal
   ```bash
   sudo ufw allow 8080/tcp
   sudo ufw deny 9091/tcp
   ```

2. **TLS**: Use reverse proxy (nginx/caddy) for HTTPS termination

3. **JWT Secret**: Keep `/etc/grimnir/jwt.secret` secure with 600 permissions

4. **Database**: Use strong passwords and limit network access

5. **File Permissions**: Media files should only be writable by grimnir user

## Performance Tuning

### For High Station Count (10+)

Increase resource limits in service files:
```ini
MemoryLimit=4G
MemoryHigh=3G
CPUQuota=200%
LimitNPROC=2048
```

### For Low-Latency Live Input

Reduce buffer sizes in media engine configuration:
```bash
LIVE_INPUT_BUFFER_MS=500
```

### For High-Quality DSP Processing

Allocate more CPU to media engine:
```ini
CPUQuota=150%
Nice=-5
```

## Multi-Instance Deployment

For horizontal scaling, see `docs/MULTI_INSTANCE.md` (to be created in Phase 5).

## Support

- GitHub Issues: https://github.com/friendsincode/grimnir_radio/issues
- Documentation: https://github.com/friendsincode/grimnir_radio/tree/main/docs
