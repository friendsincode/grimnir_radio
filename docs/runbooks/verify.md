# verify

## When to use this

Triage: is the cluster healthy right now? Non-destructive; safe to run anytime, anywhere. Probes every component on every host concurrently & prints a per-component report table. Exits 0 if everything is green; non-zero otherwise.

Also runs implicitly as a pre-flight gate for `deploy` & as the final verification step of `restore`, `cold-start-region`, & `promote-replica`.

## What it does

1. Reads the host list from config (`local` plus `$GRIMNIR_DEPLOY_PEER_HOST`).
2. Probes every host concurrently (so a slow host doesn't stall the report past the per-probe deadline). Per host, checks:
   - `/healthz` on the control plane (HTTP)
   - gRPC `health.Check` on the media engine
   - gRPC `health.Check` on the edge encoder
   - Fan-out byte flow probe
3. Prints a tabwriter-formatted table: one row per host, one column per component, cell is `OK` or `FAIL: <error>`.
4. Returns a non-zero exit code if any component on any host is unhealthy.

The audit row is metadata only; verify mutates nothing.

## Pre-flight gates this respects

None. Verify is itself a gate for other subcommands.

## What to check after it completes

Read the table. Every cell should be `OK`. Any `FAIL` cell shows the error inline:

```
host       control plane  media engine  edge encoder  fan-out
local      OK             OK            OK            OK
peer-host  OK             OK            FAIL: ...     OK
```

A common pattern: one node green, the peer red on one component. The named component on that host needs investigation; the cluster is still serving listeners via the green node (assuming VIPs are floating correctly).

For a full cluster cross-check, also run:

```bash
psql -tAc "SELECT pg_is_in_recovery()"  # 'f' on the primary, 't' on the replica
redis-cli ping                            # PONG on both nodes' Redis
ip addr show | grep <VIP>                 # exactly one node should hold each VIP
```

## What to do if it fails mid-way

Verify itself shouldn't fail mid-way; each per-host probe has its own bounded timeout & runs in a goroutine. A `FAIL` cell is a finding, not a runtime error.

If verify itself returns a Go error (e.g. SSH unreachable, config DSN bad), the audit_log row captures the error & the binary exits non-zero. Fix the config or the network path & re-run.

## Audit trail

- `audit_log` rows with `subcommand='verify'`, START + COMPLETE / FAILED phases
- ntfy topic `grimnir-audit-<region>` at `audit.PriorityLow` (verify runs frequently; high priority would be noise)
- Useful for catching long-term degradation: query `SELECT outcome, count(*) FROM audit_log WHERE subcommand='verify' GROUP BY outcome` to see the cluster's verify pass rate over time.
