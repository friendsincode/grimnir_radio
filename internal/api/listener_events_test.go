/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/audit"
	"github.com/friendsincode/grimnir_radio/internal/events"
	"github.com/friendsincode/grimnir_radio/internal/executor"
	"github.com/friendsincode/grimnir_radio/internal/integrity"
	"github.com/friendsincode/grimnir_radio/internal/models"
	"github.com/friendsincode/grimnir_radio/internal/priority"
)

func newListenerEventsAPITest(t *testing.T) (*API, *gorm.DB) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "test.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(
		&models.Station{},
		&models.ListenerEvent{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	return &API{
		db:                   db,
		logger:               zerolog.Nop(),
		listenerEventLimiter: newListenerEventRateLimiter(),
	}, db
}

func postListenerEvent(t *testing.T, a *API, body any, remoteAddr string) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(body); err != nil {
		t.Fatalf("encode body: %v", err)
	}
	req := httptest.NewRequest("POST", "/api/v1/listener-events", &buf)
	req.Header.Set("Content-Type", "application/json")
	if remoteAddr != "" {
		req.RemoteAddr = remoteAddr
	}
	rr := httptest.NewRecorder()
	a.handleListenerEventCreate(rr, req)
	return rr
}

func TestListenerEvent_Create_HappyPath(t *testing.T) {
	a, db := newListenerEventsAPITest(t)
	station := models.Station{ID: "s1", Name: "S", Active: true, Public: true, Approved: true}
	if err := db.Create(&station).Error; err != nil {
		t.Fatalf("create station: %v", err)
	}

	duration := 1234
	body := listenerEventRequest{
		EventType:   "reconnect",
		StationID:   "s1",
		StreamLabel: "HQ",
		DurationMs:  &duration,
	}
	rr := postListenerEvent(t, a, body, "10.0.0.1:54321")

	if rr.Code != http.StatusNoContent {
		t.Fatalf("got %d, want 204 (body=%s)", rr.Code, rr.Body.String())
	}

	var count int64
	db.Model(&models.ListenerEvent{}).Count(&count)
	if count != 1 {
		t.Fatalf("expected 1 row, got %d", count)
	}

	var row models.ListenerEvent
	if err := db.First(&row).Error; err != nil {
		t.Fatalf("read row: %v", err)
	}
	if row.EventType != "reconnect" || row.StationID != "s1" || row.StreamLabel != "HQ" {
		t.Fatalf("row fields wrong: %+v", row)
	}
	if row.DurationMs == nil || *row.DurationMs != 1234 {
		t.Fatalf("duration_ms not preserved: %+v", row.DurationMs)
	}
	if row.ID == "" {
		t.Fatal("ID must be populated")
	}
	if row.Timestamp.IsZero() {
		t.Fatal("Timestamp must be populated")
	}
}

func TestListenerEvent_Create_NoDuration(t *testing.T) {
	a, db := newListenerEventsAPITest(t)
	station := models.Station{ID: "s1", Name: "S", Active: true, Public: true, Approved: true}
	if err := db.Create(&station).Error; err != nil {
		t.Fatalf("create station: %v", err)
	}

	body := listenerEventRequest{
		EventType:   "play",
		StationID:   "s1",
		StreamLabel: "HQ",
	}
	rr := postListenerEvent(t, a, body, "10.0.0.2:1111")

	if rr.Code != http.StatusNoContent {
		t.Fatalf("got %d, want 204 (body=%s)", rr.Code, rr.Body.String())
	}

	var row models.ListenerEvent
	if err := db.First(&row).Error; err != nil {
		t.Fatalf("read row: %v", err)
	}
	if row.DurationMs != nil {
		t.Fatalf("duration_ms must be nil when not provided, got %v", *row.DurationMs)
	}
}

func TestListenerEvent_Create_ValidEventTypes(t *testing.T) {
	a, db := newListenerEventsAPITest(t)
	station := models.Station{ID: "s1", Name: "S", Active: true, Public: true, Approved: true}
	if err := db.Create(&station).Error; err != nil {
		t.Fatalf("create station: %v", err)
	}

	for _, et := range []string{"reconnect", "degrade", "upgrade", "exhausted", "play", "stop"} {
		body := listenerEventRequest{EventType: et, StationID: "s1", StreamLabel: "HQ"}
		// Use a fresh IP per request so rate-limit bucket doesn't drain.
		rr := postListenerEvent(t, a, body, fmt.Sprintf("10.1.%d.1:1111", len(et)))
		if rr.Code != http.StatusNoContent {
			t.Fatalf("event_type=%s: got %d, want 204 (body=%s)", et, rr.Code, rr.Body.String())
		}
	}
}

