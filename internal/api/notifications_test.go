/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/auth"
	"github.com/friendsincode/grimnir_radio/internal/events"
	"github.com/friendsincode/grimnir_radio/internal/models"
	"github.com/friendsincode/grimnir_radio/internal/notifications"
)

func newNotificationAPITest(t *testing.T) (*NotificationAPI, *gorm.DB) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "test.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(
		&models.Notification{},
		&models.NotificationPreference{},
		&models.User{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	bus := events.NewBus()
	svc := notifications.NewService(db, bus, notifications.Config{}, zerolog.Nop())
	return NewNotificationAPI(svc), db
}

func withUserClaims(req *http.Request, userID string) *http.Request {
	return req.WithContext(auth.WithClaims(req.Context(), &auth.Claims{
		UserID: userID,
		Roles:  []string{},
	}))
}

func TestNotificationAPI_Unauthorized(t *testing.T) {
	n, _ := newNotificationAPITest(t)

	handlers := []struct {
		name    string
		handler http.HandlerFunc
		method  string
	}{
		{"list", n.handleList, "GET"},
		{"unread-count", n.handleUnreadCount, "GET"},
		{"mark-read", n.handleMarkRead, "POST"},
		{"mark-all-read", n.handleMarkAllRead, "POST"},
		{"get-preferences", n.handleGetPreferences, "GET"},
		{"update-preference", n.handleUpdatePreference, "PUT"},
	}

	for _, h := range handlers {
		t.Run(h.name, func(t *testing.T) {
			req := httptest.NewRequest(h.method, "/", nil)
			rr := httptest.NewRecorder()
			h.handler(rr, req)
			if rr.Code != http.StatusUnauthorized {
				t.Fatalf("%s without auth: got %d, want 401", h.name, rr.Code)
			}
		})
	}
}

func TestNotificationAPI_List(t *testing.T) {
	n, _ := newNotificationAPITest(t)

	req := httptest.NewRequest("GET", "/", nil)
	req = withUserClaims(req, "u1")
	rr := httptest.NewRecorder()
	n.handleList(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("list: got %d, want 200", rr.Code)
	}
	var resp map[string]any
	json.NewDecoder(rr.Body).Decode(&resp) //nolint:errcheck
	if _, ok := resp["notifications"]; !ok {
		t.Fatal("expected notifications key")
	}
	if _, ok := resp["total"]; !ok {
		t.Fatal("expected total key")
	}

	// With unread_only and limit params
	req = httptest.NewRequest("GET", "/?unread_only=true&limit=10", nil)
	req = withUserClaims(req, "u1")
	rr = httptest.NewRecorder()
	n.handleList(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("list with params: got %d, want 200", rr.Code)
	}
}

func TestNotificationAPI_UnreadCount(t *testing.T) {
	n, _ := newNotificationAPITest(t)

	req := httptest.NewRequest("GET", "/", nil)
	req = withUserClaims(req, "u1")
	rr := httptest.NewRecorder()
	n.handleUnreadCount(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("unread count: got %d, want 200", rr.Code)
	}
	var resp map[string]any
	json.NewDecoder(rr.Body).Decode(&resp) //nolint:errcheck
	if _, ok := resp["unread_count"]; !ok {
		t.Fatal("expected unread_count key")
	}
}

func TestNotificationAPI_MarkRead(t *testing.T) {
	n, _ := newNotificationAPITest(t)

	// Mark non-existent → 404
	req := httptest.NewRequest("POST", "/nonexistent/read", nil)
	req = withUserClaims(req, "u1")
	req = withChiParam(req, "id", "nonexistent-id")
	rr := httptest.NewRecorder()
	n.handleMarkRead(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("mark read non-existent: got %d, want 404", rr.Code)
	}
}

func TestNotificationAPI_MarkAllRead(t *testing.T) {
	n, _ := newNotificationAPITest(t)

	req := httptest.NewRequest("POST", "/mark-all-read", nil)
	req = withUserClaims(req, "u1")
	rr := httptest.NewRecorder()
	n.handleMarkAllRead(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("mark all read: got %d, want 200", rr.Code)
	}
}

func TestNotificationAPI_Preferences(t *testing.T) {
	n, _ := newNotificationAPITest(t)

	// Get preferences (creates defaults)
	req := httptest.NewRequest("GET", "/preferences", nil)
	req = withUserClaims(req, "u1")
	rr := httptest.NewRecorder()
	n.handleGetPreferences(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("get preferences: got %d, want 200, body=%s", rr.Code, rr.Body.String())
	}
	var resp map[string]any
	json.NewDecoder(rr.Body).Decode(&resp) //nolint:errcheck
	if _, ok := resp["preferences"]; !ok {
		t.Fatal("expected preferences key")
	}

	// Update non-existent preference → 404
	body, _ := json.Marshal(map[string]any{"enabled": true})
	req = httptest.NewRequest("PUT", "/preferences/nonexistent", bytes.NewReader(body))
	req = withUserClaims(req, "u1")
	req = withChiParam(req, "id", "nonexistent-pref-id")
	rr = httptest.NewRecorder()
	n.handleUpdatePreference(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("update non-existent pref: got %d, want 404", rr.Code)
	}

	// Update with invalid JSON → 400
	req = httptest.NewRequest("PUT", "/preferences/x", bytes.NewReader([]byte("invalid")))
	req = withUserClaims(req, "u1")
	req = withChiParam(req, "id", "x")
	rr = httptest.NewRecorder()
	n.handleUpdatePreference(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("update pref invalid json: got %d, want 400", rr.Code)
	}
}
