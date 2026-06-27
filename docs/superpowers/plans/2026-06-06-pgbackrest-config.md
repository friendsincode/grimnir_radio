# pgbackrest Configuration + Rollout (Track B-5)

> **Status:** Operational + small code. Closes Track B-5 from Section 9.1 of the HA design.

**Goal:** Continuous WAL archiving + weekly base backups for the regional Postgres cluster, delivered to both same-region and cross-region S3-compatible destinations, with quarterly restore drills.

**Driver:** Section 8.4 of `docs/superpowers/plans/2026-06-01-ha-zero-loss-failover-design.md`. Zero-loss listener-stream promise requires Postgres backup that bounds data loss to seconds, not hours.

## Decision lock (from Section 8.4)

| Variable | Value | Rationale |
|---|---|---|
| Tool | **pgbackrest** | Mature; parallel restore; S3 integration; expire policies; per-stanza config |
| Strategy | **continuous WAL archiving + weekly base backups** (Sunday 04:00 local) | RPO < 5 min; RTO < 2 h same-region, < 4 h cross-region |
| Destinations | **Both** same-region (`grimnir-backup-<region>`) + cross-region (`grimnir-backup-<region>-dr`) buckets | Fast restore + disaster resilience |
| WAL push interval | `archive_timeout = 30s` | Bounds worst-case data loss to ~30 s |
| Differential backups | Daily | pgbackrest manages the chain |
| Retention | 30 days differentials + 4 weekly bases (same-region); 90 days + 12 weekly (cross-region) | Configurable per region |
| Drill cadence | Quarterly via `grimnir-deploy backup-drill` (Track B-2) | Documented runbook |

Self-hosted MinIO on its own VM (`192.168.195.24:9000`) is the object-store backend per `docs/superpowers/plans/2026-06-05-object-storage-decision.md`; the backup buckets are separate from the media bucket.

## Scope split

- **Chunk 1** (operational): provision MinIO buckets, configure pgbackrest on the Postgres host, verify a full backup completes
- **Chunk 2** (code): integration with the `grimnir-deploy restore` and `backup-drill` subcommands (Track B-2 work; this plan only documents the contract)
- **Chunk 3** (validation): manual restore drill once, document RTO/RPO measured values

## Chunk 1: Operational setup

### Task 1.1: Provision MinIO buckets

- [ ] Create `grimnir-backup-<region>` on the MinIO VM (`mc mb local/grimnir-backup-<region>`)
- [ ] Create `grimnir-backup-<region>-dr` for disaster recovery; until a second offsite MinIO exists it lives on the same VM, so true cross-region DR is deferred
- [ ] Create a MinIO access key + secret scoped to the two backup buckets (`mc admin user svcacct add`, or the MinIO Console)
- [ ] Note: access key, secret key, endpoint `http://192.168.195.24:9000`

### Task 1.2: Install pgbackrest on the Postgres primary

```bash
# On <node-a-ip> (assumes Postgres is here; adapt if relocated post-reprovision)
sudo apt-get install -y pgbackrest
pgbackrest version
```

### Task 1.3: pgbackrest stanza configuration

Create `/etc/pgbackrest/pgbackrest.conf` (root-owned, 0600):

```ini
[grimnir]
pg1-path=/var/lib/postgresql/16/main
pg1-port=5432
pg1-user=postgres

repo1-type=s3
repo1-path=/grimnir
repo1-s3-bucket=grimnir-backup-<region>
repo1-s3-endpoint=192.168.195.24:9000
repo1-s3-region=auto
repo1-s3-key=<key>
repo1-s3-key-secret=<secret>
repo1-s3-uri-style=path
repo1-retention-full=4
repo1-retention-diff=30

repo2-type=s3
repo2-path=/grimnir
repo2-s3-bucket=grimnir-backup-<region>-dr
repo2-s3-endpoint=192.168.195.24:9000
repo2-s3-region=auto
repo2-s3-key=<key>
repo2-s3-key-secret=<secret>
repo2-s3-uri-style=path
repo2-retention-full=12
repo2-retention-diff=90

[global]
process-max=4
log-level-console=info
log-level-file=detail
archive-async=y
archive-push-queue-max=1GB
```

