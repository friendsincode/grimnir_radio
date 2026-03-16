/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package api

// Validation-path and delegation tests for WebDJAPI handlers.
// These cover the branches reachable without a real webdj.Service:
//   - handleEndSession / handleGetSession (missing chi id param → 400)
//   - handleLoadTrack (invalid deck, missing media_id)
//   - handlePlay / handlePause / handleEject (invalid deck)
//   - handleSeek (invalid deck, invalid json)
//   - handleSetCue (invalid deck, cue_id out of range)
//   - handleDeleteCue (invalid deck, bad cue_id)
//   - handleSetVolume / handleSetEQ / handleSetPitch (invalid deck)
//   - handleSetCrossfader / handleSetMasterVolume (missing/invalid json)
//   - handleListSessions (empty station filter)
//   - handleStartSession (missing station_id)

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// --- handleEndSession ---

// TestHandleWebDJEndSession_MissingID verifies 400 when chi id param absent.
func TestHandleWebDJEndSession_MissingID(t *testing.T) {
	a, _ := newWebDJAPITest(t)

	req := httptest.NewRequest("DELETE", "/webdj/sessions/", nil)
	// No id chi param
	rr := httptest.NewRecorder()
	a.handleEndSession(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("missing id: got %d, want 400", rr.Code)
	}
}

// TestHandleWebDJGetSession_MissingID verifies 400 when chi id param absent.
func TestHandleWebDJGetSession_MissingID(t *testing.T) {
	a, _ := newWebDJAPITest(t)

	req := httptest.NewRequest("GET", "/webdj/sessions/", nil)
	rr := httptest.NewRecorder()
	a.handleGetSession(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("missing id: got %d, want 400", rr.Code)
	}
}

// --- handleLoadTrack ---

// TestHandleWebDJLoadTrack_InvalidDeck verifies 400 for an invalid deck name.
func TestHandleWebDJLoadTrack_InvalidDeck(t *testing.T) {
	a, _ := newWebDJAPITest(t)

	req := httptest.NewRequest("POST", "/webdj/sessions/s1/decks/z/load", nil)
	req = withChiParam(req, "id", "s1")
	req = withChiParam(req, "deck", "z") // invalid deck
	rr := httptest.NewRecorder()
	a.handleLoadTrack(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("invalid deck: got %d, want 400", rr.Code)
	}
}

// TestHandleWebDJLoadTrack_MissingMediaID verifies 400 when media_id absent.
func TestHandleWebDJLoadTrack_MissingMediaID(t *testing.T) {
	a, _ := newWebDJAPITest(t)

	body, _ := json.Marshal(map[string]any{}) // no media_id
	req := httptest.NewRequest("POST", "/webdj/sessions/s1/decks/a/load", bytes.NewBuffer(body))
	req = withChiParam(req, "id", "s1")
	req = withChiParam(req, "deck", "a")
	rr := httptest.NewRecorder()
	a.handleLoadTrack(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("missing media_id: got %d, want 400", rr.Code)
	}
}

// TestHandleWebDJLoadTrack_InvalidJSON verifies 400 for malformed body.
func TestHandleWebDJLoadTrack_InvalidJSON(t *testing.T) {
	a, _ := newWebDJAPITest(t)

	req := httptest.NewRequest("POST", "/webdj/sessions/s1/decks/a/load", bytes.NewBufferString("{bad"))
	req = withChiParam(req, "id", "s1")
	req = withChiParam(req, "deck", "a")
	rr := httptest.NewRecorder()
	a.handleLoadTrack(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("invalid json: got %d, want 400", rr.Code)
	}
}

// --- handlePlay ---

// TestHandleWebDJPlay_InvalidDeck verifies 400 for an invalid deck.
func TestHandleWebDJPlay_InvalidDeck(t *testing.T) {
	a, _ := newWebDJAPITest(t)

	req := httptest.NewRequest("POST", "/webdj/sessions/s1/decks/x/play", nil)
	req = withChiParam(req, "id", "s1")
	req = withChiParam(req, "deck", "x")
	rr := httptest.NewRecorder()
	a.handlePlay(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("invalid deck: got %d, want 400", rr.Code)
	}
}

// --- handlePause ---

// TestHandleWebDJPause_InvalidDeck verifies 400 for an invalid deck.
func TestHandleWebDJPause_InvalidDeck(t *testing.T) {
	a, _ := newWebDJAPITest(t)

	req := httptest.NewRequest("POST", "/webdj/sessions/s1/decks/x/pause", nil)
	req = withChiParam(req, "id", "s1")
	req = withChiParam(req, "deck", "x")
	rr := httptest.NewRecorder()
	a.handlePause(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("invalid deck: got %d, want 400", rr.Code)
	}
}

