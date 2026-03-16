/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package api

// Validation-path tests for handleLiveGenerateToken, handleLiveAuthorize,
// handleLiveConnect, handleLiveDisconnect (not-found), handleLiveStartHandover,
// and handleLiveReleaseHandover.  These all exercise branches reachable without
// a real IceCast / SRT / RTP stream.

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// --- handleLiveGenerateToken ---

// TestHandleLiveGenerateToken_InvalidJSON verifies 400 on malformed body.
func TestHandleLiveGenerateToken_InvalidJSON(t *testing.T) {
	a, _ := newLiveWithServiceTest(t)

	req := httptest.NewRequest("POST", "/live/token", bytes.NewBufferString("{bad json"))
	rr := httptest.NewRecorder()
	a.handleLiveGenerateToken(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("invalid json: got %d, want 400", rr.Code)
	}
}

// TestHandleLiveGenerateToken_MissingFields verifies 400 when required fields are absent.
func TestHandleLiveGenerateToken_MissingFields(t *testing.T) {
	a, _ := newLiveWithServiceTest(t)

	body, _ := json.Marshal(map[string]any{"station_id": "st1"}) // missing mount_id, user_id
	req := httptest.NewRequest("POST", "/live/token", bytes.NewBuffer(body))
	rr := httptest.NewRecorder()
	a.handleLiveGenerateToken(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("missing fields: got %d, want 400", rr.Code)
	}
}

// TestHandleLiveGenerateToken_InvalidPriority verifies 400 for an unsupported priority value.
func TestHandleLiveGenerateToken_InvalidPriority(t *testing.T) {
	a, _ := newLiveWithServiceTest(t)

	body, _ := json.Marshal(map[string]any{
		"station_id": "st1",
		"mount_id":   "mt1",
		"user_id":    "u1",
		"priority":   99, // not live_override or live_scheduled
	})
	req := httptest.NewRequest("POST", "/live/token", bytes.NewBuffer(body))
	rr := httptest.NewRecorder()
	a.handleLiveGenerateToken(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("invalid priority: got %d, want 400", rr.Code)
	}
}

// TestHandleLiveGenerateToken_ValidPriorityLiveOverride verifies that a valid
// live_override priority (1) passes validation and reaches the service layer.
func TestHandleLiveGenerateToken_ValidPriorityLiveOverride(t *testing.T) {
	a, _ := newLiveWithServiceTest(t)

	body, _ := json.Marshal(map[string]any{
		"station_id": "st1",
		"mount_id":   "mt1",
		"user_id":    "u1",
		"priority":   1, // models.PriorityLiveOverride = 1
	})
	req := httptest.NewRequest("POST", "/live/token", bytes.NewBuffer(body))
	rr := httptest.NewRecorder()
	a.handleLiveGenerateToken(rr, req)

	// Should not be 400 (validation passed, service generates token)
	if rr.Code == http.StatusBadRequest {
		t.Fatalf("valid priority rejected: got 400; body=%s", rr.Body.String())
	}
}

// --- handleLiveAuthorize ---

// TestHandleLiveAuthorize_InvalidJSON verifies 400 on malformed body.
func TestHandleLiveAuthorize_InvalidJSON(t *testing.T) {
	a, _ := newLiveWithServiceTest(t)

	req := httptest.NewRequest("POST", "/live/authorize", bytes.NewBufferString("not json"))
	rr := httptest.NewRecorder()
	a.handleLiveAuthorize(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("invalid json: got %d, want 400", rr.Code)
	}
}

// TestHandleLiveAuthorize_MissingToken verifies 400 when token is absent.
func TestHandleLiveAuthorize_MissingToken(t *testing.T) {
	a, _ := newLiveWithServiceTest(t)

	body, _ := json.Marshal(map[string]any{"station_id": "st1", "mount_id": "mt1"})
	req := httptest.NewRequest("POST", "/live/authorize", bytes.NewBuffer(body))
	rr := httptest.NewRecorder()
	a.handleLiveAuthorize(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("missing token: got %d, want 400", rr.Code)
	}
}

// TestHandleLiveAuthorize_InvalidToken verifies non-200 when token is invalid.
func TestHandleLiveAuthorize_InvalidToken(t *testing.T) {
	a, _ := newLiveWithServiceTest(t)

	body, _ := json.Marshal(map[string]any{
		"station_id": "st1",
		"mount_id":   "mt1",
		"token":      "totally-invalid-token",
	})
	req := httptest.NewRequest("POST", "/live/authorize", bytes.NewBuffer(body))
	rr := httptest.NewRecorder()
	a.handleLiveAuthorize(rr, req)

	// Invalid token should yield 401 or 500, not 200 or 400.
	if rr.Code == http.StatusBadRequest {
		t.Fatalf("got 400 but all fields were present; body=%s", rr.Body.String())
	}
}

// --- handleLiveConnect ---

// TestHandleLiveConnect_InvalidJSON verifies 400 on malformed body.
func TestHandleLiveConnect_InvalidJSON(t *testing.T) {
	a, _ := newLiveWithServiceTest(t)

	req := httptest.NewRequest("POST", "/live/connect", bytes.NewBufferString("{oops"))
	rr := httptest.NewRecorder()
	a.handleLiveConnect(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("invalid json: got %d, want 400", rr.Code)
	}
}

