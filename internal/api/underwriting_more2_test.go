/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/models"
	"github.com/friendsincode/grimnir_radio/internal/underwriting"
)

func newUnderwritingMore2Test(t *testing.T) (*UnderwritingAPI, *gorm.DB) {
	t.Helper()
	dbPath := t.TempDir() + "/uw2.db"
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(
		&models.Sponsor{},
		&models.UnderwritingObligation{},
		&models.UnderwritingSpot{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	svc := underwriting.NewService(db, zerolog.Nop())
	api := &API{db: db, logger: zerolog.Nop()}
	return NewUnderwritingAPI(api, svc), db
}

// TestUnderwritingAPI_DeleteSponsor_Success covers the 200 delete path.
func TestUnderwritingAPI_DeleteSponsor_Success(t *testing.T) {
	u, db := newUnderwritingMore2Test(t)

	db.Create(&models.Sponsor{ID: "sp-del", Name: "ToDel", StationID: "s1"}) //nolint:errcheck

	req := httptest.NewRequest("DELETE", "/", nil)
	req = withChiParam(req, "id", "sp-del")
	rr := httptest.NewRecorder()
	u.handleDeleteSponsor(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("delete sponsor: got %d, want 200", rr.Code)
	}
}

// TestUnderwritingAPI_DeleteObligation_Success covers the 200 delete path.
func TestUnderwritingAPI_DeleteObligation_Success(t *testing.T) {
	u, db := newUnderwritingMore2Test(t)

	db.Create(&models.Sponsor{ID: "sp-obl-del", Name: "SponObl", StationID: "s1"}) //nolint:errcheck
	db.Create(&models.UnderwritingObligation{                                      //nolint:errcheck
		ID:           "obl-del",
		SponsorID:    "sp-obl-del",
		StationID:    "s1",
		SpotsPerWeek: 5,
	})

	req := httptest.NewRequest("DELETE", "/", nil)
	req = withChiParam(req, "id", "obl-del")
	rr := httptest.NewRecorder()
	u.handleDeleteObligation(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("delete obligation: got %d, want 200", rr.Code)
	}
}

// TestUnderwritingAPI_MarkAired_Success covers the mark-aired success path.
func TestUnderwritingAPI_MarkAired_Success(t *testing.T) {
	u, db := newUnderwritingMore2Test(t)

	db.Create(&models.Sponsor{ID: "sp-aired", Name: "SpAired", StationID: "s1"}) //nolint:errcheck
	db.Create(&models.UnderwritingObligation{                                    //nolint:errcheck
		ID: "obl-aired", SponsorID: "sp-aired", StationID: "s1", SpotsPerWeek: 1,
	})
	scheduledAt := time.Now()
	db.Create(&models.UnderwritingSpot{ //nolint:errcheck
		ID:           "spot-aired",
		ObligationID: "obl-aired",
		ScheduledAt:  scheduledAt,
		Status:       models.SpotStatusScheduled,
	})

	req := httptest.NewRequest("POST", "/", nil)
	req = withChiParam(req, "id", "spot-aired")
	rr := httptest.NewRecorder()
	u.handleMarkAired(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("mark aired: got %d, want 200, body=%s", rr.Code, rr.Body.String())
	}
}

// TestUnderwritingAPI_MarkMissed_Success covers the mark-missed success path.
func TestUnderwritingAPI_MarkMissed_Success(t *testing.T) {
	u, db := newUnderwritingMore2Test(t)

	db.Create(&models.Sponsor{ID: "sp-missed", Name: "SpMissed", StationID: "s1"}) //nolint:errcheck
	db.Create(&models.UnderwritingObligation{                                      //nolint:errcheck
		ID: "obl-missed", SponsorID: "sp-missed", StationID: "s1", SpotsPerWeek: 1,
	})
	scheduledAt := time.Now()
	db.Create(&models.UnderwritingSpot{ //nolint:errcheck
		ID:           "spot-missed",
		ObligationID: "obl-missed",
		ScheduledAt:  scheduledAt,
		Status:       models.SpotStatusScheduled,
	})

	req := httptest.NewRequest("POST", "/", nil)
	req = withChiParam(req, "id", "spot-missed")
	rr := httptest.NewRecorder()
	u.handleMarkMissed(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("mark missed: got %d, want 200, body=%s", rr.Code, rr.Body.String())
	}
}

// TestUnderwritingAPI_GetSponsor_Success covers the getSponsor success path.
func TestUnderwritingAPI_GetSponsor_Success(t *testing.T) {
	u, db := newUnderwritingMore2Test(t)

	db.Create(&models.Sponsor{ID: "sp-get", Name: "GetMe", StationID: "s1"}) //nolint:errcheck

	req := httptest.NewRequest("GET", "/", nil)
	req = withChiParam(req, "id", "sp-get")
	rr := httptest.NewRecorder()
	u.handleGetSponsor(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("get sponsor: got %d, want 200", rr.Code)
	}
}

// TestUnderwritingAPI_UpdateSponsor_Success covers the updateSponsor success path.
func TestUnderwritingAPI_UpdateSponsor_Success(t *testing.T) {
	u, db := newUnderwritingMore2Test(t)

	db.Create(&models.Sponsor{ID: "sp-upd", Name: "UpdateMe", StationID: "s1"}) //nolint:errcheck

	body := []byte(`{"name":"Updated Sponsor"}`)
	req := httptest.NewRequest("PUT", "/", bytes.NewReader(body))
	req = withChiParam(req, "id", "sp-upd")
	rr := httptest.NewRecorder()
	u.handleUpdateSponsor(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("update sponsor: got %d, want 200, body=%s", rr.Code, rr.Body.String())
	}
}

// TestUnderwritingAPI_UpdateObligation_Success covers the updateObligation success path.
func TestUnderwritingAPI_UpdateObligation_Success(t *testing.T) {
	u, db := newUnderwritingMore2Test(t)

	db.Create(&models.Sponsor{ID: "sp-upd-obl", Name: "Sp", StationID: "s1"}) //nolint:errcheck
	db.Create(&models.UnderwritingObligation{                                 //nolint:errcheck
		ID: "obl-upd", SponsorID: "sp-upd-obl", StationID: "s1", SpotsPerWeek: 3,
	})

	body := []byte(`{"spots_per_week":5}`)
	req := httptest.NewRequest("PUT", "/", bytes.NewReader(body))
	req = withChiParam(req, "id", "obl-upd")
	rr := httptest.NewRecorder()
	u.handleUpdateObligation(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("update obligation: got %d, want 200, body=%s", rr.Code, rr.Body.String())
	}
}

// TestUnderwritingAPI_ListSpots_Success covers the listSpots success path.
func TestUnderwritingAPI_ListSpots_Success(t *testing.T) {
	u, _ := newUnderwritingMore2Test(t)

	req := httptest.NewRequest("GET", "/?obligation_id=obl-x", nil)
	rr := httptest.NewRecorder()
	u.handleListSpots(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("list spots: got %d, want 200", rr.Code)
	}
}
