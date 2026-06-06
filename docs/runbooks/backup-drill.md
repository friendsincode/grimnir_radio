# backup-drill

## When to use this

Quarterly cadence per Section 8.4 of the HA design. You run this to confirm the backup chain actually restores; an untested backup is a hopeful guess. The drill stands up a temporary Postgres on a non-production host, restores the latest backup into it, & reports measured RTO + RPO. Production is never touched.

Also useful ad-hoc after any change to the backup configuration (pgbackrest stanza changes, S3 bucket migration, new retention policy).

## What it does

1. Names a throwaway container: `grimnir-drill-<unix-ts>`.
2. Runs `docker run --rm -d --name <container> -v /tmp/drill-data:/var/lib/postgresql/data postgres:16` on the drill host. Times the start.
3. Runs `pgbackrest restore --stanza=grimnir --pg1-path=/tmp/drill-data` on the drill host. Times the restore (base restore + WAL replay to end-of-archive).
4. Prints the measured timings:
   - `staging-startup`: docker run to container running
   - `pgbackrest restore`: base restore + WAL replay
   - `total`: the sum (your measured RTO for this region)
   - `RPO bound`: archive_timeout (30s) + push retry interval (the theoretical max data loss between primary commit & WAL push to the backup repo)
5. Tears down the container on exit (best-effort; teardown failure does NOT fail the drill).

## Pre-flight gates this respects

- None. The drill is non-production by design.
- The drill host should NOT be hosting any production Postgres on `/tmp/drill-data`; the drill writes there & a name collision corrupts the live one. Use a host with no production Postgres role.

## What to check after it completes

The audit_log row carries the measured RTO. Compare against your stated SLO. If the measured total exceeds your target, investigate:

```bash
# On the drill host, while the container is still up (before teardown):
docker exec <container> psql -U postgres -tAc "SELECT pg_is_in_recovery()"  # 'f' if WAL replay completed
docker exec <container> psql -U postgres -tAc "SELECT count(*) FROM stations"

# Off-host: review the audit_log row for the timings:
psql -tAc "SELECT subcommand, started_at, completed_at, completed_at - started_at AS dur FROM audit_log WHERE subcommand='backup-drill' ORDER BY started_at DESC LIMIT 5"
```

A drill total of 60-180s for a small database (under 10GB) is typical; multi-hundred-GB databases scale linearly with base-restore time.

## What to do if it fails mid-way

- **`docker run` failure**: image not present or `/tmp/drill-data` has stale content. `docker pull postgres:16 && rm -rf /tmp/drill-data` & retry.
- **`pgbackrest restore` failure**: this is the high-signal failure. The backup chain itself is broken. Run `pgbackrest check --stanza=grimnir` against the production primary; it'll surface missing WAL segments, broken parent backups, or unreachable repo storage. Escalate to investigate the backup pipeline before the next production incident.
- **Container teardown failure**: best-effort; the drill still reports its timings. Clean up manually with `docker rm -f <container>` once the drill exits.

A failing drill is the entire point of running them. A drill that's run quarterly & always passes is doing its job; a drill that catches a broken backup chain before you need it is doing its job better.

## Audit trail

- `audit_log` rows with `subcommand='backup-drill'`, START + COMPLETE / FAILED phases
- ntfy topic `grimnir-audit-<region>` with the measured RTO in the message body