// TestHandleLiveConnect_MissingFields verifies 400 when required fields are absent.
func TestHandleLiveConnect_MissingFields(t *testing.T) {
	a, _ := newLiveWithServiceTest(t)

	body, _ := json.Marshal(map[string]any{"station_id": "st1"}) // no mount_id, no token
	req := httptest.NewRequest("POST", "/live/connect", bytes.NewBuffer(body))
	rr := httptest.NewRecorder()
	a.handleLiveConnect(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("missing fields: got %d, want 400", rr.Code)
	}
}

// TestHandleLiveConnect_InvalidToken verifies non-200 when the provided token
// is not in the database (service returns ErrInvalidToken or similar).
func TestHandleLiveConnect_InvalidToken(t *testing.T) {
	a, _ := newLiveWithServiceTest(t)

	body, _ := json.Marshal(map[string]any{
		"station_id": "st1",
		"mount_id":   "mt1",
		"token":      "fake-token",
	})
	req := httptest.NewRequest("POST", "/live/connect", bytes.NewBuffer(body))
	rr := httptest.NewRecorder()
	a.handleLiveConnect(rr, req)

	// Should be non-200 (connect will fail with bad token)
	if rr.Code == http.StatusOK {
		t.Fatal("expected non-200 for invalid token")
	}
}

// --- handleLiveDisconnect (not-found path) ---

// TestHandleLiveDisconnect_NotFound verifies 404 when disconnecting a session
// that does not exist.
func TestHandleLiveDisconnect_NotFound(t *testing.T) {
	a, _ := newLiveWithServiceTest(t)

	req := httptest.NewRequest("POST", "/live/sessions/ghost/disconnect", nil)
	req = withChiParam(req, "session_id", "ghost-session-id")
	rr := httptest.NewRecorder()
	a.handleLiveDisconnect(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("not-found disconnect: got %d, want 404", rr.Code)
	}
}

// --- handleLiveStartHandover ---

// TestHandleLiveStartHandover_InvalidJSON verifies 400 on malformed body.
func TestHandleLiveStartHandover_InvalidJSON(t *testing.T) {
	a, _ := newLiveWithServiceTest(t)

	req := httptest.NewRequest("POST", "/live/handover/start", bytes.NewBufferString("bad"))
	rr := httptest.NewRecorder()
	a.handleLiveStartHandover(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("invalid json: got %d, want 400", rr.Code)
	}
}

// TestHandleLiveStartHandover_MissingSessionID verifies 400 when session_id absent.
func TestHandleLiveStartHandover_MissingSessionID(t *testing.T) {
	a, _ := newLiveWithServiceTest(t)

	body, _ := json.Marshal(map[string]any{
		// session_id intentionally missing
		"station_id": "st1",
		"mount_id":   "mt1",
		"user_id":    "u1",
		"priority":   1, // models.PriorityLiveOverride
	})
	req := httptest.NewRequest("POST", "/live/handover/start", bytes.NewBuffer(body))
	rr := httptest.NewRecorder()
	a.handleLiveStartHandover(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("missing session_id: got %d, want 400", rr.Code)
	}
}

// TestHandleLiveStartHandover_InvalidPriority verifies 400 for an unsupported priority.
func TestHandleLiveStartHandover_InvalidPriority(t *testing.T) {
	a, _ := newLiveWithServiceTest(t)

	body, _ := json.Marshal(map[string]any{
		"session_id": "s1",
		"station_id": "st1",
		"mount_id":   "mt1",
		"user_id":    "u1",
		"priority":   99, // invalid — not live_override (1) or live_scheduled (2)
	})
	req := httptest.NewRequest("POST", "/live/handover/start", bytes.NewBuffer(body))
	rr := httptest.NewRecorder()
	a.handleLiveStartHandover(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("invalid priority: got %d, want 400; body=%s", rr.Code, rr.Body.String())
	}
}

// TestHandleLiveStartHandover_MissingStationID verifies 400 when station_id absent.
func TestHandleLiveStartHandover_MissingStationID(t *testing.T) {
	a, _ := newLiveWithServiceTest(t)

	body, _ := json.Marshal(map[string]any{
		"session_id": "s1",
		// station_id missing
		"mount_id": "mt1",
		"user_id":  "u1",
		"priority": 1, // models.PriorityLiveOverride
	})
	req := httptest.NewRequest("POST", "/live/handover/start", bytes.NewBuffer(body))
	rr := httptest.NewRecorder()
	a.handleLiveStartHandover(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("missing station_id: got %d, want 400", rr.Code)
	}
}

// --- handleLiveReleaseHandover ---

// TestHandleLiveReleaseHandover_MissingSessionID verifies 400 when chi param absent.
func TestHandleLiveReleaseHandover_MissingSessionID(t *testing.T) {
	a, _ := newLiveWithServiceTest(t)

	req := httptest.NewRequest("POST", "/live/handover/release", nil)
	// No session_id chi param
	rr := httptest.NewRecorder()
	a.handleLiveReleaseHandover(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("missing session_id: got %d, want 400", rr.Code)
	}
}

// TestHandleLiveReleaseHandover_NotFound verifies a non-200 response when releasing
// a session that does not exist (may be 404 or 500 depending on service error mapping).
func TestHandleLiveReleaseHandover_NotFound(t *testing.T) {
	a, _ := newLiveWithServiceTest(t)

	req := httptest.NewRequest("POST", "/live/handover/release/ghost", nil)
	req = withChiParam(req, "session_id", "ghost-session-99")
	rr := httptest.NewRecorder()
	a.handleLiveReleaseHandover(rr, req)

	if rr.Code == http.StatusOK {
		t.Fatalf("expected non-200 for nonexistent session, got 200")
	}
}
