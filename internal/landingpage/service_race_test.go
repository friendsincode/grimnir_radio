/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package landingpage

import (
	"context"
	"sync"
	"testing"

	"github.com/rs/zerolog"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/dbtest"
	"github.com/friendsincode/grimnir_radio/internal/models"
)

// TestGetOrCreate_ConcurrentColdStart guards the get-or-create race on the
// station-scoped landing page: station_id is unique, and GetOrCreate
// selects-then-creates without a lock, so concurrent first touches all miss and
// all Create -> 23505 on Postgres. It must converge on one row, no error.
func TestGetOrCreate_ConcurrentColdStart(t *testing.T) {
	db := dbtest.Open(t, &models.LandingPage{})
	svc := NewService(db, nil, "", zerolog.Nop())

	stationID := dbtest.UUID("station")
	errs := runConcurrent(24, func() error {
		_, err := svc.GetOrCreate(context.Background(), stationID)
		return err
	})
	for i, err := range errs {
		if err != nil {
			t.Errorf("GetOrCreate goroutine %d: %v", i, err)
		}
	}
	assertRowCount(t, db, 1, "station_id = ?", stationID)
}

// TestGetOrCreatePlatform_ConcurrentColdStart guards the platform landing page,
// which is the single row with station_id IS NULL. Concurrent first touches
// must still yield exactly one row.
func TestGetOrCreatePlatform_ConcurrentColdStart(t *testing.T) {
	db := dbtest.Open(t, &models.LandingPage{})
	svc := NewService(db, nil, "", zerolog.Nop())

	errs := runConcurrent(24, func() error {
		_, err := svc.GetOrCreatePlatform(context.Background())
		return err
	})
	for i, err := range errs {
		if err != nil {
			t.Errorf("GetOrCreatePlatform goroutine %d: %v", i, err)
		}
	}
	assertRowCount(t, db, 1, "station_id IS NULL")
}

func runConcurrent(n int, fn func() error) []error {
	start := make(chan struct{})
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			errs[i] = fn()
		}(i)
	}
	close(start)
	wg.Wait()
	return errs
}

func assertRowCount(t *testing.T, db *gorm.DB, want int64, where string, args ...any) {
	t.Helper()
	var n int64
	db.Model(&models.LandingPage{}).Where(where, args...).Count(&n)
	if n != want {
		t.Fatalf("landing_pages where %q = %d, want %d", where, n, want)
	}
}
