# Issue Triage & Fix Sprint (Apr 30 2026)

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Diagnose and resolve the cluster of audio-silence, schedule-hold, and content-not-playing issues filed Apr 28–30, plus clear the labeled backlog.

**Architecture:** Three-phase — (1) label/triage new unlabeled issues so nothing is lost, (2) pull production logs and diagnose each root cause, (3) implement targeted fixes with TDD and close issues.

**Tech Stack:** Go 1.24, GStreamer pipelines, gRPC, PostgreSQL/GORM, `gh` CLI, SSH to production at `rlmadmin@192.168.195.11`.

---

## Chunk 1: Triage

### Task 1: Label new unlabeled issues (201–208)

No code changes. Pure GitHub triage.

**Files:** none

- [ ] **Step 1: Apply labels to #208**
```bash
gh issue edit 208 --repo friendsincode/grimnir_radio \
  --add-label "priority:P1,type:bug,area:playout" \
  --title "RLMradio-M: 5 hr webstream audio silence (2–7 PM, Apr 30)"
```

- [ ] **Step 2: Apply labels to #207**
```bash
gh issue edit 207 --repo friendsincode/grimnir_radio \
  --add-label "priority:P2,type:bug,area:web-ui"
```
Comment on #207: `"Failed to load IRT state." is a client-side JavaScript error shown when \`/api/v1/analytics/now-playing\` or \`/dashboard/schedule/events\` returns an error. It does not reflect a backend state-machine failure. Investigating root cause of the API failure separately.`

- [ ] **Step 3: Apply labels to #206**
```bash
gh issue edit 206 --repo friendsincode/grimnir_radio \
  --add-label "priority:P1,type:bug,area:scheduler,area:playout" \
  --title "RLMradio-B: Jules 5–6 AM — music played instead of show content"
```

- [ ] **Step 4: Apply labels to #205**
```bash
gh issue edit 205 --repo friendsincode/grimnir_radio \
  --add-label "priority:P1,type:bug,area:playout"
```

- [ ] **Step 5: Apply labels to #204**
```bash
gh issue edit 204 --repo friendsincode/grimnir_radio \
  --add-label "priority:P1,type:bug,area:playout"
```

- [ ] **Step 6: Apply labels to #203**
```bash
gh issue edit 203 --repo friendsincode/grimnir_radio \
  --add-label "priority:P1,type:bug,area:playout,area:scheduler"
```

- [ ] **Step 7: Apply labels to #202**
```bash
gh issue edit 202 --repo friendsincode/grimnir_radio \
  --add-label "priority:P1,type:bug,area:playout"
```

- [ ] **Step 8: Apply labels to #201**
```bash
gh issue edit 201 --repo friendsincode/grimnir_radio \
  --add-label "priority:P2,type:bug,area:scheduler,area:smart-blocks"
```

- [ ] **Step 9: Close #196 as data issue**
```bash
gh issue close 196 --repo friendsincode/grimnir_radio \
  --comment "Root cause identified: a one-off Dork Table Podcast episode was manually placed in Grammy's 2–4 PM Friday slot on Apr 10. The overlap trigger blocked Grammy's materialization; the director correctly played the manual entry. No code changes needed. To prevent recurrence: remove or override conflicting manual entries before placing them in recurring-show slots."
```

---

## Chunk 2: Log Investigation

### Task 2: Pull production logs for webstream silence incidents

