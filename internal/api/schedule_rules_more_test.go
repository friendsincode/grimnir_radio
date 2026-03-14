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

	"github.com/friendsincode/grimnir_radio/internal/models"
)

func TestScheduleRulesAPI_CreateInvalidJSON(t *testing.T) {
	a, _ := newScheduleRulesTest(t)

	req := httptest.NewRequest("POST", "/", bytes.NewReader([]byte("{")))
	rr := httptest.NewRecorder()
	a.handleScheduleRulesCreate(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("create invalid json: got %d, want 400", rr.Code)
	}
}

func TestScheduleRulesAPI_CreateInvalidRuleType(t *testing.T) {
	a, _ := newScheduleRulesTest(t)

	body, _ := json.Marshal(map[string]any{
		"station_id": "s1",
		"name":       "Bad Rule",
		"rule_type":  "invalid_type",
	})
	req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	a.handleScheduleRulesCreate(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("create invalid rule type: got %d, want 400", rr.Code)
	}
}

func TestScheduleRulesAPI_CreateWithConfig(t *testing.T) {
	a, _ := newScheduleRulesTest(t)

	body, _ := json.Marshal(map[string]any{
		"station_id": "s1",
		"name":       "Gap Rule",
		"rule_type":  string(models.RuleTypeGap),
		"config":     map[string]any{"min_gap_minutes": 5},
		"severity":   string(models.RuleSeverityError),
	})
	req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	a.handleScheduleRulesCreate(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create with config: got %d, want 201, body=%s", rr.Code, rr.Body.String())
	}
}

func TestScheduleRulesAPI_GetNotFound(t *testing.T) {
	a, _ := newScheduleRulesTest(t)

	req := httptest.NewRequest("GET", "/nonexistent", nil)
	req = withChiParam(req, "ruleID", "nonexistent-id")
	rr := httptest.NewRecorder()
	a.handleScheduleRulesGet(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("get not found: got %d, want 404", rr.Code)
	}
}

func TestScheduleRulesAPI_GetMissingID(t *testing.T) {
	a, _ := newScheduleRulesTest(t)

	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	a.handleScheduleRulesGet(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("get missing id: got %d, want 400", rr.Code)
	}
}

func TestScheduleRulesAPI_Update_ErrorPaths(t *testing.T) {
	a, db := newScheduleRulesTest(t)

	// Seed a rule
	rule := models.ScheduleRule{
		ID:        "rule-upd-1",
		StationID: "s1",
		Name:      "Test Rule",
		RuleType:  models.RuleTypeGap,
		Config:    map[string]any{},
		Severity:  models.RuleSeverityWarning,
		Active:    true,
	}
	db.Create(&rule) //nolint:errcheck

	t.Run("missing rule_id", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"name": "New Name"})
		req := httptest.NewRequest("PUT", "/", bytes.NewReader(body))
		rr := httptest.NewRecorder()
		a.handleScheduleRulesUpdate(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("got %d, want 400", rr.Code)
		}
	})

	t.Run("not found", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"name": "New Name"})
		req := httptest.NewRequest("PUT", "/", bytes.NewReader(body))
		req = withChiParam(req, "ruleID", "nonexistent")
		rr := httptest.NewRecorder()
		a.handleScheduleRulesUpdate(rr, req)
		if rr.Code != http.StatusNotFound {
			t.Fatalf("got %d, want 404", rr.Code)
		}
	})

	t.Run("invalid json", func(t *testing.T) {
		req := httptest.NewRequest("PUT", "/", bytes.NewReader([]byte("{")))
		req = withChiParam(req, "ruleID", rule.ID)
		rr := httptest.NewRecorder()
		a.handleScheduleRulesUpdate(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("got %d, want 400", rr.Code)
		}
	})

	t.Run("empty body — no-op returns current", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{})
		req := httptest.NewRequest("PUT", "/", bytes.NewReader(body))
		req = withChiParam(req, "ruleID", rule.ID)
		rr := httptest.NewRecorder()
		a.handleScheduleRulesUpdate(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("got %d, want 200", rr.Code)
		}
	})

	t.Run("update name + severity", func(t *testing.T) {
		name := "Updated Rule"
		sev := string(models.RuleSeverityError)
		active := false
		body, _ := json.Marshal(map[string]any{
			"name":     &name,
			"severity": &sev,
			"active":   &active,
		})
		req := httptest.NewRequest("PUT", "/", bytes.NewReader(body))
		req = withChiParam(req, "ruleID", rule.ID)
		rr := httptest.NewRecorder()
		a.handleScheduleRulesUpdate(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("got %d, want 200, body=%s", rr.Code, rr.Body.String())
		}
	})
}

