# Grimnir Radio — Scheduling System Review

**Date:** 2026-03-15
**Version at review:** v1.36.5
**Scope:** Full audit of schedule creation, materialization, cleanup, orphan handling, and health reporting.

---

## 1. Architecture Overview

The scheduling system is a **three-layer pipeline**:

```
Layer 1 — Configuration
  ClockHour / ClockSlot  (DB)
  Manual ScheduleEntry   (source_type = 'smart_block', 'playlist', etc.)

Layer 2 — Materialization  (every 30 s tick, `scheduler/service.go`)
  Planner.Compile()       → SlotPlans from clock template
  materializeSmartBlock() → 30–80 individual media ScheduleEntry rows per slot
  createPlaylistEntry()   → one ScheduleEntry (resolved at playout time)
  createHardItemEntry()   → one ScheduleEntry (specific file)
  createWebstreamEntry()  → one ScheduleEntry (covers window)
  materializeDirectSmartBlockEntries() → second pass for non-clock smart_block entries

Layer 3 — Execution  (every 250 ms tick, `playout/director.go`)
  Director polls DB for entries within [now-2s, lookahead]
  Dispatches to media engine via gRPC
```

### How each source type flows

| Configured as | Becomes in DB | Resolved at |
|---|---|---|
| Clock slot → smart_block | Many `source_type='media'` entries (one per track) | Schedule time (pre-materialized) |
| Clock slot → playlist | One `source_type='playlist'` entry | Playout time (director shuffles) |
| Clock slot → hard_item | One `source_type='media'` entry | Schedule time |
| Clock slot → webstream | One `source_type='webstream'` entry (covers window) | Playout time (live stream) |
| Manual `source_type='smart_block'` entry | Many `source_type='media'` entries (second pass) | Schedule time |
| Manual `source_type='playlist'` entry | One `source_type='playlist'` entry | Playout time |
| Manual `source_type='media'` entry | Unchanged | Playout time |

### Priority / precedence between sources

There is **no explicit priority tier between source types**. The scheduler uses a first-in/first-served overlap check (`slotAlreadyScheduled`). The rules in practice are:

1. **Pre-existing entries win** — if any entry overlaps a proposed slot's time window on the same mount, the new slot is silently skipped.
2. **Clock processes before direct entries** — within each 30 s tick, clock plans run first (lines 231–282), then `materializeDirectSmartBlockEntries` runs second (line 286).
3. **Manual entries placed before a tick run implicitly "win"** — a manually placed entry (playlist, smart_block) already in DB blocks a clock from filling that window.

This means: **manual entries override clocks (correct), but the mechanism is invisible** — there is no log or UI indicator when a clock slot is suppressed by an existing entry.

---

## 2. Bugs Found

### BUG-1 · CRITICAL — `IsInstance` missing from all scheduler-generated entries except webstreams

**File:** `internal/scheduler/service.go`
**Lines affected:** `materializeSmartBlock` (540–555), `createPlaylistEntry` (583–593), `createHardItemEntry` (612–627), `createStopsetEntry` (640–653)
**Only correct entry:** `createWebstreamEntry` (689) — fixed in v1.36.5

All entries created by the scheduler from clock templates (smart_block media tracks, playlist blocks, hard items, stopsets) are stored with `is_instance = false` (the GORM default). Only webstream entries get `IsInstance: true`.

**Consequences:**

| Consequence | Mechanism |
|---|---|
| **DB grows forever** | 7-day cleanup at line 117 uses `WHERE is_instance = true` — never fires for these entries |
| **Orphan sweep bypassed for deleted smart blocks** | Media orphan sweep (line 135) requires `is_instance = true AND metadata->>'smart_block_id' IS NOT NULL` — always zero rows matched |
| **Deleted smart blocks leave orphaned media entries that never auto-clean** | See above; entries sit in DB until a hard DB purge |

A station running for 6 months with one smart block refreshing every 2 hours accumulates approximately **26,000 old media entries** that are never deleted.

**Fix:** Add `IsInstance: true` to all four creation functions.

---

### BUG-2 · HIGH — Clock slot duration defaults to 1 minute when `duration_ms` is absent

**File:** `internal/clock/compiler.go`, line 119

```go
} else {
    duration = time.Minute   // ← all non-webstream slots with no duration_ms
}
```

