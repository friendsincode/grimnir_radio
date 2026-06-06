# drain-a-node (operator workflow)

## When to use this

End-to-end procedure for taking a node down for maintenance & bringing it back. This is the operator-facing "before & after" wrapper around the [drain](./drain.md) subcommand.

## Before maintenance

1. Verify the peer is healthy & can absorb full load:

   ```bash
   grimnir-deploy verify
   ```

   Expect every component on every host to be OK. If the peer is degraded, fix it first; draining onto a sick peer means listeners hear a gap.

2. Optional: set emergency-pause to block any auto-deploy that might fire during the maintenance window:

   ```bash
   grimnir-deploy emergency-pause --reason="planned maintenance on node A" --ttl=2h
   ```

   The `--ttl=2h` makes the pause auto-clear; safer than relying on remembering to resume.

3. Drain the node:

   ```bash
   grimnir-deploy drain --node=self
   ```

   This drops VRRP priority, waits 3s for VIP float, then stops grimnir-radio, edge-encoder, grimnir-fanout, & grimnir-mediaengine in dependency order.

4. Verify the peer holds the VIP & serves listeners:

   ```bash
   # On the peer:
   ip addr show | grep <VIP>
   curl -sI http://<VIP>:8000/stream/<mount>   # should return 200 with icy-* headers
   ```

## During maintenance

The node is now idle. Reboot, swap hardware, patch the kernel, whatever. The peer is serving every listener.

## After maintenance

1. Bring the compose stack back up on the maintained node:

   ```bash
   cd /srv/docker/grimnir_radio
   ./grimnir up -d
   ```

2. Clear the VRRP failure file so the node can take its VIP back:

   ```bash
   rm /var/run/keepalived/vrrp_fail
   ```

   VRRP picks the higher-priority node within ~3 seconds.

3. Verify both nodes are healthy:

   ```bash
   grimnir-deploy verify
   ```

4. If you set emergency-pause in step 2 (& didn't use `--ttl`), clear it:

   ```bash
   grimnir-deploy emergency-resume --reason="planned maintenance complete on node A"
   ```

## What to do if it fails mid-way

If the peer can't handle the load on its own (CPU > 80%, listener disconnect rate climbing), abort the drain by clearing the failure file & restarting services on the original node:

```bash
rm /var/run/keepalived/vrrp_fail
cd /srv/docker/grimnir_radio && ./grimnir up -d
```

This brings the node back online & VRRP repartitions the VIP per priority within 3 seconds.

## Audit trail

Every step that mutates posts to `audit_log` & ntfy `grimnir-audit-<region>`. The compose calls (`./grimnir up -d`) do NOT post audit rows; that's the documented limitation of `./grimnir` vs `grimnir-deploy`. Prefer subcommands where they exist.
