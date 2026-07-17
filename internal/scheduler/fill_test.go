/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/models"
	"github.com/google/uuid"
)

// TestSweepFillWindow verifies that SweepFillWindow deletes only the future
// auto-fill rows for the target station that overlap [from, to), and leaves
// non-fill instances, out-of-window fills, past fills, and other stations'
// fills untouched.
func TestSweepFillWindow(t *testing.T) {
	svc, db := newRunTestService(t)

	now := time.Now().UTC()
	from := now.Add(1 * time.Hour)
	to := from.Add(2 * time.Hour)

	// The fill marker is stored as the JSON string "true" (not a JSON boolean):
	// under the SQLite test driver metadata->>'fill' coerces a JSON boolean true
	// to integer 1, so the production predicate metadata->>'fill' = 'true' would
	// match zero rows. A JSON string "true" reads back as text 'true' on both
	// SQLite and Postgres, keeping one predicate portable across backends.
	fillMeta := func() map[string]any {
		return map[string]any{"fill": "true", "smart_block_id": "sb-x"}
	}

	seed := func(id, station string, start, end time.Time, meta map[string]any) {
		t.Helper()
		e := models.ScheduleEntry{
			ID:                 id,
			StationID:          station,
			StartsAt:           start,
			EndsAt:             end,
			SourceType:         "media",
			IsInstance:         true,
			RecurrenceParentID: nil,
			SeriesID:           nil,
			Metadata:           meta,
		}
		if err := db.Create(&e).Error; err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}

	// Two in-window future FILL rows for st-1 -> should be deleted.
	inWin1 := "11111111-1111-1111-1111-111111111111"
	inWin2 := "22222222-2222-2222-2222-222222222222"
	seed(inWin1, "st-1", from, from.Add(30*time.Minute), fillMeta())
	seed(inWin2, "st-1", from.Add(30*time.Minute), from.Add(1*time.Hour), fillMeta())

	// Non-fill media instance inside the window for st-1 -> survives.
	nonFill := "33333333-3333-3333-3333-333333333333"
	seed(nonFill, "st-1", from.Add(15*time.Minute), from.Add(45*time.Minute), map[string]any{"smart_block_id": "sb-x"})

	// Fill row OUTSIDE the window (future, starts after to) -> survives.
	outWin := "44444444-4444-4444-4444-444444444444"
	seed(outWin, "st-1", to.Add(1*time.Hour), to.Add(90*time.Minute), fillMeta())

	// Past fill overlapping the window but excluded by the now-guard -> survives.
	// StartsAt is before now, EndsAt lands inside the window so it overlaps
	// (starts_at < to AND ends_at > from) but starts_at > now is false.
	pastFill := "55555555-5555-5555-5555-555555555555"
	seed(pastFill, "st-1", now.Add(-2*time.Hour), from.Add(10*time.Minute), fillMeta())

	// st-2 fill inside the window (future) -> survives (station scope).
	st2Fill := "66666666-6666-6666-6666-666666666666"
	seed(st2Fill, "st-2", from, from.Add(30*time.Minute), fillMeta())

	n, err := svc.SweepFillWindow(context.Background(), "st-1", from, to)
	if err != nil {
		t.Fatalf("SweepFillWindow: %v", err)
	}
	if n != 2 {
		t.Fatalf("deleted count = %d, want 2", n)
	}

	exists := func(id string) bool {
		t.Helper()
		var c int64
		if err := db.Model(&models.ScheduleEntry{}).Where("id = ?", id).Count(&c).Error; err != nil {
			t.Fatalf("count %s: %v", id, err)
		}
		return c == 1
	}

	if exists(inWin1) {
		t.Errorf("in-window fill %s should be deleted", inWin1)
	}
	if exists(inWin2) {
		t.Errorf("in-window fill %s should be deleted", inWin2)
	}
	if !exists(nonFill) {
		t.Errorf("non-fill instance %s should survive", nonFill)
	}
	if !exists(outWin) {
		t.Errorf("out-of-window fill %s should survive", outWin)
	}
	if !exists(pastFill) {
		t.Errorf("past fill %s should survive (now-guard)", pastFill)
	}
	if !exists(st2Fill) {
		t.Errorf("st-2 fill %s should survive (station scope)", st2Fill)
	}
}