Webstream slots fall back to the full clock window span (correct). All other slot types — including `smart_block` and `playlist` — fall back to **1 minute**. If a clock slot's `Payload` JSON does not include `"duration_ms"`, the compiled plan has a 1-minute window and the smart block or playlist is severely truncated (only the first ~1 track materializes).

**When does this happen?**
- Imported clock templates (AzuraCast, LibreTime) may not carry `duration_ms`
- Any slot edited or created without an explicit duration field

**Fix:** The fallback for smart_block and playlist slots should be `remainingInWindow()` (same logic used for webstreams), not `time.Minute`.

---

### BUG-3 · HIGH — Hard-item and stopset slots inside a smart_block window are silently skipped

**File:** `internal/scheduler/service.go`, `slotAlreadyScheduled` (lines 422–444)

`slotAlreadyScheduled` checks for **any** entry that overlaps a slot's time window on the mount:

```go
Where("starts_at < ? AND ends_at > ?", plan.EndsAt, plan.StartsAt)
```

When a smart_block slot is processed first and its 30–80 materialized media tracks are written to DB, every subsequent slot in the same clock hour that overlaps any part of that window is suppressed. Specifically:

- A `hard_item` slot at offset 0:30 within a 2-hour smart_block window → **silently dropped**
- A `stopset` slot at offset 1:00 within the same window → **silently dropped**

The operator has no indication that their hard_item never fires. The schedule UI will not show it because it was never created.

**Correct behavior:** Slots should be ordered by offset (start time) and the smart_block engine should be told its true available duration (time until next fixed slot), not the full clock-window duration.

**Workaround today:** Arrange clock slots so they do not overlap: smart_block 10:00–10:55, hard_item 10:55–11:00, smart_block 11:00–12:00. Do not place a single 2-hour smart_block in the same window as any other slot.

---

### BUG-4 · MEDIUM — Competing direct smart_block + clock can create overlapping materialized entries

**File:** `internal/scheduler/service.go`

When:
1. A clock slot at 10:00–12:00 uses smart_block A → scheduler materializes block A into 30 tracks (done in the clock-plans pass, lines 231–282)
2. A manually placed `source_type='smart_block'` entry also targets 10:00–12:00 using smart_block B

During the second pass (`materializeDirectSmartBlockEntries`, line 286), the deduplication check at line 322 only looks for media entries with `metadata->>'smart_block_id' = B`. Since block A's entries have `smart_block_id = A`, it finds zero matches → block B is also materialized → **60 overlapping tracks in the same window**.

The director will see multiple entries starting at nearly the same time and may attempt to play both, or skip one unpredictably.

**Fix:** `materializeDirectSmartBlockEntries` must call `slotAlreadyScheduled` (or its equivalent) before materializing, just like the clock-plans pass does.

---

### BUG-5 · MEDIUM — Deleting a media item leaves hard-item schedule entries as silent orphans

**File:** media deletion handler; `internal/scheduler/service.go` orphan sweep

The orphan sweep covers webstream, smart_block, and playlist sources. It **does not** check if `source_type = 'media'` entries with `is_instance = false` (i.e., hard-item clock entries or manually placed media entries) still reference a valid media item.

Deleting a media item that is hard-coded in a clock template results in:
- The schedule entry remains with `source_type = 'media'`
- The director will attempt to play it → file-not-found error → silence
- The entry will not appear as "orphaned" in the schedule health view (orphan detection in `pages_schedule.go` only checks webstreams, smart_blocks, and playlists)

**Fix:**
1. Cascade delete future `schedule_entries` where `source_type = 'media' AND source_id = deleted_id AND starts_at > NOW()` on media item deletion.
2. Add a media orphan check to the hourly sweep (analogous to the webstream check).
3. Extend orphan detection in the schedule calendar view to include `source_type = 'media'` entries.

---

### BUG-6 · MEDIUM — Deleting a mount leaves all its schedule entries orphaned indefinitely

**File:** mount deletion handler (not found in cascade audit)

Schedule entries are keyed by `mount_id`. There is no cascade delete or orphan sweep that removes schedule entries when a mount is deleted. The director will attempt to dispatch to the deleted mount → error → silence. The entries never clean up.

**Fix:** Add cascade delete of schedule entries (and possibly play history) when a mount is deleted.

---

### BUG-7 · LOW — `slotAlreadyScheduled` suppresses clock slots silently with no log or UI feedback

