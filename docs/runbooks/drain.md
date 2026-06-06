# drain

## When to use this

You need to take a node out of service for a reboot, kernel patch, hardware swap, or any non-deploy maintenance. Drain hands listeners & leadership off to the peer cleanly so listeners don't hear a gap.

For the full "node down for hours" workflow including post-maintenance return steps, see [drain-a-node.md](./drain-a-node.md).

## What it does

1. Touches `/var/run/keepalived/vrrp_fail` on the target node. Keepalived's `vrrp_script` watches this file; presence drops the node's VRRP priority below the peer's, causing the VIP to float.
2. Sleeps 3 seconds for the VIP transition.
3. Stops each service in order via `docker compose stop`: grimnir-radio, edge-encoder, grimnir-fanout, grimnir-mediaengine. Order is outermost-listener-first so in-flight listener sockets close before the engines feeding them go away.

The default grace period per service is 30 seconds (compose's default `--timeout`). Override with `--grace=DURATION`.

## Pre-flight gates this respects

- None directly; drain is itself a remediation step.
- Operators typically run `grimnir-deploy verify` first to confirm the peer is healthy enough to absorb the load.

## What to check after it completes

```bash
# On the drained node:
docker ps --format '{{.Names}}\t{{.Status}}'   # grimnir-* should be absent
ip addr show | grep <VIP>                       # should print nothing

# On the peer:
ip addr show | grep <VIP>                       # should show the VIP
grimnir-deploy verify                           # peer should be all-OK
```

## What to do if it fails mid-way

The VRRP failure file is the most reliable hand-off mechanism; if it didn't trigger, check `systemctl status keepalived` & inspect `/etc/keepalived/keepalived.conf` for the `vrrp_script` definition. If a `compose stop` hangs past the grace period, compose escalates to SIGKILL; that's fine for grimnir-radio & edge-encoder (stateless / restartable). For the media engine, a SIGKILL leaves no chance to flush DSP state; on the next start, expect a half-second of silence as the engine re-warms its loudness normalizer.

To return the node to service after maintenance: `rm /var/run/keepalived/vrrp_fail` then start the compose stack (`./grimnir up -d`). VRRP picks the higher-priority node within ~3s.

## Audit trail

- `audit_log` rows with `subcommand='drain'`, START + COMPLETE / FAILED phases
- ntfy topic `grimnir-audit-<region>`
