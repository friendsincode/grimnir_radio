/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package webstream

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/events"
	"github.com/friendsincode/grimnir_radio/internal/models"
)

// newCheckerDB gives the health checker somewhere to persist status updates.
func newCheckerDB(t *testing.T, ws *models.Webstream) *gorm.DB {
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
	return db
}

func TestMountNameFromLiveURL(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"https://rlmradio.xyz/live/behind-the-woodshed", "behind-the-woodshed"},
		{"http://127.0.0.1:8080/live/doc-mike", "doc-mike"},
		{"https://rlmradio.xyz/live/doc-mike?_t=123", "doc-mike"},
		{"https://rlmradio.xyz/live/doc-mike-lq", "doc-mike-lq"},
		// Not a /live/ path, or not a URL at all.
		{"https://example.com/stream.mp3", ""},
		{"https://rlmradio.xyz/", ""},
		{"://nonsense", ""},
		{"", ""},
	} {
		if got := MountNameFromLiveURL(tc.in); got != tc.want {
			t.Errorf("MountNameFromLiveURL(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestLoopbackURL(t *testing.T) {
	local := func(names ...string) func(string) bool {
		set := map[string]bool{}
		for _, n := range names {
			set[n] = true
		}
		return func(m string) bool { return set[m] }
	}

	t.Run("rewrites one of our own mounts", func(t *testing.T) {
		got, ok := LoopbackURL("https://rlmradio.xyz/live/behind-the-woodshed", 8080, local("behind-the-woodshed"))
		if !ok {
			t.Fatal("a local mount was not rewritten")
		}
		if got != "http://127.0.0.1:8080/live/behind-the-woodshed" {
			t.Errorf("got %q", got)
		}
	})

	t.Run("honours a non-default port", func(t *testing.T) {
		got, _ := LoopbackURL("https://rlmradio.xyz/live/doc-mike", 9100, local("doc-mike"))
		if got != "http://127.0.0.1:9100/live/doc-mike" {
			t.Errorf("got %q", got)
		}
	})

	t.Run("falls back to 8080 for a bad port", func(t *testing.T) {
		got, _ := LoopbackURL("https://rlmradio.xyz/live/doc-mike", 0, local("doc-mike"))
		if got != "http://127.0.0.1:8080/live/doc-mike" {
			t.Errorf("got %q", got)
		}
	})

	// A genuine external relay must never be redirected at our own loopback:
	// that would silently swap somebody else's stream for one of ours.
	t.Run("leaves external relays alone", func(t *testing.T) {
		for _, in := range []string{
			"https://someone-else.example/live/their-show", // /live/ but not our mount
			"https://example.com/stream.mp3",               // not a /live/ path
			"icecast://example.com:8000/mount",
			"",
		} {
			got, ok := LoopbackURL(in, 8080, local("behind-the-woodshed"))
			if ok {
				t.Errorf("LoopbackURL(%q) rewrote an external relay to %q", in, got)
			}
			if got != in {
				t.Errorf("LoopbackURL(%q) = %q, want the input unchanged", in, got)
			}
		}
	})

	t.Run("a nil predicate rewrites nothing", func(t *testing.T) {
		in := "https://rlmradio.xyz/live/behind-the-woodshed"
		got, ok := LoopbackURL(in, 8080, nil)
		if ok || got != in {
			t.Errorf("got (%q, %v), want the input unchanged", got, ok)
		}
	})
}

// The health checker must probe the loopback URL, not the configured public one.
// Before this, every check for an internal relay went out through the public
// edge, so a wobble on that link could mark a healthy local mount unhealthy.
func TestHealthChecker_ChecksLoopbackNotPublicURL(t *testing.T) {
	var publicHits, loopbackHits int32

	publicSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&publicHits, 1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(make([]byte, 64*1024))
	}))
	defer publicSrv.Close()

	loopbackSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&loopbackHits, 1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(make([]byte, 64*1024))
	}))
	defer loopbackSrv.Close()

	ws := &models.Webstream{
		ID:                  "ws-1",
		URLs:                []string{publicSrv.URL + "/live/doc-mike"},
		HealthCheckMethod:   "GET",
		HealthCheckTimeout:  2 * time.Second,
		HealthCheckMinBytes: 1,
	}

	hc := &HealthChecker{
		webstreamID: ws.ID,
		db:          newCheckerDB(t, ws),
		bus:         events.NewBus(),
		logger:      zerolog.Nop(),
		httpClient:  &http.Client{},
		localURLResolver: func(configured string) string {
			if configured == publicSrv.URL+"/live/doc-mike" {
				return loopbackSrv.URL + "/live/doc-mike"
			}
			return configured
		},
	}

	hc.performHealthCheck(ws)

	if got := atomic.LoadInt32(&loopbackHits); got != 1 {
		t.Errorf("loopback hits = %d, want 1", got)
	}
	if got := atomic.LoadInt32(&publicHits); got != 0 {
		t.Errorf("public hits = %d, want 0; the check went out over the public edge", got)
	}
}

// With no resolver installed the configured URL is used unchanged, so a genuine
// external webstream is still checked where it actually lives.
func TestHealthChecker_WithoutResolverUsesConfiguredURL(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(make([]byte, 64*1024))
	}))
	defer srv.Close()

	ws := &models.Webstream{
		ID:                  "ws-2",
		URLs:                []string{srv.URL + "/live/external"},
		HealthCheckMethod:   "GET",
		HealthCheckTimeout:  2 * time.Second,
		HealthCheckMinBytes: 1,
	}
	hc := &HealthChecker{
		webstreamID: ws.ID,
		db:          newCheckerDB(t, ws),
		bus:         events.NewBus(),
		logger:      zerolog.Nop(),
		httpClient:  &http.Client{},
	}

	hc.performHealthCheck(ws)

	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Errorf("hits = %d, want 1", got)
	}
}
