# recover-partition

## When to use this

The link between the two HA nodes was partitioned & is now recovering. The cluster may now have two VIP holders (split-brain at the listener layer), two leader-election claimants, or two diverged Postgres write tails. This subcommand reports the conflict surface for operator decision. It does NOT auto-merge; merging diverged writes needs a human picking which side is canonical.

## What it does

1. For every VIP (`$GRIMNIR_LISTENER_VIP`, `$GRIMNIR_DJ_VIP`), runs `ip addr show | grep <VIP>` on every host & counts holders. Records a conflict if the count is not exactly 1.
2. For every host, queries `SELECT pg_current_wal_lsn()`. Records the LSN per host (informational; operator compares the two LSNs to decide which side has more recent writes).
3. Reads `grimnir:leader` from Redis. Records a conflict if the key is missing (lease expired during the partition; no current leader).
4. Prints the report. If any conflicts were recorded, exits non-zero so the audit row reflects the conflict & the shell exit code is non-zero.

## Pre-flight gates this respects

None. This is itself a triage subcommand & runs even during incidents.

## What to check after it completes

If the subcommand exits zero: cluster looks healthy post-partition. Confirm with:

```bash
grimnir-deploy verify
```

If the subcommand exits non-zero: the operator decides per conflict.

- **VIP count = 2**: pick the survivor. Run `grimnir-deploy drain --node=<loser>` to release that side's VIP, then re-verify.
- **VIP count = 0**: VRRP didn't elect either side. Check `systemctl status keepalived` on both nodes; usually a config drift or a stale failure file. Clear the failure file on the intended primary (`rm /var/run/keepalived/vrrp_fail`); VRRP picks within ~3s.
- **Diverged Postgres LSNs**: the side with the higher LSN took more writes. If the lower-LSN side is the current primary (the cluster failed over during the partition), the higher-LSN writes were on the OLD primary & are lost. Recover them from `pgbackrest` WAL archive if the WAL was pushed before the partition cut; otherwise, gone. If the higher-LSN side IS the current primary, no recovery action needed.
- **No leader lease in Redis**: run `grimnir-deploy promote-replica` if the cluster is leaderless because the prior leader crashed during the partition. If both nodes are healthy & just need to re-elect, restart `grimnir-radio` on one node; it will reclaim the lease.

## What to do if it fails mid-way

The subcommand only reads; nothing to roll back. If it crashes (e.g. Redis unreachable), the audit row records the failure & nothing else changed.

## Audit trail

- `audit_log` rows with `subcommand='recover-partition'`, START + COMPLETE / FAILED phases
- ntfy topic `grimnir-audit-<region>`
- The printed report (VIPs, LSNs, leader lease) is in the audit row's stdout capture for post-mortem review
