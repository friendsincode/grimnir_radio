/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package webstream

import (
	"errors"
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

// An idle local mount must be skipped entirely: no probe, no DB write, no event.
// This is the monitoring blind spot. Most webstreams here relay another mount on
// the same box, those mounts only carry audio while their show is scheduled, and
// probing one in between looks identical to a dead upstream. On prod that left 56
// of 66 webstreams permanently unhealthy, so the signal could not report a real
// outage.
func TestHealthChecker_SkipsIdleLocalMount(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ws := &models.Webstream{
		ID:                  "ws-idle",
		URLs:                []string{srv.URL + "/live/night-show"},
		HealthStatus:        "healthy",
		HealthCheckMethod:   "GET",
		HealthCheckTimeout:  time.Second,
		HealthCheckMinBytes: 1,
	}
	hc := &HealthChecker{
		webstreamID:    ws.ID,
		db:             newCheckerDB(t, ws),
		bus:            events.NewBus(),
		logger:         zerolog.Nop(),
		httpClient:     &http.Client{},
		localMountIdle: func(string) bool { return true },
	}

	hc.performHealthCheck(ws)

	if got := atomic.LoadInt32(&hits); got != 0 {
		t.Errorf("probe hits = %d, want 0; an idle mount must not be probed", got)
	}
	if hc.consecutiveFails != 0 {
		t.Errorf("consecutiveFails = %d, want 0", hc.consecutiveFails)
	}
	if ws.HealthStatus != "healthy" {
		t.Errorf("HealthStatus = %q, want it left alone", ws.HealthStatus)
	}
}

// A local mount that IS being fed still gets checked, so a genuine failure on an
// on-air stream is still caught.
func TestHealthChecker_ChecksActiveLocalMount(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(make([]byte, 64*1024))
	}))
	defer srv.Close()

	ws := &models.Webstream{
		ID:                  "ws-live",
		URLs:                []string{srv.URL + "/live/on-air"},
		HealthCheckMethod:   "GET",
		HealthCheckTimeout:  2 * time.Second,
		HealthCheckMinBytes: 1,
	}
	hc := &HealthChecker{
		webstreamID:    ws.ID,
		db:             newCheckerDB(t, ws),
		bus:            events.NewBus(),
		logger:         zerolog.Nop(),
		httpClient:     &http.Client{},
		localMountIdle: func(string) bool { return false },
	}

	hc.performHealthCheck(ws)

	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Errorf("probe hits = %d, want 1; an on-air mount must still be checked", got)
	}
}

// Failover machinery must not run when there is nowhere to go. Every webstream on
// this deployment has exactly one URL, so the threshold, grace timer and
// "failover threshold reached" log were firing thousands of times an hour toward
// a switch that could never happen.
func TestCanFailover(t *testing.T) {
	for _, tc := range []struct {
		name string
		ws   models.Webstream
		want bool
	}{
		{"single url, failover enabled", models.Webstream{FailoverEnabled: true, URLs: []string{"a"}}, false},
		{"two urls, failover enabled", models.Webstream{FailoverEnabled: true, URLs: []string{"a", "b"}}, true},
		{"two urls, failover disabled", models.Webstream{FailoverEnabled: false, URLs: []string{"a", "b"}}, false},
		{"no urls", models.Webstream{FailoverEnabled: true, URLs: nil}, false},
	} {
		if got := canFailover(&tc.ws); got != tc.want {
			t.Errorf("%s: canFailover = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// recordUnhealthy writes and publishes only on a status change. Before this, every
// failed check re-saved the same row and re-published the same event.
func TestRecordUnhealthy_OnlyWritesOnChange(t *testing.T) {
	ws := &models.Webstream{ID: "ws-1", URLs: []string{"http://x/live/a"}, HealthStatus: "healthy"}
	db := newCheckerDB(t, ws)
	bus := events.NewBus()
	sub := bus.Subscribe(events.EventWebstreamHealth)
	defer bus.Unsubscribe(events.EventWebstreamHealth, sub)

	hc := &HealthChecker{webstreamID: ws.ID, db: db, bus: bus, logger: zerolog.Nop()}

	hc.recordUnhealthy(ws)
	if ws.HealthStatus != "unhealthy" {
		t.Fatalf("HealthStatus = %q, want unhealthy", ws.HealthStatus)
	}
	select {
	case <-sub:
	case <-time.After(time.Second):
		t.Fatal("first transition must publish an event")
	}

	// Already unhealthy: no second event.
	hc.recordUnhealthy(ws)
	select {
	case ev := <-sub:
		t.Fatalf("repeat call published another event: %+v", ev)
	case <-time.After(200 * time.Millisecond):
	}
}

// The call site, not just the predicate: repeated failures on a single-URL stream
// must never start the failover grace timer. On prod that timer logged
// "failover threshold reached" 3,433 times an hour toward a switch that could
// never happen, because every webstream carries exactly one URL and
// GetNextFailoverURL returns empty below two.
func TestHandleFailedCheck_NoFailoverMachineryWithoutSecondURL(t *testing.T) {
	ws := &models.Webstream{
		ID:              "ws-single",
		URLs:            []string{"http://x/live/a"},
		FailoverEnabled: true,
		FailoverGraceMs: 5000,
		HealthStatus:    "healthy",
	}
	hc := &HealthChecker{
		webstreamID: ws.ID,
		db:          newCheckerDB(t, ws),
		bus:         events.NewBus(),
		logger:      zerolog.Nop(),
	}

	for i := 0; i < 5; i++ {
		hc.handleFailedCheck(ws, errors.New("stream stalled"))
	}

	if !hc.failoverEligibleAt.IsZero() {
		t.Errorf("grace timer armed at %v; there is no second URL to fail over to", hc.failoverEligibleAt)
	}
	if ws.HealthStatus != "unhealthy" {
		t.Errorf("HealthStatus = %q, want unhealthy still recorded", ws.HealthStatus)
	}
}

// With a real second URL the machinery must still arm, so this does not quietly
// disable failover for streams that actually have a backup.
func TestHandleFailedCheck_ArmsFailoverWithSecondURL(t *testing.T) {
	ws := &models.Webstream{
		ID:              "ws-dual",
		URLs:            []string{"http://x/live/a", "http://y/live/b"},
		FailoverEnabled: true,
		FailoverGraceMs: 60000,
		HealthStatus:    "healthy",
	}
	hc := &HealthChecker{
		webstreamID: ws.ID,
		db:          newCheckerDB(t, ws),
		bus:         events.NewBus(),
		logger:      zerolog.Nop(),
	}

	for i := 0; i < 4; i++ {
		hc.handleFailedCheck(ws, errors.New("stream stalled"))
	}

	if hc.failoverEligibleAt.IsZero() {
		t.Error("grace timer must arm when a second URL exists")
	}
}
