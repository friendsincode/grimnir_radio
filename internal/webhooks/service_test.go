/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package webhooks

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/events"
	"github.com/friendsincode/grimnir_radio/internal/models"
)

func openWebhookTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("db handle: %v", err)
	}
	// A single connection keeps every session on the same in-memory database
	// and serializes writes from delivery goroutines.
	sqlDB.SetMaxOpenConns(1)

	if err := db.AutoMigrate(
		&models.User{},
		&models.Station{},
		&models.Show{},
		&models.ShowInstance{},
		&models.WebhookTarget{},
		&models.WebhookLog{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func newWebhookTestService(t *testing.T) (*Service, *gorm.DB, *events.Bus) {
	t.Helper()
	db := openWebhookTestDB(t)
	bus := events.NewBus()
	return NewService(db, bus, zerolog.Nop()), db, bus
}

type receivedRequest struct {
	method string
	header http.Header
	body   []byte
}

func newCaptureServer(t *testing.T, status int) (*httptest.Server, chan receivedRequest) {
	t.Helper()
	requests := make(chan receivedRequest, 32)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, r.ContentLength)
		if r.ContentLength > 0 {
			if _, err := r.Body.Read(body); err != nil && err.Error() != "EOF" {
				t.Errorf("read body: %v", err)
			}
		}
		requests <- receivedRequest{method: r.Method, header: r.Header.Clone(), body: body}
		w.WriteHeader(status)
	}))
	t.Cleanup(srv.Close)
	return srv, requests
}

func waitRequest(t *testing.T, requests chan receivedRequest) receivedRequest {
	t.Helper()
	select {
	case req := <-requests:
		return req
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for webhook delivery")
		return receivedRequest{}
	}
}

func waitLogCount(t *testing.T, db *gorm.DB, targetID string, want int64) []models.WebhookLog {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var count int64
		if err := db.Model(&models.WebhookLog{}).Where("target_id = ?", targetID).Count(&count).Error; err != nil {
			t.Fatalf("count logs: %v", err)
		}
		if count >= want {
			var logs []models.WebhookLog
			if err := db.Where("target_id = ?", targetID).Find(&logs).Error; err != nil {
				t.Fatalf("fetch logs: %v", err)
			}
			return logs
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d webhook logs for target %s", want, targetID)
	return nil
}

func testInstance(id, stationID, showName string, startsAt, endsAt time.Time) *models.ShowInstance {
	return &models.ShowInstance{
		ID:        id,
		ShowID:    "show-" + id,
		StationID: stationID,
		StartsAt:  startsAt,
		EndsAt:    endsAt,
		Status:    models.ShowInstanceScheduled,
		Show: &models.Show{
			ID:          "show-" + id,
			StationID:   stationID,
			Name:        showName,
			Description: "desc",
			Color:       "#FF5733",
		},
	}
}

func TestWebhookHandlesEvent(t *testing.T) {
	svc, _, _ := newWebhookTestService(t)

	tests := []struct {
		name      string
		events    string
		eventType string
		want      bool
	}{
		{name: "empty events handles everything", events: "", eventType: EventShowStart, want: true},
		{name: "exact match", events: "show_start", eventType: EventShowStart, want: true},
		{name: "list with spaces", events: "show_end, show_start", eventType: EventShowStart, want: true},
		{name: "not subscribed", events: "show_end", eventType: EventShowStart, want: false},
		{name: "partial name does not match", events: "show_startle", eventType: EventShowStart, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wh := models.WebhookTarget{Events: tt.events}
			if got := svc.webhookHandlesEvent(wh, tt.eventType); got != tt.want {
				t.Fatalf("webhookHandlesEvent(%q, %q) = %v, want %v", tt.events, tt.eventType, got, tt.want)
			}
		})
	}
}

