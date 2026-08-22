/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package recording

import (
	"context"
	"testing"

	"github.com/friendsincode/grimnir_radio/internal/dbtest"
	"github.com/friendsincode/grimnir_radio/internal/models"
)

// TestDeleteRecording_DecrementsQuota guards #86: every existing delete test used
// a zero-size recording, so the SizeBytes>0 branch that decrements station and
// per-DJ recording_storage_used never ran. If it broke, storage accounting drifts
// upward and DJs get wrongly quota-blocked over time. Also covers the GREATEST
// clamp so usage never goes negative.
func TestDeleteRecording_DecrementsQuota(t *testing.T) {
	svc, db := newSvc(t)
	ctx := context.Background()

	st := dbtest.UUID("st")
	uid := dbtest.UUID("dj")
	must(t, db.Create(&models.Station{ID: st, OwnerID: dbtest.UUID("o"), Name: "S", RecordingStorageUsed: 1000}).Error)
	must(t, db.Create(&models.StationUser{ID: dbtest.UUID("su"), StationID: st, UserID: uid, Role: models.StationRoleDJ, RecordingStorageUsed: 1000}).Error)

	rec := dbtest.UUID("rec")
	must(t, db.Create(&models.Recording{ID: rec, StationID: st, UserID: uid, MountID: dbtest.UUID("mnt"), Status: models.RecordingStatusComplete, SizeBytes: 300}).Error)

	if err := svc.DeleteRecording(ctx, rec); err != nil {
		t.Fatalf("DeleteRecording: %v", err)
	}

	var station models.Station
	must(t, db.First(&station, "id = ?", st).Error)
	if station.RecordingStorageUsed != 700 {
		t.Errorf("station storage = %d, want 700 (1000-300)", station.RecordingStorageUsed)
	}
	var su models.StationUser
	must(t, db.First(&su, "station_id = ? AND user_id = ?", st, uid).Error)
	if su.RecordingStorageUsed != 700 {
		t.Errorf("dj storage = %d, want 700", su.RecordingStorageUsed)
	}

	// Clamp: deleting a recording larger than remaining usage floors at 0.
	must(t, db.Model(&models.Station{}).Where("id = ?", st).Update("recording_storage_used", 100).Error)
	rec2 := dbtest.UUID("rec2")
	must(t, db.Create(&models.Recording{ID: rec2, StationID: st, UserID: uid, MountID: dbtest.UUID("mnt"), Status: models.RecordingStatusComplete, SizeBytes: 500}).Error)
	if err := svc.DeleteRecording(ctx, rec2); err != nil {
		t.Fatalf("DeleteRecording 2: %v", err)
	}
	must(t, db.First(&station, "id = ?", st).Error)
	if station.RecordingStorageUsed != 0 {
		t.Errorf("station storage = %d, want 0 (clamped, not negative)", station.RecordingStorageUsed)
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
}
