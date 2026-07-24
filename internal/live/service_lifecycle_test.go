/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package live

import (
	"context"
	"testing"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/events"
	"github.com/friendsincode/grimnir_radio/internal/models"
	"github.com/friendsincode/grimnir_radio/internal/priority"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

func newLiveService(t *testing.T) (*Service, *gorm.DB, *events.Bus) {
	t.Helper()
	db := setupTestDB(t)
	bus := events.NewBus()
	svc := NewService(db, priority.NewService(db, bus, zerolog.Nop()), bus, zerolog.Nop())
	return svc, db, bus
}

// ---------------------------------------------------------------------------
// GenerateToken
// ---------------------------------------------------------------------------

func TestGenerateToken(t *testing.T) {
	svc, db, _ := newLiveService(t)
	station := createTestStation(t, db)

	token, err := svc.GenerateToken(context.Background(), GenerateTokenRequest{
		StationID: station.ID,
		MountID:   uuid.NewString(),
		UserID:    uuid.NewString(),
		Username:  "dj-nova",
		Priority:  models.PriorityLiveOverride,
		ExpiresIn: time.Hour,
	})
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}
	if len(token) != 64 { // 32 random bytes hex-encoded
		t.Fatalf("token length = %d, want 64", len(token))
	}

	var session models.LiveSession
	if err := db.First(&session, "token = ?", token).Error; err != nil {
		t.Fatalf("session not persisted: %v", err)
	}
	if session.TokenUsed || session.Active {
		t.Fatalf("fresh session should be unused and inactive: %+v", session)
	}
	if session.Username != "dj-nova" {
		t.Fatalf("username = %q", session.Username)
	}
}

func TestGenerateToken_InvalidPriority(t *testing.T) {
	svc, db, _ := newLiveService(t)
	station := createTestStation(t, db)
	_, err := svc.GenerateToken(context.Background(), GenerateTokenRequest{
		StationID: station.ID,
		Priority:  models.PriorityAutomation, // not a live priority
	})
	if err == nil {
		t.Fatal("expected error for non-live priority")
	}
}

// ---------------------------------------------------------------------------
// AuthorizeSource
// ---------------------------------------------------------------------------

func TestAuthorizeSource(t *testing.T) {
	svc, db, _ := newLiveService(t)
	station := createTestStation(t, db)
	mountID := uuid.NewString()
	token, err := svc.GenerateToken(context.Background(), GenerateTokenRequest{
		StationID: station.ID, MountID: mountID, UserID: uuid.NewString(), Username: "dj", Priority: models.PriorityLiveScheduled,
	})
	if err != nil {
		t.Fatalf("gen: %v", err)
	}

	ok, err := svc.AuthorizeSource(context.Background(), station.ID, mountID, token)
	if err != nil || !ok {
		t.Fatalf("authorize valid token: ok=%v err=%v", ok, err)
	}

	ok, err = svc.AuthorizeSource(context.Background(), station.ID, mountID, "wrong-token")
	if ok || err != ErrInvalidToken {
		t.Fatalf("authorize bad token: ok=%v err=%v, want false/ErrInvalidToken", ok, err)
	}
}

// ---------------------------------------------------------------------------
// HandleConnect / HandleDisconnect
// ---------------------------------------------------------------------------

func seedConnectableSession(t *testing.T, db *gorm.DB, stationID, mountID, token string, prio models.PriorityLevel) *models.LiveSession {
	t.Helper()
	session := &models.LiveSession{
		ID:          uuid.NewString(),
		StationID:   stationID,
		MountID:     mountID,
		UserID:      uuid.NewString(),
		Username:    "dj-live",
		Priority:    prio,
		Token:       token,
		ConnectedAt: time.Now(),
		Metadata:    make(map[string]any),
	}
	if err := db.Create(session).Error; err != nil {
		t.Fatalf("seed session: %v", err)
	}
	return session
}

func TestHandleConnect_Override(t *testing.T) {
	svc, db, _ := newLiveService(t)
	station := createTestStation(t, db)
	mountID := uuid.NewString()
	sess := seedConnectableSession(t, db, station.ID, mountID, "tok-override", models.PriorityLiveOverride)

	got, err := svc.HandleConnect(context.Background(), ConnectRequest{
		StationID: station.ID, MountID: mountID, Token: "tok-override",
		SourceIP: "203.0.113.5", SourcePort: 8000, UserAgent: "butt/1.0",
	})
	if err != nil {
		t.Fatalf("HandleConnect: %v", err)
	}
	if !got.Active || got.SourceIP != "203.0.113.5" || got.SourcePort != 8000 {
		t.Fatalf("session not updated on connect: %+v", got)
	}
	if got.ID != sess.ID {
		t.Fatalf("returned wrong session %s", got.ID)
	}
}

