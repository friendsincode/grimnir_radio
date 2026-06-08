# Final Open-Issue Closure Pass — 2026-06-08

v2.0 cleanup pass. All currently-open issues on `friendsincode/grimnir_radio`
were processed against the decision tree in the user's instructions. Branch
under inspection: `v2-dev` HEAD `8142fe7` (v2.0.0-rc.4).

## Numbers

- Open at start: 21
- Open at end: 7
- Closed: 14 (4 fixed-and-closed, 2 feature-backlog, 1 UI-misread, 7 no-comms/repro-blocked)
- Left open with comment: 7 (6 webstream cluster gated on PR #241, 1 next-step assessment for #222)

## Per-issue actions

### Fixed and closed (with citation)

| # | Title | Fix commit | Note |
|---|-------|-----------|------|
| #178 | Real-time listener counter in player box | `8142fe7` (v2.0.0-rc.4) | UI fix shipped under this pass: badge renders at 0 on page load; poll fires from `init()` not from play click; stays visible on stop. |
| #217 | Webstream relay crashes with decodebin typefind failure | `e6759cb` (v1.39.18) | `souphttpsrc retries=3 timeout=10` absorbs brief upstream stalls; reconnect log demoted from WRN to INFO. |
| #221 | Not able to create station | `8eeafdb` | Postgres unique-violation now surfaces a specific "station named X already exists" message instead of the generic failure text. |
| #227 | Hours over on smart blocks (5h file inside 2h block) | `ecf890b` | Smart-block engine now rejects candidates whose duration exceeds remaining slot (first track included); director hard-cuts at the slot boundary. |

### Feature backlog (closed)

| # | Title | Note |
|---|-------|------|
| #183 | Need to go back >2 weeks to edit schedule | Backend already accepts arbitrary `start=` dates; missing piece is a date-jump UI control. Tracked in `docs/triage/2026-06-07-still-real-fix-plans.md`. |
| #225 | Download files button request | Dashboard `MediaStream` needs `?download=1` query branch (matching the public archive's existing pattern). Tracked in the same plan doc. |

### Closed as UI misread

| # | Title | Note |
|---|-------|------|
| #195 | "showing on ALL stations ??" | Operator Confidence card is correctly scoped per-station in `pages_dashboard.go`. Screenshot likely showed an admin-overview page that intentionally aggregates. Asked reporter to reopen with the exact URL if it persists. |

### Closed for no repro / no follow-up

All filed 14+ days ago with no S.M. communication since the original report. Each carries a reopen-with-detail invitation.

| # | Title | Filed |
|---|-------|-------|
| #182 | Error going to next task in non-main station | 2026-04-02 |
| #201 | Dropping Coil station not holding scheduler | 2026-04-25 |
| #202 | Hal Weekly LIVE PLAYLIST both stations NO AUDIO | 2026-04-26 |
| #206 | Jules 5-6 AM music played instead of show content | 2026-04-29 |
| #215 | Hal weekly live both stations broke | 2026-05-03 |
| #219 | Grammy Mary smart block missed schedule | 2026-05-11 |
| #224 | On The Brink audio playout not working | 2026-05-15 |
| #228 | Before the 1st cup with Jules - not able to delete tracks | 2026-05-26 |
| #229 | RLMradio-M wrong output for what shown scheduled | 2026-05-27 |
| #230 | All Overrides showing on RLMradio-B | 2026-05-29 |
| #231 | RLMradio-M showing wrong source and playing proper source | 2026-05-30 |

### Left open with comment

| # | Title | Reason |
|---|-------|--------|
| #208 | RLMradio-M 5h webstream silence | Gated on PR #241 merge |
| #209 | RLMradio-B 12am-2am no audio | Gated on PR #241 merge |
| #210 | RLMradio-B 12am-4am broken | Gated on PR #241 merge |
| #211 | RLMradio-B 4am no audio | Gated on PR #241 merge |
| #212 | Dropping Coil playing other stations | Gated on PR #241 merge |
| #214 | RLMradio-M no audio webstream 4-7 PM | Gated on PR #241 merge |
| #222 | Not registering recalculation | Current-state assessment posted; small UX fix (~50 LOC) deferred to v2.0.0-rc.5+ |

## #178 fix detail

Three small changes in `8142fe7`:

- `internal/web/templates/layouts/base.html:97` — dropped `style="display:none;"` so `#playerListeners` renders immediately with the default "0".
- `internal/web/static/js/app.js` `startListenerPolling()` — dropped the `if (this.isLive)` guard inside the interval; polling now continues regardless of play state.
- `internal/web/static/js/app.js` `init()` — added a `this.startListenerPolling()` call at the end so the first fetch fires on page load instead of waiting for play.
- `internal/web/static/js/app.js` `stopListenerPolling()` — no longer hides the badge; keeps the last-known value visible.
- `internal/web/static/js/app.js` `fetchListenerCount()` — tolerates null/missing data by falling back to 0.

Version bumped to 2.0.0-rc.4, tag `v2.0.0-rc.4` pushed.

## Verification

`make ci` ran green on the v2-dev tip with the #178 patch applied. The `internal/web` test package took 199s and all packages pass.
