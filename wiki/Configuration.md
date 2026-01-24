# Configuration Guide

Grimnir Radio is configured via environment variables or configuration files.

## Environment Variables

### Core Settings

**HTTP Server:**
```bash
GRIMNIR_HTTP_BIND=0.0.0.0        # Bind address (default: 0.0.0.0)
GRIMNIR_HTTP_PORT=8080            # HTTP port (default: 8080)
```

**Database:**
```bash
GRIMNIR_DB_BACKEND=postgres       # postgres, mysql, or sqlite
GRIMNIR_DB_DSN=postgres://user:pass@host:5432/db?sslmode=disable

# Examples:
# PostgreSQL: postgres://grimnir:password@localhost:5432/grimnir_radio?sslmode=disable
# MySQL:      mysql://grimnir:password@tcp(localhost:3306)/grimnir_radio?parseTime=true
# SQLite:     sqlite:///var/lib/grimnir-radio/grimnir.db
```

**Authentication:**
```bash
GRIMNIR_JWT_SIGNING_KEY=your-secret-key-here  # Required! Use strong random string
```

### Media Storage

**Filesystem (default):**
```bash
GRIMNIR_MEDIA_ROOT=/var/lib/grimnir-radio/media
```

**S3-Compatible:**
```bash
# AWS S3
GRIMNIR_S3_BUCKET=my-media-bucket
GRIMNIR_S3_REGION=us-east-1
AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE
AWS_SECRET_ACCESS_KEY=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY

# MinIO
GRIMNIR_S3_BUCKET=media
GRIMNIR_S3_ENDPOINT=https://minio.example.com
GRIMNIR_S3_ACCESS_KEY_ID=minioadmin
GRIMNIR_S3_SECRET_ACCESS_KEY=minioadmin
GRIMNIR_S3_USE_PATH_STYLE=true

# DigitalOcean Spaces
GRIMNIR_S3_BUCKET=my-space
GRIMNIR_S3_ENDPOINT=https://nyc3.digitaloceanspaces.com
GRIMNIR_S3_REGION=nyc3

# CloudFront CDN
GRIMNIR_S3_PUBLIC_BASE_URL=https://d1234567890.cloudfront.net
```

### Multi-Instance Configuration

**Event Bus - Redis:**
```bash
GRIMNIR_REDIS_ADDR=localhost:6379
GRIMNIR_REDIS_PASSWORD=your-redis-password
GRIMNIR_REDIS_DB=0
```

**Event Bus - NATS:**
```bash
GRIMNIR_NATS_URL=nats://localhost:4222
GRIMNIR_NATS_TOKEN=your-nats-token
```

**Leader Election:**
```bash
GRIMNIR_LEADER_ELECTION_ENABLED=true
GRIMNIR_INSTANCE_ID=instance-1        # Unique per instance
```

### Observability

**Metrics:**
```bash
GRIMNIR_METRICS_BIND=0.0.0.0:9000  # Prometheus metrics endpoint
```

**Tracing:**
```bash
GRIMNIR_TRACING_ENABLED=true
GRIMNIR_OTLP_ENDPOINT=localhost:4317     # OpenTelemetry collector
GRIMNIR_TRACING_SAMPLE_RATE=0.1          # Sample 10% of traces
```

### Scheduler

```bash
GRIMNIR_SCHEDULER_LOOKAHEAD_MINUTES=2880  # 48 hours (default)
```

### Media Engine

```bash
MEDIAENGINE_GRPC_ADDR=localhost:9091   # Media engine gRPC address
MEDIAENGINE_GRPC_BIND=0.0.0.0          # Media engine bind address
MEDIAENGINE_GRPC_PORT=9091             # Media engine gRPC port
```

## Configuration Files

### .env File

Create `.env` in the working directory:

```bash
# Copy from example
cp .env.example .env

# Edit with your settings
nano .env
```

**Example .env:**
```bash
GRIMNIR_HTTP_PORT=8080
GRIMNIR_DB_BACKEND=postgres
GRIMNIR_DB_DSN=postgres://grimnir:password@localhost:5432/grimnir_radio?sslmode=disable
GRIMNIR_JWT_SIGNING_KEY=$(openssl rand -base64 32)
GRIMNIR_MEDIA_ROOT=/var/lib/grimnir-radio/media
GRIMNIR_REDIS_ADDR=localhost:6379
```

