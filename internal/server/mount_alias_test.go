/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package server

import (
	"testing"

	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

func TestAliasSiblingMountNames(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&models.Mount{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	stationA := uuid.NewString()
	stationB := uuid.NewString()
	seed := []models.Mount{
		{ID: uuid.NewString(), StationID: stationA, Name: "main-a", Format: "mp3"},
		{ID: uuid.NewString(), StationID: stationA, Name: "fic-stream", Format: "mp3"}, // custom alias
		{ID: uuid.NewString(), StationID: stationB, Name: "main-b", Format: "mp3"},
	}
	for _, m := range seed {
		if err := db.Create(&m).Error; err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	// The custom alias resolves to its station's other mount, and only that.
	got := aliasSiblingMountNames(db, "fic-stream")
	if len(got) != 1 || got[0] != "main-a" {
		t.Errorf("fic-stream siblings = %v, want [main-a]", got)
	}
	// Never crosses into another station's mounts.
	for _, n := range got {
		if n == "main-b" {
			t.Error("alias must not resolve across stations")
		}
	}
	// The main mount's siblings exclude itself and include the alias.
	if g := aliasSiblingMountNames(db, "main-a"); len(g) != 1 || g[0] != "fic-stream" {
		t.Errorf("main-a siblings = %v, want [fic-stream]", g)
	}
	// An unknown name resolves to nothing (falls through to 404).
	if g := aliasSiblingMountNames(db, "does-not-exist"); g != nil {
		t.Errorf("unknown mount siblings = %v, want nil", g)
	}
	// Guard rails.
	if aliasSiblingMountNames(db, "") != nil || aliasSiblingMountNames(nil, "x") != nil {
		t.Error("empty name / nil db should return nil")
	}
}
