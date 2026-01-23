# Grimnir Radio Deployment

This directory contains deployment configurations and examples for Grimnir Radio observability stack.

## Quick Start

### 1. Start Observability Stack

```bash
cd deploy
docker-compose -f docker-compose.observability.yml up -d
```

This starts:
- **Prometheus** (metrics): http://localhost:9090
- **AlertManager** (alerts): http://localhost:9093
- **Jaeger** (tracing): http://localhost:16686
- **Grafana** (dashboards): http://localhost:3000 (admin/admin)
- **Node Exporter** (system metrics): http://localhost:9100

### 2. Start Grimnir Radio with Observability Enabled

**Terminal 1 - Control Plane:**
```bash
export GRIMNIR_TRACING_ENABLED=true
export GRIMNIR_OTLP_ENDPOINT=localhost:4317
./bin/grimnirradio
```

**Terminal 2 - Media Engine:**
```bash
export MEDIAENGINE_TRACING_ENABLED=true
export MEDIAENGINE_OTLP_ENDPOINT=localhost:4317
./bin/mediaengine
```

### 3. Verify Metrics

```bash
# Control plane metrics
curl http://localhost:8080/metrics

# Media engine metrics
curl http://localhost:9092/metrics

# Check Prometheus targets
curl http://localhost:9090/api/v1/targets | jq
```

### 4. View Dashboards

- **Prometheus**: http://localhost:9090
  - Query metrics
  - View alert rules
  - Check targets

- **Jaeger**: http://localhost:16686
  - View distributed traces
  - Analyze request flows
  - Identify performance bottlenecks

- **Grafana**: http://localhost:3000
  - Pre-configured dashboards (TODO: add dashboard JSON)
  - Custom queries
  - Alert visualization

- **AlertManager**: http://localhost:9093
  - View active alerts
  - Manage silences
  - Configure notification routes

## Directory Structure

```
deploy/
├── prometheus/
│   ├── prometheus.yml      # Prometheus configuration
│   └── alerts.yml          # Alert rules
├── alertmanager/
│   └── alertmanager.yml    # AlertManager configuration
├── grafana/
│   ├── provisioning/       # Grafana datasource/dashboard provisioning
│   └── dashboards/         # Dashboard JSON files
├── systemd/                # Systemd service files
│   ├── grimnirradio.service
│   └── mediaengine.service
└── docker-compose.observability.yml  # Full observability stack
```

## Configuration

### Prometheus

Edit `prometheus/prometheus.yml` to configure:
- Scrape targets (Grimnir Radio instances)
- Scrape intervals
- AlertManager endpoint
- Remote write (for long-term storage)

### AlertManager

Edit `alertmanager/alertmanager.yml` to configure:
- Notification receivers (Slack, PagerDuty, Email)
- Routing rules
- Grouping/inhibition rules
- Notification timing

See [docs/ALERTING.md](../docs/ALERTING.md) for detailed configuration guide.

### Alert Rules

Edit `prometheus/alerts.yml` to:
- Add new alert rules
- Modify thresholds
- Change alert severities
- Customize alert descriptions

### Tracing

Jaeger is configured to accept OTLP traces on:
- gRPC: `localhost:4317`
- HTTP: `localhost:4318`

Configure Grimnir Radio to send traces:
```bash
export GRIMNIR_TRACING_ENABLED=true
export GRIMNIR_OTLP_ENDPOINT=localhost:4317
export GRIMNIR_TRACING_SAMPLE_RATE=1.0  # Sample 100% of traces
```

## Production Deployment

### Systemd Services

Install systemd service files:

```bash
sudo cp systemd/grimnirradio.service /etc/systemd/system/
sudo cp systemd/mediaengine.service /etc/systemd/system/

# Edit service files to set correct paths and environment
sudo systemctl edit grimnirradio
sudo systemctl edit mediaengine

# Enable and start services
sudo systemctl enable --now grimnirradio
sudo systemctl enable --now mediaengine
```

