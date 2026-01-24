# Grimnir Radio - Docker Quick Start Guide

Interactive deployment script with intelligent port detection.

## Features

### Automatic Port Detection
The script automatically detects which ports are in use and suggests available alternatives.

**Example:**
```
Port Usage Check
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

ℹ Checking default ports for conflicts...

✓ HTTP API (port 8080) - Available
⚠ Metrics (port 9000) - IN USE, will suggest alternative
✓ gRPC (port 9091) - Available
⚠ Icecast (port 8000) - IN USE, will suggest alternative
✓ PostgreSQL (port 5432) - Available
✓ Redis (port 6379) - Available

ℹ Found 2 port conflict(s). Alternative ports will be suggested.
```

### Smart Suggestions
When a port is in use, the script finds the next available port:

```
Port Configuration
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

⚠ Port 9000 (Prometheus metrics) is in use
ℹ Suggesting available port: 9001

Prometheus metrics port (default: 9001): [press Enter]
```

### Three Deployment Modes

#### 1. Quick Start (Development)
```bash
./scripts/docker-quick-start.sh

# Select: 1) Quick Start
# - Auto-detects available ports
# - Uses local storage (./media-data, ./postgres-data, etc.)
# - Generates secure passwords
# - Single API instance
```

**Quick Start Output:**
```
Quick Start Mode
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

ℹ Using default settings with automatic port detection...

⚠ Port 8000 (Icecast streaming) is in use
ℹ Suggesting available port: 8001

✓ Port allocation complete
ℹ   HTTP API:      8080
ℹ   Metrics:       9000
ℹ   Media Engine:  9091
ℹ   Icecast:       8001  ← Auto-adjusted
ℹ   PostgreSQL:    5432
ℹ   Redis:         6379
```

#### 2. Custom Configuration
```bash
./scripts/docker-quick-start.sh

# Select: 2) Custom Configuration
# - Configure all ports interactively
# - Configure all storage paths
# - Choose external vs local databases
# - Enable multi-instance if desired
```

**Example Flow:**
```
Port Configuration
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

HTTP API port (default: 8080): 3000
Prometheus metrics port (default: 9000): [Enter]
Media Engine gRPC port (default: 9091): [Enter]
Icecast streaming port (default: 8000): [Enter]

Volume Configuration
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Media storage path (default: /path/to/grimnir_radio/media-data): /mnt/storage/media
PostgreSQL data path (default: /path/to/grimnir_radio/postgres-data): [Enter]
...

Use external PostgreSQL database? [y/N]: n
Use external Redis? [y/N]: n
Enable multi-instance deployment? [y/N]: y

Multi-Instance Configuration
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Number of API instances (2-10) (default: 3): 5
ℹ Will deploy 5 API instances with leader election
```

#### 3. Production Mode
```bash
./scripts/docker-quick-start.sh

# Select: 3) Production
# - Designed for real deployments
# - Prompts for NAS/SAN storage mounts
# - External database recommended by default
# - Multi-instance enabled by default
# - Validates storage paths are writable
```

**Production Example:**
```
Production Configuration
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

ℹ This mode is designed for production deployments with:
ℹ   - External storage mounts (NAS/SAN)
ℹ   - Custom port mappings
ℹ   - Optional external databases
ℹ   - Multi-instance support

Production Storage Configuration
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

ℹ Enter absolute paths to your storage mounts
⚠ For NAS/SAN mounts, ensure they are mounted before starting

Media storage path (for audio files) (default: /mnt/storage/grimnir/media): /mnt/nas/radio/media
✓ Created /mnt/nas/radio/media

Icecast logs path (default: /var/log/grimnir/icecast): /var/log/grimnir/icecast
✓ Created /var/log/grimnir/icecast

Use external PostgreSQL database? [Y/n]: y

External PostgreSQL Configuration
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

PostgreSQL host (default: postgres.example.com): db.prod.internal
PostgreSQL port (default: 5432): 5432
PostgreSQL username (default: grimnir): grimnir_prod
PostgreSQL database (default: grimnir): grimnir_production
PostgreSQL password (default: ***): [enter password]

Use external Redis? [y/N]: n

Redis data path (default: /var/lib/grimnir/redis): /mnt/nas/radio/redis
✓ Created /mnt/nas/radio/redis

Enable multi-instance deployment for HA? [Y/n]: y

Number of API instances (2-10) (default: 3): 3
```

