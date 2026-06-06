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
	"time"
)

// HarborAuthenticator validates the (mount, user, pass) tuple a DJ client sent
// in the SOURCE/PUT request. Returns opaque claims (stored on the Session via
// Session.AuthClaims) when accepted, or an error to reject the connection.
//
// Chunk 7 ships the real Redis-backed authenticator; Chunk 3 only ships
// AcceptAllAuthenticator (any non-empty creds OK) so the rest of the pipeline
// can be exercised end-to-end against a local Mixxx/Butt instance.
type HarborAuthenticator interface {
	Validate(mount, user, pass string) (claims any, err error)
}

// AcceptAllAuthenticator is the placeholder validator used until Chunk 7
// wires the real auth. Rejects only empty user/pass; accepts every other
// combination. Suitable for development & integration tests, not production.
type AcceptAllAuthenticator struct{}

// Validate accepts any non-empty user/pass; returns a tiny claims map with
// the mount so downstream code has something to log.
func (AcceptAllAuthenticator) Validate(mount, user, pass string) (any, error) {
	if user == "" || pass == "" {
		return nil, errors.New("empty credentials")
	}
	return map[string]string{"mount": mount, "user": user}, nil
}

// HarborSessionSink is the policy interface the listener uses to construct &
// feed per-session pipelines. Production wiring (see main.go) builds a
// Pipeline + a decoder subprocess inside Begin; unit tests inject a stub.
//
// Begin runs once after auth; if it returns an error the listener responds
// 500 and closes the conn. Bytes is called for every PCM/encoded chunk read
// off the socket. End runs exactly once per accepted connection regardless
// of how it terminated.
type HarborSessionSink interface {
	Begin(sess *Session, mount string) error
	Bytes(sess *Session, p []byte) error
	End(sess *Session)
}

// HarborListenerConfig wires a HarborListener. Listener is a pre-bound TCP
// listener (so tests can pick :0 and discover the port). Auth + Sink are
// required. Sessions is the manager that owns lifecycle counters & the
// per-protocol totals surfaced via gRPC status.
type HarborListenerConfig struct {
	Listener      net.Listener
	Auth          HarborAuthenticator
	Sink          HarborSessionSink
	Sessions      *SessionMgr
	ReadTimeout   time.Duration // per-read deadline while reading the request
	IdleTimeout   time.Duration // max gap between body bytes before close
	MaxHeaderSize int           // request-line + headers byte cap
}

// HarborListener is the Icecast/Shoutcast SOURCE/PUT acceptor. One listener
// per fanout binary; each accepted conn becomes one Session.
type HarborListener struct {
	cfg HarborListenerConfig
}

// NewHarborListener wires defaults and returns a listener ready for Serve.
// Doesn't accept connections until Serve is called.
func NewHarborListener(cfg HarborListenerConfig) *HarborListener {
	if cfg.ReadTimeout == 0 {
		cfg.ReadTimeout = 10 * time.Second
	}
	if cfg.IdleTimeout == 0 {
		cfg.IdleTimeout = 30 * time.Second
	}
	if cfg.MaxHeaderSize == 0 {
		cfg.MaxHeaderSize = 8192
	}
	return &HarborListener{cfg: cfg}
}

