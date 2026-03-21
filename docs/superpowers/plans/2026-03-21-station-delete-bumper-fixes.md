# Station / Bumper / Orphan-Sweep Bug-Fix Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix five confirmed production bugs: broken orphan-sweep SQL, empty-UUID play-history crash, MediaItem.Format template crash, bulk-delete missing cascade, and ScheduleSuppression orphan on station delete.

**Architecture:** All fixes are surgical — no new packages, no new files. Each bug has a single root cause and a minimal targeted fix.

**Tech Stack:** Go 1.24, GORM, PostgreSQL, Go html/template, Chi router.

---

## Bug inventory (from logs + code review)

| # | Severity | File | Root cause | Symptom in logs |
|---|----------|------|------------|-----------------|
| 1 | CRITICAL | `internal/scheduler/service.go:141-143` | `source_id != ''` and `mount_id != ''` are invalid SQL for UUID columns — PostgreSQL throws SQLSTATE 22P02 | `orphan sweep failed … SQLSTATE 22P02` every scheduler tick |
| 2 | HIGH | `internal/playout/director.go:3233-3248` | Some ScheduleEntry objects reach the play-history recorder with `MountID=""`, then GORM tries to INSERT `""` into a UUID column | `failed to record play history … SQLSTATE 22P02` |
| 3 | HIGH | `internal/models/models.go:525` | `Format()` is a pointer-receiver method (`*MediaItem`); template engine receives value type `MediaItem` in range loops and can't call pointer-receiver methods on non-addressable values | `template: archive-detail:91:35: can't evaluate field Format in type models.MediaItem` |
| 4 | HIGH | `internal/web/pages_admin.go:565-567` | `AdminStationsBulk` delete case uses a raw `db.Delete()` on the station row only — no cascade — leaving all child rows (media, schedule entries, smart blocks, mounts, etc.) as orphans | Data corruption; station appears deleted but data persists |
| 5 | LOW | `internal/web/pages_stations.go:289` | `cascadeDeleteStation()` does not delete `schedule_suppressions` rows, leaving orphans after single-station delete | Silent data leak |

---

## Task 1 — Fix orphan-sweep SQL (SQLSTATE 22P02, Bug #1)

**Files:**
- Modify: `internal/scheduler/service.go:133-152`
- Test: `internal/scheduler/service_coverage_test.go` (add test) OR `internal/scheduler/service_test.go`

### Background

The two failing queries:
```sql
-- media_item_orphan
DELETE FROM schedule_entries
WHERE source_type = 'media' AND starts_at > NOW()
  AND source_id IS NOT NULL AND source_id != ''          -- ← invalid for UUID column
  AND source_id NOT IN (SELECT id FROM media_items)

-- mount_orphan
DELETE FROM schedule_entries
WHERE starts_at > NOW()
  AND mount_id IS NOT NULL AND mount_id != ''             -- ← invalid for UUID column
  AND mount_id NOT IN (SELECT id FROM mounts)
