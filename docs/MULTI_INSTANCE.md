# Grimnir Radio Multi-Instance Deployment Guide

This guide covers deploying multiple instances of Grimnir Radio for high availability and horizontal scaling.

## Table of Contents

- [Overview](#overview)
- [Architecture](#architecture)
- [Leader Election](#leader-election)
- [Configuration](#configuration)
- [Deployment](#deployment)
- [Load Balancing](#load-balancing)
- [Monitoring](#monitoring)
- [Troubleshooting](#troubleshooting)

## Overview

Grimnir Radio supports multi-instance deployment with automatic leader election for the scheduler. This enables:

- **High Availability**: Automatic failover if an instance fails
- **Horizontal Scaling**: Multiple API servers handle HTTP traffic
- **Zero Downtime**: Rolling updates without interrupting service
- **Geographic Distribution**: Deploy instances in multiple regions

### Key Components

- **Scheduler Leader**: Only one instance runs the scheduler (elected via Redis)
- **API Servers**: All instances serve HTTP API requests
- **Shared State**: PostgreSQL for persistent data, Redis for leader election
- **Load Balancer**: Distributes HTTP traffic across instances

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                       Load Balancer                         │
│                    (nginx/haproxy)                          │
└──────────────┬──────────────────┬──────────────────┬────────┘
               │                  │                  │
       ┌───────▼────────┐ ┌──────▼───────┐ ┌───────▼────────┐
       │  Instance 1    │ │ Instance 2   │ │  Instance 3    │
       │  (Leader)      │ │ (Follower)   │ │  (Follower)    │
       │  ✓ Scheduler   │ │  ✗ Scheduler │ │  ✗ Scheduler   │
       │  ✓ API         │ │  ✓ API       │ │  ✓ API         │
       └───────┬────────┘ └──────┬───────┘ └───────┬────────┘
               │                  │                  │
               └──────────────────┼──────────────────┘
                                  │
                    ┌─────────────┴──────────────┐
                    │                            │
           ┌────────▼─────────┐        ┌────────▼─────────┐
           │   PostgreSQL     │        │      Redis       │
           │  (Shared State)  │        │ (Leader Election)│
           └──────────────────┘        └──────────────────┘
```

## Leader Election

### How It Works

1. **Startup**: Each instance attempts to acquire leadership by writing its ID to Redis
2. **Lease**: The leader holds a lease (default: 15 seconds) and renews it periodically (default: 5 seconds)
3. **Monitoring**: Followers check every 2 seconds if leadership is available
4. **Failover**: If the leader fails to renew its lease, a follower takes over
5. **Scheduler**: Only the leader instance runs the scheduler loop

### Redis Key Structure

```
grimnir:leader:scheduler = <instance-id>
TTL: 15 seconds
```

### Leader Transition Flow

```
Instance 1 (Leader)          Redis                    Instance 2 (Follower)
      │                        │                              │
      ├─ SET leader=inst1 ────▶│                              │
      │    (TTL: 15s)           │                              │
      │                         │                              │
      ├─ Renew every 5s ───────▶│                              │
      │                         │                              │
      X  [CRASH]                │                              │
                                │                              │
                                │   [TTL expires after 15s]    │
                                │                              │
                                │◀─── Check leadership ────────┤
                                │                              │
                                │◀─── SET leader=inst2 ────────┤
                                │    (TTL: 15s)                │
                                │                              │
                                │                         [Now Leader]
```

## Configuration

### Environment Variables

#### Leader Election

```bash
# Enable leader election (default: false)
export GRIMNIR_LEADER_ELECTION_ENABLED=true

# Redis connection for leader election
export GRIMNIR_REDIS_ADDR=localhost:6379
export GRIMNIR_REDIS_PASSWORD=""
export GRIMNIR_REDIS_DB=0

# Unique instance identifier (auto-generated if not set)
export GRIMNIR_INSTANCE_ID=instance-1
```

#### Database (Shared)

```bash
# All instances must use the same database
export GRIMNIR_DB_BACKEND=postgres
export GRIMNIR_DB_DSN="host=postgres-server port=5432 user=grimnir password=secret dbname=grimnir sslmode=disable"
```

### Configuration File

Create a configuration file for each instance:

**instance-1.env:**
```bash
GRIMNIR_INSTANCE_ID=instance-1
GRIMNIR_HTTP_PORT=8081
GRIMNIR_LEADER_ELECTION_ENABLED=true
GRIMNIR_REDIS_ADDR=redis:6379
GRIMNIR_DB_DSN="host=postgres port=5432 user=grimnir password=secret dbname=grimnir"
```

**instance-2.env:**
```bash
GRIMNIR_INSTANCE_ID=instance-2
GRIMNIR_HTTP_PORT=8082
GRIMNIR_LEADER_ELECTION_ENABLED=true
GRIMNIR_REDIS_ADDR=redis:6379
GRIMNIR_DB_DSN="host=postgres port=5432 user=grimnir password=secret dbname=grimnir"
```

## Deployment

### Docker Compose

```yaml
version: '3.8'

services:
  postgres:
    image: postgres:15
    environment:
      POSTGRES_USER: grimnir
      POSTGRES_PASSWORD: secret
      POSTGRES_DB: grimnir
    volumes:
      - postgres-data:/var/lib/postgresql/data
    ports:
      - "5432:5432"

  redis:
    image: redis:7-alpine
    ports:
      - "6379:6379"
    volumes:
      - redis-data:/data

  grimnir-1:
    image: grimnir-radio:latest
    environment:
      GRIMNIR_INSTANCE_ID: instance-1
      GRIMNIR_HTTP_PORT: 8080
      GRIMNIR_LEADER_ELECTION_ENABLED: "true"
      GRIMNIR_REDIS_ADDR: redis:6379
      GRIMNIR_DB_BACKEND: postgres
      GRIMNIR_DB_DSN: "host=postgres port=5432 user=grimnir password=secret dbname=grimnir sslmode=disable"
      GRIMNIR_JWT_SIGNING_KEY: "your-secret-key"
    ports:
      - "8081:8080"
    depends_on:
      - postgres
      - redis

  grimnir-2:
    image: grimnir-radio:latest
    environment:
      GRIMNIR_INSTANCE_ID: instance-2
      GRIMNIR_HTTP_PORT: 8080
      GRIMNIR_LEADER_ELECTION_ENABLED: "true"
      GRIMNIR_REDIS_ADDR: redis:6379
      GRIMNIR_DB_BACKEND: postgres
      GRIMNIR_DB_DSN: "host=postgres port=5432 user=grimnir password=secret dbname=grimnir sslmode=disable"
      GRIMNIR_JWT_SIGNING_KEY: "your-secret-key"
    ports:
      - "8082:8080"
    depends_on:
      - postgres
      - redis

  grimnir-3:
    image: grimnir-radio:latest
    environment:
      GRIMNIR_INSTANCE_ID: instance-3
      GRIMNIR_HTTP_PORT: 8080
      GRIMNIR_LEADER_ELECTION_ENABLED: "true"
      GRIMNIR_REDIS_ADDR: redis:6379
      GRIMNIR_DB_BACKEND: postgres
      GRIMNIR_DB_DSN: "host=postgres port=5432 user=grimnir password=secret dbname=grimnir sslmode=disable"
      GRIMNIR_JWT_SIGNING_KEY: "your-secret-key"
    ports:
      - "8083:8080"
    depends_on:
      - postgres
      - redis

  nginx:
    image: nginx:alpine
    volumes:
      - ./nginx.conf:/etc/nginx/nginx.conf:ro
    ports:
      - "80:80"
    depends_on:
      - grimnir-1
      - grimnir-2
      - grimnir-3

volumes:
  postgres-data:
  redis-data:
```

### Kubernetes

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: grimnir-radio
spec:
  replicas: 3
  selector:
    matchLabels:
      app: grimnir-radio
  template:
    metadata:
      labels:
        app: grimnir-radio
    spec:
      containers:
      - name: grimnir-radio
        image: grimnir-radio:latest
        ports:
        - containerPort: 8080
        env:
        - name: GRIMNIR_INSTANCE_ID
          valueFrom:
            fieldRef:
              fieldPath: metadata.name
        - name: GRIMNIR_HTTP_PORT
          value: "8080"
        - name: GRIMNIR_LEADER_ELECTION_ENABLED
          value: "true"
        - name: GRIMNIR_REDIS_ADDR
          value: "redis-service:6379"
        - name: GRIMNIR_DB_BACKEND
          value: "postgres"
        - name: GRIMNIR_DB_DSN
          valueFrom:
            secretKeyRef:
              name: grimnir-secrets
              key: database-dsn
        - name: GRIMNIR_JWT_SIGNING_KEY
          valueFrom:
            secretKeyRef:
              name: grimnir-secrets
              key: jwt-signing-key
        readinessProbe:
          httpGet:
            path: /healthz
            port: 8080
          initialDelaySeconds: 5
          periodSeconds: 10
        livenessProbe:
          httpGet:
            path: /healthz
            port: 8080
          initialDelaySeconds: 15
          periodSeconds: 20
---
apiVersion: v1
kind: Service
metadata:
  name: grimnir-radio
spec:
  selector:
    app: grimnir-radio
  ports:
  - port: 80
    targetPort: 8080
  type: LoadBalancer
```

### Systemd (Multiple Instances)

**Service file template: `/etc/systemd/system/grimnir-radio@.service`**

```ini
[Unit]
Description=Grimnir Radio Instance %i
After=network.target postgresql.service redis.service

[Service]
Type=simple
User=grimnir
Group=grimnir
WorkingDirectory=/opt/grimnir-radio
EnvironmentFile=/etc/grimnir-radio/instance-%i.env
ExecStart=/opt/grimnir-radio/bin/grimnirradio
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
```

**Start instances:**
```bash
sudo systemctl enable --now grimnir-radio@1
sudo systemctl enable --now grimnir-radio@2
sudo systemctl enable --now grimnir-radio@3
```

## Load Balancing

### Nginx Configuration

```nginx
upstream grimnir_backend {
    least_conn;

    server 127.0.0.1:8081 max_fails=3 fail_timeout=30s;
    server 127.0.0.1:8082 max_fails=3 fail_timeout=30s;
    server 127.0.0.1:8083 max_fails=3 fail_timeout=30s;

    # Health check
    check interval=5000 rise=2 fall=3 timeout=3000;
}

server {
    listen 80;
    server_name grimnir-radio.local;

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
    }

    location /healthz {
        proxy_pass http://grimnir_backend;
        access_log off;
    }
}
```

### HAProxy Configuration

```haproxy
global
    log /dev/log local0
    maxconn 4096

defaults
    mode http
    timeout connect 5s
    timeout client 50s
    timeout server 50s

frontend grimnir_frontend
    bind *:80
    default_backend grimnir_backend

backend grimnir_backend
    balance roundrobin
    option httpchk GET /healthz
    http-check expect status 200

    server instance1 127.0.0.1:8081 check inter 5s fall 3 rise 2
    server instance2 127.0.0.1:8082 check inter 5s fall 3 rise 2
    server instance3 127.0.0.1:8083 check inter 5s fall 3 rise 2
```

## Monitoring

### Health Check Endpoint

Check instance health and leader status:

```bash
curl http://localhost:8081/healthz
# {"status":"ok","leader":true}

curl http://localhost:8082/healthz
# {"status":"ok","leader":false}
```

### Prometheus Metrics

Monitor leader election status:

```promql
# Current leader (1) or follower (0)
grimnir_leader_election_status{instance_id="instance-1"}

# Leadership changes over time
rate(grimnir_leader_election_changes_total[5m])

# Identify current leader
grimnir_leader_election_status == 1
```

### Grafana Dashboard Queries

**Leader Status Panel:**
```promql
grimnir_leader_election_status
```

**Leadership Changes:**
```promql
increase(grimnir_leader_election_changes_total[1h])
```

**Current Leader:**
```promql
label_replace(
  grimnir_leader_election_status == 1,
  "leader", "$1", "instance_id", "(.*)"
)
```

## Troubleshooting

### Split Brain (Multiple Leaders)

**Symptoms:**
- Multiple instances report `leader:true`
- Duplicate schedule entries in database
- Scheduler ticks metric increases too fast

**Causes:**
- Redis connection issues
- Network partition
- Clock skew between instances

**Resolution:**
```bash
# Check Redis connectivity from all instances
redis-cli -h <redis-host> PING

# Check current leader in Redis
redis-cli -h <redis-host> GET grimnir:leader:scheduler

# Manually clear leadership (emergency only)
redis-cli -h <redis-host> DEL grimnir:leader:scheduler

# Check instance clocks
for host in instance1 instance2 instance3; do
  ssh $host "date -u"
done
```

### No Leader Elected

**Symptoms:**
- All instances report `leader:false`
- Scheduler not running
- No schedule entries being generated

**Causes:**
- Redis unavailable
- All instances can't reach Redis
- Redis key manually deleted

**Resolution:**
```bash
# Check Redis is running
redis-cli -h <redis-host> PING

# Check Redis logs
docker logs redis

# Restart instances to trigger new election
systemctl restart grimnir-radio@1
```

### Leader Flapping

**Symptoms:**
- Leader changes frequently (every few seconds)
- High `grimnir_leader_election_changes_total`
- Scheduler starts and stops repeatedly

**Causes:**
- Network instability
- Redis overloaded
- Instance resource exhaustion

**Resolution:**
```bash
# Check instance resource usage
top
free -h
df -h

# Check Redis latency
redis-cli -h <redis-host> --latency

# Increase lease duration in configuration
# Edit /etc/grimnir-radio/instance-1.env
GRIMNIR_LEADER_LEASE_DURATION=30s  # Increase from 15s
```

### Instance Not Joining Cluster

**Symptoms:**
- Instance starts but never participates in election
- No metrics for instance in Prometheus
- Instance not visible in health checks

**Causes:**
- Leader election disabled
- Wrong Redis address
- Network firewall blocking Redis port

**Resolution:**
```bash
# Check configuration
grep LEADER_ELECTION /etc/grimnir-radio/instance-*.env

# Test Redis connectivity
telnet <redis-host> 6379

# Check firewall rules
iptables -L -n | grep 6379
```

## Best Practices

1. **Use Odd Number of Instances**: 3 or 5 instances for clear majority
2. **Monitor Leader Status**: Alert on frequent leadership changes
3. **Shared Database**: All instances must use same PostgreSQL database
4. **Unique Instance IDs**: Set explicit IDs for easier debugging
5. **Health Check Interval**: Configure load balancer to check health every 5-10 seconds
6. **Graceful Shutdown**: Allow 30 seconds for graceful shutdown during deployments
7. **Redis Persistence**: Enable Redis RDB or AOF for leader election durability
8. **Database Connection Pool**: Adjust pool size based on number of instances
9. **Rolling Updates**: Update one instance at a time, wait for health check
10. **Backup Leadership**: Always run at least 2 instances for automatic failover

## Scaling Considerations

### Horizontal Scaling

Add more instances as traffic grows:
- **2-3 instances**: Suitable for small deployments (< 10 stations)
- **3-5 instances**: Medium deployments (10-50 stations)
- **5-10 instances**: Large deployments (50+ stations)

### Database Scaling

As you add instances, database becomes bottleneck:
- **Connection pooling**: Max connections = (instances × connections_per_instance)
- **Read replicas**: Direct read-only queries to replicas
- **Connection pooler**: Use PgBouncer to reduce connection overhead

### Redis Scaling

For large deployments:
- **Redis Sentinel**: Automatic failover for leader election
- **Redis Cluster**: Not needed for leader election (simple key-value)

## Migration from Single Instance

1. **Add Redis to infrastructure**
2. **Enable leader election on single instance**
3. **Verify single instance still works**
4. **Deploy second instance**
5. **Verify failover works** (stop first instance)
6. **Add load balancer**
7. **Deploy remaining instances**

## References

- [Leader Election Pattern](https://en.wikipedia.org/wiki/Leader_election)
- [Redis SET NX Documentation](https://redis.io/commands/set)
- [CAP Theorem](https://en.wikipedia.org/wiki/CAP_theorem)
- [High Availability Architecture](https://en.wikipedia.org/wiki/High_availability)
