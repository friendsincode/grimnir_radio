/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package db

import (
	"testing"

	"github.com/friendsincode/grimnir_radio/internal/dbtest"
	"github.com/friendsincode/grimnir_radio/internal/models"
	"gorm.io/gorm"
)

// TestAfterCallback_AllBranches drives every branch of the after-callback:
// the happy path, a real query error, a missing start time, a non-time start
// value, and an empty table name.
func TestAfterCallback_AllBranches(t *testing.T) {
	db := dbtest.Open(t, &models.Station{})
	if err := RegisterCallbacks(db); err != nil {
		t.Fatalf("RegisterCallbacks: %v", err)
	}

	// Success path: before sets the start time, after records duration, no error.
	if err := db.Create(&models.Station{ID: dbtest.UUID("st1"), OwnerID: dbtest.UUID("owner"), Name: "S"}).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	// Error path: duplicate primary key -> db.Error set (not RecordNotFound).
	_ = db.Create(&models.Station{ID: dbtest.UUID("st1"), OwnerID: dbtest.UUID("owner"), Name: "S2"}).Error

	// Empty table name: a raw query has no table, hitting the "unknown" default
	// (the real before-callback from RegisterCallbacks still sets the start time).
	var one int
	db.Raw("SELECT 1").Scan(&one)

	// Missing start time: register only an after-callback, so InstanceGet misses.
	noBefore := dbtest.Open(t, &models.Station{})
	noBefore.Callback().Query().After("gorm:query").Register("test:after_only", afterCallback("query"))
	var s models.Station
	_ = noBefore.First(&s, "id = ?", dbtest.UUID("nope")).Error

	// Non-time start value: a before-callback that stores a string trips the
	// time.Time type assertion.
	weird := dbtest.Open(t)
	weird.Callback().Query().Before("gorm:query").Register("test:bad_before", func(d *gorm.DB) {
		d.InstanceSet(_startTime, "not-a-time")
	})
	weird.Callback().Query().After("gorm:query").Register("test:after_weird", afterCallback("query"))
	var two int
	weird.Raw("SELECT 1").Scan(&two)
}
