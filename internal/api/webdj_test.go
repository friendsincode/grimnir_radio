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

	"github.com/friendsincode/grimnir_radio/internal/models"
	"github.com/friendsincode/grimnir_radio/internal/webdj"
)

func newWebDJAPITest(t *testing.T) (*WebDJAPI, *gorm.DB) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "test.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&models.WebDJSession{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// nil webdjSvc and waveformSvc are ok for validation-only tests
	return &WebDJAPI{db: db, webdjSvc: nil, waveformSvc: nil, logger: zerolog.Nop()}, db
}

func TestWebDJ_ParseDeck(t *testing.T) {
	tests := []struct {
		input   string
		wantErr bool
		wantID  models.DeckID
	}{
		{"a", false, models.DeckA},
		{"A", false, models.DeckA},
		{"b", false, models.DeckB},
		{"B", false, models.DeckB},
		{"c", true, ""},
		{"", true, ""},
		{"deck_a", true, ""},
	}
	for _, tc := range tests {
		got, err := parseDeck(tc.input)
		if tc.wantErr {
			if err == nil {
				t.Errorf("parseDeck(%q): expected error, got nil", tc.input)
			}
		} else {
			if err != nil {
				t.Errorf("parseDeck(%q): unexpected error: %v", tc.input, err)
			}
			if got != tc.wantID {
				t.Errorf("parseDeck(%q): got %q, want %q", tc.input, got, tc.wantID)
			}
		}
	}
}

func TestWebDJ_HandleWebDJError(t *testing.T) {
	tests := []struct {
		err      error
		wantCode int
	}{
		{webdj.ErrSessionNotFound, http.StatusNotFound},
		{webdj.ErrSessionNotActive, http.StatusBadRequest},
		{webdj.ErrInvalidDeck, http.StatusBadRequest},
		{webdj.ErrNoTrackLoaded, http.StatusBadRequest},
		{webdj.ErrMediaNotFound, http.StatusNotFound},
		{webdj.ErrUnauthorized, http.StatusForbidden},
	}
	for _, tc := range tests {
		rr := httptest.NewRecorder()
		handleWebDJError(rr, tc.err)
		if rr.Code != tc.wantCode {
			t.Errorf("handleWebDJError(%v): got %d, want %d", tc.err, rr.Code, tc.wantCode)
		}
	}
}

func TestWebDJ_StartSession_Validation(t *testing.T) {
	a, _ := newWebDJAPITest(t)

	t.Run("invalid JSON", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/", bytes.NewReader([]byte("{")))
		rr := httptest.NewRecorder()
		a.handleStartSession(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("got %d, want 400", rr.Code)
		}
	})

	t.Run("missing station_id", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{})
		req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
		rr := httptest.NewRecorder()
		a.handleStartSession(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("got %d, want 400", rr.Code)
		}
	})

	t.Run("missing auth", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"station_id": "s1"})
		req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
		// No auth claims in context
		rr := httptest.NewRecorder()
		a.handleStartSession(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("got %d, want 401", rr.Code)
		}
	})
}

func TestWebDJ_EndSession_Validation(t *testing.T) {
	a, _ := newWebDJAPITest(t)

	// Empty sessionID (no chi param set) → 400
	req := httptest.NewRequest("DELETE", "/", nil)
	rr := httptest.NewRecorder()
	a.handleEndSession(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400", rr.Code)
	}
}

func TestWebDJ_GetSession_Validation(t *testing.T) {
	a, _ := newWebDJAPITest(t)

	// Empty sessionID → 400
	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	a.handleGetSession(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400", rr.Code)
	}
}

