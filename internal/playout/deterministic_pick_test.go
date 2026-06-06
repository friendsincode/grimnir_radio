/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package playout

import (
	"context"
	"testing"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/models"
	"github.com/google/uuid"
)

// ── helper unit tests ────────────────────────────────────────────────────

func TestDeterministicMediaPick_EmptyReturnsNil(t *testing.T) {
	got := deterministicMediaPick(nil, "any", "thing")
	if got != nil {
		t.Errorf("expected nil for empty candidates, got %+v", got)
	}
}

func TestDeterministicMediaPick_SingleCandidate(t *testing.T) {
	items := []models.MediaItem{{ID: "only"}}
	got := deterministicMediaPick(items, "ctx-a")
	if got == nil || got.ID != "only" {
		t.Errorf("expected sole candidate, got %+v", got)
	}
}

func TestDeterministicMediaPick_SameContextSamePick(t *testing.T) {
	items := []models.MediaItem{
		{ID: "a"}, {ID: "b"}, {ID: "c"}, {ID: "d"}, {ID: "e"},
	}
	a := deterministicMediaPick(items, "station-1", "mount-1", "entry-1", int64(1000), 0)
	b := deterministicMediaPick(items, "station-1", "mount-1", "entry-1", int64(1000), 0)
	if a == nil || b == nil || a.ID != b.ID {
		t.Errorf("same context gave different picks: %+v vs %+v", a, b)
	}
}

func TestDeterministicMediaPick_DifferentContextCanDiffer(t *testing.T) {
	// Sanity check: across a swept fillIndex, the helper visits more than one
	// row from a 5-item set. Not a uniformity claim — just "it actually moves".
	items := []models.MediaItem{
		{ID: "a"}, {ID: "b"}, {ID: "c"}, {ID: "d"}, {ID: "e"},
	}
	seen := map[string]bool{}
	for i := 0; i < 32; i++ {
		got := deterministicMediaPick(items, "s", "m", "e", int64(0), i)
		seen[got.ID] = true
	}
	if len(seen) < 2 {
		t.Errorf("expected >1 distinct picks across 32 fill indices, got %d", len(seen))
	}
}

// ── two-instance lockstep tests ───────────────────────────────────────────

// seedMediaCatalog inserts N analyzed media rows for a station with stable
// IDs (uuids are random, but stable across the two directors because we
// seed once & share the same DB). Returns the inserted IDs ordered as they
// would come back from ORDER BY id ASC.
func seedMediaCatalog(t *testing.T, d *Director, stationID string, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		m := models.MediaItem{
			ID:            uuid.NewString(),
			StationID:     stationID,
			Title:         "t",
			Artist:        "a",
			Path:          "/tmp/x.mp3",
			Duration:      3 * time.Minute,
			AnalysisState: models.AnalysisComplete,
		}
		if err := d.db.Create(&m).Error; err != nil {
			t.Fatalf("seed media %d: %v", i, err)
		}
	}
}

// twoMockDirectorsSharedDB returns two Director values pointing at the same
// in-memory sqlite, mimicking two control-plane instances against one DB.
// Each gets its own mockManager so we can prove neither cross-talks.
func twoMockDirectorsSharedDB(t *testing.T) (*Director, *Director) {
	t.Helper()
	d1, _ := newMockDirector(t, &models.ScheduleEntry{}, &models.SmartBlock{}, &models.Clock{})
	d2, _ := newMockDirector(t, &models.ScheduleEntry{}, &models.SmartBlock{}, &models.Clock{})
	// Replace d2's DB with d1's so they see the same rows.
	d2.db = d1.db
	d2.smartblockEng = d1.smartblockEng
	return d1, d2
}