Context: Multiple issues (#208, 205, 204, 203, 199, 197) report 2–7 hr audio silences on a webstream relay slot. The source stream ("Conversations Without Compromise") is confirmed working. We need to see what the playout director was doing.

**Files:** no code changes — read-only log collection.

- [ ] **Step 1: SSH and grep logs around the most recent incident (Apr 30, 2 PM CDT = 19:00 UTC)**

Run from the server (`ssh rlmadmin@192.168.195.11`), inside `/srv/docker/grimnir_radio`:

```bash
./grimnir logs --since 8h grimnir-radio 2>&1 \
  | grep -E "19:[0-9]{2}|20:[0-9]{2}|webstream|pipeline|reconnect|fill|silence|crash|EOS|error" \
  | grep -v "GORM\|SQL\|SELECT\|UPDATE\|INSERT" \
  | head -200
```

Look for:
- `"starting webstream playout"` — did the pipeline start at 19:00?
- `"webstream pipeline crashed"` — did it crash?
- `"reconnecting webstream pipeline"` — did reconnect fire?
- `"all reconnect attempts failed, entering slow retry"` — did slow-retry kick in?
- `"webstream down — filling with automation"` — did fill start?
- `"no media found for next track"` — did fill fail to find media?
- `"failed to start webstream pipeline"` — did the pipeline fail at startup?
- `"pipeline already running"` — did a race prevent the new pipeline from starting?

- [ ] **Step 2: Check if the GStreamer process was running during silence**

```bash
./grimnir logs --since 8h grimnir-radio 2>&1 \
  | grep -E "19:[0-9]{2}" \
  | grep -v "SQL\|GORM\|SELECT" \
  | head -100
```

Also check:
```bash
./grimnir logs --since 8h grimnir-radio 2>&1 \
  | grep -E "watchdog|timeout=15000|stall" \
  | head -50
```

- [ ] **Step 3: Check Apr 28 incidents (#203, 204, 202) — Hal smart block at 9 PM CDT (02:00 UTC Apr 29)**

```bash
./grimnir logs --since 72h grimnir-radio 2>&1 \
  | grep -E "smart block sequence complete|regenerating|no media found|smart block produced|generation failed|02:[0-9]{2}|03:[0-9]{2}" \
  | grep -v "SQL\|GORM" \
  | head -100
```

- [ ] **Step 4: Check Jules 5–6 AM slot (#206) — Apr 29, 5 AM CDT = 10:00 UTC**

```bash
./grimnir logs --since 48h grimnir-radio 2>&1 \
  | grep -E "10:[0-9]{2}|11:[0-9]{2}|jules|Jules|schedule entry|handle.*entry|failed to handle" \
  | grep -v "SQL\|GORM" \
  | head -100
```

Also check what was in the schedule at that time:
```bash
./grimnir exec -it grimnir-db psql -U grimnir -d grimnir \
  -c "SELECT id, source_type, source_id, starts_at, ends_at, is_instance, recurrence_type
      FROM station_schedule_entries
      WHERE starts_at >= '2026-04-29 09:00:00' AND starts_at <= '2026-04-29 12:00:00'
      ORDER BY starts_at;"
```

- [ ] **Step 5: Check the Hal LIVE playlist incident (#202) — Apr 27, ~1:52 AM CDT = ~06:52 UTC**

```bash
./grimnir logs --since 96h grimnir-radio 2>&1 \
  | grep -E "06:[0-9]{2}|07:[0-9]{2}|live|Live|IRT|irt|playlist|skip|track.*end|4 sec|broke" \
  | grep -v "SQL\|GORM" \
  | head -100
```

- [ ] **Step 6: Document findings**

Based on the log output, determine which of these root causes applies to the webstream silence:

**A**: Pipeline fails to start (GStreamer launch error) → entry marked played → silence until slot ends.  
**B**: Pipeline starts but crashes immediately → reconnect succeeds or fill starts → but something in the fill/reconnect path fails silently.  
**C**: Pipeline runs but produces silence (upstream serving silence without EOS) → watchdog fires at 15 s → reconnect/fill cycle has a bug that prevents audio recovery.  
**D**: Slot ends, next entry doesn't start → gap in schedule or next entry also fails.  
**E**: Something else.

Record the finding here before proceeding to Task 3.

---

## Chunk 3: Webstream Silence Fix

> **Before starting this chunk:** Complete Task 2 and identify the root cause. The exact fix depends on the log evidence. The most likely candidates are covered below — implement only the one(s) confirmed by logs.

### Task 3A: Fix — webstream silence when pipeline starts but GStreamer process exits before producing output

**Symptom from logs:** Pipeline starts (`"starting webstream playout"`), immediately exits (GStreamer exits with error in < 1s), `watchWebstreamPipeline` fires, reconnect fails, fill tries to start but gets `"pipeline already running"` because the stopped-but-not-yet-reaped process blocks the slot.

**Root cause:** Race between GStreamer process exit and the re-use check in `pipeline.go`. The `done` channel closes when `Wait()` returns, but there's a window where `p.cmd != nil` and `p.done` has NOT been observed closed by `EnsurePipelineWithDualOutput`.

Actually this is already guarded: `StartWithOutput` checks `select { case <-p.done: ... default: return "already running" }`. So if done is closed, it starts a new process. This path should be fine.

**Skip this task if logs show the pipeline starts successfully and runs for multiple minutes before silence begins.**

### Task 3B: Fix — fill automation fails silently when broadcast mount is nil

**Files:**
- Modify: `internal/playout/director.go:3382-3393` (playRandomNextTrack broadcast mount nil check)

**Symptom from logs:** `"broadcast mount not found for next track"` — fill starts but no audio because the broadcast mount doesn't exist.

**Hypothesis:** The broadcast mount is created when `startWebstreamEntry` runs. If for any reason the mount doesn't exist (first run after restart, or mount name mismatch), fill fails silently.

- [ ] **Step 1: Write a failing test**

In `internal/playout/director_webstream_test.go`, add:

```go
func TestPlayRandomNextTrackCreatesBroadcastMountIfMissing(t *testing.T) {
    // Use the test director setup — broadcast server has no mounts initially.
    // playRandomNextTrack should create the broadcast mount rather than silently returning.
    d, _, _ := newTestDirector(t)
    // Verify: after playRandomNextTrack, the HQ mount exists.
    // This test fails if playRandomNextTrack just returns on nil mount.
    // ... (see implementation for specifics)
}
```

Run: `go test -v -run TestPlayRandom ./internal/playout/`  
Expected: FAIL

- [ ] **Step 2: Fix playRandomNextTrack to create mount if missing**

Modify `internal/playout/director.go:3382`:

```go
broadcastMount := d.broadcast.GetMount(mount.Name)
if broadcastMount == nil {
    // Create broadcast mount on-demand — it may not exist if this is the
    // first fill after a restart or if the mount name changed.
    contentType := "audio/mpeg"
    if mount.Format == "aac" {
        contentType = "audio/aac"
    } else if mount.Format == "ogg" || mount.Format == "vorbis" {
        contentType = "audio/ogg"
    }
    broadcastMount = d.broadcast.CreateMount(mount.Name, contentType, hqBitrate)
    d.logger.Warn().Str("mount", mount.Name).Msg("playRandomNextTrack: broadcast mount missing, created on demand")
}
```

Similarly for `lqMount` below it.

- [ ] **Step 3: Run test**

```bash
go test -v -run TestPlayRandom ./internal/playout/
```
Expected: PASS

- [ ] **Step 4: Run full test suite**

```bash
make test
```

- [ ] **Step 5: Commit**

```bash
git add internal/playout/director.go internal/playout/director_webstream_test.go
git commit -m "..."
```

### Task 3C: Fix — smart block returns no items, marks entry as played, silencing the slot

**Files:**
- Modify: `internal/playout/director.go:1625-1635` (startSmartBlockEntry empty-result path)

**Symptom:** After Hal smart block ends ("9 pm Hal Smart block ended and so did audio", #203). The sequence completes, `d.played[playKey]` is deleted, tick picks up entry, `startSmartBlockEntry` is called. If the engine returns 0 items (all levels of relaxation exhausted), the function returns `nil`, the tick marks the entry as played, and no audio plays until the slot ends.

- [ ] **Step 1: Write a failing test**

In `internal/playout/director_webstream_test.go` (or a new `director_smartblock_test.go`):

```go
func TestSmartBlockEmptyResultDoesNotMarkPlayed(t *testing.T) {
    // When smartblockEng.Generate returns an empty result,
    // startSmartBlockEntry should return an error so the tick does NOT
    // mark the entry as played, and retries on the next tick.
}
```

Run: `go test -v -run TestSmartBlockEmpty ./internal/playout/`  
Expected: FAIL

- [ ] **Step 2: Fix startSmartBlockEntry to return error on empty result**

Modify `internal/playout/director.go` at the empty-result path (around line 1625):

```go
if len(result.Items) == 0 {
    d.logger.Warn().Str("block", block.ID).Msg("smart block produced no items")
    d.publishNowPlaying(entry, map[string]any{...})
    return fmt.Errorf("smart block %s produced no items", block.ID)  // was: return nil
}
```

This causes the tick to NOT mark the entry as played, so it retries on the next 250ms tick. If the station eventually has media, the block will fire. If not, it'll keep retrying without silencing the slot.

- [ ] **Step 3: Run test**

```bash
go test -v -run TestSmartBlockEmpty ./internal/playout/
```
Expected: PASS

- [ ] **Step 4: Also fix the fallback path** (around line 1617-1622)

The fallback to random when `Generate` fails entirely also returns nil. Same fix: return error so tick retries.

```go
if len(fallbackItems) == 0 {
    return fmt.Errorf("smart block %s: generation failed and no fallback media found", block.ID)
}
```

- [ ] **Step 5: Run full test suite**

```bash
make test
```

- [ ] **Step 6: Commit**

```bash
git add internal/playout/director.go
git commit -m "..."
```

### Task 3D: Investigate Jules 5–6 AM not playing (#206)

**Context:** A scheduled show (Jules) at 5–6 AM didn't play — music played for the whole hour instead. This is a webstream slot (per the issue) where Jules content should air.

**Most likely cause:** The Jules entry is a webstream schedule entry that failed to start, fell through silently, and the webstream slot's fill (automation/music) played instead for the hour.

- [ ] **Step 1: Check the schedule DB for Apr 29 5–6 AM**

From server:
```bash
./grimnir exec -it grimnir-db psql -U grimnir -d grimnir \
  -c "SELECT id, source_type, source_id, starts_at, ends_at, is_instance, recurrence_type, metadata
      FROM station_schedule_entries
      WHERE starts_at BETWEEN '2026-04-29 09:00' AND '2026-04-29 11:30'
      ORDER BY starts_at;"
```

- [ ] **Step 2: If Jules is a playlist entry**

Check if the playlist had any analyzed tracks:
```bash
./grimnir exec -it grimnir-db psql -U grimnir -d grimnir \
  -c "SELECT mi.id, mi.title, mi.analysis_state, mi.duration
      FROM media_items mi
      JOIN playlist_items pi ON pi.media_id = mi.id
      JOIN playlists p ON p.id = pi.playlist_id
      WHERE p.id = '<source_id_from_step_1>'
      ORDER BY pi.position;"
```

If all tracks have `analysis_state = 'failed'` or `duration = 0`, `startPlaylistEntry` will return nil with no audio → entry marked played → silence.

- [ ] **Step 3: Fix playlist entry with all-failed tracks**

Same approach as Task 3C: return an error instead of nil when the playlist resolves to no playable tracks. This allows the tick to retry rather than silencing the slot.

File: `internal/playout/director.go:1538-1558` (startPlaylistEntry empty-items path)

Change `return nil` after the "no items after track overrides" log to `return fmt.Errorf(...)`.

- [ ] **Step 4: Write test, run suite, commit**

---

## Chunk 4: IRT API Failure (#207)

### Task 4: Diagnose and fix IRT "Failed to load IRT state" (API error)

**Context:** The webdj.html panel calls two APIs every 10 seconds:
1. `GET /api/v1/analytics/now-playing?station_id=<id>`
2. `GET /dashboard/schedule/events?start=...&end=...`

If either fails (non-2xx or network error), JS shows "Failed to load IRT state." The error is cosmetic (doesn't affect playout) but causes alarm. We need to find out why the API is returning errors.

**Files:**
- Read: `internal/api/` handlers for now-playing and schedule/events
- Read: `internal/web/pages_schedule.go` for `/dashboard/schedule/events`

- [ ] **Step 1: Check logs for API errors around the incident**

```bash
./grimnir logs --since 48h grimnir-radio 2>&1 \
  | grep -E "now-playing|schedule/events|500|timeout|panic" \
  | grep -v "SQL\|SELECT" \
  | head -50
```

- [ ] **Step 2: Check if the analytics query times out under load**

Find the now-playing handler:
```bash
grep -rn "now-playing\|NowPlaying" /home/code/projects/grimnir_radio/internal/api/ --include="*.go" | head -10
```

If the query scans a large `play_history` table without a proper index, it can time out when many tracks have played. Check:
```bash
./grimnir exec -it grimnir-db psql -U grimnir -d grimnir \
  -c "\d play_history" 2>/dev/null || \
  ./grimnir exec -it grimnir-db psql -U grimnir -d grimnir \
  -c "\d play_events"
```

- [ ] **Step 3: If a missing index is the cause, add it**

```bash
./grimnir exec -it grimnir-db psql -U grimnir -d grimnir \
  -c "CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_play_events_station_started
      ON play_events(station_id, started_at DESC);"
```

Then add a corresponding GORM AutoMigrate index or raw migration file.

- [ ] **Step 4: Improve the IRT panel error UX**

The current error message "Failed to load IRT state." is alarming. Change it to something less scary.

File: `internal/web/templates/pages/dashboard/webdj.html:1676`

Change:
```js
nowPlayingEl.innerHTML = '<span style="color:#dc2626">Failed to load IRT state.</span>';
```
To:
```js
nowPlayingEl.innerHTML = '<span style="color:#888"><i class="bi bi-wifi-off me-1"></i>Refresh failed — retrying…</span>';
```

- [ ] **Step 5: Run routes test and commit**

```bash
make test-routes
git add internal/web/templates/pages/dashboard/webdj.html
git commit -m "..."
```

---

## Chunk 5: Hal Live Playlist / No Active Playback (#202)

### Task 5: Diagnose Hal LIVE playlist — both stations silent, "no active playback" green arrow

**Context:** #202 describes "1:52 AM tried to skip first 4 secs of track, then crickets, tried to play Hal before 2 tracks in playlist, broke order. Both stations — NO ACTIVE PLAYBACK green arrow." This is the WebDJ live playlist (IRT) system.

- [ ] **Step 1: Understand the IRT skip-track flow**

Read the "skip" API handler:
```bash
grep -n "skip\|Skip" /home/code/projects/grimnir_radio/internal/api/ -r --include="*.go" | grep -v "_test.go" | head -20
grep -n "skip\|Skip" /home/code/projects/grimnir_radio/internal/playout/director.go | head -20
```

- [ ] **Step 2: Reproduce the scenario mentally**

From the issue: user skipped a track at 1:52 AM. After skip, both stations went silent. This suggests:
- Skip clears the current pipeline (expected)
- But the next track fails to start (not expected)

Check what `SkipCurrentTrack` does to the `d.played` map and `d.active` map. If it incorrectly clears the played map for ALL entries on a station (not just the mount), the tick might restart things from the wrong entry.

- [ ] **Step 3: Check for cross-station contamination in skip logic**

```bash
grep -n "ReloadStation\|SkipCurrentTrack\|skipTrack\|emergency_stop\|EmergencyStop" \
  /home/code/projects/grimnir_radio/internal/playout/director.go | head -20
```

Read the `SkipCurrentTrack` function. Confirm it only affects the specific mount/station it's called for, not all stations.

- [ ] **Step 4: If cross-station contamination exists, fix it**

Scope the skip operation to `stationID + mountID`, not globally.

- [ ] **Step 5: Write test, run suite, commit**

---

## Chunk 6: Version Bump, CI, and Deployment

### Task 6: Bump version, verify CI, tag, push

- [ ] **Step 1: Run CI gate**
```bash
make ci
```
Expected: all green. Fix any issues before proceeding.

- [ ] **Step 2: Bump version**

Edit `internal/version/version.go` — increment patch (e.g., v1.39.6 → v1.39.7).

- [ ] **Step 3: Commit, tag, push**
```bash
git add -A
git commit -m "Fix webstream silence, smart block empty-result, IRT UX (v1.39.X)"
git tag -a v1.39.X -m "Version 1.39.X"
git push origin main
git push origin v1.39.X
```

- [ ] **Step 4: Deploy to production**
```bash
ssh rlmadmin@192.168.195.11
cd /srv/docker/grimnir_radio
./grimnir pull
./grimnir up -d
./grimnir logs -f
```

Monitor for 5 minutes. Confirm no new errors.

---

## Chunk 7: Close Resolved Issues

### Task 7: Post comments and close issues as they're verified fixed

- [ ] **Step 1: Close #208, #205, #204, #203, #199, #197** (webstream silence)

After confirming the webstream fix deployed and no new incidents:
```bash
gh issue close 208 --repo friendsincode/grimnir_radio \
  --comment "Fixed in vX.Y.Z. Root cause: [fill from log evidence]. Monitoring."
# Repeat for 205, 204, 203, 199, 197
```

- [ ] **Step 2: Close #207** (IRT message)
```bash
gh issue close 207 --repo friendsincode/grimnir_radio \
  --comment "Fixed IRT panel error message UX in vX.Y.Z. The message was a JS fetch error, not a state machine failure. Now shows 'Refresh failed — retrying…' which is less alarming."
```

- [ ] **Step 3: Close #206** (Jules content not playing)

After confirming:
```bash
gh issue close 206 --repo friendsincode/grimnir_radio \
  --comment "Fixed in vX.Y.Z. Root cause: [from log evidence]. Playlist/entry now returns an error instead of nil when no playable tracks exist, so the director retries rather than silencing the slot."
```

- [ ] **Step 4: Evaluate #201, #202** (Dropping Coil promo loop, Hal skip)

If no clear code fix was found after investigation, add a comment with findings and schedule a follow-up.

- [ ] **Step 5: Assess older labeled issues**

For #195 ("showing on all stations"), #190, #188, #187, #183, #182, #181, #178, #177 — check if any were resolved by recent patches. Close with comment or escalate.

---

## Notes for Investigator

**Key log patterns to find:**
- `"starting webstream playout"` → pipeline attempted
- `"failed to start webstream pipeline"` → startup failure
- `"webstream pipeline crashed"` → crash detected
- `"all reconnect attempts failed, entering slow retry"` → entering fill mode
- `"webstream down — filling with automation"` → fill started
- `"no media found for next track"` → fill failed (no analyzed media)
- `"broadcast mount not found for next track"` → Task 3B is the fix
- `"smart block produced no items"` → Task 3C is the fix
- `"smart block sequence complete, regenerating"` → normal, should restart immediately

**Key code files:**
- `internal/playout/director.go:966` — `startWebstreamEntry`
- `internal/playout/director.go:1158` — `watchWebstreamPipeline`
- `internal/playout/director.go:1587` — `startSmartBlockEntry`
- `internal/playout/director.go:3328` — `playRandomNextTrack`
- `internal/playout/director.go:3430` — `scheduleStop`
- `internal/playout/director.go:2827` — `handleTrackEnded`
- `internal/web/templates/pages/dashboard/webdj.html:1661` — IRT refresh JS
