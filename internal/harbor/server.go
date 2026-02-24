/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package harbor

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/events"
	"github.com/friendsincode/grimnir_radio/internal/live"
	"github.com/friendsincode/grimnir_radio/internal/models"
	"github.com/friendsincode/grimnir_radio/internal/playout"
)

// SourceConnection tracks an active source connection.
type SourceConnection struct {
	SessionID   string
	StationID   string
	MountID     string
	MountName   string
	ConnectedAt time.Time
	Metadata    map[string]string
	cancel      context.CancelFunc
}

// Config holds harbor-specific configuration.
type Config struct {
	Bind         string
	Port         int
	MaxSources   int
	GStreamerBin string
}

// Server is the built-in Icecast source receiver ("harbor").
// It accepts PUT and SOURCE method connections from BUTT, Mixxx, and other
// Icecast-compatible streaming software, decodes audio to raw PCM, and
// injects it into the playout encoder pipeline.
type Server struct {
	cfg      Config
	db       *gorm.DB
	liveSvc  *live.Service
	director *playout.Director
	bus      *events.Bus
	logger   zerolog.Logger

	httpServer *http.Server

	mu    sync.Mutex
	conns map[string]*SourceConnection // sessionID -> connection
}

// NewServer creates a new harbor server.
func NewServer(cfg Config, db *gorm.DB, liveSvc *live.Service, director *playout.Director, bus *events.Bus, logger zerolog.Logger) *Server {
	return &Server{
		cfg:      cfg,
		db:       db,
		liveSvc:  liveSvc,
		director: director,
		bus:      bus,
		logger:   logger.With().Str("component", "harbor").Logger(),
		conns:    make(map[string]*SourceConnection),
	}
}

// ListenAndServe starts the harbor HTTP server.
func (s *Server) ListenAndServe() error {
	addr := fmt.Sprintf("%s:%d", s.cfg.Bind, s.cfg.Port)

	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleSource)

	s.httpServer = &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		// No read/write timeout — source connections stream indefinitely.
		ReadTimeout:  0,
		WriteTimeout: 0,
		IdleTimeout:  60 * time.Second,
		// Accept the non-standard SOURCE method used by legacy Icecast clients.
		ConnState: func(conn net.Conn, state http.ConnState) {},
	}

	s.logger.Info().Str("addr", addr).Msg("harbor server starting")
	err := s.httpServer.ListenAndServe()
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

// Shutdown gracefully stops the harbor and disconnects all sources.
func (s *Server) Shutdown(ctx context.Context) error {
	s.logger.Info().Msg("harbor server shutting down")

	// Cancel all active source connections.
	s.mu.Lock()
	for _, conn := range s.conns {
		if conn.cancel != nil {
			conn.cancel()
		}
	}
	s.mu.Unlock()

	if s.httpServer != nil {
		return s.httpServer.Shutdown(ctx)
	}
	return nil
}

// ActiveConnections returns the number of active source connections.
func (s *Server) ActiveConnections() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.conns)
}

