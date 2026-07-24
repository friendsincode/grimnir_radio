/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package webhooks

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
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

func testService(t *testing.T) (*Service, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.Station{}, &models.User{}, &models.Show{}, &models.ShowInstance{}, &models.WebhookTarget{}, &models.WebhookLog{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return NewService(db, events.NewBus(), zerolog.Nop()), db
}

func TestSignPayload_MatchesHMAC(t *testing.T) {
	s := &Service{}
	body := []byte(`{"event":"test"}`)
	got := s.signPayload(body, "supersecret")

	mac := hmac.New(sha256.New, []byte("supersecret"))
	mac.Write(body)
	want := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if got != want {
		t.Fatalf("signature = %q, want %q", got, want)
	}
}

func TestWebhookHandlesEvent(t *testing.T) {
	s := &Service{}
	cases := []struct {
		events string
		event  string
		want   bool
	}{
		{"", EventShowStart, true}, // empty = all events
		{"show_start,show_end", "show_start", true},
		{"show_start, show_end", "show_end", true}, // whitespace tolerated
		{"show_start", "show_end", false},
	}
	for _, tc := range cases {
		got := s.webhookHandlesEvent(models.WebhookTarget{Events: tc.events}, tc.event)
		if got != tc.want {
			t.Fatalf("handles(%q,%q) = %v, want %v", tc.events, tc.event, got, tc.want)
		}
	}
}

func TestInstanceToPayload(t *testing.T) {
	s := &Service{}
	if s.instanceToPayload(nil) != nil {
		t.Fatal("nil instance should give nil payload")
	}
	if s.instanceToPayload(&models.ShowInstance{}) != nil {
		t.Fatal("instance without show should give nil payload")
	}

	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	inst := &models.ShowInstance{
		ID:       "inst1",
		StartsAt: start,
		EndsAt:   start.Add(time.Hour),
		Show:     &models.Show{Name: "Morning", Description: "News", Color: "#FFF"},
	}
	// Instance host takes precedence.
	inst.Host = &models.User{Email: "host@station.fm"}
	inst.Show.Host = &models.User{Email: "showhost@station.fm"}
	p := s.instanceToPayload(inst)
	if p.ID != "inst1" || p.Name != "Morning" || p.HostName != "host@station.fm" {
		t.Fatalf("payload = %+v", p)
	}

	// Falls back to the show's host when the instance has none.
	inst.Host = nil
	if p := s.instanceToPayload(inst); p.HostName != "showhost@station.fm" {
		t.Fatalf("expected show-host fallback, got %q", p.HostName)
	}
}

func TestSendWebhook_DeliversSignedAndLogs(t *testing.T) {
	svc, db := testService(t)

	var gotSig, gotEvent string
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSig = r.Header.Get("X-Grimnir-Signature")
		gotEvent = r.Header.Get("X-Grimnir-Event")
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	wh := models.WebhookTarget{ID: "wh1", StationID: "st1", URL: srv.URL, Secret: "sk", Active: true}
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	current := &models.ShowInstance{ID: "i1", StartsAt: start, EndsAt: start.Add(time.Hour), Show: &models.Show{Name: "Morning"}}

	svc.sendWebhook(t.Context(), wh, EventShowStart, current, nil)

	if gotEvent != EventShowStart {
		t.Fatalf("event header = %q", gotEvent)
	}
	// Signature header must be the HMAC of the exact delivered body.
	mac := hmac.New(sha256.New, []byte("sk"))
	mac.Write(gotBody)
	want := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if gotSig != want {
		t.Fatalf("signature = %q, want %q", gotSig, want)
	}

	// A delivery row is recorded with the 200 status.
	var log models.WebhookLog
	if err := db.First(&log, "target_id = ?", "wh1").Error; err != nil {
		t.Fatalf("expected a delivery log: %v", err)
	}
	if log.StatusCode != http.StatusOK || log.Event != EventShowStart {
		t.Fatalf("delivery log = %+v", log)
	}
}

func TestTestWebhook(t *testing.T) {
	svc, _ := testService(t)

	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Grimnir-Event") != "test" {
			t.Errorf("expected test event header")
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer ok.Close()
	if err := svc.TestWebhook(&models.WebhookTarget{URL: ok.URL, Secret: "s"}); err != nil {
		t.Fatalf("TestWebhook on 204 should succeed: %v", err)
	}

	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer bad.Close()
	if err := svc.TestWebhook(&models.WebhookTarget{URL: bad.URL}); err == nil {
		t.Fatal("TestWebhook on 500 should error")
	}

	if err := svc.TestWebhook(&models.WebhookTarget{URL: "http://127.0.0.1:0"}); err == nil {
		t.Fatal("TestWebhook to an unreachable URL should error")
	}
}

func TestGetNextShow(t *testing.T) {
	svc, db := testService(t)
	base := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	db.Create(&models.Show{ID: "sh1", StationID: "st1", Name: "Later"})
	db.Create(&models.ShowInstance{ID: "n1", ShowID: "sh1", StationID: "st1", StartsAt: base.Add(2 * time.Hour), EndsAt: base.Add(3 * time.Hour), Status: models.ShowInstanceScheduled})

	if next := svc.getNextShow("st1", base); next == nil || next.ID != "n1" {
		t.Fatalf("expected next show n1, got %+v", next)
	}
	if next := svc.getNextShow("st1", base.Add(10*time.Hour)); next != nil {
		t.Fatalf("expected no next show after everything, got %+v", next)
	}
}

func TestHandleShowEvent(t *testing.T) {
	svc, db := testService(t)
	base := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	db.Create(&models.Show{ID: "sh1", StationID: "st1", Name: "Morning"})
	db.Create(&models.ShowInstance{ID: "i1", ShowID: "sh1", StationID: "st1", StartsAt: base, EndsAt: base.Add(time.Hour), Status: models.ShowInstanceScheduled})

	// No station_id in the payload => early return, no panic.
	svc.handleShowEvent(t.Context(), events.Payload{}, EventShowStart)

	// With a valid instance and no webhook targets, it resolves the instance and
	// next show then fires zero webhooks (deterministic, no goroutines spawned).
	svc.handleShowEvent(t.Context(), events.Payload{"station_id": "st1", "instance_id": "i1"}, EventShowStart)

	// Missing instance still resolves via the now-based next-show lookup.
	svc.handleShowEvent(t.Context(), events.Payload{"station_id": "st1"}, EventShowEnd)
}

func TestCheckTransitions_TracksActiveShow(t *testing.T) {
	svc, db := testService(t)
	now := time.Now()
	db.Create(&models.Station{ID: "st1", Name: "Rock", Public: true})
	db.Create(&models.Show{ID: "sh1", StationID: "st1", Name: "On Air"})
	db.Create(&models.ShowInstance{ID: "cur", ShowID: "sh1", StationID: "st1", StartsAt: now.Add(-time.Hour), EndsAt: now.Add(time.Hour), Status: models.ShowInstanceScheduled})

	active := map[string]string{}
	svc.checkTransitions(t.Context(), active)
	if active["st1"] != "cur" {
		t.Fatalf("active show for st1 = %q, want cur", active["st1"])
	}

	// No change on a second pass.
	svc.checkTransitions(t.Context(), active)
	if active["st1"] != "cur" {
		t.Fatalf("active show changed unexpectedly to %q", active["st1"])
	}
}
