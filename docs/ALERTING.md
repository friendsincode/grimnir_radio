# Grimnir Radio Alerting Guide

This guide covers the Prometheus AlertManager integration for Grimnir Radio, including alert rules, configuration, and best practices.

## Table of Contents

- [Overview](#overview)
- [Quick Start](#quick-start)
- [Alert Rules](#alert-rules)
- [AlertManager Configuration](#alertmanager-configuration)
- [Notification Channels](#notification-channels)
- [Alert Severity Levels](#alert-severity-levels)
- [Customization](#customization)
- [Troubleshooting](#troubleshooting)

## Overview

Grimnir Radio includes comprehensive alerting rules for monitoring system health, performance, and audio quality. The alerting system uses:

- **Prometheus** - Metric collection and alert rule evaluation
- **AlertManager** - Alert routing, grouping, and notification
- **Multiple notification channels** - Slack, PagerDuty, Email, Webhooks

### Architecture

```
┌─────────────────┐     ┌──────────────┐     ┌──────────────────┐
│ Grimnir Radio   │────▶│ Prometheus   │────▶│ AlertManager     │
│ /metrics        │     │ (rules eval) │     │ (routing/notify) │
└─────────────────┘     └──────────────┘     └──────────────────┘
                                                       │
                        ┌──────────────────────────────┼──────────────┐
                        ▼                              ▼              ▼
                  ┌──────────┐                   ┌─────────┐    ┌────────┐
                  │  Slack   │                   │ Email   │    │Webhook │
                  └──────────┘                   └─────────┘    └────────┘
```

## Quick Start

### 1. Install Prometheus and AlertManager

**Docker Compose:**

```yaml
version: '3.8'
services:
  prometheus:
    image: prom/prometheus:latest
    ports:
      - "9090:9090"
    volumes:
      - ./deploy/prometheus/prometheus.yml:/etc/prometheus/prometheus.yml
      - ./deploy/prometheus/alerts.yml:/etc/prometheus/alerts.yml
      - prometheus-data:/prometheus
    command:
      - '--config.file=/etc/prometheus/prometheus.yml'
      - '--storage.tsdb.path=/prometheus'
      - '--web.console.libraries=/usr/share/prometheus/console_libraries'
      - '--web.console.templates=/usr/share/prometheus/consoles'

  alertmanager:
    image: prom/alertmanager:latest
    ports:
      - "9093:9093"
    volumes:
      - ./deploy/alertmanager/alertmanager.yml:/etc/alertmanager/alertmanager.yml
      - alertmanager-data:/alertmanager
    command:
      - '--config.file=/etc/alertmanager/alertmanager.yml'
      - '--storage.path=/alertmanager'

volumes:
  prometheus-data:
  alertmanager-data:
```

**Manual Installation:**

```bash
# Download Prometheus
wget https://github.com/prometheus/prometheus/releases/download/v2.48.0/prometheus-2.48.0.linux-amd64.tar.gz
tar xvf prometheus-2.48.0.linux-amd64.tar.gz
cd prometheus-2.48.0.linux-amd64

# Copy alert rules
cp /path/to/grimnir_radio/deploy/prometheus/alerts.yml .
cp /path/to/grimnir_radio/deploy/prometheus/prometheus.yml .

# Start Prometheus
./prometheus --config.file=prometheus.yml

# Download AlertManager
wget https://github.com/prometheus/alertmanager/releases/download/v0.26.0/alertmanager-0.26.0.linux-amd64.tar.gz
tar xvf alertmanager-0.26.0.linux-amd64.tar.gz
cd alertmanager-0.26.0.linux-amd64

# Copy configuration
cp /path/to/grimnir_radio/deploy/alertmanager/alertmanager.yml .

# Start AlertManager
./alertmanager --config.file=alertmanager.yml
```

### 2. Configure Grimnir Radio

Ensure metrics are exposed:

```bash
# Control plane metrics on :8080/metrics
curl http://localhost:8080/metrics

# Media engine metrics on :9092/metrics
curl http://localhost:9092/metrics
```

### 3. Verify Alert Rules

Check that Prometheus loaded the rules:

```bash
curl http://localhost:9090/api/v1/rules | jq
```

### 4. Test Alerting

Trigger a test alert by stopping the media engine:

```bash
# Stop media engine
pkill mediaengine

# Wait 1 minute, then check alerts in Prometheus UI
# http://localhost:9090/alerts

# Check AlertManager UI
# http://localhost:9093
```

## Alert Rules

### Critical Alerts

| Alert | Condition | For | Description |
|-------|-----------|-----|-------------|
| **MediaEngineDown** | Connection status = 0 | 1m | Media engine gRPC connection lost |
| **MediaEnginePipelineRestartLoop** | Restart rate > 0.1/sec | 2m | Pipeline restarting frequently |
| **ScheduleGap** | Schedule entries = 0 | 5m | No scheduled content |
| **AudioSilence** | Both channels < -60dBFS | 30s | Dead air detected |
| **WebstreamFailoverLoop** | Failover rate > 0.05/sec | 5m | All webstream sources failing |
| **APIHighErrorRate** | 5xx errors > 5% | 3m | API returning server errors |

### Warning Alerts

| Alert | Condition | For | Description |
|-------|-----------|-----|-------------|
| **PlayoutUnderrunHigh** | Underrun rate > 0.05/sec | 2m | Audio dropouts occurring |
| **PlayoutBufferLow** | Buffer < 4800 samples | 30s | Risk of dropout |
| **SchedulerErrors** | Error rate > 0.01/sec | 2m | Scheduler experiencing errors |
| **WebstreamUnhealthy** | Health status = 0 | 2m | Webstream failing health checks |
| **AudioLevelLow** | Level < -40dBFS | 2m | Audio level too quiet |
| **APIHighLatency** | P99 latency > 2s | 5m | API responding slowly |

### Info Alerts

| Alert | Condition | For | Description |
|-------|-----------|-----|-------------|
| **LiveSessionStuck** | Session active unchanged | 15m | DJ session running long |
| **AudioLoudnessOutOfRange** | LUFS < -23 or > -14 | 5m | Loudness outside broadcast standards |

## AlertManager Configuration

### Routing Strategy

Alerts are routed based on:

1. **Severity**: Critical, Warning, Info
2. **Component**: media_engine, scheduler, api, playout, etc.
3. **Alert Name**: Specific alert types

### Grouping

Alerts are grouped by:
- `alertname` - Same type of alert
- `station_id` - Affect same station
- `component` - Same system component

This prevents notification spam when multiple related alerts fire.

### Inhibition Rules

Certain alerts suppress others to reduce noise:

| Source Alert | Suppresses | Reason |
|--------------|------------|--------|
| **SchedulerErrors** | ScheduleGap | Schedule gap is likely caused by scheduler errors |
| **MediaEngineDown** | Playout* alerts | Playout issues are caused by media engine being down |
| **WebstreamFailoverLoop** | WebstreamUnhealthy | Individual failures expected during failover loop |

### Notification Timing

| Severity | Group Wait | Group Interval | Repeat Interval |
|----------|-----------|----------------|-----------------|
| **Critical** | 0s (immediate) | 5m | 30m |
| **Warning** | 30s | 10s | 6h |
| **Info** | 5m | 10s | 24h |

## Notification Channels

### Slack

Configure Slack webhook in `alertmanager.yml`:

```yaml
slack_configs:
  - api_url: 'https://hooks.slack.com/services/YOUR/SLACK/WEBHOOK'
    channel: '#grimnir-alerts-critical'
    title: '{{ .GroupLabels.alertname }}'
    text: |
      {{ range .Alerts }}
      *Alert:* {{ .Labels.alertname }}
      *Severity:* {{ .Labels.severity }}
      *Station:* {{ .Labels.station_id }}
      *Description:* {{ .Annotations.description }}
      {{ end }}
```

**Create Slack Webhook:**
1. Go to https://api.slack.com/apps
2. Create new app
3. Enable Incoming Webhooks
4. Add webhook to workspace
5. Copy webhook URL

### PagerDuty

Configure PagerDuty integration:

```yaml
pagerduty_configs:
  - routing_key: 'YOUR_PAGERDUTY_ROUTING_KEY'
    description: '{{ .GroupLabels.alertname }}'
    severity: '{{ .CommonLabels.severity }}'
```

**Setup PagerDuty:**
1. Create integration in PagerDuty service
2. Select "Prometheus" as integration type
3. Copy integration key to `routing_key`

### Email

Configure SMTP settings:

```yaml
global:
  smtp_smarthost: 'smtp.gmail.com:587'
  smtp_from: 'alertmanager@grimnir-radio.local'
  smtp_auth_username: 'your-email@gmail.com'
  smtp_auth_password: 'app-specific-password'
  smtp_require_tls: true

receivers:
  - name: 'critical'
    email_configs:
      - to: 'oncall@grimnir-radio.local'
        headers:
          Subject: '[CRITICAL] {{ .GroupLabels.alertname }}'
```

### Webhook

Send alerts to custom endpoint:

```yaml
webhook_configs:
  - url: 'http://localhost:8080/api/v1/alerts/webhook'
    send_resolved: true
    http_config:
      bearer_token: 'your-webhook-token-here'
```

**Webhook Payload Format:**

```json
{
  "version": "4",
  "groupKey": "{}:{alertname=\"MediaEngineDown\"}",
  "status": "firing",
  "receiver": "critical",
  "groupLabels": {
    "alertname": "MediaEngineDown"
  },
  "commonLabels": {
    "alertname": "MediaEngineDown",
    "severity": "critical",
    "executor_id": "exec-123"
  },
  "commonAnnotations": {
    "description": "The media engine gRPC connection is down for executor exec-123",
    "summary": "Media Engine disconnected for executor exec-123"
  },
  "alerts": [
    {
      "status": "firing",
      "labels": {
        "alertname": "MediaEngineDown",
        "severity": "critical",
        "executor_id": "exec-123"
      },
      "annotations": {
        "description": "The media engine gRPC connection is down for executor exec-123",
        "summary": "Media Engine disconnected for executor exec-123"
      },
      "startsAt": "2026-01-22T10:30:00Z",
      "endsAt": "0001-01-01T00:00:00Z"
    }
  ]
}
```

## Alert Severity Levels

### Critical

**Impact**: Service degradation or outage
**Response Time**: Immediate (< 5 minutes)
**Notification**: Multiple channels (Slack, PagerDuty, Email)
**Examples**:
- Media engine disconnected
- Audio silence
- No scheduled content
- High API error rate

**Action Required**: Immediate investigation and resolution

### Warning

**Impact**: Degraded performance, risk of outage
**Response Time**: Within 30 minutes
**Notification**: Slack, Email
**Examples**:
- High playout underruns
- Low buffer depth
- Scheduler errors
- High API latency

**Action Required**: Investigation during business hours, may escalate to critical

### Info

**Impact**: Informational, no immediate action required
**Response Time**: Best effort
**Notification**: Webhook only (logged)
**Examples**:
- Long-running live session
- Audio loudness outside ideal range

**Action Required**: Review during normal operations, trend monitoring

## Customization

### Adding New Alert Rules

Edit `deploy/prometheus/alerts.yml`:

```yaml
- name: my_custom_alerts
  interval: 30s
  rules:
    - alert: MyCustomAlert
      expr: my_metric > threshold
      for: 5m
      labels:
        severity: warning
        component: my_component
      annotations:
        summary: "Short description"
        description: "Detailed description with context"
```

**Alert Rule Best Practices:**
- Use descriptive alert names (PascalCase)
- Set appropriate `for` duration to avoid flapping
- Include station_id in labels when applicable
- Provide actionable descriptions
- Use template variables in annotations

### Modifying Thresholds

Common threshold adjustments:

```yaml
# Increase buffer low threshold from 4800 to 9600 samples
- alert: PlayoutBufferLow
  expr: grimnir_playout_buffer_depth_samples < 9600
  for: 30s

# Reduce underrun sensitivity from 0.05 to 0.1/sec
- alert: PlayoutUnderrunHigh
  expr: rate(grimnir_playout_dropout_count_total[5m]) > 0.1
  for: 2m

# Change audio silence threshold from -60 to -50 dBFS
- alert: AudioSilence
  expr: grimnir_media_engine_audio_level_left_db < -50 and grimnir_media_engine_audio_level_right_db < -50
  for: 30s
```

### Custom Notification Templates

Create template file `deploy/alertmanager/templates/custom.tmpl`:

```tmpl
{{ define "slack.custom.title" }}
[{{ .Status | toUpper }}{{ if eq .Status "firing" }}:{{ .Alerts.Firing | len }}{{ end }}] {{ .GroupLabels.alertname }}
{{ end }}

{{ define "slack.custom.text" }}
{{ range .Alerts }}
*Station:* {{ .Labels.station_id }}
*Component:* {{ .Labels.component }}
*Description:* {{ .Annotations.description }}
*Time:* {{ .StartsAt.Format "2006-01-02 15:04:05" }}
{{ end }}
{{ end }}
```

Reference in `alertmanager.yml`:

```yaml
templates:
  - '/etc/alertmanager/templates/*.tmpl'

receivers:
  - name: 'critical'
    slack_configs:
      - title: '{{ template "slack.custom.title" . }}'
        text: '{{ template "slack.custom.text" . }}'
```

## Troubleshooting

### Alerts Not Firing

**Check Prometheus:**
```bash
# Verify metrics are being scraped
curl http://localhost:9090/api/v1/targets

# Check if alert rules are loaded
curl http://localhost:9090/api/v1/rules | jq

# Query metric directly
curl 'http://localhost:9090/api/v1/query?query=grimnir_media_engine_connection_status'
```

**Common Issues:**
- Metrics endpoint not accessible
- Alert rule syntax error
- Threshold not met
- `for` duration not elapsed

### Notifications Not Received

**Check AlertManager:**
```bash
# View AlertManager status
curl http://localhost:9093/api/v1/status

# View active alerts
curl http://localhost:9093/api/v1/alerts

# View silences
curl http://localhost:9093/api/v1/silences
```

**Common Issues:**
- AlertManager not configured in Prometheus
- Routing rule doesn't match alert labels
- Alert is silenced
- Receiver configuration error (webhook URL, API key, etc.)

### Testing Alerts

**Force an alert to fire:**

```bash
# Stop media engine to trigger MediaEngineDown
systemctl stop grimnir-media-engine

# Check Prometheus alerts page
# http://localhost:9090/alerts

# Check AlertManager
# http://localhost:9093
```

**Create test alert rule:**

```yaml
- alert: TestAlert
  expr: vector(1)
  labels:
    severity: warning
  annotations:
    description: "This is a test alert that always fires"
```

### Silence Alerts

**Via AlertManager UI:**
1. Go to http://localhost:9093
2. Click "Silence" button
3. Set matcher (e.g., `alertname="PlayoutUnderrunHigh"`)
4. Set duration
5. Add comment

**Via CLI:**

```bash
amtool silence add alertname=PlayoutUnderrunHigh --duration=2h --comment="Maintenance window"
```

**Via API:**

```bash
curl -X POST http://localhost:9093/api/v1/silences \
  -H "Content-Type: application/json" \
  -d '{
    "matchers": [{"name": "alertname", "value": "PlayoutUnderrunHigh", "isRegex": false}],
    "startsAt": "2026-01-22T10:00:00Z",
    "endsAt": "2026-01-22T12:00:00Z",
    "createdBy": "admin",
    "comment": "Maintenance window"
  }'
```

## Webhook Integration Example

Create webhook handler in Grimnir Radio API:

```go
// internal/api/alerts.go
func (a *API) HandleAlertWebhook(w http.ResponseWriter, r *http.Request) {
    var payload AlertManagerPayload
    if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
        http.Error(w, "Invalid payload", http.StatusBadRequest)
        return
    }

    for _, alert := range payload.Alerts {
        a.logger.Info().
            Str("alert", alert.Labels["alertname"]).
            Str("status", alert.Status).
            Str("severity", alert.Labels["severity"]).
            Str("station_id", alert.Labels["station_id"]).
            Msg("received alert")

        // Store alert in database, trigger remediation, etc.
    }

    w.WriteHeader(http.StatusOK)
}
```

## Best Practices

1. **Start with defaults** - Use provided alert rules, tune thresholds based on your environment
2. **Group related alerts** - Avoid notification spam by using proper grouping
3. **Set appropriate `for` durations** - Prevent flapping alerts
4. **Test notification channels** - Verify all receivers work before production
5. **Document escalation procedures** - Define who handles what severity
6. **Review alerts regularly** - Remove noisy alerts, add missing ones
7. **Use inhibition rules** - Prevent cascading notifications
8. **Monitor AlertManager** - Set up alerts for AlertManager itself
9. **Maintain runbooks** - Document remediation steps for each alert
10. **Silence during maintenance** - Use silences for planned work

## References

- [Prometheus Alerting Documentation](https://prometheus.io/docs/alerting/latest/)
- [AlertManager Configuration](https://prometheus.io/docs/alerting/latest/configuration/)
- [Alert Rule Best Practices](https://prometheus.io/docs/practices/alerting/)
- [Grimnir Radio Metrics Reference](./METRICS.md)
