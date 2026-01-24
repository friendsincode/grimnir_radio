# Grimnir Radio - Observability Guide

**Version:** 1.0
**Last Updated:** 2026-01-22

This guide covers monitoring, metrics, tracing, and debugging for Grimnir Radio in production environments.

---

## Table of Contents

- [Overview](#overview)
- [Prometheus Metrics](#prometheus-metrics)
- [Distributed Tracing](#distributed-tracing)
- [Monitoring Dashboard](#monitoring-dashboard)
- [Alerting](#alerting)
- [Troubleshooting](#troubleshooting)

---

## Overview

Grimnir Radio provides comprehensive observability through:

1. **Prometheus Metrics** - Real-time performance and health metrics
2. **OpenTelemetry Tracing** - End-to-end request tracing
3. **Structured Logging** - JSON logs with context and correlation IDs
4. **Event Bus** - Real-time event streaming for monitoring

### Observability Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                     Grimnir Radio Instances                      │
│  [API] → [Scheduler] → [Executor] → [Media Engine]             │
│    ↓         ↓            ↓              ↓                       │
│  Metrics  Metrics      Metrics        Metrics                   │
│  Traces   Traces       Traces         Traces                    │
│  Logs     Logs         Logs           Logs                      │
└────┬──────────┬──────────┬─────────────┬─────────────────────────┘
     │          │          │             │
     ▼          ▼          ▼             ▼
┌────────────────────────────────────────────────────────────────┐
│                    Observability Stack                          │
│                                                                 │
│  ┌─────────────┐  ┌──────────────┐  ┌────────────────┐        │
│  │ Prometheus  │  │ Jaeger/Tempo │  │  Loki/ELK      │        │
│  │  (Metrics)  │  │   (Traces)   │  │   (Logs)       │        │
│  └──────┬──────┘  └──────┬───────┘  └─────┬──────────┘        │
│         │                │                 │                   │
│         └────────────────┴─────────────────┘                   │
│                          │                                      │
│                    ┌─────▼──────┐                               │
│                    │  Grafana   │                               │
│                    │ (Dashboard)│                               │
│                    └────────────┘                               │
└─────────────────────────────────────────────────────────────────┘
```

---

## Prometheus Metrics

### Metrics Endpoint

All Grimnir Radio instances expose Prometheus metrics at:

```
GET http://localhost:8080/metrics
```

### Metric Categories

#### 1. Scheduler Metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `grimnir_schedule_build_duration_seconds` | Histogram | `station_id` | Time to generate schedule |
| `grimnir_schedule_entries_total` | Gauge | `station_id` | Number of schedule entries |
| `grimnir_smart_block_materialize_duration_seconds` | Histogram | `station_id`, `smart_block_id` | Smart block generation time |
| `grimnir_scheduler_ticks_total` | Counter | - | Total scheduler ticks |
| `grimnir_scheduler_errors_total` | Counter | `station_id`, `error_type` | Scheduler errors |

**Example Query:**
```promql
# Average schedule build time over 5 minutes
rate(grimnir_schedule_build_duration_seconds_sum[5m]) /
rate(grimnir_schedule_build_duration_seconds_count[5m])
```

#### 2. Executor Metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `grimnir_executor_state` | Gauge | `station_id`, `executor_id` | Current state (0-5) |
| `grimnir_playout_buffer_depth_samples` | Gauge | `station_id`, `mount_id` | Buffer depth in samples |
| `grimnir_playout_dropout_count_total` | Counter | `station_id`, `mount_id` | Underrun count |
| `grimnir_playout_cpu_usage_percent` | Gauge | `station_id`, `mount_id` | CPU usage |
| `grimnir_executor_state_transitions_total` | Counter | `station_id`, `from_state`, `to_state` | State changes |
| `grimnir_executor_priority_changes_total` | Counter | `station_id`, `from_priority`, `to_priority` | Priority changes |

**Executor States:**
- `0` = Idle
- `1` = Preloading
- `2` = Playing
- `3` = Fading
- `4` = Live
- `5` = Emergency

**Example Query:**
```promql
# Dropout rate per minute
rate(grimnir_playout_dropout_count_total[1m]) * 60
```

#### 3. Media Engine Metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `grimnir_media_engine_loudness_lufs` | Gauge | `station_id`, `mount_id` | Current LUFS level |
| `grimnir_media_engine_output_health` | Gauge | `station_id`, `mount_id`, `output_type` | Output status (0/1) |
| `grimnir_media_engine_connection_status` | Gauge | `executor_id` | gRPC connection status |
| `grimnir_media_engine_pipeline_restarts_total` | Counter | `station_id`, `mount_id`, `reason` | Pipeline restart count |
| `grimnir_media_engine_audio_level_left_db` | Gauge | `station_id`, `mount_id` | Left channel level (dB) |
| `grimnir_media_engine_audio_level_right_db` | Gauge | `station_id`, `mount_id` | Right channel level (dB) |
| `grimnir_media_engine_operations_total` | Counter | `station_id`, `mount_id`, `operation`, `status` | Operation counts |
| `grimnir_media_engine_operation_duration_seconds` | Histogram | `station_id`, `mount_id`, `operation` | Operation latency |
| `grimnir_media_engine_playback_state` | Gauge | `station_id`, `mount_id` | Playback state (0-6) |
| `grimnir_media_engine_active_pipelines` | Gauge | `station_id`, `mount_id` | Active pipeline count |

**Example Query:**
```promql
# Average loudness across all stations
avg(grimnir_media_engine_loudness_lufs)
```

#### 4. API Metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `grimnir_api_request_duration_seconds` | Histogram | `method`, `endpoint`, `status_code` | Request latency |
| `grimnir_api_requests_total` | Counter | `method`, `endpoint`, `status_code` | Request count |
| `grimnir_api_active_connections` | Gauge | - | Active HTTP connections |
| `grimnir_api_websocket_connections` | Gauge | - | Active WebSocket connections |

**Example Query:**
```promql
# 95th percentile API latency
histogram_quantile(0.95,
  rate(grimnir_api_request_duration_seconds_bucket[5m]))
```

#### 5. Live & Webstream Metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `grimnir_live_sessions_active` | Gauge | `station_id` | Active DJ sessions |
| `grimnir_live_session_duration_seconds` | Histogram | `station_id`, `user_id` | Session duration |
| `grimnir_webstream_health_status` | Gauge | `webstream_id`, `station_id` | Health status (0-2) |
| `grimnir_webstream_failovers_total` | Counter | `webstream_id`, `station_id`, `from_url`, `to_url` | Failover count |
| `grimnir_webstream_health_checks_total` | Counter | `webstream_id`, `status` | Health check count |

**Example Query:**
```promql
# Webstream failover rate
rate(grimnir_webstream_failovers_total[5m])
```

#### 6. Database Metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `grimnir_database_query_duration_seconds` | Histogram | `operation`, `table` | Query latency |
| `grimnir_database_connections_active` | Gauge | - | Active connections |
| `grimnir_database_errors_total` | Counter | `operation`, `error_type` | Database errors |

**Example Query:**
```promql
# Slow database queries (>100ms)
histogram_quantile(0.95,
  rate(grimnir_database_query_duration_seconds_bucket[5m])) > 0.1
```

#### 7. Leader Election Metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `grimnir_leader_election_status` | Gauge | `instance_id` | Leadership status (0/1) |
| `grimnir_leader_election_changes_total` | Counter | `instance_id`, `event` | Leadership changes |

**Example Query:**
```promql
# Current leader
grimnir_leader_election_status == 1
```

---

## Distributed Tracing

### OpenTelemetry Integration

Grimnir Radio uses OpenTelemetry for distributed tracing across all components.

### Configuration

Set these environment variables:

```bash
# Enable tracing
GRIMNIR_TRACING_ENABLED=true

# OTLP endpoint (Jaeger, Tempo, etc.)
GRIMNIR_OTLP_ENDPOINT=localhost:4317

# Sample rate (0.0 to 1.0)
GRIMNIR_TRACING_SAMPLE_RATE=1.0  # 100% sampling for development
GRIMNIR_TRACING_SAMPLE_RATE=0.1  # 10% sampling for production
```

### Trace Propagation

Traces propagate through:

1. **HTTP API** → **Scheduler** → **Executor** → **Media Engine**
2. **HTTP API** → **Database** queries
3. **HTTP API** → **Event Bus** events

### Viewing Traces

#### With Jaeger

```bash
# Start Jaeger all-in-one
docker run -d --name jaeger \
  -p 16686:16686 \
  -p 4317:4317 \
  jaegertracing/all-in-one:latest

# Open Jaeger UI
open http://localhost:16686
```

#### With Grafana Tempo

```yaml
# tempo.yaml
server:
  http_listen_port: 3200

distributor:
  receivers:
    otlp:
      protocols:
        grpc:
          endpoint: 0.0.0.0:4317

storage:
  trace:
    backend: local
    local:
      path: /tmp/tempo/blocks
```

```bash
# Start Tempo
docker run -d --name tempo \
  -p 3200:3200 \
  -p 4317:4317 \
  -v $(pwd)/tempo.yaml:/etc/tempo/tempo.yaml \
  grafana/tempo:latest \
  -config.file=/etc/tempo/tempo.yaml

# Query via Grafana
```

### Trace Examples

#### Schedule Generation Trace

```
HTTP POST /api/v1/schedule/generate
├─ scheduler.scheduleStation (5ms)
│  ├─ database.query: clocks (2ms)
│  ├─ smartblock.materialize (3ms)
│  │  └─ database.query: media_items (2ms)
│  └─ database.insert: schedule_entries (1ms)
└─ executor.notifyScheduleUpdate (1ms)
```

#### Media Playback Trace

```
executor.playTrack
├─ database.query: media_item (2ms)
├─ media_engine.Play (gRPC) (50ms)
│  ├─ pipeline.loadGraph (10ms)
│  ├─ pipeline.startPlayback (40ms)
│  └─ telemetry.startStream (1ms)
└─ events.publish: now_playing (1ms)
```

---

## Monitoring Dashboard

### Grafana Setup

```bash
# Start Grafana
docker run -d --name grafana \
  -p 3000:3000 \
  grafana/grafana:latest

# Add Prometheus data source
# URL: http://prometheus:9090
```

### Recommended Dashboards

#### 1. System Overview Dashboard

**Panels:**

- **Request Rate** - `rate(grimnir_api_requests_total[1m])`
- **Error Rate** - `rate(grimnir_api_requests_total{status_code=~"5.."}[1m])`
- **P95 Latency** - `histogram_quantile(0.95, rate(grimnir_api_request_duration_seconds_bucket[5m]))`
- **Active Connections** - `grimnir_api_active_connections`
- **WebSocket Connections** - `grimnir_api_websocket_connections`

#### 2. Scheduler Dashboard

**Panels:**

- **Schedule Build Time** - `rate(grimnir_schedule_build_duration_seconds_sum[5m]) / rate(grimnir_schedule_build_duration_seconds_count[5m])`
- **Schedule Entries** - `sum(grimnir_schedule_entries_total)`
- **Scheduler Ticks** - `rate(grimnir_scheduler_ticks_total[1m])`
- **Scheduler Errors** - `rate(grimnir_scheduler_errors_total[1m])`

#### 3. Executor Dashboard

**Panels:**

- **Executor States** (gauge) - `grimnir_executor_state`
- **Buffer Depth** - `grimnir_playout_buffer_depth_samples`
- **Dropout Rate** - `rate(grimnir_playout_dropout_count_total[1m])`
- **State Transitions** - `rate(grimnir_executor_state_transitions_total[1m])`
- **Priority Changes** - `rate(grimnir_executor_priority_changes_total[1m])`

#### 4. Media Engine Dashboard

**Panels:**

- **Loudness Levels** - `grimnir_media_engine_loudness_lufs`
- **Audio Levels L/R** - `grimnir_media_engine_audio_level_left_db`, `grimnir_media_engine_audio_level_right_db`
- **Pipeline Restarts** - `rate(grimnir_media_engine_pipeline_restarts_total[5m])`
- **Operation Latency** - `rate(grimnir_media_engine_operation_duration_seconds_sum[5m]) / rate(grimnir_media_engine_operation_duration_seconds_count[5m])`
- **Connection Status** - `grimnir_media_engine_connection_status`

#### 5. Database Dashboard

**Panels:**

- **Query Latency** - `histogram_quantile(0.95, rate(grimnir_database_query_duration_seconds_bucket[5m]))`
- **Active Connections** - `grimnir_database_connections_active`
- **Query Rate** - `rate(grimnir_database_query_duration_seconds_count[1m])`
- **Error Rate** - `rate(grimnir_database_errors_total[1m])`

---

## Alerting

### Prometheus AlertManager Rules

```yaml
# /etc/prometheus/rules/grimnir.yml
groups:
  - name: grimnir_critical
    interval: 30s
    rules:
      - alert: MediaEngineDown
        expr: grimnir_media_engine_connection_status == 0
        for: 1m
        labels:
          severity: critical
        annotations:
          summary: "Media engine disconnected for {{ $labels.executor_id }}"
          description: "Media engine gRPC connection down for 1 minute"

      - alert: HighDropoutRate
        expr: rate(grimnir_playout_dropout_count_total[5m]) > 0.1
        for: 2m
        labels:
          severity: critical
        annotations:
          summary: "High dropout rate on {{ $labels.station_id }}"
          description: "Dropout rate: {{ $value | humanize }} per second"

      - alert: ScheduleGap
        expr: grimnir_schedule_entries_total < 10
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "Low schedule entries for {{ $labels.station_id }}"
          description: "Only {{ $value }} schedule entries remaining"

      - alert: HighAPILatency
        expr: histogram_quantile(0.95, rate(grimnir_api_request_duration_seconds_bucket[5m])) > 1
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "High API latency"
          description: "P95 latency: {{ $value }}s (threshold: 1s)"

      - alert: LeaderElectionFailure
        expr: sum(grimnir_leader_election_status) != 1
        for: 1m
        labels:
          severity: critical
        annotations:
          summary: "Leader election problem"
          description: "{{ $value }} leaders elected (expected 1)"

      - alert: DatabaseConnectionPoolExhausted
        expr: grimnir_database_connections_active > 45
        for: 2m
        labels:
          severity: warning
        annotations:
          summary: "Database connection pool nearly full"
          description: "{{ $value }}/50 connections used"

  - name: grimnir_performance
    interval: 1m
    rules:
      - alert: SlowDatabaseQueries
        expr: histogram_quantile(0.95, rate(grimnir_database_query_duration_seconds_bucket[5m])) > 0.1
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "Slow database queries"
          description: "P95 query time: {{ $value }}s"

      - alert: WebstreamFailover
        expr: rate(grimnir_webstream_failovers_total[5m]) > 0
        for: 1m
        labels:
          severity: warning
        annotations:
          summary: "Webstream failover for {{ $labels.webstream_id }}"
          description: "Failover from {{ $labels.from_url }} to {{ $labels.to_url }}"
```

### AlertManager Configuration

```yaml
# /etc/alertmanager/alertmanager.yml
global:
  resolve_timeout: 5m

route:
  group_by: ['alertname', 'station_id']
  group_wait: 10s
  group_interval: 10s
  repeat_interval: 12h
  receiver: 'grimnir-team'

receivers:
  - name: 'grimnir-team'
    email_configs:
      - to: 'ops@example.com'
        from: 'alertmanager@example.com'
        smarthost: 'smtp.example.com:587'
    slack_configs:
      - api_url: 'https://hooks.slack.com/services/YOUR/SLACK/WEBHOOK'
        channel: '#grimnir-alerts'
        title: 'Grimnir Radio Alert'
        text: '{{ range .Alerts }}{{ .Annotations.description }}{{ end }}'
```

---

## Troubleshooting

### High CPU Usage

**Check:**
```promql
rate(process_cpu_seconds_total[1m]) * 100
```

**Common Causes:**
- Too many concurrent schedule builds
- Media engine pipeline overload
- Database query performance issues

**Solutions:**
1. Reduce scheduler tick frequency
2. Add database indexes (see DATABASE_OPTIMIZATION.md)
3. Increase media engine resources
4. Enable query caching

### Memory Leaks

**Check:**
```promql
process_resident_memory_bytes
```

**Common Causes:**
- Unclosed database connections
- Event bus subscriber leaks
- GStreamer pipeline cleanup issues

**Solutions:**
1. Review connection pool settings
2. Check event bus subscription cleanup
3. Monitor executor lifecycle
4. Restart affected instances

### Schedule Gaps

**Check:**
```promql
grimnir_schedule_entries_total < 20
```

**Common Causes:**
- Scheduler not running (leader election issue)
- Smart block materialization failures
- Insufficient media library

**Solutions:**
1. Check leader election status
2. Review scheduler logs for errors
3. Verify media library has sufficient items
4. Check clock hour configuration

### Dropouts/Underruns

**Check:**
```promql
rate(grimnir_playout_dropout_count_total[1m])
```

**Common Causes:**
- Network latency to media engine
- Media engine CPU overload
- Disk I/O bottleneck
- Buffer depth too low

**Solutions:**
1. Check media engine system resources
2. Increase buffer size in DSP config
3. Use faster storage (SSD) for media files
4. Reduce concurrent pipeline count

---

## Best Practices

### 1. Metric Retention

```yaml
# prometheus.yml
global:
  scrape_interval: 15s
  evaluation_interval: 15s

storage:
  tsdb:
    retention.time: 30d
    retention.size: 50GB
```

### 2. Log Aggregation

Use structured logging with correlation IDs:

```json
{
  "level": "info",
  "time": "2026-01-22T10:00:00Z",
  "request_id": "abc123",
  "station_id": "station-1",
  "message": "schedule generated",
  "entries": 48
}
```

### 3. Sampling Strategy

**Development:** 100% tracing
```bash
GRIMNIR_TRACING_SAMPLE_RATE=1.0
```

**Production:** 10% tracing
```bash
GRIMNIR_TRACING_SAMPLE_RATE=0.1
```

**High-traffic:** 1% tracing
```bash
GRIMNIR_TRACING_SAMPLE_RATE=0.01
```

### 4. Dashboard Organization

Create separate dashboards for:
- **Operations** - System health and performance
- **Development** - Detailed metrics for debugging
- **Business** - Station uptime, listener stats

---

## Resources

- [Prometheus Documentation](https://prometheus.io/docs/)
- [Grafana Dashboards](https://grafana.com/grafana/dashboards/)
- [OpenTelemetry Go SDK](https://opentelemetry.io/docs/instrumentation/go/)
- [Jaeger Documentation](https://www.jaegertracing.io/docs/)

---

**Version:** 1.0
**Last Updated:** 2026-01-22
