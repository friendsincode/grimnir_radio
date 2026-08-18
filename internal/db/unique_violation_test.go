/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package db

import (
	"errors"
	"testing"

	"github.com/friendsincode/grimnir_radio/internal/dbtest"
	"github.com/friendsincode/grimnir_radio/internal/models"
)

// TestIsUniqueViolation covers the helper that create endpoints use to turn a
// duplicate key into a clean error: a real 23505 from Postgres must report true,
// while nil and unrelated errors must report false.
func TestIsUniqueViolation(t *testing.T) {
	database := dbtest.Open(t, &models.Station{})

	// stations.name is unique; a second row with the same name violates it.
	if err := database.Create(&models.Station{ID: dbtest.UUID("a"), OwnerID: dbtest.UUID("o"), Name: "dup"}).Error; err != nil {
		t.Fatalf("seed first: %v", err)
	}
	dupErr := database.Create(&models.Station{ID: dbtest.UUID("b"), OwnerID: dbtest.UUID("o"), Name: "dup"}).Error
	if dupErr == nil {
		t.Fatal("expected duplicate-name insert to fail")
	}
	if !IsUniqueViolation(dupErr) {
		t.Errorf("IsUniqueViolation(dupErr) = false, want true (err: %v)", dupErr)
	}

	if IsUniqueViolation(nil) {
		t.Error("IsUniqueViolation(nil) = true, want false")
	}
	if IsUniqueViolation(errors.New("some other error")) {
		t.Error("IsUniqueViolation(plain) = true, want false")
	}
}
