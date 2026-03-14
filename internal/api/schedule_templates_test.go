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
	"path/filepath"
	"testing"

	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

func newScheduleTemplatesTest(t *testing.T) (*API, *gorm.DB) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "test.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(
		&models.ScheduleTemplate{},
		&models.ScheduleEntry{},
		&models.ShowInstance{},
		&models.Show{},
		&models.ScheduleVersion{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	return &API{db: db, logger: zerolog.Nop()}, db
}

func TestScheduleTemplatesAPI(t *testing.T) {
	a, _ := newScheduleTemplatesTest(t)

	// List templates (requires station_id)
	req := httptest.NewRequest("GET", "/?station_id=s1", nil)
	rr := httptest.NewRecorder()
	a.handleTemplatesList(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("list templates: got %d, want 200", rr.Code)
	}
	var listResp map[string]any
	json.NewDecoder(rr.Body).Decode(&listResp) //nolint:errcheck
	templates, _ := listResp["templates"].([]any)
	if len(templates) != 0 {
		t.Fatalf("expected 0 templates, got %d", len(templates))
	}

	// Create a template (captures empty schedule → 0 entries)
	body, _ := json.Marshal(map[string]any{
		"station_id":  "s1",
		"name":        "Default Week",
		"description": "A reusable week template",
		"start_date":  "2026-03-09",
		"end_date":    "2026-03-15",
	})
	req = httptest.NewRequest("POST", "/", bytes.NewReader(body))
	req = withAdminClaims(req)
	rr = httptest.NewRecorder()
	a.handleTemplatesCreate(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create template: got %d, want 201, body=%s", rr.Code, rr.Body.String())
	}
	var tmpl models.ScheduleTemplate
	json.NewDecoder(rr.Body).Decode(&tmpl) //nolint:errcheck
	if tmpl.ID == "" {
		t.Fatal("expected template id in response")
	}

	// Get the template
	req = httptest.NewRequest("GET", "/"+tmpl.ID, nil)
	req = withChiParam(req, "templateID", tmpl.ID)
	rr = httptest.NewRecorder()
	a.handleTemplatesGet(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("get template: got %d, want 200", rr.Code)
	}

	// Update template name
	newName := "Updated Week"
	body, _ = json.Marshal(map[string]any{"name": &newName})
	req = httptest.NewRequest("PUT", "/"+tmpl.ID, bytes.NewReader(body))
	req = withChiParam(req, "templateID", tmpl.ID)
	rr = httptest.NewRecorder()
	a.handleTemplatesUpdate(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("update template: got %d, want 200", rr.Code)
	}

	// Apply template (target_date required)
	body, _ = json.Marshal(map[string]any{
		"target_date":    "2026-04-07",
		"clear_existing": false,
	})
	req = httptest.NewRequest("POST", "/"+tmpl.ID+"/apply", bytes.NewReader(body))
	req = withAdminClaims(req)
	req = withChiParam(req, "templateID", tmpl.ID)
	rr = httptest.NewRecorder()
	a.handleTemplatesApply(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("apply template: got %d, want 200, body=%s", rr.Code, rr.Body.String())
	}
	var applyResp map[string]any
	json.NewDecoder(rr.Body).Decode(&applyResp) //nolint:errcheck
	if applyResp["status"] != "applied" {
		t.Fatalf("expected status=applied, got %v", applyResp["status"])
	}

	// Delete template
	req = httptest.NewRequest("DELETE", "/"+tmpl.ID, nil)
	req = withChiParam(req, "templateID", tmpl.ID)
	rr = httptest.NewRecorder()
	a.handleTemplatesDelete(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("delete template: got %d, want 200", rr.Code)
	}

	// Get after delete → 404
	req = httptest.NewRequest("GET", "/"+tmpl.ID, nil)
	req = withChiParam(req, "templateID", tmpl.ID)
	rr = httptest.NewRecorder()
	a.handleTemplatesGet(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("get deleted template: got %d, want 404", rr.Code)
	}
}

func TestScheduleTemplatesAPI_Errors(t *testing.T) {
	a, _ := newScheduleTemplatesTest(t)

	t.Run("list templates requires station_id", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		rr := httptest.NewRecorder()
		a.handleTemplatesList(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", rr.Code)
		}
	})

	t.Run("create template missing station_id", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"name": "No Station"})
		req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
		rr := httptest.NewRecorder()
		a.handleTemplatesCreate(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", rr.Code)
		}
	})

	t.Run("create template invalid start_date", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"station_id": "s1",
			"name":       "Bad Date",
			"start_date": "not-a-date",
		})
		req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
		rr := httptest.NewRecorder()
		a.handleTemplatesCreate(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", rr.Code)
		}
	})

	t.Run("get non-existent template", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/missing", nil)
		req = withChiParam(req, "templateID", "nonexistent")
		rr := httptest.NewRecorder()
		a.handleTemplatesGet(rr, req)
		if rr.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", rr.Code)
		}
	})

	t.Run("apply template missing target_date", func(t *testing.T) {
		// Seed a template first
		tmpl := models.ScheduleTemplate{
			ID:           "tmpl-err",
			StationID:    "s1",
			Name:         "Error Test",
			TemplateData: map[string]any{"entries": []any{}},
		}
		a.db.Create(&tmpl) //nolint:errcheck

		body, _ := json.Marshal(map[string]any{})
		req := httptest.NewRequest("POST", "/tmpl-err/apply", bytes.NewReader(body))
		req = withChiParam(req, "templateID", "tmpl-err")
		rr := httptest.NewRecorder()
		a.handleTemplatesApply(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", rr.Code)
		}
	})
}
