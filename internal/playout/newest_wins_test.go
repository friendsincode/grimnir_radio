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

// TestTick_NewestInstanceWins verifies the playout safety net for unresolved
// overlaps: when two is_instance=true media entries on one mount both cover
// now, the director must play the NEWEST instance (latest created_at), never
// the older one. Two sub-cases exercise different code paths:
//
//   - equal starts_at: the newer created_at must win purely on the query's
//     created_at DESC secondary sort (newest claims the mount first).
//   - older starts LATER: the older instance starts after the newer, so under
//     starts_at ASC it is reached LAST in the loop; without a guard its
//     last-write would overwrite the newer. The guard must skip the older once
//     the newer overlapping instance also covers now.
func TestTick_NewestInstanceWins(t *testing.T) {
	now := time.Now().UTC()

	cases := []struct {
		name          string
		olderStartsAt time.Time
		newerStartsAt time.Time
	}{
		{
			name:          "equal starts_at newer created_at wins",
			olderStartsAt: now.Add(-2 * time.Minute),
			newerStartsAt: now.Add(-2 * time.Minute),
		},
		{
			// Newer instance starts EARLIER, older instance starts LATER — both
			// still cover now. Under starts_at ASC the newer is processed first and
			// the older LAST, so plain last-write-wins would leave the older active.
			// Only the guard (skip an older instance when a newer one covers now)
			// makes the newer win here, so this sub-case genuinely exercises it.
			name:          "older starts later must not overwrite newer",
			olderStartsAt: now.Add(-30 * time.Second),
			newerStartsAt: now.Add(-2 * time.Minute),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d, _ := newMockDirector(t, &models.ScheduleEntry{})
			ctx := context.Background()

			stationID := uuid.NewString()
			mountID := uuid.NewString()

			mount := models.Mount{
				ID:         mountID,
				StationID:  stationID,
				Name:       "newest-wins-" + mountID[:8],
				Format:     "mp3",
				Bitrate:    128,
				SampleRate: 44100,
				Channels:   2,
			}
			if err := d.db.Create(&mount).Error; err != nil {
				t.Fatalf("seed mount: %v", err)
			}

			// Two media items so each entry resolves a real source.
			olderMediaID := uuid.NewString()
			newerMediaID := uuid.NewString()
			for _, m := range []models.MediaItem{
				{
					ID:            olderMediaID,
					StationID:     stationID,
					Title:         "Older Track",
					Artist:        "A",
					Path:          "/tmp/older.mp3",
					Duration:      3 * time.Minute,
					AnalysisState: models.AnalysisComplete,
				},
				{
					ID:            newerMediaID,
					StationID:     stationID,
					Title:         "Newer Track",
					Artist:        "B",
					Path:          "/tmp/newer.mp3",
					Duration:      3 * time.Minute,
					AnalysisState: models.AnalysisComplete,
				},
			} {
				if err := d.db.Create(&m).Error; err != nil {
					t.Fatalf("seed media: %v", err)
				}
			}

			// Both instances cover now on the same mount. The newer instance has
			// a later created_at. Set CreatedAt explicitly so gorm's auto-timestamp
			// can't make ordering arbitrary.
			olderID := uuid.NewString()
			newerID := uuid.NewString()
			older := models.ScheduleEntry{
				ID:         olderID,
				StationID:  stationID,
				MountID:    mountID,
				SourceType: "media",
				SourceID:   olderMediaID,
				IsInstance: true,
				StartsAt:   tc.olderStartsAt,
				EndsAt:     now.Add(5 * time.Minute),
				CreatedAt:  now.Add(-1 * time.Hour),
			}
			newer := models.ScheduleEntry{
				ID:         newerID,
				StationID:  stationID,
				MountID:    mountID,
				SourceType: "media",
				SourceID:   newerMediaID,
				IsInstance: true,
				StartsAt:   tc.newerStartsAt,
				EndsAt:     now.Add(5 * time.Minute),
				CreatedAt:  now.Add(-1 * time.Minute),
			}
			// Insert newer first so raw DB (rowid) order is newer-then-older. For the
			// equal-starts case, without created_at DESC the tick loop's last-write-wins
			// would leave the OLDER entry active. For the later-starting-older case, the
			// older is processed last under starts_at ASC and would win without the guard.
			if err := d.db.Create(&newer).Error; err != nil {
				t.Fatalf("seed newer entry: %v", err)
			}
			if err := d.db.Create(&older).Error; err != nil {
				t.Fatalf("seed older entry: %v", err)
			}

			d.markScheduleDirty()

			if err := d.tick(ctx); err != nil {
				t.Fatalf("tick returned error: %v", err)
			}

			d.mu.Lock()
			state, ok := d.active[mountID]
			d.mu.Unlock()
			if !ok {
				t.Fatal("expected an active entry on the mount after tick")
			}
			if state.EntryID != newerID {
				t.Errorf("active entry = %q, want newest %q (older was %q)", state.EntryID, newerID, olderID)
			}
		})
	}
}