// --- handleEject ---

// TestHandleWebDJEject_InvalidDeck verifies 400 for an invalid deck.
func TestHandleWebDJEject_InvalidDeck(t *testing.T) {
	a, _ := newWebDJAPITest(t)

	req := httptest.NewRequest("POST", "/webdj/sessions/s1/decks/x/eject", nil)
	req = withChiParam(req, "id", "s1")
	req = withChiParam(req, "deck", "x")
	rr := httptest.NewRecorder()
	a.handleEject(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("invalid deck: got %d, want 400", rr.Code)
	}
}

// --- handleSeek ---

// TestHandleWebDJSeek_InvalidDeck verifies 400 for an invalid deck.
func TestHandleWebDJSeek_InvalidDeck(t *testing.T) {
	a, _ := newWebDJAPITest(t)

	req := httptest.NewRequest("POST", "/webdj/sessions/s1/decks/x/seek", nil)
	req = withChiParam(req, "id", "s1")
	req = withChiParam(req, "deck", "x")
	rr := httptest.NewRecorder()
	a.handleSeek(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("invalid deck: got %d, want 400", rr.Code)
	}
}

// TestHandleWebDJSeek_InvalidJSON verifies 400 for malformed body.
func TestHandleWebDJSeek_InvalidJSON(t *testing.T) {
	a, _ := newWebDJAPITest(t)

	req := httptest.NewRequest("POST", "/webdj/sessions/s1/decks/a/seek", bytes.NewBufferString("{bad"))
	req = withChiParam(req, "id", "s1")
	req = withChiParam(req, "deck", "a")
	rr := httptest.NewRecorder()
	a.handleSeek(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("invalid json: got %d, want 400", rr.Code)
	}
}

// --- handleSetCue ---

// TestHandleWebDJSetCue_InvalidDeck verifies 400 for an invalid deck.
func TestHandleWebDJSetCue_InvalidDeck(t *testing.T) {
	a, _ := newWebDJAPITest(t)

	req := httptest.NewRequest("POST", "/webdj/sessions/s1/decks/x/cue", nil)
	req = withChiParam(req, "id", "s1")
	req = withChiParam(req, "deck", "x")
	rr := httptest.NewRecorder()
	a.handleSetCue(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("invalid deck: got %d, want 400", rr.Code)
	}
}

// TestHandleWebDJSetCue_CueIDTooLarge verifies 400 when cue_id > 8.
func TestHandleWebDJSetCue_CueIDTooLarge(t *testing.T) {
	a, _ := newWebDJAPITest(t)

	body, _ := json.Marshal(map[string]any{"cue_id": 9, "position_ms": 1000})
	req := httptest.NewRequest("POST", "/webdj/sessions/s1/decks/a/cue", bytes.NewBuffer(body))
	req = withChiParam(req, "id", "s1")
	req = withChiParam(req, "deck", "a")
	rr := httptest.NewRecorder()
	a.handleSetCue(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("cue_id > 8: got %d, want 400", rr.Code)
	}
}

// TestHandleWebDJSetCue_CueIDZero verifies 400 when cue_id is 0.
func TestHandleWebDJSetCue_CueIDZero(t *testing.T) {
	a, _ := newWebDJAPITest(t)

	body, _ := json.Marshal(map[string]any{"cue_id": 0, "position_ms": 0})
	req := httptest.NewRequest("POST", "/webdj/sessions/s1/decks/b/cue", bytes.NewBuffer(body))
	req = withChiParam(req, "id", "s1")
	req = withChiParam(req, "deck", "b")
	rr := httptest.NewRecorder()
	a.handleSetCue(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("cue_id=0: got %d, want 400", rr.Code)
	}
}

// TestHandleWebDJSetCue_InvalidJSON verifies 400 for malformed body.
func TestHandleWebDJSetCue_InvalidJSON(t *testing.T) {
	a, _ := newWebDJAPITest(t)

	req := httptest.NewRequest("POST", "/webdj/sessions/s1/decks/a/cue", bytes.NewBufferString("{bad"))
	req = withChiParam(req, "id", "s1")
	req = withChiParam(req, "deck", "a")
	rr := httptest.NewRecorder()
	a.handleSetCue(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("invalid json: got %d, want 400", rr.Code)
	}
}

// --- handleDeleteCue ---

// TestHandleWebDJDeleteCue_InvalidDeck verifies 400 for an invalid deck.
func TestHandleWebDJDeleteCue_InvalidDeck(t *testing.T) {
	a, _ := newWebDJAPITest(t)

	req := httptest.NewRequest("DELETE", "/webdj/sessions/s1/decks/x/cue/1", nil)
	req = withChiParam(req, "id", "s1")
	req = withChiParam(req, "deck", "x")
	req = withChiParam(req, "cue_id", "1")
	rr := httptest.NewRecorder()
	a.handleDeleteCue(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("invalid deck: got %d, want 400", rr.Code)
	}
}

