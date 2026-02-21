# Grimnir Radio Production Deployment Guide

**Version:** 1.0
**Last Updated:** 2026-01-22
**Target Platforms:** Docker Compose, Kubernetes, Bare Metal

This guide covers deploying Grimnir Radio to production environments with best practices for security, reliability, and performance.

---

## Table of Contents

- [Infrastructure Requirements](#infrastructure-requirements)
- [Pre-Deployment Checklist](#pre-deployment-checklist)
- [Deployment Options](#deployment-options)
  - [Docker Compose](#docker-compose-deployment)
  - [Kubernetes](#kubernetes-deployment)
  - [Bare Metal](#bare-metal-deployment)
- [Post-Deployment Verification](#post-deployment-verification)
- [Security Hardening](#security-hardening)
- [Backup and Disaster Recovery](#backup-and-disaster-recovery)
- [Monitoring and Alerting](#monitoring-and-alerting)
- [Performance Tuning](#performance-tuning)
- [Troubleshooting](#troubleshooting)
- [Maintenance](#maintenance)

---

## Infrastructure Requirements

### Minimum Requirements (Small Deployment, 1-5 Stations)

| Component | CPU | RAM | Storage | Notes |
|-----------|-----|-----|---------|-------|
| **Control Plane** | 2 cores | 2GB | 10GB | Grimnir Radio binary |
| **Media Engine** | 2 cores | 4GB | 5GB | GStreamer processing |
| **PostgreSQL** | 2 cores | 2GB | 20GB | Database |
| **Redis** | 1 core | 512MB | 1GB | Leader election, caching |
| **Media Storage** | - | - | 50GB+ | Audio files, varies by library |

**Total Minimum:** 7 CPU cores, 8.5GB RAM, 86GB storage

### Recommended Requirements (Medium Deployment, 5-20 Stations)

| Component | CPU | RAM | Storage | Notes |
|-----------|-----|-----|---------|-------|
| **Control Plane (3x)** | 2 cores | 4GB | 10GB | High availability |
| **Media Engine (2x)** | 4 cores | 8GB | 10GB | Per-station isolation |
| **PostgreSQL** | 4 cores | 8GB | 100GB | Connection pooling |
| **Redis** | 2 cores | 2GB | 5GB | Persistence enabled |
| **Media Storage** | - | - | 200GB+ | Shared storage (NFS/S3) |

**Total Recommended:** 22 CPU cores, 50GB RAM, 345GB storage

### Enterprise Requirements (Large Deployment, 20+ Stations)

| Component | CPU | RAM | Storage | Notes |
|-----------|-----|-----|---------|-------|
| **Control Plane (5x)** | 4 cores | 8GB | 20GB | Multi-AZ deployment |
| **Media Engine (5x)** | 8 cores | 16GB | 20GB | Dedicated per-station |
| **PostgreSQL (Primary)** | 8 cores | 16GB | 500GB | Managed RDS/Cloud SQL |
| **PostgreSQL (Replicas)** | 8 cores | 16GB | 500GB | Read replicas |
| **Redis Sentinel (3x)** | 2 cores | 4GB | 10GB | High availability |
| **Media Storage** | - | - | 1TB+ | Distributed storage |
| **Load Balancer** | 2 cores | 2GB | 10GB | Nginx/HAProxy |

**Total Enterprise:** 94+ CPU cores, 220GB+ RAM, 2.6TB+ storage

### Network Requirements

- **Bandwidth:** 10 Mbps minimum per concurrent stream (25-50 Mbps recommended)
- **Latency:** < 50ms between components
- **Firewall Rules:**
  - Inbound: 80/443 (HTTP/HTTPS), 8000-8100 (Icecast mounts)
  - Outbound: 80/443 (CDN/webstreams), 1935 (RTMP if used)
  - Internal: 5432 (PostgreSQL), 6379 (Redis), 9091 (Media engine gRPC)

### Supported Operating Systems

- **Docker:** Any Linux distribution with Docker 20.10+
- **Kubernetes:** 1.24+ (tested on EKS, GKE, AKS, k3s)
- **Bare Metal:** Ubuntu 22.04 LTS, Debian 12, RHEL 9/Rocky Linux 9

---

## Pre-Deployment Checklist

### Security

- [ ] Generate strong random passwords for all services
- [ ] Create JWT signing key: `openssl rand -base64 32`
- [ ] Configure firewall rules (block unnecessary ports)
- [ ] Set up TLS certificates (Let's Encrypt recommended)
- [ ] Review and customize security contexts (non-root users)
- [ ] Disable database remote root access
- [ ] Enable Redis authentication
- [ ] Configure rate limiting on API endpoints

### Networking

- [ ] Reserve static IP addresses or configure DNS
- [ ] Set up load balancer (if multi-instance)
- [ ] Configure CDN for stream delivery (optional)
- [ ] Test connectivity between components
- [ ] Configure reverse proxy (nginx/Traefik)
- [ ] Set up WebSocket-compatible proxy

### Storage

- [ ] Provision persistent volumes for database
- [ ] Set up shared storage for media files (NFS/S3/GlusterFS)
- [ ] Configure backup storage location
- [ ] Test storage performance (IOPS, throughput)
- [ ] Set up disk space monitoring

### Observability

- [ ] Deploy Prometheus for metrics collection
- [ ] Set up Grafana dashboards
- [ ] Configure log aggregation (ELK/Loki)
- [ ] Set up distributed tracing (Jaeger/Tempo) - optional
- [ ] Configure alerting rules
- [ ] Set up uptime monitoring (external)

### Disaster Recovery

- [ ] Plan database backup schedule (daily minimum)
- [ ] Configure media file backups
- [ ] Document restore procedures
- [ ] Test backup restoration
- [ ] Set up offsite backup storage

---

## Deployment Options

### Docker Compose Deployment

**Best for:** Development, small deployments, single-server setups

#### 1. Prepare Server

```bash
# Update system
sudo apt update && sudo apt upgrade -y

# Install Docker
curl -fsSL https://get.docker.com | sh
sudo usermod -aG docker $USER

# Install Docker Compose
sudo apt install docker-compose-plugin -y

# Verify installation
docker --version
docker compose version
```

#### 2. Clone Repository

```bash
git clone https://github.com/friendsincode/grimnir_radio.git
cd grimnir_radio
```

#### 3. Build Images

```bash
# Build control plane
docker build -t grimnir-radio:latest -f Dockerfile .

# Build media engine
docker build -t grimnir-mediaengine:latest -f Dockerfile.mediaengine .
```

#### 4. Configure Environment

```bash
# Copy example environment file
cp .env.docker.example .env

# Generate secure secrets
POSTGRES_PASS=$(openssl rand -base64 32)
REDIS_PASS=$(openssl rand -base64 32)
JWT_KEY=$(openssl rand -base64 32)

# Update .env file
sed -i "s/POSTGRES_PASSWORD=.*/POSTGRES_PASSWORD=$POSTGRES_PASS/" .env
sed -i "s/REDIS_PASSWORD=.*/REDIS_PASSWORD=$REDIS_PASS/" .env
sed -i "s/JWT_SIGNING_KEY=.*/JWT_SIGNING_KEY=$JWT_KEY/" .env
```

#### 5. Start Services

```bash
# Start in detached mode
docker compose up -d

# View logs
docker compose logs -f

# Check service health
docker compose ps
```

#### 6. Initialize Database

```bash
# Database migrations run automatically on first start
# Verify by checking grimnir container logs
docker compose logs grimnir | grep migration
```

#### 7. Create Admin User

```bash
# Access the database
docker compose exec postgres psql -U grimnir -d grimnir

# Create admin user (execute in psql)
INSERT INTO users (id, username, email, password_hash, role, active)
VALUES (
  gen_random_uuid()::text,
  'admin',
  'admin@example.com',
  '$2a$10$YourBcryptHashHere',  -- Generate with: htpasswd -bnBC 10 "" password | tr -d ':\n'
  'admin',
  true
);
\q
```

#### 8. Verify Deployment

```bash
# Health check
curl http://localhost:8080/healthz

# Metrics
curl http://localhost:9000/metrics

# API test (get JWT token first)
curl -X POST http://localhost:8080/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username":"admin","password":"your_password"}'
```

#### 9. Configure Reverse Proxy (Nginx)

```nginx
# /etc/nginx/sites-available/grimnir-radio
upstream grimnir_backend {
    server localhost:8080;
}

server {
    listen 80;
    server_name radio.example.com;

    # Redirect to HTTPS
    return 301 https://$server_name$request_uri;
}

server {
    listen 443 ssl http2;
    server_name radio.example.com;

    # TLS configuration
    ssl_certificate /etc/letsencrypt/live/radio.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/radio.example.com/privkey.pem;
    ssl_protocols TLSv1.2 TLSv1.3;
    ssl_ciphers HIGH:!aNULL:!MD5;

    # Security headers
    add_header Strict-Transport-Security "max-age=31536000" always;
    add_header X-Frame-Options "SAMEORIGIN" always;
    add_header X-Content-Type-Options "nosniff" always;

    # API proxy
    location / {
        proxy_pass http://grimnir_backend;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;

        # WebSocket support
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";

        # Streaming support - disable buffering for audio streams
        proxy_buffering off;
        proxy_cache off;
    }

    # Audio streams - special handling for /live/ and /stream/ endpoints
    location ~ ^/(live|stream)/ {
        proxy_pass http://grimnir_backend;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;

        # Critical for streaming: disable all buffering
        proxy_buffering off;
        proxy_cache off;
        proxy_request_buffering off;

        # No timeout for long-running stream connections
        proxy_read_timeout 24h;
        proxy_send_timeout 24h;

        # Chunked transfer encoding for streaming
        chunked_transfer_encoding on;
    }

    # Rate limiting
    limit_req_zone $binary_remote_addr zone=api:10m rate=10r/s;
    limit_req zone=api burst=20 nodelay;

    # File upload size
    client_max_body_size 100M;
}
```

Enable and restart:
```bash
sudo ln -s /etc/nginx/sites-available/grimnir-radio /etc/nginx/sites-enabled/
sudo nginx -t
sudo systemctl restart nginx
```

---

### Kubernetes Deployment

**Best for:** Production, multi-instance, high availability

See [kubernetes/README.md](../kubernetes/README.md) for detailed Kubernetes deployment instructions.

**Quick Start:**

```bash
# 1. Create namespace
kubectl apply -f kubernetes/namespace.yaml

# 2. Create secrets (DO NOT use example values!)
kubectl create secret generic grimnir-secrets \
  --namespace=grimnir-radio \
  --from-literal=POSTGRES_PASSWORD=$(openssl rand -base64 32) \
  --from-literal=REDIS_PASSWORD=$(openssl rand -base64 32) \
  --from-literal=GRIMNIR_JWT_SIGNING_KEY=$(openssl rand -base64 32) \
  --from-literal=GRIMNIR_DB_DSN="host=grimnir-postgres port=5432 user=grimnir password=$(openssl rand -base64 32) dbname=grimnir sslmode=disable" \
  --from-literal=GRIMNIR_REDIS_ADDR="grimnir-redis:6379" \
  --from-literal=GRIMNIR_REDIS_PASSWORD="$(openssl rand -base64 32)"

# 3. Customize configuration
vi kubernetes/configmap.yaml
vi kubernetes/ingress.yaml  # Update hostname

# 4. Deploy all resources
kubectl apply -f kubernetes/

# 5. Wait for rollout
kubectl rollout status deployment/grimnir-radio -n grimnir-radio

# 6. Verify deployment
kubectl get pods -n grimnir-radio
kubectl logs -n grimnir-radio -l app=grimnir-radio --tail=50
```

---

### Bare Metal Deployment

**Best for:** On-premises, legacy infrastructure, full control

#### Prerequisites

```bash
# Install PostgreSQL 15
sudo apt install postgresql-15 postgresql-contrib-15

# Install Redis
sudo apt install redis-server

# Install GStreamer
sudo apt install \
  gstreamer1.0-tools \
  gstreamer1.0-plugins-base \
  gstreamer1.0-plugins-good \
  gstreamer1.0-plugins-bad \
  gstreamer1.0-plugins-ugly \
  gstreamer1.0-libav
```

#### Database Setup

```bash
# Create database user
sudo -u postgres createuser grimnir

# Create database
sudo -u postgres createdb grimnir -O grimnir

# Set password
sudo -u postgres psql -c "ALTER USER grimnir PASSWORD 'secure_password_here';"

# Configure PostgreSQL for remote access (if needed)
sudo vi /etc/postgresql/15/main/postgresql.conf
# Set: listen_addresses = 'localhost'

sudo vi /etc/postgresql/15/main/pg_hba.conf
# Add: host grimnir grimnir 127.0.0.1/32 scram-sha-256

sudo systemctl restart postgresql
```

#### Redis Setup

```bash
# Configure Redis authentication
sudo vi /etc/redis/redis.conf
# Set: requirepass your_redis_password

sudo systemctl restart redis-server
```

#### Build and Install Grimnir Radio

```bash
# Install Go 1.22
wget https://go.dev/dl/go1.22.0.linux-amd64.tar.gz
sudo tar -C /usr/local -xzf go1.22.0.linux-amd64.tar.gz
export PATH=$PATH:/usr/local/go/bin

# Clone and build
git clone https://github.com/friendsincode/grimnir_radio.git
cd grimnir_radio

# Build control plane
go build -o /usr/local/bin/grimnirradio ./cmd/grimnirradio

# Build media engine
go build -o /usr/local/bin/mediaengine ./cmd/mediaengine

# Create directories
sudo mkdir -p /var/lib/grimnir/media
sudo mkdir -p /etc/grimnir
sudo useradd -r -s /bin/false grimnir
sudo chown -R grimnir:grimnir /var/lib/grimnir
```

#### Configuration

```bash
# Create environment file
sudo vi /etc/grimnir/grimnir.env
```

```bash
GRIMNIR_HTTP_PORT=8080
GRIMNIR_HTTP_BIND=127.0.0.1
GRIMNIR_DB_BACKEND=postgres
GRIMNIR_DB_DSN="host=localhost port=5432 user=grimnir password=secure_password_here dbname=grimnir sslmode=disable"
GRIMNIR_REDIS_ADDR=localhost:6379
GRIMNIR_REDIS_PASSWORD=your_redis_password
GRIMNIR_JWT_SIGNING_KEY=$(openssl rand -base64 32)
GRIMNIR_ICECAST_SOURCE_PASSWORD=$(openssl rand -base64 24)
GRIMNIR_MEDIA_ROOT=/var/lib/grimnir/media
GRIMNIR_MEDIA_ENGINE_GRPC_ADDR=localhost:9091
GRIMNIR_LOG_LEVEL=info
```

Production secret requirements:
- `GRIMNIR_ICECAST_SOURCE_PASSWORD` must be set and must not be a default placeholder.
- If TURN is enabled (`GRIMNIR_WEBRTC_TURN_URL` is set), both of the following are required:
  - `GRIMNIR_WEBRTC_TURN_USERNAME`
  - `GRIMNIR_WEBRTC_TURN_PASSWORD`

#### Systemd Services

See [deploy/systemd/grimnirradio.service](../deploy/systemd/grimnirradio.service) and [deploy/systemd/mediaengine.service](../deploy/systemd/mediaengine.service).

```bash
# Copy service files
sudo cp deploy/systemd/mediaengine.service /etc/systemd/system/
sudo cp deploy/systemd/grimnirradio.service /etc/systemd/system/

# Reload systemd
sudo systemctl daemon-reload

# Start services
sudo systemctl enable --now mediaengine
sudo systemctl enable --now grimnirradio

# Check status
sudo systemctl status mediaengine
sudo systemctl status grimnirradio
```

---

## Post-Deployment Verification

### Health Checks

```bash
# Control plane health
curl http://localhost:8080/healthz
# Expected: {"status":"ok"}

# Metrics endpoint
curl http://localhost:9000/metrics | grep grimnir_

# Database connectivity
curl http://localhost:8080/api/v1/stations
# Expected: [] or list of stations
```

### Functional Tests

```bash
# 1. Create a station
curl -X POST http://localhost:8080/api/v1/stations \
  -H "Authorization: Bearer $JWT_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Test FM",
    "description": "Test station",
    "active": true
  }'

# 2. Upload media file
curl -X POST http://localhost:8080/api/v1/media \
  -H "Authorization: Bearer $JWT_TOKEN" \
  -F "file=@test_audio.mp3" \
  -F "station_id=<station_id>"

# 3. Create a smart block
# 4. Create a clock
# 5. Verify schedule generation
```

### Performance Baseline

```bash
# Response time test
ab -n 1000 -c 10 http://localhost:8080/healthz

# Database query performance
docker compose exec postgres psql -U grimnir -d grimnir -c "EXPLAIN ANALYZE SELECT * FROM stations;"
```

---

## Security Hardening

### 1. Network Security

```bash
# Configure firewall (UFW example)
sudo ufw default deny incoming
sudo ufw default allow outgoing
sudo ufw allow 22/tcp   # SSH
sudo ufw allow 80/tcp   # HTTP
sudo ufw allow 443/tcp  # HTTPS
sudo ufw enable

# Block direct database access from outside
sudo ufw deny from any to any port 5432
sudo ufw deny from any to any port 6379
```

### 2. Application Security

```bash
# Disable debug mode
export GRIMNIR_LOG_LEVEL=info

# Enable rate limiting in nginx (see nginx config above)

# Set session timeout
export GRIMNIR_JWT_TTL_MINUTES=15

# Disable unnecessary endpoints in production
# (configure in code if needed)
```

### 3. Database Security

```sql
-- Disable remote root login
ALTER USER postgres PASSWORD 'strong_password';

-- Create limited user for application
CREATE ROLE grimnir_app LOGIN PASSWORD 'app_password';
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO grimnir_app;

-- Enable row-level security (if needed)
ALTER TABLE stations ENABLE ROW LEVEL SECURITY;
```

### 4. Secrets Management

**For Kubernetes:**
- Use External Secrets Operator
- Integrate with HashiCorp Vault
- Use cloud provider secrets (AWS Secrets Manager, GCP Secret Manager)

**For Docker Compose:**
- Use Docker secrets (Swarm mode)
- Mount secrets from files with restricted permissions
- Rotate secrets regularly

```bash
# Example: Rotate JWT signing key
NEW_KEY=$(openssl rand -base64 32)
kubectl patch secret grimnir-secrets -n grimnir-radio \
  -p "{\"data\":{\"GRIMNIR_JWT_SIGNING_KEY\":\"$(echo -n $NEW_KEY | base64)\"}}"

# Rolling restart to pick up new key
kubectl rollout restart deployment/grimnir-radio -n grimnir-radio
```

### 5. TLS/SSL Configuration

```bash
# Generate Let's Encrypt certificate
sudo certbot --nginx -d radio.example.com

# Auto-renewal
sudo systemctl enable certbot.timer
```

---

## Backup and Disaster Recovery

### Database Backup

**Automated Daily Backups:**

```bash
#!/bin/bash
# /usr/local/bin/backup-grimnir-db.sh

BACKUP_DIR=/var/backups/grimnir
DATE=$(date +%Y%m%d_%H%M%S)
BACKUP_FILE="grimnir_db_$DATE.sql.gz"

# Create backup directory
mkdir -p $BACKUP_DIR

# Docker Compose deployment
docker compose exec -T postgres pg_dump -U grimnir grimnir | gzip > $BACKUP_DIR/$BACKUP_FILE

# OR for bare metal
pg_dump -U grimnir grimnir | gzip > $BACKUP_DIR/$BACKUP_FILE

# Keep only last 30 days
find $BACKUP_DIR -name "grimnir_db_*.sql.gz" -mtime +30 -delete

# Upload to S3 (optional)
aws s3 cp $BACKUP_DIR/$BACKUP_FILE s3://your-bucket/backups/

echo "Backup completed: $BACKUP_FILE"
```

**Cron Job:**
```bash
sudo crontab -e
# Add: 0 2 * * * /usr/local/bin/backup-grimnir-db.sh
```

### Database Restore

```bash
# Stop application
docker compose stop grimnir

# Restore from backup
gunzip < /var/backups/grimnir/grimnir_db_20260122_020000.sql.gz | \
  docker compose exec -T postgres psql -U grimnir -d grimnir

# Restart application
docker compose start grimnir
```

### Media Files Backup

```bash
#!/bin/bash
# /usr/local/bin/backup-grimnir-media.sh

MEDIA_DIR=/var/lib/grimnir/media
BACKUP_DIR=/var/backups/grimnir/media
DATE=$(date +%Y%m%d)

# Incremental backup with rsync
rsync -av --delete $MEDIA_DIR/ $BACKUP_DIR/current/

# Create snapshot
cp -al $BACKUP_DIR/current $BACKUP_DIR/$DATE

# Upload to S3 (sync)
aws s3 sync $MEDIA_DIR s3://your-bucket/media/
```

### Disaster Recovery Plan

**RTO (Recovery Time Objective):** < 1 hour
**RPO (Recovery Point Objective):** < 24 hours (daily backups)

**Recovery Steps:**

1. **Provision new infrastructure**
2. **Restore database from latest backup**
3. **Restore media files from S3/backup**
4. **Deploy application (same version)**
5. **Verify all stations and mounts**
6. **Update DNS if needed**
7. **Monitor for 24 hours**

**Test Recovery Quarterly:**
```bash
# 1. Create test environment
# 2. Restore latest backup
# 3. Verify application starts
# 4. Test API endpoints
# 5. Test media playback
# 6. Document any issues
```

---

## Monitoring and Alerting

### Prometheus Metrics

**Key Metrics to Monitor:**

| Metric | Alert Threshold | Description |
|--------|-----------------|-------------|
| `grimnir_scheduler_errors_total` | > 10/hour | Scheduler failures |
| `grimnir_playout_dropout_count_total` | > 5/hour | Audio dropouts |
| `grimnir_executor_state` | stuck | Executor not transitioning |
| `grimnir_leader_election_changes_total` | > 5/hour | Leader flapping |
| `up{job="grimnir-radio"}` | 0 | Service down |
| `process_resident_memory_bytes` | > 1GB | Memory leak |
| `http_request_duration_seconds{quantile="0.99"}` | > 1s | Slow API responses |

**Prometheus Alerts:**

See [deploy/prometheus/alerts.yml](../deploy/prometheus/alerts.yml) for complete alert rules.

### Grafana Dashboards

**Import Pre-built Dashboard:**

1. Navigate to Grafana → Dashboards → Import
2. Upload `deploy/grafana/grimnir-dashboard.json` (if exists)
3. Configure data source (Prometheus)

**Key Panels:**
- Scheduler tick rate
- Active executors per station
- API request rate and latency
- Database connection pool usage
- Media engine CPU/memory
- Leader election status

### Log Aggregation

**Using Loki (Kubernetes):**

```yaml
# promtail-config.yaml
scrape_configs:
  - job_name: kubernetes-pods
    kubernetes_sd_configs:
      - role: pod
    relabel_configs:
      - source_labels: [__meta_kubernetes_namespace]
        regex: grimnir-radio
        action: keep
```

**Using Filebeat (Docker Compose):**

```yaml
# filebeat.yml
filebeat.inputs:
  - type: container
    paths:
      - '/var/lib/docker/containers/*/*.log'
    processors:
      - add_docker_metadata: ~
      - decode_json_fields:
          fields: ["message"]
          target: ""

output.elasticsearch:
  hosts: ["elasticsearch:9200"]
```

### Uptime Monitoring

**External Services:**
- UptimeRobot
- Pingdom
- StatusCake

**Health Check Endpoint:** `https://radio.example.com/healthz`

---

## Performance Tuning

### Database Optimization

**PostgreSQL Configuration** (`postgresql.conf`):

```ini
# Memory
shared_buffers = 2GB              # 25% of RAM
effective_cache_size = 6GB        # 75% of RAM
work_mem = 50MB
maintenance_work_mem = 512MB

# Connections
max_connections = 200

# Query Planning
random_page_cost = 1.1            # For SSD
effective_io_concurrency = 200

# WAL
wal_buffers = 16MB
min_wal_size = 1GB
max_wal_size = 4GB

# Checkpoints
checkpoint_completion_target = 0.9
```

**Connection Pooling** (PgBouncer):

```ini
[databases]
grimnir = host=localhost port=5432 dbname=grimnir

[pgbouncer]
pool_mode = transaction
max_client_conn = 1000
default_pool_size = 25
```

**Indexes:**

```sql
-- Add indexes for common queries
CREATE INDEX CONCURRENTLY idx_schedule_entries_station_time
  ON schedule_entries(station_id, starts_at);

CREATE INDEX CONCURRENTLY idx_media_station_active
  ON media(station_id, active)
  WHERE active = true;

CREATE INDEX CONCURRENTLY idx_smart_block_rules_station
  ON smart_block_rules(station_id, smart_block_id);
```

### Application Tuning

**Environment Variables:**

```bash
# Increase scheduler lookahead for large libraries
GRIMNIR_SCHEDULER_LOOKAHEAD=72h

# Adjust tick interval based on load
GRIMNIR_SCHEDULER_TICK_INTERVAL=60s

# Database connection pool
GRIMNIR_DB_MAX_OPEN_CONNS=50
GRIMNIR_DB_MAX_IDLE_CONNS=10
```

### Media Engine Tuning

**GStreamer Buffer Sizes:**

```bash
# Increase buffer for high-bitrate streams
GRIMNIR_MEDIA_ENGINE_BUFFER_SIZE=4096  # KB

# Adjust latency tolerance
GRIMNIR_MEDIA_ENGINE_LATENCY=200  # ms
```

---

## Troubleshooting

### Common Issues

#### 1. Database Connection Errors

**Symptoms:** `connection refused` or `too many connections`

**Solutions:**
```bash
# Check PostgreSQL is running
sudo systemctl status postgresql

# Check connection from application
docker compose exec grimnir nc -zv postgres 5432

# Increase max_connections
sudo vi /etc/postgresql/15/main/postgresql.conf
# Set: max_connections = 200

sudo systemctl restart postgresql
```

#### 2. Scheduler Not Running

**Symptoms:** No schedule entries generated, scheduler_ticks_total not increasing

**Solutions:**
```bash
# Check if leader election is working
curl http://localhost:8080/healthz | jq .leader

# Check Redis connectivity
redis-cli -a your_password PING

# View scheduler logs
docker compose logs grimnir | grep scheduler
```

#### 3. Media Engine Crashes

**Symptoms:** gRPC connection errors, media playback stops

**Solutions:**
```bash
# Check media engine logs
docker compose logs mediaengine

# Verify GStreamer plugins
docker compose exec mediaengine gst-inspect-1.0

# Check resource usage
docker stats mediaengine

# Increase memory limit if needed
# Edit docker-compose.yml, increase memory limit
```

#### 4. High API Latency

**Symptoms:** Slow responses, p99 > 1s

**Solutions:**
```sql
-- Check slow queries
SELECT query, mean_exec_time, calls
FROM pg_stat_statements
ORDER BY mean_exec_time DESC
LIMIT 10;

-- Add missing indexes
-- Increase database resources
-- Enable query caching
```

#### 5. WebSocket Disconnections

**Symptoms:** Frequent reconnections, event loss

**Solutions:**
```nginx
# Increase proxy timeouts in nginx
proxy_read_timeout 3600s;
proxy_send_timeout 3600s;

# Check for idle timeout
# Implement client-side heartbeat
```

#### 6. ResponseController Flush Errors

**Symptoms:** Debug logs showing `ResponseController flush failed error="feature not supported"`

**Cause:** The reverse proxy doesn't support HTTP/2 ResponseController flushing. This happens when nginx or another proxy buffers responses instead of streaming them directly.

**Solutions:**

1. **Disable proxy buffering for streams** (recommended):
```nginx
location ~ ^/(live|stream)/ {
    proxy_pass http://grimnir_backend;

    # Critical: disable all buffering for streaming
    proxy_buffering off;
    proxy_cache off;
    proxy_request_buffering off;

    # Long timeout for stream connections
    proxy_read_timeout 24h;
    proxy_send_timeout 24h;

    # Enable chunked transfer
    chunked_transfer_encoding on;
}
```

2. **For Traefik**, add middleware:
```yaml
http:
  middlewares:
    streaming:
      buffering:
        maxRequestBodyBytes: 0
        maxResponseBodyBytes: 0
        retryExpression: "false"
```

3. **For Caddy**:
```
reverse_proxy localhost:8080 {
    flush_interval -1
}
```

The error is logged once per connection and doesn't affect functionality - it's a debug message indicating the fallback flush mechanism is being used.

---

## Maintenance

### Regular Tasks

**Daily:**
- Monitor error rates
- Check disk space
- Review slow query log

**Weekly:**
- Review security updates
- Check backup success
- Analyze performance trends

**Monthly:**
- Rotate logs
- Update dependencies
- Test disaster recovery

**Quarterly:**
- Security audit
- Capacity planning
- Performance benchmarking

### Updates and Upgrades

**Minor Version Update (0.0.1 → 0.0.2):**

```bash
# 1. Backup database
/usr/local/bin/backup-grimnir-db.sh

# 2. Pull new images
docker compose pull

# 3. Restart services
docker compose up -d

# 4. Verify health
curl http://localhost:8080/healthz
```

**Major Version Update (0.x → 1.0):**

```bash
# 1. Read release notes and migration guide
# 2. Test in staging environment
# 3. Backup database
# 4. Schedule maintenance window
# 5. Stop application
docker compose stop grimnir

# 6. Run database migrations
docker compose run grimnir migrate

# 7. Start application
docker compose start grimnir

# 8. Monitor for 24 hours
# 9. Document any issues
```

### Database Maintenance

```sql
-- Vacuum and analyze (run weekly)
VACUUM ANALYZE;

-- Reindex (run monthly)
REINDEX DATABASE grimnir;

-- Update statistics
ANALYZE;

-- Check for bloat
SELECT schemaname, tablename,
       pg_size_pretty(pg_total_relation_size(schemaname||'.'||tablename)) AS size
FROM pg_tables
WHERE schemaname = 'public'
ORDER BY pg_total_relation_size(schemaname||'.'||tablename) DESC;
```

---

## Scaling Guide

### Horizontal Scaling

**Add API Instances:**
```bash
# Kubernetes
kubectl scale deployment grimnir-radio -n grimnir-radio --replicas=5

# Docker Compose (requires Docker Swarm)
docker service scale grimnir_grimnir=5
```

**Load Balancer Configuration:**
- Use round-robin or least-connections algorithm
- Enable sticky sessions for WebSocket connections
- Health check interval: 5-10 seconds

### Vertical Scaling

**Increase Resources:**
```yaml
# Kubernetes
resources:
  requests:
    memory: "4Gi"
    cpu: "2000m"
  limits:
    memory: "8Gi"
    cpu: "4000m"
```

### Database Scaling

**Read Replicas:**
```bash
# Configure read-only connection string
export GRIMNIR_DB_READ_DSN="host=postgres-replica ..."

# Direct read-only queries to replica
# (requires code changes)
```

**Connection Pooling:**
```bash
# Deploy PgBouncer
docker run -d \
  -p 6432:6432 \
  -v /etc/pgbouncer:/etc/pgbouncer \
  edoburu/pgbouncer
```

---

## Support and Resources

- **Documentation:** https://github.com/friendsincode/grimnir_radio/tree/main/docs
- **Issue Tracker:** https://github.com/friendsincode/grimnir_radio/issues
- **Community:** https://discord.gg/grimnir-radio (if exists)
- **Commercial Support:** Contact hello@example.com

---

**Document Version:** 1.0
**Last Updated:** 2026-01-22
**License:** MIT
