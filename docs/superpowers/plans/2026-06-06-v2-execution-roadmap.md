# v2 Execution Roadmap

> **Status:** Living doc. Updates as phases advance.

**Purpose:** Concrete plan-of-attack for delivering the v2 HA architecture to a real production cutover. References every per-subsystem plan and orders them by dependency + risk.

**Audience:** The next session — and the one after that. So whoever picks up the v2-dev branch can see exactly where we are and what to do next without re-deriving it.

## The goal that anchors everything

**The cutover** per Section 9 of `docs/superpowers/plans/2026-06-01-ha-zero-loss-failover-design.md`: stand up a parallel HA stack alongside current prod, sync data continuously, swap edge-VPS reverse-proxy upstream in one config reload. One DNS swap; zero listener-visible downtime.

Every Track A/B chunk exists to make that cutover safe + reversible. Anything not directly serving that goal is out of scope or deferred.

## Phase ordering — why this order

Three classes of work, in dependency order:

1. **Phase 0 — operational substrate.** Redis, R2, ntfy.sh, pgbackrest. Without these, no Track B chunk can deploy cleanly. Skipping any of these = tech debt that compounds when later chunks try to use them.
2. **Phase 1 — the deploy tool (B-2).** `grimnir-deploy` is the cutover mechanism. Without it, every deployment is ad-hoc shell. After Phase 1, every change deploys via `grimnir-deploy`.
3. **Phase 2 — observability (B-4) in parallel with B-2 runbook subcommands.** Without B-4 metrics + alerts + auto-rollback, the cutover's 5-min soak window can't be enforced.
4. **Phase 3 — A3 deployed + soaked.** Two control planes against shared DB + Redis; verify lockstep under real load for ~1 week before anything new builds on it.
5. **Phase 4 — A6 fan-out + A2 R2 media migration.** Inbound live audio + media library on R2.
6. **Phase 5 — A7 keepalived + cutover dry-runs.** Final assembly.
7. **Phase 6 — actual cutover.** One config reload at the edge VPS.

## Phase 0 — Operational substrate

**Current state (as of 2026-06-06 evening):**

| Component | Status | Where |
|---|---|---|
| Postgres 16 primary + replica + slot streaming | ✅ shipped | <node-a-ip> (primary), <node-b-ip> (replica) |
| pgbouncer | installed on both, not yet config'd for auth_query | both VMs |
| Redis 7.0 primary + replica + replication up | ✅ shipped | <node-a-ip> (primary), <node-b-ip> (replica) |
| pgbackrest with local POSIX repo + WAL archiving + first full backup | ✅ shipped | <node-a-ip> (`/var/lib/pgbackrest`) |
| R2 bucket for media + backups | ⏳ pending — **operator action: provision Cloudflare account + buckets** | Cloudflare |
| ntfy.sh self-hosted VPS | ⏳ pending — **operator action: provision a small VPS separate from grimnir hosts** | TBD |
| Media library migrated to R2 | ⏳ pending — waits on R2 bucket |
| pgbackrest swapped from POSIX repo to R2 | ⏳ pending — waits on R2 bucket (3 config lines once R2 exists) |

**Operator-pending list** (no engineering work, just account / VPS provisioning + the operator running an rclone migration once):

1. **Cloudflare R2** — create account if needed; create two buckets `grimnir-backup-lab` + `grimnir-backup-lab-dr` (cross-region); create API token scoped to those two; record account ID / endpoint / key / secret somewhere durable (Vault when B-4 lands; for now an env file with 0600 perms).
2. **ntfy.sh VPS** — provision a 1 vCPU / 1 GB VPS on a different provider than the Proxmox; `apt install ntfy` + Caddy for TLS + a per-region topic.
3. **Media migration** — once R2 exists: `rclone sync /srv/data/grimnir_radio/media-data r2:grimnir-media-lab --progress` from current prod.
4. **pgbackrest R2 swap** — update `/etc/pgbackrest/pgbackrest.conf` to point `repo1-type=s3` at the R2 bucket; one stanza-upgrade; verify.

These four are sized for one operator afternoon. They unblock everything Phase 1+ depends on.

## Phase 1 — `grimnir-deploy` (Track B-2)

