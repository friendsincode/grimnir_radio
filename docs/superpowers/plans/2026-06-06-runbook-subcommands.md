# Runbook Subcommands (Track B-6)

> **Status:** Coordination doc. The actual implementation lives in Track B-2's chunks 7-12 (`docs/superpowers/plans/2026-06-06-grimnir-deploy.md`).

**Goal:** Operational runbooks as first-class subcommands of the `grimnir-deploy` Go binary, per Section 8.2 of the HA design.

**Why a separate "plan":** The original HA design's Section 9.1 listed B-6 as a parallel work track. Practically, the runbook subcommands ARE chunks 7+ of the grimnir-deploy binary (B-2) — they can't ship without the binary's scaffold (B-2 Chunks 0-2: skeleton + audit_log + emergency-pause) being in place first. This doc exists to keep the cross-reference in `docs/superpowers/plans/` honest, not to duplicate the work.

## What lives where

| Subcommand | Plan reference |
|---|---|
| `grimnir-deploy verify` | B-2 Chunk 6 |
| `grimnir-deploy drain --node=N` | B-2 Chunk 7 |
| `grimnir-deploy promote-replica` | B-2 Chunk 8 |
| `grimnir-deploy cold-start-region --region=R` | B-2 Chunk 9 |
| `grimnir-deploy restore --from=<id> [--target-time=<TS>]` | B-2 Chunk 10 (depends on B-5 pgbackrest config) |
| `grimnir-deploy recover-partition` | B-2 Chunk 11 |
| `grimnir-deploy backup-drill --region=R` | B-2 Chunk 12 (depends on B-5) |
| `grimnir-deploy emergency-pause` / `emergency-resume` | B-2 Chunk 2 |

## Cross-cutting requirements (apply to every subcommand)

Per Section 8.2:
- `--dry-run` flag
- `--help` shows the procedure inline (not just flag descriptions)
- Audit log entry written before AND after via the `audit_log` table (B-4 Subsystem 3)
- ntfy notification posted on completion (B-4 Subsystem 2)

## Companion `docs/runbooks/` markdown

Per Section 8.2:
> The markdown index in `docs/runbooks/index.md` is a table: symptom → subcommand → short description. Operator opens it at 3am, finds the symptom, runs the named subcommand. The subcommand's `--help` and inline prompts carry the rest.

Create this index as part of B-2 Chunk 14 (docs + version bump). Each subcommand also gets a per-symptom runbook page (`docs/runbooks/drain-a-node.md`, etc.) that mirrors the subcommand's `--help` output for offline / web-readable consumption.

## Acceptance

B-6 closes when:
- All subcommands in B-2 chunks 6-12 are shipped
- `docs/runbooks/index.md` exists and links to per-symptom pages
- Solo operator (the only on-call per Q-F2a) can find the right subcommand for a symptom within 30 seconds of opening the index

## Out of scope

- Per-customer / per-region runbooks beyond the generic ones — those are operator playbooks specific to a deployment, not part of the v2 codebase
- Runbook execution from Slack / chat-ops integrations — separate plan, not part of v2

## Estimated effort

Subsumed entirely into B-2's effort estimate (6-8 calendar weeks for grimnir-deploy as a whole, of which ~3-4 weeks is the runbook subcommands).

## Filed

2026-06-06 as part of the full v2 plan-writing pass. Cross-references B-2 + B-4 + B-5.
