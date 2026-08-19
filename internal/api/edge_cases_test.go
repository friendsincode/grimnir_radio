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

	"github.com/friendsincode/grimnir_radio/internal/dbtest"
	"github.com/friendsincode/grimnir_radio/internal/models"
)

func postJSON(path, body string) *http.Request {
	return httptest.NewRequest("POST", path, bytes.NewReader([]byte(body)))
}

// TestHandleStationsCreate_Validation covers the create handler's guard rails:
// malformed body and missing name are rejected, and a duplicate name returns a
// clean 409 (the unique-violation path added in #328) rather than a 500.
func TestHandleStationsCreate_Validation(t *testing.T) {
	a, _ := newSerializerPGTest(t)

	cases := []struct {
		name string
		body string
		want int
	}{
		{"invalid json", "{", http.StatusBadRequest},
		{"missing name", `{"name":""}`, http.StatusBadRequest},
		{"ok", `{"name":"Fresh Station"}`, http.StatusCreated},
	}
	for _, c := range cases {
		rr := httptest.NewRecorder()
		a.handleStationsCreate(rr, postJSON("/", c.body))
		if rr.Code != c.want {
			t.Errorf("%s: got %d, want %d (body=%s)", c.name, rr.Code, c.want, rr.Body.String())
		}
	}

	// Duplicate name -> 409 conflict.
	if err := a.db.Create(&models.Station{ID: dbtest.UUID("dup"), OwnerID: dbtest.UUID("o"), Name: "Dup"}).Error; err != nil {
		t.Fatalf("seed dup station: %v", err)
	}
	rr := httptest.NewRecorder()
	a.handleStationsCreate(rr, postJSON("/", `{"name":"Dup"}`))
	if rr.Code != http.StatusConflict {
		t.Errorf("duplicate name: got %d, want 409 (body=%s)", rr.Code, rr.Body.String())
	}
}

// TestHandleScheduleRulesCreate_Validation covers the required-field and
// rule-type validation on the rule create handler.
func TestHandleScheduleRulesCreate_Validation(t *testing.T) {
	a, stationID := newSerializerPGTest(t)

	cases := []struct {
		name string
		body string
		want int
	}{
		{"missing fields", `{}`, http.StatusBadRequest},
		{"invalid rule type", `{"station_id":"` + stationID + `","name":"R","rule_type":"nonsense"}`, http.StatusBadRequest},
		{"valid", `{"station_id":"` + stationID + `","name":"R","rule_type":"gap"}`, http.StatusCreated},
	}
	for _, c := range cases {
		rr := httptest.NewRecorder()
		a.handleScheduleRulesCreate(rr, postJSON("/", c.body))
		if rr.Code != c.want {
			t.Errorf("%s: got %d, want %d (body=%s)", c.name, rr.Code, c.want, rr.Body.String())
		}
	}
}

// TestHandlers_NotFound covers the not-found paths for the get/update handlers,
// which must 404 on an unknown id rather than error.
func TestHandlers_NotFound(t *testing.T) {
	a, _ := newSerializerPGTest(t)
	missing := dbtest.UUID("missing")

	rr := httptest.NewRecorder()
	a.handleShowsGet(rr, withAdminClaims(withChiParam(httptest.NewRequest("GET", "/", nil), "showID", missing)))
	if rr.Code != http.StatusNotFound {
		t.Errorf("handleShowsGet unknown: got %d, want 404", rr.Code)
	}

	rr = httptest.NewRecorder()
	a.handleScheduleRulesGet(rr, withAdminClaims(withChiParam(httptest.NewRequest("GET", "/", nil), "ruleID", missing)))
	if rr.Code != http.StatusNotFound {
		t.Errorf("handleScheduleRulesGet unknown: got %d, want 404", rr.Code)
	}

	rr = httptest.NewRecorder()
	a.handleShowsUpdate(rr, withAdminClaims(withChiParam(postJSON("/", `{"name":"x"}`), "showID", missing)))
	if rr.Code != http.StatusNotFound {
		t.Errorf("handleShowsUpdate unknown: got %d, want 404", rr.Code)
	}
}
