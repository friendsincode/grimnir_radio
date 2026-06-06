# Observability Topology (HA v2)

What scrapes what, where alerts go, where dashboards live, & how secrets resolve. One page. If you're triaging at 3am, start at the runbook index (`docs/runbooks/index.md`); this doc is for the operator who needs to understand the wiring.

For the legacy single-instance observability guide (covers OpenTelemetry tracing, event-bus inspection, troubleshooting recipes), see `docs/OBSERVABILITY.md`. This document covers only the HA-track additions shipped in v2.0.0-alpha.5 through v2.0.0-alpha.6.

## Component map

```
                              +---------------------+
   control plane (8080) ----> |                     |
   media engine    (9090) --> |   Prometheus        |---> Alertmanager (9093)
   edge encoder    (9095) --> |   (15s scrape)      |              |
   grimnir-fanout  (9097) --> |                     |              v
   grimnir-deploy  (9100) --> |                     |     alertmanager-ntfy
   node-exporter   (9100) --> +---------------------+     bridge (loopback :9098)
                                       |                          |
                                       v                          v
                                  Grafana                       ntfy.sh
                                  (dashboards as code)          (self-hosted)
                                                                  |
                                                                  v
                                                    operator phones / desktops
```

Auto-rollback runs as a goroutine inside `grimnir-deploy`, not a separate process. It polls Prometheus directly via the same URL Grafana uses; see "Auto-rollback" below.

## Scrape targets

Defined in `ops/prometheus/prometheus.yml`:

| Job | Targets | Port | Metrics path |
|---|---|---|---|
| `grimnirradio` | `node-1`, `node-2` | 8080 | `/metrics` |
| `mediaengine` | `node-1`, `node-2` | 9090 | `/metrics` |
| `edge-encoder` | `node-1`, `node-2` | 9095 | `/metrics` |
| `grimnir-fanout` | `node-1`, `node-2` | 9097 | `/metrics` |
| `grimnir-deploy` | `grimnir-deploy.internal` | 9100 | `/metrics` |
| `node-exporter` | `node-1`, `node-2` | 9100 | `/metrics` |

Scrape interval is 15s, eval interval 30s. Every binary registers HA-specific metrics through `internal/metrics/` (per-binary registry, prefixed `grimnir_`). The legacy cross-binary metrics still live in `internal/telemetry/`; both are exposed on the same `/metrics` endpoint.

Two new health probes feed Prometheus through the control plane's registry:

- `internal/dbhealth/` exports `grimnir_pg_replication_lag_seconds` (poll interval 10s). Used by the `PostgresReplicationLagWarn` / `...Critical` alerts.
- `internal/vrrphealth/` exports `grimnir_vrrp_master_count` and `grimnir_vrrp_state` (poll interval 5s). Used by the `VrrpSplitBrain` alert.

## Alert tiers

Rule files live in `ops/prometheus/rules/`:

- `grimnir-ha.yml` is the production rule set.
- `grimnir-ha-tests.yml` is the promtool test file. `make prometheus-validate` runs both.

Every rule carries a `severity` label. Three values, three routes (`ops/alertmanager/config.yml`):

| Severity | Bridge route | ntfy topic | Wake the operator? |
|---|---|---|---|
| `notify` | `bridge-notify` | `grimnir-audit-<region>` (priority 3) | No |
| `page` | `bridge-page` | `grimnir-region-<region>-page` (priority 5) | Yes |
| `page-and-rollback` | `bridge-page-and-rollback` | page topic + grimnir-deploy webhook | Yes; rollback already kicked |

The `alertmanager-ntfy` bridge (binary at `cmd/alertmanager-ntfy`, package at `internal/alertbridge/`) is a loopback sidecar. Alertmanager POSTs to `localhost:9098/webhook`; the bridge re-emits as ntfy POSTs through `internal/notify/`. A bridge outage doesn't silently disable rollback: page-and-rollback fans out to BOTH the bridge AND directly to `http://grimnir-deploy.internal:9100/webhook/auto-rollback`.

Unknown / missing severity defaults to `page` (loud failure mode beats silent).

## Auto-rollback

Lives in `internal/grimnirdeploy/autorollback/`. The deploy orchestrator (`internal/grimnirdeploy/cmd_deploy.go`) constructs an Observer for the soak window. Production builds use `Monitor`, which polls Prometheus on `GRIMNIR_DEPLOY_AUTOROLLBACK_TICK` (default 15s) for the duration of the soak window.

Default rule set (`DefaultRules()` in `rules.go`):

