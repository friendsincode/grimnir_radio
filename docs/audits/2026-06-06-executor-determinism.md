# Executor determinism audit

Issue #233. Branch v2-dev. Audit-only; no code changed.

## What this audit answers

Section 3 of the HA design doc proposes that both control planes run the executor against the shared DB, each driving its co-located media engine. Two engines stay in lockstep without cross-instance gRPC traffic. That only works if `play` / `crossfade` / `stop` commands produced by the two control planes are byte-identical at the same wall-clock moments.

This audit walks the named code paths and lists every place that would diverge between two instances. Findings are classified Critical (will produce different gRPC commands), Important (could diverge under realistic timing), or Minor (internal state, doesn't escape).

## Method

Read in full: `internal/executor/{executor,state,pool,distributor,media_controller}.go`, `internal/scheduler/{service,leader_aware}.go`, `internal/smartblock/engine.go`, `internal/playout/director.go`. Greps for `time.Now`, `rand.`, `map` ranges, and goroutine spawns.

One scope clarification matters before the findings: the scheduler is leader-only (`internal/scheduler/leader_aware.go`); only one instance writes `schedule_entries`. So scheduler non-determinism is out of scope for lockstep. The director (`internal/playout/director.go`) currently spawns GStreamer processes locally inside the control-plane process, not via gRPC, but the design treats it as part of the same per-station decision surface. Both director and the executor's `priorityEventLoop` therefore need to be deterministic for lockstep to hold.

## Findings

### Critical — observable difference in commands

**C1. `playRandomNextTrack` picks via SQL `RANDOM()`.**
`internal/playout/director.go:3582-3586`. The query is `Where("station_id = ?", ...).Order("RANDOM()").First(&media)`. Two instances hit the same DB at the same instant and Postgres returns different rows. Every random-fill track (called from `handleTrackEnded` default branch at line 3215, from the `webstream_fill` branch at line 3203, and indirectly from a dozen clock/stopset fallbacks) will diverge between instances.

Proposed fix: replace with deterministic selection seeded on `(stationID, mountID, entry.ID, entry.StartsAt, fillIndex)`. Hash → row offset in a stably-ordered candidate set. The candidate query must add `ORDER BY id` (or another stable key) so the offset is meaningful.

**C2. `startSmartBlockEntry` fallback uses SQL `RANDOM()`.**
`internal/playout/director.go:1858-1862`. When `smartblockEng.Generate` fails, the fallback path runs `Order("RANDOM()").First(&media)`. Same root cause as C1. Hits whenever rule relaxation also fails (rare but observable in production: I've seen `smart_block_generation_failed` events on stations with thin catalogs).

Same proposed fix as C1.

**C3. `startClockEntry` stopset fallback uses SQL `RANDOM()`.**
`internal/playout/director.go:2015-2019`. The stopset slot type falls back to random media when neither `playlist_id` nor `media_id` is set in the slot payload. Reachable from any clock template with a misconfigured stopset slot.

Same proposed fix.

**C4. Smart-block engine `recentPlays` uses `time.Now()` for the separation cutoff.**
`internal/smartblock/engine.go:668`. `cutoff := time.Now().Add(-maxWindow)` — the query reads `play_history` rows newer than `cutoff`. Two instances calling `Generate` a few hundred milliseconds apart will see slightly different `recentCache` contents (one row may straddle the cutoff). That changes which candidates `violatesSeparation` excludes, which changes the candidate list, which changes the sequence.

Proposed fix: pass `now` into `Generate` via `GenerateRequest`. For director-side calls, use `entry.StartsAt` (already in DB). For the leader's pre-materialization path in `scheduler/service.go:792`, pass `plan.StartsAt`. The cutoff becomes a deterministic function of DB state.

**C5. `violatesSeparation` uses `time.Now()` to compare against cached recent timestamps.**
`internal/smartblock/engine.go:1230`. Same problem as C4 from the other direction: even if `recentCache` were identical between instances, `now.Sub(ts) < dur` evaluates with each instance's local clock. A track 4m59s old on instance A may be 5m00s old on instance B → one excludes, the other doesn't.

Proposed fix: same as C4 (thread `now` through, use entry.StartsAt).

**C6. `applyFilterRule` for `added_date` uses `time.Now()`.**
`internal/smartblock/engine.go:829` (and the in-memory counterpart at 1394). The SQL-level filter for "newer than N days" and "older than N days" both compute cutoffs from `time.Now()`. Two instances generating the same smart block can pick up or drop a track whose `created_at` straddles the cutoff.

Same fix: thread `now` through `GenerateRequest`.

**C7. `tailFillBumpers` seeds with `time.Now().UnixNano()`.**
`internal/smartblock/engine.go:435`. `rng := rand.New(rand.NewSource(time.Now().UnixNano()))`. The rng is used by `rng.Shuffle(...)` on line 488 to reorder the remaining bumper pool between iterations. The selection itself prefers longest-fitting (deterministic), but after each pick the pool is reshuffled, which changes ties on subsequent picks. Two instances will produce different bumper sequences when more than one bumper has the same best-fit duration.

Proposed fix: derive the seed from the same hash as `deterministicSmartBlockSeed` (request-scoped seed available via `req.Seed`). Pass `req.Seed` into `tailFillBumpers` and use `rand.New(rand.NewSource(req.Seed ^ 0xBu_mper))` or similar.

**C8. `selectSequence`/`selectCandidate` ordering depends on DB row order.**
`internal/smartblock/engine.go:707`. `query.Find(&items)` in `fetchCandidates` has no `ORDER BY`. Postgres makes no guarantee about row order without one. Once `candidates` is built in that arbitrary order, `selectCandidate` does `sort.Slice` on `(idx, score)` with `score += rng.Float64() * 0.1`. The jitter is the same between instances (seed is deterministic), but the input order isn't, so equal-score ties resolve differently. Worse, `sort.Slice` is unstable — equal scores produce non-reproducible order even with identical input.

Proposed fix: add `Order("id ASC")` to the `fetchCandidates` query. Replace `sort.Slice` with `sort.SliceStable`, or add `idx` as a tiebreaker so no two entries compare equal.

**C9. `sbGeneration` is in-memory only.**
`internal/playout/director.go:128, 178`. The map `sbGeneration[playKey]int` is incremented in `handleTrackEnded` when a smart block exhausts (line 3148) and fed into the seed at `startSmartBlockEntry:1843-1847`. The map is local to each director, not persisted. Two instances both starting an executor for the same station from cold won't disagree on the first cycle (both `gen=0`), but after either restarts they diverge — instance A on its first run sees `gen=0`, instance B mid-run sees `gen=2`.

Proposed fix: persist generation counter to DB. The natural key is `(entry_id, occurrence_start)`. Add a small `smart_block_generations` table or push it into `mount_playout_state`. Read on every `startSmartBlockEntry`.

### Important — could diverge under timing

**I1. `popNextQueuedMedia` is a "first one wins" race against the DB queue.**
`internal/playout/director.go:3218-3274`. Pops the next item from `playout_queue_items` with `Clauses(clause.Locking{Strength: "UPDATE"})`. Both instances call this after a track ends. Whoever acquires the row lock first consumes it; the other instance gets an empty pop and falls through to the source-type branch.

If the queue is a user-facing "play this next" feature, both instances must see the queued track, but only one will (the loser gets `RANDOM()` or smart-block continuation instead). This produces divergent commands until the queue empties.

Proposed fix: scheduling decisions should not consume manual queue entries on both sides. Either (a) the leader pops and writes a transient "next track" row to a per-entry decision table that both instances read; or (b) treat the queue as scheduler input (leader pulls + materializes into `schedule_entries`). (a) is cheaper.

**I2. `handleTrackEnded` reads `time.Now()` for the schedule-end check.**
`internal/playout/director.go:3080`. `now.After(entry.EndsAt)` decides whether to start the next track inside the same entry or yield to the next scheduled entry. Two instances ending the same track ~hundreds of ms apart can take different branches near the slot boundary. This is the same class of timing-skew issue as C5 but observably smaller (sub-second) because GStreamer EOS fires within milliseconds of the real track end on both instances.

Mitigation: probably fine in practice if the two instances' `handleTrackEnded` fire within the 100ms `time.Sleep` window at line 3089. Worth a check via integration test before promoting to Critical.

**I3. Director `tick` reads `time.Now().UTC()` and gates entry start on it.**
`internal/playout/director.go:265`. The 250ms ticker fires on each instance independently. `entry.StartsAt.After(now)` at line 320, `now.Before(active.Ends)` at line 293, the crossfade lookahead at line 329 — all of these are wall-clock decisions made independently by each instance. On a well-synced pair (NTP, or the NetClock from Section 2.2 of the design doc) the gap is sub-10ms. But two ticks 240ms apart can put one instance on the wrong side of a slot boundary.

Mitigation: the NetClock work in `2026-06-05-netclock-engine-sync.md` addresses this for audio sample alignment, but Director still reads system `time.Now()`. Either (a) align ticks to a shared cadence anchored on NetClock, or (b) let mediaengine handle slot boundaries directly (the long-term goal per Section 3). Probably acceptable for v1 of lockstep because the edge encoder's divergence detector will catch sample-level drift.

**I4. `playMediaWithState` records `TrackEndsAt: time.Now().Add(media.Duration + grace)`.**
`internal/playout/director.go:2400, 2628, 3340, 3479, 3623`. The TrackEndsAt watchdog uses local `time.Now()` at start, which differs by up to the inter-instance clock skew. Combined with I2 this widens the divergence window for end-of-track decisions.

Fix: derive from `entry.StartsAt + (position * average_duration)` or store start time once per (entry_id, position) in DB.

**I5. `played` and `active` maps are local in-memory state.**
`internal/playout/director.go:126-127, 176-177`. Both instances independently track which entries have been started. Designed to be idempotent — both should start every entry, so divergent local state is fine in principle. Caveat: `prunePlayed` runs on local time (line 266), so the two instances may forget a `playKey` at different moments. Combined with the soft-boundary check at line 293 this is a low-probability divergence path but worth flagging.

**I6. Crossfade activation uses local `time.Now()` for `time.Until(stopAt)`.**
`internal/playout/director.go:3694`. The scheduled stop timer fires at `delay + 200ms` from when `scheduleStop` was called. Two instances calling at different times → fire at different absolute times. For the audio pipeline this matters because the crossfade window is sample-aligned by the engine, but the *decision to start* the crossfade lookahead branch in tick (lines 318-332) is based on local clock.

Same mitigation path as I3/I4.

### Minor — internal-only or low blast radius

**M1. Smart-block `collectTags` iterates a map.**
`internal/smartblock/engine.go:881-887`. Result is a `map[string]struct{}` used as a set, never iterated downstream. No leak.

**M2. `recordPlayHistory` and `publishNowPlaying` iterate `entry.Metadata`.**
`internal/playout/director.go:3761-3767`. The map is copied into a payload that goes to the event bus and DB. Map iteration order shows up in the JSON payload ordering of `play_history.metadata`, which is JSONB on Postgres. The values are equal; only key order differs. Doesn't change executor behavior; only matters if a downstream test does byte-equal on the JSON.

**M3. `Pool.executors` map iteration during `Stop()`.**
`internal/executor/pool.go:270`. Iterates `p.executors` in arbitrary order to call `Stop()`. Stops happen independently; order doesn't affect external behavior.

**M4. `emitHealthSnapshot` iterates `d.active`.**
`internal/playout/director.go:3737`. Publishes health events for each active mount in map iteration order. Only affects telemetry ordering, not playback.

**M5. `priorityEventLoop` reads from two channels via `select`.**
`internal/executor/executor.go:440-449`. If both a `priority_change` and `emergency` event arrive simultaneously, `select` picks one at random (Go spec). Two instances may handle them in different order. Outcome converges (emergency always wins via the state machine), but the intermediate gRPC call sequence differs.

Worth flagging but probably benign because emergency events should be rare enough that simultaneous priority+emergency arrival is itself rare.

**M6. `heartbeatLoop` writes `LastHeartbeat: time.Now()`.**
`internal/executor/state.go:152-156`. Writes to DB on a 5s tick. Two instances write at different times → DB row contention but no observable command divergence. The `executor_state.last_heartbeat` column will jitter, which is what it's for.

**M7. Telemetry stream non-determinism (`StreamTelemetry`).**
`internal/executor/executor.go:520-597`. Receives mediaengine telemetry on each instance separately; numbers differ because the engines are physically separate processes. Doesn't affect commands sent.

**M8. Distributor / Pool consistent hashing.**
`internal/executor/{distributor,pool}.go`. Hashing is CRC32/FNV with sorted ring nodes. Identical instance lists → identical assignments. No non-determinism here as long as the instance list is the same on both nodes (it is, since they read it from Redis leadership). Not an issue for lockstep because under lockstep both instances run all executors anyway.

## Summary by severity

| Severity  | Count |
| --------- | ----- |
| Critical  | 9     |
| Important | 6     |
| Minor     | 8     |

## Top 3 Critical findings

1. **C1 / C2 / C3: SQL `RANDOM()` everywhere.** Three separate call sites in the director use Postgres's `RANDOM()` for media selection. Postgres has no determinism guarantee here. This is the single biggest source of divergence and the cheapest to fix (hash-based deterministic selection from a stably-ordered query).

2. **C4 / C5 / C6: `time.Now()` inside `smartblock/engine.go` filters.** The engine reads the local clock for separation windows, recent-play cutoffs, and "added in last N days" filters. Two instances generating a smart block ~100ms apart can produce different sequences. Fix: thread a single `now time.Time` (derived from `entry.StartsAt`) through `GenerateRequest`.

3. **C8: `fetchCandidates` returns rows in undefined Postgres order.** Even with C4-C6 fixed, the candidate list itself is non-deterministic because the underlying query has no `ORDER BY`. Combined with unstable `sort.Slice`, any score tie produces a different selection. Fix: add `Order("id ASC")` and switch to `SliceStable` (or add `idx` as tiebreaker).

## Recommendation

**PROCEED with the lockstep architecture.**

All Critical findings are local, well-bounded code changes:
- C1–C3 are three near-identical SQL-replacement patches (~30 lines each).
- C4–C6 are a single API change to `smartblock.GenerateRequest` (add `Now time.Time`) plus three call-site updates.
- C7 is a one-line seed change.
- C8 is `Order("id ASC")` plus `sort.SliceStable` or an `idx` tiebreaker.
- C9 needs a small DB table or a column on `mount_playout_state` plus read/write at two sites.

None of these require architectural rework. None demand new infrastructure. None touch the gRPC contract. Total scope is maybe a day of focused work plus tests.

The Important findings (I1–I6) cluster around per-tick `time.Now()` reads. Those are partially mitigated by Section 2.2's NetClock work (already planned in `2026-06-05-netclock-engine-sync.md`) and fully mitigated by the edge encoder's divergence detector catching what slips through. They should not block lockstep adoption — they should ride alongside the NetClock implementation as a coordinated cleanup.

I1 (queue race) is the only one that's a genuine product-correctness bug under lockstep, not just a divergence problem. It needs a design decision regardless: should manual queue entries be a scheduler input or a per-decision shared table? Worth filing as its own follow-up.

## Issue #233 status

**Keep open.** This audit closes the "can we lockstep at all?" gate, but #233's acceptance criterion is "Multi-instance lockstep integration test passing in CI." That integration test is the right verification artifact after the Critical fixes land. Recommended sequence:

1. Land C1–C9 as a single PR (`fix(executor): make scheduling decisions deterministic for HA lockstep`). The patches don't cross-depend and should review cleanly together.
2. Add the integration test: spin up two control planes against one Postgres + one Redis, drive the same schedule, diff the gRPC command stream. The test framework already supports multi-instance setups (see `internal/executor/distributor_test.go` and `internal/scheduler/leader_aware_test.go` for patterns).
3. Close #233 when the test is green.
4. File a separate follow-up issue for I1 (queue-consumer race) and a tracking issue for I2–I6 paired with the NetClock rollout.
