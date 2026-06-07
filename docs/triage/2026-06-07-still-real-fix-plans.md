# Still-Real Issues — Fix Plans (2026-06-07)

Deep-dive plans for the 9 issues triaged as STILL REAL in
`docs/triage/2026-06-07-open-issue-triage.md`. Branch under inspection:
`v2-dev` HEAD `c1376bd` (v2.0.0-rc.2). Read-only — no code changes here.

Issues covered: #178, #182, #183, #195, #217, #221, #222, #225, #227.

---

## #217 — Webstream relay crashes with decodebin typefind failure on transient upstream stalls

**Filed:** 2026-05-10 by chrobione
**One-line:** A `decodebin/typefind` "Could not determine type of stream" on the relayed-internal webstream tears down the pipeline & costs an audible gap on reconnect.

**Severity:** P3 — low. Self-recovering on attempt 1, ~1 incident per 2h on
relayed-internal mounts only, no listener loss; only the WRN-log signal:noise
ratio suffers.
**Reasoning:** Audible blip but no outage; reporter explicitly tagged it P3.

**Root cause:** The fix already partially landed in v1.39.18 (`e6759cb`).
- `internal/playout/director.go:1789` adds `souphttpsrc retries=3 timeout=10`
  so brief upstream stalls are absorbed before `typefind` sees them.
- `internal/playout/director.go:1473-1476` demoted the post-exit reconnect log
  from WRN to INFO (the routine path), so the noisy WARN the reporter saw is
  gone.
What's still real: if `souphttpsrc` exhausts its retries (3) the pipeline DOES
crash & decodebin's typefind error is the proximate signal. The watchdog at
`director.go:1832` (`watchdog timeout=15000`) also still converts long stalls
into pipeline aborts on purpose. There's no "soft retry" classifier in
`watchWebstreamPipeline` (`director.go:1430-1567`) — every crash counts toward
`maxAttempts` and bumps the backoff.

**Fix sketch:**
1. In `watchWebstreamPipeline` after the `Done()` fires, peek at the last
   pipeline stderr (already captured by `manager.LastError(mountID)` —
   pattern used elsewhere) & if it matches `Could not determine type of
   stream` OR `watchdog.*timed out`, reset the backoff to `baseDelay` for
   the first 2 attempts instead of doubling. Treat it as a "soft" recovery
   shape.
2. Optionally bump `souphttpsrc retries=3` to `retries=5` & `timeout=15`
   for the relayed-internal case (detect by checking if `sourceURL`
   host matches `r.Host`).
3. Add `multiqueue max-size-time=3000000000` between `souphttpsrc` & `decodebin`
   so typefind sees a fuller buffer before deciding.
~40 LOC.

**Effort:** small (2-3 hr).
**Risk:** low — touches only webstream pipeline path; existing reconnect tests
in `internal/playout/director_webstream_test.go` exercise the loop & should
still pass. No schema change.
**Test plan:** `TestWatchWebstreamPipeline_SoftRetryOnTypefindError` —
inject a stub pipeline whose `LastError` returns the typefind string,
trigger `Done()`, assert the backoff stays at `baseDelay` for the first
2 attempts (vs the current doubling).
**Dependencies:** none. PR #241's stall watchdog is orthogonal & complementary.

---

## #227 — Hours over on smart blocks (5h file inside 2h block)

**Filed:** 2026-05-18 by reallibertymedia-01
**One-line:** A smart block can pick a media item longer than the slot, so the
single track plays past the slot end & eats the next 2-3h of programming.