func TestWebDJ_DeckHandlers_InvalidDeck(t *testing.T) {
	a, _ := newWebDJAPITest(t)

	// All deck-based handlers return 400 for invalid deck before any service call

	t.Run("load_track", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"media_id": "m1"})
		req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
		req = withChiParam(withChiParam(req, "id", "sess-1"), "deck", "invalid")
		rr := httptest.NewRecorder()
		a.handleLoadTrack(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("load_track: got %d, want 400", rr.Code)
		}
	})

	t.Run("play", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/", nil)
		req = withChiParam(withChiParam(req, "id", "sess-1"), "deck", "x")
		rr := httptest.NewRecorder()
		a.handlePlay(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("play: got %d, want 400", rr.Code)
		}
	})

	t.Run("pause", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/", nil)
		req = withChiParam(withChiParam(req, "id", "sess-1"), "deck", "x")
		rr := httptest.NewRecorder()
		a.handlePause(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("pause: got %d, want 400", rr.Code)
		}
	})

	t.Run("seek", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"position_ms": 1000})
		req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
		req = withChiParam(withChiParam(req, "id", "sess-1"), "deck", "x")
		rr := httptest.NewRecorder()
		a.handleSeek(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("seek: got %d, want 400", rr.Code)
		}
	})

	t.Run("set_cue", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"position_ms": 0})
		req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
		req = withChiParam(withChiParam(req, "id", "sess-1"), "deck", "x")
		rr := httptest.NewRecorder()
		a.handleSetCue(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("set_cue: got %d, want 400", rr.Code)
		}
	})

	t.Run("delete_cue", func(t *testing.T) {
		req := httptest.NewRequest("DELETE", "/", nil)
		req = withChiParam(withChiParam(req, "id", "sess-1"), "deck", "x")
		rr := httptest.NewRecorder()
		a.handleDeleteCue(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("delete_cue: got %d, want 400", rr.Code)
		}
	})

	t.Run("eject", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/", nil)
		req = withChiParam(withChiParam(req, "id", "sess-1"), "deck", "x")
		rr := httptest.NewRecorder()
		a.handleEject(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("eject: got %d, want 400", rr.Code)
		}
	})

	t.Run("set_volume", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"volume": 0.5})
		req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
		req = withChiParam(withChiParam(req, "id", "sess-1"), "deck", "x")
		rr := httptest.NewRecorder()
		a.handleSetVolume(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("set_volume: got %d, want 400", rr.Code)
		}
	})

	t.Run("set_eq", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"low": 0.0, "mid": 0.0, "high": 0.0})
		req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
		req = withChiParam(withChiParam(req, "id", "sess-1"), "deck", "x")
		rr := httptest.NewRecorder()
		a.handleSetEQ(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("set_eq: got %d, want 400", rr.Code)
		}
	})

	t.Run("set_pitch", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"pitch": 1.0})
		req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
		req = withChiParam(withChiParam(req, "id", "sess-1"), "deck", "x")
		rr := httptest.NewRecorder()
		a.handleSetPitch(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("set_pitch: got %d, want 400", rr.Code)
		}
	})
}

func TestWebDJ_LoadTrack_MissingMediaID(t *testing.T) {
	a, _ := newWebDJAPITest(t)

	// Valid deck but missing media_id → 400
	body, _ := json.Marshal(map[string]any{})
	req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
	req = withChiParam(withChiParam(req, "id", "sess-1"), "deck", "a")
	rr := httptest.NewRecorder()
	a.handleLoadTrack(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400", rr.Code)
	}
}

func TestWebDJ_Seek_Validation(t *testing.T) {
	a, _ := newWebDJAPITest(t)

	// Valid deck but invalid JSON → 400
	req := httptest.NewRequest("POST", "/", bytes.NewReader([]byte("{")))
	req = withChiParam(withChiParam(req, "id", "sess-1"), "deck", "a")
	rr := httptest.NewRecorder()
	a.handleSeek(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400", rr.Code)
	}
}

func TestWebDJ_SetCue_Validation(t *testing.T) {
	a, _ := newWebDJAPITest(t)

	// Valid deck but invalid JSON → 400
	req := httptest.NewRequest("POST", "/", bytes.NewReader([]byte("{")))
	req = withChiParam(withChiParam(req, "id", "sess-1"), "deck", "a")
	rr := httptest.NewRecorder()
	a.handleSetCue(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400", rr.Code)
	}
}