### YAML Configuration (Future)

YAML configuration support is planned for version 1.2.0.

## Docker Compose Configuration

See [Docker Deployment](Docker-Deployment) for docker-compose.yml examples.

## Configuration Validation

Grimnir Radio validates configuration on startup. Check logs for errors:

```bash
# Docker
docker-compose logs grimnirradio | grep -i error

# Systemd
journalctl -u grimnir-radio | grep -i error

# Direct
./grimnirradio serve 2>&1 | grep -i error
```

## Security Best Practices

### JWT Signing Key

Generate a strong random key:

```bash
# OpenSSL (recommended)
openssl rand -base64 32

# Or use uuidgen
uuidgen | sha256sum | cut -d' ' -f1
```

**Never** use the default or example keys in production!

### Database Security

```bash
# Use SSL connections
GRIMNIR_DB_DSN=postgres://grimnir:password@host:5432/db?sslmode=require

# Restrict database user permissions
# - Grant only necessary permissions to grimnir user
# - Use separate users for migrations vs runtime
```

### Redis Security

```bash
# Set password
GRIMNIR_REDIS_PASSWORD=strong-password

# Or use TLS
GRIMNIR_REDIS_ADDR=rediss://localhost:6380  # Note: rediss://
```

### S3 Security

```bash
# Use IAM roles (AWS) instead of access keys when possible
# Limit S3 bucket permissions to specific prefix
# Enable encryption at rest
# Use VPC endpoints for private access
```

## Performance Tuning

See [Performance Tuning](Performance-Tuning) and [Database Optimization](Database-Optimization) for detailed tuning guides.

## Environment-Specific Configurations

### Development

```bash
GRIMNIR_ENV=development
GRIMNIR_DB_BACKEND=sqlite
GRIMNIR_DB_DSN=sqlite:///grimnir_dev.db
GRIMNIR_MEDIA_ROOT=./media
GRIMNIR_HTTP_PORT=8080
GRIMNIR_TRACING_ENABLED=false
```

### Staging

```bash
GRIMNIR_ENV=staging
GRIMNIR_DB_BACKEND=postgres
GRIMNIR_DB_DSN=postgres://grimnir:password@staging-db:5432/grimnir_radio?sslmode=require
GRIMNIR_REDIS_ADDR=staging-redis:6379
GRIMNIR_S3_BUCKET=staging-media
GRIMNIR_TRACING_ENABLED=true
GRIMNIR_TRACING_SAMPLE_RATE=1.0
```

### Production

```bash
GRIMNIR_ENV=production
GRIMNIR_DB_BACKEND=postgres
GRIMNIR_DB_DSN=postgres://grimnir:password@prod-db:5432/grimnir_radio?sslmode=require
GRIMNIR_REDIS_ADDR=prod-redis:6379
GRIMNIR_REDIS_PASSWORD=strong-password
GRIMNIR_S3_BUCKET=production-media
GRIMNIR_S3_REGION=us-east-1
GRIMNIR_LEADER_ELECTION_ENABLED=true
GRIMNIR_TRACING_ENABLED=true
GRIMNIR_TRACING_SAMPLE_RATE=0.1
```

## Configuration Reference

For complete reference, see:
- `.env.example` in repository
- [Environment Variables](https://github.com/friendsincode/grimnir_radio/blob/main/.env.example)
- [Engineering Spec](Engineering-Spec) - Technical details

## Troubleshooting Configuration

### Configuration Not Loading

1. Check file location (must be in working directory or specify path)
2. Check file permissions (must be readable)
3. Check for syntax errors (no quotes around values unless needed)

### Database Connection Failed

```bash
# Test connection manually
psql "$GRIMNIR_DB_DSN"

# Or for MySQL
mysql --defaults-file=<(echo "[client]"; echo "url=$GRIMNIR_DB_DSN")
```

### Environment Variables Not Working

```bash
# Verify environment variables are set
printenv | grep GRIMNIR

# Docker: Pass via docker-compose.yml
environment:
  - GRIMNIR_HTTP_PORT=8080

# Or via .env file
env_file:
  - .env
```

## Further Reading

- [Production Deployment](Production-Deployment) - Production configuration
- [Multi-Instance Setup](Multi-Instance) - Cluster configuration
- [Observability](Observability) - Monitoring configuration
- [Troubleshooting](Troubleshooting) - Common configuration issues
