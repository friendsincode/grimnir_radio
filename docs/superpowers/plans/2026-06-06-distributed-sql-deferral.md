# Distributed SQL Deferral — Decision Document

> **Status:** Decision. Closes issue [#232](https://github.com/friendsincode/grimnir_radio/issues/232) per acceptance option (b) — "we deliberately decide 'Postgres with per-region sharding is the answer' and document why."

**Date:** 2026-06-06

## TL;DR

**Stay on Postgres for the foreseeable future. Defer distributed SQL (CockroachDB / YugabyteDB / Aurora / Spanner) indefinitely; re-evaluate only when one of these triggers fires:**

1. The HA architecture genuinely operates in **3+ geographic regions** with meaningful cross-region write traffic, AND
2. Postgres + pgbouncer + per-region read replicas + (optionally) app-level write routing measurably hurts user experience, AND
3. The operational cost of running distributed SQL is lower than the engineering cost of working around Postgres's single-primary limit.

None of those are true today. None look likely within the next 12 months.

## Context

Issue #232 was filed during the 2026-06-01 HA architecture brainstorm. The user's exact directive in that conversation:

> let's do A+c and do an issue for the technical debt for D

Where A+C = Postgres primary + streaming replica + pgbouncer (phase 1 + phase 1.5), and D = switch to a distributed SQL engine. The issue exists so the option isn't forgotten when the time comes.

## Why Postgres still wins

### Workload characteristics

Grimnir's write load is dominated by:
- Schedule changes (DJ/admin actions; human-frequency, < 1/sec peak)
- Play history inserts (per-track-end; ~1/track ≈ 1/3min per station)
- Listener event telemetry (anonymous reconnect events; bursty but small)

None of these are write-throughput-bound. A single Postgres primary handles this volume on commodity hardware with multiple orders of magnitude of headroom. The throughput problem distributed SQL would solve doesn't exist for us.

### Latency characteristics

Listener experience depends on the executor + media engine. Neither is in the synchronous DB write path. A write to `play_history` happening 50ms after a track-end is fine. A schedule edit propagating to all engines within 5 seconds is fine. We don't have a sub-100ms cross-region write-latency requirement.

### Operational characteristics

- **Postgres + pgbouncer + Patroni** (phase 1.5): well-understood; runs on commodity Linux; backup via pgbackrest; one operator can hold the failure modes in their head.
- **CockroachDB / Yugabyte**: distributed-systems failure modes; requires comfort with Raft / consensus; clock-skew sensitivity; bigger blast radius when something goes wrong; commercial offerings (Cockroach Cloud) start to look attractive but that's vendor lock-in.
- **Aurora / Spanner**: single cloud lock-in. Grimnir's design (per Section 2 of the HA design) explicitly chose mixed-infra (own VPS + colo + cloud), which rules out cloud-managed services.

### Per-region sharding works

For the 3+ region case (when we ever get there): the natural answer is per-region Postgres + per-region grimnir control planes + cross-region replication for read-only views. Each region's writes go to its own primary. Cross-region reads happen via async-replicated read replicas. The only cross-region writes would be system-wide config changes (rare, batched, eventually-consistent acceptable).

This is **not** harder than running distributed SQL. It's the standard pattern that every regional service uses.

## When to revisit

Open this back up if **ALL** of the following become true:

1. **Cross-region write contention is real.** Specifically: at least one workflow exists where a write in region A must be readable in region B within 100ms, AND that workflow is on the listener-or-DJ-experience hot path. Today no such workflow exists.
2. **The per-region sharding pattern is hitting friction.** Concrete signs: operators routinely have to manually reconcile data across regions; users complain about region-specific inconsistencies; the application has accumulated > 1000 lines of cross-region-coordination code that distributed SQL would erase.
3. **The team has DBA bandwidth to operate distributed SQL.** A single operator who barely keeps Postgres alive will not be served by a more complex system.

Until then, the right answer for everything we'd ask distributed SQL to do is "use Postgres + the right indexes + per-region replication + better app design."

## Specific re-evaluation triggers

- Adding a 3rd region (currently: 1 region planned; phase 2 is the second; phase 3+ is hypothetical)
- A real production incident where Postgres being the single point of writes causes meaningful user impact (not just operator inconvenience)
- A pricing change in S3-compatible distributed SQL offerings that makes "fully managed" actually cheap (currently: $1k+/month at our scale)

## Closes this issue

Setting #232's resolution to **option (b)** per its own acceptance criteria. The issue stays linked in the v2.0 HA design doc as a documented deferral, not an open commitment.

If anyone later disagrees with this decision: re-open the issue, point at which of the three "open this back up" triggers fired, and we re-evaluate. Decisions made on principle are easier to revisit than decisions made under time pressure.

## Related

- `docs/superpowers/plans/2026-06-01-ha-zero-loss-failover-design.md` Section 4 — Postgres HA strategy (the path we're on)
- `docs/superpowers/plans/2026-06-05-object-storage-decision.md` — sibling decision on the storage side (resolved to self-hosted MinIO on its own VM; the cloud/R2 option was deferred)