// TestHandleWebDJDeleteCue_InvalidCueID verifies 400 for an out-of-range cue_id.
func TestHandleWebDJDeleteCue_InvalidCueID(t *testing.T) {
	a, _ := newWebDJAPITest(t)

	req := httptest.NewRequest("DELETE", "/webdj/sessions/s1/decks/a/cue/99", nil)
	req = withChiParam(req, "id", "s1")
	req = withChiParam(req, "deck", "a")
	req = withChiParam(req, "cue_id", "99")
	rr := httptest.NewRecorder()
	a.handleDeleteCue(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("cue_id out of range: got %d, want 400", rr.Code)
	}
}

// TestHandleWebDJDeleteCue_NonNumericCueID verifies 400 for a non-numeric cue_id.
func TestHandleWebDJDeleteCue_NonNumericCueID(t *testing.T) {
	a, _ := newWebDJAPITest(t)

	req := httptest.NewRequest("DELETE", "/webdj/sessions/s1/decks/a/cue/abc", nil)
	req = withChiParam(req, "id", "s1")
	req = withChiParam(req, "deck", "a")
	req = withChiParam(req, "cue_id", "abc")
	rr := httptest.NewRecorder()
	a.handleDeleteCue(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("non-numeric cue_id: got %d, want 400", rr.Code)
	}
}

// --- handleSetVolume ---

// TestHandleWebDJSetVolume_InvalidDeck verifies 400 for an invalid deck.
func TestHandleWebDJSetVolume_InvalidDeck(t *testing.T) {
	a, _ := newWebDJAPITest(t)

	req := httptest.NewRequest("POST", "/webdj/sessions/s1/decks/x/volume", nil)
	req = withChiParam(req, "id", "s1")
	req = withChiParam(req, "deck", "x")
	rr := httptest.NewRecorder()
	a.handleSetVolume(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("invalid deck: got %d, want 400", rr.Code)
	}
}

// TestHandleWebDJSetVolume_InvalidJSON verifies 400 for malformed body.
func TestHandleWebDJSetVolume_InvalidJSON(t *testing.T) {
	a, _ := newWebDJAPITest(t)

	req := httptest.NewRequest("POST", "/webdj/sessions/s1/decks/a/volume", bytes.NewBufferString("bad"))
	req = withChiParam(req, "id", "s1")
	req = withChiParam(req, "deck", "a")
	rr := httptest.NewRecorder()
	a.handleSetVolume(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("invalid json: got %d, want 400", rr.Code)
	}
}

// --- handleSetEQ ---

// TestHandleWebDJSetEQ_InvalidDeck verifies 400 for an invalid deck.
func TestHandleWebDJSetEQ_InvalidDeck(t *testing.T) {
	a, _ := newWebDJAPITest(t)

	req := httptest.NewRequest("POST", "/webdj/sessions/s1/decks/x/eq", nil)
	req = withChiParam(req, "id", "s1")
	req = withChiParam(req, "deck", "x")
	rr := httptest.NewRecorder()
	a.handleSetEQ(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("invalid deck: got %d, want 400", rr.Code)
	}
}

// TestHandleWebDJSetEQ_InvalidJSON verifies 400 for malformed body.
func TestHandleWebDJSetEQ_InvalidJSON(t *testing.T) {
	a, _ := newWebDJAPITest(t)

	req := httptest.NewRequest("POST", "/webdj/sessions/s1/decks/a/eq", bytes.NewBufferString("bad"))
	req = withChiParam(req, "id", "s1")
	req = withChiParam(req, "deck", "a")
	rr := httptest.NewRecorder()
	a.handleSetEQ(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("invalid json: got %d, want 400", rr.Code)
	}
}

// --- handleSetPitch ---

// TestHandleWebDJSetPitch_InvalidDeck verifies 400 for an invalid deck.
func TestHandleWebDJSetPitch_InvalidDeck(t *testing.T) {
	a, _ := newWebDJAPITest(t)

	req := httptest.NewRequest("POST", "/webdj/sessions/s1/decks/x/pitch", nil)
	req = withChiParam(req, "id", "s1")
	req = withChiParam(req, "deck", "x")
	rr := httptest.NewRecorder()
	a.handleSetPitch(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("invalid deck: got %d, want 400", rr.Code)
	}
}

// --- handleSetCrossfader ---