func TestWebDJ_SetVolume_Validation(t *testing.T) {
	a, _ := newWebDJAPITest(t)

	// Valid deck but invalid JSON → 400
	req := httptest.NewRequest("POST", "/", bytes.NewReader([]byte("{")))
	req = withChiParam(withChiParam(req, "id", "sess-1"), "deck", "a")
	rr := httptest.NewRecorder()
	a.handleSetVolume(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400", rr.Code)
	}
}

func TestWebDJ_SetEQ_Validation(t *testing.T) {
	a, _ := newWebDJAPITest(t)

	// Valid deck but invalid JSON → 400
	req := httptest.NewRequest("POST", "/", bytes.NewReader([]byte("{")))
	req = withChiParam(withChiParam(req, "id", "sess-1"), "deck", "a")
	rr := httptest.NewRecorder()
	a.handleSetEQ(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400", rr.Code)
	}
}

func TestWebDJ_SetPitch_Validation(t *testing.T) {
	a, _ := newWebDJAPITest(t)

	// Valid deck but invalid JSON → 400
	req := httptest.NewRequest("POST", "/", bytes.NewReader([]byte("{")))
	req = withChiParam(withChiParam(req, "id", "sess-1"), "deck", "a")
	rr := httptest.NewRecorder()
	a.handleSetPitch(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400", rr.Code)
	}
}

func TestWebDJ_Crossfader_Validation(t *testing.T) {
	a, _ := newWebDJAPITest(t)

	// Invalid JSON → 400
	req := httptest.NewRequest("POST", "/", bytes.NewReader([]byte("{")))
	req = withChiParam(req, "id", "sess-1")
	rr := httptest.NewRecorder()
	a.handleSetCrossfader(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("crossfader invalid JSON: got %d, want 400", rr.Code)
	}
}

func TestWebDJ_MasterVolume_Validation(t *testing.T) {
	a, _ := newWebDJAPITest(t)

	// Invalid JSON → 400
	req := httptest.NewRequest("POST", "/", bytes.NewReader([]byte("{")))
	req = withChiParam(req, "id", "sess-1")
	rr := httptest.NewRecorder()
	a.handleSetMasterVolume(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("master_volume invalid JSON: got %d, want 400", rr.Code)
	}
}

func TestWebDJ_GoLive_Validation(t *testing.T) {
	a, _ := newWebDJAPITest(t)

	// Missing sessionID → 400 (before service call)
	t.Run("missing session_id", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"mount_id": "m1"})
		req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
		// No chi param for "id" → empty string → 400
		rr := httptest.NewRecorder()
		a.handleGoLive(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("go_live no session: got %d, want 400", rr.Code)
		}
	})

	t.Run("invalid JSON", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/", bytes.NewReader([]byte("{")))
		req = withChiParam(req, "id", "sess-1")
		rr := httptest.NewRecorder()
		a.handleGoLive(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("go_live invalid JSON: got %d, want 400", rr.Code)
		}
	})

	t.Run("missing mount_id", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"input_type": "icecast"})
		req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
		req = withChiParam(req, "id", "sess-1")
		rr := httptest.NewRecorder()
		a.handleGoLive(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("go_live missing mount: got %d, want 400", rr.Code)
		}
	})
}

func TestWebDJ_GoOffAir_Validation(t *testing.T) {
	a, _ := newWebDJAPITest(t)

	// Empty sessionID (no chi param) → 400 (before service call)
	req := httptest.NewRequest("POST", "/", nil)
	rr := httptest.NewRecorder()
	a.handleGoOffAir(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("go_off_air empty session: got %d, want 400", rr.Code)
	}
}

func TestWebDJ_GetWaveform_Validation(t *testing.T) {
	a, _ := newWebDJAPITest(t)

	// Empty media_id → 400 (waveformSvc is nil but validation happens first)
	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	a.handleGetWaveform(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("get_waveform no media_id: got %d, want 400", rr.Code)
	}
}
