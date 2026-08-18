/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/friendsincode/grimnir_radio/internal/db"
	"github.com/friendsincode/grimnir_radio/internal/dbtest"
	"github.com/friendsincode/grimnir_radio/internal/events"
	"github.com/friendsincode/grimnir_radio/internal/models"
)

// newSerializerPGTest builds an API over a real Postgres schema with a seeded
// station. These handlers write serializer:json columns, and that bug only
// reproduces on Postgres (sqlite is lenient), so the sqlite handler harness
// cannot guard it.
func newSerializerPGTest(t *testing.T) (*API, string) {
	t.Helper()
	database := dbtest.Open(t)
	if err := db.Migrate(database); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	stationID := dbtest.UUID("st")
	if err := database.Create(&models.Station{ID: stationID, OwnerID: dbtest.UUID("owner"), Name: "S"}).Error; err != nil {
		t.Fatalf("seed station: %v", err)
	}
	return &API{db: database, bus: events.NewBus(), logger: zerolog.Nop()}, stationID
}

// TestHandleShowsUpdate_PersistsMetadata guards Show.metadata (serializer:json):
// the update handler must persist a metadata map, not fail the raw-map write.
func TestHandleShowsUpdate_PersistsMetadata(t *testing.T) {
	a, stationID := newSerializerPGTest(t)
	showID := dbtest.UUID("show")
	if err := a.db.Create(&models.Show{ID: showID, StationID: stationID, Name: "Morning"}).Error; err != nil {
		t.Fatalf("seed show: %v", err)
	}

	body, _ := json.Marshal(map[string]any{"metadata": map[string]any{"color": "blue"}})
	req := httptest.NewRequest("PUT", "/"+showID, bytes.NewReader(body))
	req = withAdminClaims(withChiParam(req, "showID", showID))
	rr := httptest.NewRecorder()
	a.handleShowsUpdate(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("update show: got %d, body=%s", rr.Code, rr.Body.String())
	}

	var got models.Show
	a.db.First(&got, "id = ?", showID)
	if got.Metadata["color"] != "blue" {
		t.Fatalf("Show.Metadata = %v, want color=blue", got.Metadata)
	}
}

// TestHandleInstancesUpdate_PersistsMetadata guards ShowInstance.metadata.
func TestHandleInstancesUpdate_PersistsMetadata(t *testing.T) {
	a, stationID := newSerializerPGTest(t)
	showID := dbtest.UUID("show")
	if err := a.db.Create(&models.Show{ID: showID, StationID: stationID, Name: "Morning"}).Error; err != nil {
		t.Fatalf("seed show: %v", err)
	}
	instID := dbtest.UUID("inst")
	if err := a.db.Create(&models.ShowInstance{
		ID: instID, ShowID: showID, StationID: stationID,
		StartsAt: time.Now(), EndsAt: time.Now().Add(time.Hour),
		Status: models.ShowInstanceScheduled,
	}).Error; err != nil {
		t.Fatalf("seed instance: %v", err)
	}

	body, _ := json.Marshal(map[string]any{"metadata": map[string]any{"note": "swap"}})
	req := httptest.NewRequest("PUT", "/"+instID, bytes.NewReader(body))
	req = withAdminClaims(withChiParam(req, "instanceID", instID))
	rr := httptest.NewRecorder()
	a.handleInstancesUpdate(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("update instance: got %d, body=%s", rr.Code, rr.Body.String())
	}

	var got models.ShowInstance
	a.db.First(&got, "id = ?", instID)
	if got.Metadata["note"] != "swap" {
		t.Fatalf("ShowInstance.Metadata = %v, want note=swap", got.Metadata)
	}
}

// TestHandleScheduleUpdate_PersistsMetadata guards ScheduleEntry.metadata.
func TestHandleScheduleUpdate_PersistsMetadata(t *testing.T) {
	a, stationID := newSerializerPGTest(t)
	entryID := dbtest.UUID("entry")
	if err := a.db.Create(&models.ScheduleEntry{
		ID: entryID, StationID: stationID,
		MountID: dbtest.UUID("mnt"), SourceID: dbtest.UUID("src"),
		StartsAt: time.Now(), EndsAt: time.Now().Add(time.Hour),
	}).Error; err != nil {
		t.Fatalf("seed entry: %v", err)
	}

	body, _ := json.Marshal(map[string]any{"metadata": map[string]any{"k": "v"}})
	req := httptest.NewRequest("PUT", "/"+entryID, bytes.NewReader(body))
	req = withAdminClaims(withChiParam(req, "entryID", entryID))
	rr := httptest.NewRecorder()
	a.handleScheduleUpdate(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("update entry: got %d, body=%s", rr.Code, rr.Body.String())
	}

	var got models.ScheduleEntry
	a.db.First(&got, "id = ?", entryID)
	if got.Metadata["k"] != "v" {
		t.Fatalf("ScheduleEntry.Metadata = %v, want k=v", got.Metadata)
	}
}

// TestHandleScheduleRulesUpdate_PersistsConfig guards ScheduleRule.config.
func TestHandleScheduleRulesUpdate_PersistsConfig(t *testing.T) {
	a, stationID := newSerializerPGTest(t)
	ruleID := dbtest.UUID("rule")
	if err := a.db.Create(&models.ScheduleRule{
		ID: ruleID, StationID: stationID, Name: "R",
		RuleType: "no_overlap", Config: map[string]any{"a": float64(1)},
	}).Error; err != nil {
		t.Fatalf("seed rule: %v", err)
	}

	body, _ := json.Marshal(map[string]any{"config": map[string]any{"max": float64(5)}})
	req := httptest.NewRequest("PUT", "/"+ruleID, bytes.NewReader(body))
	req = withAdminClaims(withChiParam(req, "ruleID", ruleID))
	rr := httptest.NewRecorder()
	a.handleScheduleRulesUpdate(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("update rule: got %d, body=%s", rr.Code, rr.Body.String())
	}

	var got models.ScheduleRule
	a.db.First(&got, "id = ?", ruleID)
	if got.Config["max"] != float64(5) {
		t.Fatalf("ScheduleRule.Config = %v, want max=5", got.Config)
	}
}