func TestListenerEvent_Create_InvalidEventType(t *testing.T) {
	a, _ := newListenerEventsAPITest(t)
	body := listenerEventRequest{EventType: "explode", StationID: "s1", StreamLabel: "HQ"}
	rr := postListenerEvent(t, a, body, "10.0.0.5:1")
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400 (body=%s)", rr.Code, rr.Body.String())
	}
}

func TestListenerEvent_Create_MissingStationID(t *testing.T) {
	a, _ := newListenerEventsAPITest(t)
	body := listenerEventRequest{EventType: "play", StationID: "", StreamLabel: "HQ"}
	rr := postListenerEvent(t, a, body, "10.0.0.6:1")
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400 (body=%s)", rr.Code, rr.Body.String())
	}
}

func TestListenerEvent_Create_MissingStreamLabel(t *testing.T) {
	a, _ := newListenerEventsAPITest(t)
	body := listenerEventRequest{EventType: "play", StationID: "s1", StreamLabel: ""}
	rr := postListenerEvent(t, a, body, "10.0.0.7:1")
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400 (body=%s)", rr.Code, rr.Body.String())
	}
}

func TestListenerEvent_Create_NegativeDuration(t *testing.T) {
	a, _ := newListenerEventsAPITest(t)
	d := -5
	body := listenerEventRequest{EventType: "reconnect", StationID: "s1", StreamLabel: "HQ", DurationMs: &d}
	rr := postListenerEvent(t, a, body, "10.0.0.8:1")
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400 (body=%s)", rr.Code, rr.Body.String())
	}
}

func TestListenerEvent_Create_UnknownStation(t *testing.T) {
	a, _ := newListenerEventsAPITest(t)
	body := listenerEventRequest{EventType: "play", StationID: "ghost", StreamLabel: "HQ"}
	rr := postListenerEvent(t, a, body, "10.0.0.9:1")
	if rr.Code != http.StatusNotFound {
		t.Fatalf("got %d, want 404 (body=%s)", rr.Code, rr.Body.String())
	}
}

func TestListenerEvent_Create_InvalidJSON(t *testing.T) {
	a, _ := newListenerEventsAPITest(t)
	req := httptest.NewRequest("POST", "/api/v1/listener-events", bytes.NewBufferString("{not json"))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "10.0.0.10:1"
	rr := httptest.NewRecorder()
	a.handleListenerEventCreate(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400", rr.Code)
	}
}

func TestListenerEvent_RateLimit_PerIP(t *testing.T) {
	a, db := newListenerEventsAPITest(t)
	station := models.Station{ID: "s1", Name: "S", Active: true, Public: true, Approved: true}
	if err := db.Create(&station).Error; err != nil {
		t.Fatalf("create station: %v", err)
	}

	body := listenerEventRequest{EventType: "play", StationID: "s1", StreamLabel: "HQ"}

	// Send 10 from one IP; all should pass.
	for i := 0; i < 10; i++ {
		rr := postListenerEvent(t, a, body, "10.99.0.1:1234")
		if rr.Code != http.StatusNoContent {
			t.Fatalf("req %d: got %d, want 204 (body=%s)", i, rr.Code, rr.Body.String())
		}
	}
	// 11th from same IP must be 429.
	rr := postListenerEvent(t, a, body, "10.99.0.1:1234")
	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("got %d, want 429 (body=%s)", rr.Code, rr.Body.String())
	}

	// Different IP still allowed.
	rr = postListenerEvent(t, a, body, "10.99.0.2:1234")
	if rr.Code != http.StatusNoContent {
		t.Fatalf("different IP: got %d, want 204 (body=%s)", rr.Code, rr.Body.String())
	}
}