| Rule | Query | Threshold | Dwell |
|---|---|---|---|
| `listener_reconnects` | `sum(rate(grimnir_listener_reconnects_total[1m]))` | > 5/sec | 2 ticks |
| `http_5xx_rate` | `sum(rate(grimnir_http_requests_total{status=~"5.."}[1m]))` | > 0.5/sec | 2 ticks |
| `alert_firing` | `sum(ALERTS{severity="page-and-rollback",alertstate="firing"})` | > 0 | 1 tick |

A breached rule flips the Verdict to `Rollback`. The deploy orchestrator then invokes the same code path as a manual `grimnir-deploy --rollback`. Every Verdict (including `OK`) writes an `audit_log` row plus posts a tier-3 ntfy.

Disable per-invocation with `GRIMNIR_DEPLOY_AUTOROLLBACK_ENABLED=false`. Tests do this to avoid needing a live Prometheus. The webhook receiver path (Alertmanager → grimnir-deploy) is independent of the polling path; either can trigger rollback.

## Dashboards

Three dashboards as code in `ops/grafana/dashboards/`:

- `ha-overview.json` — VRRP master count, replication lag, ntfy backlog, deploy phase.
- `audio-pipeline.json` — per-station listener counts, edge-encoder byte flow, PCM input switches, decoder process count (the v1.40.8 leak indicator).
- `deploy-and-audit.json` — `audit_log` rows by subcommand & outcome, deploy duration histogram, auto-rollback verdicts.

Provisioning lives in `ops/grafana/provisioning/`. Grafana reads dashboards from disk; edits happen in JSON & ship through the same git workflow as code. The edit workflow is in `ops/grafana/README.md`.

## Secrets resolution

`internal/secrets/` is the single read path. Backend selected by `GRIMNIR_SECRETS_BACKEND`:

- `env` (default) — reads `.env` (or `GRIMNIR_SECRETS_ENV_FILE`). Matches the single-instance / local-disk philosophy of Grimnir's baseline.
- `vault` — HashiCorp Vault KV v2 with AppRole auth via `VAULT_ADDR` / `VAULT_ROLE_ID` / `VAULT_SECRET_ID`.

Every backend implements the same `Backend` interface (Get / Put / List / Rotate / Close). The `Rotate` method takes a verifier callback; on verifier failure the old value is restored. The contract test suite in `secrets_test.go` runs against both backends so behavior doesn't drift.

Rotation is exposed through `grimnir-deploy rotate-secret --name X`. Each rotation writes an `audit_log` row & posts a tier-1 ntfy notification.

To rotate a secret, see `docs/runbooks/secrets/rotation.md`.

## Audit notifications

The `audit_log` table (migration `047_audit_log.sql`, shipped in B-2 Chunk 1) holds every operator action that mutates state. The writer lives at `internal/grimnirdeploy/audit/` & every `grimnir-deploy` subcommand runs through the cobra middleware that wraps execution in a start row + completion row.

Every audit row also fires a tier-1 ntfy to `grimnir-audit-<region>`. If you see an audit notification you didn't trigger, treat as a security event; see the runbook index.

## Where each thing lives

| Thing | Path |
|---|---|
| HA metric definitions | `internal/metrics/ha.go` |
| ntfy client | `internal/notify/` |
| Alertmanager-to-ntfy bridge | `internal/alertbridge/`, `cmd/alertmanager-ntfy/` |
| Auto-rollback observer | `internal/grimnirdeploy/autorollback/` |
| Postgres replication health | `internal/dbhealth/` |
| VRRP split-brain health | `internal/vrrphealth/` |
| Prometheus scrape config | `ops/prometheus/prometheus.yml` |
| Prometheus rules | `ops/prometheus/rules/grimnir-ha.yml` |
| Alertmanager routing | `ops/alertmanager/config.yml` |
| Grafana dashboards | `ops/grafana/dashboards/` |
| Audit log writer | `internal/grimnirdeploy/audit/` |
| Secrets backend | `internal/secrets/` |
| Runbook index | `docs/runbooks/index.md` |

## Validation

`make prometheus-validate` runs `promtool check rules` & `promtool test rules` against the files in `ops/prometheus/`. CI runs it; you should too before pushing rule edits.

`make ci` runs `make verify` plus `gofmt -l` check. Every Go change in the observability stack passes through `internal/metrics/`, `internal/notify/`, `internal/alertbridge/`, or `internal/grimnirdeploy/autorollback/` — the unit tests for those packages cover the bridge routing, the rule evaluation, & the notify retry budget.