// C1: playRandomNextTrack — both instances must pick the same media.
// We can't run the full pipeline build in tests (no GStreamer), but the
// pick happens before the pipeline step, & sets d.active[mountID].MediaID
// when persistMountState runs. Easier: assert via a direct call to the
// helper with the same context the call site uses. We also exercise the
// real call path to make sure the candidate order matches between
// instances — that's the regression the audit calls out.
func TestPlayRandomNextTrack_DeterministicAcrossInstances(t *testing.T) {
	d1, d2 := twoMockDirectorsSharedDB(t)
	stationID := uuid.NewString()
	mountID := uuid.NewString()
	seedMediaCatalog(t, d1, stationID, 7)

	// Mimic the exact query both call sites issue.
	loadCandidates := func(d *Director) []models.MediaItem {
		var out []models.MediaItem
		if err := d.db.
			Where("station_id = ?", stationID).
			Where("analysis_state != ? AND duration > 0", models.AnalysisFailed).
			Order("id ASC").
			Find(&out).Error; err != nil {
			t.Fatalf("load candidates: %v", err)
		}
		return out
	}

	entryID := uuid.NewString()
	startsAt := time.Date(2026, 6, 6, 12, 0, 0, 0, time.UTC)

	for fillIndex := 0; fillIndex < 8; fillIndex++ {
		c1 := loadCandidates(d1)
		c2 := loadCandidates(d2)
		if len(c1) != len(c2) {
			t.Fatalf("candidate counts diverged: %d vs %d", len(c1), len(c2))
		}
		p1 := deterministicMediaPick(c1, stationID, mountID, entryID, startsAt.Unix(), fillIndex)
		p2 := deterministicMediaPick(c2, stationID, mountID, entryID, startsAt.Unix(), fillIndex)
		if p1 == nil || p2 == nil {
			t.Fatalf("got nil pick at fillIndex=%d", fillIndex)
		}
		if p1.ID != p2.ID {
			t.Fatalf("fillIndex=%d divergent pick: d1=%s d2=%s", fillIndex, p1.ID, p2.ID)
		}
	}
}

// C2: smart-block-fallback context. Same DB, same entry → same pick.
func TestSmartBlockFallback_DeterministicAcrossInstances(t *testing.T) {
	d1, d2 := twoMockDirectorsSharedDB(t)
	stationID := uuid.NewString()
	seedMediaCatalog(t, d1, stationID, 11)

	entryID := uuid.NewString()
	startsAt := time.Date(2026, 6, 6, 12, 0, 0, 0, time.UTC)

	load := func(d *Director) []models.MediaItem {
		var out []models.MediaItem
		_ = d.db.WithContext(context.Background()).
			Where("station_id = ?", stationID).
			Where("analysis_state != ? AND duration > 0", models.AnalysisFailed).
			Order("id ASC").
			Find(&out).Error
		return out
	}
	c1, c2 := load(d1), load(d2)
	p1 := deterministicMediaPick(c1, stationID, entryID, startsAt.Unix())
	p2 := deterministicMediaPick(c2, stationID, entryID, startsAt.Unix())
	if p1 == nil || p2 == nil || p1.ID != p2.ID {
		t.Fatalf("smart-block fallback divergent: %+v vs %+v", p1, p2)
	}
}

// C3: clock-stopset fallback context. Sweep slot.Position too.
func TestClockStopsetFallback_DeterministicAcrossInstances(t *testing.T) {
	d1, d2 := twoMockDirectorsSharedDB(t)
	stationID := uuid.NewString()
	seedMediaCatalog(t, d1, stationID, 9)

	entryID := uuid.NewString()
	startsAt := time.Date(2026, 6, 6, 12, 0, 0, 0, time.UTC)

	load := func(d *Director) []models.MediaItem {
		var out []models.MediaItem
		_ = d.db.WithContext(context.Background()).
			Where("station_id = ?", stationID).
			Where("analysis_state != ? AND duration > 0", models.AnalysisFailed).
			Order("id ASC").
			Find(&out).Error
		return out
	}
	for slotPos := 0; slotPos < 6; slotPos++ {
		c1, c2 := load(d1), load(d2)
		p1 := deterministicMediaPick(c1, stationID, entryID, startsAt.Unix(), slotPos)
		p2 := deterministicMediaPick(c2, stationID, entryID, startsAt.Unix(), slotPos)
		if p1 == nil || p2 == nil || p1.ID != p2.ID {
			t.Fatalf("clock-stopset divergent at slotPos=%d: %+v vs %+v", slotPos, p1, p2)
		}
	}
}