```

`schedule_entries.source_id` and `mount_id` are **uuid** type in PostgreSQL. Comparing a UUID column to a string literal `''` causes SQLSTATE 22P02 at parse time. The `!= ''` guards are meaningless anyway — a UUID column cannot store an empty string; it can only be NULL or a valid UUID.

### Fix

Remove the `!= ''` guards. Replace with `NOT IN` using explicit text casting on the subquery side to ensure PostgreSQL does not attempt implicit UUID→text conversion in unpredictable order:

```go
{"media_item_orphan", `DELETE FROM schedule_entries WHERE source_type = 'media' AND starts_at > NOW() AND source_id IS NOT NULL AND source_id NOT IN (SELECT id FROM media_items)`},
{"mount_orphan", `DELETE FROM schedule_entries WHERE starts_at > NOW() AND mount_id IS NOT NULL AND mount_id NOT IN (SELECT id FROM mounts)`},
```

- [ ] **Step 1: Read the current queries**

Open `internal/scheduler/service.go` lines ~133-152 and confirm the two failing query strings.

- [ ] **Step 2: Write a regression test**

In `internal/scheduler/service_coverage_test.go` (or create `internal/scheduler/orphan_sweep_test.go`), add a test that:
1. Creates a station, mount, and a schedule entry whose `mount_id` points to a non-existent mount
2. Calls the orphan sweep (or directly exercises the SQL via the service)
3. Asserts no error is returned and the orphan entry is removed

```go
func TestOrphanSweep_NoSQLSTATE22P02(t *testing.T) {
    // Use SQLite in-memory for unit test; skip UUID validation quirk
    // OR tag as integration and use the test postgres instance
    // The key assertion: s.sweepOrphanedScheduleEntries(ctx) returns nil
}
```

Run: `go test -v -run TestOrphanSweep ./internal/scheduler/...`
Expected: FAIL (service returns error currently)

- [ ] **Step 3: Apply the fix**

In `internal/scheduler/service.go`, remove `AND source_id != ''` from the `media_item_orphan` query and `AND mount_id != ''` from the `mount_orphan` query.

Full replacement:
```go
{"media_item_orphan", `DELETE FROM schedule_entries WHERE source_type = 'media' AND starts_at > NOW() AND source_id IS NOT NULL AND source_id NOT IN (SELECT id FROM media_items)`},
{"mount_orphan", `DELETE FROM schedule_entries WHERE starts_at > NOW() AND mount_id IS NOT NULL AND mount_id NOT IN (SELECT id FROM mounts)`},
```

- [ ] **Step 4: Run test to confirm fix**

Run: `go test -v -run TestOrphanSweep ./internal/scheduler/...`
Expected: PASS

- [ ] **Step 5: Run full test suite**

```bash
make test
```
Expected: all pass

- [ ] **Step 6: Commit**

```bash
git add internal/scheduler/service.go
git commit -m "Fix orphan sweep SQL: remove invalid empty-string guards on UUID columns (SQLSTATE 22P02)"
```

---

## Task 2 — Guard play-history recorder against empty MountID (Bug #2)

**Files:**
- Modify: `internal/playout/director.go` around line 3233

### Background

`recordPlayHistory()` at director.go:3233 assembles a `PlayHistory` struct from a `ScheduleEntry`. If `entry.MountID == ""`, the INSERT into `play_histories.mount_id` (uuid column) fails with SQLSTATE 22P02. This happens when synthetic or incomplete entries (e.g. webstream handover, live DJ events) flow through without a mount.

### Fix

Add an early-return guard:

```go
// Don't record play history if MountID is empty — would fail uuid column constraint.
if entry.MountID == "" {
    return
}
```

Place this immediately after the existing `if title == ""` guard (around line 3212).

- [ ] **Step 1: Read the function**

Read `internal/playout/director.go` lines 3150–3250. Confirm `entry.MountID` is used at 3236.

- [ ] **Step 2: Write a failing test**

In `internal/playout/` (existing test file or new), add:
```go
func TestRecordPlayHistory_EmptyMountID_NoError(t *testing.T) {
    // Setup: director with a mock/sqlite db
    // Call recordPlayHistory with entry.MountID = ""
    // Assert: no error, no DB write attempted
}
```
Run: `go test -v -run TestRecordPlayHistory_EmptyMountID ./internal/playout/...`
Expected: FAIL (currently panics or logs a DB error)

- [ ] **Step 3: Apply the guard**

In `internal/playout/director.go`, after the `if title == ""` guard, add:

```go
if entry.MountID == "" {
    return
}
```

- [ ] **Step 4: Run test**

Run: `go test -v -run TestRecordPlayHistory_EmptyMountID ./internal/playout/...`
Expected: PASS

- [ ] **Step 5: Run full test suite**

```bash
make test
```

- [ ] **Step 6: Commit**

```bash
git add internal/playout/director.go
git commit -m "Guard play-history recorder: skip if MountID is empty to prevent UUID column error"
```

---

## Task 3 — Fix MediaItem.Format() pointer receiver crash in templates (Bug #3)

**Files:**
- Modify: `internal/models/models.go:525`

### Background

The `archive-detail.html` template calls `{{if $media.Format}}` where `$media` is of type `models.MediaItem` (value, not pointer). Go's `html/template` engine cannot call pointer-receiver methods on non-addressable values. Since `Format()` is declared `func (m *MediaItem) Format() string`, the template panics with:

```
can't evaluate field Format in type models.MediaItem
```

### Fix

Change the method to a value receiver. `Format()` only reads fields, never modifies the struct, so this is safe:

```go
// Before:
func (m *MediaItem) Format() string {

// After:
func (m MediaItem) Format() string {
```

- [ ] **Step 1: Read the method**

Read `internal/models/models.go` lines 524–540. Confirm it only reads `m.StorageKey` and `m.Path`.

- [ ] **Step 2: Write a test**

In `internal/models/` add or extend a test:
```go
func TestMediaItem_Format_ValueReceiver(t *testing.T) {
    m := models.MediaItem{StorageKey: "station/ab/cd/file.mp3"}
    // Call Format() on a value (not pointer) — this is what templates do
    got := m.Format()  // would fail to compile if pointer-only
    if got != "mp3" {
        t.Errorf("expected mp3, got %q", got)
    }
}
```
Run: `go test -v -run TestMediaItem_Format ./internal/models/...`
Expected: FAIL (currently pointer receiver, can't call on value type)

- [ ] **Step 3: Change receiver**

In `internal/models/models.go` line 525:
```go
func (m MediaItem) Format() string {
```

- [ ] **Step 4: Run test**

Run: `go test -v -run TestMediaItem_Format ./internal/models/...`
Expected: PASS

- [ ] **Step 5: Run full suite**

```bash
make test
```

- [ ] **Step 6: Commit**

```bash
git add internal/models/models.go
git commit -m "Fix MediaItem.Format(): change to value receiver so templates can call it on range values"
```

---

## Task 4 — Fix AdminStationsBulk delete: add cascade (Bug #4)

**Files:**
- Modify: `internal/web/pages_admin.go:565-567`

### Background

`AdminStationsBulk` handles the admin "All Stations" bulk-action UI. The delete case:

```go
case "delete":
    result := h.db.Where("id IN ?", req.IDs).Delete(&models.Station{})
```

This deletes the `stations` row only. All child data (media items, schedule entries, smart blocks, mounts, playlists, play history, recordings, webhooks, etc.) is left as orphaned rows. The proper cascade function `cascadeDeleteStation()` already exists in `internal/web/pages_stations.go`.

After bulk-delete, the station appears gone but orphaned child data pollutes the DB. This causes:
- Smart block engine to find orphaned schedule entries for non-existent stations
- Re-creating a station leaves behind old data

### Fix

Loop over the IDs and call `cascadeDeleteStation` inside a transaction for each one:

```go
case "delete":
    var deleteErr error
    var totalAffected int64
    for _, id := range req.IDs {
        var station models.Station
        if err := h.db.First(&station, "id = ?", id).Error; err != nil {
            continue // station not found — skip
        }
        err := h.db.Transaction(func(tx *gorm.DB) error {
            return cascadeDeleteStation(tx, id, &station)
        })
        if err != nil {
            deleteErr = err
            break
        }
        totalAffected++
    }
    affected, err = totalAffected, deleteErr
```

Note: `cascadeDeleteStation` is defined in `pages_stations.go` in the same `web` package, so it's directly accessible.

- [ ] **Step 1: Read the existing bulk handler**

Read `internal/web/pages_admin.go` lines 524–586. Confirm the delete case and that `cascadeDeleteStation` is in the same package.

- [ ] **Step 2: Write a failing test**

In `internal/web/pages_admin_test.go` (or a new file), add:
```go
func TestAdminStationsBulk_Delete_CascadesChildData(t *testing.T) {
    // Setup: create station + child records (media item, schedule entry)
    // POST bulk delete
    // Assert: station row gone, media items gone, schedule entries gone
    // Assert: no orphaned rows remain
}
```
Run: `go test -v -run TestAdminStationsBulk_Delete_CascadesChildData ./internal/web/...`
Expected: FAIL (orphaned rows remain)

- [ ] **Step 3: Apply the fix**

Replace the `case "delete":` block in `internal/web/pages_admin.go`:

```go
case "delete":
    var deleteErr error
    var totalAffected int64
    for _, id := range req.IDs {
        var station models.Station
        if err := h.db.First(&station, "id = ?", id).Error; err != nil {
            continue
        }
        if err := h.db.Transaction(func(tx *gorm.DB) error {
            return cascadeDeleteStation(tx, id, &station)
        }); err != nil {
            deleteErr = err
            break
        }
        totalAffected++
    }
    affected, err = totalAffected, deleteErr
```

- [ ] **Step 4: Run test**

Run: `go test -v -run TestAdminStationsBulk_Delete ./internal/web/...`
Expected: PASS

- [ ] **Step 5: Run full suite**

```bash
make test
```

- [ ] **Step 6: Commit**

```bash
git add internal/web/pages_admin.go
git commit -m "Fix AdminStationsBulk delete: use cascadeDeleteStation to prevent orphaned child data"
```

---

## Task 5 — Add ScheduleSuppression to cascadeDeleteStation (Bug #5)

**Files:**
- Modify: `internal/web/pages_stations.go:289` (cascadeDeleteStation function)

### Background

`cascadeDeleteStation` deletes all child records when a station is deleted, but it misses `schedule_suppressions` — the table that records which clock slots were skipped to avoid overlapping programming. These orphan rows are low-severity but pollute the DB.

### Fix

Add the deletion after the ScheduleEntry deletion block (at the top of the function, with the other schedule-related deletes):

```go
// Schedule suppressions
if err := tx.Where("station_id = ?", id).Delete(&models.ScheduleSuppression{}).Error; err != nil {
    return err
}
```

- [ ] **Step 1: Read cascadeDeleteStation**

Read `internal/web/pages_stations.go` lines 289–488. Find the schedule entry deletion block (~line 293).

- [ ] **Step 2: Write a test**

Extend or add to the existing cascade-delete test:
```go
func TestCascadeDeleteStation_CleansScheduleSuppressions(t *testing.T) {
    // Create station + ScheduleSuppression record
    // Call cascadeDeleteStation
    // Assert ScheduleSuppression record is deleted
}
```
Run: `go test -v -run TestCascadeDeleteStation ./internal/web/...`
Expected: FAIL

- [ ] **Step 3: Apply the fix**

In `internal/web/pages_stations.go`, inside `cascadeDeleteStation`, immediately after the ScheduleEntry delete block, add:

```go
// Schedule suppressions
if err := tx.Where("station_id = ?", id).Delete(&models.ScheduleSuppression{}).Error; err != nil {
    return err
}
```

- [ ] **Step 4: Run test**

Run: `go test -v -run TestCascadeDeleteStation ./internal/web/...`
Expected: PASS

- [ ] **Step 5: Run full suite + verify**

```bash
make verify
```

- [ ] **Step 6: Bump version, commit, tag, push**

```bash
# Update internal/version/version.go to next patch (e.g. v1.38.14)
git add internal/scheduler/service.go internal/playout/director.go internal/models/models.go \
        internal/web/pages_admin.go internal/web/pages_stations.go internal/version/version.go
git commit -m "Fix station/bumper/orphan bugs: UUID sweep SQL, play history guard, Format() receiver, bulk cascade, suppression cleanup (v1.38.14)"
git tag -a v1.38.14 -m "Version 1.38.14"
git push origin main && git push origin v1.38.14
```

---

## Verification checklist (run on prod after deploy)

After deploying v1.38.14:

```bash
# On the server:
ssh rlmadmin@192.168.195.11
cd /srv/docker/grimnir_radio
./grimnir pull && ./grimnir up -d

# Wait 2 minutes, then:
./grimnir logs --tail=100 grimnir 2>&1 | grep -E '(ERR|orphan sweep failed|play history|Format)'
# Expected: zero matches for those patterns
```

---

## What this does NOT fix

- **Archive MP3 transcode `signal: killed`** — FFmpeg/ffprobe process is being OOM-killed during audio analysis. This is a resource management issue (needs container memory limit review or concurrency cap on the analyzer). Not a code bug per se; separate investigation required.
- **GStreamer missing MPEG-4 Video codec** — MP4 video files uploaded to station media libraries. GStreamer doesn't have `video/mpeg` decoder. These are not audio files; they should be rejected at upload time with a user-facing error.
