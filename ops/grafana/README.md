# Grimnir Grafana dashboards

Three dashboards live here, plus the provisioning glue that loads them on Grafana startup.

| File | Purpose |
|---|---|
| `dashboards/ha-overview.json` | Engine health, VRRP holder count, Postgres replication lag, Redis reachability, cache hit rate. First tab to open when paged. |
| `dashboards/audio-pipeline.json` | PCM input packets/sec per engine-source, edge encoder bytes/sec per node, listener reconnects/sec per mount. Watch during failover drills. |
| `dashboards/deploy-and-audit.json` | `grimnir_deploy_history_failed_total`, Redis unreachable seconds, audit-log write rate. Pull up for every deploy & post-incident review. |

All three are tagged `grimnir` so they're discoverable from Grafana's dashboard search.

## Metric coverage

Dashboards query the metrics registered in `internal/metrics/ha.go`:

- `grimnir_engine_health{node}`
- `grimnir_vrrp_holder_count{vip}`
- `grimnir_postgres_replication_lag_seconds`
- `grimnir_pcm_input_packets_total{engine,source}`
- `grimnir_edge_encoder_bytes_total{node}`
- `grimnir_listener_reconnect_total{mount}`
- `grimnir_deploy_history_failed_total`
- `grimnir_redis_unreachable_seconds`
- `grimnir_cache_hit_rate_ratio`

The audit panel on `deploy-and-audit.json` reads `grimnir_audit_log_writes_total{action}`, which is wired in Chunk 6 of the same plan.

## Local preview

```bash
docker run --rm -p 3000:3000 \
  -v "$(pwd)/ops/grafana/provisioning:/etc/grafana/provisioning" \
  -v "$(pwd)/ops/grafana/dashboards:/etc/grafana/provisioning/dashboards/grimnir" \
  -e GF_AUTH_ANONYMOUS_ENABLED=true \
  -e GF_AUTH_ANONYMOUS_ORG_ROLE=Admin \
  -e PROMETHEUS_URL=http://host.docker.internal:9090 \
  grafana/grafana-oss:11.2.0
```

Open `http://localhost:3000`, search for the `grimnir` tag, & the three dashboards appear under the `Grimnir` folder.

The datasource URL defaults to `http://prometheus:9090` (the service name inside the production docker network). Set `PROMETHEUS_URL` to point elsewhere; the provisioning file expands the variable on Grafana startup.

## Production install

`docker-compose.yml` mounts these two paths into the Grafana container:

- `./ops/grafana/provisioning` → `/etc/grafana/provisioning`
- `./ops/grafana/dashboards` → `/etc/grafana/provisioning/dashboards/grimnir`

`updateIntervalSeconds: 30` in `provisioning/dashboards/ha.yml` means committed JSON changes go live within 30 seconds of being pulled onto the host. No Grafana restart needed.

`disableDeletion: true` blocks the UI from removing dashboards that were provisioned from disk. Edits made in the UI stick (`allowUiUpdates: true`) until the file changes, then the file wins.

## Editing workflow

1. Edit in the Grafana UI; verify the panels render against real data.
2. Dashboard settings -> JSON Model -> copy.
3. Paste over the file's contents, keep `"id": null` & `"version": 1` so re-imports don't clash with Grafana's internal IDs.
4. `jq . ops/grafana/dashboards/<file>.json > /dev/null` to confirm valid JSON.
5. PR with a screenshot of the new or changed panels.

## Adding a dashboard

1. Build in the UI.
2. Export JSON to `ops/grafana/dashboards/<name>.json`.
3. Add a row to the table at the top of this file.
4. Tag it `grimnir` inside the dashboard settings so search finds it.