// handleSource is the main HTTP handler for incoming source connections.
func (s *Server) handleSource(w http.ResponseWriter, r *http.Request) {
	method := r.Method

	// Accept PUT only. Legacy SOURCE method is rewritten to PUT at the
	// connection level by sourceMethodConn (see ListenAndServeWithSOURCE).
	if method != http.MethodPut {
		http.Error(w, "Method not allowed. Use PUT.", http.StatusMethodNotAllowed)
		return
	}

	// Check max sources.
	s.mu.Lock()
	if len(s.conns) >= s.cfg.MaxSources {
		s.mu.Unlock()
		s.logger.Warn().Int("max", s.cfg.MaxSources).Msg("max sources reached, rejecting connection")
		http.Error(w, "Too many sources", http.StatusServiceUnavailable)
		return
	}
	s.mu.Unlock()

	// Parse Basic auth: username is ignored ("source"), password is the token.
	token, ok := s.parseBasicAuth(r)
	if !ok {
		w.Header().Set("WWW-Authenticate", `Basic realm="Grimnir Harbor"`)
		http.Error(w, "Authorization required", http.StatusUnauthorized)
		return
	}

	// Resolve mount from URL path (e.g., "/live.mp3" -> mount name "live.mp3").
	mountPath := strings.TrimPrefix(r.URL.Path, "/")
	if mountPath == "" {
		http.Error(w, "Mount path required", http.StatusBadRequest)
		return
	}

	// Look up mount by name.
	var mount models.Mount
	if err := s.db.Where("name = ?", mountPath).First(&mount).Error; err != nil {
		// Try without extension (e.g., "live.mp3" -> "live").
		baseName := mountPath
		if idx := strings.LastIndex(mountPath, "."); idx > 0 {
			baseName = mountPath[:idx]
		}
		if err2 := s.db.Where("name = ?", baseName).First(&mount).Error; err2 != nil {
			s.logger.Warn().Str("mount", mountPath).Msg("mount not found")
			http.Error(w, "Mount not found", http.StatusNotFound)
			return
		}
	}

	// Validate and consume the token before connecting.
	if _, err := s.liveSvc.AuthorizeSource(r.Context(), mount.StationID, mount.ID, token); err != nil {
		s.logger.Warn().Err(err).Str("mount", mountPath).Msg("harbor token validation failed")
		w.Header().Set("WWW-Authenticate", `Basic realm="Grimnir Harbor"`)
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Activate the session and trigger priority handover.
	sourceIP, _, _ := net.SplitHostPort(r.RemoteAddr)
	session, err := s.liveSvc.HandleConnect(r.Context(), live.ConnectRequest{
		StationID: mount.StationID,
		MountID:   mount.ID,
		Token:     token,
		SourceIP:  sourceIP,
		UserAgent: r.Header.Get("User-Agent"),
	})
	if err != nil {
		s.logger.Warn().Err(err).Str("mount", mountPath).Msg("harbor auth failed")
		w.Header().Set("WWW-Authenticate", `Basic realm="Grimnir Harbor"`)
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	meta := parseIceHeaders(r)
	contentType := r.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "audio/mpeg"
	}

	s.logger.Info().
		Str("session_id", session.ID).
		Str("station_id", mount.StationID).
		Str("mount", mountPath).
		Str("username", session.Username).
		Str("content_type", contentType).
		Str("remote_addr", r.RemoteAddr).
		Int64("content_length", r.ContentLength).
		Str("transfer_encoding", fmt.Sprintf("%v", r.TransferEncoding)).
		Str("proto", r.Proto).
		Msg("harbor source connected")

	// Create connection context. Use Background instead of r.Context() because
	// we will hijack the connection below, which causes r.Context() to cancel.
	connCtx, connCancel := context.WithCancel(context.Background())

	conn := &SourceConnection{
		SessionID:   session.ID,
		StationID:   mount.StationID,
		MountID:     mount.ID,
		MountName:   mountPath,
		ConnectedAt: time.Now(),
		Metadata:    meta,
		cancel:      connCancel,
	}

	s.mu.Lock()
	s.conns[session.ID] = conn
	s.mu.Unlock()

	// Cleanup on disconnect.
	defer func() {
		connCancel()
		s.mu.Lock()
		delete(s.conns, session.ID)
		s.mu.Unlock()

		if err := s.liveSvc.HandleDisconnect(context.Background(), session.ID); err != nil {
			s.logger.Error().Err(err).Str("session_id", session.ID).Msg("harbor disconnect error")
		}

		s.logger.Info().
			Str("session_id", session.ID).
			Str("mount", mountPath).
			Msg("harbor source disconnected")
	}()

	// Hijack the connection to read raw audio bytes directly from the socket.
	// Nginx proxies streaming PUT requests with Content-Length: 0 (no chunked
	// Transfer-Encoding), which causes Go's http.Request.Body to return EOF
	// immediately. Hijacking bypasses Go's HTTP body handling entirely.
	hj, ok := w.(http.Hijacker)
	if !ok {
		s.logger.Error().Msg("harbor: ResponseWriter does not support hijacking")
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	hjConn, buf, err := hj.Hijack()
	if err != nil {
		s.logger.Error().Err(err).Msg("harbor: hijack failed")
		return
	}
	defer hjConn.Close()

	// Send a minimal HTTP response so nginx knows we accepted the stream.
	_, _ = hjConn.Write([]byte("HTTP/1.1 200 OK\r\nConnection: close\r\n\r\n"))

	// Always use the buffered reader — it wraps the raw connection and
	// ensures we don't lose any bytes that were read-ahead by the HTTP server.
	audioSource := io.Reader(buf.Reader)

	// Do a blocking test read to verify data is actually flowing.
	testBuf := make([]byte, 4096)
	hjConn.SetReadDeadline(time.Now().Add(10 * time.Second))
	n, readErr := audioSource.Read(testBuf)
	hjConn.SetReadDeadline(time.Time{}) // clear deadline

	s.logger.Info().
		Int("first_read_bytes", n).
		Err(readErr).
		Msg("harbor first read from hijacked connection")

	if n == 0 || readErr != nil {
		s.logger.Error().Err(readErr).Msg("harbor: no data from hijacked connection")
		return
	}

	// Prepend the test bytes we already read.
	audioSource = io.MultiReader(bytes.NewReader(testBuf[:n]), buf.Reader)

	// Inject live audio into the playout pipeline.
	s.streamAudio(connCtx, conn, mount, contentType, audioSource)
}

// streamAudio decodes compressed audio and injects raw PCM into the encoder pipeline.
func (s *Server) streamAudio(ctx context.Context, conn *SourceConnection, mount models.Mount, contentType string, audioSource io.Reader) {
	sampleRate := mount.SampleRate
	if sampleRate == 0 {
		sampleRate = 44100
	}
	channels := mount.Channels
	if channels == 0 {
		channels = 2
	}

	// Acquire the encoder's stdin from the playout director.
	encoderIn, release, err := s.director.InjectLiveSource(ctx, conn.StationID, mount.ID)
	if err != nil {
		s.logger.Error().Err(err).
			Str("session_id", conn.SessionID).
			Str("mount_id", conn.MountID).
			Msg("failed to inject live source into pipeline")
		return
	}
	defer release()

	// Start decoder: compressed audio → raw PCM.
	dec, err := startDecoder(ctx, s.cfg.GStreamerBin, contentType, sampleRate, channels, s.logger)
	if err != nil {
		s.logger.Error().Err(err).
			Str("session_id", conn.SessionID).
			Msg("failed to start harbor decoder")
		return
	}
	defer dec.Close()

	// Pipe decoded PCM from decoder stdout to encoder stdin.
	type copyResult struct {
		label string
		err   error
	}
	done := make(chan copyResult, 2)

	// Goroutine 1: decoder stdout → encoder stdin.
	go func() {
		_, err := io.Copy(encoderIn, dec.stdout)
		done <- copyResult{"decoder→encoder", err}
	}()

	// Goroutine 2: audio source (HTTP body) → decoder stdin.
	go func() {
		_, err := io.Copy(dec.stdin, audioSource)
		// Close decoder stdin to signal EOF to GStreamer.
		_ = dec.stdin.Close()
		done <- copyResult{"source→decoder", err}
	}()

	// Wait for either goroutine to finish (source disconnect or error).
	select {
	case <-ctx.Done():
		s.logger.Warn().Str("session_id", conn.SessionID).Msg("harbor connection context cancelled")
	case r := <-done:
		if r.err != nil {
			s.logger.Warn().Err(r.err).
				Str("session_id", conn.SessionID).
				Str("pipe", r.label).
				Msg("harbor stream pipe error")
		} else {
			s.logger.Info().
				Str("session_id", conn.SessionID).
				Str("pipe", r.label).
				Msg("harbor stream pipe closed (EOF)")
		}
	}

	// Log decoder stderr if it captured any errors.
	if stderrOutput := dec.Stderr(); stderrOutput != "" {
		s.logger.Warn().
			Str("session_id", conn.SessionID).
			Str("stderr", stderrOutput).
			Msg("harbor decoder stderr output")
	}
}

// parseBasicAuth extracts the password from a Basic auth header.
// The username is ignored (conventionally "source" for Icecast).
func (s *Server) parseBasicAuth(r *http.Request) (string, bool) {
	auth := r.Header.Get("Authorization")
	if auth == "" {
		return "", false
	}

	if !strings.HasPrefix(auth, "Basic ") {
		return "", false
	}

	decoded, err := base64.StdEncoding.DecodeString(auth[6:])
	if err != nil {
		return "", false
	}

	parts := strings.SplitN(string(decoded), ":", 2)
	if len(parts) != 2 {
		return "", false
	}

	// parts[0] = username (ignored), parts[1] = token
	return parts[1], true
}

// Addr returns the listen address of the harbor server.
func (s *Server) Addr() string {
	return fmt.Sprintf("%s:%d", s.cfg.Bind, s.cfg.Port)
}

// sourceMethodHandler wraps the standard http.Server to also accept the non-standard
// SOURCE method used by legacy Icecast clients. Go's HTTP server rejects unknown methods
// with 501 by default, so we intercept at the connection level.
type sourceMethodListener struct {
	net.Listener
}

func (l *sourceMethodListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return &sourceMethodConn{Conn: conn}, nil
}

// sourceMethodConn peeks at the first bytes to detect the SOURCE method and
// rewrites it to PUT so the standard HTTP parser can handle it.
type sourceMethodConn struct {
	net.Conn
	reader *bufio.Reader
	once   sync.Once
}

func (c *sourceMethodConn) Read(b []byte) (int, error) {
	c.once.Do(func() {
		c.reader = bufio.NewReaderSize(c.Conn, 4096)

		// Peek to see if this is a SOURCE request.
		peek, err := c.reader.Peek(7)
		if err != nil {
			return
		}
		if string(peek) == "SOURCE " {
			// Read and discard "SOURCE "
			buf := make([]byte, 7)
			_, _ = c.reader.Read(buf)
			// Prepend "PUT " for the HTTP parser
			c.reader = bufio.NewReaderSize(
				io.MultiReader(strings.NewReader("PUT "), c.reader),
				4096,
			)
		}
	})
	return c.reader.Read(b)
}

// ListenAndServeWithSOURCE starts the harbor server with support for the
// non-standard SOURCE HTTP method used by legacy Icecast clients.
func (s *Server) ListenAndServeWithSOURCE() error {
	addr := fmt.Sprintf("%s:%d", s.cfg.Bind, s.cfg.Port)

	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleSource)

	s.httpServer = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       0,
		WriteTimeout:      0,
		IdleTimeout:       60 * time.Second,
	}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("harbor listen: %w", err)
	}

	s.logger.Info().Str("addr", addr).Msg("harbor server starting (with SOURCE method support)")
	err = s.httpServer.Serve(&sourceMethodListener{Listener: ln})
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}
