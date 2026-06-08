# Watchdog port + #222 fix (v2.0.0-rc.5)

**Date:** 2026-06-08
**Branch:** v2-dev
**Predecessor commit:** 8701471

This pass closes two gaps in the v2.0 release path: porting the webstream
stall watchdog out of v1.40.9 (which lives on `fix/webstream-stall-watchdog`
off `main`, & is open as PR #241), & fixing the recalc-not-registering
behavior reported in issue #222.

## Part 1 — Best-practice notes

### Task A: porting a hot fix between long-lived branches

Three moves are on the table when `branch-X` has a needed fix & `branch-Y`
has diverged: cherry-pick (one commit at a time, clean history), merge
(folds branch-X wholesale into branch-Y), or manual re-apply (read the
diff & re-implement against the divergent target). The pick depends on
two questions: (1) how many commits is the fix? & (2) does the source
branch carry anything else you don't want? If the answer is "few commits,
& no other unrelated work on the source branch", cherry-pick wins; the
history stays linear & the conflict surface is small. Merge wins when the
source branch is a long-running feature line where you want every later
commit to auto-merge cleanly. Manual re-apply wins when the target's code
in the affected region has diverged enough that a cherry-pick would
require so many resolutions that you'd be re-implementing anyway.

### Task B: fixing an "X not registering" bug

The trap with this class of bug is that "doesn't register" almost never
means "the click event vanished into the DOM"; it usually means the
backend ran successfully, returned a result the user couldn't interpret,
& the UI showed it in a form that looked like nothing happened. Read the
bug report, find what "register" means (a DB write, a queue insert, an
event, an HTMX swap), then reproduce the failure in a test before
touching the code. The test must demonstrate the user-visible symptom,
not just exercise the function. Apply the smallest fix that makes the
test pass & confirm related paths still work.

## Part 2 — Watchdog port

**Strategy chosen: cherry-pick.** The three watchdog commits sit on top
of `307a67e` (v1.40.8), which is also v2-dev's merge-base with `main`.
broadcast/server.go has zero divergence between v2-dev & main, so the
first commit is a no-conflict apply. director.go has 467 lines added on
the v2-dev side from the HA work (#227 sweepOverrunEntries, NetClock,
PCM-RTP), but none of those changes touch `watchWebstreamPipeline` or
`startWebstreamEntry`; the watchdog hook points are intact.

Result: both cherry-picks applied with no manual resolution. The second
commit reported `Auto-merging internal/playout/director.go` because of
line-shift around the trackWatchdogGrace constant block, but git's 3-way
merge resolved it correctly.

**Commits applied to v2-dev:**
- `b08748e` — Add BytesReceivedAt to broadcast.Mount for stall detection
  (cherry-pick of `a0431c3`)
- `3487545` — Add webstream stall watchdog to detect silent zombie
  pipelines (cherry-pick of `e1f836e`)

The original v1.40.9 version-bump commit (`776b076`) was deliberately
skipped; it tags v1.40.9 on the main line, & we're tagging v2.0.0-rc.5
here instead.

**Regression tests verified passing on v2-dev:**
- `TestMount_BytesReceivedAt_ZeroBeforeFeed`
- `TestMount_BytesReceivedAt_UpdatedAfterFeed`
- `TestWatchWebstreamPipeline_StallWatchdogStopsPipeline`
- `TestWatchWebstreamPipeline_StallWatchdogExitsOnPipelineDone`

## Part 3 — Fix for #222

**Root cause:** `MediaReanalyzeDurations` filtered out media that already
had a pending or running analysis job, then rendered a generic "No media
queued (already pending/running or no media found)" alert in the empty
state. When a previous analyzer batch had died mid-run leaving jobs
stuck in `pending`/`running` forever, the next click hit zero new media,
rendered the empty alert, & gave the operator no signal that 47 stuck
jobs were the actual blocker. From the operator's seat it looked like
the button didn't register.

**Fix:** Two changes in `internal/web/pages_media.go`:

1. `countStuckAnalysisJobs(stationID)` returns the count of pending /
   running jobs whose `updated_at` is older than 1 hour, along with the
   oldest such job's timestamp. The empty branch of
   `MediaReanalyzeDurations` calls it & surfaces both via the rendered
   partial.
2. New handler `MediaReanalyzeDurationsResetStuck` (route:
   `POST /dashboard/media/reanalyze-durations/reset-stuck`) marks every
   stale (>1h) pending/running job for the station as `failed` with a
   "reset by operator" error message. Gated on `edit_metadata`
   permission. Never touches jobs younger than the cutoff, so a busy
   analyzer running real work doesn't get killed.

The empty-state partial `duration-recalc-empty.html` now renders the
stuck-count, oldest timestamp, & a Reset button that posts to the new
endpoint.

**Regression tests (TDD red→green):**
- `TestMediaReanalyzeDurations_StuckJobs_SurfacesCountInPartial`
- `TestMediaReanalyzeDurations_ResetStuckJobs_OnlyAffectsStale`
- `TestMediaReanalyzeDurations_ResetStuckJobs_RequiresEditMetadata`

All three pass; the original four `TestMediaReanalyzeDurations_*` tests
still pass too (no regression).

## Commits & tag

- `b08748e` — cherry-pick: BytesReceivedAt on broadcast.Mount
- `3487545` — cherry-pick: webstream stall watchdog
- `fc692c8` — #222: stuck-job count + reset endpoint
- (next) version bump to 2.0.0-rc.5 & tag

## Issue closures

- #208, #209, #210, #211, #212, #214 — closed; fix shipped via the
  cherry-pick of `e1f836e` on v2-dev, originally PR #241 on main.
  Regression test: `TestWatchWebstreamPipeline_StallWatchdogStopsPipeline`
  in `internal/playout/director_webstream_test.go`.
- #222 — closed; fix shipped in `fc692c8`. Regression tests above.