func TestListenerEventRateLimiter_RefillOverTime(t *testing.T) {
	l := newListenerEventRateLimiter()
	now := time.Date(2026, 6, 6, 12, 0, 0, 0, time.UTC)

	// Drain the bucket.
	for i := 0; i < 10; i++ {
		if !l.allow("1.2.3.4", now) {
			t.Fatalf("token %d: expected allow", i)
		}
	}
	if l.allow("1.2.3.4", now) {
		t.Fatal("bucket should be empty")
	}

	// Advance 7s -> refill ~1.16 tokens -> 1 request should succeed.
	if !l.allow("1.2.3.4", now.Add(7*time.Second)) {
		t.Fatal("expected refill to allow one request after 7s")
	}
	// The next one should be denied (only ~0.16 tokens left).
	if l.allow("1.2.3.4", now.Add(7*time.Second)) {
		t.Fatal("expected second request to be denied")
	}
}

func TestListenerEventRateLimiter_SweepDropsStaleIPs(t *testing.T) {
	l := newListenerEventRateLimiter()
	now := time.Date(2026, 6, 6, 12, 0, 0, 0, time.UTC)

	l.allow("1.1.1.1", now)
	l.allow("2.2.2.2", now)

	// Advance 11 minutes; the next allow() triggers a sweep.
	later := now.Add(11 * time.Minute)
	l.allow("3.3.3.3", later)

	l.mu.Lock()
	defer l.mu.Unlock()
	if _, ok := l.buckets["1.1.1.1"]; ok {
		t.Fatal("stale 1.1.1.1 should have been swept")
	}
	if _, ok := l.buckets["2.2.2.2"]; ok {
		t.Fatal("stale 2.2.2.2 should have been swept")
	}
	if _, ok := l.buckets["3.3.3.3"]; !ok {
		t.Fatal("active 3.3.3.3 must still be tracked")
	}
}

func TestClientIP_StripsPort(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"10.0.0.1:1234", "10.0.0.1"},
		{"[::1]:8080", "::1"},
		{"no-port-here", "no-port-here"},
	}
	for _, tc := range tests {
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = tc.in
		if got := clientIP(req); got != tc.want {
			t.Fatalf("clientIP(%q)=%q want %q", tc.in, got, tc.want)
		}
	}
}

// TestListenerEvent_Integration_FullRouter verifies the endpoint reaches a
// 204 through the full chi router (no auth header) so a future regression
// that drops the route registration fails this test.
func TestListenerEvent_Integration_FullRouter(t *testing.T) {
	db := newRoutesTestDB(t)
	if err := db.AutoMigrate(&models.ListenerEvent{}); err != nil {
		t.Fatalf("migrate ListenerEvent: %v", err)
	}

	bus := events.NewBus()
	prioritySvc := priority.NewService(db, bus, zerolog.Nop())
	auditSvc := audit.NewService(db, bus, zerolog.Nop())
	integritySvc := integrity.NewService(db, zerolog.Nop())
	stateMgr := executor.NewStateManager(db, zerolog.Nop())

	a := New(
		db, []byte("test-secret"),
		nil, nil, nil, nil, nil, nil,
		prioritySvc, stateMgr, auditSvc, integritySvc,
		nil, bus, nil, 0, zerolog.Nop(),
	)

	station := models.Station{
		ID:       "st-int-le",
		Name:     "Integration Station",
		Active:   true,
		Public:   true,
		Approved: true,
	}
	if err := db.Create(&station).Error; err != nil {
		t.Fatalf("create station: %v", err)
	}

	r := chi.NewRouter()
	a.Routes(r)

	body := listenerEventRequest{
		EventType:   "reconnect",
		StationID:   "st-int-le",
		StreamLabel: "HQ",
	}
	dur := 750
	body.DurationMs = &dur

	var buf bytes.Buffer
	_ = json.NewEncoder(&buf).Encode(body)
	req := httptest.NewRequest("POST", "/api/v1/listener-events", &buf)
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "10.10.10.10:55555"

	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code == http.StatusNotFound {
		t.Fatal("listener-events endpoint not registered; got 404 from full router")
	}
	if rr.Code != http.StatusNoContent {
		t.Fatalf("got %d, want 204; body=%s", rr.Code, rr.Body.String())
	}

	var count int64
	db.Model(&models.ListenerEvent{}).Count(&count)
	if count != 1 {
		t.Fatalf("expected 1 row written through full router, got %d", count)
	}
}
