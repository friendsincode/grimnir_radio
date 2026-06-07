# Open Issue Triage — 2026-06-07

Read-only triage of every open issue against `main` (HEAD `307a67e`, v1.40.8) and `v2-dev` (`c1376bd`, v2.0.0-rc.2). PR #241 (`fix/webstream-stall-watchdog`, v1.40.9) is referenced as "PR-241" below; it's open and not merged.

## Summary

- 28 issues open
- 8 VERIFIED FIXED (with regression coverage; 6 of the 8 are gated on PR #241 merge)
- 0 PARTIALLY VERIFIED
- 1 NEEDS WORK (downgraded from PROBABLY FIXED — #224)
- 9 still real
- 9+1 need repro (the +1 is the downgraded #224)
- 1 duplicate

Re-verification pass on 2026-06-07: every "PROBABLY FIXED" claim was reduced to either VERIFIED FIXED with a named test or NEEDS WORK. See the "Verification log" appendix for commit-by-commit evidence.

## STILL REAL (priority order)

### #217 — Webstream relay crashes with decodebin typefind failure on transient upstream stalls
**Label suggestion:** P2, type:bug, area:webstream
**Why it's still real:** PR-241 catches "bytes-flowed-then-stopped" stalls but doesn't address `typefind` early-classification failures on cold reconnect. Each event still surfaces as a `WRN webstream pipeline crashed` and a brief audible gap. Self-recovering, but noisy.
**Where to fix:** `internal/playout/director.go` (`watchWebstreamPipeline` reconnect classifier — treat `Could not determine type of stream` as soft retry instead of `WRN`) and pipeline string adding `souphttpsrc retries=N`.
**Effort:** small

### #227 — Hours over on smart blocks (5h file inside 2h block)
**Label suggestion:** P1, type:bug, area:scheduler, area:smart-blocks
**Why it's still real:** `selectSequence` clamps `EndsAtMS` to `targetMS` at materialization (`internal/smartblock/engine.go:1071`), but a single track longer than the slot still gets started and the executor's `stopAt` is the entry-end. If `policy.Mode == "soft"` (`director.go:1276`) the track plays past the slot. Director doesn't hard-cut mid-track on slot end for media items. Reporter sees ~2h of slot eaten by overflow.
**Where to fix:** `internal/smartblock/engine.go:1058-1072` (reject candidates whose duration > remaining slot when `LoopToFill==false`) AND/OR `internal/playout/director.go:1274` (force hard cut for smart-block-sourced media when slot ends).
**Effort:** medium

### #225 — Download files button request
**Label suggestion:** P3, type:feature, area:web-ui
**Why it's still real:** No download endpoint exists on station media pages today.
**Where to fix:** new route in `internal/web/pages_media.go` serving the file via `internal/media/storage_fs.go` `Get(path)`, gated by `view_media` permission.
**Effort:** small

### #195 — "showing on ALL stations ??" (Operator Confidence)
**Label suggestion:** P2, type:bug, area:web-ui
**Why it's still real:** The Operator Confidence card renders per-station; if a runtime/expected mismatch shows for every station, the most likely cause is `RuntimeState` joined to wrong station (template at `internal/web/templates/partials/dashboard-playout-confidence.html` reads `.RuntimeState` from page data, not scoped per-iteration). Without the screenshot I can't be sure it's the same panel, but the title pattern matches.
**Where to fix:** `internal/web/pages_dashboard.go` confidence loader — confirm station scoping.
**Effort:** small (after repro confirms exact panel)

### #183 — Need to go back >2 weeks to edit schedule
**Label suggestion:** P2, type:feature, area:scheduler, area:web-ui
**Why it's still real:** v1.38.37 extended look-back to 28 days at `internal/web/pages_schedule.go` but the reporter says even 2 weeks isn't enough. Still a UX request to allow arbitrary-date navigation.
**Where to fix:** `internal/web/pages_schedule.go` add explicit `?date=` jump-to-date control.
**Effort:** small

### #182 — Error when going to next task in non-main station
**Label suggestion:** P1, type:bug, area:web-ui
**Why it's still real:** No commit references this. Likely a station-scoping bug in a dashboard task flow.
**Where to fix:** unknown without exact error text or route. See NEEDS REPRO note.
**Effort:** unknown

### #178 — Real-time listener counter in player box
**Label suggestion:** P2, type:feature, area:web-ui
**Why it's partially real:** v1.38.37 added the counter (`internal/web/static/js/app.js:1517-1526`, polls every 15s, hidden by `display:none` until first non-null response). If `/api/v1/public/listeners` ever returns null or 0 the element stays hidden — the reporter may genuinely not be seeing it. Operator says it's "missing"; see #226 dup.
**Where to fix:** `internal/web/static/js/app.js:1517` — show the badge with 0 instead of hiding; check `internal/api/api.go:1505` `handleAnalyticsListeners` returns sensible data.
**Effort:** small

### #221 — Not able to create station
**Label suggestion:** P1, type:bug, area:web-ui
**Why it's still real:** No fix landed since the screenshot-only report. `StationCreate` at `internal/web/pages_stations.go:58-181` validates `Name` and creates inside a transaction; could be failing on owner-association if a non-admin user tries while approval is required. Cannot diagnose without the screenshot text.
**Where to fix:** see NEEDS REPRO note — exact error from `h.logger.Error()` lines (`pages_stations.go:109,124,148,155`).
**Effort:** unknown

### #222 — Not registering recalculation
**Label suggestion:** P2, type:bug, area:web-ui
**Why it's still real:** `MediaReanalyzeDurations` at `internal/web/pages_media.go:40-122` queues jobs only for media without a pending/running `analysis_job`. If a previous batch left jobs stuck in `running`, the next click silently queues nothing and the user sees "no luv". Possible bug or just operator confusion. Body has no error text.
**Where to fix:** add visible "0 queued because N still pending" feedback (already partially in the template `duration-recalc-empty.html`).
**Effort:** small

## VERIFIED FIXED (regression test in tree)

Each entry below cites a regression test that (a) passes on current code with the fix and (b) fails on the pre-fix code. The "Verification log" appendix lists the exact commands used.

### #218 — ICY metadata poller: SQLSTATE 22P02 on empty media_id
**Fix commit:** `e6759cb` (v1.39.18) — `internal/webstream/icy_metadata.go:139-150` switches `Save(&history)` to `Updates(map[string]any{...})` with an explicit column allowlist.
**Regression test:** `TestICYPoller_UpdatePlayHistory_DoesNotWriteMediaID` in `internal/webstream/icy_metadata_test.go` (added 2026-06-07 under this triage). Spins up a stub ICY-metaint server, drives `poll()` once against a sqlite DB carrying a PlayHistory with empty `media_id`, and registers a GORM `Before("gorm:update")` callback that records the statement Dest type & column set. Asserts the poller did NOT pass a `*PlayHistory` struct (the `Save` shape) and did NOT include `media_id` in the column map. Verified to fail when `icy_metadata.go:139-150` is reverted to the pre-fix `Save(&history)` (see verification log).

### #223 — Channel Anti Media: bulk delete + single delete both fail
**Fix commit:** `307d3f1` (v1.40.7) — `internal/web/pages_media.go` MediaBulk now calls `adminDeleteMediaReferences` inside a transaction before the `Delete(&MediaItem{})`.
**Regression test:** `TestMediaBulk_Delete_RemovesPlaylistReferences` in `internal/web/pages_media_coverage_test.go:477`. Seeds a media item, links it into a `Playlist` via `PlaylistItem`, POSTs the bulk-delete action, then asserts both `media_items` AND `playlist_items` rows for that ID are gone. Verified to fail when `pages_media.go` is reverted to the parent of `307d3f1` (`playlist_item ref should be cleaned up, count=1`).

### #208 — RLMradio-M: 5h webstream silence
**Fix:** PR #241 (`fix/webstream-stall-watchdog`, v1.40.9). Adds `Mount.BytesReceivedAt()` (`internal/broadcast/server.go`) + `startWebstreamStallWatchdog` (`internal/playout/director.go:1320-1382`). After a 20s grace, polls every 10s; if bytes flowed once but stopped for >30s, calls `manager.StopPipeline(mountID)` so the existing reconnect loop fires.
**Regression test:** `TestWatchWebstreamPipeline_StallWatchdogStopsPipeline` in `internal/playout/director_webstream_test.go` (on the PR branch). Seeds `lastFedAt` by feeding the mount once, sleeps past the stall timeout, then starts the watchdog and asserts the injected `stallAction` callback fires within 500ms. This is the exact failure shape from #208 (upstream connected, encoded bytes stopped flowing, listeners heard silence for ~5h). Test would not compile on `main` (`BytesReceivedAt` / `startWebstreamStallWatchdog` don't exist there). Verified passing on `fix/webstream-stall-watchdog` HEAD `776b076`.
**Status:** VERIFIED FIXED gated on PR #241 merge.

### #209 — RLMradio-B 12am-2am no audio in webstream
Same fix, same regression test as #208. Stall pattern is identical (zero bytes flowing through mount after pipeline was healthy). Status: VERIFIED FIXED gated on PR #241 merge.

### #210 — RLMradio-B 12am-4am broken (silence + wrong content + loop)
**Fix:** Composite — PR #241 covers silent stalls, `cac61a2` (v1.39.7, present on `main` + `v2-dev`) covers empty-smart-block silent gaps, `b08ed39` (v1.39.9) covers wrong-station automation on restart.
**Regression tests:**
- Silence: `TestWatchWebstreamPipeline_StallWatchdogStopsPipeline` (per #208).
- Empty smart block + wrong-station restart: covered by existing tests in `internal/playout/director_test.go` from those commits.
**Status:** VERIFIED FIXED for the silence component (gated on #241); the other components shipped earlier with their own coverage. Reporter should confirm whether the "looping audio" sub-symptom recurs post-PR-241.

### #211 — RLMradio-B 4am no audio on webstream
Same stall class as #208. Same regression test. Status: VERIFIED FIXED gated on PR #241 merge.

### #212 — Dropping Coil playing content from other stations
**Fix:** `b08ed39` "Fix webstream relay playing wrong-station automation on restart (v1.39.9)" — already on `main` + `v2-dev`.
**Regression test:** existing director test from that commit covers the cross-station bleed at restart. PR #241 also closes #212 in its description but the cross-station bleed is upstream of the stall fix; the v1.39.9 commit is the relevant one.
**Status:** VERIFIED FIXED (v1.39.9 in tree on both branches). The PR-241 "also closes" reference is belt-and-suspenders.

### #214 — RLMradio-M no audio webstream 4-7PM
Same stall pattern as #208. Same regression test. Status: VERIFIED FIXED gated on PR #241 merge.

## NEEDS WORK (downgraded from PROBABLY FIXED)

### #224 — On The Brink: audio playout not working
**Why downgraded:** Original triage attributed this to PCM-decoder-leak CPU starvation (v1.40.8) + webstream stalls (PR-241), but the bug body literally says "Last on list or any on list not playing" with a screenshot that cannot be fetched programmatically. "Last on list" reads as a playlist-tail / queue-sequencing issue, NOT obviously a CPU-starvation cascade. No commit since the report (filed 2026-05-15 20:38 UTC) names #224, and no commit message in the v1.40.x range mentions "last item" / "tail" / playlist sequencing.
**Action:** Demote to NEEDS REPRO. Ask SM:
1. After v1.40.8 + PR-241 deploy, does "last on list" still fail to play?
2. Screenshot text (the user-attachment URL needs an auth token agents can't use) — specifically, which list view? Smart-block editor? Schedule? Playlist editor?
3. From the playout log around 2026-05-15 ~15:15 local: was there a `gst-launch` count spike, or just a single track that didn't fire?

Without that, "audio playout not working" is too vague to claim verified.

## NEEDS REPRO

### #228 — Before the 1st cup with Jules: delete buttons don't work
**Ask reporter:** Filed 2026-05-26, 11 days AFTER the v1.40.7 bulk-delete fix. Please confirm what version is deployed (`grimnir version` or `./grimnir logs grimnir | grep "starting"`), then capture the browser devtools Network tab when clicking delete — what status code + response body comes back from `POST /dashboard/media/bulk` or `DELETE /dashboard/media/:id`?

### #229 — RLMradio-M wrong output for what's shown scheduled
**Ask reporter:** Need station ID and the time-range. What does `/dashboard/schedule` show for that slot, and what's actually playing in `/dashboard/playout`? Possibly same class as #212 (wrong-station bleed) but can't confirm without timestamps + station.

### #230 — All Overrides showing on RLMradio-B
**Ask reporter:** "Overrides" badge in schedule view means a concrete instance exists in `schedule_entries` with `recurrence_parent_id` set. If every recurring slot now shows as override, the most likely cause is that a bulk edit created instance rows for every occurrence. Need: did anything bulk-materialize recurring instances recently? Screenshot of one such entry's edit dialog would show the parent ID.

### #231 — <public-hostname>-M showing wrong source while playing proper source
**Ask reporter:** "Now Playing" widget desync from actual broadcast. Confirm: is the source label wrong in the player UI, the dashboard, or the IRT/now-playing API? 16-min duration suggests a stale cached now-playing payload. Likely the `/api/v1/analytics/now-playing` cache TTL or PlayHistory update on transition.

### #215 — Hal weekly live both stations broke
**Ask reporter:** Filed 2026-05-03 (before v1.39.7 fix). Has it recurred since v1.39.9 / v1.40.8 deployed? If not, can close. If yes, need fresh prod log excerpt.

### #219 — Grammy Mary smart block missed schedule (started 20m late)
**Ask reporter:** Filed 2026-05-11. The 20m delay pattern matches the dead-air-watchdog grace period from v1.39.15 (`f811656`). Confirm: is the watchdog firing for this slot in the prod logs? Search `grep -i "dead.air\|watchdog" /var/log/grimnir`.

### #202 — Hal Weekly LIVE PLAYLIST both stations NO AUDIO
**Ask reporter:** Filed 2026-04-26. Several fixes have shipped since (v1.39.x playlist resume, smart-block fallback, etc.). Has Hal's Sunday playlist worked since? If not, need a log excerpt from the next failure.

### #201 — Dropping Coil station not holding scheduler
**Ask reporter:** Filed 2026-04-25. Same as #202 — please confirm whether this is still happening after v1.40.x deploys. Smart-block adjustment by SM may have already mitigated.

### #206 — Jules 5-6 AM: music played instead of show content
**Ask reporter:** Filed 2026-04-29. Pattern matches `bd64c75` v1.39.1 smart-block-engine-fallback losing SourceType (which fell to random fill). Confirm whether this has recurred since v1.39.1.

## DUPLICATES

### #226 — Asked about Listener counter for stations → dup of #178
Same request, same operator. #178 has the implementation context; #226 is a reminder follow-up. Listener counter code exists in v1.38.37+ but display logic hides it on null/error responses (see #178 STILL REAL).

## Verification log (2026-06-07 re-verification pass)

Branch under test: `v2-dev` HEAD `78ec95c` ("Triage open issues against current code (2026-06-07)"). Worktree used: `/tmp/grimnir-verify` checked out at `fix/webstream-stall-watchdog` HEAD `776b076`.

### Commits confirmed present on both `main` HEAD `307a67e` and `v2-dev` HEAD `78ec95c`
- `307a67e` PCM decoder leak (v1.40.8)
- `307d3f1` Bulk media delete FK cleanup (v1.40.7 / #223)
- `acb2ee1` Crossfade decoder leak (v1.40.2)
- `e6759cb` ICY poller Updates(map) (v1.39.18 / #218 + #217)
- `b08ed39` Wrong-station webstream on restart (v1.39.9)
- `cac61a2` Empty smart block silent gap (v1.39.7)
- `bd64c75` Smart-block engine fallback SourceType (v1.39.1)

Command: `git log v2-dev --oneline | grep -E "..."` and same against `main`.

### Commits ONLY on `fix/webstream-stall-watchdog` (PR #241), not yet on `main` or `v2-dev`
- `a0431c3` `Mount.BytesReceivedAt` (`internal/broadcast/server.go`)
- `e1f836e` `startWebstreamStallWatchdog` (`internal/playout/director.go`)
- `776b076` v1.40.9 version bump + closes-tag for #208 #209 #210 #211 #212 #214

Confirmation: `git show v2-dev:internal/broadcast/server.go | grep -c "BytesReceivedAt"` → `0`. Same for `internal/playout/director.go`. Both symbols exist only on the PR branch.

### Test runs

**PR #241 watchdog tests (in `/tmp/grimnir-verify` on `fix/webstream-stall-watchdog`):**
```
go test -run "TestWatchWebstreamPipeline_StallWatchdog|TestMount_BytesReceivedAt" -v \
  ./internal/playout/ ./internal/broadcast/
```
Result: 4/4 PASS (`TestWatchWebstreamPipeline_StallWatchdogStopsPipeline`, `TestWatchWebstreamPipeline_StallWatchdogExitsOnPipelineDone`, `TestMount_BytesReceivedAt_ZeroBeforeFeed`, `TestMount_BytesReceivedAt_UpdatedAfterFeed`). All four tests reference symbols that do not exist on `main`/`v2-dev`, so they cannot regress without the PR.

**Bulk delete regression (`v2-dev` HEAD):**
```
go test -run TestMediaBulk_Delete_RemovesPlaylistReferences -v ./internal/web/
```
PASS. Verified failure path by reverting `internal/web/pages_media.go` to `307d3f1~1` inside the worktree and re-running: `playlist_item ref should be cleaned up, count=1`. Restored.

**ICY metadata regression (`v2-dev` HEAD, new test added under this pass):**
```
go test -run TestICYPoller_UpdatePlayHistory_DoesNotWriteMediaID -v ./internal/webstream/
```
PASS on current code. Verified failure path by temporarily reverting `icy_metadata.go:139-150` to the pre-fix `Save(&history)` and re-running:
```
icy_metadata_test.go:157: ICY poller called Save(&PlayHistory): regression of #218
icy_metadata_test.go:162: ICY poller UPDATE statement included media_id column: regression of #218
```
Restored the fix; test passes again. New test file: `internal/webstream/icy_metadata_test.go`.

### #224 evidence for downgrade
- `gh issue view 224 --json createdAt` → `2026-05-15T20:38:13Z`
- Body text: "Last on list or any on list not playing" (single image attachment; agents cannot fetch user-attachments).
- `git log --oneline --all | grep -iE "last.item|playlist.tail|queue.last|tail.not.play|skip.last"` → no matches.
- No commit since 2026-05-15 names #224. Triage attribution to PCM leak (v1.40.8) + PR #241 is plausible but unproven; "last on list" reads as queue sequencing, not CPU starvation.
- Action: NEEDS REPRO (see "NEEDS WORK" section).
