/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package models_test

import (
	"testing"

	"github.com/friendsincode/grimnir_radio/internal/dbtest"
	"github.com/friendsincode/grimnir_radio/internal/models"
)

// TestOptionalUUIDColumn_ClearToEmpty_PersistsNull guards the update side of the
// nulluuid contract: the Create path is covered elsewhere, but clearing a
// previously-set optional uuid must also round-trip. A struct-field Save runs
// the serializer, so setting the field back to "" has to store SQL NULL (not ""
// -> 22P02), and reading that NULL back must scan to "". A DJ handing off a mount
// or a session losing its user hits exactly this path.
func TestOptionalUUIDColumn_ClearToEmpty_PersistsNull(t *testing.T) {
	db := dbtest.Open(t, &models.LiveSession{})

	ls := &models.LiveSession{
		ID:        dbtest.UUID("ls"),
		StationID: dbtest.UUID("st"),
		MountID:   dbtest.UUID("mnt"),
		UserID:    dbtest.UUID("u"),
	}
	if err := db.Create(ls).Error; err != nil {
		t.Fatalf("create with refs set: %v", err)
	}

	// Clear both optional refs through struct fields so the serializer runs.
	ls.MountID = ""
	ls.UserID = ""
	if err := db.Save(ls).Error; err != nil {
		t.Fatalf("clearing optional uuids to empty must persist as NULL, got: %v", err)
	}

	var reloaded models.LiveSession
	if err := db.First(&reloaded, "id = ?", dbtest.UUID("ls")).Error; err != nil {
		t.Fatalf("reload: %v", err)
	}
	if reloaded.MountID != "" {
		t.Errorf("MountID = %q, want empty (NULL scanned back to \"\")", reloaded.MountID)
	}
	if reloaded.UserID != "" {
		t.Errorf("UserID = %q, want empty (NULL scanned back to \"\")", reloaded.UserID)
	}
}
