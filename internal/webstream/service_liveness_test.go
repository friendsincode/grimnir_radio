/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package webstream

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/events"
	"github.com/friendsincode/grimnir_radio/internal/models"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

// Test-tuned liveness parameters keep the suite fast (sub-second) while still
// exercising the sustained-throughput logic.
const (
	testSampleWindow = 800 * time.Millisecond
	testMinBytes     = 4096
	testMaxStall     = 300 * time.Millisecond
	testTimeout      = 3 * time.Second
)

func newLivenessService(t *testing.T) *Service {
	t.Helper()
	db := setupTestDB(t)
	svc := &Service{
		db:     db,
		bus:    events.NewBus(),
		logger: zerolog.Nop(),
	}
	return svc
}

// flushWriter writes n bytes and flushes, returning false if the client is gone.
func flushWrite(w http.ResponseWriter, n int) bool {
	buf := make([]byte, n)
	if _, err := w.Write(buf); err != nil {
		return false
	}
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
	return true
}

func TestCheckURL_HealthyContinuous(t *testing.T) {
	svc := newLivenessService(t)

	// ~20KB/s: write 2KB every 100ms for the duration of the window.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/mpeg")
		w.WriteHeader(http.StatusOK)
		for i := 0; i < 20; i++ {
			if !flushWrite(w, 2048) {
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
	}))
	defer srv.Close()

	err := svc.checkURL(srv.URL, "GET", testTimeout, testSampleWindow, testMinBytes, testMaxStall)
	if err != nil {
		t.Fatalf("expected healthy, got error: %v", err)
	}
}

func TestCheckURL_BurstThenSilence(t *testing.T) {
	svc := newLivenessService(t)

	// The #73 repro: send 32KB immediately, then hold the connection open
	// without sending anything more so the stall gap is detected.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		flushWrite(w, 32000)
		// Block past the sample window so the reader observes the stall.
		time.Sleep(2 * time.Second)
	}))
	defer srv.Close()

	err := svc.checkURL(srv.URL, "GET", testTimeout, testSampleWindow, testMinBytes, testMaxStall)
	if err == nil {
		t.Fatal("expected stall error, got nil (healthy)")
	}
}

func TestCheckURL_ImmediateEOF(t *testing.T) {
	svc := newLivenessService(t)

	// 200 with a tiny body, then the handler returns (EOF) with < minBytes.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		flushWrite(w, 100)
	}))
	defer srv.Close()

	err := svc.checkURL(srv.URL, "GET", testTimeout, testSampleWindow, testMinBytes, testMaxStall)
	if err == nil {
		t.Fatal("expected insufficient-data error, got nil (healthy)")
	}
}

func TestCheckURL_SlowSteadyTrickle(t *testing.T) {
	svc := newLivenessService(t)

	// Small writes every 100ms (< maxStall) totaling >= minBytes.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		for i := 0; i < 8; i++ {
			if !flushWrite(w, 700) {
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
	}))
	defer srv.Close()

	err := svc.checkURL(srv.URL, "GET", testTimeout, testSampleWindow, testMinBytes, testMaxStall)
	if err != nil {
		t.Fatalf("expected healthy trickle, got error: %v", err)
	}
}

func TestCheckURL_404(t *testing.T) {
	svc := newLivenessService(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()

	err := svc.checkURL(srv.URL, "GET", testTimeout, testSampleWindow, testMinBytes, testMaxStall)
	if err == nil {
		t.Fatal("expected error for HTTP 404, got nil")
	}
}

func TestCheckURL_ConnectionRefused(t *testing.T) {
	svc := newLivenessService(t)

	// Reserve a port then close, so the connection is refused.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	badURL := srv.URL
	srv.Close()

	err := svc.checkURL(badURL, "GET", testTimeout, testSampleWindow, testMinBytes, testMaxStall)
	if err == nil {
		t.Fatal("expected error for refused connection, got nil")
	}
}

func TestCheckURL_HEADStillReadsBody(t *testing.T) {
	svc := newLivenessService(t)

	// Even when the configured method is HEAD, the liveness check must issue a
	// GET so it can read the body. A HEAD server here would return no body and
	// fail; we assert the healthy GET path is used instead.
	gotMethod := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		w.WriteHeader(http.StatusOK)
		for i := 0; i < 20; i++ {
			if !flushWrite(w, 2048) {
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
	}))
	defer srv.Close()

	err := svc.checkURL(srv.URL, "HEAD", testTimeout, testSampleWindow, testMinBytes, testMaxStall)
	if err != nil {
		t.Fatalf("expected healthy (GET issued despite HEAD), got error: %v", err)
	}
	if gotMethod != http.MethodGet {
		t.Fatalf("expected liveness check to issue GET, server saw %q", gotMethod)
	}
}

func TestCreateWebstream_PopulatesLivenessDefaults(t *testing.T) {
	db := setupTestDB(t)
	svc := NewService(db, events.NewBus(), zerolog.Nop())
	defer svc.Shutdown()

	ws := &models.Webstream{
		ID:                 uuid.NewString(),
		StationID:          uuid.NewString(),
		Name:               "Defaults Stream",
		URLs:               []string{"http://example.com/stream.mp3"},
		HealthCheckEnabled: false,
		PreflightCheck:     false,
	}

	if err := svc.CreateWebstream(context.Background(), ws); err != nil {
		t.Fatalf("CreateWebstream() failed: %v", err)
	}

	if ws.HealthCheckSampleMS != 4000 {
		t.Errorf("HealthCheckSampleMS: expected 4000, got %d", ws.HealthCheckSampleMS)
	}
	if ws.HealthCheckMinBytes != 16384 {
		t.Errorf("HealthCheckMinBytes: expected 16384, got %d", ws.HealthCheckMinBytes)
	}
	if ws.HealthCheckMaxStallMS != 2000 {
		t.Errorf("HealthCheckMaxStallMS: expected 2000, got %d", ws.HealthCheckMaxStallMS)
	}
}
