/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package webstream

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/events"
	"github.com/friendsincode/grimnir_radio/internal/models"
)

// ---------------------------------------------------------------------------
// pure helpers
// ---------------------------------------------------------------------------

func TestParseStreamTitle(t *testing.T) {
	title, artist := parseStreamTitle("StreamTitle='Depeche Mode - Enjoy the Silence';StreamUrl='x';")
	if artist != "Depeche Mode" || title != "Enjoy the Silence" {
		t.Fatalf("artist=%q title=%q", artist, title)
	}
	// Title-only (no " - " separator).
	if ti, ar := parseStreamTitle("StreamTitle='Station ID';"); ti != "Station ID" || ar != "" {
		t.Fatalf("title-only: ti=%q ar=%q", ti, ar)
	}
	// Empty StreamTitle.
	if ti, ar := parseStreamTitle("StreamTitle='';"); ti != "" || ar != "" {
		t.Fatalf("empty: ti=%q ar=%q", ti, ar)
	}
	// No StreamTitle prefix at all.
	if ti, ar := parseStreamTitle("SomethingElse='x';"); ti != "" || ar != "" {
		t.Fatalf("no prefix: ti=%q ar=%q", ti, ar)
	}
	// Unterminated quote falls back to the remainder as title.
	if ti, _ := parseStreamTitle("StreamTitle='Unterminated"); ti != "Unterminated" {
		t.Fatalf("unterminated: ti=%q", ti)
	}
}

// ---------------------------------------------------------------------------
// failover state machine (deterministic, no live network)
// ---------------------------------------------------------------------------

func newChecker(t *testing.T, ws *models.Webstream) (*HealthChecker, *gorm.DB, *events.Bus) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.Webstream{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := db.Create(ws).Error; err != nil {
		t.Fatalf("seed webstream: %v", err)
	}
	bus := events.NewBus()
	return NewHealthChecker(ws.ID, db, bus, zerolog.Nop()), db, bus
}

func TestHandleFailedCheck_EscalatesThenHoldsInGrace(t *testing.T) {
	// Large grace period so the third failure records unhealthy but does NOT
	// actually fail over (the exact bug class we want pinned down).
	ws := &models.Webstream{
		ID: "ws1", StationID: "st1", Name: "Relay",
		URLs:            []string{"http://a/", "http://b/"},
		FailoverEnabled: true, FailoverGraceMs: 600000,
	}
	hc, db, _ := newChecker(t, ws)

	hc.handleFailedCheck(ws, fmt.Errorf("boom"))
	if ws.HealthStatus != "degraded" {
		t.Fatalf("after 1 fail: status=%q, want degraded", ws.HealthStatus)
	}
	hc.handleFailedCheck(ws, fmt.Errorf("boom"))
	if ws.HealthStatus != "unhealthy" {
		t.Fatalf("after 2 fails: status=%q, want unhealthy", ws.HealthStatus)
	}
	hc.handleFailedCheck(ws, fmt.Errorf("boom")) // hits threshold, but grace holds it
	if ws.HealthStatus != "unhealthy" {
		t.Fatalf("after 3 fails in grace: status=%q, want unhealthy", ws.HealthStatus)
	}
	// No failover occurred: still on the primary URL.
	if ws.CurrentIndex != 0 {
		t.Fatalf("grace period should defer failover, but index moved to %d", ws.CurrentIndex)
	}
	if hc.failoverEligibleAt.IsZero() {
		t.Fatal("failoverEligibleAt should be armed once the threshold is reached")
	}

	// Persisted state matches.
	var reloaded models.Webstream
	db.First(&reloaded, "id = ?", "ws1")
	if reloaded.HealthStatus != "unhealthy" || reloaded.CurrentIndex != 0 {
		t.Fatalf("persisted state wrong: %+v", reloaded)
	}
}