**File:** `internal/scheduler/service.go`, lines 239–246

```go
if alreadyScheduled {
    continue   // ← no log, no metric, no UI indicator
}
```

When a slot is suppressed because something already occupies its window, the operator has no way to know. The schedule UI shows only what was created — the suppressed slot is invisible.

**Fix:** Log at `Debug` level (at minimum) with slot ID, station, and reason. Ideally surface in the schedule health report as a "skipped slot" diagnostic.

---

## 3. Smart Blocks as First-Class Scheduling Objects

**Current status: Partially first-class.**

✅ Supported and working:
- Smart blocks can be placed directly on the calendar without any clock template
- `materializeDirectSmartBlockEntries` handles this second pass correctly
- Constraint relaxation (4 levels), fallback chains, loop-to-fill, bumper tracks all work
- Deterministic seeding (reproducible sequences per start time)
- Emergency random-track fallback when smart block is unresolved

⚠️ Limitations:
- Smart blocks are **materialized at schedule time**, not at playout time. The track sequence is frozen up to 24 hours ahead. Changes to a smart block's rules don't affect already-materialized entries.
- There is **no way to force re-materialization** of an already-scheduled smart block window short of deleting entries manually.
- Smart block entries do **not** set `is_instance = true` (BUG-1) — cleanup does not work.
- A smart block with no valid candidates and no fallback produces **zero entries + no schedule coverage** → potential dead air.

---

## 4. Timing Math

| Component | Value | Config |
|---|---|---|
| Scheduler tick | 30 seconds | Hard-coded |
| Lookahead window | 24 hours default | `New(lookahead)` param, configurable |
| Director poll | 250 ms | Hard-coded |
| Schedule cache TTL (director) | 2 seconds | Hard-coded |
| Old-entry cleanup | Once per hour, >7 days old | Hard-coded |
| Orphan sweep | Once per hour | Same pass as cleanup |
| Mount state prune (restart) | >6 hours old | Hard-coded |
| Track position flush (crash recovery) | ~15 seconds | Hard-coded |

**Note:** CLAUDE.md mentions "48h rolling window" — this does not match the code. The default lookahead is `24 * time.Hour` (service.go line 45). The "48h" is an informal description; the actual DB-query horizon is `start + s.lookahead`.

---

## 5. Cleanup & Orphan Sweep — Current Coverage

| Scenario | Covered? | Mechanism |
|---|---|---|
| Old materialized entries (webstream) | ✅ Yes | `is_instance=true` 7-day cleanup |
| Old materialized entries (smart_block media, playlist, hard_item) | ❌ No | `is_instance=false` → cleanup never fires |
| Future entries for deleted webstream | ✅ Yes | Orphan sweep + cascade delete (v1.36.5) |
| Future entries for deleted smart_block | ✅ Partial | Orphan sweep catches `source_type='smart_block'`; does NOT catch `source_type='media'` entries (BUG-1) |
| Future entries for deleted playlist | ✅ Yes | Orphan sweep + cascade delete (v1.36.5) |
| Future entries for deleted clock | ✅ Yes | Cascade delete (v1.36.5) |
| Future entries for deleted hard_item media | ❌ No | No cascade, no sweep |
| Future entries for deleted mount | ❌ No | No cascade, no sweep |
| Ghost clock_hours from imports | ✅ Fixed | Manually deleted in v1.36.5 investigation; no preventive check yet |

---

## 6. Schedule Health Report

The schedule calendar view (`pages_schedule.go`) detects orphaned entries inline (loading all sources and checking existence). This adds DB queries per calendar render.

**Current orphan detection:**
- webstream: checks `webstreams` table ✅
- smart_block: checks `smart_blocks` table ✅
- playlist: checks `playlists` table ✅
- media (hard_item): **not checked** ❌
- mount existence: **not checked** ❌

**`health` field coloring logic:**
- `red`: orphaned source
- `yellow`: emergency fallback active, constraint relaxed, or runtime mismatch
- `green`: all good (default)

The health field correctly detects constraint-relaxed blocks and emergency fallbacks **only if** those flags are stored in `Metadata` at materialization time. Review whether `materializeSmartBlock` stores these flags — current code (lines 548–553) stores `intro_end`, `outro_in`, `energy`, `smart_block_id` but **does not store `constraint_relaxed` or `emergency_fallback`**. The yellow health indicator for these cases can never fire.

