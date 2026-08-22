/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package server

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

// TestHandleHealthz guards #86: /healthz is the liveness probe. Without leader
// election configured it must answer 200 with a JSON {"status":"ok"} body and no
// leader field, so a load balancer or orchestrator health check gets a clean,
// parseable signal. The leader field only appears when a leader-aware scheduler
// is wired in.
func TestHandleHealthz(t *testing.T) {
	s := &Server{} // no leaderAwareScheduler

	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/healthz", nil)
	s.handleHealthz(rr, req)

	if rr.Code != 200 {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	var body map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not valid JSON: %v (%q)", err, rr.Body.String())
	}
	if body["status"] != "ok" {
		t.Errorf("status field = %v, want ok", body["status"])
	}
	if _, ok := body["leader"]; ok {
		t.Errorf("leader field must be absent without leader election, got %v", body["leader"])
	}
}
