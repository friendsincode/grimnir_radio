/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package grimnirfanout

import (
	"bufio"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeAuthenticator records calls and returns a programmable verdict. Chunk 7
// will replace this with a Redis-backed real auth, but the interface stays
// the same so the listener test contract doesn't change.
type fakeAuthenticator struct {
	mu        sync.Mutex
	calls     []fakeAuthCall
	allow     bool
	allowFunc func(mount, user, pass string) (any, error)
}

type fakeAuthCall struct {
	Mount string
	User  string
	Pass  string
}

func (f *fakeAuthenticator) Validate(mount, user, pass string) (any, error) {
	f.mu.Lock()
	f.calls = append(f.calls, fakeAuthCall{mount, user, pass})
	allow := f.allow
	fn := f.allowFunc
	f.mu.Unlock()
	if fn != nil {
		return fn(mount, user, pass)
	}
	if !allow {
		return nil, errors.New("unauthorized")
	}
	return map[string]string{"mount": mount, "user": user}, nil
}

func (f *fakeAuthenticator) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func (f *fakeAuthenticator) lastCall() (fakeAuthCall, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.calls) == 0 {
		return fakeAuthCall{}, false
	}
	return f.calls[len(f.calls)-1], true
}

// stubSessionSink swaps in for the real Pipeline-attaching behaviour during
// unit tests. The listener calls Begin to construct (or fail) a session and
// Bytes(...) to feed each chunk read off the TCP conn; End on disconnect.
type stubSessionSink struct {
	mu          sync.Mutex
	begins      int
	ends        int
	bytes       []byte
	beginErr    error
	beginCalled chan struct{}
	endCalled   chan struct{}
}

func newStubSessionSink() *stubSessionSink {
	return &stubSessionSink{
		beginCalled: make(chan struct{}, 4),
		endCalled:   make(chan struct{}, 4),
	}
}

func (s *stubSessionSink) Begin(sess *Session, mount string) error {
	s.mu.Lock()
	s.begins++
	err := s.beginErr
	s.mu.Unlock()
	select {
	case s.beginCalled <- struct{}{}:
	default:
	}
	return err
}

func (s *stubSessionSink) Bytes(sess *Session, p []byte) error {
	s.mu.Lock()
	s.bytes = append(s.bytes, p...)
	s.mu.Unlock()
	return nil
}

func (s *stubSessionSink) End(sess *Session) {
	s.mu.Lock()
	s.ends++
	s.mu.Unlock()
	select {
	case s.endCalled <- struct{}{}:
	default:
	}
}

func (s *stubSessionSink) snapshot() (int, int, []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := make([]byte, len(s.bytes))
	copy(cp, s.bytes)
	return s.begins, s.ends, cp
}

// startHarborForTest brings up a HarborListener on :0 and returns the bound
// port plus a teardown func.
func startHarborForTest(t *testing.T, auth HarborAuthenticator, sink HarborSessionSink, mgr *SessionMgr) (int, func()) {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := lis.Addr().(*net.TCPAddr).Port

	hl := NewHarborListener(HarborListenerConfig{
		Listener:      lis,
		Auth:          auth,
		Sink:          sink,
		Sessions:      mgr,
		ReadTimeout:   500 * time.Millisecond,
		IdleTimeout:   2 * time.Second,
		MaxHeaderSize: 4096,
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		_ = hl.Serve(ctx)
		close(done)
	}()
	return port, func() {
		cancel()
		_ = lis.Close()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Error("harbor Serve did not exit on cancel within 2s")
		}
	}
}

func basicAuth(user, pass string) string {
	tok := base64.StdEncoding.EncodeToString([]byte(user + ":" + pass))
	return "Basic " + tok
}

// writeHarborRequest writes a SOURCE request and returns the (already-read)
// response status line.
func writeHarborRequest(t *testing.T, conn net.Conn, method, mount, auth string, body []byte) string {
	t.Helper()
	var sb strings.Builder
	fmt.Fprintf(&sb, "%s %s HTTP/1.0\r\n", method, mount)
	if auth != "" {
		fmt.Fprintf(&sb, "Authorization: %s\r\n", auth)
	}
	sb.WriteString("Content-Type: audio/mpeg\r\n")
	sb.WriteString("User-Agent: test-dj/1.0\r\n")
	sb.WriteString("\r\n")
	if _, err := conn.Write([]byte(sb.String())); err != nil {
		t.Fatalf("write request: %v", err)
	}
	if len(body) > 0 {
		if _, err := conn.Write(body); err != nil {
			t.Fatalf("write body: %v", err)
		}
	}
	br := bufio.NewReader(conn)
	line, err := br.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		t.Fatalf("read status: %v", err)
	}
	return strings.TrimRight(line, "\r\n")
}

