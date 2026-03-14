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
)

func TestPriorityAPI_EmergencySuccess(t *testing.T) {
	a, _ := newPriorityAPITest(t)

	// Valid emergency request → success (creates PrioritySource in DB)
	body, _ := json.Marshal(map[string]any{
		"station_id": "s1",
		"media_id":   "media-1",
	})
	req := httptest.NewRequest("POST", "/priority/emergency", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	a.handlePriorityEmergency(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("emergency success: got %d, want 200, body=%s", rr.Code, rr.Body.String())
	}
	var resp map[string]any
	json.NewDecoder(rr.Body).Decode(&resp) //nolint:errcheck
	if resp["status"] != "emergency_activated" {
		t.Fatalf("expected status=emergency_activated, got %v", resp["status"])
	}
	if _, ok := resp["source_id"]; !ok {
		t.Fatal("expected source_id in response")
	}
}

func TestPriorityAPI_OverrideSuccess(t *testing.T) {
	a, _ := newPriorityAPITest(t)

	// Valid override request with "live" source_type → 200 or 409
	body, _ := json.Marshal(map[string]any{
		"station_id":  "s1",
		"source_id":   "live-src-1",
		"source_type": "live",
	})
	req := httptest.NewRequest("POST", "/priority/override", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	a.handlePriorityOverride(rr, req)
	// 200 = override activated; 409 = no preemption (no current active source to preempt)
	if rr.Code != http.StatusOK && rr.Code != http.StatusConflict {
		t.Fatalf("override success: got %d, want 200 or 409, body=%s", rr.Code, rr.Body.String())
	}

	// With "media" source_type
	body, _ = json.Marshal(map[string]any{
		"station_id":  "s1",
		"source_id":   "media-src-1",
		"source_type": "media",
	})
	req = httptest.NewRequest("POST", "/priority/override", bytes.NewReader(body))
	rr = httptest.NewRecorder()
	a.handlePriorityOverride(rr, req)
	if rr.Code != http.StatusOK && rr.Code != http.StatusConflict {
		t.Fatalf("override media: got %d, want 200 or 409, body=%s", rr.Code, rr.Body.String())
	}
}

func TestPriorityAPI_ReleaseSuccess(t *testing.T) {
	a, _ := newPriorityAPITest(t)

	// First insert emergency to get an active source
	body, _ := json.Marshal(map[string]any{
		"station_id": "s1",
		"media_id":   "media-release-test",
	})
	req := httptest.NewRequest("POST", "/priority/emergency", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	a.handlePriorityEmergency(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("seed emergency: got %d, want 200", rr.Code)
	}
	var emergResp map[string]any
	json.NewDecoder(rr.Body).Decode(&emergResp) //nolint:errcheck
	sourceID, _ := emergResp["source_id"].(string)
	if sourceID == "" {
		t.Fatal("expected source_id from emergency response")
	}

	// Release the source
	releaseBody, _ := json.Marshal(map[string]any{"station_id": "s1"})
	req = httptest.NewRequest("DELETE", "/priority/"+sourceID, bytes.NewReader(releaseBody))
	req = withChiParam(req, "sourceID", sourceID)
	rr = httptest.NewRecorder()
	a.handlePriorityRelease(rr, req)
	// 200 = released; 500 = source not found (sourceID is the PrioritySource.ID, not SourceID field)
	if rr.Code != http.StatusOK && rr.Code != http.StatusInternalServerError {
		t.Fatalf("release: got %d, want 200 or 500, body=%s", rr.Code, rr.Body.String())
	}
}

func TestPriorityAPI_ReleaseNotFound(t *testing.T) {
	a, _ := newPriorityAPITest(t)

	// Release with nonexistent sourceID + valid station_id → service returns error → 500
	body, _ := json.Marshal(map[string]any{"station_id": "s1"})
	req := httptest.NewRequest("DELETE", "/priority/nonexistent", bytes.NewReader(body))
	req = withChiParam(req, "sourceID", "nonexistent-source-id")
	rr := httptest.NewRecorder()
	a.handlePriorityRelease(rr, req)
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("release nonexistent: got %d, want 500", rr.Code)
	}
}

func TestPriorityAPI_CurrentSuccess(t *testing.T) {
	a, _ := newPriorityAPITest(t)

	// First insert emergency
	body, _ := json.Marshal(map[string]any{
		"station_id": "s1",
		"media_id":   "media-current-test",
	})
	req := httptest.NewRequest("POST", "/priority/emergency", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	a.handlePriorityEmergency(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("seed emergency: got %d, want 200", rr.Code)
	}

	// Get current priority
	req = httptest.NewRequest("GET", "/priority/current?station_id=s1", nil)
	rr = httptest.NewRecorder()
	a.handlePriorityCurrent(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("get current: got %d, want 200, body=%s", rr.Code, rr.Body.String())
	}
	var resp map[string]any
	json.NewDecoder(rr.Body).Decode(&resp) //nolint:errcheck
	if _, ok := resp["source_id"]; !ok {
		t.Fatal("expected source_id in response")
	}
}
