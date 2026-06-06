/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package notify

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type capturedRequest struct {
	Path        string
	Body        string
	AuthHeader  string
	TitleHeader string
	PrioHeader  string
	TagsHeader  string
}

func newFakeNtfy(t *testing.T, capture *capturedRequest) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		capture.Path = r.URL.Path
		capture.Body = string(body)
		capture.AuthHeader = r.Header.Get("Authorization")
		capture.TitleHeader = r.Header.Get("Title")
		capture.PrioHeader = r.Header.Get("Priority")
		capture.TagsHeader = r.Header.Get("Tags")
		w.WriteHeader(http.StatusOK)
	}))
}

func TestClient_Notify_HitsAuditTopic(t *testing.T) {
	cap := &capturedRequest{}
	srv := newFakeNtfy(t, cap)
	defer srv.Close()

	c := NewClient(Config{
		BaseURL: srv.URL, Region: "default",
		AuditToken: "tk_audit",
	})
	if err := c.Notify(context.Background(), Message{Title: "test", Body: "hi"}); err != nil {
		t.Fatal(err)
	}
	if cap.Path != "/grimnir-audit-default" {
		t.Errorf("path = %q", cap.Path)
	}
	if cap.AuthHeader != "Bearer tk_audit" {
		t.Errorf("auth = %q", cap.AuthHeader)
	}
	if cap.Body != "hi" {
		t.Errorf("body = %q", cap.Body)
	}
	if cap.PrioHeader != "3" {
		t.Errorf("priority = %q, want 3 for Notify", cap.PrioHeader)
	}
}

func TestClient_Page_HitsPageTopicAtHighPriority(t *testing.T) {
	cap := &capturedRequest{}
	srv := newFakeNtfy(t, cap)
	defer srv.Close()

	c := NewClient(Config{BaseURL: srv.URL, Region: "default", PageToken: "tk_page"})
	if err := c.Page(context.Background(), Message{Title: "wake up", Body: "engines dead"}); err != nil {
		t.Fatal(err)
	}
	if cap.Path != "/grimnir-region-default-page" {
		t.Errorf("path = %q", cap.Path)
	}
	if cap.PrioHeader != "5" {
		t.Errorf("priority = %q, want 5 (max) for Page", cap.PrioHeader)
	}
	if !strings.Contains(cap.TagsHeader, "rotating_light") {
		t.Errorf("tags missing rotating_light: %q", cap.TagsHeader)
	}
}

func TestClient_PageAndRollback_HitsRollbackTopic(t *testing.T) {
	cap := &capturedRequest{}
	srv := newFakeNtfy(t, cap)
	defer srv.Close()

	c := NewClient(Config{BaseURL: srv.URL, Region: "default", RollbackToken: "tk_roll"})
	if err := c.PageAndRollback(context.Background(), Message{Body: "auto-rollback fired"}); err != nil {
		t.Fatal(err)
	}
	if cap.Path != "/grimnir-region-default-rollback" {
		t.Errorf("path = %q", cap.Path)
	}
	if cap.AuthHeader != "Bearer tk_roll" {
		t.Errorf("auth = %q", cap.AuthHeader)
	}
}

func TestClient_NetworkErrorReturned(t *testing.T) {
	c := NewClient(Config{BaseURL: "http://127.0.0.1:1", Region: "default", PageToken: "tk"})
	c.backoff = []time.Duration{1 * time.Millisecond, 1 * time.Millisecond, 1 * time.Millisecond}
	err := c.Page(context.Background(), Message{Body: "x"})
	if err == nil {
		t.Error("expected network error")
	}
}

func TestClient_Non2xxReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	c := NewClient(Config{BaseURL: srv.URL, Region: "default", PageToken: "tk"})
	err := c.Page(context.Background(), Message{Body: "x"})
	if err == nil {
		t.Error("expected 401 error")
	}
}

func TestClient_RetriesOn5xx(t *testing.T) {
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		if attempts < 3 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	c := NewClient(Config{BaseURL: srv.URL, Region: "default", PageToken: "tk"})
	// Shrink backoff so the test isn't slow.
	c.backoff = []time.Duration{1 * time.Millisecond, 1 * time.Millisecond, 1 * time.Millisecond}
	if err := c.Page(context.Background(), Message{Body: "x"}); err != nil {
		t.Fatalf("expected success after retries: %v", err)
	}
	if attempts != 3 {
		t.Errorf("attempts = %d, want 3", attempts)
	}
}

func TestClient_DoesNotRetryOn4xx(t *testing.T) {
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	c := NewClient(Config{BaseURL: srv.URL, Region: "default", PageToken: "tk"})
	c.backoff = []time.Duration{1 * time.Millisecond, 1 * time.Millisecond, 1 * time.Millisecond}
	_ = c.Page(context.Background(), Message{Body: "x"})
	if attempts != 1 {
		t.Errorf("attempts = %d, want 1 (no retry on 401)", attempts)
	}
}