## Configuration Persistence

The script saves your configuration for reuse:

```bash
# First run
./scripts/docker-quick-start.sh
# ... interactive configuration ...

# Second run
./scripts/docker-quick-start.sh

Found saved configuration
Load previous configuration? [Y/n]: y
✓ Loaded configuration from .docker-deploy/deployment.conf

# Skips all prompts, uses saved settings
```

**Saved Configuration Location:**
- File: `.docker-deploy/deployment.conf`
- Permissions: `600` (secure, passwords included)
- Contents: All settings including ports, paths, passwords

## Port Conflict Resolution

### Scenario 1: Single Conflict
```
⚠ Port 8080 (HTTP API) is in use
ℹ Suggesting available port: 8081

HTTP API port (default: 8081): [Enter]
✓ Using port 8081
```

### Scenario 2: User Enters Conflicting Port
```
HTTP API port (default: 8080): 3000
⚠ Port 3000 (HTTP API) is already in use
ℹ Try port: 3001

HTTP API port (default: 3001): [Enter]
```

### Scenario 3: Multiple Conflicts
```
⚠ Port 8000 (Icecast streaming) is in use
ℹ Suggesting available port: 8001

⚠ Port 9000 (Prometheus metrics) is in use
ℹ Suggesting available port: 9001

⚠ Port 5432 (PostgreSQL) is in use
ℹ Suggesting available port: 5433
```

## Deployment Summary

After deployment, you'll see which ports were changed:

```
Deployment Complete!
╔════════════════════════════════════════════════════════╗
║                                                        ║
║         Grimnir Radio is now running!                  ║
║                                                        ║
╚════════════════════════════════════════════════════════╝

NOTE: Some ports were changed from defaults due to conflicts:
  HTTP API: 8080 → 8081
  Icecast: 8000 → 8001
  PostgreSQL: 5432 → 5433

Service URLs:
  API:           http://localhost:8081
  Metrics:       http://localhost:9000/metrics
  Icecast:       http://localhost:8001
  Icecast Admin: http://localhost:8001/admin

Additional API Instances:
  Instance 2:    http://localhost:8082
  Instance 3:    http://localhost:8083

Credentials:
  Icecast Admin: admin / x7Kp9mN3qL8vR2wT5yU6

Storage Locations:
  Media:         /mnt/nas/radio/media
  PostgreSQL:    /var/lib/grimnir/postgres
  Redis:         /mnt/nas/radio/redis
  Icecast Logs:  /var/log/grimnir/icecast

Configuration Files:
  Environment:   .env
  Override:      docker-compose.override.yml
  Saved Config:  .docker-deploy/deployment.conf
```

## Multi-Instance Deployment

When you enable multi-instance, the script automatically:

1. **Creates additional containers:**
   - `grimnir-radio-1` (port 8080)
   - `grimnir-radio-2` (port 8081)
   - `grimnir-radio-3` (port 8082)

2. **Configures leader election:**
   - All instances connect to shared Redis
   - Leader election enabled automatically
   - Scheduler runs only on leader

3. **Shares storage:**
   - All instances mount same media volume
   - Consistent database access
   - Coordinated via Redis event bus

4. **Port allocation:**
   - HTTP ports: sequential (8080, 8081, 8082, ...)
   - Metrics ports: sequential (9000, 9001, 9002, ...)
   - Automatically checks each port for conflicts

## External Database Configuration

### PostgreSQL (RDS, Cloud SQL, etc.)
```
Use external PostgreSQL database? [Y/n]: y

PostgreSQL host: db-prod.us-east-1.rds.amazonaws.com
PostgreSQL port: 5432
PostgreSQL username: grimnir_admin
PostgreSQL database: grimnir_production
PostgreSQL password: [secure password]
```

**Result:**
- Local PostgreSQL container disabled
- Connection string generated: `host=db-prod.us-east-1.rds.amazonaws.com port=5432 user=grimnir_admin password=*** dbname=grimnir_production sslmode=require`
- TLS/SSL enabled by default

### Redis (ElastiCache, MemoryStore, etc.)
```
Use external Redis? [y/N]: y

Redis host: redis-cluster.cache.amazonaws.com
Redis port: 6379
Redis password: [secure password]
```

