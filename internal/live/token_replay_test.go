/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package live

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

// TestHandleConnect_SingleUse_RejectsReplay guards #82: a live DJ token is
// one-time use. The first connect consumes it (token_used=true); a replay of the
// same token must be rejected, and AuthorizeSource must reject the used token
// too. Before the fix the token was replayable forever.
func TestHandleConnect_SingleUse_RejectsReplay(t *testing.T) {
	svc, db, _ := newLiveService(t)
	ctx := context.Background()
	station := createTestStation(t, db)
	mountID := uuid.NewString()
	seedConnectableSession(t, db, station.ID, mountID, "tok-replay", models.PriorityLiveOverride)

	if _, err := svc.HandleConnect(ctx, ConnectRequest{StationID: station.ID, MountID: mountID, Token: "tok-replay", SourceIP: "1.2.3.4"}); err != nil {
		t.Fatalf("first connect: %v", err)
	}
	var sess models.LiveSession
	if err := db.First(&sess, "token = ?", "tok-replay").Error; err != nil {
		t.Fatalf("reload: %v", err)
	}
	if !sess.TokenUsed {
		t.Fatal("token not marked used after connect")
	}

	if _, err := svc.HandleConnect(ctx, ConnectRequest{StationID: station.ID, MountID: mountID, Token: "tok-replay", SourceIP: "1.2.3.4"}); err != ErrTokenAlreadyUsed {
		t.Fatalf("replay connect: got %v, want ErrTokenAlreadyUsed", err)
	}
	if ok, err := svc.AuthorizeSource(ctx, station.ID, mountID, "tok-replay"); ok || err != ErrTokenAlreadyUsed {
		t.Fatalf("AuthorizeSource on used token: ok=%v err=%v, want false/ErrTokenAlreadyUsed", ok, err)
	}
}

// TestLiveToken_ExpiryRejected guards the expiry half: a past-expiry token is
// refused by both the pre-check and the connect.
func TestLiveToken_ExpiryRejected(t *testing.T) {
	svc, db, _ := newLiveService(t)
	ctx := context.Background()
	station := createTestStation(t, db)
	mountID := uuid.NewString()
	seedConnectableSession(t, db, station.ID, mountID, "tok-exp", models.PriorityLiveOverride)
	if err := db.Model(&models.LiveSession{}).Where("token = ?", "tok-exp").
		Update("expires_at", time.Now().Add(-time.Minute)).Error; err != nil {
		t.Fatalf("set expiry: %v", err)
	}

	if ok, err := svc.AuthorizeSource(ctx, station.ID, mountID, "tok-exp"); ok || err != ErrInvalidToken {
		t.Fatalf("AuthorizeSource expired: ok=%v err=%v, want false/ErrInvalidToken", ok, err)
	}
	if _, err := svc.HandleConnect(ctx, ConnectRequest{StationID: station.ID, MountID: mountID, Token: "tok-exp"}); err != ErrInvalidToken {
		t.Fatalf("HandleConnect expired: got %v, want ErrInvalidToken", err)
	}
}

// TestGenerateToken_DefaultsExpiry checks that a token with no explicit lifetime
// still gets a bounded expiry, so it is never valid forever.
func TestGenerateToken_DefaultsExpiry(t *testing.T) {
	svc, db, _ := newLiveService(t)
	ctx := context.Background()
	station := createTestStation(t, db)

	token, err := svc.GenerateToken(ctx, GenerateTokenRequest{
		StationID: station.ID, MountID: uuid.NewString(), UserID: uuid.NewString(),
		Username: "dj", Priority: models.PriorityLiveOverride, // ExpiresIn unset
	})
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}
	var sess models.LiveSession
	if err := db.First(&sess, "token = ?", token).Error; err != nil {
		t.Fatalf("reload: %v", err)
	}
	if sess.ExpiresAt.IsZero() {
		t.Fatal("ExpiresAt not set; token would never expire")
	}
	if d := time.Until(sess.ExpiresAt); d < 50*time.Minute || d > 70*time.Minute {
		t.Fatalf("default TTL ~1h expected, expires in %v", d)
	}
}
