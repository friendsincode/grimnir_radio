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
)

// newLiveAPITest creates an API with nil live service (sufficient for validation-only tests).
func newLiveAPITest(t *testing.T) *API {
	t.Helper()
	return &API{logger: zerolog.Nop()}
}

func TestLive_GenerateToken(t *testing.T) {
	a := newLiveAPITest(t)

	t.Run("invalid JSON", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/live/token", bytes.NewReader([]byte("{")))
		rr := httptest.NewRecorder()
		a.handleLiveGenerateToken(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("got %d, want 400", rr.Code)
		}
	})

	t.Run("missing required fields", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"station_id": "s1"})
		req := httptest.NewRequest("POST", "/live/token", bytes.NewReader(body))
		rr := httptest.NewRecorder()
		a.handleLiveGenerateToken(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("got %d, want 400", rr.Code)
		}
	})

	t.Run("invalid priority", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"station_id": "s1",
			"mount_id":   "m1",
			"user_id":    "u1",
			"priority":   99,
		})
		req := httptest.NewRequest("POST", "/live/token", bytes.NewReader(body))
		rr := httptest.NewRecorder()
		a.handleLiveGenerateToken(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("got %d, want 400", rr.Code)
		}
	})
}

func TestLive_Authorize(t *testing.T) {
	a := newLiveAPITest(t)

	t.Run("invalid JSON", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/live/authorize", bytes.NewReader([]byte("{")))
		rr := httptest.NewRecorder()
		a.handleLiveAuthorize(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("got %d, want 400", rr.Code)
		}
	})

	t.Run("missing fields", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"station_id": "s1"})
		req := httptest.NewRequest("POST", "/live/authorize", bytes.NewReader(body))
		rr := httptest.NewRecorder()
		a.handleLiveAuthorize(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("got %d, want 400", rr.Code)
		}
	})
}

func TestLive_Connect(t *testing.T) {
	a := newLiveAPITest(t)

	t.Run("invalid JSON", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/live/connect", bytes.NewReader([]byte("{")))
		rr := httptest.NewRecorder()
		a.handleLiveConnect(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("got %d, want 400", rr.Code)
		}
	})

	t.Run("missing fields", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"station_id": "s1"})
		req := httptest.NewRequest("POST", "/live/connect", bytes.NewReader(body))
		rr := httptest.NewRecorder()
		a.handleLiveConnect(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("got %d, want 400", rr.Code)
		}
	})
}

func TestLive_Disconnect_EmptySessionID(t *testing.T) {
	a := newLiveAPITest(t)

	req := httptest.NewRequest("POST", "/live/sessions//disconnect", nil)
	// No chi param → empty session_id → 400
	rr := httptest.NewRecorder()
	a.handleLiveDisconnect(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400", rr.Code)
	}
}

func TestLive_GetSession_EmptySessionID(t *testing.T) {
	a := newLiveAPITest(t)

	req := httptest.NewRequest("GET", "/live/sessions/", nil)
	// No chi param → empty session_id → 400
	rr := httptest.NewRecorder()
	a.handleGetLiveSession(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400", rr.Code)
	}
}

func TestLive_StartHandover(t *testing.T) {
	a := newLiveAPITest(t)

	t.Run("invalid JSON", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/live/handover", bytes.NewReader([]byte("{")))
		rr := httptest.NewRecorder()
		a.handleLiveStartHandover(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("got %d, want 400", rr.Code)
		}
	})

	t.Run("missing fields", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"session_id": "s1"})
		req := httptest.NewRequest("POST", "/live/handover", bytes.NewReader(body))
		rr := httptest.NewRecorder()
		a.handleLiveStartHandover(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("got %d, want 400", rr.Code)
		}
	})

	t.Run("invalid priority", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"session_id": "s1",
			"station_id": "st1",
			"mount_id":   "m1",
			"user_id":    "u1",
			"priority":   99,
		})
		req := httptest.NewRequest("POST", "/live/handover", bytes.NewReader(body))
		rr := httptest.NewRecorder()
		a.handleLiveStartHandover(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("got %d, want 400", rr.Code)
		}
	})
}

func TestLive_ReleaseHandover_EmptySessionID(t *testing.T) {
	a := newLiveAPITest(t)

	req := httptest.NewRequest("DELETE", "/live/handover/", nil)
	// No chi param → empty session_id → 400
	rr := httptest.NewRecorder()
	a.handleLiveReleaseHandover(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400", rr.Code)
	}
}
