/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package alertbridge

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/friendsincode/grimnir_radio/internal/notify"
)

// recordingNotifier captures every Tier1/Tier2 call so the tests can assert
// the bridge routed each alert to the right tier.
type recordingNotifier struct {
	mu    sync.Mutex
	calls []notify.FakeCall
	err   error
}

func (r *recordingNotifier) Tier1(_ context.Context, title, body string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, notify.FakeCall{Tier: 1, Title: title, Body: body})
	return r.err
}

func (r *recordingNotifier) Tier2(_ context.Context, title, body string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, notify.FakeCall{Tier: 2, Title: title, Body: body})
	return r.err
}

func (r *recordingNotifier) snapshot() []notify.FakeCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]notify.FakeCall, len(r.calls))
	copy(out, r.calls)
	return out
}

// testPayload mirrors the bridge's internal payload struct for the test
// fixtures. Keeping it local avoids exporting the type just for tests.
type testPayload struct {
	Version  string      `json:"version"`
	Status   string      `json:"status"`
	Receiver string      `json:"receiver"`
	Alerts   []testAlert `json:"alerts"`
}

type testAlert struct {
	Status      string            `json:"status"`
	Labels      map[string]string `json:"labels"`
	Annotations map[string]string `json:"annotations"`
	StartsAt    string            `json:"startsAt"`
}

func postJSON(t *testing.T, srv *httptest.Server, body any) *http.Response {
	t.Helper()
	buf, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	resp, err := http.Post(srv.URL+"/webhook", "application/json", bytes.NewReader(buf))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	return resp
}

func TestBridge_RoutesNotifyToTier1(t *testing.T) {
	rn := &recordingNotifier{}
	srv := httptest.NewServer(NewHandler(rn))
	defer srv.Close()

	payload := testPayload{
		Version: "4", Status: "firing", Receiver: "notify-chat",
		Alerts: []testAlert{{
			Status: "firing",
			Labels: map[string]string{
				"severity":  "notify",
				"alertname": "PostgresReplicationLagWarn",
			},
			Annotations: map[string]string{
				"summary":     "Postgres replication lag > 5s",
				"description": "Lag = 6s for over 2 minutes.",
			},
		}},
	}
	resp := postJSON(t, srv, payload)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("got %d, want 204", resp.StatusCode)
	}

	calls := rn.snapshot()
	if len(calls) != 1 {
		t.Fatalf("got %d calls, want 1", len(calls))
	}
	if calls[0].Tier != 1 {
		t.Errorf("got Tier %d, want 1", calls[0].Tier)
	}
	if !strings.Contains(calls[0].Title, "PostgresReplicationLagWarn") {
		t.Errorf("title %q missing alertname", calls[0].Title)
	}
	if !strings.Contains(calls[0].Body, "Lag = 6s") {
		t.Errorf("body %q missing description", calls[0].Body)
	}
}

func TestBridge_RoutesPageToTier2(t *testing.T) {
	rn := &recordingNotifier{}
	srv := httptest.NewServer(NewHandler(rn))
	defer srv.Close()

	payload := testPayload{
		Version: "4", Status: "firing", Receiver: "page-ntfy",
		Alerts: []testAlert{{
			Status: "firing",
			Labels: map[string]string{
				"severity":  "page",
				"alertname": "VrrpSplitBrain",
				"vip":       "listener",
			},
			Annotations: map[string]string{
				"summary": "VRRP split brain on listener (2 holders)",
			},
		}},
	}
	resp := postJSON(t, srv, payload)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("got %d, want 204", resp.StatusCode)
	}

	calls := rn.snapshot()
	if len(calls) != 1 || calls[0].Tier != 2 {
		t.Fatalf("got %#v, want one Tier2 call", calls)
	}
}