**Severity:** P1 — high. Schedule drift is a trust killer for an operator;
reporter says it has happened to multiple shows ("Red Pill Sunday School and
others").
**Reasoning:** Affects core scheduling promise; recurring; visible to listeners.

**Root cause:** Two cooperating gaps.
- `internal/smartblock/engine.go:1038-1056` — the "fitting" filter
  (`d.Milliseconds() <= maxDurMS`) only runs when `cursor > 0`. The FIRST
  track in the slot is picked from the unfiltered pool, so a 5h file can be
  chosen for a 2h slot. The comment on line 1054-1055 acknowledges the
  "executor hard-cuts" plan, but…
- `internal/playout/director.go:2518-2536` — `playMedia` sets
  `state.Ends = stopAt` (clamped to `entry.EndsAt`) BUT the GStreamer
  `filesrc` pipeline reads the whole file. The only stop hook is the
  scheduler tick (`director.go:340-358`) which only preempts when a
  DIFFERENT `entry.ID` becomes due. Inside the smart-block's own slot,
  no different entry arrives, so the 5h file plays to natural end.
  `TrackEndsAt` (line 2536) is `media.Duration + 5min`, not `stopAt`.
- Confirming gap: there's no `state.Ends`-driven preemption anywhere; the
  scheduler tick at `director.go:325-338` only checks `policy.Mode ==
  "soft"`. In `hard` mode it falls through to the next-entry preemption at
  line 344, which doesn't fire mid-smart-block.

**Fix sketch:**
1. `engine.go:1039` — drop the `cursor > 0` guard so the first track is
   also size-filtered. Add a fallback: if NO track fits, pick the shortest
   available & log a `warning:slot_too_short_for_any_track`.
2. `director.go:340-358` — extend the existing preemption check: in addition
   to "next entry is due", also fire when `now.After(state.Ends)` AND the
   current state's source is `media`/`smart_block` with no scheduled
   successor in the next 60s. This forces a hard cut at the slot boundary
   without waiting for the next entry to arrive.
3. Optionally raise the `TrackEndsAt` watchdog to be `min(media.Duration,
   stopAt-now) + grace` so the watchdog also catches the case.
~60 LOC across both files.

**Effort:** medium (4-6 hr).
**Risk:** medium — hard-cutting a track mid-play could clip the last track of
a slot. Mitigate by only firing when `now > state.Ends + 30s` grace, & only
for `smart_block`/`media` source types (never webstream/live). Also need to
make sure crossfade lookahead at `director.go:360-374` still works; the
preemption shouldn't kill a fade-in that's already in flight.
**Test plan:** `TestSmartBlock_RejectsOverlongFirstTrack` in
`internal/smartblock/engine_test.go` (seeds a 2h slot + a single 5h
candidate; asserts result is empty or warns) AND
`TestDirector_HardCutsAtSlotEnd_SmartBlock` in
`internal/playout/director_test.go` (advances clock past `state.Ends`,
asserts `StopPipeline` was called).
**Dependencies:** none. Blocks #227.

---

## #225 — Download files button request

**Filed:** 2026-05-18 by reallibertymedia-01
**One-line:** Operators want a download button on station media pages
(parity with AzuraCast); no download endpoint exists today on the
authenticated dashboard.

**Severity:** P3 — feature request, no broadcast impact.
**Reasoning:** Pure UX feature; reporter has a workaround (public archive
already supports `?download=1`).

**Root cause:** Not a bug — gap. Public archive at
`internal/web/pages_public.go:631-693` already implements the
`?download=1` → `Content-Disposition: attachment` pattern (gated by
`media.AllowDownload`). The dashboard equivalent
`MediaStream` at `internal/web/pages_media.go:1342-1401` always emits
`Content-Disposition: inline` regardless of query string.

**Fix sketch:**
1. In `internal/web/pages_media.go:1397`, branch on
   `r.URL.Query().Get("download") == "1"` to emit `attachment` instead of
   `inline`. Don't gate on `AllowDownload` — dashboard users with
   `view_media` are already authenticated station members; the toggle
   is for public listeners only.
2. Add a download icon next to the play/preview button in the media list
   template (likely `internal/web/templates/pages/dashboard/media/list.html`
   or `media-table.html`) pointing at `/dashboard/media/{{.ID}}/stream?download=1`.
~25 LOC.

**Effort:** small (1-2 hr).
**Risk:** low — only adds a query-string branch & a UI button. No schema
change. Existing `TestMediaStream_*` tests in
`internal/web/pages_media_coverage_test.go:795+` still pass with default
(inline) behavior.
**Test plan:** `TestMediaStream_DownloadFlag_SetsAttachmentDisposition` in
`pages_media_coverage_test.go` — request `/dashboard/media/:id/stream?download=1`,
assert `Content-Disposition` starts with `attachment;`.
**Dependencies:** none.

---

## #195 — "showing on ALL stations ??" (Operator Confidence)

**Filed:** 2026-04-13 by reallibertymedia-01
**One-line:** A dashboard panel (operator believes it's the Operator
Confidence card) appears to show entries from every station instead of just
the selected one.

**Severity:** P2 — affects operator trust but only if confirmed; without
the screenshot the actual symptom is ambiguous.
**Reasoning:** Could be a real station-scoping bug or could be operator
misreading a global admin view. Cannot diagnose without screenshot.

**Root cause:** Read of the dashboard confidence loader at
`internal/web/pages_dashboard.go:418-498` shows EVERY query is filtered
on `station_id = ?` (lines 423, 430, 436, 505, 553, 600). The model
`ExecutorState` (`internal/models/priority.go:73-97`) has a
`uniqueIndex` on `station_id` so there can only be one row per station.
`MountPlayoutState` queries are also station-scoped (line 430). The
template `internal/web/templates/partials/dashboard-playout-confidence.html`
reads `.RuntimeState`, `.CurrentMount`, `.ExpectedMount` directly from
the page data — all of which were just filtered per-station.

The non-station-scoped queries in the file are name lookups by ID
(`internal/web/pages_dashboard.go:243, 250, 257, 264, 271`) which is fine —
they enrich entries that were already station-filtered upstream.

Conclusion: the Operator Confidence card is correctly scoped per-station
in the current code. Either (a) the screenshot is showing a different
panel (possibly an admin-overview page like `/dashboard/admin/stations`
that intentionally aggregates), or (b) the bug was real in 2026-04 & has
since been fixed by intervening commits; the file's git log shows
multiple station-scope corrections in v1.38.x.

**Fix sketch:** Reproducibility-blocked. Once the panel is identified:
1. If it's an admin-overview page, label clearly with "All stations" header.
2. If a real bug is found, add `station_id` predicate to the offending
   query.
~5-30 LOC depending on which panel.

**Effort:** small (1-2 hr) once repro lands.
**Risk:** low.
**Test plan:** Once the panel is identified, add a coverage test asserting
that calling the endpoint with two stations seeded only returns entries
for the requested one.
**Dependencies:** NEEDS REPRO — ask reporter for exact URL & a fresh
screenshot post-v1.40.8 to confirm the panel still misbehaves.

---

## #183 — Need to go back >2 weeks to edit schedule

**Filed:** 2026-04-02 by reallibertymedia-01
**One-line:** Schedule calendar lets you arrow-step backward but operators
need a date-jump control to reach a date several weeks back without 14+ clicks.

**Severity:** P2 — degraded workflow. Operators have a workaround (click
prev-day repeatedly) but it's tedious.
**Reasoning:** No data loss, no broadcast impact, just UX friction.

**Root cause:** Not a bug — feature gap.
- Backend already supports arbitrary historical date ranges:
  `internal/web/pages_schedule.go:382-396` accepts FullCalendar's `start`
  query param without any minimum date bound. The 28-day default only
  applies when `start` is empty (line 383).
- Frontend calendar at
  `internal/web/templates/pages/dashboard/schedule/calendar.html:1378-1438`
  has `prevDay`, `nextDay`, `prevWeek`, `nextWeek` buttons but NO date
  jump-to control & no `validRange` (so navigation is unconstrained but
  step-only).
- v1.38.37 (per triage) extended look-back to 28 days but didn't add a
  date picker.

**Fix sketch:**
1. Add a `<input type="date">` plus "Go" button to the calendar's
   `headerToolbar` (or a custom `customButtons` entry). On change, call
   `calendar.gotoDate(value)`.
2. Add a "Today" button if not already present (line 1436 references
   `today` so it's already there).
~30 LOC (mostly template HTML + 1 small JS handler).

**Effort:** small (1-2 hr).
**Risk:** low — frontend-only; no backend or schema change.
**Test plan:** `make test-e2e` add a go-rod step that loads
`/dashboard/schedule`, sets the date input to 60 days ago, clicks Go,
asserts the calendar's title text contains the target month.
**Dependencies:** none.

---

## #182 — Error when going to next task in non-main station

**Filed:** 2026-04-02 by reallibertymedia-01
**One-line:** Some dashboard "next task" action errors when run on a station
that isn't the user's first/default station.

**Severity:** P1 — high IF it blocks core workflow; until repro we can't
confirm scope.
**Reasoning:** Body is a screenshot only; "tasks" is ambiguous. Could be the
analyzer queue, the import wizard, or an HTMX pagination action.

**Root cause:** Unknown. The most likely candidates by code inspection:
- Analyzer queue at `internal/web/pages_media.go:34-122`
  (`MediaReanalyzeDurations`) — correctly station-scoped (line 56).
- Schedule pagination — `internal/web/pages_schedule.go` is station-scoped.
- Import review — `internal/web/pages_import_review.go:22-58` — needs check.
- Station context middleware at
  `internal/web/middleware.go:380-385` returns `nil` if the request context
  doesn't carry a station; many handlers then 400. If the station-switcher
  doesn't persist across nav, "main station" works (cookie default) but
  switching to a non-main station then clicking a button could drop the
  selection.
- `internal/web/pages_stations.go:162` calls `h.SetStation` after creating
  a station; similar SetStation flow may not run on switch.

**Fix sketch:** Reproducibility-blocked. Once URL & error text are known:
1. Audit the failing handler's `h.GetStation(r)` call & confirm the
   station cookie is set correctly by the switcher.
2. If middleware drops the station on certain routes, add station context
   to those routes.
~15-50 LOC depending on which handler.

**Effort:** unknown (small-to-medium).
**Risk:** depends on root cause.
**Test plan:** once handler is identified, a coverage test that seeds two
stations & calls the handler with the non-default station context.
**Dependencies:** NEEDS REPRO — ask reporter for the URL, browser devtools
Network tab response body, & exact button name from the screenshot.

---

## #221 — Not able to create station

**Filed:** 2026-05-15 by reallibertymedia-01
**One-line:** Station create form fails for the reporter with a generic
"Failed to create station" message.

**Severity:** P1 — high. Operator can't onboard a new station; no workaround
short of DB-level INSERT.
**Reasoning:** Core admin workflow blocked.

**Root cause:** Read of `internal/web/pages_stations.go:58-181` shows the
handler runs a transaction (line 105-158) creating a `Station`, a
`StationUser`, & a default `Mount`. Three failure modes plausible:
- `models.Station.Name` has `uniqueIndex` (`internal/models/models.go:376`).
  Reporter's screenshot likely shows a 23505 unique-violation if a station
  with the same name already exists. The handler logs the error
  (`pages_stations.go:109`) but the user-facing message at line 110 is the
  generic "Failed to create station".
- `defaultMountURL` (line 25) uses request host; if the auth proxy mangles
  Host header to empty, the resulting mount URL is empty & some downstream
  validation could fail (though no explicit URL validation in the create
  path).
- `Mount.Name` has `index` not `uniqueIndex` (`models.go:446`), so mount
  name collisions aren't the issue.
The reporter saw a screenshot we cannot fetch (S3-auth). The most likely
cause is duplicate-station-name. Whatever the cause, the current handler
gives the user no actionable feedback.

**Fix sketch:**
1. `pages_stations.go:107-112` — detect Postgres unique-violation error
   (errors.As to `*pgconn.PgError` with Code "23505") & surface a specific
   message: "A station named X already exists. Please choose a different
   name."
2. Add pre-check: `h.db.Where("name = ?", station.Name).Count(...)` before
   `tx.Create(&station)` to fail fast with a clean message.
3. Improve all 4 `renderStationFormError` calls (lines 110, 125, 149, 156)
   to include the underlying error class (without leaking SQL detail).
4. Log at INFO when the error is a known-shape (duplicate name, missing
   field) vs ERROR for unexpected.
~30 LOC.

**Effort:** small (2-3 hr).
**Risk:** low — only changes error-handling branches; happy path unchanged.
No schema change.
**Test plan:** `TestStationCreate_DuplicateName_ReturnsSpecificMessage` in
`internal/web/pages_stations_*_test.go` — seed a station "Test FM", POST
create with same name, assert response body contains the specific message
string.
**Dependencies:** would benefit from reporter confirming the duplicate-name
hypothesis, but fix is safe to ship blind since it strictly improves error
messaging.

---

## #222 — Not registering recalculation

**Filed:** 2026-05-15 by reallibertymedia-01
**One-line:** Clicking the duration-recalculation button doesn't appear to
register; operator sees no visible progress.

**Severity:** P2 — degraded admin workflow, no broadcast impact.
**Reasoning:** Operator can work around by waiting for prior batch to
finish, or by manual reanalysis per file.

**Root cause:** `internal/web/pages_media.go:34-122` (`MediaReanalyzeDurations`)
filters out media that already have a pending/running analysis job
(line 50-61). If a prior batch is stuck in `running` (e.g. analyzer crashed
mid-batch & jobs were never marked `failed`), the next click queues 0 jobs &
returns the empty-state partial at
`internal/web/templates/partials/duration-recalc-empty.html`:

```
<div class="alert alert-info mb-3">
    {{.Message}}
</div>
```

The Message is "No media queued (already pending/running or no media found)"
(line 64). That IS rendered to the user — but only inside the HTMX target,
& the message doesn't tell the user HOW MANY jobs are stuck or how to
unstick them. Reporter likely sees the alert but doesn't connect it to
"there are 47 stuck jobs from 3 days ago".

**Fix sketch:**
1. `pages_media.go:50-64` — when result is 0, also COUNT the pending/running
   jobs older than 1h & include that count + the oldest timestamp in the
   message: "No new jobs queued — 47 jobs from 3d ago are still pending.
   [Reset stuck jobs]".
2. Add a `POST /dashboard/media/reanalyze-durations/reset` admin button
   that marks `analysis_jobs.status='pending'` rows older than 1h as
   `'failed'` so the next click can re-queue them.
3. Update `duration-recalc-empty.html` to render the count, timestamp, &
   the Reset button when applicable.
~50 LOC across handler + template.

**Effort:** small (2-3 hr).
**Risk:** low — the Reset button could mass-fail legitimately-running jobs
if the analyzer is slow; gate with a 60-min minimum age & require
`edit_metadata` permission (already required for the parent endpoint).
**Test plan:** `TestMediaReanalyzeDurations_StuckJobs_ShowsResetHint` in
`internal/web/pages_media_coverage_test.go` — seed a media + a pending job
created 25h ago, POST reanalyze, assert response contains the stuck-job
count & a Reset link.
**Dependencies:** none.

---

## #178 — Real-time listener counter in player box

**Filed:** 2026-03-30 by chrobione
**One-line:** Operator (S.M.) wants a listener counter visible in the global
player widget; today it's polled but hidden until first play.

**Severity:** P2 — partial feature. Visible after pressing play; reporter
wants it visible all the time.
**Reasoning:** No broadcast impact; UX gap with public-pages comparison.

**Root cause:** Three behavioral choices conspire to hide the badge:
- `internal/web/static/js/app.js:1499-1507` — `startListenerPolling()` is
  only called from the play-start paths
  (line 1214 LQ direct & line 1242 normal flow). User hasn't clicked
  play → no polling → no display.
- `internal/web/templates/layouts/base.html:97` — the
  `<div id="playerListeners">` defaults to `style="display:none;"`.
- `internal/web/static/js/app.js:1521-1523` — only un-hides if `data` is
  truthy. Returns 200 from `handleAnalyticsListeners`
  (`internal/api/api.go:1505-1521`) even with 0 listeners, so once polling
  runs the badge does show.

**Fix sketch:**
1. Move `startListenerPolling()` out of the play paths & into the player's
   `init()`. Poll regardless of `isLive` (drop the `if (this.isLive)`
   guard at app.js:1505).
2. `app.js:1514` (`stopListenerPolling`) — keep the badge visible even
   when polling stops; just freeze the last value.
3. `base.html:97` — drop `style="display:none;"` so the badge shows
   immediately at "0" while the first poll is in flight.
4. Confirm `handleAnalyticsListeners` returns sensible data when
   `a.broadcast == nil` (it already returns `{total: 0, mounts: []}` at
   `api.go:1507-1511`) so the badge stays at 0 cleanly during cold start.
~15 LOC.

**Effort:** small (1 hr).
**Risk:** very low — frontend only.
**Test plan:** add a small JS unit (or go-rod E2E) that loads the
dashboard, asserts `#playerListeners` is visible before any play action,
asserts `#playerListenerCount` shows a number within 16s.
**Dependencies:** none. Closes #178 & duplicate #226.

---

# Recommended sprint order

Sort: severity (P0 → P3), then within each severity by (low risk, small
effort) first to build velocity. P0 — none. Then P1, P2, P3.

| # | Order | Issue | Severity | Effort | Risk | Why this slot |
|---|-------|-------|----------|--------|------|---------------|
| 1 | P1a | #221 | P1 | small (2-3h) | low | Pure error-messaging upgrade; unblocks station create even if root cause is operator-side. Smallest P1 win. |
| 2 | P1b | #182 | P1 | unknown | unknown | Needs repro first. Slot here so reporter follow-up runs in parallel with #221 work. |
| 3 | P1c | #227 | P1 | medium (4-6h) | medium | Biggest P1; touches scheduler + director. Do it after the small P1 lands so a regression on this larger change doesn't block #221. |
| 4 | P2a | #178 | P2 | small (1h) | very low | Pure frontend; fastest win in the P2 tier. Closes #226 dup at the same time. |
| 5 | P2b | #225 | P3-ish but listed P2 elsewhere | small (1-2h) | low | One-line backend tweak + a button. Reuses existing public-archive download pattern. |
| 6 | P2c | #222 | P2 | small (2-3h) | low | UX feedback fix; pairs well with the analyzer-jobs cleanup. |
| 7 | P2d | #183 | P2 | small (1-2h) | low | Date picker in calendar; frontend-only. |
| 8 | P2e | #195 | P2 | small once repro lands | low | Needs repro. Slot late so it can ride with #182's reporter ping. |
| 9 | P3 | #217 | P3 | small (2-3h) | low | Signal:noise improvement; ship after the user-visible fixes. |

**Total estimated effort:** ~22-30 hours of focused work (under 1 sprint
week), assuming #182 & #195 repro arrives. Both P1-unknown items
(#182, #195) could expand the estimate by 8-16 hours each in the worst
case.

**Items to potentially close as not-a-bug after repro:**
- #195 (likely admin-overview page misread as per-station panel).
- #182 (could be a transient session bug, not reproducible).
- #217 (largely already fixed in v1.39.18; remaining work is polish).