### Task 1.4: Postgres config changes for WAL archiving

Add to `/etc/postgresql/16/main/postgresql.conf`:

```
archive_mode = on
archive_command = 'pgbackrest --stanza=grimnir archive-push %p'
archive_timeout = 30
max_wal_senders = 8
wal_level = replica
```

Restart: `sudo systemctl restart postgresql`.

### Task 1.5: Stanza creation + first base backup

```bash
sudo -u postgres pgbackrest --stanza=grimnir stanza-create
sudo -u postgres pgbackrest --stanza=grimnir --type=full backup
```

Verify both buckets received segments via the MinIO Console, or `mc ls local/grimnir-backup-<region>`.

### Task 1.6: Cron for differential backups

`/etc/cron.d/pgbackrest-grimnir`:

```
# Differential backup nightly at 02:00
0 2 * * * postgres pgbackrest --stanza=grimnir --type=diff backup >> /var/log/pgbackrest-cron.log 2>&1

# Full backup weekly Sunday at 04:00
0 4 * * 0 postgres pgbackrest --stanza=grimnir --type=full backup >> /var/log/pgbackrest-cron.log 2>&1
```

### Task 1.7: Monitoring hook

Set up Prometheus exporter for pgbackrest (e.g., `pgbackrest_exporter`) on the Postgres host. Wire into the regional Prometheus per Section 8.1.

Alerts:
- WAL push lag > 60s → tier-1 notify
- Last successful backup > 36h ago → tier-2 page
- Repo size growth > 10× weekly average → tier-1 notify (could indicate replication runaway)

## Chunk 2: Integration with grimnir-deploy

(Code work; lives in `cmd/grimnir-deploy` per Track B-2's plan. This chunk just documents the contract.)

### `grimnir-deploy restore --from=<backup-id> [--target-time=<RFC3339>]`

Calls `pgbackrest --stanza=grimnir --type=time --target=<TS> restore`. Pre-flight checks:
- Verify Postgres is stopped on the target host
- Verify target backup exists (`pgbackrest --stanza=grimnir info`)
- Refuse if `--target-time` is older than the oldest available WAL
- Audit log entry per Section 8.3

### `grimnir-deploy backup-drill --region=<region>`

Runs a full restore against a staging Postgres on a non-prod host. Measures:
- Base restore wall time (RTO component)
- Time from start to first queryable state (Postgres up after WAL replay)
- Computed worst-case WAL gap from now (RPO)

Outputs to audit log + ntfy channel per Section 8.1.

A drill failure (RTO over 2h, integrity check fails) raises tier-2 page.

## Chunk 3: First validation drill

Once Chunks 1 + 2 are deployed:

- [ ] Run `grimnir-deploy backup-drill --region=lab` against the test cluster
- [ ] Measure actual RTO and RPO
- [ ] If RTO > 2h: adjust pgbackrest config (`process-max`, `archive-async`, network bandwidth to MinIO)
- [ ] Document the measured values in `docs/runbooks/restore-from-backup.md` (created as part of B-2 runbook subcommands)

## Acceptance

- Both buckets receive WAL segments within 30s of commit
- A full base backup completes weekly per cron
- `pgbackrest info` reports a clean repo
- The first drill (manual or via grimnir-deploy) completes within target RTO
- Prometheus alerts wired in regional monitoring

## Out of scope

- Postgres logical replication to a cross-region replica (phase 2)
- Backup encryption at rest (MinIO can encrypt at rest via SSE; pgbackrest can layer with `repo-cipher-pass` if needed; defer until compliance requires it)
- Point-in-time recovery to fractional-second precision (pgbackrest supports it; default `xid` recovery is sufficient for our RPO)

## Estimated effort

- Chunk 1 (operational): half a day per Postgres host (need both nodes for replication setup; do primary first then replica)
- Chunk 2 (code integration): subsumed into Track B-2 (~1 day on top of B-2 scaffold)
- Chunk 3 (validation): 1-2 hours including the drill measurement

**Total: ~1-2 days of focused work, gated on Track B-2 being far enough along for restore/drill subcommands.**

## Filed

2026-06-06 alongside other Track B plans. Operational rollout pending operator availability + MinIO VM access.