// TestFillPass_TagsFillRows seeds a station whose recurring smart-block pool has ONE
// parent (a pool of 1) backed by analyzed media, then carves a horizon HOLE with no
// real entry covering [start, start+2h). fillStationHoles must expand the pool block
// into that hole and tag every produced media row with metadata["fill"]=="true" (the
// STRING) and a non-empty smart_block_id.
func TestFillPass_TagsFillRows(t *testing.T) {
	svc, db := newRunTestService(t)
	stationID, mountID := "st-fillpass", "mt-fillpass"
	if err := db.Create(&models.Station{ID: stationID, Name: "Test", Timezone: "UTC"}).Error; err != nil {
		t.Fatalf("create station: %v", err)
	}
	if err := db.Create(&models.Mount{
		ID: mountID, StationID: stationID, Name: "Main",
		URL: "https://example.invalid/main.mp3", Format: "mp3",
	}).Error; err != nil {
		t.Fatalf("create mount: %v", err)
	}

	// A single recurring smart-block parent forms the fill pool. seedNamedSmartBlock
	// gives it loopToFill + 40 analyzed tracks so it can physically fill a 2h hole.
	poolBlockID := seedNamedSmartBlock(t, db, stationID, "sb-pool")
	parent := models.ScheduleEntry{
		ID: "parent-pool", StationID: stationID, MountID: mountID,
		SourceType: "smart_block", SourceID: poolBlockID,
		// Recurring parent template (is_instance=false). Its own window is irrelevant
		// to the hole; the pool query only needs it to be a live recurring parent.
		StartsAt:       time.Now().UTC().Add(-24 * time.Hour),
		EndsAt:         time.Now().UTC().Add(-24 * time.Hour).Add(time.Hour),
		RecurrenceType: models.RecurrenceDaily, IsInstance: false,
		CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	if err := db.Create(&parent).Error; err != nil {
		t.Fatalf("create pool parent: %v", err)
	}

	// Horizon hole: no real entry covers [start, start+2h).
	start := time.Now().UTC().Truncate(time.Minute)
	horizonEnd := start.Add(2 * time.Hour)

	if err := svc.fillStationHoles(context.Background(), stationID, start, horizonEnd); err != nil {
		t.Fatalf("fillStationHoles: %v", err)
	}

	var media []models.ScheduleEntry
	db.Where("station_id = ? AND source_type = 'media'", stationID).
		Order("starts_at ASC").Find(&media)
	if len(media) == 0 {
		t.Fatal("fill pass produced no media in the 2h hole")
	}
	for _, m := range media {
		if got, _ := m.Metadata["fill"].(string); got != "true" {
			t.Errorf("fill row %s: metadata[fill]=%v, want string \"true\"", m.ID, m.Metadata["fill"])
		}
		if sb, _ := m.Metadata["smart_block_id"].(string); sb == "" {
			t.Errorf("fill row %s: missing smart_block_id", m.ID)
		}
	}
}

// TestFillPass_RoundRobinLRU proves fillStationHoles rotates across the recurring pool by
// least-recently-used order instead of always using pool[0]. Two future holes must be
// filled by DIFFERENT pool blocks; a never-filled block outranks a previously-filled one;
// and with no prior fill, ties break deterministically by the lexicographically-smaller
// block id.
func TestFillPass_RoundRobinLRU(t *testing.T) {
	// --- Scenario 1: two holes fill with two different pool blocks. ------------------
	t.Run("two_holes_different_blocks", func(t *testing.T) {
		svc, db := newRunTestService(t)
		stationID, mountID := "st-rr", "mt-rr"
		if err := db.Create(&models.Station{ID: stationID, Name: "Test", Timezone: "UTC"}).Error; err != nil {
			t.Fatalf("create station: %v", err)
		}
		if err := db.Create(&models.Mount{
			ID: mountID, StationID: stationID, Name: "Main",
			URL: "https://example.invalid/main.mp3", Format: "mp3",
		}).Error; err != nil {
			t.Fatalf("create mount: %v", err)
		}

		// Two recurring pool parents, sb-a and sb-b.
		for _, sbID := range []string{"sb-a", "sb-b"} {
			seedNamedSmartBlock(t, db, stationID, sbID)
			p := models.ScheduleEntry{
				ID: "parent-" + sbID, StationID: stationID, MountID: mountID,
				SourceType: "smart_block", SourceID: sbID,
				StartsAt:       time.Now().UTC().Add(-24 * time.Hour),
				EndsAt:         time.Now().UTC().Add(-23 * time.Hour),
				RecurrenceType: models.RecurrenceDaily, IsInstance: false,
				CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			}
			if err := db.Create(&p).Error; err != nil {
				t.Fatalf("create pool parent %s: %v", sbID, err)
			}
		}

		// Fixed future date so both holes are ahead of the pass start. Anchor the day on
		// tomorrow to guarantee 12:00 and 14:00 are in the future.
		day := time.Now().UTC().Add(24 * time.Hour).Truncate(24 * time.Hour)
		hole1Start := day.Add(12 * time.Hour)
		hole1End := day.Add(13 * time.Hour)
		hole2Start := day.Add(14 * time.Hour)
		hole2End := day.Add(15 * time.Hour)

		// A real entry occupies 13:00-14:00 between the two holes so subtractCovered
		// yields two distinct gaps rather than one merged span.
		bridge := models.ScheduleEntry{
			ID: uuid.NewString(), StationID: stationID, MountID: mountID,
			SourceType: "media", SourceID: uuid.NewString(), IsInstance: true,
			StartsAt: hole1End, EndsAt: hole2Start,
		}
		if err := db.Create(&bridge).Error; err != nil {
			t.Fatalf("create bridge entry: %v", err)
		}

		if err := svc.fillStationHoles(context.Background(), stationID, day.Add(11*time.Hour), day.Add(16*time.Hour)); err != nil {
			t.Fatalf("fillStationHoles: %v", err)
		}

		blocksIn := func(from, to time.Time) map[string]bool {
			var media []models.ScheduleEntry
			db.Where("station_id = ? AND source_type = 'media' AND starts_at >= ? AND starts_at < ?",
				stationID, from, to).Find(&media)
			out := map[string]bool{}
			for _, m := range media {
				if m.Metadata["fill"] != "true" {
					continue
				}
				if sb, _ := m.Metadata["smart_block_id"].(string); sb != "" {
					out[sb] = true
				}
			}
			return out
		}

		h1 := blocksIn(hole1Start, hole1End)
		h2 := blocksIn(hole2Start, hole2End)
		if len(h1) != 1 {
			t.Fatalf("hole1 filled by %v blocks, want exactly 1", h1)
		}
		if len(h2) != 1 {
			t.Fatalf("hole2 filled by %v blocks, want exactly 1", h2)
		}
		var b1, b2 string
		for k := range h1 {
			b1 = k
		}
		for k := range h2 {
			b2 = k
		}
		if b1 == b2 {
			t.Fatalf("both holes filled by the same block %q; round-robin LRU should differ", b1)
		}
		// Determinism: with no prior fill, both blocks are never-filled and tie; the tie
		// breaks by the lexicographically-smaller block id, so hole1 (chronologically
		// first) is filled by sb-a.
		if b1 != "sb-a" {
			t.Errorf("hole1 filled by %q, want deterministic tie winner sb-a", b1)
		}
	})

	// --- Scenario 2: a pre-existing fill for sb-a makes sb-b (never filled) win. -----
	t.Run("never_filled_block_wins", func(t *testing.T) {
		svc, db := newRunTestService(t)
		stationID, mountID := "st-rr2", "mt-rr2"
		if err := db.Create(&models.Station{ID: stationID, Name: "Test", Timezone: "UTC"}).Error; err != nil {
			t.Fatalf("create station: %v", err)
		}
		if err := db.Create(&models.Mount{
			ID: mountID, StationID: stationID, Name: "Main",
			URL: "https://example.invalid/main.mp3", Format: "mp3",
		}).Error; err != nil {
			t.Fatalf("create mount: %v", err)
		}
		for _, sbID := range []string{"sb-a", "sb-b"} {
			seedNamedSmartBlock(t, db, stationID, sbID)
			p := models.ScheduleEntry{
				ID: "parent-" + sbID, StationID: stationID, MountID: mountID,
				SourceType: "smart_block", SourceID: sbID,
				StartsAt:       time.Now().UTC().Add(-24 * time.Hour),
				EndsAt:         time.Now().UTC().Add(-23 * time.Hour),
				RecurrenceType: models.RecurrenceDaily, IsInstance: false,
				CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			}
			if err := db.Create(&p).Error; err != nil {
				t.Fatalf("create pool parent %s: %v", sbID, err)
			}
		}

		day := time.Now().UTC().Add(24 * time.Hour).Truncate(24 * time.Hour)

		// sb-a already has a fill row in the past. sb-b has none -> sb-b is LRU.
		priorFill := models.ScheduleEntry{
			ID: uuid.NewString(), StationID: stationID, MountID: mountID,
			SourceType: "media", SourceID: uuid.NewString(), IsInstance: true,
			StartsAt: day.Add(-2 * time.Hour), EndsAt: day.Add(-90 * time.Minute),
			Metadata: map[string]any{"fill": "true", "smart_block_id": "sb-a"},
		}
		if err := db.Create(&priorFill).Error; err != nil {
			t.Fatalf("create prior fill: %v", err)
		}

		holeStart := day.Add(12 * time.Hour)
		holeEnd := day.Add(13 * time.Hour)
		if err := svc.fillStationHoles(context.Background(), stationID, holeStart, holeEnd); err != nil {
			t.Fatalf("fillStationHoles: %v", err)
		}

		var media []models.ScheduleEntry
		db.Where("station_id = ? AND source_type = 'media' AND starts_at >= ? AND starts_at < ?",
			stationID, holeStart, holeEnd).Find(&media)
		got := map[string]bool{}
		for _, m := range media {
			if m.Metadata["fill"] != "true" {
				continue
			}
			if sb, _ := m.Metadata["smart_block_id"].(string); sb != "" {
				got[sb] = true
			}
		}
		if len(got) != 1 || !got["sb-b"] {
			t.Fatalf("fresh hole filled by %v, want only sb-b (never-filled sorts LRU)", got)
		}
	})
}
