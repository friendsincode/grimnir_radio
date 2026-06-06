# Grimnir Radio observability stack: operator install

Three files, all operator-installable (no auto-deploy in this repo):

| File | Purpose |
|------|---------|
| `ops/prometheus/prometheus.yml` | Prometheus scrape config + alertmanager target + rule file include |
| `ops/prometheus/rules/grimnir-ha.yml` | HA alerting rules (three severity tiers) |
| `ops/alertmanager/config.yml` | Alertmanager routing → alertmanager-ntfy bridge |

The `cmd/alertmanager-ntfy` binary runs as a sidecar that converts Alertmanager
webhooks into ntfy pages via `internal/notify`. Start it next to Alertmanager
on each region's prom host.

## Severity tiers

The bridge routes by the `severity` label on each alert:

| severity | bridge action | ntfy topic | priority |
|----------|---------------|------------|----------|
| `notify` | Tier1 | audit | 3 |
| `page` | Tier2 | page | 5 |
| `page-and-rollback` | Tier2 | page | 5 |
| anything else | Tier2 (defensive default) | page | 5 |

`page-and-rollback` additionally hits `grimnir-deploy`'s auto-rollback webhook;
that listener lands in Chunk 8.

## Validation

Both Prometheus and Alertmanager ship binaries that validate their own configs.
This repo does NOT depend on them at build time, so validation runs out-of-band.

### Promtool (rules + scrape config)

```bash
docker run --rm -v "$PWD/ops/prometheus:/p" prom/prometheus:v2.55.0 \
  promtool check rules /p/rules/grimnir-ha.yml

docker run --rm -v "$PWD/ops/prometheus:/p" prom/prometheus:v2.55.0 \
  promtool test rules /p/rules/grimnir-ha-tests.yml

docker run --rm -v "$PWD/ops/prometheus:/p" prom/prometheus:v2.55.0 \
  promtool check config /p/prometheus.yml
```

Each command prints `SUCCESS` on a green run.

### Amtool (Alertmanager routing)

```bash
docker run --rm -v "$PWD/ops/alertmanager:/p" prom/alertmanager:v0.27.0 \
  amtool check-config /p/config.yml
```

Expected: `Checking '/p/config.yml'  SUCCESS`.

## Bridge configuration

`cmd/alertmanager-ntfy` reads ntfy config from `internal/notify.FromEnv`:

```bash
export GRIMNIR_NTFY_URL=https://ntfy.example.com
export GRIMNIR_NTFY_AUDIT_TOPIC=grimnir-region-default-audit
export GRIMNIR_NTFY_PAGE_TOPIC=grimnir-region-default-page
export GRIMNIR_NTFY_AUDIT_TOKEN=<bearer>
export GRIMNIR_NTFY_PAGE_TOKEN=<bearer>
export GRIMNIR_ALERTBRIDGE_ADDR=:9098   # default
./alertmanager-ntfy
```

When `GRIMNIR_NTFY_URL` is unset, the bridge accepts payloads as no-ops and
logs a one-time warning. Useful for dev / CI.

## Auto-deploy vs operator-install

Nothing in this directory is wired into the Grimnir docker-compose tree yet.
The intended deploy shape (subject to Chunk 9 / 10 dashboards work):

1. Operator picks a region's prom host, installs Prometheus + Alertmanager via
   the project's own packaging (helm chart, docker-compose, plain systemd —
   not opinionated here).
2. Mount `ops/prometheus/prometheus.yml` and `ops/prometheus/rules/` into the
   Prometheus container.
3. Mount `ops/alertmanager/config.yml` into the Alertmanager container.
4. Run `alertmanager-ntfy` as a sidecar on the same host.
5. Confirm `/metrics` is reachable on each node (control plane, mediaengine,
   edge-encoder, grimnir-fanout, grimnir-deploy, node-exporter).
