/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package models_test

import (
	"testing"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/dbtest"
	"github.com/friendsincode/grimnir_radio/internal/models"
	"gorm.io/gorm"
)

// TestOptionalUUIDColumns_AcceptEmpty is a schema-integrity contract: a uuid
// column that represents an OPTIONAL reference must persist an empty value as
// NULL. A non-pointer type:uuid string field writes an empty string when unset,
// which Postgres rejects (SQLSTATE 22P02) unless the column is nulluuid-
// serialized. Each case creates the model with its required fields set and the
// optional reference left empty; a failure here is a real bug — the same class
// that silently dropped live-DJ PlayHistory rows in production.
func TestOptionalUUIDColumns_AcceptEmpty(t *testing.T) {
	cases := []struct {
		name    string
		migrate []any
		seed    func(db *gorm.DB) // seed FK parents so the target column is the only variable
		make    func() any
	}{
		{
			name:    "Notification.ReferenceID",
			migrate: []any{&models.User{}, &models.Notification{}},
			seed:    func(db *gorm.DB) { db.Create(&models.User{ID: dbtest.UUID("u"), Email: "u@t.local"}) },
			make: func() any {
				return &models.Notification{ID: dbtest.UUID("n"), UserID: dbtest.UUID("u"), NotificationType: "storage_warning", Channel: "in_app", Body: "b"}
			},
		},
		{
			name:    "AuditLog.ResourceID",
			migrate: []any{&models.AuditLog{}},
			make: func() any {
				return &models.AuditLog{ID: dbtest.UUID("a"), Timestamp: time.Now(), Action: "login"}
			},
		},
		{
			name:    "Network.OwnerID",
			migrate: []any{&models.Network{}},
			make:    func() any { return &models.Network{ID: dbtest.UUID("net"), Name: "N"} },
		},
		{
			name:    "PrioritySource.SourceID",
			migrate: []any{&models.PrioritySource{}},
			make: func() any {
				return &models.PrioritySource{ID: dbtest.UUID("ps"), StationID: dbtest.UUID("st"), MountID: dbtest.UUID("mnt")}
			},
		},
		{
			name:    "MountPlayoutState.EntryID/MediaID",
			migrate: []any{&models.MountPlayoutState{}},
			make: func() any {
				return &models.MountPlayoutState{MountID: dbtest.UUID("mnt"), StationID: dbtest.UUID("st")}
			},
		},
		{
			name:    "ExecutorState.Current/NextSourceID",
			migrate: []any{&models.ExecutorState{}},
			make: func() any {
				return &models.ExecutorState{ID: dbtest.UUID("es"), StationID: dbtest.UUID("st"), MountID: dbtest.UUID("mnt")}
			},
		},
		{
			name:    "LiveSession.MountID/UserID",
			migrate: []any{&models.LiveSession{}},
			make: func() any {
				return &models.LiveSession{ID: dbtest.UUID("ls"), StationID: dbtest.UUID("st")}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := dbtest.Open(t, tc.migrate...)
			if tc.seed != nil {
				tc.seed(db)
			}
			if err := db.Create(tc.make()).Error; err != nil {
				t.Fatalf("%s: creating with the optional uuid empty must persist as NULL, got: %v", tc.name, err)
			}
		})
	}
}
