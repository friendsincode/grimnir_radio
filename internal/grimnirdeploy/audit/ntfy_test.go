/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package audit

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNtfyPosterSendsExpectedPayload(t *testing.T) {
	var gotPath string
	var gotAuth string
	var gotCT string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotCT = r.Header.Get("Content-Type")
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	p := NewNtfyPoster(srv.URL, "tk_secret")
	err := p.Post(context.Background(), "grimnir-audit-us-east", "deploy started", "alice ran deploy v1.2.3", PriorityDefault)
	if err != nil {
		t.Fatalf("Post: %v", err)
	}
	if gotPath != "/" {
		t.Errorf("path = %q, want /", gotPath)
	}
	if gotAuth != "Bearer tk_secret" {
		t.Errorf("Authorization = %q, want Bearer tk_secret", gotAuth)
	}
	if gotCT != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", gotCT)
	}
	if gotBody["topic"] != "grimnir-audit-us-east" {
		t.Errorf("topic = %v", gotBody["topic"])
	}
	if gotBody["title"] != "deploy started" {
		t.Errorf("title = %v", gotBody["title"])
	}
	if !strings.Contains(gotBody["message"].(string), "v1.2.3") {
		t.Errorf("message missing tag: %v", gotBody["message"])
	}
}

func TestNtfyPosterNoAuthWhenTokenEmpty(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	p := NewNtfyPoster(srv.URL, "")
	if err := p.Post(context.Background(), "t", "title", "msg", PriorityDefault); err != nil {
		t.Fatalf("Post: %v", err)
	}
	if gotAuth != "" {
		t.Errorf("Authorization sent when no token: %q", gotAuth)
	}
}

func TestNtfyPosterReturnsErrorOnNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	p := NewNtfyPoster(srv.URL, "")
	if err := p.Post(context.Background(), "t", "title", "msg", PriorityDefault); err == nil {
		t.Fatal("expected error on 500")
	}
}

func TestNtfyPosterImplementsPosterInterface(t *testing.T) {
	// Compile-time check that *NtfyPoster satisfies the Poster interface so
	// callers can swap in fakes during tests.
	var _ Poster = (*NtfyPoster)(nil)
}