---

## 7. Summary Table

| Issue | Severity | Status | File(s) |
|---|---|---|---|
| BUG-1: `IsInstance=true` missing from scheduler entries | **CRITICAL** | Open | `scheduler/service.go` |
| BUG-2: Clock slot defaults to 1-minute when `duration_ms` missing | **HIGH** | Open | `clock/compiler.go` |
| BUG-3: Hard-item/stopset silently skipped inside smart_block window | **HIGH** | Open | `scheduler/service.go` |
| BUG-4: Duplicate entries from competing clock + direct smart_block | **MEDIUM** | Open | `scheduler/service.go` |
| BUG-5: Media item deletion leaves hard-item schedule entries orphaned | **MEDIUM** | Open | media handler + sweep |
| BUG-6: Mount deletion leaves schedule entries orphaned | **MEDIUM** | Open | mount handler |
| BUG-7: Suppressed clock slots have no log or UI feedback | **LOW** | Open | `scheduler/service.go` |
| `constraint_relaxed`/`emergency_fallback` flags not stored → health always green | **MEDIUM** | Open | `scheduler/service.go` |
| Ghost `clock_hours` from imports (AzuraCast/LibreTime) | **MEDIUM** | Mitigated by orphan sweep | import pipeline |
| Schedule calendar orphan check missing for `source_type='media'` | **LOW** | Open | `web/pages_schedule.go` |

---

## 8. Recommended Fix Order

1. **BUG-1** — Add `IsInstance: true` to all four scheduler entry-creation functions. Small change, massive impact on DB hygiene and orphan sweep correctness.
2. **BUG-2** — Fix default duration fallback to use clock window span instead of 1 minute for smart_block and playlist slots.
3. **BUG-5** — Cascade delete from media item deletion; add media-orphan check to sweep; extend calendar orphan detection.
4. **BUG-3** — Redesign slot processing so hard_item/stopset offsets cut the smart_block duration rather than being skipped.
5. **BUG-4** — Add `slotAlreadyScheduled` check inside `materializeDirectSmartBlockEntries`.
6. **BUG-6** — Cascade delete on mount deletion.
7. Store `constraint_relaxed` / `emergency_fallback` flags in entry Metadata.
8. **BUG-7** — Add debug-level log for suppressed slots.

---

## 9. Secondary Impact Analysis — BUG-1 Fix Cascade

A follow-up deep-impact review of adding `IsInstance: true` to scheduler entries found three additional issues that must be fixed **in the same PR** or the `IsInstance` change will break the UI.

### BUG-1a · HIGH — Calendar edit modal misfires on scheduler entries

**File:** `internal/web/templates/pages/dashboard/schedule/calendar.html`, line 3467

```javascript
if (isRecurring || isInstance) {
    recurringNotice.classList.remove('d-none');   // shows "edit this occurrence or all?" dialog
    document.getElementById('editModeAll').checked = true;
    document.getElementById('editRecurrenceSection').classList.remove('d-none');
}
```

`isInstance` is `props.is_instance`. After adding `IsInstance: true` to scheduler entries, clicking any smart block track, playlist block, hard item, or stopset in the schedule calendar would show the recurring-event edit modal ("This is a recurring event — do you want to edit all future occurrences?"). These entries are not instances of recurring rules; they are scheduler-generated concrete entries.

**Root cause:** The JS condition was written when `is_instance=true` meant only "saved override to a recurring rule." It needs a second signal to distinguish the two cases.

**Fix:** Add `"recurrence_parent_id"` (string or empty string) to the `Extendedprops` map in the `ScheduleEvents` handler (`pages_schedule.go` lines 701–722). Update the JS condition to:
```javascript
if (isRecurring || (isInstance && props.recurrence_parent_id)) {
```
Scheduler entries have no `RecurrenceParentID` so `props.recurrence_parent_id` is empty/null — the modal does not fire. True recurring overrides have `RecurrenceParentID != nil` — the modal fires correctly.

---

### BUG-1b · HIGH — `scheduleStatusForPreview` and calendar health badge label scheduler entries as "Saved Override"

**File:** `internal/web/pages_schedule.go`, lines 103 and 696

