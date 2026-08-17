/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package web

import (
	"testing"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/db"
	"github.com/friendsincode/grimnir_radio/internal/dbtest"
	"github.com/friendsincode/grimnir_radio/internal/models"
	"gorm.io/gorm"
)

// TestCascadeDeleteUser_ClearsReferences guards the FK-integrity bug: on Postgres
// every user foreign key is ON DELETE NO ACTION, so deleting a user who owns an
// api key, a notification, a hosted show, or a reviewed request fails (the old
// handler only cleaned station_users). cascadeDeleteUser must delete owned rows
// and null optional references so surviving rows outlive the user.
func TestCascadeDeleteUser_ClearsReferences(t *testing.T) {
	database := dbtest.Open(t)
	if err := db.Migrate(database); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	st := dbtest.UUID("st")
	uid := dbtest.UUID("u")
	other := dbtest.UUID("other")
	database.Create(&models.Station{ID: st, OwnerID: dbtest.UUID("owner"), Name: "S"})
	database.Create(&models.User{ID: uid, Email: "u@t.local"})
	database.Create(&models.User{ID: other, Email: "o@t.local"})

	// Owned children (must be deleted).
	must(t, database.Create(&models.APIKey{ID: dbtest.UUID("k"), UserID: uid, Name: "k", KeyHash: "h", KeyPrefix: "gr_x", ExpiresAt: time.Now().Add(time.Hour)}).Error)
	must(t, database.Create(&models.Notification{ID: dbtest.UUID("n"), UserID: uid, NotificationType: "storage_warning", Channel: "in_app", Body: "b"}).Error)
	must(t, database.Create(&models.StationUser{ID: dbtest.UUID("su"), UserID: uid, StationID: st, Role: models.StationRoleDJ}).Error)
	must(t, database.Create(&models.ScheduleRequest{ID: dbtest.UUID("req"), RequesterID: uid, StationID: st}).Error)

	// Surviving rows that only reference the user optionally (must be nulled).
	must(t, database.Create(&models.Show{ID: dbtest.UUID("sh"), StationID: st, Name: "Morning", HostUserID: &uid}).Error)
	reviewer := uid
	must(t, database.Create(&models.ScheduleRequest{ID: dbtest.UUID("req2"), RequesterID: other, StationID: st, ReviewedBy: &reviewer}).Error)

	// The fix.
	if err := database.Transaction(func(tx *gorm.DB) error {
		return cascadeDeleteUser(tx, &models.User{ID: uid})
	}); err != nil {
		t.Fatalf("cascadeDeleteUser: %v", err)
	}

	// User and its owned rows are gone.
	assertCount(t, database, &models.User{}, "id = ?", uid, 0)
	assertCount(t, database, &models.APIKey{}, "user_id = ?", uid, 0)
	assertCount(t, database, &models.Notification{}, "user_id = ?", uid, 0)
	assertCount(t, database, &models.ScheduleRequest{}, "requester_id = ?", uid, 0)

	// The show survives with its host cleared.
	var show models.Show
	must(t, database.First(&show, "id = ?", dbtest.UUID("sh")).Error)
	if show.HostUserID != nil {
		t.Fatalf("show host should be nulled, got %v", *show.HostUserID)
	}
	// The other user's request survives with reviewer cleared.
	var req models.ScheduleRequest
	must(t, database.First(&req, "id = ?", dbtest.UUID("req2")).Error)
	if req.ReviewedBy != nil {
		t.Fatalf("reviewer should be nulled, got %v", *req.ReviewedBy)
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
}

func assertCount(t *testing.T, db *gorm.DB, model any, where string, arg any, want int64) {
	t.Helper()
	var n int64
	db.Model(model).Where(where, arg).Count(&n)
	if n != want {
		t.Fatalf("count(%T where %s) = %d, want %d", model, where, n, want)
	}
}
