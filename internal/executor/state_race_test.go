/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package executor

import (
	"context"
	"sync"
	"testing"

	"github.com/rs/zerolog"

	"github.com/friendsincode/grimnir_radio/internal/dbtest"
	"github.com/friendsincode/grimnir_radio/internal/models"
)

// TestGetState_ConcurrentColdStart guards the get-or-create race: ExecutorState
// has a unique index on station_id, and GetState selects-then-creates without
// holding a lock across the DB round trip. Concurrent first touches of a fresh
// station all miss the cache, all see ErrRecordNotFound, and all Create; on
// Postgres every loser hits 23505. GetState must converge on one row with no
// error regardless of how many callers arrive together.
func TestGetState_ConcurrentColdStart(t *testing.T) {
	db := dbtest.Open(t, &models.ExecutorState{})
	sm := NewStateManager(db, zerolog.Nop())

	stationID := dbtest.UUID("station")

	const n = 24
	start := make(chan struct{})
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_, errs[i] = sm.GetState(context.Background(), stationID)
		}(i)
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("GetState goroutine %d: %v", i, err)
		}
	}

	var count int64
	db.Model(&models.ExecutorState{}).Where("station_id = ?", stationID).Count(&count)
	if count != 1 {
		t.Fatalf("executor_states for station = %d, want exactly 1", count)
	}
}
