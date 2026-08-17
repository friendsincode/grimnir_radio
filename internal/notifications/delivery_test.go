/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package notifications

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/friendsincode/grimnir_radio/internal/dbtest"
	"github.com/friendsincode/grimnir_radio/internal/models"
)

func TestSendWebhook_Success(t *testing.T) {
	svc, _ := newNotifService(t, Config{})
	var gotBody bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody = r.Header.Get("Content-Type") == "application/json"
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	if err := svc.sendWebhook(bg(), srv.URL, map[string]any{"hello": "world"}); err != nil {
		t.Fatalf("sendWebhook: %v", err)
	}
	if !gotBody {
		t.Fatal("webhook did not receive a JSON POST")
	}
}

func TestSendWebhook_Non2xx(t *testing.T) {
	svc, _ := newNotifService(t, Config{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	if err := svc.sendWebhook(bg(), srv.URL, map[string]any{}); err == nil {
		t.Fatal("expected error for 500 response")
	}
}

func TestSendWebhook_BadURL(t *testing.T) {
	svc, _ := newNotifService(t, Config{})
	if err := svc.sendWebhook(bg(), "http://127.0.0.1:1", map[string]any{}); err == nil {
		t.Fatal("expected error for unreachable webhook")
	}
}

func TestSendSMS_ConfiguredDelivers(t *testing.T) {
	svc, _ := newNotifService(t, Config{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	t.Setenv("GRIMNIR_SMS_WEBHOOK_URL", srv.URL)

	n := &models.Notification{Subject: "s", Body: "b", Metadata: map[string]any{"phone": "+15550001111"}}
	if err := svc.sendSMS(bg(), n, &models.User{ID: dbtest.UUID("u1")}); err != nil {
		t.Fatalf("sendSMS: %v", err)
	}
	if n.Status != models.NotificationStatusSent {
		t.Fatalf("status = %q, want sent", n.Status)
	}
}

func TestSendSMS_Rejections(t *testing.T) {
	svc, _ := newNotifService(t, Config{})
	n := &models.Notification{Metadata: map[string]any{"phone": "+1"}}

	if err := svc.sendSMS(bg(), n, nil); err == nil {
		t.Fatal("expected error for nil user")
	}
	// No webhook configured.
	if err := svc.sendSMS(bg(), n, &models.User{ID: dbtest.UUID("u1")}); err == nil {
		t.Fatal("expected error when SMS webhook unconfigured")
	}
	// Configured but no destination in metadata.
	t.Setenv("GRIMNIR_SMS_WEBHOOK_URL", "http://example.invalid")
	if err := svc.sendSMS(bg(), &models.Notification{}, &models.User{ID: dbtest.UUID("u1")}); err == nil {
		t.Fatal("expected error for missing sms destination")
	}
}

func TestSendPush_ConfiguredDelivers(t *testing.T) {
	svc, _ := newNotifService(t, Config{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	t.Setenv("GRIMNIR_PUSH_WEBHOOK_URL", srv.URL)

	n := &models.Notification{Subject: "s", Body: "b", Metadata: map[string]any{"device_token": "tok-123"}}
	if err := svc.sendPush(bg(), n, &models.User{ID: dbtest.UUID("u1")}); err != nil {
		t.Fatalf("sendPush: %v", err)
	}
	if n.Status != models.NotificationStatusSent {
		t.Fatalf("status = %q, want sent", n.Status)
	}
}

func TestSendPush_Rejections(t *testing.T) {
	svc, _ := newNotifService(t, Config{})
	if err := svc.sendPush(bg(), &models.Notification{}, nil); err == nil {
		t.Fatal("expected error for nil user")
	}
	if err := svc.sendPush(bg(), &models.Notification{}, &models.User{ID: dbtest.UUID("u1")}); err == nil {
		t.Fatal("expected error when push webhook unconfigured")
	}
	t.Setenv("GRIMNIR_PUSH_WEBHOOK_URL", "http://example.invalid")
	if err := svc.sendPush(bg(), &models.Notification{}, &models.User{ID: dbtest.UUID("u1")}); err == nil {
		t.Fatal("expected error for missing device token")
	}
}
