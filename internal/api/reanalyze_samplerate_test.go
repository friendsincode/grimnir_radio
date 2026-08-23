/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rs/zerolog"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

// TestHandleReanalyzeMissingSampleRate_TargetsOnlyZeroComplete guards the
// backfill for the gst-discoverer sample-rate bug: only tracks that finished
// analysis with a bogus samplerate=0 should be re-queued. A track with a real
// sample rate, and one still pending, must be left alone.
func TestHandleReanalyzeMissingSampleRate_TargetsOnlyZeroComplete(t *testing.T) {
	a, db := newReanalyzTestAPI(t)

	seed := []*models.MediaItem{
		{ID: "affected", Path: "a.mp3", Duration: 100, AnalysisState: models.AnalysisComplete, Samplerate: 0},
		{ID: "has-rate", Path: "b.mp3", Duration: 100, AnalysisState: models.AnalysisComplete, Samplerate: 44100},
		{ID: "still-pending", Path: "c.mp3", Duration: 100, AnalysisState: models.AnalysisPending, Samplerate: 0},
	}
	for _, m := range seed {
		if err := db.Create(m).Error; err != nil {
			t.Fatalf("seed %s: %v", m.ID, err)
		}
	}

	req := httptest.NewRequest("POST", "/system/reanalyze-missing-samplerate", nil)
	rr := httptest.NewRecorder()
	a.handleReanalyzeMissingSampleRate(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("got %d, want 200; body=%s", rr.Code, rr.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got := resp["total_found"]; got != float64(1) {
		t.Errorf("total_found = %v, want 1 (only the complete samplerate=0 row)", got)
	}
	if got := resp["queued"]; got != float64(1) {
		t.Errorf("queued = %v, want 1", got)
	}

	// The affected row must be flipped to pending for re-analysis; the others
	// must keep their state.
	var affected, hasRate, pending models.MediaItem
	db.First(&affected, "id = ?", "affected")
	db.First(&hasRate, "id = ?", "has-rate")
	db.First(&pending, "id = ?", "still-pending")
	if affected.AnalysisState != models.AnalysisPending {
		t.Errorf("affected row state = %q, want pending", affected.AnalysisState)
	}
	if hasRate.AnalysisState != models.AnalysisComplete {
		t.Errorf("has-rate row must be untouched, state = %q", hasRate.AnalysisState)
	}
}

// TestHandleReanalyzeMissingSampleRate_NilAnalyzer returns 503 when the analyzer
// is not wired, matching the artwork handler's contract.
func TestHandleReanalyzeMissingSampleRate_NilAnalyzer(t *testing.T) {
	a := &API{logger: zerolog.Nop()}
	req := httptest.NewRequest("POST", "/system/reanalyze-missing-samplerate", nil)
	rr := httptest.NewRecorder()
	a.handleReanalyzeMissingSampleRate(rr, req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("nil analyzer: got %d, want 503; body=%s", rr.Code, rr.Body.String())
	}
}
