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

	"github.com/google/uuid"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

func TestRecordingAPI_StopRecording_NonActive(t *testing.T) {
	ra, db := newRecordingAPITest(t)

	// Seed a completed recording (not active)
	rec := models.Recording{
		ID:        uuid.NewString(),
		StationID: "s1",
		MountID:   "m1",
		UserID:    "u1",
		Format:    models.RecordingFormatFLAC,
		Status:    models.RecordingStatusComplete,
		StartedAt: time.Now().Add(-time.Hour),
	}
	if err := db.Create(&rec).Error; err != nil {
		t.Fatalf("seed recording: %v", err)
	}

	// StopRecording with a complete recording → service returns error → 500
	req := httptest.NewRequest("POST", "/recordings/"+rec.ID+"/stop", nil)
	req = withChiParam(req, "recordingID", rec.ID)
	rr := httptest.NewRecorder()
	ra.handleStopRecording(rr, req)
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("stop non-active recording: got %d, want 500", rr.Code)
	}
}

func TestRecordingAPI_StartRecording_StationNotFound(t *testing.T) {
	ra, _ := newRecordingAPITest(t)

	// Valid body + admin claims → station not found → 500
	body, _ := json.Marshal(map[string]any{"station_id": "s-nonexistent", "mount_id": "m1"})
	req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
	req = withAdminClaims(req)
	rr := httptest.NewRecorder()
	ra.handleStartRecording(rr, req)
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("start with missing station: got %d, want 500", rr.Code)
	}
}

func TestRecordingAPI_AddChapter_InvalidJSON(t *testing.T) {
	ra, db := newRecordingAPITest(t)

	// Seed an active recording
	rec := models.Recording{
		ID:        uuid.NewString(),
		StationID: "s1",
		MountID:   "m1",
		UserID:    "u1",
		Format:    models.RecordingFormatFLAC,
		Status:    models.RecordingStatusActive,
		StartedAt: time.Now(),
	}
	if err := db.Create(&rec).Error; err != nil {
		t.Fatalf("seed recording: %v", err)
	}

	// Invalid JSON with valid recordingID → 400
	req := httptest.NewRequest("POST", "/recordings/"+rec.ID+"/chapters", bytes.NewReader([]byte("{")))
	req = withChiParam(req, "recordingID", rec.ID)
	rr := httptest.NewRecorder()
	ra.handleAddChapter(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("add chapter invalid JSON: got %d, want 400", rr.Code)
	}
}

func TestRecordingAPI_AddChapter_Success(t *testing.T) {
	ra, db := newRecordingAPITest(t)

	// Seed an active recording
	rec := models.Recording{
		ID:        uuid.NewString(),
		StationID: "s1",
		MountID:   "m1",
		UserID:    "u1",
		Format:    models.RecordingFormatFLAC,
		Status:    models.RecordingStatusActive,
		StartedAt: time.Now(),
	}
	if err := db.Create(&rec).Error; err != nil {
		t.Fatalf("seed recording: %v", err)
	}

	// Valid JSON with active recording → 201
	body, _ := json.Marshal(map[string]any{"title": "Intro", "artist": "DJ Test", "album": ""})
	req := httptest.NewRequest("POST", "/recordings/"+rec.ID+"/chapters", bytes.NewReader(body))
	req = withChiParam(req, "recordingID", rec.ID)
	rr := httptest.NewRecorder()
	ra.handleAddChapter(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("add chapter success: got %d, want 201, body=%s", rr.Code, rr.Body.String())
	}
	var resp map[string]any
	json.NewDecoder(rr.Body).Decode(&resp) //nolint:errcheck
	if resp["status"] != "chapter_added" {
		t.Fatalf("expected status=chapter_added, got %v", resp["status"])
	}
}

func TestRecordingAPI_UpdateRecording_Success(t *testing.T) {
	ra, db := newRecordingAPITest(t)

	// Seed a recording
	rec := models.Recording{
		ID:        uuid.NewString(),
		StationID: "s1",
		MountID:   "m1",
		UserID:    "u1",
		Format:    models.RecordingFormatFLAC,
		Status:    models.RecordingStatusComplete,
		StartedAt: time.Now().Add(-time.Hour),
	}
	if err := db.Create(&rec).Error; err != nil {
		t.Fatalf("seed recording: %v", err)
	}

	t.Run("update title success", func(t *testing.T) {
		newTitle := "Updated Title"
		body, _ := json.Marshal(map[string]any{"title": &newTitle})
		req := httptest.NewRequest("PATCH", "/recordings/"+rec.ID, bytes.NewReader(body))
		req = withChiParam(req, "recordingID", rec.ID)
		req = withAdminClaims(req)
		rr := httptest.NewRecorder()
		ra.handleUpdateRecording(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("update recording: got %d, want 200, body=%s", rr.Code, rr.Body.String())
		}
	})

	t.Run("empty body returns recording unchanged", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{})
		req := httptest.NewRequest("PATCH", "/recordings/"+rec.ID, bytes.NewReader(body))
		req = withChiParam(req, "recordingID", rec.ID)
		req = withAdminClaims(req)
		rr := httptest.NewRecorder()
		ra.handleUpdateRecording(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("empty update: got %d, want 200", rr.Code)
		}
	})
}

func TestRecordingAPI_DeleteRecording_Success(t *testing.T) {
	ra, db := newRecordingAPITest(t)

	// Seed a recording
	rec := models.Recording{
		ID:        uuid.NewString(),
		StationID: "s1",
		MountID:   "m1",
		UserID:    "u1",
		Format:    models.RecordingFormatFLAC,
		Status:    models.RecordingStatusComplete,
		StartedAt: time.Now().Add(-time.Hour),
	}
	if err := db.Create(&rec).Error; err != nil {
		t.Fatalf("seed recording: %v", err)
	}

	req := httptest.NewRequest("DELETE", "/recordings/"+rec.ID, nil)
	req = withChiParam(req, "recordingID", rec.ID)
	req = withAdminClaims(req)
	rr := httptest.NewRecorder()
	ra.handleDeleteRecording(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("delete recording: got %d, want 200, body=%s", rr.Code, rr.Body.String())
	}
}

func TestRecordingAPI_ListChapters_Success(t *testing.T) {
	ra, db := newRecordingAPITest(t)

	// Seed a complete recording
	rec := models.Recording{
		ID:        uuid.NewString(),
		StationID: "s1",
		MountID:   "m1",
		UserID:    "u1",
		Format:    models.RecordingFormatFLAC,
		Status:    models.RecordingStatusComplete,
		StartedAt: time.Now().Add(-time.Hour),
	}
	if err := db.Create(&rec).Error; err != nil {
		t.Fatalf("seed recording: %v", err)
	}

	// Seed a chapter
	ch := models.RecordingChapter{
		ID:          uuid.NewString(),
		RecordingID: rec.ID,
		Title:       "Intro",
		Position:    0,
	}
	if err := db.Create(&ch).Error; err != nil {
		t.Fatalf("seed chapter: %v", err)
	}

	req := httptest.NewRequest("GET", "/recordings/"+rec.ID+"/chapters", nil)
	req = withChiParam(req, "recordingID", rec.ID)
	req = withAdminClaims(req)
	rr := httptest.NewRecorder()
	ra.handleListChapters(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("list chapters: got %d, want 200, body=%s", rr.Code, rr.Body.String())
	}
	var resp map[string]any
	json.NewDecoder(rr.Body).Decode(&resp) //nolint:errcheck
	if _, ok := resp["chapters"]; !ok {
		t.Fatal("expected chapters key in response")
	}
}
