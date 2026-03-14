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

	"github.com/friendsincode/grimnir_radio/internal/auth"
	"github.com/friendsincode/grimnir_radio/internal/models"
)

func TestScheduleTemplates_CreateNoDate(t *testing.T) {
	a, _ := newScheduleTemplatesTest(t)

	// No start/end date — uses default (current week)
	body, _ := json.Marshal(map[string]any{
		"station_id":  "s1",
		"name":        "My Template",
		"description": "A test template",
	})
	req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
	req = req.WithContext(auth.WithClaims(req.Context(), &auth.Claims{UserID: "u1"}))
	rr := httptest.NewRecorder()
	a.handleTemplatesCreate(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create no dates: got %d, want 201, body=%s", rr.Code, rr.Body.String())
	}
}

func TestScheduleTemplates_CreateWithExplicitDates(t *testing.T) {
	a, _ := newScheduleTemplatesTest(t)

	body, _ := json.Marshal(map[string]any{
		"station_id": "s1",
		"name":       "Dated Template",
		"start_date": "2026-03-09",
		"end_date":   "2026-03-15",
	})
	req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	a.handleTemplatesCreate(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create with dates: got %d, want 201, body=%s", rr.Code, rr.Body.String())
	}
}

func TestScheduleTemplates_CreateInvalidStartDate(t *testing.T) {
	a, _ := newScheduleTemplatesTest(t)

	body, _ := json.Marshal(map[string]any{
		"station_id": "s1",
		"name":       "Bad Start",
		"start_date": "not-a-date",
	})
	req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	a.handleTemplatesCreate(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("create invalid start_date: got %d, want 400", rr.Code)
	}
}

func TestScheduleTemplates_CreateInvalidEndDate(t *testing.T) {
	a, _ := newScheduleTemplatesTest(t)

	body, _ := json.Marshal(map[string]any{
		"station_id": "s1",
		"name":       "Bad End",
		"start_date": "2026-03-09",
		"end_date":   "not-a-date",
	})
	req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	a.handleTemplatesCreate(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("create invalid end_date: got %d, want 400", rr.Code)
	}
}

func TestScheduleTemplates_CreateMissingFields(t *testing.T) {
	a, _ := newScheduleTemplatesTest(t)

	t.Run("missing station_id", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"name": "X"})
		req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
		rr := httptest.NewRecorder()
		a.handleTemplatesCreate(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("got %d, want 400", rr.Code)
		}
	})

	t.Run("missing name", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"station_id": "s1"})
		req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
		rr := httptest.NewRecorder()
		a.handleTemplatesCreate(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("got %d, want 400", rr.Code)
		}
	})

	t.Run("invalid json", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/", bytes.NewReader([]byte("{")))
		rr := httptest.NewRecorder()
		a.handleTemplatesCreate(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("got %d, want 400", rr.Code)
		}
	})
}

func TestScheduleTemplates_CreateWithShowInstances(t *testing.T) {
	a, db := newScheduleTemplatesTest(t)

	// Seed a show and instance within the date range
	show := models.Show{
		ID:                     "show-tpl",
		StationID:              "s1",
		Name:                   "Template Show",
		DTStart:                time.Date(2026, 3, 9, 9, 0, 0, 0, time.UTC),
		DefaultDurationMinutes: 60,
		Active:                 true,
	}
	db.Create(&show) //nolint:errcheck

	instance := models.ShowInstance{
		ID:        "inst-tpl",
		ShowID:    show.ID,
		StationID: "s1",
		StartsAt:  time.Date(2026, 3, 9, 9, 0, 0, 0, time.UTC),
		EndsAt:    time.Date(2026, 3, 9, 10, 0, 0, 0, time.UTC),
		Status:    models.ShowInstanceScheduled,
	}
	db.Create(&instance) //nolint:errcheck

	body, _ := json.Marshal(map[string]any{
		"station_id": "s1",
		"name":       "With Instances",
		"start_date": "2026-03-09",
		"end_date":   "2026-03-15",
	})
	req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	a.handleTemplatesCreate(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create with instances: got %d, want 201, body=%s", rr.Code, rr.Body.String())
	}
}

