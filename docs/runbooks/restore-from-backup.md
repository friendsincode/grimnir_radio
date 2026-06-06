# restore-from-backup

## When to use this

Data corruption was discovered & you need to roll the database back to a known-good backup or to a specific point in time before the corruption happened. Common triggers: bad migration that wrote garbage, accidental `DELETE` without `WHERE`, ransomware-style data loss.

This is the destructive one. The restore wipes the live data directory & replaces it with the named backup plus WAL replay. Get an `audit_log` snapshot first so the post-mortem isn't blind.

## What it does

1. Reads `pgbackrest info --stanza=grimnir`; refuses if `--from=<id>` doesn't appear in the backup list (unless `--from=latest`).
2. Refuses if `--target-time` falls outside the archived WAL window (it would have nothing to replay to).
3. Refuses if Postgres is still running on the primary; restoring on top of a live data directory corrupts it.
4. Stops every application service on every host (grimnir-radio, edge-encoder, grimnir-fanout, grimnir-mediaengine).
5. Runs `pgbackrest restore --stanza=grimnir --set=<id>` (or no `--set` for latest); appends `--type=time --target='<RFC3339>'` if `--target-time` was given.
6. Starts Postgres & polls `pg_isready` up to 60 seconds.
7. Starts every application service on every host (`./grimnir up -d`).
8. Runs the full probe (control plane + media engine) on every host; aborts non-zero if anything is still failing post-restore.

## Pre-flight gates this respects

- Backup ID exists in `pgbackrest info` output (or is the literal `latest`)
- `--target-time` (when set) inside the archived WAL window
- Postgres NOT running on the primary at restore time

Emergency-pause is NOT checked; restore can be the response to an incident.

## What to check after it completes

```bash
# On the primary:
psql -tAc "SELECT pg_is_in_recovery()"           # 'f'
psql -tAc "SELECT MAX(started_at) FROM audit_log" # matches expected window

grimnir-deploy verify

# Spot-check application data:
psql -tAc "SELECT count(*) FROM media_items"
psql -tAc "SELECT count(*) FROM stations"
```

If the restore hit `--target-time`, the most recent `audit_log` & `deploy_history` rows should pre-date that target.

## What to do if it fails mid-way

- **Pre-flight refusal**: read the error; the binary names the gate. Adjust the flags & retry.
- **Postgres won't start post-restore**: check `journalctl -u postgresql`. Common cause is a permissions issue on the restored data directory (`chown -R postgres:postgres /var/lib/postgresql/data`).
- **Application services won't come up post-restore**: a schema migration may have been ahead of the restored data; check the `migrations` table. If so, run `migrate up` to re-apply forward.
- **Post-restore verify still fails**: the data may be corrupted at the backup itself. Run `pgbackrest check --stanza=grimnir` to confirm backup integrity. If the backup is bad, try the next-oldest backup (`pgbackrest info` lists them).

If the restore failed before step 5, the live data is still intact; restart the application services & investigate. If the restore failed during or after step 5, the live data is gone & only the backup chain is canonical; pick a different backup or escalate.

## Audit trail

- `audit_log` rows with `subcommand='restore'`, START + COMPLETE / FAILED phases
- ntfy topic `grimnir-audit-<region>` at `audit.PriorityHigh`
- pgbackrest log file (`/var/log/pgbackrest/grimnir-restore.log` on the primary) captures the low-level restore detail
