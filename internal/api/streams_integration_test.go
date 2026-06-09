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

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"

	"github.com/friendsincode/grimnir_radio/internal/audit"
	"github.com/friendsincode/grimnir_radio/internal/events"
	"github.com/friendsincode/grimnir_radio/internal/executor"
	"github.com/friendsincode/grimnir_radio/internal/integrity"
	"github.com/friendsincode/grimnir_radio/internal/models"
	"github.com/friendsincode/grimnir_radio/internal/priority"
)

// TestStreams_Integration_FullRouter verifies the streams endpoint is reachable
// through the full chi router (no auth middleware) & returns the canonical
// JSON contract documented in docs/superpowers/plans/2026-06-06-custom-js-player.md.
//
// Uses the same Routes()/AutoMigrate pattern as coverage_routes_test.go so a
// regression that drops the route registration or breaks the response shape
// will fail this test.
func TestStreams_Integration_FullRouter(t *testing.T) {
	db := newRoutesTestDB(t)
	// StationStream isn't in newRoutesTestDB's default set; add it here.
	if err := db.AutoMigrate(&models.StationStream{}); err != nil {
		t.Fatalf("migrate StationStream: %v", err)
	}

	bus := events.NewBus()
	prioritySvc := priority.NewService(db, bus, zerolog.Nop())
	auditSvc := audit.NewService(db, bus, zerolog.Nop())
	integritySvc := integrity.NewService(db, zerolog.Nop())
	stateMgr := executor.NewStateManager(db, zerolog.Nop())

	a := New(
		db,
		[]byte("test-secret"),
		nil, nil, nil, nil, nil, nil,
		prioritySvc,
		stateMgr,
		auditSvc,
		integritySvc,
		nil, bus, nil, 0,
		zerolog.Nop(),
	)

	// Seed: one station, two streams (HQ + LQ) with deliberately-inverted
	// insertion order so the ORDER BY priority assertion has teeth.
	station := models.Station{
		ID:       "st-int-1",
		Name:     "Integration Station",
		Active:   true,
		Public:   true,
		Approved: true,
	}
	if err := db.Create(&station).Error; err != nil {
		t.Fatalf("create station: %v", err)
	}

	streams := []models.StationStream{
		{
			ID:          "stream-int-lq",
			StationID:   "st-int-1",
			URL:         "https://example.com/main/lq",
			Format:      "mp3",
			BitrateKbps: 64,
			Label:       "LQ",
			Priority:    2,
		},
		{
			ID:          "stream-int-hq",
			StationID:   "st-int-1",
			URL:         "https://example.com/main/hq",
			Format:      "mp3",
			BitrateKbps: 128,
			Label:       "HQ",
			Priority:    1,
		},
	}
	for _, s := range streams {
		if err := db.Create(&s).Error; err != nil {
			t.Fatalf("create stream %s: %v", s.ID, err)
		}
	}

	r := chi.NewRouter()
	a.Routes(r)

	// Verify the endpoint is reachable through the full router (no auth header).
	req := httptest.NewRequest("GET", "/api/v1/stations/st-int-1/streams", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code == http.StatusNotFound {
		t.Fatal("streams endpoint not registered; got 404 from full router")
	}
	if rr.Code != http.StatusOK {
		t.Fatalf("streams endpoint: got %d, want 200; body=%s", rr.Code, rr.Body.String())
	}

	// Verify the exact JSON contract from the plan.
	var resp struct {
		Streams []struct {
			URL         string `json:"url"`
			Format      string `json:"format"`
			BitrateKbps int    `json:"bitrate_kbps"`
			Label       string `json:"label"`
		} `json:"streams"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, rr.Body.String())
	}

	if len(resp.Streams) != 2 {
		t.Fatalf("expected 2 streams in response, got %d (body=%s)", len(resp.Streams), rr.Body.String())
	}

	// HQ first (priority 1)
	if got := resp.Streams[0]; got.URL != "https://example.com/main/hq" ||
		got.Format != "mp3" ||
		got.BitrateKbps != 128 ||
		got.Label != "HQ" {
		t.Fatalf("HQ row mismatch: %+v", got)
	}
	// LQ second (priority 2)
	if got := resp.Streams[1]; got.URL != "https://example.com/main/lq" ||
		got.Format != "mp3" ||
		got.BitrateKbps != 64 ||
		got.Label != "LQ" {
		t.Fatalf("LQ row mismatch: %+v", got)
	}
}

// TestStreams_Integration_NotFound verifies the 404 path through the full
// router for an unknown station id.
func TestStreams_Integration_NotFound(t *testing.T) {
	db := newRoutesTestDB(t)
	if err := db.AutoMigrate(&models.StationStream{}); err != nil {
		t.Fatalf("migrate StationStream: %v", err)
	}

	bus := events.NewBus()
	prioritySvc := priority.NewService(db, bus, zerolog.Nop())
	auditSvc := audit.NewService(db, bus, zerolog.Nop())
	integritySvc := integrity.NewService(db, zerolog.Nop())
	stateMgr := executor.NewStateManager(db, zerolog.Nop())

	a := New(
		db, []byte("test-secret"),
		nil, nil, nil, nil, nil, nil,
		prioritySvc, stateMgr, auditSvc, integritySvc,
		nil, bus, nil, 0, zerolog.Nop(),
	)

	r := chi.NewRouter()
	a.Routes(r)

	req := httptest.NewRequest("GET", "/api/v1/stations/does-not-exist/streams", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("unknown station: got %d, want 404; body=%s", rr.Code, rr.Body.String())
	}
}