// TestHandleWebDJSetCrossfader_InvalidJSON verifies 400 for malformed body.
func TestHandleWebDJSetCrossfader_InvalidJSON(t *testing.T) {
	a, _ := newWebDJAPITest(t)

	req := httptest.NewRequest("POST", "/webdj/sessions/s1/mixer/crossfader", bytes.NewBufferString("bad"))
	req = withChiParam(req, "id", "s1")
	rr := httptest.NewRecorder()
	a.handleSetCrossfader(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("invalid json: got %d, want 400", rr.Code)
	}
}

// --- handleSetMasterVolume ---

// TestHandleWebDJSetMasterVolume_InvalidJSON verifies 400 for malformed body.
func TestHandleWebDJSetMasterVolume_InvalidJSON(t *testing.T) {
	a, _ := newWebDJAPITest(t)

	req := httptest.NewRequest("POST", "/webdj/sessions/s1/mixer/volume", bytes.NewBufferString("bad"))
	req = withChiParam(req, "id", "s1")
	rr := httptest.NewRecorder()
	a.handleSetMasterVolume(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("invalid json: got %d, want 400", rr.Code)
	}
}

// (handleListSessions requires a non-nil webdjSvc, tested via nil-API delegation in webdj_nil_test.go)

// --- handleStartSession ---

// TestHandleWebDJStartSession_MissingStationID verifies 400 when station_id absent.
func TestHandleWebDJStartSession_MissingStationID(t *testing.T) {
	a, _ := newWebDJAPITest(t)

	body, _ := json.Marshal(map[string]any{}) // no station_id
	req := httptest.NewRequest("POST", "/webdj/sessions", bytes.NewBuffer(body))
	rr := httptest.NewRecorder()
	a.handleStartSession(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("missing station_id: got %d, want 400", rr.Code)
	}
}

// TestHandleWebDJStartSession_InvalidJSON verifies 400 on malformed body.
func TestHandleWebDJStartSession_InvalidJSON(t *testing.T) {
	a, _ := newWebDJAPITest(t)

	req := httptest.NewRequest("POST", "/webdj/sessions", bytes.NewBufferString("{bad"))
	rr := httptest.NewRecorder()
	a.handleStartSession(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("invalid json: got %d, want 400", rr.Code)
	}
}

// --- handleGoLive ---

// TestHandleWebDJGoLive_MissingSessionID verifies 400 when chi id param absent.
func TestHandleWebDJGoLive_MissingSessionID(t *testing.T) {
	a, _ := newWebDJAPITest(t)

	req := httptest.NewRequest("POST", "/webdj/sessions//live", nil)
	// No id chi param
	rr := httptest.NewRecorder()
	a.handleGoLive(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("missing session_id: got %d, want 400", rr.Code)
	}
}

// TestHandleWebDJGoLive_InvalidJSON verifies 400 on malformed body.
func TestHandleWebDJGoLive_InvalidJSON(t *testing.T) {
	a, _ := newWebDJAPITest(t)

	req := httptest.NewRequest("POST", "/webdj/sessions/s1/live", bytes.NewBufferString("{bad"))
	req = withChiParam(req, "id", "s1")
	rr := httptest.NewRecorder()
	a.handleGoLive(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("invalid json: got %d, want 400", rr.Code)
	}
}

// TestHandleWebDJGoLive_MissingMountID verifies 400 when mount_id absent.
func TestHandleWebDJGoLive_MissingMountID(t *testing.T) {
	a, _ := newWebDJAPITest(t)

	body, _ := json.Marshal(map[string]any{}) // no mount_id
	req := httptest.NewRequest("POST", "/webdj/sessions/s1/live", bytes.NewBuffer(body))
	req = withChiParam(req, "id", "s1")
	rr := httptest.NewRecorder()
	a.handleGoLive(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("missing mount_id: got %d, want 400", rr.Code)
	}
}

// --- handleGoOffAir ---

// TestHandleWebDJGoOffAir_MissingSessionID verifies 400 when chi id param absent.
func TestHandleWebDJGoOffAir_MissingSessionID(t *testing.T) {
	a, _ := newWebDJAPITest(t)

	req := httptest.NewRequest("POST", "/webdj/sessions//offair", nil)
	// No id chi param
	rr := httptest.NewRecorder()
	a.handleGoOffAir(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("missing session_id: got %d, want 400", rr.Code)
	}
}

// --- handleGetWaveform ---

// TestHandleWebDJGetWaveform_MissingMediaID verifies 400 when chi id param absent.
func TestHandleWebDJGetWaveform_MissingMediaID(t *testing.T) {
	a, _ := newWebDJAPITest(t)

	req := httptest.NewRequest("GET", "/webdj/waveform/", nil)
	// No id chi param
	rr := httptest.NewRecorder()
	a.handleGetWaveform(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("missing media_id: got %d, want 400", rr.Code)
	}
}
