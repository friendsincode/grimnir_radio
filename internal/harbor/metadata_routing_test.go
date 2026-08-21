/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package harbor

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/rs/zerolog"

	"github.com/friendsincode/grimnir_radio/internal/dbtest"
	"github.com/friendsincode/grimnir_radio/internal/events"
	"github.com/friendsincode/grimnir_radio/internal/models"
)

// TestHandleMetadataUpdate_RoutesByToken guards #83: a metadata update must go
// to the connection whose token sent it, not just the first connection. The old
// code did `for _, c := range s.conns { conn = c; break }`, so with more than one
// source a DJ's title overwrote the wrong mount's now-playing.
func TestHandleMetadataUpdate_RoutesByToken(t *testing.T) {
	db := dbtest.Open(t, &models.PlayHistory{})
	s := &Server{db: db, bus: events.NewBus(), logger: zerolog.Nop(), conns: make(map[string]*SourceConnection)}

	mountA, stA := dbtest.UUID("mountA"), dbtest.UUID("stationA")
	mountB, stB := dbtest.UUID("mountB"), dbtest.UUID("stationB")
	s.conns["a"] = &SourceConnection{SessionID: "a", Token: "tokA", MountID: mountA, StationID: stA, MountName: "a"}
	s.conns["b"] = &SourceConnection{SessionID: "b", Token: "tokB", MountID: mountB, StationID: stB, MountName: "b"}

	target := "/admin/metadata?mode=updinfo&song=" + url.QueryEscape("Daft Punk - One More Time")
	r := httptest.NewRequest(http.MethodGet, target, nil)
	r.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("source:tokB")))
	w := httptest.NewRecorder()
	s.handleMetadataUpdate(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", w.Code, w.Body.String())
	}

	var h models.PlayHistory
	if err := db.Order("started_at DESC").First(&h).Error; err != nil {
		t.Fatalf("no play history written: %v", err)
	}
	if h.MountID != mountB || h.StationID != stB {
		t.Fatalf("metadata routed to the wrong connection: mount=%s station=%s, want B's mount/station", h.MountID, h.StationID)
	}
	if h.Artist != "Daft Punk" || h.Title != "One More Time" {
		t.Fatalf("song parse: artist=%q title=%q", h.Artist, h.Title)
	}
}