**Result:**
- Local Redis container disabled
- Connection configured: `redis-cluster.cache.amazonaws.com:6379`

## Storage Validation

Production mode validates storage paths:

```
Media storage path (default: /mnt/storage/grimnir/media): /mnt/nas/radio

⚠ Media storage path does not exist: /mnt/nas/radio
Create directory /mnt/nas/radio? [Y/n]: y
✓ Created /mnt/nas/radio

# Or if path exists but not writable:
✗ Media storage path is not writable: /mnt/nas/radio
ℹ Try: sudo chown -R $(whoami): /mnt/nas/radio
```

## Command-Line Options

```bash
# Normal deployment (interactive)
./scripts/docker-quick-start.sh

# Stop all services
./scripts/docker-quick-start.sh --stop

# Clean everything (removes all data)
./scripts/docker-quick-start.sh --clean
```

**Clean Mode Safety:**
```
Clean Deployment
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

⚠ This will:
⚠   - Stop all services
⚠   - Remove all containers and volumes
⚠   - Delete configuration files
⚠   - PERMANENTLY DELETE ALL DATA

Are you absolutely sure? [y/N]: y

Type 'yes' to confirm data deletion [y/N]: yes

ℹ Stopping and removing containers...
✓ Cleanup complete
```

## Files Generated

After running the script, you'll have:

```
.
├── .env                           # Environment variables
├── docker-compose.override.yml    # Port mappings, volumes, multi-instance
├── .docker-deploy/
│   └── deployment.conf            # Saved configuration (chmod 600)
├── media-data/                    # Media files (or custom path)
├── postgres-data/                 # Database (or custom path)
├── redis-data/                    # Redis persistence (or custom path)
└── icecast-logs/                  # Icecast logs (or custom path)
```

## Production Deployment Checklist

Before deploying to production with this script:

- [ ] Mount NAS/SAN storage to server
- [ ] Verify storage mounts are writable
- [ ] Create external database (RDS, Cloud SQL) if using
- [ ] Create external Redis (ElastiCache) if using
- [ ] Choose custom ports if defaults conflict
- [ ] Enable multi-instance for HA (3+ instances recommended)
- [ ] Save the generated passwords securely
- [ ] Back up `.docker-deploy/deployment.conf`
- [ ] Configure firewall rules for chosen ports
- [ ] Set up monitoring for all instances
- [ ] Configure load balancer (nginx/haproxy) for multi-instance
- [ ] Test failover with `docker stop grimnir-radio-1`

## Real-World Example: App Server Deployment

**Scenario:**
- Ubuntu 22.04 app server
- NAS mounted at `/mnt/storage`
- Port 8080 used by nginx
- Port 8000 used by existing service
- Need 3 API instances for HA
- Using managed PostgreSQL (RDS)

**Script Run:**
```bash
./scripts/docker-quick-start.sh

# Selections:
Mode: 3 (Production)

Ports:
  HTTP API: 3000 (8080 conflicted)
  Metrics: 9000
  gRPC: 9091
  Icecast: 8080 (8000 conflicted)
  Redis: 6379

Storage:
  Media: /mnt/storage/grimnir/media
  Redis: /mnt/storage/grimnir/redis
  Icecast logs: /var/log/grimnir/icecast

External PostgreSQL: Yes
  Host: grimnir-db.us-east-1.rds.amazonaws.com
  Port: 5432
  User: grimnir_prod
  Database: grimnir

Multi-instance: Yes
  Instances: 3
```

**Result:**
- API instances on ports: 3000, 3001, 3002
- Metrics on ports: 9000, 9001, 9002
- Icecast on port: 8080
- Redis on port: 6379
- No PostgreSQL container (using RDS)
- All media on NAS: `/mnt/storage/grimnir/media`

## Troubleshooting

### Port Still in Use After Suggestion
```bash
# The script scans for available ports, but something might grab it between scan and deployment
# Solution: Run again, the script will find the next available port
./scripts/docker-quick-start.sh
```

### Storage Path Not Writable
```bash
# Fix permissions
sudo chown -R $(whoami): /mnt/storage/grimnir

# Or run deployment with sudo (not recommended for security)
```

### Want to Change Configuration
```bash
# Remove saved config and run again
rm -rf .docker-deploy
./scripts/docker-quick-start.sh
```

---

**For full Docker documentation, see:** [Docker Deployment](Docker-Deployment)
