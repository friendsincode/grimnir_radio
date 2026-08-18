/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package api

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/dbtest"
	"github.com/friendsincode/grimnir_radio/internal/models"
)

// TestUpsertScheduleLock_ConcurrentColdStart guards the get-or-create race:
// station_id is unique and the upsert selects-then-creates without a lock, so
// concurrent first touches all miss and all Create -> 23505 on Postgres. It must
// converge on one row with no error.
func TestUpsertScheduleLock_ConcurrentColdStart(t *testing.T) {
	db := dbtest.Open(t, &models.Station{}, &models.ScheduleLock{})
	stationID := dbtest.UUID("st")
	if err := db.Create(&models.Station{ID: stationID, OwnerID: dbtest.UUID("owner"), Name: "S"}).Error; err != nil {
		t.Fatalf("seed station: %v", err)
	}
	dates := []time.Time{time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)}

	const n = 24
	start := make(chan struct{})
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_, errs[i] = upsertScheduleLock(context.Background(), db, stationID, 5, "manager", dates)
		}(i)
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("upsertScheduleLock goroutine %d: %v", i, err)
		}
	}
	var count int64
	db.Model(&models.ScheduleLock{}).Where("station_id = ?", stationID).Count(&count)
	if count != 1 {
		t.Fatalf("schedule_locks for station = %d, want exactly 1", count)
	}
}

// TestUpsertScheduleLock_UpdatePersistsLockedDates guards the serializer on the
// update path: LockedDates is a serializer:json column, and a raw-map Update
// bypasses the serializer, so the []time.Time never persists correctly. Updating
// an existing lock must store the new dates and settings.
func TestUpsertScheduleLock_UpdatePersistsLockedDates(t *testing.T) {
	db := dbtest.Open(t, &models.Station{}, &models.ScheduleLock{})
	ctx := context.Background()
	stationID := dbtest.UUID("st")
	if err := db.Create(&models.Station{ID: stationID, OwnerID: dbtest.UUID("owner"), Name: "S"}).Error; err != nil {
		t.Fatalf("seed station: %v", err)
	}
	d1 := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	d2 := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)

	if _, err := upsertScheduleLock(ctx, db, stationID, 5, "manager", []time.Time{d1}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := upsertScheduleLock(ctx, db, stationID, 3, "manager", []time.Time{d1, d2}); err != nil {
		t.Fatalf("update: %v", err)
	}

	var lock models.ScheduleLock
	if err := db.Where("station_id = ?", stationID).Take(&lock).Error; err != nil {
		t.Fatalf("reload: %v", err)
	}
	if len(lock.LockedDates) != 2 {
		t.Fatalf("LockedDates persisted = %d, want 2", len(lock.LockedDates))
	}
	if lock.LockBeforeDays != 3 {
		t.Fatalf("LockBeforeDays = %d, want 3", lock.LockBeforeDays)
	}
}
