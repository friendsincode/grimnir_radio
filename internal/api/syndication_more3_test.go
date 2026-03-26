/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package api

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/models"
	"github.com/friendsincode/grimnir_radio/internal/syndication"
)

// newSyndicationMore3Test builds a SyndicationAPI backed by a SQLite DB whose
// underlying connection we can close to force DB errors on subsequent operations.
func newSyndicationMore3Test(t *testing.T) (*SyndicationAPI, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "syn3.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(
		&models.Network{},
		&models.NetworkShow{},
		&models.NetworkSubscription{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	svc := syndication.NewService(db, zerolog.Nop())
	api := &API{db: db, logger: zerolog.Nop()}
	return NewSyndicationAPI(api, svc), db
}

// closeDB closes the underlying sql.DB so that any subsequent GORM operation fails.
func closeDB(t *testing.T, db *gorm.DB) {
	t.Helper()
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sql.DB: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close sql.DB: %v", err)
	}
}

// TestSyndicationAPI_DeleteNetwork_DBError covers the 500 branch of handleDeleteNetwork.
// The handler calls s.db.Delete(...) directly; we close the underlying connection to
// force that call to return an error.
func TestSyndicationAPI_DeleteNetwork_DBError(t *testing.T) {
	s, db := newSyndicationMore3Test(t)

	// Seed a network so the id is valid.
	db.Create(&models.Network{ID: "net-dberr", Name: "DBErr", OwnerID: "u1"}) //nolint:errcheck

	// Close the DB to make Delete fail.
	closeDB(t, db)

	req := httptest.NewRequest("DELETE", "/", nil)
	req = withChiParam(req, "id", "net-dberr")
	rr := httptest.NewRecorder()
	s.handleDeleteNetwork(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("handleDeleteNetwork DB error: got %d, want 500", rr.Code)
	}
}

// TestSyndicationAPI_DeleteNetworkShow_DBError covers the 500 branch of handleDeleteNetworkShow.
// DeleteNetworkShow first deletes subscriptions, then the show itself; closing the connection
// causes the first Delete (subscriptions) to error.
func TestSyndicationAPI_DeleteNetworkShow_DBError(t *testing.T) {
	s, db := newSyndicationMore3Test(t)

	netID := "net-y"
	db.Create(&models.NetworkShow{ID: "ns-dberr", NetworkID: &netID, Name: "DBErr"}) //nolint:errcheck

	closeDB(t, db)

	req := httptest.NewRequest("DELETE", "/", nil)
	req = withChiParam(req, "id", "ns-dberr")
	rr := httptest.NewRecorder()
	s.handleDeleteNetworkShow(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("handleDeleteNetworkShow DB error: got %d, want 500", rr.Code)
	}
}

// TestSyndicationAPI_DeleteSubscription_DBError covers the 500 branch of handleDeleteSubscription.
// Unsubscribe calls s.db.Delete; closing the connection forces it to return an error.
func TestSyndicationAPI_DeleteSubscription_DBError(t *testing.T) {
	s, db := newSyndicationMore3Test(t)

	db.Create(&models.NetworkSubscription{ //nolint:errcheck
		ID:            "sub-dberr",
		NetworkShowID: "ns-some",
		StationID:     "s1",
	})

	closeDB(t, db)

	req := httptest.NewRequest("DELETE", "/", nil)
	req = withChiParam(req, "id", "sub-dberr")
	rr := httptest.NewRecorder()
	s.handleDeleteSubscription(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("handleDeleteSubscription DB error: got %d, want 500", rr.Code)
	}
}
