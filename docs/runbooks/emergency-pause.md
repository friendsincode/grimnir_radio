# emergency-pause / emergency-resume

## When to use this

There's an active incident & you need to freeze every deploy until it's resolved. Sets a Redis key that every grimnir-deploy mutating subcommand reads first; if set, those subcommands abort with the pause reason. The grimnirradio scheduler reads the same key before any auto-deploy gate.

Use `emergency-resume` to clear the key once the incident is over.

## What it does

`emergency-pause`:

1. Reads the current value of `grimnir-deploy:emergency-pause:<region>` so the operator can see (& overwrite) a prior pause.
2. Sets the key with payload `{reason, operator, ts}`. If `--ttl=DURATION` is given, the key expires automatically; default is sticky (manual resume required).
3. Writes an `audit_log` row with the reason.

`emergency-resume`:

1. Reads the current value & echoes who set it & when.
2. Deletes the Redis key.
3. Writes an `audit_log` row with the resume reason.

## Pre-flight gates this respects

None. Pause / resume are themselves the pause mechanism; gating them on pause would deadlock. They still write `audit_log` & post ntfy.

## What to check after it completes

```bash
redis-cli get grimnir-deploy:emergency-pause:default
# pause -> JSON with reason/operator/ts; resume -> (nil)

grimnir-deploy deploy v2.0.0-alpha.5 --dry-run
# with pause set: aborts with "emergency-pause: <reason>"
# without pause: prints the planned actions
```

## What to do if it fails mid-way

The only failure modes are Redis unreachable or a key serialization error. Both surface in the audit_log `failed` row. If Redis is unreachable, deploys are already going to fail their pause-check pre-flight (which fails closed if it can't read Redis), so the cluster is effectively frozen anyway. Restore Redis & re-run the subcommand.

## Audit trail

- `audit_log` rows with `subcommand='emergency-pause'` or `'emergency-resume'`, START + COMPLETE phases
- ntfy topic `grimnir-audit-<region>`; pause posts at `audit.PriorityHigh`, resume at `audit.PriorityDefault`
- Redis key `grimnir-deploy:emergency-pause:<region>` itself; inspect with `redis-cli get`