**Plan:** `docs/superpowers/plans/2026-06-06-grimnir-deploy.md` (6347 lines, 15 chunks)

Execute Chunks 0-6 first (skeleton + audit_log + emergency-pause + deploy_history + pre-flight + main `deploy` + `verify`). After that, **every subsequent change deploys via `grimnir-deploy`**, not ad-hoc shell.

Chunks 7-14 (runbook subcommands + docs) ship in parallel with Phase 2.

Estimate: ~3 weeks for Chunks 0-6, with chunks reasonably independent and subagent-friendly.

## Phase 2 — Observability (Track B-4) in parallel with B-2 chunks 7-14

**Plan:** `docs/superpowers/plans/2026-06-06-observability-secrets-audit.md` (4559 lines, 11 chunks)

The two tracks have weak coupling. Run them as parallel subagents:
- B-4 chunks 1-5 (metrics + ntfy client + audit writer + secrets) ride alongside B-2 chunks 7-12 (runbook subcommands).
- B-4 chunk 8 (auto-rollback webhook) depends on B-2 `--rollback` flag landing first.

Estimate: ~4-6 weeks of overlapping work.

## Phase 3 — A3 deployed + soaked

Spin up the existing `grimnirradio` binary on BOTH Proxmox VMs against the now-real shared Postgres + Redis. Flip `LEADER_ELECTION_ENABLED=true`. Soak for ~1 week.

What we're proving: the executor determinism patches from this session (commits b5f5e9d / e61b401 / c781426 / e576c0b / 8d99361) actually hold under realistic load over time. The lockstep integration test passed in-process; this is the wall-clock validation.

Use `grimnir-deploy verify` (from Phase 1) to compare the two instances' status on a 10-second interval. Alert on any divergence via B-4's ntfy integration.

Estimate: 1 day to deploy + 1 week of passive soak (during which other work proceeds).

## Phase 4 — A6 fan-out + A2 R2 media migration

**A6 fan-out plan:** `docs/superpowers/plans/2026-06-05-live-input-fan-out.md` (521 lines, 12 chunks)
**A2 R2 migration:** subsumed into Phase 0's operator-pending list + `docs/superpowers/plans/2026-06-05-object-storage-decision.md`

A6 is 5-7 weeks. A2 is operational, completes in Phase 0 once R2 exists.

## Phase 5 — A7 keepalived + cutover dry-runs

Stand up keepalived on both VMs for the listener VIP + DJ VIP. Run weekly cutover dry-runs against the parallel stack — practice the actual sequence Section 9.4 describes.

Estimate: 1-2 weeks of config + drills.

## Phase 6 — The actual cutover

Per Section 9.4 of the HA design. Single edge-VPS nginx reload. Auto-rollback if soak metrics fail.

## Tracking summary

| Phase | Status | Effort |
|---|---|---|
| Phase 0 substrate | partial: Postgres+Redis+pgbackrest done; R2+ntfy+migration pending operator | 1 operator afternoon |
| Phase 1 B-2 deploy tool | not started | 3 weeks |
| Phase 2 B-4 observability | not started | 4-6 weeks (parallel with B-2 tail) |
| Phase 3 A3 soak | not started | 1 day deploy + 1 week soak |
| Phase 4 A6 fan-out | not started | 5-7 weeks |
| Phase 5 A7 + drills | not started | 1-2 weeks |
| Phase 6 cutover | not started | 1 hour real + days of prep |

**Honest total to cutover: 4-6 calendar months at solo pace.**

That's the real number. There is no shortcut that doesn't create tech debt that catches up at the cutover moment.

## What I'll keep updating in this doc

- Phase 0's "current state" table as each item ships
- Phase markers as they complete
- Estimates as actuals come in

## Open infrastructure secrets (this session)

`/tmp/ha-secrets.env` on the workhorse has the just-generated:
- REPL_PW (Postgres replication user)
- GRIMNIR_PW (Postgres grimnir role)
- PGBOUNCER_PW (pgbouncer auth_query user)
- REDIS_PW (Redis primary + replica auth)

These need to move to Vault (B-4 Subsystem 4) or at minimum into a durable encrypted location before this workhorse session is over. **For now, sopsable file is the right move.** Operator decision pending.
