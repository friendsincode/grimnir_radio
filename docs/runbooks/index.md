# Grimnir Deploy Runbooks

3am, alert fires, what do you run? Find the symptom in the table below & run the named subcommand. Each subcommand carries a `--help` that walks through the procedure inline; the per-subcommand markdown files in this directory are the long-form versions for incident-review reading.

| Symptom | Subcommand | Long-form runbook |
|---|---|---|
| New release ready to ship | `grimnir-deploy deploy vX.Y.Z` | [deploy.md](./deploy.md) |
| Just-shipped release is causing problems | `grimnir-deploy deploy --rollback --reason="..."` | [deploy.md](./deploy.md) |
| Active incident; freeze all deploys | `grimnir-deploy emergency-pause --reason="..."` | [emergency-pause.md](./emergency-pause.md) |
| Incident resolved; let deploys run again | `grimnir-deploy emergency-resume --reason="..."` | [emergency-pause.md](./emergency-pause.md) |
| Need to reboot a node or swap hardware | `grimnir-deploy drain --node=N` | [drain.md](./drain.md), [drain-a-node.md](./drain-a-node.md) |
| Primary Postgres degraded or down | `grimnir-deploy promote-replica` | [promote-replica.md](./promote-replica.md) |
| Bringing up a new region | `grimnir-deploy cold-start-region --region=R` | [cold-start-region.md](./cold-start-region.md) |
| Data corruption; need to restore from backup | `grimnir-deploy restore --from=BACKUP_ID` | [restore-from-backup.md](./restore-from-backup.md) |
| Network partition between HA nodes is recovering | `grimnir-deploy recover-partition` | [recover-partition.md](./recover-partition.md) |
| Quarterly: verify backups actually restore | `grimnir-deploy backup-drill --region=R --drill-host=H` | [backup-drill.md](./backup-drill.md) |
| Triage: is the cluster healthy right now? | `grimnir-deploy verify` | [verify.md](./verify.md) |
| DJ can't connect; live ingest unreachable | Check `/healthz` on both fan-outs; restart the failing node | [fanout-down.md](./fanout-down.md) |
| Auto-rollback fired during deploy | Read the rollback ntfy + `audit_log` row; do NOT redeploy without root cause | [deploy.md § auto-rollback](./deploy.md), `internal/grimnirdeploy/autorollback/` |
| Alert firing in tier-2 (page) | Check the ntfy push + open the Grafana dashboard linked in the alert annotation | [docs/observability/README.md](../observability/README.md) |
| Rotate a secret (routine or post-leak) | `grimnir-deploy rotate-secret --name=X --new-value=...` | [secrets/rotation.md](./secrets/rotation.md) |
| Initial bring-up of VIP failover (per region) | apt install keepalived; copy configs from `ops/keepalived/` | [keepalived-install.md](./keepalived-install.md) |
| One-time media cutover from local disk to Cloudflare R2 | rclone sync + `GRIMNIR_MEDIA_BACKEND=s3` | [migrate-media-to-r2.md](./migrate-media-to-r2.md) |

Every subcommand:

- Writes a row to `audit_log` on start AND on completion (success or failure).
- Posts an ntfy notification to `grimnir-audit-<region>`.
- Supports `--dry-run` to print what it would do without mutating.
- Carries a `--help` with the inline procedure.

The audit row & the ntfy post both include the operator's username (from `$USER` or the SSH session), the subcommand, the arguments, the start time, the duration, & the outcome. No mutating command runs without an audit trail.