```go
// line 103
if entry.IsInstance && !isVirtualRecurringInstance(entry) {
    return "override", "Saved Override", "..."
}

// line 696
} else if statusLabel == "" && entry.IsInstance && !isVirtualRecurringInstance(entry) {
    statusLabel = "Saved Override"
```

`isVirtualRecurringInstance()` returns `false` for scheduler entries (they have no `RecurrenceParentID`). After adding `IsInstance: true`, every scheduler-generated entry would be classified as "Saved Override" in the schedule preview and the calendar health colouring. The status chip, tooltip, and health badge would all show incorrect information.

**Fix:** Add `&& entry.RecurrenceParentID != nil` to both conditions:
```go
if entry.IsInstance && entry.RecurrenceParentID != nil && !isVirtualRecurringInstance(entry) {
```

---

### BUG-1c · MEDIUM — Operator Confidence "Is Override" badge misfires; sort priority wrong

**File:** `internal/web/pages_dashboard.go`, lines 675–676 and 732

```go
// Sort: is_instance=true entries always sort first
if entries[i].IsInstance != entries[j].IsInstance {
    return entries[i].IsInstance
}

// IsOverride directly mirrors IsInstance — drives yellow "override" badge
IsOverride: entry.IsInstance,
```

After the fix, ALL scheduler entries have `IsInstance: true`. The sort no longer distinguishes "user-created recurring override" from "scheduler-generated entry" — both sort equally, falling through to a start-time comparison. More importantly, `IsOverride: entry.IsInstance` unconditionally fires the yellow "override" badge in the Operator Confidence widget for every scheduler-generated entry.

**Fix:**
```go
// Sort: prefer true recurring overrides (RecurrenceParentID set) over plain scheduler instances
if entries[i].IsInstance != entries[j].IsInstance {
    return entries[i].IsInstance
}
isIOverride := entries[i].IsInstance && entries[i].RecurrenceParentID != nil
isJOverride := entries[j].IsInstance && entries[j].RecurrenceParentID != nil
if isIOverride != isJOverride {
    return isIOverride
}

// IsOverride badge: only real recurring-event overrides
IsOverride: entry.IsInstance && entry.RecurrenceParentID != nil,
```

---

### BUG-1d · MEDIUM — Orphan sweep backlog: old `is_instance=false` entries never cleaned

After adding `IsInstance: true` to new entries, the existing media orphan sweep (line 135) still requires `is_instance = true`:

```sql
DELETE FROM schedule_entries WHERE source_type = 'media' AND starts_at > NOW()
  AND is_instance = true AND metadata->>'smart_block_id' IS NOT NULL
  AND metadata->>'smart_block_id' NOT IN (SELECT id::text FROM smart_blocks)
```

All smart block media entries created before this fix have `is_instance = false`. If their smart block is deleted, this sweep misses them — they sit as orphaned future entries driving dead air. The fix is to **remove the `is_instance = true` requirement** from this sweep so it targets all smart-block-derived media entries regardless of when they were created:

```sql
DELETE FROM schedule_entries WHERE source_type = 'media' AND starts_at > NOW()
  AND metadata->>'smart_block_id' IS NOT NULL
  AND metadata->>'smart_block_id' NOT IN (SELECT id::text FROM smart_blocks)
```

---

### BUG-1e · LOW — `pickRandomTrack` (emergency fallback) missing `IsInstance: true`

**File:** `internal/scheduler/service.go`, lines 755–766

Emergency fallback entries created by `pickRandomTrack()` when a smart block completely fails have no `IsInstance: true`. These are scheduler-generated ephemeral entries and should be subject to the same 7-day cleanup. The `emergency_fallback` metadata key is already set, so they are recognisable, but the cleanup query (`WHERE is_instance = true`) will never catch them without the flag.

**Fix:** Add `IsInstance: true` to the `ScheduleEntry` literal in `pickRandomTrack`.

---

### Complete BUG-1 fix inventory (all 13 touch points)