// Serve accepts connections until ctx is cancelled or the listener errors.
// Each accepted conn runs in its own goroutine; Serve returns after the
// listener stops, but in-flight session handlers keep running until their
// connections close.
func (h *HarborListener) Serve(ctx context.Context) error {
	if h.cfg.Listener == nil {
		return errors.New("harbor: Listener required")
	}
	if h.cfg.Auth == nil {
		return errors.New("harbor: Auth required")
	}
	if h.cfg.Sink == nil {
		return errors.New("harbor: Sink required")
	}
	if h.cfg.Sessions == nil {
		return errors.New("harbor: Sessions required")
	}

	// Close the listener when ctx cancels; that unblocks Accept.
	var closeOnce sync.Once
	go func() {
		<-ctx.Done()
		closeOnce.Do(func() { _ = h.cfg.Listener.Close() })
	}()

	var wg sync.WaitGroup
	for {
		conn, err := h.cfg.Listener.Accept()
		if err != nil {
			// Listener closed; stop accepting. In-flight conns keep running
			// — we wait on the WaitGroup before returning.
			wg.Wait()
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		wg.Add(1)
		go func(c net.Conn) {
			defer wg.Done()
			h.handleConn(ctx, c)
		}(conn)
	}
}

// handleConn parses the request line + headers, authenticates, then loops
// reading body bytes until the peer closes (or an idle timeout fires).
func (h *HarborListener) handleConn(ctx context.Context, conn net.Conn) {
	defer conn.Close()

	if err := conn.SetReadDeadline(time.Now().Add(h.cfg.ReadTimeout)); err != nil {
		return
	}
	br := bufio.NewReaderSize(conn, h.cfg.MaxHeaderSize)

	method, mount, headers, err := readRequestHead(br, h.cfg.MaxHeaderSize)
	if err != nil {
		writeStatus(conn, 400, "Bad Request")
		return
	}
	switch strings.ToUpper(method) {
	case "SOURCE", "PUT":
		// OK
	default:
		writeStatus(conn, 405, "Method Not Allowed")
		return
	}

	authHdr := headers["authorization"]
	user, pass, ok := parseBasicAuth(authHdr)
	if !ok {
		writeStatus(conn, 401, "Unauthorized")
		return
	}
	claims, err := h.cfg.Auth.Validate(mount, user, pass)
	if err != nil {
		writeStatus(conn, 401, "Unauthorized")
		return
	}

	sess := h.cfg.Sessions.Create(ProtocolHarbor)
	sess.AuthClaims = claims
	_ = sess.transitionTo(SessionAuthenticating)

	if err := h.cfg.Sink.Begin(sess, mount); err != nil {
		writeStatus(conn, 500, "Internal Server Error")
		_ = sess.transitionTo(SessionEnded)
		h.cfg.Sessions.Remove(sess.ID)
		return
	}
	_ = sess.transitionTo(SessionActive)

	// 200 OK so Icecast clients (libshout) keep the connection open and
	// start streaming body bytes.
	writeStatus(conn, 200, "OK")

	defer func() {
		h.cfg.Sink.End(sess)
		_ = sess.transitionTo(SessionEnded)
		h.cfg.Sessions.Remove(sess.ID)
	}()

	// Stream loop: read what bufio buffered first (the body bytes that came
	// in the same packet as the headers), then pump conn directly.
	buf := make([]byte, 4096)
	for {
		if ctx.Err() != nil {
			return
		}
		if err := conn.SetReadDeadline(time.Now().Add(h.cfg.IdleTimeout)); err != nil {
			return
		}
		n, err := br.Read(buf)
		if n > 0 {
			sess.recordBytesIn(uint64(n))
			sess.markPacket(time.Now())
			if err := h.cfg.Sink.Bytes(sess, buf[:n]); err != nil {
				return
			}
		}
		if err != nil {
			// io.EOF, conn reset, idle timeout — all terminal.
			return
		}
	}
}

// readRequestHead reads "METHOD PATH HTTP/x.y\r\n" + headers, terminated by
// a blank line. Returns method, mount path, and a lower-cased header map.
// Caps total bytes at maxSize so a malicious client can't OOM the listener.
func readRequestHead(br *bufio.Reader, maxSize int) (method, mount string, headers map[string]string, err error) {
	headers = make(map[string]string)
	total := 0

	line, err := br.ReadString('\n')
	if err != nil {
		return "", "", nil, fmt.Errorf("read request line: %w", err)
	}
	total += len(line)
	if total > maxSize {
		return "", "", nil, errors.New("request line too long")
	}
	parts := strings.Fields(strings.TrimRight(line, "\r\n"))
	if len(parts) < 2 {
		return "", "", nil, errors.New("malformed request line")
	}
	method = parts[0]
	mount = parts[1]

	for {
		hl, err := br.ReadString('\n')
		if err != nil {
			return "", "", nil, fmt.Errorf("read header: %w", err)
		}
		total += len(hl)
		if total > maxSize {
			return "", "", nil, errors.New("headers too large")
		}
		trimmed := strings.TrimRight(hl, "\r\n")
		if trimmed == "" {
			break // end of headers
		}
		idx := strings.IndexByte(trimmed, ':')
		if idx <= 0 {
			continue // skip malformed header line
		}
		k := strings.ToLower(strings.TrimSpace(trimmed[:idx]))
		v := strings.TrimSpace(trimmed[idx+1:])
		headers[k] = v
	}
	return method, mount, headers, nil
}

// parseBasicAuth pulls (user, pass) out of an "Authorization: Basic <b64>"
// header value. Returns ok=false for an empty header, a non-Basic scheme,
// invalid base64, or missing colon.
func parseBasicAuth(h string) (user, pass string, ok bool) {
	if h == "" {
		return "", "", false
	}
	const prefix = "Basic "
	if !strings.HasPrefix(h, prefix) && !strings.HasPrefix(h, "basic ") {
		return "", "", false
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(h[len(prefix):]))
	if err != nil {
		return "", "", false
	}
	s := string(decoded)
	idx := strings.IndexByte(s, ':')
	if idx < 0 {
		return "", "", false
	}
	return s[:idx], s[idx+1:], true
}

// writeStatus sends "HTTP/1.0 <code> <text>\r\n\r\n". Errors are ignored;
// the caller is on the way to closing the conn anyway.
func writeStatus(w io.Writer, code int, text string) {
	fmt.Fprintf(w, "HTTP/1.0 %d %s\r\n\r\n", code, text)
}
