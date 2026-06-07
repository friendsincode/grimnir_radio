# Open Issue Triage — 2026-06-07

Read-only triage of every open issue against `main` (HEAD `307a67e`, v1.40.8) and `v2-dev` (`c1376bd`, v2.0.0-rc.2). PR #241 (`fix/webstream-stall-watchdog`, v1.40.9) is referenced as "PR-241" below; it's open and not merged.

## Summary

- 28 issues open
- 9 likely fixed (need operator verification)
- 9 still real
- 9 need repro
- 1 duplicate

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

## PROBABLY FIXED (please verify in prod)

### #218 — ICY metadata poller: SQLSTATE 22P02 on empty media_id
**Fixed by:** `e6759cb` "Webstream relay resilience + ICY poller PlayHistory update fix (v1.39.18)"
**Why this likely closes it:** `internal/webstream/icy_metadata.go:139-142` now uses `Updates(map[string]any{...})` with an explicit column allowlist exactly as the bug report prescribed. Empty `media_id` no longer reaches Postgres.

### #223 — Channel Anti Media: bulk delete + single delete both fail
**Fixed by:** `307d3f1` "Bulk media delete must clean FK references first (v1.40.7)"
**Why this likely closes it:** Bulk delete now mirrors single-delete and calls `adminDeleteMediaReferences` inside the transaction. Test `TestMediaBulk_Delete_RemovesPlaylistReferences` locks in the FK-violation regression.

### #224 — On The Brink: audio playout not working
**Fixed by:** Combination of `acb2ee1` (v1.40.2 crossfade decoder leak) + `307a67e` (v1.40.8 PCM decoder leak) + PR-241 (webstream stall watchdog)
**Why this likely closes it:** The "Last on list or any on list not playing" symptom matches the CPU-starvation cascade from leaked PCM decoders (v1.40.8 root cause) and silent webstream stalls (PR-241). Verify after PR-241 ships.

### #208 — RLMradio-M: 5h webstream silence
**Fixed by:** PR-241 (v1.40.9 webstream stall watchdog) — branch open, not merged
**Why this likely closes it:** PR description explicitly closes #208. Watchdog polls `Mount.BytesReceivedAt()` and forces reconnect after 30s of zero bytes following first successful flow.

### #209 — RLMradio-B 12am-2am no audio in webstream
**Fixed by:** PR-241
**Why this likely closes it:** Same stall pattern as #208; PR closes this.

### #210 — RLMradio-B 12am-4am broken
**Fixed by:** PR-241 + `cac61a2` (v1.39.7 empty-smart-block silent gap fix) + `b08ed39` (v1.39.9 wrong-station webstream on restart)
**Why this likely closes it:** "Wrong content" component covered by v1.39.9; "looping audio 2am-3am" covered by v1.39.7 / v1.40.2 echo loop; silent gap covered by PR-241.

### #211 — RLMradio-B 4am no audio on webstream
**Fixed by:** PR-241
**Why this likely closes it:** Same stall class.

### #212 — Dropping Coil playing content from other stations
**Fixed by:** `b08ed39` "Fix webstream relay playing wrong-station automation on restart (v1.39.9)" + PR-241
**Why this likely closes it:** Cross-station content bleed matched the v1.39.9 fix exactly; PR-241 also closes this.

### #214 — RLMradio-M no audio webstream 4-7PM
**Fixed by:** PR-241
**Why this likely closes it:** Same 5h-silence pattern as #208; PR closes it.

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