| # | File | Line(s) | Change |
|---|---|---|---|
| 1 | `scheduler/service.go` | `materializeSmartBlock` ~540 | add `IsInstance: true` |
| 2 | `scheduler/service.go` | `createPlaylistEntry` ~583 | add `IsInstance: true` |
| 3 | `scheduler/service.go` | `createHardItemEntry` ~612 | add `IsInstance: true` |
| 4 | `scheduler/service.go` | `createStopsetEntry` ~640 | add `IsInstance: true` |
| 5 | `scheduler/service.go` | `pickRandomTrack` ~755 | add `IsInstance: true` |
| 6 | `scheduler/service.go` | orphan sweep media query ~135 | remove `AND is_instance = true` |
| 7 | `web/pages_schedule.go` | line 103 | add `&& entry.RecurrenceParentID != nil` |
| 8 | `web/pages_schedule.go` | line 696 | add `&& entry.RecurrenceParentID != nil` |
| 9 | `web/pages_schedule.go` | `Extendedprops` map ~715 | add `"recurrence_parent_id"` key |
| 10 | `web/pages_dashboard.go` | line 732 | `IsOverride: entry.IsInstance && entry.RecurrenceParentID != nil` |
| 11 | `web/pages_dashboard.go` | sort ~675 | add `RecurrenceParentID != nil` tier to sort |
| 12 | `web/templates/.../calendar.html` | line 3467 | `if (isRecurring \|\| (isInstance && props.recurrence_parent_id))` |
| 13 | `internal/version/version.go` | — | bump to `1.36.6` |

All 13 changes must ship together. Applying only the `service.go` changes without the UI guards will produce incorrect badges, wrong edit modals, and misleading status labels across the schedule and dashboard pages.

---

## 10. Updated Summary Table

| Issue | Severity | Status | File(s) |
|---|---|---|---|
| BUG-1: `IsInstance=true` missing from scheduler entries | **CRITICAL** | Open | `scheduler/service.go` |
| BUG-1a: Calendar edit modal misfires on scheduler entries | **HIGH** | Open (blocks BUG-1 fix) | `calendar.html` |
| BUG-1b: "Saved Override" label on all scheduler entries | **HIGH** | Open (blocks BUG-1 fix) | `pages_schedule.go` |
| BUG-1c: "Override" badge + wrong sort in Operator Confidence | **MEDIUM** | Open (blocks BUG-1 fix) | `pages_dashboard.go` |
| BUG-1d: Orphan sweep misses pre-fix `is_instance=false` entries | **MEDIUM** | Open (part of BUG-1 fix) | `scheduler/service.go` |
| BUG-1e: `pickRandomTrack` missing `IsInstance=true` | **LOW** | Open (part of BUG-1 fix) | `scheduler/service.go` |
| BUG-2: Clock slot defaults to 1-minute when `duration_ms` missing | **HIGH** | Open | `clock/compiler.go` |
| BUG-3: Hard-item/stopset silently skipped inside smart_block window | **HIGH** | Open | `scheduler/service.go` |
| BUG-4: Duplicate entries from competing clock + direct smart_block | **MEDIUM** | Open | `scheduler/service.go` |
| BUG-5: Media item deletion leaves hard-item schedule entries orphaned | **MEDIUM** | Open | media handler + sweep |
| BUG-6: Mount deletion leaves schedule entries orphaned | **MEDIUM** | Open | mount handler |
| BUG-7: Suppressed clock slots have no log or UI feedback | **LOW** | Open | `scheduler/service.go` |
| `constraint_relaxed`/`emergency_fallback` flags not stored → health always green | **MEDIUM** | Open | `scheduler/service.go` |
| Ghost `clock_hours` from imports (AzuraCast/LibreTime) | **MEDIUM** | Mitigated by orphan sweep | import pipeline |
| Schedule calendar orphan check missing for `source_type='media'` | **LOW** | Open | `web/pages_schedule.go` |

---

## 11. Updated Fix Order

1. **BUG-1 + BUG-1a–e together** — all 13 touch points in one PR. Cannot be split; the `service.go` change breaks the UI without the guards.
2. **BUG-2** — Fix default duration fallback to use clock window span.
3. **BUG-5** — Cascade delete from media item deletion; add media-orphan check; extend calendar orphan detection.
4. **BUG-3** — Redesign slot processing so hard_item/stopset offsets constrain smart_block duration.
5. **BUG-4** — Add `slotAlreadyScheduled` check inside `materializeDirectSmartBlockEntries`.
6. **BUG-6** — Cascade delete on mount deletion.
7. Store `constraint_relaxed` / `emergency_fallback` flags in entry Metadata.
8. **BUG-7** — Add debug-level log for suppressed slots.

---

*Report updated 2026-03-15 after secondary impact analysis of BUG-1 fix cascade.*
