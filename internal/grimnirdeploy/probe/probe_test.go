/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package probe

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// startHTTP starts a test server on an ephemeral port, returns host+port.
func startHTTP(t *testing.T, handler http.Handler) (string, int) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	u := strings.TrimPrefix(srv.URL, "http://")
	host, portStr, _ := net.SplitHostPort(u)
	port, _ := strconv.Atoi(portStr)
	return host, port
}

func TestProbeControlPlaneOK(t *testing.T) {
	host, port := startHTTP(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	p := NewProber()
	p.ControlPlanePort = port
	if err := p.probeControlPlane(context.Background(), host); err != nil {
		t.Errorf("probeControlPlane: %v", err)
	}
}

func TestProbeControlPlane503(t *testing.T) {
	host, port := startHTTP(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "down", 503)
	}))
	p := NewProber()
	p.ControlPlanePort = port
	if err := p.probeControlPlane(context.Background(), host); err == nil {
		t.Error("expected 503 error")
	}
}