### High Availability

For production deployments:

1. **Prometheus HA**: Deploy multiple Prometheus instances with identical configuration
2. **AlertManager Clustering**: Configure AlertManager cluster for deduplication
3. **Long-term Storage**: Use remote write to Mimir/Thanos/Cortex
4. **Jaeger Backend**: Use Elasticsearch/Cassandra backend instead of all-in-one

Example Prometheus HA config:
```yaml
global:
  external_labels:
    replica: '1'  # Change for each Prometheus instance
    cluster: 'grimnir-radio-production'
```

### Security

Production security checklist:

- [ ] Enable TLS for Prometheus
- [ ] Enable TLS for AlertManager
- [ ] Enable authentication (basic auth, OAuth)
- [ ] Restrict network access with firewall rules
- [ ] Use secrets management for API keys (Vault, AWS Secrets Manager)
- [ ] Enable audit logging
- [ ] Regular security updates

## Troubleshooting

### Prometheus not scraping metrics

```bash
# Check Prometheus targets
curl http://localhost:9090/api/v1/targets

# Verify metrics endpoint is accessible
curl http://localhost:8080/metrics

# Check Prometheus logs
docker logs grimnir-prometheus
```

### Alerts not firing

```bash
# Check alert rules loaded
curl http://localhost:9090/api/v1/rules | jq

# Query metric directly
curl 'http://localhost:9090/api/v1/query?query=grimnir_media_engine_connection_status'

# Check alert evaluation
# Visit http://localhost:9090/alerts
```

### Traces not appearing in Jaeger

```bash
# Check Jaeger health
curl http://localhost:14269/

# Verify OTLP endpoint is accessible
curl http://localhost:4318/v1/traces

# Check Grimnir Radio logs for trace export errors
journalctl -u grimnirradio -f
```

### AlertManager not sending notifications

```bash
# Check AlertManager status
curl http://localhost:9093/api/v1/status

# View active alerts
curl http://localhost:9093/api/v1/alerts

# Check AlertManager logs
docker logs grimnir-alertmanager
```

## Maintenance

### Backup

Backup persistent volumes:

```bash
# Stop services
docker-compose -f docker-compose.observability.yml down

# Backup volumes
docker run --rm -v grimnir-prometheus-data:/data -v $(pwd):/backup alpine \
  tar czf /backup/prometheus-backup-$(date +%Y%m%d).tar.gz -C /data .

docker run --rm -v grimnir-alertmanager-data:/data -v $(pwd):/backup alpine \
  tar czf /backup/alertmanager-backup-$(date +%Y%m%d).tar.gz -C /data .

docker run --rm -v grimnir-grafana-data:/data -v $(pwd):/backup alpine \
  tar czf /backup/grafana-backup-$(date +%Y%m%d).tar.gz -C /data .

# Restart services
docker-compose -f docker-compose.observability.yml up -d
```

### Retention Policy

Configure data retention in `prometheus/prometheus.yml`:

```yaml
global:
  # Default retention is 15 days
  storage.tsdb.retention.time: 30d
  storage.tsdb.retention.size: 50GB
```

For long-term storage, use remote write to dedicated TSDB.

### Updates

Update observability stack:

```bash
# Pull latest images
docker-compose -f docker-compose.observability.yml pull

# Restart with new images
docker-compose -f docker-compose.observability.yml up -d
```

## References

- [Prometheus Documentation](https://prometheus.io/docs/)
- [AlertManager Documentation](https://prometheus.io/docs/alerting/latest/alertmanager/)
- [Jaeger Documentation](https://www.jaegertracing.io/docs/)
- [Grafana Documentation](https://grafana.com/docs/)
- [Grimnir Radio Alerting Guide](../docs/ALERTING.md)
- [Grimnir Radio Metrics Reference](../docs/METRICS.md) (TODO)
