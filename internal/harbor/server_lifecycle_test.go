/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package harbor

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

// freePort reserves an ephemeral port and releases it, returning the number so
// a server can bind it deterministically (tests need the address to dial).
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// dialUntilUp retries a TCP dial until the server is accepting or the deadline
// passes, so the lifecycle tests don't race the listener goroutine's startup.
func dialUntilUp(t *testing.T, addr string) net.Conn {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		conn, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if err == nil {
			return conn
		}
		if time.Now().After(deadline) {
			t.Fatalf("server never came up at %s: %v", addr, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestNewServer_Initializes(t *testing.T) {
	cfg := Config{Bind: "127.0.0.1", Port: 8000, MaxSources: 3}
	s := NewServer(cfg, nil, nil, nil, nil, zerolog.Nop())

	if s == nil {
		t.Fatal("NewServer returned nil")
	}
	if s.conns == nil {
		t.Fatal("conns map not initialized")
	}
	if got := s.ActiveConnections(); got != 0 {
		t.Fatalf("ActiveConnections on fresh server = %d, want 0", got)
	}
	if s.cfg.MaxSources != 3 {
		t.Fatalf("cfg not stored: MaxSources = %d, want 3", s.cfg.MaxSources)
	}
}

// TestServer_ListenAndServeWithSOURCE_Lifecycle drives the production start
// path: a real listener, a legacy SOURCE request rewritten to PUT, and a clean
// Shutdown. No auth is supplied, so handleSource rejects at 401 — enough to
// exercise Accept, the SOURCE->PUT rewrite over a real connection, and the
// handler entry, without needing a wired live service or decoder.
func TestServer_ListenAndServeWithSOURCE_Lifecycle(t *testing.T) {
	port := freePort(t)
	s := NewServer(Config{Bind: "127.0.0.1", Port: port, MaxSources: 1}, nil, nil, nil, nil, zerolog.Nop())

	serveErr := make(chan error, 1)
	go func() { serveErr <- s.ListenAndServeWithSOURCE() }()

	addr := fmt.Sprintf("127.0.0.1:%d", port)
	conn := dialUntilUp(t, addr)
	defer conn.Close()

	// Legacy Icecast SOURCE request; sourceMethodConn should rewrite it to PUT.
	fmt.Fprintf(conn, "SOURCE /live.mp3 HTTP/1.1\r\nHost: %s\r\n\r\n", addr)

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	resp, err := http.ReadResponse(bufio.NewReader(conn), &http.Request{Method: http.MethodPut})
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("SOURCE without auth: status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}

	if err := s.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	select {
	case err := <-serveErr:
		if err != nil {
			t.Fatalf("ListenAndServeWithSOURCE returned %v, want nil after Shutdown", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("serve goroutine did not return after Shutdown")
	}
}

// TestServer_ListenAndServe_StartAndShutdown covers the PUT-only start path and
// a Shutdown before any listener was created is a no-op error-wise.
func TestServer_ListenAndServe_StartAndShutdown(t *testing.T) {
	port := freePort(t)
	s := NewServer(Config{Bind: "127.0.0.1", Port: port, MaxSources: 1}, nil, nil, nil, nil, zerolog.Nop())

	serveErr := make(chan error, 1)
	go func() { serveErr <- s.ListenAndServe() }()

	conn := dialUntilUp(t, fmt.Sprintf("127.0.0.1:%d", port))
	conn.Close()

	if err := s.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	select {
	case err := <-serveErr:
		if err != nil {
			t.Fatalf("ListenAndServe returned %v, want nil after Shutdown", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("serve goroutine did not return after Shutdown")
	}
}

func TestServer_Shutdown_CancelsActiveConnections(t *testing.T) {
	s := &Server{conns: make(map[string]*SourceConnection)}

	cancelled := make(chan struct{})
	s.conns["sess-1"] = &SourceConnection{
		SessionID: "sess-1",
		cancel:    func() { close(cancelled) },
	}

	// httpServer is nil here (server never started); Shutdown must still cancel
	// the live connection and return nil rather than dereferencing a nil server.
	if err := s.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown with nil httpServer: %v", err)
	}
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("active connection was not cancelled on Shutdown")
	}
}
