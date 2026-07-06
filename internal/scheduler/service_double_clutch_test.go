/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package scheduler

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/models"
	"github.com/jackc/pgx/v5/pgconn"
)

// A recurring playlist whose occurrence lands on top of another show is a
// genuine double-booking. In prod the Postgres overlap trigger rejected the
// instance insert (23514) and, because the parent is re-expanded every tick,
// the same doomed insert re-fired ~866 times/hour — the "double-clutch". The
// materializer must recognise the conflict and skip silently instead of
// retrying an insert it can't win.
//
// The old guard only looked for overlapping source_type='media' rows, so a
// clash with a playlist/webstream/smart-block instance slipped through. On
// sqlite (no trigger) that produced a duplicate overlapping row, which is what
// this test counts.
func TestMaterializeDirectInstance_SkipsOverlapWithNonMediaEntry(t *testing.T) {
	svc, db := newMaterializationTestService(t)
	ctx := context.Background()

	stationID := "station-dc"
	mountID := "mount-dc"
	createTestMount(t, db, stationID, mountID)

	start := time.Now().UTC().Truncate(time.Second)
	end := start.Add(time.Hour)

	// An existing webstream instance already occupies the whole window.
	occupied := models.ScheduleEntry{
		ID:         "00000000-0000-0000-0000-0000000000aa",
		StationID:  stationID,
		MountID:    mountID,
		StartsAt:   start,
		EndsAt:     end,
		SourceType: "webstream",
		SourceID:   "11111111-1111-1111-1111-111111111111",
		IsInstance: true,
	}
	if err := db.Create(&occupied).Error; err != nil {
		t.Fatalf("seed occupied entry: %v", err)
	}

	// A recurring playlist occurrence covering the same window.
	occ := models.ScheduleEntry{
		StationID:  stationID,
		MountID:    mountID,
		StartsAt:   start,
		EndsAt:     end,
		SourceType: "playlist",
		SourceID:   "22222222-2222-2222-2222-222222222222",
	}
	parentID := "33333333-3333-3333-3333-333333333333"

	if err := svc.materializeDirectInstanceEntry(ctx, stationID, occ, mountID, &parentID); err != nil {
		t.Fatalf("materialize returned error, want silent skip: %v", err)
	}

	var count int64
	db.Model(&models.ScheduleEntry{}).Where("station_id = ?", stationID).Count(&count)
	if count != 1 {
		t.Fatalf("expected the overlapping playlist instance to be skipped (1 row), got %d rows", count)
	}
}

// The trigger carves out one legal overlap: an instance may overlap a
// smart_block PARENT template (is_instance=false, source_type=smart_block),
// because the smart block hasn't been materialized into concrete media yet.
// The pre-check must honour the same carve-out or it would refuse to
// materialize playlists that share a window with a smart-block schedule.
func TestMaterializeDirectInstance_AllowsOverlapWithSmartBlockParent(t *testing.T) {
	svc, db := newMaterializationTestService(t)
	ctx := context.Background()

	stationID := "station-sb"
	mountID := "mount-sb"
	createTestMount(t, db, stationID, mountID)

	start := time.Now().UTC().Truncate(time.Second)
	end := start.Add(time.Hour)

	parentTemplate := models.ScheduleEntry{
		ID:         "00000000-0000-0000-0000-0000000000bb",
		StationID:  stationID,
		MountID:    mountID,
		StartsAt:   start,
		EndsAt:     end,
		SourceType: "smart_block",
		SourceID:   "44444444-4444-4444-4444-444444444444",
		IsInstance: false,
	}
	if err := db.Create(&parentTemplate).Error; err != nil {
		t.Fatalf("seed smart_block parent: %v", err)
	}

	occ := models.ScheduleEntry{
		StationID:  stationID,
		MountID:    mountID,
		StartsAt:   start,
		EndsAt:     end,
		SourceType: "playlist",
		SourceID:   "55555555-5555-5555-5555-555555555555",
	}

	if err := svc.materializeDirectInstanceEntry(ctx, stationID, occ, mountID, nil); err != nil {
		t.Fatalf("materialize returned error: %v", err)
	}

	var instances int64
	db.Model(&models.ScheduleEntry{}).
		Where("station_id = ? AND is_instance = true", stationID).
		Count(&instances)
	if instances != 1 {
		t.Fatalf("expected the playlist instance to materialize over the smart_block parent, got %d instances", instances)
	}
}

// A window with nothing else in it materializes normally — the guard must not
// over-reach and block legitimate inserts.
func TestMaterializeDirectInstance_CreatesWhenSlotIsFree(t *testing.T) {
	svc, db := newMaterializationTestService(t)
	ctx := context.Background()

	stationID := "station-free"
	mountID := "mount-free"
	createTestMount(t, db, stationID, mountID)

	start := time.Now().UTC().Truncate(time.Second)
	occ := models.ScheduleEntry{
		StationID:  stationID,
		MountID:    mountID,
		StartsAt:   start,
		EndsAt:     start.Add(time.Hour),
		SourceType: "playlist",
		SourceID:   "66666666-6666-6666-6666-666666666666",
	}

	if err := svc.materializeDirectInstanceEntry(ctx, stationID, occ, mountID, nil); err != nil {
		t.Fatalf("materialize returned error: %v", err)
	}

	var instances int64
	db.Model(&models.ScheduleEntry{}).
		Where("station_id = ? AND is_instance = true", stationID).
		Count(&instances)
	if instances != 1 {
		t.Fatalf("expected 1 materialized instance, got %d", instances)
	}
}

func TestIsScheduleOverlapError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"pg 23514", &pgconn.PgError{Code: "23514"}, true},
		{"pg other code", &pgconn.PgError{Code: "23503"}, false},
		{"message match", errors.New("ERROR: overlapping programming is not allowed for station x"), true},
		{"unrelated", errors.New("connection refused"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isScheduleOverlapError(tc.err); got != tc.want {
				t.Fatalf("isScheduleOverlapError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}