func TestHarbor_AcceptedSourceCreatesSession(t *testing.T) {
	auth := &fakeAuthenticator{allow: true}
	sink := newStubSessionSink()
	mgr := NewSessionMgr()

	port, stop := startHarborForTest(t, auth, sink, mgr)
	defer stop()

	conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	status := writeHarborRequest(t, conn, "SOURCE", "/live", basicAuth("dj", "secret"), []byte("FAKEMP3PAYLOAD"))
	if !strings.Contains(status, "200") {
		t.Errorf("status = %q, want 200 OK", status)
	}

	// Wait for Begin().
	select {
	case <-sink.beginCalled:
	case <-time.After(1 * time.Second):
		t.Fatal("Begin was not called within 1s")
	}

	if auth.callCount() != 1 {
		t.Errorf("auth call count = %d, want 1", auth.callCount())
	}
	call, _ := auth.lastCall()
	if call.Mount != "/live" || call.User != "dj" || call.Pass != "secret" {
		t.Errorf("auth call = %+v, want mount=/live user=dj pass=secret", call)
	}

	// Session was registered with the manager.
	if got := mgr.CountByProtocol(ProtocolHarbor); got < 1 {
		t.Errorf("CountByProtocol(Harbor) = %d, want >= 1", got)
	}

	// Bytes should have been forwarded.
	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		_, _, b := sink.snapshot()
		if len(b) > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	_, _, b := sink.snapshot()
	if string(b) != "FAKEMP3PAYLOAD" {
		t.Errorf("forwarded bytes = %q, want FAKEMP3PAYLOAD", string(b))
	}

	// Close the conn; sink.End must be called and the session removed.
	conn.Close()
	select {
	case <-sink.endCalled:
	case <-time.After(1 * time.Second):
		t.Fatal("End was not called within 1s of client disconnect")
	}

	// Session count returns to zero shortly after.
	deadline = time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		if mgr.CountByProtocol(ProtocolHarbor) == 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if got := mgr.CountByProtocol(ProtocolHarbor); got != 0 {
		t.Errorf("after disconnect, CountByProtocol(Harbor) = %d, want 0", got)
	}
}

func TestHarbor_RejectsBlankCredentials(t *testing.T) {
	auth := &fakeAuthenticator{allow: false}
	sink := newStubSessionSink()
	mgr := NewSessionMgr()
	port, stop := startHarborForTest(t, auth, sink, mgr)
	defer stop()

	conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	status := writeHarborRequest(t, conn, "SOURCE", "/live", "", nil)
	if !strings.Contains(status, "401") {
		t.Errorf("status = %q, want 401 Unauthorized", status)
	}
	begins, _, _ := sink.snapshot()
	if begins != 0 {
		t.Errorf("Begin called %d times, want 0", begins)
	}
}

func TestHarbor_RejectsAuthFailure(t *testing.T) {
	auth := &fakeAuthenticator{allow: false}
	sink := newStubSessionSink()
	mgr := NewSessionMgr()
	port, stop := startHarborForTest(t, auth, sink, mgr)
	defer stop()

	conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	status := writeHarborRequest(t, conn, "SOURCE", "/live", basicAuth("dj", "wrong"), nil)
	if !strings.Contains(status, "401") {
		t.Errorf("status = %q, want 401 Unauthorized", status)
	}
	if auth.callCount() != 1 {
		t.Errorf("auth call count = %d, want 1", auth.callCount())
	}
	begins, _, _ := sink.snapshot()
	if begins != 0 {
		t.Errorf("Begin called %d times, want 0", begins)
	}
}

func TestHarbor_RejectsBadMethod(t *testing.T) {
	auth := &fakeAuthenticator{allow: true}
	sink := newStubSessionSink()
	mgr := NewSessionMgr()
	port, stop := startHarborForTest(t, auth, sink, mgr)
	defer stop()

	conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	status := writeHarborRequest(t, conn, "GET", "/live", basicAuth("dj", "x"), nil)
	if !strings.Contains(status, "405") && !strings.Contains(status, "400") {
		t.Errorf("status = %q, want 4xx for non-SOURCE method", status)
	}
}

func TestHarbor_AcceptsPutMethod(t *testing.T) {
	// Newer Icecast clients (libshout 2.4+) use PUT instead of SOURCE.
	auth := &fakeAuthenticator{allow: true}
	sink := newStubSessionSink()
	mgr := NewSessionMgr()
	port, stop := startHarborForTest(t, auth, sink, mgr)
	defer stop()

	conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	status := writeHarborRequest(t, conn, "PUT", "/live", basicAuth("dj", "ok"), nil)
	if !strings.Contains(status, "200") {
		t.Errorf("status = %q, want 200 for PUT", status)
	}
	select {
	case <-sink.beginCalled:
	case <-time.After(1 * time.Second):
		t.Fatal("Begin not called for PUT")
	}
}

func TestHarbor_ParseHeadersHandlesBasicAuth(t *testing.T) {
	user, pass, ok := parseBasicAuth("Basic " + base64.StdEncoding.EncodeToString([]byte("dj:hunter2")))
	if !ok {
		t.Fatal("parseBasicAuth: ok = false")
	}
	if user != "dj" || pass != "hunter2" {
		t.Errorf("parseBasicAuth = %q/%q, want dj/hunter2", user, pass)
	}

	if _, _, ok := parseBasicAuth(""); ok {
		t.Error("parseBasicAuth(empty): ok = true, want false")
	}
	if _, _, ok := parseBasicAuth("Bearer abc"); ok {
		t.Error("parseBasicAuth(Bearer): ok = true, want false")
	}
	if _, _, ok := parseBasicAuth("Basic !!!notb64!!!"); ok {
		t.Error("parseBasicAuth(bad b64): ok = true, want false")
	}
}

func TestHarbor_AcceptDjAuthenticatorStub(t *testing.T) {
	// AcceptAllAuthenticator is the placeholder used until Chunk 7. Validate
	// it accepts any non-empty user/pass, rejects empty.
	a := AcceptAllAuthenticator{}
	if _, err := a.Validate("/live", "dj", "x"); err != nil {
		t.Errorf("AcceptAll with creds: err = %v, want nil", err)
	}
	if _, err := a.Validate("/live", "", ""); err == nil {
		t.Error("AcceptAll with empty creds: err = nil, want err")
	}
}
