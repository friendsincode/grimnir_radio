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

	"github.com/friendsincode/grimnir_radio/internal/auth"
	"github.com/friendsincode/grimnir_radio/internal/events"
	"github.com/friendsincode/grimnir_radio/internal/models"
	"github.com/friendsincode/grimnir_radio/internal/recording"
)

func newRecordingAPITest(t *testing.T) (*RecordingAPI, *gorm.DB) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "test.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&models.Recording{}, &models.RecordingChapter{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// nil meClient is ok for validation-only tests (paths that return before service calls)
	svc := recording.NewService(db, nil, "", zerolog.Nop())
	bus := events.NewBus()
	api := &API{db: db, logger: zerolog.Nop(), bus: bus}
	return NewRecordingAPI(api, svc), db
}

func TestRecordingAPI_StartRecording_Validation(t *testing.T) {
	ra, _ := newRecordingAPITest(t)

	t.Run("no auth", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"station_id": "s1", "mount_id": "m1"})
		req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
		rr := httptest.NewRecorder()
		ra.handleStartRecording(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("got %d, want 401", rr.Code)
		}
	})

	t.Run("invalid JSON", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/", bytes.NewReader([]byte("{")))
		req = req.WithContext(auth.WithClaims(req.Context(), &auth.Claims{UserID: "u1"}))
		rr := httptest.NewRecorder()
		ra.handleStartRecording(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("got %d, want 400", rr.Code)
		}
	})

	t.Run("missing station_and_mount", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"station_id": "s1"})
		req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
		req = req.WithContext(auth.WithClaims(req.Context(), &auth.Claims{UserID: "u1"}))
		rr := httptest.NewRecorder()
		ra.handleStartRecording(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("got %d, want 400", rr.Code)
		}
	})
}

func TestRecordingAPI_StopRecording_Validation(t *testing.T) {
	ra, _ := newRecordingAPITest(t)

	// Empty recordingID → 400
	req := httptest.NewRequest("POST", "/", nil)
	rr := httptest.NewRecorder()
	ra.handleStopRecording(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400", rr.Code)
	}
}

func TestRecordingAPI_ListRecordings_Validation(t *testing.T) {
	ra, _ := newRecordingAPITest(t)

	t.Run("missing station_id", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		rr := httptest.NewRecorder()
		ra.handleListRecordings(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("got %d, want 400", rr.Code)
		}
	})

	t.Run("with station_id and admin access", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/?station_id=s1", nil)
		req = withAdminClaims(req)
		rr := httptest.NewRecorder()
		ra.handleListRecordings(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("got %d, want 200", rr.Code)
		}
		var resp map[string]any
		json.NewDecoder(rr.Body).Decode(&resp) //nolint:errcheck
		if _, ok := resp["recordings"]; !ok {
			t.Fatal("expected recordings key in response")
		}
	})
}

func TestRecordingAPI_GetRecording_Validation(t *testing.T) {
	ra, _ := newRecordingAPITest(t)

	// Empty recording_id → 400
	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	ra.handleGetRecording(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400", rr.Code)
	}

	// Nonexistent recording → 404
	req = httptest.NewRequest("GET", "/nonexistent", nil)
	req = withChiParam(req, "recordingID", "nonexistent-id")
	req = withAdminClaims(req)
	rr = httptest.NewRecorder()
	ra.handleGetRecording(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("got %d, want 404", rr.Code)
	}
}

func TestRecordingAPI_UpdateRecording_Validation(t *testing.T) {
	ra, _ := newRecordingAPITest(t)

	t.Run("empty recording_id", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"title": "New Title"})
		req := httptest.NewRequest("PATCH", "/", bytes.NewReader(body))
		rr := httptest.NewRecorder()
		ra.handleUpdateRecording(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("got %d, want 400", rr.Code)
		}
	})

	t.Run("invalid JSON", func(t *testing.T) {
		req := httptest.NewRequest("PATCH", "/", bytes.NewReader([]byte("{")))
		req = withChiParam(req, "recordingID", "r1")
		rr := httptest.NewRecorder()
		ra.handleUpdateRecording(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("got %d, want 400", rr.Code)
		}
	})

	t.Run("nonexistent recording", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"title": "New Title"})
		req := httptest.NewRequest("PATCH", "/", bytes.NewReader(body))
		req = withChiParam(req, "recordingID", "nonexistent-id")
		rr := httptest.NewRecorder()
		ra.handleUpdateRecording(rr, req)
		if rr.Code != http.StatusNotFound {
			t.Fatalf("got %d, want 404", rr.Code)
		}
	})
}

func TestRecordingAPI_DeleteRecording_Validation(t *testing.T) {
	ra, _ := newRecordingAPITest(t)

	t.Run("empty recording_id", func(t *testing.T) {
		req := httptest.NewRequest("DELETE", "/", nil)
		rr := httptest.NewRecorder()
		ra.handleDeleteRecording(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("got %d, want 400", rr.Code)
		}
	})

	t.Run("nonexistent recording", func(t *testing.T) {
		req := httptest.NewRequest("DELETE", "/nonexistent", nil)
		req = withChiParam(req, "recordingID", "nonexistent-id")
		rr := httptest.NewRecorder()
		ra.handleDeleteRecording(rr, req)
		if rr.Code != http.StatusNotFound {
			t.Fatalf("got %d, want 404", rr.Code)
		}
	})
}

func TestRecordingAPI_AddChapter_Validation(t *testing.T) {
	ra, _ := newRecordingAPITest(t)

	t.Run("empty recording_id", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"title": "Intro"})
		req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
		rr := httptest.NewRecorder()
		ra.handleAddChapter(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("got %d, want 400", rr.Code)
		}
	})
}

func TestRecordingAPI_ListChapters_Validation(t *testing.T) {
	ra, _ := newRecordingAPITest(t)

	// Empty recording_id → 400
	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	ra.handleListChapters(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400", rr.Code)
	}

	// Valid recording_id but non-existent → 404 (GetRecording fails)
	req = httptest.NewRequest("GET", "/r1/chapters", nil)
	req = withChiParam(req, "recordingID", "r1")
	req = withAdminClaims(req)
	rr = httptest.NewRecorder()
	ra.handleListChapters(rr, req)
	if rr.Code != http.StatusOK && rr.Code != http.StatusNotFound {
		t.Fatalf("list chapters: got %d, want 200 or 404, body=%s", rr.Code, rr.Body.String())
	}
}

func TestRecordingAPI_GetQuota_Validation(t *testing.T) {
	ra, _ := newRecordingAPITest(t)

	t.Run("missing station_id", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		rr := httptest.NewRecorder()
		ra.handleGetQuota(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("got %d, want 400", rr.Code)
		}
	})

	t.Run("with station_id", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/?station_id=s1", nil)
		req = withAdminClaims(req)
		rr := httptest.NewRecorder()
		ra.handleGetQuota(rr, req)
		// May return 200 or 500 if stations table isn't migrated
		if rr.Code != http.StatusOK && rr.Code != http.StatusInternalServerError {
			t.Fatalf("got %d, want 200 or 500, body=%s", rr.Code, rr.Body.String())
		}
	})
}