func TestHandleConnect_ScheduledAndAutoRecord(t *testing.T) {
	svc, db, bus := newLiveService(t)
	// Station with auto-record on.
	station := &models.Station{ID: uuid.NewString(), Name: "Auto", RecordingAutoRecord: true}
	if err := db.Create(station).Error; err != nil {
		t.Fatalf("create station: %v", err)
	}
	mountID := uuid.NewString()
	seedConnectableSession(t, db, station.ID, mountID, "tok-sched", models.PriorityLiveScheduled)

	autoRec := bus.Subscribe(events.EventRecordingAutoStart)

	if _, err := svc.HandleConnect(context.Background(), ConnectRequest{
		StationID: station.ID, MountID: mountID, Token: "tok-sched",
	}); err != nil {
		t.Fatalf("HandleConnect scheduled: %v", err)
	}

	select {
	case payload := <-autoRec:
		if payload["station_id"] != station.ID {
			t.Fatalf("auto-record event station = %v", payload["station_id"])
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("expected an auto-record event for the auto-record station")
	}
}

func TestHandleConnect_SessionNotFound(t *testing.T) {
	svc, db, _ := newLiveService(t)
	station := createTestStation(t, db)
	_, err := svc.HandleConnect(context.Background(), ConnectRequest{
		StationID: station.ID, MountID: uuid.NewString(), Token: "nope",
	})
	if err != ErrSessionNotFound {
		t.Fatalf("err = %v, want ErrSessionNotFound", err)
	}
}

func TestHandleDisconnect(t *testing.T) {
	svc, db, _ := newLiveService(t)
	station := createTestStation(t, db)
	mountID := uuid.NewString()
	sess := seedConnectableSession(t, db, station.ID, mountID, "tok-dc", models.PriorityLiveOverride)
	// Connect first so there's priority state to release.
	if _, err := svc.HandleConnect(context.Background(), ConnectRequest{StationID: station.ID, MountID: mountID, Token: "tok-dc"}); err != nil {
		t.Fatalf("connect: %v", err)
	}

	if err := svc.HandleDisconnect(context.Background(), sess.ID); err != nil {
		t.Fatalf("HandleDisconnect: %v", err)
	}

	var reloaded models.LiveSession
	db.First(&reloaded, "id = ?", sess.ID)
	if reloaded.Active {
		t.Fatal("session should be inactive after disconnect")
	}
	if reloaded.DisconnectedAt == nil {
		t.Fatal("DisconnectedAt should be set")
	}
}

func TestHandleDisconnect_NotFound(t *testing.T) {
	svc, _, _ := newLiveService(t)
	if err := svc.HandleDisconnect(context.Background(), "missing"); err != ErrSessionNotFound {
		t.Fatalf("err = %v, want ErrSessionNotFound", err)
	}
}

// ---------------------------------------------------------------------------
// GetActiveSessions / GetSession
// ---------------------------------------------------------------------------

func TestGetActiveSessions(t *testing.T) {
	svc, db, _ := newLiveService(t)
	stA := &models.Station{ID: uuid.NewString(), Name: "Station A"}
	stB := &models.Station{ID: uuid.NewString(), Name: "Station B"}
	if err := db.Create(stA).Error; err != nil {
		t.Fatalf("create stA: %v", err)
	}
	if err := db.Create(stB).Error; err != nil {
		t.Fatalf("create stB: %v", err)
	}
	// Two active (one per station) + one inactive.
	mkSession := func(stationID, token string, active bool) {
		s := &models.LiveSession{
			ID: uuid.NewString(), StationID: stationID, MountID: uuid.NewString(),
			UserID: uuid.NewString(), Username: "dj", Priority: models.PriorityLiveOverride,
			Token: token, Active: active, ConnectedAt: time.Now(), Metadata: map[string]any{},
		}
		if err := db.Create(s).Error; err != nil {
			t.Fatalf("create session %s: %v", token, err)
		}
	}
	mkSession(stA.ID, "tok-a", true)
	mkSession(stB.ID, "tok-b", true)
	mkSession(stA.ID, "tok-inactive", false)

	all, err := svc.GetActiveSessions(context.Background(), "")
	if err != nil {
		t.Fatalf("all: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("active across all stations = %d, want 2", len(all))
	}

	scoped, err := svc.GetActiveSessions(context.Background(), stA.ID)
	if err != nil {
		t.Fatalf("scoped: %v", err)
	}
	if len(scoped) != 1 || scoped[0].StationID != stA.ID {
		t.Fatalf("scoped active = %+v", scoped)
	}
}

func TestGetSession(t *testing.T) {
	svc, db, _ := newLiveService(t)
	station := createTestStation(t, db)
	sess := createTestSession(t, db, station.ID, models.PriorityLiveOverride)

	got, err := svc.GetSession(context.Background(), sess.ID)
	if err != nil || got.ID != sess.ID {
		t.Fatalf("GetSession: got=%v err=%v", got, err)
	}
	if _, err := svc.GetSession(context.Background(), "missing"); err != ErrSessionNotFound {
		t.Fatalf("err = %v, want ErrSessionNotFound", err)
	}
}