func TestScheduleTemplates_UpdateErrorPaths(t *testing.T) {
	a, db := newScheduleTemplatesTest(t)

	// Seed a template
	body, _ := json.Marshal(map[string]any{
		"station_id": "s1",
		"name":       "Seed Template",
	})
	req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	a.handleTemplatesCreate(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("seed: got %d, want 201", rr.Code)
	}
	_ = db

	var created models.ScheduleTemplate
	json.NewDecoder(rr.Body).Decode(&created) //nolint:errcheck

	t.Run("not found", func(t *testing.T) {
		b, _ := json.Marshal(map[string]any{"name": "X"})
		req := httptest.NewRequest("PUT", "/", bytes.NewReader(b))
		req = withChiParam(req, "templateID", "nonexistent")
		rr := httptest.NewRecorder()
		a.handleTemplatesUpdate(rr, req)
		if rr.Code != http.StatusNotFound {
			t.Fatalf("got %d, want 404", rr.Code)
		}
	})

	t.Run("missing id", func(t *testing.T) {
		b, _ := json.Marshal(map[string]any{"name": "X"})
		req := httptest.NewRequest("PUT", "/", bytes.NewReader(b))
		rr := httptest.NewRecorder()
		a.handleTemplatesUpdate(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("got %d, want 400", rr.Code)
		}
	})

	t.Run("invalid json", func(t *testing.T) {
		req := httptest.NewRequest("PUT", "/", bytes.NewReader([]byte("{")))
		req = withChiParam(req, "templateID", created.ID)
		rr := httptest.NewRecorder()
		a.handleTemplatesUpdate(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("got %d, want 400", rr.Code)
		}
	})

	t.Run("empty body — no-op", func(t *testing.T) {
		b, _ := json.Marshal(map[string]any{})
		req := httptest.NewRequest("PUT", "/", bytes.NewReader(b))
		req = withChiParam(req, "templateID", created.ID)
		rr := httptest.NewRecorder()
		a.handleTemplatesUpdate(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("got %d, want 200", rr.Code)
		}
	})

	t.Run("update name and description", func(t *testing.T) {
		name := "Updated Name"
		desc := "Updated Description"
		b, _ := json.Marshal(map[string]any{"name": &name, "description": &desc})
		req := httptest.NewRequest("PUT", "/", bytes.NewReader(b))
		req = withChiParam(req, "templateID", created.ID)
		rr := httptest.NewRecorder()
		a.handleTemplatesUpdate(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("got %d, want 200, body=%s", rr.Code, rr.Body.String())
		}
	})
}

func TestScheduleTemplates_DeleteErrorPaths(t *testing.T) {
	a, _ := newScheduleTemplatesTest(t)

	t.Run("not found", func(t *testing.T) {
		req := httptest.NewRequest("DELETE", "/", nil)
		req = withChiParam(req, "templateID", "nonexistent")
		rr := httptest.NewRecorder()
		a.handleTemplatesDelete(rr, req)
		if rr.Code != http.StatusNotFound {
			t.Fatalf("got %d, want 404", rr.Code)
		}
	})

	t.Run("missing id", func(t *testing.T) {
		req := httptest.NewRequest("DELETE", "/", nil)
		rr := httptest.NewRecorder()
		a.handleTemplatesDelete(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("got %d, want 400", rr.Code)
		}
	})
}