func TestBridge_PageAndRollbackRoutesTier2(t *testing.T) {
	// page-and-rollback is a tier-3 concept; auto-rollback fires through a
	// separate webhook on the deploy daemon (Chunk 8). The ntfy side of the
	// bridge still pages an operator, so it lands as Tier2 here.
	rn := &recordingNotifier{}
	srv := httptest.NewServer(NewHandler(rn))
	defer srv.Close()

	payload := testPayload{
		Version: "4", Status: "firing", Receiver: "page-and-rollback",
		Alerts: []testAlert{{
			Status: "firing",
			Labels: map[string]string{
				"severity":  "page-and-rollback",
				"alertname": "ListenerReconnectSpike",
			},
			Annotations: map[string]string{
				"summary": "Listener reconnect rate spike during deploy soak",
			},
		}},
	}
	resp := postJSON(t, srv, payload)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("got %d, want 204", resp.StatusCode)
	}

	calls := rn.snapshot()
	if len(calls) != 1 || calls[0].Tier != 2 {
		t.Fatalf("got %#v, want one Tier2 call", calls)
	}
}

func TestBridge_FanoutsMultipleAlerts(t *testing.T) {
	rn := &recordingNotifier{}
	srv := httptest.NewServer(NewHandler(rn))
	defer srv.Close()

	payload := testPayload{
		Version: "4", Status: "firing", Receiver: "page-ntfy",
		Alerts: []testAlert{
			{Status: "firing", Labels: map[string]string{"severity": "page", "alertname": "A"}},
			{Status: "firing", Labels: map[string]string{"severity": "notify", "alertname": "B"}},
			{Status: "firing", Labels: map[string]string{"severity": "page", "alertname": "C"}},
		},
	}
	resp := postJSON(t, srv, payload)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("got %d, want 204", resp.StatusCode)
	}

	calls := rn.snapshot()
	if len(calls) != 3 {
		t.Fatalf("got %d calls, want 3", len(calls))
	}
	gotTiers := []int{calls[0].Tier, calls[1].Tier, calls[2].Tier}
	wantTiers := []int{2, 1, 2}
	for i := range gotTiers {
		if gotTiers[i] != wantTiers[i] {
			t.Errorf("call %d: tier=%d want %d", i, gotTiers[i], wantTiers[i])
		}
	}
}

func TestBridge_UnknownSeverityFallsToTier2(t *testing.T) {
	// Defensive default: if Alertmanager ever sends a label we don't know
	// about (operator typo, new tier added upstream first), page the
	// operator rather than silently dropping the alert.
	rn := &recordingNotifier{}
	srv := httptest.NewServer(NewHandler(rn))
	defer srv.Close()

	payload := testPayload{
		Version: "4", Status: "firing",
		Alerts: []testAlert{{
			Status: "firing",
			Labels: map[string]string{"severity": "mystery-tier", "alertname": "X"},
		}},
	}
	resp := postJSON(t, srv, payload)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("got %d, want 204", resp.StatusCode)
	}
	calls := rn.snapshot()
	if len(calls) != 1 || calls[0].Tier != 2 {
		t.Fatalf("got %#v, want one Tier2 fallback call", calls)
	}
}

func TestBridge_ResolvedAlertsPrefixTitle(t *testing.T) {
	rn := &recordingNotifier{}
	srv := httptest.NewServer(NewHandler(rn))
	defer srv.Close()

	payload := testPayload{
		Version: "4", Status: "resolved",
		Alerts: []testAlert{{
			Status: "resolved",
			Labels: map[string]string{"severity": "page", "alertname": "VrrpSplitBrain"},
		}},
	}
	resp := postJSON(t, srv, payload)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("got %d, want 204", resp.StatusCode)
	}
	calls := rn.snapshot()
	if len(calls) != 1 {
		t.Fatalf("got %d calls, want 1", len(calls))
	}
	if !strings.HasPrefix(calls[0].Title, "[RESOLVED]") {
		t.Errorf("title %q missing [RESOLVED] prefix", calls[0].Title)
	}
}

func TestBridge_RejectsNonPOST(t *testing.T) {
	srv := httptest.NewServer(NewHandler(&recordingNotifier{}))
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/webhook")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("got %d, want 405", resp.StatusCode)
	}
}

func TestBridge_RejectsMalformedJSON(t *testing.T) {
	srv := httptest.NewServer(NewHandler(&recordingNotifier{}))
	defer srv.Close()
	resp, err := http.Post(srv.URL+"/webhook", "application/json", strings.NewReader("not json"))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("got %d, want 400", resp.StatusCode)
	}
}

func TestBridge_HealthzReturns200(t *testing.T) {
	srv := httptest.NewServer(NewHandler(&recordingNotifier{}))
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/healthz")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("got %d, want 200", resp.StatusCode)
	}
}
