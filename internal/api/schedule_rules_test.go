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

func newScheduleRulesTest(t *testing.T) (*API, *gorm.DB) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "test.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(
		&models.ScheduleRule{},
		&models.ShowInstance{},
		&models.Show{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	return &API{db: db, logger: zerolog.Nop()}, db
}

func TestScheduleRulesAPI_CRUD(t *testing.T) {
	a, _ := newScheduleRulesTest(t)

	// List rules (requires station_id)
	req := httptest.NewRequest("GET", "/?station_id=s1", nil)
	rr := httptest.NewRecorder()
	a.handleScheduleRulesList(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("list rules: got %d, want 200", rr.Code)
	}
	var listResp map[string]any
	json.NewDecoder(rr.Body).Decode(&listResp) //nolint:errcheck
	rules, _ := listResp["rules"].([]any)
	if len(rules) != 0 {
		t.Fatalf("expected 0 rules, got %d", len(rules))
	}

	// Create rule (missing fields → 400)
	body, _ := json.Marshal(map[string]any{"station_id": "s1"}) // no name or type
	req = httptest.NewRequest("POST", "/", bytes.NewReader(body))
	rr = httptest.NewRecorder()
	a.handleScheduleRulesCreate(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("create rule missing name: got %d, want 400", rr.Code)
	}

	// Create rule (invalid type → 400)
	body, _ = json.Marshal(map[string]any{
		"station_id": "s1",
		"name":       "Bad Type",
		"rule_type":  "not_a_type",
	})
	req = httptest.NewRequest("POST", "/", bytes.NewReader(body))
	rr = httptest.NewRecorder()
	a.handleScheduleRulesCreate(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("create rule invalid type: got %d, want 400", rr.Code)
	}

	// Create rule (valid)
	body, _ = json.Marshal(map[string]any{
		"station_id": "s1",
		"name":       "No Gaps",
		"rule_type":  string(models.RuleTypeGap),
		"severity":   string(models.RuleSeverityWarning),
		"config":     map[string]any{"max_gap_minutes": 5},
	})
	req = httptest.NewRequest("POST", "/", bytes.NewReader(body))
	rr = httptest.NewRecorder()
	a.handleScheduleRulesCreate(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create rule: got %d, want 201, body=%s", rr.Code, rr.Body.String())
	}
	var created models.ScheduleRule
	json.NewDecoder(rr.Body).Decode(&created) //nolint:errcheck
	if created.ID == "" {
		t.Fatal("expected rule id in response")
	}

	// Get rule by ID
	req = httptest.NewRequest("GET", "/"+created.ID, nil)
	req = withChiParam(req, "ruleID", created.ID)
	rr = httptest.NewRecorder()
	a.handleScheduleRulesGet(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("get rule: got %d, want 200", rr.Code)
	}

	// Get non-existent rule
	req = httptest.NewRequest("GET", "/missing", nil)
	req = withChiParam(req, "ruleID", "nonexistent-id")
	rr = httptest.NewRecorder()
	a.handleScheduleRulesGet(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("get missing rule: got %d, want 404", rr.Code)
	}

	// Update rule
	newName := "Updated No Gaps"
	active := false
	body, _ = json.Marshal(map[string]any{"name": &newName, "active": &active})
	req = httptest.NewRequest("PUT", "/"+created.ID, bytes.NewReader(body))
	req = withChiParam(req, "ruleID", created.ID)
	rr = httptest.NewRecorder()
	a.handleScheduleRulesUpdate(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("update rule: got %d, want 200, body=%s", rr.Code, rr.Body.String())
	}

	// No-op update (empty body)
	body, _ = json.Marshal(map[string]any{})
	req = httptest.NewRequest("PUT", "/"+created.ID, bytes.NewReader(body))
	req = withChiParam(req, "ruleID", created.ID)
	rr = httptest.NewRecorder()
	a.handleScheduleRulesUpdate(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("no-op update: got %d, want 200", rr.Code)
	}

	// List with active filter (created rule is now inactive)
	req = httptest.NewRequest("GET", "/?station_id=s1&active=true", nil)
	rr = httptest.NewRecorder()
	a.handleScheduleRulesList(rr, req)
	json.NewDecoder(rr.Body).Decode(&listResp) //nolint:errcheck
	rules, _ = listResp["rules"].([]any)
	if len(rules) != 0 {
		t.Fatalf("expected 0 active rules after deactivation, got %d", len(rules))
	}

	// Delete rule
	req = httptest.NewRequest("DELETE", "/"+created.ID, nil)
	req = withChiParam(req, "ruleID", created.ID)
	rr = httptest.NewRecorder()
	a.handleScheduleRulesDelete(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("delete rule: got %d, want 200", rr.Code)
	}

	// Get after delete → 404
	req = httptest.NewRequest("GET", "/"+created.ID, nil)
	req = withChiParam(req, "ruleID", created.ID)
	rr = httptest.NewRecorder()
	a.handleScheduleRulesGet(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("get deleted rule: got %d, want 404", rr.Code)
	}
}

func TestScheduleRulesAPI_Validate(t *testing.T) {
	a, _ := newScheduleRulesTest(t)

	// Validate requires station_id
	req := httptest.NewRequest("GET", "/schedule/validate", nil)
	rr := httptest.NewRecorder()
	a.handleScheduleValidate(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("validate missing station_id: got %d, want 400", rr.Code)
	}

	// Validate with station_id (empty schedule → valid)
	req = httptest.NewRequest("GET", "/schedule/validate?station_id=s1", nil)
	rr = httptest.NewRecorder()
	a.handleScheduleValidate(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("validate: got %d, want 200, body=%s", rr.Code, rr.Body.String())
	}
	var result map[string]any
	json.NewDecoder(rr.Body).Decode(&result) //nolint:errcheck
	if valid, _ := result["valid"].(bool); !valid {
		t.Fatalf("expected valid=true for empty schedule, got %v", result["valid"])
	}
}

func TestScheduleRulesAPI_Errors(t *testing.T) {
	a, _ := newScheduleRulesTest(t)

	t.Run("list requires station_id", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		rr := httptest.NewRecorder()
		a.handleScheduleRulesList(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", rr.Code)
		}
	})

	t.Run("delete non-existent rule", func(t *testing.T) {
		req := httptest.NewRequest("DELETE", "/missing", nil)
		req = withChiParam(req, "ruleID", "nonexistent")
		rr := httptest.NewRecorder()
		a.handleScheduleRulesDelete(rr, req)
		if rr.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", rr.Code)
		}
	})

	t.Run("update non-existent rule", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"name": "nope"})
		req := httptest.NewRequest("PUT", "/missing", bytes.NewReader(body))
		req = withChiParam(req, "ruleID", "nonexistent")
		rr := httptest.NewRecorder()
		a.handleScheduleRulesUpdate(rr, req)
		if rr.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", rr.Code)
		}
	})
}