func TestHandleSuccessfulCheck_ResetsState(t *testing.T) {
	ws := &models.Webstream{ID: "ws1", StationID: "st1", URLs: []string{"http://a/"}, HealthStatus: "degraded"}
	hc, _, bus := newChecker(t, ws)
	hc.consecutiveFails = 2
	hc.failoverEligibleAt = time.Now().Add(time.Hour)

	sub := bus.Subscribe(events.EventWebstreamHealth)
	hc.handleSuccessfulCheck(ws)

	if ws.HealthStatus != "healthy" {
		t.Fatalf("status = %q, want healthy", ws.HealthStatus)
	}
	if hc.consecutiveFails != 0 || !hc.failoverEligibleAt.IsZero() {
		t.Fatalf("counters not reset: fails=%d eligible=%v", hc.consecutiveFails, hc.failoverEligibleAt)
	}
	select {
	case p := <-sub:
		if p["status"] != "healthy" {
			t.Fatalf("health event status = %v", p["status"])
		}
	default:
		t.Fatal("expected a health event on recovery")
	}
}

func TestTriggerFailover_DisabledMarksUnhealthy(t *testing.T) {
	ws := &models.Webstream{ID: "ws1", StationID: "st1", URLs: []string{"http://a/", "http://b/"}, FailoverEnabled: false}
	hc, _, _ := newChecker(t, ws)

	hc.triggerFailover(ws)
	if ws.HealthStatus != "unhealthy" {
		t.Fatalf("disabled failover status = %q, want unhealthy", ws.HealthStatus)
	}
	if ws.CurrentIndex != 0 {
		t.Fatal("disabled failover must not advance the URL index")
	}
}

func TestTriggerFailover_NoNextURLMarksUnhealthy(t *testing.T) {
	// Failover enabled but only one URL => no next URL to move to.
	ws := &models.Webstream{ID: "ws1", StationID: "st1", URLs: []string{"http://only/"}, FailoverEnabled: true}
	hc, _, _ := newChecker(t, ws)

	hc.triggerFailover(ws)
	if ws.HealthStatus != "unhealthy" {
		t.Fatalf("no-next-url status = %q, want unhealthy", ws.HealthStatus)
	}
}

func TestHandleFailedThenSuccess_Recovers(t *testing.T) {
	ws := &models.Webstream{ID: "ws1", StationID: "st1", URLs: []string{"http://a/"}, FailoverEnabled: true, FailoverGraceMs: 600000}
	hc, _, _ := newChecker(t, ws)
	hc.handleFailedCheck(ws, fmt.Errorf("x"))
	hc.handleFailedCheck(ws, fmt.Errorf("x"))
	if hc.consecutiveFails != 2 {
		t.Fatalf("consecutiveFails = %d, want 2", hc.consecutiveFails)
	}
	hc.handleSuccessfulCheck(ws)
	if hc.consecutiveFails != 0 || ws.HealthStatus != "healthy" {
		t.Fatalf("recovery failed: fails=%d status=%q", hc.consecutiveFails, ws.HealthStatus)
	}
}

// ---------------------------------------------------------------------------
// ICY metadata parsing over httptest
// ---------------------------------------------------------------------------

func TestParseICYMetadata_HeaderFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// No icy-metaint => the parser falls back to the icy-name header.
		w.Header().Set("icy-name", "Jazz FM")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	p := &ICYPoller{}
	title, artist, err := p.parseICYMetadata(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if title != "Jazz FM" || artist != "" {
		t.Fatalf("header fallback: title=%q artist=%q", title, artist)
	}
}

func TestParseICYMetadata_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	p := &ICYPoller{}
	if _, _, err := p.parseICYMetadata(context.Background(), srv.URL); err == nil {
		t.Fatal("expected an error for HTTP 404")
	}
}

func TestParseICYMetadata_InlineStreamTitle(t *testing.T) {
	const metaInt = 16
	meta := "StreamTitle='Depeche Mode - Enjoy the Silence';"
	blocks := (len(meta) + 15) / 16
	padded := make([]byte, blocks*16)
	copy(padded, meta)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("icy-metaint", fmt.Sprintf("%d", metaInt))
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		w.Write(make([]byte, metaInt)) // one audio block
		w.Write([]byte{byte(blocks)})  // metadata length in 16-byte units
		w.Write(padded)                // the metadata block
		if flusher != nil {
			flusher.Flush()
		}
	}))
	defer srv.Close()

	p := &ICYPoller{}
	title, artist, err := p.parseICYMetadata(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("parse inline: %v", err)
	}
	if artist != "Depeche Mode" || title != "Enjoy the Silence" {
		t.Fatalf("inline parse: artist=%q title=%q", artist, title)
	}
}
