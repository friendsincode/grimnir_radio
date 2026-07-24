/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package models

import (
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Webstream failover chain
// ---------------------------------------------------------------------------

func TestWebstream_HealthAndDurations(t *testing.T) {
	ws := &Webstream{HealthStatus: "healthy", HealthCheckSampleMS: 2000, HealthCheckMaxStallMS: 5000}
	if !ws.IsHealthy() {
		t.Fatal("healthy status should report healthy")
	}
	ws.HealthStatus = "degraded"
	if ws.IsHealthy() {
		t.Fatal("non-healthy status should not report healthy")
	}
	if ws.HealthCheckSampleWindow() != 2*time.Second {
		t.Fatalf("sample window = %v", ws.HealthCheckSampleWindow())
	}
	if ws.HealthCheckMaxStall() != 5*time.Second {
		t.Fatalf("max stall = %v", ws.HealthCheckMaxStall())
	}
}

func TestWebstream_URLSelection(t *testing.T) {
	empty := &Webstream{}
	if empty.GetPrimaryURL() != "" || empty.GetCurrentURL() != "" {
		t.Fatal("empty URL list should yield empty strings")
	}

	ws := &Webstream{URLs: []string{"a", "b", "c"}}
	if ws.GetPrimaryURL() != "a" {
		t.Fatalf("primary = %q", ws.GetPrimaryURL())
	}
	// Stale index is normalized back to 0.
	ws.CurrentIndex = 9
	if ws.GetCurrentURL() != "a" || ws.CurrentIndex != 0 {
		t.Fatalf("stale index not reset: url=%q idx=%d", ws.GetCurrentURL(), ws.CurrentIndex)
	}
}

func TestWebstream_NextFailoverURL(t *testing.T) {
	// Failover disabled -> no next.
	if got := (&Webstream{URLs: []string{"a", "b"}, FailoverEnabled: false}).GetNextFailoverURL(); got != "" {
		t.Fatalf("disabled failover = %q, want empty", got)
	}
	// Single URL -> no next.
	if got := (&Webstream{URLs: []string{"a"}, FailoverEnabled: true}).GetNextFailoverURL(); got != "" {
		t.Fatalf("single url = %q, want empty", got)
	}
	// Middle of chain -> next URL.
	ws := &Webstream{URLs: []string{"a", "b", "c"}, FailoverEnabled: true, CurrentIndex: 0}
	if got := ws.GetNextFailoverURL(); got != "b" {
		t.Fatalf("next = %q, want b", got)
	}
	// End of chain, auto-recover -> wraps to primary.
	ws2 := &Webstream{URLs: []string{"a", "b"}, FailoverEnabled: true, CurrentIndex: 1, AutoRecoverEnabled: true}
	if got := ws2.GetNextFailoverURL(); got != "a" {
		t.Fatalf("wrap = %q, want a", got)
	}
	// End of chain, no auto-recover -> exhausted.
	ws3 := &Webstream{URLs: []string{"a", "b"}, FailoverEnabled: true, CurrentIndex: 1}
	if got := ws3.GetNextFailoverURL(); got != "" {
		t.Fatalf("exhausted = %q, want empty", got)
	}
}

func TestWebstream_FailoverToNextAndReset(t *testing.T) {
	ws := &Webstream{URLs: []string{"a", "b", "c"}, FailoverEnabled: true}
	if !ws.FailoverToNext() || ws.CurrentIndex != 1 || ws.CurrentURL != "b" {
		t.Fatalf("first failover: idx=%d url=%q", ws.CurrentIndex, ws.CurrentURL)
	}
	ws.FailoverToNext() // -> c (idx 2)
	// At the end without auto-recover, advancing fails and leaves state put.
	if ws.FailoverToNext() {
		t.Fatal("should not advance past the end without auto-recover")
	}
	if ws.CurrentIndex != 2 {
		t.Fatalf("index moved unexpectedly to %d", ws.CurrentIndex)
	}

	// Auto-recover wraps back to primary.
	wrap := &Webstream{URLs: []string{"a", "b"}, FailoverEnabled: true, AutoRecoverEnabled: true, CurrentIndex: 1}
	if !wrap.FailoverToNext() || wrap.CurrentIndex != 0 {
		t.Fatalf("auto-recover wrap failed: idx=%d", wrap.CurrentIndex)
	}

	// Disabled failover never advances.
	if (&Webstream{URLs: []string{"a", "b"}}).FailoverToNext() {
		t.Fatal("disabled failover should not advance")
	}

	ws.ResetToPrimary()
	if ws.CurrentIndex != 0 || ws.CurrentURL != "a" {
		t.Fatalf("reset: idx=%d url=%q", ws.CurrentIndex, ws.CurrentURL)
	}
}

func TestWebstream_HealthMarkers(t *testing.T) {
	ws := &Webstream{}
	ws.MarkHealthy()
	if ws.HealthStatus != "healthy" || ws.LastHealthCheck == nil {
		t.Fatalf("mark healthy: %+v", ws)
	}
	ws.MarkUnhealthy()
	if ws.HealthStatus != "unhealthy" {
		t.Fatal("mark unhealthy")
	}
	ws.MarkDegraded()
	if ws.HealthStatus != "degraded" {
		t.Fatal("mark degraded")
	}
}

// ---------------------------------------------------------------------------
// Other predicate/helper methods
// ---------------------------------------------------------------------------

func TestAPIKeyValidity(t *testing.T) {
	future := time.Now().Add(time.Hour)
	past := time.Now().Add(-time.Hour)

	valid := &APIKey{ExpiresAt: future}
	if valid.IsExpired() || valid.IsRevoked() || !valid.IsValid() {
		t.Fatal("fresh key should be valid")
	}
	expired := &APIKey{ExpiresAt: past}
	if !expired.IsExpired() || expired.IsValid() {
		t.Fatal("past-expiry key should be expired and invalid")
	}
	revoked := &APIKey{ExpiresAt: future, RevokedAt: &past}
	if !revoked.IsRevoked() || revoked.IsValid() {
		t.Fatal("revoked key should be invalid")
	}
}

func TestRecordingHelpers(t *testing.T) {
	active := &Recording{Status: RecordingStatusActive, DurationMs: 90000}
	if !active.IsActive() || active.IsComplete() {
		t.Fatal("active recording state")
	}
	if active.Duration() != 90*time.Second {
		t.Fatalf("duration = %v", active.Duration())
	}
	done := &Recording{Status: RecordingStatusComplete}
	if !done.IsComplete() || done.IsActive() {
		t.Fatal("complete recording state")
	}
}

func TestLiveSessionHelpers(t *testing.T) {
	ls := &LiveSession{Active: true, ConnectedAt: time.Now().Add(-time.Minute)}
	if !ls.IsActive() {
		t.Fatal("active session")
	}
	if ls.Duration() < time.Minute {
		t.Fatalf("live duration too small: %v", ls.Duration())
	}
	ls.Disconnect()
	if ls.IsActive() || ls.DisconnectedAt == nil {
		t.Fatal("disconnect should clear active and stamp DisconnectedAt")
	}
	// After disconnect, duration is fixed between connect and disconnect.
	if ls.Duration() <= 0 {
		t.Fatalf("post-disconnect duration = %v", ls.Duration())
	}
}

func TestPrioritySourceHelpers(t *testing.T) {
	ps := &PrioritySource{Active: true, Priority: PriorityEmergency}
	if !ps.IsActive() || !ps.IsEmergency() {
		t.Fatal("active emergency source")
	}
	ps.Deactivate()
	if ps.IsActive() || ps.DeactivatedAt == nil {
		t.Fatal("deactivate should clear active and stamp DeactivatedAt")
	}
	live := &PrioritySource{SourceType: SourceTypeLive}
	if !live.IsLive() {
		t.Fatal("live source-type should report live")
	}
}

func TestLandingPageHelpers(t *testing.T) {
	platform := &LandingPage{StationID: nil}
	if !platform.IsPlatformPage() {
		t.Fatal("nil station is platform page")
	}
	if platform.LogoURL() != "/landing-assets/by-type/logo?platform=true" {
		t.Fatalf("platform logo url = %q", platform.LogoURL())
	}

	st := "st1"
	station := &LandingPage{StationID: &st, DraftConfig: map[string]any{"x": 1}}
	if station.IsPlatformPage() {
		t.Fatal("station page should not be platform")
	}
	if !station.HasDraft() {
		t.Fatal("non-empty draft should report HasDraft")
	}
	if station.LogoURL() != "/landing-assets/by-type/logo?station_id=st1" {
		t.Fatalf("station logo url = %q", station.LogoURL())
	}

	empty := "" // empty station id also counts as platform
	if !(&LandingPage{StationID: &empty}).IsPlatformPage() {
		t.Fatal("empty station id should count as platform page")
	}
}

func TestShowInstanceHelpers(t *testing.T) {
	cancelled := &ShowInstance{Status: ShowInstanceCancelled}
	if !cancelled.IsCancelled() {
		t.Fatal("cancelled status")
	}
	exc := &ShowInstance{ExceptionType: ShowExceptionCancelled}
	if !exc.IsCancelled() || !exc.IsException() {
		t.Fatal("exception-cancelled instance")
	}
	regular := &ShowInstance{Status: ShowInstanceScheduled}
	if regular.IsCancelled() || regular.IsException() {
		t.Fatal("scheduled instance is neither cancelled nor an exception")
	}
}
