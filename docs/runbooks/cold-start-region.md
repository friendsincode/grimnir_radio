# cold-start-region

## When to use this

A brand new region needs to come up from bare metal (or after a region-wide outage where every node was rebuilt). Brings up infrastructure & application services in dependency order on every host in the region.

## What it does

1. **Firewall**: sets `iptables -P INPUT DROP`, then adds the loopback & SSH allow rules. Application port allows come from the per-region rule file (not in the binary; in `/etc/grimnir/firewall.rules` on each node per the secret-managed config).
2. **WireGuard mesh**: `wg-quick up wg0` on every host. Brings up the encrypted overlay between region nodes.
3. **Postgres primary**: `systemctl start postgresql && pg_isready` on the first host.
4. **Postgres replica**: same on the second host. The replica's `postgresql.conf` already knows its primary from the rebuild template.
5. **Redis**: `systemctl start redis && redis-cli ping` on every host.
6. **Object-storage probe**: `curl` the configured `$GRIMNIR_S3_ENDPOINT/minio/health/ready`. Confirms the region can reach its backup target before the application stack starts trying to write.
7. **Application stack**: `./grimnir up -d` on every host (the wrapper handles compose file ordering).

Each step is idempotent. Safe to re-run on a half-up region; the binary will print the step name & continue if a step is already complete.

## Pre-flight gates this respects

- Emergency-pause is NOT checked; cold-start can be needed during an incident.
- Audit row is still written so the timeline of the region bring-up is recoverable.

## What to check after it completes

```bash
# On every host in the region:
systemctl is-active postgresql redis        # both 'active'
docker ps --format '{{.Names}}\t{{.Status}}' # all grimnir-* containers Up

# From the operator's workstation:
grimnir-deploy verify

# Listener path:
curl -sI http://<VIP>:8000/stream/<mount>  # 200 + icy-*
```

If verify is green & a `curl` against the VIP returns ICY headers, the region is serving listeners.

## What to do if it fails mid-way

The binary halts at the first failed step & writes a failed `audit_log` row with the failing host + command. Common failures:

- **Firewall step**: iptables binary missing or the rule conflicts with an existing chain. Inspect with `iptables -L -n -v` & adjust the regional rule file.
- **WireGuard step**: `wg0` config missing or peer keys wrong. Inspect `/etc/wireguard/wg0.conf` & retry.
- **Postgres primary won't start**: check `journalctl -u postgresql -n 200`. Typical cause is a stale `postmaster.pid` from a crash.
- **S3 probe fails**: object-storage endpoint unreachable from the region. Fix that before continuing; the application will write WAL & backups there & a missing endpoint corrupts the backup chain silently.
- **Compose up fails**: read the compose logs (`./grimnir logs <service>`); usually missing env file or wrong image tag.

Fix the failing step, then re-run `cold-start-region --region=R`. Idempotency lets it pick up where it left off.

## Audit trail

- `audit_log` rows with `subcommand='cold-start-region'`, START + COMPLETE / FAILED phases
- ntfy topic `grimnir-audit-<region>`; cold-start posts at `audit.PriorityHigh`
