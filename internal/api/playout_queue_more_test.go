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

	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/auth"
	"github.com/friendsincode/grimnir_radio/internal/models"
)

func newPlayoutQueueMoreTest(t *testing.T) (*API, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared&_busy_timeout=5000"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(
		&models.StationUser{},
		&models.Mount{},
		&models.MediaItem{},
		&models.PlayoutQueueItem{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return &API{db: db, logger: zerolog.Nop()}, db
}

func withAdminQueueClaims(req *http.Request) *http.Request {
	return req.WithContext(auth.WithClaims(req.Context(), &auth.Claims{
		UserID: "u-admin",
		Roles:  []string{string(models.PlatformRoleAdmin)},
	}))
}

// TestPlayoutQueueReorder_ErrorPaths covers the missing branches in handlePlayoutQueueReorder.
func TestPlayoutQueueReorder_ErrorPaths(t *testing.T) {
	a, _ := newPlayoutQueueMoreTest(t)

	t.Run("empty queueID returns 400", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"position": 1})
		req := httptest.NewRequest("PATCH", "/", bytes.NewReader(body))
		req = withAdminQueueClaims(req)
		// No chi param set → queueID == ""
		rr := httptest.NewRecorder()
		a.handlePlayoutQueueReorder(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("empty queueID: got %d, want 400", rr.Code)
		}
	})

	t.Run("invalid JSON returns 400", func(t *testing.T) {
		req := httptest.NewRequest("PATCH", "/", bytes.NewReader([]byte("{")))
		req = withChiParam(req, "queueID", "q-any")
		req = withAdminQueueClaims(req)
		rr := httptest.NewRecorder()
		a.handlePlayoutQueueReorder(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("invalid json: got %d, want 400", rr.Code)
		}
	})

	t.Run("position zero returns 400", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"position": 0})
		req := httptest.NewRequest("PATCH", "/", bytes.NewReader(body))
		req = withChiParam(req, "queueID", "q-any")
		req = withAdminQueueClaims(req)
		rr := httptest.NewRecorder()
		a.handlePlayoutQueueReorder(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("position=0: got %d, want 400", rr.Code)
		}
	})

	t.Run("negative position returns 400", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"position": -5})
		req := httptest.NewRequest("PATCH", "/", bytes.NewReader(body))
		req = withChiParam(req, "queueID", "q-any")
		req = withAdminQueueClaims(req)
		rr := httptest.NewRecorder()
		a.handlePlayoutQueueReorder(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("negative position: got %d, want 400", rr.Code)
		}
	})

	t.Run("not found returns 404", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"position": 2})
		req := httptest.NewRequest("PATCH", "/", bytes.NewReader(body))
		req = withChiParam(req, "queueID", "nonexistent-id")
		req = withAdminQueueClaims(req)
		rr := httptest.NewRecorder()
		a.handlePlayoutQueueReorder(rr, req)
		if rr.Code != http.StatusNotFound {
			t.Fatalf("not found: got %d, want 404", rr.Code)
		}
	})
}

// TestPlayoutQueueDelete_ErrorPaths covers the missing branches in handlePlayoutQueueDelete.
func TestPlayoutQueueDelete_ErrorPaths(t *testing.T) {
	a, _ := newPlayoutQueueMoreTest(t)

	t.Run("empty queueID returns 400", func(t *testing.T) {
		req := httptest.NewRequest("DELETE", "/", nil)
		req = withAdminQueueClaims(req)
		rr := httptest.NewRecorder()
		a.handlePlayoutQueueDelete(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("empty queueID: got %d, want 400", rr.Code)
		}
	})

	t.Run("not found returns 404", func(t *testing.T) {
		req := httptest.NewRequest("DELETE", "/", nil)
		req = withChiParam(req, "queueID", "nonexistent-id")
		req = withAdminQueueClaims(req)
		rr := httptest.NewRecorder()
		a.handlePlayoutQueueDelete(rr, req)
		if rr.Code != http.StatusNotFound {
			t.Fatalf("not found: got %d, want 404", rr.Code)
		}
	})
}