func TestScheduleRulesAPI_DeleteNotFound(t *testing.T) {
	a, _ := newScheduleRulesTest(t)

	req := httptest.NewRequest("DELETE", "/", nil)
	req = withChiParam(req, "ruleID", "nonexistent-id")
	rr := httptest.NewRecorder()
	a.handleScheduleRulesDelete(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("delete not found: got %d, want 404", rr.Code)
	}
}

func TestScheduleRulesAPI_ListWithActiveFilter(t *testing.T) {
	a, db := newScheduleRulesTest(t)

	rule1 := models.ScheduleRule{
		ID: "rule-1", StationID: "s1", Name: "Rule 1",
		RuleType: models.RuleTypeGap, Config: map[string]any{},
		Severity: models.RuleSeverityWarning, Active: true,
	}
	rule2 := models.ScheduleRule{
		ID: "rule-2", StationID: "s1", Name: "Rule 2",
		RuleType: models.RuleTypeGap, Config: map[string]any{},
		Severity: models.RuleSeverityWarning, Active: true,
	}
	db.Create(&rule1) //nolint:errcheck
	db.Create(&rule2) //nolint:errcheck

	// Force rule2 inactive via raw SQL
	db.Exec("UPDATE schedule_rules SET active=0 WHERE id=?", rule2.ID) //nolint:errcheck

	req := httptest.NewRequest("GET", "/?station_id=s1&active=true", nil)
	rr := httptest.NewRecorder()
	a.handleScheduleRulesList(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("list active=true: got %d, want 200", rr.Code)
	}
	var resp map[string]any
	json.NewDecoder(rr.Body).Decode(&resp) //nolint:errcheck
	rules, _ := resp["rules"].([]any)
	if len(rules) != 1 {
		t.Fatalf("expected 1 active rule, got %d", len(rules))
	}
}

func TestScheduleRulesAPI_ValidateWithDateRange(t *testing.T) {
	a, _ := newScheduleRulesTest(t)

	// Default range (no start/end params)
	req := httptest.NewRequest("GET", "/?station_id=s1", nil)
	rr := httptest.NewRecorder()
	a.handleScheduleValidate(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("validate default: got %d, want 200, body=%s", rr.Code, rr.Body.String())
	}

	// Custom range
	start := time.Now().UTC().Format(time.RFC3339)
	end := time.Now().Add(3 * 24 * time.Hour).UTC().Format(time.RFC3339)
	req = httptest.NewRequest("GET", "/?station_id=s1&start="+start+"&end="+end, nil)
	rr = httptest.NewRecorder()
	a.handleScheduleValidate(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("validate custom range: got %d, want 200, body=%s", rr.Code, rr.Body.String())
	}
}

func TestScheduleRulesAPI_ValidateRangeCapped(t *testing.T) {
	a, _ := newScheduleRulesTest(t)

	// Range exceeding 90 days is capped
	start := "2026-01-01T00:00:00Z"
	end := "2026-12-31T00:00:00Z" // ~365 days - should be capped to 90
	req := httptest.NewRequest("GET", "/?station_id=s1&start="+start+"&end="+end, nil)
	rr := httptest.NewRecorder()
	a.handleScheduleValidate(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("validate capped range: got %d, want 200, body=%s", rr.Code, rr.Body.String())
	}
}
