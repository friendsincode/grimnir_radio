# Grimnir Radio - Docker Deployment Guide

Complete guide for deploying Grimnir Radio using Docker Compose.

## Table of Contents

- [Quick Start](#quick-start)
- [Prerequisites](#prerequisites)
- [Deployment Modes](#deployment-modes)
  - [Turn-Key Deployment (Recommended)](#turn-key-deployment-recommended)
  - [Basic Deployment](#basic-deployment)
  - [Development Deployment](#development-deployment)
- [Configuration](#configuration)
- [Services](#services)
- [Advanced Usage](#advanced-usage)
- [Troubleshooting](#troubleshooting)
- [Upgrading](#upgrading)

---

## Quick Start

Get Grimnir Radio running in 30 seconds:

```bash
# Clone repository
git clone https://github.com/friendsincode/grimnir_radio.git
cd grimnir_radio

# Run quick-start script (handles everything automatically)
./scripts/docker-quick-start.sh
```

The script will:
1. ✓ Check Docker and Docker Compose are installed
2. ✓ Create `.env` file with secure random passwords
3. ✓ Build Docker images
4. ✓ Start all services (API, Media Engine, PostgreSQL, Redis, Icecast2)
5. ✓ Display access URLs and credentials

**Default URLs:**
- API: http://localhost:8080
- Metrics: http://localhost:9000/metrics
- Icecast2: http://localhost:8000
- Icecast Admin: http://localhost:8000/admin

---

## Prerequisites

### Required Software

- **Docker** 20.10+ ([Install Docker](https://docs.docker.com/get-docker/))
- **Docker Compose** 2.0+ (included with Docker Desktop, or [Install separately](https://docs.docker.com/compose/install/))

### System Requirements

**Minimum:**
- 2 CPU cores
- 4 GB RAM
- 20 GB disk space

**Recommended (Production):**
- 4+ CPU cores
- 8+ GB RAM
- 100+ GB disk space (for media library)

### Port Requirements

Ensure these ports are available:
- **8080** - Grimnir Radio HTTP API
- **9000** - Prometheus metrics
- **9091** - Media Engine gRPC
- **8000** - Icecast2 streaming server
- **5432** - PostgreSQL database
- **6379** - Redis

---

## Deployment Modes

### Turn-Key Deployment (Recommended)

Full production-ready deployment with all dependencies included.

**What's Included:**
- ✓ Grimnir Radio Control Plane
- ✓ Media Engine (GStreamer)
- ✓ PostgreSQL database
- ✓ Redis (event bus & leader election)
- ✓ Icecast2 streaming server
- ✓ Health checks on all services
- ✓ Auto-generated secure passwords
- ✓ Persistent data volumes

**Deploy:**

```bash
./scripts/docker-quick-start.sh
```

**Stop:**

```bash
./scripts/docker-quick-start.sh --stop
```

**Clean (remove all data):**

```bash
./scripts/docker-quick-start.sh --clean
```

---

### Basic Deployment

Manual deployment with full control over configuration.

**Steps:**

1. **Create environment file:**

```bash
cp .env.example .env
# Edit .env with your preferred settings
nano .env
```

2. **Build images:**

```bash
docker-compose build
```

3. **Start services:**

```bash
docker-compose up -d
```

4. **Check status:**

```bash
docker-compose ps
docker-compose logs -f
```

---

### Development Deployment

Optimized for local development with debug logging and hot-reloading.

**Deploy:**

```bash
./scripts/docker-quick-start.sh --dev
```

**Or manually:**

```bash
cp .env.example .env

# Enable debug logging
sed -i 's/LOG_LEVEL=info/LOG_LEVEL=debug/' .env
sed -i 's/ENVIRONMENT=production/ENVIRONMENT=development/' .env

docker-compose up --build
```

---

## Configuration

### Environment Variables

All configuration is managed via `.env` file. See `.env.example` for full list.

**Key Settings:**

#### HTTP Server

```bash
GRIMNIR_HTTP_BIND=0.0.0.0
GRIMNIR_HTTP_PORT=8080
```

#### Database

```bash
POSTGRES_PASSWORD=your-secure-password
GRIMNIR_DB_BACKEND=postgres
GRIMNIR_DB_DSN=host=postgres port=5432 user=grimnir password=... dbname=grimnir
```

#### Authentication

```bash
JWT_SIGNING_KEY=your-random-secret-change-in-production
GRIMNIR_JWT_TTL_MINUTES=15
```

#### Icecast2

```bash
ICECAST_ADMIN_USERNAME=admin
ICECAST_ADMIN_PASSWORD=secure-password
ICECAST_SOURCE_PASSWORD=source-password
ICECAST_PORT=8000
ICECAST_MAX_CLIENTS=100
ICECAST_MAX_SOURCES=10
```

#### Media Storage

```bash
# Filesystem (default)
GRIMNIR_MEDIA_BACKEND=filesystem
GRIMNIR_MEDIA_ROOT=/var/lib/grimnir/media

# S3 (optional)
# GRIMNIR_MEDIA_BACKEND=s3
# GRIMNIR_S3_BUCKET=grimnir-media
# GRIMNIR_S3_REGION=us-east-1
# GRIMNIR_S3_ACCESS_KEY_ID=...
# GRIMNIR_S3_SECRET_ACCESS_KEY=...
```

### Custom Configuration

Create `docker-compose.override.yml` for customizations:

```bash
cp docker-compose.override.yml.example docker-compose.override.yml
nano docker-compose.override.yml
```

**Example overrides:**

**Change API port:**

```yaml
services:
  grimnir:
    ports:
      - "3000:8080"
```

**Mount local media directory:**

```yaml
services:
  grimnir:
    volumes:
      - ./my-media:/var/lib/grimnir/media
```

**Use external database:**

```yaml
services:
  grimnir:
    environment:
      GRIMNIR_DB_DSN: "host=external-db.example.com port=5432 user=grimnir password=secret dbname=grimnir"
    depends_on:
      - redis
      - mediaengine

  postgres:
    profiles:
      - disabled  # Disable local postgres service
```

---

## Services

### Service Architecture

```
┌─────────────────────────────────────────────┐
│             Load Balancer / Nginx           │
│               (optional)                    │
└────────────────┬────────────────────────────┘
                 │
┌────────────────▼────────────────────────────┐
│        Grimnir Radio Control Plane          │
│         (HTTP API, Scheduler)               │
│              Port: 8080, 9000               │
└──┬─────────┬─────────┬───────────┬──────────┘
   │         │         │           │
   ▼         ▼         ▼           ▼
┌──────┐ ┌────────┐ ┌──────┐ ┌──────────┐
│ DB   │ │ Redis  │ │Media │ │ Icecast2 │
│(PG)  │ │        │ │Engine│ │          │
│:5432 │ │ :6379  │ │:9091 │ │  :8000   │
└──────┘ └────────┘ └──────┘ └──────────┘
```

### Service Details

#### 1. Grimnir Radio (Control Plane)

**Container:** `grimnir-radio`
**Ports:** 8080 (HTTP API), 9000 (metrics)
**Purpose:** REST API, scheduler, authentication, playout coordinator

**Endpoints:**
- `GET /healthz` - Health check
- `GET /api/v1/*` - REST API
- `GET /metrics` - Prometheus metrics
- `WS /api/v1/events` - WebSocket event stream

#### 2. Media Engine

**Container:** `grimnir-mediaengine`
**Ports:** 9091 (gRPC), 9092 (metrics)
**Purpose:** Audio processing, GStreamer pipelines, DSP

**Features:**
- Crossfading with cue points
- Loudness normalization (EBU R128)
- AGC, compression, limiting
- Live input routing
- Webstream relay with failover

#### 3. PostgreSQL

**Container:** `grimnir-postgres`
**Ports:** 5432
**Purpose:** Primary database for metadata, schedule, users

**Volume:** `postgres-data` (persistent)

#### 4. Redis

**Container:** `grimnir-redis`
**Ports:** 6379
**Purpose:** Event bus, leader election, session storage

**Volume:** `redis-data` (persistent with AOF)

#### 5. Icecast2

**Container:** `grimnir-icecast`
**Ports:** 8000
**Purpose:** HTTP streaming server for audio broadcast

**Volume:** `icecast-logs` (logs)

**Admin URL:** http://localhost:8000/admin

---

## Advanced Usage

### Multi-Instance Deployment

Run multiple API instances with leader election for high availability.

**Create `docker-compose.override.yml`:**

```yaml
version: '3.8'

services:
  grimnir:
    environment:
      LEADER_ELECTION_ENABLED: "true"
      GRIMNIR_INSTANCE_ID: grimnir-1

  grimnir-2:
    image: grimnir-radio:latest
    container_name: grimnir-radio-2
    environment:
      # Copy all from grimnir service
      LEADER_ELECTION_ENABLED: "true"
      GRIMNIR_INSTANCE_ID: grimnir-2
      # ... other env vars ...
    ports:
      - "8081:8080"
      - "9001:9000"
    networks:
      - grimnir-network
    depends_on:
      postgres:
        condition: service_healthy
      redis:
        condition: service_healthy
      mediaengine:
        condition: service_healthy

  grimnir-3:
    image: grimnir-radio:latest
    container_name: grimnir-radio-3
    environment:
      LEADER_ELECTION_ENABLED: "true"
      GRIMNIR_INSTANCE_ID: grimnir-3
      # ... other env vars ...
    ports:
      - "8082:8080"
      - "9002:9000"
    networks:
      - grimnir-network
    depends_on:
      postgres:
        condition: service_healthy
      redis:
        condition: service_healthy
      mediaengine:
        condition: service_healthy
```

**Start:**

```bash
docker-compose up -d
```

**Add nginx load balancer:**

```nginx
upstream grimnir_api {
    least_conn;
    server localhost:8080;
    server localhost:8081;
    server localhost:8082;
}

server {
    listen 80;
    server_name radio.example.com;

    location /api/ {
        proxy_pass http://grimnir_api;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
    }

    location /events {
        proxy_pass http://grimnir_api;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
    }
}
```

---

### Adding Monitoring Stack

**Add to `docker-compose.override.yml`:**

```yaml
services:
  prometheus:
    image: prom/prometheus:latest
    container_name: grimnir-prometheus
    ports:
      - "9090:9090"
    volumes:
      - ./deploy/prometheus/prometheus.yml:/etc/prometheus/prometheus.yml:ro
      - ./deploy/prometheus/alerts.yml:/etc/prometheus/alerts.yml:ro
      - prometheus-data:/prometheus
    command:
      - '--config.file=/etc/prometheus/prometheus.yml'
      - '--storage.tsdb.path=/prometheus'
    networks:
      - grimnir-network

  grafana:
    image: grafana/grafana:latest
    container_name: grimnir-grafana
    ports:
      - "3000:3000"
    environment:
      GF_SECURITY_ADMIN_PASSWORD: admin
    volumes:
      - grafana-data:/var/lib/grafana
      - ./deploy/grafana/dashboards:/etc/grafana/provisioning/dashboards:ro
      - ./deploy/grafana/datasources:/etc/grafana/provisioning/datasources:ro
    networks:
      - grimnir-network
    depends_on:
      - prometheus

volumes:
  prometheus-data:
  grafana-data:
```

**Start:**

```bash
docker-compose up -d prometheus grafana
```

**Access:**
- Prometheus: http://localhost:9090
- Grafana: http://localhost:3000 (admin/admin)

---

### Adding Distributed Tracing

**Add Jaeger to `docker-compose.override.yml`:**

```yaml
services:
  jaeger:
    image: jaegertracing/all-in-one:latest
    container_name: grimnir-jaeger
    ports:
      - "16686:16686"  # Jaeger UI
      - "4317:4317"    # OTLP gRPC
    environment:
      COLLECTOR_OTLP_ENABLED: "true"
    networks:
      - grimnir-network

  grimnir:
    environment:
      TRACING_ENABLED: "true"
      OTLP_ENDPOINT: "jaeger:4317"
      TRACING_SAMPLE_RATE: "1.0"
```

**Start:**

```bash
docker-compose up -d
```

**Access Jaeger UI:** http://localhost:16686

---

### SSL/TLS with Let's Encrypt

**Add nginx with certbot:**

```yaml
services:
  nginx:
    image: nginx:alpine
    container_name: grimnir-nginx
    ports:
      - "80:80"
      - "443:443"
    volumes:
      - ./nginx.conf:/etc/nginx/nginx.conf:ro
      - ./certbot/conf:/etc/letsencrypt:ro
      - ./certbot/www:/var/www/certbot:ro
    networks:
      - grimnir-network
    depends_on:
      - grimnir

  certbot:
    image: certbot/certbot
    container_name: grimnir-certbot
    volumes:
      - ./certbot/conf:/etc/letsencrypt
      - ./certbot/www:/var/www/certbot
    entrypoint: "/bin/sh -c 'trap exit TERM; while :; do certbot renew; sleep 12h & wait $${!}; done;'"
```

**Get certificate:**

```bash
docker-compose run --rm certbot certonly --webroot \
  --webroot-path /var/www/certbot \
  -d radio.example.com \
  --email admin@example.com \
  --agree-tos \
  --no-eff-email
```

---

## Troubleshooting

### Check Service Status

```bash
# View all services
docker-compose ps

# View logs
docker-compose logs -f

# View specific service logs
docker-compose logs -f grimnir
docker-compose logs -f mediaengine
docker-compose logs -f icecast

# Check health
docker-compose exec grimnir curl -f http://localhost:8080/healthz
```

### Common Issues

#### 1. Database Connection Failed

**Error:** `failed to connect to database`

**Solution:**

```bash
# Check postgres is running and healthy
docker-compose ps postgres

# Check database credentials
docker-compose exec postgres psql -U grimnir -d grimnir -c "SELECT 1;"

# Restart database
docker-compose restart postgres

# Verify DSN in .env matches container settings
grep GRIMNIR_DB_DSN .env
```

#### 2. Media Engine Not Connecting

**Error:** `rpc error: code = Unavailable`

**Solution:**

```bash
# Check media engine is running
docker-compose ps mediaengine

# Check gRPC port
docker-compose exec mediaengine netstat -tlnp | grep 9091

# Test gRPC connection
docker-compose exec grimnir nc -zv mediaengine 9091

# Check media engine logs
docker-compose logs mediaengine
```

#### 3. Icecast Not Accessible

**Error:** `connection refused on port 8000`

**Solution:**

```bash
# Check icecast is running
docker-compose ps icecast

# Check port binding
docker-compose port icecast 8000

# Test connection
curl -I http://localhost:8000/status.xsl

# Check logs
docker-compose logs icecast
```

#### 4. Port Already in Use

**Error:** `bind: address already in use`

**Solution:**

```bash
# Find what's using the port (e.g., 8080)
lsof -i :8080
netstat -tlnp | grep 8080

# Kill the process or change port in docker-compose.override.yml
```

#### 5. Permission Denied on Volumes

**Error:** `permission denied` when accessing volumes

**Solution:**

```bash
# Fix volume permissions
docker-compose down
sudo chown -R 1000:1000 ./media-data
docker-compose up -d

# Or run with user override
docker-compose run --user $(id -u):$(id -g) grimnir
```

### Reset Everything

```bash
# Stop and remove all containers, volumes, and images
docker-compose down -v --rmi all

# Clean up Docker system
docker system prune -a --volumes

# Start fresh
./scripts/docker-quick-start.sh
```

---

## Upgrading

### From 1.0.x to 1.1.0

1. **Backup data:**

```bash
# Backup database
docker-compose exec postgres pg_dump -U grimnir grimnir > backup.sql

# Backup media
docker-compose cp grimnir:/var/lib/grimnir/media ./media-backup
```

2. **Pull new version:**

```bash
git pull origin main
```

3. **Update images:**

```bash
docker-compose build --pull
```

4. **Run migrations:**

```bash
docker-compose run --rm grimnir /usr/local/bin/grimnirradio migrate
```

5. **Restart services:**

```bash
docker-compose up -d
```

6. **Verify:**

```bash
docker-compose logs -f grimnir
curl http://localhost:8080/healthz
```

### Rolling Back

```bash
# Stop current version
docker-compose down

# Checkout previous version
git checkout v1.0.0

# Restore database backup
docker-compose up -d postgres
docker-compose exec -T postgres psql -U grimnir grimnir < backup.sql

# Restart services
docker-compose up -d
```

---

## Production Checklist

Before deploying to production:

- [ ] Change all default passwords in `.env`
- [ ] Use strong JWT signing key (32+ random characters)
- [ ] Enable TLS/SSL with valid certificates
- [ ] Configure firewall rules (only expose 80, 443)
- [ ] Set up database backups (daily)
- [ ] Configure Prometheus alerting
- [ ] Set up log aggregation (ELK, Loki)
- [ ] Enable Redis persistence (AOF + RDB)
- [ ] Configure resource limits in docker-compose.yml
- [ ] Set up health check monitoring (UptimeRobot, Pingdom)
- [ ] Test disaster recovery procedure
- [ ] Document runbooks for common issues
- [ ] Enable multi-instance deployment for HA
- [ ] Configure CDN for Icecast streams (optional)
- [ ] Set up automated updates (Watchtower)

---

## Resources

- **Documentation:** [Wiki Home](Home)
- **API Reference:** [API Reference](API-Reference)
- **Migration Guide:** [Migration Guide](Migration-Guide)
- **Architecture:** [Architecture](Architecture)
- **GitHub Issues:** https://github.com/friendsincode/grimnir_radio/issues
- **Docker Hub:** (TBD)

---

## License

Grimnir Radio is licensed under the GNU Affero General Public License v3.0 or later.

See [LICENSE](../LICENSE) for full text.

---

**Built in honor of Grimnir. For the community, by the community.**
