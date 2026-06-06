# deploy

## When to use this

Shipping a new release (`grimnir-deploy deploy vX.Y.Z`); or rolling back after a bad deploy (`grimnir-deploy deploy --rollback --reason="..."`). Both code paths take the same per-node sequence; rollback just resolves the prior tag from `deploy_history` first.

## What it does

1. Reads pre-flight gates: emergency-pause not set, deploy policy is `auto` or matches `--force-policy`, image tag exists on the registry, both HA nodes are currently healthy via the verify probe.
2. Writes a `deploy_history` row in the `started` phase with the current & target tags.
3. Picks the first node (non-leader) & rolls it: drain (drop VRRP priority, stop services in order grimnir-radio, edge-encoder, grimnir-fanout, grimnir-mediaengine), pull the new image, run `migrate up` (control plane container), start services, wait up to 60s for `/healthz` on the control plane plus gRPC health on the engine.
4. Restores VRRP priority on the first node so the VIP can come back.
5. Repeats step 3 for the second node (the leader at start).
6. Soaks for 5 minutes; if any node's health probe trips during soak, marks `deploy_history` failed & exits non-zero so the operator decides on rollback.
7. Writes `deploy_history` `complete`.

For `--rollback`: step 1 also refuses rollbacks aged past the eligibility window unless `--force-aged-rollback` is set, & refuses to cross a contract-phase migration boundary unless `--force-through-contract-migration` is set. Both require `--reason="..."`.

## Pre-flight gates this respects

- `grimnir:emergency-pause:<region>` Redis key (set via `emergency-pause`)
- `deploy_policy` config (`auto` / `window` / `manual`)
- Tag-suffix convention (the binary refuses tags without a `v` prefix unless `--force-policy=manual --go` is set)
- Image-exists registry probe
- Pre-deploy `verify` on both nodes (both must be green)

## What to check after it completes

```bash
grimnir-deploy verify
docker inspect --format '{{ .Config.Image }}' grimnir-radio   # on both nodes
psql -tAc "SELECT tag, phase, started_at, completed_at FROM deploy_history ORDER BY started_at DESC LIMIT 5"
```

The verify table should show OK on every component for every host. The image tag should match the deployed tag on both nodes. The most recent `deploy_history` row should be `complete` with a `completed_at` set.

## What to do if it fails mid-way

A per-node failure auto-reverts that node to the prior tag & writes a `failed` row to `deploy_history`. The other node is untouched. Run `grimnir-deploy verify` to confirm the surviving node is still serving listeners. If the auto-revert failed (the binary will say so & exit non-zero), the operator runs `grimnir-deploy deploy --rollback --reason="auto-revert failed: <detail>"` to drive the prior tag back onto the broken node.

If both nodes are now in a mixed state (one new, one prior, neither soaking cleanly), set emergency-pause first (`emergency-pause --reason="mixed-version state, investigating"`), then triage manually.

## Audit trail

- `audit_log` rows with `subcommand='deploy'`, paired START + COMPLETE / FAILED phases
- ntfy topic `grimnir-audit-<region>` (one post on START, one on terminal)
- `deploy_history` table: prior tag, target tag, phase, reason (rollbacks), operator
