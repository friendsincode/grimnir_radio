/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package harbor

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParseBasicAuth(t *testing.T) {
	s := &Server{}

	tests := []struct {
		name      string
		authHdr   string
		wantToken string
		wantOK    bool
	}{
		{
			name:      "valid basic auth",
			authHdr:   "Basic " + base64.StdEncoding.EncodeToString([]byte("source:my-secret-token")),
			wantToken: "my-secret-token",
			wantOK:    true,
		},
		{
			name:      "valid with empty username",
			authHdr:   "Basic " + base64.StdEncoding.EncodeToString([]byte(":token123")),
			wantToken: "token123",
			wantOK:    true,
		},
		{
			name:      "valid hex token",
			authHdr:   "Basic " + base64.StdEncoding.EncodeToString([]byte("source:a1b2c3d4e5f6")),
			wantToken: "a1b2c3d4e5f6",
			wantOK:    true,
		},
		{
			name:    "missing header",
			authHdr: "",
			wantOK:  false,
		},
		{
			name:    "wrong scheme",
			authHdr: "Bearer some-token",
			wantOK:  false,
		},
		{
			name:    "invalid base64",
			authHdr: "Basic not-valid-base64!!!",
			wantOK:  false,
		},
		{
			name:    "no colon separator",
			authHdr: "Basic " + base64.StdEncoding.EncodeToString([]byte("no-colon-here")),
			wantOK:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPut, "/live.mp3", nil)
			if tt.authHdr != "" {
				r.Header.Set("Authorization", tt.authHdr)
			}

			token, ok := s.parseBasicAuth(r)
			if ok != tt.wantOK {
				t.Errorf("parseBasicAuth() ok = %v, want %v", ok, tt.wantOK)
			}
			if ok && token != tt.wantToken {
				t.Errorf("parseBasicAuth() token = %q, want %q", token, tt.wantToken)
			}
		})
	}
}

func TestParseIceHeaders(t *testing.T) {
	r := httptest.NewRequest(http.MethodPut, "/live.mp3", nil)
	r.Header.Set("Ice-Name", "My Show")
	r.Header.Set("Ice-Description", "Best show ever")
	r.Header.Set("Ice-Genre", "Rock")
	r.Header.Set("Ice-Bitrate", "128")
	r.Header.Set("Content-Type", "audio/mpeg")
	r.Header.Set("User-Agent", "BUTT/0.1.34")

	meta := parseIceHeaders(r)

	expected := map[string]string{
		"Ice-Name":        "My Show",
		"Ice-Description": "Best show ever",
		"Ice-Genre":       "Rock",
		"Ice-Bitrate":     "128",
		"Content-Type":    "audio/mpeg",
		"User-Agent":      "BUTT/0.1.34",
	}

	for key, want := range expected {
		if got := meta[key]; got != want {
			t.Errorf("parseIceHeaders()[%q] = %q, want %q", key, got, want)
		}
	}

	// Verify no extra headers are included.
	if len(meta) != len(expected) {
		t.Errorf("parseIceHeaders() returned %d headers, want %d", len(meta), len(expected))
	}
}

func TestParseIceHeaders_Empty(t *testing.T) {
	r := httptest.NewRequest(http.MethodPut, "/live.mp3", nil)
	meta := parseIceHeaders(r)
	if len(meta) != 0 {
		t.Errorf("parseIceHeaders() returned %d headers for empty request, want 0", len(meta))
	}
}

func TestHandleSource_MethodNotAllowed(t *testing.T) {
	s := &Server{
		conns: make(map[string]*SourceConnection),
		cfg:   Config{MaxSources: 10},
	}

	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodDelete, http.MethodPatch} {
		t.Run(method, func(t *testing.T) {
			r := httptest.NewRequest(method, "/live.mp3", nil)
			w := httptest.NewRecorder()
			s.handleSource(w, r)
			if w.Code != http.StatusMethodNotAllowed {
				t.Errorf("handleSource(%s) status = %d, want %d", method, w.Code, http.StatusMethodNotAllowed)
			}
		})
	}
}

func TestHandleSource_NoAuth(t *testing.T) {
	s := &Server{
		conns: make(map[string]*SourceConnection),
		cfg:   Config{MaxSources: 10},
	}

	r := httptest.NewRequest(http.MethodPut, "/live.mp3", nil)
	w := httptest.NewRecorder()
	s.handleSource(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("handleSource() no auth status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
	if w.Header().Get("WWW-Authenticate") == "" {
		t.Error("handleSource() should set WWW-Authenticate header")
	}
}

func TestHandleSource_MaxSources(t *testing.T) {
	s := &Server{
		conns: make(map[string]*SourceConnection),
		cfg:   Config{MaxSources: 1},
	}
	// Fill up sources.
	s.conns["existing"] = &SourceConnection{SessionID: "existing"}

	r := httptest.NewRequest(http.MethodPut, "/live.mp3", nil)
	r.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("source:token")))
	w := httptest.NewRecorder()
	s.handleSource(w, r)
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("handleSource() max sources status = %d, want %d", w.Code, http.StatusServiceUnavailable)
	}
}

func TestHandleSource_EmptyMount(t *testing.T) {
	s := &Server{
		conns: make(map[string]*SourceConnection),
		cfg:   Config{MaxSources: 10},
	}

	r := httptest.NewRequest(http.MethodPut, "/", nil)
	r.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("source:token")))
	w := httptest.NewRecorder()
	s.handleSource(w, r)
	if w.Code != http.StatusBadRequest {
		t.Errorf("handleSource() empty mount status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestActiveConnections(t *testing.T) {
	s := &Server{
		conns: make(map[string]*SourceConnection),
	}

	if got := s.ActiveConnections(); got != 0 {
		t.Errorf("ActiveConnections() = %d, want 0", got)
	}

	s.conns["a"] = &SourceConnection{SessionID: "a"}
	s.conns["b"] = &SourceConnection{SessionID: "b"}

	if got := s.ActiveConnections(); got != 2 {
		t.Errorf("ActiveConnections() = %d, want 2", got)
	}
}

func TestAddr(t *testing.T) {
	s := &Server{
		cfg: Config{Bind: "0.0.0.0", Port: 8088},
	}
	if got := s.Addr(); got != "0.0.0.0:8088" {
		t.Errorf("Addr() = %q, want %q", got, "0.0.0.0:8088")
	}
}