// TestPlayoutQueueCreate_ErrorPaths covers missing branches in handlePlayoutQueueCreate.
func TestPlayoutQueueCreate_ErrorPaths(t *testing.T) {
	a, _ := newPlayoutQueueMoreTest(t)

	t.Run("invalid JSON returns 400", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/", bytes.NewReader([]byte("{")))
		req = withAdminQueueClaims(req)
		rr := httptest.NewRecorder()
		a.handlePlayoutQueueCreate(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("invalid json: got %d, want 400", rr.Code)
		}
	})

	t.Run("missing media_id returns 400", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"station_id": "s1"})
		req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
		req = withAdminQueueClaims(req)
		rr := httptest.NewRecorder()
		a.handlePlayoutQueueCreate(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("missing media_id: got %d, want 400", rr.Code)
		}
	})

	t.Run("missing station_id returns 400", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"media_id": "m1"})
		req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
		// No claims.StationID and no station_id in body
		req = req.WithContext(auth.WithClaims(req.Context(), &auth.Claims{
			UserID: "u-admin",
			Roles:  []string{string(models.PlatformRoleAdmin)},
		}))
		rr := httptest.NewRecorder()
		a.handlePlayoutQueueCreate(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("missing station_id: got %d, want 400", rr.Code)
		}
	})

	t.Run("media not found returns 404", func(t *testing.T) {
		// Create a mount for station s1
		a2, db2 := newPlayoutQueueMoreTest(t)
		if err := db2.Create(&models.Mount{
			ID: "m-test", StationID: "s1", Name: "M", Format: "mp3", URL: "/s",
		}).Error; err != nil {
			t.Fatalf("seed mount: %v", err)
		}
		body, _ := json.Marshal(map[string]any{
			"station_id": "s1",
			"media_id":   "nonexistent-media",
			"mount_id":   "m-test",
		})
		req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
		req = withAdminQueueClaims(req)
		rr := httptest.NewRecorder()
		a2.handlePlayoutQueueCreate(rr, req)
		if rr.Code != http.StatusNotFound {
			t.Fatalf("media not found: got %d, want 404", rr.Code)
		}
	})

	t.Run("media belongs to wrong station returns 400", func(t *testing.T) {
		a3, db3 := newPlayoutQueueMoreTest(t)
		if err := db3.Create(&models.Mount{
			ID: "m-x", StationID: "s1", Name: "X", Format: "mp3", URL: "/x",
		}).Error; err != nil {
			t.Fatalf("seed mount: %v", err)
		}
		if err := db3.Create(&models.MediaItem{
			ID:            "media-x",
			StationID:     "s2", // Different station!
			Title:         "Track",
			AnalysisState: "complete",
		}).Error; err != nil {
			t.Fatalf("seed media: %v", err)
		}
		body, _ := json.Marshal(map[string]any{
			"station_id": "s1",
			"media_id":   "media-x",
			"mount_id":   "m-x",
		})
		req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
		req = withAdminQueueClaims(req)
		rr := httptest.NewRecorder()
		a3.handlePlayoutQueueCreate(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("wrong station media: got %d, want 400, body=%s", rr.Code, rr.Body.String())
		}
	})
}

// TestPlayoutQueueCreate_WithPosition tests the explicit position override path.
func TestPlayoutQueueCreate_WithPosition(t *testing.T) {
	a, db := newPlayoutQueueMoreTest(t)

	if err := db.Create(&models.Mount{
		ID: "m-pos", StationID: "s-pos", Name: "Pos", Format: "mp3", URL: "/pos",
	}).Error; err != nil {
		t.Fatalf("seed mount: %v", err)
	}
	if err := db.Create(&models.MediaItem{
		ID:            "media-pos",
		StationID:     "s-pos",
		Title:         "Position Track",
		AnalysisState: "complete",
	}).Error; err != nil {
		t.Fatalf("seed media: %v", err)
	}

	pos := 1
	body, _ := json.Marshal(map[string]any{
		"station_id": "s-pos",
		"media_id":   "media-pos",
		"mount_id":   "m-pos",
		"position":   &pos,
	})
	req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
	req = withAdminQueueClaims(req)
	rr := httptest.NewRecorder()
	a.handlePlayoutQueueCreate(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create with position: got %d, want 201, body=%s", rr.Code, rr.Body.String())
	}
}