func TestSignPayload(t *testing.T) {
	svc, _, _ := newWebhookTestService(t)

	// Known-answer test: HMAC-SHA256("test-secret", `{"hello":"world"}`).
	got := svc.signPayload([]byte(`{"hello":"world"}`), "test-secret")
	want := "sha256=84cc33df716ed0b0598f07437c94069ace3730358778a592bd6bbd1423d111f3"
	if got != want {
		t.Fatalf("signPayload = %q, want %q", got, want)
	}

	if svc.signPayload([]byte(`{"hello":"world"}`), "other-secret") == want {
		t.Fatal("different secrets must produce different signatures")
	}
}

func TestInstanceToPayload(t *testing.T) {
	svc, _, _ := newWebhookTestService(t)
	starts := time.Date(2026, 7, 6, 9, 0, 0, 0, time.UTC)
	ends := starts.Add(time.Hour)

	instanceHost := &models.User{ID: "u-inst", Email: "instance-host@example.com"}
	showHost := &models.User{ID: "u-show", Email: "show-host@example.com"}

	tests := []struct {
		name     string
		instance *models.ShowInstance
		want     *ShowPayload
	}{
		{name: "nil instance", instance: nil, want: nil},
		{
			name:     "instance without show",
			instance: &models.ShowInstance{ID: "i1"},
			want:     nil,
		},
		{
			name: "instance host overrides show host",
			instance: func() *models.ShowInstance {
				inst := testInstance("i2", "st-1", "Morning Show", starts, ends)
				inst.Host = instanceHost
				inst.Show.Host = showHost
				return inst
			}(),
			want: &ShowPayload{
				ID: "i2", Name: "Morning Show", Description: "desc", Color: "#FF5733",
				HostName: "instance-host@example.com", StartsAt: starts, EndsAt: ends,
			},
		},
		{
			name: "falls back to show host",
			instance: func() *models.ShowInstance {
				inst := testInstance("i3", "st-1", "Evening Show", starts, ends)
				inst.Show.Host = showHost
				return inst
			}(),
			want: &ShowPayload{
				ID: "i3", Name: "Evening Show", Description: "desc", Color: "#FF5733",
				HostName: "show-host@example.com", StartsAt: starts, EndsAt: ends,
			},
		},
		{
			name:     "no host at all",
			instance: testInstance("i4", "st-1", "Anon Show", starts, ends),
			want: &ShowPayload{
				ID: "i4", Name: "Anon Show", Description: "desc", Color: "#FF5733",
				StartsAt: starts, EndsAt: ends,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := svc.instanceToPayload(tt.instance)
			if (got == nil) != (tt.want == nil) {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
			if got == nil {
				return
			}
			if *got != *tt.want {
				t.Fatalf("got %+v, want %+v", *got, *tt.want)
			}
		})
	}
}

func TestSendWebhookDelivery(t *testing.T) {
	svc, db, _ := newWebhookTestService(t)
	srv, requests := newCaptureServer(t, http.StatusOK)

	target := models.WebhookTarget{
		ID:        "wh-1",
		StationID: "st-1",
		URL:       srv.URL,
		Secret:    "test-secret",
		Active:    true,
	}

	starts := time.Now().UTC().Truncate(time.Second)
	current := testInstance("cur", "st-1", "Current Show", starts, starts.Add(time.Hour))
	next := testInstance("next", "st-1", "Next Show", starts.Add(time.Hour), starts.Add(2*time.Hour))

	svc.sendWebhook(context.Background(), target, EventShowStart, current, next)

	req := waitRequest(t, requests)
	if req.method != http.MethodPost {
		t.Fatalf("method = %s, want POST", req.method)
	}
	if ct := req.header.Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type = %q", ct)
	}
	if ua := req.header.Get("User-Agent"); ua != "Grimnir-Radio-Webhook/1.0" {
		t.Fatalf("User-Agent = %q", ua)
	}
	if ev := req.header.Get("X-Grimnir-Event"); ev != EventShowStart {
		t.Fatalf("X-Grimnir-Event = %q", ev)
	}
	if ts := req.header.Get("X-Grimnir-Timestamp"); ts == "" {
		t.Fatal("missing X-Grimnir-Timestamp header")
	}

	// The signature must verify against the delivered body.
	mac := hmac.New(sha256.New, []byte("test-secret"))
	mac.Write(req.body)
	wantSig := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if sig := req.header.Get("X-Grimnir-Signature"); sig != wantSig {
		t.Fatalf("signature = %q, want %q", sig, wantSig)
	}

	var payload WebhookPayload
	if err := json.Unmarshal(req.body, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload.Event != EventShowStart || payload.StationID != "st-1" {
		t.Fatalf("unexpected payload: %+v", payload)
	}
	if payload.Show == nil || payload.Show.Name != "Current Show" {
		t.Fatalf("payload.Show = %+v", payload.Show)
	}
	if payload.NextShow == nil || payload.NextShow.Name != "Next Show" {
		t.Fatalf("payload.NextShow = %+v", payload.NextShow)
	}

	logs := waitLogCount(t, db, "wh-1", 1)
	if logs[0].StatusCode != http.StatusOK || logs[0].Error != "" || logs[0].Event != EventShowStart {
		t.Fatalf("unexpected log: %+v", logs[0])
	}
}

func TestSendWebhookNoSecretSkipsSignature(t *testing.T) {
	svc, _, _ := newWebhookTestService(t)
	srv, requests := newCaptureServer(t, http.StatusOK)

	target := models.WebhookTarget{ID: "wh-nosec", StationID: "st-1", URL: srv.URL, Active: true}
	svc.sendWebhook(context.Background(), target, EventShowEnd, nil, nil)

	req := waitRequest(t, requests)
	if sig := req.header.Get("X-Grimnir-Signature"); sig != "" {
		t.Fatalf("expected no signature header, got %q", sig)
	}
	var payload WebhookPayload
	if err := json.Unmarshal(req.body, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload.Show != nil || payload.NextShow != nil {
		t.Fatalf("expected empty show payloads, got %+v", payload)
	}
}

func TestSendWebhookErrorStatusLogged(t *testing.T) {
	svc, db, _ := newWebhookTestService(t)
	srv, _ := newCaptureServer(t, http.StatusInternalServerError)

	target := models.WebhookTarget{ID: "wh-err", StationID: "st-1", URL: srv.URL, Active: true}
	svc.sendWebhook(context.Background(), target, EventShowStart, nil, nil)

	logs := waitLogCount(t, db, "wh-err", 1)
	if logs[0].StatusCode != http.StatusInternalServerError {
		t.Fatalf("log status = %d, want 500", logs[0].StatusCode)
	}
}

func TestSendWebhookConnectionErrorLogged(t *testing.T) {
	svc, db, _ := newWebhookTestService(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close()

	target := models.WebhookTarget{ID: "wh-conn", StationID: "st-1", URL: url, Active: true}
	svc.sendWebhook(context.Background(), target, EventShowStart, nil, nil)

	logs := waitLogCount(t, db, "wh-conn", 1)
	if logs[0].StatusCode != 0 {
		t.Fatalf("log status = %d, want 0", logs[0].StatusCode)
	}
	if logs[0].Error == "" {
		t.Fatal("expected connection error to be recorded")
	}
}

func TestFireWebhooksFiltering(t *testing.T) {
	svc, db, _ := newWebhookTestService(t)
	srvA, requestsA := newCaptureServer(t, http.StatusOK)
	srvB, requestsB := newCaptureServer(t, http.StatusOK)

	targets := []models.WebhookTarget{
		{ID: "wh-a", StationID: "st-1", URL: srvA.URL, Events: "show_start", Active: true},
		{ID: "wh-b", StationID: "st-1", URL: srvB.URL, Events: "show_end", Active: true},
		{ID: "wh-inactive", StationID: "st-1", URL: srvB.URL, Events: "", Active: false},
		{ID: "wh-other-station", StationID: "st-2", URL: srvB.URL, Events: "", Active: true},
	}
	for _, target := range targets {
		row := target
		if err := db.Create(&row).Error; err != nil {
			t.Fatalf("seed target %s: %v", target.ID, err)
		}
		// Active=false is a zero value shadowed by the column default.
		if !target.Active {
			if err := db.Model(&models.WebhookTarget{}).Where("id = ?", target.ID).Update("active", false).Error; err != nil {
				t.Fatalf("deactivate: %v", err)
			}
		}
	}

	svc.fireWebhooks(context.Background(), "st-1", EventShowStart, nil, nil)

	waitRequest(t, requestsA)
	waitLogCount(t, db, "wh-a", 1)

	select {
	case <-requestsB:
		t.Fatal("webhook subscribed only to show_end must not receive show_start")
	case <-time.After(200 * time.Millisecond):
	}

	var totalLogs int64
	if err := db.Model(&models.WebhookLog{}).Count(&totalLogs).Error; err != nil {
		t.Fatalf("count logs: %v", err)
	}
	if totalLogs != 1 {
		t.Fatalf("expected exactly 1 delivery log, got %d", totalLogs)
	}
}

func seedStationWithWebhook(t *testing.T, db *gorm.DB, stationID, url string, public bool) {
	t.Helper()
	station := models.Station{ID: stationID, Name: "Station " + stationID, Public: public}
	if err := db.Create(&station).Error; err != nil {
		t.Fatalf("seed station: %v", err)
	}
	if !public {
		if err := db.Model(&models.Station{}).Where("id = ?", stationID).Update("public", false).Error; err != nil {
			t.Fatalf("set public: %v", err)
		}
	}
	target := models.WebhookTarget{ID: "wh-" + stationID, StationID: stationID, URL: url, Active: true}
	if err := db.Create(&target).Error; err != nil {
		t.Fatalf("seed target: %v", err)
	}
}

func seedInstance(t *testing.T, db *gorm.DB, inst *models.ShowInstance) {
	t.Helper()
	if inst.Show != nil {
		if err := db.Create(inst.Show).Error; err != nil {
			t.Fatalf("seed show: %v", err)
		}
	}
	show := inst.Show
	inst.Show = nil
	if err := db.Create(inst).Error; err != nil {
		t.Fatalf("seed instance: %v", err)
	}
	inst.Show = show
}

func TestCheckTransitions(t *testing.T) {
	svc, db, _ := newWebhookTestService(t)
	srv, requests := newCaptureServer(t, http.StatusOK)

	seedStationWithWebhook(t, db, "st-pub", srv.URL, true)
	seedStationWithWebhook(t, db, "st-priv", srv.URL, false)

	now := time.Now() // local: the sqlite driver compares time text in local format
	currentPub := testInstance("cur-pub", "st-pub", "On Air", now.Add(-10*time.Minute), now.Add(50*time.Minute))
	nextPub := testInstance("next-pub", "st-pub", "Up Next", now.Add(50*time.Minute), now.Add(110*time.Minute))
	currentPriv := testInstance("cur-priv", "st-priv", "Private Show", now.Add(-10*time.Minute), now.Add(50*time.Minute))
	seedInstance(t, db, currentPub)
	seedInstance(t, db, nextPub)
	seedInstance(t, db, currentPriv)

	activeShows := make(map[string]string)

	// Transition 1: nothing -> current show. Fires show_start for the public
	// station only.
	svc.checkTransitions(context.Background(), activeShows)

	req := waitRequest(t, requests)
	var payload WebhookPayload
	if err := json.Unmarshal(req.body, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if payload.Event != EventShowStart || payload.StationID != "st-pub" {
		t.Fatalf("unexpected payload: %+v", payload)
	}
	if payload.Show == nil || payload.Show.Name != "On Air" {
		t.Fatalf("payload.Show = %+v", payload.Show)
	}
	if payload.NextShow == nil || payload.NextShow.Name != "Up Next" {
		t.Fatalf("payload.NextShow = %+v", payload.NextShow)
	}
	if activeShows["st-pub"] != "cur-pub" {
		t.Fatalf("activeShows[st-pub] = %q, want cur-pub", activeShows["st-pub"])
	}
	if _, tracked := activeShows["st-priv"]; tracked {
		t.Fatal("private station must not be tracked")
	}

	// No transition: same show still on air, no new webhook.
	svc.checkTransitions(context.Background(), activeShows)
	select {
	case req := <-requests:
		t.Fatalf("unexpected delivery without transition: %s", req.body)
	case <-time.After(200 * time.Millisecond):
	}

	// Transition 2: tracked show replaced by a different one. Fires show_end
	// then show_start.
	activeShows["st-pub"] = "some-older-instance"
	svc.checkTransitions(context.Background(), activeShows)

	got := map[string]bool{}
	for i := 0; i < 2; i++ {
		req := waitRequest(t, requests)
		var p WebhookPayload
		if err := json.Unmarshal(req.body, &p); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		got[p.Event] = true
	}
	if !got[EventShowEnd] || !got[EventShowStart] {
		t.Fatalf("expected show_end and show_start, got %v", got)
	}

	// Transition 3: tracked show ends with nothing on air. Fires show_end.
	if err := db.Model(&models.ShowInstance{}).Where("id = ?", "cur-pub").
		Update("ends_at", now.Add(-time.Minute)).Error; err != nil {
		t.Fatalf("expire instance: %v", err)
	}
	svc.checkTransitions(context.Background(), activeShows)

	req = waitRequest(t, requests)
	if err := json.Unmarshal(req.body, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if payload.Event != EventShowEnd || payload.StationID != "st-pub" {
		t.Fatalf("unexpected payload: %+v", payload)
	}
	if activeShows["st-pub"] != "" {
		t.Fatalf("activeShows[st-pub] = %q, want empty", activeShows["st-pub"])
	}
}

func TestGetNextShow(t *testing.T) {
	svc, db, _ := newWebhookTestService(t)
	now := time.Now().UTC()

	if got := svc.getNextShow("st-1", now); got != nil {
		t.Fatalf("expected nil with empty schedule, got %+v", got)
	}

	later := testInstance("later", "st-1", "Later Show", now.Add(3*time.Hour), now.Add(4*time.Hour))
	sooner := testInstance("sooner", "st-1", "Sooner Show", now.Add(1*time.Hour), now.Add(2*time.Hour))
	cancelled := testInstance("cancelled", "st-1", "Cancelled Show", now.Add(30*time.Minute), now.Add(time.Hour))
	otherStation := testInstance("other", "st-2", "Other Station", now.Add(15*time.Minute), now.Add(time.Hour))
	seedInstance(t, db, later)
	seedInstance(t, db, sooner)
	seedInstance(t, db, cancelled)
	seedInstance(t, db, otherStation)
	if err := db.Model(&models.ShowInstance{}).Where("id = ?", "cancelled").
		Update("status", models.ShowInstanceCancelled).Error; err != nil {
		t.Fatalf("cancel instance: %v", err)
	}

	got := svc.getNextShow("st-1", now)
	if got == nil || got.ID != "sooner" {
		t.Fatalf("getNextShow = %+v, want sooner", got)
	}
	if got.Show == nil || got.Show.Name != "Sooner Show" {
		t.Fatal("expected Show preloaded")
	}
}

func TestTestWebhook(t *testing.T) {
	svc, _, _ := newWebhookTestService(t)

	t.Run("success with signature", func(t *testing.T) {
		srv, requests := newCaptureServer(t, http.StatusOK)
		target := &models.WebhookTarget{ID: "wh-test", StationID: "st-1", URL: srv.URL, Secret: "s3cret"}

		if err := svc.TestWebhook(target); err != nil {
			t.Fatalf("TestWebhook: %v", err)
		}

		req := waitRequest(t, requests)
		if ev := req.header.Get("X-Grimnir-Event"); ev != "test" {
			t.Fatalf("X-Grimnir-Event = %q, want test", ev)
		}
		mac := hmac.New(sha256.New, []byte("s3cret"))
		mac.Write(req.body)
		if sig := req.header.Get("X-Grimnir-Signature"); sig != "sha256="+hex.EncodeToString(mac.Sum(nil)) {
			t.Fatalf("bad signature %q", sig)
		}
		var payload WebhookPayload
		if err := json.Unmarshal(req.body, &payload); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if payload.Event != "test" || payload.Show == nil || payload.Show.Name != "Test Show" {
			t.Fatalf("unexpected payload: %+v", payload)
		}
	})

	t.Run("error status returned", func(t *testing.T) {
		srv, _ := newCaptureServer(t, http.StatusBadGateway)
		target := &models.WebhookTarget{ID: "wh-bad", StationID: "st-1", URL: srv.URL}

		err := svc.TestWebhook(target)
		if err == nil || !strings.Contains(err.Error(), "502") {
			t.Fatalf("expected status 502 error, got %v", err)
		}
	})

	t.Run("connection failure returned", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
		url := srv.URL
		srv.Close()
		target := &models.WebhookTarget{ID: "wh-down", StationID: "st-1", URL: url}

		if err := svc.TestWebhook(target); err == nil {
			t.Fatal("expected connection error")
		}
	})
}

func TestStartDeliversBusEvents(t *testing.T) {
	svc, db, bus := newWebhookTestService(t)
	srv, requests := newCaptureServer(t, http.StatusOK)

	seedStationWithWebhook(t, db, "st-live", srv.URL, true)
	now := time.Now().UTC()
	current := testInstance("live-cur", "st-live", "Live Show", now.Add(-5*time.Minute), now.Add(55*time.Minute))
	seedInstance(t, db, current)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		svc.Start(ctx)
		close(done)
	}()

	// Start subscribes asynchronously; publish until the subscription is live
	// and a delivery arrives.
	payload := events.Payload{"station_id": "st-live", "instance_id": "live-cur"}
	var req receivedRequest
	deadline := time.Now().Add(5 * time.Second)
	for {
		bus.Publish(events.EventShowStart, payload)
		select {
		case req = <-requests:
		case <-time.After(50 * time.Millisecond):
			if time.Now().After(deadline) {
				t.Fatal("timed out waiting for show_start delivery via bus")
			}
			continue
		}
		break
	}

	var delivered WebhookPayload
	if err := json.Unmarshal(req.body, &delivered); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if delivered.Event != EventShowStart || delivered.StationID != "st-live" {
		t.Fatalf("unexpected payload: %+v", delivered)
	}
	if delivered.Show == nil || delivered.Show.Name != "Live Show" {
		t.Fatalf("payload.Show = %+v", delivered.Show)
	}

	// show_end without an instance ID still delivers.
	deadline = time.Now().Add(5 * time.Second)
	for {
		bus.Publish(events.EventShowEnd, events.Payload{"station_id": "st-live"})
		var got receivedRequest
		select {
		case got = <-requests:
		case <-time.After(50 * time.Millisecond):
			if time.Now().After(deadline) {
				t.Fatal("timed out waiting for show_end delivery via bus")
			}
			continue
		}
		var p WebhookPayload
		if err := json.Unmarshal(got.body, &p); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if p.Event == EventShowEnd {
			break
		}
	}

	// A payload without station_id is ignored.
	bus.Publish(events.EventShowStart, events.Payload{"instance_id": "live-cur"})

	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Start did not stop on context cancel")
	}
}
