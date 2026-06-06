# promote-replica

## When to use this

Primary Postgres is degraded or down & the replica needs to take over. Common triggers: primary host is unreachable, primary's disk is full, primary's pg_stat shows runaway WAL accumulation, or planned primary maintenance.

## What it does

1. Queries `pg_stat_wal_receiver` on the replica; refuses to promote unless `status = 'streaming'`.
2. Queries `pg_last_xact_replay_timestamp()` on the replica; refuses to promote if the replication lag exceeds 5 seconds. The threshold is intentionally tight; a higher lag means data loss on promotion.
3. Runs `pg_ctl promote -D /var/lib/postgresql/data` on the replica. This is the actual failover.
4. Rewrites `primary_conninfo` in `/etc/pgbouncer/pgbouncer.ini` on both nodes to point at the new primary, then `systemctl reload pgbouncer` on both.
5. Unless `--skip-rebuild` is set: runs `pg_basebackup` against the new primary, from the old primary's host, to rebuild the old primary as the new replica.

`--skip-rebuild` is for the case where the old primary is dead & cannot run `pg_basebackup` at all (e.g. disk hardware failure). Skip in that case & rebuild the replica manually once new hardware is up.

## Pre-flight gates this respects

- Replica must be in `streaming` state (queried in step 1)
- Replication lag must be < 5s (queried in step 2)
- Emergency-pause is NOT checked here; promotion is itself a remediation & may be needed during an incident-pause window

## What to check after it completes

```bash
# On the new primary (was the replica):
psql -tAc "SELECT pg_is_in_recovery()"          # should print 'f'

# On both nodes:
psql -h localhost -p 6432 -tAc "SELECT 1"        # pgbouncer should route fine

# If --skip-rebuild was NOT set:
# On the rebuilt replica (was the old primary):
psql -tAc "SELECT status FROM pg_stat_wal_receiver"   # 'streaming'

grimnir-deploy verify
```

## What to do if it fails mid-way

The subcommand fails closed at each step. Common failures:

- **Replica not streaming**: the replica was already broken before promotion. Restore the replica from a base backup before re-attempting (see [restore-from-backup.md](./restore-from-backup.md)).
- **Lag > 5s**: writes were happening faster than the replica could keep up. Either wait for the lag to drop & retry, or accept the data loss by manually running `pg_ctl promote` outside the subcommand. The audit trail then has to come from somewhere else; do this only as a last resort.
- **pgbouncer reload fails on one node**: applications routed through that pgbouncer can't reach the new primary. Fix the pgbouncer config manually on the failing node, then `systemctl reload pgbouncer` there.
- **pg_basebackup fails**: re-run with `--skip-rebuild` so the promotion finishes & rebuild the old primary manually later.

## Audit trail

- `audit_log` rows with `subcommand='promote-replica'`, START + COMPLETE / FAILED phases
- ntfy topic `grimnir-audit-<region>` at `audit.PriorityHigh` (failover events are high-signal)