func TestScheduleTemplates_ApplyErrorPaths(t *testing.T) {
	a, _ := newScheduleTemplatesTest(t)

	t.Run("missing template id", func(t *testing.T) {
		b, _ := json.Marshal(map[string]any{"target_date": "2026-03-09"})
		req := httptest.NewRequest("POST", "/", bytes.NewReader(b))
		rr := httptest.NewRecorder()
		a.handleTemplatesApply(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("got %d, want 400", rr.Code)
		}
	})

	t.Run("invalid json", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/", bytes.NewReader([]byte("{")))
		req = withChiParam(req, "templateID", "some-id")
		rr := httptest.NewRecorder()
		a.handleTemplatesApply(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("got %d, want 400", rr.Code)
		}
	})

	t.Run("missing target_date", func(t *testing.T) {
		b, _ := json.Marshal(map[string]any{})
		req := httptest.NewRequest("POST", "/", bytes.NewReader(b))
		req = withChiParam(req, "templateID", "some-id")
		rr := httptest.NewRecorder()
		a.handleTemplatesApply(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("got %d, want 400", rr.Code)
		}
	})

	t.Run("invalid target_date", func(t *testing.T) {
		b, _ := json.Marshal(map[string]any{"target_date": "not-a-date"})
		req := httptest.NewRequest("POST", "/", bytes.NewReader(b))
		req = withChiParam(req, "templateID", "some-id")
		rr := httptest.NewRecorder()
		a.handleTemplatesApply(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("got %d, want 400", rr.Code)
		}
	})

	t.Run("template not found", func(t *testing.T) {
		b, _ := json.Marshal(map[string]any{"target_date": "2026-03-09"})
		req := httptest.NewRequest("POST", "/", bytes.NewReader(b))
		req = withChiParam(req, "templateID", "nonexistent")
		rr := httptest.NewRecorder()
		a.handleTemplatesApply(rr, req)
		if rr.Code != http.StatusNotFound {
			t.Fatalf("got %d, want 404", rr.Code)
		}
	})
}

func TestScheduleTemplates_ApplyWithEntries(t *testing.T) {
	a, db := newScheduleTemplatesTest(t)

	// Seed a show for the show-type entry
	show := models.Show{
		ID:                     "show-apply",
		StationID:              "s1",
		Name:                   "Apply Show",
		DTStart:                time.Date(2026, 3, 9, 10, 0, 0, 0, time.UTC),
		DefaultDurationMinutes: 60,
		Active:                 true,
	}
	db.Create(&show) //nolint:errcheck

	// Create template with entries in template_data
	tpl := models.ScheduleTemplate{
		ID:        "tpl-apply",
		StationID: "s1",
		Name:      "Apply Template",
		TemplateData: map[string]any{
			"entries": []map[string]any{
				{
					"day_of_week":      1,
					"start_time":       "10:00",
					"duration_minutes": 60,
					"source_type":      "show",
					"show_id":          show.ID,
				},
				{
					"day_of_week":      2,
					"start_time":       "14:00",
					"duration_minutes": 30,
					"source_type":      "smart_block",
					"source_id":        "sb-1",
				},
			},
		},
	}
	db.Create(&tpl) //nolint:errcheck

	b, _ := json.Marshal(map[string]any{
		"target_date":    "2026-03-09",
		"clear_existing": true,
	})
	req := httptest.NewRequest("POST", "/", bytes.NewReader(b))
	req = withChiParam(req, "templateID", tpl.ID)
	rr := httptest.NewRecorder()
	a.handleTemplatesApply(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("apply with entries: got %d, want 200, body=%s", rr.Code, rr.Body.String())
	}
	var resp map[string]any
	json.NewDecoder(rr.Body).Decode(&resp) //nolint:errcheck
	if resp["status"] != "applied" {
		t.Fatalf("expected status=applied, got %v", resp["status"])
	}
}

func TestScheduleTemplates_ListMissingStation(t *testing.T) {
	a, _ := newScheduleTemplatesTest(t)

	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	a.handleTemplatesList(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("list without station_id: got %d, want 400", rr.Code)
	}
}

func TestScheduleTemplates_GetMissingID(t *testing.T) {
	a, _ := newScheduleTemplatesTest(t)

	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	a.handleTemplatesGet(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("get missing id: got %d, want 400", rr.Code)
	}
}

func TestScheduleTemplates_GetNotFound(t *testing.T) {
	a, _ := newScheduleTemplatesTest(t)

	req := httptest.NewRequest("GET", "/nonexistent", nil)
	req = withChiParam(req, "templateID", "nonexistent")
	rr := httptest.NewRecorder()
	a.handleTemplatesGet(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("get not found: got %d, want 404", rr.Code)
	}
}
