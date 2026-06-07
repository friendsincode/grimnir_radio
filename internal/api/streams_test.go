/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package api

import (
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

func newStreamsAPITest(t *testing.T) (*API, *gorm.DB) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "test.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(
		&models.Station{},
		&models.StationStream{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	return &API{db: db, logger: zerolog.Nop()}, db
}

// streamResponseItem matches the public stream response shape (Chunk 1 contract).
type streamResponseItem struct {
	URL         string `json:"url"`
	Format      string `json:"format"`
	BitrateKbps int    `json:"bitrate_kbps"`
	Label       string `json:"label"`
}

type streamsResponse struct {
	Streams []streamResponseItem `json:"streams"`
}

func TestStreams_Get_StationNotFound(t *testing.T) {
	a, _ := newStreamsAPITest(t)

	req := httptest.NewRequest("GET", "/api/v1/stations/missing/streams", nil)
	req = withChiParam(req, "stationID", "missing")
	rr := httptest.NewRecorder()

	a.handleStreamsGet(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("missing station: got %d, want 404 (body=%s)", rr.Code, rr.Body.String())
	}
}

func TestStreams_Get_EmptyStreams(t *testing.T) {
	a, db := newStreamsAPITest(t)

	station := models.Station{ID: "s1", Name: "Test Station", Active: true, Public: true, Approved: true}
	if err := db.Create(&station).Error; err != nil {
		t.Fatalf("create station: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/v1/stations/s1/streams", nil)
	req = withChiParam(req, "stationID", "s1")
	rr := httptest.NewRecorder()

	a.handleStreamsGet(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("empty streams: got %d, want 200 (body=%s)", rr.Code, rr.Body.String())
	}

	var resp streamsResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Streams == nil {
		t.Fatal("streams field must be non-nil array (even when empty)")
	}
	if len(resp.Streams) != 0 {
		t.Fatalf("expected 0 streams, got %d", len(resp.Streams))
	}
}

func TestStreams_Get_OrderedByPriority(t *testing.T) {
	a, db := newStreamsAPITest(t)

	station := models.Station{ID: "s1", Name: "Test Station", Active: true, Public: true, Approved: true}
	if err := db.Create(&station).Error; err != nil {
		t.Fatalf("create station: %v", err)
	}

	// Insert LQ first (priority 2) then HQ (priority 1); endpoint must order HQ-first.
	lq := models.StationStream{
		ID:          "stream-lq",
		StationID:   "s1",
		URL:         "https://<public-hostname>/main/lq",
		Format:      "mp3",
		BitrateKbps: 64,
		Label:       "LQ",
		Priority:    2,
	}
	hq := models.StationStream{
		ID:          "stream-hq",
		StationID:   "s1",
		URL:         "https://<public-hostname>/main/hq",
		Format:      "mp3",
		BitrateKbps: 128,
		Label:       "HQ",
		Priority:    1,
	}
	if err := db.Create(&lq).Error; err != nil {
		t.Fatalf("create lq: %v", err)
	}
	if err := db.Create(&hq).Error; err != nil {
		t.Fatalf("create hq: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/v1/stations/s1/streams", nil)
	req = withChiParam(req, "stationID", "s1")
	rr := httptest.NewRecorder()

	a.handleStreamsGet(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("get streams: got %d, want 200 (body=%s)", rr.Code, rr.Body.String())
	}

	var resp streamsResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Streams) != 2 {
		t.Fatalf("expected 2 streams, got %d", len(resp.Streams))
	}
	if resp.Streams[0].Label != "HQ" {
		t.Fatalf("HQ must sort first; got %q", resp.Streams[0].Label)
	}
	if resp.Streams[0].URL != "https://<public-hostname>/main/hq" {
		t.Fatalf("HQ url: got %q", resp.Streams[0].URL)
	}
	if resp.Streams[0].BitrateKbps != 128 {
		t.Fatalf("HQ bitrate_kbps: got %d, want 128", resp.Streams[0].BitrateKbps)
	}
	if resp.Streams[0].Format != "mp3" {
		t.Fatalf("HQ format: got %q, want mp3", resp.Streams[0].Format)
	}
	if resp.Streams[1].Label != "LQ" {
		t.Fatalf("LQ must sort second; got %q", resp.Streams[1].Label)
	}
}

func TestStreams_Get_MissingStationID(t *testing.T) {
	a, _ := newStreamsAPITest(t)

	req := httptest.NewRequest("GET", "/api/v1/stations//streams", nil)
	rr := httptest.NewRecorder()

	a.handleStreamsGet(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("missing station id: got %d, want 400 (body=%s)", rr.Code, rr.Body.String())
	}
}
